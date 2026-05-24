# Chitin Factory — System State, 2026-05-23

Detailed multi-level system diagram showing what's **done**, what's **in progress**, and what's **future**. Five levels: from the operator's mental model down to the spec / file inventory.

## Status legend

| Symbol | Meaning |
|---|---|
| `✅` | **DONE** — on `main`, exercised in production |
| `🟡` | **MERGED, NEEDS VERIFICATION** — code on main but no live end-to-end run yet |
| `🔨` | **IN PROGRESS** — open PR or partial impl |
| `📋` | **PLANNED** — spec authored, no impl yet |
| `💭` | **IDEA** — discussed in-session, not yet spec'd |
| `🗑️` | **DEPRECATED / RETIRED** — being or already removed |

---

## Level 0 — The operator's loop

```
                            ┌──────────────────────────┐
                            │   1. Operator writes      │
                            │   spec in .specify/specs/ │
                            └──────────┬────────────────┘
                                       │
                                       ▼
                            ┌──────────────────────────┐
                            │   2. Trigger:             │
                            │   CLI / push webhook /    │
                            │   GitHub issue            │
                            └──────────┬────────────────┘
                                       │
                                       ▼
                            ┌──────────────────────────┐
                            │   3. Orchestrator picks   │
                            │   the right driver        │
                            └──────────┬────────────────┘
                                       │
                                       ▼
                            ┌──────────────────────────┐
                            │   4. Driver opens PR      │
                            └──────────┬────────────────┘
                                       │
                                       ▼
                            ┌──────────────────────────┐
                            │   5. spec 094 reviews PR  │
                            │   (dialectic verdict)     │
                            └──────────┬────────────────┘
                                       │
                                       ▼
                            ┌──────────────────────────┐
                            │   6. Human / merge queue  │
                            │   merges (spec 093 v1.1)  │
                            └──────────┬────────────────┘
                                       │
                                       ▼
                            ┌──────────────────────────┐
                            │   7. Telemetry → sentinel │
                            │   → operator dashboards   │
                            └──────────────────────────┘
```

Steps 1, 4, 5, 6 are operator-facing. Steps 2, 3, 7 are factory-internal.

---

## Level 1 — System surfaces with status

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              CHITIN FACTORY                                      │
│                                                                                  │
│  ┌────────────────────┐    ┌────────────────────┐    ┌────────────────────┐    │
│  │   SPEC AUTHORING   │    │   TRIGGER SURFACE  │    │   ORCHESTRATOR     │    │
│  │                    │    │                    │    │                    │    │
│  │  ✅ spec-kit       │    │  ✅ schedule CLI   │    │  ✅ Scheduler      │    │
│  │  ✅ spec lint      │───▶│     (spec 097)     │───▶│     (spec 070+076) │    │
│  │  ✅ INDEX.md       │    │  🔨 webhook RX     │    │  ✅ WorkUnit       │    │
│  │     convention     │    │     (spec 098)     │    │     workflow       │    │
│  │                    │    │  📋 Copilot issue  │    │  ✅ Worktree mgr   │    │
│  │                    │    │     (spec 099)     │    │  ✅ DAG compile    │    │
│  └────────────────────┘    └────────────────────┘    └─────────┬──────────┘    │
│                                                                 │                │
│                                                                 ▼                │
│  ┌────────────────────┐    ┌────────────────────┐    ┌────────────────────┐    │
│  │   GOVERNANCE       │◀──▶│   DRIVERS          │◀───│   DRIVER REGISTRY  │    │
│  │                    │    │                    │    │                    │    │
│  │  ✅ chitin-kernel  │    │  ✅ codex          │    │  ✅ spec 075       │    │
│  │  ✅ gate evaluate  │    │  ✅ gemini         │    │     capability     │    │
│  │  ✅ session        │    │  ✅ hermes         │    │     cards          │    │
│  │     lock/unlock/   │    │  ✅ claudecode     │    │  ✅ select-driver  │    │
│  │     status         │    │  ✅ copilot (CLI)  │    │     activity       │    │
│  │     (spec 096)     │    │  ✅ openclaw       │    │  ✅ no-self-review │    │
│  │  ✅ stop-hook      │    │  ✅ local          │    │     exclusion      │    │
│  │     recovery       │    │  📋 copilot        │    │     (spec 094)     │    │
│  │     (spec 091 v1.1)│    │     (GitHub-native)│    │                    │    │
│  └────────────────────┘    └─────────┬──────────┘    └────────────────────┘    │
│                                       │                                          │
│                                       ▼                                          │
│  ┌────────────────────┐    ┌────────────────────┐    ┌────────────────────┐    │
│  │   REVIEW SURFACE   │    │   MERGE SURFACE    │    │   TELEMETRY        │    │
│  │                    │    │                    │    │                    │    │
│  │  🟡 PR review      │    │  📋 merge queue    │    │  ✅ kernel chain   │    │
│  │     workflow       │    │     (spec 093)     │    │     (events-*.    │    │
│  │     (spec 094)     │    │  ✅ GitHub merge   │    │      jsonl)        │    │
│  │  🟡 dialectic      │    │     (manual)       │    │  ✅ sentinel       │    │
│  │     verdict math   │    │  💭 governance     │    │     (analyzer +    │    │
│  │  🟡 4-value        │    │     classes        │    │      Neon)         │    │
│  │     StructuredV.   │    │  💭 review_req     │    │  ✅ Discord notif. │    │
│  │  💭 class-routed   │    │     col on PRs     │    │     (spec 080)     │    │
│  │     arbiter        │    │     (094 v1.1)     │    │  🗑️ /evolve        │    │
│  └────────────────────┘    └────────────────────┘    └────────────────────┘    │
│                                                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

---

## Level 2 — Per-surface detail

### 2.1 Spec authoring surface

```
operator
   │
   ▼
write .specify/specs/NNN-name/{spec,plan,tasks}.md
   │
   ▼
✅ scripts/check-spec-frontmatter.py           (spec linter; runs in pre-commit + CI)
✅ scripts/check-spec-index-sync.sh            (catches INDEX.md drift)
✅ scripts/regen-spec-index.py                 (one-shot INDEX rebuild)
✅ .claude/skills/speckit-{specify,plan,tasks,checklist}/  (interactive authoring)
✅ chitin-kernel speckit-lint                  (deterministic format linter, spec 088)
   │
   ▼
operator runs `git push` → push reaches main → spec is live
   │
   ▼
🔨 spec 098 webhook detects tasks.md change → auto-dispatch  ──┐
✅ operator runs `chitin-orchestrator schedule <ref>` manually ─┘── into orchestrator
📋 spec 099 — operator opens GitHub issue, assigns @copilot
```

### 2.2 Trigger surface

```
                        ┌─────────────────────────────────────┐
                        │   THREE TRIGGER PATHS               │
                        └─────────────────────────────────────┘

  ✅ CLI (spec 097, on main)                  🔨 Webhook (spec 098, PR #946)
  ────────────────────────                    ──────────────────────────────
  chitin-orchestrator schedule <ref>          GitHub push webhook
       │                                              │
       │ resolve spec ref                             │ verify HMAC
       │ compile DAG via spec 077                     │ extract spec refs from
       │ validate against driver registry             │   commits[].added/modified
       │ dial Temporal                                │ for each: invoke runSchedule
       │ ExecuteWorkflow(SchedulerWorkflow)           │ emit factory_triggered
       │ emit scheduler_started                       │
       ▼                                              ▼
  Same SchedulerWorkflow                       Same SchedulerWorkflow

                                  📋 Copilot issue (spec 099, design only)
                                  ─────────────────────────────────────────
                                  chitin-orchestrator schedule --driver copilot <ref>
                                       │
                                       │ NOT a SchedulerWorkflow run
                                       │ gh issue create on target repo
                                       │ assign to @copilot
                                       │ emit copilot_dispatched
                                       ▼
                                  Copilot drafts PR inside GitHub
                                       │
                                       ▼
                                  Webhook detects pull_request.opened
                                       │
                                       ▼
                                  Routes through spec 094 review
```

### 2.3 Orchestrator surface (spec 070 + 076 + 077)

```
SchedulerWorkflow (spec 076)
   │
   │ tick loop with deterministic order
   │ for each runnable node:
   │
   ▼
✅ SelectDriver activity (spec 075)
   │ - capability match
   │ - registry filter
   │ - no-self-review exclusion (spec 094)
   │
   ▼
✅ WorkUnitWorkflow per node
   │
   ▼
✅ CreateWorktree activity (spec 070)
   │ - fresh worktree per unit under CHITIN_WORKTREE_ROOT
   │
   ▼
✅ Driver.Drive (spec 075 contract)
   │ - chosen driver runs in the worktree
   │ - returns PR ref + status
   │
   ▼
✅ EmitTelemetry activity (spec 070 FR-008)
   │ - OTLP/HTTP exporter, no-op if not configured
   │
   ▼
✅ Notification activity (spec 080)
   │ - Discord webhook, no-op if CHITIN_DISCORD_WEBHOOK_URL unset
   │
   ▼
✅ TeardownWorktree activity

📋 Schedule-backed cron migrations (spec 081 US2)
   - EnsureSchedules at worker-host startup
   - swarm-audit already migrated; ~14 others queued
```

### 2.4 Driver surface (spec 075)

| Driver | ID | Capabilities | Status | Where it runs |
|---|---|---|---|---|
| Claude Code | `claudecode` | `code.implement`, `code.review` | ✅ DONE | Local worktree |
| Codex | `codex` | `code.implement` | ✅ DONE | Local worktree |
| Copilot CLI | `copilot` | `code.implement` | ✅ DONE | Local worktree |
| Gemini | `gemini` | `code.implement` | ✅ DONE | Local worktree |
| Hermes | `hermes` | `code.implement` | ✅ DONE | Local worktree |
| openclaw | `openclaw` | `code.implement` | ✅ DONE | Local worktree |
| local | `local` | `code.implement` (operator hand-off) | ✅ DONE | Operator's IDE |
| **Copilot (GitHub-native)** | `copilot-gh` | `code.implement` via issue assign | 📋 spec 099 | **GitHub infra** |

The "GitHub-native" Copilot is structurally different — does NOT run in our worktree. Spec 099 makes the orchestrator aware of this via the producer (issue-create) / consumer (PR-detect) split.

### 2.5 Review surface (spec 094)

```
✅ PR opened (any source)
   │
   ▼
🟡 PRReviewWorkflow starts                  [on main but needs more live exercise]
   │
   ▼
🟡 SelectDriver × 2 (capability=reviewer, exclude=author)
   │ - parallel two-primary dispatch
   │
   ▼
🟡 Driver.Review per reviewer
   │ - returns StructuredVerdict
   │   {approve / approve-with-comments / request-changes / abstain}
   │
   ▼
🟡 Dialectic short-circuit on agreement
   │ - both approve → APPROVED
   │ - both request-changes → CHANGES_REQUESTED
   │ - disagreement → arbiter
   │
   ▼
💭 Class-routed arbiter [NOT YET LIVE]
   │ - governance class → operator (structured GitHub comment)
   │ - spec-only class → operator
   │ - impl / research-docs → third machine driver
   │
   ▼
🟡 Verdict commented on PR
   │
   ▼
💭 re-review / override-review signals
   │ - governance verdicts non-overridable per spec 094 FR-014
```

### 2.6 Governance surface (chitin-kernel)

```
✅ kernel CLI                  ✅ event chain                   ✅ session state
   ├─ gate evaluate              ├─ events-<run_id>.jsonl         ├─ unlock (spec 096)
   ├─ emit                       ├─ seq + this_hash + prev_hash   ├─ lock (operator)
   ├─ session unlock             ├─ chain_id framing              ├─ status
   ├─ session lock               └─ kernel is sole writer (§1)    └─ unlock_ts +
   ├─ session status                                                 lock_epoch cols
   └─ speckit-lint                                                   (spec 096)

✅ stop-hook recovery (spec 091 v1.1)
   ├─ openclaw plugin captures lock_epoch via session status
   ├─ stop:true honored AND kernel-unlock detected → clear stopHookActive
   ├─ chain-emits stop_hook_cleared
   └─ closes the original "clawta channel dying" loop

✅ governance-mutation-authority-required gate
   └─ blocks agent self-invocation of kernel state changes

✅ bounds:max_lines_changed gate (2000-line ceiling)
   └─ Honored — caused agent-side bounds cascade earlier this session

🔨 git-ops-recorder (post-merge of #937, observability not gate)
   ├─ reference-transaction + post-checkout hooks
   ├─ process-tree walk → ~/.chitin/git-ops.jsonl
   └─ replay tool: swarm/bin/git-ops-replay
```

### 2.7 Telemetry surface

```
✅ kernel chain (events-*.jsonl)
   │
   ▼
✅ sentinel ingestion (/sentinel skill)
   │ - GitHub Actions logs → Neon execution_events
   │ - 7 analyzer passes for failure detection
   │ - findings routed to ops surfaces
   │
   ▼
✅ /sentinel mine → invariant proposals (operator-gated)

🗑️ /evolve  (RETIRED — chitinhq/workspace PR #424)

🔨 Discord notifications (spec 080, on main)
   └─ write-only, no-op when CHITIN_DISCORD_WEBHOOK_URL unset

💭 Telemetry-recovery sentinel adapter [NOT YET SPEC'D]
   └─ How sentinel consumes copilot_pr_activity events
      (spec 099 FR-013) alongside execution_events.
      Would close the spec 099 telemetry-loss gap.
```

---

## Level 3 — Spec inventory by status

### Recent specs (070+)

| Spec | Title | Status | Notes |
|---|---|---|---|
| **070** | chitin-orchestrator | ✅ DONE | Temporal worker host, worktrees, registry — substrate for everything below |
| **075** | agent-driver-contract | ✅ DONE | 7 drivers registered |
| **076** | spec-dag-scheduler | ✅ DONE | SchedulerWorkflow + WorkUnitWorkflow |
| **077** | spec-kit-adapter | ✅ DONE | tasks.md → DAG compile |
| **078** | self-improvement-loop | 🔨 Partial | ImprovementLoopWorkflow registered with nil deps (safe defaults) |
| **079** | information-ingestion-pipeline | 🔨 Partial | IngestionWorkflow registered with nil deps |
| **080** | orchestrator-ops-completion | ✅ DONE | Gemini + Copilot drivers + Discord notifier |
| **081** | cron-migration-board-retirement | 🔨 Partial | swarm-audit migrated; ~14 others queued |
| **082** | agent-cron-audit | 📋 Draft | Audit existing cron jobs for orchestration migration |
| **083** | driver-governance-telemetry | 📋 Draft | Per-driver governance telemetry |
| **084** | sdd-admission-gate | 📋 Draft | Spec-driven-development gate |
| **085** | operator-report-delivery | 📋 Draft | Delivery channels for operator reports |
| **086** | event-hash-consolidation | 📋 Draft | Chain event hash consolidation |
| **091** | fix-clawta-lockdown-loop | ✅ DONE | v1.0 (sticky stopHookActive) + v1.1 (operator-unlock recovery) both on main |
| **092** | codify-swarm-orchestrator | ✅ DONE | Constitution §7 — the swarm IS the orchestrator |
| **093** | merge-queue-orchestrator | 📋 Draft | Spec + plan + Phase 1 contracts merged; impl pending |
| **094** | pr-review-mechanism | 🟡 MERGED | US1 MVP on main; needs more live exercise |
| **096** | operator-session-state-surface | ✅ DONE | `session unlock/lock/status`; consumed by spec 091 v1.1 |
| **097** | operator-scheduler-entrypoint | ✅ DONE | `schedule/status/cancel` CLI; live-verified |
| **098** | factory-webhook | 🔨 PR #946 | Webhook receiver + simulator + 23 tests; awaiting merge |
| **099** | github-native-dispatch | 🔨 PR #947 | Design-only; Copilot driver via issue assignment; explicit telemetry tradeoff |

### Ideas surfaced this session, not yet spec'd

| Idea | Origin | What it solves |
|---|---|---|
| Telemetry-recovery sentinel adapter | spec 099 telemetry risk | Closes the visibility gap for Copilot-dispatched work |
| Auto-routing policy (Copilot vs local) | "we have a budget on Copilot" | Encodes operator's mental routing rule into code |
| Cross-org GitHub App manifest | "private, chitinhq, organization, benchdevs" | Multi-org installation of the webhook receiver |
| Spec authoring from GitHub issues | spec 099 "out of scope" callout | Lets non-orchestrator humans propose specs via issue body |
| Bounds-gate fail-soft escape hatch | The 3-PR bounds-cascade lockout this session | Operator pre-authorizes high-LOC PRs |

---

## Level 4 — Active PRs

### Open right now

| PR | Repo | Title | Status |
|---|---|---|---|
| **#946** | chitinhq/chitin | spec 098 webhook impl | 🔨 awaiting Copilot review + checks |
| **#947** | chitinhq/chitin | spec 099 design | 🔨 design-only, awaiting review |
| **#945** | chitinhq/chitin | spec 097 target_repo fix follow-up | 🔨 awaiting checks |
| **#424** | chitinhq/workspace | /evolve retirement | 🔨 trivial, awaiting merge |
| **#416** | chitinhq/workspace | overnight 2026-05-17 observations | 🔨 long-running operator branch |

### Recently merged (this session)

| PR | What |
|---|---|
| #944 | spec 091 v1.1 — operator-unlock recovery in openclaw plugin |
| #943 | post-merge review followups for spec 096 + 097 |
| #942 | spec 097 impl Part B (schedule/status/cancel handlers) |
| #941 | spec 097 impl Part A (dispatcher scaffold) |
| #940 | spec 096 impl (kernel session-state surface) |
| #938 | install-kernel.sh dedicated-worktree fix |
| #937 | git-ops recorder |
| #936 | spec 097 design |
| #935 | spec 096 design + 091 v1.1 amendment |
| #933 | spec 094 PR review mechanism — US1 MVP |
| #929 | spec 093 merge queue — spec + plan + contracts |
| #928 | speckit-lint deterministic format linter |

---

## Level 5 — Operator follow-ups, by priority

### Soon (this week)

1. **Land PR #946** (spec 098) — webhook receiver is the trigger substrate spec 099 depends on
2. **Land PR #424** (/evolve retirement) — trivial cleanup
3. **Live-exercise spec 094** — route one of the three open PRs through `PRReviewWorkflow` to prove it works end-to-end on real PRs (not just CI tests)
4. **Implement spec 099 US2** — the cheapest Copilot impl path: PR-detection-only, no `--driver` flag yet. Operator manually creates the issue + assigns @copilot; orchestrator picks up the resulting PR

### Next (this month)

5. **Author the telemetry-recovery sentinel adapter spec** — closes the spec 099 telemetry blind spot at the data-model level
6. **Implement spec 093** (merge queue) — Phase 1 contracts are merged; impl is the next phase
7. **Land spec 094 v1.1 amendment** — `review_required` + `arbiter_type` columns once class-routed arbiter is live
8. **Operator runbooks** — `docs/operator/factory-webhook.md`, `docs/operator/copilot-driver.md`, `docs/operator/session-state.md` (the last one is already on main from spec 096 but needs review)

### Eventually

9. **Spec the bounds-gate escape hatch** — fired three times this session and required operator unlock, indicating the gate's threshold-vs-cascade behavior needs design work
10. **Cross-org GitHub App manifest** — multi-org factory webhook deployment
11. **Auto-routing policy** — when does the orchestrator pick Copilot vs. local without operator flag

---

## Summary

The **factory substrate is done**. Trigger surfaces (`schedule` CLI, webhook receiver, future Copilot dispatch), orchestrator core (Scheduler + WorkUnit workflows + worktrees), driver registry (7 local drivers), governance (kernel + session state + stop-hook recovery), and review surface (spec 094 dialectic) are all on `main` or one PR away.

The **operator-facing loop** is live for local drivers — push a spec → dispatch via CLI → driver runs → PR gets reviewed. The webhook receiver makes that automatic; the Copilot driver makes it scale to GitHub's infrastructure (at the cost of telemetry granularity).

The **next gap** is telemetry parity between local and Copilot-dispatched work. That's the spec 099 + future sentinel-adapter story.

**What's NOT done and worth knowing about:**

- Merge queue (spec 093) — currently humans merge by hand
- Class-routed arbiter for review disagreement (spec 094 v1.1) — currently disagreements stay open
- Live exercise of spec 094 on real PRs — the impl is on main but the dialectic verdict has not yet routed a real PR to a real merge
- /evolve replacement — retired without a clear successor; sentinel still does the analysis side, but the "bench evolution" half is gone
