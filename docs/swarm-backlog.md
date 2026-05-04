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
status: partial
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
status: partial
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
status: partial
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
status: partial
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
status: partial
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
status: partial
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
status: partial
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
status: partial
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
status: partial
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
status: partial
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
status: partial
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
status: partial
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
status: partial
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
status: partial
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
status: partial
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

## Scheduler + MCP + Slack rollout (filed 2026-05-02)

Eight entries covering the scheduler, MCP server, and Slack integrations. Locked architecture in `docs/superpowers/plans/2026-05-02-scheduler-design.md`. Designed to run overnight as a parallel cohort; operator merges in dependency order tomorrow.

### `nx-angular-workspace-install`

```yaml
id: nx-angular-workspace-install
tier: T1
status: partial
estimated_loc: 30
blocks: []
file: package.json, pnpm-lock.yaml
references_design: docs/superpowers/plans/2026-05-02-scheduler-design.md §"PR sequence" PR-Z
role: programmer
```

Add `@nx/angular@^22.0.0` as a workspace devDependency to match the existing `@nx/js@22.6.5`. Single `pnpm add -D -w @nx/angular@22.6.5` followed by `pnpm install` to refresh `pnpm-lock.yaml`. No code changes — just the dependency. Verify with `pnpm exec nx list @nx/angular` after install (should show the schematic generators).

**Acceptance:**
- [ ] `package.json` devDependencies includes `@nx/angular@^22.6.5`
- [ ] `pnpm-lock.yaml` regenerated
- [ ] `pnpm exec nx list @nx/angular` lists generators
- [ ] CI green

This frees PR-D to scaffold the dashboard via `nx generate @nx/angular:application`.

### `scheduler-lib-foundation`

```yaml
id: scheduler-lib-foundation
tier: T2
status: ready
estimated_loc: 400
blocks: []
file: libs/scheduler/package.json, libs/scheduler/project.json, libs/scheduler/src/index.ts, libs/scheduler/src/schema.ts, libs/scheduler/src/store/sqlite.ts, libs/scheduler/tests/, apps/cli/src/commands/scheduler.ts, tsconfig.json
references_design: docs/superpowers/plans/2026-05-02-scheduler-design.md
role: programmer
```

Bootstrap the `@chitin/scheduler` library with Item schema + sqlite store + CLI subcommands. NO heuristic, NO ingest, NO notify — those are PR-C. Just the substrate.

Steps:
1. Generate library: `nx generate @nx/js:library scheduler --directory=libs/scheduler --tags=layer:scheduler,scope:lib --no-interactive`
2. Set `package.json` to mirror existing chitin libs: `name: @chitin/scheduler`, `type: module`, `main: ./src/index.ts`, `exports: {".": "./src/index.ts"}`, `private: true`.
3. Implement `src/schema.ts`: Item tagged variant + zod discriminatedUnion per design §"Item — tagged variant".
4. Implement `src/store/sqlite.ts`: ItemStore class with `add(item)`, `get(id)`, `list(filter)`, `update(id, patch)`, `delete(id)`. WAL mode (per #179 pattern). Uses `better-sqlite3`.
5. `src/index.ts` re-exports public API: types from schema, ItemStore + openStore from store.
6. Tests in `libs/scheduler/tests/`: schema parse/validate, store CRUD, WAL mode pragma set.
7. CLI: extend `apps/cli/src/commands/` with `scheduler.ts` exposing `add`, `list`, `complete`, `delete` subcommands.
8. Wire to `tsconfig.json` references.

**Acceptance:**
- [ ] `nx test scheduler` passes
- [ ] `chitin scheduler add --title "test" --type task` writes to sqlite
- [ ] `chitin scheduler list` reads back the item
- [ ] tsconfig references include the new lib
- [ ] CI green (no Angular touch — that's PR-D)

### `mcp-server-chitin-cli`

```yaml
id: mcp-server-chitin-cli
tier: T2
status: partial
estimated_loc: 400
blocks: []
file: libs/mcp-chitin/, apps/mcp-server/
references_design: docs/superpowers/plans/2026-05-02-scheduler-design.md (parallel track), Anthropic MCP spec
role: programmer
```

Wrap `chitin-kernel` CLI as an MCP server so any MCP client (Claude Code, Cursor, mobile Claude) can query/manage chitin state. Uses `@modelcontextprotocol/sdk` for the protocol layer.

Tools to expose (each shells out to `chitin-kernel`):
- `chitin_envelope_list` — list envelopes
- `chitin_envelope_grant` — grant additional budget
- `chitin_envelope_close` — close envelope
- `chitin_gate_status` — per-agent escalation level
- `chitin_gate_reset` — lockdown reset
- `chitin_chain_info` — chain state for a session
- `chitin_chain_verify` — Phase-1.5 stub verification
- `chitin_decisions_recent` — windowed decision log

Steps:
1. `nx generate @nx/js:library mcp-chitin --directory=libs/mcp-chitin --tags=layer:mcp,scope:lib`
2. `nx generate @nx/js:application mcp-server --directory=apps/mcp-server --tags=layer:mcp,scope:app`
3. Implement each tool in `libs/mcp-chitin/src/tools/<name>.ts` — pure TS functions that spawn `chitin-kernel` and return parsed JSON.
4. `apps/mcp-server/src/main.ts` boots `@modelcontextprotocol/sdk`'s stdio server and registers all tools.
5. Tests: each tool's argument validation + error path (kernel binary missing, parse failure).
6. README in `apps/mcp-server/` documenting the install path: `claude mcp add chitin path/to/dist/main.js`.

**Acceptance:**
- [ ] `chitin-mcp-server` binary launches via stdio
- [ ] Each tool returns valid MCP tool result envelope
- [ ] Error path covered (kernel exit non-zero → MCP error response with kind)
- [ ] CI green
- [ ] README has copy-paste install command for Claude Code

### `scheduler-gov-rule`

```yaml
id: scheduler-gov-rule
tier: T1
status: ready
estimated_loc: 50
blocks: []
file: chitin.yaml, .eslintrc.json (or eslint.config.js — match existing)
references_design: docs/superpowers/plans/2026-05-02-scheduler-design.md §"Gov rule"
role: programmer
```

Enforces the hard rule architecturally:

1. `chitin.yaml` gains:
   ```yaml
   - id: scheduler-heuristic-only
     action: file.write
     effect: deny
     target_regex: '^libs/scheduler/(?!src/(rank|ingest)\.ts$)'
     reason: "Swarm may tune rank.ts and ingest.ts only. Schema, store, notify, and the dashboard are operator-authored."
   ```
2. eslint config gains `@nx/enforce-module-boundaries` tag rules:
   ```jsonc
   {
     "depConstraints": [
       { "sourceTag": "scope:app",   "onlyDependOnLibsWithTags": ["scope:lib"] },
       { "sourceTag": "layer:scheduler", "onlyDependOnLibsWithTags": ["layer:scheduler", "layer:contracts"] },
       { "sourceTag": "layer:mcp", "onlyDependOnLibsWithTags": ["layer:mcp", "layer:contracts"] }
     ]
   }
   ```

**Acceptance:**
- [ ] `chitin.yaml` rule fires on a synthetic write to `libs/scheduler/src/store/sqlite.ts` (test via `chitin-kernel gate evaluate`)
- [ ] `chitin.yaml` rule allows a write to `libs/scheduler/src/rank.ts`
- [ ] Nx tag rule catches a synthetic import from `libs/scheduler/` into `libs/contracts/` (allowed) vs `apps/scheduler-dashboard/` (denied — apps can only depend on libs)
- [ ] CI green

### `scheduler-rank-ingest-notify`

```yaml
id: scheduler-rank-ingest-notify
tier: T2
status: partial
estimated_loc: 400
blocks: [scheduler-lib-foundation]
file: libs/scheduler/src/rank.ts, libs/scheduler/src/ingest.ts, libs/scheduler/src/notify.ts, libs/scheduler/src/notify/ntfy.ts, libs/scheduler/src/notify/slack.ts, libs/scheduler/tests/, apps/cli/src/commands/scheduler.ts
references_design: docs/superpowers/plans/2026-05-02-scheduler-design.md §"rank.next" §"ingest.parse"
role: programmer
```

Ship the swarm-tunable surface (rank + ingest) plus the notification adapter dispatch.

Steps:
1. `rank.ts` — greedy slot-picker v1: sort items by deadline urgency, slot into earliest open window matching `window_pref`. Emit `item_decision` chain events for telemetry. Pure function, no side effects.
2. `ingest.ts` — text → Item[] via Opus structured-output prompt. Few-shot examples for task/event/backlog parsing in the prompt. Returns `[]` + telemetry on parse failure (don't throw).
3. `notify.ts` — Notifier registry pattern. `register(name, fn)`, `dispatch(item, notifier_name)`.
4. `notify/ntfy.ts` — POST to `<NTFY_URL>/<topic>` with item summary.
5. `notify/slack.ts` — POST to incoming webhook URL.
6. CLI extends to: `chitin scheduler ingest "<text>"` (parse + persist), `chitin scheduler today` (rank + format), `chitin scheduler tick` (notify-due).
7. Tests: rank fixture-driven (assert slot ordering), ingest with mock Opus client, notify with mock fetch.

**Acceptance:**
- [ ] `chitin scheduler ingest "..."` produces structured items in store
- [ ] `chitin scheduler today` returns ranked slots
- [ ] `chitin scheduler tick --dry-run` lists what WOULD notify
- [ ] item_decision chain events visible in `chitin-kernel chain-info`
- [ ] CI green

### `scheduler-dashboard-angular`

```yaml
id: scheduler-dashboard-angular
tier: T3
status: ready
estimated_loc: 600
blocks: [nx-angular-workspace-install, scheduler-rank-ingest-notify]
file: apps/scheduler-dashboard/
references_design: docs/superpowers/plans/2026-05-02-scheduler-design.md §"Angular dashboard wiring"
role: programmer
```

Local Angular app on `localhost:3737`. Three views, one tiny Express server.

Steps:
1. `nx generate @nx/angular:application scheduler-dashboard --directory=apps/scheduler-dashboard --standalone --tags=layer:scheduler,scope:app --style=css --no-interactive`
2. Three routes: `/today` (timeline), `/inbox` (paste/dictate), `/edit/:id` (item detail).
3. `app/shared/services/scheduler.service.ts` wraps `@chitin/scheduler` library calls.
4. `apps/scheduler-dashboard/server.ts` — small Express:
   - `GET /api/today` → `rank.next()` result
   - `POST /api/items/ingest { text }` → `ingest()` result
   - `GET/PUT/DELETE /api/items[/:id]`
   - `POST /api/voice/transcribe` → multer multipart → whisper.cpp shell-out → text
5. Browser MediaRecorder → upload to `/api/voice/transcribe`.
6. systemd-timer notification dispatch (a lightweight bash wrapper around `chitin scheduler tick`).
7. README: how to run dev (`nx serve scheduler-dashboard`), how to run prod (`nx build` + node server.ts).

**Acceptance:**
- [ ] `nx serve scheduler-dashboard` starts on localhost:3737
- [ ] Today view renders today's slotted items
- [ ] Inbox accepts pasted text + transcribed voice → creates items
- [ ] Edit view supports complete + reschedule
- [ ] `nx build scheduler-dashboard` produces a deployable artifact
- [ ] CI green (lint + Angular component tests)

### `slack-l1-notifier`

```yaml
id: slack-l1-notifier
tier: T1
status: ready
estimated_loc: 150
blocks: [scheduler-rank-ingest-notify]
file: libs/scheduler/src/notify/slack.ts (extends), libs/scheduler/tests/, apps/cli/src/commands/scheduler.ts
references_design: docs/superpowers/plans/2026-05-02-scheduler-design.md §"Notifications"
role: programmer
```

Read-only Slack notifier — sends events from chitin to a Slack channel via incoming webhook. NO interactivity, NO commands, just outbound. Builds on the notify.ts dispatch pattern from PR-C.

Events to push:
- Scheduled item starts (5–15 min lead)
- Gov denial (when severity is high or escalation > 5)
- Lockdown trigger
- Swarm PR merged

Steps:
1. Extend `notify/slack.ts` to format Slack Block Kit messages for each event family.
2. Config in `~/.chitin/secrets/slack-webhook.url` (gitignored).
3. Hook into `chitin-kernel emit` via the F4 OnDecision callback for gov-decision events.
4. CLI: `chitin scheduler notify slack --test` smoke-tests the webhook.

**Acceptance:**
- [ ] Slack receives a formatted message for each event family
- [ ] `--test` flag succeeds without hitting the actual Slack channel
- [ ] Missing webhook URL gracefully no-ops
- [ ] CI green

### `slack-l2-actions`

```yaml
id: slack-l2-actions
tier: T3
status: partial
estimated_loc: 400
blocks: [mcp-server-chitin-cli]
file: apps/slack-app/, libs/mcp-chitin/ (uses)
references_design: docs/superpowers/plans/2026-05-02-scheduler-design.md (Slack L2 brief)
role: programmer
```

Two-way Slack app: receives slash commands and button clicks, calls into chitin via the MCP tools (already wrapped in PR-M).

Commands:
- `/chitin envelope-status` → list envelopes
- `/chitin envelope-grant <id> <calls>` → grant budget
- `/chitin gate-reset <agent>` → unlock
- `/chitin chain-info <session_id>`
- Buttons on L1 notification messages: "Reset lockdown" / "Grant +500 calls" / "Approve PR"

Steps:
1. `nx generate @nx/js:application slack-app --directory=apps/slack-app --tags=layer:slack,scope:app`
2. Slack Bolt setup with signed-request verification.
3. Each command handler calls into the corresponding `libs/mcp-chitin` tool function (don't re-implement; reuse).
4. ngrok or similar for dev (Slack needs a public URL); document the prod hosting story (Cloudflare Tunnel? small VPS?).
5. Tests: each command handler with mocked Slack request envelope.

**Acceptance:**
- [ ] `/chitin envelope-status` returns formatted envelope list
- [ ] Reset lockdown button on a denial message works end-to-end (mock test, plus dev ngrok manual verify)
- [ ] CI green

---

That's the cohort. PRs Z, B, M, E run in parallel tonight; tomorrow operator merges in order, the dispatcher picks up the next wave (C → D, S1, S2). All eight unblocked-or-soft-blocked.

### `pr-event-ingester`

```yaml
id: pr-event-ingester
tier: T2
status: partial
estimated_loc: 250
blocks: [comment-responder]
file: apps/temporal-worker/src/pr-event-ingester.ts (new), apps/temporal-worker/src/worker.ts (wire)
references_finding: 2026-05-03-review-graph-not-firing-on-non-dispatcher-prs
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §5
role: programmer
```

The review-graph (`reviewGraphWorkflow`, in production per factory
design §5) currently only runs on PRs the dispatcher itself opens —
`enqueueReviewGraph` is called from exactly one place,
`apps/temporal-worker/src/dispatcher.ts:711`, on the programmer-success
path. PRs opened by humans, by interactive Claude Code sessions, by
Copilot (when wired), or by any caller that's not the dispatcher
never trigger the §5 trigger matrix.

Concrete impact (2026-05-03 morning): four PRs (#196, #197, #198, #199)
opened overnight by interactive Claude Code sat with Copilot's R0
review comments unactioned. PR #199 had 6 inline comments — the §5
matrix says ">2 comments → escalate to R1" — but no review-graph was
ever enqueued because no programmer-dispatch ran.

Fix: a poller (or webhook receiver, but poller is cheaper for v1)
that watches GitHub PR events, evaluates each open PR against the §5
trigger matrix, and calls `enqueueReviewGraph` when a PR matches and
has no existing review-graph workflow.

Steps:
1. Create `apps/temporal-worker/src/pr-event-ingester.ts`. Polls
   `gh api repos/chitinhq/chitin/pulls?state=open` every 5 minutes
   (mirrors `chitin-dispatcher.timer` cadence — file the
   companion systemd timer separately or fold into the dispatcher
   tick).
2. For each open PR not authored by the dispatcher (i.e., no
   `swarm/` branch prefix), check if a review-graph workflow with
   id `${parent_workflow_id}-review-graph` already exists in
   Temporal. The `parent_workflow_id` for ingester-spawned graphs
   is `pr-ingest-<pr_number>` — see step 4 — giving a stable id
   per PR. Skip if the workflow already exists.
3. Read PR metadata: review comment count, diff size, files
   touched. Match against the §5 trigger matrix in
   `apps/temporal-worker/src/review-graph.ts: computeStartingTier`,
   which returns a `ReviewTier` (R0–R4). R0 means "no chitin
   dispatch needed; Copilot's server-side review covers it"; R4
   means "ping operator." R1–R3 are the dispatchable tiers.
4. Synthesize a `BacklogEntry`-shaped object for the existing
   `enqueueReviewGraph` API. The reviewer's *driver tier* (T0–T4)
   is what gets stamped on the BacklogEntry's `tier:` field — that
   value is read by the dispatcher's tier-driver map. The reviewer
   *review tier* (R0–R4) is computed inside the workflow from the
   same PR metadata via `computeStartingTier`, so it doesn't go on
   the BacklogEntry. Set `role: reviewer`, `file:` from the PR's
   changed-files list, `parent_workflow_id: pr-ingest-<pr_number>`.
5. `enqueueReviewGraph(...)`. The graph runs as it does today; the
   PR gets the same R1 → R2 → R3 escalation chain.
6. Write a chain event of kind `pr_ingest_decision` per evaluated
   PR (skipped / dispatched / errored) so audit can reconstruct
   which PRs hit the matrix and which didn't.

Tests:
- Unit: `pickPrsToIngest(prs, runningWorkflows)` returns the right
  set under various PR states (closed PR skipped, swarm-branch
  skipped, already-running graph skipped, qualifying PR included).
- Integration: with a fake `gh` and Temporal client, poller picks
  up #199-shape PR (6 comments, 1100 LOC, 1 layer:governance package)
  and calls `enqueueReviewGraph` once.

**Acceptance:**
- [ ] Ingester polls + matches §5 trigger matrix
- [ ] Existing review-graph workflows are not duplicated
- [ ] Chain event emitted per evaluated PR
- [ ] Backfill demo: running once against current open PRs picks up
      qualifying ones (e.g., #199 if still open) and starts review
- [ ] CI green

---

### `comment-responder`

```yaml
id: comment-responder
tier: T2
status: partial
estimated_loc: 350
blocks: []
file: apps/temporal-worker/src/role-prompts.ts (extend), apps/temporal-worker/src/comment-responder/* (new), libs/contracts/src/execution-request.schema.ts (extend RoleSchema)
references_finding: 2026-05-03-no-comment-responder-role
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §3 (role registry)
role: programmer
```

> **Dependency:** soft-blocked on `pr-event-ingester` shipping first
> — without the ingester, the responder has no upstream trigger for
> human/interactive-opened PRs. Backlog parser only reads `blocks:`
> (forward direction); `pr-event-ingester`'s entry above already
> declares `blocks: [comment-responder]`, which is sufficient. The
> dispatcher will not pick this up before its blocker.

The factory's `reviewer` role (R1-R3) produces *findings* on a PR.
There is no role responsible for *acting on* findings — pulling the
comments off the PR, evaluating each on merit, writing patches,
running tests, and pushing the fix commit. Today, addressing comments
is a human-only step.

Concrete impact (2026-05-03 morning): operator manually addressed all
six review comments on PR #199 (5 Copilot + 1 GHAS). Each was
legitimate per the "do NOT dismiss as noise" rule
(memory: `project_copilot_review_is_heuristic_not_reviewer.md`,
2026-04-30 update). A swarm role doing the same work would have closed
the loop without the operator's morning.

Add a 13th role to the factory: `comment-responder`. Input = PR with
unresolved review comments. Output = a fix commit pushed to the PR's
branch, addressing each comment on its merits, with a chain event per
comment recording which were applied vs dismissed (and why).

Steps:
1. Extend `RoleSchema` in `libs/contracts/src/execution-request.schema.ts`
   to include `'comment-responder'`. Bump
   `apps/temporal-worker/src/role-prompts.ts` `ROLE_PROMPTS` map.
2. Author `apps/temporal-worker/src/comment-responder/prompt.ts`. The
   prompt walks the agent through:
   - List comments via `gh api repos/<owner>/<repo>/pulls/<pr>/comments`
   - For each: read the comment + the diff_hunk + the linked file/line
   - Evaluate on merit (the `do NOT dismiss as noise` rule). Decide
     apply / dismiss-with-reason / escalate-to-operator.
   - For applies: edit the file, run targeted tests, commit
   - At end: post one summary comment to the PR (`gh pr comment`)
     with apply/dismiss/escalate per comment + reasons
3. Author `apps/temporal-worker/src/comment-responder/dispatch.ts` —
   companion to `review-graph-dispatch.ts`. Triggered by the
   review-graph (or by `pr-event-ingester` directly) when a PR's
   comment count crosses a threshold AND the PR has no in-flight
   comment-responder workflow. Default driver tier is T2 (Copilot
   Sonnet); escalates to T3 (claude-code-headless Opus) if the
   responder fails or stalls — same ladder shape as the implementor
   escalation. The frontmatter `tier: T2` matches this default.
4. Wire into the §5 review-graph: when a reviewer flags 🟡 / 🔴
   findings, instead of ending the chain, dispatch a
   comment-responder for the same PR. Re-run the reviewer chain on
   the responder's fix commit. Loop until clean or escalation to R4.
5. Tests: with a fixture PR that has known comments, the responder
   produces the expected fix commit and apply/dismiss summary.

**Acceptance:**
- [ ] `comment-responder` role added to RoleSchema + role-prompts
- [ ] Dispatch path wires from review-graph (or ingester) to responder
- [ ] Responder produces apply/dismiss/escalate decision per comment
- [ ] Each decision recorded as a chain event
- [ ] Backfill demo on a synthesized PR with 3+ Copilot comments:
      responder lands a fix commit that addresses all and posts the
      summary comment
- [ ] CI green

**Note on dependency ordering:** `pr-event-ingester` must land first.
Without it, the responder has no upstream trigger for human-opened
PRs. Once both ship, the chain is: ingester → review-graph (R1+) →
findings → comment-responder → fix commit → review-graph re-runs →
gatekeeper auto-merge (or operator at R4). That closes the loop the
factory design described.

### `swarm-implementor-pnpm-lock-discipline`

```yaml
id: swarm-implementor-pnpm-lock-discipline
tier: T2
status: ready
estimated_loc: 50
blocks: []
file: apps/temporal-worker/src/role-prompts.ts, apps/temporal-worker/src/gatekeeper.ts
references_finding: 2026-05-03-swarm-cohort-lockfile-drift
role: programmer
```

Three of seven open swarm PRs from the 2026-05-02 → 03 overnight run
(#189 nx-angular-workspace-install, #193 scheduler-dashboard-angular,
#195 slack-l2-actions) failed CI with `ERR_PNPM_OUTDATED_LOCKFILE` — the
implementor agent edited a `package.json` (root, scheduler-dashboard, or
slack-app) without regenerating `pnpm-lock.yaml`.

The pattern is structural, not per-PR: the implementor harness lacks a
post-edit gate that detects "package.json modified, pnpm-lock.yaml not
modified" and runs `pnpm install` (or fails the run with a clear message).
Adding the dep manually as a JSON edit and skipping `pnpm install` is the
fast path the model takes when not corrected.

Three places this can be enforced (pick one — the cheapest is best):

1. **Pre-commit hook in implementor worktree** — refuse to stage
   `package.json` changes without a matching `pnpm-lock.yaml` change.
   Cheapest, but only fires at the implementor's commit step.
2. **Role-prompt rule** in `apps/temporal-worker/src/role-prompts.ts`
   ("if you edit package.json, run `pnpm install --no-frozen-lockfile`
   before committing"). Soft enforcement, but cheap and reusable.
3. **Dispatcher post-write check** — after the implementor returns, the
   dispatcher inspects the worktree diff; if `package.json` is dirty and
   `pnpm-lock.yaml` is not, the dispatcher runs `pnpm install` itself
   before `git push`. Hard enforcement, slightly more work.

Recommended: (2) + (3). Role-prompt sets the expectation; dispatcher
post-check enforces it in case the agent forgets. Same shape as the
existing `gatekeeper.ts` post-write checks for governance paths.

Steps:
1. Add the rule to the relevant role-prompt section in `role-prompts.ts`
   (probably the `programmer` role; check current shape).
2. Extend `gatekeeper.ts` (or wherever post-write inspection lives) with
   a `pnpm-lock-coherent` invariant: package.json modified ⇒ lockfile
   must be modified. On violation, run `pnpm install` in the worktree,
   stage the lockfile, append a chain event noting the auto-fix.
3. Tests: synthesize a worktree with the violation; assert the
   gatekeeper auto-fix lands the right files.

**Acceptance:**
- [ ] Role-prompt mentions the rule
- [ ] Gatekeeper auto-fixes a worktree where `package.json` changed and
      lockfile didn't
- [ ] Backfill: re-run one of #189/#193/#195 (or synthesize the
      pattern); after the post-write check, `pnpm install
      --frozen-lockfile` succeeds
- [ ] CI green

### `tc-extend-to-tests-and-tools`

```yaml
id: tc-extend-to-tests-and-tools
tier: T1
status: ready
estimated_loc: 200
blocks: []
file: libs/*/tsconfig.spec.json (new), libs/*/tsconfig.json (add references), tools/lint/tsconfig.json (new)
references_finding: 2026-05-03-typecheck-coverage-gap
role: programmer
```

The CI typecheck gate added in PR #203 catches accumulated errors in
each project's `src/**/*.ts` (via tsconfig.lib.json) but misses:

- **Lib tests** (`libs/*/tests/*.ts`): they're not in any tsconfig
  the typecheck target reaches. A test that imports a non-existent
  symbol or has a type error still merges green.
- **`tools/`**: only `tools/lint/` has its own package + tsconfig
  (added in PR #204). Other tools/ scripts (`generate-go-types.ts`,
  `lint/role-coverage.ts` if/when added) are unchecked.
- **Root configs** (`vite.config.ts`, etc.): not in any project ref.

Apps' tests ARE covered today (each app's tsconfig.json includes
tests/). Libs aren't, by current convention.

Fix: each lib gets a `tsconfig.spec.json` that includes both `src/`
and `tests/`, referenced from `tsconfig.json` so `tsc --build`
catches it. The nx typecheck target then walks all references.

Steps:
1. For each `libs/*/`, add `tsconfig.spec.json` extending the base,
   `include: ["src/**/*.ts", "tests/**/*.ts"]`.
2. Update each `libs/*/tsconfig.json` `references` to include
   `./tsconfig.spec.json`.
3. Add `tools/*/tsconfig.json` for tooling scripts that aren't yet
   workspace packages.
4. Verify `pnpm exec nx run-many -t typecheck` covers the whole
   workspace. Document in CI yml comment.

**Acceptance:**
- [ ] Every `libs/*/tests/*.ts` is type-checked by the existing
      typecheck CI gate
- [ ] Every `tools/*/*.ts` either has its own tsconfig or is part
      of an existing workspace package
- [ ] A test introducing a deliberate type error fails the
      `TypeScript typecheck (Nx affected)` step (proves the gate
      now catches what it used to miss)
- [ ] CI green on current main

## Tooling cohort — generators + structural linters

Filed 2026-05-03 after this morning's review-loop cycle surfaced the
same omission patterns across multiple swarm PRs: missing `layer:*`
depConstraints (PRs #194, #195, #199), backlog entry heading-id /
frontmatter-id mismatch (#200), absent `tsconfig` extending base
(#195), and missing role-prompt entries when a new role is added.

Each of these is a *structural* failure that current code review
catches one PR at a time. Linters and generators move the catch from
review-time to author-time (or eliminate the failure mode entirely
by scaffolding correctly from day zero).

Order is deliberate: linters first (highest leverage — they apply to
every existing code path, not just new ones); then generators
(ossify shape, only worth it once the shape is stable).

### `lint-role-coverage`

```yaml
id: lint-role-coverage
tier: T1
status: ready
estimated_loc: 100
blocks: []
file: tools/lint/role-coverage.ts (new), package.json (add lint script), .github/workflows/ci.yml (wire)
references_finding: 2026-05-03-role-prompts-drift-risk
role: programmer
```

Compile-time guarantee that every `Role` enum value in
`libs/contracts/src/execution-request.schema.ts` has a corresponding
`ROLE_PROMPTS` entry in `apps/temporal-worker/src/role-prompts.ts`.
Today, adding a role to the enum without touching the prompts map
fails silently at dispatch time (the dispatcher tries to build
a prompt for the unknown role). Lint catches it at PR time.

Steps:
1. Author `tools/lint/role-coverage.ts` — read RoleSchema's enum
   values, read `ROLE_PROMPTS` keys, assert symmetric difference is
   empty.
2. Add `lint:role-coverage` script to root `package.json`.
3. Wire into CI as a separate gate.
4. Tests with synthesized RoleSchema + ROLE_PROMPTS shapes (no
   @chitin/contracts dependency — pass as args).

**Acceptance:**
- [ ] Lint catches a role added to RoleSchema but not ROLE_PROMPTS
- [ ] Lint catches a ROLE_PROMPTS key not in RoleSchema (other direction)
- [ ] CI green on current main; both halves of the symmetric check
- [ ] Wired into the `test` job's gate set

### `lint-layer-tag-coverage`

```yaml
id: lint-layer-tag-coverage
tier: T1
status: ready
estimated_loc: 150
blocks: []
file: tools/lint/layer-tag-coverage.ts (new), package.json
references_finding: 2026-05-03-layer-tag-omission-pattern
role: programmer
```

For every `nx.tags` entry in any workspace `package.json` that
starts with `layer:`, assert there's a matching `depConstraints`
entry in `eslint.config.mjs`. Today, four separate Copilot review
cycles (PRs #194, #195, #199, #192-cli-edge) caught the same
omission: a new layer tag was added without a corresponding
depConstraint, so the boundary rule didn't actually constrain the
new layer. Lint moves the catch from review-time to author-time.

Steps:
1. Walk all `package.json` files under `apps/` and `libs/` (skip
   node_modules), collect `layer:*` tags from `nx.tags`.
2. Parse `eslint.config.mjs` (use a TS AST walker; the file is
   structured), extract `sourceTag` values from `depConstraints`.
3. Assert: every `layer:*` tag in (1) has a matching `sourceTag`
   in (2). Allow extra depConstraints (some layers — `kernel` —
   exist but no package carries them yet; warn, don't fail).
4. Wire into CI alongside `lint-role-coverage`.

**Acceptance:**
- [ ] Catches a new `layer:foo` tag without a depConstraint
- [ ] Allows depConstraints without a matching tag (warn only)
- [ ] CI green on current main
- [ ] Backfill — running once on main with current PRs cohort
      flags zero false positives

### `lint-backlog-entry-shape`

```yaml
id: lint-backlog-entry-shape
tier: T1
status: ready
estimated_loc: 200
blocks: []
file: tools/lint/backlog-entry-shape.ts (new), package.json, ci.yml
references_finding: 2026-05-03-backlog-entry-format-drift
role: programmer
```

Validates `docs/swarm-backlog.md` entries against the parser's
expectations:

- Heading id (` ### \`<id>\` `) matches frontmatter `id:` field
  (Copilot caught this on PR #200 — comment-responder vs
  comment-responder-role)
- `tier:` is one of T0..T5 (or absent)
- `role:` is in RoleSchema (so the dispatcher can find a prompt
  builder)
- `status:` is in the parser's accepted set (in_design / ready /
  in_flight / done / blocked)
- `blocks:` and similar lists parse as YAML arrays
- No `blockedBy:` field (the parser only reads `blocks:` — wrong
  field is silent footgun, also from PR #200's review)

Steps:
1. Reuse `parseBacklog` from
   `apps/temporal-worker/src/grooming/parse-backlog.ts`.
2. Run it; collect parse errors + cross-check id matching.
3. Validate `role` against `RoleSchema`.
4. CI gate on PRs that touch `docs/swarm-backlog.md`.

**Acceptance:**
- [ ] Catches heading vs frontmatter id mismatch
- [ ] Catches `blockedBy:` field
- [ ] Catches role not in RoleSchema
- [ ] Catches malformed YAML in entries
- [ ] CI green on current backlog

### `nx-generator-agent-role`

```yaml
id: nx-generator-agent-role
tier: T2
status: ready
estimated_loc: 350
blocks: []
file: tools/generators/agent-role/*
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §3
role: programmer
```

`nx g @chitin/agent-role <name> --shape <reviewer|patcher|analyst|researcher>`
scaffolds the cross-cutting set every new agent role touches:

- libs/contracts/src/execution-request.schema.ts: add `<name>` to RoleSchema
- apps/temporal-worker/src/role-prompts.ts: register prompt builder + import
- apps/temporal-worker/src/<name>/prompt.ts: shape-appropriate prompt template
- apps/temporal-worker/src/<name>/dispatch.ts: enqueue helper companion
- apps/temporal-worker/src/<name>/index.ts: barrel
- apps/temporal-worker/test/<name>.test.ts: stub tests covering invariants

The `--shape` flag picks defaults:
- `reviewer`: bounds matching R1-R3 (read-only, network=allowlist),
  prompt template "review the diff at the PR URL"
- `patcher`: bounds for write+commit+push (network=allowlist,
  write_policy=branch), prompt template "address each comment on
  merit"
- `analyst`: bounds for python-tooling (network=allowlist,
  write_policy=worktree), prompt template "investigate, write report"
- `researcher`: bounds for outbound web (network=open,
  write_policy=none), prompt template "fetch + summarize"

Steps:
1. Standard nx generator scaffold (TypeScript + JSON schema for
   args validation).
2. Templates with `<%= name %>` substitution.
3. AST-aware updates for RoleSchema + role-prompts.ts (don't
   regex; use ts-morph or jsonc-eslint-parser).
4. Tests for the generator itself (run it against a fixture
   workspace, assert files generated correctly).

**Acceptance:**
- [ ] Generator runs cleanly against a fresh workspace fixture
- [ ] All four shapes produce compilable scaffolds
- [ ] RoleSchema + role-prompts.ts updated AST-aware (idempotent —
      re-running is a no-op when role already exists)
- [ ] lint-role-coverage passes on the generated workspace
- [ ] Generated tests pass on first run

### `nx-generator-workspace-lib`

```yaml
id: nx-generator-workspace-lib
tier: T2
status: ready
estimated_loc: 250
blocks: []
file: tools/generators/workspace-lib/*
references_finding: 2026-05-03-workspace-lib-package-json-convention-discovery
role: programmer
```

`nx g @chitin/workspace-lib <name> --layer <layer>` scaffolds the
chitin-flavored library shape: package.json with the right
`nx:run-commands` test target, tsconfig.json + tsconfig.lib.json
extending base, src/index.ts, tests/, **AND** automatically adds the
layer to `eslint.config.mjs` depConstraints.

Would have prevented at minimum:
- PR #194's missing `layer:scheduler` depConstraint
- PR #195's missing `layer:slack` depConstraint + missing
  tsconfig-extends-base + missing `allowImportingTsExtensions`
- PR #199's missing `layer:governance` depConstraint
- PR #194's missing tsconfig + tsconfig.lib files entirely

Each of those was a separate Copilot review cycle on the same
omission pattern.

Steps:
1. Templates for package.json, tsconfig.json, tsconfig.lib.json,
   src/index.ts, tests/.gitkeep.
2. AST-aware update to eslint.config.mjs (add depConstraint with
   defaults: layer can depend on contracts + telemetry).
3. Flag `--allows-deps <comma,sep,layers>` to override the default
   outbound rule.

**Acceptance:**
- [ ] Generated lib passes typecheck + tests on first run
- [ ] eslint depConstraints regenerate correctly for new layer
- [ ] lint-layer-tag-coverage passes on generated lib
- [ ] Idempotent: re-running with same name + layer is a no-op

### `nx-generator-backlog-entry`

```yaml
id: nx-generator-backlog-entry
tier: T1
status: ready
estimated_loc: 200
blocks: []
file: tools/generators/backlog-entry/*, scripts/swarm-backlog-add.ts (new wrapper)
references_finding: 2026-05-03-backlog-entry-format-drift
role: programmer
```

Interactive scaffold for `docs/swarm-backlog.md` entries. Prompts
for id, tier, status, role (picker over RoleSchema), file scope,
blocks, references_finding/spec/design. Emits a properly-shaped
section into the file at the right insertion point with heading id
matching frontmatter id (today's PR #200 footgun).

Validates against `parseBacklog` before writing — refuses to add
malformed entries.

Steps:
1. CLI wrapper using commander or similar.
2. Use jsonc-eslint-parser or yaml lib to emit the frontmatter.
3. Round-trip through `parseBacklog` to validate.
4. Insert at end-of-file before any final prose paragraph.

**Acceptance:**
- [ ] Generator emits a valid entry parseable by parseBacklog
- [ ] Heading id matches frontmatter id (verified post-write)
- [ ] Refuses to write a duplicate id (idempotent + safe)
- [ ] Tab-completes role choices from RoleSchema

### `nx-generator-app`

```yaml
id: nx-generator-app
tier: T2
status: ready
estimated_loc: 300
blocks: []
file: tools/generators/app/*
role: programmer
```

`nx g @chitin/app <name> [--daemon]` scaffolds an app shape:
package.json with `nx:run-commands` run + test targets,
tsconfig.json + tsconfig.spec.json (extending base), src/main.ts
stub, tests/, README. With `--daemon`: also emits
systemd .service + .timer templates under `apps/<name>/systemd/`
following the `chitin-<name>.{service,timer}` convention used by
dispatcher/researcher/groomer/etc.

Same layer-tag depConstraint update as the workspace-lib generator.

**Acceptance:**
- [ ] Generated app boots via `nx run @chitin/<name>:run`
- [ ] --daemon variant produces working systemd units
- [ ] lint-layer-tag-coverage passes

### `nx-generator-spec-plan-doc`

```yaml
id: nx-generator-spec-plan-doc
tier: T2
status: ready
estimated_loc: 150
blocks: []
file: tools/generators/spec-plan-doc/*
role: programmer
```

Templates for the structured docs at `docs/superpowers/{specs,plans,observations}/`.
Each follows a consistent shape: Date / Status / Active lens /
Supersedes / TL;DR / numbered sections. Generator auto-stamps the
date, prompts for the rest, emits a starter section outline.

```
nx g @chitin/doc spec <id>         → docs/superpowers/specs/<date>-<id>-design.md
nx g @chitin/doc plan <id>         → docs/superpowers/plans/<date>-<id>.md
nx g @chitin/doc observation <id>  → docs/observations/<date>-<id>.md
```

**Acceptance:**
- [ ] Each shape produces a spec/plan/observation with the right
      preamble, dated correctly
- [ ] Frontmatter metadata (active lens, status) filled per flag
      defaults

### `systemd-unit-generator`

```yaml
id: systemd-unit-generator
tier: T1
status: ready
estimated_loc: 150
blocks: []
file: tools/generators/systemd-unit/*, scripts/install-systemd-units.sh (refresh)
role: programmer
```

The systemd timer/service pair has a consistent shape; chitin
already has 8 of them (dispatcher / researcher / groomer /
debt-curator / lessons / alarm-feeder / stale-doc-detector /
swarm-rollup). Adding pr-event-ingester (in-flight) and future
periodic tasks repeats the same boilerplate.

Steps:
1. Templates for `.service` (one-shot exec + StandardOutput=journal)
   and `.timer` (OnCalendar / OnUnitActiveSec).
2. Update install-systemd-units.sh to symlink the new pair.
3. Document the default behavior (one-shot, journal logging,
   per-tick timing).

**Acceptance:**
- [ ] Generated pair installs cleanly via existing install script
- [ ] Timer fires at the configured interval
- [ ] systemd shows the unit alongside existing chitin-* services



## Skill-folder cohort — authoring + linter + migration + cost report + tier-router

Filed 2026-05-03, then amended same-day to reflect Anthropic's
skill-folder direction over inline-prompt walkthroughs. Skill folders
(SKILL.md + supporting markdown + scripts + examples) are the new
canonical source of truth for agent capability; the existing
prompt.ts builders (programmer, researcher, analyst, comment-responder
in #207, peer-reviewer in #207) become migration targets, not
templates for new work.

Five entries, ordered:
1. skill-authoring-best-practices-doc (T1, blocking) — canonical
   skill-authoring guide, replaces the originally-filed
   prompt-authoring doc.
2. lint-skill-folder-shape (T1) — structural linter over
   apps/temporal-worker/skills/**/SKILL.md.
3. skill-folder-dispatcher-stitcher (T2) — adapter for tiers
   without harness-native skill discovery (Copilot CLI, ollama
   models including the new GLM-4.7-flash T0). Loads SKILL.md
   plus referenced files into the agent's prompt at dispatch
   time so SKILL.md is the single source of truth across tiers.
4. migrate-role-prompts-to-skill-folders (T2, blocked on #3) —
   move the five existing prompt.ts builders to skill folders;
   the prompt.ts files become thin shims that read SKILL.md.
5. tier-router-with-advisor-consultation (T2) — slice 4 of the
   predictive-execution-policy design spec
   (docs/superpowers/specs/2026-05-03-predictive-execution-policy-design.md
   §3) — concrete backlog version. Higher-tier advisor consultation
   for lower-tier dispatches, especially relevant once GLM-4.7-flash
   is doing T0 work that hits judgment calls.
6. skill-runtime-cost-report-cli (T2) — telemetry-driven feedback
   on which skills are expensive at runtime; closes the loop on
   lower-class-model efficacy.

The order matters: doc first (canonical reference), then the
mechanical pieces (linter, stitcher, migration), then the
empirical loops (cost report, tier router).

### `skill-authoring-best-practices-doc`

```yaml
id: skill-authoring-best-practices-doc
tier: T1
status: ready
estimated_loc: 500
blocks: [lint-skill-folder-shape]
file: docs/skill-authoring.md (new)
references_design: docs/design/2026-05-02-swarm-as-software-factory.md §3
role: tech-writer
```

Canonical authoring guide for chitin's skill-folder shape. Anthropic's
public guidance on skills is the starting point; chitin-specific
extensions cover tier-shape (T0 vs T4 skills look different) and the
stitcher (how SKILL.md flows through to non-Claude-Code tiers).

Sections to cover:

- **Skill-folder anatomy:** SKILL.md (always), referenced markdown
  for templates / rubrics / examples, optional scripts/ for
  delegated CLI calls. Each file's purpose explicit.
- **SKILL.md frontmatter:** activation triggers (when does the harness
  load this skill?), required tools, output format, tier hint.
- **Progressive disclosure:** SKILL.md is the read-first artifact.
  Drill-down files load only when the agent decides they're
  relevant. Implication: SKILL.md must summarize sufficiently.
- **Speak as the model, don't narrate at it:** "You are X. Do Y."
  beats "The X role does Y."
- **Tool descriptions explain WHEN, not WHAT:** the model already
  knows what `gh pr diff` does; tell it when to call it.
- **Negative-space rules:** explicit DON'T sections; pattern-matching
  drift is the primary failure mode for lower-class models.
- **Lower-class-model adaptations:** break tasks into N explicit
  steps; cap tool count; pin output format; smaller working
  memory means less context-juggling per step.
- **Source-of-truth for verifications:** any "verify against X"
  instruction names X explicitly.
- **Composition:** when one skill builds on another, both load.
  Document the import-shape (relative path? skill-name reference?).
- **Tier-shape variations:** T0 skills target small models — short,
  imperative, low tool count. T4 skills can be richer. Examples of
  each.

Each practice tagged `lintable: yes/no` for the followup linter.
Cite Anthropic's skill docs by URL. Reference each existing chitin
prompt.ts file (which migrates to a skill folder per entry #4) as a
worked example.

**Acceptance:**
- [ ] Doc covers all 10 sections above with concrete examples
- [ ] Each practice tagged lintable: yes/no
- [ ] At least 2 worked examples drawn from chitin's own
      (post-migration) skill folders
- [ ] Stale-doc detector confirms the doc lands cleanly

### `lint-skill-folder-shape`

```yaml
id: lint-skill-folder-shape
tier: T1
status: ready
estimated_loc: 300
blocks: []
file: tools/lint/skill-folder-shape.ts (new), tools/lint/tests/skill-folder-shape.test.ts (new)
references_finding: 2026-05-03-skill-authoring-quality-gate
role: programmer
```

Structural linter over apps/temporal-worker/skills/**/. Same shape
as the three linters from #204/#205/#206: pure rules + dynamic-import
I/O + nx target + CI step. Soft-blocked on the skill-authoring doc
(linter rules cite specific sections).

Lintable rules (the practices tagged "lintable: yes" in the doc):

- Every skill folder has a SKILL.md (the entry point).
- SKILL.md has required frontmatter fields: name, activation,
  tools (or "no tools"), tier_hint.
- Referenced files (templates, examples) actually exist at the
  paths SKILL.md cites.
- SKILL.md token count caps per tier_hint: T0 ≤ 1.5K, T1 ≤ 3K,
  T2-T4 ≤ 6K.
- Tool count caps per tier: T0 ≤ 6, T1 ≤ 12, T2-T4 ≤ 25.
- Required sections: SKILL.md must include INVARIANTS and DON'T
  blocks (negative-space rules).
- Output marker convention: any structured emit uses
  `<<<NAME>>>{json}` and the marker is named in SKILL.md.
- Source-of-truth check: any line containing verify/validate/confirm
  must reference a file/path/test on the same or next line.

Steps:
1. Pure rules over parsed SKILL.md (markdown AST) + filesystem walk.
2. Wire into @chitin/tooling-lint as `lint:skill-folder-shape`.
3. CI step.
4. Backfill: clean any reported gaps after the migration entry
   moves the existing prompt.ts files to skill folders.

**Acceptance:**
- [ ] All 8 rules implemented + unit-tested
- [ ] Linter runs as @chitin/tooling-lint:lint:skill-folder-shape
- [ ] CI green on current main after migration entry lands
- [ ] Per-tier budget rules tunable via env or config

### `skill-folder-dispatcher-stitcher`

```yaml
id: skill-folder-dispatcher-stitcher
tier: T2
status: ready
estimated_loc: 400
blocks: [migrate-role-prompts-to-skill-folders]
file: apps/temporal-worker/src/skill-loader/stitcher.ts (new), apps/temporal-worker/src/skill-loader/tests/stitcher.test.ts (new)
references_finding: 2026-05-03-cross-tier-skill-loading
role: programmer
```

The skill-folder pattern depends on the harness for discovery
(Claude Code headless = T3-T4 native; Copilot CLI / ollama models
including the new T0 GLM-4.7-flash = no native skill loading). The
stitcher closes that gap so SKILL.md is the single source of truth
across all tiers.

What it does:
- Given a role + entry, locates the corresponding skill folder
  (apps/temporal-worker/skills/<role>/).
- Reads SKILL.md and any files SKILL.md references.
- For T3-T4 (Claude Code headless): copies the skill folder into
  the agent's working dir; the harness handles loading.
- For T0-T2 (Copilot CLI, ollama): inlines SKILL.md + referenced
  templates into the prompt string at dispatch time. Falls back to
  the same shape current prompt.ts builders produce — but the
  source is markdown, not TypeScript.
- Substitutes entry-specific values (entry.id, entry.description,
  PR URL, etc.) via simple template variables (`{{entry.id}}`).
- Caches the load step (in-process LRU); skill folders are static
  files that change rarely.

Steps:
1. Implement stitcher as a pure function over (role, entry, tier)
   → string (the assembled prompt).
2. Tests with synthesized skill folders + entries.
3. Integrate into role-prompts.ts: builders call into the stitcher
   with the role name; stitcher reads from disk.
4. Caching layer with explicit invalidation (env var or TTL).

**Acceptance:**
- [ ] Stitcher loads SKILL.md + referenced files for a sample role
- [ ] Tier-shape branching: T3-T4 returns folder path; T0-T2
      returns inlined string
- [ ] Variable substitution (entry.id et al)
- [ ] Caching with measurable hit rate on repeat dispatches
- [ ] Round-trip: a SKILL.md authored per the linter rules produces
      a runnable prompt at every tier

### `migrate-role-prompts-to-skill-folders`

```yaml
id: migrate-role-prompts-to-skill-folders
tier: T2
status: ready
estimated_loc: 600
blocks: []
file: apps/temporal-worker/skills/{programmer,researcher,analyst,comment-responder,peer-reviewer}/SKILL.md (new), apps/temporal-worker/src/role-prompts.ts (refactor)
references_finding: 2026-05-03-skill-folder-migration
role: programmer
```

Move the five existing prompt.ts builders to skill folders per the
authoring doc. The prompt.ts files become thin shims that call the
stitcher rather than building strings inline.

Per role:
- apps/temporal-worker/skills/<role>/SKILL.md: the role frame +
  workflow, lifted from the existing prompt.ts string (rewritten
  to authoring-doc shape).
- apps/temporal-worker/skills/<role>/<supporting>.md: per-skill
  rubrics, templates, examples extracted from the inline prompt.
- apps/temporal-worker/src/<role>/prompt.ts: simplifies to
  `(entry) => stitcher.assemble('<role>', entry, tier)`.

Migration order: simplest first. peer-reviewer (read-only) →
analyst (recipe-driven) → researcher → comment-responder (write
flow) → programmer (most context).

**Acceptance:**
- [ ] All 5 existing roles have skill folders that pass
      lint-skill-folder-shape
- [ ] role-prompts.ts builders reduce to stitcher calls
- [ ] All existing tests still pass (the stitcher's output is
      observably equivalent for tiers without harness skill support)
- [ ] Smoke test: a real dispatch at T2 produces the same agent
      behavior pre/post migration

### `tier-router-with-advisor-consultation`

```yaml
id: tier-router-with-advisor-consultation
tier: T2
status: ready
estimated_loc: 500
blocks: []
file: libs/governance/src/advisor.ts (new), libs/governance/src/decide.ts (extend), libs/governance/tests/advisor.test.ts (new)
references_design: docs/superpowers/specs/2026-05-03-predictive-execution-policy-design.md §3
role: programmer
```

Concrete implementation of slice 4 from the predictive-execution-
policy spec — the kernel + tiered advisor pattern. Especially load-
bearing once GLM-4.7-flash is doing T0 work that hits judgment
calls the local model can't resolve confidently.

What it adds:
- AdvisorRequest and AdvisorResponse types (the consultation
  contract — recommendation, reason, agent_guidance, structured
  artifacts).
- Escalation heuristic (deterministic): when does a ToolCallRequest
  warrant advisor consultation? Initial signals — low classifier
  confidence, no exact policy match, blast_vector non-trivial,
  or N consecutive denies in session.
- Advisor dispatch — calls into a higher-tier model with limited
  context (NOT the full agent transcript; just the ExecutionRequest
  + relevant policy + recent chain events). Structured output
  parsed back as AdvisorResponse.
- Chain-event recording: every consultation goes on the chain as
  an `advisor_consultation` event with tier, inputs, outputs,
  and latency. Audit + future training data.
- Policy diff queueing: advisor's PolicyDiff artifacts queue at
  docs/policy-diffs/ for human review (not auto-applied; that
  reintroduces nondeterminism the kernel-as-authority forbids).

Routing table: (action_class, blast_vector) → advisor tier. Direct
routing (T0 → T2 for shell_exec, T0 → T3 for irreversible-external),
not strict chain. Configurable via libs/governance/src/advisor-route.ts
table.

Steps:
1. Types + escalation heuristic (pure logic, unit-testable).
2. Advisor dispatch (calls the swarm's existing tier-driver
   infrastructure to spawn a one-shot higher-tier model).
3. Chain-event emission (extends the existing F4 OTEL emit work).
4. Policy diff queueing (markdown sidecars under docs/policy-diffs/).
5. Tests: heuristic table-test, dispatch with mocked client,
   chain-event shape.

**Acceptance:**
- [ ] Heuristic table-tested across all (action_class,
      blast_vector) combinations
- [ ] Mocked-client dispatch produces correct AdvisorResponse shape
- [ ] Chain emits advisor_consultation event with required fields
- [ ] Policy diffs land at docs/policy-diffs/ with metadata
- [ ] Demo: trigger a T0 dispatch with an unfamiliar tool call;
      observe T2 consultation in chain; observe lower-tier
      compliance with the recommendation

### `skill-runtime-cost-report-cli`

```yaml
id: skill-runtime-cost-report-cli
tier: T2
status: ready
estimated_loc: 350
blocks: []
file: apps/cli/src/commands/skill-cost-report.ts (new), libs/telemetry/src/skill-cost.ts (new)
references_finding: 2026-05-03-lower-tier-skill-efficacy
role: programmer
```

`chitin skill-cost-report --skill <name> [--since <duration>] [--tier <T>]`
queries the canonical chain for skill-tagged tool-call events and
produces a cost summary per skill / tier:

- Tokens per dispatch (prompt + completion) split into cached vs
  uncached.
- Tool-call count per dispatch (mean, p95).
- Tool-call output size (median, p95) — surfaces tools returning
  mostly-discardable structure (MCP candidates).
- Cache hit rate (post-hoc).
- Cost in USD if applicable.
- Lower-tier degradation flag: T0/T1 dispatches with notably
  higher retry rate vs T2+ → "tier model under-fit" signal.
- Advisor-consultation count per dispatch — reads the
  advisor_consultation events from `tier-router-with-advisor-
  consultation` (#5). High consultation rate at a tier suggests
  the tier model is being asked work it can't do alone.

The MCP-decision rubric falls out of the data: when a tool
consistently returns large output the model only summarizes,
that's an MCP-shaped opportunity.

Steps:
1. libs/telemetry: add a query helper for skill + tier rollups.
2. apps/cli: add the skill-cost-report command, output options
   (--format text|json|markdown).
3. Document the queries; common presets (--last-week, --tier T0).
4. Tests against a synthesized chain.

**Acceptance:**
- [ ] Report runs against current main's chain data and produces
      sensible per-skill rollups
- [ ] MCP-candidate flag fires for at least one skill
- [ ] Tier-degradation flag fires correctly on synthesized chain
      with T0 retries
- [ ] Advisor-consultation rate visible per skill / tier
## Strategic evaluations — community signal vs current architecture

Filed 2026-05-03 in response to community signal about
ollama-vs-llama.cpp and hermes-vs-openclaw. Both are tier-T5
strategic questions (operator decision, not auto-dispatchable);
filed as backlog entries to keep the evaluation trail open without
speculatively reversing existing architecture.

### `evaluate-llamacpp-vs-ollama`

```yaml
id: evaluate-llamacpp-vs-ollama
tier: T5
status: in_design
estimated_loc: TBD
blocks: []
file: TBD
references_finding: 2026-05-03-community-signal-llamacpp-over-ollama
role: analyst
```

Community signal: "everyone is saying use llama.cpp instead of ollama."
Honest analysis: ollama IS llama.cpp + a daemon + a model registry
+ a friendlier CLI. Switching to llama.cpp direct keeps the engine,
drops the wrapper.

Tradeoff at the chitin scope:

| | ollama (current) | llama.cpp direct |
|---|---|---|
| Cold-start ergonomics | `ollama run X` works | manage GGUF files + llama-server flags + ports |
| Model registry | central store, one-line pulls | manual HuggingFace + GGUF conversion |
| Tuning surface | limited (Modelfile params, env) | full quant / batch / GPU-layer / mlock |
| Production fitness | fine for dev | better for high-throughput |
| Daemon overhead | negligible | none (operator runs llama-server) |
| OpenAI-compat HTTP | yes | yes (llama-server `--api-key`) |

Chitin-side change is small: `~/.openclaw/openclaw.json` swaps
`"ollama/glm-4.7-flash:latest"` for an OpenAI-compatible endpoint
pointing at `llama-server`. Operator-side cost: discipline (manage
GGUF files yourself).

**Recommended posture (until we hit a real ceiling):** keep ollama.
glm-4.7-flash:latest just landed via ollama and is hot. Switching
speculatively trades a working setup for theoretical gains. Revisit
when:
- Tuning params unavailable in ollama become load-bearing
- Cold-load latency on a model becomes a hot path
- Production throughput per-rig matters (multiple concurrent
  agents, 24/7 swarm at scale)

Steps when this entry is picked up:
1. Identify the specific ceiling that prompted the evaluation
   (vague "everyone is saying" doesn't qualify; cite a concrete
   chitin-side limit).
2. Smoke-test llama-server alongside the current ollama setup;
   measure the delta on the limit identified in (1).
3. If material: file the swap as a programmer-tier backlog entry
   with the new openclaw config + runbook updates.

**Acceptance:**
- [ ] A concrete ceiling identified (not anecdotal community signal)
- [ ] Quantitative comparison on that ceiling
- [ ] Decision: stay-with-ollama / swap-llama-cpp / hybrid
- [ ] If swap: implementation entry filed with concrete steps

### `evaluate-hermes-revival`

```yaml
id: evaluate-hermes-revival
tier: T5
status: in_design
estimated_loc: TBD
blocks: []
file: TBD
references_finding: 2026-05-03-community-signal-hermes-over-openclaw
role: architect
```

Community signal: "everyone is saying use Hermes over openclaw."
Hermes (the chitin-built orchestrator) was killed 2026-04-23 — see
memory entry `project_hermes_killed_chitin_as_governance.md` and
`docs/observations/2026-04-22-autonomy-v1-post-mortem.md`. The kill
was based on:

- Bias toward substrates, not full-stack rebuilds (after clawta
  + hermes both proved heavy to maintain — see
  `project_clawta_archived.md`)
- openclaw exists, has a hook surface (`before_tool_call`)
- Chitin's wedge is governance, not orchestration; killing the
  orchestration layer let chitin focus on the audit + policy
  primitives that are its actual differentiator

That logic still holds in the GENERAL case. The community signal
deserves examination: WHAT specifically does hermes do that
openclaw can't?

Two possibilities for what "use hermes" means:

(a) **Hermes-the-orchestrator** (chitin's killed driver). Reasons
    it might be coming back into vogue: openclaw's hook surface
    might not give what people need; the model-split pattern
    (coder=hands + reasoner=brain) was hermes-specific and not
    directly portable to openclaw's single-agent model.

(b) **Hermes-the-model** (NousResearch's Mistral fine-tunes).
    Different question — that's a model choice, not an
    orchestrator one. local-glm / local-deepseek already exist
    as drivers; adding `local-hermes` would be a small enum
    extension if the model proves better than alternatives.

Steps when this entry is picked up:
1. Identify which Hermes (the orchestrator or the model family)
   the community is recommending.
2. If (a) the orchestrator: name the specific gap openclaw has
   that hermes filled. Compare against openclaw's current
   capability — many gaps are addressable as openclaw extensions
   without reverting.
3. If gaps are real and not easily addressed, file
   `revive-hermes-orchestrator-or-equivalent` as a real
   architectural entry with cost estimates (revival vs. wrapper
   vs. new orchestrator).
4. If (b) the model: smoke-test the recommended hermes variant
   on the 3090; benchmark vs. glm-4.7-flash and qwen3-coder; file
   `add-local-hermes-driver` as a programmer-tier entry only if
   the model materially beats the current set.

**Acceptance:**
- [ ] Disambiguate: orchestrator or model
- [ ] If orchestrator: concrete gap identified, comparison done
- [ ] If model: benchmark complete vs. current local-* drivers
- [ ] Decision: status-quo / extend-openclaw / revive-hermes /
      add-hermes-model
- [ ] Implementation entry filed (or explicit "no action" with
      reasoning recorded for the next time the signal returns)

### `personal-computer-use-substrate`

```yaml
id: personal-computer-use-substrate
tier: T5
status: in_design
estimated_loc: TBD (substrate-spanning)
blocks: []
file: TBD (libs/governance/, libs/adapters/openclaw/, apps/openclaw-plugin-governance/, new browser-driver plugin)
references_finding: 2026-05-03 operator conversation — extending chitin's wedge from coding agents to personal computer-use agents
role: architect
```

Operator framing (2026-05-03): "OpenClaw has a lot of plugins. I've heard
things where it'll go open an LLC for me, or accidentally delete all my
email. What I want is for it to talk to my ChatGPT, sync project + Claude
Code memory + ChatGPT memory + personal-life schedule, do research via
NotebookLM, build decks I can copy back into the repo. The browser is
the surface. Chitin is the safety boundary." Filed as a T5 strategic
evaluation, NOT committed to building yet — coding-agent wedge is still
not empirically proven (comment-responder + peer-reviewer just shipped
2026-05-03 and have <24h of production data).

Why this fits chitin's wedge: today chitin governs *coding* agents on
one machine. Personal computer-use agents are the same shape — tool
calls, blast radius, hash-chained log — over a much larger surface
(browser, apps, files). The verifiable-execution-layer pitch holds in
both domains. Specifically the "open an LLC for me" failure mode is
exactly the threat chitin's policy gate is designed to catch: a
governance rule like `no-financial-transactions` denies the form-submit
before the model can complete the LLC filing.

What this requires (decomposed for grooming):

1. **Browser-automation driver as an OpenClaw plugin.** Wrap Playwright
   or Anthropic's computer-use API. Emits each browser action as a
   `ToolCallRequest` so chitin's `before_tool_call` hook fires per
   click / type / form-submit. Biggest piece — browser automation on
   modern dynamic UIs (ChatGPT, Notion, Gmail) needs vision+text
   models for navigation; Playwright alone won't cut it on those
   surfaces.

2. **Action-class taxonomy expansion in `libs/governance`.** Add
   `browser_navigate`, `browser_click`, `browser_type`,
   `browser_form_submit`, `browser_screenshot` to `SemanticEnvelope`.
   Each gets a default blast-vector profile (navigate=reversible,
   form_submit=often-irreversible, screenshot=self-only).

3. **Conservative default policies for personal-machine threat
   model.** PC has email, banking, signed-in social — blast radius is
   bigger than a coding worktree. Default rules:
   `read-only-by-default` (deny everything except explicitly-granted
   action classes), `no-outbound-messages-without-approval` (any
   send-mail / send-DM / post-tweet escalates to operator),
   `no-financial-transactions` (deny by domain on banking + payment
   providers).

4. **Cross-context memory bridge.** Today chitin's chain is the
   unifying log for coding agents. Extending it to personal life
   means:
   - Ingest ChatGPT conversation exports (their JSON dump format) →
     chain events
   - Ingest NotebookLM artifacts when produced → chain events
   - Link to existing scheduler library (`libs/scheduler/`) for life
     calendar
   - Hindsight-style retrieval over the unified log: "what did I
     research on topic X?" becomes a chain query
   - Note: Hindsight (vectorize-io/hindsight) was previously evaluated
     as not-load-bearing for the coding-agent case; for *personal-
     life cross-context recall* the recall/retain/reflect pattern fits
     directly. Re-evaluate if (b) below ships.

5. **Sample skill folders for the killer workflows.**
   - `research-via-notebook-lm`: drive a NotebookLM session, query,
     export results to a markdown artifact in the repo
   - `sync-chatgpt-to-chitin-context`: pull recent ChatGPT context,
     write a structured summary to `~/.chitin/context/chatgpt/`
     (the artifact lives in chitin's home-directory state, not the
     repo — distinct from the research workflow above which DOES
     write into the repo. Renamed from the original
     `…to-repo` shape since the path was global state, not
     repo-scoped, and that ambiguity matters for replay/storage)
   - `extract-deck-from-doc`: drive a slide-deck generator, produce
     a copyable artifact
   These follow the SKILL.md pattern proven by peer-reviewer +
   comment-responder migrations.

Risk to flag honestly (operator-stated):

- **Scope creep.** Browser automation is its own deep domain. Doing it
  well (modern UIs, vision-aided nav, anti-bot evasion) is a real
  engineering investment — not a weekend.
- **Personal-machine threat model.** The blast radius IS bigger than a
  coding agent in a worktree. Conservative defaults (rule 3 above) are
  table stakes; the hard part is the human-in-the-loop UX for granting
  one-shot exceptions without becoming approval-fatigue spam.
- **Two unproven layers stacked.** Coding-agent wedge is <24h-old in
  production as of filing. Widening to personal computer-use before
  that wedge is empirically sound is putting two unproven layers on
  top of each other. The strategic discipline is: nail one, then
  expand.

Recommended posture (until a forcing function appears):

(a) **Status quo: file, don't commit.** This entry preserves the trail
    + decomposition. Don't build yet. Coding-agent wedge needs a week
    of production data; personal computer-use is the next wedge, not
    the current one.

(b) **One-workflow vertical slice if a forcing function appears.**
    "Drive Chrome → open NotebookLM → ask a research question → copy
    result to a markdown file in the repo. Every action goes through
    chitin's gate. Every action in the chain. Nothing else." That's
    a complete vertical slice; learn whether OpenClaw + Playwright is
    tractable, whether `before_tool_call` fires correctly on browser
    actions, what the action-class taxonomy needs. ~5 days of work; if
    it ships and works, the next two workflows become incremental.

Forcing functions that would bump this from (a) to (b): a talk demo
(2026-05-07 talk), an investor meeting where the demo lands harder
than coding-agent governance, a content piece where computer-use
governance is the headline. Without one, hold at (a).

Steps when this entry is picked up:

1. Confirm or refute the "Triton" / chitin disambiguation in the
   operator's framing — operator referenced a "Triton" for the safety
   layer; clarify whether that's chitin (homophone) or a separate
   product they want to evaluate alongside.
2. Identify the forcing function (if any) — without one, the
   recommendation is (a) status quo.
3. If (b): scope down rule 1 (browser driver) into the smallest
   shippable plugin — single workflow, single browser, no vision-aided
   navigation, no anti-bot — and file as a programmer-tier entry.
4. Decompose rules 2 + 3 (action classes + default policies) into
   parallel scope-down entries; both are libs/governance edits and
   fit the same backlog cadence as the coding-agent governance work.

**Acceptance:**
- [ ] "Triton" disambiguation resolved
- [ ] Forcing function identified (or "none, hold at status quo"
      explicitly recorded)
- [ ] If (b) chosen: vertical-slice entry filed with one workflow,
      one browser, one chain proof
- [ ] Action-class taxonomy update entry filed (libs/governance)
- [ ] Default-policy entry filed (read-only-by-default,
      no-outbound-messages-without-approval, no-financial-transactions)
- [ ] Cross-context memory entry filed (Hindsight re-evaluation +
      ChatGPT export ingester)
- [ ] Sample-skill entries filed for at least the three workflows
      named in rule 5 (`research-via-notebook-lm`,
      `sync-chatgpt-to-chitin-context`, `extract-deck-from-doc`),
      each as its own programmer-tier scope-down with concrete
      browser-driver bounds + acceptance criteria. Without this
      checkbox the entry could mark complete after only the
      policy/memory follow-ups land, leaving the killer-workflow
      cohort unfiled.

### `investigate-low-success`

```yaml
id: investigate-low-success
tier: TBD
status: in_design
estimated_loc: TBD
blocks: []
file: TBD
references_signal: chitin-swarm-rollup alarms
role: analyst
```

Auto-filed by chitin-alarm-feeder.timer at 2026-05-03T03:45:14.730Z from a swarm-rollup alarm:

> LOW SUCCESS: driver=claude-code-headless 56% (5/9)

Analyst role: use `python/analysis/` to read the latest swarm-rollup JSON at `~/.cache/chitin/swarm-rollups/<YYYY-MM-DD>.json` + the events-jsonl chain; identify the root cause (recent dispatch failures, driver regressions, governance edits, etc); write a markdown report to `python/analysis/out/<entry-id>.md` and emit a `<<<ANALYSIS>>>` JSON line with root_cause + recommended_action. Operator: groom this entry once it has a real `tier` / `file:` / `estimated_loc`.
## Follow-ups from low-success alarm (2026-05-03)

Filed by the investigation in
`docs/observations/2026-05-03-low-success-alarm-investigation.md`.
The alarm fired on 5/9 c-c-h success rate; root cause was a
21-hour-deploy-lag on `~/.local/bin/chitin-kernel` (PR #171's
closed-enum normalizer in source but not in the running binary).
Operational fix (rebuild + agent reset) applied alongside this
PR. Two structural follow-ups below.

## Swarm-lessons distillation cohort (filed 2026-05-03)

The afternoon's PR cascade (#211-#223) surfaced 20+ Copilot review
findings across two rounds. Patterns recurring across PRs:
`Date.now()` for unique IDs, `as ExecutionRequest` instead of
`Schema.parse()`, stable workflow_id without explicit conflict
policy, Windows-absolute path injection in user content, ops/
vs infra/ convention drift, CRLF body handling, deploy lag.

Operator framing (2026-05-03): "this is all data for us to see
how to improve the swarm" — the manual review-and-fix loop being
done in this session IS the swarm-improvement signal. Today's
`chitin-lessons.timer` distills one-sentence summaries from
merged-PR commit messages and prepends them to the **programmer**
prompt only. That's undersized for what the data says.

Four entries below close the gap. The first three are
infrastructure (per-role files, Copilot-review ingestion,
code-pattern lesson schema); the fourth is the T4 distillation
agent that turns the manual cascade-review work into a scheduled
swarm role. Together they make the lessons loop genuinely
self-improving — patterns Copilot catches today become lessons
the agents read tomorrow, without operator transcription.

### `per-role-lessons-files`

```yaml
id: per-role-lessons-files
tier: T2
status: ready
estimated_loc: 150
blocks: []
file: docs/swarm-lessons/, apps/temporal-worker/src/lessons.ts, apps/temporal-worker/src/role-prompts.ts, apps/temporal-worker/src/grooming/parse-backlog.ts (role label per PR)
references_finding: 2026-05-03 PR cascade — peer-reviewer + comment-responder roles produce review findings but don't get lessons today
role: programmer
```

Today's `docs/swarm-lessons.md` is one flat file prepended to the
programmer prompt. peer-reviewer + comment-responder + analyst
get nothing — yet they are the roles producing the review-cycle
findings we'd most want to learn from.

Steps:

1. Migrate `docs/swarm-lessons.md` → `docs/swarm-lessons/programmer.md`
   (preserve content; this is the existing scope).
2. Add empty `docs/swarm-lessons/{peer-reviewer,comment-responder,
   analyst}.md` with the same header format.
3. Lessons extractor (`apps/temporal-worker/src/lessons.ts`)
   reads the merged PR's role label (the dispatch marker
   `~/.cache/chitin/swarm-state/dispatched/<entry-id>.json`
   already records it) and routes the distilled lesson to the
   right per-role file.
4. `role-prompts.ts` reads the per-role lessons file at prompt
   build time; falls back to `programmer.md` for roles without a
   dedicated file (graceful degradation).
5. Tests: extractor routes correctly per role; role-prompts
   prepends the right file; missing role file doesn't crash.

**Acceptance:**
- [ ] `docs/swarm-lessons/<role>.md` exists for each role in
      `RoleSchema`
- [ ] Extractor distills + appends to the correct file based on
      the merged PR's role
- [ ] Each role's prompt includes its own lessons block (verified
      via prompt snapshot tests)
- [ ] CI green

### `copilot-review-lessons-extractor`

```yaml
id: copilot-review-lessons-extractor
tier: T3
status: ready
estimated_loc: 300
blocks: [per-role-lessons-files]
file: apps/temporal-worker/src/lessons.ts (extends), apps/temporal-worker/src/lessons/copilot-review-distill.ts, apps/temporal-worker/test/copilot-review-distill.test.ts
references_finding: 2026-05-03 PR cascade — Copilot reviews are the richest lesson signal; today's extractor reads commit messages only
role: programmer
```

Today's lesson distillation reads a merged PR's title + first
body paragraph + a couple of file/diff signals. The richest signal
in this afternoon's cascade was IN THE REVIEW COMMENTS, not the
commit messages. Examples:

- "`Date.now()` can collide under concurrent dispatch in the same
  millisecond — prefer `crypto.randomUUID()`" — this is a Copilot
  comment, not a commit message
- "buildPeerReviewerRequest ends with `} as ExecutionRequest` —
  other dispatch paths use `ExecutionRequestSchema.parse(...)`" —
  Copilot comment

Extend the lessons extractor to fetch each merged PR's
`/pulls/{n}/comments` (review comments) + `/pulls/{n}/reviews`
(top-level reviews). Distill lessons from comments where:
- The comment is from `copilot-pull-request-reviewer` or other
  reviewer accounts
- A subsequent commit on the PR addresses the comment (commit
  diff overlaps the comment's file/line, OR commit message
  references the comment)

Per-comment-resolved lessons go to the same per-role file as
above (entry blocks on `per-role-lessons-files`).

**Acceptance:**
- [ ] Extractor pulls `/pulls/{n}/comments` for each merged PR
- [ ] Heuristic identifies comment→fix-commit pairs (file/line
      overlap OR commit-msg cite)
- [ ] Distilled lesson includes the BUGGY pattern and the FIX
      pattern (depends on `code-pattern-lessons` for schema)
- [ ] Tests with fixture PR comments + commits
- [ ] CI green

### `code-pattern-lessons`

```yaml
id: code-pattern-lessons
tier: T2
status: ready
estimated_loc: 200
blocks: []
file: scripts/install-kernel.sh, infra/systemd/chitin-kernel-redeploy.service, infra/systemd/chitin-kernel-redeploy.timer
references_finding: docs/observations/2026-05-03-low-success-alarm-investigation.md
role: programmer
```

Close the deploy-lag gap that produced the 2026-05-03 low-success
alarm. PRs touching `go/` or `chitin.yaml` only take effect when
an operator manually runs `go build`. The swarm runs unattended;
nobody redeploys; policy fixes sit dark for hours-to-days.

The simplest mechanic that closes the gap (avoid GitHub Actions
because the binary needs to land on the operator's rig, not in CI):

1. **`scripts/install-kernel.sh`** — idempotent shell script that
   - `cd /home/red/workspace/chitin && git fetch origin && git
     pull --ff-only origin main`
   - if `git diff --quiet HEAD@{1} HEAD -- go/ chitin.yaml` returns
     non-zero (i.e. changes), rebuild via `( cd
     go/execution-kernel && go build -o
     ~/.local/bin/chitin-kernel ./cmd/chitin-kernel )` — the
     subshell is required because chitin's go module is rooted at
     `go/execution-kernel/go.mod` (no top-level `go.mod`)
   - log start + end + sha + duration to the chain (use
     `chitin-kernel emit` if the previous binary was healthy enough
     to call, otherwise stderr); the log line is the operator's
     "what changed when" trail
   - exit 0 on no-op; exit 0 on rebuild-success; exit non-zero on
     git-pull-conflict OR build-failure (neither should auto-recover —
     surface to the operator)

2. **`infra/systemd/chitin-kernel-redeploy.service`** — oneshot unit
   that runs the script under the operator's user (NOT root — the
   binary lives under `~/.local/bin`).

3. **`infra/systemd/chitin-kernel-redeploy.timer`** — `OnUnitActiveSec=15min`,
   `OnBootSec=2min`, `Persistent=true`. 15-minute redeploy cadence
   keeps the lag bounded; persistent + boot-delay handles
   reboots cleanly. Operator can suspend by `systemctl --user stop
   chitin-kernel-redeploy.timer` without breaking anything.

4. **Rollback rule:** if the new build fails to start (smoke-test:
   `chitin-kernel gate evaluate --hook-stdin --agent=smoke` with a
   canned input must exit 0 within 2s), restore the previous binary
   from `~/.local/bin/chitin-kernel.prev` (kept by the script) and
   chain-log the rollback. We don't want a bad merge to brick the
   gate for the swarm.

5. **README + telemetry:** `docs/runbooks/chitin-kernel-redeploy.md`
   covering install, suspend, manual override, where the chain
   logs land. Operator should be able to read "when did the kernel
   last update" off the chain in one query.

**Acceptance:**
- [ ] `scripts/install-kernel.sh` no-ops cleanly when no go/ or
      chitin.yaml changes since last run
- [ ] Script rebuilds + reinstalls when go/ or chitin.yaml changes
- [ ] Smoke-test (canned `Task` PreToolUse evaluate) exits 0
      against the new binary OR the script auto-rolls-back
- [ ] systemd timer + service install cleanly via
      `systemctl --user enable --now chitin-kernel-redeploy.timer`
- [ ] Chain emits a `kernel_redeploy` event per rebuild with
      `{old_sha, new_sha, duration_ms, smoke_test_passed}`
- [ ] Runbook in docs/runbooks/

### `scheduler-gov-rule-retier-or-action-class`

```yaml
id: scheduler-gov-rule-retier-or-action-class
tier: T5
status: in_design
estimated_loc: TBD
blocks: []
file: docs/swarm-backlog.md (the scheduler-gov-rule entry), chitin.yaml (potentially), libs/governance/ (potentially)
references_finding: docs/observations/2026-05-03-low-success-alarm-investigation.md
role: architect
```

The `scheduler-gov-rule` entry is the 4th failure in the
2026-05-03 alarm window. It asks the swarm to add a new chitin
governance rule by editing `chitin.yaml`, but chitin's own
`no-governance-self-modification: enforce` rule denies the write.
The agent has no path to complete the task as specified;
deterministic 0-commits failure on every dispatch.

Operator decision required. Two paths:

(a) **Re-tier the entry to T5 (human action).** Match the
    existing convention that human operators author governance
    edits. Simple, correct, but limits the swarm's reach into
    governance evolution work — and there's a real argument that
    the swarm SHOULD be able to propose governance edits as long
    as the actual write goes through review. This option is
    "narrow swarm authority on chitin.yaml; humans only."

(b) **Add a `chitin.gov.proposed-rule.add` action class.**
    The swarm's writes to `chitin.yaml` are intercepted and
    rerouted to a structured proposal: "agent X proposes adding
    rule Y for reason Z." A human reviews + approves. On approve,
    the proposal becomes an actual chitin.yaml edit (still
    operator-authored, but seeded by the agent). Bigger lift;
    same protective property; preserves swarm-author capability.

Both are valid. (b) is the better long-term shape if the swarm
authoring governance becomes a regular pattern — but the data we
have today is: ONE entry hit this. (a) is correct if it's an
edge case; (b) is correct if it becomes a routine.

**Acceptance:**
- [ ] Operator picks (a) or (b) and records reasoning
- [ ] If (a): scheduler-gov-rule entry's `tier:` field updated to
      `T5` and `status:` updated; entry's prose updated to make
      the human-action requirement explicit
- [ ] If (b): action-class definition + policy rule + intercept
      logic filed as a programmer-tier follow-up; scheduler-gov-rule
      entry stays as-is and waits on the action-class shipping
- [ ] Either way: groomer-side check that catches the pattern (
      "entry asks for a write to chitin.yaml under writepolicy=branch
      → flag for human review") so the next entry of this shape
      doesn't slip through unnoticed
## Swarm-loop hardening — deferred from Copilot review on PRs #211, #212

These three entries were identified by Copilot review during the
peer-reviewer + comment-responder cohort but deferred out of the
critical-fix PRs to keep them small. They share a theme: the
swarm dispatch loop is functional today, but its idempotency over
PR state is not. Without these, the loop will:

- silently skip dispatch-decision regressions (no test coverage);
- re-fire peer reviews every 5 min after the prior review completes
  (dedup is against running workflows, not completed-once);
- re-fire comment-responders even when no new comments have arrived
  (commentCount is total, not "new since last responder run").

Each is small (50-150 LOC). Bundling here so the operator can
groom them into a single follow-up cohort or pick one off
individually.

### `pr-event-ingester-extract-decision-helper`

```yaml
id: pr-event-ingester-extract-decision-helper
tier: T2
status: ready
estimated_loc: 100
blocks: []
file: apps/temporal-worker/src/pr-event-ingester.ts, apps/temporal-worker/test/pr-event-ingester.test.ts
references_finding: Copilot review on PR #211 #4 + PR #212 #1 (same finding, different PR)
role: programmer
```

The new per-PR agent dispatch logic in `pr-event-ingester.ts`
(always enqueue peer-reviewer; enqueue comment-responder when
`copilotCommentCount > COMMENT_RESPONDER_THRESHOLD`; dedup against
`runningAgents`) is not exercised by tests. The existing test suite
covers only `pickPrsToIngest` (the pure classification helper).
The dispatch decision branching includes:

- threshold check on `copilotCommentCount`
- dedup check against `runningAgents` set (workflow_id presence)
- per-decision counter accounting (`peer_reviewers_enqueued`,
  `comment_responders_enqueued`)
- error accounting on individual dispatch failure (the loop
  shouldn't break the whole tick)

Steps:

1. Extract the per-PR dispatch decision into a pure helper
   `decideAgentDispatches(pr, runningAgents, opts):
   { dispatchPeerReviewer: boolean, dispatchCommentResponder: boolean,
     reasons: { skip_peer_reviewer?: string, skip_comment_responder?: string } }`.
   Mirror the shape of `pickPrsToIngest`'s return: structured
   decisions, not booleans.

2. Ingester's main loop calls the helper, then maps decisions to
   dispatch + counter increments.

3. Tests for the helper (in the existing `pr-event-ingester.test.ts`):
   - peer-reviewer always proposed when no run is running
   - peer-reviewer skipped when its workflow_id is in runningAgents
   - comment-responder skipped below threshold
   - comment-responder proposed at + above threshold
   - comment-responder skipped when its workflow_id is in
     runningAgents (regardless of comment count)
   - reasons match expectations on each skip

**Acceptance:**
- [ ] `decideAgentDispatches` is a pure function with no
      Temporal-client dependency
- [ ] Existing ingester behavior is observably identical
      (`pickPrsToIngest` tests still pass)
- [ ] Six new helper tests cover the matrix above
- [ ] CI green

### `pr-event-ingester-dedup-against-completed-workflows`

```yaml
id: pr-event-ingester-dedup-against-completed-workflows
tier: T3
status: ready
estimated_loc: 200
blocks: []
file: apps/temporal-worker/src/pr-event-ingester.ts, apps/temporal-worker/src/peer-reviewer/dispatch.ts, apps/temporal-worker/src/comment-responder/dispatch.ts
references_finding: Copilot review on PR #212 #4
role: programmer
```

`listRunningAgentWorkflows` only returns workflows in
`ExecutionStatus="Running"`. After a peer-reviewer's run completes
on PR #207, the next ingester tick (5 min later) will see no
running workflows for `peer-review-pr-207` — and will dispatch
another peer review for the same unchanged PR. The dispatch will
keep posting duplicate review comments every 5 minutes.

The robust fix is dedup against PR state rather than workflow
state: each peer-reviewer run is per (PR#, head_sha). A new
commit pushed to the PR → new review needed; same head_sha → no
review needed.

Two implementation paths to consider during grooming:

(a) **Marker-file dedup** (mirrors the dispatcher's existing
    `~/.cache/chitin/swarm-state/dispatched/<entry-id>.json`
    pattern): per-PR marker at
    `~/.cache/chitin/peer-reviewer-state/pr-<n>.json` recording
    `{ head_sha, ran_at, workflow_run_id }`. Ingester reads the
    marker before dispatch and skips if `head_sha` matches the PR's
    current HEAD. Peer-reviewer (the agent) writes the marker when
    its run completes successfully.

(b) **Temporal visibility query against completed workflows**:
    list completed workflows with `WorkflowId="peer-review-pr-<n>"`,
    inspect their result envelope for the recorded head_sha. More
    Temporal-native; doesn't introduce new state. Risk: workflow
    history retention windows may evict old runs.

Same shape applies to comment-responder, with the additional
twist that a comment-responder run can produce new commits +
new pushes; head_sha will change as a result OF the run.
Idempotency key for comment-responder is probably (PR#,
unresolved_comment_set_hash) rather than head_sha — needs a
groomer-pass before code lands.

**Acceptance:**
- [ ] Peer-reviewer is dispatched at most ONCE per (PR#, head_sha)
- [ ] Comment-responder dispatch is gated on a similar idempotency
      key (decision in design phase, then implement)
- [ ] Ingester does not re-fire either agent on the same PR every
      tick after the first run completes
- [ ] Tests: same PR ingested twice across two ticks dispatches
      exactly once unless head_sha changes

### `pr-event-ingester-comment-count-is-unresolved`

```yaml
id: pr-event-ingester-comment-count-is-unresolved
tier: T2
status: ready
estimated_loc: 100
blocks: [pr-event-ingester-dedup-against-completed-workflows]
file: apps/temporal-worker/src/pr-event-ingester.ts
references_finding: Copilot review on PR #212 #5
role: programmer
```

`copilotCommentCount` (the value compared against
`COMMENT_RESPONDER_THRESHOLD`) is the total number of Copilot
inline comments returned by `/pulls/{n}/comments`. Once a
responder run completes, the historical Copilot comments are
still there — so every later ingester tick will re-enqueue
another comment-responder for the same PR even if all threads
were already replied-to.

GitHub's review-comment API distinguishes:

- root-level vs threaded (`in_reply_to_id` is null vs set)
- resolved vs unresolved (via the GraphQL endpoint;
  `pullRequestReviewThread.isResolved`)

The threshold check should count root-level UNRESOLVED comments
from reviewers. Two engineering pieces:

1. Replace `gh api repos/.../pulls/<n>/comments` with the GraphQL
   endpoint that exposes `isResolved` per-thread.
2. Filter to (a) root-level (not in_reply_to), (b) unresolved,
   (c) authored by a reviewer (Copilot, github-advanced-security,
   reviewers — exclude the operator's own comments).

`blocks: pr-event-ingester-dedup-against-completed-workflows` —
this fix only matters once the broader dedup story is in place;
without it, the dedup-by-completion change supersedes the count
distinction (responder won't refire anyway).

**Acceptance:**
- [ ] `copilotCommentCount` reflects unresolved-root-level reviewer
      comments only
- [ ] Resolved threads do not contribute to the threshold count
- [ ] Test: PR with 5 root-level Copilot comments, 3 marked
      resolved → count returns 2
- [ ] Test: PR with 5 reviewer comments, 5 reply comments
      (`in_reply_to_id` set) → count returns 5 (only root-level)
file: docs/swarm-lessons/<role>.md (format change), apps/temporal-worker/src/lessons.ts (entry parser), apps/temporal-worker/src/role-prompts.ts (renderer)
references_finding: 2026-05-03 PR cascade — one-sentence lesson loses the WHY and the SHAPE
role: programmer
```

Current lesson schema:
```
- 2026-05-03 #207 — Don't dismiss Copilot comments as noise; verify each on merit.
```

Loses the WHY (what was the original failure mode?) and the
SHAPE (what does the buggy code look like vs the fixed code?).
Programmers reading "don't use Date.now() for unique IDs" still
have to learn what `randomUUID` looks like in our context.

Proposed schema (markdown sections, each labeled):
```
## #211 (2026-05-03, programmer) — Date.now() for unique IDs collides under concurrency

### Bad
```ts
run_id: `${workflowId}-${Date.now()}`
```

### Good
```ts
import { randomUUID } from 'node:crypto';
run_id: `${workflowId}-${randomUUID()}`
```

### Why
Concurrent dispatches in the same millisecond produce
identical run_ids → kernel writes both runs to the same
`.chitin/events-<run_id>.jsonl` → per-run audit trail lost.
Caught by Copilot in PR #211 round-2 review.
```

Steps:

1. New parser in `lessons.ts` that handles the structured-
   markdown shape (forward-compatible with existing flat lines).
2. Extractor distillation prompt updates to produce the new
   shape (LLM-mode only; heuristic stays one-sentence as
   fallback).
3. `role-prompts.ts` renders the structured form as markdown
   blocks; old flat lines render unchanged.
4. Migration script: convert existing flat lines to the new
   shape over time (one-shot when this entry lands; new lessons
   land in the new shape automatically).

**Acceptance:**
- [ ] New schema parses with both old (flat) + new (structured)
      entries
- [ ] LLM distillation produces the structured shape
- [ ] Programmer prompt renders Bad/Good/Why blocks as markdown
- [ ] Migration of existing `swarm-lessons.md` runs once + produces
      sensible structured entries (or leaves them flat if
      conversion isn't possible)
- [ ] CI green

### `lessons-curator-tiered-pipeline`

```yaml
id: lessons-curator-tiered-pipeline
tier: T0
status: ready
estimated_loc: 500
blocks: [per-role-lessons-files, copilot-review-lessons-extractor, code-pattern-lessons]
file: apps/temporal-worker/src/lessons-curator/dispatch.ts, apps/temporal-worker/src/lessons-curator/prompt.ts (or skill folder), apps/temporal-worker/src/lessons-curator/cluster.ts (deterministic), infra/systemd/chitin-lessons-curator.{service,timer}, docs/runbooks/chitin-lessons-curator.md
references_finding: 2026-05-03 operator framing — "this is all data for us to see how to improve the swarm" + "shift majority of work left, push towards T0 doing majority of work"
role: analyst
```

Note on role: filed under `analyst` (the closest existing role — work is analyzing PR-review patterns and producing structured output) to satisfy the backlog-shape linter's RoleSchema check. The implementation will likely introduce a dedicated `lessons-curator` role once the work is scoped — if so, this entry's role + the `add lessons-curator to RoleSchema` step in acceptance criteria below get bundled. Until then `analyst` is the load-bearing label.

The first three entries above are PASSIVE infrastructure
(per-role files, review-comment ingestion, structured schema).
None of them learn cross-PR patterns or self-file improvement
backlog entries when the swarm hits a recurring class of bug.
This entry adds the ACTIVE half: an agent that runs after each
merge cycle, reads the recent PR + Copilot-review history,
distills lessons, and self-files improvement entries when
patterns cluster.

This is what the operator was doing manually in this PR cascade
(reading Copilot reviews, distilling patterns into proposed
backlog entries). The agent does the same loop on a schedule.

**Tier-decomposed by design** — operator framing 2026-05-03 is
"shift majority of work left, push towards T0 doing majority of
work." Original draft of this entry was monolithic T4 (Opus
across the whole pipeline), which would lock in expensive
reasoning forever. Reframed: the pipeline is a T0 mechanical
shell that escalates to T4 ONLY for the one step that needs
real judgment (cross-PR pattern detection / categorization).
This is the advisor-pattern from `#208` entry 5 made concrete.

Role: `lessons-curator` (new — needs to be added to `RoleSchema`
in `libs/contracts`).

Pipeline (with explicit tier per step):

| Step | Tier | What | Why this tier |
|---|---|---|---|
| 1. List merged PRs in window | **T0** | `gh pr list --search "is:merged merged:>YYYY-MM-DD"` parsed into a list | Pure shell + JSON parsing; no judgment |
| 2. For each PR: pull commits + reviews + comments + diff | **T0** | `gh api` + `gh pr diff`; structured into a per-PR record | Pure data fetch + transformation |
| 3. Cluster comments by similarity (root-cause category, file+line shape, proposed-fix shape) | **T4 advisor** | Calls T4 for the categorization judgment ONCE per cycle, with the full PR set as input. Returns clusters. | Cross-PR pattern detection is the genuine reasoning step |
| 4. Per-cluster action: ≥2 occurrences → file backlog entry; 1 occurrence → distill lesson | **T0** | Deterministic dispatch on cluster size; calls Entry 1-3's existing infrastructure | Mechanical; the action is determined by the cluster shape T4 returned |
| 5. Output structured `<<<LESSONS_CURATOR>>>` JSON to chain | **T0** | Standard kernel emit | Mechanical |

Net cost shape: ONE T4 call per cycle (step 3), ALL OTHER WORK
at T0. The naive "wrap everything in Opus" approach burns
~50× the cost. The advisor pattern keeps T4 for the judgment
call only.

Bounds (overall workflow):
- write_policy=branch (commits + PRs lessons-files + new backlog
  entries; needs PR review like any other agent)
- network=allowlist (gh CLI only)
- max_tool_calls=200 (cross-PR analysis is expensive in I/O)
- wall_timeout=3600s (1h ceiling — gives the T4 advisor room
  but the rest of the pipeline runs in seconds)

Schedule:
- `chitin-lessons-curator.timer` fires daily at 04:00 local
  (after the daily rollup, before the morning's first dispatches)
- `Persistent=true` so missed runs catch up
- Pause via `systemctl --user stop chitin-lessons-curator.timer`

Connection to the analysis station: distillation outputs (per-
pattern frequency counts, recurring categories, lessons-yielded
ratios, **and the T4-advisor-cost-per-cycle**) become first-class
telemetry for the rollup. The operator reads "lessons-curator
distilled 7 lessons, filed 2 recurring-pattern improvement
entries; spent $0.42 on T4 advisor; rest of pipeline ran on
local-glm-flash" in the morning rollup.

Connection to the shift-left thesis: this entry IS a shift-left
example — work that the operator does manually today (reviewing
the PR cascade for patterns) becomes mostly-T0 with one T4
escalation. As clustering heuristics improve over time, even
step 3 can collapse to T0 (deterministic similarity scoring) +
T4 only when the heuristic flags ambiguity.

**Acceptance:**
- [ ] `lessons-curator` role added to RoleSchema + role-prompts
- [ ] Dispatch helper builds an ExecutionRequest with the cycle
      window + PR list as input
- [ ] Pipeline implementation is tier-explicit: steps 1, 2, 4, 5
      run at T0; step 3 is the only T4 escalation
- [ ] Skill folder (or prompt) instructs the agent through the
      5-step workflow with explicit tier annotations
- [ ] systemd timer + service install cleanly; 04:00 local cadence
- [ ] Smoke run produces ≥1 distilled lesson + records T4 cost
      separately in chain telemetry
- [ ] Pattern-cluster threshold (2+ recurrences) is exercised
      with a synthetic-input test
- [ ] Output rollup-style summary surfaces in chain telemetry,
      including the T4-cost-per-cycle line item
- [ ] Runbook: install / verify / suspend / manual override / how
      to read the chain output / **how to evaluate when step 3
      can collapse to T0** (the next shift-left target)

## Dispatcher: skip already-implemented entries (filed 2026-05-03)

### `dispatcher-skip-already-implemented-entries`

```yaml
id: dispatcher-skip-already-implemented-entries
tier: T2
status: ready
estimated_loc: 250
blocks: []
file: apps/temporal-worker/src/dispatcher.ts, apps/temporal-worker/test/dispatcher-skip-shipped.test.ts, apps/temporal-worker/src/grooming/parse-backlog.ts
references_finding: 2026-05-03 cascade — swarm dispatched #216 (comment-responder) and #218 (pr-event-ingester) against entries already implemented by hand-merged PRs, producing regressive stub PRs that had to be closed
role: programmer
```

The dispatcher's "is this entry available to dispatch?" check today
relies on:

1. `status: ready` in the backlog frontmatter
2. Marker file at `~/.cache/chitin/swarm-state/dispatched/<entry-id>.json`
   absent OR escalation-eligible (failed prior tier)
3. No swarm branch matching `swarm/swarm-<entry-id>-*` on origin

This MISSES the case where a hand-merged PR shipped the entry's
work without flipping the entry's status to `shipped`. Today's
2026-05-03 cascade hit this: the operator merged comment-responder
+ pr-event-ingester implementations via #207/#211/#215. Those PRs
didn't update `status: ready → status: shipped` in
`docs/swarm-backlog.md`, so the dispatcher's next tick saw the
entries as still-available and dispatched them. The agents tried
to implement entries that were already shipped, producing
regressive stub PRs (closed with comments).

Three approaches in order of confidence (groomer picks the
right combo):

(a) **Backlog-side: title-substring scan of recent merged PRs**
    (the auto-flipper). A `chitin-shipped-entry-flipper.timer`
    scans merged PRs from the last N days; for each, find any
    backlog entry whose id appears in the PR title. For matches,
    file a PR (or auto-edit) flipping `status: ready → status:
    partial` (NOT directly to shipped — the operator inspects
    + promotes). Closes the dispatch-against-shipped gap from
    the merge side. PR-mediated so misclassifications are
    caught at review.

(b) **Strict sentinel check** — for entries that DECLARE a
    `sentinel:` field (function name / file path / chain-event
    type), the dispatcher verifies the sentinel exists in HEAD
    via `git grep` or `git show HEAD:<path>` before dispatching.
    Requires backlog-schema extension. More accurate than (a)
    when applicable; only works for entries that opted in.

(c) **Heuristic file-path check** — Copilot review on this
    entry's first draft flagged the brittleness: 14-day file-path
    scan flags ALL ready entries that share any file with a
    recent commit. The backlog has multiple independent ready
    entries targeting `apps/temporal-worker/src/dispatcher.ts`;
    one recent dispatcher commit would suppress all of them
    for two weeks. Plus the `file:` field today routinely
    includes annotations like `(new)`, `(extend)`, `(new dir)`,
    `*` — opaque strings that break path-parsing. (c) is OFF
    THE TABLE until the backlog schema is tightened to
    machine-parseable file lists.

So the safe shippable shape is **(a) first, (b) optional**.
(a) catches the today-observed incident class without the
false-positive risk of (c). The auto-flipper sets `partial`
not `shipped` because Copilot was right that the existing
`pr-event-ingester` + `comment-responder` entries had
chain-event acceptance items still unshipped — partial is
the more honest status for "implementation merged but not
all acceptance criteria met."

**Acceptance:**
- [ ] `chitin-shipped-entry-flipper.{service,timer}` scans merged
      PRs from last N days (default 7), finds entries whose id
      appears in PR title, files a PR or commits the status flip
      (`ready` → `partial`)
- [ ] Auto-flipper logs each match with: pr_url, entry_id,
      old_status, new_status; rejection reasons logged when no
      action is taken
- [ ] Operator can suspend via `systemctl --user stop
      chitin-shipped-entry-flipper.timer`
- [ ] Optional: backlog-schema extension to add a `sentinel:`
      field; dispatcher reads it and verifies before dispatch
- [ ] Test (in `apps/temporal-worker/test/dispatcher-skip-shipped.test.ts`):
      synthetic entry id appears in synthetic PR title →
      auto-flipper proposes status update
- [ ] Test: entry id NOT in any PR title → no action
## Formalize the no-GitHub-issues choice (filed 2026-05-03)

### `formalize-no-github-issues-decision`

```yaml
id: formalize-no-github-issues-decision
tier: T2
status: ready
estimated_loc: 100
blocks: []
file: docs/decisions/2026-05-03-no-github-issues.md, docs/superpowers/plans/2026-05-02-scheduler-design.md
references_finding: 2026-05-03 operator question — "did we decide to no longer use gh issues?"
role: tech-writer
```

The chitin workflow drifted away from per-PR GitHub issues months
ago — closed-issue history shows the OLD pattern (`[swarm/T1]
openclaw-tool-coverage-audit` etc.) ending around 2026-04-23. Today
the only open issues (#22, #13, #4) are real bug reports filed by
humans, not work-tracking artifacts. The active work surface is
`docs/swarm-backlog.md`: the dispatcher reads markdown directly,
PRs reference backlog-entry-IDs in titles, lessons-extractor
reads from PRs not issues.

Operator framing 2026-05-03: "I agree [with formalizing no-issues]
because I think our scheduler will eventually be the backlog
instead of a flat file."

So the decision has TWO components:

1. **Today's formalization** (status quo): GitHub issues are
   reserved for outside-the-swarm bug reports filed by humans.
   The swarm doesn't create issues. Backlog entries in
   `docs/swarm-backlog.md` are the kanban + work-tracking
   surface.

2. **Forward direction**: the flat-file backlog is INTERIM.
   `libs/scheduler` (life scheduling lib being built) will
   eventually subsume the swarm backlog — backlog entries become
   scheduler items with the same `status`/`tier`/`role`/`blocks`
   shape as today, plus deadline + window-pref fields native to
   the scheduler. `apps/scheduler-dashboard` (the planned Angular
   UI) becomes the kanban view across both life-scheduling work
   AND swarm-backlog work. No separate "swarm-backlog-kanban-view"
   app needed — the scheduler-dashboard handles both.

This entry's job is the docs + decision record, not the
implementation. Implementation lands in scheduler entries.

Steps:

1. Add a one-page decision doc at `docs/decisions/2026-05-03-no-github-issues.md`
   (or amend CONTRIBUTING.md if that file exists / is the right
   home — operator's call). Sections:
   - Decision (what's true today)
   - Why (the structural reasons: backlog file beats issues for
     machine-readable kanban with deps + blocks; lessons + PR
     workflow already routes around issues; no operator-side
     gain)
   - Forward direction (the scheduler subsumes the backlog;
     the flat file is interim)
   - Exception (real bug reports filed by humans use issues
     normally; the no-issues rule is for SWARM work-tracking,
     not all GitHub usage)
   - When this decision should be revisited (if the swarm
     stops scaling under operator-side visibility; if external
     contributors need an issues-mediated entrypoint; if the
     scheduler absorption stalls)

2. Forward-pointer in `docs/superpowers/plans/2026-05-02-scheduler-design.md`:
   add a note that the scheduler is also slated to subsume the
   swarm backlog, with the data model implications (status/tier/
   role/blocks fields, dispatcher reads scheduler API instead of
   markdown).

3. Optional: add a short note to `docs/swarm-backlog.md`'s file
   header explaining "this file is the interim work-tracking
   surface; expect migration to the scheduler when libs/scheduler
   ships its workflow-item shape."

**Acceptance:**
- [ ] `docs/decisions/2026-05-03-no-github-issues.md` exists with
      the five sections above
- [ ] Scheduler design doc has the forward-pointer
- [ ] swarm-backlog.md header notes the interim status (optional)
- [ ] Existing 3 open issues (#22, #13, #4) are NOT closed by
      this entry — they're real bug reports
## Agent-router architecture (filed 2026-05-03 evening)

### `agent-router-architecture`

```yaml
id: agent-router-architecture
tier: T5
status: in_design
estimated_loc: TBD (multi-subsystem; this entry is the design doc and the rollup of MVP entries below)
blocks: []
file: docs/design/2026-05-03-agent-router.md (design), apps/temporal-worker/src/router/, go/execution-kernel/internal/gov/advisor.go, python/analysis/floundering.py
references_finding: 2026-05-03 evening operator framing — multi-dimensional routing (model + agent + cost) with uncertainty + floundering + shared memory + flat-cost-only billing
role: architect
```

Operator framing 2026-05-03 evening (one session):

> "I want to escalate to a higher agent when needed based on
> uncertainty, or some way of deterministically seeing that an
> agent may be floundering and need help."

> "almost like model routing but with agents routing to each
> other when needed then routing back. So both model and agent
> routing. And cost routing too and being able to see budget
> constraints for things like codex Claude code, ollama cloud."

> "Shared memory seems like it would be very helpful for this as
> well. And I don't want to use api calls except for ollama cloud."

> "I want to get it all done tonight then turn it on."

Five primitives compose the router:

1. **Uncertainty-triggered escalation** — agents emit a structured
   `<<<UNCERTAIN>>>{"reason": "...", "blocker": "..."}` marker;
   parser detects, calls advisor, returns advice in next turn.

2. **Floundering detection** — Python analysis pass over chain
   events for one session. Signals: looping tool calls (same
   call N times with same args), wall-clock without commits,
   repeated permission_denials, multi-turn without file writes,
   token budget approaching cap. Returns `floundering: bool,
   reason: enum` per session.

3. **Mid-task handoff** — on uncertainty OR floundering, route to
   advisor (T+1 tier). MVP shape: hard restart at higher tier
   (reuses existing tier-escalation primitive, loses in-flight
   progress). Future: Temporal-signal-driven mid-task continuation.

4. **Shared memory** — agents in the same workflow read/write a
   per-workflow scratchpad. MVP shape: JSON file at
   `~/.chitin/shared-memory/<workflow-id>.json`. Future: cross-
   workflow vector retrieval (Hindsight pattern).

5. **Cost routing + budget visibility** — driver-cost data already
   in swarm-rollup JSON; surface as `chitin budget` CLI. MVP shape:
   visibility only (operator reads, decides). Future: dispatcher
   auto-rejects entries that would exceed remaining budget.

### Hard constraints

- **No metered API calls.** Sub-billed CLIs only (claude-code via
  Pro plan, codex via ChatGPT Plus, gemini via Google AI Pro
  Developers SKU) plus ollama (local + cloud).
- **Self-hostable / open-source.** No SaaS dependency for the
  router itself.
- **MCP-compatible** where applicable (chitin already speaks MCP
  via openclaw).

### MVPs shipping tonight (one PR for the whole router substrate)

| primitive | MVP shape | what's deferred |
|---|---|---|
| Uncertainty marker | `<<<UNCERTAIN>>>` parser + dispatcher hook calling advisor; advice rendered into next-turn prompt | true mid-task continuation (Temporal signals) |
| Guardian advisor | `chitin-kernel gate evaluate --with-advisor` flag; on deny, calls `claude -p` advisor, appends advisory note to chain event | auto-routing on advisor verdict; default-on |
| Floundering detector | Python analysis pass over chain events for one session; detects looping/stalling/over-budget; logs `agent_floundering` event | auto-intervention; today is detect-only |
| Shared memory | per-workflow JSON scratchpad at `~/.chitin/shared-memory/<wfid>.json`; agent reads/writes via simple CLI | cross-workflow vector retrieval (Hindsight pattern) |
| Cost + budget | `chitin budget` CLI reads existing swarm-rollup JSON + `~/.chitin/budgets.json`; surfaces per-driver spend + remaining quota | auto-reject + dynamic tier routing |

All five MVPs ship as code in the same PR for tonight's velocity.
Per-primitive backlog entries can be filed retrospectively for
the production-grade follow-up work.

### Verification

A single end-to-end smoke test gates "turn it on":

- Synthetic backlog entry + dispatch
- Agent emits `<<<UNCERTAIN>>>` marker mid-task
- Parser intercepts, calls guardian advisor
- Advisor returns advice via shared-memory write
- Agent reads advice, continues
- Floundering detector confirms no looping signals fire
- PR created with the work
- Chain has all events recorded (uncertainty fire, advisor call,
  shared-memory write, completion)

If smoke passes, dispatcher is re-enabled. If smoke fails, dispatcher
stays paused and individual primitive entries get re-attempted.

## Router follow-ups (filed 2026-05-03 evening)

### `router-heuristics-go-sdk`

```yaml
id: router-heuristics-go-sdk
tier: T3
status: ready
estimated_loc: 600
blocks: []
file: go/execution-kernel/internal/router/, go/execution-kernel/cmd/chitin-kernel/router.go
references_finding: 2026-05-03 evening operator concern — TS pipeline adds ~500ms-1s startup per tool call; should expose Go SDK to keep hot path fast
role: programmer
```

The router's heuristic+policy layer ships as TypeScript MVP in
`apps/temporal-worker/src/router/`. Operator hot-path concern:
every tool call that hits the slow path eats `pnpm tsx` startup
(~500ms-1s). With many tool calls per session, that adds up.

Move the deterministic-fast pieces into the Go kernel:

1. **policy reader** — Go YAML parser for chitin.yaml `router:`
   section; matches the TS `policy-loader.ts` shape
2. **blast-radius scorer** — pure Go function; same axes as
   TS version (reversibility / scope / visibility / counterparties)
3. **floundering detector** — Go reads chain JSONL, applies same
   signals (looping / stalled / denial-cascade)
4. **session-state reads** — chain index lookups (already in Go)

Only the ADVISOR call stays external (it's slow anyway —
`claude -p` is a multi-second LLM call). The Go kernel exposes
`--with-router` flag on `gate evaluate` that runs heuristics in-
process and prints `{decision, advisor_needed, advisor_request}`.
The TS layer (or a thin shim) handles the advisor call when
flagged.

End-state: hot path is pure-Go. Slow path (advisor) is TS or
external. No `pnpm tsx` overhead per tool call.

**Acceptance:**
- [ ] Go modules at `go/execution-kernel/internal/router/{policy,blast_radius,floundering}.go`
- [ ] `gate evaluate --with-router` flag wires them in-process
- [ ] Same test fixtures pass against Go + TS implementations (port the TS tests)
- [ ] Bin script `chitin-router-hook` updated to skip the TS pipeline when kernel handles it
- [ ] Benchmark: average hot-path latency before / after (target: < 50ms)
- [ ] CI green

### `router-plugin-loader`

```yaml
id: router-plugin-loader
tier: T2
status: ready
estimated_loc: 250
blocks: [router-heuristics-go-sdk]
file: apps/temporal-worker/src/router/plugin-loader.ts, libs/router-plugin-api/
references_finding: 2026-05-03 evening operator framing — heuristics should be exposed as plugin surface so others can configure custom plugins via chitin.yaml
role: programmer
```

The router's heuristic modules currently use static imports. The
operator-side schema in chitin.yaml only covers built-in plugins
(blast_radius, floundering, drift). Operator wants a plugin
surface so custom heuristics and advisor implementations can be
declared in chitin.yaml + dynamically loaded.

Steps:

1. Define `RouterPlugin` interface in `libs/router-plugin-api/`:
   ```ts
   export interface RouterPlugin {
     name: string;
     type: 'heuristic' | 'advisor';
     score?(input: HookInput, context: PluginContext): Promise<HeuristicScore>;
     advise?(request: AdvisorRequest): Promise<AdvisorResponse>;
   }
   ```
2. `chitin.yaml` schema extension:
   ```yaml
   router:
     plugins:
       - name: my-custom-heuristic
         module: '@my-org/chitin-plugin-foo'   # npm package
         config: { threshold: 0.4 }
       - name: local-experimental
         module: './local/my-plugin.ts'        # repo-local path
         config: {}
   ```
3. Plugin loader (`plugin-loader.ts`) dynamically imports
   modules + validates they implement the interface.
4. Hook wrapper iterates plugins instead of hardcoded heuristic
   imports.
5. Built-in heuristics get rewritten as plugins (compatibility
   shim to keep tonight's MVP working unchanged).

Blocks on `router-heuristics-go-sdk` because the Go SDK migration
should land first — otherwise the plugin shape gets defined
twice (TS interface + Go interface).

**Acceptance:**
- [ ] `RouterPlugin` interface + types published as `@chitin/router-plugin-api`
- [ ] Plugin loader handles npm-package + local-path modules
- [ ] Validation: plugins missing required methods rejected with clear error
- [ ] Built-in heuristics ported to plugin shape; tonight's tests still pass
- [ ] Example custom plugin documented in `docs/runbooks/chitin-router.md`
- [ ] CI green

## Recursive governance + everything-starts-at-T0 (filed 2026-05-03 evening)

### `everything-governs-everything-recursively`

```yaml
id: everything-governs-everything-recursively
tier: T5
status: in_design
estimated_loc: TBD (multi-subsystem; this entry captures the architectural principle)
blocks: []
file: TBD (cross-cutting; spans router, gov, scheduler, lessons-curator)
references_finding: 2026-05-03 evening operator framing — chitin must be bulletproof; every action surface must be governed including plugin side effects, advisor recommendations, and recursive escalation chains
role: architect
```

Operator framing 2026-05-03 evening (one session):

> "I thought about the plug in loader. Right? Like, needs to govern
> everything. Right? So, even if I write a custom Python plugin,
> with a side effect, but the side effect is bad or it's, like,
> dangerous. There needs to be a policy allows it or it'll get
> destroyed."

> "We need probably a way to look into what — like, if you're
> running a Python library or you're running a TypeScript library,
> like, we need to audit what is it gonna be doing. Right?
> Probably still with Go. Like, Go needs probably do that since
> it's fast, and then still stop it like, that'll stop us from,
> like, if we're using something like an openclaw plug in you
> know, at the tool call layer, we be able to see that or an MCP
> tool. We should be able to see and prevent bad actions from
> that."

> "What if you escalate to the higher agent, and it does something
> that's wrong? The chitin still should still catch that. Right?
> Or what if it tries, but it fails? Then maybe you need to
> escalate to a higher an even higher agent up to tier four,
> even."

> "I feel like there's an endless amount of things that we can
> govern, but we need to identify those, and we need to try to
> to make sure that chitin is pretty bulletproof."

> "You could eventually have all actions start on tier zero. If
> this works correctly. Right? Like, then you wouldn't actually
> be doing, like, routing or anything through anything but the
> kernel. So you could try to have t zero take a stab, and the
> second it starts to fail escalate. If that fails escalate, and
> then that's how you run into tier four. But then we'll get
> better heuristics and analysis data as we go to know like,
> alright, like certain things probably need to start at a higher
> level rather than us, you know, arbitrarily saying what needs
> to use, what model. And what driver, you know? Over time, we
> should get enough data to be like, oh, like, this can only
> handle these types of actions. Right? Or this can like, t zero
> can handle this action, but it needs to have a t four plan
> first."

This entry is the architectural ROLLUP of three intertwined principles:

### Principle 1 — Recursive governance

Every entity that emits side effects goes through the kernel.
This includes:

- The agent (already wired via PreToolUse hook)
- Plugins (router-plugin-loader; needs trust-allowlist + side-effect
  gate)
- Advisor recommendations (when an advisor says "do X" the agent's
  X-execution still goes through the kernel)
- Higher-tier escalations (T1's tool calls go through the kernel
  same as T0's; chain bounded by max chain depth)
- MCP tools (today partially; openclaw plugins in particular
  need to be governed at the MCP-tool-call layer)

The kernel is the SINGLE choke point. Anything that can do real
work must pass through it.

### Principle 2 — Plugin auditing

Operator-declared plugins (router-plugin-loader) must be:

- Trust-verified at load (allowlist of paths + content hashes —
  shipped as `router-plugin-allowlist`)
- Side-effect-gated at runtime (plugin's tool calls go through
  the kernel; today plugins are NOT gated this way)
- Optionally: statically analyzed for declared capability matching
  (deferred to future work)

The "Go is fast" framing matters here — static analysis + trust
verification belong in the kernel binary, not in TS. Hot path
stays cheap.

### Principle 3 — Everything starts at T0 (the data-driven tier ladder)

The end-state of the shift-left thesis. Today tier→driver mapping
is operator-declared (T0=local-glm-flash, T1=copilot, etc). Future:
all actions START at T0; the router escalates ONLY when:

- T0 fails (heuristics flag low success on this action class)
- T0 gets stuck (floundering detected)
- T0's advisor verdict says "this needs higher tier"

Over time, accumulated chain data tells us:
- Which action classes CAN'T succeed at T0 (auto-route to T1+)
- Which action classes need a T4 plan THEN T0 execution
- Which action classes are pure-T0 (no escalation ever needed)

The chitin chain + lessons-curator (PR #224) + router-heuristics
together form the data substrate for this. The accumulated
"escalation telemetry by action class" becomes input to a
tier-router-by-data primitive that supersedes the static
tier→driver map.

### Concrete shipping pieces (each is its own scope-down entry)

1. **`router-plugin-side-effect-gate`** (T3, ~300 LOC) — plugins'
   own tool calls go through the kernel. Plugin runtime emits
   structured action declarations; kernel's gov.Gate evaluates
   each before plugin proceeds.
2. **`router-plugin-static-audit`** (T3, ~400 LOC) — static-
   analysis pass over plugin code (Python AST, TS AST) before
   load. Reports declared capability set; reject if it exceeds
   manifest's declared scope.
3. **`mcp-tool-governance`** (T2, ~250 LOC) — extend the
   PreToolUse hook adapter to recognize MCP tool calls (today
   covered partially). Each MCP tool call → kernel verdict.
4. **`recursive-escalation-budget`** (T2, ~150 LOC) — bound the
   chain depth across recursive escalations. Today the router
   has chain.max_depth=3 for advisor chains; same primitive
   needs to apply to agent escalations.
5. **`tier-router-by-data`** (T3, ~500 LOC) — new heuristic +
   policy primitive: read chain telemetry for action_class →
   {success_rate by tier} → recommend starting tier. Operator
   reviews + promotes recommendations. Eventually replaces
   static tier→driver map.
6. **`pre-action-analysis-plugin-class`** (T2, ~200 LOC) — new
   plugin TYPE: `pre_action_analysis`. Runs analysis BEFORE the
   action it gates (e.g., `nx affected -t test` before git
   commit). Returns block/allow + reason. Different shape from
   heuristic plugins (which only score; pre-action plugins can
   block).

### Bulletproofness checklist (operator-facing)

For chitin to be bulletproof, every action surface must be
governable. The current matrix (2026-05-03 evening):

| Surface | Governed? | Where |
|---|---|---|
| Claude Code agent tool calls | ✓ | PreToolUse hook → router |
| Copilot CLI tool calls | ✓ | acpx hook → router |
| Openclaw agent tool calls | ✓ | chitin-governance plugin |
| Router heuristic plugins (input scoring) | partial | trust allowlist (PR for `router-plugin-allowlist`) |
| Router heuristic plugins (their own side effects) | NO | `router-plugin-side-effect-gate` (T3) |
| Router advisor recommendations | partial | advisor returns nudge; agent's enacting tool call IS governed |
| MCP tools | partial | `mcp-tool-governance` (T2) |
| Higher-tier escalation tool calls | ✓ | recursive — same hook fires |
| Pre-action analysis (e.g., test-before-commit) | NO | `pre-action-analysis-plugin-class` (T2) |
| Plugin static capability declaration | NO | `router-plugin-static-audit` (T3) |

This entry's job is the rollup + breakdown. Each row of the
matrix becomes its own scope-down entry as it ships.

**Acceptance:**
- [ ] Each matrix row reaches "✓" status (or operator decision: "won't ship — explain why")
- [ ] tier-router-by-data primitive proven against 30+ days of chain telemetry
- [ ] End-to-end smoke: a swarm dispatch starts at T0, escalates through router heuristics + advisor consultation, reaches successful completion (or operator-takeover) without any operator manual intervention

## Cohort: gemini + codex governance follow-ups (filed 2026-05-04)

Filed after PRs #267 (gemini-cli governance), #268 (codex_mine), #269 (universal usage schema), #270 (codex reviewer driver). These five are the deferred items called out in those PR descriptions.

### `gemini-usage-feed-producer`

```yaml
id: gemini-usage-feed-producer
tier: T2
status: ready
estimated_loc: 200
blocks: []
file: apps/temporal-worker/src/activity.ts, python/analysis/gemini_mine.py (new)
role: programmer
```

Gemini doesn't expose quota the way codex does (no `rate_limits` in its session JSONL today). Need to scrape gemini's stderr for rate-limit signatures (`429`, `quota exceeded`, `daily limit reached`) and project them into the universal usage-feed schema at `~/.cache/chitin/usage/gemini.json`. Schema in `docs/runbooks/usage-feeds.md`; producer interface mirrors `analysis.codex_mine.usage_to_feed`.

Activity-layer integration: when an activity spawns gemini, pipe stderr through a scraper that detects rate-limit lines and writes a transient warning. A periodic timer aggregates the warnings into the feed file, same shape as the codex feed.

**Acceptance:**
- [ ] `~/.cache/chitin/usage/gemini.json` exists after a gemini run that exercises tools
- [ ] `chitin-budget --check` exits 1 if a rate-limit was seen in the last hour
- [ ] Live-tested against a real gemini session (with intentional throttle if possible)

### `ollama-cloud-usage-capture`

```yaml
id: ollama-cloud-usage-capture
tier: T2
status: ready
estimated_loc: 250
blocks: []
file: apps/temporal-worker/src/activity.ts, libs/adapters/openclaw/ (existing)
role: programmer
```

Ollama Cloud surfaces quota via response headers (`X-RateLimit-Remaining`, `X-RateLimit-Reset`, etc) on every API call. Capture those at the activity layer (or in the openclaw plugin if openclaw's the dispatch path) and emit a `~/.cache/chitin/usage/ollama-cloud.json` feed with `axis: rpm_tpm`.

Why this matters: ollama-cloud is the local-tier fallback when 3090 is busy or off; without quota visibility, dispatching to ollama-cloud right after hitting the 5h codex window is a pattern that'll silently degrade. Universal usage feed surfaces both.

**Acceptance:**
- [ ] Feed file populated after a 3090-fallback dispatch
- [ ] `chitin-budget` renders an ollama-cloud row with `rpm` + `tpm` percentages
- [ ] Stale-feed detection in chitin-status (warn if `last_observed > 30min`)

### `codex-via-openclaw-collapse`

Upstream dependency: openclaw codex provider support. Status flips to `ready` when openclaw releases it. Tracked here as `blocked` because there's no parent entry inside chitin to express the dependency from (the parser's `blocks:` field reads from the BLOCKER's frame, but the blocker is external).

```yaml
id: codex-via-openclaw-collapse
tier: T2
status: blocked
estimated_loc: 100
blocks: []
file: apps/temporal-worker/src/activity.ts (planInvocation case), libs/contracts/src/execution-request.schema.ts (no change)
role: programmer
```

PR #270 ships `codex` as a direct-exec driver. Today's openclaw (2026.4.25) has providers `{ollama, ollama-cloud}` only — no codex provider. Sam Altman publicly stated codex CLI auth can power openclaw (i.e., openclaw drives `codex exec` per turn under the operator's ChatGPT Plus credentials, no OpenAI API key needed). When openclaw ships that, the codex case in `activity.ts::planInvocation` collapses into the local-* / openclaw branch — same chitin gate surface as the other openclaw-managed agents.

Note: PR #272 ships REAL-TIME codex governance via the codex-cli PreToolUse hook (codex 0.128.0+), so this entry is no longer a security-only concern — it's about consolidating dispatch through openclaw for consistency.

**Acceptance:**
- [ ] codex dispatch goes through openclaw, not direct `codex exec`
- [ ] chitin's `before_tool_call` plugin fires for codex tool calls, gating in real time
- [ ] `codex_mine` post-hoc ingest still runs as a safety net; chain has both pre and post records for the same call
- [ ] Direct `codex exec` path either removed OR retained as fallback when openclaw isn't running

### `chitin-codex-chain-ingest-timer`

```yaml
id: chitin-codex-chain-ingest-timer
tier: T1
status: ready
estimated_loc: 80
blocks: []
file: infra/systemd/chitin-codex-chain-ingest.{service,timer}, scripts/chitin-codex-chain-ingest.sh
role: programmer
```

PR #269 ships a `chitin-codex-usage-feed.timer` (10-min cadence) that refreshes the budget feed but NOT the chain events. Add a parallel `chitin-codex-chain-ingest.timer` that runs `python -m analysis.codex_mine ingest --out-dir ~/.chitin` on a longer cadence (1h is enough — chain is for analysis, not real-time). After it lands, `chain stats` and `skill_mine` see codex traffic alongside Claude Code and gemini, closing the cross-driver mining loop.

Why two separate timers: usage feed needs to be fresh (operator dashboard), chain ingest is bulk-process (analysis only). Splitting the cadence keeps the cheap thing cheap.

**Acceptance:**
- [ ] After 24h: `~/.chitin/codex-events-*.jsonl` files exist for every codex session
- [ ] `chitin-kernel chain stats --by=action_type` shows codex `shell.exec` calls alongside other drivers
- [ ] `python -m analysis.skill_mine` includes codex sessions in its n-gram surface (chain_id grouping handles it for free)

### `nx-target-consistency-and-validate`

```yaml
id: nx-target-consistency-and-validate
tier: T2
status: ready
estimated_loc: 300
blocks: []
file: nx.json, project.json (across apps/*), apps/cli/tsconfig.json, package.json, .github/workflows/ci.yml
references_finding: 2026-05-04 codex review of chitin repo (operational-polish gaps)
role: programmer
```

Codex's read of the chitin repo (2026-05-04) flagged operational-polish gaps:

1. `nx run cli:build` is broken (referenced in `README.md:16` quickstart but only `execution-kernel` actually has a buildable Nx target)
2. App-level TS typecheck targets are effectively disabled because `noEmit: true` conflicts with Nx's inferred typecheck flow (`apps/cli/tsconfig.json:8` is the canonical example)
3. No top-level `validate` target unifying Go tests + TS tests/typecheck + Python pytest + boundary lint

Fixes:
- (a) Make `nx run cli:build` actually work, OR remove the README reference (don't ship a broken quickstart)
- (b) Restore TS typecheck for app projects via per-project `typecheck` target that doesn't conflict with `noEmit`
- (c) Add a `nx run-many --target=validate` (or root package.json script) that runs the full validation matrix; wire into CI as the merge gate

**Acceptance:**
- [ ] `nx run cli:build` succeeds OR README updated to reflect reality
- [ ] `nx run-many --target=typecheck` passes across all app projects
- [ ] A single `pnpm validate` (or equivalent) runs Go tests + TS tests + TS typecheck + Python pytest + boundary lint, all of which pass
- [ ] CI workflow uses the unified target

## Ecosystem opportunity — chain as the substrate (filed 2026-05-03 evening)

### `chitin-ecosystem-opportunity-rollup`

```yaml
id: chitin-ecosystem-opportunity-rollup
tier: T5
status: in_design
estimated_loc: TBD (rollup of independently-shippable pieces below)
blocks: []
file: TBD
references_finding: 2026-05-03 evening operator framing — chitin's chain + governance substrate enables a marketplace of plugins, replay/simulation tooling, predictive models, and third-party analysis
role: architect
```

Operator framing 2026-05-03 evening:

> "I really like this because that means that everything that an
> agent does becomes auditable. Replayable, you know. What's the
> thing where you can, like, simulate? Like, you can simulate and
> what will happen based off of past data and heuristics. Eventually
> we could have you know, a Python, like, kind of model that runs.
> There's just so many things we can do with this kind of setup.
> And by we I mean like the ecosystem. If people get in line with
> it and like it it could there's a lot that could be done here,
> right, that could add you know, awesome, like, governance,
> heuristics, analysis on the chain, things like that."

Tonight's substrate (chain + router + plugins + advisor) enables
an ecosystem layer chitin doesn't have to build all of. Each
ecosystem capability is its own scope-down entry; this rollup
keeps them aligned with the substrate.

### What the chain enables

| Capability | What it is | Why chitin's substrate uniquely enables it |
|---|---|---|
| **Auditability** | Operator + auditor can reconstruct any session's full decision trail from chain events | Hash-chained events make tampering detectable; OTEL projection makes external auditors viable |
| **Replayability** | Replay a session's events into a sandbox, re-evaluate gate decisions against current policy → see what TODAY's policy would have done | Chain captures every input + decision; gov.Gate is pure-function over (policy, action) |
| **Simulation** | Given a proposed action + heuristics + recent chain, predict outcome (would advisor fire? would gate deny?) | Heuristics are pure; advisor + kernel are wrap-able as a simulator harness |
| **Predictive model** | Python ML model trained on chain → "this action class on this driver succeeds 87% of the time" | Chain is structured + emits per-decision telemetry; analysis lib already in chitin (`python/analysis/`) |
| **Counterfactual analysis** | "What if we had used T0 instead of T3 here?" — re-run the entry through a different tier | Tier-router is policy-driven; pure functions over input |
| **Plugin marketplace** | Third parties ship heuristic + advisor plugins; operator declares trust policy | Plugin loader (PR #235) + trust allowlist (PR #237) — substrate is in place |
| **Cross-org governance baselines** | Industry-standard policy bundles (security baseline, financial-compliance baseline) shipped as chitin.yaml fragments | Policy is YAML; rules are composable |

### Concrete shipping pieces (each is its own scope-down entry; T2-T3)

1. **`chain-replay-cli`** (T2, ~250 LOC) — `chitin chain replay <session_id> [--policy <path>]` re-evaluates a session's gate decisions against current (or specified) policy. Output: per-event decision diff (today_decision vs replay_decision).

2. **`chain-simulate-action`** (T2, ~200 LOC) — `chitin simulate <hook_input.json>` runs the heuristics + advisor against a synthetic action without executing it. Useful for operator's "would this be allowed?" pre-check.

3. **`analysis-action-success-by-driver`** (T2, ~300 LOC, Python) — extends `python/analysis/swarm_health.py` with per-action_class-by-tier success rates. Output: input data for `tier-router-by-data` (filed in `everything-governs-everything-recursively`).

4. **`chain-predict-outcome`** (T3, ~500 LOC, Python) — Naive-Bayes or logistic-regression model trained on chain events: given (action_type, target, agent, recent_outcomes), predict P(success). Reads trained model at hook time; advisor.when can include `predicted_failure_above_threshold`.

5. **`policy-bundles-marketplace-spec`** (T3, ~400 LOC docs + schema) — declared schema + manifest for sharable chitin.yaml policy bundles. Operator: `chitin policy install @<vendor>/<bundle>` — pulls from a registry, verifies signature, installs into chitin.yaml under a `policies:` array. Bundles are composable.

6. **`plugin-marketplace-spec`** (T3, ~400 LOC docs + schema + example) — declared shape for npm-distributable router plugins. Includes `@chitin/router-plugin-api` package, plugin manifest schema, example plugin, and registry-hosting decision (npm vs custom).

7. **`chain-snapshot-export`** (T2, ~200 LOC) — `chitin chain export <session_id> --format=json|otlp|sigstore` produces an immutable snapshot suitable for external audit / regulatory submission. Includes hash-chain proof.

8. **`chain-replay-as-memory-context`** (T3, ~400 LOC) — when a NEW agent picks up a task related to a prior session, surface the prior session's chain as MEMORY CONTEXT in the new agent's prompt. Operator framing 2026-05-03 evening: "the next agent can easily replay what's happened.. and we can have that replay in our memory layer too." Concrete shape: `chitin chain summarize <session_id>` produces a markdown summary suitable for prompt injection (decisions made, files touched, open questions, advisor nudges). Related-session detection: heuristic match on entry_id substring + file paths overlap. Composes with existing shared-memory primitive (per-workflow JSON scratchpad in PR #230) and the Hindsight evaluation track for cross-context recall.

### What this means strategically

Chitin becomes a **substrate**, not a product. The product layer (replay UI, simulation playground, predictive dashboard, plugin marketplace, policy bundles) can come from the ecosystem.

This is the verifiable-execution-layer pitch made concrete: chitin
provides the chain + governance primitives; everyone else builds
on top. Chitin's role: keep the substrate sharp (kernel
deterministic, chain immutable, hooks fast), publish stable
APIs, document the patterns.

The two operator-asks today — "make chitin bulletproof" and
"the ecosystem could do a lot here" — are the same ask viewed
from inside-out vs outside-in: a bulletproof substrate IS what
makes ecosystem participation safe + valuable.

**Acceptance:**
- [ ] At least 3 of the 7 scope-down entries shipped (chitin
      proves the substrate by building the most critical
      consumer-side pieces first)
- [ ] At least 1 third-party-shaped artifact: a policy bundle OR
      a plugin OR a docs landing page that's operator-followable
      from "I want to write a chitin plugin" to "my plugin runs"
- [ ] OTEL emit + chain snapshot proven across the wire (someone
      else can ingest chitin's chain into their own tooling)
