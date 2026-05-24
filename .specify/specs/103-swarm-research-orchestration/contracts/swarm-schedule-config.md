# Contract — `~/.chitin/swarm-schedule.yml`

Operator-managed YAML at `$HOME/.chitin/swarm-schedule.yml`. Chitin reads only.

## Top-level shape

```yaml
schedules:
  - <ScheduleEntry>
  - <ScheduleEntry>
```

## ScheduleEntry fields

| Field | Type | Required | Notes |
|---|---|---|---|
| `id` | string | yes | Unique within file. Becomes Schedule ID `swarm-<id>` in Temporal. ASCII, `[a-z0-9-]+`. |
| `agent` | string | yes | One of `ares`, `clawta`. Must be in driver registry. |
| `cadence` | string | yes | Duration: `5m`, `30m`, `1h`, `6h`, `24h`. Cron also accepted: `"0 */6 * * *"`. |
| `message` | string | yes | Literal prompt sent to the agent. No templating. |
| `skills` | []string | no | Free-form hint list. Chitin prepends `Available skills you may use: [<list>]. ` to the message before dispatch. Chitin does NOT verify skills exist on the agent. |
| `tag` | string | no | Free-form operator tag. Used by `swarm-queue list --tag` and `swarm-summary --tag`. |
| `gateway_override` | string | no | Overrides default agent→gateway mapping. Useful for testing. |
| `gateway_session` | string | no | Names the agent's session on the gateway. If omitted, chitin auto-resolves via `<gateway> sessions list`. |
| `wait_for_reply` | bool | no | Default false. Only honored on the `swarm-ask` on-demand path; ignored for scheduled invocations. |

## Validation (per FR-002)

Chitin enforces structural validation only:
- `id` is present and unique
- `agent` resolves in the driver registry
- `cadence` parses
- `message` is non-empty

No taxonomy validation on `message`, `tag`, or `skills` — these are free-form operator surfaces.

## Example

```yaml
schedules:
  - id: ares-arxiv-scan
    agent: ares
    cadence: 6h
    skills: [web-search, arxiv-fetcher, obsidian-vault]
    message: "Scan arXiv for new AI agent governance papers. Write per-source files to Research/AI Agent Governance/sources/."
    tag: research-scan

  - id: clawta-discord-digest
    agent: clawta
    cadence: 24h
    skills: [discord-read, discord-post]
    message: "Summarize today's #ops activity and post to #operator-digest."
    tag: ecosystem
    gateway_session: clawta-ops

  - id: clawta-chain-mine
    agent: clawta
    cadence: 24h
    skills: [filesystem-read, jsonl-parse]
    message: "Read ~/.chitin/events from last 24h. Identify patterns in WorkUnitWorkflow failures by capability. Write to ~/.chitin/sentinel-findings/swarm-mined-{date}.json."
    tag: chain-mine
```

## Reload semantics (FR-003)

`chitin-orchestrator ensure-swarm` reads the file and reconciles Temporal Schedules:
- Entries in file but not in Temporal → `Create`
- Entries in Temporal but not in file (with `swarm-` prefix) → `Delete`
- Entries in both with mismatched cadence/message → `Update`

Idempotent: re-running with unchanged file is a no-op.
