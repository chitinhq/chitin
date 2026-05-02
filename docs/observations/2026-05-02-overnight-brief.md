---
date: 2026-05-02
status: observation
audience: Jared, on waking
purpose: One-page brief — what happened overnight, what to look at first.
---

# Overnight brief — wake up to this

## What got shipped (PRs open for your review)

**My PRs (3 — coordinated overnight work):**

| # | Title | Type | Why it matters |
|---|-------|------|----------------|
| #107 | docs: overnight research — openclaw usage survey + swarm SoTA | docs | Research on (a) how others use openclaw — they closed Temporal integration as not planned, our three-plane architecture fills exactly that gap. (b) 2026 self-improving-swarm SoTA — Live-SWE-agent, MiniMax M2.7, Kimi K2.5. Concrete next moves prioritized. **Read first — frames everything else.** |
| #109 | feat(swarm): Slack notifier for dispatcher events + TimeoutStartSec fix | code | Wires `CHITIN_SLACK_WEBHOOK_URL` env so you see swarm activity in Slack. Also bumps `TimeoutStartSec` 900→2400 because slice-7-tuning's longer wall_timeouts opened a window where systemd killed the dispatcher mid-workflow. 9 unit tests, typecheck clean. |
| #110 | docs(swarm-backlog): 9 research-informed in-design entries | docs | New entries derived from #107: role-typed-backlog-entries, lessons-learned-sidecar, eval-harness-wiring, multi-step-flows, openclaw-mission-control-otel-hookup, openclaw-temporal-issue-10164-public-comment, chitin-readme-positioning-rewrite, playwright-driver-prototype, notebooklm-ingest-via-playwright, soul-md-schema-alignment. All `in_design` — you promote what looks right. |

**Swarm-produced PRs (6 — autonomous, while we worked):**

| # | Title | Status | Notes |
|---|-------|--------|-------|
| #101 | swarm: normalize-decision-params-truthiness | open | Tiny, correct, +4/-1 |
| #103 | swarm: repo-regex-tighten | open, **CI green** | Excellent — tightened regex + 6 tests for path-traversal cases |
| #105 | swarm: dispatcher-prompt-relative-path-prefix | open, CI green | The swarm fixing its OWN prompt bug. Real recursion. |
| #106 | swarm: dispatcher-prompt-scope-discipline | open | **Partial** — added the prompt constraint but skipped the post-run integration check (apply-step scope-drift detection). Entry was bigger than the agent delivered. |
| #108 | swarm: activity-include-hook-events-flag | open | Adds `--include-hook-events` to spawn args + parses hook events from stream-json. Sloppy `any[]` typing for `hookEvents` field — would tighten before merge. |
| #112 | swarm: qwen-ollama-stream-instability-investigation | open, CI green | **244-line investigation doc — substantive.** Identifies two distinct failure modes (KvSize=262144 forces CPU offload, qwen3coder.go XML parser rejects malformed tool calls) with actual ollama log excerpts. Haiku produced this in 4 min on T2. **CAVEAT:** the agent also overwrote `.claude/settings.json` (replaced `extraKnownMarketplaces`/`enabledPlugins` content with chitin gate hooks) — out of scope for the entry. Revert that file before merging or strip it after. |

## What to merge first (suggested order)

1. **#103** — autonomous, CI green, contained. Builds confidence in the autonomous loop.
2. **#107** — docs only, frames roadmap discussion.
3. **#109** — Slack notifier; once merged you get visibility on the next swarm tick. **You'll need to set `CHITIN_SLACK_WEBHOOK_URL` in `~/.config/systemd/user/chitin.env` afterward** (PR body has the snippet).
4. **#101, #105, #106, #108** — review one at a time; the swarm is stable but each PR deserves a glance for scope drift.
5. **#110** — promote 1-3 of the new entries to `ready` if you want the swarm to chew on them next.

## What's running right now

- **Worker:** `chitin-worker.service` active (Restart=on-failure)
- **Dispatcher timer:** firing every 5 min, last tick 03:43 UTC
- **Currently in flight:** `qwen-ollama-stream-instability-investigation` (T2 → claude-code-headless haiku, 30min budget). Heads-up: this is an "investigation" task. Haiku may produce a generic analysis rather than dig into ollama internals. If the output is shallow, the entry might want to be re-tiered to T3 (sonnet) and re-dispatched.

## What didn't ship (and why)

- **No Playwright integration prototype.** Backlogged as `playwright-driver-prototype` (T3). Real implementation is ~600 LOC + Playwright bootstrap + auth handling — too much for one-shot overnight without your scope review.
- **No NotebookLM ingest.** Backlogged depends on Playwright. Same reason.
- **No `lessons-learned-sidecar` implementation.** Backlogged at T1 — a future swarm tick can pick it up after you promote it.

## What I want you to look at FIRST (60-second skim)

1. `docs/observations/2026-05-02-openclaw-usage-survey.md` (PR #107) — the OpenClaw closed-as-not-planned finding is strategic. Want your take before the 2026-05-07 talk.
2. `docs/observations/2026-05-02-self-improving-swarm-sota.md` (PR #107) — the "where chitin is ahead / behind" sections are the real meat.
3. PR #109 — operator visibility you asked for. Configure the webhook and you start seeing dispatch events in Slack within 5 min.

## Cost tally (rough)

Tonight's autonomous dispatches were almost entirely on **Copilot (free under your plan)** — the qwen-layer entries route there. The currently-running T2 `claude-code-headless`/haiku dispatch is the first paid usage of the night; budget ~$1-2 max per workflow. If this stays the only paid run by morning, total cost is sub-$5.

## My open question for you

The overnight flywheel works. But the autonomous swarm's value is bounded by:
1. **Backlog drying up.** ~5 ready entries left after current finishes. Want a cron'd grooming pass that auto-mints new entries from the chain / from gov-decisions?
2. **Single-shot per entry.** No multi-step flows yet (#110 has the in-design entry). Next leverage point.

Pick which to push on next session.
