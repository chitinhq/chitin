"""Daily digest report generator with qwen narration."""
from __future__ import annotations

import json
import sqlite3
import subprocess
from datetime import datetime
from pathlib import Path
from typing import Optional

from argus.detectors import Finding, run_all_detectors


def _get_summary_stats(db_path: str) -> dict:
    """Get summary statistics from the index."""
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row

    try:
        # Overall stats
        total = conn.execute("SELECT COUNT(*) as cnt FROM events").fetchone()["cnt"]
        denies = conn.execute("SELECT COUNT(*) as cnt FROM events WHERE allowed = 0").fetchone()["cnt"]
        allows = conn.execute("SELECT COUNT(*) as cnt FROM events WHERE allowed = 1").fetchone()["cnt"]

        # Top denies
        top_deny_rows = conn.execute("""
            SELECT rule_id, COUNT(*) as cnt
            FROM events
            WHERE allowed = 0
            GROUP BY rule_id
            ORDER BY cnt DESC
            LIMIT 5
        """).fetchall()

        top_denies = [{"rule_id": r["rule_id"], "count": r["cnt"]} for r in top_deny_rows]

        # Top agents
        top_agent_rows = conn.execute("""
            SELECT agent, COUNT(*) as cnt
            FROM events
            WHERE allowed = 0
            GROUP BY agent
            ORDER BY cnt DESC
            LIMIT 5
        """).fetchall()

        top_agents = [{"agent": r["agent"], "deny_count": r["cnt"]} for r in top_agent_rows]

        return {
            "total_events": total,
            "deny_count": denies,
            "allow_count": allows,
            "deny_percent": (denies / total * 100) if total > 0 else 0,
            "top_deny_rules": top_denies,
            "top_deny_agents": top_agents,
        }

    finally:
        conn.close()


def _call_qwen(prompt: str) -> Optional[str]:
    """Call qwen3.6:27b via ollama for narration. Returns None if unavailable."""
    try:
        result = subprocess.run(
            ["ollama", "run", "qwen3.6:27b", prompt],
            capture_output=True,
            text=True,
            timeout=30,
        )
        if result.returncode == 0:
            return result.stdout.strip()
    except (FileNotFoundError, subprocess.TimeoutExpired):
        pass
    return None


def _format_finding_table(findings: list[Finding]) -> str:
    """Format findings as markdown table."""
    if not findings:
        return "No findings detected.\n"

    lines = [
        "| Detector | Severity | Title | Details |",
        "|----------|----------|-------|---------|",
    ]

    for f in findings:
        details_str = json.dumps(f.details, indent=2).replace("\n", "<br>")
        lines.append(f"| {f.detector} | {f.severity} | {f.title} | {details_str} |")

    return "\n".join(lines) + "\n"


def generate_daily_report(db_path: str, report_dir: Optional[Path] = None) -> Path:
    """Generate daily markdown digest with detector findings and qwen narration.

    Returns path to written report.
    """
    if report_dir is None:
        report_dir = Path.home() / ".chitin" / "reports"
    report_dir.mkdir(parents=True, exist_ok=True)

    now = datetime.utcnow()
    date_str = now.date().isoformat()
    report_path = report_dir / f"{date_str}-digest.md"

    # Gather data
    stats = _get_summary_stats(db_path)
    findings = run_all_detectors(db_path)

    # Build markdown
    lines = [
        f"# Argus Observatory Report — {date_str}\n",
        f"*Generated: {now.isoformat()}Z*\n",
        "\n## Executive Summary\n",
    ]

    if not findings:
        lines.append("**All quiet.** No detector alerts in the last 24h.\n")
    else:
        lines.append(f"**{len(findings)} findings detected.**\n")

    # Try qwen narration
    narration_prompt = f"""
    Summarize these governance audit findings in 1-2 sentences:
    - Total decisions: {stats['total_events']}
    - Denies: {stats['deny_count']} ({stats['deny_percent']:.1f}%)
    - Top rule: {stats['top_deny_rules'][0]['rule_id'] if stats['top_deny_rules'] else 'N/A'}
    - Top agent: {stats['top_deny_agents'][0]['agent'] if stats['top_deny_agents'] else 'N/A'}
    """

    qwen_summary = _call_qwen(narration_prompt)
    if qwen_summary:
        lines.append(f"\n{qwen_summary}\n")
    else:
        lines.append("\n*(Qwen narration unavailable)*\n")

    # Statistics
    lines.append("\n## Statistics\n")
    lines.append(f"- **Total decisions:** {stats['total_events']}\n")
    lines.append(f"- **Denies:** {stats['deny_count']} ({stats['deny_percent']:.1f}%)\n")
    lines.append(f"- **Allows:** {stats['allow_count']}\n")

    # Top denies
    if stats["top_deny_rules"]:
        lines.append("\n### Top Deny Rules\n")
        for rule in stats["top_deny_rules"]:
            lines.append(f"- `{rule['rule_id']}`: {rule['count']} denies\n")

    # Top agents
    if stats["top_deny_agents"]:
        lines.append("\n### Top Deny Agents\n")
        for agent in stats["top_deny_agents"]:
            lines.append(f"- `{agent['agent']}`: {agent['deny_count']} denies\n")

    # Findings
    lines.append("\n## Detector Findings\n")
    lines.append(_format_finding_table(findings))

    # Write report
    report_path.write_text("".join(lines))
    return report_path
