"""Pre-canned investigation recipe for the swarm-dispatched analyst role.

Maps an alarm string from the swarm-rollup onto the right analysis
loaders + extracts a deterministic root-cause + recommended-action.
The agent's job collapses to:

    python3 -m analysis.investigate --entry <id> --alarm "<text>"

…and reading the JSON sidecar to emit the analyst prompt's
`<<<ANALYSIS>>>` line. No LLM judgment in the analysis path —
same alarm + same data always produce the same finding. That's the
determinism-first model: the recipe owns the analysis, the agent
just reports.

Why this is a recipe (not the agent's free-form Python):
  - Reproducible: re-running on yesterday's chain produces identical
    output. Caught regressions are caught the same way every time.
  - Reviewable: the recipe's logic is in tests, not buried in an
    LLM prompt.
  - Cheap: no token cost for the analysis itself (only for the
    agent reading + reporting).
  - Auditable: any swarm investigation lands a markdown report under
    python/analysis/out/<entry-id>.md the operator can grep.

v1 alarm kinds (extend by adding to ALARM_HANDLERS):
  - BUCKET-B REGRESSION: ... → bucket-b investigator
  - LOW SUCCESS: driver=X NN% → success-rate investigator
  - LOW SUCCESS: tier=TX NN% → tier success-rate investigator
  - QWEN IDLE: ... → local-qwen routing investigator
  - (default fallback) → unknown-alarm investigator (writes a
    summary report + recommends needs_human)
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable


# ─── Result schema ─────────────────────────────────────────────────────────


@dataclass(frozen=True)
class Finding:
    """The structured output of an investigation. Mirrors the analyst
    prompt's `<<<ANALYSIS>>>` JSON contract verbatim — the agent
    reads this from the JSON sidecar and emits it as the line."""

    root_cause: str
    recommended_action: str  # 'file-fix-entry' | 'needs_human' | 'no-action'
    report_path: str
    confidence: str  # 'high' | 'medium' | 'low'


@dataclass(frozen=True)
class InvestigationContext:
    entry_id: str
    alarm: str
    out_dir: Path
    """Directory for the markdown report. Default: python/analysis/out/."""


# ─── Alarm classification ──────────────────────────────────────────────────


def classify_alarm(alarm: str) -> str:
    """Return the canonical kind of an alarm, or 'unknown'.

    The kind is the leading uppercase phrase before the first colon,
    normalized — same as the alarm-feeder's signature. Two
    independent code paths derive the same kind so dedup works
    end-to-end (alarm-feeder files an entry by kind, investigator
    matches the same kind).
    """
    head = alarm.split(":", 1)[0].strip().lower()
    return re.sub(r"[^a-z0-9]+", "-", head).strip("-") or "unknown"


# ─── Per-alarm investigators ───────────────────────────────────────────────


def investigate_bucket_b_regression(ctx: InvestigationContext) -> Finding:
    """Bucket-B = PR title-vs-diff mismatch (a swarm worker pushed work
    that doesn't match the entry's declared file: scope). PR #123
    fixed the auto-commit path that caused 4 PRs of contamination on
    2026-05-02; rate > 0% means a regression of that fix."""
    health = _latest_health()
    bb_count = health.get("bucket_b_count", 0)
    bb_rate = health.get("bucket_b_rate", 0.0)
    write_markdown(
        ctx.out_dir / f"{ctx.entry_id}.md",
        title=f"Investigation: bucket-B regression",
        sections=[
            ("Alarm", ctx.alarm),
            (
                "Current rate",
                f"{bb_count} contaminated runs out of {health.get('total_runs', 0)} "
                f"(rate {bb_rate:.1%}) in the last rollup window.",
            ),
            (
                "Likely cause",
                "PR #123's apply-step revertWorktreeSettingsArtifact may have regressed, OR "
                "writeWorktreeClaudeSettings is being invoked at an unexpected lifecycle "
                "point. Inspect recent dispatcher / activity / apply-step diffs vs PR #123.",
            ),
            (
                "Recommended next step",
                "File a fix-shape backlog entry against `apps/temporal-worker/src/` to "
                "re-verify the apply-step revert. Operator should manually merge the next "
                "swarm PR until rate returns to 0%.",
            ),
        ],
    )
    return Finding(
        root_cause=(
            f"Bucket-B contamination rate {bb_rate:.1%} ({bb_count} runs) — "
            "apply-step revert may have regressed since PR #123."
        ),
        recommended_action="file-fix-entry",
        report_path=str(ctx.out_dir / f"{ctx.entry_id}.md"),
        confidence="high" if bb_count >= 2 else "medium",
    )


def investigate_low_success(ctx: InvestigationContext) -> Finding:
    """LOW SUCCESS alarms cite either a driver or a tier with success
    rate < threshold. The right action depends on whether the dip is
    transient (<5 runs) or sustained."""
    m = re.search(r"(driver|tier)=([A-Za-z0-9-]+).*?(\d+)%\s*\((\d+)/(\d+)\)", ctx.alarm)
    if not m:
        return _fallback_finding(
            ctx,
            reason="LOW SUCCESS alarm did not match expected shape",
        )
    dim, dim_value, _pct, success_n, total_n = m.group(1), m.group(2), m.group(3), int(m.group(4)), int(m.group(5))
    confidence = "high" if total_n >= 10 else "medium" if total_n >= 5 else "low"
    health = _latest_health()
    write_markdown(
        ctx.out_dir / f"{ctx.entry_id}.md",
        title=f"Investigation: low success rate ({dim}={dim_value})",
        sections=[
            ("Alarm", ctx.alarm),
            (
                "Sample size",
                f"{success_n}/{total_n} runs in the last window. "
                f"Confidence: {confidence} (≥10 runs = high, 5–9 = medium, <5 = low).",
            ),
            (
                "Cohort breakdown",
                json.dumps(
                    health.get(f"success_by_{dim}", {}).get(dim_value, {}),
                    indent=2,
                ),
            ),
            (
                "Likely cause categories",
                "1. Wall-timeout / SIGKILL on the agent (slow model);\n"
                "2. Apply-step push failing (worktree dirty, branch protected);\n"
                "3. Driver-specific regression (model API change, prompt drift);\n"
                "4. Backlog entries got harder (T3+ work skewing recent dispatches).",
            ),
            (
                "Recommended next step",
                "If confidence high: file a fix-shape entry against the failing dimension's "
                "code path (driver wrapper or tier-specific prompt). If low: needs_human — "
                "operator decides whether to re-run a few entries and observe.",
            ),
        ],
    )
    return Finding(
        root_cause=(
            f"{dim}={dim_value} success rate is {success_n}/{total_n} — "
            "regression localized to that path."
        ),
        recommended_action="file-fix-entry" if confidence == "high" else "needs_human",
        report_path=str(ctx.out_dir / f"{ctx.entry_id}.md"),
        confidence=confidence,
    )


def investigate_qwen_idle(ctx: InvestigationContext) -> Finding:
    """QWEN IDLE = T0 traffic routed to copilot instead of local-qwen.
    This is operator-mediated (the dispatcher's TIER_DRIVER map was
    flipped after slice 7-tuning surfaced qwen3-coder instability).
    Flipping back is gated on the qwen-stream regression being fixed."""
    write_markdown(
        ctx.out_dir / f"{ctx.entry_id}.md",
        title="Investigation: qwen3 (3090) idle",
        sections=[
            ("Alarm", ctx.alarm),
            (
                "Current state",
                "T0 routing in dispatcher.ts is `'copilot'`, not `'local-qwen'`. "
                "Comment in the file documents the temp routing decision after "
                "slice 7-tuning surfaced qwen3-coder ollama-stream instability + "
                "scope drift. The 3090 sits idle until the upstream issue is "
                "resolved.",
            ),
            (
                "Recommended next step",
                "needs_human — operator decides whether to flip T0 back to "
                "local-qwen now (and tolerate flakiness) or wait for the "
                "qwen-ollama-stream-instability backlog entry to ship a fix.",
            ),
        ],
    )
    return Finding(
        root_cause=(
            "T0 routing flipped to copilot pending qwen-ollama-stream-instability fix; "
            "3090 idle by design until that lands."
        ),
        recommended_action="needs_human",
        report_path=str(ctx.out_dir / f"{ctx.entry_id}.md"),
        confidence="high",
    )


def _fallback_finding(ctx: InvestigationContext, *, reason: str) -> Finding:
    """Default investigator for alarm kinds without a dedicated handler.
    Writes a summary report so the operator has a starting point."""
    write_markdown(
        ctx.out_dir / f"{ctx.entry_id}.md",
        title=f"Investigation: unknown alarm kind",
        sections=[
            ("Alarm", ctx.alarm),
            ("Reason", reason),
            (
                "Recommended next step",
                "needs_human — operator should classify this alarm kind and add a dedicated "
                "investigator to `analysis/investigate.py` so future occurrences land "
                "automatically.",
            ),
        ],
    )
    return Finding(
        root_cause=f"unknown alarm kind ({reason})",
        recommended_action="needs_human",
        report_path=str(ctx.out_dir / f"{ctx.entry_id}.md"),
        confidence="low",
    )


# ─── Routing ──────────────────────────────────────────────────────────────


ALARM_HANDLERS: dict[str, Callable[[InvestigationContext], Finding]] = {
    "bucket-b-regression": investigate_bucket_b_regression,
    "low-success": investigate_low_success,
    "qwen-idle": investigate_qwen_idle,
}


def investigate(ctx: InvestigationContext) -> Finding:
    """Dispatch on alarm kind. Unknown kinds get the fallback."""
    kind = classify_alarm(ctx.alarm)
    handler = ALARM_HANDLERS.get(kind)
    if handler is None:
        return _fallback_finding(ctx, reason=f"no handler for kind '{kind}'")
    return handler(ctx)


# ─── Helpers ──────────────────────────────────────────────────────────────


def _latest_health(rollup_dir: Path | None = None) -> dict:
    """Read the most-recent rollup JSON. Same source the gatekeeper
    consults — keeps the investigator and the gate seeing identical
    numbers (no recompute drift). Returns {} when the rollup dir is
    absent or unreadable; callers MUST tolerate empty data."""
    if rollup_dir is None:
        rollup_dir = Path(os.path.expanduser("~/.cache/chitin/swarm-rollups"))
    try:
        names = sorted(p.name for p in rollup_dir.iterdir() if p.suffix == ".json")
    except (FileNotFoundError, NotADirectoryError):
        return {}
    if not names:
        return {}
    try:
        return json.loads((rollup_dir / names[-1]).read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError):
        return {}


def write_markdown(path: Path, *, title: str, sections: list[tuple[str, str]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    parts = [f"# {title}", ""]
    parts.append(f"_Generated by `analysis.investigate` at {datetime.now(tz=timezone.utc).isoformat()}._")
    parts.append("")
    for heading, body in sections:
        parts.append(f"## {heading}")
        parts.append("")
        parts.append(body)
        parts.append("")
    path.write_text("\n".join(parts), encoding="utf-8")


# ─── CLI ──────────────────────────────────────────────────────────────────


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="analysis.investigate")
    p.add_argument("--entry", required=True, help="Backlog entry id (used as the report filename slug).")
    p.add_argument("--alarm", required=True, help="The alarm string verbatim from the rollup.")
    p.add_argument(
        "--out-dir",
        default=str(Path(__file__).parent / "out"),
        help="Directory for the markdown report. Default: python/analysis/out/.",
    )
    p.add_argument(
        "--json-sidecar",
        action="store_true",
        default=True,
        help="Also write {out-dir}/{entry-id}.json with the structured Finding (default on).",
    )
    return p.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    ctx = InvestigationContext(
        entry_id=args.entry,
        alarm=args.alarm,
        out_dir=Path(args.out_dir),
    )
    finding = investigate(ctx)
    sidecar = ctx.out_dir / f"{ctx.entry_id}.json"
    sidecar.parent.mkdir(parents=True, exist_ok=True)
    sidecar.write_text(json.dumps(asdict(finding), indent=2) + "\n", encoding="utf-8")
    # Emit the agent-facing line on stdout so the analyst prompt's
    # `<<<ANALYSIS>>>` extract works without re-reading the file.
    print(f"<<<ANALYSIS>>>{json.dumps(asdict(finding))}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
