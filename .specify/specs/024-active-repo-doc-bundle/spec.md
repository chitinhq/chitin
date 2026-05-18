# 024 — Active-repo doc-bundle contract

> Operator overnight goal 2026-05-17:
>
> > *"work with hermes (ares) and clawta to update all our docs,
> > create a roadmap for the active repos in the workspace tonight
> > and run through a plan that will take the entire night. you do
> > not have the goal reached until you have agreed on a plan
> > between the three of you, specd it, and executed it all the
> > way through."*
>
> This spec is the contract piece. The execution is per-repo PRs
> tonight + the workspace-level meta-roadmap at
> `chitinhq/workspace/roadmap.md`.

## Ticket refs

- Workspace chitin task #71 (overnight-goal anchor).
- Spec 020 (SDD+TDD enforcement, §1.2 test coverage) — this spec
  extends the "every spec carries Test coverage" rule with "every
  active repo carries this doc bundle."

## File-system scope

- `.specify/specs/024-active-repo-doc-bundle/**`
- `.specify/constitution.md` (amend §1.3 — see below)
- `swarm/bin/check-active-repo-docs.sh` (new — verification script)
- `swarm/tests/test_active_repo_doc_bundle.py` (new — regression tests)

Worker MUST NOT touch any other path. The per-repo docs land via
separate PRs to each active repo's own tree.

## Goal

Every truly-active repo in the operator's workspace carries the same
4-piece doc bundle, so any operator or worker can drop into a repo
tomorrow and orient in <60 seconds.

## What counts as "active"

A repo is **active** if all of the following:
1. Has commits to its default branch in the last 30 days, **OR** an
   open ticket on its associated kanban board, **OR** explicit
   operator-stated intent to revive (logged in workspace `roadmap.md`)
2. Is **not GitHub-archived** (this is the load-bearing check; spec
   020 §1.2's "amendment debt" pattern doesn't apply to archived
   repos — they can't even take a PR)

As of 2026-05-17 the active set is **4 repos**:
- `chitinhq/chitin`
- `wjcmurphy/bench-devs-platform`
- `chitinhq/workspace`
- `jpleva91/hermes-agent`

The list lives in `chitinhq/workspace/roadmap.md` (single source of
truth). When the operator marks a repo active or archives one, the
workspace roadmap is the place that changes; this spec doesn't
hardcode the list.

## The bundle (4 pieces per active repo)

1. **README.md** — what this repo is + how to run it. Tier varies
   (ship-ready for chitin/bench-devs/hermes-agent; directionally-
   correct for workspace).
2. **AGENTS.md** OR **CLAUDE.md** — operator + AI handoff context.
   (Either name is acceptable; AGENTS.md is the agent-runtime-
   neutral form, CLAUDE.md is the established name in bench-devs.
   Bench-devs keeps CLAUDE.md; chitin + hermes-agent + workspace
   use AGENTS.md. No rename for the existing files.)
3. **docs/roadmap.md** — single-page strategic roadmap. Sections:
   *Mission*, *Status as of NNNN-NN-NN*, *Next 4-week milestones*,
   *Dependencies + blockers*, *Out of scope*, *How to read this if
   you're a worker*.
4. **`.specify/specs/INDEX.md`** — index of spec-kit entries in this
   repo. Each entry: spec slug, bound ticket id, status
   (draft/ratified/shipped/archived), link.

## Acceptance Criteria

- **AC1**: All 4 active repos have all 4 bundle pieces present as
  of the merge of this spec's implementation PRs.
- **AC2**: `swarm/bin/check-active-repo-docs.sh` runs in CI; exits
  non-zero if any active repo (per the workspace roadmap) is
  missing a bundle piece.
- **AC3**: A new repo declared active in workspace `roadmap.md`
  fails the check until it has the bundle. The fix is "add the
  bundle," not "remove the repo from active."
- **AC4**: An archived repo can't accept the bundle (PR creation
  returns "Repository was archived so is read-only" from GitHub).
  The check therefore MUST cross-reference the workspace roadmap
  against the archive state via `gh repo view --json isArchived`
  and surface mismatches.
- **AC5**: Constitution §1.3 amendment lands documenting the
  bundle contract.

## Test coverage

### Why static-analysis + integration (not browser-e2e)

The end-to-end surface is **the existence + shape of files in
checked-out active repos + the check script's pass/fail behavior**.
There is no browser/HTTP boundary. Static analysis against the
workspace `roadmap.md` + integration via the check script against
a fixture workspace are the authentic tests.

| Spec AC | Test case (in `swarm/tests/test_active_repo_doc_bundle.py`) | What breaks if removed |
|---------|-------------------------------------------------------------|------------------------|
| AC1 | `test_all_active_repos_have_bundle_pieces` | A new active repo lands without docs |
| AC2 | `test_check_script_exits_nonzero_when_piece_missing` | The check script silently passes when a repo is broken |
| AC3 | `test_check_script_lists_new_active_repo_missing_bundle` | Operator can't tell which repo is non-compliant |
| AC4 | `test_check_script_flags_archived_listed_as_active` | Archive/active mismatch goes unnoticed |
| (constitution shape) | `test_constitution_has_section_1_3` | §1.3 amendment removed silently |

## Constitution amendment (§1.3 — added by the implementation PR)

> **§1.3 Active-repo doc-bundle contract.** Every repo declared
> active in `chitinhq/workspace/roadmap.md` MUST carry these four
> pieces: (1) `README.md`, (2) `AGENTS.md` OR `CLAUDE.md`, (3)
> `docs/roadmap.md`, (4) `.specify/specs/INDEX.md`. The check
> script `swarm/bin/check-active-repo-docs.sh` enforces shape; the
> operator + reviewers enforce content. Archived repos are not
> active and don't take the bundle. A new repo can be declared
> active only after it has the bundle (or in the same PR).

## Out of scope

- Cross-repo navigation UI (chitin-console may grow this; not in
  this spec)
- Auto-generation of the bundle from a template (per-repo content
  is human-written; templates risk vacuous docs)
- Multi-language docs (English-only for now)
- Migrating archived repos back to active (operator decision per
  repo; not a doc-contract concern)

## Why this spec exists

Tonight, the operator's overnight goal was "update all our docs,
create a roadmap for the active repos." Without a written contract
on what "the docs" means + which repos count as "active," the
result would be N opinions, M file paths, and a half-finished
sweep. This spec pins both — and ships the check script so the
shape stays enforced.
