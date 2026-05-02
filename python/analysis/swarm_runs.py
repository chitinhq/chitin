"""Load swarm dispatcher runs from marker + envelope files.

Joins ``~/.cache/chitin/swarm-state/dispatched/<entry-id>.json`` (the
dispatcher's persistent marker) with ``<repo>/tmp/result-swarm-<wfid>.json``
(the workflow's result envelope) by ``workflow_id``. Output is a flat
``SwarmRun`` dataclass per (entry × workflow) pair.

Envelope schema (per ``apps/temporal-worker/src/dispatcher.ts`` + the
``ActivityResult`` interface in ``activity-types.ts``):

.. code-block::

    {
      "workflow_id": "swarm-...",
      "pr_title": "...",
      "pr_body": "...",
      "result": {
        "exit_code": 0,
        "stdout_tail": "...",       # capped at 2000 bytes
        "stderr_tail": "...",
        "duration_ms": 111599,
        "worktree": {
          "path": "...",
          "branch": "...",
          "head_sha": "...",
          "commits_added": 0,
          "has_uncommitted_changes": true,
          "diff_shortstat": "1 file changed, 12 insertions(+), 10 deletions(-)"
        }
      }
    }

Marker schema:

.. code-block::

    {
      "entry_id": "...",
      "workflow_id": "swarm-...",
      "tier": "T0|T1|T2|T3|T4",
      "driver": "copilot|claude-code-headless|local-qwen|...",
      "dispatched_at": "2026-05-02T03:54:06.332Z"
    }

Cost / model are parsed out of ``stdout_tail`` for ``claude-code-headless``
runs (the ``claude`` CLI emits a final JSON summary including
``"total_cost_usd"`` and ``"modelUsage"``). ``copilot`` runs report no
per-run cost — the field stays ``None``.
"""
from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Optional

from analysis.loaders import Window


@dataclass(frozen=True)
class SwarmRun:
    """One dispatcher tick's outcome — marker × envelope."""

    entry_id: str
    tier: str
    driver: str
    dispatched_at: datetime
    exit_code: int
    duration_ms: int
    commits_added: int
    diff_shortstat: str
    pr_url: Optional[str]
    cost_usd: Optional[float]
    model: Optional[str]
    bucket_b: bool


# claude-code-headless's stream-json final-message tail carries the cost +
# modelUsage block. These regexes are tolerant of pretty-printed JSON
# (whitespace between key and value) but anchor on the field name so they
# don't false-match on entry descriptions that mention "cost" generically.
_COST_RE = re.compile(r'"total_cost_usd"\s*:\s*([0-9.]+)')
_MODEL_USAGE_RE = re.compile(r'"modelUsage"\s*:\s*\{\s*"([^"]+)"')
_PR_URL_RE = re.compile(r'https://github\.com/[\w.-]+/[\w.-]+/pull/\d+')

# Bucket-B signature: the apply-step revert (PR #123) should make this
# rate 0 going forward. Pre-fix runs had a byte-identical
# `.claude/settings.json` overwrite — diff_shortstat exactly
# "1 file changed, 12 insertions(+), 10 deletions(-)" with
# commits_added=1. The structural form is preferred (modified-set ==
# {.claude/settings.json} AND type=M) but that requires git inspection
# of the branch. The shortstat shape is a workable proxy in the marker
# + envelope window. Conservative — false-negative-leaning, since a
# legitimate small PR could match the shortstat. Pair with file-list
# inspection when escalating to an alarm.
_BUCKET_B_SHORTSTAT = "1 file changed, 12 insertions(+), 10 deletions(-)"


def _parse_iso(s: str) -> datetime:
    """ISO-8601 parser tolerant of the 'Z' suffix Node emits."""
    return datetime.fromisoformat(s.replace("Z", "+00:00"))


def _extract_pr_url(stdout_tail: str) -> Optional[str]:
    m = _PR_URL_RE.search(stdout_tail)
    return m.group(0) if m else None


def _extract_cost_usd(stdout_tail: str) -> Optional[float]:
    m = _COST_RE.search(stdout_tail)
    if not m:
        return None
    try:
        return float(m.group(1))
    except ValueError:
        return None


def _extract_model(stdout_tail: str) -> Optional[str]:
    """Pull the first model id out of the modelUsage block."""
    m = _MODEL_USAGE_RE.search(stdout_tail)
    return m.group(1) if m else None


def _is_bucket_b(diff_shortstat: str, commits_added: int) -> bool:
    """Conservative bucket-B detector — see module docstring + PR #124.

    Returns True iff the run's diff matches the byte-identical signature
    of `writeWorktreeClaudeSettings`'s overwrite. Pair with file-list
    inspection at the alarm layer; a daily-rollup that fires on this
    signal alone will have false positives on small legitimate PRs.
    """
    return diff_shortstat == _BUCKET_B_SHORTSTAT and commits_added == 1


def load_swarm_runs(state_dir: Path, tmp_dir: Path, window: Optional[Window]) -> list[SwarmRun]:
    """Load all swarm runs in ``window`` (or all-time if ``window`` is None).

    Mirrors ``loaders.load_gov_decisions`` semantics: missing input dirs
    return an empty list rather than raising. Markers without matching
    envelopes are skipped (envelope is the source of truth for outcome).
    """
    state_dir = Path(state_dir)
    tmp_dir = Path(tmp_dir)

    markers_by_wfid: dict[str, dict] = {}
    if state_dir.exists():
        for path in sorted(state_dir.iterdir()):
            if path.suffix != ".json" or not path.is_file():
                continue
            try:
                marker = json.loads(path.read_text())
            except (json.JSONDecodeError, OSError):
                continue
            wfid = marker.get("workflow_id")
            if wfid:
                markers_by_wfid[wfid] = marker

    runs: list[SwarmRun] = []
    if not tmp_dir.exists():
        return runs

    for path in sorted(tmp_dir.iterdir()):
        if not (path.name.startswith("result-swarm-") and path.suffix == ".json" and path.is_file()):
            continue
        try:
            envelope = json.loads(path.read_text())
        except (json.JSONDecodeError, OSError):
            continue

        wfid = envelope.get("workflow_id")
        marker = markers_by_wfid.get(wfid) if wfid else None
        if not marker:
            continue
        if "dispatched_at" not in marker:
            continue
        try:
            dispatched_at = _parse_iso(marker["dispatched_at"])
        except (TypeError, ValueError):
            continue

        if window is not None and not (window.since <= dispatched_at < window.until):
            continue

        result = envelope.get("result") or {}
        worktree = result.get("worktree") or {}
        stdout_tail = result.get("stdout_tail") or ""
        diff_shortstat = worktree.get("diff_shortstat", "") or ""
        commits_added = worktree.get("commits_added", 0) or 0

        runs.append(
            SwarmRun(
                entry_id=marker.get("entry_id", ""),
                tier=marker.get("tier", ""),
                driver=marker.get("driver", ""),
                dispatched_at=dispatched_at,
                exit_code=result.get("exit_code", -1),
                duration_ms=result.get("duration_ms", 0),
                commits_added=commits_added,
                diff_shortstat=diff_shortstat,
                pr_url=_extract_pr_url(stdout_tail),
                cost_usd=_extract_cost_usd(stdout_tail),
                model=_extract_model(stdout_tail),
                bucket_b=_is_bucket_b(diff_shortstat, commits_added),
            )
        )
    return runs


def cost_by_driver(runs: list[SwarmRun]) -> dict[str, float]:
    """Sum of `cost_usd` per driver. Drivers with no per-run cost (e.g.
    ``copilot`` under the Pro plan) report 0.0."""
    totals: dict[str, float] = {}
    for run in runs:
        if not run.driver:
            continue
        totals.setdefault(run.driver, 0.0)
        if run.cost_usd is not None:
            totals[run.driver] += run.cost_usd
    return totals


def outcomes_by_driver(runs: list[SwarmRun]) -> dict[str, dict[str, int]]:
    """Per-driver breakdown across exit-code categories.

    Keys: ``success`` (exit_code 0), ``partial`` (exit_code 1 — agent
    declined or fell short), ``timeout`` (exit_code -1 — SIGKILL),
    ``other`` (any other non-zero).
    """
    out: dict[str, dict[str, int]] = {}
    for run in runs:
        if not run.driver:
            continue
        bucket = out.setdefault(run.driver, {"success": 0, "partial": 0, "timeout": 0, "other": 0})
        if run.exit_code == 0:
            bucket["success"] += 1
        elif run.exit_code == 1:
            bucket["partial"] += 1
        elif run.exit_code == -1:
            bucket["timeout"] += 1
        else:
            bucket["other"] += 1
    return out


def bucket_b_rate(runs: list[SwarmRun]) -> float:
    """Fraction of runs that match the bucket-B signature.

    Post-PR #123 this should be 0.0. Any non-zero rate is a regression
    signal — the apply-step revert silently failed to fire on those
    runs.
    """
    if not runs:
        return 0.0
    return sum(1 for r in runs if r.bucket_b) / len(runs)
