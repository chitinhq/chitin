---
date: 2026-05-02
status: design
audience: operator + future agents picking up swarm-shape work
purpose: Crystallize the shape of the chitin swarm beyond a single dispatcher
  loop — the role taxonomy, the review-tier escalation policy, the hand-off
  contract, and the phased path from "the loop runs" to "an instrumented
  factory we leave running."
supersedes: nothing yet — this is the first pass; future revisions live
  alongside as `2026-MM-DD-swarm-as-software-factory-vN.md`.
---

# Swarm as software factory

## 1. Context — why this doc exists

The 2026-05-02 overnight run produced 15 PRs unattended. This morning's
batch reviewed and merged 20+ of them. The slice-7 question — *does the
loop close?* — is answered. The next question is structural: **what
shape is this thing and how do we instrument it so the leverage scales
without the operator becoming the bottleneck?**

This doc is the source-of-truth framing for that question. It captures:

- A mental model the next batch of backlog entries can point at
- The role taxonomy — which agents we want, what they each own
- The review-tier escalation policy — when does a PR auto-merge vs.
  walk up to the operator
- The hand-off contract — how role-typed agents pass typed artifacts
  to each other
- What we're explicitly *not* building (anti-scope)
- A phased ordering of the next backlog entries

The intended audience is (a) the operator, who needs one review
surface for the architectural call rather than N separate backlog
entries, and (b) any agent reading these docs cold who needs to know
why the swarm is shaped the way it is.

## 2. Mental model — assembly line × SDLC × policy plane

Software development life-cycle and assembly line aren't competing
metaphors — they're the same metaphor at different zoom levels. The
conveyor is the SDLC. The stations are role-typed agents. The QC at
each station is `chitin.yaml` rules + adversarial review. The line
manager is telemetry.

**What chitin uniquely brings** vs. Live-SWE-agent / Devin / Cursor's
Composer / MetaGPT / OpenAI Swarm: the **policy plane and audit
chain** that make the line trustworthy enough to leave running. The
governance hook fires on every tool call; the gov-decisions chain
records every decision; the PR's branch + commits + reviewer
artifacts are all linkable to a single workflow_id. Other frameworks
build the multi-agent dance; chitin makes the dance auditable.

End-to-end:

```
DISCOVER → DEFINE → GROOM → DESIGN → IMPLEMENT → REVIEW → QA → MERGE → DOCUMENT → MEASURE
   │          │        │        │         │          │      │     │        │           │
   └──────────────────── feedback loop (telemetry + lessons) ─────────────────────────┘
```

Each arrow is a typed hand-off — an `ExecutionRequest`-shape with a
station-specific `role`. The agent doing IMPLEMENT consumes a
`design.md` produced by DESIGN. The agent doing DOCUMENT consumes the
merged diff + decision-chain. The MEASURE stage feeds the rollup
which surfaces backlog candidates back to GROOM. Refactor and
tech-debt are first-class side-channels, not stages — they can branch
off any node when telemetry says "this needs cleanup."

## 3. Station taxonomy

Eleven roles, one row each. Most map onto existing in_design entries
or well-known SoTA patterns. **Today** = current chitin state.
**Reference** = the SoTA work we should mine before re-deriving the
prompt and tool-set.

| Role | Owns | Today | Reference patterns |
|------|------|-------|--------------------|
| `researcher` | Pull external signals (arxiv, Reddit, X, openclaw upstream, ollama releases). Open candidate entries in `roadmap.md`. | Manual (operator + this assistant) | NotebookLM ingestion, awesome-openclaw-agents registry, dev.to community mining |
| `product` | Turn raw signals into 1-paragraph problem statements with success criteria. | Manual | MetaGPT's PM role; LangChain agentic-engineering writeups |
| `groomer` | Tier-classify entries; size; identify file scope; mark blockers; verify against schema. | Slice 7 has a partial groomer (Copilot GPT-4.1 with `chitin-groom-pass.ts`) | Lobster's deterministic YAML-first pattern (ggondim) |
| `architect` | Write `docs/design/<entry-id>.md` ADRs: context / options / decision / tradeoffs. | Mixed into implementation today | MetaGPT architect role; AutoCodeRover patch-context |
| `programmer` | Read entry's `file:`, edit, commit, push branch. The current swarm worker. | **In production.** | SWE-agent, Live-SWE-agent's tool-registry pattern |
| `reviewer` | Tier-escalating review (R0-R3, see §5). | R0 (Copilot bot) automated; R3 (Opus) manual via operator | Anthropic's plan/code/review pattern |
| `qa` | Generate or run E2E tests against shipped diffs; smoke-test. | Unit tests exist; no E2E generation | Cursor's test-author flow; Playwright codegen agent |
| `gatekeeper` | Read CI + reviews + telemetry; decide self-merge or escalate. | Manual (operator) | This is novel — we're defining it |
| `tech-writer` | Update wiki + ADRs + runbooks from merged work. Maintain `lessons-learned`. | Partial via operator-driven fix passes | Wiki pipeline; NotebookLM artifact generation |
| `analyst` | The Python analysis lib. Daily rollup (PR #127). Author new queries on demand. | **In production.** | LangSmith / Helicone / Phoenix patterns |
| `refactorer` + `debt-curator` | Surface duplication / dead code / hot-path debt. Maintain `docs/debt-ledger.md`. | Doesn't exist | Zoncolan-style static analysis; AutoFlake-style mechanical cleanup |

The taxonomy isn't a hierarchy — it's a *registry*. A backlog entry
at any point can name `role: researcher` or `role: qa` and the
dispatcher will pick the right prompt template + driver tier. Which
brings us to the contract that makes that work.

## 4. Hand-off contract — extending `ExecutionRequest`

Today an `ExecutionRequest` carries `prompt`, `allowed_drivers`,
`bounds`, `tier`, `base_ref`. To enable typed multi-step flows
(programmer → reviewer → fixer; researcher → groomer; etc.) we add
two optional fields:

```ts
// libs/contracts/src/execution-request.schema.ts
{
  // ... existing fields ...

  /** Station role this workflow plays. Picks the prompt template
   *  + tier defaults from the role registry. Absent = generic
   *  programmer (current behavior). */
  role?: 'researcher' | 'product' | 'groomer' | 'architect'
       | 'programmer' | 'reviewer' | 'qa' | 'gatekeeper'
       | 'tech-writer' | 'analyst' | 'refactorer' | 'debt-curator';

  /** Workflow that produced the input artifact this run consumes.
   *  E.g., a reviewer's parent_workflow_id is the programmer run
   *  that opened the PR. Lets the chain traverse hand-offs. */
  parent_workflow_id?: string;

  /** Step index within a multi-step flow (0-based). Lets a
   *  flow cap iterations (Lobster's loop.maxIterations
   *  equivalent). */
  step_index?: number;
}
```

Backlog entries gain a parallel `role:` field; the dispatcher's
`TIER_DRIVER` map becomes `(tier, role) → driver` so the analyst's
T2 work routes differently than the programmer's T2 work (analyst
needs python tools, programmer needs git + edit).

This is the structural piece behind `role-typed-backlog-entries`
(in_design from PR #110) and `multi-step-flows` (also in_design). The
two entries should land together — they're co-dependent.

## 5. Review-tier escalation policy

The headline architectural decision in this doc. Locked 2026-05-02.

```
PR opens
   │
   ▼
R0: Copilot bot review (always, free)        ── if clean → MERGE (per gates §6)
   │ (comments? complexity high?)
   ▼
R1: Copilot CLI w/ GPT-4.1 or Haiku-4.5      ── if resolves → MERGE
   │ (still flags issues?)
   ▼
R2: Copilot CLI w/ GPT-5.4 or Sonnet-4.6     ── if resolves → MERGE
   │ (still flags issues?)
   ▼
R3: claude-code-headless w/ Opus-4.7         ── if resolves → MERGE
   │ (Opus says confidence: low or self-disagrees)
   ▼
R4: ESCALATE TO OPERATOR
```

**Triggers that bump up a tier** (tunable; starting values shown):

| Signal | Bumps to | Threshold |
|--------|----------|-----------|
| Copilot bot leaves > N inline comments | R1 | N=2 |
| Diff > N LOC OR > M files | R2 | mid: 200 LOC / 10 files; high (R3): 500 / 20 |
| Touches schema files (`libs/contracts/src/*.schema.ts`) | R2 minimum | always |
| Touches kernel internals (`go/execution-kernel/internal/gov/`, `internal/canon/`, hook installers) | R3 minimum | always |
| Touches public API exports (top-level `export`s in `apps/*`) | R2 minimum | always |
| Implementor was tier T3+ | R2 minimum | always |
| Previous attempts at this entry failed (re-dispatched) | one tier above last reviewer | always |

**Triggers that escalate to R4 (operator):**

- R3 (Opus) returns explicit `confidence: low`
- R3 disagrees with R2 (or earlier reviewer) about whether to merge
- PR touches T5-shape work (`chitin.yaml`, `.chitin/`,
  `no-governance-self-modification` paths) — even if R3 approves
- Adversarial reviewer flags any 🔴 (real bug); 🟡/🟢 are doc
  tighten signals and don't block

**Cost shape**: R0 free, R1/R2 free under Copilot Pro plan, R3
~$0.10–0.50 per run on Opus. Heavy reviewer fires only on PRs the
cheaper reviewers couldn't resolve, so cost is naturally bounded by
the difficulty distribution. Empirically this session: PRs Opus
reviewed (manually, via this assistant) caught real bugs Copilot
missed in 11+ cases — *Opus-as-reviewer is empirically worth its
cost when escalation criteria fire*.

**Why escalating reviewers (not a single uniform reviewer)**:
mirrors the implementor `TIER_DRIVER` graph. Cheap reviewers handle
the simple cases; expensive reviewers earn their cost on hard ones.
A single uniform reviewer is either wasteful (Opus on every PR) or
under-powered (Copilot bot on cross-cutting refactors).

## 6. Auto-merge guards

Any of the following → escalate to R4, never auto-merge:

- CI not green
- Bucket-B rate > 0% in last 24h (regression of PR #123 — see
  `analysis.swarm_health` daily rollup)
- Diff doesn't intersect entry's declared `file:` field — the PR
  title-vs-diff mismatch signal that surfaced bucket-B in the first
  place
- Adversarial reviewer flagged any 🔴 (real bug)
- T5-tier (`chitin.yaml` / `.chitin/` / governance path) touched
- Telemetry says the implementor's driver+tier combo has < 70%
  success rate this week (avoid amplifying a regressing path)

The gates are AND'd against the reviewer's approval — both must say
yes to merge. The `gatekeeper` role implements this.

## 7. Telemetry → backlog flywheel

Telemetry isn't a station in the line — it's the line manager.

```
            ┌─────────────────────────────────────┐
            │   gov-decisions.jsonl chain         │
            │   ~/.cache/chitin/swarm-state/      │
            │   tmp/result-swarm-*.json           │
            └─────────────────────────────────────┘
                          │
                          ▼
            ┌─────────────────────────────────────┐
            │  python/analysis/                   │
            │  - swarm_runs (PR #126)             │
            │  - swarm_health daily rollup (#127) │
            │  - decisions / debt / souls         │
            │    (existing streams)               │
            └─────────────────────────────────────┘
                          │
                          ▼
            ┌─────────────────────────────────────┐
            │  Alarms + insights                  │
            │  → Slack daily rollup               │
            │  → operator console                 │
            │  → candidate backlog entries        │
            │    (researcher role pickup)         │
            └─────────────────────────────────────┘
                          │
                          ▼
                  GROOM stage / new entries
```

This loop is what closes the factory. Without it, problems are
invisible until someone trips over them (the bucket-B incident is
the cautionary case — 4 PRs of contamination only got noticed by
human-eyeballing the open PR list). With the loop in place, the
daily rollup raises an alarm the moment bucket-B re-appears, and a
researcher-role agent can be triggered to investigate without
operator action.

## 8. What we're explicitly *not* building

Critical to keep the wedge sharp. Anti-scope:

- **An IDE integration** — that's openclaw's slot. Chitin is an
  execution-kernel + policy plane that *composes with* openclaw, not
  a competitor.
- **A workflow YAML engine** — Lobster + Temporal cover this. We
  use Temporal for durability; we don't build the orchestration
  language.
- **A dashboard / fleet UI** — `abhi1693/openclaw-mission-control`
  exists. We emit OTEL spans (slice F4); mission-control consumes.
- **Our own LLM** — we route to Claude / GPT / qwen / glm via
  drivers. Model competition is a different fight.
- **A general-purpose agent framework** — we are not CrewAI /
  AutoGen / LangGraph. The role taxonomy in §3 is a *registry of
  roles for chitin's specific factory*, not a framework anyone would
  import.
- **Closed-source vending** — chitin is OSS; nothing
  Readybench-specific lands on `main`. (Memory: `feedback_chitin_oss_boundary.md`)

Saying these out loud cuts off entire classes of "should we
also..." conversations.

## 9. Phasing — next backlog entries in dependency order

Each entry below either already exists (in `docs/swarm-backlog.md`)
or needs to be filed. Listed in order of most-leverage-first +
respecting blockers.

**Phase 1: structure** (must land before Phase 2 makes sense)

1. `role-typed-backlog-entries` — already in_design (PR #110).
   Promote to ready. Adds the `role` field to schema + dispatcher.
2. `multi-step-flows` — already in_design (PR #110). Promote to
   ready. Adds `parent_workflow_id` + `step_index` to schema; lets
   the dispatcher submit a child workflow on completion.

**Phase 2: review-graph executor**

3. `review-graph-executor` — NEW. Implements the §5 escalation. A
   Temporal workflow that consumes a freshly-opened PR, runs R0,
   evaluates triggers, optionally fans out R1/R2/R3, and either
   merges (if all gates §6 pass) or escalates. Each reviewer's
   decision is a chain event with its own gov-decision row.
4. `agent-adversarial-review-pass` — NEW. The R3-tier reviewer
   prompt template; consumes the diff + Copilot's comments + entry's
   declared file scope; emits structured findings (severity 🔴/🟡/🟢
   + per-finding location + reasoning). This is what I've been doing
   manually all session — formalize.

**Phase 3: side-channels**

5. `tech-debt-ledger` — NEW. Maintains `docs/debt-ledger.md`. The
   GROOM stage consumes it when sizing entries; the researcher feeds
   it from telemetry signals (e.g., "this file's commit churn is in
   the top 10%").
6. `refactor-debt-detector` — NEW (T2). Periodic agent that reads
   the last 30 days of merged diffs + telemetry and proposes
   refactoring entries.
7. `external-signal-collector` — NEW (T2). Cron'd researcher-role
   agent: arxiv `agent` + `software engineering` filter, openclaw
   release watcher, ollama release watcher, Reddit r/LocalLLaMA top
   posts, X/HN. Dedupes against existing roadmap; opens candidate
   entries.

**Phase 4: documentation + smoke**

8. `lessons-learned-sidecar` — already in_design (PR #110, blocks:
   role-typed). The tech-writer role's first deliverable: append a
   one-sentence lesson per merged swarm PR to `docs/swarm-lessons.md`;
   dispatcher prepends recent lessons to future programmer prompts.
9. `qa-automation-from-merged-diff` — NEW (T3). For PRs that ship
   user-facing surfaces, generate Playwright / curl / unit-test
   coverage. Lobster has prior art.

**Phase 5: gatekeeper auto-merge** (last — needs trust built up)

10. `auto-merge-gate-with-escalation` — NEW (T3). Implements §6
    gates. Off by default; flip on for T0/T1 entries first; expand
    coverage as confidence builds. Always escalates T5-shape.

## 10. Open questions for the operator

These are things this doc *can't* answer alone — they need an
explicit decision before the corresponding entries flip to ready.

- **Auto-merge default-on tier(s)**: T0 only? T0+T1? When? — see
  Phase 5.
- **Researcher cadence**: every 4h? once daily? on-demand? Cost is
  low (HN/arxiv reads are free) but rollup-noise risk is real if the
  researcher opens too many candidate entries.
- **Tech-writer scope**: just `swarm-lessons.md` (cheap, narrow), or
  also wiki updates (broader, fuzzier success criteria)?
- **Local-qwen as default T0 again**: gated on the
  `qwen-ollama-config-bump-and-validate` runbook landing + a clean
  smoke test. Operator action.
- **OpenClaw + ChatGPT auth as a fourth driver**: gated on the T5
  entry already filed — needs a hands-on session with a Plus/Pro
  account.

These five are the operator-shape decisions in the next phase.
Everything else is implementation that can run on the swarm itself
once the role-typed structure lands.

## See also

- `docs/swarm-backlog.md` — execution-shape entries; the "what
  individual issues are ready" surface this doc abstracts over.
- `docs/observations/2026-05-02-bucket-b-after-action.md` — the
  cautionary tale that motivated the telemetry-first pillar and the
  review-tier policy's "telemetry alarm blocks auto-merge" gate.
- `docs/observations/2026-05-02-self-improving-swarm-sota.md` (PR
  #107) — the SoTA review that named Live-SWE / MiniMax M2.7 / Kimi
  K2.5 / Lobster as reference patterns.
- `docs/observations/2026-05-02-openclaw-usage-survey.md` (PR #107)
  — the openclaw-as-runtime-OS positioning that anchors the
  anti-scope §8.
- `python/analysis/swarm_runs.py` + `swarm_health.py` — the
  telemetry substrate the §7 flywheel runs on.
