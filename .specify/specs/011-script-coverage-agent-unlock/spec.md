# Script Coverage: chitin-agent-unlock.sh

> Spec-kit entry for ticket `t_6bcf5081`
> Parent spec: `002-scripts-manifest`

## Goal

Runtime-critical script audit followup. `scripts/chitin-agent-unlock.sh` is invoked by a systemd timer and currently lacks dedicated regression coverage. Deliver either (a) a focused regression/smoke test or (b) a concrete Go-port plan with acceptance criteria.

## Acceptance criteria

- [ ] Either a test file exists under `swarm/tests/` or a Go-port stub exists under `go/execution-kernel/` for `chitin-agent-unlock.sh`
- [ ] If test: script runs successfully and test covers lock-failure and normal-unlock paths
- [ ] If Go-port plan: stub compiles, plan document lists migration steps with AC
- [ ] Existing script functionality is not changed by this ticket

## Boundaries

- **Invariant**: scripts/chitin-agent-unlock.sh continues to work unchanged if coverage/Go-port is not yet merged
- **empty**: no coverage exists yet
- **script-returns-nonzero-on-lock-failure**: negative test case
- **Go-port-stub-compiles-but-no-behavior-change**: port must not regress existing behavior
