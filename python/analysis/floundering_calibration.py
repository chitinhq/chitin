"""Calibrate the kernel's floundering heuristic from real session data.

The kernel's router/advisor only fires when a heuristic flags an action.
For the always-T0 + escalation-loop architecture, T0 silent failures
must trigger the floundering heuristic so the advisor can either
nudge the agent (continue + nudge) or escalate it (takeover + escalate).

Today's `chitin.yaml` has GLOBAL floundering thresholds:
    max_loop_count: 3        # 3 identical tool+target calls
    max_stall_seconds: 600   # 10 min without a write

Live test 2026-05-05: glm-flash at T0 ran 172s on a 4-LOC task,
exited cleanly with 0 commits. Floundering never fired (loop required
3 IDENTICAL calls; stall required 600s — neither tripped in 172s).
Result: silent failure with no advisor engagement.

This script computes per-driver thresholds from the actual chain
event distributions so the operator can drop calibrated values into
chitin.yaml. Drivers map to tiers via the well-known
TIER_DRIVER_DEFAULTS in apps/runner/src/dispatcher.ts:
    T0/T1 → openclaw-glm-flash
    T2    → copilot
    T3    → openclaw-glm-cloud
    T4    → claude-code-headless

Usage:
    cd python/analysis && uv run python -m analysis.floundering_calibration

Reads:
    ~/.chitin/events-<session_id>.jsonl  (per-session decision chain)

Writes:
    out/floundering-calibration-<DATE>.md  (operator-readable)
    out/floundering-calibration-<DATE>.json (machine-readable for CI)
"""
from __future__ import annotations

import argparse
import json
import math
import os
from collections import defaultdict
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

CHITIN_HOME = Path(os.environ.get("CHITIN_HOME") or os.path.expanduser("~/.chitin"))
OUT_DIR = Path(__file__).parent / "out"

# Heuristic input: the floundering heuristic considers an action a
# "write" if its action_type is one of these. Mirrors the kernel's
# detectStall in go/execution-kernel/internal/router/floundering.go.
WRITE_ACTION_TYPES = {"file.write", "git.commit", "git.push"}

# Driver-to-tier reverse map. The kernel doesn't tag tier on chain
# events directly today; we infer from agent_instance_id / driver
# fields. When neither is decisive, drop the session into 'unknown'.
DRIVER_TIER = {
    "openclaw-glm-flash": "T0/T1",
    "openclaw-glm-cloud": "T3",
    "copilot": "T2",
    "claude-code-headless": "T4",
    "qwen-agent": "T0/T1",  # legacy id from the chain — qwen3-coder via openclaw
    "glm-flash-agent": "T0/T1",
}


@dataclass
class SessionMetrics:
    session_id: str
    driver: str
    tier: str
    decision_count: int = 0
    write_count: int = 0
    duration_s: float = 0.0
    max_consecutive_same: int = 0  # longest run of identical tool+target
    max_stall_s: float = 0.0       # longest gap between writes
    first_ts: str = ""
    last_ts: str = ""
    succeeded: bool = False        # at least one write happened


def parse_ts(s: str) -> datetime:
    return datetime.fromisoformat(s.replace("Z", "+00:00"))


def load_session(path: Path) -> SessionMetrics | None:
    """Walk one session's events.jsonl, compute SessionMetrics."""
    decisions: list[dict] = []
    driver = ""
    with path.open() as f:
        for line in f:
            try:
                ev = json.loads(line)
            except json.JSONDecodeError:
                continue
            if ev.get("event_type") != "decision":
                continue
            payload = ev.get("payload", {}) or {}
            tool = payload.get("tool_name") or ""
            target = payload.get("action_target") or ""
            if not tool or not target:
                continue
            decisions.append({
                "ts": ev.get("ts", ""),
                "tool": tool,
                "target": target,
                "action_type": payload.get("action_type", ""),
                "decision": payload.get("decision", ""),
            })
            if not driver:
                driver = ev.get("agent_instance_id", "") or ""

    if not decisions:
        return None

    # Loop detection — longest run of consecutive identical (tool, target).
    max_consec = 1
    cur_consec = 1
    for i in range(1, len(decisions)):
        prev = decisions[i - 1]
        cur = decisions[i]
        if prev["tool"] == cur["tool"] and prev["target"] == cur["target"]:
            cur_consec += 1
            if cur_consec > max_consec:
                max_consec = cur_consec
        else:
            cur_consec = 1

    # Stall detection — longest gap between consecutive write actions.
    write_ts = [parse_ts(d["ts"]) for d in decisions
                if d["action_type"] in WRITE_ACTION_TYPES and d["decision"] == "allow"]
    if len(write_ts) >= 2:
        gaps = [(write_ts[i] - write_ts[i - 1]).total_seconds() for i in range(1, len(write_ts))]
        max_stall = max(gaps) if gaps else 0.0
    elif len(write_ts) == 1:
        # One write — measure from first decision to that write
        max_stall = (write_ts[0] - parse_ts(decisions[0]["ts"])).total_seconds()
    else:
        # No writes — full session duration is "stalled"
        max_stall = (parse_ts(decisions[-1]["ts"]) - parse_ts(decisions[0]["ts"])).total_seconds()

    duration = (parse_ts(decisions[-1]["ts"]) - parse_ts(decisions[0]["ts"])).total_seconds()
    tier = DRIVER_TIER.get(driver, "unknown")

    return SessionMetrics(
        session_id=path.stem.replace("events-", ""),
        driver=driver or "unknown",
        tier=tier,
        decision_count=len(decisions),
        write_count=len(write_ts),
        duration_s=duration,
        max_consecutive_same=max_consec,
        max_stall_s=max_stall,
        first_ts=decisions[0]["ts"],
        last_ts=decisions[-1]["ts"],
        succeeded=len(write_ts) > 0,
    )


def percentile(xs: list[float], p: float) -> float:
    """p in [0, 100]. Returns 0.0 for empty list."""
    if not xs:
        return 0.0
    s = sorted(xs)
    k = (len(s) - 1) * (p / 100.0)
    f = math.floor(k)
    c = math.ceil(k)
    if f == c:
        return s[int(k)]
    return s[f] + (s[c] - s[f]) * (k - f)


@dataclass
class TierThresholds:
    tier: str
    n_sessions: int
    n_succeeded: int
    n_failed: int
    # Suggested thresholds, derived from successful runs:
    # - max_stall_seconds = 75th percentile of max_stall among successful runs.
    #   Above this, the agent is genuinely outside normal-execution territory.
    # - max_loop_count = 90th percentile of max_consecutive_same among
    #   successful runs. Above this, the agent is repeating itself harder
    #   than even the longest legitimate retry sequences.
    suggested_max_stall_s: int = 0
    suggested_max_loop_count: int = 0
    # Also surface failure-mode stats so the operator can sanity-check
    # whether the suggested thresholds would have caught past failures.
    failed_max_stall_s_p50: float = 0.0
    failed_max_loop_count_p50: float = 0.0


def calibrate(sessions: list[SessionMetrics]) -> dict[str, TierThresholds]:
    by_tier: dict[str, list[SessionMetrics]] = defaultdict(list)
    for s in sessions:
        by_tier[s.tier].append(s)

    out: dict[str, TierThresholds] = {}
    for tier, sess_list in sorted(by_tier.items()):
        succ = [s for s in sess_list if s.succeeded]
        fail = [s for s in sess_list if not s.succeeded]
        thresh = TierThresholds(
            tier=tier,
            n_sessions=len(sess_list),
            n_succeeded=len(succ),
            n_failed=len(fail),
        )
        if succ:
            thresh.suggested_max_stall_s = int(percentile([s.max_stall_s for s in succ], 75))
            thresh.suggested_max_loop_count = max(2, int(percentile([s.max_consecutive_same for s in succ], 90)))
        if fail:
            thresh.failed_max_stall_s_p50 = percentile([s.max_stall_s for s in fail], 50)
            thresh.failed_max_loop_count_p50 = percentile([s.max_consecutive_same for s in fail], 50)
        out[tier] = thresh
    return out


def render_markdown(sessions: list[SessionMetrics], thresholds: dict[str, TierThresholds], when: datetime) -> str:
    lines = [
        f"# Floundering calibration — {when.date().isoformat()}",
        "",
        f"Source: `~/.chitin/events-*.jsonl` ({len(sessions)} sessions parsed).",
        "",
        "## Suggested per-tier thresholds",
        "",
        "Drop these into `chitin.yaml` once tier-aware floundering config",
        "lands in the kernel (today the config is global; this calibration",
        "informs both the global tuning and the future per-tier shape).",
        "",
        "| tier | n_sessions | succeeded | failed | suggested max_stall_seconds | suggested max_loop_count | failed-run p50 stall_s | failed-run p50 loop_count |",
        "|---|---|---|---|---|---|---|---|",
    ]
    for tier in sorted(thresholds):
        t = thresholds[tier]
        lines.append(
            f"| {t.tier} | {t.n_sessions} | {t.n_succeeded} | {t.n_failed} "
            f"| {t.suggested_max_stall_s} | {t.suggested_max_loop_count} "
            f"| {t.failed_max_stall_s_p50:.0f} | {t.failed_max_loop_count_p50:.0f} |"
        )
    lines.append("")
    lines.append("## How to read this")
    lines.append("")
    lines.append(
        "`suggested max_stall_seconds` = 75th percentile of the longest stall "
        "between writes among SUCCESSFUL sessions at that tier. The advisor "
        "should engage when a session exceeds this — by p75, normal "
        "successful runs have already crossed their hardest stall point."
    )
    lines.append("")
    lines.append(
        "`suggested max_loop_count` = 90th percentile of the longest "
        "consecutive identical (tool, target) sequence among successful "
        "sessions. Above this, the agent is repeating itself harder than "
        "even the longest legitimate retry loops."
    )
    lines.append("")
    lines.append(
        "`failed-run p50 stall_s` and `failed-run p50 loop_count` show what "
        "the median FAILED session looked like. If the suggested threshold "
        "is BELOW these values, the new setting would have caught half of "
        "past failures mid-flight (advisor engages, nudges or escalates)."
    )
    return "\n".join(lines)


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--events-dir", default=str(CHITIN_HOME), help="Where events-*.jsonl live")
    args = p.parse_args()

    events_dir = Path(args.events_dir)
    files = sorted(events_dir.glob("events-*.jsonl"))
    sessions: list[SessionMetrics] = []
    for f in files:
        m = load_session(f)
        if m is not None:
            sessions.append(m)

    thresholds = calibrate(sessions)
    when = datetime.now(timezone.utc)

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    md_path = OUT_DIR / f"floundering-calibration-{when.date().isoformat()}.md"
    json_path = OUT_DIR / f"floundering-calibration-{when.date().isoformat()}.json"
    md_path.write_text(render_markdown(sessions, thresholds, when))
    json_path.write_text(json.dumps({
        "generated_at": when.isoformat(),
        "source_dir": str(events_dir),
        "sessions_parsed": len(sessions),
        "thresholds_by_tier": {
            tier: {
                "n_sessions": t.n_sessions,
                "n_succeeded": t.n_succeeded,
                "n_failed": t.n_failed,
                "suggested_max_stall_seconds": t.suggested_max_stall_s,
                "suggested_max_loop_count": t.suggested_max_loop_count,
                "failed_max_stall_s_p50": t.failed_max_stall_s_p50,
                "failed_max_loop_count_p50": t.failed_max_loop_count_p50,
            }
            for tier, t in thresholds.items()
        },
    }, indent=2))

    print(f"floundering calibration: {len(sessions)} sessions → {md_path.name} + {json_path.name}")
    # Echo the table to stdout for operator quick-look.
    print()
    print(render_markdown(sessions, thresholds, when))


if __name__ == "__main__":
    main()
