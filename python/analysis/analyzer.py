"""Slice 5 analyzer: recent-session rubric -> SQLite suggestions.

Reads recent gov-decisions rows, derives bounded suggestions from a
deterministic rubric, optionally asks a frontier model to refine the
proposed diff/rationale, then upserts rows into ~/.chitin/analyzer.db.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import sqlite3
import sys
import time
import urllib.error
import urllib.request
from collections import Counter, defaultdict
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import yaml

from analysis.loaders import load_gov_decisions, parse_window_str
from analysis.models import Decision

ANTHROPIC_API_URL = "https://api.anthropic.com/v1/messages"
DEFAULT_SONNET_MODEL = os.environ.get("CHITIN_ANALYZER_SONNET_MODEL", "claude-sonnet-4-6")
DEFAULT_OPUS_MODEL = os.environ.get("CHITIN_ANALYZER_OPUS_MODEL", "claude-opus-4-7")
DEFAULT_TIMEOUT_SECONDS = int(os.environ.get("CHITIN_ANALYZER_TIMEOUT", "45"))
DB_SCHEMA = """
CREATE TABLE IF NOT EXISTS analyzer_suggestions (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    target TEXT NOT NULL,
    diff TEXT NOT NULL,
    rationale TEXT NOT NULL,
    applied INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_analyzer_suggestions_created_at
    ON analyzer_suggestions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_analyzer_suggestions_type_target
    ON analyzer_suggestions(type, target);
"""
SUGGESTION_TYPES = {"prompt_edit", "new_skill", "policy_rule", "route_tweak", "drop"}


@dataclass(frozen=True)
class Suggestion:
    type: str
    target: str
    diff: str
    rationale: str
    created_at: str
    confidence: float
    disagreement: bool = False
    high_stakes: bool = False

    @property
    def id(self) -> str:
        payload = "\n".join([self.type, self.target, self.diff])
        return hashlib.sha256(payload.encode("utf-8")).hexdigest()[:24]

    @property
    def model(self) -> str:
        if self.disagreement or self.high_stakes:
            return DEFAULT_OPUS_MODEL
        return DEFAULT_SONNET_MODEL


@dataclass(frozen=True)
class SessionSummary:
    session_id: str
    driver: str
    task_class: str
    started_at: datetime
    ended_at: datetime
    decisions: tuple[Decision, ...]
    deny_count: int
    cost_usd: float
    tool_counts: dict[str, int]


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(
        prog="analysis.analyzer",
        description="Analyze recent sessions and write suggestions to analyzer.db.",
    )
    p.add_argument("--window", default="24h", help="Window: e.g. 24h, 7d")
    p.add_argument("--decisions-dir", default=os.path.join(os.environ.get("HOME", "/"), ".chitin"))
    p.add_argument("--db-path", default=os.path.join(os.environ.get("HOME", "/"), ".chitin", "analyzer.db"))
    p.add_argument("--policy-file", default="chitin.yaml")
    p.add_argument("--now", default=None, help="ISO-8601 timestamp for deterministic tests")
    p.add_argument("--top-n", type=int, default=25, help="Maximum suggestions to keep per run")
    p.add_argument("--skip-llm", action="store_true", help="Skip frontier refinement even if API key is present")
    return p.parse_args(argv)


def _now_from_args(now_arg: str | None) -> datetime:
    if now_arg:
        now = datetime.fromisoformat(now_arg)
        if now.tzinfo is None:
            now = now.replace(tzinfo=timezone.utc)
        return now
    return datetime.now(tz=timezone.utc)


def _session_key(decision: Decision) -> str:
    if decision.workflow_id:
        return decision.workflow_id
    if decision.envelope_id:
        return decision.envelope_id
    agent = decision.agent or "unknown"
    return f"{agent}:{decision.ts.strftime('%Y-%m-%dT%H')}"


def _normalize_driver(agent: str | None) -> str:
    return (agent or "unknown").strip() or "unknown"


def _classify_task_class(items: list[Decision]) -> str:
    writes = sum(1 for d in items if (d.action_type or "").startswith("file.write"))
    reads = sum(1 for d in items if (d.action_type or "").startswith("file.read"))
    shell = sum(1 for d in items if d.action_type == "shell.exec")
    push = sum(1 for d in items if d.action_type == "git.push")
    denies = sum(1 for d in items if not d.allowed)
    if push > 0:
        return "integration"
    if writes + shell >= 4:
        return "code"
    if reads >= max(3, writes + shell):
        return "research"
    if denies >= 3:
        return "debug"
    return "general"


def build_sessions(decisions: list[Decision]) -> list[SessionSummary]:
    buckets: dict[str, list[Decision]] = defaultdict(list)
    for item in decisions:
        buckets[_session_key(item)].append(item)
    sessions: list[SessionSummary] = []
    for session_id, items in buckets.items():
        ordered = sorted(items, key=lambda d: d.ts)
        tool_counts = Counter((d.action_type or "unknown") for d in ordered)
        sessions.append(
            SessionSummary(
                session_id=session_id,
                driver=_normalize_driver(ordered[0].agent),
                task_class=_classify_task_class(ordered),
                started_at=ordered[0].ts,
                ended_at=ordered[-1].ts,
                decisions=tuple(ordered),
                deny_count=sum(1 for d in ordered if not d.allowed),
                cost_usd=sum(d.cost_usd for d in ordered),
                tool_counts=dict(tool_counts),
            )
        )
    return sorted(sessions, key=lambda s: s.ended_at, reverse=True)


def _median(values: list[float]) -> float:
    if not values:
        return 0.0
    vals = sorted(values)
    mid = len(vals) // 2
    if len(vals) % 2:
        return vals[mid]
    return (vals[mid - 1] + vals[mid]) / 2


def _safe_prefix(target: str | None) -> str:
    if not target:
        return ""
    target = target.strip()
    if not target:
        return ""
    if "/" in target:
        return target.split("/", 1)[0]
    if " " in target:
        return target.split(" ", 1)[0]
    return target[:24]


def _confidence(count: int, floor: float = 0.55) -> float:
    return min(0.95, floor + min(count, 8) * 0.05)


def detect_wasted_denials(decisions: list[Decision], now: datetime) -> list[Suggestion]:
    groups: dict[tuple[str, str], list[Decision]] = defaultdict(list)
    for d in decisions:
        if d.allowed:
            continue
        key = (d.rule_id or "<unknown>", _normalize_driver(d.agent))
        groups[key].append(d)

    suggestions: list[Suggestion] = []
    for (rule_id, driver), items in groups.items():
        ordered = sorted(items, key=lambda d: d.ts)
        if len(ordered) < 3:
            continue
        recent_hours = (now - ordered[0].ts).total_seconds() / 3600
        sample_targets = sorted({_safe_prefix(d.action_target) for d in ordered if _safe_prefix(d.action_target)})
        prompt_diff = (
            f"target: scripts/hermes/prompt-code.md\n"
            f"change: add a denial-recovery note for `{driver}` when `{rule_id}` fires.\n"
            f"note: after 2 repeats, stop retrying and switch to the suggested safe path.\n"
        )
        rationale = (
            f"{driver} hit `{rule_id}` {len(ordered)} times over {recent_hours:.1f}h"
            f"; sample targets: {', '.join(sample_targets[:3]) or 'n/a'}."
        )
        suggestions.append(
            Suggestion(
                type="prompt_edit",
                target=f"{driver}:{rule_id}",
                diff=prompt_diff,
                rationale=rationale,
                created_at=now.isoformat(),
                confidence=_confidence(len(ordered), floor=0.6),
            )
        )
        if rule_id not in {"<unknown>", "default"}:
            policy_diff = (
                f"rules:\n"
                f"  - id: {rule_id}-narrow-retry-path\n"
                f"    match:\n"
                f"      action_type: {ordered[0].action_type or 'unknown'}\n"
                f"    effect: guide\n"
                f"    reason: repeated denial pattern indicates the rule needs a narrower safe-path suggestion.\n"
            )
            suggestions.append(
                Suggestion(
                    type="policy_rule",
                    target=rule_id,
                    diff=policy_diff,
                    rationale=rationale,
                    created_at=now.isoformat(),
                    confidence=_confidence(len(ordered), floor=0.62),
                    disagreement=len(ordered) < 5,
                    high_stakes=True,
                )
            )
    return suggestions


def detect_tool_thrashing(sessions: list[SessionSummary], now: datetime) -> list[Suggestion]:
    suggestions: list[Suggestion] = []
    for session in sessions:
        tool, count = max(session.tool_counts.items(), key=lambda item: item[1], default=("unknown", 0))
        if count < 5:
            continue
        diff = (
            f"name: {tool.replace('.', '-')}-recovery\n"
            f"description: Reduce repeated `{tool}` calls in {session.driver} sessions.\n"
            f"body: |\n"
            f"  Before the fifth `{tool}` call in one session, summarize the failed attempts,\n"
            f"  confirm the next hypothesis, and batch the next edit or command.\n"
        )
        rationale = (
            f"Session {session.session_id} called `{tool}` {count} times; "
            f"this is consistent with tool thrashing rather than directed execution."
        )
        suggestions.append(
            Suggestion(
                type="new_skill",
                target=f"{session.driver}:{tool}",
                diff=diff,
                rationale=rationale,
                created_at=now.isoformat(),
                confidence=_confidence(count),
            )
        )
    return suggestions


def detect_cost_outliers(sessions: list[SessionSummary], now: datetime) -> list[Suggestion]:
    by_class: dict[str, list[SessionSummary]] = defaultdict(list)
    for session in sessions:
        by_class[session.task_class].append(session)

    suggestions: list[Suggestion] = []
    for task_class, items in by_class.items():
        median_cost = _median([s.cost_usd for s in items if s.cost_usd > 0])
        if median_cost <= 0:
            continue
        for session in items:
            if session.cost_usd <= median_cost * 2:
                continue
            diff = (
                "file: swarm/workflows/_pick_driver.py\n"
                f"change: bias {task_class} tasks toward the cheaper lane before frontier escalation.\n"
                f"guard: only escalate after a deny cluster or complexity=high.\n"
            )
            rationale = (
                f"Session {session.session_id} cost ${session.cost_usd:.2f}, "
                f"which is >2x the {task_class} median (${median_cost:.2f})."
            )
            suggestions.append(
                Suggestion(
                    type="route_tweak",
                    target=f"{task_class}:{session.driver}",
                    diff=diff,
                    rationale=rationale,
                    created_at=now.isoformat(),
                    confidence=0.7,
                    disagreement=session.task_class == "general",
                )
            )
    return suggestions


def detect_routing_failures(sessions: list[SessionSummary], now: datetime) -> list[Suggestion]:
    suggestions: list[Suggestion] = []
    for session in sessions:
        if session.driver != "claude-code":
            continue
        write_count = session.tool_counts.get("file.write", 0)
        shell_count = session.tool_counts.get("shell.exec", 0)
        read_count = session.tool_counts.get("file.read", 0)
        if write_count + shell_count < 4 or read_count > write_count + shell_count:
            continue
        diff = (
            "file: swarm/workflows/_pick_driver.py\n"
            "change: when code-writing dominates and no research-only capability is required, "
            "prefer codex before claude-code.\n"
        )
        rationale = (
            f"Session {session.session_id} ran under claude-code but looked codex-shaped "
            f"({write_count} writes, {shell_count} shell calls, {read_count} reads)."
        )
        suggestions.append(
            Suggestion(
                type="route_tweak",
                target="claude-code->codex",
                diff=diff,
                rationale=rationale,
                created_at=now.isoformat(),
                confidence=0.78,
                disagreement=True,
                high_stakes=True,
            )
        )
    return suggestions


def detect_stale_rules(policy_file: Path, decisions: list[Decision], now: datetime) -> list[Suggestion]:
    if not policy_file.exists():
        return []
    try:
        policy = yaml.safe_load(policy_file.read_text()) or {}
    except yaml.YAMLError:
        return []
    rules = policy.get("rules")
    if not isinstance(rules, list):
        return []

    fired = {d.rule_id for d in decisions if d.rule_id}
    suggestions: list[Suggestion] = []
    for rule in rules:
        if not isinstance(rule, dict):
            continue
        rule_id = str(rule.get("id") or "").strip()
        if not rule_id or rule_id in fired:
            continue
        diff = (
            "action: review for removal\n"
            f"rule_id: {rule_id}\n"
            "reason: no fires in the last 30d sample window.\n"
        )
        rationale = f"Rule `{rule_id}` did not fire in the 30d sample ending {now.date().isoformat()}."
        suggestions.append(
            Suggestion(
                type="drop",
                target=rule_id,
                diff=diff,
                rationale=rationale,
                created_at=now.isoformat(),
                confidence=0.66,
            )
        )
    return suggestions


def build_suggestions(decisions: list[Decision], policy_file: Path, now: datetime, top_n: int) -> list[Suggestion]:
    sessions = build_sessions(decisions)
    suggestions = [
        *detect_wasted_denials(decisions, now),
        *detect_tool_thrashing(sessions, now),
        *detect_cost_outliers(sessions, now),
        *detect_routing_failures(sessions, now),
        *detect_stale_rules(policy_file, decisions, now),
    ]
    deduped: dict[tuple[str, str], Suggestion] = {}
    for item in sorted(suggestions, key=lambda s: (s.confidence, s.created_at), reverse=True):
        deduped.setdefault((item.type, item.target), item)
    return list(deduped.values())[:top_n]


def _llm_available(skip_llm: bool) -> bool:
    return (not skip_llm) and bool(os.environ.get("ANTHROPIC_API_KEY"))


def _build_llm_prompt(suggestion: Suggestion) -> str:
    payload = {
        "type": suggestion.type,
        "target": suggestion.target,
        "candidate_diff": suggestion.diff,
        "candidate_rationale": suggestion.rationale,
        "confidence": suggestion.confidence,
        "disagreement": suggestion.disagreement,
        "high_stakes": suggestion.high_stakes,
    }
    return (
        "You are refining a local chitin analyzer suggestion. "
        "Return JSON only with keys diff and rationale. Keep the suggestion bounded.\n"
        + json.dumps(payload, indent=2, sort_keys=True)
    )


def refine_with_frontier(suggestions: list[Suggestion], *, skip_llm: bool) -> list[Suggestion]:
    if not _llm_available(skip_llm):
        return suggestions

    api_key = os.environ["ANTHROPIC_API_KEY"]
    refined: list[Suggestion] = []
    for suggestion in suggestions:
        body = {
            "model": suggestion.model,
            "max_tokens": 500,
            "temperature": 0,
            "system": "Return strict JSON only. Never add markdown fences.",
            "messages": [{"role": "user", "content": _build_llm_prompt(suggestion)}],
        }
        req = urllib.request.Request(
            ANTHROPIC_API_URL,
            data=json.dumps(body).encode("utf-8"),
            headers={
                "content-type": "application/json",
                "x-api-key": api_key,
                "anthropic-version": "2023-06-01",
            },
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=DEFAULT_TIMEOUT_SECONDS) as resp:
                payload = json.loads(resp.read().decode("utf-8"))
            text_blocks = payload.get("content") or []
            raw_text = "".join(
                block.get("text", "") for block in text_blocks
                if isinstance(block, dict) and block.get("type") == "text"
            ).strip()
            parsed = json.loads(raw_text)
            diff = str(parsed.get("diff") or suggestion.diff).strip() or suggestion.diff
            rationale = str(parsed.get("rationale") or suggestion.rationale).strip() or suggestion.rationale
            refined.append(
                Suggestion(
                    type=suggestion.type,
                    target=suggestion.target,
                    diff=diff,
                    rationale=f"{rationale} [frontier:{suggestion.model}]",
                    created_at=suggestion.created_at,
                    confidence=suggestion.confidence,
                    disagreement=suggestion.disagreement,
                    high_stakes=suggestion.high_stakes,
                )
            )
        except (urllib.error.URLError, TimeoutError, json.JSONDecodeError, KeyError, ValueError):
            refined.append(suggestion)
    return refined


def open_db(path: Path) -> sqlite3.Connection:
    path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path)
    conn.row_factory = sqlite3.Row
    conn.executescript(DB_SCHEMA)
    conn.commit()
    return conn


def write_suggestions(conn: sqlite3.Connection, suggestions: list[Suggestion]) -> None:
    for suggestion in suggestions:
        if suggestion.type not in SUGGESTION_TYPES:
            raise ValueError(f"unsupported suggestion type: {suggestion.type}")
        conn.execute(
            """
            INSERT INTO analyzer_suggestions (id, type, target, diff, rationale, applied, created_at)
            VALUES (?, ?, ?, ?, ?, 0, ?)
            ON CONFLICT(id) DO UPDATE SET
                type=excluded.type,
                target=excluded.target,
                diff=excluded.diff,
                rationale=excluded.rationale,
                created_at=excluded.created_at,
                applied=analyzer_suggestions.applied
            """,
            (
                suggestion.id,
                suggestion.type,
                suggestion.target,
                suggestion.diff,
                suggestion.rationale,
                suggestion.created_at,
            ),
        )
    conn.commit()


def run(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    now = _now_from_args(args.now)
    decisions_dir = Path(args.decisions_dir)
    if not decisions_dir.exists():
        print(f"Error: decisions-dir does not exist: {decisions_dir}", file=sys.stderr)
        return 2

    window = parse_window_str(args.window, now)
    result = load_gov_decisions(decisions_dir, window)
    decisions = result.decisions

    stale_window = parse_window_str("30d", now)
    stale_result = load_gov_decisions(decisions_dir, stale_window)
    policy_file = Path(args.policy_file)

    suggestions = build_suggestions(decisions, policy_file, now, args.top_n)
    if policy_file.exists():
        stale_suggestions = detect_stale_rules(policy_file, stale_result.decisions, now)
        merged = {(s.type, s.target): s for s in suggestions}
        for item in stale_suggestions:
            merged.setdefault((item.type, item.target), item)
        suggestions = list(merged.values())[: args.top_n]

    suggestions = refine_with_frontier(suggestions, skip_llm=args.skip_llm)
    frontier_enabled = _llm_available(args.skip_llm)
    conn = open_db(Path(args.db_path))
    try:
        write_suggestions(conn, suggestions)
    finally:
        conn.close()

    summary = {
        "window": args.window,
        "decision_count": len(decisions),
        "parse_errors": result.parse_errors,
        "suggestions_written": len(suggestions),
        "db_path": str(Path(args.db_path)),
        "frontier_enabled": frontier_enabled,
        "models_used": sorted({s.model for s in suggestions}) if frontier_enabled and suggestions else [],
        "generated_at": now.isoformat(),
    }
    print(json.dumps(summary, indent=2, sort_keys=True))
    return 0


def main() -> None:
    sys.exit(run())


if __name__ == "__main__":
    main()
