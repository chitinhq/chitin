# Phase 1 Data Model: Operator Report Delivery

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

All entities here are **in-memory composition values** plus one append-only
audit log. This feature introduces no database — it reads existing telemetry
sinks and writes one delivery log.

## ComponentStatus *(value object)*

One liveness fact about one system component.

| Field | Type | Notes |
|---|---|---|
| `name` | string | `gateway`, `kernel`, `agents` (or a named agent) |
| `state` | enum | `healthy` \| `degraded` \| `unknown` |
| `detail` | string | why degraded/unknown; empty when healthy |

**Rule**: `state` is `unknown` when the component could not be probed — never
`healthy` on the absence of a signal (FR-003).

## Heartbeat

A point-in-time liveness snapshot (US1).

| Field | Type | Notes |
|---|---|---|
| `ts` | timestamp | composition time (UTC) |
| `components` | []ComponentStatus | gateway, kernel, agents |
| `kernel_staleness` | string | `current` \| `stale` \| `unknown` (from `chitin health`) |
| `last_redeploy` | string | `ok` \| `failed` \| `unknown` (from `chitin health`) |
| `missed_reports` | []string | reports that failed to deliver since the last heartbeat (FR-010) |

**Rule**: a heartbeat is composed and delivered even when one or more
components are `unknown`; it never blocks on a slow probe.

## DigestSection / DigestLine

The digest is four sections; each section is summary lines (US2).

**DigestSection**

| Field | Type | Notes |
|---|---|---|
| `key` | enum | `orchestration` \| `kernel` \| `drivers` \| `prs` |
| `title` | string | human heading |
| `lines` | []DigestLine | summary rows |
| `available` | bool | false when the source could not be read |
| `unavailable_reason` | string | set when `available` is false (FR-009) |

**DigestLine**

| Field | Type | Notes |
|---|---|---|
| `text` | string | one skimmable summary line |
| `console_url` | string | deep link into chitin-console; empty if no detail view |

**Rule**: a section with `available=false` is rendered with its
`unavailable_reason` — never silently dropped (FR-009).

## TelemetryDigest

| Field | Type | Notes |
|---|---|---|
| `ts` | timestamp | composition time (UTC) |
| `window` | duration | the period summarised (default 24h) |
| `sections` | []DigestSection | exactly the four keys above, in order |
| `on_demand` | bool | true when operator-requested rather than scheduled |

## DriverActivity *(value object, feeds the `drivers` and `prs` sections)*

| Field | Type | Notes |
|---|---|---|
| `driver` | string | claude, codex, copilot, gemini, hermes, openclaw |
| `decisions` | int | governance decisions in the window |
| `allowed` / `denied` | int | verdict split |
| `summary` | string | what the driver worked on (most-touched targets) |
| `prs` | []PRRef | PRs shipped in the window |

**PRRef**: `number` (int), `title` (string), `state` (string), `url` (string)
— attributed to a driver by the `agent/<driver>-<slug>` branch convention.

## DeliveryDestination *(operator-configured)*

| Field | Type | Notes |
|---|---|---|
| `channel` | string | `discord` |
| `account` | string | `openclaw` account name (default `default`) |
| `target` | string | `channel:<id>` — the operator's report channel |

**Rule**: an agent MUST NOT deliver to a `target` other than the configured one
(FR-013).

## ReportSchedule *(operator-configured)*

| Field | Type | Notes |
|---|---|---|
| `heartbeat_cron` | string | default hourly |
| `digest_cron` | string | default once daily at an operator-set time |
| `on_demand` | trigger | a Clawta-routed Discord command |

## ReportDeliveryRecord *(append-only audit — `~/.cache/chitin/operator-report.jsonl`)*

One line per delivery attempt; mirrors `install-kernel.jsonl`'s shape.

| Field | Type | Notes |
|---|---|---|
| `ts` | RFC3339 | attempt time |
| `kind` | enum | `heartbeat` \| `digest` |
| `outcome` | enum | `delivered` \| `failed` |
| `trigger` | enum | `scheduled` \| `on-demand` |
| `detail` | string | failure reason, or the destination on success |

**Rule**: every attempt — success or failure — appends exactly one record. The
next heartbeat reads this log to populate `missed_reports` (FR-010).

## State & relationships

- `Heartbeat` and `TelemetryDigest` are composed fresh per run — no persistence
  beyond the `ReportDeliveryRecord` audit line.
- `Heartbeat.missed_reports` is derived: delivery records with
  `outcome=failed` since the previous successful heartbeat.
- `TelemetryDigest.sections` always has all four keys; an unreadable source
  yields `available=false`, not a missing section.
