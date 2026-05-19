"""Transcript capture via polling kitty @ get-text.

Appends new tail to <state_dir>/transcript.log.
"""

from __future__ import annotations

import hashlib
from pathlib import Path


def append_new_tail(transcript_path: Path, new_screen_text: str, *, overlap_bytes: int = 4096) -> int:
    """Compute the non-overlapping suffix of new_screen_text vs the tail of transcript.log,
    append it, return the number of bytes appended.
    """
    transcript_path.parent.mkdir(parents=True, exist_ok=True)
    transcript_path.touch(exist_ok=True)
    prev_tail = ""
    if transcript_path.stat().st_size > 0:
        with transcript_path.open("rb") as f:
            f.seek(max(0, transcript_path.stat().st_size - overlap_bytes))
            prev_tail = f.read().decode("utf-8", "replace")
    # Find longest suffix of prev_tail that is a prefix of new_screen_text
    max_overlap = min(len(prev_tail), len(new_screen_text))
    overlap = 0
    for i in range(max_overlap, 0, -1):
        if prev_tail.endswith(new_screen_text[:i]):
            overlap = i
            break
    suffix = new_screen_text[overlap:]
    if not suffix:
        return 0
    with transcript_path.open("ab") as f:
        return f.write(suffix.encode("utf-8"))


def hash_text(s: str) -> str:
    return hashlib.sha1(s.encode("utf-8")).hexdigest()
