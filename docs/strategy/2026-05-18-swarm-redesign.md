# Swarm Redesign — Operator-Mandated 3-Way Design

> Convened: 2026-05-18 ~08:30 EDT by operator emergency goal
> Participants: red (Claude Opus 4.7 1M) · Ares (hermes-agent / Ollama Cloud) · Clawta (OpenClaw / Codex GPT-5.5) · Copilot (GitHub PR review aux)
> Status: **DRAFT** — pending Clawta lane proposal + operator ratification
> Source thread: agent-bus thread 9 (#swarm), msgs 3441-onward

## Mandate (verbatim from operator)

> hold an emergency meeting with ares and clawta. i want for you three to design the ideal swarm implementation between the three of you. **discarding worker complexity**, knowing that all three of your capabilities are complementary. copilot has code review in github which is helpful for all chitinhq repos. hermes and clawta can communicate via discord, and have their own ecosystems and hermes can use any ollama cloud model via my subscription, clawta can use the openclaw ecosystem as well as my codex subscription to use frontier model gpt5.5. You and me work HITL and have a max subscription, goal mode and the claude code ecosystem. for this exercise i want you to all forget the existing swarm architecture and propose a better one based on what we now know.

## Capability map

| Agent | Ecosystem | Model | Native strengths |
|---|---|---|---|
| **red** | Claude Code · MCP · sub-agents · goal-mode · HITL | Opus 4.7 1M ctx | Long-context reasoning, spec authorship, multi-file refactors, e2e implementation, sees operator terminal directly |
| **Ares** | hermes-agent · kanban-as-source-of-truth · cron · chitin governance · agent-bus | any Ollama Cloud (operator sub) | Orchestration, board state machine, governance gating, autonomous loops, 18-month operational history |
| **Clawta** | OpenClaw · lobster workflows · Codex subscription | GPT-5.5 frontier | Heavy code-gen, frontier-model reasoning, lobster automations |
| **Copilot** | GitHub-native PR review on chitinhq | (their model) | Async PR reviewer. Auxiliary, not a peer. Zero integration cost. |
| **operator** | This terminal · Discord · push notifications | (human) | Ratification, ground truth, ambiguity resolver, HITL constraint we design AROUND |

## The reframe: discard the driver-proxy layer

The thing operator is asking us to discard is **"dispatch a ticket → spawn a worker driver subprocess → proxy results back."** That whole layer caused:
- The 9-hour silent-dead window (worker died, controller didn't know)
- The classify-step JSONDecodeError (proxy returned non-JSON, parser crashed)
- The salvage-self anti-pattern (operator manually re-doing worker output)
- The driver-cards / ELO / `_pick_driver.py` machinery (complex routing for a layer we're removing)

**New model:** the three of us ARE the worker pool. A ticket gets routed by `skill` field → one of us picks it up in our native ecosystem → ships PR → other two review.

No more `kanban-dispatch.lobster`. No more `_pick_driver.py`. No more codex/claude-code/copilot driver subprocesses. We don't need an external worker pool because the pool IS us.

## The four bright lines

1. **Loud failure beats silent recovery.** Operator is right here on Discord + this terminal. When something breaks, push-notify him and surface the ticket as blocked. Don't engineer 3-retry loops that hide failures for 45 min — the recovery primitive is the operator's eyes, not auto-retry.

2. **Kanban is the bus is the source of truth.** Three lightweight contracts:
   - **Kanban** (per board: chitin / readybench / personal-os / swarm) = work assignment + status
   - **agent-bus** (#swarm thread 9) = real-time 3-way coordination
   - **GitHub PRs** (chitinhq org) = code hand-off, Copilot auto-review, peer approval
   No shared filesystem, no subprocess spawning, no shared worktrees.

3. **Channel routing is contractual.** Per Clawta's pos-002 work (already shipped: spec + e2e red tests on `clawta/pos-002-channel-routing-spec`):
   - **#swarm** = cross-agent coordination, design discussion, proof-of-life reports
   - **#hermes** = Hermes/Ares cron output + hermes-side workflows
   - **#clawta** = Clawta cron output + Clawta-side workflows
   - **Forbidden:** ambient sends (no `--target`), cross-agent posts (Clawta into #hermes, Hermes into #clawta)
   - The e2e for pos-002 currently fails on the deployed kanban-dispatch.lobster (correct — that's the failing-test-first contract).

4. **HITL is loud and sparse.** Operator gets pinged when ratification needed (governance/spec changes), cross-lane conflict, or proof-of-life gate trips. Otherwise ship under autonomy. Sparse = don't ping for routine ops; loud = when you DO ping, make it actionable.

## Lane mapping (skill-pull, not turf)

Each lane is a `skill` value the ticket carries. The controller routes by skill match, not by who normally owns that area.

| Skill | Lane | Why |
|---|---|---|
| `governance` · `cron` · `board-orchestration` · `agent-bus-mechanics` · `chitin-policy` | **Ares** | hermes-agent ecosystem owns these primitives; 18mo operational history |
| `heavy-codegen` (>200 LOC new logic) · `frontier-reasoning` (e.g. complex review, architecture decision) · `lobster-automation` | **Clawta** | Codex GPT-5.5 is the strongest model available to the swarm for these |
| `claude-code` · `mcp-integration` · `sub-agent-orchestration` · `multi-file-refactor` · `e2e-suites` · `spec-authorship` · `HITL-facing` | **red** | Native sub-agent spawning + MCP + 1M ctx + operator-terminal access |
| `pr-review` (any PR on chitinhq/*) | **Copilot (auto) + 1 peer agent** | GitHub-native + a human-loop peer agent for substantive sign-off |

Tickets without a clear skill route get an operator ratification gate.

## State machine (kept; trimmed)

```
triage → ready → in_progress → (blocked | review) → done
                                       ↓
                                  PR opened
                                       ↓
                              copilot auto-review
                                       ↓
                              peer-agent review
                                       ↓
                                    merged
                                       ↓
                                     done
```

What's removed vs current:
- No `claim` step that spawns a driver subprocess. Ticket assignment IS the claim.
- No `dispatch-finalize` flock dance — single-actor flow per ticket.
- No `_pick_driver.py` ELO routing — `skill` field is deterministic.
- No `kanban-dispatch.lobster` — replaced by 30-line shell function inside the controller for `assign-to-self + create-worktree + open-PR`.

## Hand-off contracts

| Boundary | Trigger | Contract |
|---|---|---|
| **Ares ↔ red** | Spec needs authoring or refactor needs claude-code primitives | Ares assigns kanban ticket with `skill: claude-code`. red picks up in session, ships PR. Ares verifies governance, merges. |
| **Ares ↔ Clawta** | Heavy codegen needed (e.g. >200 LOC, frontier reasoning task) | Ares assigns with `skill: heavy-codegen`. Clawta picks up via Codex, ships PR. Ares verifies. |
| **red ↔ Clawta** | PR review across agents | Either author opens PR. Copilot auto-reviews. The non-author agent approves. Merge requires both: Copilot ≥ comment AND peer agent approve. |
| **any agent → operator** | Ratification needed OR proof-of-life gate trips OR cross-lane conflict | Push notification via PushNotification tool / Discord webhook with explicit ask. Block ticket until ack. |
| **any agent → #swarm** | State change on a cross-lane ticket OR finished work that affects others | bus_reply(thread_id=9, ...) with the receipt. |

## Proof-of-life test (smallest end-to-end)

**The Haiku Test.** A single ticket on the `swarm` board (or new `proof-of-life` micro-board) with body:

> All three agents post a 1-line haiku about their ecosystem to #swarm within 5 minutes of this ticket reaching `ready` status.

Pass = three haikus in #swarm thread 9 within 5 min. Fail = any silent gap > 5 min.

This trivially exercises:
- Board promotion triggering each agent's wake-up loop
- All three reading the SAME ticket from the SAME board
- All three writing to the SAME channel (testing the pos-002 contract end-to-end in production)
- 5-min silent-dead invariant (spec 036 retro contract)

If this fails, nothing else will work. If it passes, we have the substrate and can scale up complexity.

## Migration plan (no rip-out until replacement proven)

1. **Week 1 (now):** Run NEW architecture on `swarm` board only. Old architecture stays running on chitin / readybench / personal-os. The Haiku Test must pass. Then increase ticket complexity (small refactor → spec authoring → cross-lane work) on `swarm` board.
2. **Week 2:** Run new architecture in parallel with old on `personal-os` board. Compare: dispatch latency, silent-death incidents, operator-escalations. If new dominates on every metric, proceed.
3. **Week 3:** Cut over `personal-os` to new architecture exclusively. Keep `chitin` + `readybench` on old.
4. **Week 4:** Cut over `chitin` and `readybench` after MVP cutover lands (Friday 2026-05-23 + post-MVP stabilization).
5. **Decommission:** Remove `kanban-dispatch.lobster`, `_pick_driver.py`, driver cards, ELO machinery. Spec 036 retro becomes historical.

## What we keep from the old architecture (Ares's Q5 was right)

- **kanban.db as single source of truth** — every state transition lands in SQLite, every agent queries
- **`kanban-flow` as state-machine CLI** — already battle-tested
- **spec-kit as promotion gate** — every ticket needs `.specify/specs/NNN-<slug>/spec.md` before `ready`
- **chitin governance gate** — policy-signed `chitin.yaml` blocks the high-risk operations (rm -rf, .env writes, governance mutation without operator)
- **constitutional rule: red tickets are sacred** — operator-owned tickets don't auto-promote, don't auto-claim, don't auto-anything
- **agent-bus as #swarm coordination channel** — discord_message_id stamping fixed via pos-002

## What we kill

- `kanban-dispatch.lobster` (~500 LOC of workflow)
- `_pick_driver.py` driver routing (replaced by `skill` field lookup)
- driver cards (`~/.openclaw/agents/*/card.json`)
- `swarm_elo` / `swarm_dispatch_scores` tables (no driver pool to rate)
- worker proxy spawning (no subprocess for external CLI driver)
- `clawta-poller` / readybench-poller subprocess polling (replaced by Ares's deterministic controller per skill route)
- 45-min stale-worker watchdog (replaced by 5-min push-notify-operator invariant)

## Open questions for operator

1. **Copilot peer-review depth.** Auto-review on every PR (cost) vs only cross-lane PRs (governance only)?
2. **Cooling status for repeatedly-failed tickets.** Ares proposed 4-hour cooldown after N=3 failures. Acceptable, or always-escalate-on-fail?
3. **Operator-tickets workflow.** Red tickets stay sacred (don't auto-promote). But do operator tickets need a separate `operator-attended` skill that ONLY surfaces to operator at next interactive turn?
4. **Hermes Ollama Cloud model selection.** Ares can use any. Does operator want default model per skill (e.g. `governance` → llama 4 405B for cost; `frontier-reasoning` → never gets Hermes lane, always Clawta)?
5. **Migration risk: cut over fully or run parallel for safety?** Plan above is parallel-for-3-weeks. Operator may prefer faster.

## Verification

Each lane proposal in #swarm:

| ID | Author | msg(s) | Status |
|---|---|---|---|
| sw-002 — Ares | Ares | 3465-3468 | ✅ Delivered |
| sw-002 — red | red | 3449 | ✅ Delivered |
| sw-003 — Clawta | Clawta | (pending — pos-002 work shipped at msgs 3439-3440 as behavioral evidence) | ⏳ |

Synthesis ships once Clawta posts. Operator-presentation gates the implementation (sw-005 ticket).

## Sign-off log

- [ ] red — author, this draft
- [ ] Ares — must amend if synthesis misrepresents msgs 3465-3468
- [ ] Clawta — must amend after her lane proposal lands
- [ ] **operator** — final ratification gate
