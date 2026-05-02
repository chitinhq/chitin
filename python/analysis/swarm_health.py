"""Daily rollup of swarm dispatch health metrics.

Invariant: ``generate_rollup(runs, since, until)`` returns a
``RollupReport`` where every metric covers exactly those ``SwarmRun``
records whose ``dispatched_at`` is in ``[since, until)``.

Loading + parsing of the marker / envelope schema lives in
``analysis.swarm_runs`` (shipped in PR #126) and is reused here. We add
a marker-only "in-flight" count alongside, so a workflow whose marker
was written but envelope hasn't landed yet (e.g. workflow still running
or temporal lost the envelope) doesn't get silently dropped from the
total dispatch count.

Usage:
    cd python/analysis && uv run python -m analysis.swarm_health [--window 24h]
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from collections import Counter, defaultdict
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Optional

from analysis.loaders import Window
from analysis.swarm_runs import (
    SwarmRun,
    bucket_b_rate as _swarm_bucket_b_rate,
    cost_by_driver as _swarm_cost_by_driver,
    load_swarm_runs,
)

# Short-run: agent exited in < 15s with zero commits — gave up fast.
SHORT_RUN_MS = 15_000
# Alarm thresholds.
SUCCESS_ALARM_PCT = 70.0   # < this triggers LOW SUCCESS alarm
SHORT_RUN_ALARM_PCT = 25.0  # > this triggers HIGH SHORT-RUN alarm
QWEN_IDLE_ALARM_PCT = 80.0  # > this triggers QWEN IDLE alarm


@dataclass
class RollupReport:
    window_since: datetime
    window_until: datetime
    total_runs: int                              # markers + envelopes joined (envelope processed)
    in_flight_or_lost: int                       # markers w/o envelopes (workflow still running OR envelope lost)
    dispatches_by_driver: dict[str, int]
    dispatches_by_tier: dict[str, int]
    success_by_driver: dict[str, dict[str, int]]   # per key: {"success": int, "total": int}
    success_by_tier: dict[str, dict[str, int]]
    bucket_b_count: int
    bucket_b_rate: float
    short_run_by_driver: dict[str, dict[str, int]]   # per key: {"short": int, "total": int}
    short_run_by_tier: dict[str, dict[str, int]]
    local_qwen_t0_total: int
    local_qwen_t0_idle: int                          # T0 runs NOT on local-qwen
    cost_total_usd: float
    cost_by_driver: dict[str, float]
    failure_modes: dict[str, int]                    # mode -> count
    alarms: list[str]


# ---------------------------------------------------------------------------
# Loading
# ---------------------------------------------------------------------------

def _load_marker_count(state_dir: Path, window: Window) -> int:
    """Count markers whose dispatched_at is in ``window``.

    Used to compute the in-flight-or-lost gap: total markers in window
    minus envelopes-processed = how many dispatches don't yet have an
    outcome. ``analysis.swarm_runs.load_swarm_runs`` is envelope-driven
    and silently drops marker-only entries; this fills that gap so the
    rollup's "X dispatched" count matches what the dispatcher actually
    fired.
    """
    if not state_dir.exists():
        return 0
    count = 0
    for path in state_dir.iterdir():
        if path.suffix != ".json" or not path.is_file():
            continue
        try:
            marker = json.loads(path.read_text())
        except (json.JSONDecodeError, OSError):
            continue
        ts_str = marker.get("dispatched_at")
        if not isinstance(ts_str, str):
            continue
        try:
            dispatched_at = datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
        except ValueError:
            continue
        if window.since <= dispatched_at < window.until:
            count += 1
    return count


# ---------------------------------------------------------------------------
# Failure-mode classification
# ---------------------------------------------------------------------------

def _failure_mode(run: SwarmRun) -> str:
    """Map a no-work run to a canonical failure mode label.

    Called only for runs that produced no committed work. Tie-breaker
    order is most-specific first so a run that's both bucket-B AND
    short doesn't double-count.

    Order: contamination > timeout > short-run-no-work > exit_code=N
        > no-work-produced
    """
    if run.bucket_b:
        return "contamination"
    if run.exit_code == -1:
        # SIGKILL fired at wall_timeout (slice-7a). Distinct from a
        # genuinely missing envelope, which doesn't reach this loader
        # path (load_swarm_runs requires both marker AND envelope).
        return "timeout"
    if run.duration_ms < SHORT_RUN_MS and run.commits_added == 0:
        return "short-run-no-work"
    if run.exit_code != 0:
        # Use the actual exit code so future log readers don't have to
        # guess what "partial" meant.
        return f"exit_code={run.exit_code}"
    return "no-work-produced"


# ---------------------------------------------------------------------------
# Metric computation
# ---------------------------------------------------------------------------

def generate_rollup(
    runs: list[SwarmRun],
    in_flight_or_lost: int,
    since: datetime,
    until: datetime,
) -> RollupReport:
    """Compute all health metrics over the given run list."""
    dispatches_by_driver: Counter[str] = Counter()
    dispatches_by_tier: Counter[str] = Counter()
    success_by_driver: dict[str, dict[str, int]] = defaultdict(lambda: {"success": 0, "total": 0})
    success_by_tier: dict[str, dict[str, int]] = defaultdict(lambda: {"success": 0, "total": 0})
    short_run_by_driver: dict[str, dict[str, int]] = defaultdict(lambda: {"short": 0, "total": 0})
    short_run_by_tier: dict[str, dict[str, int]] = defaultdict(lambda: {"short": 0, "total": 0})
    failure_modes: Counter[str] = Counter()
    bucket_b_count = 0
    local_qwen_t0_total = 0
    local_qwen_t0_idle = 0

    for run in runs:
        dispatches_by_driver[run.driver] += 1
        dispatches_by_tier[run.tier] += 1

        # Success = agent committed at least one commit on top of base_ref.
        # has_uncommitted_changes alone isn't success: the apply step's
        # auto-commit fallback used to ship those, which is what produced
        # the bucket-B contamination this rollup is supposed to detect.
        # Now (post-PR #123) un-committed changes are reverted on
        # .claude/settings.json and the rest is auto-committed only if a
        # tracked diff remains — so commits_added > 0 is the honest
        # signal.
        produced_work = run.commits_added > 0
        success_by_driver[run.driver]["total"] += 1
        success_by_tier[run.tier]["total"] += 1
        if produced_work:
            success_by_driver[run.driver]["success"] += 1
            success_by_tier[run.tier]["success"] += 1
        else:
            failure_modes[_failure_mode(run)] += 1

        # Short-run detection (volume signal, independent of failure-mode
        # bucketing): runs that exit fast with no commits are a prompt /
        # tier-fit alarm even when not bucket-B.
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

    # Cost: reuse the swarm_runs extractor (claude-code-headless emits
    # total_cost_usd in stdout_tail; copilot reports no per-run cost so
    # rolls up to 0.0).
    cost_driver_map = _swarm_cost_by_driver(runs)
    cost_total = sum(cost_driver_map.values())

    n = len(runs)
    rate = _swarm_bucket_b_rate(runs)
    alarms: list[str] = []

    if bucket_b_count > 0:
        alarms.append(
            f"BUCKET-B REGRESSION: {bucket_b_count}/{n} runs contaminated "
            f"({rate:.1%}) — apply-step revert (PR #123) may have regressed"
        )

    for driver, counts in success_by_driver.items():
        if counts["total"] > 0:
            r = 100.0 * counts["success"] / counts["total"]
            if r < SUCCESS_ALARM_PCT:
                alarms.append(
                    f"LOW SUCCESS: driver={driver} {r:.0f}% "
                    f"({counts['success']}/{counts['total']})"
                )

    for tier, counts in success_by_tier.items():
        if counts["total"] > 0:
            r = 100.0 * counts["success"] / counts["total"]
            if r < SUCCESS_ALARM_PCT:
                alarms.append(
                    f"LOW SUCCESS: tier={tier} {r:.0f}% "
                    f"({counts['success']}/{counts['total']})"
                )

    for driver, counts in short_run_by_driver.items():
        if counts["total"] > 0:
            r = 100.0 * counts["short"] / counts["total"]
            if r > SHORT_RUN_ALARM_PCT:
                alarms.append(
                    f"HIGH SHORT-RUN: driver={driver} {r:.0f}% "
                    f"({counts['short']}/{counts['total']}) — prompt template may not fit this driver"
                )

    for tier, counts in short_run_by_tier.items():
        if counts["total"] > 0:
            r = 100.0 * counts["short"] / counts["total"]
            if r > SHORT_RUN_ALARM_PCT:
                alarms.append(
                    f"HIGH SHORT-RUN: tier={tier} {r:.0f}% "
                    f"({counts['short']}/{counts['total']}) — prompt template may not fit this tier"
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
        in_flight_or_lost=in_flight_or_lost,
        dispatches_by_driver=dict(dispatches_by_driver),
        dispatches_by_tier=dict(dispatches_by_tier),
        success_by_driver={k: dict(v) for k, v in success_by_driver.items()},
        success_by_tier={k: dict(v) for k, v in success_by_tier.items()},
        bucket_b_count=bucket_b_count,
        bucket_b_rate=rate,
        short_run_by_driver={k: dict(v) for k, v in short_run_by_driver.items()},
        short_run_by_tier={k: dict(v) for k, v in short_run_by_tier.items()},
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
    """Serialize report to a JSON-serializable dict for the journal file."""
    return {
        "window_since": report.window_since.isoformat(),
        "window_until": report.window_until.isoformat(),
        "total_runs": report.total_runs,
        "in_flight_or_lost": report.in_flight_or_lost,
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
            "text": {"type": "plain_text", "text": f"{alarm_icon} Swarm Daily Rollup — {date_str}"},
        }
    ]

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

    in_flight_note = (
        f"  _+ {report.in_flight_or_lost} in-flight or lost (markers without envelopes)_"
        if report.in_flight_or_lost > 0
        else ""
    )

    blocks.append({
        "type": "section",
        "text": {
            "type": "mrkdwn",
            "text": (
                f"*24h dispatches — {n} processed*\n"
                + "\n".join(driver_lines)
                + ("\n" + in_flight_note if in_flight_note else "")
                + ("\n_Tiers: " + "  ".join(tier_parts) + "_" if tier_parts else "")
            ),
        },
    })

    bb_icon = "🚨" if report.bucket_b_count > 0 else "✅"
    blocks.append({
        "type": "section",
        "fields": [{
            "type": "mrkdwn",
            "text": (
                f"*Bucket-B rate*\n"
                f"{bb_icon} {report.bucket_b_rate:.1%} ({report.bucket_b_count}/{n})"
            ),
        }],
    })

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

    if report.local_qwen_t0_total > 0:
        idle_pct = int(100 * report.local_qwen_t0_idle / report.local_qwen_t0_total)
        qw_icon = "🚨" if idle_pct > QWEN_IDLE_ALARM_PCT else "✅"
        blocks.append({
            "type": "section",
            "fields": [{
                "type": "mrkdwn",
                "text": (
                    f"*T0 / local-qwen idle*\n"
                    f"{qw_icon} {idle_pct}% of T0 routed away "
                    f"({report.local_qwen_t0_idle}/{report.local_qwen_t0_total})"
                ),
            }],
        })

    if report.cost_total_usd > 0:
        cost_lines = "\n".join(
            f"  *{d}*: ${c:.2f}" for d, c in sorted(report.cost_by_driver.items()) if c > 0
        )
        blocks.append({
            "type": "section",
            "text": {
                "type": "mrkdwn",
                "text": f"*Cost (24h): ${report.cost_total_usd:.2f}*\n{cost_lines}",
            },
        })

    top3 = list(report.failure_modes.items())[:3]
    if top3:
        lines = "\n".join(f"  · {mode}: {count}" for mode, count in top3)
        blocks.append({
            "type": "section",
            "text": {"type": "mrkdwn", "text": f"*Top failure modes*\n{lines}"},
        })

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
    """Post to Slack webhook. Errors are swallowed — visibility must not block rollup."""
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
    p.add_argument("--state-dir", default=f"{home}/.cache/chitin/swarm-state/dispatched", help="Dispatch marker directory")
    p.add_argument("--tmp-dir", default=f"{repo}/tmp", help="Workflow result envelope directory")
    p.add_argument("--rollup-dir", default=f"{home}/.cache/chitin/swarm-rollups", help="Journal output directory")
    p.add_argument("--window", default="24h", help="Window: e.g. 24h, 7d (default: 24h)")
    p.add_argument("--no-slack", action="store_true", help="Skip Slack post even if CHITIN_SLACK_WEBHOOK_URL is set")
    return p.parse_args(argv)


def _parse_window(s: str, now: datetime) -> tuple[datetime, datetime]:
    """Parse 'Nd' / 'Nh' / 'Nm' as a window [now-delta, now)."""
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
    window = Window(since=since, until=until)

    runs = load_swarm_runs(state_dir, tmp_dir, window=window)
    in_flight = max(_load_marker_count(state_dir, window) - len(runs), 0)
    report = generate_rollup(runs, in_flight, since, until)
    report_dict = rollup_to_dict(report)

    print(json.dumps(report_dict, indent=2))

    rollup_dir.mkdir(parents=True, exist_ok=True)
    journal_path = rollup_dir / f"{date_str}.json"
    journal_path.write_text(json.dumps(report_dict, indent=2))
    print(f"[rollup] wrote {journal_path}", file=sys.stderr)

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
