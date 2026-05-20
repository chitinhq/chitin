# 054 — Octi assembly line: the canonical swarm operating procedure

> Parent: spec 038 (Octi master), spec 049 (role architecture).
> Substrate: specs 040-048 (Octi runtime).
> This spec is the **process spec** — it ties the role architecture
> (049) and the runtime (040-048) into one end-to-end, deterministic
> assembly line and names every stage, gate, handoff, and telemetry
> point. Once ratified it supersedes `workspace/claude/skills/spec-factory.md`
> as the canonical swarm operating procedure.
>
> Synthesized 2026-05-19 from the operator's stated vision across
> this session, the two RFP rounds (agent-bus threads 17 + 19), the
> three proposals (Ares/Clawta/claude-code), and Ares' spec reviews.
> Alignment sign-off from Ares + Clawta required before ratification.

## Summary

The swarm today produces noise: work happens async, unplanned —
"operator posts in a channel, who knows what happens." This spec
replaces that with a deterministic 10-stage assembly line with two
human-in-the-loop gates. Research surfaces a gap; a spec bounds it;
review hardens it; implementation ships it; a verifier proves it;
the operator merges it; the close emits telemetry that feeds the
next research cycle. Every stage emits replayable events, every
transition crosses a typed handoff contract, every Activity passes
the chitin gate. The chitin thesis — gate, record, replay, signal,
policy, improve — lifted from the tool-call level to the process
level.

## Ticket refs

- Parent: spec 038 (Octi — Deterministic Workflow Governance),
  spec 049 (Octi swarm role architecture).
- Substrate: specs 040 (scaffolding), 041 (event mirror), 042
  (agent-bus identity), 043 (dispatch), 044 (poller), 045 (bridge),
  046 (autonomous claim), 047 (mention routing), 048 (HA template).
- Ratification trail: agent-bus thread 17 (tech-pick RFP), thread
  19 (role-architecture RFP), thread 20/21 (role bindings).
- Supersedes on ratification: `workspace/claude/skills/spec-factory.md`
  (the informal factory-line skill that operated before this spec).

## File-system scope

Worker MAY write under:

- `.specify/specs/054-octi-assembly-line/**`
- `.specify/specs/INDEX.md` (add the 054 row)
- `swarm/octi/process/` — Go package: the top-level
  `AssemblyLineWorkflow` that sequences the 10 stages as child
  workflows
- `swarm/octi/process/tests/` — unit tests
- `swarm/octi/e2e/assembly_line_e2e_test.go` — the full-pipeline
  e2e (fixture spec idea → merged PR)
- `swarm/bin/octi-line` — operator CLI (`octi-line status`,
  `octi-line trace <spec-id>`)
- `workspace/claude/skills/spec-factory.md` — append a pointer
  marking this spec as the canonical successor (do NOT delete the
  skill file; it stays as the operator-facing quick reference)

Worker MUST NOT write under:

- `go/` (chitin kernel)
- `chitin.yaml` (governance policy)
- The substrate specs 040-049 — this spec references them, does
  not modify them
- Agent runtime code (`swarm/mini/`, hermes scripts, openclaw
  workflows)

## The thesis

Three layers, each governing the layer's unit of work:

| Layer | Name | Unit | Status |
|---|---|---|---|
| 1 — kernel | **chitin** | the tool call | shipped |
| 2 — harness | **Icarus** | the agent loop | in design |
| 3 — orchestration | **Octi** | the workflow + the process | this corpus |

Chitin is the base — the hard shell around soft agents. Octi runs
on top of it; this spec is Octi's process layer.

Eight non-negotiables, every stage honors all: spec-driven · SDD
workflow · e2e tests · guardrails · determinism · telemetry ·
replayable-from-telemetry-alone · auditable + improvable.

## The pipeline — 10 stages, 2 gates

Every stage is a **Temporal child workflow** of the top-level
`AssemblyLineWorkflow` (R5). The orchestration is deterministic Go;
the agent's actual reasoning inside a stage is a recorded Activity;
the two gates are Signal-await points.

```
[0] RESEARCH ──────────► Ares (autonomous)
[1] INTAKE ────────────► board-groomer (conflict-routed)
[2] SPEC AUTHORING ────► claude (spec-writer)
[3] SPEC REVIEW ───────► Ares (spec-reviewer) + Copilot (signal)
═══ GATE A — OPERATOR RATIFIES SPEC ═══  red merges spec PR
[4] BOARD GROOMING ────► Ares (board-groomer) — creates ticket
[5] IMPLEMENTATION ────► Clawta (implementer) | Mini | driver
[6] COPILOT REVIEW ────► github-copilot (verifier signal)
[7] AGENT PR REVIEW ───► Ares (pr-review signal)
[8] VERIFIER ──────────► Temporal verifier Activity + CI
═══ GATE B — OPERATOR MERGES PR ═══  red merges code PR
[9] CLOSE + LOOP ──────► Ares (board-groomer) → back to [0]
```

Everything between Gate A and Gate B runs autonomously. The
operator is external — exactly two touch points.

## Requirements

### R1 — Stage definitions

Each stage has a fixed `{owner role, trigger, produces, emits}`:

| # | Stage | Owner role | Produces |
|---|---|---|---|
| 0 | research | research | candidate triage ticket `{research_summary, proposed_invariants, source_citations, priority}` |
| 1 | intake | board-groomer (conflict-routed) | reviewed + prioritized triage queue; `triage → ready-for-spec` promotion (R2a) |
| 2 | spec authoring | spec-writer | `.specify/specs/NNN-*/spec.md` → spec PR |
| 3 | spec review | spec-reviewer | APPROVE / REQUEST_CHANGES on spec PR |
| 4 | board grooming | board-groomer | kanban ticket (assignee, `Spec:` ref, invariants_and_boundaries) |
| 5 | implementation | implementer | code PR on `agent/<driver>-<ticket>` |
| 6 | copilot review | (verifier signal) | inline PR comments |
| 7 | agent PR review | (verifier signal, Ares) | REVIEW / REQUEST_CHANGES |
| 8 | verifier | verifier Activity | `octi.verifier.result` pass/fail |
| 9 | close + loop | board-groomer | ticket → done; nightly confidence recompute |

### R1a — Stage 1: Intake (review · prioritize · promote)

Stage 0 (research) emits candidate triage tickets. Stage 1 closes
the gap between raw research and spec authoring:

- **Review** — is the candidate sound, non-duplicate, not already
  covered by an existing spec or open ticket? Unsound or duplicate
  candidates are merged or demoted with a reason.
- **Prioritize** — rank the surviving candidates (priority +
  dependency-unlock + age, per the spec 046 scoring family).
- **Promote** — move a candidate `triage → ready-for-spec`. This
  is the spec-worthiness decision (R2a).

**Owner: board-groomer, conflict-routed.** Ares owns board-grooming,
but Ares also owns research (stage 0) — so Ares grading its own
research's spec-worthiness is a conflict. Per spec 049 `conflict_sets`,
when the candidate under intake was produced by the *same agent*
acting as researcher, the intake routes to a non-conflicted agent
(**claude**, never the operator). Intake of another agent's research
stays with Ares. The routing decision is recorded as
`octi.routing.decision`.

### R2 — The two human-in-the-loop gates

- **Gate A** (after stage 3): operator merges the spec PR. No spec
  becomes work without it.
- **Gate B** (after stage 8): operator merges the code PR. No code
  ships without it.

### R2a — Spec-worthiness is autonomous; Gate A is the backstop

The intake owner promotes `triage → ready-for-spec` **autonomously**
— there is no third operator gate. If a spec gets authored that
should not have been, the operator rejects it at Gate A; the cost
is one wasted authoring cycle, and the `octi.verifier.result` +
routing telemetry surface the waste pattern so intake self-corrects.
The operator stays at exactly two gates.

Octi MUST NOT auto-cross either gate. No auto-merge, no
auto-approval, no operator-pressure bypass code path. Stages 4-8
run autonomously between the gates; stages 0-1 (research + intake)
run autonomously before Gate A.

### R3 — Roles (per spec 049, soft-routed)

Six roles, capability-routed: research, spec-writer, spec-reviewer,
board-groomer, implementer, verifier. Initial v1 assignment:
research/spec-reviewer/board-groomer = Ares; spec-writer = claude;
implementer = Clawta (Mini + drivers as execution surfaces);
verifier = Temporal Activity (Copilot + CI feed it). claude-code is
NOT in the autonomous loop — it is the operator's HITL session.

Routing is deterministic and recorded (`octi.routing.decision`).
Confidence is derived nightly from verifier outcomes, never
self-declared (spec 049 §R7).

### R4 — Handoff contract

Every stage transition passes a typed `HandoffPacket` (spec 049
§R4). The next stage's verifier Activity asserts the entry
invariant before work begins; failure re-routes the producing
stage.

### R5 — Determinism + telemetry

- **Every stage is a Temporal workflow.** The top-level
  `AssemblyLineWorkflow` sequences all 10 stages as Temporal child
  workflows; all of them are Temporal Go, `workflowcheck`-clean
  (spec 040 §R2). The workflow code (stage sequencing, routing,
  gate-await) is deterministic; the agent's actual reasoning
  inside a stage (research, authoring, review) is a **recorded
  Activity** — nondeterministic work captured as a replayable
  result. The two operator gates are `workflow.Signal`-await
  points. Nothing in the pipeline runs outside a Temporal
  workflow.
- Every stage emits OctiEvents mirrored into the chitin event
  store (spec 041). The full set: `octi.research.surfaced`,
  `octi.handoff.created`, `octi.routing.decision`,
  `octi.role.claimed`, `octi.review.decision`,
  `octi.board.groom_decision`, `octi.verifier.result`,
  `octi.stage.role_overdue`.
- The pipeline is replayable from the OctiEvent stream alone — no
  Temporal visibility API dependency (spec 041 §I1).
- Every Activity crosses the chitin gate before any side effect
  (spec 040 §R7).

### R6 — Communication rules

- Inter-agent communication is ONLY via artifacts: kanban tickets,
  spec PRs, code PRs. No agent-to-agent chat.
- Discord is operator↔agent only — `#ares` and `#clawta`, one
  channel per agent. No shared channel.
- The agent-bus is the replayable coordination record; Discord is
  the human-readable mirror.

### R7 — Reviewer bottleneck is honored, not hidden

Ares holds research + spec-review + board-groom. The throughput
ceiling is the careful-review pace; adding reviewers does not
raise it (spec 049 §R8). `max_role_wait` timers escalate a
stalled stage to the operator — never auto-approve, never silently
rotate to a fallback. The operator owns the throughput trade.

### R8 — The flywheel

The loop self-sustains: stage 9's close emits telemetry that
becomes a stage-0 research signal. Confidence sharpens nightly.
Each piece builds on the last. The operator touches only the two
gates. This is the "momentum" property — the line, once seeded,
runs without per-task operator direction.

### R9 — `octi-line` operator surface

`swarm/bin/octi-line`:
- `octi-line status` — every spec currently in the line, which
  stage, which agent, how long
- `octi-line trace <spec-id>` — full stage-by-stage history of one
  spec from research to merge, derived from the OctiEvent stream

### R10 — Supersede `spec-factory.md`

On ratification, `workspace/claude/skills/spec-factory.md` gets a
header pointer naming this spec as canonical. The skill file
remains as the operator-facing quick reference; this spec is the
contract.

## Acceptance criteria

1. The full-pipeline e2e (`assembly_line_e2e_test.go`) drives a
   fixture spec idea from a stage-0 research emission through to a
   stage-9 close, asserting each of the 10 stages fires in order
   with the correct owner role, and that each stage runs as a
   Temporal child workflow.
2. The two gates are enforced: the e2e asserts the workflow blocks
   at Gate A and Gate B and does not proceed without an operator
   merge signal.
3. Every stage transition emits the OctiEvents named in R5;
   `octi-line trace <spec-id>` reconstructs the full history from
   the event stream alone (no Temporal API call).
4. `octi-line status` lists in-flight specs with stage + owner +
   dwell time.
5. A stalled review past `max_role_wait` emits
   `octi.stage.role_overdue` and escalates to the operator — never
   auto-approves (asserted by mock-clock e2e).
6. `workflowcheck` passes on `swarm/octi/process/`.
7. Every stage Activity issues a chitin gate evaluation before its
   side effect (CI gate per spec 040 §R7).
8. Inter-agent comms isolation: the e2e asserts no stage completes
   via a chat message — every handoff is a kanban/PR/spec artifact.
9. `spec-factory.md` carries the canonical-successor pointer to
   this spec.
10. Ares and Clawta have posted alignment sign-off on the
    ratification thread before this spec's status moves to
    `ratified`.

## Test coverage

- `swarm/octi/process/process_test.go` — unit: stage sequencing,
  gate-blocking logic, with all stage child workflows mocked
- `swarm/octi/e2e/assembly_line_e2e_test.go` — **e2e**: AC1, AC2,
  AC3, AC8 — the full fixture-spec-to-merge pipeline
- `swarm/octi/e2e/assembly_line_gate_test.go` — **e2e**: AC2, AC5
  (gate enforcement + role-overdue escalation)
- `swarm/octi/e2e/octi_line_cli_test.go` — **e2e**: AC3, AC4
  (`octi-line status` + `trace`)

All test files carry `// spec: 054-octi-assembly-line`.

## Invariants

- **I1**: the pipeline has exactly 10 stages and exactly 2 operator
  gates. No stage is skippable; no gate is auto-crossable.
- **I1a**: every stage runs as a Temporal child workflow of
  `AssemblyLineWorkflow`. No stage executes outside a Temporal
  workflow; agent reasoning inside a stage is a recorded Activity.
- **I2**: every stage transition is a recorded OctiEvent — the
  full pipeline replays from telemetry alone.
- **I3**: inter-agent work moves only through artifacts (kanban,
  spec PR, code PR) — never through chat.
- **I4**: the operator is external — exactly two touch points
  (Gate A, Gate B). Everything else is autonomous.
- **I5**: every Activity crosses the chitin gate. Chitin is the
  floor; Octi cannot bypass it.
- **I6**: the reviewer bottleneck is visible (R7) — throughput is
  honestly capped, never faked with auto-approval.
- **I7**: confidence is derived from verifier history, never
  self-declared (spec 049 §R7).

## Out of scope

- Implementation of the substrate (specs 040-049 own that) — this
  spec sequences them, does not re-specify them.
- Multi-reviewer parallel review — the bottleneck is structural
  per R7; parallelizing review is a v2 topic.
- Operator-inline workflows — operator stays external per I4; a
  workflow needing operator mid-stream declares `requires_operator`
  and blocks (spec 040 §R10).
- Cross-repo assembly lines (chitin + readybench in one line) —
  v1 is one line per board.
- Changing the 8-point thesis or the 6-role inventory — those are
  ratified; this spec operationalizes them.

## References

- Parent: spec 038, spec 049
- Substrate: specs 040-048
- Determinism + telemetry: specs 040, 041
- Communication contract: spec 042
- Operating predecessor: `workspace/claude/skills/spec-factory.md`
- Ratification trail: agent-bus threads 17, 19, 20, 21
