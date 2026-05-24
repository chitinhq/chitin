# Requirements Checklist — 103 Swarm Work Orchestration

Design-stage verification. Items marked `[x]` were satisfied at spec authoring; the "Deferred to implementation" section enumerates gates the impl PRs (PR-A + PR-B + PR-C) must satisfy.

## Empirical grounding

- [x] Spec opens with the actual state (both agents' crons removed 2026-05-23; both idle; chitin now responsible)
- [x] Concrete paths cited (Obsidian vault, gateway ports, kernel governance)
- [x] Architecture pivot named (chitin is scheduler+observer+ingester, not classifier)
- [x] Hermes ecosystem breadth acknowledged (messaging, browser, calendar, content — not just research)
- [x] OpenClaw substrate acknowledged (session graph, broad skill set)

## Open taxonomy discipline

- [x] No closed enum of job classes (explicitly rejected, FR-001/002 framing makes it explicit)
- [x] Schedule entry `message` is literal; no template substitution
- [x] `tag` field is free-form; chitin doesn't validate against any list
- [x] Five "starter recipes" live in operator runbook (deferred to PR-C), not in spec as enumerated types
- [x] New use cases (calendar, customer-triage, vendor-monitoring) require zero spec amendments
- [x] SC-006 explicitly measures open-taxonomy usage (≥5 distinct unforeseen tags in production)

## Architecture constraints

- [x] Security boundary explicit (Hermes MCP + OpenClaw CLI stay local-stdio, FR-008 load-bearing)
- [x] Private surfaces (local SQLite + filesystem; never GitHub Issues; never the OSS repo)
- [x] Kernel governance preserved (both agents already kernel-governed; chain events flow through existing emit)
- [x] No new chain writer (constitution §1 — 3 new event types via existing kernel emit)
- [x] Chitin doesn't write to the vault (read-only relationship)

## Composition with existing specs

- [x] Spec 070 (orchestrator): reuses Temporal worker; no changes
- [x] Spec 075 (driver contract): unchanged — swarm members aren't drivers
- [x] Spec 078 (self-improvement-loop): downstream consumer of queue (US8 stubs structural fields)
- [x] Spec 079 (information-ingestion-pipeline): direct extension — implements TODO'd FetchAndRead
- [x] Spec 080 (orchestrator-ops-completion): same Discord notifier pattern, no conflict
- [x] Spec 081 (cron-migration): schedule layer reuses EnsureSchedules pattern
- [x] Spec 094 (PR review): unchanged — swarm-derived specs flow through dialectic like any other
- [x] Spec 098 (factory webhook): security boundary explicit — webhook receiver does NOT bridge to swarm gateways
- [x] Spec 099 (Copilot driver): orthogonal
- [x] Spec 101 (cost matrix): schedule cadence × agent × model cost-tier composes (future)
- [x] Spec 102 (PR review wiring): unchanged

## Constitution

- [x] §1 kernel-only chain writer: preserved
- [x] §6 swarm tooling exception: code lives under `go/orchestrator/`
- [x] §7 swarm is the orchestrator: load-bearing — chitin becomes the scheduler/observer/ingester for the swarm

## Risk acknowledgment

- [x] Idle period named: ~1 week impl gap during which no research/ecosystem work produced
- [x] Open-taxonomy ambiguity acknowledged (typos / inconsistent tags) — mitigated via operator runbook + cardinality warnings, not solved by code
- [x] Reply correlation is heuristic, not deterministic
- [x] Security boundary is adapter-level — future specs touching gateways must explicitly re-affirm
- [x] Chain-mining adds load to mining agent — fallback to pre-aggregated views if cadence × window grows

## Deferred to implementation

### PR-A (ingestion + queue)

1. **fsnotify robustness**: vault root deleted/recreated (Obsidian sync events) — re-establish watch on root-disappear-then-reappear.
2. **Frontmatter parser**: yaml.v3 vs alternatives; verify Obsidian conventions handled.
3. **Body excerpt length**: 500 chars default; verify operator-readable without bloating rows.
4. **ULID for queue_id**: sortable IDs (chronological by default browse). Use `oklog/ulid` library.
5. **SQLite WAL mode**: enable so watcher + queries don't block.

### PR-B (gateway adapters + schedule layer)

6. **Hermes MCP subprocess lifecycle**: spawn-per-invocation (~500ms-1s overhead) vs long-lived pool. Recommend per-invocation for v1.
7. **OpenClaw CLI session-ID source**: declared in schedule entry's `gateway_session` field OR auto-resolved via `openclaw sessions list`. Decide which is operator-friendlier.
8. **MCP JSON-RPC schema**: verify `messages_send`, `events_wait` arg shapes match Hermes documentation (per ares's response in 2026-05-23 session).
9. **Per-agent auth verification**: assert OpenClaw CLI + Hermes MCP both work end-to-end when invoked by the orchestrator's systemd-service identity (red), not just operator's interactive shell.
10. **Schedule config validation**: reject configs naming unknown agents or unparseable cadence at `ensure-swarm` time, not at first invocation.
11. **Open-tag canonical names**: optional `sentinel pass` that warns on tag-cardinality (≥2 distinct tags used for same agent×cadence pattern in 7 days) — soft governance for the open taxonomy.

### PR-C (sentinel routing + operator surface + recipes + role lock)

12. **Sentinel ingester for swarm-mined-*.json**: extend sentinel's existing finding watcher; verify content-hash dedup so re-running a mining query doesn't create duplicates.
13. **Console interactivity**: REST endpoints (`POST /api/swarm-queue/<id>/discard` etc.) defined alongside the page.
14. **`swarm-ask` partial-finding semantics**: deadline hit with 1 of expected N findings → `partial_success` with what was captured.
15. **Role-split lock**: two weeks of Option A observation; impl PR captures verdict.
16. **Operator runbook**: `docs/operator/swarm-work.md` — schedule declaration, gateway auth, troubleshooting, free-tag conventions.
17. **Recipe library**: `docs/operator/swarm-recipes.md` — the five starter recipes from this spec, expanded with full example schedule entries (including `skills` hint lists) and result-handling examples; encouraged to extend.
18. **Sentinel adapter for swarm chain events**: sentinel learns to display `swarm_invocation` event clustering (which schedules are firing, which agents are most active, error rates per tag).
19. **Skill discovery research**: operator pings ares + clawta directly (via Discord or gateway) and asks each to document its skill-discovery interface — `hermes skills list`? MCP `tools_list`? `openclaw skills list`? Whatever the answer, document in operator runbook so subsequent skills entries in `swarm-schedule.yml` reference real skill identifiers. NOT a chitin-side enforcement — purely operator-onboarding.
20. **Skill-hint prepending unit test**: assert that when `skills: [a, b]` is set, the message sent to the gateway is `"Available skills you may use: [a, b]. <original>"`; when `skills` is empty/unset, message is sent literally with no prefix.
