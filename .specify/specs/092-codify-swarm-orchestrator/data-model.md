# Data Model: The §7 Amendment

**Feature**: 092-codify-swarm-orchestrator
**Date**: 2026-05-22

This is not a runtime data model — it's the structural decomposition of the §7 constitution amendment, so the implementation phase has a clear inventory of what gets added to `.specify/memory/constitution.md` and how the pieces relate.

## The amendment, decomposed

The §7 amendment is a single textual edit to `.specify/memory/constitution.md`, consisting of two distinct components:

### Component 1: Amendment HTML comment (prepended)

A block comment at the top of the file (immediately after the `# Chitin Repo Constitution Overlay` heading, before or after the existing 2026-05-20 amendment), of the form:

```html
<!--
Amendment 2026-05-22 — §7 added: "The swarm is the orchestrator."
Codifies the post-2026-05-20 SDLC refocus as gate-level truth. Driver
table enumerated (claudecode, openclaw=Clawta+GLM-5.1, hermes=Ares+
GPT-5.5, codex, copilot, local-llm, gemini). Implementation gate (MUST):
no implementation PR may be opened, no implementation file mutated, no
destructive implementation action may execute unless the work has first
entered the orchestrator as either a DAG-resolved work-unit or an
orchestrator-intaked ad-hoc work-unit. Ad-hoc top-of-funnel work
(reports, reviews, spec creation) remains kernel-gated but DAG-free.
§5 (board-aware scripts) and §6 (swarm/ as transitional housing) are
superseded where they describe kanban or swarm/ as live driving surfaces.
Ratified through multi-agent review with Ares (hermes) + Clawta
(openclaw) + operator (Jared); deep research grounding the architecture
in industry consensus lands separately at
docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md.
-->
```

### Component 2: §7 section (appended)

The full §7 prose as preserved in `contracts/canonical-section-7.md`, appended to the end of `.specify/memory/constitution.md`. Structurally §7 contains:

```
## 7. The swarm is the orchestrator
├── Opening declaration            (3 sentences: "swarm IS orchestrator", "not a/not a/not a", "orchestrator drives")
├── Three load-bearing parts       (bulleted: deterministic orchestration / telemetry / kernel gating)
├── Driver paragraph + table       (7 driver rows + SelectDriver note)
├── Implementation gate (MUST)     (1 paragraph: the load-bearing rule)
├── Hierarchical paragraph         (1 paragraph: supervisor / capability-scoped executors)
├── 4-layer safety paragraph       (1 paragraph: model / harness / tools / environment mapping)
├── Ad-hoc work paragraph          (1 paragraph + 6 ✅/❌ examples)
├── Reactive-work paragraph        (1 paragraph: ad-hoc work-units still gate-bound)
├── Bypass-paths paragraph         (1 paragraph: drift to eliminate)
├── Substrate-deferral paragraph   (1 paragraph: MCP Tasks etc. are feature decisions)
├── Supersedes block               (3 bullets: agent-as-member / §5+§6 / legacy paths)
└── Sources-of-truth paragraph     (1 paragraph: console diagrams + strategy doc)
```

## Relationships

| §7 component | Refers to | Type of reference |
|---|---|---|
| Opening declaration | spec 070 | predecessor (orchestrator is the swarm because spec 070 built it) |
| Driver table | model identities for openclaw/hermes | operational fact (GLM-5.1, GPT-5.5) |
| Driver table | SelectDriver activity, spec 076 | predecessor (capability-tag routing) |
| Implementation gate | spec-kit task DAG | mechanism (the gate references how DAGs originate) |
| Hierarchical paragraph | constitution §2 | reinforcement (worker+worktree discipline) |
| 4-layer safety paragraph | (no explicit citation — vendor-name rule) | mental model only |
| Supersedes block | constitution §5, §6 | supersession (named explicitly) |
| Supersedes block | spec 087 | concurrent (kanban substrate retirement in flight) |
| Supersedes block | agent-bus mention listeners, kanban pull loops, etc. | retired (spec 069 + others) |
| Sources-of-truth | apps/chitin-console diagrams + strategy doc | external (visual source) |

## Validation rules (FRs from spec.md, restated as data invariants)

After the amendment lands, the file MUST satisfy:

| Invariant | Check |
|---|---|
| Section 7 exists with the correct heading | `grep '^## 7\. The swarm is the orchestrator' .specify/memory/constitution.md` matches |
| Amendment block is dated 2026-05-22 | `grep 'Amendment 2026-05-22' .specify/memory/constitution.md` matches |
| Driver table enumerates all 7 drivers | each of {claudecode, openclaw, hermes, codex, copilot, local-llm, gemini} appears in a code-fenced row |
| The implementation-MUST sentence is present verbatim | `grep 'No implementation PR may be opened' .specify/memory/constitution.md` matches |
| Supersession block names agent-as-member | `grep 'agent-as-swarm-member' .specify/memory/constitution.md` matches |
| No vendor names in §7 body | `awk '/^## 7\./,/^## 8\.|^EOF$/' .specify/memory/constitution.md \| grep -ciE 'Anthropic\|OpenAI\|Cognition\|Devin'` returns 0 |

## State transitions

There is one state transition: **pre-amendment → post-amendment**.

| State | constitution.md content | Spec-kit Constitution Check behavior |
|---|---|---|
| Pre-amendment | §1-§6 only | Gates against existing 6 rules |
| Post-amendment | §1-§7, with §5/§6 marked as superseded where kanban/swarm-folder framing applied | Gates against 7 rules; the supersession block disambiguates §5/§6 conflicts with §7 |

No mid-state. The amendment is atomic — one PR, one commit, one merge.
