# Feature Specification: Factory webhook trigger surface

**Feature Branch**: `feat/098-factory-webhook`

**Created**: 2026-05-23

**Status**: Draft

**Input**: User description: "After spec 097 + 094 + 091 v1.1 landed, the orchestrator substrate has every link except the automation trigger. Today an operator runs `chitin-orchestrator schedule <ref>` manually. The full §7 vision — 'operator commits a spec, the factory handles everything else' — needs a trigger surface that listens for repo changes and dispatches automatically. Target end-state: any tracked repo (operator's private repo, chitinhq org repos, organization repos, benchdevs in a different org) commits a `.specify/specs/NNN/tasks.md` → factory dispatches → driver does the work → PR opens → spec 094 reviews it → PR lands. Spec 098 covers the trigger surface only; multi-repo credentials and repo materialization defer to spec 099."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Auto-dispatch on tasks.md commit (Priority: P1)

An operator commits `.specify/specs/098-foo/tasks.md` to chitin's main branch and pushes. Within seconds, a SchedulerWorkflow run for spec 098-foo appears in Temporal — without the operator running `chitin-orchestrator schedule` by hand. The factory listener detected the new spec via a webhook POST and called the schedule subcommand on the operator's behalf.

**Why this priority**: this IS the deliverable. Spec 097 gives operators the manual dispatch surface; spec 098 makes dispatch automatic on commit. Without it, §7's "factory" framing is aspirational — every spec needs operator hands today.

**Independent Test**: with the factory listener running on a known port, POST a synthetic GitHub `push` webhook payload describing a commit that adds `.specify/specs/098-fixture/tasks.md`. Within 5 seconds, assert (a) a new SchedulerWorkflow with `spec_ref="098-fixture"` is running in Temporal, (b) a `factory_triggered` chain event is in `~/.chitin/events-*.jsonl`, (c) the listener's stdout/log shows the receive → detect → dispatch flow.

**Acceptance Scenarios**:

1. **Given** the factory listener is running, **When** a synthetic `push` payload POSTs with `commits[].added` containing `.specify/specs/098-foo/tasks.md`, **Then** the listener calls `chitin-orchestrator schedule 098-foo`, emits a `factory_triggered` chain event, returns HTTP 200 with a JSON body containing the new Temporal RunID.
2. **Given** the listener receives a `push` to a non-main branch, **When** the payload is processed, **Then** the listener returns HTTP 200 with `{"dispatched": false, "reason": "non-main branch"}` — does NOT dispatch.
3. **Given** the payload signature header `X-Hub-Signature-256` does NOT match the configured HMAC secret, **When** the listener processes it, **Then** the listener returns HTTP 401 with `{"error": "invalid signature"}` and emits no chain event.
4. **Given** a payload that touches `.specify/specs/098-foo/spec.md` but NOT `tasks.md`, **When** processed, **Then** the listener returns HTTP 200 with `{"dispatched": false, "reason": "no tasks.md changes"}` — spec-only changes do not dispatch (tasks.md is the load-bearing artifact).
5. **Given** a single push that adds tasks.md for `098-foo` AND `098-bar`, **When** processed, **Then** TWO schedule subcommands fire, two `factory_triggered` events emit, one HTTP response listing both run_ids.

---

### User Story 2 — Simulated webhook for local end-to-end testing (Priority: P1)

A developer (you, me, anyone setting up the factory) wants to demonstrate the trigger surface works WITHOUT setting up a public endpoint, ngrok tunnel, or GitHub webhook configuration. The factory binary exposes a `simulate-webhook` subcommand that constructs a synthetic GitHub `push` payload, signs it with the listener's secret, and POSTs to the local listener. End-to-end demo runs from one terminal in under 10 seconds.

**Why this priority**: tonight's user can't reasonably set up a Cloudflare tunnel + GitHub webhook before validating the dispatch logic works. The simulate subcommand makes the listener self-testable. After it proves itself locally, the user wires real GitHub webhooks at their leisure.

**Independent Test**: run `chitin-orchestrator factory-listen --port 8765 &` then `chitin-orchestrator simulate-webhook --port 8765 --spec-ref 098-foo --branch main`; assert the listener received the POST, verified the signature, dispatched the schedule, and the Temporal workflow exists.

**Acceptance Scenarios**:

1. **Given** a listener on port 8765 with secret `S`, **When** `simulate-webhook --port 8765 --spec-ref 098-foo` runs, **Then** the listener returns HTTP 200, a `factory_triggered` chain event lands, and a SchedulerWorkflow is started.
2. **Given** the listener is NOT running on the target port, **When** simulate runs, **Then** simulate exits non-zero with `error: listener unreachable at <host:port>`.

### Edge Cases

- **Tasks.md modified, not added** — also dispatches. The factory treats any `tasks.md` mutation as a "spec ready to (re)dispatch" signal. Operators who want explicit gating can amend the spec without dispatch by NOT touching tasks.md.
- **Spec dir exists but tasks.md is malformed** — the `chitin-orchestrator schedule` call fails the user-error exit code (1) per spec 097's contract. The listener returns HTTP 200 with `{"dispatched": false, "reason": "schedule failed: <error>"}` and emits a `factory_dispatch_failed` chain event for operator visibility.
- **The same spec's tasks.md is touched multiple times in a short window** — every push dispatches a fresh SchedulerWorkflow run with a new RunID. The factory does not deduplicate; the orchestrator's per-run audit makes parallel runs visible. (A future amendment may add debouncing.)
- **The webhook arrives during a network partition** — GitHub retries delivery automatically (standard webhook behavior). The listener's idempotency is delegated to the orchestrator: re-dispatching the same spec produces a new RunID, never a partial state.
- **The listener restarts mid-flight** — in-flight HTTP requests fail with connection-reset; GitHub retries them. Once running, the listener has no persistent state to recover; every request is processed afresh.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A new `chitin-orchestrator factory-listen [--port <N>] [--secret-file <path>] [--repo-root <path>]` subcommand MUST start an HTTP server on the given port (default 8765) that accepts POSTed GitHub-style `push` webhook payloads and dispatches matching spec changes via the existing `chitin-orchestrator schedule` flow.

- **FR-002**: The listener MUST verify the `X-Hub-Signature-256` HMAC header against a configured secret. The secret is loaded from `--secret-file <path>` (default: `$HOME/.chitin/factory-webhook.secret`). Mismatch returns HTTP 401 and no further processing. Empty / missing secret file is a startup error.

- **FR-003**: The listener MUST process the payload to detect added or modified files matching `.specify/specs/(NNN-[a-z0-9-]+)/tasks.md` across the push's commits. Each unique spec ref MUST dispatch exactly one schedule invocation (per FR-001 / spec 097 contract).

- **FR-004**: The listener MUST emit a `factory_triggered` chain event per dispatched spec with payload `{spec_ref, run_id, source: "github_webhook" | "simulated", branch, commit_sha, ts}`. Schedule failures emit `factory_dispatch_failed` with `{spec_ref, error, ts}` (per Edge Cases).

- **FR-005**: The listener MUST only dispatch for pushes to the main branch (configurable via `--main-branch <name>`, default `main`). Pushes to other branches return HTTP 200 with `{"dispatched": false, "reason": "non-main branch"}` and no chain event.

- **FR-006**: A new `chitin-orchestrator simulate-webhook [--port <N>] [--spec-ref <ref>] [--branch <name>] [--secret-file <path>]` subcommand MUST construct a synthetic GitHub push payload describing one tasks.md addition for the given spec ref, sign it with the loaded secret, POST it to `http://127.0.0.1:<port>/webhook/push`, and print the response JSON. Used for local end-to-end testing.

- **FR-007**: The listener's HTTP response MUST be a JSON object: `{"dispatched": bool, "spec_refs": [<ref>...], "run_ids": [<uuid>...], "skipped_reasons": [...]}` so an operator (or a `gh pr comment`-style integration) can render a meaningful status from a single response.

- **FR-008**: The listener MUST log every received request to `~/.cache/chitin/factory-listen.jsonl` (one JSON line per request: timestamp, signature-verified bool, branch, commits processed, dispatch results). Log path overridable via `$CHITIN_FACTORY_LOG`.

- **FR-009**: The listener MUST honor a SIGTERM / SIGINT for graceful shutdown — finishes in-flight requests, then exits 0. Suitable for systemd-managed restart.

- **FR-010**: The listener MUST NOT clone, fetch, or modify any repo state. Repo materialization is spec 099's scope. The listener only DETECTS spec changes from the webhook payload's `commits[].added` / `commits[].modified` lists and dispatches via the schedule subcommand against the operator's existing local checkout (via `--repo-root` flag, default `$CHITIN_REPO_ROOT` or `git rev-parse --show-toplevel` from cwd).

### Key Entities

- **GitHub push webhook payload** — the JSON body GitHub POSTs on `push` events. Subset of fields this spec consumes: `ref` (e.g., `refs/heads/main`), `after` (the new commit sha), `commits[]` (each carrying `added` and `modified` string lists). Full schema: <https://docs.github.com/en/webhooks/webhook-events-and-payloads#push>.

- **Factory webhook secret** — a shared HMAC key on disk at `$HOME/.chitin/factory-webhook.secret` (mode 0600). Used by both the listener (verify) and the simulate-webhook subcommand (sign). Operator generates with `openssl rand -hex 32 > ~/.chitin/factory-webhook.secret`.

- **`factory_triggered` chain event** — one per successful dispatch. Payload schema in `contracts/chain-events.md`.

- **`factory_dispatch_failed` chain event** — one per schedule-invocation failure inside the listener. Payload schema in `contracts/chain-events.md`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With the listener running and a real `.specify/specs/098-fixture/tasks.md` on disk, a `simulate-webhook --spec-ref 098-fixture` invocation produces a started SchedulerWorkflow within 5 seconds of the simulate command exiting. Verified by `chitin-orchestrator status` finding the new run_id.

- **SC-002**: HMAC verification rejects 100% of payloads with a wrong/missing signature (HTTP 401, no chain event, no dispatch). Verified by a test that POSTs unsigned and wrong-signed payloads.

- **SC-003**: A push to a non-main branch is ignored 100% of the time (HTTP 200, no dispatch). Verified by a test with `ref: refs/heads/feature/x`.

- **SC-004**: After this spec ships and the operator wires a real GitHub webhook + Cloudflare tunnel (or equivalent), committing a spec to chitin's main produces a Temporal workflow run without operator hands. Measured by chain query: every `scheduler_started` event from `main` post-deploy has a corresponding `factory_triggered` event preceding it.

- **SC-005**: The listener serves at least 100 webhook requests/minute without dropping (basic load resilience — the schedule call is the slow leg, not the receive leg). Verified by a small benchmark wrapping `simulate-webhook` in a loop.

## Assumptions

- The orchestrator's `chitin-orchestrator schedule` subcommand is on `main` (specs 097 + #945). Without #945's TargetRepo/BaseRef population, the dispatch chain fails at CreateWorktree — spec 098 inherits that fix.
- The operator's Temporal cluster (the dispatch target) is reachable from the listener. Today: same host. Multi-host: out of scope; spec 099.
- Repo materialization is spec 099's scope. Spec 098 dispatches against an existing local checkout at `--repo-root`. Multi-repo with auto-clone is the next link.
- GitHub webhook delivery is at-least-once with retries; the listener delegates idempotency to the orchestrator (each dispatch is a fresh RunID). Future amendment may add debouncing.
- The listener is local-only by default (binds to `127.0.0.1`). Production deployment with a public endpoint requires either a tunnel (Cloudflare / ngrok) or a reverse proxy with TLS — operator's responsibility, not the listener's.

### Scope

**In scope**:

- `chitin-orchestrator factory-listen` HTTP receiver subcommand
- `chitin-orchestrator simulate-webhook` test harness subcommand
- HMAC-SHA256 verification of the `X-Hub-Signature-256` header
- `factory_triggered` and `factory_dispatch_failed` chain event types
- Path-based spec detection from `commits[].added` / `commits[].modified`
- Non-main branch filtering
- Per-request JSONL log at `~/.cache/chitin/factory-listen.jsonl`
- A systemd user unit for the listener (operator-installable via existing `swarm/bin/install-*.sh` pattern)

**Out of scope**:

- Multi-repo support (different repos beyond chitin) → spec 099
- Repo materialization (cloning remote repos for work units to operate on) → spec 099
- Credentials for cross-org access (GitHub App, PAT management) → spec 099
- TLS termination / public endpoint setup → operator-side infrastructure
- Debouncing repeated pushes to the same spec → future amendment if needed
- Cancel / retry semantics on already-running workflows for the same spec → future amendment

### Dependencies

- **Spec 097 (operator entrypoint)**: the listener calls `chitin-orchestrator schedule`. PR #945 must merge first (TargetRepo + BaseRef fix).
- **Spec 094 (PR review)**: not a direct dependency, but completes the closed loop — once the listener dispatches a spec, the driver opens a PR, and spec 094's PRReviewWorkflow auto-reviews it.
- **Spec 070 / 075 / 076 (orchestrator + drivers + scheduler)**: substrate. Already on main.
- **Constitution §7**: spec 098 makes §7's "implementation MUST flow through the orchestrator" enforceable at the trigger seam, not just the dispatch seam. After 098 ships, even ad-hoc spec drops automatically enter the orchestrator.
