# 070 — Quickstart

How to stand up the Chitin Orchestrator's Phase 0 foundation and run the
first workflow. This is the Phase 0 exit check.

## 1. Temporal dev server

```sh
# install the temporal CLI (one binary), then:
temporal server start-dev --ui-port 8233
```

The dev server is self-hosted, single-binary, embedded persistence — the UI
at `localhost:8233` is the live operational view of every workflow run.
Runs as its own systemd user unit (`temporal-dev.service`).

## 2. Build and run the orchestrator worker host

```sh
cd go/orchestrator
go build ./cmd/chitin-orchestrator
./chitin-orchestrator        # registers workflows + activities, polls the task queue
```

Runs as `chitin-orchestrator.service` (installed by
`swarm/bin/install-chitin-orchestrator.sh`).

## 3. Trigger the hello-world workflow

```sh
temporal workflow start --type HelloWorkflow --task-queue chitin --workflow-id smoke-1
temporal workflow show --workflow-id smoke-1
```

**Phase 0 exit check**: the run appears in the Temporal UI as a complete,
inspectable, replayable timeline, and its telemetry reaches Chitin
Telemetry (FR-008).

## 4. Determinism gate

```sh
go run go.temporal.io/sdk/contrib/tools/workflowcheck ./workflows/...
```

Wired into CI — fails the build on non-deterministic workflow code (D4).

## 5. Next — Phase 1 (pull-loop)

Migrate the kanban pull-loop to a durable workflow and run it **beside** the
existing `kanban-pull-loop` cron for 7 days (SC-005). The cron is retired
only once every tick is confirmed inspectable and replayable.
