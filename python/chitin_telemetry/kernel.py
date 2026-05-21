"""Continuous research kernel — the always-on Argus daemon.

A single-threaded tick loop:

  1. heartbeat: write a JSON liveness record for the operator to inspect
  2. gpu_check: defer this tick if the operator's opencode is active
  3. fast_detectors: deterministic detectors over the index
  4. urgent_push: rate-limited Discord push for new critical findings
  5. plan + execute: pick ONE LLM-bound task by weighted random:
       - narrate today's findings cluster
       - record an action-rate meta-finding (if engagement is low)
       - keep the model warm (when no other work is available)
  6. journal: append a one-line summary to ~/.argus/journal.ndjson
  7. sleep: tick_interval_s seconds

Invariants:
  * Read-only on every source EXCEPT ~/.argus/index.db.
  * GPU politeness: never run an LLM call while opencode is active.
  * Daily cap on LLM calls (config.kernel.daily_llm_cap).
  * Findings/memory/reports never persist uncited or unjudged LLM prose.

This is M2's kernel: it ships the loop, the safety scaffolding, the
schema, and the narrate/push tasks. M3 plugs hypothesis advancement,
synth detector promotion, and policy proposals into step 5.
"""
from __future__ import annotations

import json
import os
import random
import signal
import sqlite3
import sys
import time
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Optional

from chitin_telemetry import config, findings_store, gpu, llm, migrations, prompts
from chitin_telemetry.detectors import run_all_detectors
from chitin_telemetry.judge import judge


HEARTBEAT_PATH = Path.home() / ".argus" / "heartbeat.json"
JOURNAL_PATH = Path.home() / ".argus" / "journal.ndjson"
STATE_HTML_PATH = Path.home() / ".argus" / "argus-state.html"


@dataclass
class TickRecord:
    ts_unix: int
    action: str
    detail: str
    gpu_util_pct: float
    vram_free_mib: int
    operator_active: bool
    llm_calls_today: int


_stop_requested = False


def _install_signal_handlers() -> None:
    def _handler(signum, frame):  # noqa: ARG001
        global _stop_requested
        _stop_requested = True

    signal.signal(signal.SIGINT, _handler)
    signal.signal(signal.SIGTERM, _handler)


def _write_heartbeat(conn: sqlite3.Connection, gs: gpu.GpuStatus) -> None:
    HEARTBEAT_PATH.parent.mkdir(parents=True, exist_ok=True)
    data = {
        "ts": int(time.time()),
        "pid": os.getpid(),
        "gpu_util_pct": gs.util_pct,
        "vram_free_mib": gs.vram_free_mib,
        "operator_active": gs.operator_active,
        "llm_calls_today": llm.count_calls_today(conn),
    }
    try:
        HEARTBEAT_PATH.write_text(json.dumps(data, indent=2))
    except OSError:
        pass


def _journal_append(rec: TickRecord) -> None:
    JOURNAL_PATH.parent.mkdir(parents=True, exist_ok=True)
    try:
        with JOURNAL_PATH.open("a") as f:
            f.write(json.dumps(asdict(rec)) + "\n")
    except OSError:
        pass


def _critical_findings_since(
    conn: sqlite3.Connection, since_ts: int
) -> list[findings_store.StoredFinding]:
    return findings_store.since(
        conn, since_ts, severity="critical", include_acked=False, limit=10
    )


def _last_push_ts(conn: sqlite3.Connection) -> int:
    row = conn.execute(
        "SELECT value FROM kernel_state WHERE key = 'last_critical_push_ts'"
    ).fetchone()
    return int(row["value"]) if row else 0


def _set_last_push_ts(conn: sqlite3.Connection, ts: int) -> None:
    conn.execute(
        """
        INSERT INTO kernel_state (key, value, updated_ts)
        VALUES ('last_critical_push_ts', ?, ?)
        ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_ts = excluded.updated_ts
        """,
        (str(ts), ts),
    )
    conn.commit()


def _maybe_urgent_push(
    conn: sqlite3.Connection, cfg: config.ArgusConfig
) -> Optional[str]:
    if not cfg.discord.webhook_url:
        return None
    last = _last_push_ts(conn)
    now = int(time.time())
    if now - last < cfg.kernel.critical_push_rate_limit_s:
        return None
    crit = _critical_findings_since(conn, now - 24 * 3600)
    new = [f for f in crit if not f.pushed_ts]
    if not new:
        return None
    titles = "; ".join(f.title for f in new[:3])
    body = f"⚠️ Argus: {len(new)} new critical finding(s) — {titles}"
    _post_discord(cfg.discord.webhook_url, body)
    findings_store.mark_pushed(conn, [f.id for f in new])
    _set_last_push_ts(conn, now)
    return f"push:{len(new)} critical"


def _post_discord(webhook: str, content: str) -> None:
    import urllib.error
    import urllib.request

    payload = json.dumps({"content": content[:1800]}).encode()
    req = urllib.request.Request(
        webhook,
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=10):
            pass
    except (urllib.error.URLError, TimeoutError, OSError):
        pass


def _expected_citations_from_findings(
    findings: list[findings_store.StoredFinding],
) -> list[str]:
    """Build the closed citation set from a finding set's detector outputs."""
    cites: set[str] = set()
    for f in findings:
        cites.update(f.citations)
        # Pull common reference shapes out of the body JSON.
        try:
            body = json.loads(f.body)
        except json.JSONDecodeError:
            continue
        for v in _walk_strings(body):
            for token in v.split():
                if token.startswith("t_") or token.startswith("h_"):
                    cites.add(token.rstrip(".,;:"))
    return sorted(cites)


def _walk_strings(obj) -> list[str]:
    out: list[str] = []
    if isinstance(obj, str):
        out.append(obj)
    elif isinstance(obj, dict):
        for v in obj.values():
            out.extend(_walk_strings(v))
    elif isinstance(obj, list):
        for v in obj:
            out.extend(_walk_strings(v))
    return out


def _narrate_recent(
    conn: sqlite3.Connection, cfg: config.ArgusConfig
) -> str:
    """Generate and judge a short narration of the last 24h findings."""
    recent = findings_store.since(
        conn, int(time.time()) - 24 * 3600, include_acked=True, limit=20
    )
    if not recent:
        return "no_recent_findings"

    # Build a system prompt with the parent untrusted-preamble plus the
    # narration instruction.
    sys_prompt = prompts.NARRATE_DAILY_SYSTEM
    # User prompt: aggregate stats.
    stats = _stats_snapshot(conn)
    user_prompt = prompts.narrate_daily_user(stats)
    result = llm.call(
        conn,
        purpose="narrate_recent",
        system=sys_prompt,
        user=user_prompt,
    )
    if not result.ok or not result.text:
        return f"narrate_skipped:{result.skipped_reason or result.error}"

    # Judge it.
    expected = _expected_citations_from_findings(recent)
    # Also allow rule_ids and agent names from stats — they're the bound
    # citation set for the narration.
    expected.extend(s["rule_id"] for s in stats["top_deny_rules"])
    expected.extend(s["agent"] for s in stats["top_deny_agents"])
    verdict = judge(
        conn,
        purpose="narrate_recent",
        prompt=user_prompt,
        response=result.text,
        expected_citations=expected,
        sample_p=cfg.kernel.judge_sample_p_report,
    )
    return f"narrate:{verdict.verdict}:{verdict.reason}"


def _stats_snapshot(conn: sqlite3.Connection) -> dict:
    total = conn.execute("SELECT COUNT(*) AS c FROM events").fetchone()["c"]
    denies = conn.execute(
        "SELECT COUNT(*) AS c FROM events WHERE allowed = 0"
    ).fetchone()["c"]
    top_rules = conn.execute(
        """
        SELECT rule_id, COUNT(*) AS cnt FROM events
        WHERE allowed = 0 AND rule_id IS NOT NULL
        GROUP BY rule_id ORDER BY cnt DESC LIMIT 5
        """
    ).fetchall()
    top_agents = conn.execute(
        """
        SELECT agent, COUNT(*) AS cnt FROM events
        WHERE allowed = 0 AND agent IS NOT NULL
        GROUP BY agent ORDER BY cnt DESC LIMIT 5
        """
    ).fetchall()
    return {
        "total_events": int(total),
        "deny_count": int(denies),
        "allow_count": int(total - denies),
        "deny_percent": (denies / total * 100) if total else 0.0,
        "top_deny_rules": [{"rule_id": r["rule_id"], "count": r["cnt"]} for r in top_rules],
        "top_deny_agents": [{"agent": r["agent"], "deny_count": r["cnt"]} for r in top_agents],
    }


def _write_state_html(
    conn: sqlite3.Connection,
    cfg: config.ArgusConfig,
    last_record: Optional[TickRecord],
) -> None:
    """Free-running 'live state' page — local only, never published.

    Per R-6: this file is rewritten every tick but does NOT publish to
    the asset server and is not Discord-pushed. Operator can curl it.
    """
    stats = _stats_snapshot(conn)
    metrics = findings_store.action_rate(conn)
    recent = findings_store.since(
        conn, int(time.time()) - 24 * 3600, include_acked=True, limit=10
    )

    def _e(s):  # tiny escape helper
        import html as _html
        return _html.escape(str(s), quote=True)

    rec_rows = "".join(
        f"<tr><td>{_e(f.severity)}</td><td>{_e(f.detector)}</td>"
        f"<td>{_e(f.title)}</td>"
        f"<td>{_e(f.operator_action or '')}</td></tr>"
        for f in recent
    ) or "<tr><td colspan='4'>none yet</td></tr>"

    html = f"""<!doctype html><html><head><meta charset=utf-8>
<meta http-equiv=refresh content=30>
<title>Argus — live state</title>
<style>
:root {{ color-scheme: dark; }}
body {{ margin:0; font-family:ui-monospace,monospace; background:#0b1020; color:#e6edf7; }}
main {{ max-width:1000px; margin:0 auto; padding:24px; }}
h1 {{ font-size:24px; }}
.grid {{ display:grid; grid-template-columns:repeat(auto-fit,minmax(140px,1fr)); gap:10px; margin:14px 0; }}
.card {{ background:#111a2e; padding:12px; border-radius:10px; }}
.card span {{ color:#94a3b8; font-size:11px; display:block; }}
.card strong {{ font-size:22px; }}
table {{ width:100%; border-collapse:collapse; margin-top:14px; }}
td,th {{ padding:6px 8px; border-bottom:1px solid #1f2937; text-align:left; font-size:13px; }}
th {{ color:#67e8f9; }}
.muted {{ color:#94a3b8; }}
</style></head><body><main>
<h1>Argus — live state</h1>
<p class=muted>last tick: {_e(last_record.ts_unix if last_record else "—")}
  action: {_e(last_record.action if last_record else "—")} ·
  {_e(last_record.detail if last_record else "—")}</p>
<div class=grid>
  <div class=card><span>events total</span><strong>{stats['total_events']}</strong></div>
  <div class=card><span>denies</span><strong>{stats['deny_count']}</strong></div>
  <div class=card><span>deny %</span><strong>{stats['deny_percent']:.1f}</strong></div>
  <div class=card><span>findings 7d</span><strong>{metrics['surfaced']}</strong></div>
  <div class=card><span>action rate</span><strong>{metrics['action_rate']*100:.0f}%</strong></div>
  <div class=card><span>llm today</span><strong>{llm.count_calls_today(conn)}/{cfg.kernel.daily_llm_cap}</strong></div>
</div>
<h2>Recent findings</h2>
<table><thead><tr><th>sev</th><th>detector</th><th>title</th><th>action</th></tr></thead>
<tbody>{rec_rows}</tbody></table>
</main></body></html>"""
    try:
        STATE_HTML_PATH.parent.mkdir(parents=True, exist_ok=True)
        STATE_HTML_PATH.write_text(html)
    except OSError:
        pass


def tick(
    conn: sqlite3.Connection,
    cfg: config.ArgusConfig,
) -> TickRecord:
    """One pass through the kernel loop."""
    started = time.time()
    gs = gpu.status(
        util_threshold_pct=cfg.kernel.gpu_util_threshold_pct,
        vram_free_floor_mib=cfg.kernel.gpu_vram_floor_mib,
    )
    _write_heartbeat(conn, gs)

    # 1. Fast deterministic detectors — always run, cheap.
    try:
        findings = run_all_detectors(_get_index_path(conn))
    except Exception as e:  # noqa: BLE001
        findings = []
        action = "detectors_failed"
        detail = f"{type(e).__name__}: {e}"
    else:
        inserted, _ = findings_store.persist(conn, findings)
        action = "detectors_ok"
        detail = f"inserted={inserted} active={len(findings)}"

    # 2. Urgent critical push (rate-limited).
    push_detail = _maybe_urgent_push(conn, cfg)
    if push_detail:
        detail = f"{detail} | {push_detail}"

    # 3. LLM-bound task — only if GPU is available and we have budget.
    if gs.available and llm.count_calls_today(conn) < cfg.kernel.daily_llm_cap:
        task = _pick_task(conn)
        if task == "narrate":
            r = _narrate_recent(conn, cfg)
            detail = f"{detail} | {r}"
            action = "narrate"
        elif task == "keep_warm":
            llm.keep_warm()
            action = "keep_warm"
            detail = f"{detail} | keepwarm"
        elif task == "action_rate_meta":
            ar = findings_store.action_rate(conn)
            if ar["surfaced"] >= 10 and ar["action_rate"] < 0.10:
                _emit_meta_finding(conn, ar)
                action = "action_rate_meta"
                detail = f"{detail} | low_engagement={ar['action_rate']:.2f}"
            else:
                action = "noop"
    else:
        # Cap or GPU unavailable
        if not gs.available:
            detail = f"{detail} | gpu_unavail:{gs.reason}"
        else:
            detail = f"{detail} | cap_reached"

    rec = TickRecord(
        ts_unix=int(started),
        action=action,
        detail=detail,
        gpu_util_pct=gs.util_pct,
        vram_free_mib=gs.vram_free_mib,
        operator_active=gs.operator_active,
        llm_calls_today=llm.count_calls_today(conn),
    )
    _journal_append(rec)
    _write_state_html(conn, cfg, rec)
    return rec


def _pick_task(conn: sqlite3.Connection) -> str:
    """Weighted random choice over the small task set M2 ships."""
    weights = [("narrate", 5), ("action_rate_meta", 1), ("keep_warm", 2)]
    # If we narrated within the last 30 minutes, deprioritize.
    row = conn.execute(
        """
        SELECT MAX(ts_unix) AS last FROM llm_calls
        WHERE purpose = 'narrate_recent'
        """
    ).fetchone()
    last_narrate = int(row["last"] or 0)
    if time.time() - last_narrate < 1800:
        weights = [("narrate", 1), ("action_rate_meta", 1), ("keep_warm", 6)]
    items, weight_list = zip(*weights)
    return random.choices(items, weights=weight_list, k=1)[0]


def _emit_meta_finding(conn: sqlite3.Connection, action_rate_data: dict) -> None:
    """Emit a self-meta finding when operator engagement is low.

    Per R-12: low action rate is itself a signal to surface.
    """
    import hashlib
    import json as _json

    title = (
        f"Low operator engagement: action_rate={action_rate_data['action_rate']*100:.0f}% "
        f"over {action_rate_data['surfaced']} findings"
    )
    body = _json.dumps(action_rate_data, indent=2)
    # Idempotent within the same calendar day.
    bucket = time.strftime("%Y-%m-%d")
    fhash = hashlib.sha256(f"argus_meta_low_engagement|{bucket}".encode()).hexdigest()
    try:
        conn.execute(
            """
            INSERT INTO findings (
                finding_hash, ts_unix, detector, severity, title, body, citations
            ) VALUES (?, ?, ?, ?, ?, ?, '[]')
            """,
            (fhash, int(time.time()), "argus_meta:engagement", "info", title, body),
        )
        conn.commit()
    except sqlite3.IntegrityError:
        pass


def _get_index_path(conn: sqlite3.Connection) -> str:
    """Best-effort: extract sqlite filename from the connection's PRAGMA."""
    row = conn.execute("PRAGMA database_list").fetchone()
    if row and row["file"]:
        return row["file"]
    return str(Path.home() / ".argus" / "index.db")


def run(
    *,
    db_path: Optional[str] = None,
    tick_interval_s: Optional[int] = None,
    max_ticks: Optional[int] = None,
) -> int:
    """Main kernel entry point. Returns process exit code."""
    cfg = config.load()
    interval = tick_interval_s if tick_interval_s is not None else cfg.kernel.tick_interval_s

    db_path = db_path or str(Path.home() / ".argus" / "index.db")
    if not Path(db_path).exists():
        print(f"chitin-telemetry: index not found at {db_path}; run 'argus index' first", file=sys.stderr)
        return 1

    conn = migrations.open_writable(db_path)
    # Ensure schema is up to date before the loop starts.
    integrity = migrations.integrity_check(conn)
    if integrity != "ok":
        print(f"chitin-telemetry: integrity_check returned {integrity!r}; refusing to start", file=sys.stderr)
        return 2
    applied = migrations.apply_pending(conn)
    if applied:
        print(f"chitin-telemetry: applied schema migrations: {applied}", file=sys.stderr)

    _install_signal_handlers()

    print(f"argus kernel: tick_interval={interval}s db={db_path} cap={cfg.kernel.daily_llm_cap}/day", file=sys.stderr)
    ticks = 0
    try:
        while not _stop_requested:
            rec = tick(conn, cfg)
            print(
                f"[{time.strftime('%H:%M:%S')}] action={rec.action} {rec.detail}",
                file=sys.stderr,
            )
            ticks += 1
            if max_ticks is not None and ticks >= max_ticks:
                break
            # Sleep in small slices so SIGTERM is responsive.
            for _ in range(interval):
                if _stop_requested:
                    break
                time.sleep(1)
    finally:
        conn.close()
    return 0
