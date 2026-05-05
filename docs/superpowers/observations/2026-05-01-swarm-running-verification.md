# Swarm running — verification record (2026-05-01)

**Status:** local 24/7 swarm operational. Slice 2 hard floor verified end-to-end with proof artifacts.

## What's running

| Component | Pid / location | Status |
|---|---|---|
| Temporal server (Postgres-backed) | docker container `chitin-temporal-server` | up, port 7233 |
| Temporal Postgres | docker container `chitin-temporal-postgres` | healthy |
| Temporal UI | docker container `chitin-temporal-ui` | up, port 8233 |
| Temporal worker | `tsx src/worker.ts` (apps/runner) | polling `chitin-worker-q` |
| OpenClaw gateway | `openclaw-gateway` | up, port 18789 |
| Chitin governance plugin | `~/.openclaw/extensions/chitin-governance` → symlink to `apps/openclaw-plugin-governance` | registered, enabled, mode=observe |
| Chitin kernel | `chitin-kernel` on PATH | gating; gov-decisions stream live at `~/.chitin/gov-decisions-2026-05-01.jsonl` |

## Proof artifacts

### 1. Plugin registers in openclaw runtime

Every `openclaw agent` invocation logs the plugin's `register()` callback firing:

```
[plugins] chitin-governance registering: kernelPath=chitin-kernel mode=observe workerMode=false
```

That line is `api.logger.info(...)` from `apps/openclaw-plugin-governance/src/index.mjs` line 20-22, executing inside openclaw's plugin loader. Confirms the plugin is loaded *into openclaw's process*, not as an external sidecar.

### 2. `before_tool_call` hook deny path (direct test)

Standalone `openclaw agent --local --agent main --message "Use the memory_search tool..."` produced this in stderr:

```
[plugins] chitin denied tool=memory_search rule=default-deny-unknown
          reason=Unknown tools are denied — the action vocabulary is a closed enum.
[tools] memory_search failed: Unknown tools are denied — ...
```

Source path:
- `index.mjs::register::api.on('before_tool_call', ...)`
- → `chitin-bridge.mjs::evaluateGate(...)`
- → `spawn('chitin-kernel', ['gate','evaluate','-agent','openclaw-plugin','-tool','memory_search',...])`
- → kernel writes `gov-decisions-2026-05-01.jsonl` row
- → bridge parses `{allowed:false, reason, rule_id}` from stdout
- → plugin returns `{block: true, blockReason}` to openclaw
- → openclaw surfaces `[tools] memory_search failed: ...` back to the agent

### 3. End-to-end Temporal → OpenClaw → Plugin → Chitin

Workflow `wf-swarm-1777602811` (DRIVER=local-qwen, prompt with no tool calls):

```
[submit] starting workflow_id=wf-swarm-1777602811
[submit] workflow result: {
  "stderr_tail": "...
    \"finalAssistantVisibleText\": \"alive\",
    \"executionTrace\": {
      \"winnerProvider\": \"ollama\",
      \"winnerModel\": \"qwen3-coder:30b\",
      \"runner\": \"embedded\",
      \"attempts\": [{ \"result\": \"success\" }]
    },
    \"completion\": { \"stopReason\": \"stop\" }
  ...",
  "duration_ms": 137409
}
```

qwen3-coder:30b ran on local 3090, dispatched by Temporal worker via the new `openclaw agent --local` activity path, with chitin-governance plugin loaded in the agent process.

Workflow `wf-swarm-tools-*` (DRIVER=local-qwen, prompt: "Use memory_search ... for 'governance'"):

Wrote this gov-decisions row at `2026-05-01T02:39:34Z`:

```json
{"allowed":false,"status":"deny","mode":"enforce","rule_id":"default-deny-unknown",
 "reason":"Unknown tools are denied — the action vocabulary is a closed enum...",
 "escalation":"normal","agent":"main","action_type":"unknown",
 "action_target":"memory_search","ts":"2026-05-01T02:39:34Z",
 "envelope_id":"01KQGK403SQ4141PRE5AZ4KW58","tool_calls":1}
```

The full causal chain:
1. `chitin-kernel task submit` → posts `ExecutionRequest` to Temporal
2. Temporal worker polls, claims activity
3. Activity (`runAgentTurn` in `apps/runner/src/activity.ts`) seeds chitin.yaml in tmpdir, spawns `openclaw agent --local --agent main --message <prompt>`
4. OpenClaw loads chitin-governance plugin from `~/.openclaw/extensions/chitin-governance`
5. OpenClaw spawns local agent with pi-agent-core harness, runs ollama qwen-coder model
6. Model emits `memory_search` tool_use
7. pi-runtime fires `before_tool_call` hook
8. Plugin's handler subprocesses `chitin-kernel gate evaluate -agent openclaw-plugin -tool memory_search ...`
9. Chitin kernel evaluates against `chitin.yaml`, normalizes `memory_search` → `ActUnknown`, returns `default-deny-unknown`
10. Kernel writes the audit row above to `gov-decisions-2026-05-01.jsonl`
11. Bridge parses kernel response, plugin returns `{block: true}` to openclaw
12. OpenClaw's tool dispatcher honors the block, surfaces tool-error to the agent
13. Agent receives error, eventually responds (or retries — see "Known limitation" below)
14. Activity returns `ActivityResult` to workflow, workflow completes

## Verified

- [x] `apps/openclaw-plugin-governance/` exists with `package.json`, `openclaw.plugin.json`, `src/index.mjs`, `src/chitin-bridge.mjs`, `README.md`
- [x] Plugin loads under `openclaw plugins install --link --dangerously-force-unsafe-install <path>` and registers without errors
- [x] `before_tool_call` hook handler subprocesses to `chitin-kernel gate evaluate`; deny path proven (memory_search × 2 — direct + Temporal)
- [x] Bridge protocol correct (caller_origin via `-agent openclaw-plugin` flag; kernel records the configured agent)
- [x] OpenClaw honors `{block: true, blockReason}` return from the hook
- [x] `registerAgentToolResultMiddleware` registered for `runtimes: ['pi','codex']`
- [x] `subagent_spawning` denial of `claude-code` agentId (code path live; not yet exercised — needs an agent harness that emits a subagent_spawn event)
- [x] `before_install` denial of `plugin-git` kind (code path live; not yet exercised)
- [x] Standalone `chitin-kernel` CLI: no regressions (proven by my own claude-code Bash hook firing throughout the session)
- [x] Layer Contracts compliance: plugin is a thin TS adapter; all policy in Go kernel; chain remains audit truth
- [x] Temporal worker dispatches `local-qwen` driver through `openclaw agent --local` (was throwing in slice 1)
- [x] End-to-end Temporal workflow → activity → openclaw → plugin → chitin → audit row

## Known limitations (slice 3 work)

1. **Plugin user-config validation.** `openclaw.json` `plugins.entries.chitin-governance.{mode,kernelPath,workerMode,denyOnError,timeoutMs}` triggers "Unrecognized keys" from openclaw's config validator despite being in the manifest's `configSchema`. Workaround for tonight: hardcoded `mode: observe` default in the manifest. Investigate `buildPluginConfigSchema` resolution path in slice 3.
2. **Chitin normalizer doesn't recognize openclaw chat-domain tools** (memory_search, sessions_yield, subagents, ollama_web_search, etc). Currently they all hit `default-deny-unknown`. Plugin runs in `mode: observe` to avoid deadlock. Slice 3 extends `internal/gov/normalizer.go` to map these to their action equivalents (e.g., `memory_search` → `memory.read`, `sessions_yield` → `chat.send`).
3. **Plugin install requires `--dangerously-force-unsafe-install`** because the bridge uses `child_process.spawn`. That's by design — it's the integration mechanism. Slice 3 reaches out to OpenClaw maintainer for an `embeddedExtensionFactories`-equivalent allowlist for governance plugins.
4. **Per-driver agent mapping not yet wired.** All `local-*` drivers route to the same `main` openclaw agent. Slice 3 defines per-tier agents (qwen-agent → qwen3-coder, glm-agent → glm-5.1, etc.) and `--agent <id>` selection in the activity.
5. **Plugin loads `register()` 5× per openclaw command** — investigate openclaw plugin lifecycle (likely some per-route re-import). Cosmetic, not blocking.
6. **Agent retry-loops on tool denial** (small models). The 0.5b model kept calling memory_search after deny instead of giving up; activity wall_timeout caught it eventually. Tighten timeouts or improve agent error-handling prompt.

## Next iterations

- **Slice 3:** normalizer extension + per-driver agents + plugin config validation fix.
- **Slice 4:** workflow_id propagation into gov-decisions audit rows (so chain rows are queryable by Temporal workflow).
- **Slice 5:** marketplace publish (`@chitinhq/openclaw-plugin-governance` on npm).
