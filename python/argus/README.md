# Argus Observatory

Tail governance decision events, index them, ingest Hermes/OpenClaw/Discord observability sources, and generate daily markdown digests with LLM narration.

## Architecture

- **Indexer**: Tails `~/.chitin/gov-decisions-*.jsonl` files and indexes into SQLite at `~/.argus/index.db` with replay-safety via line-hash idempotency.
- **Log ingesters**: Follow `~/.hermes/logs/*.log` and `~/.openclaw/logs/*.log` with checkpoint-resume, truncated-line protection, and rotation reopen.
- **Discord transcript ingester**: Pulls `#ares` + `#clawta` history incrementally using the operator's openclaw Discord token/config.
- **Detectors**: Four deterministic anomaly detectors run over the index:
  - **Deny Cluster**: N deny events within M-second window (default: N=4, M=300s)
  - **Unknown Rate Spike**: Unknown action_type rate >threshold% over 24h (default: >1%)
  - **Agent Failure Run**: Agent with ≥N consecutive deny events (default: N=3)
  - **Stuck Flow**: Agent idle for >M seconds (default: M=3600s)
- **Log-derived detectors**:
  - **Hermes Standup Gap**: >8h between consecutive Hermes standups.
  - **OpenClaw Workflow Failure Correlation**: workflow failure with or without matching `kanban-flow block`.
  - **Discord Narration Gap**: dispatch with no `#clawta` announce.
- **Reporter**: Daily digest generator with qwen3.6:27b LLM narration paragraph + structured detector tables.
- **CLI**: `argus query "<q>"` for NL→SQL queries via qwen.

## Installation

Argus is shipped as a Python package in `python/argus/` inside the chitin
repo. The recommended install uses `uv` to expose `argus` as a console
script in `~/.local/bin/`:

```bash
# From repo root
uv tool install ./python/argus

# Install ollama and qwen model (optional — narration and `argus query` use it)
ollama pull qwen3.6:27b

# Create the index dir (the indexer auto-creates it on first write, but
# pre-creating it lets you point another tool at the path immediately)
mkdir -p ~/.argus
```

To point the daily report at a Discord webhook for the `#ares` summary,
write the URL into a systemd-style env file (no quoting):

```bash
mkdir -p ~/.config/argus
printf 'ARGUS_DISCORD_WEBHOOK=https://discord.com/api/webhooks/...\n' \
    > ~/.config/argus/env
```

Quiet days (no detector findings) skip the Discord post by default — pass
`--discord-always` to override.

## Usage

### Systemd Setup

```bash
cp python/argus/systemd/*.service ~/.config/systemd/user/
cp python/argus/systemd/*.timer  ~/.config/systemd/user/
systemctl --user daemon-reload

# Always-on indexer (true tail/follow, handles date rollover)
systemctl --user enable --now argus-indexer.service

# Daily report at 07:00 local
systemctl --user enable --now argus-report.timer
```

### Manual Commands

```bash
# Index all decision events
python -m argus index --decisions-dir ~/.chitin

# Generate daily report
python -m argus report --report-dir ~/.chitin/reports

# Query the index with natural language
python -m argus query "How many denies by rule_id in the last 24h?"
```

Optional source overrides:

```bash
export ARGUS_HERMES_LOGS_DIR=~/.hermes/logs
export ARGUS_OPENCLAW_LOGS_DIR=~/.openclaw/logs
export ARGUS_OPENCLAW_CONFIG=~/.openclaw/openclaw.json
export ARGUS_DISCORD_ARES_CHANNEL_ID=<channel_id>
export ARGUS_DISCORD_CLAWTA_CHANNEL_ID=<channel_id>
export ARGUS_PATTERN_TIMEOUT_SECONDS=5
export ARGUS_PATTERN_HOURLY_TOKEN_BUDGET=12000
```

## Design Invariants

**Read-only**: Indexer reads JSONL, detectors read index, reporter reads index. Zero writes to kanban/chain/agent state.

**Replay-safe**: Indexer can be restarted at any point and produces the same index given the same input files via line-hash idempotency (UNIQUE constraint on line_hash).

**Deterministic detectors**: Same index snapshot always produces the same detector outputs. LLM narration is generative but separated from structured findings.

## Boundary Conditions (Tested)

- **Empty source file**: Indexer handles without panic.
- **Single event**: Daily digest renders without divide-by-zero.
- **Date rollover at midnight**: Drains yesterday's tail, switches to today's file, no events dropped.
- **Malformed JSONL line**: Skip + count, never block.
- **Duplicate replay**: Idempotent via line_hash (no duplicate rows).
- **Detector edge cases**: Exactly N events at exactly M seconds boundary; N-1 does NOT trigger, N DOES trigger.
- **Qwen unavailable**: Daily report still produces structured detector output; narration degrades to placeholder.
- **Index corruption**: Indexer detects + refuses to write (sqlite PRAGMA journal_mode=WAL); alerts operator.
- **Quiet day**: Digest renders "all quiet"; no #ares push.

## Schema

### events table

```sql
CREATE TABLE events (
    id INTEGER PRIMARY KEY,
    line_hash TEXT UNIQUE NOT NULL,         -- Idempotency key
    ts TEXT NOT NULL,                       -- ISO 8601 timestamp
    ts_unix INTEGER NOT NULL,               -- Unix epoch (indexed)
    allowed INTEGER NOT NULL,               -- 0=deny, 1=allow
    mode TEXT,
    rule_id TEXT,                           -- Indexed for fast deny grouping
    reason TEXT,
    escalation TEXT,
    agent TEXT,                             -- Indexed for agent analysis
    action_type TEXT,
    action_target TEXT,
    envelope_id TEXT,
    tier TEXT,
    cost_usd REAL,
    input_bytes INTEGER,
    tool_calls INTEGER,
    model TEXT,
    role TEXT,
    workflow_id TEXT,
    fingerprint TEXT
);

-- Indexes for fast queries
CREATE INDEX idx_ts_unix ON events(ts_unix);
CREATE INDEX idx_rule_id ON events(rule_id);
CREATE INDEX idx_allowed ON events(allowed);
CREATE INDEX idx_agent ON events(agent);
```

## Detectors

### Deny Cluster
Triggers when ≥N deny events occur within M seconds.
- **Invariant**: At least N events at timestamps within [t, t+M).
- **Boundary**: N-1 does NOT trigger; N DOES trigger.
- **Default**: N=4, M=300 seconds (matches #513 escalation threshold).

### Unknown Rate Spike
Triggers when action_type unknown rate exceeds threshold over 24h window.
- **Invariant**: (unknown_count / total_count) * 100 > threshold_percent.
- **Boundary**: Exactly threshold% does NOT trigger; >threshold% DOES.
- **Default**: >1% over 24h.

### Agent Failure Run
Triggers when an agent has ≥N deny events.
- **Invariant**: Agent has ≥N denies (sorted by ts_unix).
- **Boundary**: N-1 does NOT trigger; N DOES trigger.
- **Default**: N=3.

### Stuck Flow
Triggers when agent idle for >M seconds.
- **Invariant**: Agent's last event is >M seconds ago.
- **Boundary**: Exactly M seconds ago does NOT trigger; >M DOES.
- **Default**: M=3600 seconds (1 hour).

## Examples

### Sample Report

```markdown
# Argus Observatory Report — 2026-05-13

*Generated: 2026-05-13T12:00:00Z*

## Executive Summary

**2 findings detected.**

In the last 24 hours, the governance system processed 1,250 decisions with a 3.2% deny rate. The most frequent deny rule was "no-destructive-rm" with 32 violations, primarily from the copilot-cli agent.

## Statistics

- **Total decisions:** 1,250
- **Denies:** 40 (3.2%)
- **Allows:** 1,210

### Top Deny Rules

- `no-destructive-rm`: 32 denies
- `bounds:max_files_changed`: 5 denies
- `no-curl-pipe-bash`: 3 denies

### Top Deny Agents

- `copilot-cli`: 35 denies
- `claude-code`: 5 denies

## Detector Findings

| Detector | Severity | Title | Details |
|----------|----------|-------|---------|
| deny_cluster | warning | Deny cluster: 4 denies in 300s | count: 4<br>rules: ["no-destructive-rm"]<br>agents: ["copilot-cli"] |
| agent_failure_run | warning | Agent copilot-cli failure run: 5 consecutive denies | agent: copilot-cli<br>failure_count: 5<br>rules: ["no-destructive-rm"] |
```

### Query Examples

```bash
# How many denies per agent?
python -m argus query "Show deny count grouped by agent"

# What are the most common deny rules?
python -m argus query "List top 10 deny rules by count"

# When was the last activity for each agent?
python -m argus query "Show each agent's last event timestamp"
```

## Testing

```bash
# Run all tests
python -m pytest python/argus/tests/ -v

# Run specific test suite
python -m pytest python/argus/tests/test_indexer.py -v
python -m pytest python/argus/tests/test_detectors.py -v
python -m pytest python/argus/tests/test_reporter.py -v
```

## Files

- `indexer.py`: JSONL→SQLite with replay-safety.
- `detectors.py`: Four deterministic anomaly detectors.
- `reporter.py`: Daily digest + qwen narration.
- `cli.py`: CLI interface (index, report, query).
- `tests/`: Comprehensive boundary-condition tests.
- `systemd/`: Systemd service + timer units.
