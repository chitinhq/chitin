# Superpowers

This directory holds the specs, plans, and observations that shape
chitin's design.

## Quick links

- [Spec index](specs/INDEX.md) — every spec grouped by lifecycle
  status (open / amended / implemented / draft / superseded), with
  owner, kanban id, and implementation PR. Auto-generated; do not
  edit by hand.
- [Spec lifecycle runbook](../runbooks/spec-lifecycle.md) — how to
  fill the front-matter, what each status means, when to transition.
- [`specs/`](specs/) — current specs (open / amended / implemented /
  draft).
- [`superseded/`](../superpowers/superseded/) — specs replaced by
  later decisions; kept for historical reference.
- [`plans/`](plans/) — implementation plans derived from specs.
- [`observations/`](observations/) — post-implementation notes,
  retros, and durable findings.

## Adding a spec

1. Create the file under `specs/` with the convention
   `YYYY-MM-DD-<slug>.md`.
2. Add the required YAML front-matter block at the top — see
   [`docs/runbooks/spec-lifecycle.md`](../runbooks/spec-lifecycle.md)
   for the schema.
3. Run `python3 scripts/regen-spec-index.py` and commit the updated
   `INDEX.md` in the same PR. CI will reject PRs whose committed
   INDEX disagrees with the regenerated one.
