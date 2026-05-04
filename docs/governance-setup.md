# Chitin Governance Setup

The `gov.Gate.Evaluate(action, agent) → Decision` API is the single enforcement point. Every tool call across every driver evaluates against `chitin.yaml`. Five install paths — one per supported vendor surface — wire this API into the driver's tool-call lifecycle.

## Install paths by driver

```
                 chitin.yaml (single policy)
                          │
                          ▼
                  gov.Gate.Evaluate ◄──────────────┐
       ▲       ▲      ▲       ▲       ▲             │
       │       │      │       │       │             │
   Claude   Codex  Gemini   Copilot  openclaw       │
    Code     CLI    CLI      CLI      plugin        │
   PreToo  PreToo  BeforeT   SDK      before_       │
   Use     Use     ool       wrap     tool_call     │
   hook    hook    hook                             │
       │       │      │       │       │             │
       ▼       ▼      ▼       ▼       ▼             │
                   tool calls  ───────────────────  │
```

### 1. Claude Code (PreToolUse hook)

```bash
chitin-kernel install --surface claude-code --global
```

Writes a `PreToolUse` entry to `~/.claude/settings.json` matching the nested
`{matcher, hooks:[{type, command}]}` schema. The hook execs the router-hook
shim with the hook payload on stdin.

To uninstall:

```bash
chitin-kernel uninstall --surface claude-code --global
```

### 2. Codex CLI (PreToolUse hook — codex 0.128.0+)

```bash
bash scripts/install-codex-hook.sh
```

Writes a `[features] codex_hooks=true` block + `[[hooks.PreToolUse]]` entry into `~/.codex/config.toml`. Wire shape is byte-compatible with Claude Code's PreToolUse — only the per-tool name set differs (codex emits `Bash`, `apply_patch`, MCP tool names). The router-hook shim handles the protocol; per-tool normalization in `internal/driver/codex/normalize.go`.

### 3. Gemini CLI (BeforeTool hook — same wire shape, different event name)

```bash
bash scripts/install-gemini-hook.sh
```

Writes a `BeforeTool` block to `~/.gemini/settings.json`. Stdin payload is byte-identical to Claude Code's PreToolUse (gemini even sets `CLAUDE_PROJECT_DIR`/`CLAUDE_CODE_ENTRYPOINT` envvars for compatibility); only the per-tool name set differs (`run_shell_command`, `read_file`, `edit`, `replace`, `write_file`, `web_fetch`, `google_web_search`, etc.). Per-tool normalization in `internal/driver/gemini/normalize.go`.

Both `install-codex-hook.sh` and `install-gemini-hook.sh` are idempotent and refused-on-malformed-config; they run after each `install-kernel.sh` invocation so kernel rebuilds refresh the hook wiring automatically.

### 4. Copilot CLI (in-kernel driver — wrapping orchestrator)

```bash
chitin-kernel drive copilot "<prompt>"
```

The kernel spawns Copilot CLI as a child of a chitin-driven harness (closed-vendor pattern: see [architecture.md](./architecture.md#vendor-integration-patterns-open-vs-closed-vendor)). Tool calls are gated via the SDK; chitin enforces the same `gov.Gate` policy.

### 5. openclaw (`local-*` drivers via `before_tool_call` plugin)

```yaml
# ~/.config/openclaw/openclaw.json
plugins:
  allow:
    - chitin-governance     # ships with chitin; loaded at openclaw startup
```

The plugin is loaded by openclaw at startup; every tool call dispatched by openclaw-managed agents (qwen / glm / glm-flash / deepseek) passes through `before_tool_call` → `chitin-kernel gate evaluate`. Same policy file.

## Build the kernel

```bash
cd go/execution-kernel
go build -o ~/.local/bin/chitin-kernel ./cmd/chitin-kernel
```

Or, from the monorepo:

```bash
pnpm install
pnpm exec nx build execution-kernel
bash scripts/install-kernel-symlink.sh    # symlinks ~/.local/bin/chitin-kernel
```

## chitin.yaml — the policy file

```yaml
version: "1"
mode: guide                  # global default: monitor | enforce | guide
rules:
  - id: no-rm-rf-root
    when: { tool: Bash, command_match: "^rm -rf /( |$)" }
    deny: true
    invariantModes: [enforce] # per-rule override
    reason: "Recursive root deletion is never the right answer."
    suggestion: "Identify the specific path you meant; never use `/`."
```

### The three modes

- **monitor** — log decisions; allow execution. Governance-visible but non-blocking. Use during policy development.
- **enforce** — block silently; return `reason` only. No agent-readable feedback.
- **guide** — block AND return `reason` + `suggestion` + `correctedCommand` as the agent's next-turn input. The agent sees why it was blocked and the recommended alternative. **This is the differentiator.**

Global `mode:` is the default. Per-rule `invariantModes:` overrides.

## Cost-gov v3 (envelope + tier classification)

On top of `gov.Gate`, three invariants govern cost across drivers:

- **Bounded enforcement** — `MaxToolCalls`, `MaxInputBytes` enforced; `BudgetUSD` informational only.
- **Compiler-tier classification** — T0/T2 are audit-log labels, not execution branches.
- **Cross-process envelope** — sqlite at `~/.chitin/gov.db` (WAL). Multiple processes share the same envelope state.

```bash
chitin-kernel envelope status
chitin-kernel envelope reset
```

The envelope is the live cost view; gov-decisions JSONL is the audit log.

## Kill switches

- **Soft** — set `mode: monitor` in `chitin.yaml`. All denials become log-only.
- **Hard** — `chitin-kernel gate lockdown --agent=<agent-name>`. That agent is denied all actions until reset.
- **Clear** — `chitin-kernel gate reset --agent=<agent-name>`.

## Escalation ladder

Denials accumulate per-agent in `~/.chitin/gov.db`:

| Denials | State | Behavior |
|---------|-------|----------|
| 0–2 | normal | deny with feedback |
| 3–6 | elevated | feedback includes a warning |
| 7–9 | high | tighter restrictions (reserved for v2 policy features) |
| 10+ | lockdown | agent-wide; all actions denied |

Lockdown is sticky across sessions. Only `gate reset` clears.

## CLI reference

```bash
chitin-kernel gate evaluate --tool=<name> --args-json=<json> --agent=<name> [--cwd=<path>]
chitin-kernel gate evaluate --hook-stdin --agent=<name>     # for hook integrations
chitin-kernel gate status   --cwd=<path> --agent=<name>
chitin-kernel gate lockdown --agent=<name>
chitin-kernel gate reset    --agent=<name>
```

Exit codes: `0` = allow, `1` = deny, `2` = internal error.

## Decision log

Every gate call appends one JSON line to `~/.chitin/gov-decisions-<YYYY-MM-DD>.jsonl`. These are folded into the chitin event chain via `decision` events emitted by the kernel. The on-disk JSONL is the audit log; the in-chain `decision` event is the canonical record for replay and policy backtesting.

## Closed-enum action vocabulary

`action_type` is a closed enum of 6 classes: `read | write | exec | git | net | dangerous`. Unknown actions are **denied**, not allowed-by-default. If `Normalize()` returns `ActUnknown`, the fix is to extend the normalizer — never to broaden the rule. See [event-model.md](./event-model.md#field-ownership).
