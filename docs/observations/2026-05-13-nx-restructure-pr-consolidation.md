# Nx restructure PR consolidation

Date: 2026-05-13

Ticket: `t_4bca2526`

Related ticket: `t_917e2aaf`

## Question

Three Nx restructure PRs were left open with failing `test` jobs:

- #517, step 3 dep constraints
- #518, step 4 missing project metadata
- #539, duplicate missing project metadata from `t_917e2aaf`

Swarm-audit flagged these as duplicate or stacked against the newer #535.
This note records the root cause and consolidation decision so the stale PRs
can be closed instead of rebased independently.

## CI root causes

The failures were all reproducible from the GitHub Actions `test` job logs.
They are related, but not identical:

| PR | Head | Failing task | Root cause |
| --- | --- | --- | --- |
| #517 | `49b23fd` | `@chitin/tooling-lint:lint:layer-tag-coverage` | The PR removed the `layer:app` depConstraint while `apps/openclaw-plugin-governance/package.json` still carried `layer:app`; the linter reported `layer:app` as missing. |
| #518 | `dde91b1` | `@chitin/router-plugin-api:test` | The PR added router-plugin-api metadata that made Nx run a `test` target, but that package did not have a valid test script/suite for CI. |
| #539 | `f4a05ae` | `analysis:test` | The duplicate metadata PR added a Python analysis `test` target that shells to `uv run --extra dev pytest -q`; CI does not install `uv`, so `/bin/sh: 1: uv: not found`. |

## Canonical resolution

#535 is the canonical fix. It landed the needed structural linter correction
without introducing the broken router-plugin-api or Python analysis test
targets:

- `eslint.config.mjs` uses `layer:analysis` instead of the obsolete
  `layer:app` depConstraint.
- `python/analysis/project.json` gives the analytics package a valid Nx
  project identity and `layer:analysis` tag.
- `tools/lint/layer-tag-coverage.ts` scans `python/` project metadata so
  `layer:analysis` is treated as used.

Do not merge #517, #518, or #539 after #535. They are stale relative to main
and either duplicate #535 or add CI-breaking targets. #539 should resolve
`t_917e2aaf` as superseded by #535 rather than done-via-merge.

## Local verification notes

This worktree could not run the full local `test` job end-to-end:

- `go` is not installed on PATH in this worker environment, so `go vet ./...`
  and `go test ./...` could not run locally.
- Broad Nx/Vitest commands exhausted the session's available thread quota after
  parallel Vitest processes had started; direct `tsx` retries failed with the
  same worker-thread resource limit.

The GitHub Actions logs did confirm that Go vet/test and the Go kernel build
had passed before the failing Nx/layer-tag steps on the affected PRs.
