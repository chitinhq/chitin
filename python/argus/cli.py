"""CLI entry: argus command."""
from __future__ import annotations

import argparse
import sqlite3
import sys
from pathlib import Path

from argus.indexer import follow_all_decisions, tail_all_decisions
from argus.reporter import generate_daily_report
from argus.kanban_ingest import ingest_all_kanban
from argus.git_ingest import ingest_repo, discover_repos
from argus.log_ingest import ingest_logs


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

    # ingest-kanban subcommand
    ink_p = subparsers.add_parser(
        "ingest-kanban",
        help="Poll ~/.hermes/kanban/boards/*/kanban.db into the cross-source index",
    )
    ink_p.add_argument("--boards-root",
                       default=str(Path.home() / ".hermes" / "kanban" / "boards"),
                       help="Root directory containing per-board kanban DBs")
    ink_p.add_argument("--xs-db",
                       default=str(Path.home() / ".argus" / "cross_source.db"),
                       help="Cross-source SQLite index path")

    # ingest-git subcommand
    ing_p = subparsers.add_parser(
        "ingest-git",
        help="Poll commits + PRs for tracked repos into the cross-source index",
    )
    ing_p.add_argument("--repo", action="append", default=None,
                       help="Repo path to poll (repeatable). Default: auto-discover under --root")
    ing_p.add_argument("--root", action="append", default=None,
                       help="Directory to scan for child .git repos (repeatable)")
    ing_p.add_argument("--xs-db",
                       default=str(Path.home() / ".argus" / "cross_source.db"),
                       help="Cross-source SQLite index path")

    # ingest-logs subcommand
    inl_p = subparsers.add_parser(
        "ingest-logs",
        help="One-shot snapshot of hermes + openclaw logs into the cross-source index",
    )
    inl_p.add_argument("--hermes-logs",
                       default=str(Path.home() / ".hermes" / "logs"),
                       help="Directory containing hermes *.log files")
    inl_p.add_argument("--openclaw-logs",
                       default=str(Path.home() / ".openclaw" / "logs"),
                       help="Directory containing openclaw *.log files")
    inl_p.add_argument("--xs-db",
                       default=str(Path.home() / ".argus" / "cross_source.db"),
                       help="Cross-source SQLite index path")

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
    elif args.cmd == "ingest-kanban":
        return cmd_ingest_kanban(args)
    elif args.cmd == "ingest-git":
        return cmd_ingest_git(args)
    elif args.cmd == "ingest-logs":
        return cmd_ingest_logs(args)

    return 1


def cmd_ingest_logs(args) -> int:
    """Pull hermes + openclaw logs into the cross-source index (one-shot)."""
    xs_db = Path(args.xs_db)
    for source, root in (("hermes", Path(args.hermes_logs)),
                         ("openclaw", Path(args.openclaw_logs))):
        if not root.exists():
            print(f"log ingest: skip source={source} (missing dir {root})", file=sys.stderr)
            continue
        summary, _ = ingest_logs(root, source, xs_db)
        print(
            f"log ingest: source={source} files={len(summary['files'])} "
            f"inserted={summary['inserted']} skipped={summary['skipped']} "
            f"unparsed={summary['unparsed']}",
            file=sys.stderr,
        )
    return 0


def cmd_ingest_kanban(args) -> int:
    """Pull kanban events into the cross-source index."""
    boards_root = Path(args.boards_root)
    xs_db = Path(args.xs_db)
    if not boards_root.exists():
        print(f"Error: boards root not found: {boards_root}", file=sys.stderr)
        return 1
    result = ingest_all_kanban(boards_root, xs_db)
    print(f"kanban ingest: boards={result['boards']} inserted={result['inserted']} skipped={result['skipped']}",
          file=sys.stderr)
    return 0


def cmd_ingest_git(args) -> int:
    """Pull git commits + PRs into the cross-source index for the given repos."""
    xs_db = Path(args.xs_db)
    repos: list[Path] = []
    if args.repo:
        repos.extend(Path(p) for p in args.repo)
    if args.root:
        repos.extend(discover_repos([Path(r) for r in args.root]))
    if not repos:
        # Default: cwd if it's a repo
        cwd = Path.cwd()
        if (cwd / ".git").exists():
            repos.append(cwd)
    if not repos:
        print("Error: no repos to ingest. Pass --repo PATH or --root DIR.", file=sys.stderr)
        return 1
    for r in repos:
        result = ingest_repo(r, xs_db)
        print(
            f"git ingest: repo={result['repo']} "
            f"commits=+{result['commits_inserted']}/-{result['commits_skipped']} "
            f"prs=+{result['prs_inserted']}/-{result['prs_skipped']}",
            file=sys.stderr,
        )
    return 0


if __name__ == "__main__":
    sys.exit(main())
