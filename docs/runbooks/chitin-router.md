# chitin-router runbook

> The router wraps the kernel's `PreToolUse` hook with advisory
> signal stamping. When enabled, it scores each tool call for blast
> radius / floundering / drift and writes a second `router.signal`
> decision row to the chain. The router never spawns an LLM in the
> gate; downstream consumers such as hermes can read the signal rows
> and decide what to do.
>
> **Off by default** — operator opts in via a sentinel file. Hot
> path stays fast (kernel only) until then.

## Supported agents

The router-hook shim works for any agent whose hook surface speaks
the Claude Code PreToolUse wire shape (or one byte-compatible with
it). As of 2026-05-04 that's all of:

| Driver | Hook event | Config file | Installer |
|---|---|---|---|
| `claude-code-headless` | `PreToolUse` | `~/.claude/settings.json` | `chitin-kernel install --surface claude-code` |
| `codex` | `PreToolUse` (codex 0.128.0+) | `~/.codex/config.toml` (`[features] codex_hooks=true`) | `scripts/install-codex-hook.sh` |
| `gemini` | `BeforeTool` (same wire shape; renamed event) | `~/.gemini/settings.json` | `scripts/install-gemini-hook.sh` |
| `hermes` | `pre_tool_call` (same wire shape) | `~/.hermes/config.yaml` | `scripts/install-hermes-hook.sh` |
| `copilot` | n/a — wrapping driver | (in-kernel) | `chitin-kernel drive copilot` |
| `local-*` (qwen/glm/glm-flash/deepseek) | `before_tool_call` plugin | openclaw plugin config | openclaw-side |

The `chitin-router-hook` shim is the same binary across all hook-
surface drivers; per-vendor tool-name normalization lives in
`internal/driver/<vendor>/normalize.go` (claudecode, codex, gemini,
hermes).

## What it does

For each `PreToolUse` hook call from any chitin-governed agent:

1. **Kernel verdict (deterministic)** — `chitin-kernel gate evaluate
   --hook-stdin`. If the kernel denies, the router returns the deny
   immediately.
2. **Signals** — pure functions over the hook input + recent chain
   events:
   - `blast_radius` — scores actions on reversibility / scope /
     visibility / counterparties (0.0–1.0; > threshold = fired)
   - `floundering` — detects looping tool calls, stalled progress,
     denial cascades over the agent's chain
   - `drift` — detects high-risk actions that diverge from the
     recent task/session context
3. **Plugins (optional)** — operator-declared subprocess checks can
   add advisory scores. Pre-action plugins may return `block=true`
   for deterministic checks.
4. **Stamp** — non-zero signal scores are written as a second
   `gov.Decision` row with action type `router.signal`.
5. **Return** — the kernel verdict is emitted unchanged. Heuristics
   do not deny or allow; kernel policy and pre-action plugin blocks
   are the only authoritative verdicts.

Hot path performance: when the sentinel file is absent, the
wrapper is a single `exec chitin-kernel` — adds ~0ms. When the
sentinel is present, the Go router evaluates heuristics in-process.

## Install

The router lives in the chitin repo — no separate install needed.
Activity provisioning (`writeWorktreeClaudeSettings`) writes the
hook config pointing at `chitin-router-hook` automatically.

To **enable**:

```bash
mkdir -p ~/.chitin
touch ~/.chitin/router-enabled
```

To **disable** (instant — no config edit needed):

```bash
rm ~/.chitin/router-enabled
```

## Verify

Smoke a low-blast action (should pass-through to kernel):

```bash
echo '{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/tmp/x.txt"},"cwd":"'"$PWD"'","session_id":"smoke"}' \
  | ~/workspace/chitin/bin/chitin-router-hook --agent=smoke
# Expect: {"decision":"allow","source":"kernel-allow"}
```

Smoke a high-blast action (should fire blast_radius and stamp a
router signal row):

```bash
echo '{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git push --force origin main"},"cwd":"'"$PWD"'","session_id":"smoke"}' \
  | ~/workspace/chitin/bin/chitin-router-hook --agent=smoke
# Expect: the kernel verdict on stdout; router telemetry on stderr.
```

Signal rows are written to the same chain as normal kernel
decisions. Use the chain readers to inspect `router.signal` rows.

## Configure

Policy lives in `chitin.yaml` under the `router:` section:

```yaml
router:
  enabled: true                     # toggle the policy layer
  heuristics:
    blast_radius:
      enabled: true
      threshold: 0.6                # 0.0–1.0; lower = more sensitive
    floundering:
      enabled: true
      max_loop_count: 3             # same tool+target N times → fire
      max_stall_seconds: 600        # no writes in N seconds → fire
  plugins:
    - name: tests-required-before-commit
      type: pre-action
      runtime: python3
      module: scripts/check-tests-before-commit.py
      timeout_ms: 750
      allowlist_paths: ["."]
```

Most edits don't require restarting anything — the router reads
`chitin.yaml` per-call (with a small cache, but cache is per-
process and the hook is per-process).

## Operate

Live tail of router decisions (the slow path emits structured JSON
to stderr; journald captures it for the systemd-driven hooks):

```bash
journalctl --user -u chitin-worker -f | grep '"component":"router-'
```

Read stamped router signal rows:

```bash
chitin-kernel chain stats --action router.signal
```

Disable a heuristic without touching policy YAML — set the env var
override on the worker:

```bash
# (Future — not yet wired; today edit chitin.yaml.)
```

## Failure modes

| Failure | Impact | Fix |
|---|---|---|
| `chitin-kernel` binary missing | Wrapper logs warn, falls open (allow). Agent unaffected. | Rebuild kernel; `chitin-kernel-redeploy.timer` should catch this. |
| Plugin times out or errors | Router logs warn and ignores that plugin result. | Fix the plugin command or lower its scope. |
| `chitin.yaml` unreadable / missing `router:` | Wrapper uses `DefaultPolicy` (router disabled). | Add or fix the section. |
| Router hook crashes | Wrapper returns a non-blocking hook error. Agent unaffected. | Check stderr; report bug. |

## Why a sentinel file (not a config flag)

Two reasons:
1. **Instant disable** — `rm ~/.chitin/router-enabled` doesn't
   require restarting a worker or reloading config. The next hook
   call sees no sentinel → fast path.
2. **Hot-path performance** — if disabled, the bash shim short-
   circuits to the kernel directly (microsecond cost).

The Go SDK port shipped (PR #231) — heuristics now run in-process
in the kernel binary at ~26ms cold start instead of ~500ms via
`pnpm tsx`. The sentinel still exists because it's the operator-
side instant-disable; the slow-path performance just isn't a
reason to keep it anymore.

## Related

- Strategic entry: `agent-router-architecture` (in
  `docs/swarm-backlog.md`)
- Auto-flipper companion: `chitin-shipped-entry-flipper` (separate
  systemd timer, separate concern — backlog hygiene)
- Kernel binary: `chitin-kernel-redeploy.timer` keeps the
  underlying gate fresh; rebuild script at
  `scripts/install-kernel.sh`
