# Chitin Governance — OpenClaw Plugin

> Execution kernel for AI coding agents. One install gates every tool call openclaw orchestrates.

This plugin attaches **chitin** to openclaw's lifecycle hooks. Once enabled, every tool call dispatched by openclaw's native agent runtime (the pi-agent-core harness) is evaluated by chitin's policy engine before it runs. Allowed calls proceed; denied calls return to the agent with a structured reason. Every decision lands in chitin's hash-linked event chain and (if configured) projects to OTEL.

## What you get

- **`before_tool_call` gate** — every tool call dispatched by openclaw's pi-agent-core harness is run past `chitin-kernel gate evaluate` before execution. Block / allow / params-rewrite all supported.
- **`subagent_spawning` gate** — block disallowed subagents (e.g., enforce that `claude-code` cannot be spawned as a worker driver under Anthropic ToS).
- **`before_install` gate** — block plugin/skill installs by class (e.g., `git`-kind installs in worker mode).
- **Hash-linked event chain** — chitin records every gate decision with a deterministic hash chain (audit-grade replay).
- **OTEL projection** — if chitin is configured to emit OTEL, every gate decision becomes a span in your existing observability stack.

> Slice 3: a post-tool-result emit path (`registerAgentToolResultMiddleware` → `chitin-kernel emit` v2 `post_tool_use`) lands once the kernel exposes a streaming emit subcommand. The slice 2 plugin only emits `gate.decision` events from `before_tool_call`.

## Install

This plugin requires the [`chitin-kernel`](https://github.com/chitinhq/chitin) binary on your `PATH` (or set `kernelPath` in the plugin config).

```bash
# from a local source tree (development)
openclaw plugins install --link --dangerously-force-unsafe-install /path/to/openclaw-plugin-governance

# from npm (once published)
openclaw plugins install @chitinhq/openclaw-plugin-governance
```

The `--dangerously-force-unsafe-install` flag is currently required because the plugin shells out to the `chitin-kernel` binary via `child_process.spawn` — that's the integration mechanism, not a smell. (The plugin marketplace allowlist will resolve this in a future release.)

After install, openclaw rewrites `~/.openclaw/openclaw.json` to wire the plugin in. Restart the gateway to apply changes if you're running one.

## Configure

Plugin config goes under `plugins.entries.chitin-governance` in `openclaw.json`:

```json
{
  "plugins": {
    "allow": ["chitin-governance"],
    "entries": {
      "chitin-governance": {
        "enabled": true,
        "kernelPath": "chitin-kernel",
        "mode": "enforce",
        "workerMode": false,
        "denyOnError": true,
        "timeoutMs": 5000
      }
    }
  }
}
```

| Key | Default | Meaning |
|-----|---------|---------|
| `kernelPath` | `chitin-kernel` | Absolute path to the chitin-kernel binary. Defaults to a PATH lookup. |
| `mode` | `enforce` | `enforce` — deny disallowed tool calls. `observe` — log only, never block (flagged as a dangerous opt-out in `configContracts.dangerousFlags`). Default flipped to `enforce` in slice 3 once chitin's normalizer covered all 19 pi-runtime tools. |
| `workerMode` | `false` | Apply chitin's worker bootstrap rules (no-trunk-write, no-pr-merge, no-recursive-delete, ToS-driver allowlist). |
| `denyOnError` | `true` | Fail-closed when the kernel binary is missing or times out. Set to `false` for fail-open (NOT recommended outside development). |
| `timeoutMs` | `5000` | Per-call gate timeout in milliseconds. |

You also need a `chitin.yaml` policy file in (or above) the working directory openclaw runs from. See [chitin's docs](https://github.com/chitinhq/chitin) for policy authoring. A minimal starter:

```yaml
id: my-policy-v1
mode: enforce
rules:
  - id: no-destructive-rm
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "Recursive delete blocked"
```

## What this plugin does NOT cover

The pre-tool gate fires for tool calls dispatched by **openclaw's own pi-agent-core harness** — local agents driven via providers like ollama, anthropic, openai, etc. through openclaw's native runtime.

Tool calls inside an **acpx-spawned subprocess** (Claude Code interactive sessions, Codex via ACP, Copilot CLI v1) do *not* traverse this hook surface — those subprocesses have their own tool runtimes inside the spawned process. For those vendors, use chitin's per-vendor shims:

- **Claude Code** — chitin's [`PreToolUse` hook driver](https://github.com/chitinhq/chitin) (lives in the Claude Code config, not in openclaw).
- **Copilot CLI v1** — `chitin-kernel drive copilot` shim.
- **Copilot v2 (open SDK)** — `joinSession`-based extension (in development).

This plugin is one of three integration shapes. It's the cleanest one — but it's scoped.

## Verifying it works

```bash
# 1. Confirm plugin loads. Look for a "registering" log line on any openclaw command:
openclaw agents list
# expected: "[plugins] chitin-governance registering: kernelPath=chitin-kernel mode=enforce workerMode=false"

# 2. Run a one-shot agent turn that triggers a tool call:
openclaw agent --local --agent main --json --message "Use bash to run: pwd"
# expected: chitin gate evaluation in chitin's chain log; agent receives the tool result

# 3. Trigger a denial (assumes chitin.yaml has a no-rm-rf rule in cwd):
openclaw agent --local --agent main --message 'Use bash to delete /tmp/foo recursively'
# expected: tool call blocked; agent surfaces "denied by chitin policy" back as the result
```

## License

MIT. See `LICENSE`.
