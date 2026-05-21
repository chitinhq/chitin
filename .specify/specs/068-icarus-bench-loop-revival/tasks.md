# 068 — Tasks

> Canonical task list for spec 068. One kanban ticket per task (spec 067
> derivation). Ordered by dependency.

- [x] T001 Restore PR #794's icarus-bench machinery from git
  (`git checkout 389b486^ -- swarm/bin/icarus-bench-runner
  swarm/bin/icarus-bench-ticket-emitter swarm/bin/install-icarus-bench-cron.sh
  swarm/icarus_harness/ swarm/tests/test_icarus_harness.py`). Satisfies AC1.

- [ ] T002 Smoke-test a single run: `icarus-bench-runner --n-tasks 1
  --skip-emitter` produces `jobs/icarus/<job>/` with a `results.json`.
  Depends: T001. Satisfies AC2.

- [ ] T003 Fix any infra blocker surfaced by T002 (import error, missing
  dep, broken harbor flag). Depends: T002.

- [ ] T004 Install the non-stop loop: link & enable `icarus-bench.service`
  (systemd user service running `icarus-bench-loop`); confirm it is
  `active (running)` and ticks land in `~/.icarus/bench-runner.log`.
  Depends: T003. Satisfies AC3.

- [ ] T005 Verify `icarus-bench-ticket-emitter` turns a failed bench
  result into a kanban ticket idempotently (no duplicates on re-run).
  Depends: T004. Satisfies AC4.

- [ ] T006 Hand off: post to Ares and Clawta on the agent-bus that the
  loop is live; they own "decide what's next." Depends: T004. Satisfies AC5.

- [ ] T007 Commit spec 068 + restored machinery on branch
  `068-icarus-bench-loop-revival`; open PR. Depends: T006.
