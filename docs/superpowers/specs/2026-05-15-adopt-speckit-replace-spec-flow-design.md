---
status: draft
owner: jared+claude
kanban: null
implementation_pr: null
superseded_by: null
effective_from: 2026-05-15
effective_to: null
---

# Adopt GitHub spec-kit; retire `docs/superpowers/specs/` as canonical home

> **Recursion note.** This is the last spec authored under the existing
> `docs/superpowers/specs/` flow. The change it describes retires that
> flow in favor of `.specify/specs/NNN-<slug>/`. The spec itself migrates
> via PR2 along with the 14 other living specs.

## 1. Goal and scope

### Goal
Make `.specify/` the canonical home for chitin specs. Retire the bespoke
YAML-front-matter + linter + auto-INDEX flow. Preserve the chitin-specific
load-bearing pieces (kanban linkage, status semantics, invariants) by
carrying them forward as spec-kit extensions.

### In scope
1. `specify init . --integration claude --integration codex --integration gemini --integration copilot` against this repo.
2. Author `.specify/memory/constitution.md` with curated chitin principles + pointers.
3. Migrate **14 living specs** (13 `open` + 1 `draft`) into `.specify/specs/NNN-<slug>/`.
4. Freeze the **2 amended specs** + `INDEX.md` in `docs/superpowers/specs/` as historical record.
5. Replace `scripts/check-spec-frontmatter.py` with `scripts/check-speckit-frontmatter.py`.
6. Retire `scripts/regen-spec-index.py` (spec-kit's directory listing IS the index).
7. Add a chitin-specific kanban-field requirement as a spec-kit extension under `.specify/extensions/chitin-frontmatter/`.
8. Update `CLAUDE.md`, `AGENTS.md`, `README.md`, `docs/runbooks/spec-lifecycle.md`.
9. Add `specify check` to CI alongside the new linter.
10. Retire chitin's `/brainstorming`, `/writing-plans`, `/executing-plans` skills with a 30-day shim.

### Out of scope
- Migrating `docs/superpowers/plans/` — plans now live next to specs as
  `.specify/specs/NNN-<slug>/plan.md`; old plans freeze in place.
- Re-running brainstorming on existing specs — carry forward as-is.
- Changing the hermes ↔ chitin kanban contract.
- Switching the agent toolchain — Claude Code, Codex, Gemini, Copilot CLI all stay.

## 2. Architecture

### Target tree

```
.specify/
├── memory/
│   └── constitution.md            ← curated: 4 invariants + side-effect rule
│                                    + audit-driven-cull pattern + "see also"
│                                    pointers
├── templates/
│   └── overrides/
│       ├── spec-template.md       ← chitin-shaped: kanban, status, owner
│       ├── plan-template.md       ← chitin's slice convention
│       └── tasks-template.md      ← kanban_flow chokepoint reminder
├── presets/                       ← unused at v1; reserved for stack packs
├── extensions/
│   └── chitin-frontmatter/        ← enforces kanban + owner + status fields
├── scripts/bash/                  ← spec-kit native; do not modify
└── specs/
    ├── 001-operator-approval-escalation/
    │   ├── spec.md
    │   ├── plan.md
    │   └── tasks.md
    ├── 002-hermes-clawta-lobster-finish/
    │   └── …
    └── … (14 dirs total at cutover)

docs/superpowers/
├── specs/                         ← FROZEN, read-only after PR2
│   ├── 2026-05-13-spec-lifecycle-metadata.md      (amended, historical)
│   ├── 2026-05-12-clawta-hermes-architecture.md   (amended, historical)
│   ├── *.md                       ← every migrated spec becomes a 1-line
│   │                                redirect to its .specify/specs/NNN-…/
│   │                                home (including this design spec)
│   ├── INDEX.md                   ← final "frozen at 2026-05-15" header
│   └── README.md                  ← new: "historical; new specs in .specify/"
└── plans/                         ← FROZEN, read-only after PR2
```

### Key contracts

1. **`.specify/specs/NNN-<slug>/spec.md`** carries chitin-shaped YAML
   front-matter (status, owner, kanban, implementation_pr, superseded_by,
   effective_from, effective_to). Same 7 fields as today, new location.
2. **`.specify/memory/constitution.md`** is loaded by every spec-kit slash
   command. Hard 300-line cap enforced by CI.
3. **`scripts/check-speckit-frontmatter.py`** enforces chitin extension
   fields in CI; replaces the old front-matter linter.
4. **No double-write window.** At PR2 merge, `docs/superpowers/specs/`
   becomes read-only via CI gate. One-shot transition.

## 3. Constitution body

Final body of `.specify/memory/constitution.md`:

```markdown
# Chitin Constitution

The non-negotiable principles every spec, plan, and implementation respects.
Curated 2026-05-15. Amendments via /speckit.constitution + PR review.

## Article I — Kernel Authority
All execution passes through `gov.Gate.Evaluate` (go/execution-kernel/internal/gov).
The kernel is the only enforcement point. No driver, orchestrator, or adapter may bypass it.
Source: docs/architecture/layer-contracts.md §1.

## Article II — Driver Constraint
ExecutionRequest carries `allowed_drivers` as a typed, schema-validated, closed-enum field.
Orchestrators pick within it; they cannot expand it. Schema-typed contract is the
upstream guarantee; gov.Gate.Evaluate at every leaf hook is the downstream guarantee.
Source: docs/architecture/layer-contracts.md §2.

## Article III — Routing Scope
Routing optimizes for capacity (latency, availability, hardware) within the
allowed-drivers set. Routing cannot expand it.
Source: docs/architecture/layer-contracts.md §3.

## Article IV — Aggregation Role
The event chain (~/.chitin/events-*.jsonl) is canonical. OTEL emit is a non-authoritative
projection. Aggregation never affects live execution. Kernel-write-survives-OTEL-failure
is invariant.
Source: docs/architecture/layer-contracts.md §4.

## Article V — Single Side-Effect Authority
Only the Go execution kernel may perform side effects. TypeScript libraries and
adapters are read-only against the filesystem except via the kernel binary.
Multiple writers = non-deterministic replay + contract drift.
Source: docs/architecture.md "Hard rule".

## Article VI — Audit-Driven Cull
When a new external substrate exposes a primitive chitin re-implements, cull the
chitin parallel implementation. Pattern proven 2026-05-06 → 2026-05-08:
~5000+ LOC removed; surface tightened to the moat. Reinvest in asymmetric strengths
(canonical vocabulary, chain depth, driver coverage); divest from substrate duplication.
Source: docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md and the
2026-05-13 substrate-composition decision.

## Article VII — Spec, Plan, PR Discipline
- Specs land in `.specify/specs/NNN-<slug>/spec.md` with chitin front-matter
  (status, owner, kanban, implementation_pr, superseded_by, effective_from, effective_to).
- Plans expand into `.specify/specs/NNN-<slug>/plan.md`.
- Tasks track implementation in `.specify/specs/NNN-<slug>/tasks.md`.
- Status enum: draft → open → implemented; or open → amended; or any → superseded.
- `implemented` requires `implementation_pr`. `superseded` requires `superseded_by`.

## See also (fetch on demand)
- docs/thesis.md — what chitin is in one paragraph
- docs/architecture.md — kernel internals + side-effect rule
- docs/architecture/layer-contracts.md — full text of the four invariants
- docs/decisions/ — durable boundary docs (8 entries; read the relevant one before
  making a contradicting design choice)
- docs/operating-model.md — how subsystems are owned today
- docs/event-model.md — canonical envelope + chain shape (the load-bearing schema)
- docs/runbooks/ — operator runbooks
```

A constitution-drift CI test (§7) asserts:
- Articles I–IV match the four invariants in `docs/architecture/layer-contracts.md`
  verbatim (modulo Article-numbering header).
- Article V matches the "Hard rule" body in `docs/architecture.md` verbatim.

If either upstream source changes without the constitution updating, CI fails.
Articles VI and VII are chitin-cultural and not drift-checked against any
single source — they are amended via `/speckit.constitution` + PR review.

## 4. Migration mechanics

### Numbering map (oldest → newest, by file date)

| New ID | Old path | Status |
|---|---|---|
| 001 | `2026-05-07-operator-approval-escalation-design.md` | open |
| 002 | `2026-05-11-hermes-clawta-lobster-finish-design.md` | open |
| 003 | `2026-05-12-argus-observatory.md` | open |
| 004 | `2026-05-12-chitin-dashboard.md` | open |
| 005 | `2026-05-12-no-commit-to-protected-branch.md` | open |
| 006 | `2026-05-12-no-gov-self-mod-bypass.md` | open |
| 007 | `2026-05-12-sticky-state-recovery-rotator.md` | open |
| 008 | `2026-05-12-swarm-observability-via-chitin-cli.md` | open |
| 009 | `2026-05-13-argus-max.md` | draft |
| 010 | `2026-05-13-go-only-governance-authority.md` | open |
| 011 | `2026-05-13-isolate-swarm-kanban-mutations.md` | open |
| 012 | `2026-05-13-regression-gate.md` | open |
| 013 | `2026-05-13-scripts-classification.md` | open |
| 014 | `2026-05-13-worktree-status-report.md` | open |

The **2 amended specs** (`2026-05-13-spec-lifecycle-metadata.md` and
`2026-05-12-clawta-hermes-architecture.md`) stay in
`docs/superpowers/specs/` as historical record.

This design spec itself (`2026-05-15-adopt-speckit-replace-spec-flow-design.md`)
is authored in the old location for narrative honesty, then migrates with PR2
as ID 015.

### Per-spec migration (scripted, one-shot)

For each old spec at `docs/superpowers/specs/<file>.md`:

1. Strip YAML front-matter; capture into a struct.
2. Create dir `.specify/specs/NNN-<slug>/`.
3. Write `spec.md`:
   - Spec-kit's required structure on top (title, summary, acceptance criteria).
   - Original body preserved under `## Original spec body` heading.
   - Chitin front-matter at top, unchanged.
4. If a matching plan exists in `docs/superpowers/plans/`, copy into `plan.md`;
   else create stub.
5. Create `tasks.md` with the chitin task-template header (kanban-flow
   chokepoint reminder).
6. Add a `MIGRATED-FROM` line at the bottom of `spec.md` citing old path + commit SHA.
7. Replace the old file at `docs/superpowers/specs/<file>.md` with a single
   redirect line:
   `> Moved to .specify/specs/NNN-<slug>/spec.md on 2026-05-15.`

### One-shot migration script

`scripts/migrate-specs-to-speckit.py` — idempotent, `--dry-run` flag, retires
after PR2 merges.

### Cutover order (three PRs)

- **PR1** — `specify init .` + write constitution + add `chitin-frontmatter`
  extension + add `.specify/templates/overrides/*.md` + CI step
  `specify check`. **Does not migrate anything.** Reversible in isolation.
- **PR2** — run migration script; commit `.specify/specs/001…015`; add
  redirect notes in `docs/superpowers/specs/`; retire old linter; retire
  `regen-spec-index.py`; add shims for retired skills. **One-way door.**
- **PR3** — update CLAUDE.md, AGENTS.md, README, `docs/runbooks/spec-lifecycle.md`,
  `docs/superpowers/specs/README.md` (new historical-record note). Reversible.

## 5. Skills, linter, CI

### Chitin superpowers skills disposition

| Old skill | Disposition | Replacement |
|---|---|---|
| `/brainstorming` | Retire (30-day shim) | `/speckit.specify` + `/speckit.clarify` |
| `/writing-plans` | Retire (30-day shim) | `/speckit.plan` |
| `/executing-plans` | Retire (30-day shim) | `/speckit.implement` |
| `/subagent-driven-development` | Keep | Orthogonal to spec authoring |
| `/tdd`, `/test-driven-development` | Keep | Orthogonal |
| `/verification-before-completion` | Keep | Orthogonal |

Shims print a one-line redirect and exit. Retired 2026-06-15
(30 days after PR2). CLAUDE.md gets a "shims retired" entry on retirement.

### Linter retirement and replacement

- `scripts/check-spec-frontmatter.py` — retires in PR2.
- `scripts/regen-spec-index.py` — retires in PR2 (directory listing IS the index).
- **New: `scripts/check-speckit-frontmatter.py`** — runs in CI, enforces:
  - Every `.specify/specs/**/spec.md` has the 7 chitin front-matter fields.
  - `status` ∈ {draft, open, implemented, amended, superseded}.
  - `status: implemented` → `implementation_pr` non-null.
  - `status: superseded` → `superseded_by` points at an existing
    `.specify/specs/**/spec.md`.
  - `kanban` is either `null` or matches `^t_[a-f0-9]{8}$` (hermes shape).
- **New: `specify check`** — runs in CI as a separate step. Catches
  spec-kit-native issues (missing `constitution.md`, malformed templates).

### CI additions to `.github/workflows/ci.yml`

```yaml
- name: Spec-kit frontmatter
  run: python3 scripts/check-speckit-frontmatter.py

- name: Install spec-kit CLI
  uses: astral-sh/setup-uv@v3
  with:
    enable-cache: true
- name: Spec-kit native check
  run: |
    uv tool install specify-cli --from git+https://github.com/github/spec-kit.git@v<PIN>
    specify check

- name: Frozen historical spec dir is read-only
  run: |
    base="origin/${{ github.event.pull_request.base.ref }}"
    if git diff --name-only "$base"...HEAD | grep -E '^docs/superpowers/specs/[^/]+\.md$' \
       | grep -v 'README.md$' | grep -v 'INDEX.md$'; then
      echo "::error::docs/superpowers/specs/ is frozen historical; new specs go in .specify/"
      exit 1
    fi
```

The last step is load-bearing — it enforces the no-double-write property
at the CI gate. Spec-kit version is pinned; bumps are deliberate.

### Hermes / kanban-flow compatibility

- `scripts/kanban-flow` unchanged. Front-matter `kanban: t_XXX` remains
  the bidirectional link.
- Hermes ticket comments referencing old spec paths continue to resolve
  because old files keep a redirect line.

## 6. Error handling

### Migration script

- Idempotent by existence check: if `.specify/specs/NNN-<slug>/` exists, skip.
- `--dry-run` mode prints the plan; default mode applies.
- **Hard-fails on:**
  - Old spec missing required front-matter (would lose `status`/`owner`/`kanban`).
  - Slug collision (two specs mapping to same `NNN-<slug>/`).
  - Old plan path exists but the referenced spec doesn't.
- **Soft-warns on:**
  - Spec body contains no acceptance-criteria-shaped section (preserved as-is
    under `## Original spec body` — agent refactors later via `/speckit.clarify`).
  - Spec references retired paths (`/brainstorming`, etc.) — flagged for
    follow-up, not blocking.

### Runtime

- `scripts/check-speckit-frontmatter.py` exits non-zero with structured
  remediation hints (which field, which file, expected shape). Same UX as
  the retired linter so muscle memory survives.
- `specify check` failures surfaced verbatim; if `uv` install fails, CI step
  fails fast (not silently green).
- **Constitution drift detection:** sentinel test asserts Articles I–IV of
  `constitution.md` match `docs/architecture/layer-contracts.md` verbatim,
  and Article V matches the "Hard rule" body of `docs/architecture.md`.
  Articles VI–VII are chitin-cultural, not drift-checked.

## 7. Testing

1. **Migration script unit tests** — fixture old-spec → expected new-spec
   layout; covers all 5 lifecycle states.
2. **Migration script end-to-end** — `--dry-run` in a CI pre-flight job
   before PR2 merges; output reviewed.
3. **Linter coverage** — golden-file tests on
   `scripts/check-speckit-frontmatter.py` with 6 cases: valid, missing-field,
   bad-status, implemented-without-PR, superseded-without-target,
   malformed-kanban.
4. **Constitution-drift test** — described above.
5. **Frozen-directory enforcement test** — synthetic PR adds a file to
   `docs/superpowers/specs/` (not README); CI rejects it.
6. **Round-trip smoke** — after migration, `specify check` against the
   migrated tree exits 0.

## 8. Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Migration loses front-matter context | Low | High | Front-matter preserved verbatim; golden-file tests |
| Spec-kit templates conflict with chitin shape | Med | Med | Use `.specify/templates/overrides/` from day 1; never edit core templates |
| `specify check` flags chitin extension fields as errors | Med | Low | Test in PR1 before merging; extensions designed for this |
| Constitution bloats with amendments | Med | Med | Hard 300-line cap enforced by CI |
| `uv tool install` adds CI flakiness | Med | Low | Cache `~/.local/share/uv/tools/`; pin spec-kit version |
| Contributors paste a new spec into old dir by habit | High | Low | Frozen-directory CI gate (§5) blocks at PR time |
| Hermes ticket comments reference old spec paths | High | Low | Old paths remain as redirects |
| Spec-kit upstream changes break our extensions | Med | Med | Pin spec-kit version; bump deliberately |
| Two specs migrate to same slug | Low | High | Migration script hard-fails on collision |
| First spec authored under new flow exposes template gap | High | Low | Acceptable — fix in flight |

### Reversibility

- **PR1** (init): trivially reversible — revert + `rm -rf .specify/`.
- **PR2** (migrate): one-way door — old paths get redirect notes; full
  rollback requires re-creating `docs/superpowers/specs/` from git history.
  Treat PR2 merge as the commitment point.
- **PR3** (docs): trivially reversible.

## 9. Acceptance criteria

The spec is **implemented** when all of the following hold:

1. `.specify/` exists with `memory/constitution.md` (≤ 300 lines),
   `templates/overrides/*.md`, `extensions/chitin-frontmatter/`,
   and `specs/001–015`.
2. The 14 living specs + this design spec are at `.specify/specs/NNN-<slug>/spec.md`
   with chitin front-matter intact.
3. The 2 amended specs and `INDEX.md` (with a final "frozen at 2026-05-15"
   header) remain in `docs/superpowers/specs/`.
4. `scripts/check-speckit-frontmatter.py` runs in CI and rejects malformed front-matter.
5. `specify check` runs in CI and exits 0 on `main`.
6. The frozen-directory CI gate rejects new `.md` files under
   `docs/superpowers/specs/` other than `README.md` / `INDEX.md`.
7. `/brainstorming`, `/writing-plans`, `/executing-plans` skills are shims
   that print a redirect and exit; CLAUDE.md notes the shim window.
8. CLAUDE.md, AGENTS.md, README, `docs/runbooks/spec-lifecycle.md` are
   updated to reference the new flow.
9. Constitution-drift test exists and is green.
10. `scripts/check-spec-frontmatter.py` and `scripts/regen-spec-index.py`
    are removed.

## 10. Follow-ups (not blocking implementation)

- After 30 days, delete the skill shims. Tracked as a follow-up task once
  PR2 merges.
- Evaluate whether to use spec-kit `presets/` for "stack packs" (e.g., a
  governance-pack preset for security-shaped specs). Defer to a separate
  spec when the demand is real.
- Consider whether `/speckit.analyze` (cross-artifact consistency check)
  should run in CI on PRs that touch `.specify/specs/`. Defer.

## 11. Open questions

- Spec-kit version pin: latest tagged release as of cutover, then deliberate
  bumps via a separate "bump-speckit" PR with full migration regression run.
  No automatic bumping.
- Whether `presets/` ever gets used: parked until a concrete need arises.
