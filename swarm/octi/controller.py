"""Octi controller loop (spec 038 slice 2).

Drives a Mini session via its public ``MiniSession`` interface only.
Watches ``status.json`` for staleness and ``state=done`` claims; nudges
on stall, runs the verifier independently on completion claims.

Invariants (Knuth: state them in one sentence each):

1. **Single source of session truth.** All interaction with the Mini
   session goes through ``MiniSession`` — no kitty/transcript/webhook
   internals are imported here. CI grep enforces this.

2. **Claude owns ``status.json``; the controller owns
   ``controller_verdict.json``.** The controller never overwrites
   Claude's status file. Its verdict is a sibling artifact.

3. **Verify-once-per-claim.** Each ``state=done`` claim is verified
   exactly once, keyed by the claim's ``updated_at``. A second tick that
   sees the same updated_at + state=done does not re-run verify; a new
   claim (different updated_at) does.

4. **Nudge cooldown.** Stall nudges fire at most once per
   ``nudge_cooldown_seconds`` regardless of how stale status becomes.
   Eliminates nudge storms when Claude is genuinely blocked.

5. **Pause is outer-loop only.** ``controller.paused`` flag file gates
   nudge + verify, but the kitty session and Claude inside it are not
   suspended. (Slice 3 may add a richer pause contract.)

6. **Terminal states end the loop.** ``failed``, ``needs_review``, and
   ``done+verifier_passed`` end the outer loop. ``blocked`` does not —
   operator may unblock.
"""

from __future__ import annotations

import dataclasses
import json
import os
import time
from pathlib import Path
from typing import Callable

from swarm.mini import MiniSession  # AC10: this is the ONLY import from swarm.mini.

from .verifier import VerifyResult, Verifier


# -------- config & enums --------

DEFAULT_POLL_SECONDS = 5.0
DEFAULT_STALL_SECONDS = 180
DEFAULT_NUDGE_COOLDOWN_SECONDS = 300
DEFAULT_FIRST_WRITE_GRACE_SECONDS = 600
DEFAULT_VERIFY_TIMEOUT_SECONDS = 600

TERMINAL_STATES_NO_VERIFY = frozenset({"failed", "needs_review"})
TERMINAL_OUTCOMES = frozenset({
    "terminal_failed", "terminal_needs_review",
    "terminal_done_passed", "terminal_done_failed",
})


@dataclasses.dataclass
class ControllerConfig:
    poll_seconds: float = DEFAULT_POLL_SECONDS
    stall_seconds: int = DEFAULT_STALL_SECONDS
    nudge_cooldown_seconds: int = DEFAULT_NUDGE_COOLDOWN_SECONDS
    first_write_grace_seconds: int = DEFAULT_FIRST_WRITE_GRACE_SECONDS
    verify_timeout_seconds: int = DEFAULT_VERIFY_TIMEOUT_SECONDS

    @classmethod
    def from_env(cls, env: dict | None = None) -> "ControllerConfig":
        e = env if env is not None else os.environ
        def _f(name: str, default: float) -> float:
            try:
                return float(e[name]) if name in e else default
            except (ValueError, TypeError):
                return default
        def _i(name: str, default: int) -> int:
            try:
                return int(e[name]) if name in e else default
            except (ValueError, TypeError):
                return default
        return cls(
            poll_seconds=_f("OCTI_POLL_SECONDS", DEFAULT_POLL_SECONDS),
            stall_seconds=_i("OCTI_STALL_SECONDS", DEFAULT_STALL_SECONDS),
            nudge_cooldown_seconds=_i("OCTI_NUDGE_COOLDOWN_SECONDS", DEFAULT_NUDGE_COOLDOWN_SECONDS),
            first_write_grace_seconds=_i("OCTI_FIRST_WRITE_GRACE_SECONDS", DEFAULT_FIRST_WRITE_GRACE_SECONDS),
            verify_timeout_seconds=_i("OCTI_VERIFY_TIMEOUT_SECONDS", DEFAULT_VERIFY_TIMEOUT_SECONDS),
        )


# Tick outcome enum (string literals so they're trivial to log/assert).
TickOutcome = str
# Possible values:
#   "idle"                  — nothing to do this tick
#   "paused"                — controller.paused flag set; skipped tick
#   "first_write_pending"   — status.json not yet written; within grace
#   "first_write_expired"   — status.json never written; exceeded grace (nudge)
#   "nudged_stale"          — status stale > stall_seconds, sent nudge
#   "nudge_cooldown"        — would have nudged but cooldown still active
#   "blocked_observed"      — state=blocked; do not nudge, do not verify
#   "verifying"             — state=done seen, running verify (transient)
#   "terminal_done_passed"  — done + verify passed → loop ends
#   "terminal_done_failed"  — done + verify failed → loop ends
#   "terminal_needs_review" — done + no verifier configured → loop ends
#   "terminal_failed"       — state=failed → loop ends


@dataclasses.dataclass
class _State:
    """Mutable controller-internal state (verify dedupe + nudge cooldown)."""
    verified_for_updated_at: int | None = None
    last_nudge_at: float | None = None
    loop_started_at: float = 0.0


# -------- file paths --------

VERDICT_FILENAME = "controller_verdict.json"
NUDGES_LOG_FILENAME = "controller_nudges.jsonl"
PAUSED_FLAG_FILENAME = "controller.paused"
PID_FILENAME = "controller.pid"


def _verdict_path(sd: Path) -> Path:
    return sd / VERDICT_FILENAME


def _nudges_path(sd: Path) -> Path:
    return sd / NUDGES_LOG_FILENAME


def paused_flag_path(sd: Path) -> Path:
    return sd / PAUSED_FLAG_FILENAME


def pid_path(sd: Path) -> Path:
    return sd / PID_FILENAME


def write_verdict_atomic(sd: Path, payload: dict) -> Path:
    """Atomic write of controller_verdict.json (tmp + rename)."""
    p = _verdict_path(sd)
    tmp = p.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(payload, indent=2) + "\n")
    tmp.replace(p)
    return p


def read_verdict(sd: Path) -> dict | None:
    p = _verdict_path(sd)
    if not p.is_file():
        return None
    try:
        return json.loads(p.read_text())
    except json.JSONDecodeError:
        return None


def append_nudge_log(sd: Path, entry: dict) -> None:
    with _nudges_path(sd).open("a") as f:
        f.write(json.dumps(entry) + "\n")


# -------- controller --------


class Controller:
    """Outer loop for a Mini session.

    Wire it as ``Controller(MiniSession.open(...))`` from a CLI, or
    ``Controller(MiniSession.attach(goal_id))`` to drive an existing
    session.
    """

    def __init__(
        self,
        session: MiniSession,
        *,
        config: ControllerConfig | None = None,
        clock: Callable[[], float] = time.time,
        sleep_fn: Callable[[float], None] = time.sleep,
        verifier: Verifier | None = None,
        notifier: Callable[[str], None] | None = None,
    ) -> None:
        self.session = session
        self.config = config or ControllerConfig.from_env()
        self._clock = clock
        self._sleep = sleep_fn
        self._verifier = verifier or Verifier(
            cwd=session.worktree,
            timeout_seconds=self.config.verify_timeout_seconds,
        )
        self._notify = notifier or (lambda _msg: None)
        self._state = _State(loop_started_at=clock())

    # ---- introspection helpers ----

    @property
    def state_dir(self) -> Path:
        return self.session.state_dir

    def is_paused(self) -> bool:
        return paused_flag_path(self.state_dir).is_file()

    # ---- single-tick logic ----

    def tick(self) -> TickOutcome:
        sd = self.state_dir
        if self.is_paused():
            return "paused"

        status = _read_status(sd)
        now = self._clock()

        if status is None:
            # No status.json yet — give Claude grace, then nudge once.
            elapsed = now - self._state.loop_started_at
            if elapsed < self.config.first_write_grace_seconds:
                return "first_write_pending"
            if self._cooldown_remaining(now) > 0:
                return "nudge_cooldown"
            self._send_stall_nudge(
                reason="no_status_yet",
                summary=f"no status.json after {int(elapsed)}s",
                now=now,
            )
            return "first_write_expired"

        state = status.get("state", "")
        updated_at = _coerce_int(status.get("updated_at"))

        # Terminal states (immediate end, no verifier needed).
        if state == "failed":
            return "terminal_failed"

        if state == "needs_review":
            return "terminal_needs_review"

        # Done claim → run verifier (once per claim).
        if state == "done":
            return self._handle_done_claim(status, updated_at)

        # Blocked → operator-needed; do not nudge or verify.
        if state == "blocked":
            return "blocked_observed"

        # Working / starting / verifying → stall check.
        if self._is_stale(updated_at, now):
            if self._cooldown_remaining(now) > 0:
                return "nudge_cooldown"
            staleness = int(now - (updated_at or 0))
            self._send_stall_nudge(
                reason="status_stale",
                summary=f"status.json stale {staleness}s (state={state})",
                now=now,
            )
            return "nudged_stale"

        return "idle"

    def run(self, *, max_ticks: int | None = None) -> TickOutcome:
        """Loop until terminal outcome (or max_ticks for tests)."""
        ticks = 0
        while True:
            outcome = self.tick()
            if outcome in TERMINAL_OUTCOMES:
                return outcome
            ticks += 1
            if max_ticks is not None and ticks >= max_ticks:
                return outcome
            self._sleep(self.config.poll_seconds)

    # ---- internals ----

    def _is_stale(self, updated_at: int | None, now: float) -> bool:
        if updated_at is None:
            return False  # handled by first-write logic above
        return (now - updated_at) > self.config.stall_seconds

    def _cooldown_remaining(self, now: float) -> float:
        last = self._state.last_nudge_at
        if last is None:
            return 0.0
        return max(0.0, self.config.nudge_cooldown_seconds - (now - last))

    def _send_stall_nudge(self, *, reason: str, summary: str, now: float) -> None:
        message = (
            "⏰ stall-nudge from Octi controller: " + summary + "\n"
            "If you are still working, write status.json now with state=working "
            "and a refreshed updated_at. If you are blocked, set state=blocked "
            "with a one-line blocker description in blockers[]."
        )
        try:
            self.session.nudge(message, holder="octi-controller")
        except Exception as exc:  # noqa: BLE001 — lease conflict, kitty unreachable, etc.
            append_nudge_log(self.state_dir, {
                "ts": int(now),
                "reason": reason,
                "result": "nudge_failed",
                "error": repr(exc),
            })
            self._state.last_nudge_at = now  # still cool down on failure
            self._notify(f"⚠️ nudge failed: {exc}")
            return
        append_nudge_log(self.state_dir, {
            "ts": int(now),
            "reason": reason,
            "result": "nudge_sent",
            "summary": summary,
        })
        self._state.last_nudge_at = now
        self._notify(f"📣 stall-nudge sent: {summary}")

    def _handle_done_claim(self, status: dict, updated_at: int | None) -> TickOutcome:
        # Verify-once-per-claim guard (invariant 3).
        if updated_at is not None and self._state.verified_for_updated_at == updated_at:
            existing = read_verdict(self.state_dir)
            if existing is None:
                pass  # no verdict on disk? re-run defensively.
            else:
                return _verdict_to_outcome(existing.get("verdict"))

        result = self._verifier.run(status.get("verify", ""))
        verdict_payload = {
            "verdict": result.verdict,
            "status_updated_at": updated_at,
            "verified_at": int(self._clock()),
            "command": result.command,
            "returncode": result.returncode,
            "stdout": result.stdout,
            "stderr": result.stderr,
            "duration_seconds": result.duration_seconds,
            "timed_out": result.timed_out,
        }
        write_verdict_atomic(self.state_dir, verdict_payload)
        self._state.verified_for_updated_at = updated_at

        outcome = _verdict_to_outcome(result.verdict)
        self._notify_verdict(result, outcome)
        return outcome

    def _notify_verdict(self, result: VerifyResult, outcome: TickOutcome) -> None:
        if outcome == "terminal_done_passed":
            self._notify(f"✅ verifier.passed `{self.session.goal_id}` rc=0")
        elif outcome == "terminal_done_failed":
            tail = (result.stderr or result.stdout).splitlines()[-3:]
            self._notify(
                f"❌ verifier.failed `{self.session.goal_id}` rc={result.returncode} "
                f"timeout={result.timed_out}\n```\n" + "\n".join(tail) + "\n```"
            )
        elif outcome == "terminal_needs_review":
            self._notify(
                f"⚠️ verifier.absent `{self.session.goal_id}` → needs_review "
                f"(no verify command in status.json)"
            )


# -------- helpers --------


def _read_status(sd: Path) -> dict | None:
    p = sd / "status.json"
    if not p.is_file():
        return None
    try:
        return json.loads(p.read_text())
    except (json.JSONDecodeError, OSError):
        return None


def _coerce_int(v) -> int | None:
    if v is None:
        return None
    try:
        return int(v)
    except (TypeError, ValueError):
        return None


def _verdict_to_outcome(verdict: str | None) -> TickOutcome:
    if verdict == "passed":
        return "terminal_done_passed"
    if verdict == "no_verifier":
        return "terminal_needs_review"
    # failed / timeout / error all end the loop with done-failed outcome:
    # operator must intervene; the verdict file carries the detail.
    return "terminal_done_failed"
