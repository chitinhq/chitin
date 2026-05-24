# Feature Specification: Retire pre-chitin-v2 / pre-orchestration skills

**Feature Branch**: `feat/089-retire-pre-v2-skills`

**Created**: 2026-05-22

**Status**: Draft

**Input**: User description: "Cull all workspace skills created before chitin v2 and before the orchestration layer landed. The dead catalog (sitrep, leverage, go, quest, peer-review, roadmap, forge, ship, ship-review, brainstorm, triage, spec-factory, spec-factory-queue) was still being fed to Claude Code via `/home/red/workspace/CLAUDE.md`, causing the assistant to suggest deprecated skills (`/sitrep`, `/leverage`, `/go`) as live grounding tools. Symptom surfaced today: investigation of an unrelated bug (clawta-on-Discord) was followed by stale-catalog suggestions that the operator had to correct three times. Retire the 13 dead skills and strip their catalog entries from workspace CLAUDE.md so future sessions are grounded against current substrate (Temporal orchestrator, governance gate v2, spec-kit flow, swarm-controller, chitin-console)."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — A fresh Claude Code session is grounded against the current substrate (Priority: P1)

The operator opens a new Claude Code session in the workspace. The assistant reads `/home/red/workspace/CLAUDE.md` and sees only skills whose substrates still exist: `/rollout`, `/sentinel`, `/evolve`, `/graphify`, `/wiki`. When the operator asks "what skill grounds me in current state?", the assistant does not suggest `/sitrep`, `/leverage`, or `/go` — because those entries are gone from the catalog AND the underlying skill files are absent from `claude/skills/`. The operator no longer has to correct the assistant's mental model.

**Why this priority**: The wrong-skill suggestion loop wastes session time and erodes trust. Today the assistant suggested `/sitrep` → operator: "deprecated" → assistant suggested `/leverage` and `/go` → operator: "Same, leverage and go" → assistant suggested `/sentinel` as a grounding skill before catching itself. That's three corrections inside one short exchange, all caused by a stale catalog. Removing the catalog at the source makes the loop impossible.

**Independent test**: From a fresh shell, `grep -E '^### /(sitrep|leverage|go|quest|roadmap|forge|peer-review|spec-factory|spec-factory-queue|brainstorm|ship|ship-review|triage)' /home/red/workspace/CLAUDE.md` returns zero hits. `ls /home/red/workspace/claude/skills/ | grep -E '^(sitrep|leverage|go|quest|roadmap|forge|peer-review|spec-factory|spec-factory-queue|brainstorm|ship|ship-review|triage)\.md$'` returns zero hits. Asking a fresh Claude Code session "what skill should I run to get on track?" does not produce any of those skill names.

---

### User Story 2 — The remaining skills still work and stay catalogued (Priority: P1)

The retained skills (`/rollout`, `/sentinel`, `/evolve`, `/graphify`, `/wiki`, plus the 5 wired chitin commands `/gate`, `/queue`, `/verdict`, `/invariant`, `/hermes-unblock`, plus `/land`, `/mine`) keep their catalog entries in workspace CLAUDE.md unchanged. Their skill files remain at `claude/skills/`. They are unaffected by the cull.

**Why this priority**: Same-as-P1 — the value of the cull is in keeping the live catalog accurate, not just smaller. A cull that accidentally drops `/sentinel` or `/rollout` would be worse than no cull.

**Independent test**: After the cull, `grep -E '^### /(rollout|sentinel|evolve|graphify|wiki)' /home/red/workspace/CLAUDE.md` returns 5 hits. `ls /home/red/workspace/claude/skills/{rollout,sentinel,evolve,graphify,wiki,land,mine}.md` returns 7 files. Running any of those skills produces the same behavior as before this spec.

---

### Edge Cases

- **A future operator wants `/sitrep`-style behavior.** Out of scope. The current substrate has its own grounding move (read `specs/INDEX.md`, `architecture.md`, `docs/operating-model.md`, recent specs). If a true situation-report skill is wanted against the new substrate, it ships as a new feature, not a revival of the dead one.
- **A skill is borderline (`/ship`, `/ship-review`, `/triage`).** Each was retired in this cull because each leaned on the dead model (clawta binary build/deploy for `/ship`, soul-routing + Hamilton lens for `/ship-review`, "use GitHub issues" for `/triage`). If the *function* of any of these (release sweep, multi-agent review, ecosystem triage) is wanted again against the current substrate, it ships as a new spec.
- **The `peer-review.md` file has uncommitted local modifications.** It is on the cull list; `git rm` resolves the conflict by deleting both the working tree and index entries. The operator's in-flight modification is lost; this is acceptable because the skill itself is being retired.
- **Workspace constitution.md, `.specify/integrations/`, `.superpowers/`, and other untracked items are unaffected.** This cull touches only `claude/skills/*.md` (the 13 named files) and `/home/red/workspace/CLAUDE.md` (the catalog block).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The following 13 files MUST be removed from the workspace repo via `git rm` (preserving deletion history): `sitrep.md`, `leverage.md`, `go.md`, `quest.md`, `peer-review.md`, `roadmap.md`, `forge.md`, `spec-factory.md`, `spec-factory-queue.md`, `brainstorm.md`, `ship.md`, `ship-review.md`, `triage.md` — all under `claude/skills/`.
- **FR-002**: The workspace `CLAUDE.md` catalog blocks for `/roadmap`, `/sitrep`, `/leverage`, `/go`, and `/quest` (lines 5–28 in the current file) MUST be removed. A single one-line note MAY replace them, pointing future readers at this spec for the "why".
- **FR-003**: The retained catalog entries (`/rollout`, `/sentinel`, `/evolve`, `/graphify`, `/wiki`) MUST NOT be altered. Their content, ordering, and sub-bullet structure stay byte-identical except for the line-number shift from removed blocks above them.
- **FR-004**: The retained skill files in `claude/skills/` (`rollout.md`, `sentinel.md`, `evolve.md`, `graphify.md`, `wiki.md`, `land.md`, `mine.md`, `gate.md`, `invariant.md`, `hermes-unblock.md`, `verdict.md`) MUST NOT be deleted or modified by this cull.
- **FR-005**: The cull MUST land as one workspace-repo commit referencing this spec. Commit subject: `chore: retire pre-v2/pre-orchestration skills (chitin spec 089)`. Commit body lists the 13 files removed and references this spec.
- **FR-006**: The chitin-repo spec 089 (this file + checklist) is committed separately on `feat/089-retire-pre-v2-skills`. The two commits cross-reference each other in their bodies for traceability.
- **FR-007**: This cull MUST NOT touch the chitin repo's `.claude/commands/` (those 5 are wired and live) or any other repository.

### Success Criteria *(mandatory)*

- **SC-001 (Catalog is current)**: `grep -E '^### /(sitrep|leverage|go|quest|roadmap|forge|peer-review|spec-factory|spec-factory-queue|brainstorm|ship|ship-review|triage)' /home/red/workspace/CLAUDE.md` returns zero hits after merge.
- **SC-002 (Files are gone)**: `find /home/red/workspace/claude/skills -type f -name '*.md' | sort` returns a list with none of the 13 culled names, and with all of the retained names present.
- **SC-003 (Retained skills still work)**: `/rollout status`, `/sentinel status`, `/wiki`, and the 5 wired chitin commands invoke without "skill not found" errors. (Verifiable by the operator at any time after merge.)
- **SC-004 (History preserved)**: `git log --diff-filter=D --name-only -- claude/skills/` on the workspace repo shows all 13 deletions in one commit attributed to this spec.

## Assumptions

- The workspace repo at `/home/red/workspace/` is git-tracked and the operator commits via the same identity (`Jared Pleva`).
- The 5 wired chitin commands (`gate`, `hermes-unblock`, `invariant`, `queue`, `verdict`) live in `chitin/.claude/commands/` and are unaffected by this cull. They were classified LIVE in the audit on grounds that they map to current substrate (governance gate, dispatch ops, sentinel, queue/ledger).
- The retained workspace skills (`rollout`, `sentinel`, `evolve`, `graphify`, `wiki`, `land`, `mine`) map to current substrate. If any of these later prove to also be stale, they are culled in a follow-up spec — this spec retires the named 13, no more, no less.
- `hermes-unblock.md` still references "clawta-poller" by name (replaced by `swarm-controller` per #908). Renaming the reference is OUT OF SCOPE for this spec — it lands as a follow-up patch or as part of a future spec touching dispatch documentation.
- Operator-owned data outside the workspace repo (`~/.openclaw/`, `~/.chitin/`, `~/.hermes/`, `~/.gstack/`) is NEVER touched by this cull.

### Scope

**In scope**:
- `claude/skills/{sitrep,leverage,go,quest,peer-review,roadmap,forge,spec-factory,spec-factory-queue,brainstorm,ship,ship-review,triage}.md` (delete, 13 files)
- `/home/red/workspace/CLAUDE.md` (strip dead catalog blocks, lines 5–28)
- `specs/089-retire-pre-v2-skills/` in chitin (this spec)

**Out of scope**:
- Any non-named workspace skill (e.g. retained skills, OR skills not yet classified)
- chitin repo source under `apps/`, `go/`, `libs/`, `services/`, `swarm/`
- chitin `.claude/commands/` (the 5 wired commands)
- The `hermes-unblock.md` clawta-poller → swarm-controller rename (separate follow-up)
- `~/.gstack/`, `~/.claude/skills/` (gstack operator skills — separate ownership)
- The chitin project CLAUDE.md (`/home/red/workspace/chitin/CLAUDE.md`)
- Designing replacement skills for any culled function

### Dependencies

- **Predecessor decisions:** chitin v2 cut; spec 070 (Temporal orchestrator); spec 081 (cron migration + board retirement); spec 069 (agent-bus + mini-mcp decommission); #908 (clawta-poller → swarm-controller); decision 2a (Soulforge↔Quest separation, party-model deprecation). Each predecessor made one of the 13 named skills obsolete.
- **No blockers**: This is pure deletion. Lands independently of in-flight specs 087 (kanban substrate retirement) and 088 (mention listener retirement).
