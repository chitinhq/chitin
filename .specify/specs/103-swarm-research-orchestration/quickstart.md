# Spec 103 — Quickstart

End-to-end smoke. ~10 minutes assuming ares + clawta are reachable on the host.

## Prerequisites

- `chitin-orchestrator` binary installed (current main + PR-A merged)
- `hermes` and `openclaw` CLIs on PATH (`hermes --version`, `openclaw --version` both green)
- Temporal dev server running (`temporal-dev.service` enabled)
- Obsidian vault present at `/home/red/Documents/Obsidian Vault/`

## Step 1 — Declare your first schedule

```bash
mkdir -p ~/.chitin
cat > ~/.chitin/swarm-schedule.yml <<'YAML'
schedules:
  - id: smoke-clawta-echo
    agent: clawta
    cadence: 5m
    skills: []
    message: "echo: spec 103 quickstart smoke fire"
    tag: smoke
YAML
```

## Step 2 — Reconcile schedules into Temporal

```bash
chitin-orchestrator ensure-swarm
```

Expected:
```text
ensure-swarm reconciled 1 schedule:
  created:  swarm-smoke-clawta-echo (cadence @every 5m)
```

Verify in Temporal:
```bash
temporal schedule list | grep swarm-smoke
```

## Step 3 — Wait one cadence (5 min), verify the invocation fired

```bash
chitin-kernel chain tail --event-type swarm_invocation | jq '.payload | {schedule_id, agent, message, ts}'
```

Expected: one row with `schedule_id: "smoke-clawta-echo"`, `agent: "clawta"`, `message: "echo: spec 103 quickstart smoke fire"`.

If the schedule never fires: check `temporal workflow list --query 'WorkflowType="SwarmInvocationWorkflow"'`; check `chain tail --event-type swarm_invocation_failed`.

## Step 4 — Set up vault ingestion

```bash
cat > ~/.chitin/ingestion-sources.yml <<'YAML'
sources:
  - name: obsidian-vault
    type: obsidian-vault
    root: /home/red/Documents/Obsidian Vault/Research
    patterns: ["**/sources/*.md", "**/index.md"]
    watch: true
    extract:
      topic: frontmatter.topic
      tag: frontmatter.tag
    tag_default: research

  - name: sentinel-mined
    type: sentinel-findings
    root: /home/red/.chitin/sentinel-findings
    patterns: ["swarm-mined-*.json"]
    watch: true
    extract:
      agent_attribution: payload.agent
      confidence_signal: payload.confidence
YAML
```

Restart `chitin-orchestrator` (the `IngestionWorkflow` registers fsnotify watchers at boot):
```bash
systemctl --user restart chitin-orchestrator
```

## Step 5 — Drop a test source file, verify ingestion

```bash
mkdir -p "/home/red/Documents/Obsidian Vault/Research/Quickstart/sources"
cat > "/home/red/Documents/Obsidian Vault/Research/Quickstart/sources/2026-05-24-test.md" <<'MD'
---
topic: Quickstart
tag: smoke
source_url: https://example.com/paper
---

# Test paper

Body content.
MD
```

Within 60 seconds:
```bash
chitin-orchestrator swarm-queue list --tag smoke
```

Expected: one row with `topic: Quickstart`, `tag: smoke`, `status: unprocessed`.

And the chain has:
```bash
chitin-kernel chain tail --event-type swarm_finding_queued | jq '.payload'
```

## Step 6 — Triage the row

```bash
queue_id=$(chitin-orchestrator swarm-queue list --tag smoke --limit 1 --json | jq -r '.[0].queue_id')

chitin-orchestrator swarm-queue mark "$queue_id" discarded --notes "smoke test, no real finding"
```

Expected stdout:
```text
swarm-queue: marked 01HZQK... discarded (operator)
```

Chain event:
```bash
chitin-kernel chain tail --event-type swarm_finding_triaged | jq '.payload | {queue_id, from_status, to_status, notes}'
```

## Step 7 — Verify observability surface

```bash
chitin-orchestrator swarm-summary --days 1
```

Expected: a table with one row per agent×tag×day, including the `clawta`×`smoke` schedule's invocations and tool-call counts.

```bash
chitin-orchestrator swarm-summary --days 1 --json | jq
```

## Step 8 — Test on-demand swarm-ask

```bash
chitin-orchestrator swarm-ask clawta --message "list your last 3 invocations" --wait-for-reply --deadline 60s
```

Expected: clawta's reply printed to stdout within 60s.

## Step 9 — Clean up

```bash
# Remove the smoke schedule
sed -i '/smoke-clawta-echo/,/tag: smoke/d' ~/.chitin/swarm-schedule.yml
chitin-orchestrator ensure-swarm
# Expected: "removed: swarm-smoke-clawta-echo"

# Discard the smoke ingestion entry (already done in step 6)
rm "/home/red/Documents/Obsidian Vault/Research/Quickstart/sources/2026-05-24-test.md"
# fsnotify IN_DELETE → queue row's status auto-updates to source_deleted
```

## Failure modes worth knowing

| Symptom | Likely cause |
|---|---|
| `ensure-swarm` says `parse error: agent "foo" not in registry` | typo in agent name; spec 103 only knows `ares` and `clawta` |
| `swarm_invocation_failed` with `failure_kind: gateway_not_installed` | `hermes` or `openclaw` not on PATH for the orchestrator service |
| `swarm_invocation_failed` with `session_unresolved` | `gateway_session` field missing in config AND auto-resolver couldn't pick a session |
| No queue row after dropping a vault file | `IngestionWorkflow` not running; check `temporal workflow list`; check fsnotify watcher overflow in logs |
| `swarm-queue list` returns `database is locked` | another writer in flight; retry (busy_timeout is 5s) |
| `swarm-ask` exits with `gateway timeout` after deadline | agent is up but slow OR agent is hung; chain has `swarm_invocation_timeout` |
