"""Independent verifier for spec 038 slice 2.

Invariant: a verify run consumes the ``verify`` field from ``status.json``
and produces exactly one ``VerifyResult`` describing pass/fail/no-verifier/
timeout/error. The verify command is treated as opaque shell text and run
through ``/bin/sh -c`` so the writer can compose pipelines (``pytest && go
test``). Verify is intentionally *not* sandboxed beyond the configured
timeout and the worktree cwd — the contract is that whoever writes
``status.json.verify`` owns its safety.

The result is returned, never written to disk by this module — the
controller decides whether/how to persist a verdict.
"""

from __future__ import annotations

import dataclasses
import subprocess
import time
from pathlib import Path


VerifyVerdict = str  # "passed" | "failed" | "no_verifier" | "timeout" | "error"

DEFAULT_TIMEOUT_SECONDS = 600
_TRUNCATE = 8_000


@dataclasses.dataclass
class VerifyResult:
    verdict: VerifyVerdict
    command: str
    returncode: int | None
    stdout: str
    stderr: str
    duration_seconds: float
    timed_out: bool

    def to_dict(self) -> dict:
        return dataclasses.asdict(self)


def _truncate(s: str, *, limit: int = _TRUNCATE) -> str:
    if len(s) <= limit:
        return s
    return s[: limit - 32] + f"\n... [truncated {len(s) - limit + 32} chars]"


def run_verify(
    command: str | None,
    *,
    cwd: Path,
    timeout_seconds: int = DEFAULT_TIMEOUT_SECONDS,
    env: dict | None = None,
) -> VerifyResult:
    """Run the verify command and return a verdict.

    Boundary behaviour (Knuth: name the boundaries):
      - command is None or empty/whitespace → verdict='no_verifier', rc=None.
      - command times out → verdict='timeout', rc=None, timed_out=True.
      - command raises OSError (eg. sh missing) → verdict='error', rc=None.
      - command returns 0 → verdict='passed'.
      - command returns non-zero → verdict='failed'.
    """
    start = time.time()

    if command is None or not command.strip():
        return VerifyResult(
            verdict="no_verifier",
            command="",
            returncode=None,
            stdout="",
            stderr="",
            duration_seconds=0.0,
            timed_out=False,
        )

    cmd = command.strip()
    try:
        proc = subprocess.run(
            ["/bin/sh", "-c", cmd],
            cwd=str(cwd),
            env=env,
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
            check=False,
        )
    except subprocess.TimeoutExpired as exc:
        return VerifyResult(
            verdict="timeout",
            command=cmd,
            returncode=None,
            stdout=_truncate(exc.stdout.decode("utf-8", "replace") if isinstance(exc.stdout, bytes) else (exc.stdout or "")),
            stderr=_truncate(exc.stderr.decode("utf-8", "replace") if isinstance(exc.stderr, bytes) else (exc.stderr or "")),
            duration_seconds=time.time() - start,
            timed_out=True,
        )
    except (OSError, ValueError) as exc:
        return VerifyResult(
            verdict="error",
            command=cmd,
            returncode=None,
            stdout="",
            stderr=str(exc),
            duration_seconds=time.time() - start,
            timed_out=False,
        )

    verdict: VerifyVerdict = "passed" if proc.returncode == 0 else "failed"
    return VerifyResult(
        verdict=verdict,
        command=cmd,
        returncode=proc.returncode,
        stdout=_truncate(proc.stdout or ""),
        stderr=_truncate(proc.stderr or ""),
        duration_seconds=time.time() - start,
        timed_out=False,
    )


class Verifier:
    """Thin wrapper that binds a worktree + timeout to ``run_verify``.

    Exists so the controller can hold a single object instead of plumbing
    cwd/timeout into every call site. Stateless beyond config.
    """

    def __init__(
        self,
        *,
        cwd: Path,
        timeout_seconds: int = DEFAULT_TIMEOUT_SECONDS,
        env: dict | None = None,
    ) -> None:
        self._cwd = Path(cwd)
        self._timeout_seconds = timeout_seconds
        self._env = env

    def run(self, command: str | None) -> VerifyResult:
        return run_verify(
            command,
            cwd=self._cwd,
            timeout_seconds=self._timeout_seconds,
            env=self._env,
        )
