# Phase 0 Research: Operator Report Delivery

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

The spec carries no `NEEDS CLARIFICATION` markers — the operator resolved the
three scope-shaping decisions up front (phased heartbeat→digest; daily +
on-demand cadence; deliverer chosen here). Phase 0 fixes the **approach**: which
agent delivers, how reports are scheduled, where report logic lives, and which
telemetry sources feed the digest.

## Decision 1 — Deliverer: Clawta, via `openclaw message send`

**Decision**: Clawta delivers operator reports through
`openclaw message send --channel discord --account default --target channel:<id>`.

**Rationale**: This path is already in production. `swarm/workflows/kanban-dispatch.lobster`
posts to Discord through `openclaw message send` at ~12 call sites (dispatch
start, finalize success/failure), proven since 2026-05-11, with
`--target channel:<id>` routing and best-effort (`|| true`) semantics. The
Hermes "Ares" agent has **no** Discord message-posting: "Ares" is a conceptual
agent role, not a binary, and a `hermes message send` command was designed in
2026-05-07 but never implemented. Choosing Clawta means zero new Discord
plumbing; choosing Ares means building a Hermes→Discord bridge first.

**Alternatives considered**: Ares/Hermes — rejected, would require a new Discord
egress path. The agent-bus Discord mirror (`services/agent-bus/discord_push.py`,
spec 021) — rejected, it is a webhook for inter-agent bus traffic, not
operator-facing report delivery.

## Decision 2 — Scheduling: Temporal Schedules (the spec 081 pattern)

**Decision**: The hourly heartbeat and the daily digest are each a `JobSpec`
registered in `go/orchestrator/schedules/`, run as Temporal Schedules.

**Rationale**: Spec 081 retired the systemd-cron layer and migrated every
periodic task to Temporal Schedules. The established pattern is a three-step
add: (1) a `JobSpec` file in `go/orchestrator/schedules/` (Name, Command, Args,
Cron, TimeZone, Description); (2) register it in `Registry()` in `schedules.go`;
(3) `EnsureSchedules()` creates the Temporal Schedule idempotently at
orchestrator-worker startup. The schedule fires `ScheduledJobWorkflow` →
`RunScheduledJob` activity, which runs the job's command as a subprocess and
records a typed `JobResult` in Temporal history.

**Alternatives considered**: A new systemd timer — rejected, spec 081 explicitly
retired that layer; a fresh timer would reintroduce what was just culled.

## Decision 3 — Report logic in Go, delivery via a thin swarm script

**Decision**: Report **composition** is a Go subcommand —
`chitin-kernel report heartbeat` and `chitin-kernel report digest` — backed by a
new `go/execution-kernel/internal/report/` package; it gathers telemetry,
composes the message (text + console links), and prints it to stdout. Report
**delivery** is a thin script, `swarm/bin/deliver-operator-report.sh`, that runs
the Go command and pipes the output to `openclaw message send`.

**Rationale**: Constitution §1 forbids the kernel from producing side effects —
it gates and writes the chain; everything else posts through hermes/openclaw.
A `chitin-kernel report` command that posted to Discord would violate §1.
Splitting it keeps the kernel command pure (read telemetry → compose → print),
which is also unit-testable (driver grouping, PR counting, link formatting,
degradation), and isolates the side effect in the openclaw-glue script.
Constitution §6 is satisfied: kernel-local composition lives in `internal/` +
`cmd/`; the cross-cutting openclaw glue lives in `swarm/`.

**Alternatives considered**: An all-shell implementation — rejected, the digest's
grouping/formatting logic has real boundary cases that deserve Go tests. The
kernel posting to Discord directly — rejected, violates Constitution §1.

## Decision 4 — Telemetry sources (US4 not yet built; read sinks directly)

**Decision**: The reports read existing telemetry directly:
- **Kernel health** → `chitin-kernel health` (now carries `kernel_staleness` and
  `redeploy_health` from spec 083 US2).
- **Per-driver activity** → `chitin-kernel decisions recent` over the central
  `~/.chitin/gov-decisions-*.jsonl` sink, grouped by `driver`/`agent`.
- **PRs per driver** → `gh pr list` filtered on the `agent/<driver>-<slug>`
  branch convention.
- **Orchestration status** → the orchestrator scheduler tick telemetry
  (`TickRecord`/`DispatchRecord`) and Temporal workflow state.

**Rationale**: Spec 083 US4's unified telemetry query interface
(`go/execution-kernel/internal/telemetry/`) is **not yet implemented** (083
tasks T021–T025, still pending). Per this spec's own assumption, the digest
reads the underlying sinks directly until US4 ships, then switches to the
unified interface behind the same `internal/report/` boundary — no consumer
change.

**Alternatives considered**: Block this feature on 083 US4 — rejected, the spec
explicitly makes US4 an optional dependency with a direct-read fallback.

## Decision 5 — Console links: deep-link existing chitin-console routes

**Decision**: Digest summary lines deep-link into existing chitin-console routes
— `/overview`, `/sessions/:chainId`, `/tickets?assignee=<driver>`,
`/orchestrator`. Where a report needs a view that does not exist, a route/API
endpoint is added to `apps/chitin-console` / `apps/chitin-console-api`.

**Rationale**: chitin-console is already an always-on systemd service (spec 080
US3) on `127.0.0.1:4280` with a read-only API and the routes above. The digest
only needs stable URLs; it does not build the console. New per-report detail
views, if needed, are small additions to `server.mjs`.

**Alternatives considered**: Put full detail in the Discord message — rejected,
the spec requires a skimmable message with detail offloaded to the console.

## Decision 6 — On-demand trigger via a Clawta-routed Discord command

**Decision**: The operator requests an off-schedule digest by issuing a command
in Discord; Clawta (which already processes inbound Discord) routes it to the
same `deliver-operator-report.sh digest` path.

**Rationale**: Clawta already has an inbound Discord surface. Reusing it avoids
a second control plane. Rapid repeat requests are coalesced/rate-limited in the
delivery script (a short cooldown window).

**Alternatives considered**: A CLI-only trigger — rejected, the operator's
interface for these reports is Discord; an on-demand request should live there.

## Decision 7 — Delivery-failure surfacing

**Decision**: The delivery script emits structured JSON per run (mirroring
`install-kernel.sh`'s `emit` pattern) and exits non-zero on a delivery failure;
`RunScheduledJob` records the failed `JobResult` in Temporal history. The next
heartbeat additionally notes any since-missed report.

**Rationale**: FR-010 requires a missed report to never be silent. Temporal
history makes a failed scheduled run visible; the next-heartbeat note covers the
operator who watches Discord, not Temporal.

**Alternatives considered**: Rely on Temporal history alone — rejected, the
operator's surface is Discord; a Discord-side signal is required.

## Decision 8 — Phasing: heartbeat (US1) → digest (US2) → research/Obsidian (US3)

**Decision**: Ship US1 (heartbeat) first, then US2 (digest), then US3.

**Rationale**: Matches the operator's stated phasing. US1 is the smallest slice
and proves the full delivery channel (compose → openclaw → Discord, scheduled)
end-to-end before the heavier digest composition is built on it.

## Cross-references

- **spec 081** — Temporal Schedule cron pattern (scheduling substrate).
- **spec 083 US2** — `chitin health` staleness + redeploy signals (heartbeat input).
- **spec 083 US4** — unified telemetry query interface (digest input; pending — direct-read fallback).
- **spec 080 US3** — chitin-console as a systemd service (the link target).
- **Constitution §1** — side-effect boundary (kernel composes, openclaw delivers).
- **Constitution §4** — the delivery script ships a tracked installer.
- **Constitution §6** — kernel-local logic in `internal/`+`cmd/`; openclaw glue in `swarm/`.
