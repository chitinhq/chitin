"""Goal-id generation for Mini sessions.

Format: <short-slug>-<8hex>
  - short-slug: first 4 words of goal, lowercased, [^a-z0-9] -> '-', collapsed, max 32 chars
  - 8hex: first 8 chars of sha1(goal_text + iso8601_timestamp)
"""

from __future__ import annotations

import datetime
import hashlib
import re

_SLUG_RE = re.compile(r"[^a-z0-9]+")


def slugify(goal: str, *, max_words: int = 4, max_len: int = 32) -> str:
    words = goal.strip().lower().split()[:max_words]
    slug = _SLUG_RE.sub("-", " ".join(words)).strip("-")
    return slug[:max_len].rstrip("-") or "goal"


def mint_goal_id(goal: str, *, now: datetime.datetime | None = None) -> str:
    if not goal or not goal.strip():
        raise ValueError("goal cannot be empty")
    ts = (now or datetime.datetime.now(datetime.timezone.utc)).isoformat()
    digest = hashlib.sha1(f"{goal}|{ts}".encode("utf-8")).hexdigest()[:8]
    return f"{slugify(goal)}-{digest}"
