"""Input lease protocol for Minnie goals.

Lock file: <state_dir>/input.lock
Format: {"holder": str, "acquired_at": float, "expires_at": float}

Acquire is O_EXCL; expired locks auto-clear on next acquire. Holder defaults
to $OCTI_OPERATOR or getpass.getuser().
"""

from __future__ import annotations

import errno
import getpass
import json
import os
import time
from dataclasses import dataclass
from pathlib import Path

LOCK_FILENAME = "input.lock"
DEFAULT_LEASE_SECONDS = 60
STALE_GRACE_SECONDS = 5


class LockHeldError(RuntimeError):
    def __init__(self, holder: str, expires_at: float):
        self.holder = holder
        self.expires_at = expires_at
        ttl = max(0, int(expires_at - time.time()))
        super().__init__(f"input lock held by {holder!r} for {ttl}s more")


def _resolve_holder(holder: str | None) -> str:
    if holder:
        return holder
    return os.environ.get("OCTI_OPERATOR") or getpass.getuser()


def _lock_path(state_dir: Path) -> Path:
    return state_dir / LOCK_FILENAME


def _cleanup_if_stale(lock_path: Path) -> None:
    try:
        data = json.loads(lock_path.read_text())
        if float(data.get("expires_at", 0)) < time.time() - STALE_GRACE_SECONDS:
            lock_path.unlink(missing_ok=True)
    except (FileNotFoundError, ValueError, json.JSONDecodeError):
        lock_path.unlink(missing_ok=True)


def acquire(
    state_dir: Path,
    *,
    holder: str | None = None,
    lease_seconds: int = DEFAULT_LEASE_SECONDS,
) -> dict:
    state_dir.mkdir(parents=True, exist_ok=True)
    lock_path = _lock_path(state_dir)
    holder_id = _resolve_holder(holder)

    for attempt in (1, 2):
        try:
            fd = os.open(str(lock_path), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        except OSError as e:
            if e.errno != errno.EEXIST:
                raise
            _cleanup_if_stale(lock_path)
            if attempt == 2:
                try:
                    existing = json.loads(lock_path.read_text())
                except (FileNotFoundError, ValueError):
                    continue
                raise LockHeldError(
                    str(existing.get("holder", "unknown")),
                    float(existing.get("expires_at", 0)),
                )
            continue
        now = time.time()
        payload = {
            "holder": holder_id,
            "acquired_at": now,
            "expires_at": now + lease_seconds,
        }
        try:
            os.write(fd, json.dumps(payload).encode("utf-8"))
        finally:
            os.close(fd)
        return payload
    raise LockHeldError(holder_id, time.time())


def release(state_dir: Path) -> None:
    _lock_path(state_dir).unlink(missing_ok=True)


@dataclass
class Lease:
    state_dir: Path
    holder: str | None = None
    lease_seconds: int = DEFAULT_LEASE_SECONDS

    def __enter__(self) -> dict:
        return acquire(
            self.state_dir,
            holder=self.holder,
            lease_seconds=self.lease_seconds,
        )

    def __exit__(self, *exc) -> None:
        release(self.state_dir)
