# Feature Specification: Swarm Work Orchestration

**Feature Branch**: `spec/103-swarm-research-orchestration`

**Created**: 2026-05-23

**Status**: Draft

**Input**: User description: "Two AI assistants live alongside chitin in the operator's swarm: **ares** (Hermes gateway, codex-backed; a general-purpose worker with broad ecosystem reach across Discord/Slack/Telegram/WhatsApp/Gmail, browser, calendar, content pipelines, dev workflows, business operations) and **clawta** (OpenClaw gateway, GLM-5.1-via-Ollama-Cloud-backed; a session-graph substrate with its own skill set). Both are governed by the chitin kernel — every tool call already lands in the chain. Until 2026-05-23 each ran on its own cron; the operator removed those crons in-session and delegated 'who runs what, when' to chitin. As of this spec's authoring, both agents are idle. The chitin factory needs to evolve from 'implementation orchestrator' to **scheduler + observer + ingester for the swarm**. Critical framing: chitin does NOT classify swarm work. Ares and clawta are capable peers with ecosystems far broader than chitin can usefully enumerate. Chitin's three jobs for the swarm are (1) **schedule** — fire (agent, cadence, message) tuples through their gateways, (2) **observe** — kernel chain captures every tool call; sentinel + new `swarm-summary` aggregation surface that activity, and (3) **ingest** — when agents write to known paths (Obsidian vault for research findings), chitin picks it up. Chitin does NOT enumerate job classes, hard-code invocation templates, or constrain what messages can be sent. The schedule entry's `message` is the literal prompt; the operator (or a future swarm-author) decides what to ask for. Three load-bearing architecture constraints: (1) Security boundary — Hermes MCP and OpenClaw CLI stay local-stdio. No tunneling, no exposure through factory-listen's HTTP receiver. The swarm-work layer never leaves chimera-ant; only the dispatch trigger (spec→PR loop) crosses the network boundary. (2) Private surfaces — research findings + chain-mining outputs stay off the OSS repo. Local SQLite + filesystem only. (3) Open by design — schedule entries are free-form; chitin doesn't validate the message against a taxonomy. New use cases (calendar, browser work, customer triage) work without spec amendments."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Free-form scheduled invocation (Priority: P1)

As the operator, I declare schedule entries in `~/.chitin/swarm-schedule.yml` of the form `(id, agent, cadence, message, tag?)`. Chitin fires the named agent's gateway on cadence with the literal message; no template substitution, no class validation. The optional `tag` is a free-text string the operator uses for filtering/aggregation later. Recognized starter recipes (research-scan, ecosystem-work, chain-mine, etc.) live in the operator runbook as examples, not as a closed enum.

**Why this priority**: This is the foundational substrate. Without free-form scheduling, every other story collapses into a closed taxonomy.

**Independent Test**: Declare three schedules — `every 6h: ares "Scan arXiv for AI agent governance" tag:research`, `every 12h: ares "Triage Discord backlog in #ops" tag:ecosystem`, `every 24h: clawta "Mine ~/.chitin/events from the last 24h for failure clusters" tag:chain-mine`. All three create Temporal Schedules on `ensure-swarm`. All three fire on cadence. Chain emits one `swarm_invocation` per firing with the tag preserved.

---

### User Story 2 — Passive vault ingestion (Priority: P1)

When ares or clawta writes a new file under `Research/<TOPIC>/sources/` or updates `Research/<TOPIC>/index.md`, chitin detects within 60s, extracts frontmatter + body, and writes a row to `~/.chitin/swarm-results.db`. Ingestion is path-pattern driven, not tag-driven — any file matching a configured glob triggers the row.

**Why this priority**: The vault is the primary output channel for research-class work. Without ingestion, findings exist only on disk and never reach the operator's triage queue.

**Independent Test**: Touch `Research/Test/sources/2026-05-23-fake.md` with valid frontmatter. Within 60s, the queue contains a row with `source=obsidian-vault`, `topic=Test`, `path=Research/Test/sources/2026-05-23-fake.md`, `status=unprocessed`.

---

### User Story 3 — Scheduled invocation through gateways (Priority: P1)

Each schedule fires via Temporal Schedules (spec 081 pattern). Chitin picks the gateway from the agent identity (ares→hermes-mcp, clawta→openclaw-cli), calls the appropriate adapter, and emits a `swarm_invocation` chain event. Schedules survive orchestrator restart.

**Why this priority**: Without gateway dispatch, US1's schedule declarations are inert config. This is what makes the agents actually run.

**Independent Test**: Set `every 5m: clawta "echo hello"`. Within 5m the OpenClaw gateway receives the message via `openclaw sessions send`. Chain records `swarm_invocation`. Restart orchestrator; next 5m boundary still fires.

---

### User Story 4 — Observability without ingestion (Priority: P2)

When ares or clawta does **ecosystem work** (Discord triage, calendar updates, browser automation — anything that produces side effects in its own environment but no vault file), chitin observes the agent's tool calls via the kernel chain (already captured). `chitin-orchestrator swarm-summary [--agent A] [--tag T] [--days N]` aggregates the chain for an operator-readable summary: count of invocations, distribution of tool calls, error rate, last-success timestamp. No separate ingestion needed.

**Why this priority**: Ecosystem work is the broadest category and produces no queue rows. Without `swarm-summary`, the operator can't tell whether ecosystem schedules are doing anything.

**Independent Test**: Schedule `every 12h: ares "Triage Discord backlog" tag:ecosystem`. After two firings, `chitin-orchestrator swarm-summary --agent ares --tag ecosystem --days 1` shows 2 invocations + the tool-call breakdown from the chain.

---

### User Story 5 — Chain-mining results route into sentinel (Priority: P2)

Chain-mining is **just another scheduled invocation** (no special job class), but the **result routing** is special: when ares or clawta produces output for a chain-mining query, the result lands in **sentinel's existing finding surface** (typed as `source=swarm-mined`) rather than the swarm-results queue. Operators see swarm-mined findings next to deterministic sentinel findings without two surfaces to monitor.

Result routing is determined by the agent's output, not by chitin's classification. If the agent writes a file matching `~/.chitin/sentinel-findings/swarm-mined-*.json`, the sentinel ingester picks it up. If it writes to the vault instead, the vault ingester picks it up. Both paths are operator-configurable.

**Why this priority**: Two finding surfaces (sentinel + swarm-results) splits operator attention. P2 because the v1 fallback (vault ingestion) still surfaces mining output, just on the less-appropriate queue.

**Independent Test**: Schedule a chain-mine job; the agent writes a finding to `~/.chitin/sentinel-findings/swarm-mined-<id>.json`; within 60s sentinel's finding surface shows it tagged `source=swarm-mined`.

---

### User Story 6 — Operator triage surface (Priority: P2)

`chitin-orchestrator swarm-queue list [--tag T] [--status S] [--topic T] [--agent A] [--limit N]` — terminal. Console page `/swarm-queue` mirrors. Per-item actions: `mark spec-drafted REF`, `discard`, `defer`. State transitions emit chain events. Filterable by free-text tag so the operator can slice by recipe (research, ecosystem, chain-mine, whatever they declared).

**Why this priority**: US2 lands rows in the DB; without triage, they accumulate without action. P2 because raw SQLite queries work as a fallback.

**Independent Test**: With 3 research findings + 2 ecosystem entries + 1 chain-mine result in the queue, `swarm-queue list --tag chain-mine` returns the 2 chain-mining rows.

---

### User Story 7 — On-demand swarm-ask (Priority: P3)

`chitin-orchestrator swarm-ask <agent> --message "..." [--deadline 30m] [--wait-for-reply]` invokes the agent through its gateway. If `--wait-for-reply`, waits up to deadline for a direct response (via Hermes `events_wait` for ares, polling for clawta). Otherwise fire-and-forget; ingestion catches whatever lands in known paths.

**Why this priority**: Scheduled invocations cover the periodic case. Ad-hoc operator questions are a convenience layer.

**Independent Test**: `chitin-orchestrator swarm-ask ares --message "summarize last week's research in AI Agent Governance" --deadline 30m --wait-for-reply` returns the synchronous response within deadline.

---

### User Story 8 — Reply correlation + heuristic stubs (Priority: P3)

Each queue/finding record carries `triggered_by_chain_event` (the `swarm_invocation` event ID, if applicable) so sentinel can trace `schedule → invocation → result → spec → PR → merge` end-to-end. Each finding also carries structural fields (`confidence_signal`, `novelty_signal`, `affects_core_infra`, `estimated_loc_range`) that future auto-spec-authoring heuristics can read.

**Why this priority**: Correlation is observability polish; the v1 surfaces still work without it.

**Independent Test**: A scheduled invocation fires; a vault finding lands 23m later; the queue row's `triggered_by_chain_event` is the invocation's chain ID.

### Edge Cases

- **Vault file deleted (operator cleanup):** queue entry's status auto-updates to `source_deleted`. Operator can still discard/reference.
- **Schedule fires but gateway down:** activity fails with `gateway_unreachable`; chain emits `swarm_invocation_failed`; Temporal retries per policy. Operator sees the gap in `swarm-summary`.
- **`hermes mcp serve` subprocess hangs:** activity timeout (5m default) → SIGKILL → `swarm_invocation_timeout` chain event. No leaked subprocess.
- **Agent ignores the message:** chain shows the invocation event + a brief tool-call burst then idle. `swarm-summary` notes low engagement. Operator can rewrite the message OR change the agent.
- **Agent produces output in an unexpected path:** chitin doesn't ingest it (we only watch declared sources). Operator either updates `ingestion-sources.yml` or accepts that some outputs live only in the chain.
- **Multiple schedules collide on same agent same minute:** both run in parallel; sessions are concurrent-safe.
- **Operator's free-form tag has a typo (`reseach-scan` instead of `research-scan`):** queue still accepts; filtering by tag won't match. Operator notices on triage; corrects the schedule entry; future invocations use the corrected tag.

## Requirements *(mandatory)*

### Functional Requirements

#### Schedule layer (US1, US3)

- **FR-001** `~/.chitin/swarm-schedule.yml` declares schedule entries. Per entry:
  ```yaml
  schedules:
    - id: ares-arxiv-scan
      agent: ares
      cadence: 6h
      skills: [web-search, arxiv-fetcher, obsidian-vault]   # optional, free-form
      message: "Scan arXiv for new AI agent governance papers. Write per-source files to Research/AI Agent Governance/sources/."
      tag: research-scan          # optional, free-form
      gateway_override: hermes-mcp  # optional, defaults from agent identity
      gateway_session: ares-default # optional, names the agent's session on the gateway; auto-resolved if omitted

    - id: clawta-discord-summary
      agent: clawta
      cadence: 24h
      skills: [discord-read, discord-post]
      message: "Summarize today's #ops channel activity and post the summary to #operator-digest."
      tag: ecosystem
      gateway_session: clawta-ops

    - id: clawta-chain-mine-failures
      agent: clawta
      cadence: 24h
      skills: [filesystem-read, jsonl-parse]
      message: "Read ~/.chitin/events from the last 24h. Identify patterns in WorkUnitWorkflow failures by capability. Write findings to ~/.chitin/sentinel-findings/swarm-mined-{date}.json."
      tag: chain-mine
  ```
- **FR-002** No taxonomy enforcement on `message`, `tag`, or `skills`. Chitin sends the literal `message` to the agent's gateway. Validation is structural only: required fields present, cadence parseable, agent known to the registry. `skills` is a free-form hint list — chitin does NOT verify the named skills exist on the agent (the agent decides at runtime what to use).
- **FR-003** `chitin-orchestrator schedules ensure-swarm` reads the config and creates/updates Temporal Schedules. Idempotent. Reuses spec 081 `EnsureSchedules` pattern. Removing an entry from config + re-running deletes the corresponding Schedule.
- **FR-004** Each scheduled invocation emits a `swarm_invocation` chain event: `{schedule_id, agent, gateway, cadence, message, tag, ts, temporal_run_id}`.
- **FR-005** Schedule executions survive orchestrator restart (Temporal-backed; durable per spec 081).

#### Gateway adapters (US3, US7)

- **FR-006** OpenClaw CLI adapter shells out: `exec("openclaw", "sessions", "send", "--session", <s>, "--message", <m>)`. Activity name: `SwarmSendOpenClaw`. Returns stdout/stderr/exit code. If the schedule entry's `skills` field is non-empty, the adapter prepends the message: `"Available skills you may use: [<comma-separated skills>]. <original message>"`. No validation of skill names — pure hint.
- **FR-007** Hermes MCP adapter spawns `hermes mcp serve` as a child process (stdio-attached). Activity name: `SwarmSendHermes`. JSON-RPC over stdio, calls `messages_send` with (channel, message). If `wait_for_reply: true`, calls `events_wait` with the activity's remaining timeout. Same `skills`-prepending behavior as FR-006.
- **FR-008** **Security boundary** (load-bearing): both adapters MUST NOT expose the gateway over HTTP, tunnel, or webhook. Adapter-level enforcement: no listen-port code, no remote-callable wrapper. New code that violates this is rejected at code-review.
- **FR-009** Per-(agent, gateway) auth handled by adapter. Hermes MCP: `HERMES_HOME=/home/red/.hermes` env-pinned. OpenClaw: relies on operator's existing CLI auth. Per-agent session lookup: declared in `swarm-schedule.yml` entry's `gateway_session` field (FR-001), OR auto-resolved via gateway's `sessions list` command when the field is omitted.

#### Vault ingestion (US2)

- **FR-010** `~/.chitin/ingestion-sources.yml` declares filesystem sources. Per source: `name`, `type`, `root`, `patterns` (glob), `watch: bool`, `extract` (frontmatter mapping), `tag_default` (optional).
- **FR-011** `IngestionWorkflow` (spec 079) consumes config. Per `watch: true` source, registers fsnotify watch. On `IN_CREATE` / `IN_MODIFY`, the `FetchAndRead` activity reads the file, parses frontmatter (yaml.v3), writes a row to `findings` table.
- **FR-012** Debounce duplicate events: file modified twice within 5s emits only the latest. Prevents Obsidian autosave churn.

#### Sentinel-routed findings (US5)

- **FR-013** Sentinel's existing finding surface (already on main) is extended with a new source type: `source=swarm-mined`. Sentinel ingester watches `~/.chitin/sentinel-findings/swarm-mined-*.json` and surfaces matched findings.
- **FR-014** Swarm-mined finding JSON schema:
  ```json
  {
    "id": "ulid",
    "source": "swarm-mined",
    "agent": "ares|clawta",
    "originated_from_chain_event": "<swarm_invocation event id>",
    "ts": "RFC3339",
    "question": "string — the original mining question",
    "window": "iso8601 duration — how far back the agent looked",
    "pattern": "string — what the agent found",
    "supporting_event_ids": ["..."],
    "confidence": 0.0-1.0
  }
  ```
- **FR-015** The agent's mining output ROUTE is the agent's choice (where it writes the file). Chitin doesn't dictate it. The recommended path (`~/.chitin/sentinel-findings/swarm-mined-*.json`) is convention; sentinel's watcher follows the convention.

#### Queue layer (US2, US6)

- **FR-016** Local SQLite at `~/.chitin/swarm-results.db`. Schema (v1):
  ```sql
  CREATE TABLE findings (
    queue_id TEXT PRIMARY KEY,                -- ULID
    ts TEXT NOT NULL,
    source TEXT NOT NULL,                     -- e.g. 'obsidian-vault', 'ares-direct'
    agent_attribution TEXT,                   -- 'ares' | 'clawta' | NULL
    tag TEXT,                                 -- free-form from schedule entry, NULL if ad-hoc
    topic TEXT,                               -- best-effort from path or frontmatter
    file_path TEXT,
    frontmatter_json TEXT,
    body_excerpt TEXT,
    status TEXT NOT NULL,                     -- 'unprocessed' | 'spec_drafted' | 'discarded' | 'deferred' | 'source_deleted'
    spec_drafted_ref TEXT,
    triggered_by_chain_event TEXT,
    confidence_signal REAL,
    novelty_signal TEXT,
    affects_core_infra INTEGER DEFAULT 0,
    estimated_loc_range TEXT,
    notes TEXT
  );
  CREATE INDEX findings_status_tag ON findings(status, tag);
  CREATE INDEX findings_ts ON findings(ts);
  ```
- **FR-017** Queue stays **private**: `~/.chitin/`. Never committed. Console exposure goes through Tailscale auth (v1 accepts).
- **FR-018** New chain event types (all via existing kernel emit): `swarm_invocation`, `swarm_finding_queued`, `swarm_finding_triaged`.

#### Operator surfaces (US4, US6)

- **FR-019** `chitin-orchestrator swarm-queue list [--tag T] [--status S] [--topic T] [--agent A] [--limit N]` — terminal queue listing
- **FR-020** `chitin-orchestrator swarm-queue show <queue_id>` — full record + linked chain events
- **FR-021** `chitin-orchestrator swarm-queue mark <queue_id> {spec-drafted REF | discarded | deferred}` — state transition + chain event
- **FR-022** `chitin-orchestrator swarm-summary [--agent A] [--tag T] [--days N]` — chain aggregation: invocations × tool calls × errors × last-success per (agent, tag, day window). Surfaces ecosystem-work that doesn't produce queue entries.
- **FR-023** Console page `/swarm-queue` mirrors FR-019/020/021 with filter chips for `tag`

#### On-demand (US7)

- **FR-024** `chitin-orchestrator swarm-ask <agent> --message "..." [--deadline 30m] [--wait-for-reply] [--tag T]`
- **FR-025** Internally starts `SwarmAskWorkflow` calling the appropriate gateway adapter. If `--wait-for-reply`, blocks up to deadline. Ingests any vault-side finding produced during the deadline window.
- **FR-026** Workflow timeout = `--deadline`. On timeout: `result: timeout, captured: [...]` — never indefinite.

### Key Entities

- **Schedule entry** — operator-declared `(id, agent, cadence, message, tag?, skills?, gateway_override?, gateway_session?)` tuple in `swarm-schedule.yml`. Drives a Temporal Schedule.
- **Swarm invocation** — chain event recorded each time a schedule fires; primary handle for correlating downstream findings to triggers.
- **Finding** — row in `~/.chitin/swarm-results.db.findings`; either ingested from a vault path or routed via the agent's explicit write.
- **Swarm-mined finding** — JSON file at `~/.chitin/sentinel-findings/swarm-mined-*.json`; surfaces alongside sentinel's deterministic findings.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001** Vault ingestion latency: new file in `Research/<TOPIC>/sources/` → queue row within 60s p99 across 100 sequential touches.
- **SC-002** Schedule resilience: 50 orchestrator restarts over a week leave every declared schedule still firing on cadence.
- **SC-003** Security: external port scan from any tailnet device shows no Hermes MCP / OpenClaw gateway port reachable from outside chimera-ant.
- **SC-004** Operator triage round-trip: queue entry → operator action median < 1 day across first 100 entries.
- **SC-005** Sentinel correlation: ≥ 90% of spec PRs whose spec derives from a swarm finding can be traced back through `swarm_finding_queued` → `swarm_invocation` → schedule.
- **SC-006** Open-taxonomy validation: at least 5 operator-declared schedules in production with **distinct tag values not foreseen at spec-authoring time** (`calendar`, `customer-triage`, `competitive-intel`, `vendor-monitoring`, anything). Proves the open shape is being used.
- **SC-007** Ecosystem-work observability: `swarm-summary` for ecosystem-tagged schedules surfaces meaningful tool-call breakdown ≥ 80% of invocations (i.e., agents are doing real work the chain captures, not just acknowledging the prompt and stopping).

## Assumptions

- Hermes MCP protocol matches the standard MCP spec (JSON-RPC over stdio).
- OpenClaw CLI exit codes are stable (0 = success, non-zero = retry).
- Operator's Obsidian vault stays at `/home/red/Documents/Obsidian Vault/`.
- Both ares and clawta are kernel-governed (operator-confirmed).
- Temporal worker has activity slots for scheduled + on-demand invocations concurrently.

### Scope

#### In scope

- Free-form schedule entries (no closed job-class taxonomy)
- Gateway adapters with security-boundary enforcement (stdio only, no remote)
- Filesystem ingestion of `Research/` vault
- Local SQLite swarm-results queue + operator triage CLI + console page
- Swarm-mined findings routing into sentinel's existing finding surface
- Chain aggregation surface (`swarm-summary`) for ecosystem-work observability
- On-demand `swarm-ask` subcommand
- Chain event types: `swarm_invocation`, `swarm_finding_queued`, `swarm_finding_triaged`
- Starter recipes documented in operator runbook (PR-C deliverable)

#### Out of scope

- **Auto-spec-authoring from queue findings.** Spec 078 territory; this spec stubs the structural fields (US8).
- **Cross-machine swarm members.** Security boundary keeps swarm on chimera-ant.
- **Reply correlation across agents.** If ares triggers and clawta synthesizes, each emits its own queue record; v1 doesn't auto-link.
- **Vault writes from chitin.** Chitin reads the vault; only agents write.
- **Closed taxonomy of job classes.** Explicitly rejected — see Input framing.
- **Templating the message.** The schedule entry's `message` is the literal prompt; no variable substitution, no jinja, no overrides.

### Dependencies

- Spec 070 — orchestrator substrate
- Spec 075 — chain event emission
- Spec 078 — auto-spec-authoring (stubs structural fields only; deferred)
- Spec 079 — IngestionWorkflow
- Spec 080 — sentinel finding surface
- Spec 081 — Temporal Schedules + EnsureSchedules pattern

## Risks

### Idle period (load-bearing)

Until PR-A + PR-B land (~1 week impl), ares + clawta produce nothing scheduled. Operator can manually invoke via Discord during the gap.

### Open-taxonomy ambiguity at observability time

With no closed enum, two operators might tag the same kind of work differently (`research`, `research-scan`, `external-research`). Aggregation by tag breaks. **Mitigation**: operator runbook documents canonical tag names; sentinel could surface tag-cardinality warnings (>1 tag for same agent×cadence pattern). Not solved by the spec — accepted as a cost of openness.

### Security boundary is adapter-enforced, not network-enforced

Future specs that touch the gateway path must explicitly re-affirm FR-008. The boundary is not a separate firewall layer.

### Reply correlation is best-effort

Time-window + topic heuristic. Some findings appear without matched triggers (operator-initiated); some triggers produce no findings (agent declined / nothing new). Sentinel surfaces both for operator review.

### Chain-mining adds load

The chain is ~5,116 event files today and growing. Each mining invocation reads a window (typically last 24h ~ several MB). If cadence × window grows, pre-aggregate into a faster view (sentinel job, not swarm job).

## Starter Recipes (operator runbook, NOT closed taxonomy)

These belong in `docs/operator/swarm-recipes.md` (deferred to PR-C), not in the spec as enumerated types. Listed here as reference for impl PRs:

| Recipe tag | Typical skills | Example message | Result destination |
|---|---|---|---|
| `research-scan` | web-search, arxiv-fetcher, obsidian-vault | "Scan arXiv for new <topic> papers; write per-source files to `Research/<TOPIC>/sources/`." | vault → queue (ingestion) |
| `research-synthesize` | obsidian-vault, file-read, file-write | "Re-read all sources under `Research/<TOPIC>/sources/`; refresh `index.md` takeaways; draft spec proposals into `~/.chitin/proposals/`." | vault + queue |
| `ecosystem-work` | discord-read, discord-post, gmail, browser, calendar | "Triage Discord backlog in #ops"; "Garbage-collect stale OpenClaw sessions"; "Send the daily digest." | agent's native ecosystem (observable via chain only) |
| `chain-mine` | filesystem-read, jsonl-parse, file-write | "Read `~/.chitin/events` from last 24h; identify patterns in <thing>; write findings to `~/.chitin/sentinel-findings/swarm-mined-{date}.json`." | sentinel finding surface |
| `customer-triage` | gmail, email-categorize, file-write | "Read new emails in <inbox>; categorize as bug/feature/question; respond to questions; flag bugs into the queue." | mixed (email replies + queue) |
| `competitive-intel` | web-search, github-api, obsidian-vault | "Check competitor X's GitHub releases since last week; summarize material changes." | vault or direct reply |

Skill names in this table are illustrative — actual skill identifiers depend on what Hermes and OpenClaw expose. Operator confirms via the agents' own skill-discovery interfaces (TBD at impl time — see PR-C deferred item 19).

Operator extends this freely. Chitin's job is to fire the schedule and observe; the recipes are documentation, not code.

## Role-Split Alternatives (DEFERRED to impl PR)

Per-agent assignment is unresolved. Schedule config (FR-001) makes the assignment a config change, not code.

### Option A — ares scout/messenger, clawta synthesizer/miner/heavy-cognition

| Agent | Most-typical schedules |
|---|---|
| ares | research-scan, ecosystem-work (Discord, email, calendar — Hermes's MCP-rich messaging surfaces) |
| clawta | research-synthesize, chain-mine, ad-hoc heavy-reasoning queries (GLM 5.1 is the frontier model) |

### Option B — ares-primary, clawta-backup

ares handles everything by default (Hermes's ecosystem breadth covers it); clawta is invoked only for heavy reasoning that ares hands off to it.

### Option C — fully open, operator-declared per schedule

Every schedule entry names the agent explicitly. No defaults, no fallbacks. Operator chooses per-recipe based on what they observe works best.

Impl PR observes both agents under Option A for two weeks; locks A/B/C based on quality + cost.

## Notes for Implementation Phase

**Implementation deferred** — design-only. Recommended sequence as 3 follow-up PRs:

### PR-A (ingestion + queue, no schedules yet)

1. Implement `IngestionWorkflow.FetchAndRead` for filesystem source (FR-011)
2. SQLite schema + write activity (FR-016, FR-018)
3. `chitin-orchestrator swarm-queue list/show/mark` (FR-019/020/021)
4. Test by manual file creation in `Research/Test/`; observe queue
5. Console page deferred to PR-C

### PR-B (gateway adapters + schedule layer)

6. OpenClaw CLI adapter (FR-006)
7. Hermes MCP stdio adapter (FR-007) — heavier
8. Schedule config + `ensure-swarm` (FR-001, FR-003)
9. Wire schedules; agents start producing again
10. Chain events for invocations (FR-004, FR-018)
11. `swarm-summary` aggregation (FR-022)

### PR-C (sentinel routing + on-demand + operator surface + recipes)

12. Sentinel ingester for `swarm-mined-*.json` (FR-013/014/015)
13. Console `/swarm-queue` page (FR-023)
14. `swarm-ask` subcommand + workflow (FR-024/025/026)
15. Operator runbook `docs/operator/swarm-work.md` + recipe library `docs/operator/swarm-recipes.md`
16. Lock role-split (Option A/B/C) based on observed two-week run

After PR-C: the swarm-work loop closes. ares + clawta run on operator-declared schedules; their outputs flow to the queue (for vault findings), sentinel (for chain-mining), or their own ecosystems (for native work observable via chain); operator triages from one surface filtered by free-form tags; new use cases require only a config edit, never a spec amendment.

## Metadata

- **spec_id**: 103
- **owner**: chitinhq
- **related**: 094, 098, 099, 101, 102
