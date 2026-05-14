#!/usr/bin/env python3
"""Build operator-facing failure text from gh pr create output."""

from __future__ import annotations

import argparse
import json
import sys

MAX_DETAIL_CHARS = 1024


def _compact_output(gh_output: str) -> str:
    lines = [line.strip() for line in gh_output.splitlines() if line.strip()]
    return " | ".join(lines)


def _truncate(text: str, limit: int = MAX_DETAIL_CHARS) -> str:
    if len(text) <= limit:
        return text
    return text[: limit - 3] + "..."


def build_pr_failure_report(ticket_id: str, branch: str, gh_output: str) -> dict[str, str]:
    compact_output = _truncate(_compact_output(gh_output))
    message = f"🦞 {ticket_id}: branch {branch} pushed, but gh pr create failed. Manual PR open needed."
    block_reason = f"branch {branch} pushed but gh pr create failed"
    if compact_output:
        message += f" gh_output={compact_output}"
        block_reason += f": {compact_output}"
    return {"message": message, "block_reason": block_reason}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--ticket-id", required=True)
    parser.add_argument("--branch", required=True)
    args = parser.parse_args()
    gh_output = sys.stdin.read()
    json.dump(build_pr_failure_report(args.ticket_id, args.branch, gh_output), sys.stdout)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
