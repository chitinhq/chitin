# Requirements Checklist — 098 factory webhook trigger surface

Pre-implementation verification gate. Checked items below were satisfied at spec-authoring time. The "Deferred to implementation" section enumerates gates the implementation PR must satisfy.

## Producer-side contract (the listener)

- [x] `chitin-orchestrator factory-listen` subcommand specified (FR-001)
- [x] HMAC-SHA256 verification via `X-Hub-Signature-256` (FR-002)
- [x] Path-based spec detection from `commits[].added` / `commits[].modified` (FR-003)
- [x] `factory_triggered` and `factory_dispatch_failed` chain event types (FR-004)
- [x] Non-main branch filter — only `refs/heads/<main-branch>` triggers (FR-005)
- [x] JSON response shape with `dispatched, spec_refs, run_ids, skipped_reasons` (FR-007)
- [x] Per-request JSONL log at `~/.cache/chitin/factory-listen.jsonl` (FR-008)
- [x] Graceful shutdown via SIGTERM / SIGINT (FR-009)
- [x] No repo mutation — listener only reads payload, dispatches via existing schedule (FR-010)

## Test harness

- [x] `chitin-orchestrator simulate-webhook` subcommand specified (FR-006)
- [x] Signs synthetic payloads with the same secret the listener verifies
- [x] POSTs to localhost; prints listener response

## Constitution

- [x] §1 — kernel is only chain writer: preserved (listener writes via existing emit path)
- [x] §6 — swarm tooling is the exception: code lives under `go/orchestrator/`, not `swarm/`
- [x] §7 — swarm is the orchestrator: load-bearing. The listener IS the missing trigger seam that makes §7's "implementation flows through the orchestrator" automatic instead of operator-attended

## Deferred to implementation

1. **HMAC verification test**: synthetic payload with wrong/missing signature → HTTP 401 + no chain event + no dispatch.
2. **Non-main branch test**: synthetic payload with `ref: refs/heads/feature/x` → HTTP 200 + no dispatch.
3. **Multi-spec push test**: synthetic payload listing two tasks.md adds → two dispatches, two chain events, one HTTP response listing both.
4. **Schedule-failure path test**: synthetic payload with a spec ref that doesn't compile → HTTP 200 with `dispatched:false`, `factory_dispatch_failed` chain event emitted.
5. **Simulate round-trip test**: `factory-listen` running + `simulate-webhook` against it → end-to-end success, including a Temporal workflow being scheduled.
6. **systemd unit**: installer at `swarm/bin/install-chitin-factory-listen.sh` per §4 convention.
7. **Documentation**: `docs/operator/factory-webhook.md` with the GitHub webhook configuration walkthrough (Cloudflare tunnel example), the secret-file setup, and the simulate-webhook recipe.
8. **CHANGELOG**: not added — chitin has no CHANGELOG.md per spec 097's polish phase finding.
