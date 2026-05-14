# Plan: delete the orchestration code from chitin

Status: ready to execute (in a fresh session, not end-of-day).

Date: 2026-05-06

## Decision reference

Per `docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md`:
chitin owns kernel + plugins + data, nothing else. Orchestration
moves to hermes + openclaw (out of this plan's scope; chitin just
needs to *not* own it).

Per the operator's sharpening on 2026-05-06 PM: **rip the entire
runner**, no incremental migration. Anything chain-side worth
keeping gets re-implemented natively in Go.

## Pre-execution checks

These should pass before any deletion. Run them at the start of
the executing session — repo state may have shifted.

1. **No runtime deps from kernel/data into apps/runner.** Verified
   on 2026-05-06: the only references from Go and Python are doc
   comments + one string literal in a router test. Re-verify:

   ```
   rg "apps/runner" go/ python/ --files-with-matches | head
   rg -l "swarm-backlog" go/ python/ scripts/ infra/systemd/ | head
   ```

   Doc comments are fine to leave or scrub later. Only a runtime
   import or shell-out from kernel/data → runner blocks.

2. **Bench scripts are external.** Verified on 2026-05-06: the
   `/evolve` skill drives `$BENCH_DIR` and `$CATA_DIR`, which are
   separate repos. Nothing bench-related in chitin to protect.

3. **No open PRs that touch apps/runner.** Close, merge, or notify
   authors before deletion. As of 2026-05-06 PM the only candidate
   is #372 (swarm-prompt-augmentation-esm-and-tests) — already
   open, presumably stale. Operator decides close vs merge.

4. **Hermes is the orchestrator going forward.** Operator's call,
   stated 2026-05-06 PM. Specifically: hermes kanban becomes the
   work-tracking source of truth (no chitin-side mirror), and
   `hermes kanban daemon` (or equivalent operator workflow)
   handles dispatch. If hermes isn't dispatching cleanly after
   deletion, that's a hermes config issue — debug there, don't
   re-add a chitin-side dispatcher.

## Execution sequence

Order matters: stop the things first, verify nothing breaks, then
delete files. Each phase is independently revertible (git revert)
until phase 4.

### Phase 1 — disable the timers (no file changes)

```
systemctl --user disable --now chitin-dispatcher.timer \
  chitin-shipped-entry-flipper.timer \
  chitin-groomer.timer \
  chitin-pr-event-ingester.timer \
  chitin-alarm-feeder.timer \
  chitin-debt-curator.timer \
  chitin-stale-doc-detector.timer \
  chitin-researcher.timer \
  chitin-lessons.timer
```

Keep enabled (kernel-internal):
- `chitin-kernel-redeploy.timer`
- `chitin-agent-unlock.timer`
- `chitin-envelope-rotate.timer`
- `chitin-chain-watch.timer`
- `chitin-codex-chain-ingest.timer` (chain ingestion — kernel-data side)

Verify still healthy after disable:
- `chitin-kernel gate status --agent=claude-code` returns normal
- `tail -f ~/.chitin/gov-decisions-$(date -u +%Y-%m-%d).jsonl`
  still receives entries when an agent runs
- `chitin-chain-watch.service` continues firing every 1min with
  noop messages
- An interactive `claude` session in a chitin-policied cwd still
  governs correctly (sample tool call denies/allows as expected)

If anything regresses, re-enable the offending timer and
investigate before continuing.

### Phase 2 — verify the kernel + plugins + data work in isolation (24-48h soak)

Optional but recommended. Run with timers disabled for a day or two.
Confirm:
- gov-decisions chain keeps populating from real agent traffic
- swarm-health analysis still runs cleanly:
  `cd python/analysis && uv run python -m analysis.swarm_health --window 24h`
- routing-elo and fingerprint-outcomes still produce output
- chitin-chain-watch surfaces nothing alarming

If hermes is the new orchestrator, this is also when you confirm
it's actually dispatching (separate concern from chitin's deletion).

If anything depends on the disabled timers in non-obvious ways,
this soak surfaces it. If clean, proceed to phase 3.

### Phase 3 — delete the files

In a single commit per group so revert granularity is sane:

**Group A — apps/runner wholesale**
```
git rm -r apps/runner/
```

**Group B — work-tracking source-of-truth**
```
git rm docs/swarm-backlog.md
```
(Also any markdown linked from the dispatcher's read paths:
`docs/swarm-lessons.md` if the lessons pipeline is being retired,
`docs/roadmap.md` if it was runner-managed, etc. Check
`apps/runner/src/**/*.ts` references before delete.)

**Group C — systemd units for deleted services**
```
git rm infra/systemd/chitin-dispatcher.{service,timer}
git rm infra/systemd/chitin-shipped-entry-flipper.{service,timer}
git rm infra/systemd/chitin-groomer.{service,timer}
git rm infra/systemd/chitin-pr-event-ingester.{service,timer}
git rm infra/systemd/chitin-alarm-feeder.{service,timer}  # if alarm-feeder is runner-implemented
git rm infra/systemd/chitin-debt-curator.{service,timer}
git rm infra/systemd/chitin-stale-doc-detector.{service,timer}
git rm infra/systemd/chitin-researcher.{service,timer}
git rm infra/systemd/chitin-lessons.{service,timer}
```

After commit, re-run `bash scripts/install-systemd-units.sh` so
the symlinks for deleted units get cleaned up. (Or manually:
`rm ~/.config/systemd/user/chitin-{dispatcher,shipped-entry-flipper,...}.{service,timer}`.)

**Group D — schema lint that no longer has a target**
```
git rm tools/lint/backlog-entry-shape.ts
# plus any tools/lint/* tests
```

**Group E — cleanup any chain-side code that was in apps/runner
but is load-bearing**

If phase 2 surfaced anything chain-relevant in apps/runner that
the kernel/data side actually needed, port it to Go (or Python
analysis) and delete the TS version in the same commit. Examples
to watch for:
- chain emission logic (should already be in Go — verify)
- review-graph state machine (if it's read-only over the chain,
  port to Python; if it owns workflow state, the orchestrator
  takes it)

### Phase 4 — clean up the toolchain

After Group A is gone, the entire pnpm + nx + node_modules
toolchain may also be deletable. Check:

```
# Are there any remaining .ts files in chitin?
fd -e ts -E node_modules -E .git
# Any remaining package.json (other than the root one we're about to delete)?
fd package.json -E node_modules -E .git
```

If clean:
- `git rm package.json pnpm-lock.yaml nx.json eslint.config.* tsconfig*.json`
- `rm -rf node_modules` (untracked, no git rm)
- Update `scripts/install-kernel.sh` to call `go build` directly
  instead of `pnpm nx build execution-kernel`. The current command
  is `pnpm nx build execution-kernel && bash scripts/install-kernel-symlink.sh`
  in `package.json`'s `install-kernel` script — replace with the
  direct invocation.
- Remove `pnpm install` from any onboarding docs.

If something still depends on nx (unlikely after phase 3 but
verify), defer phase 4 and file as a follow-up.

### Phase 5 — write the new operator runbook

The operator runbook needs to reflect:
- "How do I add work?" → "File a task in hermes kanban; assign to
  a spawnable lane; the daemon picks it up." Not "edit
  swarm-backlog.md."
- "How do I see what's running?" → "hermes kanban list," not "tail
  the dispatcher log."
- "How do I see what chitin is doing?" → unchanged: chain log,
  swarm_health, fingerprint_outcomes, routing_elo.

## Rollback

Every phase is one git revert away until phase 3 group E and
phase 4. Group E and phase 4 are deletes-with-port; rollback means
git revert + manual port reversal. If you're nervous, keep phase 4
on a separate branch for a week.

If after deletion the swarm produces zero work for too long
(operator's call on threshold), that's a hermes/openclaw config
issue, not a chitin regression — debug there. Resist the urge to
re-add a chitin-side dispatcher to "unblock"; that recreates the
problem this plan is solving.

## Estimated scope

- Phase 1 (disable timers): 5 minutes
- Phase 2 (soak): 24-48 hours of wall clock, ~0 minutes of
  operator time
- Phase 3 (deletes): 30-60 minutes including verify + commits +
  re-install systemd
- Phase 4 (toolchain cleanup): 1-2 hours if clean, defer if not
- Phase 5 (runbook): 1-2 hours

Total operator-time: 3-5 hours, spread over a few days.
Net delete: ~1700 LOC TypeScript + ~6700 lines markdown + ~10
systemd unit files + possibly the entire pnpm/nx toolchain.

## What this plan does NOT cover

- **Standing up hermes/openclaw as the new orchestrator.** That's a
  separate effort with its own design questions (lane mapping,
  daemon config, PR-event reconciliation). This plan is purely the
  deletion side.
- **Migrating in-flight work.** Phase 2's soak window is the
  natural cut point: anything in flight in the legacy dispatcher
  finishes (or is abandoned), then phase 3 deletes the dispatcher.
- **Notifying anyone external** about workflow changes. Operator
  responsibility.
