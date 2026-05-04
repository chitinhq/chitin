"""Tests for analysis.codex_mine — codex session JSONL ingest."""
from __future__ import annotations

import json
from pathlib import Path

from analysis.codex_mine import (
    Usage,
    extract_usage,
    iter_session_events,
    sessions_in,
    _to_action_type,
    _extract_target,
)


def _write_session(path: Path, events: list[dict]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w") as fh:
        for ev in events:
            fh.write(json.dumps(ev) + "\n")


def test_to_action_type_known_tools() -> None:
    assert _to_action_type("exec_command") == "shell.exec"
    assert _to_action_type("write_stdin") == "shell.exec"
    assert _to_action_type("read_file") == "file.read"
    assert _to_action_type("apply_patch") == "file.write"
    assert _to_action_type("fetch") == "http.request"
    assert _to_action_type("future_unknown_tool") == "unknown"


def test_extract_target_first_match() -> None:
    args = json.dumps({"command": "ls /tmp", "extra": "ignored"})
    assert _extract_target("exec_command", args) == "ls /tmp"
    args = json.dumps({"file_path": "/tmp/x", "content": "hi"})
    assert _extract_target("write_file", args) == "/tmp/x"
    # malformed JSON returns empty, doesn't raise
    assert _extract_target("?", "{not json") == ""


def test_iter_session_events_function_calls(tmp_path: Path) -> None:
    p = tmp_path / "rollout-test.jsonl"
    _write_session(p, [
        {"timestamp": "2026-05-04T00:30:37Z", "type": "session_meta",
         "payload": {"id": "chain-A", "cwd": "/work", "cli_version": "0.128.0", "model_provider": "openai"}},
        {"timestamp": "2026-05-04T00:30:38Z", "type": "response_item",
         "payload": {"type": "function_call", "name": "exec_command",
                     "arguments": json.dumps({"command": "ls /tmp"})}},
        {"timestamp": "2026-05-04T00:30:39Z", "type": "response_item",
         "payload": {"type": "function_call", "name": "exec_command",
                     "arguments": json.dumps({"command": "rm /tmp/x"})}},
        # non-decision events are still surfaced where applicable
        {"timestamp": "2026-05-04T00:30:40Z", "type": "event_msg",
         "payload": {"type": "exec_command_end", "exit_code": 0, "duration_ms": 12}},
    ])
    events = list(iter_session_events(p))
    # 1 task_start + 2 decision + 1 exec_result
    assert len(events) == 4
    assert events[0].event_type == "task_start"
    assert events[0].chain_id == "chain-A"
    decisions = [e for e in events if e.event_type == "decision"]
    assert len(decisions) == 2
    assert decisions[0].payload["action_type"] == "shell.exec"
    assert decisions[0].payload["action_target"] == "ls /tmp"
    assert decisions[0].chain_id == "chain-A"
    exec_result = [e for e in events if e.event_type == "exec_result"]
    assert len(exec_result) == 1
    assert exec_result[0].payload["exit_code"] == 0
    assert exec_result[0].payload["duration_ms"] == 12


def test_extract_usage_aggregates_rate_limits(tmp_path: Path) -> None:
    p1 = tmp_path / "session1.jsonl"
    _write_session(p1, [
        {"timestamp": "2026-05-04T00:30:37Z", "type": "session_meta",
         "payload": {"id": "chain-A", "cwd": "/", "cli_version": "0.128.0"}},
        {"timestamp": "2026-05-04T00:30:38Z", "type": "response_item",
         "payload": {"type": "function_call", "name": "exec_command", "arguments": "{}"}},
        {"timestamp": "2026-05-04T00:30:39Z", "type": "event_msg",
         "payload": {"type": "token_count", "rate_limits": {
             "plan_type": "plus",
             "rate_limit_reached_type": None,
             "primary": {"used_percent": 5.0, "window_minutes": 300, "resets_at": 1777872638},
             "secondary": {"used_percent": 0.5, "window_minutes": 10080, "resets_at": 1778459438},
         }}},
    ])
    p2 = tmp_path / "session2.jsonl"
    _write_session(p2, [
        {"timestamp": "2026-05-04T01:00:00Z", "type": "session_meta",
         "payload": {"id": "chain-B", "cwd": "/", "cli_version": "0.128.0"}},
        # later token_count should win
        {"timestamp": "2026-05-04T01:00:01Z", "type": "event_msg",
         "payload": {"type": "token_count", "rate_limits": {
             "plan_type": "plus",
             "rate_limit_reached_type": None,
             "primary": {"used_percent": 12.5, "window_minutes": 300, "resets_at": 1777872999},
             "secondary": {"used_percent": 1.2, "window_minutes": 10080, "resets_at": 1778459438},
         }}},
        {"timestamp": "2026-05-04T01:00:02Z", "type": "response_item",
         "payload": {"type": "function_call", "name": "read_file", "arguments": "{}"}},
        {"timestamp": "2026-05-04T01:00:03Z", "type": "response_item",
         "payload": {"type": "function_call", "name": "exec_command", "arguments": "{}"}},
    ])

    u = extract_usage([p1, p2])
    assert u.sessions_observed == 2
    assert u.plan_type == "plus"
    # latest by timestamp: session 2's 12.5%
    assert u.primary_used_percent == 12.5
    assert u.primary_window_minutes == 300
    assert u.secondary_used_percent == 1.2
    assert u.last_observed_ts == "2026-05-04T01:00:01Z"
    # function calls aggregate across sessions
    assert u.function_calls_total == 3
    assert u.function_calls_by_name == {"exec_command": 2, "read_file": 1}


def test_extract_usage_handles_missing_rate_limits(tmp_path: Path) -> None:
    """A session without token_count events leaves Usage at defaults
    rather than crashing."""
    p = tmp_path / "session.jsonl"
    _write_session(p, [
        {"timestamp": "2026-05-04T00:30:37Z", "type": "session_meta",
         "payload": {"id": "x", "cwd": "/"}},
        {"timestamp": "2026-05-04T00:30:38Z", "type": "response_item",
         "payload": {"type": "function_call", "name": "exec_command", "arguments": "{}"}},
    ])
    u = extract_usage([p])
    assert u.sessions_observed == 1
    assert u.primary_used_percent == 0.0
    assert u.function_calls_total == 1


def test_sessions_in_returns_sorted(tmp_path: Path) -> None:
    (tmp_path / "2026" / "05").mkdir(parents=True)
    (tmp_path / "2026" / "05" / "rollout-b.jsonl").write_text("{}\n")
    (tmp_path / "2026" / "05" / "rollout-a.jsonl").write_text("{}\n")
    (tmp_path / "2026" / "05" / "ignored.txt").write_text("nope")
    paths = sessions_in(tmp_path)
    assert len(paths) == 2
    assert paths[0].name == "rollout-a.jsonl"
    assert paths[1].name == "rollout-b.jsonl"


def test_sessions_in_missing_dir_returns_empty(tmp_path: Path) -> None:
    nonexistent = tmp_path / "not-here"
    assert sessions_in(nonexistent) == []


def test_iter_session_events_handles_corrupt_lines(tmp_path: Path) -> None:
    p = tmp_path / "session.jsonl"
    p.write_text(
        json.dumps({"timestamp": "2026-05-04T00:30:37Z", "type": "session_meta",
                    "payload": {"id": "chain-A", "cwd": "/"}}) + "\n"
        "this is not JSON\n"
        + json.dumps({"timestamp": "2026-05-04T00:30:39Z", "type": "response_item",
                      "payload": {"type": "function_call", "name": "exec_command",
                                  "arguments": "{}"}}) + "\n"
    )
    events = list(iter_session_events(p))
    # corrupt line skipped, others survive
    assert len(events) == 2  # task_start + 1 decision
    assert events[0].chain_id == "chain-A"
