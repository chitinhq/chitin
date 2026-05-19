# Feature Specification: Separate read-only diagnostics from governance mutation authority checks

**Feature Branch**: `feat/diagnostics-mutation-separation`

**Created**: 2026-05-16

**Status**: Draft

**Ticket**: `t_580bc20e`

**Parent spec**: `001-agent-bus/spec.md` (dispatch and authority model)

---

## Problem

Currently, `chitin-kernel gate evaluate` serves two purposes:

1. **Read-only diagnostics**: Operators and agents query the kernel to check whether a hypothetical action would be allowed (e.g., "can I push to `main`?", "what's my blast radius?"). These are safe, idempotent, and should be accessible to any agent or operator at any time.

2. **Governance mutation authority checks**: The kernel gates tool calls that mutate state (file writes, shell commands, branch pushes). These are the enforcement layer — they deny actions that violate policy.

The problem: both paths share the same `gate evaluate` call path, same chain write, and same policy resolution. This means:

- Diagnostic queries leave chain records indistinguishable from enforcement decisions, polluting the audit trail.
- Read-only queries (blast radius, floundering score, drift) are gated behind the same hot path as enforcement, adding latency.
- The `pre_tool_call` hook can't distinguish "I'm just asking" from "I'm about to do this," so operator approval prompts fire on informational queries.

## User Scenarios

1. **Agent queries blast radius** (P0): A codex worker calls `chitin-kernel gate evaluate --dry-run` to check whether a `git push` would be denied. The kernel returns a policy result without writing a chain record and without triggering any approval callback. Acceptance: `--dry-run` returns identical policy output to a real evaluation but records nothing in the chain.

2. **Operator checks drift score** (P0): An operator runs `chitin-kernel diagnostics drift` to see the current drift score for a session. This is a pure read from `chain_index.sqlite` — no chain write, no policy evaluation, no approval prompt. Acceptance: command completes in <50ms, writes nothing.

3. **Agent submits real tool call** (P0): A codex worker calls `chitin-kernel gate evaluate` (without `--dry-run`) for a `git push`. The kernel evaluates policy, writes a chain record, and may deny the action. Acceptance: chain record is written, enforcement proceeds as before.

4. **Dashboard reads chain** (P1): The chitin dashboard reads `~/.chitin/gov-decisions-*.jsonl` and `chain_index.sqlite` to display enforcement history. Acceptance: read-only path never writes to chain or triggers approvals.

## Acceptance Scenarios

```gherkin
Scenario: Dry-run evaluation does not write chain records
  Given an agent calls `chitin-kernel gate evaluate --dry-run` with a git push payload
  When the evaluation completes
  Then no new row is written to gov-decisions-*.jsonl
  And no new row is written to chain_index.sqlite
  And the exit code and stdout match a real evaluation for the same payload

Scenario: Diagnostics subcommand is read-only
  Given an operator runs `chitin-kernel diagnostics drift --session abc123`
  When the command completes
  Then no new row is written to any chain or decision file
  And the command completes in <50ms
  And the output includes drift_score as a numeric value

Scenario: Real evaluation writes chain records
  Given an agent calls `chitin-kernel gate evaluate` (without --dry-run) with a git push payload
  When the evaluation completes
  Then a new row is written to gov-decisions-*.jsonl
  And a new row is written to chain_index.sqlite
  And the chain row has `dry_run: false`

Scenario: Dry-run and real evaluation produce identical policy output
  Given identical payloads for dry-run and real evaluation
  When both are executed
  Then the allow/deny decision, blast_radius, and floundering_score are identical
  And only the real evaluation has a chain record

Scenario: Pre_tool_call hook distinguishes dry-run from enforcement
  Given an agent submits a dry-run evaluation via the hermes bridge
  When the pre_tool_call hook processes it
  Then no approval prompt is shown to the operator
  And no approval record is written
```

## Implementation Notes

1. **New subcommand**: Add `chitin-kernel diagnostics` with sub-commands `drift`, `blast-radius`, `floundering`. Each reads from `chain_index.sqlite` or `gov.db` — no writes.

2. **Dry-run flag**: Add `--dry-run` to `gate evaluate`. In dry-run mode:
   - Evaluate policy exactly as normal (same normalization, same rule engine).
   - Skip `chain.Append()` — do not write to `gov-decisions-*.jsonl` or `chain_index.sqlite`.
   - Skip approval callbacks — do not trigger hermes `pre_tool_call` prompts.
   - Return identical JSON output to real evaluation, with an additional `dry_run: true` field.

3. **Chain separation**: Add a `dry_run` boolean column to the `Decision` struct. Real evaluations set `dry_run: false` (or omit it). This makes the audit trail self-documenting.

4. **No new Go dependencies**. The `diagnostics` subcommand uses existing `internal/chain` and `internal/router` readers.

5. **Backward-compatible**: `gate evaluate` without `--dry-run` behaves exactly as before. No flag migration needed.

## Out of Scope

- Dashboard UI (that's t_8f4d2ee5, the chitin dashboard epic).
- Policy composer (that's t_3cf15ef1, dashboard-dependent).
- MCP server (that's t_dbd11405, architectural north star).