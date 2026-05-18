# sw-011 — Heartbeat + 5 Proof Tests

> **Tier:** spec (operator-ratified via "Let's take sw-011" dispatch 2026-05-18 EOD)
> **Authored:** 2026-05-18, red lane
> **Status:** draft — awaits Ares (lane owner) + Clawta (guardrail author) review
> **Companion docs:** [`2026-05-18-swarm-redesign.md`](../../docs/strategy/2026-05-18-swarm-redesign.md), Clawta msgs 4567-4568
> **Ticket:** `t_216b911c` on swarm board (ready)

## Goal

Add **liveness heartbeat** + **5 proof tests** to the swarm so that any agent (red, Ares, Clawta, Icarus) being silently dead, locked, misrouted, or stuck in dedup-loop is detected and surfaced loudly within bounded time.

This is the second leg of the swarm redesign Week-1 gate (PR #752): Haiku Test passed already (sw-006); the remaining 4 proof tests + the heartbeat substrate that backs them are required before any agent gets ratified as "autonomous."

## Why now

Three real failures already happened during this week's swarm bring-up that this spec would have caught earlier:
- **sw-008 hermes-controller silent loop** — only surfaced when operator checked PR review status (would have been caught by Ghost test)
- **sw-010 #swarm intake bug** — only caught when Haiku Test Round 3 failed (would have been caught by Misroute test)
- **ic-007 dedup re-spam** — only caught after Clawta noticed receipt flood (would have been caught by Dedup test)

Heartbeat + 5 proof tests = "the swarm proves itself alive every N seconds, and any of the 5 ways it can silently break is caught within bounded time."

## Scope (what ships in v1)

### 1. Per-agent heartbeat emit

Each agent (red, Ares, Clawta, Icarus) calls `swarm/bin/heartbeat-emit <agent_id>` periodically (60s interval) to record its liveness.

Heartbeat record fields:
- `agent` (e.g. "red", "ares", "clawta", "icarus")
- `ts` (unix epoch, UTC)
- `hostname` (machine identifier)
- `pid` (current process)
- `last_event` (optional short string — e.g. "swarm-invoker tick", "dispatch.ic-005", "manual")

Storage options (Clawta to pick; see §Open questions):
- (A) `~/.chitin/heartbeat/<agent>.json` (file per agent, atomic write)
- (B) New `heartbeat` table in `~/.chitin/agent-bus/bus.db`

### 2. Stale detection + escalation

`swarm/bin/heartbeat-check` runs every 60s via hermes cron. For each registered agent:
- Read latest heartbeat ts
- If `now - ts > STALE_THRESHOLD_SECONDS` (default 180s = 3 × 60s tick interval per Clawta msg 4567):
  - **First escalation:** post visible message to `#swarm` thread 9 + PushNotification operator
  - **Rate-limit:** suppress further "stale" alerts for that agent for 1 hour OR until agent recovers (heartbeat refreshed)
  - **Recovery:** when agent emits a fresh heartbeat after being stale, post visible "agent X recovered" message

### 3. Self-salvage rule (formalized)

After any agent's dispatch hits a deny/lock/non-zero in a MUTATING tool path:
- Stop immediately. Do not retry alternate command shapes.
- Emit WORKER_RECEIPT (or equivalent) with `block_reason=self_salvage_denied`
- Escalate to operator (not Clawta — this is a system-level issue)
- Allowed AFTER stop: NON-mutating diagnosis (`grep`, `find`, `cat`, `git log/diff`, `systemctl status`, `ollama ps`) to add context to the receipt

Forbidden:
- Reset locks (gov.db direct UPDATE)
- Bypass governance
- Mutate gov.db (any UPDATE/INSERT/DELETE)
- Retry alternate command shapes (e.g. if `rm -rf` denied, do NOT try `find -delete`)

### 4. The 5 proof tests

Each proof test is a runnable script under `swarm/tests/proofs/`. CI runs them; operator can also invoke manually.

| # | Test | Verifies | Pass condition |
|---|---|---|---|
| 1 | `test_haiku.py` | Wake-up loop works for all 3 frontier agents | 3 haikus posted to #swarm thread 9 within 5min of sw-006 going ready (re-uses existing Round-4 mechanism) |
| 2 | `test_ghost.py` | Stale agent is detected | Kill heartbeat-emit for agent X, wait STALE_THRESHOLD + 30s, verify heartbeat-check posted stale escalation to #swarm |
| 3 | `test_lock.py` | Locked agent is skipped, loud receipt | Inject lock into gov.db for agent X, dispatch a ticket to X, verify dispatch skipped + receipt emitted with block_reason=locked |
| 4 | `test_dedup.py` | One ready ticket → exactly one prompt | File ticket, wait 2 cron ticks, verify only 1 invocation logged (composite key + ic-007 fix prevents re-fire) |
| 5 | `test_misroute.py` | Posts don't leak to wrong channels; un-mentioned posts wake right agent | Post to #icarus, verify icarus woke + Ares/Clawta did NOT respond. Post to #clawta without @-mention, verify Clawta woke. Etc. |

### 5. Operator CLI

`swarm/bin/heartbeat-status` — operator-facing one-shot status snapshot. Prints table of all known agents + last heartbeat age + current health (green/yellow/red).

## Out of scope (v1)

- Cross-host heartbeat (only local-host agents)
- Heartbeat over agent-bus messages (use file or DB, not pub/sub)
- Auto-restart of dead agents (operator decides recovery action)
- Heartbeat for Discord-only agents (operator) — those are HITL, not in scope

## Invariants (locked)

1. **Heartbeat is quiet.** No `#swarm` post on each emit. Storage is file or DB row, NOT chat message. Per Clawta msg 4567: "heartbeat ≠ spam."
2. **Visible escalation only on state change.** Stale → loud. Recovered → loud. Routine "still alive" → silent. Per Clawta msg 4567 + Ares acknowledgement.
3. **Self-salvage rule:** non-mutating tool calls (grep / find / cat / git status / systemctl status / ollama ps) OK for post-failure diagnosis. Mutating tools (rm / mv / git commit / sqlite UPDATE / curl POST / pkill / kill / systemctl start|stop|restart) are FORBIDDEN after deny/lock. Stop, receipt, escalate.
4. **Rate-limit stale escalations.** Same agent stale = 1 visible escalation per hour. Recovery resets the rate-limit window.
5. **Proof tests are runnable both manually and in CI.** Each test must work as `python3 swarm/tests/proofs/test_<name>.py` AND as a pytest unit.
6. **No proof test mutates production state.** Tests use temp boards / dispatched-by-test-only tickets / mock gov.db where mutation needed.
7. **Heartbeat-check runs even if some agents missing.** Don't fail if an agent has never emitted; treat as "unknown" not "stale."

## Boundary cases

- **Heartbeat emit racing with check:** emit is atomic-write or DB-transaction; check uses snapshot read.
- **Clock skew between emit + check:** use unix epoch UTC everywhere; trust local clock.
- **Agent restarts mid-cycle:** new pid recorded; not flagged as "ghost" since heartbeat ts is fresh.
- **Heartbeat-check itself ghost:** add a meta-heartbeat (heartbeat-check writes its own heartbeat; operator can monitor it).
- **Operator manually pauses an agent:** mark agent as "paused" in a separate file/table; heartbeat-check honors pause + doesn't escalate.
- **Test_lock requires gov.db mutation:** test uses a copy of gov.db, never production.

## E2E coverage (per spec 020 §1.2)

| Surface | Test layer | File |
|---|---|---|
| heartbeat-emit: atomic write | unit | `swarm/tests/test_heartbeat_emit.py` |
| heartbeat-check: stale detection | unit | `swarm/tests/test_heartbeat_check.py` |
| heartbeat-check: rate-limit | unit | `swarm/tests/test_heartbeat_ratelimit.py` |
| heartbeat-check: recovery | unit | `swarm/tests/test_heartbeat_recovery.py` |
| heartbeat-status: operator CLI | unit | `swarm/tests/test_heartbeat_status.py` |
| self-salvage: deny then non-mutating ok | unit | `swarm/tests/test_self_salvage_rule.py` |
| 5 proof tests | runnable + pytest | `swarm/tests/proofs/test_{haiku,ghost,lock,dedup,misroute}.py` |
| Installer | bats | `swarm/tests/test_install_heartbeat_cron.bats` |

## Acceptance Criteria

- [ ] All 4 agents (red, Ares, Clawta, Icarus) emit heartbeat every 60s
- [ ] heartbeat-check cron runs every 60s; stale detection works at 180s threshold
- [ ] Stale escalation posts ONE visible #swarm message + PushNotification; subsequent ticks silent
- [ ] Recovery posts ONE visible message ("agent X recovered")
- [ ] Self-salvage rule enforced in icarus-watcher (and any other future driver)
- [ ] 5 proof tests all pass; haiku is the easiest, misroute requires gateway intake working (sw-010 fixed ✅)
- [ ] heartbeat-status CLI prints clean operator-readable table
- [ ] Constitution §6: tracked source + idempotent installer for heartbeat-check cron
- [ ] All 8 unit tests + 5 proof tests green

## Migration plan

1. **Day 1 (today/tomorrow):** Spec ratify by Ares + Clawta. Implement heartbeat-emit + heartbeat-check + heartbeat-status.
2. **Day 2:** Wire heartbeat-emit into existing cron jobs (swarm-invoker, swarm-controller, clawta-poller, icarus-watcher). Each tick calls emit before doing its main work.
3. **Day 3:** Implement 5 proof tests. Run them manually + add to CI.
4. **Day 4:** Run for 24h baseline. Verify no false-positive stale escalations.
5. **Week 2:** Ratify any agent as "autonomous" only after all 5 proof tests green + 7-day clean heartbeat.

## Open questions for operator (or Ares/Clawta to answer)

1. **Storage:** heartbeat in `~/.chitin/heartbeat/<agent>.json` (file) or new `heartbeat` table in `bus.db`? Trade-off: file = simpler, no schema migration. DB = atomic-with-message-writes, single source of truth for both bus + heartbeat. **Red recommends file (A) — simpler, no schema work.**
2. **Tick interval:** 60s or faster? Faster catches death sooner; slower reduces noise. **Red recommends 60s** matching swarm-invoker cadence.
3. **Stale threshold:** 3 ticks (180s) or wall-clock (e.g. 5min)? **Red recommends 3-tick (180s)** per Clawta msg 4567 wording.
4. **Rate-limit window:** 1h between stale escalations of same agent? Configurable? **Red recommends 1h flat.**
5. **Self-salvage allowlist:** which tools count as "non-mutating"? Red proposes: `grep, find, cat, less, head, tail, git log/diff/status/show, systemctl status, ollama ps, ls, pwd, env, hostname`. Forbidden: anything that writes/transmits.

## Constitution alignment

| Rule | Compliance |
|---|---|
| §4 Red tickets sacred | Heartbeat doesn't auto-promote red tickets |
| §6 Tracked source + idempotent installer | `install-heartbeat-cron.sh` mirrors sw-009 pattern |
| §10.5 Don't wake red | Stale escalations go to #swarm + PushNotification, never modify red's session |
| §governance | heartbeat-emit + heartbeat-check run through chitin governance gate; non-mutating policy enforced |

## Sign-off log

- [ ] red — author, this draft
- [ ] Ares — heartbeat lives in his lane; must confirm storage + cron + agent-bus integration choices
- [ ] Clawta — guardrail author; must confirm self-salvage allowlist + rate-limit policy
- [ ] **operator** — final ratification (already dispatched via "Let's take sw-011")
