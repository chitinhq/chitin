# Phase 1 Contracts: Operator Report Delivery

**Spec**: [spec.md](../spec.md) | **Plan**: [plan.md](../plan.md)

Four behavioural contracts. Each is verified by a test named in `tasks.md`.

## C1 — `chitin-kernel report` CLI (composition, side-effect-free)

The kernel subcommand that gathers telemetry and **prints** a report. It MUST
NOT post, send, or write anything except stdout (Constitution §1).

```
chitin-kernel report heartbeat [--dir <.chitin>] [--repo <path>]
chitin-kernel report digest    [--dir <.chitin>] [--repo <path>]
                               [--board <slug>] [--window-hours N]
                               [--console-base <url>]
```

| Guarantee | Detail |
|---|---|
| Output | The composed report message on **stdout** — skimmable text, with console URLs inline. Nothing else on stdout. |
| Exit 0 | A report was composed — including a *partial* report (one or more sources unavailable). |
| Exit non-zero | Only on an internal error that prevented composing any report at all. |
| No side effects | The command performs zero network posts and zero chain writes. Verified by review + a test asserting no `openclaw`/`gh pr create`/chain-write call path. |
| Degradation | A missing telemetry source yields a section/component marked `unknown`/`unavailable` with a reason — never omitted, never a crash (FR-003, FR-009). |

**Test**: `internal/report` unit tests — heartbeat with a healthy fixture, with
a degraded fixture, with an unreadable source; digest section grouping; link
formatting; the all-four-sections-present invariant.

## C2 — `deliver-operator-report.sh` (delivery, the side effect)

The swarm script that runs C1 and posts the result to Discord via `openclaw`.

```
deliver-operator-report.sh {heartbeat|digest} [--on-demand]
```

| Guarantee | Detail |
|---|---|
| Compose | Runs `chitin-kernel report <kind>`; on a non-zero exit, still delivers a minimal failure notice (the report machinery itself is down). |
| Deliver | `openclaw message send --channel discord --account <acct> --target <target> --text <message>` to the operator-configured destination only (FR-013). |
| Audit | Appends exactly one `ReportDeliveryRecord` to `~/.cache/chitin/operator-report.jsonl` per run — `delivered` or `failed`. |
| Exit codes | `0` delivered; `1` delivery failed (destination unreachable); `2` compose failed *and* the failure notice could not be sent. |
| Rate limit | `--on-demand` runs within a cooldown window of a prior on-demand run are coalesced — one delivery, not N (FR-014). |
| Never silent | A delivery failure is both audit-logged and surfaced to the operator on the next heartbeat (FR-010). |

**Test**: a bash-harness test in the `install_kernel_script_test.go` style —
mock `chitin-kernel` and `openclaw`; assert the audit record, the exit code,
and the failure path.

## C3 — Temporal Schedule JobSpecs

Two `JobSpec` entries registered in `go/orchestrator/schedules/Registry()`.

| JobSpec | Cron (default) | Command |
|---|---|---|
| `operator-heartbeat` | hourly | `deliver-operator-report.sh heartbeat` |
| `operator-digest` | once daily, operator TZ | `deliver-operator-report.sh digest` |

| Guarantee | Detail |
|---|---|
| Registration | Both appear in `Registry()`; `EnsureSchedules()` creates them idempotently at orchestrator-worker startup. |
| Overlap | Inherits `SCHEDULE_OVERLAP_POLICY_SKIP` — a run never overlaps itself. |
| Visibility | A failed run is a failed `JobResult` in Temporal history. |

**Test**: a `schedules` package test asserting both specs are in `Registry()`
with the expected names and cron shape.

## C4 — Discord message format

| Guarantee | Detail |
|---|---|
| Heartbeat | One short message: per-component status line (gateway / kernel / agents), kernel staleness, last redeploy, and any `missed_reports`. |
| Digest | One message, four labelled sections in fixed order — orchestration, kernel, drivers, PRs — each a few summary lines; every line with detail carries a chitin-console link. |
| Skimmable | The digest message stays within a bounded length; depth is offloaded to console links, never silently truncated (Edge Cases). |
| Degraded section | An unavailable section is shown with its reason, not dropped. |

**Test**: `internal/report` render tests — assert section order, the
all-four-present invariant, link presence on detail lines, and a bounded
rendered length on a large-fixture digest.
