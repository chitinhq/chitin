# Claude Code Hook Driver — Design

**Date:** 2026-04-28
**Status:** Design. Not yet planned; not yet implemented.
**Forcing function:** None — strategic. Strengthens the 2026-05-07 talk's "two-driver pattern" closing slide into a three-driver story if landed before, but ships fine after.
**Parent decisions:**
- The original "two-driver pattern" (`memory/project_two_driver_pattern.md`) split vendors into **open** (in-process extension) and **closed** (wrapping orchestrator). It was articulated for Copilot CLI specifically.
- Surfaced 2026-04-28 (this session): harness-level hooks (e.g. Claude Code's `settings.json` `PreToolUse`) are a **third** integration surface, not implied by the original framing. Same `gov.Gate.Evaluate()` underneath; new vendor-shaped shim above.
- v1 (`feat/copilot-cli-governance-v1`, PR #51) ships the `gov.Gate` API and `chitin-kernel gate evaluate` subprocess interface; this spec reuses both unchanged.
- RTK's hook-install mechanism (`rtk init -g` writing to `~/.claude/settings.json`) demonstrates the integration surface is real, user-installable, and stable enough to build on.

## Preamble

Claude Code exposes a `PreToolUse` hook in `settings.json` that fires synchronously before every tool call (`Bash`, `Edit`, `Write`, `Read`, `WebFetch`, etc.). The hook receives the tool name and arguments via stdin as JSON, and decides whether the tool runs based on its exit code and stdout. This is the canonical "RTK-shape" interception mechanism — a third party can plug in without modifying Claude Code itself, without writing an extension, and without spawning a wrapping process.

The same `gov.Gate` that v1 routes through Copilot's `OnPermissionRequest` and v2 will route through the JS extension's `onPreToolUse` can route through Claude Code's hook with a thin shim. The shim does one thing: read the hook's stdin JSON, normalize it to a `gov.Action`, call `chitin-kernel gate evaluate`, return the appropriate exit code + JSON.

This spec defines the shim and the install/uninstall flow.

## One-sentence invariant

Once `chitin-kernel install claude-code-hook` has been run, every Claude Code session in any worktree under any user account routes every `Bash`, `Edit`, `Write`, `Read`, and `WebFetch` tool call through `gov.Gate.Evaluate()` before execution, with denials surfacing the same `Reason` + `Suggestion` + `CorrectedCommand` to the model that would be visible in the audit log, and with the same SQLite-backed `gov.Counter` tracking escalation state shared across all three drivers (v1 SDK wrap, v2 extension if shipped, this hook driver).

## Scope

### In scope

- `chitin-kernel gate evaluate --hook-stdin` subcommand: reads Claude Code's `PreToolUse` JSON from stdin, normalizes to `gov.Action`, evaluates, writes Claude Code's expected response shape to stdout, sets exit code per the hook protocol.
- New `internal/driver/claudecode/` package: tool-name mapping, normalization (`Normalize(HookInput) → gov.Action`), response formatting.
- `chitin-kernel install claude-code-hook [--global|--project]` subcommand: idempotently merges the chitin hook block into `~/.claude/settings.json` (global) or `.claude/settings.json` (project-scoped); shows diff before applying; backs up original.
- `chitin-kernel uninstall claude-code-hook [--global|--project]`: reverse operation.
- Tool-name normalization for Claude Code's tool vocabulary: `Bash` → shell.exec (routed through `gov.Normalize("terminal", ...)`), `Edit`/`Write`/`NotebookEdit` → file.write, `Read` → file.read, `WebFetch`/`WebSearch` → http.request, `Task` → delegate.task. Any unmapped tool → `ActUnknown` (fail-closed via `default-deny-unknown`).
- Agent identifier `claude-code` for escalation tracking (distinct from `copilot-cli` so cross-driver lockdowns don't pollute each other's counters).
- Tests: unit (normalize, response formatting), integration (end-to-end stdin → stdout against fixture JSON), live-tag test that runs against a real Claude Code session.
- Demo runbook addendum: a section showing the same denial of `rm -rf` in Claude Code that v1 shows in Copilot CLI, evidencing "same `gov.Gate`, three vendor-shaped shims."

### Out of scope

- `PostToolUse` hooks. These are useful for observability (recording what actually executed) but not for gating; if needed, a separate spec.
- Claude Code-specific action types not in chitin's vocabulary today: `SlashCommand`, `Skill`, subagent dispatch beyond the existing `delegate.task`. Map to `ActUnknown` for v1 of this driver; expand the action vocabulary in a follow-up if a real rule emerges.
- Cross-session lockdown coordination beyond what `gov.Counter` already provides (sqlite shared file). Concurrent Claude Code sessions on the same box share the same counter — that's correct behavior, not a feature gap.
- Modifying Claude Code itself. Hook protocol is consumed as-is.
- Bundling `chitin-kernel` distribution with this PR. Install assumes the binary is already on PATH.
- Readybench / bench-devs integration content. Chitin is OSS (`memory/feedback_chitin_oss_boundary.md`).

## Architecture

```
Claude Code session
    ↓ (PreToolUse fires, JSON over stdin)
chitin-kernel gate evaluate --hook-stdin --agent=claude-code --cwd=$CLAUDE_PROJECT_DIR
    ↓
internal/driver/claudecode/Normalize(HookInput) → gov.Action
    ↓
gov.Gate.Evaluate(action, "claude-code") → gov.Decision
    ↓
internal/driver/claudecode/FormatResponse(Decision) → JSON + exit code
    ↓ (stdout JSON, exit 0 = allow / exit 2 + JSON = block)
Claude Code session resumes (allow) or refuses tool call (block)
```

No daemon. No long-lived state in the hook process — every invocation is a cold start of `chitin-kernel`. State lives in `~/.chitin/gov.db` (escalation counter) and `~/.chitin/gov-decisions-<date>.jsonl` (audit log) — both already used by v1 and the existing standalone `chitin-kernel gate evaluate` subcommand.

### Hook protocol contract

Claude Code fires `PreToolUse` with a JSON payload on stdin matching (approximately):

```json
{
  "tool_name": "Bash",
  "tool_input": {
    "command": "rm -rf /tmp/x",
    "description": "Remove temp dir"
  },
  "session_id": "...",
  "transcript_path": "/path/to/transcript.jsonl",
  "cwd": "/home/user/project"
}
```

The hook responds via stdout + exit code:

- **Exit 0**, stdout empty or `{}`: allow the tool call (default).
- **Exit 2**, stdout `{"decision": "block", "reason": "..."}`: block. The `reason` field is shown to the model.
- **Exit non-zero, non-2**: error — Claude Code logs and (per current docs) treats as soft-fail. Chitin should never produce this exit class.

Verify exact field names and exit semantics against current Claude Code docs at implementation time — protocol may have evolved.

### Tool-name mapping

| Claude Code tool | gov.Action.Type | Notes |
|---|---|---|
| `Bash` | route through `gov.Normalize("terminal", {"command": <cmd>})` | Inherits all the existing shell re-tagging: `git.push`, `git.force-push`, `infra.destroy`, `curl-pipe-bash` shape, etc. |
| `Edit` | `file.write` | Target = `file_path` from input |
| `Write` | `file.write` | Target = `file_path` |
| `NotebookEdit` | `file.write` | Target = `notebook_path` |
| `Read` | `file.read` | Target = `file_path` |
| `WebFetch` | `http.request` | Target = `url` |
| `WebSearch` | `http.request` | Target = `query` (or new `web.search` action type if a baseline rule needs to distinguish) |
| `Task` | `delegate.task` | Target = `description` or `subagent_type` |
| `Glob`, `Grep`, `LS`, `TodoWrite` | (initially) `ActUnknown` | Default-deny via fail-closed. Add baseline allow rules if these need to be permitted. **Open question**: read-only browse tools probably default-allow; resolve at install time. |

The mapping lives in `internal/driver/claudecode/normalize.go`. Tests assert that every tool name documented in Claude Code's hook payload spec produces a non-empty `Action.Type`.

### Differences from v1

| Concern | v1 (Copilot SDK wrap) | This (Claude Code hook) |
|---|---|---|
| Process model | Long-running parent + SDK subprocess | Cold-start subprocess per tool call |
| Wire-kind hack | Required (`approve-once`/`user-not-available`/`reject` mapped to SDK enum) | Not needed — exit code + JSON is the contract |
| `LockdownCh` workaround | Required (SDK swallows handler errors) | Not needed — exit 2 with reason is honored cleanly |
| `formatGuideError` reaches model | No (suppressed by SDK; operator-visible only) | **Yes** — `reason` field flows directly into the model's view; this is a UX win over v1 |
| Code size estimate | ~1500 lines + tests | ~300 lines + tests |
| Latency per call | Sub-millisecond (in-process callback) | Cold start of `chitin-kernel` per tool — measure and budget; likely 50-200ms |

The latency budget is the main risk. Cold-start cost is the dominant variable. If `chitin-kernel gate evaluate` startup proves expensive, options include: a long-running daemon (`chitin-kernel gate daemon` listening on a unix socket); a smaller dedicated binary that statically links only the gate path; or accepting the latency for v1 and optimizing later.

### Install flow

```bash
$ chitin-kernel install claude-code-hook --global
chitin: would write the following hook to ~/.claude/settings.json:

  + "hooks": {
  +   "PreToolUse": [
  +     {
  +       "matcher": "Bash|Edit|Write|NotebookEdit|Read|WebFetch|WebSearch|Task",
  +       "hooks": [{
  +         "type": "command",
  +         "command": "chitin-kernel gate evaluate --hook-stdin --agent=claude-code"
  +       }]
  +     }
  +   ]
  + }

backup: ~/.claude/settings.json.chitin-backup-<ts>
proceed? [y/N]
```

- Idempotent: re-running detects the chitin block by a marker comment or by exact-match on the `command` field, no-ops if already present.
- Backup file written every time the install changes anything; restore via `chitin-kernel uninstall claude-code-hook`.
- Project-scoped variant writes to `.claude/settings.json` in the current worktree; global variant writes to `~/.claude/settings.json`.
- `--dry-run` flag emits the diff without applying.
- Refuses to overwrite a non-chitin hook with the same matcher; emits a merge instruction instead. Don't break existing user hooks.

### Cross-driver state

The shared `~/.chitin/gov.db` (escalation counter) and `~/.chitin/gov-decisions-*.jsonl` (audit log) are intentionally cross-driver. A user running both Copilot CLI under v1 and Claude Code under the hook driver gets:
- One unified audit log (every gate decision, regardless of which driver fired it).
- Per-agent escalation counters (`copilot-cli` and `claude-code` count independently — by design, so one driver's runaway doesn't lock out the other).
- One shared policy (`chitin.yaml`).

This is the right behavior: agents are different actors, but the policy and audit story is unified. Aligns with the talk's "same `gov.Gate`, three shims" framing.

## Self-review

### Placeholder scan

No TBD / TODO. Every `<placeholder>` is a runtime substitution chitin already does or a documented field name (`<cmd>`, `<ts>`).

### Internal consistency

- Three drivers (v1 SDK, v2 extension, this hook) share `gov.Gate` and `chitin-kernel gate evaluate` — confirmed in §Architecture.
- "Same `chitin.yaml`, three shims" claim is consistent across §Preamble, §Architecture, §Cross-driver state.
- Out-of-scope items (PostToolUse, Claude Code-specific action types, daemon mode) are explicit and reinforced in §Out of scope.

### Scope check

- Single coherent integration of one harness's hook surface.
- Reuses `gov.Gate` and `chitin-kernel gate evaluate` unchanged — no governance refactor implied.
- Install/uninstall flow is mandatory infrastructure (a hook driver no one can install is not a driver).
- Demo runbook addendum is the smallest evidence the integration works end-to-end and is the talk-quality artifact.

### Ambiguity check

- "Cold start of `chitin-kernel`" (Architecture differences table): assumes the user has the chitin binary on PATH. The install command does not bundle the binary; it only writes the settings.json hook. Verify this is consistent with how RTK ships its hook (operator pre-installs the binary, then runs init).
- "Map to `ActUnknown` (fail-closed)" (Tool-name mapping): default-deny on unknown is the v1 contract — confirm chitin.yaml's `default-deny-unknown` rule is unchanged and applies here.
- "Project-scoped vs global" (Install flow): `--global` writes to `~/.claude/settings.json`, `--project` to `.claude/settings.json` of cwd. If both exist, Claude Code merges with project taking precedence. Document this; don't try to reconcile across both at install time.

### Out-of-scope leak check

- No v1 driver changes. v1 ships independently.
- No v2 spike changes. The v2 extension spike's go/no-go is unaffected.
- No openclaw changes — openclaw orchestrates Copilot CLI; this driver gates Claude Code; orthogonal surfaces.
- No new `gov.Action` types (`Glob`, `Grep`, `LS`, `TodoWrite` map to `ActUnknown` until a real rule needs distinction).
- No changes to `gov.Gate.Evaluate` signature.

## Open questions for plan phase

1. **Latency budget.** Measure cold start of `chitin-kernel gate evaluate` on the operator's box. If >300ms per tool call, that's a noticeable typing delay during interactive Claude Code sessions. Daemon mode is the fallback; spec it in the plan if measurement shows it's needed.
2. **Read-only tool default.** `Glob`, `Grep`, `LS`, `Read`, `TodoWrite` — should these default-allow (likely yes for browse tools) or default-deny (the chitin closed-enum philosophy)? Probably allow with explicit baseline rules; confirm with operator.
3. **Hook protocol stability.** Claude Code's hook system is documented but newer than the Copilot SDK. Pin the assumed protocol version in the install command and emit a warning if the runtime detection finds drift.
4. **Coexistence with RTK's hook.** If the user has `rtk init -g` already installed, both RTK and chitin would register `PreToolUse` hooks. Claude Code runs all matchers; both fire. Verify chitin's hook doesn't break RTK's output filtering (likely fine — they're orthogonal — but explicit test).
5. **Talk inclusion.** Does this land before 2026-05-07? If yes, the talk's closing slide becomes "three patterns" instead of "two." If no, this spec gets implemented post-talk and the talk closes as-written. Operator decision; spec doesn't depend on it.

## Branch + worktree

Per `memory/feedback_always_work_in_worktree.md`:
- Spec branch: this file lands on `main` (consistent with v1/v2 spec commits).
- Implementation branch when planned: `feat/claude-code-hook-driver` off `main`.
- Worktree: `~/workspace/chitin-claude-code-hook/`.
