#!/usr/bin/env python3
"""Bounded board watchdog for Hermes cron.

Deterministic replacement for the LLM board-watchdog prompt. It uses direct
SQLite reads plus a tiny fixed number of subprocess checks, so runtime does not
scale with LLM tool-call behavior.
"""

from __future__ import annotations

import datetime as dt
import json
import os
import re
import sqlite3
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

# Paths are parameterized via env vars with sensible defaults so the script
# works on any operator box without source edits (Constitution §6).
KANBAN_BOARDS_DIR = Path(os.environ.get(
    "KANBAN_BOARDS_DIR",
    str(Path.home() / ".hermes" / "kanban" / "boards"),
))
WORKSPACE_ROOT = Path(os.environ.get(
    "WORKSPACE_ROOT",
    str(Path.home() / "workspace"),
))
CHITIN_REPO = Path(os.environ.get(
    "CHITIN_REPO",
    str(WORKSPACE_ROOT / "chitin"),
))

BOARDS = {
    "chitin": {
        "db": KANBAN_BOARDS_DIR / "chitin" / "kanban.db",
        "spec_root": CHITIN_REPO / ".specify" / "specs",
    },
    "readybench": {
        "db": KANBAN_BOARDS_DIR / "readybench" / "kanban.db",
        # bench-devs-platform/.specify/specs is the source of truth. The
        # earlier workspace-overlay path treated wjcmurphy/bench-devs-platform
        # as a team repo, so the watchdog couldn't find readybench specs and
        # demoted every ready ticket back to triage. Per board-config
        # owned_orgs=wjcmurphy this is an owned repo; the poller (via
        # board_resolver.spec_dir_for_board) already resolves correctly.
        # Follow-up: unify both code paths through board_resolver (filed as
        # a separate spec) so this hardcoded BOARDS dict stops drifting.
        "spec_root": WORKSPACE_ROOT / "bench-devs-platform" / ".specify" / "specs",
    },
}

AUTHOR = "board-watchdog"
NOW = dt.datetime.now(dt.timezone.utc)
SINCE = int((NOW - dt.timedelta(hours=24)).timestamp())
TICKET_RE = re.compile(r"(^|[^A-Za-z0-9_])({})([^A-Za-z0-9_]|$)")
FORWARD_RE = re.compile(r"(?:^|[\s`])(?:[^\s`]*?)?\.specify/specs/([^\s`]+?/spec\.md)")
DEPENDENCY_RE = re.compile(r"blocked until|depends on|dependency|tracked in t_[A-Za-z0-9_]+", re.I)


@dataclass
class Task:
    id: str
    title: str
    status: str
    assignee: str | None
    body: str
    block_reason: str | None


def connect(board: str) -> sqlite3.Connection:
    con = sqlite3.connect(BOARDS[board]["db"])
    con.row_factory = sqlite3.Row
    return con


def load_tasks(board: str) -> list[Task]:
    con = connect(board)
    rows = con.execute(
        """
        SELECT id, title, status, assignee, COALESCE(body,'') AS body,
               COALESCE(block_reason,'') AS block_reason
        FROM tasks
        WHERE status IN ('blocked','triage')
        ORDER BY priority DESC, created_at ASC, id ASC
        """
    ).fetchall()
    return [Task(**dict(r)) for r in rows]


def transition_count(board: str, ticket_id: str) -> int:
    con = connect(board)
    row = con.execute(
        """
        SELECT COUNT(*) AS c FROM task_events
        WHERE task_id=? AND kind IN ('status_transition','promoted','demoted','blocked','unblocked')
          AND created_at >= ?
        """,
        (ticket_id, SINCE),
    ).fetchone()
    return int(row["c"] if row else 0)


def spec_files(root: Path) -> list[Path]:
    if not root.exists():
        return []
    return sorted(root.glob("*/spec.md"))


def find_reverse_specs(root: Path, ticket_id: str) -> list[Path]:
    """Find reverse spec bindings, ignoring incidental Out-of-Scope mentions."""
    pat = re.compile(rf"(^|[^A-Za-z0-9_]){re.escape(ticket_id)}([^A-Za-z0-9_]|$)")
    out = []
    for p in spec_files(root):
        try:
            in_out_of_scope = False
            accepted = False
            for raw in p.read_text(errors="replace").splitlines():
                line = raw.strip()
                if line.startswith("## "):
                    in_out_of_scope = "out of scope" in line.lower()
                if not pat.search(raw):
                    continue
                # A ticket mentioned only as explicitly out-of-scope is not a
                # dispatch anchor for that ticket.
                if in_out_of_scope or "out-of-scope" in raw.lower() or "that's " in raw.lower():
                    continue
                accepted = True
                break
            if accepted:
                out.append(p)
        except OSError:
            pass
    return out


def find_forward_specs(root: Path, body: str) -> list[Path]:
    out = []
    for m in FORWARD_RE.finditer(body or ""):
        candidate = root / m.group(1)
        if candidate.exists() and candidate.name == "spec.md":
            out.append(candidate)
    return out


def dependency_lines(paths: Iterable[Path]) -> list[str]:
    lines: list[str] = []
    for p in paths:
        try:
            for line in p.read_text(errors="replace").splitlines():
                if DEPENDENCY_RE.search(line):
                    clean = line.strip(" -\t")
                    if clean and clean not in lines:
                        lines.append(clean[:180])
        except OSError:
            continue
    return lines[:4]


def tracking_epic(t: Task) -> bool:
    text = f"{t.title}\n{t.body}\n{t.block_reason or ''}".lower()
    return any(s in text for s in ["tracking-epic", "tracking epic", "no_dispatch", "epic/parent", "container"])


def reviewed_readybench_spec(t: Task, specs: list[Path]) -> bool:
    # Workspace-side specs require an explicit approval signal; keep this strict.
    text = f"{t.body}\n{t.block_reason or ''}".lower()
    return bool(specs) and ("operator-approved" in text or "clawta-approved" in text or "reviewed-spec: true" in text)


def command_available_check() -> str:
    try:
        proc = subprocess.run(
            [str(CHITIN_REPO / "bin" / "chitin-kernel"), "drivers", "list", "--json"],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=8,
        )
    except Exception as e:
        return f"check failed: {e}"
    out = (proc.stdout or "").strip().splitlines()[:2]
    if proc.returncode == 0:
        return "available"
    if out:
        return out[0][:120]
    return f"exit {proc.returncode}"


def classify(board: str, t: Task, kernel_check: str) -> tuple[str, str, list[Path]]:
    root = BOARDS[board]["spec_root"]
    reverse = find_reverse_specs(root, t.id)
    forward = find_forward_specs(root, t.body)
    specs = sorted(set(reverse + forward))

    if tracking_epic(t):
        return "tracking epic", "container/no_dispatch; keep blocked, child tickets carry work", specs

    if board == "readybench" and specs and not reviewed_readybench_spec(t, specs):
        return "needs spec review", "workspace spec exists but no operator/Clawta approval signal", specs

    deps = dependency_lines(specs)
    if deps:
        reason = "; ".join(deps)
        if "chitin-kernel drivers list --json" in reason:
            reason += f" (current check: {kernel_check})"
        return "dependency-blocked", reason, specs

    if specs:
        return "spec-satisfied", "reviewed spec binding found; no dependency marker detected", specs

    if board == "readybench":
        # Be explicit about the known partial/empty directories without creating files.
        if t.id == "t_dbedef94":
            return "needs reviewed spec", "001-cra-to-vite-migration has no valid reviewed spec.md", specs
        if t.id == "t_d5ce1032":
            return "needs reviewed spec", "004-portal-scaffold has no valid reviewed spec.md", specs

    return "needs spec", "no reviewed .specify/specs/*/spec.md binding", specs


def maybe_update_dependency_block(board: str, t: Task, classification: str, reason: str) -> str:
    """Idempotently replace stale manual-spec loop blocks with dependency reasons."""
    if classification != "dependency-blocked":
        return ""
    if "dependency gate:" in (t.block_reason or ""):
        return ""
    if "promote-demote loop detected: needs manual spec" not in (t.block_reason or ""):
        return ""
    new_reason = f"dependency gate: {reason[:220]}"
    env = os.environ.copy()
    env["KANBAN_BOARD"] = board
    try:
        subprocess.run(
            [str(CHITIN_REPO / "scripts" / "kanban-flow"), "block", t.id, new_reason, "--author", AUTHOR],
            cwd=str(CHITIN_REPO),
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=12,
            check=False,
        )
        subprocess.run(
            ["hermes", "kanban", "--board", board, "assign", t.id, "red"],
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=8,
            check=False,
        )
        return "updated stale manual-spec block → dependency gate"
    except Exception as e:
        return f"update failed: {e}"


def report_board(board: str) -> tuple[list[str], int]:
    tasks = load_tasks(board)
    kernel_check = command_available_check() if board == "chitin" else "not checked"
    lines = [f"### {board} board", ""]
    if board == "chitin":
        lines.append(f"Dependency probe: `chitin-kernel drivers list --json` → `{kernel_check}`")
        lines.append("")

    rows = []
    spec_queue = []
    actions = []
    for t in tasks:
        count = transition_count(board, t.id)
        loop = "yes" if (count >= 3 or "promote-demote loop" in (t.block_reason or "")) else "no"
        cls, reason, specs = classify(board, t, kernel_check)
        action = maybe_update_dependency_block(board, t, cls, reason)
        if action:
            actions.append(f"- `{t.id}`: {action}")
        spec_label = ", ".join(str(p.relative_to(BOARDS[board]["spec_root"]).parent) for p in specs[:2]) or "—"
        rows.append((t.id, t.title, loop, cls, reason, spec_label))
        if cls in {"needs spec", "needs reviewed spec", "needs spec review"}:
            spec_queue.append((t.id, t.title, reason))

    # Loop-detection table — show only loop-flagged ("yes") tickets inline,
    # capped at 3 so the cron transport doesn't split the report into
    # multiple Discord messages. Full detail via the kanban CLI.
    # (See PR #726 + ticket t_74c2cab6 for the broader noise-reduction work.)
    loop_rows = [r for r in rows if r[2] == "yes"]
    inline_loop = loop_rows[:3]
    lines.append(
        f"Blocked/triage tickets examined: **{len(tasks)}** "
        f"({len(loop_rows)} loop-flagged)"
    )
    if inline_loop:
        lines.append("")
        lines.append("| Ticket | Classification | Finding |")
        lines.append("|---|---|---|")
        for tid, title, loop, cls, reason, spec_label in inline_loop:
            safe_title = title.replace("|", "/")[:50]
            safe_reason = reason.replace("|", "/")[:80]
            lines.append(f"| `{tid}` {safe_title} | {cls} | {safe_reason} |")
        if len(loop_rows) > 3:
            lines.append(
                f"… +{len(loop_rows) - 3} more loop-flagged tickets — "
                f"run `hermes kanban --board {board} ls --status blocked` for the full set"
            )

    if actions:
        lines.extend(["", "**Actions taken**", *actions])
    else:
        lines.extend(["", "**Actions taken**: none"])

    # Spec queue is the dominant inbox noise source. Cap inline output at the
    # top 3 and point at the kanban CLI for the rest. Without this, the
    # cron transport splits a long enumeration into 5+ Discord messages
    # every 10 minutes and buries operator messages in agent inboxes
    # (see ticket t_74c2cab6).
    lines.extend(["", f"🔔 **Spec queue — {board}**: {len(spec_queue)} tickets need specs/review"])
    if spec_queue:
        top = spec_queue[:3]
        for tid, title, reason in top:
            short_title = (title[:50] + "…") if len(title) > 50 else title
            short_reason = (reason[:80] + "…") if len(reason) > 80 else reason
            lines.append(f"- `{tid}`: {short_title} — {short_reason}")
        if len(spec_queue) > 3:
            lines.append(
                f"- … +{len(spec_queue) - 3} more — "
                f"run `hermes kanban --board {board} ls --status blocked --assignee red` for the full set"
            )
    else:
        lines.append("- none")
    lines.append("")
    return lines, len(tasks)


def main() -> int:
    lines = [
        f"## Board Watchdog Report — {NOW.strftime('%Y-%m-%d %H:%M UTC')}",
        "",
        "Deterministic bounded-script run (no LLM tool loop). No `.specify/` writes or ticket body edits.",
        "",
    ]
    examined = 0
    for board in ["chitin", "readybench"]:
        board_lines, n = report_board(board)
        lines.extend(board_lines)
        examined += n
    lines.extend([
        "---",
        f"Examined {examined} blocked/triage tickets across {len(BOARDS)} boards.",
        "Completed normally.",
    ])
    print("\n".join(lines))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
