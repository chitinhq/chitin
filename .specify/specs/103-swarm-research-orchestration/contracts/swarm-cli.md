# Contract — `chitin-orchestrator` swarm CLI surface

Four new subcommands. All extend `cmd/chitin-orchestrator/main.go`'s subcommand registry.

## `ensure-swarm`

```bash
chitin-orchestrator ensure-swarm [--config <path>] [--dry-run]
```

Reconciles `~/.chitin/swarm-schedule.yml` with Temporal Schedules.

| Flag | Default | Notes |
|---|---|---|
| `--config` | `$HOME/.chitin/swarm-schedule.yml` | Path to schedule config |
| `--dry-run` | false | Print actions (create/update/delete) without applying |

**Exit codes:**
- 0: reconciled (or dry-run printed)
- 1: config invalid (parse error, validation failure)
- 2: Temporal unreachable

**stdout (success):**
```text
ensure-swarm reconciled 3 schedules:
  created:  swarm-ares-arxiv-scan (cron @every 6h)
  updated:  swarm-clawta-discord-digest (cadence 12h → 24h)
  removed:  swarm-old-removed-entry
```

## `swarm-ask`

```bash
chitin-orchestrator swarm-ask <agent> --message "..." [--deadline 30m] [--wait-for-reply] [--tag <tag>] [--session <id>]
```

Ad-hoc invocation. Per FR-024 / FR-025.

| Flag | Default | Notes |
|---|---|---|
| `<agent>` | (positional) | `ares` \| `clawta` |
| `--message` | (required) | Literal message |
| `--deadline` | `30m` | Workflow hard ceiling |
| `--wait-for-reply` | false | Synchronously wait up to deadline for a direct response |
| `--tag` | none | Free-form tag attached to the emitted `swarm_invocation` |
| `--session` | (auto) | Override gateway session |

**Exit codes:**
- 0: invocation completed (response printed to stdout if `--wait-for-reply`)
- 1: invalid agent / missing message
- 2: gateway unreachable / timeout

**stdout (with reply):**
```text
[ares via hermes-mcp, session=ares-default, 14s elapsed]
<agent's reply>
```

**stdout (fire-and-forget):**
```text
[ares via hermes-mcp, session=ares-default]
dispatched; check chain for swarm_invocation event with temporal_run_id=<id>
```

## `swarm-queue list | show | mark`

```bash
chitin-orchestrator swarm-queue list [--tag <T>] [--status <S>] [--topic <T>] [--agent <A>] [--limit <N>] [--json]
chitin-orchestrator swarm-queue show <queue_id> [--json]
chitin-orchestrator swarm-queue mark <queue_id> <transition> [--ref <REF>] [--notes "..."]
```

| Subcommand | Notes |
|---|---|
| `list` | FR-019. Defaults: `--status unprocessed`, `--limit 20`. Output: table (default) or JSON. |
| `show` | FR-020. Full record + linked chain events (`swarm_invocation`, `swarm_finding_queued`, `swarm_finding_triaged` for this queue_id). |
| `mark` | FR-021. Transitions: `spec-drafted REF` \| `discarded` \| `deferred` \| `unprocessed` (re-undefer). |

**Exit codes:**
- 0: success
- 1: invalid queue_id / invalid transition / DB locked
- 2: DB file missing or schema mismatch

**List output (table):**
```text
queue_id            ts                        source           tag             topic                       status
01HZQK9X...J        2026-05-24T12:15:23Z      obsidian-vault   research-scan   AI Agent Governance         unprocessed
01HZQM12...L        2026-05-24T13:00:01Z      sentinel-mined   chain-mine      (none)                      unprocessed
```

## `swarm-summary`

```bash
chitin-orchestrator swarm-summary [--agent <A>] [--tag <T>] [--days <N>] [--json]
```

Per FR-022. Chain aggregation, no DB read.

| Flag | Default | Notes |
|---|---|---|
| `--agent` | all | Filter by `ares` \| `clawta` |
| `--tag` | all | Filter by free-form tag |
| `--days` | 7 | Window into the chain |
| `--json` | false | Machine-readable output |

**Exit codes:**
- 0: rendered
- 1: invalid window
- 2: chain replay failed

**stdout (table):**
```text
agent   tag             day         invocations   tool_calls   errors   last_success
ares    research-scan   2026-05-24  4             127          0        2026-05-24T18:00:01Z
clawta  ecosystem       2026-05-24  1             34           1        2026-05-24T09:01:14Z
clawta  chain-mine      2026-05-24  1             56           0        2026-05-24T03:00:42Z
```

## Subcommand dispatch (main.go)

```go
// main.go — extend runMain switch
case "ensure-swarm":   return cmdEnsureSwarm(args[1:])
case "swarm-ask":      return cmdSwarmAsk(args[1:])
case "swarm-queue":    return cmdSwarmQueue(args[1:])
case "swarm-summary":  return cmdSwarmSummary(args[1:])
```

All four follow the existing pattern: thin `cmdX` entrypoint that calls a testable `runX(ctx, args, stdout, stderr)`.

## Help text

Each subcommand registers a `--help` / `-h` that prints usage. `chitin-orchestrator help` lists all subcommands grouped by spec (`spec 097: schedule, cancel, status, validate`; `spec 098: factory-listen, simulate-webhook`; `spec 103: ensure-swarm, swarm-ask, swarm-queue, swarm-summary`).
