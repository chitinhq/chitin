"""Initial prompt rendering for Mini sessions."""

from __future__ import annotations

import datetime
from pathlib import Path

TEMPLATES_DIR = Path(__file__).resolve().parent.parent / "templates"
INITIAL_PROMPT_FILE = TEMPLATES_DIR / "initial_prompt.md"


def render_initial_prompt(
    *,
    goal: str,
    goal_id: str,
    state_dir: Path,
    recovered: bool = False,
) -> str:
    template = INITIAL_PROMPT_FILE.read_text()
    rendered = template.format(
        goal=goal,
        goal_id=goal_id,
        state_dir=str(state_dir),
        status_path=str(state_dir / "status.json"),
    )
    if recovered:
        ts = datetime.datetime.now(datetime.timezone.utc).isoformat()
        rendered += f"\n\n(recovered at {ts})\n"
    return rendered
