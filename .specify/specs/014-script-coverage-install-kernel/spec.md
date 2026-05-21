# Script Coverage: install-kernel.sh

> Spec-kit entry for ticket `t_a96ed4dd`
> Parent spec: `002-scripts-manifest`

## Goal

Runtime-critical script audit followup. `scripts/install-kernel.sh` is invoked by `chitin-kernel-redeploy.service` and currently lacks dedicated regression coverage. Deliver either (a) a focused regression/smoke test or (b) a concrete Go-port plan with acceptance criteria.

## Acceptance criteria

- [ ] Either a test file exists under `swarm/tests/` or a Go-port stub exists under `go/execution-kernel/` for `install-kernel.sh`
- [ ] If test: covers install-failure (exit non-zero) and normal-install paths
- [ ] If Go-port plan: stub compiles, plan document lists migration steps with AC
- [ ] Existing script functionality is not changed by this ticket

## Boundaries

- **Invariant**: scripts/install-kernel.sh continues to work unchanged if coverage/Go-port is not yet merged
- **no-coverage-existent**: baseline
- **script-exits-nonzero-on-install-failure**: negative test case
- **Go-port-stub-compiles-but-no-behavior-change**: port must not regress existing behavior
