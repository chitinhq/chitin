# sw-011 — Heartbeat + 5 Proof Tests

> **Tier:** spec (operator-ratified via "Let's take sw-011" dispatch 2026-05-18 EOD)
> **Authored:** 2026-05-18, red lane
> **Status:** draft — awaits Ares (lane owner) + Clawta (guardrail author) review
> **Cross-refs:** Clawta msgs 4567-4568, Ares msgs 4769-4770, swarm board ticket `t_216b911c`
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

## File-system scope

New files added by this spec:
- `swarm/bin/heartbeat-emit` — per-agent heartbeat writer (shell + Python)
- `swarm/bin/heartbeat-check` — stale detection cron (Python)
- `swarm/bin/heartbeat-status` — operator CLI status table (Python)
- `swarm/bin/sw-011-proof-tests` — proof test harness (Python)
- `~/.chitin/heartbeat/<agent>.json` — per-agent heartbeat records (runtime)
- `swarm/tests/test_sw_011_liveness_misroute_proof.py` — 22 unit + sandbox proof tests
- `swarm/tests/proofs/ci/` — CI-safe sandbox proof tests (future)
- `swarm/tests/proofs/live/` — operator-triggered live proof tests (future)
- `.specify/specs/037-sw-011-heartbeat-proof-tests/spec.md` — this spec

Modified files:
- Existing heartbeat / stale-worker code in `hermes_cli/kanban_db.py` (heartbeat_claim, release_stale_claims)
- Discord gateway `gateway/platforms/discord.py` (channel filtering)

## Scope (what ships in v1)

### 1. Per-agent heartbeat emit

Each agent (red, Ares, Clawta, Icarus) calls `swarm/bin/heartbeat-emit <agent_id>` periodically (60s interval) to record its liveness.

**Storage v1 (LOCKED per Ares msg 4769 + Clawta msg 4778 blocker #1):**
Per-agent JSON file at `~/.chitin/heartbeat/<agent>.json`, atomic write via `os.replace()`. Matches the chitin/argus precedent (argus uses `~/.argus/heartbeat.json` with identical pattern). DB-table option deferred to follow-up if v1 proves insufficient.

Heartbeat record format (aligned with argus shape per Ares):

```json
{
  "agent": "ares",
  "ts": 1779141647,
  "pid": 393329,
  "hostname": "chimera-ant",
  "tick_interval_s": 60,
  "status": "idle",
  "last_event": "swarm-invoker tick"
}
```

Fields:
- `agent` (string): agent id ("red", "ares", "clawta", "icarus")
- `ts` (int): unix epoch UTC of this emit
- `pid` (int): current process id
- `hostname` (string): machine identifier
- `tick_interval_s` (int): emit cadence in seconds — heartbeat-check uses this to compute stale threshold without hardcoding
- `status` (string): `idle` | `dispatching` | `blocked` | `down` — gives heartbeat-check more signal than just timestamp presence
- `last_event` (string, optional): short identifier of the last work unit, e.g. "swarm-invoker tick", "dispatch.ic-005", "manual"

### 2. Stale detection + escalation

`swarm/bin/heartbeat-check` runs every 60s via hermes cron. For each registered agent:
- Read latest heartbeat from `~/.chitin/heartbeat/<agent>.json`
- Compute stale threshold per record's own `tick_interval_s`:

  ```
  threshold_s = (3 * tick_interval_s) + JITTER_S
  stale = (now - ts) > threshold_s
  ```

  - `JITTER_S` default = **30s** (configurable via env `HEARTBEAT_JITTER_S`)
  - At default 60s tick → threshold = 3×60 + 30 = **210s**
  - Per Clawta msg 4778 blocker #3 + Ares msg 4770: wall-clock with jitter survives cron scheduler drift

- If stale:
  - **First escalation:** post visible message to `#swarm` thread 9 + PushNotification operator
  - **Rate-limit window:** suppress further "stale" alerts for that `(agent, stale_state)` tuple for **`HEARTBEAT_STALE_TTL_S` seconds** (default **900s = 15min** per Clawta msg 4771; configurable via env). Visible in `heartbeat-status` output so operator can see the window
  - **State-transition exceptions** (escalate again even mid-rate-limit window):
    - Recovery: agent goes stale → not-stale → stale again
    - Reason change: lock reason differs
    - Stale duration crosses next-tier threshold (15min, 30min, 1hr)
  - **Recovery:** when agent emits a fresh heartbeat after being stale, post ONE visible "agent X recovered" message + reset the rate-limit window

### 3. Self-salvage rule (formalized)

After any agent's dispatch hits a deny/lock/non-zero in a MUTATING tool path:
- Stop immediately. Do not retry alternate command shapes.
- Emit WORKER_RECEIPT (or equivalent) with `block_reason=self_salvage_denied`
- Escalate to operator (not Clawta — this is a system-level issue)
- Allowed AFTER stop: NON-mutating diagnosis to add context to the receipt

**Non-mutating allowlist (Clawta msg 4778 blocker #5: `env` REMOVED — leaks secrets):**

- File reads/search: `cat`, `head`, `tail`, `less`, `wc`, `grep`, `rg`, `find`, `ls`, `stat`, `file`, `jq`
- Process/status reads: `ps`, `pgrep`, `systemctl status`, `journalctl` (read-only flags)
- Git reads: `git status`, `git log`, `git diff`, `git show`
- Service status reads: `openclaw status`, `hermes cron list`, `gh pr view`, `gh run list`
- DB reads ONLY: `sqlite3 ... SELECT ...` and `pragma` introspection. NEVER `UPDATE/INSERT/DELETE/REPLACE`
- Targeted env-var reads OK (e.g. `echo "$KANBAN_BOARD"`) — broad dumps like `env` or `printenv` are FORBIDDEN (leak secrets)

**Forbidden — full stop on any of these:**
- Service lifecycle: `systemctl restart/start/stop`, gateway restarts, `pkill`, `kill`
- Lock resets / `gov.db` mutation of any kind
- `git reset`, `git checkout`, `git clean`, `git stash`, `git apply` that changes working tree
- Kanban mutations (status moves, assignments, comments) unless the ticket explicitly authorizes the tool path
- Retries with alternate mutating command shapes after a deny (e.g. if `rm -rf` denied, do NOT try `find -delete`)
- Broad environment dumps (`env`, `printenv` without explicit var name)

### 4. The 5 proof tests — SPLIT into CI-safe vs live-operator

Per Clawta msg 4778 blocker #2: tests that touch live Discord/gateway/gov.db cannot run in CI without mutating production state, but invariant 6 says proof tests must not mutate production. Resolved by splitting:

#### 4a. CI-safe sandbox proofs (`swarm/tests/proofs/ci/*.py`)

Run on every PR in GitHub Actions. Use temp directories, copied gov.db, mock bus/gateway clients, isolated test boards. Zero production impact.

| # | Test | Verifies | Sandbox |
|---|---|---|---|
| 2-ci | `test_ghost_sandbox.py` | Stale detection logic + rate-limit | Mock heartbeat dir under `tempfile.TemporaryDirectory()`; write stale file; verify heartbeat-check.py emits escalation event |
| 3-ci | `test_lock_sandbox.py` | Locked-agent skip + receipt format | Copy gov.db to tmp path, inject lock via SQL, run dispatcher against tmp DB, assert receipt emitted with block_reason=locked |
| 4-ci | `test_dedup_sandbox.py` | Composite key + ic-007 terminal suppression | Mock invocation log + kanban_mutations_log; verify 2 ticks → 1 dispatch |

#### 4b. Live operator-triggered proofs (`swarm/tests/proofs/live/*.py`)

Operator-invoked only (NOT in CI). Posts real messages, queries real gateway logs, exercises real cron. Output is structured + observable so operator can ratify.

| # | Test | Verifies | Live |
|---|---|---|---|
| 1-live | `test_haiku.py` | Wake-up loop end-to-end | Re-uses Round-4 mechanism: promote sw-006 ready, expect 3 haikus in #swarm thread 9 within 5min |
| 2-live | `test_ghost_live.py` | Real heartbeat-check escalation reaches Discord | Pause one agent's heartbeat-emit cron, wait threshold + 30s, verify #swarm escalation arrived |
| 5-live | `test_misroute_live.py` | Channel routing end-to-end | Post to #icarus → verify icarus woke + Ares/Clawta did NOT respond. Post to #clawta no @mention → Clawta woke. Post to #swarm → all 3 received. Asserts via gateway log inspection. |

**Pass condition for ratification:** all CI-safe proofs green in PR + all live-operator proofs run by operator + signed off in #swarm before any agent gets "autonomous" status.

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
4. **Rate-limit stale escalations** (Clawta msg 4778 blocker #4). Default TTL = `HEARTBEAT_STALE_TTL_S = 900s (15min)`. Configurable via env. **Visible in `heartbeat-status` output** so operator can see active rate-limit windows. State-transition exceptions (lock reason change, threshold crossing 15/30/60min, recovery) bypass the window.
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

## Test coverage (per spec 020 §1.2)

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

- [ ] All 4 agents (red, Ares, Clawta, Icarus) emit heartbeat every 60s to `~/.chitin/heartbeat/<agent>.json`
- [ ] heartbeat-check cron runs every 60s; stale detection uses `(3 * tick_interval_s + JITTER_S)` formula with 30s default jitter (= 210s at 60s tick)
- [ ] Stale escalation posts ONE visible #swarm message + PushNotification within the rate-limit window
- [ ] Rate-limit window default 15min via `HEARTBEAT_STALE_TTL_S` env, **visible in heartbeat-status output**
- [ ] State-transition exceptions bypass rate limit: recovery, lock-reason change, threshold-crossing
- [ ] Recovery posts ONE visible message ("agent X recovered") + resets rate-limit window
- [ ] **Self-salvage rule enforced in TOUCHED drivers** (icarus-watcher updated as part of this PR) **AND documented as mandatory for future drivers via shared helper to be added in follow-up** (Clawta msg 4778 blocker #6)
- [ ] CI-safe proofs (ghost-sandbox + lock-sandbox + dedup-sandbox) green on every PR
- [ ] Live proofs (haiku + ghost-live + misroute-live) documented as operator-triggered + signed off in #swarm before any "autonomous" ratification
- [ ] heartbeat-status CLI prints operator-readable table with agent / last-seen / status / rate-limit-window
- [ ] Constitution §6: tracked source + idempotent installer for heartbeat-check cron
- [ ] All unit tests + 3 CI-safe proof tests green; 3 live proof tests runnable

## Migration plan

1. **Day 1 (today/tomorrow):** Spec ratify by Ares + Clawta. Implement heartbeat-emit + heartbeat-check + heartbeat-status.
2. **Day 2:** Wire heartbeat-emit into existing cron jobs (swarm-invoker, swarm-controller, clawta-poller, icarus-watcher). Each tick calls emit before doing its main work.
3. **Day 3:** Implement 5 proof tests. Run them manually + add to CI.
4. **Day 4:** Run for 24h baseline. Verify no false-positive stale escalations.
5. **Week 2:** Ratify any agent as "autonomous" only after all 5 proof tests green + 7-day clean heartbeat.

## Open questions — ALL RESOLVED by Ares (msgs 4769-4770) + Clawta (msgs 4771, 4778)

1. ✅ **Storage:** `~/.chitin/heartbeat/<agent>.json` (file per agent, atomic write). DB-table deferred to follow-up if v1 insufficient.
2. ✅ **Tick interval:** 60s matching swarm-invoker cadence.
3. ✅ **Stale threshold:** `(3 * tick_interval_s) + JITTER_S` with 30s default jitter = 210s at 60s tick. Survives cron scheduler drift.
4. ✅ **Rate-limit window:** 15min default via `HEARTBEAT_STALE_TTL_S` env. Configurable + visible in `heartbeat-status` output. State-transition exceptions bypass window.
5. ✅ **Self-salvage allowlist:** explicit list in §3 above. `env` REMOVED per Clawta blocker #5 (leaks secrets). Targeted env-var reads OK.

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
