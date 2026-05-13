# Argus — Chain Observatory + Agent Memory Researcher

Date: 2026-05-12
Status: spec
Author: red + Claude (collaborative)

## Goal

Three-agent operator architecture, completing the topology beside Hermes
and Clawta:

- **Hermes** — orchestrator, kanban groomer, operator-facing chat
- **Clawta** — dispatch executor, routing brain, swarm narrator
- **Argus** — observatory, continuous chain + memory researcher,
  report producer

Argus is **read-only**. Never mutates kanban, never executes tools, never
makes routing decisions. It sees every operator-visible surface
(chitin chain ledger, kanban, git history, hermes/openclaw logs,
Discord narration, agent memory stores), indexes them locally on the
RTX 3090 box, runs continuous detectors + on-demand analyses via
qwen3.6:27b through ollama, and emits structured reports for operator
review.

The operator currently surfaces these insights manually — Argus moves
that work from once-per-session bursts to 24/7 background research.

## Target architecture

```
                                  OPERATOR (red)
                                       │
              ┌────────────────────────┼─────────────────────────┐
              │ primary chat           │ adversarial            │ ad-hoc query
              ▼                        │  read, specs            ▼
          HERMES                       │                       ARGUS
          (Ares bot)                   │                       (chain researcher)
          orchestrator                 │                       observatory
              │                        │                          ▲
              │ kanban writes          │                          │ reads everything
              │ + grooming             │                          │
              ▼                        │                          │
   ┌─────────────────────────────────────────────────────────────┴───────────┐
   │                  SHARED SOURCES (all read by Argus)                     │
   ├─────────────────────────────────────────────────────────────────────────┤
   │ chitin chain ledger     ~/.chitin/gov-decisions-*.jsonl                 │
   │ kanban DB               ~/.hermes/kanban/boards/<project>/kanban.db     │
   │ hermes logs             ~/.hermes/logs/                                 │
   │ openclaw logs           ~/.openclaw/logs/                               │
   │ openclaw agent data     ~/.openclaw/data/{agents,agent-cards,clawta.db} │
   │ hermes memory           ~/.hermes/memory/  (TBD path; agent/memory_mgr) │
   │ git history             repo-local; `git log`, `gh pr/run/issue`        │
   │ discord channels        via openclaw discord plugin transcript export   │
   │ wiki + knowledge graphs ~/.openclaw/data/wiki/, /graphify output        │
   └─────────────────────────────────────────────────────────────────────────┘
              │                        │                          │
              ▼                        ▼                          ▼
          KANBAN DB (truth)         CLAWTA-POLLER             ARGUS INDEX
              │                     (2m tick)                 ~/.argus/index.db
              │                          │                       │
              └──> Hermes standups       └──> dispatch            ├──> daily digest
                                                                  │    ~/.chitin/reports/
                                                                  │    YYYY-MM-DD-digest.md
                                                                  │
                                                                  ├──> on-demand query CLI
                                                                  │    `argus query "<q>"`
                                                                  │
                                                                  ├──> anomaly push to #ares
                                                                  │    (only when actionable)
                                                                  │
                                                                  └──> findings fed to Hermes
                                                                       (Slice 5: standup fold)

  Argus compute = qwen3.6:27b on RTX 3090 via ollama. Local-only.
  Argus telemetry = chitin gate covers every file read + every report write.
```

## Responsibility split

| Capability | Hermes | Clawta | Argus |
|---|---|---|---|
| Talk to user | ✓ (primary) | — (Q&A only) | — (reports only) |
| Own kanban (create/groom/prioritize) | ✓ | — | — (reads only) |
| Decide dispatch driver+model | — | ✓ | — |
| Run lobster workflow | — | ✓ | — |
| Watch GitHub (PRs/CI/reviews) | ✓ | — | ✓ (passive index) |
| Continuous chain mining | — | — | ✓ |
| Continuous memory mining (Hermes/Clawta/agents) | — | — | ✓ |
| Generate operator reports | — | — | ✓ |
| Surface anomalies in real time | — | — | ✓ |
| Answer "why did we decide X 3 weeks ago" | — | — | ✓ (queries index) |
| Mutate any state | ✓ | ✓ | **NEVER** |

## Hard rules (invariants)

1. **Read-only.** Argus never writes to kanban, never mutates chain
   state, never executes tools other than file reads + the qwen
   inference call. Report files are written under `~/.chitin/reports/`
   and `~/.argus/` only.
2. **Local-only compute.** No cloud token calls from Argus, ever.
   Argus's whole value proposition is "uses the 3090 that's otherwise
   idle." Cloud escalation is not in scope; if a question needs frontier
   reasoning, Argus flags it as a candidate for operator-time, doesn't
   call cloud itself.
3. **Argus liveness must not be a swarm dependency.** If the 3090 is
   down or qwen isn't loaded, Argus is silently off. Hermes + Clawta
   + workers must continue functioning without Argus reports.
4. **Operator privacy.** Argus indexes operator-visible surfaces.
   Hermes-to-operator DM transcripts are NOT indexed unless the
   operator opts in via an explicit configuration flag. Discord
   shared-channel narration is in scope; personal DMs are not.
5. **Chitin gate compliance.** Argus's own actions (file reads, report
   writes, ollama calls) flow through the chitin gate. Argus is
   governed like any other agent — its surface is auditable too.
6. **Retention bounded.** Argus's index has explicit retention:
   90 days for raw event copies, 1 year for derived findings, forever
   for operator-flagged "keep" reports. No unbounded growth.
7. **Quiet by default.** Argus's daily digest lands as a file on disk;
   push notifications to `#ares` fire only on triggered detectors
   (anomalies, stuck flows, operator-decision queues > N items).
   Low signal-to-noise is the design target.

## Phase / slice ordering

```
Slice 1 — Chain ledger indexer + daily digest [foundation, no deps]
  │
  ▼
Slice 2 — Kanban + git sources, cross-source detectors
  │
  ├─► Slice 3 — Hermes / openclaw / discord logs ingestion
  │
  └─► Slice 4 — Agent memory mining (Hermes memory, Clawta data, openclaw agents)
       │
       ▼
       Slice 5 — Hermes integration: Argus findings → Hermes standup → operator
```

Slice 1 is gating. Slices 2–4 can run in parallel after Slice 1 lands;
they each add a source × detector class. Slice 5 closes the loop
between observatory and orchestrator.

---

## Slice 1 — Chain ledger indexer + daily digest

**Goal:** prove the observatory pattern end-to-end with the cheapest
source. Tail `~/.chitin/gov-decisions-*.jsonl`, index every gate
event, run a small set of deterministic detectors over a rolling
window, generate a daily markdown digest with qwen3.6:27b summary
narration.

**Components in scope:**

- `python/argus/` (new tree) or `~/.argus/argus/` operator-local —
  Python package, single-process daemon. Decide between in-repo (chitin)
  vs operator-local (similar to clawta CLI shape); recommend chitin
  for source-of-truth + dispatchable upgrades.
- `argus-indexer` — tails JSONL, writes canonical events to SQLite
  index `~/.argus/index.db`.
- `argus-detect` — runs deterministic detectors (deny clusters,
  unknown-rate spikes, agent failure runs, stuck flows by
  `last_heartbeat_at`).
- `argus-report` — daily cron at operator-configurable time
  (default 07:00 local), generates `~/.chitin/reports/YYYY-MM-DD-digest.md`,
  posts a one-line summary to `#ares` if any detector tripped.
- `argus query "<q>"` CLI — natural-language query against the index,
  routed to qwen3.6:27b with the index schema in the system prompt.
- Systemd unit for `argus-indexer` (always-on tail) and timer for
  `argus-report` (daily).

**Tasks:**

1. Define the canonical event schema: `events(id, source, ts_unix,
   kind, subject, agent, driver, action_type, action_target,
   result, payload_json)`. Plus indexes on `(source, ts_unix)`,
   `(agent, ts_unix)`, `(action_type, result, ts_unix)`.
2. Implement `argus-indexer` as a tail-and-batch process. Tail
   `gov-decisions-<today>.jsonl`; on date rollover, also drain
   yesterday's file. Idempotent — replay-safe via `(source, line_hash)`
   uniqueness.
3. Implement the initial detector set:
   - **Deny cluster:** N denials of the same action_type from the same
     agent within M seconds (default N=4, M=300 — matches the
     escalation threshold from #513).
   - **Unknown-rate spike:** any agent's unknown-fraction > 1% over
     a rolling 24h window.
   - **Agent failure run:** any agent with N consecutive failed
     gate decisions.
   - **Stuck flow:** any chain_id with no events for > 2× its
     historical median duration.
4. Implement `argus-report` daily-digest generator. Template:
   - Top-level summary (3-5 sentences from qwen3.6:27b)
   - Detector results table
   - Top 10 (driver, agent, action_type) triples by volume
   - Top 5 unknown action targets to investigate
   - Stuck/zombie chain candidates
5. Implement `argus query "<q>"` — load schema into qwen prompt,
   ask it to emit a SQL query, run it, format results.
6. Systemd setup: `argus-indexer.service` (always-on), `argus-report.timer`
   (daily at 07:00 local), `argus-report.service` (oneshot).
7. Smoke: run for 24h on real chain data; verify daily report
   surfaces at least one real signal we'd want to know about; verify
   no false-alarm storms in `#ares`.

**Invariants & boundaries (per t_6dbe137e dogfood):**

**Invariants:**

- The index is a strict read-only view: indexer reads jsonl,
  detectors read the index, reporter reads the index. No component
  writes to kanban, the chain log, or any agent's state.
- Replay-safety: indexer can be restarted at any point and produces
  the same index given the same input files.
- Detector outputs are deterministic given the same index snapshot;
  qwen narration is generative but is separated from the structured
  finding so the finding itself is reproducible.

**Boundaries the worker MUST test:**

- **Empty source file** — indexer handles a freshly-created empty jsonl
  without panic.
- **Single event** — daily digest renders correctly with one event
  in the window (no divide-by-zero on rate calculations).
- **Date rollover at midnight** — indexer drains yesterday's tail and
  switches to today's file in one tick, no events dropped.
- **Malformed jsonl line** — indexer skips and logs, never blocks.
- **Duplicate-replay** — re-running indexer against the same file is
  idempotent (no duplicate rows).
- **Detector edge cases** — exactly N events at exactly M seconds
  boundary; N-1 events should NOT trigger; N+1 events SHOULD trigger.
- **qwen unreachable** — daily report still produces the structured
  detector output even if the LLM narration step fails. Narration
  degrades to a placeholder line.
- **Index corruption** — argus-indexer detects and refuses to write
  to a corrupt DB; alerts operator instead of silently failing.
- **Quiet day** — no detectors triggered → digest renders an "all
  quiet" report; no #ares push.

**Acceptance:**

- 7 consecutive daily reports produced from real chain data
- At least one real, actionable signal surfaced that the operator
  would not have caught otherwise
- Zero false-positive #ares pushes during the smoke period (tune
  thresholds before merging)
- `argus query "<q>"` returns useful results for at least 5 representative
  questions ("which agent had the most denies last week?", "PRs
  touched gov/ in last 30d", etc.)
- All boundaries above have a passing test

**Dependencies:** none

**Open questions:**

- In-chitin source tree vs operator-local? Recommend chitin
  (`python/argus/` or `swarm/argus/`) for source-of-truth + the swarm
  itself can dispatch slice upgrades.
- Daily digest delivery: filesystem only, or also Discord #ares cron
  message? Default to filesystem; add Discord later if it proves
  useful.
- qwen prompt for narration: how to balance structured output vs
  natural-language summary? Start with templated structure +
  qwen-filled summary paragraph.

---

## Slice 2 — Kanban + git sources, cross-source detectors

**Goal:** add the kanban DB and git/PR history as indexed sources.
Light up cross-source detectors that need to join (ticket → PR →
chain events). This is the slice that surfaces the patterns
operators actually care about — emergent debt, demote loops, lore
drift.

**Components in scope:**

- New ingester for kanban: poll `~/.hermes/kanban/boards/*/kanban.db`
  every 5min, snapshot `tasks` + `task_events` + `task_comments`
  into the Argus index with kind=`kanban_*`.
- New ingester for git: per repo under tracked workspaces, poll
  `git log --since=<last>` and `gh pr list --since` + `gh pr view`
  for PR metadata. Snapshot into the index with kind=`git_commit`,
  `git_pr_*`, `git_review_*`.
- Cross-source detector framework: detectors can join across kind
  prefixes via the canonical event schema.

**Tasks:**

1. Schema additions: `events.kind` namespace expands to include
   `kanban_ticket_create`, `kanban_status_transition`,
   `kanban_comment`, `git_commit`, `git_pr_opened`, `git_pr_merged`,
   `git_review_submitted`.
2. Kanban poller: change-detection against `last_seen_ts` per source
   DB; idempotent inserts.
3. Git poller: per repo, run `git log --format=...` since last
   indexed commit; `gh pr list` + `gh pr view` for PR metadata.
4. New cross-source detectors:
   - **Demote loop:** ticket bounced ready→triage ≥ 2× in 24h
     (would have caught t_c0fa21e3, t_580bc20e earlier this session)
   - **Stuck PR + green CI:** PR open > 24h, CI green, no merge
     activity (would have caught #514)
   - **Follow-up clustering:** N tickets filed against the same file
     within K days of a PR merge on that file (the t_d44e4648 pattern)
   - **Lore drift:** repeated kanban comments correcting the same
     misconception (would have caught the `normalizeGenericLeak`
     location confusion if it had happened twice)
   - **Time-to-merge regression:** rolling 7d median time-to-merge
     for each driver; alert if 7d > 2× rolling 30d
5. Daily digest gains cross-source sections.
6. `argus query` learns to join across kinds.

**Invariants & boundaries (per t_6dbe137e dogfood):**

**Invariants:**

- Kanban + git ingesters never write to source DBs.
- Cross-source detectors fail gracefully if one source is missing
  (e.g., kanban DB locked, gh API rate-limited) — produce findings
  from available sources only.
- Joined detector results carry source attribution: every finding
  cites the kanban event id + git commit/PR id it joined on.

**Boundaries the worker MUST test:**

- **Kanban DB locked at poll time** — ingester retries with backoff,
  doesn't block the indexer pipeline.
- **gh API rate-limit** — git ingester respects rate-limit headers,
  pauses, resumes.
- **Empty repo / no commits since last poll** — git ingester succeeds
  with zero rows added, no error.
- **PR with no reviews** — review-related detectors handle nil cleanly.
- **Cross-source join with one side missing** — demote-loop detector
  fires even if the matching PR doesn't exist yet.
- **Time-to-merge with zero-merge week** — regression detector handles
  no-data gracefully (no division by zero).

**Acceptance:**

- Kanban + git events indexed end-to-end for 7d of real data
- At least 3 cross-source detectors produce real findings
- One finding category (operator's choice) gets promoted from
  "report-only" to "push to #ares when triggered"
- All boundaries above pass

**Dependencies:** Slice 1 (index + detector framework)

**Open questions:**

- Repos to track: chitin only, or all operator workspaces?
  Recommend "all repos under `~/workspace/` and `~/.cache/chitin/`"
  — covers swarm worktrees too.
- gh API token: use operator's existing gh auth, or dedicated Argus
  token with read-only scope? Recommend dedicated read-only token
  for cleaner separation.

---

## Slice 3 — Logs ingestion (hermes, openclaw, discord)

**Goal:** add structured + unstructured log sources. Hermes orchestration
logs, openclaw workflow runs, Discord channel transcripts. These
sources are less clean than chain/kanban (free-form text) — the
detector class shifts from rule-based to pattern-matching with qwen.

**Components in scope:**

- Hermes log ingester: tails `~/.hermes/logs/*.log` (verify path
  during implementation), extracts structured records.
- OpenClaw log ingester: tails `~/.openclaw/logs/*.log` similarly.
- Discord transcript ingester: pulls `#ares` and `#clawta` history
  via openclaw discord plugin's export API (verify availability).
- New detector class: **pattern-matched events** — qwen scans
  natural-language log lines for specific event signatures
  (e.g., "worker spawned", "dispatch failed", "rate limited"),
  emits canonical events.

**Tasks:**

1. Locate + document each log path (Hermes config, openclaw config).
   File a sub-ticket if any path is unclear before implementing.
2. Ingesters with checkpoint-resume (don't re-scan from scratch
   on restart).
3. qwen-powered pattern extractor: given a known set of "event types
   to extract" and a log line, emit canonical-event or skip.
   Runs as a separate worker (CPU-light) feeding the indexer.
4. Detectors enabled by log sources:
   - **Hermes standup gaps:** > 8h between consecutive standups (cron
     misfire)
   - **Openclaw workflow failures:** workflow run that errored;
     correlate with downstream kanban-flow block
   - **Discord narration gaps:** dispatched ticket with no #clawta
     announce (Clawta broadcast failure)
5. Daily digest gains log-derived sections.

**Invariants & boundaries (per t_6dbe137e dogfood):**

**Invariants:**

- Log ingester is non-blocking: a stuck tail on one source never
  blocks others.
- Pattern extractor's qwen call is bounded: per-line timeout,
  per-hour token budget. If exceeded, skip + log; never freeze the
  pipeline.

**Boundaries:**

- **Log rotation mid-tail** — ingester detects inode change, reopens
  the new file, doesn't miss the rotation boundary.
- **Truncated last line of log** — ingester waits for newline before
  processing (don't half-parse).
- **Discord export rate-limit** — ingester respects, pauses, resumes.
- **qwen extractor timeout** — line goes to "unparsed" bucket,
  surfaces in metrics, doesn't block.
- **Empty log file** — ingester succeeds with zero rows.

**Acceptance:**

- All three log sources ingested for 7d
- At least 2 log-derived detectors produce real findings
- Pattern-extractor accuracy spot-checked: 50 random log lines, ≥40
  classified correctly

**Dependencies:** Slice 1 (foundation); Slice 2 nice-to-have for
cross-source joins.

**Open questions:**

- Discord export API: does openclaw expose it, or do we need a
  separate ingester via discord.py? File sub-ticket on confirmation.
- Log retention: source-side, Hermes/openclaw rotate aggressively.
  Argus index should keep its own copy (subject to Slice 1 Rule 6
  retention bounds).

---

## Slice 4 — Agent memory mining

**Goal:** index what each agent **believes** (its persisted memory)
alongside what **happened** (chain + kanban + logs). Surface drift
between belief and reality — the operator-tier insight class that
chain-only observation can't reach.

**Components in scope:**

- Hermes memory adapter: read `~/.hermes/memory/` (verify path —
  agent/memory_manager.py is the canonical interface).
- Clawta data adapter: read `~/.openclaw/data/clawta.db` (existing
  schema: agent-cards, swarm_elo if it lands, decision history).
- OpenClaw agent state adapter: read `~/.openclaw/data/agents/*`
  agent profiles + skill files + any per-agent memory.
- Wiki + knowledge-graph adapter: index `/wiki` and `/graphify`
  outputs as belief sources (these encode "what we've decided
  is true").
- New detector class: **belief-vs-reality drift** — compare an
  agent's stated belief about a thing to ground truth from chain/git.

**Tasks:**

1. Per-agent memory adapter (Hermes, Clawta, openclaw glm-agent,
   anything else). Each adapter normalizes to canonical-belief
   schema: `beliefs(agent, subject, claim, ts_recorded, source_path)`.
2. Wiki + graph adapter: parse markdown frontmatter + headings, emit
   beliefs.
3. Drift detectors:
   - **Stale belief:** belief recorded > 90d ago, no subsequent
     reaffirmation, but the subject is still mentioned in recent
     chain/kanban activity. Surface for refresh.
   - **Cross-agent disagreement:** Hermes thinks `t_X` is P50, Clawta
     thinks P30, kanban currently says P50 — surface to operator.
   - **Belief without evidence:** an agent's stated belief about a
     thing that has zero chain/git/kanban support.
   - **Reality without belief:** something that has happened (chain
     evidence) but no agent has recorded a belief about it.
4. Privacy boundary enforcement: each adapter explicitly opts in
   sources; default-deny on operator personal DMs.
5. Daily digest gains a "belief drift" section, capped at top 5 by
   severity to avoid spam.

**Invariants & boundaries (per t_6dbe137e dogfood):**

**Invariants:**

- Agent memory adapters NEVER write back to the agent's memory.
- Per-agent adapter is the only path to that agent's data — no
  generic catch-all that might accidentally index a personal source.
- Privacy boundary: opt-in per source, documented per source.

**Boundaries:**

- **Hermes memory format changes (schema migration)** — adapter
  detects + alerts; old beliefs preserved with version stamp.
- **Encrypted memory store** — adapter skips with a "needs operator
  key" report; doesn't fail silently.
- **Cross-agent disagreement on a high-volume field** (e.g.,
  priority churn) — detector deduplicates so the same disagreement
  doesn't fire daily; only initial detection + every-7d reminder
  if unresolved.
- **Belief about a deleted ticket** — drift detector handles
  gracefully (mark as orphan, don't error).
- **Wiki article without frontmatter** — adapter degrades to
  heading-extracted beliefs only.

**Acceptance:**

- Hermes + Clawta memory indexed
- At least 1 wiki/graph source indexed
- 2+ drift detectors firing on real data
- Operator confirms at least one "belief drift" finding represents
  a real correction the operator would want to make

**Dependencies:** Slice 1 (foundation). Independent of Slices 2 & 3.

**Open questions:**

- Hermes memory path + format: confirm during implementation
  (file sub-ticket if `agent/memory_manager.py` doesn't expose a
  clean read API).
- Wiki adapter: do we use the existing `/wiki` skill's query
  interface, or read raw markdown files? Recommend raw markdown +
  frontmatter for stability.
- Worker agents (claude-code, codex, etc.) don't have persistent
  memory in the chat-persona sense — only session transcripts.
  Skip them in Slice 4; revisit in a later slice if signal emerges.

---

## Slice 5 — Hermes integration: standup-fold

**Goal:** close the loop between observatory and orchestrator.
Argus findings feed Hermes' daily / 6-hourly standup; Hermes
summarizes top-3 alongside its kanban summary; operator gets one
unified feed instead of two.

**Components in scope:**

- `argus findings --since=<ts>` CLI — outputs structured findings
  list for downstream consumers.
- Hermes integration: existing `hermes cron` standup job calls
  `argus findings --since=last_standup`, folds top-3 into the
  Hermes summary.
- Operator action surface: Hermes' standup gains an "Argus says:"
  section with the top 3 findings + suggested actions (file ticket /
  archive belief / dispatch fix).

**Tasks:**

1. `argus findings` CLI: outputs `[{kind, severity, summary, evidence_links, suggested_action}, ...]`.
2. Hermes-side integration: modify `agent/standup.py` (or wherever
   the cron job lives) to call the CLI + fold into the report.
3. Operator-action wiring: each finding includes a one-tap suggested
   action; operator's reply to the standup ("dispatch finding-3")
   triggers Hermes to file the corresponding kanban ticket.
4. Update `swarm-sdlc-status-machine.md` runbook to document the new
   "Argus → Hermes → Operator" path.

**Invariants & boundaries (per t_6dbe137e dogfood):**

**Invariants:**

- Argus findings flow one-way: Argus → Hermes → operator. Hermes
  never writes back into Argus's index.
- If Argus is down or the CLI errors, Hermes' standup degrades
  gracefully (omits the section) — does NOT block the standup.

**Boundaries:**

- **Argus CLI unavailable** — standup omits the section, no error
  to operator.
- **Zero findings** — standup omits the section (don't post "Argus
  found nothing").
- **More than 3 critical findings** — cap at 3; remaining findings
  visible in the daily digest file.
- **Finding references a deleted ticket** — Hermes' standup degrades
  to "evidence link broken" footnote, doesn't error.

**Acceptance:**

- Hermes standup includes Argus section for 7 consecutive runs
- At least one operator action taken in response to an Argus
  finding via the standup-reply path
- All boundaries above pass

**Dependencies:** Slice 1; Hermes' existing cron-standup must be
running (Slice 4 of the clawta-hermes spec — verify status).

**Open questions:**

- Should Argus findings also push directly to `#ares` (independent
  of Hermes), or only via Hermes' fold? Recommend Hermes-only by
  default; operator can enable direct push per detector.
- Action wiring: how does the operator's "dispatch finding-3" reply
  get parsed? Reuse Hermes' existing reply-action grammar.

---

## What's NOT in this spec (out of scope)

- **Auto-action on findings.** Argus surfaces; operator decides.
  Even "obvious" findings (stuck PR with green CI) flow through
  operator confirmation.
- **Cloud LLM escalation.** Argus is local-only by design. If a
  question needs frontier reasoning, Argus tags it as "candidate for
  operator-time" — it doesn't call the cloud itself.
- **Live tool synthesis.** Argus doesn't generate tools, scripts,
  or PRs. Read-only, report-only.
- **Replacing swarm-audit.** Swarm-audit is the morning briefing;
  Argus is the continuous research. Both coexist — eventually
  swarm-audit may fold into Argus's daily digest, but not in this
  spec.
- **Replacing chitin's chain ledger.** Argus consumes the ledger
  via file reads. The ledger remains the source of truth for what
  happened.

## Where things live (file paths + conventions)

| Component | Path | Owner |
|---|---|---|
| Argus source tree | `python/argus/` or `swarm/argus/` (chitin) | chitin repo |
| Argus operator-local config | `~/.argus/config.yaml` | operator-local |
| Argus index DB | `~/.argus/index.db` | operator-local |
| Daily digest output | `~/.chitin/reports/YYYY-MM-DD-digest.md` | operator-local |
| Systemd units | `~/.config/systemd/user/argus-*.{service,timer}` | operator-local |
| Argus CLI | `~/.local/bin/argus` (symlink to chitin source) | operator-local |
| ollama backend | `qwen3.6:27b` on the RTX 3090, served via ollama | operator-local |

## Phase rollout

Recommended cadence (matches the clawta-hermes spec convention):

- **Week 1:** Slice 1 (chain ledger foundation + daily digest)
- **Week 2:** Slice 2 (kanban + git sources, cross-source detectors)
- **Week 3:** Slice 3 (logs) + Slice 4 (agent memory) — parallel
- **Week 4:** Slice 5 (Hermes integration), close the loop

After Slice 5 the operator has a continuous local-LLM-powered
observatory feeding their daily standup. The 3090 has a 24/7 job.

---

## Companion notes

- This spec assumes the existing chitin/Hermes/Clawta topology
  documented in `2026-05-12-clawta-hermes-architecture.md`. Argus
  is additive; it doesn't replace or modify any existing component.
- The `invariants_and_boundaries:` sections on each slice are the
  second adoption (after `t_2108964a` which archived) of the
  invariant-gate proposal in `t_6dbe137e`. Argus's slice tickets are
  the dogfood targets for that proposal.
- If the slice-1 smoke shows qwen3.6:27b is insufficient for
  narration quality, the fallback is to ship structured findings
  only (no LLM narration) and revisit model choice after Slice 2.
  The observatory's value is the indexed signal, not the prose.
