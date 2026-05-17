# Script Coverage: chitin-envelope-rotate.sh

> Spec-kit entry for ticket `t_5c4f6d38`
> Parent spec: `002-scripts-manifest`

## Goal

Runtime-critical script audit followup. `scripts/chitin-envelope-rotate.sh` is invoked by a systemd timer and currently lacks dedicated regression coverage. Deliver either (a) a focused regression/smoke test or (b) a concrete Go-port plan with acceptance criteria.

## Acceptance criteria

- [ ] Either a test file exists under `swarm/tests/` or a Go-port stub exists under `go/execution-kernel/` for `chitin-envelope-rotate.sh`
- [ ] If test: covers missing-envelope handling and normal-rotation paths
- [ ] If Go-port plan: stub compiles, plan document lists migration steps with AC
- [ ] Existing script functionality is not changed by this ticket

## Boundaries

- **Invariant**: scripts/chitin-envelope-rotate.sh continues to work unchanged if coverage/Go-port is not yet merged
- **no-coverage-existent**: baseline
- **script-handles-missing-envelope-gracefully**: graceful degradation test case
- **Go-port-stub-compiles-but-no-behavior-change**: port must not regress existing behavior
