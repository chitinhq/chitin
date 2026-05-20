# Script Coverage: chitin-agent-unlock.sh

> Spec-kit entry for ticket `t_6bcf5081`
> Parent spec: `002-scripts-manifest`

## Goal

Runtime-critical script audit followup. `scripts/chitin-agent-unlock.sh`
is invoked by a systemd timer and currently lacks dedicated regression
coverage.

## Acceptance criteria

- [ ] AC1: Either a test file exists or a Go-port stub exists
- [ ] AC2: If test: script runs successfully and test covers lock-failure
- [ ] AC3: If Go-port plan: stub compiles
- [ ] AC4: Existing script functionality is not changed

## Boundaries

- **Invariant**: scripts/chitin-agent-unlock.sh continues to work unchanged
- **empty**: no coverage exists yet
- **script-returns-nonzero-on-lock-failure**: negative test case