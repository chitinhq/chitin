# Usage feeds — driver quota visibility

Status: shipped (codex). Roadmap: gemini, ollama-cloud.

## Why

`chitin-budget` historically tracked $-spend from the swarm-rollup JSONs (Anthropic API per-token billing). That works for paid APIs but not for:

- **Subscription CLIs** (codex, gemini): no per-call dollar cost; flat-rate monthly with rate limits as the soft ceiling
- **Self-hosted with cloud bursting** (ollama-cloud): quota is rpm/tpm, not $
- **Rate-limited free tiers** generally

A unified "usage" surface lets the operator see all of these in one dashboard alongside $-spend.

## Where feeds live

`~/.cache/chitin/usage/<driver>.json` — one file per driver. Override the directory with `CHITIN_USAGE_FEED_DIR`.

## Schema

```json
{
  "driver": "codex",
  "axis": "quota_percent",
  "plan_type": "plus",
  "last_observed": "2026-05-04T00:43:39.125Z",
  "warnings": [],
  "calls_total": 41,
  "windows": [
    {
      "label": "primary",
      "used_percent": 1.0,
      "window_minutes": 300,
      "resets_at": 1777872638
    },
    {
      "label": "secondary",
      "used_percent": 0.0,
      "window_minutes": 10080,
      "resets_at": 1778459438
    }
  ]
}
```

Required fields:

| Field | Type | Meaning |
|---|---|---|
| `driver` | string | Canonical driver name (matches the `--agent=` flag and chain payload) |
| `axis` | string | One of `quota_percent`, `calls_count`, `rpm_tpm`, `usd` (existing $ rows; not used by feeds today) |
| `plan_type` | string | Vendor plan label (`plus`, `pro`, free, etc). Empty when irrelevant |
| `last_observed` | iso8601 | When the feed was last refreshed; used by chitin-status to flag stale feeds |
| `warnings` | array | Non-empty list = soft signal an operator should see; one element = one warning. `chitin-budget --check` exits 1 if any feed has a warning. |
| `windows` | array | Per-window axis-specific metrics. See per-axis sections below. |

### `axis: quota_percent` (codex)

```json
"windows": [
  {"label": "primary", "used_percent": 1.0, "window_minutes": 300, "resets_at": 1777872638},
  {"label": "secondary", "used_percent": 0.0, "window_minutes": 10080, "resets_at": 1778459438}
]
```

`window_minutes` documents the rolling-window length the vendor enforces (300 = 5h for codex Plus). `resets_at` is unix seconds; chitin-budget renders it as "in 4h06m" or "passed".

### `axis: calls_count` (gemini, future)

```json
"windows": [
  {"label": "primary", "used_count": 47, "cap": 100, "window_minutes": 300, "resets_at": 0}
]
```

When the vendor publishes the cap, populate `cap`. When `resets_at` is 0/unknown, chitin-budget shows "—".

### `axis: rpm_tpm` (ollama-cloud, future)

```json
"windows": [
  {"label": "rpm", "used_percent": 18.0, "window_minutes": 1, "resets_at": 0},
  {"label": "tpm", "used_percent": 35.0, "window_minutes": 1, "resets_at": 0}
]
```

Per-minute rolling windows. `used_percent` of the cap is the most useful render.

## Producers

| Driver | Source | Producer |
|---|---|---|
| `codex` | `~/.codex/sessions/**/*.jsonl` (rate_limits in token_count events) | `python -m analysis.codex_mine usage --write-feed ~/.cache/chitin/usage/codex.json` |
| `gemini` | TBD — gemini doesn't publish quota in its session logs today; may scrape stderr for rate-limit signals | TODO |
| `ollama-cloud` | response headers (`X-RateLimit-Remaining` etc.) captured at the activity layer | TODO — activity stderr scraper proposed |

## Consumers

- `chitin-budget` renders the "Quota / usage" section after the $-spend table; `--check` exits 1 on any warning
- `chitin-status` (future) — surfaces top-line usage % per driver in the operator dashboard
- `chitin-kernel chain stats` (future) — joins call_total across feeds with per-action stats from the chain

## Refresh cadence

Producers are designed to be runnable from a systemd timer:

```ini
# infra/systemd/chitin-codex-usage-feed.timer (TODO)
[Unit]
Description=Refresh codex usage feed every 10 minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=10min

[Install]
WantedBy=timers.target
```

The codex feed is cheap to refresh (reads ~1MB of JSONL, writes ~1KB). Gemini scrape will be similar; ollama-cloud capture is per-request at the activity layer.

## Operator quickstart

```bash
# Refresh the codex feed manually
mkdir -p ~/.cache/chitin/usage
python -m analysis.codex_mine usage --write-feed ~/.cache/chitin/usage/codex.json

# Then chitin-budget shows it
chitin-budget
chitin-budget --json
chitin-budget --check    # exit 1 if any rate-limit hit
```
