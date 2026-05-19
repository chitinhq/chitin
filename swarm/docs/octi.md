# Octi — operator README (slice 2: controller loop)

`octi` is the slice-2 deliverable of spec 038. It is the deterministic
outer loop that drives a Mini session: it watches `status.json` for
staleness, nudges on stall, runs the `verify` command independently on a
`state=done` claim, and writes a `controller_verdict.json` capturing
pass / fail / no_verifier.

**Slice 2 surface**: `octi open / attach / verify / pause / resume / stop / status`.

Slice 1 (`mini`) remains the interface layer; `octi` imports only
`MiniSession` from `swarm.mini`. The grep regression test in
`swarm/tests/test_mini_import_boundary.py` enforces this (spec 038 AC10).

## Install

```sh
swarm/bin/install-mini.sh   # now also symlinks `octi` into ~/.local/bin/
```

## Quickstart

Two-process pattern (controller + session):

```sh
# In one terminal — launches kitty + Claude + controller loop in foreground
octi open --goal "Fix the dispatch race in clawta-poller"

# In another terminal — at any time
octi status                # status.json + controller_verdict.json + paused flag
octi pause                 # stop outer-loop nudges/verifies
octi resume
octi verify                # one-shot independent verify against status.json
octi stop                  # terminate kitty session AND controller
```

Recovery: `octi attach --goal-id <id>` brings the controller back online
against an already-open session (e.g. one that was launched with bare
`mini open`).

## State machine (controller's view)

The controller polls `status.json` every `OCTI_POLL_SECONDS` (default 5s)
and produces one tick outcome per iteration:

| Outcome | Trigger | What happens |
|---|---|---|
| `idle` | `state ∈ {starting, working, verifying}` and updated recently | sleep, no action |
| `first_write_pending` | no `status.json` yet, inside grace | wait |
| `first_write_expired` | no `status.json` after `OCTI_FIRST_WRITE_GRACE_SECONDS` | nudge once, then cooldown |
| `nudged_stale` | `now - updated_at > OCTI_STALL_SECONDS` | send stall nudge via `MiniSession.nudge` |
| `nudge_cooldown` | would nudge but inside `OCTI_NUDGE_COOLDOWN_SECONDS` of last nudge | wait |
| `blocked_observed` | `state=blocked` | no nudge, no verify (operator must unblock) |
| `paused` | `controller.paused` flag file present | tick is a no-op |
| `terminal_done_passed` | `state=done` + verify rc=0 | loop ends, exit 0 |
| `terminal_done_failed` | `state=done` + verify rc≠0 / timeout / error | loop ends, exit 1 |
| `terminal_needs_review` | `state=done` + empty/missing `verify` | loop ends, exit 1 |
| `terminal_failed` | `state=failed` | loop ends, exit 1 |

Verify is run **once per claim**, keyed by `status.updated_at`. A new
`state=done` claim with a newer `updated_at` re-runs verify.

## Files the controller owns

| File | Owner | Purpose |
|---|---|---|
| `status.json` | Claude Code | session state contract |
| `controller_verdict.json` | Octi controller | last verify result `{verdict, returncode, stdout, stderr, duration_seconds, timed_out, status_updated_at, verified_at}` |
| `controller_nudges.jsonl` | Octi controller | append-only nudge log `{ts, reason, result, summary}` |
| `controller.paused` | `octi pause` / `octi resume` | flag file gating outer-loop ticks |
| `controller.pid` | `octi open` / `octi attach` | running controller's PID; consumed by `octi stop` |

Claude never overwrites the controller-owned files; the controller never
overwrites `status.json`. Combined they form the workflow's full state.

## `verify` command contract

The string in `status.json.verify` is fed verbatim to `/bin/sh -c` with
`cwd = <worktree>`. Use whatever shell composition you want
(`pytest tests/foo.py && go test ./internal/bar/...`).

Verdicts:
- rc = 0 → `passed`
- rc ≠ 0 → `failed`
- timed out (`OCTI_VERIFY_TIMEOUT_SECONDS`, default 600s) → `timeout`
- empty / whitespace command → `no_verifier` → terminal `needs_review`
- `OSError` (shell missing etc.) → `error` → treated as fail

The verifier never mutates the worktree itself; whatever the verify
command does is the verify command's responsibility.

## Env vars

| Var | Purpose | Default |
|---|---|---|
| `OCTI_POLL_SECONDS` | Controller poll cadence | `5` |
| `OCTI_STALL_SECONDS` | `status.updated_at` age threshold for nudge | `180` |
| `OCTI_NUDGE_COOLDOWN_SECONDS` | Minimum gap between stall nudges | `300` |
| `OCTI_FIRST_WRITE_GRACE_SECONDS` | Tolerance before nudging on missing `status.json` | `600` |
| `OCTI_VERIFY_TIMEOUT_SECONDS` | Verify subprocess timeout | `600` |
| `OCTI_DISCORD_WEBHOOK_URL` | Inherited from Mini (used by mini watch, not by controller directly) | unset → no Discord |
| `OCTI_OPERATOR` | Inherited from Mini (lease holder id) | `getpass.getuser()` |

The controller does not post to Discord directly in slice 2 — operator
visibility comes from stderr lines prefixed `[octi]` plus the side
effects of `MiniSession.nudge(...)` (which posts to the session's
webhook). `mini watch` covers the streaming Discord surface.

## Subcommands

### `octi open --goal "..."`

Calls `MiniSession.open(...)` to launch kitty + Claude, then enters the
controller loop in the foreground. Operator can:

- Run `&` for backgrounded use (controller still writes its PID).
- Ctrl-C to terminate the controller (the kitty session keeps running).
- Use `--max-ticks N` to bound the loop (testing).

### `octi attach --goal-id <id>`

Same as `open` but for an already-open session. Useful when the session
was launched with bare `mini open` (no controller) and you want to add
governance.

### `octi verify [--goal-id <id>] [--timeout SECS]`

Reads `status.json.verify`, runs it once, writes `controller_verdict.json`,
exits with:

| Exit | Meaning |
|---|---|
| 0 | `passed` |
| 6 | `status.json` not yet written |
| 10 | `failed` / `timeout` / `error` |
| 11 | `no_verifier` (empty `verify` field → `needs_review`) |

### `octi pause` / `octi resume`

Touch / remove `controller.paused`. Slice 2 semantics: outer-loop only.
The kitty session and Claude inside it keep running. (Spec slice 3 may
extend this to a Claude-honored pause contract.)

### `octi stop [--reason "..."]`

Sends `SIGTERM` to the running controller (via `controller.pid`),
then calls `MiniSession.stop(reason=...)` which closes the kitty window
and writes `state=failed` to `status.json`.

### `octi status`

Prints a JSON object combining `status.json`, `controller_verdict.json`,
and the `paused` boolean — the operator's one-shot read of the full
workflow state.

## Tests

```sh
# All octi tests (no live kitty needed)
python3 -m unittest discover -s swarm/tests -p 'test_octi_*.py' -v

# Mini + octi together (regression sweep)
python3 -m unittest discover -s swarm/tests -p 'test_mini_*.py' -p 'test_octi_*.py'
```

Test layout:

| File | Coverage |
|---|---|
| `test_octi_verifier.py` | `run_verify` boundaries (no_verifier / passed / failed / timeout / error / cwd / truncation / env) |
| `test_octi_controller.py` | Controller invariants: terminal states, done-claim verification, verify-once-per-claim, stall + cooldown, blocked observed, pause flag, run() loop |
| `test_octi_cli.py` | `octi verify / pause / resume / status` exit codes and side effects |
| `test_mini_import_boundary.py` | AC10: `swarm/octi/` only imports `MiniSession` from `swarm/mini/` |

## Boundary

Octi is **deterministic workflow governance**. It owns:

- Outer-loop polling cadence
- Stall detection thresholds + nudge cooldowns
- Verifier execution + verdict persistence
- Pause/resume of the outer loop
- Controller PID lifecycle

Octi does **not** own:

- The kitty terminal session (Mini owns it)
- The Discord webhook surface (`mini watch` owns it)
- The status.json contract (Claude owns it)
- Kanban state machines (slice 3+ may bridge to them)
- Temporal workflow state (slice 4)

These boundaries map to file ownership above and to the import boundary
enforced by AC10.
