"""CLI entry: argus command."""
from __future__ import annotations

import argparse
import sqlite3
import sys
from pathlib import Path

from argus.indexer import follow_all_decisions, tail_all_decisions
from argus.reporter import generate_daily_report


def cmd_index(args) -> int:
    """Index gov-decisions JSONL files from decisions_dir."""
    decisions_dir = Path(args.decisions_dir)
    if not decisions_dir.exists():
        print(f"Error: decisions_dir not found: {decisions_dir}", file=sys.stderr)
        return 1

    try:
        if args.follow:
            print(f"Following {decisions_dir} into {Path.home() / '.argus' / 'index.db'}", file=sys.stderr)
            follow_all_decisions(decisions_dir, poll_seconds=args.poll_seconds)
            return 0
        db_path = tail_all_decisions(decisions_dir)
        print(f"Indexed to {db_path}", file=sys.stderr)
        return 0
    except KeyboardInterrupt:
        return 130
    except Exception as e:
        print(f"Error indexing: {e}", file=sys.stderr)
        return 2


def cmd_report(args) -> int:
    """Generate daily digest report."""
    db_path = Path(args.db_path)
    if not db_path.exists():
        print(f"Error: index not found: {db_path}", file=sys.stderr)
        print("Run 'argus index' first.", file=sys.stderr)
        return 1

    # Webhook precedence: --discord-webhook flag, then ARGUS_DISCORD_WEBHOOK env.
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
    except Exception as e:
        print(f"Error generating report: {e}", file=sys.stderr)
        return 2


def cmd_query(args) -> int:
    """Query the index with a natural language question.

    Converts NL to SQL via qwen. Returns JSON results.
    """
    db_path = Path(args.db_path)
    if not db_path.exists():
        print(f"Error: index not found: {db_path}", file=sys.stderr)
        return 1

    question = args.query

    # Simple qwen-based NL to SQL translation
    try:
        import subprocess
        prompt = f"""
        Convert this question to a SQLite query against a table called 'events' with columns:
        ts, allowed, mode, rule_id, reason, escalation, agent, action_type, action_target,
        envelope_id, tier, cost_usd, input_bytes, tool_calls, model, role, workflow_id, fingerprint

        Question: {question}

        Return ONLY the SQL query, no explanation.
        """

        result = subprocess.run(
            ["ollama", "run", "qwen3.6:27b", prompt],
            capture_output=True,
            text=True,
            timeout=30,
        )

        if result.returncode != 0:
            print(f"Error from qwen: {result.stderr}", file=sys.stderr)
            return 2

        sql = _sanitize_readonly_select(result.stdout)
        if not sql:
            print("qwen returned no safe read-only SELECT query", file=sys.stderr)
            return 2

        # Execute query on a read-only SQLite connection.
        conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
        conn.execute("PRAGMA query_only = ON")
        conn.row_factory = sqlite3.Row
        try:
            rows = conn.execute(sql).fetchall()
            import json
            results = [dict(row) for row in rows]
            print(json.dumps(results, indent=2, default=str))
            return 0
        except Exception as e:
            print(f"Query error: {e}", file=sys.stderr)
            print(f"SQL: {sql}", file=sys.stderr)
            return 2
        finally:
            conn.close()

    except FileNotFoundError:
        print("Error: ollama not found. Install ollama and run: ollama pull qwen3.6:27b",
              file=sys.stderr)
        return 3



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
    forbidden = ("insert ", "update ", "delete ", "drop ", "alter ", "create ", "replace ", "attach ", "detach ", "pragma ", "vacuum ")
    padded = f" {lowered} "
    if any(token in padded for token in forbidden):
        return None
    return sql

def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    """Parse command-line arguments."""
    p = argparse.ArgumentParser(prog="argus", description="Observatory: tail, index, detect, report.")
    p.add_argument("--db-path", default=str(Path.home() / ".argus" / "index.db"),
                   help="Path to index.db")

    subparsers = p.add_subparsers(dest="cmd", required=True)

    # index subcommand
    index_p = subparsers.add_parser("index", help="Index gov-decisions JSONL files")
    index_p.add_argument("--decisions-dir",
                         default=str(Path.home() / ".chitin"),
                         help="Directory containing gov-decisions-*.jsonl")
    index_p.add_argument("--follow", action="store_true",
                         help="Stay running and index appended lines/date-rollover files")
    index_p.add_argument("--poll-seconds", type=float, default=1.0,
                         help="Polling interval for --follow")

    # report subcommand
    report_p = subparsers.add_parser("report", help="Generate daily digest report")
    report_p.add_argument("--report-dir",
                          default=str(Path.home() / ".chitin" / "reports"),
                          help="Output directory for reports")
    report_p.add_argument("--discord-webhook",
                          default=None,
                          help="Discord webhook URL for #ares summary (overrides ARGUS_DISCORD_WEBHOOK)")
    report_p.add_argument("--discord-always",
                          action="store_true",
                          help="Post Discord summary even on quiet days (default: skip if no findings)")

    # query subcommand
    query_p = subparsers.add_parser("query", help="Query index with NL question")
    query_p.add_argument("query", nargs="+", help="Natural language question")

    return p.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    """CLI entry point."""
    args = parse_args(argv)

    if args.cmd == "index":
        return cmd_index(args)
    elif args.cmd == "report":
        return cmd_report(args)
    elif args.cmd == "query":
        # Join query args back into a string
        args.query = " ".join(args.query)
        return cmd_query(args)

    return 1


if __name__ == "__main__":
    sys.exit(main())
