# Chitin Governance Setup

The `gov.Gate.Evaluate(action, agent) → Decision` API is the single enforcement point. Every tool call across every supported execution driver evaluates against `chitin.yaml`. Install paths wire this API into each driver's tool-call lifecycle where the vendor exposes a hook or SDK boundary.

## Install paths by driver

```
                 chitin.yaml (single policy)
                          │
                          ▼
                  gov.Gate.Evaluate ◄────────────────────────┐
       ▲       ▲      ▲       ▲        ▲        ▲             │
       │       │      │       │        │        │             │
   Claude   Codex  Gemini  Hermes   Copilot  openclaw        │
    Code     CLI    CLI     Agent     CLI      plugin         │
   PreToo  PreToo  BeforeT pre_tool   SDK      before_        │
   Use     Use     ool     _call      wrap     tool_call      │
   hook    hook    hook    hook                              │
       │       │      │       │        │        │             │
       ▼       ▼      ▼       ▼        ▼        ▼             │
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

### 4. Hermes (`pre_tool_call` shell hook)

```bash
bash scripts/install-hermes-hook.sh
```

Writes a `pre_tool_call` entry into `~/.hermes/config.yaml` pointing at
`chitin-router-hook --agent=hermes`. Hermes' shell-hook payload is byte-
compatible with Claude Code's PreToolUse shape; per-tool normalization lives
in `internal/driver/hermes/normalize.go`.

The installer also writes the matching entry to Hermes' shell-hook allowlist
so the worker service does not block on first-run consent.

### 5. Copilot CLI (in-kernel driver — wrapping orchestrator)

```bash
chitin-kernel drive copilot "<prompt>"
```

The kernel spawns Copilot CLI as a child of a chitin-driven harness (closed-vendor pattern: see [architecture.md](./architecture.md#vendor-integration-patterns-open-vs-closed-vendor)). Tool calls are gated via the SDK; chitin enforces the same `gov.Gate` policy.

### 6. openclaw (`local-*` drivers via `before_tool_call` plugin)

```yaml
# ~/.config/openclaw/openclaw.json
plugins:
  allow:
    - chitin-governance     # ships with chitin; loaded at openclaw startup
```

The plugin is loaded by openclaw at startup; every tool call dispatched by openclaw-managed agents (qwen / glm / glm-flash / deepseek) passes through `before_tool_call` → `chitin-kernel gate evaluate`. Same policy file.

### 7. VS Code Copilot (IDE guidance, not enforcement)

VS Code Copilot uses repository instructions rather than a chitin hook. This
repo provides:

- `AGENTS.md` as the universal product boundary.
- `.github/copilot-instructions.md` for repository-wide Copilot context.
- `.github/instructions/chitin-*.instructions.md` for path-specific guidance.
- `.vscode/settings.json` enabling instruction files and `AGENTS.md` loading.

This setup helps Copilot in the IDE follow the same boundary as other agents,
but it is not a security boundary. Use `chitin-kernel drive copilot` when
Copilot execution must be governed by the kernel.

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

The policy file uses a rule-list evaluated with deny-first semantics: all deny
rules are checked first (first match wins), then all allow rules are checked
(first match wins), then default-deny if no rule matches.

```yaml
id: my-agent
name: My Agent Policy
mode: enforce              # monitor | enforce | guide

bounds:
  max_files_changed: 25    # blast-radius ceiling per push
  max_lines_changed: 500

escalation:
  elevated_threshold: 3    # denials before elevated warning
  high_threshold: 7        # denials before high restriction
  lockdown_threshold: 10   # denials before agent-wide lockdown

rules:
  # Hard deny — catastrophic patterns
  - id: no-recursive-delete
    action: file.recursive_delete
    effect: deny
    reason: "Recursive delete is blocked"
    suggestion: "Use git rm <specific-files>"

  - id: no-protected-push
    action: git.push
    effect: deny
    branches: [main, master]
    reason: "Direct push to protected branch is blocked"

  # Allow productive work within bounds
  - id: allow-reads
    action: file.read
    effect: allow
    reason: "Reads are safe"

  - id: allow-writes
    action: file.write
    effect: allow
    reason: "Writes within blast-radius bounds are allowed"

  - id: allow-git-push
    action: git.push
    effect: allow
    reason: "Feature branch pushes are allowed"
```

### Tier example configs

Three ready-to-use policy configs for different agent tiers are in
`docs/examples/policies/`:

| Tier | File | Mode | Philosophy |
|------|------|------|------------|
| T0 (flash) | `tier-0-flash.yaml` | guide | Permissive; only blocks catastrophic mistakes |
| T2 (heavy) | `tier-2-heavy.yaml` | enforce | Balanced; blocks destructive ops, allows productive work |
| T4 (autonomous) | `tier-4-autonomous.yaml` | enforce | Strict default-deny; only allows what's explicitly needed |

These configs are validated by Go tests in
`go/execution-kernel/internal/gov/policy_tiers_test.go`.

### The three modes

- **monitor** — log decisions; allow execution. Governance-visible but non-blocking. Use during policy development.
- **enforce** — block silently; return `reason` only. No agent-readable feedback.
- **guide** — block AND return `reason` + `suggestion` + `correctedCommand` as the agent's next-turn input. The agent sees why it was blocked and the recommended alternative. **This is the differentiator.**

> **Note:** The `effect: escalate` rule effect was removed in the 2026-05-08 cull. Operator approvals are now handled by Hermes' `tools/approval.py` — see `docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`. Using `effect: escalate` in a rule will fail at parse time.

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

`gate evaluate` writes one machine-readable JSON object to stdout on every
normal evaluation path. The stable fields are:

- `allowed`: boolean verdict.
- `mode`: resolved policy mode (`monitor`, `enforce`, or `guide`).
- `rule_id`: matched rule, `default-deny`, `no_policy_found`, or
  `policy_invalid`.
- `reason`: human-readable explanation.
- `action_type`: canonical closed-enum action.
- `action_target`: normalized target used for policy matching.
- `ts`: RFC3339 timestamp when a policy decision was evaluated.

Exit codes:

- `0` = allow.
- `1` = policy denial, guide denial, lockdown denial, or no policy found.
- `2` = internal/configuration error, including malformed policy.

`no_policy_found` is intentionally distinguishable from `policy_invalid` so
operators can choose compatibility behavior for unpolicied directories without
silently allowing broken policy files.

## Decision log

Every gate call appends one JSON line to `~/.chitin/gov-decisions-<YYYY-MM-DD>.jsonl`. These are folded into the chitin event chain via `decision` events emitted by the kernel. The on-disk JSONL is the audit log; the in-chain `decision` event is the canonical record for replay and policy backtesting.

## Driver conformance

See [driver-conformance.md](./driver-conformance.md) for the driver matrix.

The cross-driver conformance test in
`go/execution-kernel/internal/driver/cross_driver_conformance_test.go`
verifies that every documented tool name from each driver (claudecode, gemini,
hermes) normalizes to a non-`ActUnknown` ActionType. When a new tool is added
to a driver, add it to the conformance test to prevent silent default-deny
regressions in enforce mode.

## Closed-enum action vocabulary

`action_type` is the closed enum in `go/execution-kernel/internal/gov/action.go`. The
current types are:

| Type | Meaning |
|------|----------|
| `file.read` | Read-only file access |
| `file.write` | File creation/modification |
| `file.delete` | File deletion |
| `file.move` | File rename/move |
| `file.recursive_delete` | `rm -rf` — re-tagged from shell.exec |
| `shell.exec` | Shell command execution |
| `git.push` | Git push (any ref) |
| `git.force_push` | Force push |
| `git.commit` | Git commit |
| `git.status` | Git status |
| `git.worktree_add` | Git worktree add |
| `git.worktree_remove` | Git worktree remove |
| `http.request` | Outbound HTTP/network |
| `delegate.task` | Subagent delegation |
| `infra_destroy` | Terraform/kubectl destroy |
| `mcp.call` | MCP tool invocation |
| `kanban.call` | Hermes kanban plumbing |
| `hermes.process` | Hermes process management |
| `unknown` | Unrecognized tool — **denied by default** |

Unknown actions are **denied**, not allowed-by-default. If `Normalize()` returns
`ActUnknown`, the fix is to extend the normalizer with tests — never to broaden
the rule. The openclaw conformance test in
`go/execution-kernel/internal/gov/normalize_openclaw_conformance_test.go` catches
regressions; the cross-driver conformance test in
`go/execution-kernel/internal/driver/cross_driver_conformance_test.go` catches
driver-level gaps. See [event-model.md](./event-model.md#field-ownership).
