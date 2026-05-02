# Swarm Backlog

Tier-tagged work the local 24/7 swarm chews through. Distinct from `roadmap.md`:
the roadmap is *strategy* (where chitin is going), this doc is *execution*
(what individual issues are ready to grab, sized for which tier).

**Source of authority:** this file. The actual GitHub issues are projections.
When a tier picks up an entry, the workflow records `swarm_backlog_id` in the
chitin event chain so audit can reconcile.

## Tier definitions

| Tier | Driver | Model (post slice 6c) | Use for |
|------|--------|-----------------------|---------|
| **T0** | `local-qwen` | `ollama/qwen3-coder:30b` on the 3090 (free, fast) | mechanical, single-file, <100 LOC |
| **T1** | `copilot` *or* `claude-code-headless` | Copilot GPT-4.1 (free) / `claude-haiku-4-5` | moderate, multi-file, clear pattern |
| **T2** | `local-glm` (rate-limited) *or* `copilot` *or* `claude-code-headless` | `ollama-cloud/glm-5.1:cloud` / Copilot Haiku 4.5 / `claude-haiku-4-5` | specialized reasoning |
| **T3** | `copilot` *or* `claude-code-headless` | Copilot GPT-5.4 / `claude-sonnet-4-6` | heavy / cross-cutting / architectural |
| **T4** | `claude-code-headless` | `claude-opus-4-7` | strongest programmatic — last resort before T5 |
| **T5** | Claude Code interactive (Jared in the loop) | n/a | strategy, ambiguous scope, irreversible decisions; **also: any edit chitin's `no-governance-self-modification` rule blocks** (governance config changes are T5 by design) |

**Activity dispatch (slice 6c):** the activity reads `ExecutionRequest.tier`
and threads `--model <id>` into the spawn args. Maps live in
`apps/temporal-worker/src/activity.ts` (`CLAUDE_TIER_MODEL`,
`COPILOT_TIER_MODEL`). Override per tier per driver via
`CHITIN_MODEL_<DRIVER_KEY>_<TIER>` env. Local-* drivers ignore tier — model
is set per openclaw agent at agent-creation time (slice 3).

**Escalation rule:** when a workflow at tier `T_n` returns non-zero or stalls
past `wall_timeout_s`, Temporal re-enqueues at `T_{n+1}` and tags the issue
`swarm-misclassified-by-T_{n-1}` so we can audit the grooming agent's hit rate.

**Grooming rule:** entries land here only after they're tier-classified. Raw
ideas live in `roadmap.md` ("Deferred") or as draft issues; they cross over
once a grooming pass (Copilot GPT-4.1 free, or interactive Jared+Claude Code)
breaks them down to tier-fit size.

**Self-governance rule (slice 6 lesson):** chitin's
`no-governance-self-modification` rule blocks all agent writes to
`chitin.yaml` and `.chitin/` paths regardless of tier. Governance changes
must come through T5 (a human path). This is a feature, not a friction —
the swarm cannot quietly grant itself broader permissions.

---

## Ready (claimable now)

### `dispatcher-respect-blocks-field`

```yaml
id: dispatcher-respect-blocks-field
tier: T1
status: ready
estimated_loc: 60
blocks: []
file: apps/temporal-worker/src/dispatcher.ts, apps/temporal-worker/test/dispatcher-blocks.test.ts
references_finding: 2026-05-02-redundant-dispatch-during-in-flight-blockers
role: programmer
```

The dispatcher's `pickEntryToDispatch` currently ignores the
`blocks:` YAML field. Result: any entry with `blocks: [other-entry]`
is claimable immediately even when `other-entry` is in-flight or
hasn't shipped yet. This produced **two redundant swarm dispatches
in this same session**:

- PR #133 (`swarm: review-graph-executor`) opened while PR #132 was
  in flight — same file path, same intent. Closed.
- PR #135 (`swarm: agent-adversarial-review-pass`) opened while
  PR #134 was in flight — same intent. Closed.

The pattern is recoverable but expensive: each closed PR is wasted
agent-run cost + reviewer time + dispatcher tick.

Fix scope:

1. Extend `pickEntryToDispatch` (in `dispatcher.ts`) to skip an
   entry when ANY of its `blocks:` ids is one of:
   - currently in-flight (matches a `swarm/` branch on origin that
     has an open PR)
   - referenced by a marker in `~/.cache/chitin/swarm-state/dispatched/`
     whose corresponding workflow hasn't reached `dispatch_complete`
2. Add a structured log line at the skip site:
   `{msg: "skip entry: blocked-by", entry_id, blocked_by, blocker_state}`
3. Tests: fixture-driven — three cases:
   - blocked-by not-yet-dispatched → skip
   - blocked-by in-flight (marker exists, no PR yet) → skip
   - blocked-by shipped (PR open or merged) → don't skip — this entry
     can be dispatched as a follow-up

Edge cases:

- `blocks: []` (empty list) — must be a no-op skip-check, not a crash.
- Cycles (entry A blocks B, B blocks A) — the existing dispatcher
  contract is "skip if any block fires"; cycles produce mutual skip,
  which is the right outcome (operator inspects). Don't add cycle
  detection.
- `blocks: [unknown-entry-id]` — treat unknown blockers as not-yet-
  dispatched (i.e., skip). Operator either fixes the typo or removes
  the bogus blocker.

T1 because mechanical: read the field, look at marker dir + origin
branches, decide. No new agents, no new workflow shape.

---

### `review-graph-executor`

```yaml
id: review-graph-executor
tier: T3
status: ready
estimated_loc: 400
blocks: []
file: apps/temporal-worker/src/review-graph.ts, apps/temporal-worker/src/workflow.ts, apps/temporal-worker/src/dispatcher.ts, apps/temporal-worker/src/grooming/apply-workflow-result.ts
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §5
role: programmer
precondition: |
  PR metadata (pr_url, pr_number, files-changed list) must be persisted
  in tmp/result-<workflow>.json by the apply step before the
  review-graph workflow can consume it. Today the envelope is just
  ActivityResult — extending it to include PR metadata is part of
  this entry's scope (apply-workflow-result.ts is in the file: list).
```

Phase 2 of the factory design. Implements the §5 review-tier
escalation graph as a Temporal multi-step workflow. First real
consumer of the `parent_workflow_id` + `step_index` schema fields
that landed in Phase 1 (PR #130).

When a programmer workflow opens a PR, the apply step writes the PR
metadata into the result envelope. The review-graph workflow consumes
that, computes the starting reviewer tier from the §5 trigger
matrix, and walks the R0→R1→R2→R3→R4 chain — each tier is a child
workflow at the right driver+model, fed the diff + entry's declared
`file:` scope + previous reviewer's findings (if any). Each
reviewer's decision is a chain event with its own gov-decision row;
the chain is traversable end-to-end via `parent_workflow_id`.

Implementation steps:
1. New `apps/temporal-worker/src/review-graph.ts`:
   - `REVIEW_TIER_DRIVER` map (R0=copilot-bot-only, R1=copilot/gpt-4.1,
     R2=copilot/gpt-5.4 or sonnet, R3=cch/opus, R4=escalate)
   - `computeStartingTier(prMeta, entry, telemetry)` — reads §5 trigger
     matrix (Copilot comment count, diff LOC, file scope, implementor
     tier, prior-attempt history)
   - `escalateOneTier(currentTier)` — bumps to next reviewer
   - Reviewer-role prompt template (consume diff + previous findings;
     emit structured JSON: `{decision, severity, location, reason}`)
2. `workflow.ts` — new `reviewGraphWorkflow` that loops:
   submit reviewer at current tier → wait for envelope → read
   structured decision → either approve+return-to-merge-gate or
   escalateOneTier and recurse (capped at step_index ≤ 3).
3. Dispatcher integration: when an apply step opens a PR, enqueue
   a reviewGraphWorkflow with `parent_workflow_id = <programmer-wfid>`,
   `step_index = 1`, `role = 'reviewer'`. The marker file gets a new
   `kind: 'review-graph'` so the daily rollup (PR #127) can break
   out review costs separately from programmer costs.
4. Tests: unit-test computeStartingTier with the §5 trigger matrix
   table (one case per row); integration test of the escalateOneTier
   loop with mock reviewers.
5. Auto-merge gate (separate entry, future) consumes review-graph's
   final decision — they're paired but not co-dependent.

Note on `step_index`: the schema caps step_index at 3, which means
the workflow can run R1 (step 1) → R2 (step 2) → R3 (step 3). R0 is
the GitHub Copilot bot's server-side review (no chitin-side step
index — it's pre-existing context, not a dispatched workflow). R4
is operator escalation (no Temporal step — the workflow ends with a
notification, not a child dispatch). So a chain that escalates from
R0 fully to R4 uses 3 dispatch steps, fitting the cap exactly.

Blocked-by: nothing (Phase 1 schema fields are in main as of PR #130).
Pairs-with: `agent-adversarial-review-pass` (the R3 reviewer template
this graph dispatches to).

T3 because it introduces a new Temporal workflow shape, touches
the dispatcher, and locks in the review escalation policy in code.

---

### `agent-adversarial-review-pass`

```yaml
id: agent-adversarial-review-pass
tier: T3
status: completed
shipped_in: PR #134
estimated_loc: 200
blocks: [review-graph-executor]
file: apps/temporal-worker/src/reviewer-prompts.ts, apps/temporal-worker/src/role-prompts.ts, docs/design/2026-05-02-swarm-as-software-factory.md
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §5
role: programmer
```

> **✅ COMPLETED 2026-05-02 in PR #134.** Real adversarial-review
> prompt builder + structured-output parser shipped in
> `apps/temporal-worker/src/reviewer-prompts.ts` (separate module
> from `role-prompts.ts` for cleaner separation between dispatcher-
> level role prompts and the review-graph's child-workflow
> prompts). 35 tests cover the schema + parser + tier-tone
> differences + every render-or-fallback branch. The backlog
> dispatcher hasn't yet been taught to respect `blocks:` (known
> gap), which is why the swarm self-dispatched the entry as PR #135
> while #134 was in flight; #135 was closed.

Phase 2 of the factory design. Replaces the `reviewer` role's stub
prompt (placeholder shipped in PR #130) with a real adversarial-
review template — the kind that's been running manually all session
and that empirically catches what Copilot misses (PR #78's 8/11
real-bug rate; today's bucket-B root-cause discovery; PR #109's
notification-ordering bug; etc.).

The template's job:

- Treat itself as a hostile reviewer. Look for cases the code
  doesn't handle, assumptions that might be wrong, race conditions,
  security issues, subtle test gaps, design choices worth questioning.
- Verify each Copilot comment against the actual code (do NOT
  auto-dismiss as noise — per memory, PR #78 caught 8/11 real bugs
  Copilot flagged).
- Read the entry's declared `file:` scope and flag if the diff
  doesn't intersect it (the bucket-B detection signal).
- Emit STRUCTURED output the review-graph-executor consumes:

```json
{
  "decision": "approve" | "request_changes" | "escalate",
  "confidence": "high" | "medium" | "low",
  "findings": [
    {"severity": "🔴" | "🟡" | "🟢", "file": "...", "line": 42,
     "category": "bug" | "test_gap" | "design" | "doc",
     "summary": "...", "suggested_fix": "..."}
  ]
}
```

Implementation steps:
1. Replace the `reviewer` stub in `role-prompts.ts` with
   `buildAdversarialReviewerPrompt(prMeta, diff, copilotComments,
   entryFileScope)`.
2. The prompt instructs the agent to verify each Copilot comment +
   do its own pass + emit the structured shape above.
3. Test the structured output parses against a JSON schema (zod);
   the review-graph-executor's `escalateOneTier` reads `decision` +
   `severity` + `confidence` to decide next-tier.
4. Document the three severity classes (🔴 real bug → block merge;
   🟡 worth fixing but not blocking; 🟢 doc/nit) — these are the
   only severities; escalate-to-operator is decided by `decision`
   + `confidence`, not severity.
5. Pair-test with review-graph-executor — at least one integration
   test where R0+R1+R2 all approve but R3 finds a 🔴.

Blocked-by: `review-graph-executor` (this is the prompt template it
calls; landing alone is harmless, but only useful with the graph).

Pairs-with: `review-graph-executor`.

T3 because the prompt engineering is load-bearing — a
hallucination here is what makes the auto-merge gate unsafe.

---

### `tech-debt-ledger`

```yaml
id: tech-debt-ledger
tier: T2
status: ready
estimated_loc: 200
blocks: []
file: docs/debt-ledger.md, python/analysis/debt.py, python/analysis/__init__.py, python/analysis/tests/test_debt_ledger.py, apps/temporal-worker/src/grooming/parse-backlog.ts
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §3 (debt-curator role) + §9 Phase 3
role: programmer
```

Phase 3 of the factory design. Without a curated debt-ledger, debt
is invisible until someone trips over it (the bucket-B incident is
the cautionary case — `writeWorktreeClaudeSettings`'s
overwriting-worktree-state was a known-but-undocumented design
choice that became a security-shaped failure).

Schema (`docs/debt-ledger.md`, the human-readable canonical surface):

```yaml
id: <slug>
discovered_at: <ISO-8601>
discovered_by: <swarm | operator | user>
severity: blocking | high | medium | low
category: code-debt | doc-debt | infra-debt | governance-debt
file: <primary file or 'cross-cutting'>
description: |
  What's wrong / why it's debt / what scenario it bites in.
status: open | claimed | shipped
shipped_in: <PR # if shipped>
```

Three feeds:

1. **Manual** — operator or analyst-role agent files entries when
   noticing during code review.
2. **Automated** — periodic `refactor-debt-detector` agent (separate
   entry, blocks-by this one) reads recent diffs + telemetry and
   proposes entries.
3. **Adversarial-review** — when the reviewer flags a 🟡 (worth
   fixing but not blocking), it gets surfaced here automatically
   instead of expecting the next implementer to catch it.

The GROOM stage consumes this when sizing entries — an entry
touching a file in the debt-ledger gets bumped a tier (cross-cutting
implications likely).

Implementation steps:
1. Create `docs/debt-ledger.md` with the schema header + 3-5
   real entries to seed (e.g., the `_load_marker_count` duplication
   between `swarm_health.py` and `swarm_runs.py` from PR #127's
   adversarial pass — finding C1).
2. Extend `python/analysis/debt.py` (already exists for gov-decision
   debt analysis) with a `load_ledger(path)` that returns typed
   `DebtEntry` records. Add `tests/test_debt_ledger.py`.
3. Update GROOM-stage prompt template (when role-typed grooming
   ships) to consult the ledger before sizing new entries.
4. Update `python/analysis/__init__.py` description to mention
   the debt-ledger stream.

Blocked-by: nothing.
Feeds-into: `refactor-debt-detector` (separate entry — auto-discovery
of debt candidates).

T2 because it's mechanical (parse + project) but the schema decision
locks in the debt vocabulary across the codebase.

---

### `debt-ledger-analysis-loader`

```yaml
id: debt-ledger-analysis-loader
tier: T1
status: ready
estimated_loc: 120
blocks: []
file: python/analysis/debt.py, python/analysis/__init__.py, python/analysis/tests/test_debt_ledger.py, apps/temporal-worker/src/grooming/parse-backlog.ts
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §3 (debt-curator role)
role: programmer
```

Follow-up to PR #137 (which shipped `docs/debt-ledger.md` alone).
This entry adds the analysis-lib + grooming-side hooks split off from
the original `tech-debt-ledger` entry's scope.

Scope:

1. `python/analysis/debt.py`: extend with `load_ledger(path: Path) -> list[DebtEntry]`
   that parses `docs/debt-ledger.md`'s yaml-fenced sections into typed
   dataclasses. Match the schema header in the doc (id, severity,
   category, file, status, shipped_in, description, discovered_at,
   discovered_by). Handle malformed entries the same way other
   loaders do (skip + count parse_errors; never raise — see
   `loaders.load_gov_decisions` for the pattern).
2. `python/analysis/__init__.py`: update description to mention
   the debt-ledger stream alongside decisions / debt / soul-routing /
   swarm-runs.
3. `python/analysis/tests/test_debt_ledger.py`: fixture-driven tests.
   At minimum: well-formed entry round-trips; missing required field
   → parse_error; status filter helper; severity filter helper.
4. `apps/temporal-worker/src/grooming/parse-backlog.ts`: when the
   GROOM stage sizes a new backlog entry, look up its `file:` paths
   against the debt-ledger and bump the tier estimate by one level
   if any debt-ledger file matches (cross-cutting implications).
   Add a `crossesDebtLedger(entry: BacklogEntry, debtFiles: string[]): boolean`
   helper exported via `__test__`. Don't auto-bump in this PR;
   surface the signal so the human/agent groomer sees it.

Blocked-by: nothing.
Pairs-with: `tech-debt-ledger` (PR #137) — that entry shipped the
doc; this one ships the consumer.

T1 — mechanical parse + project + helper.

---

### `external-signal-collector` (SUPERSEDED 2026-05-02)

> **🔻 SUPERSEDED.** PR #138 attempted this entry as one slice and
> shipped non-functional code (ESM-incompatible imports — `__dirname`
> + `require.main`; `node-fetch` not in deps; all 7 fetcher stubs
> returned `[]`; only 1 of 5 declared file targets touched). Closed
> 2026-05-02. The original 250-LOC scope was too wide for one
> dispatch — re-filed as three smaller entries below
> (`researcher-role-prompt-template`, `chitin-researcher-systemd-units`,
> `external-signal-fetchers`). Each is independently dispatchable;
> together they deliver what this entry intended.

---

### `researcher-role-prompt-template`

```yaml
id: researcher-role-prompt-template
tier: T1
status: ready
estimated_loc: 80
blocks: []
file: apps/temporal-worker/src/researcher-prompts.ts, apps/temporal-worker/src/role-prompts.ts, apps/temporal-worker/test/researcher-prompts.test.ts
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §3 (researcher role)
role: programmer
```

Replace the `researcher` role's stub in `role-prompts.ts` with a
real adversarial-research prompt template, parallel to what
`reviewer-prompts.ts` (PR #134) does for the reviewer role.

Scope:

1. New `apps/temporal-worker/src/researcher-prompts.ts`:
   - `buildResearcherPrompt({ source_summaries, existing_candidate_ids, since_window_hours })`
     — produces the agent's prompt. Tier-tone is consistent (research
     is a synthesis task; one tone fits all dispatched tiers).
   - `parseResearcherOutput(stdoutTail)` — extracts a structured
     `<<<CANDIDATES>>>{json}` emit listing new candidate entries
     `[{source, id, summary, why}]`. Mirrors the reviewer-prompts
     pattern.
   - Strict zod schema for the output.
2. Update `role-prompts.ts`: the `researcher` registry entry calls
   `buildResearcherPrompt` for backlog entries with `role: researcher`,
   threading the (limited) `BacklogEntry` context as the prompt's
   minimal input. Richer caller (the runner in
   `external-signal-fetchers`) bypasses the registry and calls
   `buildResearcherPrompt` directly with full context.
3. Tests parallel to `reviewer-prompts.test.ts`: schema validation
   (6 cases), parser (8 cases including last-marker-wins, malformed
   JSON, unknown source field), prompt builder (rendering of source
   summaries / existing candidates / since-window).

Blocked-by: nothing.
Pairs-with: `external-signal-fetchers` (which uses the prompt).

T1 because mechanical — same shape as reviewer-prompts, just
different agent persona.

---

### `chitin-researcher-systemd-units`

```yaml
id: chitin-researcher-systemd-units
tier: T1
status: ready
estimated_loc: 60
blocks: [external-signal-fetchers]
file: infra/systemd/chitin-researcher.service, infra/systemd/chitin-researcher.timer, infra/systemd/README.md
role: programmer
```

Paired systemd `.service` + `.timer` to fire the researcher
periodically. Mirrors the existing `chitin-dispatcher.service` /
`chitin-dispatcher.timer` and `chitin-swarm-rollup.service` /
`chitin-swarm-rollup.timer` patterns — copy them as the template.

Scope:

1. `chitin-researcher.service` (oneshot): runs
   `pnpm exec tsx apps/temporal-worker/src/researcher.ts`. Working
   directory is the chitin repo root (`Environment=` or
   `WorkingDirectory=`).
2. `chitin-researcher.timer`: fires every 4h
   (`OnUnitActiveSec=4h` + `OnBootSec=15min`), `Persistent=true`
   so a missed tick from a powered-off rig fires on the next boot.
   `Unit=chitin-researcher.service`.
3. Update `infra/systemd/README.md` with install instructions
   matching the existing dispatcher + rollup sections.

Blocked-by-then-unblocks: this entry's `.service` references
`researcher.ts`, which `external-signal-fetchers` creates. So this
timer should land paired with that one OR one tick after — the
service file itself is just text that points at a path that may
not yet exist (timer is happy to fire even if the service unit
fails; operator notices via systemd journal).

Mark `external-signal-fetchers` in `blocks:` so the dispatcher
ships them in the right order once `dispatcher-respect-blocks-field`
(PR #139) lands.

T1 — straight unit-file authoring.

---

### `external-signal-fetchers`

```yaml
id: external-signal-fetchers
tier: T2
status: ready
estimated_loc: 200
blocks: [researcher-role-prompt-template]
file: apps/temporal-worker/src/researcher.ts, apps/temporal-worker/test/researcher.test.ts, docs/roadmap.md
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §3 (researcher role) + §9 Phase 3
role: programmer
```

The actual signal-collection logic. Consumes the researcher prompt
template (`researcher-role-prompt-template`) + writes candidate
entries to `roadmap.md`. Triggered by
`chitin-researcher-systemd-units`'s timer.

Sources to fetch (each as a typed function returning
`{source, id, summary, raw_text}[]`):

| Source | Fetch | Filter |
|--------|-------|--------|
| arxiv | RSS feed `https://export.arxiv.org/rss/cs.SE` + `cs.AI` | new IDs since last run |
| Reddit | `https://www.reddit.com/r/LocalLLaMA/top.json?t=day` (no auth) | keyword: agent / swarm / chitin |
| HN | `https://hn.algolia.com/api/v1/search_by_date?query=AI+coding+agent` | last 24h |
| openclaw | `gh api repos/openclaw/openclaw/releases?per_page=5` + `gh api repos/openclaw/openclaw/issues?state=open&since=YYYY-MM-DDTHH:MM:SSZ` | new releases + issue activity |
| ollama | `gh api repos/ollama/ollama/releases?per_page=5` | new releases |
| awesome-openclaw-agents | `gh api repos/mergisi/awesome-openclaw-agents/commits?since=...` | new template additions |

X/Twitter intentionally NOT in the v1 — nitter/rss.app are flaky
and X auth is high-friction. Add later if needed.

Implementation requirements (these caused PR #138's bugs — encode
them explicitly):

- **ESM-compatible**: use `fileURLToPath(import.meta.url)` for
  `__dirname`-equivalent. Use `process.argv[1] === fileURLToPath(import.meta.url)`
  for the `if (require.main === module)` equivalent (matches the
  pattern PR #105/#112/#127 introduced).
- **No new deps**: use Node 18+'s native `fetch` (no `node-fetch`
  import). Use `node:fs/promises` (with the prefix). Use
  `node:child_process` for any gh CLI calls.
- **Dedup against `roadmap.md`**: parse existing
  "Candidates from external signal" section, skip ids already
  present.
- **Cap**: max 5 new candidates per run (default; configurable via
  env). Bursty news day shouldn't flood roadmap.md.
- **Telemetry**: emit a structured stdout log line
  `{component: "researcher", candidates_opened: N, sources_scanned: K}`
  for the daily rollup to consume.

Output: append `## Candidates from external signal` section to
`roadmap.md` (creating if missing), with each candidate as:
```markdown
- [<source>] [<id>](url) — <1-line why>
```

Tests: 8-12 fixture-driven cases. Mock the source fetchers (return
canned typed records); assert dedup, cap, roadmap-format, telemetry
log shape. ESM compat covered by the fact tests run via vitest
which respects ESM module resolution.

Blocked-by: `researcher-role-prompt-template` (the prompt + parser
this entry imports). Once that lands the dispatcher can pick this
up — assuming `dispatcher-respect-blocks-field` (PR #139) is in
main first; otherwise this could ship before its prompt and bust.

T2 because the implementation needs HTTP + parse + dedup + agent
synthesis — multi-step, but each step is mechanical.

---

### `verify-openclaw-chatgpt-auth-on-rig`

```yaml
id: verify-openclaw-chatgpt-auth-on-rig
tier: T5
status: ready
estimated_loc: 80
blocks: []
file: docs/runbooks/openclaw-chatgpt-driver.md (new)
references_finding: 2026-05-02-altman-tweet-chatgpt-auth-in-openclaw
escalation: human-pickup
```

> **🚨 ESCALATED — T5 / human action required.** Sam Altman tweeted
> 2026-05-02 that openclaw now supports signing in with a ChatGPT
> account, putting GPT-5-class reasoning on the swarm's substrate at
> $0 marginal cost (covered by an existing Plus/Pro subscription).
> Verifying the actual flow needs hands-on testing with a real
> ChatGPT account on the 3090 rig — not something the autonomous
> dispatcher can or should do unattended. Tier T5 ensures the
> dispatcher skips this entry.

The chitin worker currently has three driver paths in production:
copilot (free GPT-4.1 on the Copilot Pro plan), claude-code-headless
(paid Anthropic API at ~$0.10/run), and the local-* (qwen/glm/
deepseek) family via openclaw + ollama. A fourth path —
`openclaw + ChatGPT-account` — would unlock GPT-5-class reasoning at
zero marginal cost for any operator already paying for ChatGPT, and
fits chitin's three-plane architecture (chitin governs, openclaw
executes, ChatGPT provides the model).

What's already known (from a 2026-05-02 CLI poke on this rig):

- The on-rig openclaw is **2026.4.25 (aa36ee6)**.
- `openclaw channels login --verbose` exists — likely the right
  surface for a ChatGPT account binding.
- `openclaw models auth` (subcommand: "Manage model auth profiles")
  also exists.
- A bundled `openclaw auth` CLI is **plugin-gated** — disabled by
  default; `~/.openclaw/openclaw.json` `plugins.allow` excludes
  `"auth"`. Adding it back may be a precondition.
- Sam's tweet did not name a specific provider flag; the verification
  step needs to discover whether the path is `--provider chatgpt`,
  `--provider openai`, or a different shape entirely.

Verification checklist (for whoever picks this up):

1. Confirm the openclaw release notes / docs.openclaw.ai for the
   ChatGPT auth flow (release ≥ 2026.4.25 or a newer point release).
2. If `auth` plugin is needed: `jq '.plugins.allow += ["auth"]' ~/.openclaw/openclaw.json | sponge ~/.openclaw/openclaw.json`
   (or hand-edit). Restart any long-running openclaw consumers.
3. Run the login flow (probably `openclaw channels login --provider chatgpt --verbose`)
   and follow the device-code or browser path with a real ChatGPT
   Plus/Pro account.
4. Verify a model is now available: `openclaw models list` should
   include a `chatgpt-*` or `openai-gpt-*` entry.
5. Bind to a new chitin driver id. Suggested name: `chatgpt-via-openclaw`
   (matches the local-* / claude-code-headless naming pattern).
   Adding to:
   - `libs/contracts/src/execution-request.schema.ts` `DriverIdSchema`
   - `apps/temporal-worker/src/activity.ts` `planInvocation` switch +
     `DRIVER_AGENT_MAP`
   - `apps/temporal-worker/src/dispatcher.ts` `TIER_DRIVER` (don't
     route by default — gate behind a flag until benched)
6. Smoke-test: dispatch one trivial T1 entry through the new driver
   via `submit.ts` (DRIVER=chatgpt-via-openclaw). Confirm the run
   completes with exit_code=0 and a real diff.
7. Bench head-to-head against copilot on 3-5 backlog entries to
   pick the right tier defaults.
8. Document the working stack in `docs/runbooks/openclaw-chatgpt-driver.md`.

Why T5 (not T2/T3): the verification involves real-account auth,
ToS scrutiny (does Anthropic-style "no Claude as worker" have an
OpenAI parallel?), and an architectural addition to the driver enum
— not autonomous-swarm-shaped work. After the runbook lands and the
dispatcher PR is filed, individual follow-ups (e.g.,
`bench-chatgpt-vs-copilot-on-T1`) can drop back to T2/T3.

---

### `qwen-ollama-config-bump-and-validate`

```yaml
id: qwen-ollama-config-bump-and-validate
tier: T2
status: ready
estimated_loc: 80
blocks: []
file: docs/runbooks/local-qwen-stack.md (new), apps/temporal-worker/src/activity.ts
references_finding: 2026-05-02-overnight-driver-mix
```

The 2026-05-01 instability investigation (PR #112) produced a clear
remediation list but no implementation. Overnight 2026-05-02:
local-qwen received **0 dispatches** (TIER_DRIVER routes T0/T1 →
copilot until the qwen layer is fixed). The 3090 sat idle while
$0.50 went to claude-code-headless on T2/T3. Stop the bleed by
actually shipping the recommended config.

Concrete actions from the investigation:
1. Bump ollama 0.21.0 → 0.22.x (verified current via
   `gh api repos/ollama/ollama/releases | jq '.[0].tag_name'`).
2. Set `OLLAMA_KV_CACHE_TYPE=q8_0` in
   `/etc/systemd/system/ollama.service` (halves KV cache VRAM).
3. Set `num_ctx=32768` on the qwen-agent (avoids 262144 → CPU
   offload spill).
4. Smoke-test by dispatching one T0 entry to local-qwen explicitly
   (`allowed_drivers: ['local-qwen']` override) and confirm the run
   returns exit_code=0 with the qwen3-coder agent producing a real
   diff.
5. Document the resulting stack in `docs/runbooks/local-qwen-stack.md`
   so this doesn't get re-derived next time.

After this lands, a follow-up entry can flip `TIER_DRIVER[T0]` from
`copilot` back to `local-qwen` (will be a 1-line change). Keep T0
on copilot for now — local-qwen reliability is the gate.

T2 because it crosses systemd, ollama config, and verification work.

---

### `analysis-swarm-runs-loader`

```yaml
id: analysis-swarm-runs-loader
tier: T1
status: ready
estimated_loc: 120
blocks: []
file: python/analysis/swarm_runs.py (new), python/analysis/tests/test_swarm_runs.py (new)
references_finding: 2026-05-02-overnight-driver-mix
```

The Python analysis library (`python/analysis/`) only has
`gov-decisions-*.jsonl` loaders today. The 2026-05-02 overnight-mix
analysis (driver/tier breakdown, contamination rate, per-run cost)
required a bespoke `/tmp/swarm-overnight-analysis.py` script. That
substrate is the wrong place — telemetry-derived insights should
live in the lib so future questions are queryable, not re-engineered
each time.

Schema:
- Inputs: `~/.cache/chitin/swarm-state/dispatched/*.json` (markers)
  joined with `tmp/result-swarm-*.json` (envelopes) by `workflow_id`.
- Output: typed `SwarmRun` dataclasses with `entry_id`, `tier`,
  `driver`, `dispatched_at`, `exit_code`, `duration_ms`,
  `commits_added`, `pr_url` (parsed from stdout via `extractPrUrl`-
  equivalent), `cost_usd` (parsed from claude-code-headless tail),
  `model` (parsed from modelUsage), `bucket_b` (see heuristic note
  below).
- Window-filter helper matching `loaders.Window`.

**`bucket_b` heuristic (don't byte-match a moving target):** the
naive "diff equals `writeWorktreeClaudeSettings`'s exact output"
fails the next time the worker tweaks JSON spacing, adds a field, or
appends a trailing newline. Use a structural signature instead:

- post-PR-#123, the apply-step revert SHOULD eliminate this case
  entirely — the heuristic exists to detect *regression*, not to
  recover from it
- structural condition: PR's modified files set is exactly
  `{.claude/settings.json}` AND every change to that file is
  type=`M` (modification, not addition or deletion) AND
  `commits_added == 1` (the auto-commit fallback)
- when this fires, raise it as a regression alarm, not just a
  bucket_b counter — apply-step must have failed to revert

Implementation steps:
- `python/analysis/swarm_runs.py`: `load_swarm_runs(state_dir, tmp_dir, window)` returning `list[SwarmRun]`.
- Helpers for `cost_by_driver`, `outcomes_by_driver`, `bucket_b_rate`.
- Tests using fixture marker + envelope JSON files.
- Update `python/analysis/__init__.py` description to mention
  decisions, debt, soul-routing, **and swarm-runs**.

T1 because mechanical (parse JSON, dataclass projection, simple
aggregations). No external services, no model calls.

---

### `swarm-daily-rollup-healthcheck`

```yaml
id: swarm-daily-rollup-healthcheck
tier: T2
status: ready
estimated_loc: 150
blocks: [analysis-swarm-runs-loader]
file: python/analysis/swarm_health.py (new), scripts/swarm-daily-rollup.sh (new), infra/systemd/chitin-swarm-rollup.timer (new)
references_finding: 2026-05-02-overnight-driver-mix
```

Telemetry without alarms is just data. The bucket-B contamination
overnight 2026-05-02 looked indistinguishable from healthy
dispatch_complete events in Slack — operator only noticed because
they happened to inspect 18 PRs by hand the next morning. We need a
daily rollup that flags drift.

Output (Slack channel + structured stdout for journal):
- 24h dispatches by driver
- Bucket-B rate (target: 0% post-PR #123 fix; alarm: > 0% =
  regression of the apply-step revert)
- Success rate per driver and per tier (alarm: < 70%)
- Cost summary (claude-code-headless $/run; total)
- Local-qwen idle-percentage (we're paying for cloud when the 3090
  could be working — alarm: > 80% T0 routed away from local-qwen)
- **Short-run rate per driver/tier** (runs with `duration_ms < 15s`
  AND `commits_added == 0`). Overnight 2026-05-02 had two CCH T2
  bucket-B runs at 7.8s and 9.0s — agent burned <10s and gave up.
  That's a different failure mode from "agent worked, then declined"
  and worth surfacing separately so prompt-tuning vs apply-step
  fixes target the right thing. Alarm: > 25% short-run rate on any
  driver suggests the prompt template doesn't fit that tier's
  entries.
- Top 3 failure modes (timeout, exit_code=1 partial, contamination,
  short-run-no-work)

Wire-up:
- Cron: `chitin-swarm-rollup.timer` fires daily at 06:00 local.
- Service runs `swarm-daily-rollup.sh` which calls the Python
  rollup, posts to `CHITIN_SLACK_WEBHOOK_URL` if set, and writes
  `~/.cache/chitin/swarm-rollups/<YYYY-MM-DD>.json` for trend
  analysis.

T2: cross-cutting (Python analysis + Slack notifier reuse + systemd
timer). Blocked by `analysis-swarm-runs-loader` since the rollup is
its first real consumer.

---

### `dispatcher-preflight-scrub-claude-settings-backup`

```yaml
id: dispatcher-preflight-scrub-claude-settings-backup
tier: T1
status: ready
estimated_loc: 40
blocks: []
file: apps/temporal-worker/src/dispatcher.ts
references_finding: overnight-2026-05-02-bucket-b-contamination
```

The 2026-05-02 overnight run produced four PRs (#114, #117, #118, #120
— all now closed) where the swarm worker committed only a
`.claude/settings.json` diff and no actual task implementation. Root
cause: a leftover `.claude/settings.json.chitin-backup-<ts>` artifact
in the workspace's `.claude/` directory. The agent's worktree
inherited it as untracked-then-staged content, and `git add -A` in
the apply step picked it up as the diff. The actual entry's target
file was never edited.

Fix: dispatcher pre-flight refuses to dispatch (or scrubs the
artifact first) when any `.claude/settings.json.chitin-backup-*` file
is present in the cwd at tick start. Two options:

1. **Hard refuse** — log a warn-level dispatcher event, emit
   `notifyTickIdle("preflight: claude-settings-backup artifact present")`,
   exit 0. Operator deletes the artifact manually before the next tick.
2. **Auto-scrub** — `rm` the artifact at tick start (it's a backup, by
   definition disposable), log the scrub, dispatch normally.

Option 2 is more autonomous; option 1 is safer if the artifact is ever
load-bearing. Default to option 1 unless dogfood reveals a recurring
operator burden.

Also: the four closed PRs' backlog entries (`wall-timeout-sigkill-
propagation`, `task-validate-command-pre-activity-gate`,
`chitin-install-slice-3-agents`, `rename-local-cloud-driver-misnomer`)
should be re-evaluated before re-dispatch:

- `wall-timeout-sigkill-propagation` is **already shipped in main**
  (slice-7a / PR #99) — mark completed, drop from backlog.
- `rename-local-cloud-driver-misnomer` needs a human pick from its
  three naming-convention options before any worker can claim it.
- `task-validate-command-pre-activity-gate` and
  `chitin-install-slice-3-agents` are still claimable as written.

---

### `normalize-decision-params-truthiness`

```yaml
id: normalize-decision-params-truthiness
tier: T0
status: ready
estimated_loc: 5
blocks: []
file: apps/openclaw-plugin-governance/src/index.mjs
references_issue: 82
```

`apps/openclaw-plugin-governance/src/index.mjs:48` returns
`decision.params ? { params: decision.params } : undefined`. Empty object `{}`
is truthy → would clobber the agent's args with empty params if the kernel
ever returns that. Fix: `Object.keys(decision.params ?? {}).length > 0`.
Add a test in `bridge.test.ts` covering empty-object case.

---

### `workflow-name-drift-test`

```yaml
id: workflow-name-drift-test
tier: T0
status: ready
estimated_loc: 8
blocks: []
file: apps/temporal-worker/test/activity.test.ts (new file or extend)
references_issue: 82
```

`apps/temporal-worker/src/submit.ts:8` uses `WORKFLOW_NAME = 'executeRequestWorkflow'`
as a string, with `import type { executeRequestWorkflow }` for type safety.
If the export is renamed, the string goes stale silently. Add a unit test
asserting `executeRequestWorkflow.name === WORKFLOW_NAME`.

---

## Qwen-layer reliability (T0→copilot until these ship)

These five entries together aim to flip `TIER_DRIVER[T0]` back from
`copilot` to `local-qwen` in `dispatcher.ts`. Slice 7-tuning's first
live run with `qwen3-coder:30b` on the 3090 surfaced all the gaps; each
entry below targets one. Until they land, T0 routes to Copilot's free
GPT-4.1 — same cost ($0 under Jared's plan), reliable tool dispatch.

### `dispatcher-prompt-relative-path-prefix`

```yaml
id: dispatcher-prompt-relative-path-prefix
tier: T1
status: ready
estimated_loc: 8
blocks: []
file: apps/temporal-worker/src/dispatcher.ts
```

The slice-7-tuning prompt names the entry's `file` field as the
`TARGET FILE`. Live run: qwen3-coder:30b interpreted the relative path
`apps/openclaw-plugin-governance/src/index.mjs` as absolute (prepended
`/`), got `ENOENT` on `/apps/...`. Patch `buildPrompt` to prepend `./`
to the target file so it's an explicit relative path: `./apps/foo`.
Add a test asserting the prompt contains `./` + the path.

---

### `dispatcher-prompt-scope-discipline`

```yaml
id: dispatcher-prompt-scope-discipline
tier: T1
status: ready
estimated_loc: 15
blocks: []
file: apps/temporal-worker/src/dispatcher.ts
```

Slice-7-tuning live run: agent picked `test/bridge.test.ts` instead of
the entry's stated `src/index.mjs` — scope drift. Tighten
`buildPrompt`: forbid editing files not named in the entry's `file`
field, and instruct the agent to `read` ONLY the target file before
editing. Add an integration check post-run: if the diff touches files
outside the entry's `file` list, the apply step refuses to push and
flags scope drift in the chain.

---

### `activity-include-hook-events-flag`

```yaml
id: activity-include-hook-events-flag
tier: T1
status: ready
estimated_loc: 20
blocks: []
file: apps/temporal-worker/src/activity.ts
```

Add `--include-hook-events` to the `claude -p` invocation and the
openclaw `agent` invocation (where supported). When the agent's tool
calls fail (e.g., `ENOENT` on a misinterpreted path), the hook events
in the structured stream-json output give the operator visibility
without grepping verbose stderr. Update activity-types `ActivityResult`
to expose a parsed `hookEvents` summary.

---

### `qwen-ollama-stream-instability-investigation`

```yaml
id: qwen-ollama-stream-instability-investigation
tier: T2
status: ready
estimated_loc: 50
blocks: []
file: docs/observations/2026-05-XX-qwen-ollama-instability.md (new)
```

Slice-7-tuning live run errored: `Ollama API stream ended without a
final response model=qwen3-coder:30b`. Investigate: ollama logs
during the run, GPU memory pressure on the 3090, model load patterns,
ollama version. Output is an observation doc with the failure mode
characterized + a recommended fix (smaller model? quantization? other
local model?). Doesn't touch code — needs T2 reasoning to read logs
and characterize the failure.

---

### `dispatcher-flip-t0-back-to-local-qwen`

```yaml
id: dispatcher-flip-t0-back-to-local-qwen
tier: T0
status: blocked
estimated_loc: 4
blocks: [dispatcher-prompt-relative-path-prefix, dispatcher-prompt-scope-discipline, qwen-ollama-stream-instability-investigation]
file: apps/temporal-worker/src/dispatcher.ts
```

Final entry in the qwen-layer arc. Once the three blockers above ship,
flip `TIER_DRIVER[T0]` from `'copilot'` back to `'local-qwen'` in
dispatcher.ts. Add a smoke-test record showing a productive T0 run
end-to-end on local-qwen. Status `blocked` until the dependencies
land — the dispatcher's `pickEntryToDispatch` doesn't currently
respect blocks (slice 8 work) but a human reviewer will catch a
premature merge.

---

### `repo-regex-tighten`

```yaml
id: repo-regex-tighten
tier: T0
status: ready
estimated_loc: 4
blocks: []
file: libs/contracts/src/execution-request.schema.ts
references_issue: 82
```

`^[^/\s]+\/[^/\s]+$` accepts `..foo/..bar` because `..` matches `[^/\s]+`.
Tighten to forbid leading `.` — e.g., `^[\w][\w.-]*/[\w][\w.-]*$`. Add tests
that reject `../foo`, `..foo/bar`, `foo/../bar`, and accept `chitinhq/chitin`.

---

### `read-vs-read_file-file_path-alias`

```yaml
id: read-vs-read_file-file_path-alias
tier: T0
status: ready
estimated_loc: 6
blocks: []
file: go/execution-kernel/internal/gov/normalize.go
```

Slice 3 added `case "read"` with `path` → `file_path` alias fallback, but the
existing `case "read_file"` has no fallback. Make `read_file` use the same
alias logic for parity. Add a test that `read_file({file_path: "/x"})` and
`read_file({path: "/x"})` produce the same Action.

---

## In design (needs spec or breakdown before claimable)

### `wall-timeout-sigkill-propagation`

```yaml
id: wall-timeout-sigkill-propagation
tier: T2
status: ready
estimated_loc: 60
blocks: []
file: apps/temporal-worker/src/activity.ts
references_issue: 82
references_finding: 11
```

`setTimeout(() => child.kill('SIGKILL'), wall_timeout_s * 1000)` SIGKILLs
openclaw, but openclaw's child processes (model runners) inherit stdout pipes
and keep them open. Node's `'close'` event waits for all pipe FDs to close →
never fires → activity hangs until Temporal's 15-min `startToCloseTimeout`.

Two known-workable fixes; pick one and test:

1. `spawn(cmd, args, { detached: true })` then `process.kill(-pid, 'SIGKILL')`
   on timer (negative pid = process group, kills children too).
2. Force-close stdout/stderr in the timer callback after `child.kill()` —
   `child.stdout.destroy(); child.stderr.destroy()`. Less clean.

Needs an integration test that spawns a process with a hung grandchild and
confirms close fires within ~1s of the timer.

---

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear, two solution paths are given, and integration test requirements are explicit. T2 fits due to process management and test complexity.

Implementation steps:
- Update activity.ts to use spawn with { detached: true } for child processes.
- Modify SIGKILL logic to use process.kill(-pid, 'SIGKILL') for group termination.
- Add fallback to force-close stdout/stderr if needed.
- Write an integration test that spawns a process with a hung grandchild.
- Verify that the 'close' event fires within ~1s after the timer.
- Document the chosen approach and rationale in code comments.

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear, two solution paths are given, and integration test requirements are explicit. T2 fits due to process management and test complexity.

Implementation steps:
- Update activity.ts to use spawn with { detached: true } for child processes.
- Modify SIGKILL logic to use process.kill(-pid, 'SIGKILL') for group termination.
- Add fallback to force-close stdout/stderr if needed.
- Write an integration test that spawns a process with a hung grandchild.
- Verify that the 'close' event fires within ~1s after the timer.
- Document the chosen approach and rationale in code comments.

### `tools-summary-structured-result`

```yaml
id: tools-summary-structured-result
tier: T1
status: ready
estimated_loc: 40
blocks: []
file: apps/temporal-worker/src/activity-types.ts, src/activity.ts
references_issue: 82
references_finding: 12
```

`ActivityResult.stderr_tail` is a 2000-char string slice that drops the actual
tool list openclaw emits in its verbose JSON. Add a structured field like
`tool_summary?: { calls: number; tools: string[]; failures: number }` and
parse it from the openclaw JSON output (it already emits `toolSummary`).
Surface in the workflow result so reviewers don't have to grep stderr.

---

**Groomed 2026-05-02 (0.95 confidence):** The scope is clear: add a structured field, parse existing JSON, and surface it. Multi-file but straightforward, fits T1.

Implementation steps:
- Locate where ActivityResult is defined and used.
- Add an optional tool_summary field to ActivityResult with the specified structure.
- Update the code that parses openclaw JSON output to extract toolSummary and populate tool_summary.
- Ensure tool_summary is surfaced in the workflow result object.
- Write or update tests to verify tool_summary is correctly parsed and exposed.

**Groomed 2026-05-02 (0.95 confidence):** The scope is clear: add a structured field, parse existing JSON, and surface it. Multi-file but straightforward, fits T1.

Implementation steps:
- Locate where ActivityResult is defined and used.
- Add an optional tool_summary field to ActivityResult with the specified structure.
- Update the code that parses openclaw JSON output to extract toolSummary and populate tool_summary.
- Ensure tool_summary is surfaced in the workflow result object.
- Write or update tests to verify tool_summary is correctly parsed and exposed.

### `cron-subagents-image-granular-targets`

```yaml
id: cron-subagents-image-granular-targets
tier: T1
status: ready
estimated_loc: 40
blocks: []
file: go/execution-kernel/internal/gov/normalize.go
references_issue: 82
```

Slice 3a maps `cron`, `subagents`, `image`, `image_generate` to action types
with `target=toolName` (literal). Loses granular fields. For policy
precision (e.g., "deny `cron action=add` outside business hours"), extract:

- `cron`: schema is `{action, name, schedule, ...}` → target = `<action>:<name>`
- `subagents`: `{action, agentId}` → target = `<action>:<agentId>`
- `image` / `image_generate`: target = path or prompt-prefix

Read each tool's actual schema from openclaw dist before writing.

---

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear, multi-file but pattern-driven. Requires schema lookup and logic update, fits T1. No ambiguity or need for further breakdown.

Implementation steps:
- Review openclaw dist to obtain actual schemas for cron, subagents, image, and image_generate tools.
- Update normalization logic to extract granular target fields per tool type as described.
- Implement target formatting: cron as <action>:<name>, subagents as <action>:<agentId>, image/image_generate as path or prompt-prefix.
- Refactor mapping logic to use new granular targets instead of toolName literal.
- Add/adjust tests to verify correct extraction and mapping for each tool type.

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear, multi-file but pattern-driven. Requires schema lookup and logic update, fits T1. No ambiguity or need for further breakdown.

Implementation steps:
- Review openclaw dist to obtain actual schemas for cron, subagents, image, and image_generate tools.
- Update normalization logic to extract granular target fields per tool type as described.
- Implement target formatting: cron as <action>:<name>, subagents as <action>:<agentId>, image/image_generate as path or prompt-prefix.
- Refactor mapping logic to use new granular targets instead of toolName literal.
- Add/adjust tests to verify correct extraction and mapping for each tool type.

### `task-validate-command-pre-activity-gate`

```yaml
id: task-validate-command-pre-activity-gate
tier: T3
status: ready
estimated_loc: 200
blocks: []
file: go/execution-kernel/cmd/chitin-kernel/main.go (new subcommand)
references_spec: docs/superpowers/specs/2026-04-30-local-worker-design-addendum.md
```

Spec addendum says: "Before Temporal dispatches the activity, chitin validates
the request — `chitin-kernel task validate <req.json>` — and may narrow
`allowed_drivers`." Subcommand doesn't exist yet. Slice 1 `submit.ts` zod-
parses locally and posts straight to Temporal — no policy narrowing.

Needs:
1. New `task` subcommand group with `validate` (and later `submit`)
2. Reads ExecutionRequest from stdin or file
3. Returns narrowed request (or rejection) on stdout
4. Wire `submit.ts` to shell out to it before `client.workflow.start`
5. Tests for narrow / reject / passthrough cases

T3 because cross-cutting (Go kernel + TS submit + Temporal flow + spec
alignment).

---

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear: new CLI subcommand, Go/TS integration, and test cases. Cross-cutting but well-defined; ready for T3 claim.

Implementation steps:
- Add new 'task' subcommand group to chitin-kernel CLI in Go
- Implement 'validate' subcommand: read ExecutionRequest from stdin/file, apply policy narrowing logic
- Output narrowed or rejected request to stdout in correct format
- Update submit.ts to shell out to 'chitin-kernel task validate' before Temporal workflow start
- Write tests for narrow, reject, and passthrough scenarios
- Align implementation with referenced spec addendum

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear: new CLI subcommand, Go/TS integration, and test cases. Cross-cutting but well-defined; ready for T3 claim.

Implementation steps:
- Add new 'task' subcommand group to chitin-kernel CLI in Go
- Implement 'validate' subcommand: read ExecutionRequest from stdin/file, apply policy narrowing logic
- Output narrowed or rejected request to stdout in correct format
- Update submit.ts to shell out to 'chitin-kernel task validate' before Temporal workflow start
- Write tests for narrow, reject, and passthrough scenarios
- Align implementation with referenced spec addendum

### `chitin-install-slice-3-agents`

```yaml
id: chitin-install-slice-3-agents
tier: T2
status: ready
estimated_loc: 80
blocks: []
file: go/execution-kernel/cmd/chitin-kernel/main.go (extend install)
```

PR #84's slice-3 demo required the operator to manually run
`openclaw agents add qwen-agent --model ollama/qwen3-coder:30b ...` and would
need the same for `glm-agent` and `deepseek-agent`. Reproducing this on every
new install is friction. Add a `chitin-kernel install --slice-3-agents`
flag (or `chitin-kernel openclaw bootstrap-agents`) that idempotently
ensures the three per-driver agents exist with the correct model bindings.

T2 because the right model per driver depends on local stack availability
(checking ollama / ollama-cloud / Copilot CLI presence and credentials).

---

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear: add a flag/command to automate agent setup, with logic for stack detection and idempotency. No further breakdown needed.

Implementation steps:
- Add a --slice-3-agents flag to chitin-kernel install (or a new bootstrap-agents command).
- Detect local model stack availability (ollama, ollama-cloud, Copilot CLI, credentials).
- For each agent (qwen, glm, deepseek), determine the correct model binding based on stack.
- Check if each agent already exists; if not, create it with the correct model.
- Ensure idempotency: re-running does not duplicate or misconfigure agents.
- Add logging for actions taken and skipped.
- Test with various stack configurations to verify correct agent setup.

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear: add a flag/command to automate agent setup, with logic for stack detection and idempotency. No further breakdown needed.

Implementation steps:
- Add a --slice-3-agents flag to chitin-kernel install (or a new bootstrap-agents command).
- Detect local model stack availability (ollama, ollama-cloud, Copilot CLI, credentials).
- For each agent (qwen, glm, deepseek), determine the correct model binding based on stack.
- Check if each agent already exists; if not, create it with the correct model.
- Ensure idempotency: re-running does not duplicate or misconfigure agents.
- Add logging for actions taken and skipped.
- Test with various stack configurations to verify correct agent setup.

### `openclaw-tool-coverage-audit`

```yaml
id: openclaw-tool-coverage-audit
tier: T1
status: ready
estimated_loc: 40
blocks: []
file: docs/observations/2026-05-XX-openclaw-tool-coverage.md (new)
```

Slice 3a + 3-fix mapped 21 openclaw tool names. PR #84's adversarial pass
caught that `web_search` / `web_fetch` (plain forms) were missing. Other
extensions might register tools we haven't enumerated. Write a script that
greps openclaw's dist for `name: "[a-z_]+"` in tool-registration call sites,
diffs against `gov.Normalize`'s switch cases, and reports missing mappings.
Run it as a CI check eventually.

---

**Groomed 2026-05-02 (0.95 confidence):** The scope is clear, single-purpose, and multi-file but not complex. Steps are concrete and fit T1. No further breakdown needed.

Implementation steps:
- Grep openclaw's dist for tool-registration call sites with name: "[a-z_]+"
- Extract all tool names found in registration calls
- Parse gov.Normalize's switch cases to collect mapped tool names
- Diff the two sets to find unmapped tool names
- Output a report listing missing mappings

**Groomed 2026-05-02 (0.95 confidence):** The scope is clear, single-purpose, and multi-file but not complex. Steps are concrete and fit T1. No further breakdown needed.

Implementation steps:
- Grep openclaw's dist for tool-registration call sites with name: "[a-z_]+"
- Extract all tool names found in registration calls
- Parse gov.Normalize's switch cases to collect mapped tool names
- Diff the two sets to find unmapped tool names
- Output a report listing missing mappings

### `swarm-shared-memory-spike` (decomposed)

```yaml
id: swarm-shared-memory-spike
status: decomposed
decomposed_into: [event-chain-query-api, session-context-injector, failure-mode-logging]
decomposed_at: 2026-05-02
```

Today's swarm: each workflow is a fresh agent with no memory of previous
runs. Real cost to find out: qwen redoes setup work / re-fetches context /
re-derives decisions every invocation. claude-mem (or similar — chitin's
existing event chain has the data already, just not the retrieval API) is
the most defensible answer. Spike: query the chain for "what did this agent
last do for this repo" and inject as session-start context. T2 because the
right shape depends on what failure modes show up first — needs a week of
real swarm runs before we know.

---

**Groomed:** The spike's scope is cross-cutting and exploratory; needs decomposition into API, injection, and failure analysis before a concrete, claimable task emerges.

**Groomed:** The spike's scope is cross-cutting and exploratory; needs decomposition into API, injection, and failure analysis before a concrete, claimable task emerges.

### `event-chain-query-api`

```yaml
id: event-chain-query-api
tier: T1
status: in_design
parent: swarm-shared-memory-spike
```

Expose API to query event chain for agent/repo history

(Decomposed from `swarm-shared-memory-spike` on 2026-05-02.)

### `session-context-injector`

```yaml
id: session-context-injector
tier: T1
status: in_design
parent: swarm-shared-memory-spike
```

Inject retrieved memory into agent session start

(Decomposed from `swarm-shared-memory-spike` on 2026-05-02.)

### `failure-mode-logging`

```yaml
id: failure-mode-logging
tier: T2
status: in_design
parent: swarm-shared-memory-spike
```

Log and analyze failure modes from real swarm runs

(Decomposed from `swarm-shared-memory-spike` on 2026-05-02.)

---

### `rename-local-cloud-driver-misnomer`

```yaml
id: rename-local-cloud-driver-misnomer
tier: T2
status: ready
estimated_loc: 60
blocks: []
file: libs/contracts/src/execution-request.schema.ts, apps/temporal-worker/src/activity.ts, openclaw agent config
```

`local-glm` and `local-deepseek` driver ids are misnomers — `glm-5.1:cloud`
runs through Ollama Cloud and `deepseek` (in our setup) routes via cloud
too, neither is "local" in the same sense as `local-qwen` (which actually
runs on the 3090). Renaming options:

- `cloud-glm`, `cloud-deepseek` — paired with `local-qwen` keeps the
  prefix discoverable but introduces a third axis (cost / latency tier
  vs locality) the prefix doesn't fully capture.
- `glm`, `deepseek` (no prefix) + keep `local-qwen` as an exception —
  cleanest but breaks the convention.
- Tier-suffix vocabulary entirely: drop `local-*` and just name agents
  by model (`qwen-coder`, `glm-cloud`, `deepseek-cloud`) — biggest
  rename surface area, cleanest end state.

Touches: `DriverIdSchema` enum, `DRIVER_AGENT_MAP` in activity.ts, the
per-driver agent ids in openclaw config (`qwen-agent`, `glm-agent`,
`deepseek-agent` may also need renaming for consistency), CHITIN_AGENT_*
env var keys, all activity tests, `swarm-backlog.md` tier definitions
above. T2 because of the breadth (multi-file rename + downstream env
var docs).

---

## Research-informed (added 2026-05-02 from SoTA + OpenClaw survey)

These entries are derived from two observation docs that ship in PR #107
(not yet on `main`): `docs/observations/2026-05-02-self-improving-swarm-sota.md`
and `docs/observations/2026-05-02-openclaw-usage-survey.md`. To read them,
either merge #107 first or `gh pr checkout 107`. Status `in_design`
until the user reviews and promotes — they touch architecture and need a
human green-light.

### `role-typed-backlog-entries`

```yaml
id: role-typed-backlog-entries
tier: T2
status: completed
shipped_in: PR #130 (Phase 1 of swarm-as-software-factory; design doc PR #129)
estimated_loc: 200
blocks: []
file: apps/temporal-worker/src/grooming/parse-backlog.ts, apps/temporal-worker/src/dispatcher.ts, docs/swarm-backlog.md
```

> **✅ COMPLETED 2026-05-02 in PR #130.** Vocabulary landed slightly
> different from this entry's draft (`research`/`fix`/`refactor`/...
> were work-shape labels; the design doc reframed them as agent
> ROLES — programmer/reviewer/researcher/etc.). See
> `docs/design/2026-05-02-swarm-as-software-factory.md` §3 for the
> final taxonomy and `apps/temporal-worker/src/role-prompts.ts` for
> the role-prompt registry. Per-role prompt templates beyond
> `programmer` are stubs in this slice — follow-up entries (one per
> role) flesh out the dedicated prompts.

Add a `role:` field to backlog entries. Initial role vocabulary:
`research`, `fix`, `refactor`, `test`, `doc`, `gov`. Dispatcher
selects per-role prompt templates instead of one generic prompt (the
current generic prompt is biased toward "read-then-edit" which is
wrong for research-style entries). Roles also let us route per-role
to per-role tier defaults (e.g., research → T2 minimum because we
want WebSearch).

Why: the SoTA review (Live-SWE-agent, Lobster + OpenClaw multi-agent
pipeline) shows role-typed workers are the next leverage point. Right
now every entry runs through the same prompt, which is why the swarm
can't yet handle research tasks well.

Implementation steps:
- Extend `BacklogEntry` with optional `role` field
- Add per-role prompt builders to dispatcher (extract `buildPrompt`
  → `buildPromptForRole`); fall back to generic if role missing
- Document the role vocabulary in this backlog file's header
- Update existing entries to include role retroactively (tedious
  but small)

### `lessons-learned-sidecar`

```yaml
id: lessons-learned-sidecar
tier: T1
status: in_design
estimated_loc: 80
blocks: [role-typed-backlog-entries]
file: docs/swarm-lessons.md (new), apps/temporal-worker/src/grooming/apply-workflow-result.ts, apps/temporal-worker/src/dispatcher.ts
```

After every merged swarm PR, the apply step distills one sentence
("when X file pattern, prefer Y") and appends to `docs/swarm-lessons.md`.
Dispatcher prepends recent lessons (top N) to the prompt so workers
benefit from past runs without burning tokens on full context.

> Implementation note: `apply-workflow-result.ts` currently skips
> auto-committing untracked-only changes (`git diff --shortstat` check)
> and skips push/PR when there are no existing commits. The first lesson
> append on a fresh worktree would be dropped. Mitigation: commit an
> empty (tracked) `docs/swarm-lessons.md` as part of this entry's
> first PR so subsequent appends produce trackable diffs, OR add an
> explicit-track branch in the apply heuristic for this file. Decide
> when implementing.

Why: the SoTA review highlighted "long-term memory + global
observability" as the leader-agent pattern. Chitin already has the
event chain — this surfaces a digestible slice of it back into worker
prompts. Cheap, high-yield. Live-SWE has the analogous tool registry;
this is our equivalent for declarative knowledge.

Implementation steps:
- Add `appendLesson(entry, prMeta)` after successful PR open
- Prepend `## Recent lessons` section to dispatcher prompt (top 10)
- Cap file size; rotate when over (e.g., 200 lessons → trim oldest)

### `eval-harness-wiring`

```yaml
id: eval-harness-wiring
tier: T2
status: in_design
estimated_loc: 250
blocks: []
file: apps/temporal-worker/src/eval/ (new dir)
```

Periodic chitin-internal eval. Pick a reference set of past merged
swarm PRs, replay each entry through the dispatcher in sandbox mode,
score the diff against the merged version. Detects swarm regressions
before they ship — if next week's `dispatcher-prompt-relative-path-prefix`
re-run produces a worse diff, we know prompt drift happened.

Why: SoTA notes chitin lacks autonomous evaluation. Without it,
"swarm-quality regressed last week" is a vibes-based judgment.
Live-SWE has SWE-bench replay; M2.7 has eval-suite delta. We need
something analogous, even if rough.

Implementation steps:
- Define `EvalCase` schema (entry, expected merged diff, base ref)
- `eval/run-suite.ts` replays via dispatcher in `--sandbox` mode
  (workflow runs, no PR opened — just compute diff vs expected)
- Diff scorer: structural (files changed match) + content
  (line-level Jaccard on the diff body)
- `/evolve` skill (already exists) gets a `--eval` mode

### `multi-step-flows`

```yaml
id: multi-step-flows
tier: T3
status: partial
shipped_in: PR #130 (schema fields only; orchestration in Phase 2)
estimated_loc: 400
blocks: [role-typed-backlog-entries]
file: apps/temporal-worker/src/dispatcher.ts, apps/temporal-worker/src/workflow.ts
```

> **🟡 PARTIAL 2026-05-02 in PR #130.** The schema fields
> (`parent_workflow_id`, `step_index` with cap=3) landed in
> `libs/contracts/src/execution-request.schema.ts` so future
> consumers can construct multi-step requests. The dispatcher's
> `dispatchSubtask(entry, parent)` orchestration path is **not yet
> wired** — that lands with `review-graph-executor` in Phase 2 since
> the review escalation graph is the first real multi-step user. See
> `docs/design/2026-05-02-swarm-as-software-factory.md` §4 + §9
> Phase 2.

One backlog entry can spawn N sub-tasks via the same dispatcher.
Programmer-then-reviewer is the simplest case. Lobster's
`loop.maxIterations` is the prior art (see openclaw-usage-survey).
Sub-tasks share a parent workflow id so the chain is auditable.

Why: SoTA review identified single-step entries as a chitin gap. The
deterministic substrate pattern (YAML plumbing, LLM creative work) is
what the OpenClaw + Lobster community converged on; we should match.

Implementation steps:
- Extend ExecutionRequest with optional `parent_workflow_id` and
  `step_index`
- Dispatcher adds a `dispatchSubtask(entry, parent)` path
- Cap depth (e.g., 3) to prevent runaway chains
- Document the multi-step pattern in `docs/swarm-backlog.md` header

### `openclaw-mission-control-otel-hookup`

```yaml
id: openclaw-mission-control-otel-hookup
tier: T2
status: in_design
estimated_loc: 150
blocks: []
file: apps/temporal-worker/src/activity.ts, libs/telemetry/src/ (existing)
```

Emit OTEL spans in the format expected by `abhi1693/openclaw-mission-control`
(the OpenClaw-ecosystem fleet dashboard). Per the OTEL emit-direction
memo (locked 2026-04-29), chitin already emits spans; this entry
specifically aligns the span attribute schema with mission-control's
expected fields so we get a free fleet view.

Why: openclaw-usage-survey identified mission-control as the
ecosystem dashboard. Wiring chitin's spans to its schema is cheap and
gives the user a richer monitoring surface than journalctl + Slack.

Implementation steps:
- Read `mission-control` README for span attribute expectations
- Map chitin's existing `gov.decision`, `swarm.dispatch`, `worker.activity`
  spans to mission-control's vocabulary
- Document the mapping in `docs/observability/mission-control.md`
- Smoke-test in dev (one local mission-control instance)

### `openclaw-temporal-issue-10164-public-comment`

```yaml
id: openclaw-temporal-issue-10164-public-comment
tier: T3
status: in_design
estimated_loc: 80
blocks: []
file: docs/outreach/2026-openclaw-issue-10164-comment.md (new)
```

OpenClaw closed issue #10164 (native Temporal integration) as not
planned. Chitin's three-plane architecture is exactly the answer
upstream's users were asking for. This entry produces a comment on
that closed issue (non-spammy, factual) pointing at chitin as a
community-built option. (~80 LOC, mostly outreach prose.)

Why: openclaw-usage-survey identified this as the strategic outreach
opportunity. Public visibility, free distribution, aligns with
positioning chitin as policy + audit + durable workflows for the
OpenClaw ecosystem.

T3 because it's outreach prose with strategic implications — not
mechanical. User reviews the comment text before posting.

Implementation steps:
- Draft comment in `docs/outreach/...` (this branch)
- User edits
- User posts (chitin doesn't auto-comment on external repos)

### `chitin-readme-positioning-rewrite`

```yaml
id: chitin-readme-positioning-rewrite
tier: T2
status: in_design
estimated_loc: 100
blocks: []
file: README.md
```

Per openclaw-usage-survey, chitin's positioning should lead with
"policy and audit plane for AI coding agents, built to compose with
OpenClaw + Temporal" — not "execution kernel" (which sounds like a
runtime competitor).

Why: ecosystem framing matters before the 2026-05-07 talk. The
"execution kernel" framing was right when chitin had no upstream peer;
now OpenClaw owns runtime, and chitin's wedge is what OpenClaw said
no to (durable workflows + governance).

Implementation steps:
- Rewrite README hero sentence + subhead
- Add an "ecosystem" section diagramming chitin's relationship to
  OpenClaw + Temporal
- Cross-reference openclaw-usage-survey doc

### `playwright-driver-prototype`

```yaml
id: playwright-driver-prototype
tier: T3
status: in_design
estimated_loc: 600
blocks: []
file: apps/temporal-worker/src/drivers/ (new), libs/contracts/src/execution-request.schema.ts
```

Add a Playwright-based browser driver so the swarm can interact with
authenticated web UIs (NotebookLM, internal dashboards, etc.) under
the same gate machinery. Requires:

- Headed Playwright session running under user auth
- Driver shim that translates `ExecutionRequest` browser actions to
  Playwright primitives
- Gate hook for `browser.navigate`, `browser.click`, `browser.extract`

Why: user explicitly asked for "building artifacts using my notebooklm
account via playwright." Playwright is the right substrate for this
class of work; NotebookLM ingestion is a downstream consumer.

T3 because of breadth + auth handling complexity. Subentry breakdown
needed before claimable: separate auth-bootstrap from action API.

Implementation steps (high-level):
- Add `browser` driver id to `DriverIdSchema`
- Bootstrap a persistent Playwright user-data-dir for auth
- Action API: `navigate`, `click`, `type`, `extract_text`, `screenshot`
- Gate rules for browser actions (allowlist of domains)
- One smoke test against a public site (e.g., chitin's own GitHub)

### `notebooklm-ingest-via-playwright`

```yaml
id: notebooklm-ingest-via-playwright
tier: T3
status: in_design
estimated_loc: 300
blocks: [playwright-driver-prototype]
file: apps/temporal-worker/src/integrations/notebooklm/ (new)
```

Use the playwright driver to upload a markdown source to NotebookLM,
trigger summary generation, and pull the resulting artifact (audio
overview, FAQ, study guide). Output goes into the workspace-level
`/wiki` skill (defined in the parent workspace's `CLAUDE.md`, not in
this repo) as a digestible source.

Why: user asked for this directly. NotebookLM produces dense
artifacts (audio overviews especially) that are useful for solo
review of long docs. Wiring chitin to drive it = automated source
distillation pipeline.

T3 because of UI-fragility and auth handling. Likely requires retry
+ flake-tolerance from the start.

Implementation steps:
- After playwright-driver-prototype lands, write the NotebookLM
  flow: upload, wait, click "audio overview", wait, download
- Output as a chain event so the audit trail captures the artifact

### `soul-md-schema-alignment`

```yaml
id: soul-md-schema-alignment
tier: T2
status: in_design
estimated_loc: 120
blocks: []
file: souls/canonical/*.md, docs/souls/schema.md (new)
```

The OpenClaw ecosystem standardized on `SOUL.md` files for agent
templates (162 templates in awesome-openclaw-agents). Chitin already
uses `souls/canonical/<name>.md` with frontmatter — likely close to
their schema. Diff our schema against theirs, document migration
notes, optionally publish a chitin-specific extension.

Why: ecosystem alignment cheap-wins. If our soul schema matches
SOUL.md, chitin souls can be contributed to the awesome-openclaw-agents
registry directly. Free distribution.

Implementation steps:
- Read `awesome-openclaw-agents` SOUL.md schema
- Diff against our frontmatter
- Document delta in `docs/souls/schema.md`
- If the gap is small, write a migrator or align the next soul
  edit to the unified schema

---

## Strategic / user-only (T4)

These need Jared + Claude Code interactive — too ambiguous for any tier
below to groom further.

- **Slice 4 scope decision** — what's after slice 3? The roadmap-as-shipped
  doesn't define a slice 4. Options on the table: Copilot CLI v2 spike
  (post-talk per memory), terrain-B compute-fabric, A2/A4 audience expansion.
  Strategy call, not swarm work.
- **OTEL semconv full compliance** — `gen_ai.*` deferred per roadmap. Big
  scope, business value depends on talk reception.
- **octi v2 spec edits** — pre-plan-handoff, listed in roadmap deferred.

---

## Recently shipped (drop after 2 sprints)

- `slice-1-temporal-worker` — PR #81, merged 2026-05-01
- `slice-2-openclaw-plugin` — PR #81 (same), merged 2026-05-01
- `pr-81-tos-driver-fix` — `claude-code` removed from `DriverIdSchema`,
  PR #81 commit
- `slice-3a-pi-runtime-core-tools` — PR #83, merged 2026-05-01
- `slice-3-chat-domain-and-routing` — PR #84, merged 2026-05-01
- `slice-4-grooming-agent` — PR #92, merged 2026-05-02
- `slice-5-swarm-worktree` — PR #93, merged 2026-05-02
- `slice-5b-claude-code-headless` — PR #95, merged 2026-05-02 (corrected
  the 2026-04-30 ToS misread; brought claude-code back as a worker driver)
- `slice-6-cheaper-driver-gating-and-tier-routing` — PR #96, merged
  2026-05-02 (closed the audit-gap finding from slice 5b)
- `gov-policy-allow-pr-merge` — PR #97, merged 2026-05-02 (manual; can't
  go through swarm by self-governance rule)
- Closed from issue #82: `#4 driver-id-contract-theater` (slice 3b),
  `#13 normalizer-informational` (PR #83)
- Closed audit-gap PR #94 — superseded by #97 (its content was correct
  but it was produced by an unaudited slice-5b run, before slice 6 fixed
  the cwd-scoped hook gap)

---

## Tier counts (snapshot 2026-05-02 post slice-6 merge)

```
T0 ready:    4   (decision.params, workflow-name-drift, repo-regex, read/read_file alias)
T1 ready:    3   (tools-summary, cron-targets, openclaw-tool-coverage)
T2 ready:    3   (wall-timeout-sigkill, install-slice-3-agents, rename-cloud-misnomer)
T3 ready:    1   (task-validate command)
T1 in_design: 2  (event-chain-query-api, session-context-injector — sub-entries
                  of swarm-shared-memory-spike)
T2 in_design: 1  (failure-mode-logging — same parent)
T4 strategic: 3  (slice-7 scope, OTEL semconv, octi v2)
T5 only:     ∞  (governance-config edits, ambiguous strategy)
```

**Recommended next-session sequence (cheap → expensive):**

1. **Drain T0 ready (4 entries)** via `local-qwen` — free, ~5 min each.
   The slice-6 worktree path makes this straightforward; one workflow
   per entry, apply step PRs each. Each PR is single-file mechanical.
2. **Run a grooming pass on the T1/T2 in_design sub-entries** to flesh
   out implementation steps so they're claimable.
3. **Pick one T2 ready** — `wall-timeout-sigkill` is highest value
   because it unblocks slow models from timing out at Temporal's
   15-min cap. `rename-cloud-misnomer` is lower value but smaller.

**How to dispatch a T0 from this backlog:**

```bash
# Worker must be running:
CHITIN_REPO_ROOT=/home/red/workspace/chitin \
  pnpm exec tsx apps/temporal-worker/src/worker.ts &

# Submit:
PROMPT='<from swarm-backlog entry implementation_steps>' \
WORKFLOW_ID=swarm-<entry-id>-$(date +%s) \
BASE_REF=main DRIVER=local-qwen TIER=T0 \
WALL_TIMEOUT_S=120 MAX_TOOL_CALLS=10 \
  pnpm exec tsx apps/temporal-worker/src/submit.ts

# Apply:
pnpm exec tsx apps/temporal-worker/src/grooming/apply-workflow-result.ts \
  --result tmp/result-<workflow-id>.json --apply
```

## Remaining design-doc roles (filed 2026-05-02)

These are the §3 station-taxonomy roles the framework supports but
hasn't authored a dedicated prompt template / runner for yet. The
chain is plumbable (role: field on entries routes through the
dispatcher's role-prompts.ts registry), but each role needs (a) a
prompt template + structured-output parser parallel to
reviewer-prompts.ts / researcher-prompts.ts, and (b) a runner if the
role's work is operator-triggered or cron-driven (vs entry-driven).

### `product-role-prompt-template`

```yaml
id: product-role-prompt-template
tier: T2
status: in_design
estimated_loc: 150
blocks: []
file: apps/temporal-worker/src/product-prompts.ts, apps/temporal-worker/src/role-prompts.ts, apps/temporal-worker/test/product-prompts.test.ts
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §3 (product role)
role: programmer
```

Author the product-role's prompt template + structured-output parser
parallel to reviewer-prompts.ts / researcher-prompts.ts. Job per the
design's §3 row: "turn raw signals into 1-paragraph problem
statements with success criteria."

Scope:

1. `buildProductPrompt({raw_signal, context, audience})` — pure prompt
   builder. Inputs: a raw signal blob (e.g., a roadmap candidate
   summary, a debt-ledger entry description, an alarm digest); the
   chitin context the operator already has (the design doc layer);
   the audience (operator vs another swarm role).
2. `<<<PROBLEM_STATEMENT>>>{...}` structured emit:
   `{ statement: string, success_criteria: string[], owner_role: 'programmer' | 'researcher' | 'qa' | ... }`
3. Wire into role-prompts.ts so an entry with `role: product` builds
   from this template instead of the generic stub.
4. 12-15 tests parallel to reviewer-prompts.test.ts: schema, parser
   cases (well-formed / missing marker / malformed JSON / SKIP),
   prompt rendering with all input shapes.

LLM swap: heuristic v1 not useful here (turning raw signal into
success criteria is judgment); product role goes straight to LLM
via the existing dispatcher role flow. Cost shape per haiku:
~$0.001 per dispatch.

### `architect-role-prompt-template`

```yaml
id: architect-role-prompt-template
tier: T2
status: in_design
estimated_loc: 180
blocks: []
file: apps/temporal-worker/src/architect-prompts.ts, apps/temporal-worker/src/role-prompts.ts, apps/temporal-worker/test/architect-prompts.test.ts
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §3 (architect role)
role: programmer
```

Author the architect-role's prompt template. Job per the design's §3
row: "write `docs/design/<entry-id>.md` ADRs: context / options /
decision / tradeoffs."

Scope:

1. `buildArchitectPrompt({entry, problem_statement, prior_adrs})` —
   reads the entry + the product-role's problem_statement + the
   index of existing ADRs in `docs/design/`; emits an ADR draft.
2. Structured emit (`<<<ADR>>>`-marked): `{ context: string, options: [...], decision: string, tradeoffs: string }`
3. Apply step writes the ADR to `docs/design/<entry-id>.md`.
4. 12-15 tests covering schema, parser, prompt rendering, ADR-file
   write logic.

Blocks `qa-automation-from-merged-diff` (which references the ADR's
"success_criteria" to know what to test).

### `qa-automation-from-merged-diff`

```yaml
id: qa-automation-from-merged-diff
tier: T3
status: in_design
estimated_loc: 250
blocks: [architect-role-prompt-template]
file: apps/temporal-worker/src/qa-prompts.ts, apps/temporal-worker/src/role-prompts.ts, apps/temporal-worker/test/qa-prompts.test.ts
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §3 (qa role) + Phase 4
role: programmer
```

Author the qa-role's prompt template + diff-driven test generation.
Job per the design's §3 row: "generate or run E2E tests against
shipped diffs; smoke-test."

Scope:

1. `buildQaPrompt({pr_url, diff, success_criteria, surface_kind})` —
   reads the PR diff + the architect-emitted success_criteria + an
   inferred surface kind (CLI / HTTP / Python module / TS module);
   asks the agent to author Playwright / curl / vitest test code.
2. Structured emit (`<<<QA_TESTS>>>`-marked):
   `{ test_files: [{ path: string, content: string }], smoke_command: string }`
3. Apply step writes the new test files into the PR's branch (or
   an adjacent branch the gatekeeper-merge will fold in).
4. 15-20 tests covering schema, parser, surface-detection, prompt
   rendering, file-write logic.

T3 because writing tests at the integration level needs Sonnet-class
reasoning for surface inference.

### `r0-copilot-wait-on-review-graph-kickoff`

```yaml
id: r0-copilot-wait-on-review-graph-kickoff
tier: T2
status: in_design
estimated_loc: 90
blocks: []
file: apps/temporal-worker/src/review-graph-dispatch.ts, apps/temporal-worker/test/review-graph-dispatch-r0-wait.test.ts
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §5 (R0 fires before R1)
role: programmer
```

Add the R0 Copilot-bot wait the §5 escalation diagram describes.
Today reviewGraphWorkflow fires R1 immediately after the dispatcher
opens the PR; the design wants R0 (the GitHub Copilot bot review)
to land first so its inline comments are present in the
`copilot_comments` argument when R1 builds its prompt.

Scope:

1. New helper in review-graph-dispatch.ts: `waitForR0Review(prNumber,
   maxSeconds=300)`. Polls `gh pr view <num> --json reviews` every 15s
   until a review with `author.login` containing "copilot" appears,
   OR the timeout hits.
2. `enqueueReviewGraph` calls it before submitting reviewGraphWorkflow,
   then passes the formatted comments via copilot_comments. On
   timeout, proceeds with copilot_comments=undefined (current
   behavior — R0 just doesn't land in time, no harm).
3. Tests with a mock pollFn that returns the review on the Nth
   call, verifies the formatted comments shape.

Cap: 5 minutes. Beyond that, the operator's experience of "PR open
→ review chain runs" gets too laggy. The design's policy assumes
R0 typically lands within 60-120s; 5min is the upper bound.

### `investigate-bucket-b-regression`

```yaml
id: investigate-bucket-b-regression
tier: TBD
status: in_design
estimated_loc: TBD
blocks: []
file: TBD
references_signal: chitin-swarm-rollup alarms
role: researcher
```

Auto-filed by chitin-alarm-feeder.timer at 2026-05-02T19:46:35.246Z from a swarm-rollup alarm:

> BUCKET-B REGRESSION: 1/19 runs contaminated (5.3%) — PR #123 preflight may have regressed

Researcher role: read the alarm + the latest swarm-rollup JSON at `~/.cache/chitin/swarm-rollups/<YYYY-MM-DD>.json`; identify the root cause (recent dispatch failures, driver regressions, governance edits, etc); propose either a fix entry or `status: needs_human` if the cause is non-obvious. Operator: groom this entry once it has a real `tier` / `file:` / `estimated_loc`.

### `nx-affected-in-ci`

```yaml
id: nx-affected-in-ci
tier: T1
status: ready
estimated_loc: 80
blocks: []
file: .github/workflows/ci.yml, nx.json
references_design: nx affected docs / chitin's existing nx.json
role: programmer
```

CI doesn't use `nx affected`. `nx.json` is configured with the
TypeScript plugin (typecheck / build targets), but `.github/workflows/
ci.yml` runs `pnpm exec vitest run` against EVERYTHING on every PR,
plus `go test ./...` on every PR. Docs-only PRs trigger full test
suites; the marginal CI cost compounds across the 30+ PRs we ship a
day.

Scope:

1. CI workflow: replace bare `pnpm exec vitest run` with
   `pnpm exec nx affected -t test --base=origin/main` (or with
   `nx run-many` if affected detection isn't worth the complexity
   on this size repo).
2. Same for typecheck + lint targets if Nx isn't already routing
   those.
3. Keep `go test ./...` as-is unless someone files a separate entry
   to add Nx-Go integration.
4. Verify on a docs-only PR: should skip TS test step entirely.

Trade-off: Nx affected requires correct project boundaries (which
this repo has via `nx.json` + `package.json` per package). The
cost is one PR with 80-ish LOC of CI changes; the gain is faster
CI on routine PRs and a real "Nx-shaped" workflow that matches the
nx.json config we already wrote.

## Audit log: shipped entries (2026-05-02 ship session)

Many entries below this line have status: ready or in_design but
their work is actually merged. Rather than touching every entry's
status field (low-leverage edit), we record the shipped audit here
so the backlog itself stays small to scan. The dispatcher's
existing "skip — origin branch exists" check already prevents
re-dispatch of these.

| Entry id | Status field | Actual state | Shipped in |
|----------|--------------|--------------|------------|
| `dispatcher-respect-blocks-field` | ready | shipped | #144 |
| `review-graph-executor` | ready | shipped | #140 |
| `tech-debt-ledger` | ready | shipped | #137 |
| `debt-ledger-analysis-loader` | ready | shipped | #142 |
| `researcher-role-prompt-template` | ready | shipped | #143 |
| `chitin-researcher-systemd-units` | ready | shipped | #145 |
| `external-signal-fetchers` | ready | shipped | #147 |
| `analysis-swarm-runs-loader` | ready | shipped | #126 |
| `swarm-daily-rollup-healthcheck` | ready | shipped | #127 |
| `dispatcher-preflight-scrub-claude-settings-backup` | ready | shipped | (live in `dispatcher.ts` preflight + apply revert) |
| `normalize-decision-params-truthiness` | ready | shipped | #101 |
| `workflow-name-drift-test` | ready | shipped | (origin branch) |
| `dispatcher-prompt-relative-path-prefix` | ready | shipped | #105 |
| `dispatcher-prompt-scope-discipline` | ready | shipped | #106 |
| `activity-include-hook-events-flag` | ready | shipped | #108 |
| `repo-regex-tighten` | ready | shipped | #103 |
| `read-vs-read_file-file_path-alias` | ready | shipped | #113 |
| `wall-timeout-sigkill-propagation` | ready | shipped | #114 (+ rerun in #123) |
| `cron-subagents-image-granular-targets` | ready | shipped | #116 |
| `task-validate-command-pre-activity-gate` | ready | shipped | #117 |
| `chitin-install-slice-3-agents` | ready | shipped | #118 |
| `openclaw-tool-coverage-audit` | ready | shipped | #119 |
| `rename-local-cloud-driver-misnomer` | ready | shipped | #120 |
| `lessons-learned-sidecar` | in_design | shipped | #152 + #156 |
| `agent-adversarial-review-pass` | completed | already correct | #134 |
| `role-typed-backlog-entries` | completed | already correct | (Phase 1) |
| `external-signal-collector` | (SUPERSEDED) | already correct | superseded by 3 split entries |

Entries newly filed in this session (in_design, awaiting groomer):

- `product-role-prompt-template` (#161)
- `architect-role-prompt-template` (#161)
- `qa-automation-from-merged-diff` (#161)
- `r0-copilot-wait-on-review-graph-kickoff` (#161)
- `investigate-bucket-b-regression` (#162, auto-filed by alarm-feeder)
- `nx-affected-in-ci` (this commit)

Operator action item: the dispatcher's branch + marker checks already
keep these from re-dispatching. If you want a clean visual scan of
"what's left", filter the file by `status: ready` lines that are NOT
in the table above.

## Strategic roadmap entries (filed 2026-05-02)

These are not bite-sized backlog entries — they're substrate-level
investments where the OUTCOME is "lower-tier agents succeed at tasks
they'd otherwise need higher-tier (more expensive) agents for." The
positioning bet: chitin's edge isn't smarter agents, it's a better
HARNESS that makes cheaper agents succeed. Devin and Cursor make the
agent better. Chitin makes the substrate better — the determinism,
the tools, the memory, the recipes — so a copilot-tier agent solves
problems an Opus-tier agent used to need.

Each entry below is a roadmap seed: it should be groomed into smaller
scopes before the swarm can claim them.

### `agent-harness-substrate-roadmap`

```yaml
id: agent-harness-substrate-roadmap
tier: T5
status: in_design
estimated_loc: TBD
blocks: []
file: TBD (multiple — substrate-level work spans apps/, libs/, kernel)
references_design: docs/design/2026-05-02-swarm-as-software-factory.md (positioning) + this commit's PR description
role: architect
```

The strategic roadmap for "make lower-tier agents win." The
operating bet: success rate of a tier-N agent is dominated by
the harness it has, not the model. Concrete substrates worth
investing in (each becomes its own scope-down entry once an
architect prioritizes):

1. **Pre-canned recipe library** (extending `analysis.investigate`)
   - The analyst recipe pattern (#164) generalizes. Every role can
     have a recipe library: `qa.replay-failed-test`,
     `architect.draft-adr-from-entry`, `groomer.classify-tier`,
     `programmer.scaffold-test-file`. Same shape: deterministic CLI,
     structured emit, agent reports the result. The agent's job
     collapses from "figure out what to do" to "pick the right
     recipe and run it."

2. **Shared memory layer** (ClaudeMem-shape OR a chitin-native
   equivalent)
   - Today the only cross-run memory is `swarm-lessons.md` (text
     prepended to programmer prompts). That's lossy + LLM-distilled.
     A typed memory store (key-value or vector or graph) lets:
     - Reviewers cite past similar bugs by id
     - Programmers retrieve the EXACT prior diff that matched the
       current entry's pattern
     - Analysts reuse cached analysis results within a window
   - Open question: build it OR adopt? ClaudeMem exists but coupling
     chitin to it has lock-in risk. A chitin-native event-chain-
     backed memory might be the right substrate (the chain is
     already a memory; the missing piece is the typed retrieval API).

3. **MCP tool servers for the swarm's own workflows**
   - Right now agents have shell + edit + read. They could have
     typed MCP tools: `chitin.entry.read("<id>")`,
     `chitin.lesson.cite("<lesson-id>")`,
     `chitin.recipe.run("analyst.investigate", {...})`,
     `chitin.gov.gate.test(<command>)`. Typed tools turn the
     agent's prompt from "figure out the right shell command" into
     "call the typed function." Determinism goes up, success rate
     goes up.

4. **Better entry shape**
   - Today `BacklogEntry` is mostly free-form prose under a yaml
     header. Higher-determinism shape would include:
     `acceptance_criteria: ["test X passes", "no rule rendered changes"]`,
     `read_first: ["./apps/foo.ts", "./docs/design/..."]`,
     `prompt_examples: ["see how it was done in #142"]`. Closer to
     a Lobster-style YAML-first task definition.

5. **Implementor-side escalation telemetry**
   - The escalation feature shipped in this PR fires re-dispatch on
     commits=0. We need an observation pass after some soak: which
     entries are NEVER cleared by escalation? Those are the entries
     where ALL tiers (T1-T4) fail — meaning the entry itself is
     malformed, not the agent. The pattern matters: groomers should
     learn to spot those entry shapes before dispatch.

The architect role (when shipped) is the right home for this entry.
Operator: when picking up, decompose into 5+ T1-T2 scope-down
entries with concrete files + LOC estimates. Each substrate piece
ships independently; the "substrate" is the union, not a monolith.

### `agent-identification-fingerprinting-v2`

```yaml
id: agent-identification-fingerprinting-v2
tier: T3
status: in_design
estimated_loc: TBD
blocks: []
file: TBD (libs/adapters/claude-code/src/hook-context.ts, go/execution-kernel/internal/event/event.go, plus copilot + openclaw adapters)
references_design: docs/event-model.md (existing fingerprint surface) + GitHub issue #9
role: architect
```

The current fingerprinting surface is plumbed but shallow. Real
"this agent uniquely identifies as <X> with capabilities <Y>" doesn't
exist yet, which limits:

- **Per-agent telemetry slicing** — today every claude-code hook produces
  the same `agent_fingerprint` regardless of model / token budget /
  extension surface. Can't slice success rates by agent capability.
- **Cross-session continuity** — `agent_instance_id` is `randomUUID()`
  per HOOK CALL. Same Claude Code session emits dozens of different
  instance IDs. Should be stable per-session at minimum.
- **Provenance audit** — auto-merge gates can't prove "this PR was
  produced by an agent with capability set X" because the fingerprint
  doesn't encode the capability surface.

Today's surface (verbatim from `libs/adapters/claude-code/src/hook-context.ts`):
- `machine_fingerprint = sha256(hostname + uid + 'chitin-machine-fingerprint-v2')`
  — partially-stable but doesn't use systemd machine-id (issue #9
  names this exact gap).
- `agent_fingerprint = sha256({surface, machine, version})` — same value
  for every claude-code hook on this machine; useless for slicing.
- `agent_instance_id = randomUUID()` — fresh every hook.

What v2 should look like:

1. **Stable machine_fingerprint** — derive from `/etc/machine-id`
   (Linux) or `system_profiler` UUID (mac); fallback to current
   sha256 only when the canonical source is missing. Closes issue #9.

2. **Capability-encoding agent_fingerprint** — sha256 of:
   ```
   {surface, model_name, model_version, allowed_tools[], extension_pack[],
    chitin_kernel_version}
   ```
   Stable across runs of "the same configured agent"; differs when
   any capability dimension changes.

3. **Session-stable agent_instance_id** — first hook in a session
   generates the UUID; subsequent hooks read it from session_state.json
   (already exists in `<repo>/.chitin/`) instead of generating fresh.

4. **OTEL projection update** — the F4 projector adds
   `agent.fingerprint` and `machine.fingerprint` attributes (currently
   only `agent.id`). Required for downstream observability tools
   (mission-control etc.) to slice by fingerprint.

5. **Cross-surface parity** — copilot-cli + openclaw adapters need
   the same logic. Today only claude-code is wired; the others
   inherit empty fingerprint fields.

Why T3: capability-encoding requires reading model metadata from
each driver (claude-code, copilot, openclaw) — non-trivial cross-
surface work. Decompose into per-surface entries when an architect
prioritizes.

### `canon-ast-upgrade-mvdan-sh`

```yaml
id: canon-ast-upgrade-mvdan-sh
tier: T3
status: shipped
shipped_in: PR #173 (gov-bypass-hardening, follow-up commit)
estimated_loc: ~600 actual (canon/ast.go + ast_test.go + IsRemoteCodeExec bidirectional update)
blocks: []
file: go/execution-kernel/internal/canon/ast.go, gov/normalize.go (uses ParseAST)
references_design: gov-bypass-hardening PR + research finding mvdan.cc/sh/v3/syntax (BSD-3, 8.7k stars, active)
role: architect
```

The current `canon` package is tokenizer-grade — it splits on shell
separators (`&&`, `||`, `;`, `|`) and respects quotes, but has known
blind spots that show up as governance bypasses:

1. **Subshell descent** — `(rm -rf /)` is treated as a single token,
   not parsed as a subshell. An agent can wrap any destructive command
   in parentheses to evade detection.
2. **Process substitution** — `bash <(curl ...)` mangles the proc-subst
   tokens; `ContainsProcSubstFetch` is a regex-over-raw band-aid
   (closes the common case for #61 but only the common case).
3. **Command substitution** — `$(rm -rf /)` and backtick forms aren't
   descended into.
4. **Heredoc destinations** — `WriteDestinations` regex catches
   redirects+heredocs but the parsing is approximate; multi-line
   heredocs with quoted delimiters slip through.
5. **`bash -c "<string>"`** — re-parsing the string-literal argument of
   known shell-launchers requires recursive descent we don't do today.

`mvdan.cc/sh/v3/syntax` is the only Go library that produces a real
bash AST: typed `CmdSubst`, `ProcSubst`, `Subshell`, `Redirect`
(heredocs as `Op == Hdoc`), `CallExpr.Assigns` for env-prefix.
BSD-3-Clause, MIT/Apache-compatible.

Plan when picked up:

1. Vendor `mvdan.cc/sh/v3` as a kernel dep.
2. Add `canon.ParseAST(raw string) Pipeline` — same return type as
   existing `Parse`, different engine. AST walk recurses into every
   `CmdSubst`/`ProcSubst`/`Subshell` and emits each as its own
   pipeline segment so the bypass detectors fire on the inner command.
3. For known shell-launchers (`bash -c`, `sh -c`, `eval`), re-parse
   the string-literal argument and merge segments.
4. Switch `gov.classifyShellCommand` from `canon.Parse` → `canon.ParseAST`.
5. Re-run the bypass-detector test suite — every variation in
   `detectors_test.go` should pass against the AST variant, plus new
   cases for subshell, command-subst, proc-subst, heredoc, and `bash -c`.
6. Benchmark before/after: AST parsing is ~3-5× slower than tokenize,
   but per-call cost is microseconds, not milliseconds. Acceptable.

Why T3: vendoring + AST walk + re-targeting all bypass detectors is
~400 LOC plus integration. Worth doing because each bypass class the
AST closes is a security regression today, but not blocking the talk
demo (the regex/tokenizer-grade canon catches the demo cases).

Operator note when picking up: confirm mvdan/sh#256 (heredoc-in-procsubst
formatter bug) doesn't affect the parser path we use — only the
formatter is buggy per the upstream issue, but verify with a fixture
test before committing the upgrade.
