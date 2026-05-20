# 049 — Octi swarm role architecture (the behavior layer above 040-048)

> Parent: spec 038 (Octi master), spec 040 (Octi scaffolding).
> Sibling: thread 18's `workspace/claude/skills/spec-factory.md` —
> THIS spec formalizes what that skill has been operating informally.
> Ratified 2026-05-19 via agent-bus thread 19 msg 7717. Three
> proposals (Ares msgs 7740-7743, Clawta msg 7722, claude-code
> msg 7726). Hybrid ratified:
> **Ares' 5-role frame + Clawta's conflict_sets + claude-code's
> derived confidence**.

## Summary

Octi infrastructure (specs 040-048) makes workflows durable and
replayable. This spec defines **what flows through them**: the five
roles in the assembly line, the capability schema each agent
registers, the handoff packet that crosses every stage gate, and
the verifier Activity that asserts the next stage's entry
invariant. Operator (red) is **external**, at two HITL gates only
(spec ratification + PR merge). Inter-agent communication happens
exclusively through artifacts — kanban tickets, spec PRs, code PRs —
never through chat. Discord exists only for operator ↔ individual
agent comms.

This is the chitin thesis lifted to the process level:
**gate → record → replay → signals → policy → improve**, but at the
"who is doing what role, why, and what did the verifier say"
granularity.

## Ticket refs

- Parent specs: 038 (Octi master), 040 (Octi scaffolding)
- Ratification thread: agent-bus **thread 19**, msg 7717 (RFP),
  7722 (Clawta proposal), 7726 (claude-code proposal),
  msgs 7740-7743 (Ares proposal — landed in thread 1 due to spec
  042 routing bug; cross-referenced for record).
- Reconciliation source: `workspace/claude/skills/spec-factory.md`
  (live since 2026-05-19), `workspace/claude/skills/spec-factory-queue.md`
  (live runway).
- Live evidence: PRs #928 (spec 020), #929 (spec 022) on
  bench-devs-platform — first specs through the factory line
  before this spec ratified.
- Channel architecture change: 2026-05-19 ~20:50 EDT operator
  deleted #swarm, #mini, #hermes; remaining channels are
  **#ares** (1503438297597350062) and **#clawta**
  (1503439202719760405). Bus routes updated accordingly.

## File-system scope

Worker MAY write under:

- `swarm/octi/roles/` — new Go package
  - `swarm/octi/roles/schema.go` — `CapabilityProfile`,
    `HandoffPacket`, `RoleAssignment` structs (v1, frozen)
  - `swarm/octi/roles/router.go` — deterministic routing function
  - `swarm/octi/roles/verifier.go` — entry-invariant Activity
  - `swarm/octi/roles/conflict.go` — `conflict_sets` evaluation
  - `swarm/octi/roles/confidence_update.go` — derived-confidence
    workflow (nightly schedule)
- `swarm/octi/roles/tests/` — unit tests
- `swarm/octi/e2e/role_routing_test.go` — **e2e**: full pipeline
  routes deterministically over a fixture of 10 spec lifecycles
- `swarm/octi/config/capability_profiles/` — per-agent profiles
  - `ares.yaml` (research + spec-reviewer + board-groomer +
    pr-reviewer-signal)
  - `claude.yaml` (spec-writer)
  - `clawta.yaml` (implementer)
  - `mini.yaml` (execution-surface only — Mini is dispatched to,
    not a role-holder)
  - `copilot.yaml`, `codex.yaml`, `claudecode.yaml` (driver
    workers / verifier signals)
- `swarm/octi/cmd/octi-roles/main.go` — operator CLI
- `swarm/bin/octi-roles` — wrapper
- `.specify/specs/049-octi-swarm-role-architecture/**`
- `workspace/claude/skills/spec-factory.md` — extend with a
  pointer to this spec, NOT rewrite (the skill file is the
  operator-facing summary; this spec is the contract)

Worker MUST NOT write under:

- Agent runtime code (`swarm/mini/`, hermes scripts,
  openclaw workflows) — agents declare capability profiles,
  Octi consumes them
- `chitin.yaml` (kernel gate unchanged)
- Existing factory-line skill files in `workspace/claude/skills/`
  beyond the pointer reference — they continue to be the
  operator-facing docs

## Goal

A spec idea enters the assembly line via a `research` stage
emission. Octi routes the resulting handoff packet through the six
roles: research → spec-writer → spec-reviewer (operator-ratify
gate) → board-groomer → implementer → verifier (operator-merge
gate). Every routing decision is recorded as an OctiEvent;
"why did this stage go to Ares this time" is a one-query answer
against the event mirror. Verifier failures decay agent confidence
automatically. The reviewer bottleneck is **visible**, not
**solved** — by design — and surfaces to operator via the
`max_role_wait` timer (R8) rather than auto-approving.

## Requirements

### R1 — Six roles

**Operator override (2026-05-19, mid-draft)**: Ares' own proposal cut
board-maintainer as a role, arguing kanban transitions are a
deterministic workflow. Operator overruled — Ares does the
judgment-heavy board grooming (which specs are stale, which
dependencies block, which tickets are duplicates), so board-groomer
becomes a role. Mechanical state transitions (claim, assign on
merge) still ride as workflows; ROLE owns the judgment.

| Role | Produces | Constraint |
|---|---|---|
| **research** | candidate ticket with `research_summary`, `proposed_invariants`, `source_citations` | findings only; never produces specs or code |
| **spec-writer** | `.specify/specs/NNN-<slug>/spec.md` PR | one spec per PR; cites research |
| **spec-reviewer** | APPROVE / REQUEST_CHANGES on spec PR | `conflict_sets` enforced (R3); never reviews own work; review must cite specific invariants/criticisms (soft guard for research-author overlap, R6) |
| **board-groomer** | kanban hygiene decisions: stale-demotion, dependency veto, duplicate flag, priority adjustment | runs over ratified specs + open tickets; never grooms ticket they implement or review |
| **implementer** | code PR with linked spec + ticket | test-first per spec 020; scope matches `allowed_paths` |
| **verifier** | pass/fail on stage-exit invariant + confidence delta | Temporal Activity, not a person; copilot + CI are signals INTO verifier |

**What's NOT a role** (explicitly):
- `pr-reviewer (copilot)` — copilot's auto-review is a verifier
  signal, fed into the verifier Activity's input. Not a role.
- `Mini` — Mini is an **execution surface** for implementer (kitty
  + Claude Code goal mode), the same way codex/claudecode/copilot
  drivers are. Octi dispatches a ticket to Mini's task queue;
  Mini doesn't hold a role binding.
- Mechanical kanban state transitions (e.g., create ticket on spec
  merge, mark ticket done on PR merge) — these run as deterministic
  Octi workflows (spec 046). Board-grooming is the JUDGMENT layer
  on top, owned by the role.

### R2 — Capability schema (v1 frozen)

```yaml
agent_id: ares
schema: octi.capability.v1
roles:
  - name: spec_reviewer
    confidence: 0.75
    constraints:
      - never_review_own_work
  - name: implementer
    confidence: 0.40
availability:
  status: online                  # online | busy | offline
  in_flight: 0
  max_concurrent: 3
conflict_sets:                    # Clawta's generalization (R3)
  pr_reviewer:
    - never_review_pr_for_ticket_i_dispatched
    - never_review_pr_for_ticket_i_implemented
    - never_review_pr_for_ticket_i_repaired
```

Profile is **per-agent YAML** under
`swarm/octi/config/capability_profiles/`. v1 is frozen; v2 lives
in a parallel file (`*.v2.yaml`).

Confidence is **derived, not declared** (R7).

### R3 — `conflict_sets` (Clawta's generalization)

Beyond `never_review_own_work`, the schema names a `conflict_sets`
map per role. Example (clawta as pr-reviewer):

```yaml
conflict_sets:
  pr_reviewer:
    - never_review_pr_for_ticket_i_dispatched
    - never_review_pr_for_ticket_i_implemented
    - never_review_pr_for_ticket_i_repaired
```

The router evaluates conflict_sets BEFORE confidence ranking.
A candidate is dropped if ANY conflict_set predicate matches the
current handoff packet's history.

Conflict evaluation is a pure function `(profile, packet) → bool`,
recorded as part of the routing decision OctiEvent.

### R4 — HandoffPacket (typed, verifier-asserted)

```go
type HandoffPacket struct {
    Schema             string   `json:"schema"`         // "octi.handoff.v1"
    ArtifactRefs       []string `json:"artifact_refs"`  // [spec_path, ticket_id, pr_url]
    SpecRef            string   `json:"spec_ref"`       // ".specify/specs/NNN-<slug>/"
    TicketRef          string   `json:"ticket_ref"`     // "t_<id>" or ""
    SourceCitations    []string `json:"source_citations"`
    AcceptanceCriteria []string `json:"acceptance_criteria"`
    AllowedPaths       []string `json:"allowed_paths"`     // file-system scope
    ForbiddenPaths     []string `json:"forbidden_paths"`   // explicit exclusions
    NextRole           string   `json:"next_role"`
    ProducerAgentID    string   `json:"producer_agent_id"`
    ReviewerAgentID    string   `json:"reviewer_agent_id,omitempty"`
    ConflictCheckResult string  `json:"conflict_check_result"` // "passed" | "violated:<rule>"
    DecisionLog        []string `json:"decision_log"`
    BlockingQuestions  []string `json:"blocking_questions"`
}
```

Verifier asserts at entry to next stage:

| Transition | Entry invariant |
|---|---|
| research → spec-writer | `source_citations` non-empty AND each `proposed_invariant` is falsifiable |
| spec-writer → spec-reviewer | spec.md has all 5 chitin sections (per spec 020 + 024) AND `allowed_paths` non-empty |
| spec-reviewer → operator-ratify | `ReviewerAgentID != ProducerAgentID` AND `ConflictCheckResult == "passed"` |
| operator-ratify → implementer | spec status=ratified (merged) AND ticket exists with `assignee=<implementer_agent_id>` |
| implementer → verifier | CI green AND branch matches `agent/<x>-<ticket>` AND test-first hook passed |
| verifier → operator-merge | `ConflictCheckResult == "passed"` (verifier ≠ implementer) AND all CI signals green |

Failure raises an Activity error; the producing stage gets re-routed.

### R5 — Routing function (deterministic, replayable)

```go
func RouteStage(role string, packet HandoffPacket, profiles []CapabilityProfile) RoutingDecision {
    candidates := filter(profiles, func(p) bool {
        return hasRole(p, role) &&
               isRoutable(p.availability.status) &&
               p.availability.in_flight < p.availability.max_concurrent &&
               !violatesConflictSets(p, packet)
    })
    // tie-break: confidence desc, in_flight asc, agent_id asc (stable)
    sort(candidates, by(-confidence, +in_flight, +agent_id))
    chosen := candidates[0]
    return RoutingDecision{
        Role: role,
        Candidates: candidates, // full list for audit
        Chosen: chosen.agent_id,
        Reason: humanReason(chosen, candidates),
        PolicyVersion: "v1",
    }
}
```

Decision is recorded as `octi.routing.decision` OctiEvent
(extending spec 041 schema). "Why this agent this time" is one
`jq` query against the mirror.

**Availability status enum (v1, frozen)**: `online` | `busy` |
`offline` | `pool` | `always_online`. `isRoutable(status)` returns
true for `online`, `pool`, and `always_online`; false for `busy`
and `offline`. Rationale: `pool` (driver workers spawned on
demand — codex, claudecode) and `always_online` (GitHub-hosted
verifier signals — copilot, CI) are both eligible for routing;
only `busy`/`offline` agents are filtered out. A bare `online`
agent past its `max_concurrent` is filtered by the in-flight
check, not the status check.

### R6 — Initial role assignment (v1 capability profiles)

**Operator override (2026-05-19, mid-draft)**: research moves from
claude (originally proposed primary) to **Ares** (autonomous);
**board-groomer** is added with **Ares** as primary. Ares now
holds 3 of 6 roles: research, spec-reviewer, board-groomer.
This concentrates the bottleneck on Ares deliberately —
operator owns the trade.

Reconciled with thread 18's spec-factory.md operating reality:

| Agent | Roles (confidence) | Notes |
|---|---|---|
| **ares** (Hermes/GLM 5.1) | **research (.85)**, spec-reviewer (.85), **board-groomer (.80)**, pr-reviewer-via-verifier-signal (.85) | Owns the front + middle of the line. `never_review_own_work` + research-author soft guard via cited-criticism verifier check |
| **claude** | spec-writer (.90) | Primary spec authorship; consumes Ares' research output + operator priority |
| **clawta** (OpenClaw/Codex 5.5) | implementer (.85) | Canonical implementer; test-first contract via worker-pre-commit hook |
| **codex / claudecode / copilot drivers** | implementer-driver (.70) | Single-task spawn; Octi-dispatched per-ticket |
| **mini** (Claude Code CLI + kitty + goal mode) | execution-surface only | Not a role; implementer dispatches TO Mini for long-horizon work |
| **github-copilot** | verifier-signal (1.0) | Auto-review on PR open; output feeds verifier Activity |
| **CI (chitin-gated)** | verifier-signal (1.0) | Tests, security, base-freshness, scope check |
| **claude-code (HITL with red)** | operator-assist (.95) | **Explicitly NOT in autonomous routing** — claude-code is red's session assistant |

Fallbacks (when Ares is offline or over-allocated):
- research → claude (.65), with cited-source requirement (R6's
  research-author soft guard does NOT apply since claude won't
  also review)
- spec-reviewer → clawta (.65), constrained by `conflict_sets`
  (never reviews specs for tickets clawta dispatched / implemented
  / repaired)
- board-groomer → operator (red) — explicit operator role, NOT
  another agent; rationale: board judgment requires holistic
  context that only the operator reliably has when Ares is offline

Routing transparency in OctiEvent shows degraded choices so red
sees them.

**Research-author soft guard** (R6 amendment):
Since Ares produces research AND reviews specs derived from that
research, the spec-reviewer verifier Activity asserts:
- review body cites ≥1 specific invariant or scope-criticism
- review body is ≥200 characters
- review references the source citations from the originating
  research, distinguishing "I produced this research" from "I
  approve this spec without engagement"
A rubber-stamp review fails the verifier and the spec stays
unmerged. This is a soft guard, not a hard conflict — Ares CAN
review specs derived from its research, but must engage
substantively.

### R7 — Derived confidence (claude-code's contribution, ratified)

Confidence values in capability profiles are **bootstrapped**
manually at v1, then **derived** going forward by a nightly
`ConfidenceUpdateWorkflow` that:

1. Reads `octi.verifier.result` events from the past 30 days
2. Computes `exponentially_weighted_pass_rate(agent_id, role)`
   with half-life = 7 days
3. Updates the profile YAML in a git PR (operator reviews
   significant deltas)
4. Manual confidence edits outside the workflow require a
   `// spec: 049 R7-confidence-override` comment AND operator
   approval

This closes the "agent overclaims after model upgrade" hole all
three proposals flagged.

### R8 — Bottleneck honored and wider (Ares' structural truth, amplified)

Per Ares' own proposal: the reviewer bottleneck is structural —
adding reviewers does not fix it; the throughput ceiling is the
time it takes to do a careful review.

R6's operator override **widens** the bottleneck: Ares now holds
research (front of the line) + spec-reviewer (middle gate) +
board-groomer (post-ratification grooming). When Ares is offline,
three stations stall, not one.

This spec does NOT pretend to solve the bottleneck. It makes it
**visible per role** and falls back deliberately:

- Every routing-stalled stage emits `octi.stage.role_pending`
  at T+0 with the role name and current queue depth
- `max_role_wait` timer per role:
  - research: 24h (autonomous; long async expected)
  - spec-review: 4h
  - board-groom: 8h
  - impl-review (via verifier signal): 8h
- On timer expiry, emit `octi.stage.role_overdue` → posts to
  operator inbox in red's #ares channel (since Ares owns these
  roles), NEVER auto-approves and NEVER auto-routes-to-fallback
  silently
- Operator decides: nudge Ares, manually engage fallback
  (claude/clawta/red), or accept the stall

R8 is an explicit anti-pattern guard: **no auto-approval, no
auto-bypass of reviewer, no "operator pressure to unblock the
line" code path, no silent fallback rotation**. The line has a
hard throughput ceiling measured in specs per day, capped by
Ares' careful-review pace. Operator owns the trade.

If sustained Ares-bottleneck becomes intolerable, the structural
fix is one of:
1. Reassign research OR board-groomer back to claude/clawta
   (operator decision, with capability profile edit per R7)
2. Decompose Ares' roles across multiple Hermes agents (spec
   amendment required)
3. Accept the throughput ceiling

None of these are auto-resolved by the system. All are operator
choices made on signal.

### R9 — Inter-agent comms only via artifacts (per operator directive)

Per the 2026-05-19 channel reorganization: agents communicate
**only** through:

- **kanban tickets** (assignee transitions, status updates,
  comments)
- **spec PRs** (review comments)
- **code PRs** (review comments)
- **agent-bus threads** with `audience=<single_agent>` (operator
  ↔ agent only, not agent ↔ agent)

There is NO shared chat surface. Multi-audience bus threads
(`audience=ares,clawta`) are **bus-only** in v1 — they appear in
each agent's inbox-at-session-start (per spec 023) but do NOT
mirror to Discord. Fan-out to per-agent Discord channels with
shared anchor is a v1.1 capability (spec 042 §R2 covers the
implementation).

This is the operator's "keep them away from each other" directive
made architectural.

### R10 — Operator at two HITL gates only

```
research → spec-writer → spec-reviewer → [OPERATOR RATIFIES SPEC] → implementer → verifier → [OPERATOR MERGES PR]
                                          ↑ HITL gate #1                                       ↑ HITL gate #2
```

Everything between the two gates runs autonomously. Operator is
NOT scheduled into the assembly line as a role — the gates are
event boundaries the operator opens by hand (merging the spec PR,
merging the impl PR). Operator-side tooling (`/peer-review`,
`/merge-spec`, etc.) lives in `workspace/claude/skills/` and is
out of scope for this spec.

### R11 — Telemetry events (extends spec 041)

| Event | Fields | When |
|---|---|---|
| `octi.role.claimed` | agent_id, role, ticket_id, confidence_at_claim | start of work |
| `octi.routing.decision` | candidates[], chosen, reason, policy_version, conflict_check_result | every dispatch |
| `octi.handoff.created` | from_role, to_role, packet, producer_agent_id | stage exit |
| `octi.verifier.result` | invariant_id, pass/fail, evidence_path, agent_id, role | every transition |
| `octi.review.requested` | reviewer_agent_id, artifact_ref, deadline | spec/impl PR opens |
| `octi.review.decision` | reviewer_agent_id, decision, artifact_ref, cites_count | review submitted (cites_count = # invariants cited, R6 soft guard) |
| `octi.board.groom_decision` | groomer_agent_id, ticket_id, action (demote/veto/dup_flag/repri), reason | board-groomer issues a judgment |
| `octi.research.surfaced` | researcher_agent_id, candidate_ticket, source_citations[], priority | research stage produces output |
| `octi.stage.role_pending` | role, agent_id, since_unix_ns, queue_depth | T+0 |
| `octi.stage.role_overdue` | role, agent_id, elapsed_ms, threshold_ms | timer expiry (R8 max_role_wait) |
| `octi.bottleneck.detected` | role, agent_id, queue_depth, since_unix_ns | queue > N for > T |
| `octi.confidence.updated` | agent_id, role, old, new, evidence_event_ids[] | nightly ConfidenceUpdateWorkflow |

All events mirror to chitin event store per spec 041 §R2.

### R12 — Audit queries are one-liners

```bash
# "what is every agent doing right now"
jq -r 'select(.event_type == "octi.role.claimed") | "\(.payload.agent_id) \(.payload.role) \(.payload.ticket_id)"' \
   ~/.chitin/octi-events-$(date -u +%F).jsonl | sort -u

# "which routing decisions hit a conflict_set this week"
jq -r 'select(.event_type == "octi.routing.decision" and .payload.conflict_check_result != "passed")' \
   ~/.chitin/octi-events-*.jsonl

# "role overdue summary"
jq -r 'select(.event_type == "octi.stage.role_overdue")' \
   ~/.chitin/octi-events-*.jsonl
```

These three queries become operator-CLI verbs:
- `octi-roles snapshot` (R12 query 1)
- `octi-roles conflicts` (R12 query 2)
- `octi-roles overdue` (R12 query 3)

## Acceptance criteria

1. `swarm/octi/config/capability_profiles/` contains YAML for
   each named agent in R6 (7 files: ares, claude, clawta, mini,
   copilot, codex, claudecode).
2. `RouteStage()` returns deterministic results on the same
   input — fixture test asserts identical RoutingDecision over
   100 invocations.
3. `conflict_sets` evaluation drops a candidate that matches any
   predicate — e2e test with Clawta dispatching ticket X then
   being filtered from pr-review for ticket X.
4. `HandoffPacket` verifier rejects a transition missing required
   fields per R4 — e2e test fires the failure for each transition.
5. `ConfidenceUpdateWorkflow` runs nightly; e2e test simulates
   30 days of verifier results and asserts confidence updates
   are within ±0.05 of expected exponentially-weighted pass rate.
6. Role overdue: spec-review with no response for >4h emits
   `octi.stage.role_overdue` and posts to red's inbox —
   verified by mock-clock e2e.
7. `octi-roles snapshot` returns the current set of
   `octi.role.claimed` events with no terminal_ts; matches
   thread 18's live state.
8. Inter-agent comms isolation: e2e asserts no Discord mirror
   fires for an `audience=ares,clawta` bus thread post (v1
   bus-only behavior).
9. Two-gate operator model: e2e asserts a workflow blocks at the
   `operator-ratify` boundary and at the `operator-merge`
   boundary; no auto-bypass.
10. `workflowcheck` passes on all of
    `swarm/octi/roles/`; CI gate per spec 040 §R2 enforces.
11. Reconciliation with thread 18: PRs #928 + #929 (the live
    factory-line specs) re-run through the formal Octi role
    routing produces the same outcomes (claude wrote, ares
    reviews); e2e parity test.

## Test coverage

- `swarm/octi/roles/router_test.go` — unit: R5 determinism,
  conflict_sets evaluation, tie-break stability
- `swarm/octi/roles/conflict_test.go` — unit: R3 predicate
  evaluation
- `swarm/octi/roles/verifier_test.go` — unit: each R4 transition's
  entry invariant
- `swarm/octi/roles/confidence_update_test.go` — unit: R7
  exponentially-weighted pass rate
- `swarm/octi/e2e/role_routing_test.go` — **e2e**: AC2, AC3,
  AC4, AC7
- `swarm/octi/e2e/role_reviewer_overdue_test.go` — **e2e**: AC6
- `swarm/octi/e2e/role_two_gates_test.go` — **e2e**: AC9
- `swarm/octi/e2e/role_factory_parity_test.go` — **e2e**: AC11
  (parity with thread 18's PRs #928, #929)
- `swarm/octi/e2e/role_confidence_update_test.go` — **e2e**: AC5

All files carry `// spec: 049-octi-swarm-role-architecture`.

## Invariants

- **I1**: every role assignment is recorded as a routing decision
  in the OctiEvent stream. "Why this agent this time" is
  replayable.
- **I2**: reviewer ≠ author for the same spec, and reviewer ≠
  implementer for the same impl PR. Conflict_sets enforce.
  Verifier rejects on violation.
- **I3**: operator is external — autonomous routing never crosses
  the two HITL gates. No auto-approve, no auto-merge.
- **I4**: agents communicate only via artifacts (kanban, PR,
  spec) and per-agent bus threads. No shared chat surface in v1.
- **I5**: confidence is derived, not declared. Manual edits
  require explicit override + operator approval.
- **I6**: Mini is an execution surface, not a role. Same for
  codex/claudecode/copilot drivers — they're implementer-driver
  endpoints.
- **I7**: kanban state transitions (claim, assign on merge, mark
  done) run as deterministic Octi workflows (spec 046). The
  **judgment layer** above them — stale demotion, dependency
  veto, duplicate flag, priority calls — is the **board-groomer
  role** (R1), owned by Ares (R6).
- **I8**: reviewer bottleneck is visible, not solved. Throughput
  ceiling honored; no operator-pressure-driven auto-approval
  code path exists.

## Out of scope

- New role inventory beyond the 5 in R1 (v2 spec required)
- Multi-reviewer parallel review (Ares' bottleneck is structural
  per R8; parallel review is a v2+ topic)
- Operator-as-inline-role (operator stays external per R10;
  if a workflow needs operator inline, it declares `requires_operator`
  and blocks until signal — see spec 040 §R10)
- Cross-platform agent identities (Slack, Telegram agents) — v1
  is Discord + bus only
- Auto-tuning confidence weights (half-life, evidence window) —
  fixed at v1; tuning is a v1.1 spec
- Spec 049 changes do NOT modify thread 18's `spec-factory.md`
  skill content beyond a pointer reference; that file is operator
  docs, this file is contract

## References

- Parent: spec 038 (Octi master)
- Foundation: spec 040 (scaffolding) — `workflowcheck` + gate floor
- Event mirror: spec 041
- Identity contract: spec 042 (multi-audience fan-out semantics)
- Per-channel mention routing: spec 047 (related but now narrowed
  given the channel architecture change)
- Test-first contract: spec 020
- File-system scope convention: spec 024
- Ratification thread: agent-bus thread 19 (msg 7717 RFP, msg
  7722 Clawta, msg 7726 claude-code, msgs 7740-7743 Ares)
- Operating sibling: `workspace/claude/skills/spec-factory.md` +
  `workspace/claude/skills/spec-factory-queue.md`
- Live evidence: PRs #928 (spec 020), #929 (spec 022) on
  bench-devs-platform — first specs through the line before
  this spec ratified
- Channel architecture: 2026-05-19 operator directive — only
  #ares + #clawta survive; inter-agent comms via artifacts only
