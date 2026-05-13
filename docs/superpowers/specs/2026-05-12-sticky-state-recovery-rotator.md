# Spec: sticky-state recovery rotator for chitin gate

Date: 2026-05-12
Status: spec — open
Kanban: `t_c8307795` (priority 35, but raised in importance — see "Why now")
Author: claude-code (operator-controlled, spec writer)

## Why now

Chitin's deny-cascade lockdown has bit live operations at least three times in 2026:

- **2026-05-04** — envelope-closed deny-cascade halted the swarm for 3h before operator could intervene. Memory `feedback_sticky_state_needs_recovery_automation.md` records this as the originating incident.
- **2026-05-11 (#509)** — same family hit twice during one implementation session, each requiring operator reset.
- **Implied by PR #513's Copilot findings** — `Counter.Reset()` doesn't clear `denial_events`, so even after operator reset, the next denial can re-trip immediately. That's a related-but-distinct sticky-state bug now tracked separately on `t_8dcac720`.

Pattern is consistent: **a single root failure produces a cascade of identical denies, each adding to the counter, until lockdown trips. Operator reset is the only recovery, but reset doesn't address the root cause, so the cascade re-fires.** This spec closes the recurring outage.

## Problem shape

Chitin's escalation today (from `chitin.yaml`):

```yaml
elevated_threshold: 3
high_threshold: 7
lockdown_threshold: 10
max_retries_per_action: 3
```

Plus the `denial_events` table in `internal/gov/escalation.go` records every denial.

What happens during a cascade:

1. Some upstream condition fails (envelope closed, lock stale, transient I/O error, etc.).
2. Every subsequent agent action evaluates against the same broken state and is denied with the same `rule_id`.
3. Counter climbs through `elevated → high → lockdown` in seconds.
4. Lockdown blocks ALL further actions, including the operator's read-only diagnostics (the bug separately filed as `t_580bc20e`).
5. Operator manually resets. Cascade resumes if root cause persists.

**The gap:** chitin has no mechanism to recognize "this is a cascade, not normal traffic, slow down upstream so the gate stops accumulating pressure."

## Decision

Add a **rotator** — a small detection + backoff loop that runs alongside the gate and:

1. **Detects cascade patterns** in the live denial stream (same `rule_id` × same `agent` repeating within a short window).
2. **Applies progressive backoff** to the cascading agent: refuses to dispatch new gate evaluations from that agent for an increasing window (30s → 5m → 30m). This is "pressure relief at the source" rather than "auto-reset the gate."
3. **Never auto-resets lockdown.** Lockdown is the fail-closed end-state; only operator authority unlocks it. The rotator's job is to PREVENT lockdown, not to silently recover from it.
4. **Surfaces to operator** when backoff is engaged: posts to `#ares` (Hermes-side channel) with the agent, rule, count, and current backoff window. So the operator can investigate the root cause while the swarm keeps making forward progress on non-cascading agents.

## Architecture

### Component 1: cascade detector

A goroutine spawned by the gate at startup. Reads the `denial_events` table on a short tick (default 5s). Per (`agent`, `rule_id`) bucket in the last 60s window:

- 3 denials → CASCADE_EMERGING (info-level log; no action yet)
- 5 denials → CASCADE_ACTIVE (engage backoff for this agent)
- 8 denials → CASCADE_SEVERE (escalate backoff, post to #ares)
- 10 denials → STILL_HITS_LOCKDOWN (per existing threshold — operator must reset)

Threshold values land in `chitin.yaml` under a new `rotator:` block so operators can tune per deployment:

```yaml
rotator:
  detector_tick_seconds: 5
  cascade_window_seconds: 60
  cascade_emerging_count: 3
  cascade_active_count: 5
  cascade_severe_count: 8
  backoff_initial_seconds: 30
  backoff_max_seconds: 1800
  backoff_growth: 5.0      # exponential factor
  alert_channel: "ares"    # operator channel for cascade-severe posts
```

### Component 2: backoff store

In-memory map: `(agent, rule_id) → BackoffState{ until_unix, attempts }`. Persists ONLY in-memory (rotator state is intentionally ephemeral; restart clears it, which is the right behavior for a transient-state guard).

When the gate evaluates a new action, it consults the backoff store BEFORE running the rule matching:

```go
// pseudo-go
func (g *Gate) Evaluate(req Request) Decision {
    if state := g.rotator.Backoff(req.Agent); state != nil && time.Now().Before(state.Until) {
        return Decision{
            Allowed: false,
            RuleID:  "rotator:backoff",
            Reason:  fmt.Sprintf("rotator backoff active for agent=%s rule=%s until=%s; root cause likely persistent — investigate before retry", req.Agent, state.RuleID, state.Until),
            Suggestion: "Operator: read 'chitin-kernel decisions recent' to find the cascading rule. Fix the root cause (lock cleanup, envelope re-open, etc.). Backoff expires automatically.",
        }
    }
    // ... existing evaluation logic
}
```

The backoff deny is logged in `denial_events` like any other deny, but with `rule_id: "rotator:backoff"` so it's distinguishable in audit + queryable in `chitin-kernel chain stats`.

### Component 3: operator surface

Backoff state is observable via two new chitin-kernel subcommands:

```bash
chitin-kernel rotator status
  → JSON: list of active backoff windows with (agent, rule, until_ts, attempts)

chitin-kernel rotator clear --agent=<id> [--rule-id=<id>]
  → operator-authority command: clears one or all backoff windows for that agent.
    Use when the root cause has been fixed and the operator wants to resume.
```

`rotator clear` is gated by chitin's existing operator-mutation-authority rule (mode = operator-authority required), same posture as gate reset.

### Component 4: alert path

When CASCADE_SEVERE fires (8 denies in 60s), the rotator emits a chain event:

```json
{
  "kind": "rotator.cascade_severe",
  "v": 1,
  "ts": "<RFC3339>",
  "agent": "...",
  "payload": {
    "rule_id": "...",
    "count_in_window": 8,
    "window_seconds": 60,
    "backoff_until": "<RFC3339>",
    "sample_targets": ["…three sample action_target strings…"]
  }
}
```

This event lands in the chain ledger. Hermes' standup cron (per spec PR #549) picks it up via `chitin-kernel chain related --kind rotator.cascade_severe` and surfaces in the next standup. **No DM path required** (consistent with the amended Hermes+Clawta architecture, PR #545).

For latency-sensitive cases (operator wants real-time, not next-standup), the rotator ALSO writes to `~/.openclaw/logs/rotator.log` for the existing journalctl-tailing operator pattern. Optional `--alert-channel` config writes to a Discord channel via `openclaw message send` (best-effort, never blocks the gate).

## Acceptance

1. **Reproduce the 2026-05-04 cascade pattern** via a test fixture: synthesize 10 envelope-closed denials within 5s for the same agent, against the pre-rotator gate. Verify lockdown trips. **Same fixture against the post-rotator gate** triggers backoff at the 5-denial mark and prevents lockdown.
2. **`chitin-kernel rotator status`** returns the active backoff window with correct fields during a CASCADE_ACTIVE state.
3. **Backoff expires automatically** when `time.Now() >= until_unix`. After expiry, normal evaluation resumes.
4. **`chitin-kernel rotator clear`** is operator-authority gated and clears the named window. Worker agents calling it are denied with the existing operator-mutation-authority rule.
5. **CASCADE_SEVERE emits a `rotator.cascade_severe` chain event** queryable via `chitin-kernel chain related --kind rotator.cascade_severe` (the `--kind` flag added by spec #549).
6. **No false positives on healthy traffic.** A test fixture with 20 denials spread across 5 different rule_ids within 60s does NOT trigger backoff (the per-(agent, rule_id) bucketing prevents this).
7. **Existing lockdown behavior preserved.** If somehow 10 denies still accumulate in the lockdown_threshold window (e.g., 10 different rule_ids), lockdown still fires. The rotator REDUCES the surface area but doesn't replace lockdown.

## Out of scope

- **Auto-reset of lockdown.** Lockdown is intentionally operator-only. The rotator prevents lockdown by relieving pressure earlier; it does not bypass operator authority when lockdown has nonetheless been reached.
- **Cross-machine rotator coordination.** Single-operator-box deployment for now; the in-memory state per host is fine. When the swarm runs on multiple boxes, the rotator would need a shared backoff store (Redis / SQLite over NFS / etc.) — separate ticket.
- **Predictive ML-based cascade detection.** The threshold-based detector is deterministic and auditable. ML adds opacity for marginal recall gains. Out unless the deterministic detector shows real false-positive issues in production.
- **Root-cause auto-remediation.** The rotator pauses the cascade but doesn't FIX whatever broke upstream (envelope closure, lock contention, etc.). Root-cause fixing is a different layer; operator or a dedicated remediation worker handles it.
- **Fixing `Counter.Reset()` to clear `denial_events`.** Separate ticket: `t_8dcac720` (PR #513 followup). Wired tangentially because once that's fixed, the rotator's data source is cleaner.

## Implementation pointers for the worker

- **Detector + backoff** live in `internal/gov/rotator/` (new package). Goroutine spawned from `gate_hook.go` startup. The detector reads via `gov.CountActionDenialsSince` (already exists per PR #513's introduction) or directly against the `denial_events` SQLite table.
- **Config block** in `chitin.yaml`: add the `rotator:` map. Default values land if the block is absent (backward-compatible).
- **CLI subcommands** in `cmd/chitin-kernel/`: new file `rotator.go` with `cmdRotatorStatus` and `cmdRotatorClear`. Add the dispatch in `main.go` next to `case "decisions":`.
- **Chain event emission** uses the existing `chitin-kernel emit` path (or directly via the in-process `gov.WriteLog`).
- **Test fixtures** in `internal/gov/rotator/rotator_test.go` cover the acceptance scenarios. Reuse the `denial_events` fixture pattern from existing escalation_test.go.

## Companion runbook

After implementation, add `docs/runbooks/sticky-state-recovery.md` covering:

- How to read `chitin-kernel rotator status` during a swarm-stall episode
- When to `chitin-kernel rotator clear` (and when NOT to — if the root cause isn't fixed, clearing just re-fires the cascade)
- How to find the root cause via `chitin-kernel decisions recent --window-hours 1 --limit 200 | jq 'group_by(.rule_id) | …'`
- Reference: this spec + the originating incident memory

## Why the rotator is upstream of lockdown, not a replacement for it

Lockdown is the fail-closed end-state. It's the kernel's last-ditch protection against "we genuinely can't tell what's happening; refuse all action until human verifies." That's the right behavior under true uncertainty.

Cascades are NOT true uncertainty — they're a recognizable pattern (same rule, same agent, in a short window). The rotator handles that pattern at the layer where it's actually detectable (the deny-event stream), letting the kernel stop accumulating fear-pressure into lockdown for what's really a single recoverable upstream issue.

In short: **lockdown still guards real unknowns. The rotator just stops single-root-cause cascades from masquerading as unknowns.**

## Related

- Memory `feedback_sticky_state_needs_recovery_automation.md` (2026-05-04) — the originating prediction this spec finally addresses.
- `t_8dcac720` — PR #513 followup on `Counter.Reset()` not clearing `denial_events` (related sticky-state bug; rotator data quality benefits when that lands).
- `t_580bc20e` — separate triage ticket about read-only diagnostics being blocked by gov-mutation-authority; relevant because once lockdown is active today, operator can't even READ the diagnostics they need.
- Spec PR #547 (no-commit-to-protected) — same "pre-empt the bad outcome at the right layer" pattern; both rules close gov-side gaps.
- Spec PR #549 (swarm observability via chitin CLI) — the rotator's chain events are queryable via the CLI surfaces that spec aligns.
