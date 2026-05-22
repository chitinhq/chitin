# Quickstart: Operator Report Delivery

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

This is the acceptance procedure — run it to prove the feature works. It is
also the spec's Independent Tests, made concrete.

## Prerequisites

- `chitin-kernel` built from this branch (carries the `report` subcommand).
- `openclaw` configured with a Discord account and the operator's report
  channel id.
- `gh` authenticated (for the digest's PRs-per-driver section).

## US1 — Heartbeat

1. **Compose only** (no side effect):

   ```sh
   chitin-kernel report heartbeat --dir ~/.chitin --repo ~/workspace/chitin
   ```

   Expect: a short message on stdout — gateway / kernel / agents status, kernel
   staleness, last redeploy outcome. Exit 0. Nothing posted anywhere.

2. **Deliver**:

   ```sh
   swarm/bin/deliver-operator-report.sh heartbeat
   ```

   Expect: the heartbeat message arrives in the operator's Discord channel; one
   `delivered` line appended to `~/.cache/chitin/operator-report.jsonl`.

3. **Degraded path**: induce a degraded state (e.g. the installed kernel is
   stale). Re-run step 1. Expect: the kernel component reads `stale` /
   `degraded` — not a generic "ok".

4. **Scheduled**: confirm the `operator-heartbeat` Temporal Schedule exists and
   fires; after one interval, a heartbeat lands in Discord with no manual step.

## US2 — Telemetry digest

1. **Compose only**:

   ```sh
   chitin-kernel report digest --window-hours 24 --repo ~/workspace/chitin \
     --console-base http://127.0.0.1:4280
   ```

   Expect: a four-section message — orchestration, kernel, drivers, PRs — on
   stdout, each detail line carrying a `http://127.0.0.1:4280/...` link. Exit 0.

2. **On-demand delivery**:

   ```sh
   swarm/bin/deliver-operator-report.sh digest --on-demand
   ```

   Expect: the digest arrives in Discord within 2 minutes (SC-002); a
   `delivered` audit line is written.

3. **Click-through**: open any digest link. Expect: the corresponding
   chitin-console view loads.

4. **Degraded source**: make one telemetry source unreadable; re-run step 1.
   Expect: that section is present and marked unavailable with a reason — the
   digest is still composed and the other three sections are intact (FR-009).

5. **Scheduled**: confirm the `operator-digest` Temporal Schedule fires once
   daily and the digest lands in Discord.

## US3 — Research & Obsidian delivery

1. Produce (or simulate) a research report / Obsidian note. Expect: a Discord
   message announcing it, with a click-through link, via the same channel.

## Failure surfacing (FR-010)

1. Point the destination at an unreachable target; run
   `deliver-operator-report.sh heartbeat`. Expect: exit 1, a `failed` audit
   line.
2. Restore the destination; run the next heartbeat. Expect: its
   `missed_reports` names the failed delivery — the miss was not silent.

## Acceptance

The feature passes when: heartbeat and digest both deliver on schedule and
on-demand; every digest detail line links to a working console view; a
degraded state and a delivery failure are both visibly surfaced; and zero
reports are silently missed across a multi-day run (SC-001, SC-005).
