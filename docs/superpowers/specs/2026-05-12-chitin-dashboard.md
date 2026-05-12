# Chitin Dashboard — visual replay + self-improving feedback loop

Date: 2026-05-12
Status: spec — under discussion
Author: Jared + Claude (collaborative)

## Goal

Make chitin's dashboard a first-class visualization + analysis surface over the chain ledger, with a feedback loop that **closes back into the system**: the dashboard surfaces sessions, an LLM analyzer reads them, suggestions land as prompt/skill/policy proposals, and the operator adopts them through a UI form. Each adoption updates the live config and is itself a chain event — fully observable.

The chain ledger already captures most of the raw signal (every gated tool call, every decision, every cost, every driver/agent stamp). The dashboard makes it **comprehensible** and **actionable**.

## North-star UX

> Operator opens dashboard → sees ELO leaderboard → clicks a session → scrubs through a Chrome-devtools-style timeline showing prompts, thinking, tool calls, denials, cost, token usage, all stacked and color-coded → notices a pattern (say, claude-code thrashing on the same file 5 times) → clicks "Analyze with LLM" → analyzer suggests "Add a routing rule: large refactors route to opus, not sonnet" → operator clicks Accept → `chitin.yaml` updates → next session is gated by the new rule, and the rule's first 24h of decisions are auto-shown so the operator can roll back if it's wrong.

## What chitin already gives us (foundation)

The chain ledger at `~/.chitin/gov-decisions-<date>.jsonl` already has:
- Every tool call (action_type, action_target, allowed, rule_id, reason)
- Per-event ULID + timestamp (microsecond ordered)
- Driver + agent + role + authority + workflow_id attribution
- Cost (`cost_usd`), input bytes, tier, envelope id
- Router signals: predicted_blast, floundering_score, escalation level
- Worktree diagnostics + path
- Decision metadata: rule_id, reason, suggestion, corrected_command

That's most of the timeline data. The dashboard is mostly a **rendering** problem on existing data + a small amount of new capture.

## What's net-new (gap analysis)

| Item | Captured today? | Needed for dashboard |
|---|---|---|
| Tool call name + type | ✅ | — |
| Tool call target | ✅ (truncated for shell commands) | full body, sidecar storage |
| Tool call input args | ❌ | new capture |
| Tool call output / response | ❌ | new capture (size-bounded sidecar) |
| Model prompt envelope | ❌ | new capture at gateway hook |
| Model thinking blocks | ❌ | model-side capture (Anthropic API exposes this) |
| Model final response | ❌ | new capture |
| Cost per event | ✅ (`cost_usd`) | — |
| Input tokens | ✅ (`input_bytes` proxy) | better: track real tokens |
| Output tokens | ❌ | new capture |
| Worktree path | ✅ | — |
| Session identity | ✅ (`chain_id`) | — |
| Driver / agent | ✅ | — |

## Architecture

```
        Existing                          New
  ┌───────────────────┐         ┌──────────────────────┐
  │  Chain ledger     │         │  Sidecar prompt /    │
  │  (gov-decisions)  │         │  thinking / I-O      │
  │                   │         │  store (SQLite blob) │
  │  ✅ scratch ready │         │  ❌ to build         │
  └─────────┬─────────┘         └──────────┬───────────┘
            │                              │
            └───────────────┬──────────────┘
                            ▼
                  ┌──────────────────┐
                  │  Replay API      │   ← chitin chain replay --session <id>
                  │  (Go subcommand) │     returns timeline JSON
                  └────────┬─────────┘
                           │
                           ▼
                  ┌──────────────────┐
                  │  Dashboard UI    │   ← web app, served by chitin-kernel
                  │  (apps/chitin-   │     or a separate dev server
                  │   dashboard/)    │
                  │                  │
                  │  • ELO board     │
                  │  • Session list  │
                  │  • Timeline view │
                  │  • Cost / token  │
                  │  • Policy view   │
                  └────────┬─────────┘
                           │
                           ▼
                  ┌──────────────────┐
                  │ LLM Analyzer     │   ← cron-triggered or on-demand
                  │ (frontier model) │
                  │                  │     reads recent sessions,
                  │  Suggests:       │     emits structured recs
                  │   • prompt edits │
                  │   • new skills   │
                  │   • policy diffs │
                  │   • route tweaks │
                  └────────┬─────────┘
                           │
                           ▼
                  ┌──────────────────┐
                  │ Policy Composer  │   ← UI form, chain-replay preview,
                  │ + adoption flow  │     one-click adoption
                  │                  │
                  │  Live preview:   │     dry-run new rules over the
                  │   "If this rule  │     last N days of ledger and
                  │    were active   │     show what changes
                  │    last 7 days,  │
                  │    X events"     │
                  └──────────────────┘
```

## Hard rules (invariants)

1. **Read-only by default.** Dashboard reads the ledger. Adoption flows (writing chitin.yaml) require explicit operator click + are themselves chain events.
2. **No PII or secrets in the prompt/thinking sidecar.** A redaction pass runs before persistence; same scrubber chitin already uses for decisions.
3. **Replay is local-only.** Sessions don't leave the operator's machine without explicit export. The analyzer LLM may run remotely but only on opted-in sessions.
4. **Every adoption is reversible.** Each `chitin.yaml` change goes through git (auto-commit + branch + PR) so it's diff-able and revertable.
5. **The dashboard does not bypass the gate.** All chitin-kernel calls from the dashboard go through chitin-router-hook like any other agent. Self-modification rules apply.

## Slice ordering + dependencies

```
Slice 1 (Capture extension)   ──┐
                                ├─► Slice 2 (Replay API) ─► Slice 3 (Dashboard MVP)
                                │                              │
                                │                              ├─► Slice 4 (Cost/token viz)
                                │                              │
                                ▼                              ▼
                       Slice 5 (LLM analyzer) ───► Slice 6 (Policy composer)
```

Slices 1 → 2 → 3 are the critical path to a usable v1. Slices 4-6 add depth.

---

## Slice 1 — Capture extension (prompt + thinking + tool I/O sidecar)

**Goal:** capture the missing fields (model prompt envelopes, thinking blocks, full tool inputs/outputs, output tokens) alongside every existing chain event. Stored in a sidecar SQLite so the main ledger stays a fast append-only NDJSON.

**Components:**
- New SQLite DB at `~/.chitin/sidecar.db`
  - Schema: `event_blobs(event_id ULID PRIMARY KEY, blob_type TEXT, blob BLOB, redacted BOOL, ts INTEGER)`
  - `blob_type ∈ {prompt, thinking, tool_input, tool_output, model_response}`
- New chitin-kernel writer: `internal/sidecar/store.go` — writes blobs keyed by event ULID
- Capture hook in `gate_hook.go` extension: after the gate decides, write the full prompt + tool input from the HookInput payload
- Driver-side capture for model thinking + final response: hooks into each leaf CLI driver (claudecode, codex, gemini) — requires plumbing in `internal/driver/<cli>/`
- Redaction pass: reuse existing `internal/redact/` if it exists, or new module

**Tasks:**
1. Design + add `sidecar` package with `Put(eventID, blobType, body)` / `Get(eventID, blobType)` API.
2. Wire prompt + tool_input capture in `cmd/chitin-kernel/gate_hook.go` (after `gate.Evaluate`, before exit).
3. Add tool_output capture path: leaf CLI's PostToolUse hook (Claude Code has it; codex/gemini equivalents).
4. Implement `model_response` + `thinking` capture for claudecode driver (Anthropic API exposes thinking blocks).
5. Redaction: scrub secrets (API keys, tokens, paths matching known sensitive patterns) before persistence.
6. Add a `chitin chain blobs --event-id <ULID>` CLI for inspection.
7. Size cap + retention: bound blob size (e.g., 256 KB per blob), prune after N days (e.g., 30).

**Acceptance:**
- For every chain event with a tool call, `chitin chain blobs --event-id <X>` returns the prompt + tool_input
- For events from claudecode driver, also returns thinking + model_response
- Sidecar DB stays bounded (size + age)
- Redaction verified: secrets in test payloads don't appear in stored blobs

**Dependencies:** none

**Open questions:**
- How do we capture model thinking without intercepting model API calls? Two paths: (a) read it from the leaf CLI's transcript file (Claude Code stores transcripts); (b) wrap the model API call. Recommend (a) — non-invasive.
- Output tokens: leaf CLIs surface this in some form (Anthropic API returns it). Need to map per CLI.
- Tool inputs can be large (e.g., a full file content for Edit). Size cap or hash + sidecar object store?

---

## Slice 2 — Replay API

**Goal:** a `chitin chain replay --session <chain_id>` Go subcommand that returns a structured JSON timeline ready for rendering. The timeline aggregates ledger rows + sidecar blobs + cost rollups into a clean format.

**Components:**
- New Go subcommand `chitin-kernel chain replay` (extends existing `chain` family)
- Timeline data model: ordered list of `Step` records with type, ts, driver, agent, tool, input, output, decision, cost, prediction
- Aggregations: total cost, total tokens, tool-call count, decision-breakdown, time-on-tool histogram
- Output format: JSON for the dashboard; optional `--format=text` for CLI inspection

**Tasks:**
1. Define `Timeline` + `Step` Go structs.
2. Implement `chitin-kernel chain replay --session <id> [--format json|text]` subcommand.
3. Join ledger events with sidecar blobs by event ULID.
4. Compute aggregations: cost, tokens, tool count, denials per rule, time spent per driver.
5. Add filters: `--from <ts>`, `--to <ts>`, `--driver <name>`, `--tool <name>`.
6. Add text format for terminal inspection (Chrome-devtools-style ASCII timeline).
7. Add `chitin chain sessions --recent <N>` for listing.

**Acceptance:**
- `chitin chain replay --session <id>` returns valid Timeline JSON with all expected fields
- Joins blob data when sidecar has entries; gracefully omits when not
- Aggregations are correct (verified against hand-counted small sessions)
- Text format renders a readable timeline in the terminal

**Dependencies:** Slice 1 (for prompt/thinking/output data)

**Open questions:**
- Should the replay include router predictions (predicted_blast, floundering_score) inline or as a separate "annotations" layer? Recommend inline; one less round-trip for the dashboard.
- How to handle very long sessions (1000+ events)? Pagination + lazy-load of blobs.

---

## Slice 3 — Dashboard MVP (ELO board + session list + timeline view)

**Goal:** a web frontend that renders the replay JSON. MVP is read-only: ELO leaderboard, recent sessions list, click into a session for the Chrome-devtools-style timeline view.

**Components:**
- New app at `apps/chitin-dashboard/`
- Tech: React + Vite or similar lightweight; styled with Tailwind
- Backend: served by chitin-kernel itself via a new `chitin-kernel serve dashboard --port 9090` subcommand
- Routes:
  - `/` — recent sessions list + ELO board
  - `/session/<chain_id>` — timeline view
  - `/policy` — policy view (read-only in MVP)
- Components:
  - Session list: ts, driver, agent, ticket id, cost, success, ELO delta
  - ELO board (sourced from Hermes-Clawta epic Slice 5)
  - Timeline view: Chrome-devtools-style stacked rows (agent, tools, decisions, cost) with hover popovers showing prompt + thinking + tool I/O
  - Color coding: green = allow, red = deny, amber = heuristic-allow, gray = router-signal

**Tasks:**
1. Scaffold `apps/chitin-dashboard/` with Vite + React + Tailwind.
2. Add `chitin-kernel serve dashboard` subcommand: serves static files + a JSON API proxy to the replay subcommand.
3. Build session-list page: hits API for recent sessions, renders table.
4. Build timeline page: hits API for replay JSON, renders stacked-row timeline with scrub bar.
5. Wire ELO board (reads from `swarm_elo` table once Slice 5 of Hermes-Clawta epic lands).
6. Hover popovers: click an event → side panel shows prompt, thinking, tool I/O, decision details.
7. Basic auth or local-only binding (no remote exposure of operator session data).

**Acceptance:**
- `chitin-kernel serve dashboard --port 9090` starts the server
- Operator opens http://localhost:9090, sees recent sessions
- Clicking a session navigates to timeline view; timeline renders all events with correct stacking and ordering
- Hovering an event shows prompt / thinking / tool I/O in a side panel
- ELO board renders (placeholder data if Slice 5 not yet landed)

**Dependencies:** Slice 2 (replay API)

**Open questions:**
- Stand-alone web app or embedded in chitin-kernel? Recommend embedded for v1 (one binary, simpler ops); separate dev server for development.
- Auth model: localhost-only? Token? OAuth? Recommend localhost-only + optional token for remote operators.
- Mobile-friendly or desktop-only? Recommend desktop-only for v1 (Chrome-devtools-style UX is desktop-native).

---

## Slice 4 — Token + cost visualization

**Goal:** stacked area + per-step breakdown of token usage and cost over a session's timeline. Operators see at a glance which steps dominated cost; which drivers ran hot.

**Components:**
- New chart components in the dashboard: stacked area (cost over time, by driver), bar chart (per-tool cost), heatmap (cost x driver x model)
- Aggregation extension to replay API: per-step cost + token deltas
- Cost reconciliation: chain events have `cost_usd` for many but not all events; fill from envelope spend

**Tasks:**
1. Extend `Timeline.Step` with `tokens_in`, `tokens_out`, `cost_usd` fields (already partial from Slice 1).
2. Add aggregations: cumulative cost over session time; per-driver totals; per-tool totals.
3. Build dashboard charts: stacked area (cost over time), pie/bar (per-driver), heatmap (cost matrix).
4. Add session-level rollup: total cost, total tokens, dispatches, success rate.
5. Surface cost-per-ticket on the kanban: when a ticket completes, post a comment with cost summary.

**Acceptance:**
- Timeline view shows a cost-over-time chart aligned with the event timeline
- Per-driver/per-tool charts render correctly
- Kanban ticket comment shows total cost on completion

**Dependencies:** Slice 1 (token capture), Slice 2 (replay), Slice 3 (dashboard surface)

**Open questions:**
- Cost currency: USD only, or local? Recommend USD-only v1.
- Real tokens vs `input_bytes` proxy: which is canonical? Real tokens when available; fall back to bytes.

---

## Slice 5 — LLM analyzer cron

**Goal:** a periodic job that reads recent sessions, calls a frontier model with a structured analysis prompt, and emits suggestions (prompt edits, new skills, policy diffs, routing tweaks).

**Components:**
- New cron-driven workflow: `analyzer-cron.lobster` (runs every N hours)
- Reads last N sessions from ledger; samples or batches them
- Calls a frontier LLM (opus / gpt-5.5) with a structured analysis prompt
- Stores suggestions in a new SQLite table `analyzer_suggestions` (id, type, target, diff, rationale, applied, created_at)
- Surfaces suggestions on the dashboard (new `/suggestions` route)

**Suggestion types (initial):**
- `prompt_edit`: a proposed change to a hermes guidance block or a clawta system prompt
- `new_skill`: a proposed new skill file with body
- `policy_rule`: a proposed addition/modification to chitin.yaml
- `route_tweak`: a proposed change to `_pick_driver.py` heuristics or to the ELO weight on a particular task class

**Analysis prompt rubric (initial):**
1. **Wasted denials**: did the same rule deny the same agent 3+ times in N hours? → propose either a rule refinement or an agent prompt update
2. **Cost outliers**: did a session cost >2x median for its task class? → analyze + propose
3. **Tool thrashing**: did an agent call the same tool 5+ times with similar args? → propose a skill or prompt
4. **Routing failures**: did claude-code get dispatched to a clearly-better-suited-for-codex task? → propose route tweak
5. **Stale rules**: are there policy rules that haven't fired in 30 days? → propose removal

**Tasks:**
1. Design the analysis prompt + rubric.
2. Build `analyzer-cron.lobster` workflow.
3. Implement `analyzer_suggestions` table + writer.
4. Add `/suggestions` route to dashboard with filter + sort.
5. Cron registration via `openclaw cron` or `hermes cron`.

**Acceptance:**
- Cron fires on schedule; analysis runs over recent sessions
- Suggestions written to DB with all required fields
- Dashboard displays suggestions list, filterable by type and target
- At least 1 actionable suggestion produced over a 24h sample (manual rubric calibration may be required)

**Dependencies:** Slice 1 (rich data), Slice 2 (replay), Slice 3 (dashboard surface)

**Open questions:**
- Which frontier model? Recommend Sonnet for cost; escalate to Opus on high-disagreement or high-stakes suggestions.
- Cron frequency: hourly? Daily? Recommend daily for v1, with on-demand `/analyze` button on dashboard.
- Should the analyzer be allowed to also propose deletions of existing prompts/skills/rules? Recommend yes for v1; deletions go through the same adoption flow.
- Bias: if the analyzer always favors more rules, we drift toward over-governance. Counter: include a "drop" suggestion type and reward proposals that simplify.

---

## Slice 6 — Policy composer + adoption flow

**Goal:** a form-driven UI for editing chitin.yaml (or accepting LLM-proposed diffs), with chain-replay preview so the operator sees what would change before adopting. Adoption auto-commits to git on a branch + opens a PR.

**Components:**
- New dashboard route: `/policy` (was read-only in MVP; now editable)
- Schema-aware form: rule fields (id, action, effect, driver, target_regex, reason, suggestion) with validation
- Chain-replay preview: "if this rule were active for the last 7 days, X events change verdict"
- Adoption flow: validate → diff → operator-click-Accept → branch + commit + PR
- Suggestion adoption: clicking "Accept" on an analyzer suggestion runs the same flow
- Rollback: every adoption gets a "Revert" button (rolls forward with the inverse diff)

**Tasks:**
1. Design the schema-aware form: which fields are required, which are dropdowns, which validate against regex.
2. Build chain-replay preview: dry-run the proposed rule against ledger; show counts (denied → allowed, allowed → denied).
3. Implement adoption flow:
   - Validate YAML
   - `git checkout -b policy/<slug>-<date>`
   - Edit `chitin.yaml`
   - `git commit` with structured message
   - `git push` + `gh pr create`
4. Track adoption events in chain ledger (use a synthetic `policy.adopt` action).
5. Add suggestion-accept handler on dashboard: clicking Accept on a suggestion auto-fills the form.
6. Add rollback: clicking Revert on an adoption opens a new branch reverting the rule change.

**Acceptance:**
- Operator can edit a rule via the form, see preview counts, click Accept
- Acceptance creates a real PR in the chitin repo
- Suggestion acceptance round-trips: LLM proposes → operator clicks Accept → PR opens → merged → live policy
- Rollback works: every adoption has a reverse path

**Dependencies:** Slice 3 (dashboard), Slice 5 (suggestions); ideally Slice 1 for preview-against-blobs

**Open questions:**
- Branch naming: `policy/<rule-id>-<date>` or `policy/<short-description>`? Latter is more readable; use the LLM's slug from the suggestion if available.
- Auto-merge or human-merge? Recommend human-merge always (per the "every public swarm system keeps a human on merge" rule).
- Diff resolution if two adoptions happen concurrently? Branches are independent; the second to merge has to rebase. Acceptable.

---

## What's NOT in this spec (out of scope)

- **Multi-operator dashboard.** v1 is single-operator local. Later: shared SaaS-style.
- **Replay across multiple machines.** v1 is single-machine. Later: aggregate ledgers from a fleet.
- **Live (real-time) timeline streaming.** v1 reloads on navigate. Later: WebSocket push from chain-event writer.
- **Replay of model thinking inside the timeline as a "thought-stream" animation.** Maybe v2. v1 just shows the captured blocks in side panels.
- **Mobile UI.** Desktop-first.

## Where things live (file paths + conventions)

| Component | Path | Owner |
|---|---|---|
| Sidecar blob store | `~/.chitin/sidecar.db` | operator-local SQLite |
| Sidecar Go package | `go/execution-kernel/internal/sidecar/` | chitin repo |
| Replay subcommand | `go/execution-kernel/cmd/chitin-kernel/chain_replay.go` | chitin repo |
| Dashboard app | `apps/chitin-dashboard/` | chitin repo |
| Dashboard serve subcommand | `go/execution-kernel/cmd/chitin-kernel/serve_dashboard.go` | chitin repo |
| Analyzer workflow | `~/.openclaw/workflows/analyzer-cron.lobster` + repo mirror | operator-local + repo |
| Analyzer suggestions DB | `~/.chitin/analyzer.db` | operator-local SQLite |
| Policy composer | inside `apps/chitin-dashboard/` | chitin repo |

## Plugin recommendations (cross-epic)

- **Openclaw `cron`** for the analyzer schedule
- **Anthropic/OpenAI SDK** for analyzer LLM calls (already in chitin's stack via leaf CLIs)
- **SQLite** for sidecar + suggestions (already used by chain ledger)
- **Vite + React** for dashboard frontend (or any modern stack; not opinionated)
- **chitin-router-hook** continues to gate every action including the dashboard's own HTTP API calls

## Phase rollout

Recommended cadence:
- **Week 1:** Slice 1 (capture extension), Slice 2 (replay API)
- **Week 2:** Slice 3 (dashboard MVP)
- **Week 3:** Slice 4 (cost/token viz), Slice 5 (analyzer cron)
- **Week 4:** Slice 6 (policy composer + adoption)

After all 6, chitin has a self-improving feedback loop: every session is observable + analyzable + actionable, and the policy plane is mutable through a one-click flow grounded in real chain data.

## Cross-epic dependencies

- **Hermes/Clawta architecture epic (`t_b7af50db`)** — independent but the ELO board (Slice 5 of that epic) feeds the dashboard's ELO view. Built in parallel; integration is just a SQL JOIN.
- **PR judge LLM (Slice 5 of Hermes/Clawta)** — the analyzer cron (this spec's Slice 5) is the broader cousin: judge per PR; analyzer over time windows. Could share infrastructure.
