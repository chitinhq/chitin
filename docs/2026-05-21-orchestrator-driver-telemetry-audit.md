# Orchestrator Driver Telemetry Audit & Regression Analysis

**Date:** 2026-05-21 (EDT) · **Author:** red
**Goal:** Get every orchestrator driver governed by the chitin kernel with
working, *proven* telemetry — and explain why several regressed.

## TL;DR

**Resolved 2026-05-22 — all 6 in-scope drivers proven governed.** The audit
found only 3 (claude, codex, openclaw). The rest were fixed this session:

- **hermes / clawta** regressed on a **stale kernel** — #861 (the telemetry
  fix) was committed but never deployed because `chitin-kernel-redeploy` was
  broken. Fixed: kernel redeployed; both the Hermes and OpenClaw gateways
  restarted to load #861's plugins. Both now emit attributed governance
  decisions — verified by live probe.
- **copilot** was ungoverned — its hook never fired and the `chitin-kernel
  drive copilot` shim died on a `copilot-sdk` `timestamp` type mismatch
  (CLI emits a string; SDK typed it `int64`, in every version). Fixed: forked
  the SDK into `third_party/copilot-sdk-go` with a tolerant `flexInt64`
  decoder, rebuilt + installed the kernel, and rewired the orchestrator's
  copilot driver to the shim. Copilot now emits governed `gov-decisions`
  (`agent:copilot-cli`) — verified by live probe.
- **gemini** is descoped by operator decision (Antigravity migration pending).

**Open follow-up:** the copilot fix is live but **uncommitted** — the SDK fork,
`replace` directive, and driver rewiring need a PR to be durable (tracked in
spec 083 tasks; gemini's `unverified`-state work and US2/US4 also remain).

## Method

For each driver: verify the hook, run a **real** one-shot invocation in a fresh
`git worktree`, and check whether a governance event landed in a `~/.chitin/`
telemetry sink. Then mine 30 days of `gov-decisions-*.jsonl` history and the
git log to timeline regressions. "Proven" = a probe produced an inspectable
telemetry row — not a green `chitin doctor` (see Finding 2).

## Current state — live probes

| Driver | Telemetry sink | Proven today | Status |
|---|---|---|---|
| **claude** | `gov-decisions-*.jsonl` | ✅ 828 rows today | working |
| **codex** | `codex-events-<session>.jsonl` | ✅ probe `019e4d9b` | working (post-hoc, see below) |
| **openclaw** | `events-openclaw-clawta-*.jsonl` | ✅ live `decision` events | working |
| **hermes** | `gov-decisions-*.jsonl` | ✅ probe 04:18Z — `shell.exec` `allowed` | **RESTORED** (kernel redeploy + gateway restart) |
| **clawta** | `gov-decisions-*.jsonl` | ✅ 121 rows post-restart | **RESTORED** (kernel redeploy + gateway restart) |
| **gemini** | — | ❌ | blocked — CLI unauthenticated (operator credentials) |
| **copilot** | — | ❌ | broken — shim SDK mismatch (needs authorized kernel rebuild) |

## The regression — drivers that *were* governed

Mining every `gov-decisions-2026-*.jsonl` (Apr 22 – May 22), driver attribution
began May 11. The per-day timeline:

```
codex   gov-decisions: May 11–18  → STOPPED after May 18
hermes  gov-decisions: May 14,16,18,19,20,21 → STOPPED after May 21 (66 rows on the 21st)
clawta  gov-decisions: May 11–21 (3422 on the 21st) → 0 today; now in a lockdown loop
```

All-time totals: claude-code 34,928 · clawta 10,940 · codex 2,805 · **hermes
318** · gemini 1 · null 36,764. So hermes was genuinely governed — as recently
as **yesterday** — and codex had 2,805 decisions before May 18.

## Root cause — the kernel binary is stale

`git log` surfaced **`e39fa7473` — "fix: restore chitin governance telemetry
for Hermes and Clawta" (#861)**, committed **May 21 14:04 EDT**. It root-caused
and fixed *three* bugs in this exact path:
1. kernel chain-id stamping,
2. Hermes plugin `session_id` forwarding,
3. OpenClaw plugin policy-file resolution (silent fail-open).

It modified `go/execution-kernel/` (`gate_hook.go`, `main.go`,
`internal/gov/{gate,decision,policy}.go`).

**But the installed kernel predates the fix:**

| Binary | Built | vs #861 (May 21 14:04) |
|---|---|---|
| `chitin-kernel` (`dist/go/execution-kernel/`) | **May 21 12:48** | **76 min too early** |
| `chitin` (`~/go/bin/chitin` — the binary hooks call) | **Apr 17** | **~5 weeks stale** |

`chitin-kernel-redeploy.service` — the cron that rebuilds the kernel from main —
is **failing** (`install-kernel.sh: fatal: Cannot fast-forward to multiple
branches`). So #861 (and a month of other kernel fixes) was merged but **never
deployed**. The running kernel still has the three telemetry bugs #861 fixed →
hermes/clawta governance telemetry is in its pre-#861 broken state.

Verified: `go build ./cmd/chitin-kernel` from current main **compiles clean**
(21.5 MB), and `internal/gov/decision.go` carries #861's `ChainID`/`SessionID`
fields. The fix is purely a build-and-deploy away.

## Per-driver detail

- **claude** ✅ — `chitin claude-hook`, global. 828 `gov-decisions` rows today.
  The only enforcing, central-log driver.
- **codex** ✅ but degraded — emits `decision` events to per-session
  `codex-events-*.jsonl` (`rule_id:codex-post-hoc` — records, does not gate).
  Its **central `gov-decisions` telemetry stopped after May 18** (2,805 rows
  before then). Fix applied: migrated the deprecated `[features].codex_hooks`
  flag to `[features].hooks`. The gov-decisions regression is separate from the
  hooks flag and likely shares the stale-kernel/router cause.
- **openclaw** ✅ — openclaw governance plugin; live `decision` events.
- **gemini** ❌ — hook installed (`chitin gemini-hook`, `BeforeTool`); the
  **Gemini CLI is unauthenticated** (`GEMINI_API_KEY` unset) so it cannot run.
  Environment gap, not chitin. 1 gov-decision ever.
- **copilot** ❌ — both paths broken. The hook (`chitin copilot-hook`) never
  fires — proven with probes against both the global `~/.copilot/hooks/` and
  project `.github/hooks/` locations. The `chitin-kernel drive copilot` shim
  dies at startup: copilot CLI v1.0.51 returns `timestamp` as a string;
  `copilot-sdk` declares it `int64` in **every** version (v0.2.2–v1.0.0-beta.4).
- **hermes** ❌ — regressed; restored by #861, undeployed (see root cause).
  The orchestrator's hermes driver runs `hermes chat -q` and historically
  produced 318 governed decisions.

## Systemic findings

1. **Telemetry sink fragmentation** — `gov-decisions-*.jsonl` (claude),
   `codex-events-*.jsonl` (codex), `events-openclaw-clawta-*.jsonl` (openclaw).
   No unified governance log; per-driver regressions hide in separate files.
2. **`chitin doctor` false positives** — reported codex & copilot
   `test-emit=ok [OK]` while both were broken end-to-end. It checks hook files,
   not the live CLI path.
3. **`chitin doctor` false negatives** — project-scoped; ignores global hooks,
   so genuinely-governed claude/gemini read `FAIL`.
4. **Codex governance is post-hoc**; claude's is enforcing — inconsistent.
5. **Repo-level hooks don't reach orchestrator worktrees** — each work unit is
   a fresh `git worktree` from HEAD; only home-rooted/global hooks reach them.
6. **The kernel redeploy pipeline is broken** — `chitin-kernel-redeploy` fails
   on a git fast-forward error, so merged kernel fixes silently never ship.
   This is the meta-cause: it will keep stranding fixes until repaired.

## Fix sequence (priority order)

1. ✅ **Kernel redeployed** — `install-kernel.sh` ran clean; `chitin-kernel`
   rebuilt from current main (post-#861), smoke-tested, installed.
2. ✅ **Hermes + OpenClaw gateways restarted** — both ran pre-#861 plugin code;
   `hermes gateway restart` + `systemctl --user restart openclaw-gateway.service`
   loaded the #861 plugins.
3. ✅ **Verified** — clawta: 121 `gov-decisions` rows post-restart; hermes: a
   live probe at 04:18Z produced attributed `shell.exec` decisions.
4. **copilot** — align `copilot-sdk` with copilot CLI v1.x (patch the
   `timestamp` type or pin a compatible CLI); rebuild; wire the orchestrator's
   copilot driver to `chitin-kernel drive copilot`; fix the shim's
   exit-0-on-failure bug.
5. **gemini** — authenticate the Gemini CLI, then re-probe.
6. **Harden `install-kernel.sh`** — the `git pull --ff-only origin main` can hit
   `Cannot fast-forward to multiple branches` when a pull is needed; replace
   with `git fetch` + `git merge --ff-only origin/main` (a single explicit ref
   cannot produce multiple merge heads).
7. **Fix `chitin doctor`** — validate the live CLI path; credit global hooks.
8. **Unify telemetry sinks** — one governance log, or a reader across all three.

## Changes applied this session

- `chitin init --all -g` — global hooks for claude/codex/copilot/gemini.
- Migrated codex `[features].codex_hooks` → `[features].hooks`.
- Reverted redundant repo-level `chitin init` changes.
- **Redeployed the kernel** — ran `install-kernel.sh`; `chitin-kernel` rebuilt
  from current main (now post-#861), smoke-tested, installed; hooks
  reinstalled, systemd units synced, budget envelope rotated. The transient
  git bug did not fire (`HEAD == origin/main`, nothing to pull).

## hermes/clawta telemetry — RESTORED (2026-05-22)

The kernel was redeployed to post-#861, then both gateways restarted to load
#861's plugins (`hermes gateway restart`; `systemctl --user restart
openclaw-gateway.service`). Verification:

- **clawta** — 121 attributed `gov-decisions` rows in the central log within
  minutes of the restart (`agent:clawta`, `allowed:true`).
- **hermes** — a live `hermes chat` probe produced `driver:hermes` /
  `agent:hermes` decisions (`shell.exec`, `router.signal`, `allowed:true`).

Both now flow into the central `gov-decisions` sink — confirming #861's
OpenClaw policy-resolution and Hermes session-id fixes are live.

## Final state — all 6 in-scope drivers proven governed

| Driver | State |
|---|---|
| claude | ✅ proven |
| codex | ✅ proven (post-hoc sink) |
| openclaw | ✅ proven |
| hermes | ✅ **restored** + proven |
| clawta | ✅ **restored** + proven |
| copilot | ✅ **fixed** + proven — `chitin-kernel drive copilot` shim, `agent:copilot-cli` decisions |
| gemini | ⊘ descoped (operator decision — Antigravity migration pending) |

The copilot fix is **functionally complete and verified** but its code change
(SDK fork + `replace` directive + orchestrator driver rewiring) is **uncommitted** —
it needs a PR to survive a clean rebuild. Spec 083's remaining stories (US2
redeploy hardening, US3 codex-central-routing + governed worktrees, US4 unified
telemetry + `chitin doctor` fix) are tasked in `specs/083-driver-governance-telemetry/tasks.md`.
