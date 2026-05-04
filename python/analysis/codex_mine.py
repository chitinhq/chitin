"""Mine ~/.codex/sessions/**/*.jsonl into chitin-shaped chain events
and a per-session usage rollup.

Codex doesn't have a PreToolUse hook, so chitin can't gate codex
calls in real time. But codex DOES emit a structured session
JSONL with everything we need post-hoc:

  - session_meta:        cwd, model_provider, cli_version
  - event_msg.task_started:   turn_id, started_at, model_context_window
  - response_item.function_call:  tool name + arguments
  - event_msg.exec_command_end:   exit code, duration, stdout/stderr
  - event_msg.token_count:        rate_limits with used_percent,
                                  resets_at, window_minutes, plan_type
                                  ← THIS IS THE BUDGET API
  - event_msg.task_complete

This module projects each function_call into a chitin chain
decision event (action_type/action_target shape) and rolls up
the rate_limits into a per-driver usage record.

Public API:
    iter_session_events(path) -> list[ChainEvent]
    extract_usage(path) -> Usage
    sessions_in(dir) -> list[Path]

CLI:
    python -m analysis.codex_mine usage      # summarize quota
    python -m analysis.codex_mine ingest     # write chain JSONL
"""
from __future__ import annotations

import argparse
import json
import sys
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterable

CODEX_SESSIONS_ROOT = Path.home() / ".codex" / "sessions"


# ──────────────────────────────────────────────────────────────────
# Shapes
# ──────────────────────────────────────────────────────────────────

@dataclass(frozen=True)
class ChainEvent:
    """Subset of chitin chain.event.Event needed for ingest. The Go
    kernel writes the full envelope; this Python projection carries
    just enough to drive analytics + usage tracking."""
    ts: str
    chain_id: str
    event_type: str  # "decision" | "exec_result" | "task_start"
    payload: dict


@dataclass
class Usage:
    """Per-driver usage rollup. Codex's rate_limits structure
    exposes percent + reset times for two windows (primary 5h,
    secondary 1w). chitin-budget reads this to render a unified
    "% of cap" per driver across vendors."""
    driver: str = "codex"
    plan_type: str = ""
    primary_used_percent: float = 0.0
    primary_window_minutes: int = 0
    primary_resets_at: int = 0  # unix seconds
    secondary_used_percent: float = 0.0
    secondary_window_minutes: int = 0
    secondary_resets_at: int = 0
    rate_limit_reached_type: str | None = None
    last_observed_ts: str = ""
    sessions_observed: int = 0
    function_calls_total: int = 0
    function_calls_by_name: dict[str, int] = field(default_factory=dict)


# ──────────────────────────────────────────────────────────────────
# Loaders
# ──────────────────────────────────────────────────────────────────

def sessions_in(root: Path) -> list[Path]:
    """Return all rollout-*.jsonl session files under root."""
    if not root.exists():
        return []
    return sorted(root.rglob("rollout-*.jsonl"))


def _parse_session_meta(line_obj: dict) -> tuple[str, str]:
    """Returns (chain_id, cwd) from a session_meta event."""
    p = line_obj.get("payload") or {}
    return p.get("id", ""), p.get("cwd", "")


def iter_session_events(path: Path) -> Iterable[ChainEvent]:
    """Project each codex function_call into a chitin chain
    decision event. Yields events in source order."""
    if not path.exists():
        return
    chain_id = ""
    cwd = ""
    try:
        data = path.read_text(errors="replace")
    except OSError:
        return
    for line in data.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            ev = json.loads(line)
        except json.JSONDecodeError:
            continue

        ts = ev.get("timestamp", "")
        ev_type = ev.get("type", "")
        payload = ev.get("payload") or {}
        ptype = payload.get("type", "")

        if ev_type == "session_meta":
            chain_id, cwd = _parse_session_meta(ev)
            yield ChainEvent(
                ts=ts,
                chain_id=chain_id,
                event_type="task_start",
                payload={
                    "tool_name": "codex.session_start",
                    "action_type": "delegate.task",
                    "action_target": payload.get("cli_version", ""),
                    "decision": "allow",
                    "rule_id": "codex-post-hoc",
                    "cwd": cwd,
                    "model_provider": payload.get("model_provider", ""),
                },
            )
            continue

        if ev_type == "response_item" and ptype == "function_call":
            name = payload.get("name") or "unknown"
            args = payload.get("arguments") or ""
            target = _extract_target(name, args)
            yield ChainEvent(
                ts=ts,
                chain_id=chain_id,
                event_type="decision",
                payload={
                    "tool_name": name,
                    "action_type": _to_action_type(name),
                    "action_target": target,
                    "decision": "allow",
                    "rule_id": "codex-post-hoc",
                },
            )
            continue

        if ev_type == "event_msg" and ptype == "exec_command_end":
            yield ChainEvent(
                ts=ts,
                chain_id=chain_id,
                event_type="exec_result",
                payload={
                    "tool_name": "exec_command",
                    "exit_code": payload.get("exit_code", -1),
                    "duration_ms": payload.get("duration_ms"),
                    "session_id": chain_id,
                },
            )


def _to_action_type(name: str) -> str:
    """Codex tool names → chitin action types. Mirror of
    internal/driver/gemini/normalize.go but in Python because the
    miner runs post-hoc, not in the kernel hot path."""
    mapping = {
        "exec_command": "shell.exec",
        "shell": "shell.exec",
        "write_stdin": "shell.exec",
        "read_file": "file.read",
        "apply_patch": "file.write",
        "edit_file": "file.write",
        "search_replace": "file.write",
        "fetch": "http.request",
        "web_search": "http.request",
    }
    return mapping.get(name, "unknown")


def _extract_target(name: str, args: str) -> str:
    """Best-effort: parse the first relevant field out of the
    function_call arguments JSON. Codex's args are a JSON-encoded
    string; we don't fail on parse errors (post-hoc, not safety-
    critical)."""
    try:
        a = json.loads(args) if isinstance(args, str) else args or {}
    except json.JSONDecodeError:
        return ""
    if not isinstance(a, dict):
        return ""
    for key in ("command", "file_path", "path", "url", "query", "pattern"):
        v = a.get(key)
        if isinstance(v, list) and v:
            v = v[0]
        if isinstance(v, str):
            return v
    return ""


# ──────────────────────────────────────────────────────────────────
# Usage rollup
# ──────────────────────────────────────────────────────────────────

def extract_usage(paths: Iterable[Path]) -> Usage:
    """Walk all sessions; pull the latest rate_limits + sum
    function_call counts. Returns a Usage record suitable for
    chitin-budget's multi-axis schema."""
    u = Usage()
    latest_ts = ""
    for path in paths:
        if not path.exists():
            continue
        u.sessions_observed += 1
        try:
            data = path.read_text(errors="replace")
        except OSError:
            continue
        for line in data.splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                ev = json.loads(line)
            except json.JSONDecodeError:
                continue
            payload = ev.get("payload") or {}
            ptype = payload.get("type", "")
            if ev.get("type") == "response_item" and ptype == "function_call":
                u.function_calls_total += 1
                name = payload.get("name") or "?"
                u.function_calls_by_name[name] = u.function_calls_by_name.get(name, 0) + 1
            if ev.get("type") == "event_msg" and ptype == "token_count":
                rl = payload.get("rate_limits") or {}
                ts = ev.get("timestamp", "")
                if ts and ts > latest_ts:
                    latest_ts = ts
                    u.last_observed_ts = ts
                    u.plan_type = rl.get("plan_type", u.plan_type)
                    u.rate_limit_reached_type = rl.get("rate_limit_reached_type")
                    primary = rl.get("primary") or {}
                    u.primary_used_percent = float(primary.get("used_percent", u.primary_used_percent))
                    u.primary_window_minutes = int(primary.get("window_minutes", u.primary_window_minutes))
                    u.primary_resets_at = int(primary.get("resets_at", u.primary_resets_at))
                    secondary = rl.get("secondary") or {}
                    u.secondary_used_percent = float(secondary.get("used_percent", u.secondary_used_percent))
                    u.secondary_window_minutes = int(secondary.get("window_minutes", u.secondary_window_minutes))
                    u.secondary_resets_at = int(secondary.get("resets_at", u.secondary_resets_at))
    return u


def usage_to_dict(u: Usage) -> dict:
    return {
        "driver": u.driver,
        "plan_type": u.plan_type,
        "primary": {
            "used_percent": u.primary_used_percent,
            "window_minutes": u.primary_window_minutes,
            "resets_at": u.primary_resets_at,
        },
        "secondary": {
            "used_percent": u.secondary_used_percent,
            "window_minutes": u.secondary_window_minutes,
            "resets_at": u.secondary_resets_at,
        },
        "rate_limit_reached_type": u.rate_limit_reached_type,
        "last_observed_ts": u.last_observed_ts,
        "sessions_observed": u.sessions_observed,
        "function_calls_total": u.function_calls_total,
        "function_calls_by_name": u.function_calls_by_name,
    }


# ──────────────────────────────────────────────────────────────────
# CLI
# ──────────────────────────────────────────────────────────────────

def _humanize_resets_at(ts: int) -> str:
    if not ts:
        return "?"
    delta = ts - int(datetime.now(timezone.utc).timestamp())
    if delta <= 0:
        return "passed"
    h, m = divmod(delta // 60, 60)
    if h:
        return f"in {h}h{m:02d}m"
    return f"in {m}m"


def _cli_usage(args: argparse.Namespace) -> int:
    root = Path(args.sessions_dir).expanduser()
    paths = sessions_in(root)
    u = extract_usage(paths)
    if args.json:
        print(json.dumps(usage_to_dict(u), indent=2))
        return 0
    print(f"codex usage — {u.sessions_observed} sessions observed")
    print(f"  plan:                  {u.plan_type or '?'}")
    print(f"  primary  (5h):         {u.primary_used_percent:5.1f}% used, resets {_humanize_resets_at(u.primary_resets_at)}")
    print(f"  secondary (weekly):    {u.secondary_used_percent:5.1f}% used, resets {_humanize_resets_at(u.secondary_resets_at)}")
    if u.rate_limit_reached_type:
        print(f"  ⚠  rate-limit hit:     {u.rate_limit_reached_type}")
    print(f"  function calls total:  {u.function_calls_total}")
    if u.function_calls_by_name:
        print("  by tool:")
        for name, n in sorted(u.function_calls_by_name.items(), key=lambda kv: -kv[1]):
            print(f"    {n:5}  {name}")
    return 0


def _cli_ingest(args: argparse.Namespace) -> int:
    """Project codex function_calls into chitin events JSONL.
    Default output: ~/.chitin/codex-events-<chain_id>.jsonl per
    session (matches the kernel's per-chain file convention)."""
    root = Path(args.sessions_dir).expanduser()
    out_dir = Path(args.out_dir).expanduser()
    out_dir.mkdir(parents=True, exist_ok=True)

    paths = sessions_in(root)
    written = 0
    for path in paths:
        events = list(iter_session_events(path))
        if not events:
            continue
        chain_id = events[0].chain_id or path.stem
        out_path = out_dir / f"codex-events-{chain_id}.jsonl"
        with out_path.open("w") as fh:
            for ev in events:
                fh.write(json.dumps({
                    "ts": ev.ts,
                    "chain_id": ev.chain_id,
                    "event_type": ev.event_type,
                    "payload": ev.payload,
                }) + "\n")
        written += 1
    print(f"wrote {written} session(s) to {out_dir}")
    return 0


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="analysis.codex_mine")
    sub = p.add_subparsers(dest="cmd", required=True)

    pu = sub.add_parser("usage", help="summarize codex quota state")
    pu.add_argument("--sessions-dir", default=str(CODEX_SESSIONS_ROOT))
    pu.add_argument("--json", action="store_true")
    pu.set_defaults(func=_cli_usage)

    pi = sub.add_parser("ingest", help="project codex function_calls into chitin events JSONL")
    pi.add_argument("--sessions-dir", default=str(CODEX_SESSIONS_ROOT))
    pi.add_argument("--out-dir", default="~/.chitin")
    pi.set_defaults(func=_cli_ingest)

    args = p.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
