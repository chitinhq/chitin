"""Daily rollup of swarm dispatch health metrics.

Invariant: generate_rollup(runs, since, until) returns a RollupReport
where every metric covers exactly those SwarmRun records whose
dispatched_at is in [since, until).

Usage:
    cd python && python -m analysis.swarm_health [--window 24h] [--state-dir ...] [--tmp-dir ...]
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.request
import urllib.error
from collections import Counter, defaultdict
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Optional

# Short-run: agent exited in < 15s with zero commits — gave up fast.
SHORT_RUN_MS = 15_000
# Alarms
SUCCESS_ALARM_PCT = 70.0   # < this triggers LOW SUCCESS alarm
SHORT_RUN_ALARM_PCT = 25.0  # > this triggers HIGH SHORT-RUN alarm
QWEN_IDLE_ALARM_PCT = 80.0  # > this triggers QWEN IDLE alarm


@dataclass(frozen=True)
class SwarmRun:
    entry_id: str
    workflow_id: str
    tier: str
    driver: str
    dispatched_at: datetime
    exit_code: int
    duration_ms: int
    commits_added: int
    has_uncommitted_changes: bool
    bucket_b: bool


@dataclass
class RollupReport:
    window_since: datetime
    window_until: datetime
    total_runs: int
    dispatches_by_driver: dict[str, int]
    dispatches_by_tier: dict[str, int]
    # per key: {"success": int, "total": int}
    success_by_driver: dict[str, dict[str, int]]
    success_by_tier: dict[str, dict[str, int]]
    bucket_b_count: int
    bucket_b_rate: float
    short_run_by_driver: dict[str, dict[str, int]]
    short_run_by_tier: dict[str, dict[str, int]]
    local_qwen_t0_total: int
    local_qwen_t0_idle: int  # T0 runs NOT on local-qwen
    cost_total_usd: Optional[float]
    cost_by_driver: dict[str, float]
    failure_modes: dict[str, int]   # mode -> count, top 3 surfaced in Slack
    alarms: list[str]


# ---------------------------------------------------------------------------
# Data loading
# ---------------------------------------------------------------------------

_BUCKET_B_RE = re.compile(r"settings\.json\.chitin-backup-")
_COST_RE = re.compile(r"cost:\s*\$([0-9]+(?:\.[0-9]+)?)", re.IGNORECASE)
_TOTAL_COST_RE = re.compile(r"Total cost:\s*\$([0-9]+(?:\.[0-9]+)?)", re.IGNORECASE)


def _bucket_b_heuristic(stdout_tail: str) -> bool:
    """Heuristic: run contaminated diff with a settings backup artifact."""
    return bool(_BUCKET_B_RE.search(stdout_tail))


def _parse_cost_usd(stdout_tail: str) -> Optional[float]:
    """Best-effort cost extraction from claude-code-headless stdout."""
    for pattern in (_TOTAL_COST_RE, _COST_RE):
        m = pattern.search(stdout_tail)
        if m:
            try:
                return float(m.group(1))
            except ValueError:
                pass
    return None


def _load_markers(state_dir: Path) -> dict[str, dict]:
    """Load dispatch markers keyed by workflow_id. Skips bad files silently."""
    markers: dict[str, dict] = {}
    if not state_dir.exists():
        return markers
    for p in state_dir.iterdir():
        if p.suffix != ".json" or not p.is_file():
            continue
        try:
            raw = json.loads(p.read_text())
            wid = raw.get("workflow_id")
            if wid:
                markers[wid] = raw
        except (json.JSONDecodeError, OSError):
            pass
    return markers


def _load_envelopes(tmp_dir: Path) -> dict[str, dict]:
    """Load result envelopes keyed by workflow_id. Skips bad files silently."""
    envelopes: dict[str, dict] = {}
    if not tmp_dir.exists():
        return envelopes
    for p in tmp_dir.iterdir():
        if not (p.name.startswith("result-swarm-") and p.suffix == ".json"):
            continue
        try:
            raw = json.loads(p.read_text())
            wid = raw.get("workflow_id")
            if wid:
                envelopes[wid] = raw
        except (json.JSONDecodeError, OSError):
            pass
    return envelopes


def load_runs(
    state_dir: Path,
    tmp_dir: Path,
    since: datetime,
    until: datetime,
) -> list[SwarmRun]:
    """Load SwarmRun records whose dispatched_at is in [since, until).

    Joins dispatch markers (state_dir) with result envelopes (tmp_dir)
    by workflow_id. Runs whose marker has no matching envelope are
    included with defaults (exit_code=-1, duration_ms=0) so dispatches
    that are still in-flight or whose envelope was lost aren't silently
    dropped from the count.
    """
    markers = _load_markers(state_dir)
    envelopes = _load_envelopes(tmp_dir)

    runs: list[SwarmRun] = []
    for wid, marker in markers.items():
        ts_str = marker.get("dispatched_at")
        if not isinstance(ts_str, str):
            continue
        try:
            dispatched_at = datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
        except ValueError:
            continue
        if not (since <= dispatched_at < until):
            continue

        env = envelopes.get(wid, {})
        result = env.get("result", {})
        stdout_tail = result.get("stdout_tail", "")
        worktree = result.get("worktree") or {}

        runs.append(SwarmRun(
            entry_id=marker.get("entry_id", ""),
            workflow_id=wid,
            tier=marker.get("tier", ""),
            driver=marker.get("driver", ""),
            dispatched_at=dispatched_at,
            exit_code=result.get("exit_code", -1) if env else -1,
            duration_ms=result.get("duration_ms", 0),
            commits_added=worktree.get("commits_added", 0),
            has_uncommitted_changes=bool(worktree.get("has_uncommitted_changes", False)),
            bucket_b=_bucket_b_heuristic(stdout_tail),
        ))
    return runs


# ---------------------------------------------------------------------------
# Failure mode classification
# ---------------------------------------------------------------------------

def _failure_mode(run: SwarmRun) -> str:
    """Map a run to a canonical failure mode label.

    Called only for runs that didn't produce committed work (commits_added == 0
    and has_uncommitted_changes == False), so all returned strings are
    genuine failure signals.

    Tie-breaker order (most specific first):
        contamination > timeout > short-run-no-work > exit_code=1-partial > no-work
    """
    if run.bucket_b:
        return "contamination"
    if run.exit_code == -1:
        return "timeout"
    if run.duration_ms < SHORT_RUN_MS and run.commits_added == 0:
        return "short-run-no-work"
    if run.exit_code != 0:
        return "exit_code=1-partial"
    return "no-work-produced"


# ---------------------------------------------------------------------------
# Metric computation
# ---------------------------------------------------------------------------

def generate_rollup(runs: list[SwarmRun], since: datetime, until: datetime) -> RollupReport:
    """Compute all health metrics over the given run list."""
    dispatches_by_driver: Counter = Counter()
    dispatches_by_tier: Counter = Counter()
    success_by_driver: dict[str, dict[str, int]] = defaultdict(lambda: {"success": 0, "total": 0})
    success_by_tier: dict[str, dict[str, int]] = defaultdict(lambda: {"success": 0, "total": 0})
    short_run_by_driver: dict[str, dict[str, int]] = defaultdict(lambda: {"short": 0, "total": 0})
    short_run_by_tier: dict[str, dict[str, int]] = defaultdict(lambda: {"short": 0, "total": 0})
    failure_modes: Counter = Counter()
    cost_by_driver: dict[str, list[float]] = defaultdict(list)
    bucket_b_count = 0
    local_qwen_t0_total = 0
    local_qwen_t0_idle = 0

    for run in runs:
        dispatches_by_driver[run.driver] += 1
        dispatches_by_tier[run.tier] += 1

        # Success = agent committed or left uncommitted tracked changes
        produced_work = run.commits_added > 0 or run.has_uncommitted_changes
        success_by_driver[run.driver]["total"] += 1
        success_by_tier[run.tier]["total"] += 1
        if produced_work:
            success_by_driver[run.driver]["success"] += 1
            success_by_tier[run.tier]["success"] += 1
        else:
            failure_modes[_failure_mode(run)] += 1

        # Short-run detection (independent of success — a bucket_b short run
        # is already counted in contamination but also counted here for volume)
        is_short = run.duration_ms < SHORT_RUN_MS and run.commits_added == 0
        short_run_by_driver[run.driver]["total"] += 1
        short_run_by_tier[run.tier]["total"] += 1
        if is_short:
            short_run_by_driver[run.driver]["short"] += 1
            short_run_by_tier[run.tier]["short"] += 1

        if run.bucket_b:
            bucket_b_count += 1

        if run.tier == "T0":
            local_qwen_t0_total += 1
            if run.driver != "local-qwen":
                local_qwen_t0_idle += 1

    # Cost: not available from markers alone; placeholder for future
    # integration with gov-decisions cost_usd field or CCH stdout parsing.
    cost_total: Optional[float] = None
    cost_driver_map: dict[str, float] = {}

    n = len(runs)
    bucket_b_rate = bucket_b_count / n if n else 0.0
    alarms: list[str] = []

    if bucket_b_count > 0:
        alarms.append(
            f"BUCKET-B REGRESSION: {bucket_b_count}/{n} runs contaminated "
            f"({bucket_b_rate:.1%}) — PR #123 preflight may have regressed"
        )

    for driver, counts in success_by_driver.items():
        if counts["total"] > 0:
            rate = 100.0 * counts["success"] / counts["total"]
            if rate < SUCCESS_ALARM_PCT:
                alarms.append(
                    f"LOW SUCCESS: driver={driver} {rate:.0f}% "
                    f"({counts['success']}/{counts['total']})"
                )

    for tier, counts in success_by_tier.items():
        if counts["total"] > 0:
            rate = 100.0 * counts["success"] / counts["total"]
            if rate < SUCCESS_ALARM_PCT:
                alarms.append(
                    f"LOW SUCCESS: tier={tier} {rate:.0f}% "
                    f"({counts['success']}/{counts['total']})"
                )

    for driver, counts in short_run_by_driver.items():
        if counts["total"] > 0:
            rate = 100.0 * counts["short"] / counts["total"]
            if rate > SHORT_RUN_ALARM_PCT:
                alarms.append(
                    f"HIGH SHORT-RUN: driver={driver} {rate:.0f}% "
                    f"({counts['short']}/{counts['total']}) — "
                    "prompt template may not fit this driver's entries"
                )

    for tier, counts in short_run_by_tier.items():
        if counts["total"] > 0:
            rate = 100.0 * counts["short"] / counts["total"]
            if rate > SHORT_RUN_ALARM_PCT:
                alarms.append(
                    f"HIGH SHORT-RUN: tier={tier} {rate:.0f}% "
                    f"({counts['short']}/{counts['total']}) — "
                    "prompt template may not fit this tier's entries"
                )

    if local_qwen_t0_total > 0:
        idle_pct = 100.0 * local_qwen_t0_idle / local_qwen_t0_total
        if idle_pct > QWEN_IDLE_ALARM_PCT:
            alarms.append(
                f"QWEN IDLE: {idle_pct:.0f}% of T0 routed away from local-qwen "
                f"({local_qwen_t0_idle}/{local_qwen_t0_total}) — 3090 underused"
            )

    return RollupReport(
        window_since=since,
        window_until=until,
        total_runs=n,
        dispatches_by_driver=dict(dispatches_by_driver),
        dispatches_by_tier=dict(dispatches_by_tier),
        success_by_driver=dict(success_by_driver),
        success_by_tier=dict(success_by_tier),
        bucket_b_count=bucket_b_count,
        bucket_b_rate=bucket_b_rate,
        short_run_by_driver=dict(short_run_by_driver),
        short_run_by_tier=dict(short_run_by_tier),
        local_qwen_t0_total=local_qwen_t0_total,
        local_qwen_t0_idle=local_qwen_t0_idle,
        cost_total_usd=cost_total,
        cost_by_driver=cost_driver_map,
        failure_modes=dict(failure_modes.most_common()),
        alarms=alarms,
    )


# ---------------------------------------------------------------------------
# Output formatting
# ---------------------------------------------------------------------------

def rollup_to_dict(report: RollupReport) -> dict:
    """Serialize report to a JSON-serializable dict for the journal."""
    return {
        "window_since": report.window_since.isoformat(),
        "window_until": report.window_until.isoformat(),
        "total_runs": report.total_runs,
        "dispatches_by_driver": report.dispatches_by_driver,
        "dispatches_by_tier": report.dispatches_by_tier,
        "success_by_driver": report.success_by_driver,
        "success_by_tier": report.success_by_tier,
        "bucket_b_count": report.bucket_b_count,
        "bucket_b_rate": report.bucket_b_rate,
        "short_run_by_driver": report.short_run_by_driver,
        "short_run_by_tier": report.short_run_by_tier,
        "local_qwen_t0_total": report.local_qwen_t0_total,
        "local_qwen_t0_idle": report.local_qwen_t0_idle,
        "cost_total_usd": report.cost_total_usd,
        "cost_by_driver": report.cost_by_driver,
        "failure_modes": report.failure_modes,
        "alarms": report.alarms,
    }


def format_slack(report: RollupReport) -> dict:
    """Format the rollup as a Slack blocks payload."""
    date_str = report.window_since.strftime("%Y-%m-%d")
    n = report.total_runs
    alarm_icon = "🚨" if report.alarms else "✅"

    blocks: list[dict] = [
        {
            "type": "header",
            "text": {
                "type": "plain_text",
                "text": f"{alarm_icon} Swarm Daily Rollup — {date_str}",
            },
        },
    ]

    # Dispatches by driver with success rate
    driver_lines = []
    for driver in sorted(report.dispatches_by_driver):
        count = report.dispatches_by_driver[driver]
        counts = report.success_by_driver.get(driver, {"success": 0, "total": count})
        total_d = counts["total"] or 1
        rate = int(100 * counts["success"] / total_d)
        driver_lines.append(f"  *{driver}*: {count} dispatches  {rate}% success")

    tier_parts = []
    for tier in sorted(report.dispatches_by_tier):
        count = report.dispatches_by_tier[tier]
        counts = report.success_by_tier.get(tier, {"success": 0, "total": count})
        total_t = counts["total"] or 1
        rate = int(100 * counts["success"] / total_t)
        tier_parts.append(f"{tier}: {count} ({rate}%)")

    blocks.append({
        "type": "section",
        "text": {
            "type": "mrkdwn",
            "text": (
                f"*24h dispatches — {n} total*\n"
                + "\n".join(driver_lines)
                + ("\n_Tiers: " + "  ".join(tier_parts) + "_" if tier_parts else "")
            ),
        },
    })

    # Bucket-B
    bb_icon = "🚨" if report.bucket_b_count > 0 else "✅"
    blocks.append({
        "type": "section",
        "fields": [
            {
                "type": "mrkdwn",
                "text": (
                    f"*Bucket-B rate*\n"
                    f"{bb_icon} {report.bucket_b_rate:.1%} "
                    f"({report.bucket_b_count}/{n})"
                ),
            },
        ],
    })

    # Short-run rate per driver
    short_lines = []
    for driver in sorted(report.short_run_by_driver):
        counts = report.short_run_by_driver[driver]
        total_d = counts["total"] or 1
        rate = int(100 * counts["short"] / total_d)
        icon = "🚨" if rate > SHORT_RUN_ALARM_PCT else "·"
        short_lines.append(f"  {icon} {driver}: {rate}% ({counts['short']}/{counts['total']})")

    if short_lines:
        blocks.append({
            "type": "section",
            "text": {
                "type": "mrkdwn",
                "text": "*Short-run rate* (< 15s, 0 commits)\n" + "\n".join(short_lines),
            },
        })

    # Local-qwen T0 idle
    if report.local_qwen_t0_total > 0:
        idle_pct = int(100 * report.local_qwen_t0_idle / report.local_qwen_t0_total)
        qw_icon = "🚨" if idle_pct > QWEN_IDLE_ALARM_PCT else "✅"
        blocks.append({
            "type": "section",
            "fields": [
                {
                    "type": "mrkdwn",
                    "text": (
                        f"*T0 / local-qwen idle*\n"
                        f"{qw_icon} {idle_pct}% of T0 routed away "
                        f"({report.local_qwen_t0_idle}/{report.local_qwen_t0_total})"
                    ),
                },
            ],
        })

    # Top 3 failure modes
    top3 = list(report.failure_modes.items())[:3]
    if top3:
        lines = "\n".join(f"  · {mode}: {count}" for mode, count in top3)
        blocks.append({
            "type": "section",
            "text": {"type": "mrkdwn", "text": f"*Top failure modes*\n{lines}"},
        })

    # Alarms
    if report.alarms:
        alarm_text = "\n".join(f"• {a}" for a in report.alarms)
        blocks.append({
            "type": "section",
            "text": {"type": "mrkdwn", "text": f"*🚨 Alarms*\n{alarm_text}"},
        })

    fallback = (
        f"{alarm_icon} Swarm rollup {date_str}: {n} dispatches, "
        f"{report.bucket_b_count} bucket-B, {len(report.alarms)} alarm(s)"
    )
    return {"text": fallback, "blocks": blocks}


def post_slack(payload: dict, webhook_url: str) -> None:
    """Post to Slack webhook. Swallows errors — visibility must not block rollup."""
    data = json.dumps(payload).encode()
    req = urllib.request.Request(
        webhook_url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            if resp.status not in (200, 201, 204):
                print(f"[rollup] slack post non-ok: {resp.status}", file=sys.stderr)
    except urllib.error.URLError as exc:
        print(f"[rollup] slack post failed: {exc}", file=sys.stderr)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def _parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(
        prog="analysis.swarm_health",
        description="Daily swarm dispatch health rollup.",
    )
    home = os.environ.get("HOME", "/root")
    repo = os.environ.get("CHITIN_REPO_ROOT", f"{home}/workspace/chitin")
    p.add_argument(
        "--state-dir",
        default=f"{home}/.cache/chitin/swarm-state/dispatched",
        help="Dispatch marker directory",
    )
    p.add_argument(
        "--tmp-dir",
        default=f"{repo}/tmp",
        help="Workflow result envelope directory",
    )
    p.add_argument(
        "--rollup-dir",
        default=f"{home}/.cache/chitin/swarm-rollups",
        help="Journal output directory",
    )
    p.add_argument(
        "--window",
        default="24h",
        help="Window: e.g. 24h, 7d (default: 24h)",
    )
    p.add_argument(
        "--no-slack",
        action="store_true",
        help="Skip Slack post even if CHITIN_SLACK_WEBHOOK_URL is set",
    )
    return p.parse_args(argv)


def _parse_window(s: str, now: datetime) -> tuple[datetime, datetime]:
    """Parse 'Nd' / 'Nh' as a window [now-delta, now)."""
    if s.endswith("d"):
        delta = timedelta(days=int(s[:-1]))
    elif s.endswith("h"):
        delta = timedelta(hours=int(s[:-1]))
    elif s.endswith("m"):
        delta = timedelta(minutes=int(s[:-1]))
    else:
        raise ValueError(f"Unrecognized window: {s!r}. Use Nd, Nh, or Nm.")
    return now - delta, now


def main(argv: list[str] | None = None) -> int:
    args = _parse_args(argv)

    now = datetime.now(tz=timezone.utc)
    since, until = _parse_window(args.window, now)
    date_str = since.strftime("%Y-%m-%d")

    state_dir = Path(args.state_dir)
    tmp_dir = Path(args.tmp_dir)
    rollup_dir = Path(args.rollup_dir)

    runs = load_runs(state_dir, tmp_dir, since, until)
    report = generate_rollup(runs, since, until)
    report_dict = rollup_to_dict(report)

    # Structured stdout for systemd journal
    print(json.dumps(report_dict, indent=2))

    # Write journal file
    rollup_dir.mkdir(parents=True, exist_ok=True)
    journal_path = rollup_dir / f"{date_str}.json"
    journal_path.write_text(json.dumps(report_dict, indent=2))
    print(f"[rollup] wrote {journal_path}", file=sys.stderr)

    # Post to Slack
    webhook = os.environ.get("CHITIN_SLACK_WEBHOOK_URL", "").strip()
    if webhook and not args.no_slack:
        post_slack(format_slack(report), webhook)
        print("[rollup] posted to Slack", file=sys.stderr)

    if report.alarms:
        print(f"[rollup] {len(report.alarms)} alarm(s) fired", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
