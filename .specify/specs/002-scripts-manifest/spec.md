# Scripts Classification: MANIFEST + Runtime-Critical Audit

> Spec-kit entry for ticket `t_75c8c8c1`
> Source spec: `docs/superpowers/specs/2026-05-13-scripts-classification.md` (merged via #580)

## Goal

Every file under `scripts/` (recursively) is tagged with one of four categories
(`ci`, `migration`, `operator`, `runtime-critical`) and runtime-critical scripts
have a test or a Go-port plan.

## Acceptance criteria

- [ ] `scripts/MANIFEST.yaml` exists; covers every file under `scripts/`
      recursively; each entry has `path`, `category`, `purpose`
- [ ] `runtime-critical` entries have either `tested_by: <path>` or
      `port_ticket: <id>` (linter rejects missing both)
- [ ] `migration` entries have `added_on` and `expires_on` (90-day TTL);
      linter rejects expired migrations
- [ ] `scripts/check-scripts-manifest.sh` exists; CI-wired via
      `check-scripts-manifest` make target or CI step; fails on:
      (a) untracked script, (b) runtime-critical with neither `tested_by`
      nor `port_ticket`, (c) expired migration
- [ ] Generated/vendor scripts (e.g. `scripts/node_modules/`,
      `scripts/*.generated.*`) are excluded from MANIFEST coverage via a
      top-level `exclude_patterns` list in `MANIFEST.yaml`

## Boundaries

- **Empty MANIFEST.yaml**: linter must pass with zero scripts (baseline)
- **Max script set**: linter must pass when every non-excluded file under
  `scripts/` is represented exactly once in `MANIFEST.yaml`
- **Manifest error**: malformed YAML, unknown categories, and missing
  runtime-critical coverage fields fail closed with a non-zero exit
- **Missing category**: every entry must have exactly one category from the
  four-value enum; linter rejects unknown categories
- **Untracked script**: any `.sh`/`.py` under `scripts/` not in MANIFEST.yaml
  fails CI
- **Expired migration**: `expires_on < today` fails CI regardless of
  runtime-critical status
- **Dual-tagged entries**: a script cannot be both `runtime-critical` and
  `migration`; linter rejects

## Implementation test obligations

- Boundary: empty - empty `MANIFEST.yaml` with zero scripts passes
- Boundary: max - full recursive `scripts/` inventory passes when every
  non-excluded script appears once
- Boundary: error - malformed YAML or invalid manifest entries fail closed

## Scope

- `scripts/` directory only (not `swarm/`, not `cmd/`)
- New MANIFEST.yaml + linter script + CI wiring
- No changes to existing script behavior

## Out of scope

- Go-porting of runtime-critical scripts (separate tickets per `port_ticket`)
- `swarm/` classification (separate governance effort)
