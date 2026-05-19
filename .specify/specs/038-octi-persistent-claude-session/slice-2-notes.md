# Spec 038 slice-2 — AC tightening proposal

**Status**: draft, not ratified. Decision-log for the slice-2 controller
loop after PR #782 shipped (`feat(swarm): octi slice 2 — controller loop +
independent verifier`). The implementation made concrete choices that the
spec leaves ambiguous. This file proposes binding those choices in (or
out of) spec 038 before slice 3.

**Author lens (Knuth)**: name the boundaries, name the tie-breakers.
Every "TBD" in the spec is a place two implementations would disagree.
Each section below states the current spec text (or its silence), the
boundary case that exposes the gap, the slice-2 choice in PR #782, and a
proposed binding AC.

---

## (a) Stall threshold: what counts as "stale `status.json`"

### Current spec text (lines 129–138)

> ### Stall detection (three layers, priority order)
>
> 1. `status.json` `updated_at` stale beyond `3 × interval + jitter` → nudge
> 2. Kitty window output activity (tool calls, file writes) → still alive,
>    just not updating JSON
> 3. Cursor at input prompt for >N seconds with no status update → likely stuck
>
> Slice 1: layers 1 and 3 are observable via `mini status`. Automatic
> nudge on stall is slice 2.

### Gaps

1. **"interval" is undefined.** Is it the controller poll cadence
   (`OCTI_POLL_SECONDS`)? The expected status-write cadence Claude
   commits to? The two can differ by an order of magnitude. Two
   implementations reading this could pick e.g. 15s vs. 180s.
2. **"jitter" is undefined.** Random additive? Random multiplicative?
   What seed? Without a definition the value is non-reproducible and
   un-testable.
3. **No first-write rule.** When the session is freshly opened and
   `status.json` does not exist yet, `updated_at` is undefined.
   "Stale beyond X seconds" has no meaning. The spec is silent on the
   grace window before the first write.
4. **Layers 2 and 3 are not implemented in slice 2.** Spec leaves it
   open whether kitty-output-activity and cursor-at-prompt detection
   are slice-2 binding. PR #782 implements only layer 1.
5. **No definition of "nudge" frequency under sustained stall.** If
   the model is wedged, a 5-second-poll controller would otherwise
   send a nudge every 5 seconds.

### Slice-2 choices in PR #782

- Stall threshold = `OCTI_STALL_SECONDS` (default 180s). Configurable per
  session via env, not via `status.json` writer self-declaring a cadence.
- No jitter implemented. Threshold is a deterministic constant.
- First-write grace = `OCTI_FIRST_WRITE_GRACE_SECONDS` (default 600s).
  Inside grace → `first_write_pending` tick outcome (no nudge). Past
  grace with no `status.json` → one nudge then cooldown.
- Nudge cooldown = `OCTI_NUDGE_COOLDOWN_SECONDS` (default 300s). After
  a nudge, no further nudges until cooldown elapses regardless of
  staleness.
- Layers 2 and 3 deferred to slice 3 (and re-titled "presence detection"
  in the proposed wording below).

### Proposed binding AC

> **AC15 (slice 2, binding).** The controller defines staleness as:
> `now() - status.updated_at > stall_seconds`, where `stall_seconds`
> is a controller-config constant (default 180s, env override
> `OCTI_STALL_SECONDS`). No jitter is applied; staleness is a
> deterministic threshold so tests can pin behaviour.
>
> **AC16 (slice 2, binding).** Before `status.json` is first written
> by the model, the controller observes a `first_write_grace`
> window (default 600s, env override `OCTI_FIRST_WRITE_GRACE_SECONDS`).
> Inside the grace window the controller takes no nudge action;
> past it, the controller emits one nudge per cooldown window.
>
> **AC17 (slice 2, binding).** Once a stall nudge is sent, no further
> stall nudges fire until `nudge_cooldown_seconds` elapses (default
> 300s, env override `OCTI_NUDGE_COOLDOWN_SECONDS`). The cooldown
> applies whether or not the nudge successfully reached the kitty
> session.
>
> **AC18 (slice 3, non-binding for MVP).** Presence detection layers
> 2 (kitty output activity) and 3 (cursor at prompt for N seconds
> with no status update) are slice-3 work. They refine staleness but
> are not gating for slice 2 to ship.

---

## (b) Nudge on stall: what does the nudge look like?

### Current spec text

Spec says only: "sends nudges on stall." Nothing else.

### Gaps

1. **Message content is undefined.** Two implementations could send
   wildly different prompts ("are you alive?" vs. an elaborate
   regenerated context dump). The model's response correlates with
   the prompt — without a spec, behaviour is non-reproducible.
2. **No escalation policy.** What happens after N nudges? Mark
   blocked? Page the operator? Stop the session?
3. **No nudge audit trail.** Spec doesn't say nudges must be logged,
   where, or with what schema. Without an audit trail, post-incident
   forensics ("did the controller try to recover?") are guesswork.
4. **No interaction with the slice-1 input lease.** Spec mentions
   `input.lock` only in the context of operator nudges. The
   controller-issued nudge needs a documented lease policy too —
   does it wait? Steal? Skip?

### Slice-2 choices in PR #782

- Nudge content is templated, deterministic, and short:
  > ⏰ stall-nudge from Octi controller: `<reason summary>`
  > If you are still working, write status.json now with state=working
  > and a refreshed updated_at. If you are blocked, set state=blocked
  > with a one-line blocker description in blockers[].
- `holder="octi-controller"` is used as the lease identity. The
  controller does not steal the lease; if the operator holds it, the
  nudge raises `LockHeldError` and the failure is logged but cooldown
  still ticks (so the controller doesn't spin).
- Every nudge attempt (success or failure) appends a JSON line to
  `<state_dir>/controller_nudges.jsonl`:
  `{ts, reason, result, summary, error?}` where `reason` ∈
  {`no_status_yet`, `status_stale`} and `result` ∈ {`nudge_sent`,
  `nudge_failed`}.
- No escalation policy yet — the controller will keep nudging at
  cooldown rhythm until a terminal state is reached. Operator
  observes via `octi status` + `controller_nudges.jsonl`.

### Proposed binding AC

> **AC19 (slice 2, binding).** Stall nudges are issued via
> `MiniSession.nudge(message, holder="octi-controller")`. The
> message is a deterministic, short template that:
>
> 1. Names the controller as the sender (so the model can
>    distinguish controller nudges from operator nudges).
> 2. States the reason in one short clause
>    (e.g. `status.json stale 240s (state=working)`).
> 3. Instructs the model to either refresh `status.json` with a
>    current `updated_at` or transition to `state=blocked` with a
>    one-line blocker reason.
>
> **AC20 (slice 2, binding).** The controller writes one JSON line per
> nudge attempt to `<state_dir>/controller_nudges.jsonl`. Required
> keys: `ts` (unix seconds), `reason` (enum), `result` ∈
> {`nudge_sent`, `nudge_failed`}. Optional: `summary`, `error`.
> The file is append-only; the controller never rotates or truncates
> it. (Slice 4 may add rotation if size becomes a concern.)
>
> **AC21 (slice 2, binding).** If the input lease is held by another
> holder, the controller MUST NOT steal it. The nudge attempt is
> recorded as `result=nudge_failed` with `error="lock held by <holder>"`
> and cooldown ticks normally. The operator's hold takes precedence.
>
> **AC22 (slice 3, non-binding for MVP).** Escalation policy after N
> nudges (Discord operator ping, mark blocked, suspend session) is
> slice-3 work. Slice 2 holds the policy at "nudge at cooldown
> rhythm until terminal state."

---

## (c) Verifier execution contract

### Current spec text (lines 119–127 + slice-2 AC, lines 273–278)

> - `state=done` is a **claim**, not acceptance. The controller runs the
>   `verify` command independently before marking completion.
> - If no `verify` command is configured, completion lands in
>   `needs_review`, never auto-`done`.
> - The controller does NOT trust exit codes or TUI output for completion.
>
> **Slice 2:** `octi verify` runs the `verify` command from
> `status.json` independently and reports pass/fail. Completion
> requires independent verifier pass. No verifier configured →
> `needs_review`, not `done`. Stall detection nudges automatically.

### Gaps

1. **"Independently" is undefined.** Same Python process? Subprocess
   via `subprocess.run`? Container? Different machine? Each is a
   different security and timing posture.
2. **Exit code semantics are missing.** `verify` returns an integer
   — what is `pass` vs `fail`? rc=0 is conventional but the spec
   doesn't say. What about negative rcs (signals)? What about
   timeouts?
3. **Stdout/stderr policy is missing.** Captured? Streamed? Discarded?
   Where surfaced — Discord, log file, operator stdout? Truncated?
4. **cwd is undefined.** Operator's cwd? Worktree? State dir?
5. **Timeout is missing.** "verify command from status.json" can be
   anything — a 1ms `true` or an unbounded `pytest -p no:cacheprovider`.
   Without a timeout the controller's loop could deadlock.
6. **Environment scrubbing is missing.** Inherits operator env?
   Scrubbed? Whitelisted? Secret-leak risk if a verify command
   echoes env vars.
7. **The phrase "controller does NOT trust exit codes ... for
   completion"** (line 125) directly contradicts the slice-2 binding
   AC ("reports pass/fail"). The controller has to trust *something*
   from the verify command. The intent is presumably: "doesn't trust
   exit codes from the *agent*'s claim of done, only from an
   independently-invoked verify command." The wording should be
   tightened to remove the apparent contradiction.
8. **Concurrency: can two verify invocations run for the same goal?**
   Slice 2 deduplicates by `status.updated_at`. Spec doesn't say.
9. **Verdict persistence is missing from spec.** Slice 2 writes
   `controller_verdict.json` — spec mentions no such file.

### Slice-2 choices in PR #782

- `independently` = `subprocess.run(["/bin/sh", "-c", cmd], cwd=worktree,
   capture_output=True, timeout=OCTI_VERIFY_TIMEOUT_SECONDS)`.
   Same host, separate process tree, full env inherited unless caller
   overrides (`Verifier(env=...)`).
- Verdict mapping:
  - rc = 0 → `passed`
  - rc ≠ 0 → `failed`
  - timeout → `timeout` (treated as `done_failed` for outcome routing)
  - OSError (e.g. shell missing) → `error` (treated as `done_failed`)
  - empty / whitespace command → `no_verifier` → `needs_review`
- Stdout + stderr captured and truncated to 8000 chars each (so a
  pathological loop doesn't blow up the verdict file or Discord post).
- cwd = `session.worktree` (the operator's worktree, not their shell
  cwd). State dir is for state, not for code under test.
- Timeout = `OCTI_VERIFY_TIMEOUT_SECONDS` (default 600s).
- No env scrubbing in slice 2 — verify command inherits operator env
  including any webhooks and tokens. Documented as caller responsibility.
- Verdict persisted atomically to
  `<state_dir>/controller_verdict.json` via tmp+rename. One-shot
  `octi verify` writes a `via: "octi verify (one-shot)"` marker; the
  controller loop writes nothing in the `via` field.
- Verify-once-per-claim, keyed by `status.updated_at`. A new claim
  (different `updated_at`) re-runs verify; a duplicate tick with the
  same `updated_at` reads the existing verdict.

### Proposed binding AC

> **AC23 (slice 2, binding, supersedes line 125 wording).** "Trust"
> language is tightened: the controller does not trust the model's
> *claim* of completion. The model's claim is encoded as
> `status.json.state=done`. The controller's acceptance of that claim
> requires an independent verifier run, scored by the verifier's exit
> code (see AC24).
>
> **AC24 (slice 2, binding).** The verifier is a single shell
> subprocess: `["/bin/sh", "-c", <status.json.verify>]`, cwd =
> session worktree, timeout = `verify_timeout_seconds` (default 600s,
> env override `OCTI_VERIFY_TIMEOUT_SECONDS`). The verdict mapping is:
>
> | Result | Verdict | Outer outcome |
> |---|---|---|
> | rc == 0 | `passed` | `terminal_done_passed` |
> | rc != 0 | `failed` | `terminal_done_failed` |
> | timeout | `timeout` | `terminal_done_failed` |
> | OSError / spawn fail | `error` | `terminal_done_failed` |
> | empty / whitespace verify | `no_verifier` | `terminal_needs_review` |
>
> **AC25 (slice 2, binding).** stdout and stderr are captured and
> truncated to a fixed cap (8000 chars each in slice 2; adjustable).
> Both are persisted to `controller_verdict.json`. No streaming to
> Discord by default.
>
> **AC26 (slice 2, binding).** Verify-once-per-claim. The controller
> deduplicates by `status.json.updated_at`: a second tick that sees
> the same `state=done` + `updated_at` does NOT re-run the verifier;
> it returns the existing verdict outcome. A new claim with a newer
> `updated_at` re-runs the verifier.
>
> **AC27 (slice 2, binding).** The verifier's verdict is persisted
> atomically to `<state_dir>/controller_verdict.json` via
> write-tmp-then-rename. Required keys: `verdict`, `status_updated_at`,
> `verified_at`, `command`, `returncode`, `stdout`, `stderr`,
> `duration_seconds`, `timed_out`. Optional: `via` (e.g. `"octi
> verify (one-shot)"`). The controller never overwrites
> `status.json`; Claude never overwrites `controller_verdict.json`.
>
> **AC28 (slice 2, binding, security note).** The verifier inherits
> the operator's environment by default. Callers wanting a scrubbed
> environment must construct `Verifier(env=...)` explicitly.
> `status.json.verify` is treated as opaque shell text; the writer is
> responsible for not embedding shell injection of operator-supplied
> data. (Sentinel-style scrubbing of secrets in verify output is
> slice-3+ work.)

---

## Spec-text edit summary (proposed)

If ratified, edit `.specify/specs/038-octi-persistent-claude-session/spec.md`:

1. **Replace lines 129–138** (Stall detection three-layers block) with
   a slice-by-slice breakdown: slice-2 binds layer 1 only with the
   AC15/AC16/AC17 numbers; layers 2 + 3 are slice-3 "presence
   detection" with AC18.
2. **Replace lines 119–127** wording to remove the "controller does
   NOT trust exit codes" line, replacing with AC23 wording.
3. **Append AC15–AC28 to the slice-2 AC list** (currently lines 268–
   282 only contain a one-paragraph slice-2 bullet).
4. **Add a new "Controller-owned files" subsection** under
   `Architecture` listing `controller_verdict.json`,
   `controller_nudges.jsonl`, `controller.paused`, `controller.pid`,
   keyed by owner per AC27.

---

## Open questions (operator decision needed)

These are intentionally left for red/Ares/Clawta to ratify, not for
the implementing agent to pick:

1. **Should `OCTI_STALL_SECONDS` defaults be tightened?** 180s might
   be too lenient if the Mini-controlled session is expected to write
   `status.json` every 60s. A 90s default would match the loose 1.5×
   expected-cadence rule of thumb.
2. **Should the verify command get an isolated environment by
   default?** Slice 2 says "no, operator owns it." A safer default
   would be a `PATH`-only-passthrough environment with explicit
   whitelisting of `HOME`, `USER`, etc. Risk: brittle tests; reward:
   secret-leak insurance.
3. **Should `octi verify` (one-shot CLI) also enforce
   verify-once-per-claim?** Slice 2 says no — operators can re-run it
   freely. The implicit contract: one-shot is for humans, controller
   loop is for the workflow. The operator can re-verify after fixing
   the worktree without bumping `status.updated_at`. If this is
   acceptable, encode it; if not, share the dedupe key.
4. **Should AC18 (presence detection layers 2+3) move from slice-3 to
   slice-2.1?** They're cheap to add against the kitty get-text and
   `mini status` machinery. The question is whether they belong with
   the controller loop or with the state machine. Probably the
   former, in which case fast-track them.
5. **Cooldown override?** Operator might want to force a nudge
   immediately after a failed one ("the lease was held, I just
   released it, please try again"). Slice 2 has no `--force` switch.
   Worth adding?

---

## Cross-references

- Spec lines 88–138 (Architecture > Slices 2-4, Completion semantics,
  Stall detection).
- Spec lines 268–282 (Slice 2-4 AC, non-binding for MVP — these
  proposed AC turn the slice-2 paragraph into a binding numbered list).
- PR #782 — `feat(swarm): octi slice 2 — controller loop +
  independent verifier`. Implementation under `swarm/octi/`,
  `swarm/bin/octi`, tests under `swarm/tests/test_octi_*.py`.
- Slice-1 AC10 (line 250) — import boundary — continues to hold:
  `swarm/octi/controller.py` has exactly one `from swarm.mini import
  MiniSession` line.
