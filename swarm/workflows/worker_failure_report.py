#!/usr/bin/env python3
"""Build operator-facing failure text from structured worker results."""

from __future__ import annotations

import argparse
import json
import sys


def build_failure_report(ticket_id: str, worker_result: dict) -> dict[str, str]:
    status = worker_result.get("status") or "unknown"
    exit_reason = worker_result.get("exit_reason") or "unknown"
    model = worker_result.get("model") or "unknown"
    driver = worker_result.get("driver") or "unknown"
    transcript_tail = (worker_result.get("transcript_tail") or "").strip()
    error = (worker_result.get("error") or "").strip()
    commit_count = worker_result.get("commit_count_ahead")

    details = [f"status={status}", f"exit_reason={exit_reason}", f"driver={driver}", f"model={model}"]
    if commit_count is not None:
        details.append(f"commits_ahead={commit_count}")

    message = f"🦞 {ticket_id}: worker stopped before PR open ({', '.join(details)})."
    if error:
        message += f" error={error}."
    if transcript_tail:
        compact_tail = transcript_tail.replace("\n", " | ")
        message += f" transcript_tail={compact_tail}"

    block_reason = f"worker {exit_reason}"
    return {"message": message, "block_reason": block_reason}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--ticket-id", required=True)
    args = parser.parse_args()
    worker_result = json.load(sys.stdin)
    json.dump(build_failure_report(args.ticket_id, worker_result), sys.stdout)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
