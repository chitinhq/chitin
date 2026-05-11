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

## Agent worktrees

Chitin policy can require side-effecting agent work to happen outside the
primary checkout. Use the helper instead of hand-rolling `git worktree`
commands or symlinking `node_modules`:

```bash
cd ~/workspace/chitin
pnpm worktree -- --agent codex --task small-fix
cd ~/workspace/chitin-codex-small-fix
```

The helper creates or reuses a branch/worktree pair, then runs
`scripts/bootstrap-worktree.sh` to hydrate local dependencies from a shared
pnpm store. Each worktree gets its own `node_modules`; only the pnpm content
store is shared.

For exact branch/path control:

```bash
pnpm worktree -- --branch openclaw/coverage-router --path ~/workspace/chitin-openclaw-coverage-router
```

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

When these commands are attempted from inside a governed hook session,
Chitin classifies `chitin-kernel` commands before envelope spend:

- **Worker-safe reads:** `gate status`, `envelope inspect/list/tail`,
  `decisions`, `chain`, `health`, `chain-info`, `chain-verify`, `router`,
  and `simulate`.
- **Supervisor/operator/system mutations:** `gate reset`, `gate lockdown`,
  envelope mutation (`create/use/grant/close`), install/uninstall, ingest,
  emit, init, drive, and unknown `chitin-kernel` subcommands.

Env claims such as `CHITIN_AUTHORITY=supervisor` are recorded as
`claimed_authority`, but do not grant mutation rights. The effective
`authority` must resolve through trusted identity metadata, such as an
`authority.trusted` policy grant anchored by `agent_fingerprint`,
`agent_instance_id`, or `workflow_id`.

Rules can also constrain actions by identity dimensions. These are exact-match
selectors and accept either a scalar or list value:

```yaml
rules:
  - id: reviewers-read-only
    action: shell.exec
    effect: deny
    role: reviewer
    reason: reviewers cannot run shell commands

  - id: supervisor-can-merge-prs
    action: github.pr.merge
    effect: allow
    authority: supervisor
```

The `authority` selector uses the effective kernel-resolved authority, not a
raw env claim.

## Escalation ladder

Denials accumulate per-agent in `~/.chitin/gov.db`:

| Denials | State | Behavior |
|---------|-------|----------|
| 0–2 | normal | deny with feedback |
| 3–6 | elevated | feedback includes a warning |
| 7–9 | high | tighter restrictions (reserved for v2 policy features) |
| 10+ | lockdown | agent-wide; all actions denied |

The lifetime ladder is complemented by a short-window shell denial cascade:
`escalation.deny_cascade_count` `shell.exec` denials inside
`escalation.deny_cascade_window_seconds` seconds force the same sticky
lockdown. The baseline policy uses 4 denials in 300 seconds.

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

## Driver conformance

See [driver-conformance.md](./driver-conformance.md) for the current driver
matrix, known normalizer gaps, and the next mapping work to prioritize from
live `default-deny` / `unknown` chain rows.

## Closed-enum action vocabulary

`action_type` is the closed enum in `go/execution-kernel/internal/gov/action.go`:
file, shell, git, GitHub, delegation, HTTP, npm, test, MCP, memory, custom
tool, hook, Hermes plumbing, infra, and `unknown`. Unknown actions are
**denied**, not allowed-by-default. If `Normalize()` returns `ActUnknown`, the
fix is to extend the normalizer with tests — never to broaden the rule. See
[event-model.md](./event-model.md#field-ownership).
