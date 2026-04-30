# Chitin Governance Setup

The `gov.Gate.Evaluate(action, agent) → Decision` API is the single enforcement point. Every tool call across every driver evaluates against `chitin.yaml`. Three install paths — one per supported driver — wire this API into the driver's tool-call lifecycle.

## Install paths by driver

```
   chitin.yaml (single policy)
          │
          ▼
   gov.Gate.Evaluate ◄─────────┐
     ▲       ▲       ▲          │
     │       │       │          │
     │       │       │          │
 Claude    Copilot  openclaw    │
  Code      CLI      acpx       │
 PR #66    PR #51    config     │
 hook     SDK driver override   │
     │       │       │          │
     ▼       ▼       ▼          │
        tool calls  ────────────┘
```

### 1. Claude Code (PreToolUse hook — PR #66)

```bash
chitin-kernel install --surface claude-code --global
```

Writes a `PreToolUse` entry to `~/.claude/settings.json` matching the nested
`{matcher, hooks:[{type, command}]}` schema. The hook execs the kernel binary
with the hook payload on stdin.

To uninstall:

```bash
chitin-kernel uninstall --surface claude-code --global
```

### 2. Copilot CLI (in-kernel driver — PR #51)

```bash
chitin-kernel drive copilot "<prompt>"
```

The kernel spawns Copilot CLI as a child of a chitin-driven harness (closed-vendor pattern: see [architecture.md](./architecture.md#two-driver-pattern-open-vs-closed-vendor)). Tool calls are gated via the SDK; chitin enforces the same `gov.Gate` policy.

This is the v1 path used in the 2026-05-07 talk demo. v2 (in-process extension via Copilot SDK `joinSession({tools, hooks})`) starts post-talk.

### 3. openclaw (acpx config-override)

One-line install — no chitin-side wrapper code:

```yaml
# ~/.config/openclaw/acpx.yaml
preToolUse:
  command: chitin-kernel
  args: ["gate", "evaluate", "--hook-stdin", "--agent=openclaw"]
```

openclaw forwards each tool call to chitin-kernel via the configured pre-tool-use hook. Same `gov.Gate` API, same policy file.

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
