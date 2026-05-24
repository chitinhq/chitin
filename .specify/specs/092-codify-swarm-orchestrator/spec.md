# Feature Specification: Codify the swarm-is-orchestrator architecture (constitution §7)

**Feature Branch**: `feat/092-codify-swarm-orchestrator`

**Created**: 2026-05-22

**Status**: Draft (ratified through multi-agent review)

**Input**: User description: "Codify the swarm-is-orchestrator architecture as chitin constitution §7. The chitin swarm IS the Go Temporal orchestrator (spec 070), not a collection of agents. Agents are DRIVERS — claudecode (operator-pair), openclaw (Clawta, async execution surrogate, GLM-5.1), hermes (Ares, async spec/review/coordination surrogate, GPT-5.5), codex, copilot, local-llm, gemini. Implementation work MUST flow through orchestrator-dispatched work-units (DAG-resolved or ad-hoc); top-of-funnel work (reports, research, code reviews, spec creation) MAY enter ad-hoc but is still kernel-gated. Ratified through multi-agent review with Ares + Clawta + operator."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — All drivers read the same architectural ground truth (Priority: P1)

A new Claude Code, Hermes, or OpenClaw session starts. Their session-start path reads `.specify/memory/constitution.md`. They see §7 stating: "The chitin swarm IS the Go Temporal orchestrator. Agents are DRIVERS." When the operator (or another driver) tries to "have Clawta change a bunch of code" without a spec, the receiving driver rejects on the constitutional gate rather than producing a PR. When the operator says "Ares, write a spec for X," Ares produces `spec.md` and opens a PR — that path is constitutionally allowed.

**Why this priority**: this is the deliverable. Today three drivers operate from incompatible mental models — Clawta describes itself as a reviewer while the model-tier signal says it's a worker; Claude Code rediscovers the orchestrator-as-driver framing every session; Ares re-derives its role from scratch each time. Until the constitution names the architecture in one ratified place, every session burns cycles re-discovering it.

**Independent test**: in a fresh Claude Code session, ask "what is the swarm?" — the answer cites §7's load-bearing sentence verbatim (or near-verbatim) and names drivers per the driver table. Repeat for a fresh Hermes/Ares session and a fresh OpenClaw/Clawta session. All three answers converge on the same architectural frame within one operator-question.

---

### User Story 2 — The implementation gate is empirically auditable (Priority: P1)

After §7 lands, a property test (planned as a follow-up spec) walks every PR merged to `main` and confirms each one is associated with an orchestrator-dispatched work-unit ID. The constitutional gate ("no implementation reaches a driver except via the orchestrator's dispatch") is not aspiration — it's a queryable invariant. A PR that lacks an orchestrator work-unit ID, or whose chain trail shows un-gated tool calls, surfaces as `invariant_violation:driver_bypass`.

**Why this priority**: a constitutional gate without an automated check is prose, not a gate. The constitution must be testable for it to be load-bearing.

**Independent test**: against a snapshot of recent merged PRs, run the bypass-invariant check (Sentinel-based). Expected outcome: zero violations for PRs landed after §7's merge date. Pre-§7 PRs may surface as expected drift, documented as historical baseline.

---

### Edge Cases

- **Existing §5 (Board-aware scripts) and §6 (Swarm tooling is the exception)** carry stale framing about kanban-as-runtime and `swarm/` as transitional housing. §7's supersession clause names them explicitly; they remain in the document as historical reference but no longer load-bearing where they conflict with §7.
- **A driver tries to invoke a tool call without going through `chitin-kernel gate evaluate`** — this is already blocked by §1 and the kernel itself; §7 reinforces but does not change that gate.
- **A reactive work-unit (Discord mention, cron trigger, operator escalation)** that needs to produce implementation MUST enter the orchestrator as an ad-hoc work-unit, not flow directly to a driver. §7 makes this explicit.
- **The MCP 2026-07-28 Tasks extension** is named as a candidate substrate for the implementation surface but explicitly NOT made constitutional. A separate spec evaluates it before promotion.
- **Clawta's self-model is currently inverted** (describes itself as reviewer rather than worker) — §7 fixes the canonical mapping. A small follow-up patch to Clawta's startup context aligns its self-description; not blocking on §7's landing.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: `.specify/memory/constitution.md` MUST gain a new top-level section §7 with the exact text ratified in this spec's Phase 1 design (the canonical wording lands in `plan.md` Phase 1 design).
- **FR-002**: A 2026-05-22 amendment block (HTML comment) MUST be added at the top of `.specify/memory/constitution.md`, naming what §7 adds + which earlier framings it supersedes.
- **FR-003**: §7 MUST contain a driver table enumerating: `claudecode`, `openclaw`, `hermes`, `codex`, `copilot`, `local-llm`, `gemini`. Each row names the driver's surface (terminal/Discord/dispatched) and purpose (operator-pair, async execution surrogate, async spec/review surrogate, implementation, PR-review, etc.). `openclaw` is identified with Clawta and GLM-5.1; `hermes` is identified with Ares and GPT-5.5.
- **FR-004**: §7 MUST contain a "Single-surface dispatch rule (MUST)" stating: no implementation PR may be opened, no implementation file mutated, no destructive implementation action may execute, unless the work has first entered the orchestrator as either a DAG-resolved work-unit or an orchestrator-intaked ad-hoc work-unit.
- **FR-005**: §7 MUST contain an "Ad-hoc work allowed" paragraph with concrete ✅/❌ examples covering: spec creation (✅), PR review (✅), research reports (✅), scheduled report cron (✅), "go change a bunch of code" (❌), cron-driven mutation (❌).
- **FR-006**: §7 MUST contain a hierarchy paragraph stating that the orchestrator is the supervisor and drivers are capability-scoped executors, and a 4-layer safety paragraph mapping model / harness / tools / environment to Chitin's stack.
- **FR-007**: §7 MUST contain a "Supersedes" block naming: (a) "agent-as-swarm-member" framing in earlier docs, (b) §5/§6 where they describe kanban or `swarm/` as live driving surface, (c) legacy implementation control paths (agent-bus mention listeners triggering mutations, Discord-native agent-to-agent implementation dispatch, kanban pull loops, watchdog/poller actuation, cron-driven worker dispatch).
- **FR-008**: §7 MUST cite the chitin-console diagrams (`apps/chitin-console/src/app/pages/{sdlc,orchestrator}-diagram.page.ts`) and the 2026-05-20 strategy doc as visual sources of truth.
- **FR-009**: The §7 amendment MUST NOT name Anthropic, OpenAI, Cognition, or other vendors directly in the constitution body. References use "industry-standard safety layering" framing. Citations live in `docs/strategy/` or `docs/decisions/`, not in the constitution.
- **FR-010**: The deep research report (Anthropic best practices, Cognition Devin-manages-Devins, Heterogeneous Swarms research, MCP 2026-07-28 implications) MUST land as a separate PR to `docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md`. NOT bundled with the §7 amendment PR.

### Success Criteria *(mandatory)*

- **SC-001 (Constitution carries §7)**: `grep -c '^## 7\. The swarm is the orchestrator' .specify/memory/constitution.md` returns 1 after the §7 amendment merges.
- **SC-002 (Driver table present)**: A grep for each of `claudecode`, `openclaw`, `hermes`, `codex`, `copilot`, `local-llm`, `gemini` finds at least one row in the driver table in §7.
- **SC-003 (Single-surface rule present)**: `grep -c 'No implementation PR may be opened' .specify/memory/constitution.md` returns 1.
- **SC-004 (Supersession block present)**: `grep -c 'agent-as-swarm-member' .specify/memory/constitution.md` returns at least 1.
- **SC-005 (Amendment block dated)**: `grep -c 'Amendment 2026-05-22' .specify/memory/constitution.md` returns 1.
- **SC-006 (No vendor names in constitution body)**: A search for `Anthropic`, `OpenAI`, `Cognition`, or `Devin` in the §7 prose body returns zero hits. (Amendment HTML comments may cite for traceability.)
- **SC-007 (Research doc landed separately)**: `test -f docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md` returns true after the companion PR lands.
- **SC-008 (Multi-driver alignment)**: a follow-up smoke check (post-merge) — three drivers each asked "what is the swarm?" — converge on the §7 framing without operator prompting.

## Assumptions

- The full text of §7 was drafted, edited, and ratified in conversation across multiple turns with Ares (hermes/GPT-5.5) and Clawta (openclaw/GLM-5.1) reviewing. The canonical wording (with all agreed edits — 4-layer paragraph, "implementation PR" tightening, supersession of §5/§6, no-vendor-names rule) is what gets committed.
- The driver table is canonical for now; future driver additions (local-llm specialization, gemini-specific role) are FR amendments, not constitution rewrites.
- Spec-kit skills already read `.specify/memory/constitution.md` as a gate (`/speckit-plan` Constitution Check) — that mechanism is unchanged. §7 just adds rules the gate evaluates.
- The "session-start" path varies by driver: Claude Code reads CLAUDE.md → SPECKIT pointer → spec; Hermes/Ares reads its own startup context; OpenClaw/Clawta reads its own. FR-001 ensures the constitution is the canonical truth source; downstream drivers' startup configs may need patching as a separate small operation (Clawta's self-model inversion is one such patch).
- The research report at `docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md` is reference material; it informs §7 but is NOT in §7's normative path.

### Scope

**In scope** (this spec's PR):
- `.specify/memory/constitution.md` — append §7, prepend 2026-05-22 amendment block.

**Out of scope** (separate PRs / specs / operator actions):
- `docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md` — companion research report (separate PR).
- Spec 091 AC amendments (`stop_hook_active` reentrancy, N-bounded forced continuations, orchestrator-marks-failed-after-N) — edit on the existing `feat/091-fix-clawta-lockdown-loop` branch.
- Clawta startup context patch (fixes inverted self-model) — operator action; may live outside the chitin repo.
- The "no-driver-bypass invariant test" Sentinel property test — separate spec.
- Plan-level review checkpoint — separate spec.
- Fan-out cap / scaling policy — separate spec.
- Verifier as first-class driver class — separate spec.
- MCP 2026-07-28 Tasks ingress evaluation — separate spec (gated behind spec 091 landing).
- Any changes to `apps/chitin-console/src/app/pages/{sdlc,orchestrator}-diagram.page.ts` — the diagrams are visual sources of truth that the spec cites; updates if needed are follow-ups.

### Dependencies

- **Predecessor (operational, already landed)**: spec 070 (Chitin Orchestrator), spec 076 (SchedulerWorkflow + SelectDriver), spec 077 (Spec-Kit adapter), spec 081 (Temporal Schedules + board retirement), spec 087 (kanban substrate retirement, in flight), the 2026-05-20 SDLC refocus strategy doc.
- **Predecessor (review)**: ratification by Ares (hermes) and Clawta (openclaw) — both signed off on the final wording with the agreed edits. Operator green-light received.
- **No code dependencies on other in-flight specs.** §7 lands independently of 087-091. Where §7 mentions kanban-as-runtime retiring "under spec 087," that's a forward reference; if spec 087 lands first, the wording is already correct; if §7 lands first, the reference reads as "the substrate is being retired in flight."
