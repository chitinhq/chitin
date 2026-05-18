# Swarm Performance Comparison — Old vs New Architecture

> **Date:** 2026-05-18 (Day 1 of new architecture)
> **Author:** red
> **Companion:** [`2026-05-18-swarm-redesign.md`](2026-05-18-swarm-redesign.md) (PR #752)
> **Status:** initial baseline — needs 1-2 weeks of steady-state for sustained-load comparison

## Caveat (read first)

The new architecture went live this morning (2026-05-18 ~08:30 EDT). This document compares **1 day of new arch** data against **7-30 days of old arch** data. Sample sizes are wildly different; small-n bias on the new side. Treat structural/architectural improvements as certain; treat throughput claims as plausible but unmeasured at scale.

## Data sources

- `~/.hermes/kanban/boards/chitin/kanban.db` — old swarm board (multi-week history)
- `~/.hermes/kanban/boards/swarm/kanban.db` — new swarm board (Day 1)
- `~/.hermes/kanban/boards/chitin/kanban_mutations_log` — state transition timestamps
- `gh pr list` queries against `chitinhq/chitin` — PR merge metadata

All queries logged in §Appendix for reproducibility.

---

## 1. PR cycle time (created → merged) — lower is better

| Metric | Old arch (5/11–5/17, 7 days) | New arch (today 5/18) | Δ |
|---|---|---|---|
| Sample size | 50 PRs | 15 PRs | — |
| Median (p50) | 0h | 0h | tied |
| **p90** | **9h** | **1h** | **9× faster on the long tail** ✅ |
| Average | 1h | 0h | tied |

**Read:** Median PR cycle time was already fast under both architectures. The headline win is **p90 collapsing from 9h to 1h** — meaning the worst 10% of PRs are no longer dragging for nearly half a day. This matches the redesign spec's "loud failure > silent recovery" principle: when something stalls, the architecture surfaces it immediately rather than absorbing the delay invisibly.

---

## 2. Daily PR-merge volume (informational only)

| Day | PRs merged to main |
|---|---|
| 5/14 | 10 |
| 5/15 | 28 |
| 5/16 | 12 |
| 5/17 | **35** (old arch peak; overnight goal sprint) |
| **5/18 (today)** | **15 (mid-day, new arch transition)** |

**Read:** New arch hasn't beaten old peak yet, but today is half-over and a chunk of today's PRs are *redesign-meta* work (#752 swarm-redesign spec, #760 sw-009 agent-invocation, etc.) — not steady-state product work. Throughput comparison requires a full week of new arch operation before drawing real conclusions.

---

## 3. Silent-death + failure-pattern incidents (the headline old-arch failure modes)

| Failure mode | Old (chitin, 30 days) | New (swarm, today) |
|---|---|---|
| Tickets stuck > 24h with no state change | **7** | 0 |
| Tickets tagged `block_reason=silent_death` | **3** | 0 |
| Tickets tagged `block_reason=no_pr` | 3 | 0 |
| Tickets tagged `block_reason=promote-demote loop detected` | **15** | 0 |
| Currently blocked | varies | 1 (sw-006, **intentional dependency** on sw-009 — not silent dead) |

**Read:** The three most-common old-arch failure modes (promote-demote loops, silent-death, no-PR) show **0 occurrences in new arch**. Sample is small, but the architectural cause has been removed: no driver subprocess to die silently, no opaque worker to fail without surface, no auto-retry loops that hide failures for 45 minutes. The redesign discarded the entire failure surface, not just the symptoms.

---

## 4. Operator-finishing burden (the salvage-self anti-pattern)

| Metric | Old (chitin, 30 days) | New (swarm, today) |
|---|---|---|
| Tickets assigned to `red` | **95** | 5 |
| Of those, done | 90 (95%) | 4 (80%) |
| Rate | ~3.2 / day | ~5 / day (small sample, biased by redesign work) |

**Read:** Old arch had operator finishing ~3 tickets/day on average. New arch — the 5 today were genuine red-lane work (sw-001 emergency meeting, sw-004 synthesis, sw-005 ratification gate, sw-006 Haiku Test author, sw-007 wake-up layer), **not** salvage-self. Structurally, salvage-self is impossible in new arch because there's no opaque worker to clean up after — owners post receipts and own failure end-to-end (Clawta's contribution: WORKER_RECEIPT contract).

---

## 5. Architectural-change metrics (qualitative but real)

| Dimension | Old | New | Improvement |
|---|---|---|---|
| LOC of dispatch infrastructure | ~500 (kanban-dispatch.lobster + _pick_driver.py + driver cards + ELO machinery) | ~340 (swarm-invoker + cron installer + controller) | **~32% less code** |
| Failure-detection latency | 45 min (stale-worker watchdog) | 5 min (push-notify-operator) | **9× faster** |
| Layers between ticket-ready and code-shipping | 4 (controller → dispatch → driver → worker subprocess) | 2 (controller → owner-agent) | **2× thinner** |
| Subprocess spawning for agent work | Yes — kanban-dispatch.lobster spawns driver | None | Eliminated |
| Merge gate | Variable / ad-hoc | Copilot auto-review + 1 peer agent (builder-OR-verifier invariant) | Standardized |
| Cooldown after retries | None formalized | 4hr after N=3 consecutive fails | New invariant |
| Routing mechanism | ELO scores + driver cards (`_pick_driver.py`) | `skill` field / owner field on ticket | Deterministic, no ML in the loop |
| Worker pool | External CLI driver subprocesses (codex / claude-code / copilot) | The three of us (red + Ares + Clawta) | Eliminates worker-as-blackbox |

---

## 6. What we cannot measure yet (honest gap list)

| Unknown | Why we can't measure | When we'll know |
|---|---|---|
| New arch under sustained load | 1 day of data; need 1-2 weeks of operation across swarm + personal-os | Week 2 of migration |
| Haiku Test pass rate | Round 3 (today ~14:40 EDT) FAILED — gap surfaced sw-009. Round 4 pending PR #760 merge. **The proof-of-life gate itself is unproven** | Once PR #760 lands + Round 4 runs |
| Operator escalations / day (steady-state) | Today had hermes-lockdown 4x but that's a **gov.db threshold issue** (sibling ticket t_2356307a), not a new-arch failure | Need 1 week of clean steady-state |
| End-to-end ticket-triage → ticket-done time | Need more cycles to build a credible histogram | Week 1+ |
| Cross-board generalization | Migration plan runs swarm → personal-os → chitin + readybench over 3 weeks. New arch unproven on production boards | Week 4 |

---

## 7. Headline takeaways

1. ✅ **PR cycle p90: 9h → 1h** (9× faster on the long tail) — most credible measurable win
2. ✅ **Silent-death + promote-demote + no-PR incidents: ~21 in 30 days (old) → 0 (new today)** — sample small, but architectural cause has been removed entirely
3. ✅ **Failure-detection latency: 45 min → 5 min** (9× faster, architectural guarantee)
4. ✅ **Salvage-self anti-pattern: structurally impossible** in new arch (no opaque worker to salvage)
5. ⚠️ **Proof-of-life unproven** — Haiku Test Round 4 still pending PR #760 merge
6. ⚠️ **Throughput parity unproven** — need full week of steady-state data

## 8. What to recheck in 1 week, 1 month

### Week-1 retro (target 2026-05-25)
- Re-run all queries against swarm board with 7 days of data
- Compare PR cycle time + throughput to chitin baseline
- Count silent-death incidents
- Count operator-escalations broken down by category (gov.db lockdown vs new-arch failure vs other)
- Validate Haiku Test repeatability (run 3-5 times, measure pass rate)

### Week-4 retro (target 2026-06-15, post full migration)
- Re-run all queries across swarm + personal-os + chitin + readybench
- Direct head-to-head comparison: same metric type, same time window, old arch vs new arch
- Decommission decision: ready to remove kanban-dispatch.lobster + _pick_driver.py + driver cards permanently?

---

## Appendix — Reproducible queries

### A.1 Chitin board (old arch) — last 7 days throughput

```sql
SELECT
  DATE(created_at,'unixepoch','localtime') as day,
  COUNT(*) as total,
  SUM(CASE WHEN status='done' THEN 1 ELSE 0 END) as done,
  SUM(CASE WHEN status='blocked' THEN 1 ELSE 0 END) as blocked,
  ROUND(AVG(CASE WHEN completed_at IS NOT NULL THEN (completed_at - created_at)/3600.0 END), 1) as avg_done_hrs
FROM tasks
WHERE created_at > strftime('%s','now','-7 days')
GROUP BY day ORDER BY day DESC;
```

### A.2 Chitin board — silent-dead (>24h no transition) in last 30 days

```sql
SELECT COUNT(*) as silent_dead_count
FROM tasks
WHERE created_at > strftime('%s','now','-30 days')
  AND status NOT IN ('done', 'cancelled')
  AND (
    SELECT MAX(ts) FROM kanban_mutations_log m
    WHERE m.task_id = tasks.id AND m.table_name='tasks'
  ) < strftime('%s','now','-1 day');
```

### A.3 Chitin board — block_reason distribution

```sql
SELECT block_reason, COUNT(*) as n
FROM tasks
WHERE block_reason IS NOT NULL
  AND created_at > strftime('%s','now','-30 days')
GROUP BY block_reason ORDER BY n DESC;
```

### A.4 PR cycle time (gh CLI + jq)

```bash
gh pr list --state merged --search "merged:>=2026-05-11 merged:<2026-05-18 base:main" \
  --limit 50 --json number,createdAt,mergedAt --jq '
  [.[] | ((.mergedAt|fromdate) - (.createdAt|fromdate))/3600 | floor] |
  {count: length, p50: (sort | .[length/2 | floor]),
   p90: (sort | .[(length*0.9) | floor]), avg: (add/length | floor)}'
```

### A.5 Daily PR-merge volume

```bash
gh pr list --state merged --search "merged:>=2026-05-11 base:main" \
  --limit 100 --json mergedAt --jq '
  [.[] | .mergedAt[:10]] | group_by(.) |
  map({day:.[0], n:length}) | sort_by(.day) | reverse'
```

### A.6 Swarm board (new arch) — ticket lifecycle

```sql
SELECT
  id, status, assignee,
  ROUND((COALESCE(completed_at, strftime('%s','now')) - created_at)/3600.0, 1) as hrs_open,
  substr(title, 1, 60) as title
FROM tasks WHERE id LIKE 't_%' ORDER BY created_at;
```
