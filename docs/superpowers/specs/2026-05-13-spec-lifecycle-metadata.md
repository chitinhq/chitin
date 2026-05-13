---
status: open
owner: claude-code
kanban: t_5f50f6a8
implementation_pr: null
superseded_by: null
effective_from: '2026-05-13'
effective_to: null
---

# Spec: spec lifecycle metadata + index

Date: 2026-05-13
Status: spec — open
Kanban: `t_5f50f6a8` (priority 25)
Source: `docs/audits/2026-05-13-architecture-audit.md` — Top finding 4
Author: claude-code (operator-controlled, spec writer)

## Problem

Architecture audit 2026-05-13: `docs/` has 159 files and 54 commits
in 7d. Most of that churn is `docs/superpowers/specs/` (34 specs as
of writing) and `docs/superpowers/observations/`. The current spec
format is a plain markdown header — a date, a free-text Status
line, sometimes a Kanban id, often a free-text Author line.

Observed status values across the existing 34 specs (sampled
2026-05-13):

- `spec — open`
- `spec — under discussion`
- `spec — amended 2026-05-12 to reflect actual built architecture`
- (some specs in `docs/superpowers/superseded/` with no marker
  in the body)
- (some specs in `docs/superpowers/specs/` filename-suffixed
  `-superseded.md`)

There is no machine-readable lifecycle. An agent asked "find the
current architecture for cost-governance-kernel" must read both
`2026-04-28-cost-governance-kernel-design-superseded.md` and
`2026-04-29-cost-governance-kernel-design.md`, parse their bodies,
and infer that the `-superseded` suffix means "do not act on this."
That is a high failure-mode risk: a stale spec that looks current
is a confident-but-wrong instruction.

## Invariant (the claim)

> Every file under `docs/superpowers/specs/` and
> `docs/superpowers/superseded/` has a YAML front-matter block
> with: `status`, `owner`, `kanban`, `implementation_pr`,
> `superseded_by`, `effective_from`, `effective_to`. The
> `status` field comes from a closed enum. The
> `docs/superpowers/specs/INDEX.md` file is regenerated from the
> front-matter and is the operator's entry point.

A spec is **current** iff `status == implemented OR status == open`
AND `effective_to` is null AND no other spec has it in its
`superseded_by` field. Anything else is historical reference.

## Decision

Adopt YAML front-matter. Build an index generator. Run the index
generator from CI; commit a regenerated INDEX.md to PRs that touch
specs. The format is intentionally tiny (six fields) so retrofit
across the existing 34 specs is a half-day, not a multi-day port.

## In scope

1. **YAML front-matter schema** (defined below).
2. **`scripts/check-spec-frontmatter.sh`** — validates schema +
   enum on every spec; CI-wired.
3. **`scripts/regen-spec-index.sh`** — reads front-matter across
   `docs/superpowers/specs/**/*.md` + `docs/superpowers/superseded/**/*.md`,
   writes `docs/superpowers/specs/INDEX.md` grouped by status.
4. **Retrofit pass** — add front-matter to all 34 existing specs.
   Inferred from the existing header lines where possible; flagged
   for operator review where ambiguous.
5. **Operator-facing entry point** — `docs/superpowers/specs/INDEX.md`
   linked from `docs/superpowers/README.md` (and from
   `docs/runbooks/spec-lifecycle.md`, new).
6. **`docs/runbooks/spec-lifecycle.md`** — explains the enum, when
   to mark a spec implemented, when to move it to superseded/, and
   how to fill the fields.

## Out of scope (followups)

- Auto-detecting `implementation_pr` from git log — operator fills
  it on merge. Cheap to inspect; expensive to infer reliably.
- Auto-promoting a spec from `open` to `implemented` — needs
  human judgment. The linter only enforces presence + enum
  validity, not state transitions.
- Generating an HTML spec viewer / wiki page — INDEX.md is
  sufficient and renders directly on GitHub.
- Renaming the `docs/superpowers/superseded/` directory —
  superseded specs stay where they are; the `status` field plus
  the directory provide the same signal.

## Approach detail

### Front-matter schema

```yaml
---
status: open                # enum (see below); REQUIRED
owner: claude-code          # who maintains this spec; REQUIRED
kanban: t_5f50f6a8          # source ticket id, or null; REQUIRED
implementation_pr: null     # int (PR number) or null; REQUIRED
superseded_by: null         # path to the spec that replaced this; REQUIRED
effective_from: 2026-05-13  # date the spec was first marked open
effective_to: null          # date the spec was implemented or superseded
---

# Spec: <title>
...
```

Status enum:

| value         | meaning                                              |
|---------------|------------------------------------------------------|
| `draft`       | being written; not yet ready for implementation work |
| `open`        | accepted; implementation eligible                    |
| `implemented` | shipped; `implementation_pr` MUST be set             |
| `amended`     | live, but the body has post-spec amendments above it |
| `superseded`  | replaced; `superseded_by` MUST point to successor    |

### Linter contract

`scripts/check-spec-frontmatter.sh`:

- Every file under `docs/superpowers/specs/**/*.md` and
  `docs/superpowers/superseded/**/*.md` must start with `---\n`.
- The YAML block must parse and contain all six fields.
- `status` must be one of the enum values.
- If `status == implemented`, `implementation_pr` must be non-null.
- If `status == superseded`, `superseded_by` must point to an
  existing spec path.
- Exit non-zero with file:line on any violation.

### Index generator

`scripts/regen-spec-index.sh` produces
`docs/superpowers/specs/INDEX.md`:

```markdown
# Spec index — auto-generated, do not edit by hand

_Last regenerated: <ts>_

## Open (N)

| Date       | Title                                   | Owner       | Kanban       |
|------------|-----------------------------------------|-------------|--------------|
| 2026-05-13 | Go is the only governance authority     | claude-code | t_742ee3ea   |
| ...        |                                         |             |              |

## Implemented (M)

| Date       | Title                | Owner | PR    | Implemented on |
|------------|----------------------|-------|-------|----------------|
| ...        |                      |       |       |                |

## Superseded (K)

| Date       | Title | Superseded by |
|------------|-------|---------------|
| ...        |       |               |

## Draft (J)

...

## Amended (L)

...
```

CI regenerates INDEX.md and fails if the committed file disagrees
with the regenerated one. That makes the index a build artifact
the operator does not hand-edit but does commit (so it's reviewable
in the PR diff).

### Retrofit pass

For each of the 34 existing specs:

1. Open the file, read the existing `Status:` / `Author:` /
   `Kanban:` header lines.
2. Insert a `---` front-matter block at the top with the fields
   populated from the header lines (best-effort).
3. Where a field is ambiguous (no header line, or contradictory
   status), set it to a sentinel like `status: open` with a
   comment `# TODO(operator): confirm` and surface those files in
   a single "ambiguous retrofit" comment on the PR.

The existing free-text Status lines stay in the body (they're
narrative, not contract); the front-matter is the machine
read-out.

## Verification

- **Bootstrap**: after retrofit, `check-spec-frontmatter.sh` exits 0.
- **`regen-spec-index.sh`** produces a deterministic INDEX.md (same
  output on repeat runs).
- **CI loop**: PR that adds a spec without front-matter fails CI
  with a pointer to this spec.
- **Status-enforcement**: PR that marks a spec
  `status: implemented` but leaves `implementation_pr: null` fails
  CI with an explanatory message.

## Done-condition

- All specs under `docs/superpowers/specs/**` and
  `docs/superpowers/superseded/**` have front-matter passing the
  linter.
- `docs/superpowers/specs/INDEX.md` exists and is regenerated by
  the script.
- `scripts/check-spec-frontmatter.sh` + `scripts/regen-spec-index.sh`
  are CI-wired.
- `docs/runbooks/spec-lifecycle.md` is committed and linked from
  `docs/superpowers/README.md`.

## Effort

S. Schema + linter + index generator: half a day. Retrofit of 34
existing specs: half a day. Total ~1 day.
