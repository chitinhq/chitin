"""CLI entry: argus command."""
from __future__ import annotations

import argparse
import sqlite3
import sys
from pathlib import Path

from argus import findings_cli, migrations
from argus.logs import follow_all_sources, index_all_sources
from argus.reporter import generate_daily_report


def cmd_index(args) -> int:
    """Index chain decisions plus configured Slice 3 sources."""
    decisions_dir = Path(args.decisions_dir)
    if not decisions_dir.exists():
        print(f"Error: decisions_dir not found: {decisions_dir}", file=sys.stderr)
        return 1

    # Apply pending migrations before any indexing — keeps the schema
    # current even when the kernel hasn't run yet.
    try:
        db_path = Path.home() / ".argus" / "index.db"
        if db_path.exists():
            conn = migrations.open_writable(db_path)
            migrations.apply_pending(conn)
            conn.close()
    except sqlite3.DatabaseError:
        pass

    try:
        db_path = Path(args.db_path)
        if args.follow:
            print(f"Following {decisions_dir} into {db_path}", file=sys.stderr)
            follow_all_sources(
                db_path=db_path,
                decisions_dir=decisions_dir,
                poll_seconds=args.poll_seconds,
            )
            return 0
        out = index_all_sources(
            db_path=db_path,
            decisions_dir=decisions_dir,
        )
        print(f"Indexed to {out}", file=sys.stderr)
        return 0
    except KeyboardInterrupt:
        return 130
    except Exception as e:  # noqa: BLE001
        print(f"Error indexing: {e}", file=sys.stderr)
        return 2


def cmd_report(args) -> int:
    """Generate daily digest report."""
    db_path = Path(args.db_path)
    if not db_path.exists():
        print(f"Error: index not found: {db_path}", file=sys.stderr)
        print("Run 'argus index' first.", file=sys.stderr)
        return 1

    import os
    webhook = args.discord_webhook or os.environ.get("ARGUS_DISCORD_WEBHOOK") or None

    try:
        report_path = generate_daily_report(
            str(db_path),
            Path(args.report_dir),
            discord_webhook=webhook,
            quiet_day_skip_discord=not args.discord_always,
        )
        print(f"Wrote {report_path}", file=sys.stderr)
        print(report_path)
        return 0
    except Exception as e:  # noqa: BLE001
        print(f"Error generating report: {e}", file=sys.stderr)
        return 2


def cmd_query(args) -> int:
    """Query the index with a natural language question."""
    from argus import llm, prompts

    db_path = Path(args.db_path)
    if not db_path.exists():
        print(f"Error: index not found: {db_path}", file=sys.stderr)
        return 1

    question = args.query
    system = prompts.system_with_preamble(
        "You translate a natural-language question into a single read-only "
        "SQL SELECT against the table `events` with columns: source, kind, subject, "
        "source_ref, payload_json, ts, allowed, mode, rule_id, reason, escalation, "
        "agent, action_type, action_target, envelope_id, tier, cost_usd, input_bytes, "
        "tool_calls, model, role, workflow_id, fingerprint. Return ONLY the SELECT "
        "statement, no prose, no semicolon, no code fence."
    )

    conn = migrations.open_writable(db_path)
    try:
        result = llm.call(conn, purpose="query_nl2sql", system=system, user=question)
    finally:
        conn.close()
    if not result.ok or not result.text:
        print(f"argus: LLM unavailable ({result.error or result.skipped_reason})", file=sys.stderr)
        return 2

    sql = _sanitize_readonly_select(result.text)
    if not sql:
        print("argus: LLM returned no safe read-only SELECT", file=sys.stderr)
        print(f"raw: {result.text!r}", file=sys.stderr)
        return 2

    ro = migrations.open_readonly(db_path)
    try:
        rows = ro.execute(sql).fetchall()
        import json as _json
        results = [dict(row) for row in rows]
        print(_json.dumps(results, indent=2, default=str))
        return 0
    except Exception as e:  # noqa: BLE001
        print(f"argus: query error: {e}", file=sys.stderr)
        print(f"SQL: {sql}", file=sys.stderr)
        return 2
    finally:
        ro.close()


def cmd_kernel(args) -> int:
    from argus import kernel

    return kernel.run(
        db_path=args.db_path,
        tick_interval_s=args.tick_interval,
        max_ticks=args.max_ticks,
    )


def cmd_migrate(args) -> int:
    db_path = Path(args.db_path)
    if not db_path.exists():
        print(f"Error: index not found: {db_path}", file=sys.stderr)
        return 1
    conn = migrations.open_writable(db_path)
    integrity = migrations.integrity_check(conn)
    print(f"integrity_check: {integrity}", file=sys.stderr)
    if integrity != "ok":
        return 2
    applied = migrations.apply_pending(conn)
    if applied:
        print(f"applied: {applied}", file=sys.stderr)
    else:
        print("schema up to date", file=sys.stderr)
    conn.close()
    return 0


def _sanitize_readonly_select(raw_sql: str) -> str | None:
    """Return a single read-only SELECT statement, or None if unsafe."""
    sql = raw_sql.strip()
    if sql.startswith("```"):
        sql = sql.strip("`").strip()
        if sql.lower().startswith("sql"):
            sql = sql[3:].strip()
    if sql.endswith(";"):
        sql = sql[:-1].strip()
    lowered = sql.lower()
    if not lowered.startswith("select "):
        return None
    if ";" in sql:
        return None
    forbidden = (
        "insert ", "update ", "delete ", "drop ", "alter ", "create ",
        "replace ", "attach ", "detach ", "pragma ", "vacuum ",
    )
    padded = f" {lowered} "
    if any(token in padded for token in forbidden):
        return None
    return sql


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="argus", description="Observatory: tail, index, detect, report.")
    p.add_argument("--db-path", default=str(Path.home() / ".argus" / "index.db"),
                   help="Path to index.db")

    subparsers = p.add_subparsers(dest="cmd", required=True)

    # index
    index_p = subparsers.add_parser("index", help="Index chain, logs, and Discord sources")
    index_p.add_argument("--decisions-dir", default=str(Path.home() / ".chitin"),
                         help="Directory containing gov-decisions-*.jsonl")
    index_p.add_argument("--follow", action="store_true",
                         help="Stay running and index appended lines/date-rollover files")
    index_p.add_argument("--poll-seconds", type=float, default=1.0,
                         help="Polling interval for --follow")

    # report
    report_p = subparsers.add_parser("report", help="Generate daily digest report")
    report_p.add_argument("--report-dir",
                          default=str(Path.home() / ".chitin" / "reports"),
                          help="Output directory for reports")
    report_p.add_argument("--discord-webhook", default=None,
                          help="Discord webhook URL (overrides ARGUS_DISCORD_WEBHOOK)")
    report_p.add_argument("--discord-always", action="store_true",
                          help="Post Discord even on quiet days")

    # query
    query_p = subparsers.add_parser("query", help="Query index with NL question")
    query_p.add_argument("query", nargs="+", help="Natural language question")

    # kernel
    kernel_p = subparsers.add_parser("kernel", help="Run the always-on research kernel")
    kernel_p.add_argument("--tick-interval", type=int, default=None,
                          help="Override config.kernel.tick_interval_s")
    kernel_p.add_argument("--max-ticks", type=int, default=None,
                          help="Stop after N ticks (smoke testing)")

    # migrate
    migrate_p = subparsers.add_parser("migrate", help="Apply pending schema migrations")
    migrate_p.set_defaults(func=cmd_migrate)

    # findings
    findings_p = subparsers.add_parser("findings",
                                       help="Emit JSON findings since a timestamp")
    findings_p.add_argument("--since", required=True,
                            help="Epoch seconds or ISO 8601 timestamp")
    findings_p.add_argument("--severity", choices=["info", "warning", "critical"],
                            default=None)
    findings_p.add_argument("--include-acked", action="store_true",
                            help="Include findings the operator has acked/snoozed/applied")
    findings_p.add_argument("--limit", type=int, default=None)
    findings_p.add_argument("--pretty", action="store_true",
                            help="Indent JSON; otherwise NDJSON")

    # finding {ack,snooze,flag,apply,unack} <id>
    finding_p = subparsers.add_parser("finding",
                                      help="Set operator action on a finding")
    finding_sub = finding_p.add_subparsers(dest="finding_cmd", required=True)
    for action in ("ack", "snooze", "flag", "apply"):
        sp = finding_sub.add_parser(action, help=f"Mark a finding {action}ed")
        sp.add_argument("finding_id", type=int)
        sp.set_defaults(action=action)

    # action-rate
    ar_p = subparsers.add_parser("action-rate",
                                 help="Print operator engagement metrics as JSON")
    ar_p.add_argument("--window-days", type=int, default=7)

    return p.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)

    if args.cmd == "index":
        return cmd_index(args)
    if args.cmd == "report":
        return cmd_report(args)
    if args.cmd == "query":
        args.query = " ".join(args.query)
        return cmd_query(args)
    if args.cmd == "kernel":
        return cmd_kernel(args)
    if args.cmd == "migrate":
        return cmd_migrate(args)
    if args.cmd == "findings":
        return findings_cli.cmd_findings(args)
    if args.cmd == "finding":
        return findings_cli.cmd_finding_action(args)
    if args.cmd == "action-rate":
        return findings_cli.cmd_action_rate(args)

    return 1


if __name__ == "__main__":
    sys.exit(main())
