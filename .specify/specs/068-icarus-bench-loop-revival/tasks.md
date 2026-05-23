# 068 — Tasks

> Canonical task list for spec 068. One kanban ticket per task (spec 067
> derivation). Ordered by dependency.

- [x] T001 Restore PR #794's icarus-bench machinery from git
  (`git checkout 389b486^ -- swarm/bin/icarus-bench-runner
  swarm/bin/icarus-bench-ticket-emitter swarm/bin/install-icarus-bench-cron.sh
  swarm/icarus_harness/ swarm/tests/test_icarus_harness.py`). Satisfies AC1.

- [x] T002 Smoke-test a single run: `chitin-bench-runner --n-tasks 1
  --skip-emitter` produces `jobs/chitin-bench/<job>/` with a `result.json`.
  Depends: T001. Satisfies AC2. Verified 2026-05-22: trial completed with
  result.json, reward=0.0 (ollama_error, known; LRU rotates past).

- [x] T003 Fix any infra blocker surfaced by T002 (import error, missing
  dep, broken harbor flag). Depends: T002. N/A — no blocker found;
  ollama_error is handled by LRU rotation.

- [x] T004 Install the non-stop loop: link & enable
  `chitin-bench.service` (systemd user service running
  `chitin-bench-loop`); confirmed `active (running)` and ticks land in
  `~/.chitin-bench/bench-runner.log`. Depends: T003. Satisfies AC3.

- [x] T005 Verify `chitin-bench-ticket-emitter` turns a failed bench
  result into a kanban ticket idempotently (no duplicates on re-run).
  Depends: T004. Satisfies AC4. Verified 2026-05-22: 3 tickets created
  on first run, same IDs on second run (idempotent).

- [ ] T006 Hand off: post to Ares and Clawta on the agent-bus that the
  loop is live; they own "decide what's next." Depends: T004. Satisfies AC5.

- [ ] T007 Commit spec 068 + restored machinery on branch
  `068-icarus-bench-loop-revival`; open PR. Depends: T006.
  Note: all restored machinery was already on main (renamed icarus→chitin
  in spec 073). Only operational changes (systemd enable) were needed.
