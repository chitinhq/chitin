"""Soul resolution and loading for Mini sessions.

Reuses the same soul files and resolution logic from the dispatch pipeline
(_pick_driver.py), but adapted for the Mini session lifecycle:

- ``resolve_soul_for_session(soul_id)`` finds the soul file, reads it,
  computes its hash, and returns a SoulConfig.
- ``DEFAULT_AGENT_SOULS`` maps agent names to their default soul.
- ``render_soul_section(soul_config)`` produces the markdown to inject
  into Mini's initial prompt.
"""

from __future__ import annotations

import hashlib
import os
from dataclasses import dataclass
from pathlib import Path


# Per-agent default souls. Override via MINI_DEFAULT_SOULS env (JSON).
DEFAULT_AGENT_SOULS: dict[str, str] = {
    "mini": "knuth",
    "icarus": "turing",
    "clawta": "curie",
    "ares": "sun-tzu",
}


def _load_default_soul_map() -> dict[str, str]:
    """Load the agent→soul map from MINI_DEFAULT_SOULS env or defaults."""
    raw = os.environ.get("MINI_DEFAULT_SOULS", "").strip()
    if not raw:
        return dict(DEFAULT_AGENT_SOULS)
    import json

    try:
        parsed = json.loads(raw)
        if isinstance(parsed, dict):
            return {str(k): str(v) for k, v in parsed.items()}
    except json.JSONDecodeError:
        pass
    return dict(DEFAULT_AGENT_SOULS)


def souls_dir_candidates() -> list[Path]:
    """Ordered list of directories that may hold ``<soul_id>.md`` files.

    Mirrors the logic from _pick_driver.py:
    - ``CHITIN_SOULS_DIR`` env wins (supports flat and canonical/experimental layouts)
    - Falls back to the repo's ``souls/canonical`` and ``souls/experimental`` dirs
    - Also checks ``~/workspace/soulforge/souls`` (the promoted souls repo)
    """
    candidates: list[Path] = []
    override = os.environ.get("CHITIN_SOULS_DIR", "").strip()
    if override:
        base = Path(override).expanduser()
        candidates += [base, base / "canonical", base / "experimental"]

    # Repo checkout (honors CHITIN_REPO).
    repo = os.environ.get("CHITIN_REPO", "").strip()
    if repo:
        root = Path(repo).expanduser()
    else:
        # Walk up from this file to find the repo root.
        root = Path(__file__).resolve().parents[3]  # swarm/mini/_internal → chitin/
    candidates += [
        root / "souls" / "canonical",
        root / "souls" / "experimental",
    ]

    # Soulforge repo (canonical promoted souls).
    soulforge = Path.home() / "workspace" / "soulforge" / "souls"
    if soulforge.is_dir() and soulforge not in candidates:
        candidates.append(soulforge)

    # De-duplicate while preserving order.
    seen: set[Path] = set()
    ordered: list[Path] = []
    for p in candidates:
        if p not in seen:
            seen.add(p)
            ordered.append(p)
    return ordered


@dataclass(frozen=True)
class SoulConfig:
    """Resolved soul configuration for a session."""

    soul_id: str
    soul_hash: str
    soul_body: str  # Full markdown content of the soul file

    @property
    def is_empty(self) -> bool:
        return not self.soul_body.strip()


EMPTY_SOUL = SoulConfig(soul_id="", soul_hash="", soul_body="")


def find_soul_file(soul_id: str) -> Path | None:
    """Return the path to ``<soul_id>.md`` if it can be located, else None."""
    for base in souls_dir_candidates():
        candidate = base / f"{soul_id}.md"
        if candidate.is_file():
            return candidate
    return None


def resolve_soul_for_session(
    soul_id: str | None = None,
    *,
    agent: str | None = None,
) -> SoulConfig:
    """Resolve a soul for a Mini session.

    Priority:
      1. Explicit soul_id (from --soul flag)
      2. Agent default (from MINI_DEFAULT_SOULS or DEFAULT_AGENT_SOULS)
      3. No soul (EMPTY_SOUL)

    Returns SoulConfig with the full body text and hash. If the soul
    file cannot be found, returns EMPTY_SOUL (dispatch can still proceed;
    the fingerprint is simply unstamped).
    """
    if soul_id is None and agent is not None:
        soul_id = _load_default_soul_map().get(agent)

    if not soul_id:
        return EMPTY_SOUL

    soul_path = find_soul_file(soul_id)
    if soul_path is None:
        # Soul ID was requested but file not found — return with empty
        # body so dispatch can proceed but fingerprint is incomplete.
        return SoulConfig(soul_id=soul_id, soul_hash="", soul_body="")

    try:
        content = soul_path.read_text(encoding="utf-8")
    except OSError:
        return SoulConfig(soul_id=soul_id, soul_hash="", soul_body="")

    soul_hash = hashlib.sha256(content.encode("utf-8")).hexdigest()
    return SoulConfig(soul_id=soul_id, soul_hash=soul_hash, soul_body=content)


def render_soul_section(soul_config: SoulConfig) -> str:
    """Render the soul body as a markdown section for injection into
    the Mini initial prompt.

    Returns an empty string if the soul is empty (no section added).
    The section is clearly delimited so the downstream agent knows
    it's a cognitive lens, not a persona.
    """
    if soul_config.is_empty:
        return ""

    return f"""

---

## Active Soul: {soul_config.soul_id}

*You are operating with the {soul_config.soul_id} cognitive lens. This lens
shapes how you approach the work — it is not a persona, voice, or costume.
Apply the heuristics below to your reasoning. If you catch yourself performing
the character rather than using the method, stop and reset.*

{soul_config.soul_body}

---
"""