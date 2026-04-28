---
date: 2026-04-28
status: deferred (again, new failure mode)
predecessor: docs/observations/2026-04-20-sp1-dogfood-gate.md
workstream: docs/superpowers/specs/2026-04-20-otel-genai-ingest-workstream-design.md (SP-1)
---

# SP-1 Dogfood Gate — retry, 2026-04-28

The 2026-04-21 deferral was unblocked on one axis (model availability) but
re-blocked on a different axis (gateway unit + empty diagnostic events).
Box now produces successful agent turns end-to-end but emits zero non-empty
OTLP bodies, so SP-1's success criterion ("first non-empty `v1/traces-*.pb`
captured and ingested") is still not met.

## What changed since 2026-04-21

1. `~/.openclaw/agents/main/agent/models.json` no longer hard-codes
   `qwen2.5-coder:7b`. The `ollama` provider now lists `qwen3-coder:30b`,
   `qwen2.5:0.5b`, `gemma4`, `gemma4:latest`, plus four cloud-bridged
   entries. Both local-tag entries are pulled (`ollama list` confirms
   `qwen3-coder:30b` 18 GB, `qwen2.5:0.5b` 397 MB).
2. The original SP-0/SP-1 unblock paths (pull the 7b tag *or* surgery
   `models.json` with cache invalidation) are therefore both moot —
   the config no longer references the missing tag and the cached
   profile (`sha256:9c018ec112cf`) was either invalidated by the
   models.json edit or is irrelevant on this code path.
3. `~/.openclaw/openclaw.json` still had `diagnostics.otel.enabled=false`
   (SP-0's Option 2 revert was never reverted). That is a one-line edit
   when needed; left disabled at the end of this session.

## New blocker A — systemd unit pointed at a non-existent user

`systemctl --user is-active openclaw-gateway` showed `activating
(auto-restart)` with restart counter at **77,799** when this session
started. The journal showed the unit looping every 5 s on:

```
openclaw-gateway.service: Failed to load environment files: No such file or directory
openclaw-gateway.service: Failed to spawn 'start' task: No such file or directory
openclaw-gateway.service: Failed with result 'resources'.
```

Cause: the unit at `~/.config/systemd/user/openclaw-gateway.service`
and its drop-in `~/.config/systemd/user/openclaw-gateway.service.d/env.conf`
contained `/home/jared/...` paths everywhere — `ExecStart=`,
`Environment=PATH=`, `Environment=HOME=`, and the drop-in's
`EnvironmentFile=/home/jared/workspace/.env`. This box has no
`/home/jared` directory; the only user is `red`. Provenance of the
`/home/jared` paths is unknown — the unit was working at SP-0/SP-1
time (both observations record `is-active=active`), so something
between 2026-04-21 and 2026-04-28 rewrote the unit to point at a
user that doesn't exist.

### Fix applied

Mechanical `sed -i.chitin-bak 's|/home/jared|/home/red|g'` against both
files, followed by `systemctl --user daemon-reload` and `restart`.
Backup files left at `*.chitin-bak` for rollback. Gateway came up
active on the first restart after `daemon-reload`.

This fix is outside the chitin repo; it lives in `~/.config/systemd/user/`.
If openclaw's installer overwrites the unit on next upgrade the bug
will return — and if so, that is an upstream report worth filing.

## New blocker B — diagnostic events never fire on this code path

With the gateway active, `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318`,
the SP-0 receiver re-stood-up at `/tmp/otel-capture/receiver.py`, and
`diagnostics.otel.enabled=true`, the run

```
openclaw agent --agent main -m "say hi in one word"
```

succeeded (model returned `Hi`), but the receiver captured:

- 0 POSTs to `/v1/traces`
- ≥10 POSTs to `/v1/metrics` — every body 0 bytes
- 2 POSTs to `/v1/logs` — every body 0 bytes

This is the same 0-byte pattern SP-0 saw, and the same SP-0 explanation
applies: the OTLP exporter SDK is flushing empty batches every
`flushIntervalMs` (2 s). What is missing is the upstream
`emitDiagnosticEvent({type: "model.usage"})` call that would put a
real span/metric in the batch. The plugin source confirms the chain:

```js
// agent-runner.runtime: only emits on non-zero usage
if (isDiagnosticsEnabled(cfg) && hasNonzeroUsage(usage)) {
    emitDiagnosticEvent({ type: "model.usage", ... usage: { input, output, ... } });
}

// diagnostics-otel index.js: model.usage event drives both metrics and span
case "model.usage":
    recordModelUsage(evt);
// inside recordModelUsage:
//   tokensCounter.add(usage.input, ...) etc
//   if (!tracesEnabled) return;
//   spanWithDuration("openclaw.model.usage", ..., evt.durationMs).end();
```

So the run that produced `Hi` did not satisfy `hasNonzeroUsage(usage)`.
The most likely cause is the local ollama provider not returning usage
fields the agent-runner can see — local ollama responses don't always
carry `prompt_eval_count` / `eval_count` in the shape openclaw's
provider adapter looks for. Untested but plausible alternates:
diagnostics is gated by something in `cfg` other than the otel block,
or this gateway-mode run takes a path that doesn't reach
`agent-runner.runtime` at all.

The SP-1 instrumentation observation
(`docs/observations/2026-04-20-openclaw-otel-capture.md`) noted this
risk indirectly: *"openclaw only instruments success paths"* —
extended now to *"openclaw only instruments success paths whose
provider returns usage fields."*

## Unblock candidates (ordered by cost)

Each of these would be sufficient on its own to capture a real
non-empty `v1/traces-*.pb`:

1. ~~**Cloud provider with reliable usage reporting.**~~ **Tried;
   still zero traces.** With the patched systemd drop-in pulling
   `OLLAMA_CLOUD_API_KEY` from `/home/red/workspace/.env`, the gateway
   process now has the key (verified via `/proc/<pid>/environ`); the
   default `agents.defaults.model.primary = ollama/glm-5.1:cloud`
   route succeeded (model returned `Hey`); same 0-byte heartbeat
   pattern; no `/v1/traces` POSTs. The blocker is therefore *not* the
   provider's usage shape — it is upstream of the
   `emitDiagnosticEvent` call itself. Either `agent-runner.runtime`
   is bypassed for `--agent main` runs (gateway routes through a
   different code path), or `isDiagnosticsEnabled(cfg)` reads a
   different field than the one in `~/.openclaw/openclaw.json`. A
   deeper plugin-loading investigation is needed before any candidate
   below is worth trying.

2. **DeepSeek.** API key is already present in
   `~/.openclaw/agents/main/agent/auth-profiles.json` under
   `deepseek:default`. Same `usage` shape as #1. Smallest config
   delta to test the diagnostic path.

3. **Local ollama with verified usage shape.** Capture the raw
   ollama response from a `qwen3-coder:30b` run and confirm whether
   it carries `prompt_eval_count` + `eval_count` in the JSON; if it
   does, the gap is in openclaw's provider adapter mapping ollama
   counts to the canonical `usage` object — a deeper investigation,
   not a one-line fix.

4. **Bypass the gate**: skip the dogfood gate entirely, treat SP-1
   as shipped on synthesized fixtures only (where it already passes
   294 Go tests + 44 TS tests), and revisit when a real production
   stream becomes available — e.g., when the talk demo or
   embedded-agent runtime emits real `openclaw.model.usage` spans
   under different conditions.

## Box state at end of session

- `~/.openclaw/openclaw.json` — `diagnostics.otel.enabled=false`
  (reverted from `true` to avoid spam-logging connection errors to a
  stopped receiver).
- `~/.config/systemd/user/openclaw-gateway.service` — patched to
  `/home/red` paths; backup at `*.chitin-bak`.
- `~/.config/systemd/user/openclaw-gateway.service.d/env.conf` —
  patched to `EnvironmentFile=/home/red/workspace/.env`; backup at
  `*.chitin-bak`.
- `openclaw-gateway.service` — active, no longer restart-looping.
- `/tmp/otel-capture/receiver.py` — restored from the SP-0 fixture
  (`docs/observations/fixtures/2026-04-20-openclaw-otel-capture/receiver.py`).
- Receiver is **not running**.
- All 0-byte capture artefacts deleted; no real-capture artefact
  produced.
- `local main` updated to include the SP-2 merge (`ca6a7c3`, PR #36).

## Impact on SP-1 / SP-2

Same as the 2026-04-21 deferral: none on correctness. The translator
still has full unit + integration + CLI coverage against the SP-0-
source-derived synthesized fixture (now at `sp1/synthesized-model-usage.pb`,
plus SP-2's `sp2/` extensions). The "real-capture artefact"
(`real-model-usage.pb`) is still uncommitted. Either of unblock
candidates 1–3 is a single Task-8-shape rerun once taken.
