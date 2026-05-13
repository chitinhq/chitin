---
status: open
owner: claude-code
kanban: t_c8307795
implementation_pr: null
superseded_by: null
effective_from: '2026-05-12'
effective_to: null
---

# Spec: sticky-state recovery rotator for chitin gate

Date: 2026-05-12 (revised 2026-05-13 after Copilot review)
Status: spec — open
Kanban: `t_c8307795` (priority 35, but raised in importance — see "Why now")
Author: claude-code (operator-controlled, spec writer)

## Revision note (2026-05-13)

First draft of this spec described an `envelope-closed` cascade as the
motivating incident and proposed a 5s polling detector against a
hypothetical `denial_events` append-only table. Copilot's review on
PR #554 caught **six substantive design errors**, all confirmed by
reading `internal/gov/escalation.go` and `gate.go`:

1. The table is `denials` (aggregate count + first_ts/last_ts), not
   `denial_events`. There is no append-only event stream to poll.
2. `Counter.Reset()` already clears `denials` AND `agent_state` — the
   "Reset doesn't clear" claim cited from PR #513's review was either
   misremembered or referred to an earlier code state.
3. All `envelope-*` rule IDs are **already exempt** from counter
   increment (`gate.go`, commit 93cf4a7, "Milestone A primitives").
   The 2026-05-04 envelope-closed cascade class is closed in code
   today; the motivating example was obsolete.
4. A 5s polling tick can lose the race against a burst that arrives
   in under 5s — backoff decision lands after lockdown trips.
5. Backoff store keyed `(agent, rule_id)` but pre-eval check uses
   `agent` alone; rule_id isn't known pre-eval. Internally inconsistent.
6. Spec referenced `gov.CountActionDenialsSince` — no such function.

This revision corrects all six. The rotator's purpose is unchanged
(prevent same-rule cascades from accumulating into lockdown); the
mechanism is reworked to use the chitin internals as they actually
exist today.

## Why still relevant after the envelope-* exemption

Envelope-* is exempt from the counter — that class of cascade is
closed. But ANY OTHER deny rule can still cascade. Concrete pattern
that triggers today (observed during this very session, 2026-05-12):

- Operator runs `chitin-kernel --help` → denied with
  `governance-mutation-authority-required` (the
  `t_580bc20e` friction).
- A worker in a tight retry loop hitting the same gov-authority
  rule could climb the counter ladder in seconds. Workers don't
  back off natively.
- Same shape for any rule with a misclassification root cause:
  a single broken upstream condition produces a chain of identical
  denies, each incrementing the counter, until lockdown.

The envelope-* fix shut one door. The rotator generalizes the
defense: for ANY deny rule, recognize same-rule cascades and back
off the agent before the counter reaches lockdown.

## Problem shape (corrected)

Chitin's escalation today (from `chitin.yaml` policy block):

```yaml
elevated_threshold: 3
high_threshold: 7
lockdown_threshold: 10
max_retries_per_action: 3
```

Storage (verified against `internal/gov/escalation.go`):

```sql
CREATE TABLE denials (
    agent TEXT NOT NULL,
    action_fp TEXT NOT NULL,    -- action fingerprint (action-type + target hash)
    count INTEGER NOT NULL DEFAULT 0,
    first_ts TEXT NOT NULL,
    last_ts TEXT NOT NULL,
    PRIMARY KEY (agent, action_fp)
);
CREATE TABLE agent_state (
    agent TEXT PRIMARY KEY,
    total INTEGER NOT NULL DEFAULT 0,
    locked INTEGER NOT NULL DEFAULT 0,
    locked_ts TEXT
);
```

The per-(`agent`, `action_fp`) row aggregates count + last_ts. **This
is sufficient for cascade detection** without a separate event
stream: a row whose `count >= 5` and `last_ts` is recent (within last
30s) is a cascading row. We don't need per-denial timestamps; we
need "is this single fingerprint accumulating fast?"

The `agent_state.total` rolls up across fingerprints and drives the
lockdown ladder via `RecordDenial`.

## Decision

Add a **rotator** that hooks into `Counter.RecordDenial` synchronously
(not via polling) and:

1. **On each denial recorded**, inspects the row that was just
   incremented. If `count >= cascade_active_count` and the previous
   value was below the threshold (i.e., this denial crossed the line),
   engage backoff for the agent.
2. **Backs off the agent broadly** — pauses all gate evaluations
   from that agent for an exponentially-growing window (30s → 5m
   → 30m). The backoff is agent-level because most cascade root
   causes are upstream state (lock, envelope, branch) that affect
   all agent actions, not just one.
3. **Marks `rotator:backoff` denials as counter-exempt**, alongside
   envelope-*, so backoff responses don't themselves accumulate
   toward lockdown. (The single most important fix from review.)
4. **Never auto-resets lockdown.** Lockdown stays operator-only.
   Rotator's job is to **prevent** lockdown, not bypass operator
   authority once it's reached.
5. **Surfaces to operator** via a chain event on backoff engage,
   queryable via `chitin-kernel chain related --kind rotator.cascade_severe`.

## Architecture

### Hook point: `Counter.RecordDenial`

Modify `internal/gov/escalation.go::RecordDenial(agent, fp, weight)` to
return cascade-detection info alongside its existing return value, or
add a sibling method `RecordDenialAndCheck` that returns a
`CascadeSignal` struct. Either way: no separate goroutine, no polling.

```go
// pseudo-go — exact shape decided by implementer
type CascadeSignal struct {
    Detected    bool
    Agent       string
    ActionFP    string
    CountInRow  int   // current row count
    LastTs      time.Time
    Recommendation string  // "back off" | "lockdown imminent"
}

func (c *Counter) RecordDenial(agent, fp string, weight int) (CascadeSignal, error) {
    // existing logic: INSERT/UPDATE denials row, UPDATE agent_state.total
    // new: after UPDATE, read back the row's new count + last_ts; if
    // count crossed cascade_active_count threshold AND last_ts is
    // recent (within cascade_window_seconds), return Detected=true.
}
```

The gate calls `RecordDenial`, gets `CascadeSignal`, and if
`Detected=true` engages the rotator's backoff store.

### Backoff store (in-memory, per-agent)

```go
type BackoffState struct {
    Until    time.Time
    Attempts int   // how many cascade-severe events for this agent
}

// in-process map; rotator.New() initializes; cleared on process restart.
var backoff sync.Map  // agent (string) -> BackoffState
```

Pre-eval check in `Gate.Evaluate`:

```go
if state, ok := backoff.Load(req.Agent); ok && time.Now().Before(state.Until) {
    return Decision{
        Allowed:    false,
        RuleID:     "rotator:backoff",   // <-- exempt from counter, see below
        Reason:     "rotator backoff active for agent — same-rule cascade detected; root cause likely persistent",
        Suggestion: "Operator: chitin-kernel rotator status; chitin-kernel decisions recent | find the cascading rule; fix root cause. Backoff expires automatically.",
    }
}
```

Crucial: place this check BEFORE the existing rule evaluation, but
AFTER envelope-budget evaluation, so:

- Envelope-budget denies still fire normally (operator-imposed caps).
- Rotator backoff applies to policy-deny territory.

### Counter-exempt list: add `rotator:backoff`

In `gate.go`, the existing exempt block:

```go
envelopeDeny := d.RuleID == "envelope-exhausted" ||
    d.RuleID == "envelope-closed" ||
    d.RuleID == "envelope-not-found"
if !d.Allowed && !envelopeDeny && g.Counter != nil {
    // record to counter
}
```

becomes:

```go
exempt := d.RuleID == "envelope-exhausted" ||
    d.RuleID == "envelope-closed" ||
    d.RuleID == "envelope-not-found" ||
    d.RuleID == "rotator:backoff"   // <-- new
if !d.Allowed && !exempt && g.Counter != nil {
    // record to counter
}
```

Without this carve-out, backoff denies would themselves push the
counter past lockdown — undermining the rotator's purpose. (This was
review-finding #6.)

### Backoff growth + clearing

When a cascade signal fires for an agent:

- First time: `until = now + backoff_initial_seconds (30s)`.
- Subsequent fires while backoff is still active: nothing changes
  (the existing window already covers it).
- Subsequent fires AFTER expiry but within `cascade_window_seconds`:
  `until = now + min(prev_window * backoff_growth, backoff_max_seconds)`.
- A clean window with no cascades for `backoff_max_seconds` resets
  `Attempts` to 0.

Clearing: `chitin-kernel rotator clear --agent=<id>` (operator-authority
gated).

### Operator surface

Two new chitin-kernel subcommands:

```bash
chitin-kernel rotator status
  → JSON: active backoff windows: [{agent, until_ts, attempts, last_cascade_ts}]

chitin-kernel rotator clear --agent=<id>
  → operator-authority command: removes the backoff window for that agent
```

### Config block (chitin.yaml)

```yaml
rotator:
  cascade_window_seconds: 30        # "recent" definition for last_ts
  cascade_active_count: 5           # row count threshold to engage backoff
  cascade_severe_count: 8           # emits rotator.cascade_severe chain event
  backoff_initial_seconds: 30
  backoff_max_seconds: 1800
  backoff_growth: 5.0
```

Backwards-compatible — defaults apply if absent.

### Chain event on severe cascade

When `count >= cascade_severe_count`, emit:

```json
{
  "kind": "rotator.cascade_severe",
  "v": 1,
  "ts": "<RFC3339>",
  "agent": "<id>",
  "payload": {
    "action_fp": "<fingerprint>",
    "count_in_row": 8,
    "last_ts": "<RFC3339>",
    "backoff_until": "<RFC3339>",
    "backoff_attempts": 1
  }
}
```

Picked up by Hermes' next standup via `chitin-kernel chain related
--kind rotator.cascade_severe` (the `--kind` flag added by spec #549).

## Acceptance

1. **Reproduce a cascade against pre-rotator gate**: synthesize 10
   denials for the same `(agent, action_fp)` within 3s. Verify
   lockdown trips at the 10th. **Same fixture against the
   post-rotator gate**: backoff engages at the 5th, agent's
   subsequent evals are rejected with `rule_id=rotator:backoff`,
   counter does NOT advance past 5, lockdown does NOT trip.
2. **Backoff denies are counter-exempt**: synthesize 20 rotator:backoff
   denials in a row; verify `agent_state.total` did not advance for
   the affected agent.
3. **`chitin-kernel rotator status`** returns the active backoff
   window with correct fields during a cascade-active state.
4. **Backoff expires automatically** at `until` time; subsequent
   gate evaluations are no longer denied with `rotator:backoff`.
5. **`chitin-kernel rotator clear`** is operator-authority gated;
   worker invocations are denied with the existing
   gov-mutation-authority rule.
6. **`rotator.cascade_severe` chain event** is emitted when count
   reaches 8 in the cascade window. Queryable via the `--kind`
   flag from spec #549.
7. **No false positives** on mixed traffic: 20 denials across 5
   different `action_fp` values (4 each) do NOT trigger backoff
   (per-fingerprint count threshold means single-fp cascades only).
8. **Existing lockdown still fires** if denials genuinely spread
   across many fingerprints in a way the rotator can't recognize —
   rotator REDUCES the surface, doesn't replace lockdown.

## Out of scope

- **Auto-reset of lockdown.** Lockdown is intentionally operator-only.
- **Cross-machine rotator coordination.** In-memory state per host is
  fine for single-operator-box deployment. Multi-host needs a shared
  backoff store — separate ticket.
- **Predictive ML cascade detection.** The threshold-based detector
  is deterministic and auditable.
- **Root-cause auto-remediation.** Rotator pauses the cascade; doesn't
  fix the upstream (lock cleanup, etc.).
- **A separate detector goroutine / polling loop.** Synchronous hook
  inside `RecordDenial` avoids the race the first-draft polling
  design suffered from (review finding #4).
- **Per-fingerprint backoff.** Backoff is per-agent only (review
  finding #5 — rule_id isn't available pre-eval); using the
  fingerprint as the cascade detector but the agent as the backoff
  key is the right asymmetry.

## Implementation pointers for the worker

- **Hook point**: `internal/gov/escalation.go::RecordDenial`. Add the
  cascade-signal return shape; existing call sites need to handle the
  new return value (or use a sibling method to keep call sites that
  don't care unchanged).
- **Backoff store**: new file `internal/gov/rotator.go` (NOT a separate
  package — sits next to the Counter so it can access the same DB
  handle if it grows beyond in-memory in a future revision).
- **Gate hook**: `internal/gov/gate.go::Evaluate` — pre-eval backoff
  check + counter-exempt list update.
- **CLI subcommands**: new file `cmd/chitin-kernel/rotator.go`, mirror
  the patterns in `decisions.go` and `replay_cmd.go`.
- **Chain event emission**: use the existing `WriteLog` path that
  produces JSONL rows in `~/.chitin/gov-decisions-*.jsonl`.
- **Tests**: `internal/gov/rotator_test.go` covering all 8 acceptance
  cases. Reuse the existing `escalation_test.go` fixture patterns
  (synthetic agent, in-memory SQLite).
- **Config**: extend the YAML parser to accept the `rotator:` block.
  Defaults defined as package-level constants so absent block →
  default values, no warning.

## Companion runbook (`docs/runbooks/sticky-state-recovery.md`)

After implementation, add a short operator runbook:

- How to read `chitin-kernel rotator status` during a swarm-stall
  episode.
- When to `chitin-kernel rotator clear --agent=<id>` (after root
  cause is fixed) vs when NOT to (clearing without fixing just
  restarts the cascade).
- How to find the root cause via `chitin-kernel decisions recent
  --window-hours 1 --limit 200 | jq 'group_by(.rule_id) | …'`.

## Related

- Memory `feedback_sticky_state_needs_recovery_automation.md` —
  originating prediction (2026-05-04). Note: that specific incident's
  rule class is now exempt; the rotator generalizes the defense.
- `t_8dcac720` — PR #513 followup on Counter.Reset semantics. The
  earlier review finding cited there ("Counter.Reset doesn't clear
  denial_events") doesn't match current code; that ticket should
  also be revisited under reality of `denials` + `agent_state`
  schema with `DELETE FROM` in `Reset`. Probably a no-op now or
  scope-narrowed.
- `t_580bc20e` — read-only diagnostics blocked by gov-mutation-
  authority during lockdown; rotator reduces the chance of getting
  to lockdown.
- Spec PR #549 (swarm observability via chitin CLI) — chain event
  flow used here.
- Spec PR #548 (no-gov-self-mod-via-shell) — same "pre-empt the bad
  outcome at the right layer" pattern.
