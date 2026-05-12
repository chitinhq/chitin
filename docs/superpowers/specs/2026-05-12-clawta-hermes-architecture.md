# Hermes + Clawta Architecture — 5-slice spec

Date: 2026-05-12
Status: spec — under discussion
Author: Jared + Claude (collaborative)

## Goal

Two-agent operator architecture with a clean separation of concerns:

- **Hermes** (Ares bot) — personal assistant, kanban orchestrator, talks to the user
- **Clawta** (Clawta bot) — swarm manager, dispatch executor, manages leaf-CLI workers
- **Workers** — claude-code / codex / gemini / copilot CLIs, pure executors with no decision authority

The two agents communicate peer-to-peer via Discord DM. The user talks to Hermes primarily; talks to Clawta only when asking questions ("why did you dispatch X to codex?", "show swarm health"). Chitin gates every hop, so the chain ledger is the canonical telemetry plane.

## Target architecture

```
                              USER
                               │
       primary chat ───────────┤────────── questions only
                               ▼                    ▼
       ┌─────────────────── HERMES ◄──── DM ────► CLAWTA ───────────────┐
       │  orchestrator                            swarm manager          │
       │  • owns kanban + grooming                • owns dispatch        │
       │  • prioritization                        • picks agent + model  │
       │  • research + ingestion                  • explains routing     │
       │  • watches GitHub                        • short + long memory  │
       │  • cron: highest-leverage-next           • ELO leaderboard      │
       │  • escalates to operator                 • escalates to Hermes  │
       └──────────────────┬───────────────────────────┬─────────────────┘
                                                      │
                                                      ▼
                                                 LOBSTER WORKFLOW
                                                      │
                                                      ▼
                                      LEAF CLIs (pure executors)
                                      claude-code · codex · gemini · copilot
                                                      │
                                                      ▼
                                                 WORK PRODUCT (PR)
                                                      │
                                                      ▼
                                                 JUDGE LLM (frontier)
                                                      │
                                                      ▼
                                                 ELO LEDGER ──► routing feedback
```

Telemetry plane = chitin (chain ledger across every hop, every Discord post, every tool call).

## Responsibility split

| Capability | Owner | Notes |
|---|---|---|
| Talk to user | Hermes | Primary surface |
| Answer user questions about swarm | Clawta | Q&A only, user-initiated |
| Own the kanban (create, groom, prioritize) | Hermes | Including stale-ticket detection |
| Watch GitHub (PRs, CI, reviews) | Hermes | Notifies user about new PRs |
| Decide what to do next (highest-leverage) | Hermes | Runs cron; surfaces to user |
| Decide *which agent + model* to dispatch | Clawta | Reads ticket, picks driver, justifies |
| Run the lobster workflow | Clawta | spawn_worker → leaf CLI |
| Escalate dispatch errors | Clawta → Hermes via DM | Hermes translates to user |
| Score work product (ELO judge) | Clawta | Frontier-model judge per PR |
| Persist long-term memory | Both | Hermes for board state; Clawta for routing decisions |

## Hard rules (invariants)

1. **No subprocess invocation between agents.** Hermes does not exec `clawta`. Clawta does not exec `hermes`. They speak via Discord. The `clawta` CLI wrapper stays for operator manual use.
2. **Workers never decide.** Leaf CLIs (claude-code, codex, gemini, copilot) execute. They never pick the next ticket, never re-dispatch, never escalate to user.
3. **One operator thread.** User, Hermes, and Clawta all post in the same Discord thread/channel. No fragmented narration across channels.
4. **Chitin telemetry covers every hop.** Every shell exec, every Discord post (via openclaw plugin), every tool call routes through the chain ledger with `driver` + `agent` stamps.
5. **Clawta never DMs the user unsolicited.** User initiates Q&A; Clawta replies. Hermes is the only agent that can DM the user proactively.
6. **Failures escalate up the chain.** Worker fails → Clawta sees it → Clawta DMs Hermes → Hermes decides whether to re-dispatch, ask user, or close.

## Phase ordering + dependencies

```
Slice 1 (Smoke current path) — no deps
  │
  ▼
Slice 2 (Discord DM peer comm) — needs Slice 1
  │
  ├─► Slice 3 (Routing reasoning log) — needs Slice 1
  ├─► Slice 4 (Hermes board-audit cron) — needs Slice 2
  └─► Slice 5 (PR judge + ELO) — needs Slice 1; benefits from Slice 3
```

Slices 3, 4, 5 can run in parallel after Slice 2 lands. Slice 1 is the gating verification.

---

## Slice 1 — Smoke the current dispatch path

**Goal:** prove the rewired `clawta` CLI + `kanban-dispatch.lobster` (with `audit_comment` broadcast + new `finalize_dispatch` step) actually works end-to-end on one substantive ticket. NO new code; this is verification of changes already made today.

**Components in scope:**
- `/home/red/.local/bin/clawta` (rewired today: pattern detection → lobster invocation with auto-approve)
- `~/.openclaw/workflows/kanban-dispatch.lobster` + repo mirror at `docs/governance-setup-extras/kanban-dispatch.lobster`
- Chitin chain ledger (read-only inspection)
- One substantive kanban ticket assigned to a frontier coder

**Tasks:**
1. Pick a ready ticket with substantive code work (not a "print OK" smoke). Candidates: `t_cb0311ab` (codex, Nx step 2), `t_41b18659` (claude-code, classifier bug), or a freshly filed one.
2. Invoke `clawta "dispatch ticket t_X to <driver>"` directly from a shell to confirm the wrapper detects the dispatch pattern.
3. Observe the workflow steps fire: `fetch_ticket → classify → pick_driver → confirm (auto-approve) → reassign → audit_comment → spawn_worker → finalize_dispatch`.
4. Verify the four observable beats:
   - Start: kanban comment `🦞 Starting dispatch to <driver> ...` from author `clawta`
   - Start broadcast: Discord message in Clawta's channel (best-effort)
   - Spawn: leaf CLI runs, chain ledger shows driver=clawta + agent=<driver>
   - Finalize: branch pushed; PR opened; final kanban comment `🦞 Done. PR: <url>`; final Discord broadcast
5. Inspect ledger: `~/.chitin/gov-decisions-<date>.jsonl` — confirm every action has non-empty driver and agent.

**Acceptance:**
- One real ticket dispatched via the new wrapper, completes with a PR on origin
- Kanban has both start and final comments authored by `clawta`
- Chain ledger shows expected driver/agent attribution across the workflow
- No silent step failures (look for `|| true` paths that swallowed an error)

**Dependencies:** none

**Open questions:** none

---

## Slice 2 — Hermes ↔ Clawta Discord DM peer comm

**Goal:** Hermes and Clawta communicate via Discord DM instead of CLI subprocess. Both bots post in the same operator thread; user sees the full conversation under each bot's identity.

**Components in scope:**
- Hermes-side tool: `dispatch_via_clawta` (or extend an existing send_message tool) that DMs Clawta on Discord
- Clawta-side trigger: detect dispatch-shaped DMs and run the lobster workflow
- Operator-thread routing: confirm both bots are active in the same Discord channel/thread
- `clawta` CLI wrapper stays as-is (still works for operator manual invocation)

**Sub-problems:**
- **Clawta-side trigger.** Openclaw's glm-agent currently responds to @-mentions with text. We need a programmatic intercept: when an incoming Discord message matches `/dispatch (ticket )?(t_[a-z0-9]+)/i`, route directly to lobster (not to glm-agent's LLM). Two paths:
  - (a) Add a Clawta-specific skill that pattern-matches and shells out to `clawta` (the CLI wrapper) — reuses Slice 1's plumbing
  - (b) Build an openclaw plugin that intercepts before glm-agent — cleaner but more code
  - Recommend (a) for minimum surface area.
- **Hermes-side send.** Hermes already has terminal tool, so `openclaw message send --channel discord --account default --to <clawta-handle> --text "dispatch ticket t_X to codex"` works today. Need to know Clawta's user/handle and confirm the message reaches Clawta's listener.
- **Reply path.** When Clawta finishes, it DMs Hermes back: `🦞 t_X done. PR: <url>`. Hermes detects the DM, summarizes to user. Need Hermes's user/handle for the reply target.
- **Identity discovery.** Use `openclaw directory self` or `openclaw channels list` to get bot handles. Document in the operator-runbook.

**Tasks:**
1. Discover Clawta's and Hermes's Discord user ids / handles via `openclaw directory`.
2. Update `DISPATCH_AND_TICKETING_GUIDANCE` in `agent/prompt_builder.py`: replace "use `clawta` CLI" with "send a Discord DM to @Clawta". Include the exact handle.
3. Add a Clawta-side trigger skill or plugin that pattern-matches incoming Discord DMs and runs `clawta "dispatch ticket t_X to <driver>"` (the wrapper handles auto-approve from Slice 1).
4. Add Clawta-side reply: at the end of the workflow (or in finalize_dispatch), post a Discord DM to Hermes with the result.
5. Smoke: user @Hermes ("dispatch t_X") → Hermes DMs @Clawta → Clawta runs lobster → Clawta DMs @Hermes ("done, PR <url>") → Hermes summarizes to user.

**Acceptance:**
- User dispatches by messaging Hermes; Hermes does NOT exec clawta CLI; Hermes DMs Clawta on Discord
- Clawta receives the DM, runs lobster, narrates progress on Discord
- Clawta DMs Hermes when done; Hermes relays to user
- No CLI subprocess between Hermes and Clawta during dispatch
- Full conversation visible in the operator Discord thread under three identities (user, Hermes/Ares, Clawta)

**Dependencies:** Slice 1 (dispatch path must work first)

**Open questions:**
- DMs vs single channel: if both bots have a shared channel they post in (vs each pair DMing), the user sees one threaded conversation. Recommend shared operator channel.
- Does openclaw glm-agent's Discord listener forward all incoming messages to the LLM, or can a plugin intercept? Need to verify before picking the trigger approach.
- Hermes already uses Discord — how does it currently differentiate between user messages and bot-DM messages? May need a sender-filter in Hermes's inbound handler.

---

## Slice 3 — Clawta routing-reasoning log

**Goal:** every dispatch includes a 2-3 sentence natural-language justification from glm-agent explaining why it chose that driver + model. Recorded in kanban, chain, and Clawta's memory so the user can later ask "why did you dispatch t_X to codex?" and get the original reasoning.

**Components in scope:**
- New step in `kanban-dispatch.lobster`: `router_explanation` (between `pick_driver` and `confirm`)
- Decision storage: a new SQLite table `clawta_decisions` (or chain-event metadata) recording (ticket_id, driver, model, reasoning, ts)
- Clawta's openclaw skill/identity: teach it to look up past explanations when asked "why?"

**Tasks:**
1. Add `router_explanation` step to lobster workflow. Calls glm-agent with a prompt like: *"Ticket: {{fetch_ticket.stdout}}. You picked {{pick_driver.json.driver}} ({{model}}). In 2 sentences, explain why that's the right pick for this ticket. Reference: code complexity, language, ticket type, capabilities, cost tradeoff."*
2. Capture the reasoning. Post it to kanban as a clawta comment: `🦞 Routing: {{driver}}/{{model}} chosen because <reasoning>`.
3. Persist for later lookup. Pick one:
   - (a) Store in chain-event metadata on the dispatch event (lightweight, reuses existing ledger)
   - (b) New SQLite table at `~/.openclaw/data/clawta_decisions.db` with full schema
   - Recommend (b) for query speed and structured access.
4. Update Clawta's persona: add a skill or system-prompt section that says "when asked 'why did you dispatch X to Y?', look up the decision record at <db>/<api> and quote the reasoning verbatim."
5. Smoke: dispatch a ticket; check kanban comment + Discord broadcast; ask `@Clawta why did you dispatch t_X to <driver>?` and verify it returns the recorded reasoning.

**Acceptance:**
- Each dispatch has a recorded reasoning string visible in kanban + chain
- User can ask Clawta "why did you dispatch X to Y?" via Discord DM and get the original justification
- Reasoning is grounded in ticket content, not generic

**Dependencies:** Slice 1 (workflow path); ideally Slice 2 (Clawta on Discord for Q&A)

**Open questions:**
- Use glm-agent (cheap) for reasoning, or a frontier model (better quality, more $$$)? Recommend glm-agent for now — Clawta's persona is glm-5.1; using the same model preserves voice consistency. Upgrade to opus/gpt-5.5 if reasoning quality is consistently weak.
- Storage: chain-event metadata (no new infrastructure) vs new SQLite table (queryable). The user wants Clawta to maintain a "swarm health" view — that suggests a real table.
- Retention: keep forever? Bound to last 90 days?
- Reasoning length: 2 sentences is tight; 1 paragraph might be more useful for low-context tickets. Make it configurable.

---

## Slice 4 — Hermes board-audit cron

**Goal:** Hermes proactively reviews the board every N minutes and DMs the user when there's a highest-leverage next action. No more "user has to remember to ask Hermes about the board".

**Components in scope:**
- New `hermes cron` job (Hermes has cron via `hermes cron` subcommand)
- Audit ruleset (what's stale, what's blocked, what's missing context)
- Leverage scoring (effort vs impact heuristic)
- DM templates (what Hermes says to the user)

**Audit rules (initial):**
1. **Stale ready tickets:** ready state, no activity > 7 days → "deferred?"
2. **Missing-context tickets:** body length < 100 chars + no parent task → "needs grooming?"
3. **PRs without reviews:** open > 24h, no review comments → "want a code-review dispatch?"
4. **PRs with failing CI:** open + failing checks → "needs attention?"
5. **Stuck dispatches:** running > 2h, no kanban heartbeat → "kill or escalate?"
6. **Duplicate tickets:** title similarity > 0.85 → "merge or close one?"
7. **Operator-only tickets:** explicitly tagged `operator-job` → "ready for you when you have time"

**Tasks:**
1. Spec the audit rules formally (`docs/runbooks/hermes-board-audit.md`)
2. Build a `board_audit` Python tool/skill that returns a structured candidate list
3. Add leverage scoring: each candidate gets a score (effort × impact); top 3 surface to user
4. Wire to `hermes cron` with default 30min interval; configurable in `~/.hermes/config.yaml`
5. DM template: bullet list of candidates with `[dispatch] [defer] [snooze] [dismiss]` action buttons (or text prompts)
6. Smoke: let cron fire on schedule; verify a real audit message lands; respond → Hermes acts

**Acceptance:**
- Cron fires on schedule (configurable interval)
- Identifies real, actionable candidates from the board (low false-positive rate)
- User receives structured DM with 1-3 top candidates and clear options
- User's response routes to the right action (dispatch, defer, file, dismiss)
- Quiet on quiet boards (no message if nothing actionable)

**Dependencies:** Slice 2 (Hermes can DM user without prompt — currently works for replies, may need verification for cron-driven sends)

**Open questions:**
- Leverage scoring: hard-coded heuristic vs LLM-judged ranking? Start with hard-coded; can upgrade later.
- Chattiness: 30min interval might be too frequent. Make it user-tunable. Default to "once an hour, only if something is actionable".
- Should the cron also DM Clawta to dispatch autonomously when no operator decision is needed (e.g., a clear-cut "this ticket is ready, no ambiguity, dispatch to X")? Recommend: no for v1; user-in-loop. Reconsider after observing usage.
- Quiet hours: don't ping at night unless critical?

---

## Slice 5 — Post-PR judge LLM + ELO leaderboard

**Goal:** every PR (or completed dispatch with a PR) gets reviewed by a frontier judge LLM. Judge scores on multiple dimensions. Scores feed an ELO leaderboard for (driver, model, task-class) tuples. Eventually, Clawta uses ELO when picking driver.

**Components in scope:**
- New workflow: `pr-judge.lobster` (separate from kanban-dispatch.lobster)
- Trigger: PR open event (GitHub webhook OR polling) OR `kanban_complete` with `pr_url` in metadata
- Judge model: frontier (opus-4-7 or gpt-5.5) reads ticket + final code + tests + commit messages + chain telemetry from the dispatch run
- ELO storage: SQLite table `swarm_elo`
- Surface: `clawta status leaderboard` returns current rankings; queryable via Discord DM

**Judge rubric (initial — 5 dimensions, each 1-5):**
1. **Code quality:** clarity, idioms, no dead code, sensible structure
2. **Test coverage:** new tests added, exercises the change, edge cases
3. **Scope adherence:** matches ticket; no scope creep
4. **Efficiency:** time-to-PR, number of iterations, no thrashing
5. **Review-friendliness:** small diff, good commit messages, well-bounded

Total score: sum of 5 dimensions (range 5-25). Mapped to ELO delta via expected-vs-actual.

**ELO storage schema:**
```sql
CREATE TABLE swarm_elo (
  id INTEGER PRIMARY KEY,
  driver TEXT NOT NULL,
  model TEXT NOT NULL,
  task_class TEXT,           -- 'refactor' | 'feature' | 'bugfix' | 'research' | 'docs' | 'unknown'
  elo_score REAL NOT NULL,   -- starts at 1500; updates per dispatch
  dispatches_count INTEGER NOT NULL DEFAULT 0,
  last_dispatch_id TEXT,
  last_updated INTEGER NOT NULL,
  UNIQUE(driver, model, task_class)
);

CREATE TABLE swarm_dispatch_scores (
  id INTEGER PRIMARY KEY,
  ticket_id TEXT NOT NULL,
  pr_url TEXT,
  driver TEXT NOT NULL,
  model TEXT NOT NULL,
  task_class TEXT,
  code_quality INTEGER,
  test_coverage INTEGER,
  scope_adherence INTEGER,
  efficiency INTEGER,
  review_friendliness INTEGER,
  total_score INTEGER,
  judge_model TEXT NOT NULL,
  judge_reasoning TEXT,
  scored_at INTEGER NOT NULL
);
```

**Tasks:**
1. Design the judge prompt with the 5-dimension rubric. Iterate on a few historical PRs to calibrate.
2. Build `pr-judge.lobster` workflow. Inputs: ticket id + PR url. Steps: fetch ticket → fetch PR diff → fetch worker chain events → call judge LLM → parse scores → update ELO table → comment on PR + kanban.
3. Trigger: poll new PRs every N minutes (simpler than webhook for v1). Alternative: hook into `kanban_complete` to fire immediately.
4. Implement ELO update with K-factor tuned for low-volume comparisons (start K=32; reduce as `dispatches_count` grows).
5. Build `clawta status leaderboard [--task-class X]` command. Returns rankings.
6. Surface on Discord: `@Clawta show leaderboard` returns top 5 (driver, model) pairs.
7. Wire to `pick_driver`: as a SECONDARY signal (not primary) until ELO has 50+ dispatches per pair. Initially logs the ELO-suggested choice alongside the existing pick.

**Acceptance:**
- After a PR opens, the judge run fires within N minutes (poll interval)
- Judge produces structured scores stored in `swarm_dispatch_scores` + ELO update in `swarm_elo`
- Leaderboard queryable via Discord under Clawta's identity
- `pick_driver` step logs the ELO-suggested choice alongside its actual choice (observable, not yet authoritative)
- After 50+ dispatches per (driver, model, task-class), routing decisions show measurable improvement (track over time)

**Dependencies:** Slice 1 (dispatch must produce PRs), ideally Slice 3 (routing reasoning recorded — useful context for judge)

**Open questions:**
- Judge model: opus is best but $$$ per PR. Sonnet might be enough. Could also use a cheap fast pass + escalate to opus for high-disagreement scores.
- Trigger: PR open (early signal) vs PR merge (final signal). Recommend score both — they measure different things (worker quality vs final review-adjusted quality).
- Task class detection: how do we tag tickets with task_class for ELO bucketing? Heuristic from ticket title/body, or explicit field? Recommend heuristic v1.
- ELO maturity threshold: how many dispatches before ELO is trustworthy enough to drive routing decisions? Convention: 50 (chess uses ~30 for provisional rating). Make configurable.
- Bias: the judge might consistently favor certain styles (e.g., Go-idiomatic over Python-ish in mixed codebases). Counter-measure: judge separate models in head-to-head occasionally (route the same ticket to two drivers and compare).
- Cost ceiling: each judge call costs $0.05-$0.50 depending on model. At 20 dispatches/day, that's $30/month for opus or $3/month for sonnet. Budget-bound the workflow.

---

## What's NOT in this spec (out of scope)

- **Auto-merge.** Every public 2026 swarm system keeps human-in-loop on merge (per `project_self_improving_swarm_landscape_2026.md`). We follow that convention.
- **Live tool synthesis.** Live-SWE-agent pattern (workers generating their own tools) is deferred (memory: widens blast radius too much for v1).
- **Replacing OpenClaw's workflow engine.** We sit above as policy + orchestration; we don't rebuild Lobster.
- **Replacing chitin's gate.** The gate stays the authoritative side-effect adjudicator. New flows route through it; they don't bypass it.

## Where things live (file paths + conventions)

| Component | Path | Owner |
|---|---|---|
| Hermes operator code | `/home/red/.hermes/hermes-agent/` | hermes-agent repo |
| Hermes system prompt | `agent/prompt_builder.py` | KANBAN_GUIDANCE, DISPATCH_AND_TICKETING_GUIDANCE |
| Hermes skills | `skills/devops/{kanban-worker,kanban-orchestrator}/SKILL.md` | hermes-agent repo |
| Clawta CLI wrapper | `/home/red/.local/bin/clawta` | operator-local |
| Clawta agent config | `~/.openclaw/agents/glm-agent/` | openclaw operator state |
| Dispatch workflow | `~/.openclaw/workflows/kanban-dispatch.lobster` | operator-local; mirror in chitin repo |
| Dispatch workflow mirror | `docs/governance-setup-extras/kanban-dispatch.lobster` | chitin repo, audit trail |
| Agent cards | `~/.openclaw/data/agent-cards/*.json` | operator-local; mirror in chitin repo |
| Chitin gate code | `go/execution-kernel/internal/gov/` | chitin repo |
| Chain ledger | `~/.chitin/gov-decisions-<date>.jsonl` | operator-local persistent |
| ELO + decision storage (new) | `~/.openclaw/data/clawta.db` | operator-local; SQLite |
| pr-judge workflow (new) | `~/.openclaw/workflows/pr-judge.lobster` | mirrored to repo |
| Hermes cron jobs (new) | `~/.hermes/cron.yaml` or `hermes cron add` | operator-local |
| Audit ruleset (new) | `docs/runbooks/hermes-board-audit.md` | chitin repo |

## Plugin recommendations

**Openclaw plugins for Clawta:**
- Memory: openclaw has built-in memory primitives (`openclaw memory` subcommand exists). Audit whether it persists across sessions; add a vector-recall plugin if not (Cipher / Mem0 via MCP).
- Discord: openclaw's Discord adapter already exists; we just need to use it for Clawta-side broadcasts (Slices 2, 3).

**Hermes plugins:**
- Memory: `agent/memory_manager.py` exists. Verify it's wired to persistent storage.
- Cron: `hermes cron` subcommand exists. Use for Slice 4.
- GitHub watch: use `gh` via terminal + cron polling.

**Cross-cutting:**
- Chitin extension: a chain-ledger consumer that records (driver, model, ticket_class, success) for ELO feeding.
- Discord adapter shared between bots: confirm both Ares and Clawta can post in the same channel under separate identities.

## Phase rollout

Recommended cadence:

- **Week 1:** Slice 1 (smoke), Slice 2 (Discord DM)
- **Week 2:** Slice 3 (routing reasoning), Slice 4 (board-audit cron)
- **Week 3:** Slice 5 (PR judge + ELO)

Each slice ends with a kanban ticket marked complete + a PR merged. After all 5, the architecture is operational and self-improving.
