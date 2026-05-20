# 040 — Octi scaffolding: Temporal Go module + workflowcheck CI gate

> Parent: spec 038 (Octi master, slice 4 = "Octi worker + Temporal integration").
> This spec operationalizes slice 4 and seeds the 041-048 corpus that
> migrates today's orchestration sprawl onto deterministic workflows.
>
> Ratification trail: agent-bus thread 17 msg 7702 (red, 2026-05-19).
> Three proposals received from Ares, Clawta, claude-code; hybrid
> ratified — Temporal Go + Clawta's three critiques baked in.

## Summary

Stand up the Octi workflow plane: a new Go module under `swarm/octi/`
that runs Temporal Go workflows against a single-binary
`temporal server start-dev` backend, gated by `workflowcheck` in CI
so determinism violations fail the build, with a first hello-world
workflow that proves the end-to-end shape — workflow → Activity →
chitin-kernel gate → event-store record → replay — before any
production cron is migrated onto it.

This is the **foundation** spec. Spec 041 layers in the Temporal-→-chitin
event mirror so audit reconstruction never needs Temporal visibility
APIs. Spec 042 layers in the agent-bus / Discord identity contract.
Specs 043-047 migrate one production surface each. Spec 048 defines
the tripwired `start-dev` → HA cluster migration template.

## Ticket refs

- Parent: `.specify/specs/038-octi-persistent-claude-session/spec.md`
  (slice 4: "Octi worker + Temporal integration").
- Ratification thread: agent-bus thread **17**, msgs 7679 (RFP),
  7680 (claude-code), 7685-7687 (Ares), 7689/7698-7699 (Clawta),
  7702 (red ratification), 7703 (Clawta ack).
- Ares' chitin-native counter-proposal absorbed as **constraints**,
  not rejected: Temporal MUST NOT become a second source of truth;
  Octi must remain replayable from chitin/Octi telemetry alone.

## File-system scope

### MAY write under

- `swarm/octi/` — new Go package root
  - `swarm/octi/workflows/` — Temporal workflow code (Go, deterministic)
  - `swarm/octi/activities/` — Activity code (nondeterministic side
    effects allowed: HTTP, sqlite, subprocess, LLM)
  - `swarm/octi/worker/` — Worker registration + task-queue plumbing
  - `swarm/octi/internal/` — shared helpers that workflows MAY import
    (must themselves pass `workflowcheck`)
- `swarm/bin/octi-worker` — single-binary entrypoint (replaces the
  spec-038 §12 slice-4 placeholder stub)
- `swarm/bin/octi` — operator CLI (start dev server, submit workflow,
  inspect history)
- `swarm/octi/tests/` — Go-native test files for workflows + activities
- `swarm/octi/e2e/` — end-to-end Go tests (per chitin spec 020 §1.2)
- `go.mod`, `go.sum` — at workspace root, add Temporal Go SDK
  dependencies (`go.temporal.io/sdk`, `go.temporal.io/sdk/contrib/tools/workflowcheck`)
- `.github/workflows/octi-workflowcheck.yml` — CI gate (new file)
- `swarm/bin/install-octi-cron.sh` — installer for the dev-server
  systemd/launchd unit (idempotent)
- `.specify/specs/040-octi-scaffolding/**`
- `.specify/specs/INDEX.md` — add 040-048 rows under Active specs

### MUST NOT write under

- `go/` (chitin kernel — Octi is a consumer of the gate, not an author)
- `chitin.yaml` (governance policy — kernel gate stays unchanged)
- `apps/` (chitin console — Octi UI work is out of scope here)
- `swarm/workflows/` (existing lobster files — migration is per
  043-047, not here)
- `~/.openclaw/`, `~/.hermes/` (live cron installations — handled by
  installer scripts, not direct edits)
- Any path under `~/workspace/chitin/` primary checkout (constitution §2)

## Goal

A new operator running `swarm/bin/octi dev start && swarm/bin/octi
hello "world"` sees the hello-world workflow execute, every
intermediate step land as a chitin gate decision in
`~/.chitin/gov-decisions-YYYY-MM-DD.jsonl`, and the same workflow
replayable from Temporal event history alone via
`swarm/bin/octi replay --workflow-id=<id>` — bit-for-bit identical
output to the original run. CI fails any PR that introduces
`time.Now`, `goroutine`, `chan`, `select`, `map`-range iteration,
`math/rand`, or direct I/O into `swarm/octi/workflows/` or
`swarm/octi/internal/`.

## Requirements

### R1 — Module layout and dependencies

Add a Go module skeleton under `swarm/octi/` with the four
subdirectories listed in MAY-write scope. Add Temporal Go SDK to
`go.mod`:

```
go.temporal.io/sdk v1.x.x
go.temporal.io/sdk/contrib/tools/workflowcheck v1.x.x
```

(Concrete minor/patch versions pinned at implementation time; the
spec freezes only the major and the SDK identity.)

`swarm/octi/internal/` packages MUST pass `workflowcheck` because
workflows import them. `swarm/octi/activities/` packages MAY use any
Go primitive — they are the side-effect boundary.

### R2 — `workflowcheck` as a CI gate

`.github/workflows/octi-workflowcheck.yml` runs
`workflowcheck ./swarm/octi/workflows/... ./swarm/octi/internal/...`
on every PR that touches those paths. Any violation fails the
check, blocking merge. The gate is **non-bypassable** — no
`workflowcheck: ignore` annotations are allowed in production
workflow code without an explicit `// spec: 040 §R2-waiver`
comment that links to an open ticket.

This is the operationalization of Ares' "workflow complexity attracts
imperative logic" risk callout. CI catches it before review does.

### R3 — Single-binary `octi-worker`

`swarm/bin/octi-worker` is a Go binary that registers the worker
against the dev or HA Temporal cluster. It reads its target
namespace + task queues from `~/.octi/worker.toml` (created by the
installer, never committed). On startup it:

1. Connects to the configured Temporal frontend
2. Registers all workflows under `swarm/octi/workflows/`
3. Registers all activities under `swarm/octi/activities/`
4. Listens on the configured task queues
5. Logs a single structured line per workflow start/complete/fail

The worker is the **only** Octi process that talks to Temporal. The
operator CLI (`swarm/bin/octi`) talks to Temporal via the Go SDK
client, not via the worker.

### R4 — Operator CLI `swarm/bin/octi`

Minimum verbs for slice-1 of this spec:

| Verb | Purpose |
|---|---|
| `octi dev start` | Launch `temporal server start-dev` in the background with SQLite persistence at `~/.octi/dev.db` |
| `octi dev stop` | Terminate the dev server |
| `octi dev status` | Report server PID, port, namespace, persistence path |
| `octi hello <msg>` | Submit the hello-world workflow with `<msg>` as input |
| `octi history <workflow-id>` | Print Temporal event history (json) |
| `octi replay <workflow-id>` | Re-execute the workflow against recorded history; assert bit-for-bit output match |

### R5 — Hello-world workflow proves the contract

`swarm/octi/workflows/hello.go` defines `HelloWorkflow(ctx, msg
string) (string, error)`. It:

1. Logs the input via `workflow.GetLogger(ctx)` (deterministic — log
   sink is a workflow primitive, not Go's `log`)
2. Calls `workflow.Now(ctx)` to get the deterministic timestamp
3. Invokes one Activity, `EchoActivity`, which:
   - Takes the message
   - Calls the chitin-kernel gate (`chitin-kernel gate eval --agent=octi
     --action=echo --input=...`) and verifies allow
   - Returns the echoed message + the gate decision id
4. Returns `"echoed:<msg>@<ts>:gate=<decision_id>"`

The hello-world is deliberately trivial — it exists to prove the
end-to-end shape: workflow code → Activity → kernel gate → event
record → replay. Every later spec (043-047) follows the same
shape with real work in the Activity.

### R6 — Replay proves determinism

`octi replay <workflow-id>` re-executes `HelloWorkflow` against the
recorded Temporal event history (using `worker.WorkflowReplayer`).
The replayer asserts that the workflow code, when fed the recorded
history, produces the exact same Commands in the exact same order.
Any drift fails the replay with a non-zero exit code and a diff
report.

An e2e test in `swarm/octi/e2e/replay_test.go` runs `octi hello "x"`,
captures the workflow id, runs `octi replay <id>`, and asserts
success. This e2e is the CI proof that determinism holds for the
shipped scaffolding.

### R7 — chitin gate is the floor

Every Activity that produces an externally visible side effect
(network, filesystem, subprocess, sqlite write, MCP call) MUST
issue a chitin-kernel gate evaluation before the side effect. The
gate decision (`allow` / `deny` / `defer`) is recorded in chitin's
existing `~/.chitin/gov-decisions-YYYY-MM-DD.jsonl` log.

Activity code MUST NOT bypass the gate by issuing side effects
directly. CI enforces this via a grep gate
(`.github/workflows/octi-no-gate-bypass.yml`) that flags any
`http.Get`, `os/exec`, `sql.Open`, etc. in `swarm/octi/activities/`
not preceded by a `chitin.GateEval(...)` call within the same
function.

### R8 — Event mirror is a separate spec (041)

The Temporal-→-chitin event mirror (Clawta critique #1) lives in
spec **041-octi-event-mirror-contract**. This spec (040) defines
the hook point only: every workflow start/complete/fail in this
scaffolding emits a structured event to a stub mirror function
`octi.MirrorEvent(ctx, event)` that — in 040 — writes to
`~/.chitin/octi-events-YYYY-MM-DD.jsonl` directly. Spec 041
replaces the stub with the real contract.

### R9 — Worker installer

`swarm/bin/install-octi-cron.sh` installs:
- `~/.octi/worker.toml` (default config)
- `~/.octi/dev.db` (SQLite for `start-dev` mode)
- A systemd user unit (linux) or launchd plist (macOS) named
  `octi-worker` that runs `swarm/bin/octi-worker` and restarts on
  failure
- `octi-worker` enabled but NOT started — operator runs `octi dev
  start` then `systemctl --user start octi-worker` explicitly

Installer is idempotent: re-running rewrites only changed files.

### R10 — HA migration is a separate spec (048)

Spec **048-octi-ha-migration-template** defines the tripwired
migration from `start-dev` (SQLite) to HA (Postgres + Elasticsearch
+ multi-service deployment). Tripwire conditions: >7 concurrent
workflows, OR any customer-facing workflow, OR sustained workflow
latency > 5s. This spec (040) ships only `start-dev`.

## Acceptance criteria

1. `go build ./swarm/octi/...` succeeds on a fresh checkout after
   running `go mod tidy`.
2. `workflowcheck ./swarm/octi/workflows/... ./swarm/octi/internal/...`
   passes with no violations.
3. CI gate `octi-workflowcheck.yml` runs on every PR touching
   `swarm/octi/workflows/` or `swarm/octi/internal/`; demonstrated
   by intentionally introducing `time.Now()` in a draft PR and
   verifying CI fails.
4. `swarm/bin/octi dev start` launches `temporal server start-dev`,
   confirmed by `swarm/bin/octi dev status` reporting the running
   PID.
5. `swarm/bin/octi hello "test"` returns
   `"echoed:test@<ts>:gate=<decision_id>"` where `<decision_id>` is
   present in `~/.chitin/gov-decisions-YYYY-MM-DD.jsonl`.
6. `swarm/bin/octi replay <workflow-id>` for the run from AC5
   completes with exit code 0 and a "replay match" message.
7. e2e test `swarm/octi/e2e/replay_test.go` runs in CI and passes.
8. CI gate `octi-no-gate-bypass.yml` flags any Activity introducing
   a side effect without a preceding `chitin.GateEval(...)` call;
   demonstrated by an intentional violation in a draft PR.
9. Every workflow start/complete/fail emits a record to
   `~/.chitin/octi-events-YYYY-MM-DD.jsonl` via the
   `octi.MirrorEvent` stub (real contract in spec 041).
10. `swarm/bin/install-octi-cron.sh` is idempotent: running it twice
    produces no diff in `~/.octi/` and no error.

## Test coverage

Per chitin spec 020 §1.2: e2e default.

- `swarm/octi/workflows/hello_test.go` — unit: workflow logic in
  isolation, using `testsuite.WorkflowTestSuite`
- `swarm/octi/activities/echo_test.go` — unit: Activity logic with
  mocked chitin-kernel gate
- `swarm/octi/e2e/replay_test.go` — **e2e**: launches dev server,
  submits hello workflow, runs replay, asserts match (AC6 + AC7)
- `swarm/octi/e2e/gate_test.go` — **e2e**: asserts gate decision id
  appears in `gov-decisions-*.jsonl` (AC5)
- `swarm/octi/e2e/workflowcheck_test.go` — **e2e**: spawns a
  subprocess that runs `workflowcheck` against a fixture
  containing `time.Now()` and asserts non-zero exit (AC3)

Every test file carries `// spec: 040-octi-scaffolding` reference
comment in first 20 lines per spec 020 §1.1.

## Invariants

- **I1**: workflow code is deterministic — `workflowcheck` passes on
  every commit to `main`. CI gate non-bypassable.
- **I2**: every workflow Activity that produces a side effect calls
  chitin-kernel gate first. CI gate `octi-no-gate-bypass.yml`
  enforces.
- **I3**: every workflow run is replayable bit-for-bit from
  Temporal event history alone. e2e test `replay_test.go` asserts.
- **I4**: every workflow start/complete/fail is mirrored to a
  chitin-side event store. Spec 041 hardens this; spec 040 ships
  the stub.
- **I5**: Octi never imports chitin kernel internals — only the
  documented gate-evaluation entrypoint. Enforced by grep gate:
  `grep -r 'chitin/internal' swarm/octi/` must return zero matches.
- **I6**: `swarm/octi/` MUST NOT write to `go/`, `apps/`, `libs/`,
  `chitin.yaml`, or primary-checkout paths (constitution §2).

## Out of scope

- Production HA Temporal cluster (Postgres + Elasticsearch) —
  spec 048
- Migration of any production cron / lobster workflow — specs
  043-047
- Event mirror durability contract — spec 041
- agent-bus / Discord identity + dedup contract — spec 042
- Mention-routing workflow with listener ownership — spec 047
- Python / OpenClaw / Mini Activity worker SDKs — handled per
  consuming spec (each migration spec defines its own task-queue
  contract)
- Operator UI for workflow inspection — Temporal Web UI suffices
  for `start-dev`
- Versioning and rollout policy (`workflow.GetVersion`) — handled
  per consuming spec; this spec only seeds the import

## Migration sequencing

After this spec ships, the order of subsequent specs is:

1. **041** — event mirror contract (closes Clawta critique #1)
2. **042** — agent-bus identity contract (closes Clawta critique #2
   and the thread-1-vs-thread-17 routing failure that surfaced
   during the RFP itself)
3. **043** — dispatch workflow (ports `kanban-dispatch.lobster` —
   the single highest-value migration)
4. **044** — poller workflow (replaces `clawta-poller`)
5. **045** — bridge workflow (replaces `hermes-clawta-bridge.py`)
6. **046** — autonomous claim workflow (replaces
   `autonomous-board-engine.sh`)
7. **047** — mention routing (closes Clawta critique #3 +
   subsumes `clawta-mention-listener`, `mini-mention-listener`)
8. **048** — HA migration template (tripwired)

Each consuming spec MUST NOT promote its ticket to `ready` until
this spec ships (status: shipped) and the green CI badges on
`workflowcheck` and `no-gate-bypass` exist on `main`.

## References

- Parent: `.specify/specs/038-octi-persistent-claude-session/spec.md`
- Test-coverage convention: chitin spec 020 §1.2 (e2e default)
- Spec-reference comment convention: spec 020 §1.1
- File-system-scope convention: spec 024 §1.3
- chitin gate evaluation interface: `go/execution-kernel/internal/gov/`
- Temporal Go SDK docs: https://docs.temporal.io/develop/go
- `workflowcheck`: https://pkg.go.dev/go.temporal.io/sdk/contrib/tools/workflowcheck
- Ratification thread: agent-bus thread 17 (msg 7702)
