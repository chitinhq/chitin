# 075 — Icarus: Local-LLM Driver on RTX 3090

> **Tier:** spec (operator-ratification gated)
> **Authored:** 2026-05-18, red lane
> **Status:** draft — awaits Ares + Clawta lane review, then operator ratification
> **Companion docs:** [`2026-05-18-swarm-redesign.md`](../../docs/strategy/2026-05-18-swarm-redesign.md), [`2026-05-12-argus-observatory.md`](../../docs/superpowers/specs/2026-05-12-argus-observatory.md)
> **Ticket:** ic-001 on swarm board (to be filed at triage status; operator promotes)

## Goal

Add a **deterministic local-LLM worker** ("Icarus") to the swarm, backed by the operator's RTX 3090 + existing ollama install, scoped to a narrow set of mechanical/repetitive lanes where local-model capability is sufficient and frontier-model cost is wasteful.

**What Icarus is:**
- A driver (like hermes-agent / openclaw) the existing 3 agents (red / Ares / Clawta) can route mechanical tickets to via the `skill` field.
- Single-process: ollama serve + a thin Python harness that watches the swarm board for tickets with matching skill labels, runs the prompt locally, posts WORKER_RECEIPT to #swarm + the kanban board.

**What Icarus is NOT:**
- Not a peer agent (no own opinions in #swarm coordination; doesn't get a vote on spec ratification).
- Not a substitute for Clawta on heavy code-gen or red on spec authoring. Loud-fails to escalate when ticket exceeds its capability ceiling.

## Why now (the operator's case)

| Pressure | Current state |
|---|---|
| GPU utilization | RTX 3090 holds qwen3.6:27b hot for argus (98% VRAM), but uses ~14 min of compute time per day. **23h 46min idle GPU/day.** |
| Cost per token | Frontier APIs (Claude / Codex / Ollama Cloud) all bill per call. Repetitive work (lint fixes, log parses, format runs) pays full frontier rate for trivial transforms. |
| Latency | Cloud calls add 1-5 sec round-trip; local 30B Q4 model on 3090 ≈ 50-150 tokens/sec, often faster end-to-end for short prompts. |
| Reliability | Cloud API rate-limits + outages take the swarm offline. Local GPU survives WAN dropouts. |
| Privacy | Local model never sends prompt text off-host. |

**Counter-pressures (honest):**
- Local-model capability ceiling. A 3090-runnable model (Qwen2.5-Coder 32B Q4, DeepSeek-Coder-V2 16B FP16, Yi-Coder 9B FP16) is significantly weaker than Opus 4.7 / GPT-5.5 / Sonnet 4.6.
- VRAM contention with argus. 24 GB cap. Argus's qwen3.6:27b currently takes 22.6 GB → no headroom for a concurrent second model unless shared or swapped.
- Just-merged simplification. PR #752 reduced surface to "three of us ARE the worker pool." Icarus re-expands surface by 33%.

The spec accepts these and designs around them.

## Scope (what ships in v1)

1. `swarm/bin/icarus-watcher` — Python harness that polls swarm board for tickets with `skill IN (mechanical, lint-fix, log-pattern, doc-from-code, triage-classify)` and dispatches the prompt to a local ollama endpoint.
2. `swarm/bin/install-icarus-cron.sh` — idempotent installer per Constitution §6 (mirrors the swarm-invoker installer pattern shipped in PR #760).
3. `swarm/bin/icarus-receipt` — WORKER_RECEIPT poster (board + #swarm via agent-bus).
4. **One-model-shared-with-argus contract** — Icarus uses the same loaded ollama model argus is hot on (default: qwen3-coder:30b or qwen2.5-coder:32b — operator picks per benchmark). When argus's hot model differs, Icarus warm-loads its preferred model and accepts argus's cold-start hit on the next argus tick.
5. The 5 skill lanes listed below in §Lane spec.
6. Loud-fail-on-ceiling primitive — if model exceeds N=2 internal retries OR output fails a deterministic post-check (e.g. lint-fixed code still doesn't lint), Icarus posts a `block_reason=local_ceiling_exceeded` and escalates to Clawta with the partial work.

## Out of scope (v1)

- Multi-GPU / cluster support (operator has one 3090).
- Frontier model fallback (escalation is human-mediated via Clawta, not auto-cloud).
- Custom fine-tuning. Use stock open-weight models.
- Voice / vision / multi-modal (text-only).
- Argus replacement or absorption (Icarus is a separate driver; argus stays read-only observatory).

## Lane spec (5 skills, ENABLED progressively per Clawta amendment)

Per Clawta vote (agent-bus thread 9 msgs 4475-4476): **Week 1 ships only `lint-fix` end-to-end; the other 4 lanes are spec-approved but DISABLED in code until the Week-1 metric clears.** Don't try to validate 5 lanes simultaneously on a brand-new driver.

| Skill | Enable | Example tickets | Tightened scope | Post-check |
|---|---|---|---|---|
| **lint-fix** | **Week 1** | run linter, propose fixes, write fixed file | unchanged | `lint` exit 0 on fixed file |
| **log-pattern** | Week 2+ | parse log file, surface top-N anomaly patterns + counts | **schema-only output** (no prose narration; JSON only) | output is valid JSON matching schema |
| **triage-classify** | Week 2+ | given ticket title + body, suggest owner + skill | **read-only suggestion** — Icarus posts the suggestion as a comment, **never auto-mutates the board** | output is a valid (owner, skill) tuple from the enum |
| **doc-from-code** | Week 3+ | generate README section from module exports | **docs-only PRs** (no `.py`/`.ts` changes) with explicit link/ref check | renders as valid markdown, no broken refs |
| **mechanical** | Week 3+ | bulk import-sort, dead-code prune, rename symbols | **format / import / rename / dead-code ONLY. No semantic refactors. No logic changes.** | git diff applies cleanly + tests still pass |

**Rule:** every lane must have a **deterministic post-check** that runs after the LLM call. If the post-check fails, Icarus loud-fails (no silent recovery, no retry beyond N=2). This is the loud-failure-beats-silent-recovery bright line from the swarm redesign.

**Disabled-lane enforcement:** the watcher MUST skip tickets whose `skill` field maps to a not-yet-enabled lane. Skipped tickets get a `[icarus-disabled-lane]` log entry for operator audit; no error, no escalation, no receipt — silent skip until the lane enables.

## Architecture

```
                    ┌──────────────────────────────────┐
                    │  swarm board (kanban.db)         │
                    │  tickets with skill ∈ {5 above}  │
                    └────────────┬─────────────────────┘
                                 │ poll every 1 min
                                 ▼
                    ┌──────────────────────────────────┐
                    │  icarus-watcher (Python)         │
                    │  fetch ready tickets → dispatch  │
                    └────────────┬─────────────────────┘
                                 │ HTTP POST /api/generate
                                 ▼
                    ┌──────────────────────────────────┐
                    │  ollama serve (local, port 11434)│
                    │  shared model with argus         │
                    │  default: qwen3-coder:30b-32k    │
                    └────────────┬─────────────────────┘
                                 │ generated text
                                 ▼
                    ┌──────────────────────────────────┐
                    │  deterministic post-check        │
                    │  (lint OK? diff applies? JSON?)  │
                    └────────────┬─────────────────────┘
                          ┌──────┴──────┐
                          ▼             ▼
                     PASS = ship    FAIL = escalate to Clawta
                     PR + receipt   + block_reason
                                    + #swarm post
```

## Invariants (locked)

1. **Skill-scoped only.** Icarus reads ticket.skill and acts ONLY if skill ∈ the 5 enumerated lanes. Any other ticket: no-op. Prevents Icarus from accidentally trying to author specs.
2. **Deterministic post-check required.** Every lane MUST have a post-check that runs after LLM output. No post-check = no merge. The post-check is the loud-failure trigger.
3. **N=2 retry cap with split escalation routes** (Clawta amendment msg 4475). If post-check fails twice on the SAME ticket = **capability ceiling** → escalate to Clawta with full WORKER_RECEIPT + `block_reason=local_ceiling_exceeded`. **Infra failures route differently**: GPU OOM, ollama daemon crash, gov.db lockdown, VRAM contention go **operator-visible immediately** with `block_reason=infra_failure` (NOT silently to "Clawta fix it"). Infra is operator's problem; capability is Clawta's.
4. **VRAM-shared with argus, with explicit lease/lock** (Clawta amendment msg 4475). Before any LLM call:
   - Run `ollama ps` preflight to inspect current load
   - If argus is mid-compute on a different model, wait up to **60 seconds** for a model-free window using an advisory file-lock at `~/.icarus/model-lease.lock`
   - If 60s elapses without a free window, **loud-fail** the ticket with `block_reason=vram_contention` (operator-visible)
   - Model swap requires holding the lease lock; concurrent swap attempts queue, not race
   - NO concurrent second large-model load. Hard 24 GB cap.
5. **WORKER_RECEIPT required with 6 enumerated fields** (Clawta amendment msg 4475). Every Icarus action (success OR loud-fail) posts a WORKER_RECEIPT to swarm board + #swarm thread 9 via agent-bus. Receipt MUST include:
   - `lane` (which of the 5 skills)
   - `prompt_class` (lint-fix / log-pattern / etc.)
   - `post_check_output` (deterministic check's return + captured stderr)
   - `diff_path` or `artifact_path` (reviewer can inspect actual work)
   - `retry_count` (0, 1, or 2)
   - `model_used` (e.g. `qwen3-coder:30b-32k`)
   Receipts missing any field = protocol violation. Same contract as red / Ares / Clawta per swarm redesign.
6. **Channel routing contractual.** Icarus posts to #icarus (its own Discord channel, to be created) + #swarm only. Never to #hermes / #clawta / #argus / #ares. (Same pos-002 contract; see open-questions §6 about whether this needs a new channel or shares #swarm.)
7. **No governance bypass.** Icarus runs under chitin governance gate. Tool calls go through the same hook as red / Ares / Clawta. Local-LLM does NOT exempt from policy.
8. **Read-only on argus's index.** Icarus may READ ~/.argus/index.db for log-pattern lane context but never writes to it.

## Boundary cases

- **GPU OOM during Icarus run** → ollama returns error → Icarus loud-fails the ticket + escalates.
- **Ollama daemon crashed** → connection refused → Icarus loud-fails + posts incident to #swarm (operator wakes up).
- **Argus is mid-compute when Icarus tries to dispatch** → Icarus waits (queue), max wait 60s, then escalates.
- **Operator restarts GPU** (driver update, etc.) → Icarus's poll loop survives, ollama daemon is the dependency; cron survives reboot.
- **Multiple Icarus tickets ready simultaneously** → process serially (single-process model). Don't fork concurrent LLM calls — one-at-a-time per VRAM constraint.
- **Ticket assignee=`*` (wildcard) with skill matching Icarus's lanes** → Icarus claims if no other owner has it. Otherwise hands off.

## E2E coverage (per spec 020 §1.2)

| Surface | Test layer | File |
|---|---|---|
| icarus-watcher: skill match + dispatch | e2e | `swarm/tests/test_icarus_watcher_dispatch.py` |
| icarus-watcher: skip non-matching skills | unit | `swarm/tests/test_icarus_skill_scope.py` |
| installer: idempotent install/remove | bats | `swarm/tests/test_install_icarus_cron.bats` |
| post-check: lint-fix lane | unit | `swarm/tests/test_icarus_postcheck_lint.py` |
| post-check: log-pattern lane (JSON schema) | unit | `swarm/tests/test_icarus_postcheck_log_pattern.py` |
| loud-fail on N=2 retries exceeded | e2e | `swarm/tests/test_icarus_loud_fail_escalation.py` |
| WORKER_RECEIPT to board + #swarm | e2e | `swarm/tests/test_icarus_receipt_contract.py` |
| VRAM-share with argus (warm-load handoff) | manual + integration | `swarm/tests/test_icarus_argus_vram_share.py` |
| Channel routing (no posts to #hermes/#clawta/#ares) | e2e | `swarm/tests/test_icarus_channel_routing.py` |

## Acceptance Criteria

- [ ] `icarus-watcher` polls swarm board every 1 min via cron (hermes cron preferred; fail-loud if hermes missing — same pattern as PR #760 sw-009)
- [ ] All 5 lanes have deterministic post-checks wired
- [ ] Loud-fail-on-ceiling escalates to Clawta with `block_reason=local_ceiling_exceeded` + partial work attached
- [ ] WORKER_RECEIPT posted to board + #swarm thread 9 on EVERY action (success + loud-fail)
- [ ] VRAM-share contract: Icarus refuses to load a second large model if argus is hot; warm-load swap with cold-start tolerated
- [ ] Constitution §6: tracked source + idempotent installer
- [ ] All 9 e2e/unit tests pass
- [ ] PR includes a 7-day local benchmark report on at least one real lane (lint-fix recommended) showing: throughput, post-check pass rate, escalation rate, time-saved vs cloud baseline

## Migration plan

1. **Week 1 (after operator ratify):** Stub icarus-watcher with **`lint-fix` lane ONLY** enabled; other 4 lanes silent-skip per disabled-lane enforcement. Wire to swarm board, run for 5 days with operator-curated tickets.
   **Gate (Clawta amendment msg 4476):** ≥70% post-check pass rate measured over **≥10 real lint-fix tickets**. Smaller samples are insufficient — must reach 10 actual ticket runs OR 7 days elapsed (whichever comes first). If <70%, kill the project.
2. **Week 2:** If Week-1 gate passed, enable log-pattern + triage-classify lanes (with their Clawta-tightened scopes: schema-only / read-only-suggestion). Re-measure with same ≥10-ticket sample-size rule per lane.
3. **Week 3:** Enable doc-from-code + mechanical lanes if Week-2 succeeded (with their Clawta-tightened scopes: docs-only / syntactic-only). Same sample-size rule per lane. After Week-3, ratify Icarus as default for all 5 lanes.
4. **Decommission criteria:** Per-lane independent kill — if any lane fails to clear 50% after ≥10 tickets, that specific lane is disabled (not the whole project). The whole project dies only if Week-1 lint-fix lane itself fails to clear 50% (it's the easiest; if it can't ship, nothing harder will).

## Open questions (operator decides at ratification)

1. **Channel:** Icarus gets its own #icarus Discord channel, or shares #swarm? (Argus has #argus already; precedent supports own channel.)
2. **Default model:** qwen3-coder:30b-32k vs qwen2.5-coder:32b vs DeepSeek-Coder-V2 Lite 16B? Operator runs a 1-hour benchmark on 5 lint-fix tickets each, picks winner.
3. **Cron tick interval:** 1 min (matches swarm-invoker) or 5 min (more conservative for local LLM)?
4. **Escalation target:** Clawta (her lane is heavy-codegen, fits "harder than Icarus") or operator directly?
5. **Argus consolidation:** Long-term, is Icarus the right place to also do argus's analysis layer (which is mostly LLM-driven anyway)? Or stay separate? Don't decide in v1; revisit Week 4.
6. **Spec ratification:** does this need Ares + Clawta sign-off before operator ratifies, or just operator?

## Constitution alignment

| Rule | Compliance |
|---|---|
| §4 Red tickets sacred | Icarus never auto-claims red tickets |
| §6 Tracked source + idempotent installer | `install-icarus-cron.sh` mirrors PR #760 pattern |
| §10.5 Don't wake red | Loud-fail escalates to Clawta first, not red |
| §governance | Icarus runs through chitin governance gate (no policy bypass) |

## Sign-off log

- [ ] red — author, this draft
- [ ] Ares — must amend if architecture misrepresents the watcher / cron / agent-bus primitives
- [ ] Clawta — must amend re: heavy-codegen escalation path + WORKER_RECEIPT contract
- [ ] **operator** — final ratification gate (filed as ic-001 ticket on swarm board)
