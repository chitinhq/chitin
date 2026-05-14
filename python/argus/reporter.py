"""Daily digest report generator with qwen narration."""
from __future__ import annotations

import json
import os
import sqlite3
import subprocess
import tempfile
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from argus.detectors import Finding, run_all_detectors
from argus.cross_detectors import CrossFinding, run_all_cross_detectors
from argus.log_detectors import run_all_log_detectors


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
    """Call qwen3.6:27b via ollama for narration. Returns None if unavailable.

    Setting ARGUS_SKIP_QWEN=1 bypasses the subprocess (used in tests and on
    quiet-day runs where narration adds no value).
    """
    if os.environ.get("ARGUS_SKIP_QWEN"):
        return None
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


def _atomic_write_text(target: Path, content: str) -> None:
    """Write `content` to `target` atomically via temp-file + os.replace."""
    target.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp_path = tempfile.mkstemp(
        prefix=target.name + ".", suffix=".tmp", dir=str(target.parent)
    )
    try:
        with os.fdopen(fd, "w") as tmp:
            tmp.write(content)
        os.replace(tmp_path, target)
    except Exception:
        try:
            os.unlink(tmp_path)
        except OSError:
            pass
        raise


def _atomic_symlink(target: Path, points_to: Path) -> None:
    """Atomically point `target` symlink at `points_to` (file in same dir)."""
    target.parent.mkdir(parents=True, exist_ok=True)
    rel = points_to.name if points_to.parent == target.parent else str(points_to)
    tmp_link = target.with_name(target.name + f".tmp.{os.getpid()}")
    try:
        if tmp_link.is_symlink() or tmp_link.exists():
            os.unlink(tmp_link)
    except OSError:
        pass
    os.symlink(rel, tmp_link)
    os.replace(tmp_link, target)


def _post_discord_summary(webhook_url: str, headline: str, link: Optional[str]) -> bool:
    """Best-effort POST to a Discord webhook. Returns True on 2xx, False otherwise."""
    body = headline if not link else f"{headline}\n{link}"
    payload = json.dumps({"content": body}).encode("utf-8")
    req = urllib.request.Request(
        webhook_url,
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return 200 <= resp.status < 300
    except (urllib.error.URLError, TimeoutError, OSError):
        return False


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


def _html_escape(value: object) -> str:
    import html
    return html.escape(str(value), quote=True)


def _render_html(
    date_str: str,
    generated: str,
    stats: dict,
    findings: list[Finding],
    summary: str,
    cross_findings: Optional[list["CrossFinding"]] = None,
) -> str:
    cross_findings = cross_findings or []
    cards = "".join([
        f"<div class='card'><span>Total</span><strong>{stats['total_events']}</strong></div>",
        f"<div class='card'><span>Denies</span><strong>{stats['deny_count']}</strong></div>",
        f"<div class='card'><span>Deny rate</span><strong>{stats['deny_percent']:.1f}%</strong></div>",
        f"<div class='card'><span>Chain Findings</span><strong>{len(findings)}</strong></div>",
        f"<div class='card'><span>Cross-Source</span><strong>{len(cross_findings)}</strong></div>",
    ])
    finding_rows = "".join(
        f"<tr><td>{_html_escape(f.detector)}</td><td>{_html_escape(f.severity)}</td><td>{_html_escape(f.title)}</td><td><pre>{_html_escape(json.dumps(f.details, indent=2))}</pre></td></tr>"
        for f in findings
    ) or "<tr><td colspan='4'>No findings detected.</td></tr>"
    cross_rows = "".join(
        "<tr><td>{det}</td><td>{sev}</td><td>{title}</td><td><pre>{det_json}</pre></td><td><pre>{ev}</pre></td></tr>".format(
            det=_html_escape(f.detector),
            sev=_html_escape(f.severity),
            title=_html_escape(f.title),
            det_json=_html_escape(json.dumps(f.details, indent=2)),
            ev=_html_escape("\n".join(f.evidence)),
        )
        for f in cross_findings
    ) or "<tr><td colspan='5'>No cross-source findings.</td></tr>"
    top_rules = "".join(f"<li><code>{_html_escape(r['rule_id'])}</code>: {r['count']}</li>" for r in stats['top_deny_rules']) or "<li>None</li>"
    top_agents = "".join(f"<li><code>{_html_escape(a['agent'])}</code>: {a['deny_count']}</li>" for a in stats['top_deny_agents']) or "<li>None</li>"
    return f"""<!doctype html>
<html lang='en'>
<head>
<meta charset='utf-8'>
<meta name='viewport' content='width=device-width, initial-scale=1'>
<title>Argus Research — {date_str}</title>
<style>
:root {{ color-scheme: dark; --bg:#0b1020; --panel:#111a2e; --text:#e6edf7; --muted:#94a3b8; --accent:#67e8f9; --danger:#fb7185; }}
body {{ margin:0; font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, sans-serif; background:linear-gradient(135deg,#07111f,#111827); color:var(--text); }}
main {{ max-width:1100px; margin:0 auto; padding:32px 20px; }}
h1 {{ margin:0 0 6px; font-size:clamp(28px,5vw,48px); }}
.muted {{ color:var(--muted); }}
.grid {{ display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr)); gap:14px; margin:24px 0; }}
.card, section {{ background:rgba(17,26,46,.86); border:1px solid rgba(148,163,184,.18); border-radius:18px; padding:18px; box-shadow:0 14px 40px rgba(0,0,0,.25); }}
.card span {{ color:var(--muted); display:block; font-size:13px; }}
.card strong {{ display:block; font-size:32px; margin-top:8px; }}
section {{ margin:18px 0; overflow:auto; }}
table {{ width:100%; border-collapse:collapse; min-width:700px; }}
th, td {{ text-align:left; padding:10px 12px; border-bottom:1px solid rgba(148,163,184,.16); vertical-align:top; }}
th {{ color:var(--accent); }}
pre {{ white-space:pre-wrap; margin:0; color:#cbd5e1; }}
code {{ color:var(--accent); }}
</style>
</head>
<body><main>
<h1>Argus Research</h1>
<p class='muted'>Daily governance digest · {date_str} · generated {generated}Z</p>
<div class='grid'>{cards}</div>
<section><h2>Executive Summary</h2><p>{_html_escape(summary)}</p></section>
<section><h2>Top Deny Rules</h2><ul>{top_rules}</ul></section>
<section><h2>Top Deny Agents</h2><ul>{top_agents}</ul></section>
<section><h2>Chain Detector Findings</h2><table><thead><tr><th>Detector</th><th>Severity</th><th>Title</th><th>Details</th></tr></thead><tbody>{finding_rows}</tbody></table></section>
<section><h2>Cross-Source Findings</h2><table><thead><tr><th>Detector</th><th>Severity</th><th>Title</th><th>Details</th><th>Evidence</th></tr></thead><tbody>{cross_rows}</tbody></table></section>
</main></body></html>
"""


def generate_daily_report(
    db_path: str,
    report_dir: Optional[Path] = None,
    *,
    discord_webhook: Optional[str] = None,
    quiet_day_skip_discord: bool = True,
    cross_source_db: Optional[Path] = None,
) -> Path:
    """Generate daily markdown digest with detector findings and qwen narration.

    Writes a dated HTML report and atomically retargets the
    `argus-research-latest.html` symlink at it. If `discord_webhook` is set,
    posts a one-line summary unless `quiet_day_skip_discord` and no detectors
    fired. Returns path to the dated HTML report.
    """
    if report_dir is None:
        report_dir = Path.home() / ".chitin" / "reports"
    report_dir.mkdir(parents=True, exist_ok=True)

    now = datetime.now(timezone.utc)
    date_str = now.date().isoformat()
    md_path = report_dir / f"{date_str}-digest.md"
    report_path = report_dir / f"{date_str}-argus-research.html"
    latest_path = report_dir / "argus-research-latest.html"

    # Gather data
    stats = _get_summary_stats(db_path)
    findings = run_all_detectors(db_path)

    # Cross-source findings (Slice 2 + Slice 3). Default to
    # ~/.argus/cross_source.db; degrade silently if it doesn't exist yet.
    if cross_source_db is None:
        cross_source_db = Path.home() / ".argus" / "cross_source.db"
    cross_findings: list[CrossFinding] = []
    if cross_source_db.exists():
        try:
            cross_findings = run_all_cross_detectors(cross_source_db)
            cross_findings.extend(run_all_log_detectors(cross_source_db))
            cross_findings.sort(key=lambda f: (-f.ts_unix, f.subject, f.detector))
        except Exception:
            pass

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
        summary = qwen_summary
        lines.append(f"\n{qwen_summary}\n")
    else:
        summary = "Qwen narration unavailable; see statistics and detector findings below."
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

    # Cross-source section (markdown projection)
    if cross_findings:
        lines.append("\n## Cross-Source Findings\n")
        for cf in cross_findings:
            lines.append(f"- **[{cf.detector}]** {cf.title}\n")
            if cf.evidence:
                lines.append(f"    Evidence: {', '.join(cf.evidence)}\n")

    # Write markdown compatibility output plus Hermes-style dark HTML/latest contract.
    _atomic_write_text(md_path, "".join(lines))
    html = _render_html(
        date_str,
        now.isoformat(),
        stats,
        findings,
        summary,
        cross_findings=cross_findings,
    )
    _atomic_write_text(report_path, html)
    _atomic_symlink(latest_path, report_path)

    # Discord summary: best-effort. Suppressed on quiet days unless the
    # operator opts in to always-post via quiet_day_skip_discord=False.
    total_findings = len(findings) + len(cross_findings)
    if discord_webhook and (total_findings or not quiet_day_skip_discord):
        headline = (
            f"Argus daily digest {date_str}: {len(findings)} chain + "
            f"{len(cross_findings)} cross-source finding(s); deny rate "
            f"{stats['deny_percent']:.1f}% across {stats['total_events']} events."
            if total_findings
            else f"Argus daily digest {date_str}: all quiet ({stats['total_events']} events)."
        )
        _post_discord_summary(discord_webhook, headline, link=None)

    return report_path
