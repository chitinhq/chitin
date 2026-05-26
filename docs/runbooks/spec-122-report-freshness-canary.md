# Spec 122 Report Freshness Canary

The report freshness canary watches the generated HTML dashboards operators
read from the local reports hub. It detects stale or missing reports from file
metadata alone, so report producers do not need to change before the canary is
useful.

## Default Watches

The bundled config is `go/orchestrator/internal/reportfreshness/default-config.yaml`.
The installer copies it to `~/.chitin/report-freshness.yaml` only when no live
config exists. The default watched paths are:

| Path | SLA |
| --- | ---: |
| `/home/red/labs/local-ai-lab/wiki/assets/chain-summary-latest.html` | 24h |
| `/home/red/labs/local-ai-lab/wiki/assets/board-audit-latest.html` | 24h |
| `/home/red/labs/local-ai-lab/wiki/assets/industry-scan-latest.html` | 24h |

These are the three stale reports from the Ares scan captured on May 26, 2026
in `Chitin/Product Agent/2026-05-26 Telemetry Product Scan.md`.

## Install And Verify

Run the orchestrator installer:

```bash
swarm/bin/install-chitin-orchestrator.sh
```

The installer builds `chitin-orchestrator`, installs the systemd user unit, and
installs `~/.chitin/report-freshness.yaml` if it is missing. It does not
overwrite an existing config.

Verify the schedule is registered:

```bash
chitin-orchestrator schedules list | grep report-freshness-canary
```

Manually check report state:

```bash
chitin-orchestrator reports check
chitin-orchestrator reports list
```

`reports check` exits `0` when all reports are fresh, `2` when any report is
stale, and `3` when any report is missing.

## Configuration

The config path resolution order is:

1. `--config <path>` for CLI checks.
2. `$CHITIN_REPORT_FRESHNESS_CONFIG`.
3. `~/.chitin/report-freshness.yaml`.
4. The bundled default config.

Example:

```yaml
paths:
  - path: /absolute/path/to/report.html
    sla_hours: 24
cadence_minutes: 360
escalation_cooldown_hours: 24
```

To add a path, append a `paths` entry with an absolute path and positive
`sla_hours`.

To tune cadence, set `cadence_minutes`. The default is `360` minutes. Restart
the orchestrator so `schedules.EnsureSchedules` can register the new cadence.

To tune escalation spam control, set `escalation_cooldown_hours`. The default
is `24` hours per path.

To silence a known-broken report temporarily, remove or comment out that path
from the live config and restart the orchestrator. Re-add it when the producer
is repaired.

## Notifications

For every stale report outside its cooldown window, the canary emits
`stale_report_detected`, routes a `stale_report` reason through the spec 114
operator escalation surface, posts a Discord message, and emits
`stale_report_escalated`.

The Discord body includes:

- report path
- age in hours
- SLA in hours
- age source, either `mtime` or `embedded`
- `file://` URL

Repeated stale detections for the same path inside the cooldown window emit
`stale_report_suppressed` instead of notifying again.

## Producer Metadata Contract

Producers can improve the signal by embedding a metadata block near the top of
the HTML. The canary reads only the first 4 KiB and the first block wins.

```html
<!-- chitin-report-meta: {
  "generated_at": "2026-05-26T12:00:00Z",
  "source_window_start": "2026-05-26T00:00:00Z",
  "source_window_end": "2026-05-26T12:00:00Z",
  "source_commands": ["chitin-kernel chain stats --window-hours 24"],
  "freshness_sla_hours": 24,
  "board_db_path": "/home/red/.hermes/kanban/boards/chitin/kanban.db",
  "board_slug": "chitin"
} -->
```

When `generated_at` is present and parseable, it wins over file `mtime`. When
the block is absent or unparseable, the canary falls back to `mtime`.

## Replay Validation

After deployment, let one cadence cycle pass, at most 6 hours with the default
config. Then verify:

```bash
grep -h '"stale_report_detected"' ~/.chitin/events-*.jsonl
grep -h '"stale_report_escalated"' ~/.chitin/events-*.jsonl
```

There should be at least one detection for each default path and exactly one
escalation per path inside the cooldown window. `stale_report_escalated`
payloads include `notify_message_id`; it is empty when the current Discord
notifier does not return a provider message id.
