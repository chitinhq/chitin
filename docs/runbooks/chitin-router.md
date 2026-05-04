# chitin-router runbook

> The router wraps the kernel's `PreToolUse` hook with a heuristic
> + advisor pipeline. When enabled, it scores each tool call for
> blast radius / floundering / drift; if any heuristic fires, it
> consults a higher-tier advisor (via `claude -p`) for a nudge or
> takeover verdict.
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
| `copilot` | n/a — wrapping driver | (in-kernel) | `chitin-kernel drive copilot` |
| `local-*` (qwen/glm/glm-flash/deepseek) | `before_tool_call` plugin | openclaw plugin config | openclaw-side |

The `chitin-router-hook` shim is the same binary across all hook-
surface drivers; per-vendor tool-name normalization lives in
`internal/driver/<vendor>/normalize.go` (claudecode, codex, gemini).

## What it does

For each `PreToolUse` hook call from any chitin-governed agent:

1. **Kernel verdict (deterministic)** — `chitin-kernel gate evaluate
   --hook-stdin`. If the kernel denies, the router returns the deny
   immediately.
2. **Heuristics** — pure functions over the hook input + recent chain
   events:
   - `blast_radius` — scores actions on reversibility / scope /
     visibility / counterparties (0.0–1.0; > threshold = fired)
   - `floundering` — detects looping tool calls, stalled progress,
     denial cascades over the agent's chain
3. **Advisor (optional)** — if any heuristic fires AND the policy
   triggers match, calls `claude -p` with a structured prompt
   asking for a nudge + verdict (continue / takeover) + escalate
   bool. The advisor's response is composed into the hook reply.
4. **Compose** — kernel decision + advisor nudge → Claude Code
   sees the nudge in the tool-call result and continues.

Hot path performance: when the sentinel file is absent, the
wrapper is a single `exec chitin-kernel` — adds ~0ms. When the
sentinel is present, the TS pipeline runs (~500ms cold, but most
tool calls don't fire heuristics so the advisor isn't invoked).

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

Smoke a high-blast action (should fire blast_radius + try advisor):

```bash
echo '{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git push --force origin main"},"cwd":"'"$PWD"'","session_id":"smoke"}' \
  | ~/workspace/chitin/bin/chitin-router-hook --agent=smoke
# Expect: {"decision":"allow|deny","message":"<nudge>","source":"advisor-allow|advisor-deny"}
# (Takes ~30-60s if advisor is reachable.)
```

If the advisor errors / times out, the wrapper falls through to
the kernel verdict (fail-open) — the agent isn't blocked.

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
  advisor:
    enabled: true
    when:                           # which signals trigger the advisor
      - blast_radius_above_threshold
      - floundering_detected
      - kernel_denied               # advise on every deny too
    chain:
      max_depth: 3                  # cap on advisor-chain depth
      tier_steps: [T2, T3, T4]      # tier ladder for chained advice
    model: claude-code-headless     # advisor model id
```

Most edits don't require restarting anything — the TS hook reads
`chitin.yaml` per-call (with a small cache, but cache is per-
process and the hook is per-process).

## Operate

Live tail of router decisions (the slow path emits structured JSON
to stderr; journald captures it for the systemd-driven hooks):

```bash
journalctl --user -u chitin-worker -f | grep '"component":"router-'
```

Read the per-workflow shared memory (advisor nudges):

```bash
cat ~/.chitin/shared-memory/<workflow-id>.json | jq .
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
| Advisor (`claude -p`) times out | Wrapper logs warn, returns kernel verdict only. Agent gets no nudge but isn't blocked. | Increase `--timeoutMs` in `advisor.ts` (default 60s); verify Claude Code auth. |
| `chitin.yaml` unreadable / missing `router:` | Wrapper uses `DEFAULT_ROUTER_POLICY` (advisor disabled). | Add or fix the section. |
| TS hook crashes | Wrapper catches, falls open (allow). Agent unaffected. | Check stderr; report bug. |

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
