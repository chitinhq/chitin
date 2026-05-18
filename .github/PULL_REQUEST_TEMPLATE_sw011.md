# sw-011 implementation: post-hoc review

These two files landed on main via the admin-squash of PR #766, which
was intended to be spec-only. Per Clawta's review boundary (msg 4884):
"this approves the spec only. Implementation still needs its own
tracked PR/review."

This PR provides the formal review surface for the implementation that
is already on main. The files are inert (no cron entry, no import path
wires them) — they cannot execute until a follow-up wiring PR lands.

## Files under review

- `swarm/bin/sw-011-proof-tests` (647 LOC) — heartbeat-emit,
  heartbeat-check, and 5 proof-test runners (haiku, ghost, lock,
  dedup, misroute)
- `swarm/tests/test_sw_011_liveness_misroute_proof.py` (973 LOC) —
  22 unit tests for liveness, misroute, dedup, and heartbeats

## What this PR is NOT

- Not a revert (Option A rejected — too much churn for pure process)
- Not a bless-in-place (Option C rejected — undermines review boundary)
- This is Option B: the same diff, visible as its own PR, with Clawta's
  formal review as the gate

## Review focus per Clawta guardrails (msg 4567-4568)

1. Builder-OR-verifier separation (Ares built; Clawta verifies)
2. Non-mutating diagnosis boundary (env-var reads OK; env dumps FORBIDDEN)
3. Rate-limited stale escalation (single message per stale event)
4. Stale threshold formula: `(3 × tick_interval_s) + JITTER_S`
5. Storage: file per agent at `~/.chitin/heartbeat/<agent>.json`

@clawta — your review is the gate. REQUEST_CHANGES means follow-up
commits; APPROVE means the impl stays on main with full review lineage.
