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


def _build_detail(gh_stdout: str, gh_stderr: str, exit_code: int | None) -> str:
    parts: list[str] = []
    compact_stdout = _compact_output(gh_stdout)
    compact_stderr = _compact_output(gh_stderr)
    if compact_stdout:
        parts.append(f"stdout={compact_stdout}")
    if compact_stderr:
        parts.append(f"stderr={compact_stderr}")
    if exit_code not in (None, 0):
        parts.append(f"exit_code={exit_code}")
    return _truncate(" ".join(parts))


def build_pr_failure_report(
    ticket_id: str,
    branch: str,
    gh_stdout: str,
    gh_stderr: str = "",
    exit_code: int | None = None,
) -> dict[str, str]:
    detail = _build_detail(gh_stdout, gh_stderr, exit_code)
    message = f"🦞 {ticket_id}: branch {branch} pushed, but gh pr create failed. Manual PR open needed."
    block_reason = f"branch {branch} pushed but gh pr create failed"
    if detail:
        message += f" {detail}"
        block_reason += f": {detail}"
    return {"message": message, "block_reason": block_reason}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--ticket-id", required=True)
    parser.add_argument("--branch", required=True)
    args = parser.parse_args()
    raw_input = sys.stdin.read()
    gh_stdout = raw_input
    gh_stderr = ""
    exit_code = None
    try:
        payload = json.loads(raw_input)
    except json.JSONDecodeError:
        payload = None
    if isinstance(payload, dict):
        gh_stdout = str(payload.get("stdout", ""))
        gh_stderr = str(payload.get("stderr", ""))
        raw_exit_code = payload.get("exit_code")
        if isinstance(raw_exit_code, int):
            exit_code = raw_exit_code
    json.dump(
        build_pr_failure_report(args.ticket_id, args.branch, gh_stdout, gh_stderr, exit_code),
        sys.stdout,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
