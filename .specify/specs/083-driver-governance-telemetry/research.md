# Phase 0 Research: Driver Governance & Telemetry Integrity

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

The spec carries no `NEEDS CLARIFICATION` markers — the 2026-05-21 audit
(`docs/2026-05-21-orchestrator-driver-telemetry-audit.md`) supplied the
inventory and the root causes. Phase 0 therefore fixes the **approach** for each
of the seven audit defects: which governance path, which sink, which rollout
posture.

## Decision 1 — copilot governance: fix the shim, retire the hook

**Decision**: Govern copilot through the kernel shim (`chitin-kernel drive
copilot`), not a CLI hook. The shim's startup failure is a `copilot-sdk` vs
copilot-CLI version mismatch: the CLI (v1.0.51) returns `timestamp` as a
string; the SDK declares `int64` in every published version (v0.2.2–v1.0.0-beta.4).
Resolve by pinning a compatible CLI/SDK pair or carrying a minimal local SDK
patch (replace directive) that accepts a string/number timestamp. Also fix the
shim's exit-0-on-failure bug.

**Rationale**: The audit proved the copilot **hook never fires** — tested at
both `~/.copilot/hooks/hooks.json` and project `.github/hooks/hooks.json`, zero
telemetry either way. The Copilot CLI does not honour chitin's hook config. The
shim is the only viable path and is the one the operator already built.

**Alternatives considered**: (a) keep debugging the hook — rejected, two
locations already proven dead; (b) wrap copilot in a generic subprocess
governor — rejected, duplicates the shim.

## Decision 2 — codex telemetry: route to the central sink

**Decision**: Codex governance decisions must land in the central
`gov-decisions` log (FR-005), not only per-session `codex-events-*.jsonl`.
Route the codex hook's decisions through the same kernel path claude uses.

**Rationale**: The audit found codex emits only `rule_id:codex-post-hoc`
records into per-session files; its central telemetry stopped after May 18.
Post-hoc, per-session telemetry is invisible to the watchdogs and the unified
view. The central sink is the contract every other driver meets.

**Alternatives considered**: leave codex on per-session files and let US4's
reader merge them — rejected as the *only* fix: it normalises the read side but
leaves codex's telemetry second-class and post-hoc. US4's reader still covers
historical codex-events; routing is the forward fix.

## Decision 3 — gemini: introduce the *unverified* driver state

**Decision**: Add a third driver-governance state — *unverified* — distinct
from *governed* and *ungoverned*. A driver whose CLI cannot start
(unauthenticated, missing) is *unverified*; the system reports it as such and
never as governed (FR-014). Authenticating the Gemini CLI is an operator
precondition, out of feature scope.

**Rationale**: The Gemini CLI has no credentials on the box, so no probe can
run. A binary governed/ungoverned model forces a wrong answer; *unverified* is
the honest one and prevents a false "healthy".

**Alternatives considered**: treat unverifiable as ungoverned — rejected,
conflates "proven bad" with "can't tell" and would cry wolf.

## Decision 4 — hermes/clawta: deploy, don't rewrite

**Decision**: US1 ships no new kernel code. PR #861 already root-caused and
fixed the three hermes/clawta telemetry bugs (kernel chain-id stamping, Hermes
plugin `session_id` forwarding, OpenClaw plugin policy-file resolution). US1 is:
deploy the #861 kernel (done this session) and **restart the Hermes gateway +
agent** so they load the #861 plugins.

**Rationale**: The audit proved the running kernel pre-dated #861 by 76 minutes
and both Hermes processes started before #861. The fix exists in `main`; the
gap was purely delivery.

**Alternatives considered**: re-fix in a new PR — rejected, #861 is already
correct and merged.

## Decision 5 — redeploy git step: fetch + `merge --ff-only`

**Decision**: In `install-kernel.sh`, replace `git pull --ff-only origin main`
with `git fetch origin main` followed by `git merge --ff-only origin/main`.
Preserve the autostash behaviour with an explicit stash/pop around the merge.

**Rationale**: `git pull origin main` can die with *"Cannot fast-forward to
multiple branches"* when `FETCH_HEAD` carries more than one merge candidate.
`git merge --ff-only origin/main` takes exactly one explicit ref — it cannot
produce multiple merge heads. Deterministic.

**Alternatives considered**: `git reset --hard origin/main` — rejected, discards
the operator's uncommitted working-tree writes that autostash protects.

## Decision 6 — staleness & failure must be loud

**Decision**: (a) Add kernel-staleness detection — compare the running kernel
build against the merged kernel source — surfaced through `chitin health`.
(b) A redeploy failure raises an operator-visible alert, not only a line in
`~/.cache/chitin/install-kernel.jsonl`.

**Rationale**: The regression was invisible for hours because a failed redeploy
only wrote a structured log nobody watched. Staleness and failure must page.

**Alternatives considered**: rely on the existing structured log — rejected,
that is exactly what failed to surface the regression.

## Decision 7 — `chitin doctor`: validate live, not by file

**Decision**: Rebuild `chitin doctor` to validate each driver by observing a
real governed invocation produce a `gov-decision` row (live probe), and to
credit a driver governed by a global hook as passing.

**Rationale**: The audit caught doctor reporting copilot and codex
`test-emit=ok [OK]` while both produced zero real telemetry — a file-marker
check, not a behaviour check. It also marked globally-hooked claude/gemini
`FAIL` because it only inspects the project file.

**Alternatives considered**: keep the file check, add a separate probe command
— rejected, leaves the misleading verdict in the tool operators already trust.

## Decision 8 — observe before enforce

**Decision**: Any change that tightens governance enforcement rolls out as
**observe/record first** (emit a flagged decision), then deny once the
telemetry shows the blast radius. Applies to codex-routing parity and any
future gate change.

**Rationale**: Constitution-aligned and the established chitin pattern (spec
028 phased rollout; the post-hoc recording model). Hard-denying on day one
risks a governance outage worse than the gap.

**Alternatives considered**: flip straight to enforce — rejected, unmeasured
blast radius.

## Cross-references

- **Spec 070** — orchestrator; FR-008 telemetry-as-side-effect; FR-013/014 worktrees.
- **Spec 081** — systemd cron layer (out of scope here).
- **PR #861** — the merged hermes/clawta telemetry fix US1 deploys.
- **Constitution §1** — kernel is the only chain writer; US4's interface is read-only.
