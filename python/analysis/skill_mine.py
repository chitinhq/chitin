"""Mine ~/.chitin/events-*.jsonl for repeated workflow patterns.

Important: events files are keyed by `run_id`, NOT by session.
Each hook invocation gets a fresh run_id, so a single Claude Code
session spans many events-<run_id>.jsonl files. Mining n-grams
per FILE fragments one real workflow into dozens of tiny sequences.
This module groups events by `payload.chain_id` (the canonical
session id) across all files before mining.

Two-pass strategy:
  Pass 1 (coarse): action_type sequences. Useful only as a sanity
  check — confirms most coding sessions are file.read+shell.exec
  permutations, which doesn't help skill mining.

  Pass 2 (semantic): abstract action_target into a CANONICAL VERB
  (gh-pr-view, git-status, npm-test, edit-yaml, etc.) and mine
  n-grams over verbs. This is where real workflow shapes surface.

Invariants (see SPEC.md):
    I3  No network — pure function of events JSONL contents.
    I5  Bad JSONL lines silently skipped (post-hoc miner, not safety-critical).
    Determinism: outputs are sorted by `(n_sess * n, n_total)` desc with a
    stable input ordering — events sorted chronologically per chain. Two runs
    over identical files produce identical reports.

Boundaries:
    - Events with no `chain_id` → bucket under "<unknown>" (not dropped).
    - A chain with zero decision events → silently ignored (no rows yielded).
    - N-grams of all-same verbs or all-`read-*` / all-`edit-*` → trivial,
      filtered out (see `is_trivial`).
    - N-gram threshold: a candidate must appear in ≥3 distinct sessions to
      surface in the report. Single-session repeats are not skills.
    - No events files in CHITIN_DIR → exit 2.

Verb extraction rules:
  - shell.exec target starting with "gh "      → gh-<subcommand>
  - shell.exec target starting with "git "     → git-<subcommand>
  - shell.exec target starting with "rtk "     → strip prefix, recurse
  - shell.exec target starting with a tool     → <tool>
  - file.read with .yaml/.yml                  → read-yaml
  - file.read with .md                         → read-md
  - file.read with .ts/.tsx                    → read-ts
  - file.read with .py                         → read-py
  - file.read with .go                         → read-go
  - file.read other                            → read-other
  - file.write/edit by extension               → edit-<ext>
  - everything else                            → <action_type>
"""
from __future__ import annotations

import json
import re
import sys
from collections import Counter, defaultdict
from pathlib import Path

CHITIN_DIR = Path.home() / ".chitin"

# Tools whose first arg is the subcommand we want to keep.
SUBCOMMAND_TOOLS = {"gh", "git", "npm", "pnpm", "uv", "cargo", "go", "kubectl",
                    "docker", "systemctl", "journalctl", "chitin-kernel", "chitin"}

EXT_RE = re.compile(r"\.([a-zA-Z0-9]+)$")


def to_verb(action_type: str, target: str) -> str:
    target = (target or "").strip()
    # Strip rtk-prefix wrapper
    if target.startswith("rtk "):
        target = target[4:].strip()
    # shell.exec — extract command shape
    if action_type == "shell.exec":
        if not target:
            return "shell-empty"
        # Pull the first token (the command)
        first = target.split()[0] if target else ""
        # Pipes are signal; preserve "|" as a pseudo-token
        if first in SUBCOMMAND_TOOLS:
            tokens = target.split()
            if len(tokens) >= 2 and not tokens[1].startswith("-"):
                # gh pr view, git status, npm install, etc.
                sub = tokens[1]
                # Trim arg-shaped subs to just the verb
                sub = re.sub(r"[^a-zA-Z0-9_-]", "", sub)
                return f"{first}-{sub}" if sub else first
            return first
        # `cd workspace && something` — collapse to "shell-chain"
        if "&&" in target or "||" in target or ";" in target:
            return "shell-chain"
        # Bare cmd: just the first token, sanitized
        first = re.sub(r"[^a-zA-Z0-9_-]", "", first)
        return first or "shell-other"
    # file.read / file.write / etc.
    if action_type in ("file.read", "file.write"):
        m = EXT_RE.search(target.split()[0] if target else "")
        ext = (m.group(1) if m else "other").lower()
        # Common extensions get short canonical labels
        ext_map = {"yml": "yaml", "tsx": "ts", "jsx": "ts", "mjs": "js"}
        ext = ext_map.get(ext, ext)
        verb = "read" if action_type == "file.read" else "edit"
        return f"{verb}-{ext}"
    if action_type == "delegate.task":
        return "delegate"
    if action_type == "http.request":
        return "http"
    if action_type == "git.worktree.add":
        return "git-worktree-add"
    if action_type == "git.worktree.remove":
        return "git-worktree-remove"
    return action_type or "unknown"


def iter_decision_events(path: Path):
    """Yield (chain_id, ts, action_type, action_target) per decision.
    Tolerant of malformed lines (skips them)."""
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
        if ev.get("event_type") != "decision":
            continue
        payload = ev.get("payload") or {}
        # chain_id lives at the TOP level of the event (not in payload),
        # see go/execution-kernel/internal/event/event.go.
        chain_id = ev.get("chain_id") or ev.get("session_id") or ""
        ts = ev.get("ts") or ""
        at = payload.get("action_type") or "unknown"
        tgt = payload.get("action_target") or ""
        yield chain_id, ts, at, tgt


def collect_sessions(events_files):
    """Group decision events from ALL files by chain_id, ordered by ts.

    Returns dict[chain_id -> list[(verb, target)]]. Sessions with no
    chain_id (older log lines without it) are bucketed under "<unknown>"
    so they aren't silently dropped — but ranked separately later.
    """
    by_chain: defaultdict[str, list[tuple[str, str, str]]] = defaultdict(list)
    for path in events_files:
        for chain_id, ts, at, tgt in iter_decision_events(path):
            cid = chain_id or "<unknown>"
            by_chain[cid].append((ts, at, tgt))
    sessions: dict[str, list[tuple[str, str]]] = {}
    for cid, rows in by_chain.items():
        rows.sort(key=lambda r: r[0])  # chronological
        sessions[cid] = [(to_verb(at, tgt), tgt) for _, at, tgt in rows]
    return sessions


def ngrams(seq: list, n: int):
    for i in range(len(seq) - n + 1):
        yield tuple(seq[i : i + n])


def is_trivial(ngram: tuple[str, ...]) -> bool:
    if len(set(ngram)) == 1:
        return True
    # patterns dominated by a single read-* are noise too
    distinct_kinds = {x.split("-")[0] for x in ngram}
    if distinct_kinds == {"read"} or distinct_kinds == {"edit"}:
        return True
    return False


def main() -> int:
    events_files = sorted(CHITIN_DIR.glob("events-*.jsonl"))
    if not events_files:
        print(f"no events files in {CHITIN_DIR}", file=sys.stderr)
        return 2

    occurrences: Counter[tuple] = Counter()
    sessions: defaultdict[tuple, set[str]] = defaultdict(set)
    samples: defaultdict[tuple, list[tuple[str, ...]]] = defaultdict(list)

    sessions_with_data = 0
    total_decisions = 0
    verb_freq: Counter[str] = Counter()

    chain_sessions = collect_sessions(events_files)
    for chain_id, pairs in chain_sessions.items():
        seq = [v for v, _ in pairs]
        if not seq:
            continue
        sessions_with_data += 1
        total_decisions += len(seq)
        sid = chain_id[:8] if chain_id != "<unknown>" else "<unknown>"

        for v in seq:
            verb_freq[v] += 1

        for n in range(2, 6):
            for i, ng in enumerate(ngrams(seq, n)):
                if is_trivial(ng):
                    continue
                occurrences[ng] += 1
                sessions[ng].add(sid)
                if len(samples[ng]) < 3:
                    target_trail = tuple(t for _, t in pairs[i : i + n])
                    samples[ng].append(target_trail)

    candidates = [
        (ng, len(sessions[ng]), occurrences[ng], len(ng))
        for ng in occurrences
        if len(sessions[ng]) >= 3
    ]
    candidates.sort(key=lambda r: (r[1] * r[3], r[2]), reverse=True)

    print("# Skill-mining report (semantic verbs)")
    print()
    print(f"- sessions analyzed: {sessions_with_data}")
    print(f"- total decisions:   {total_decisions}")
    print(f"- distinct verbs:    {len(verb_freq)}")
    print(f"- candidate n-grams in ≥3 sessions: {len(candidates)}")
    print()

    print("## Verb frequency (top 30)")
    print()
    print("| verb | count |")
    print("|------|------:|")
    for verb, ct in verb_freq.most_common(30):
        print(f"| `{verb}` | {ct} |")
    print()

    print("## Top 30 n-gram candidates")
    print()
    print("Score = sessions × n. Patterns appearing in many sessions AND of substantial length.")
    print()
    print("| score | n | sessions | total | pattern |")
    print("|------:|--:|---------:|------:|---------|")
    for ng, n_sess, n_total, n in candidates[:30]:
        pattern = " → ".join(ng)
        print(f"| {n_sess * n} | {n} | {n_sess} | {n_total} | `{pattern}` |")
    print()

    print("## Sample target trails (top 15)")
    print()
    for ng, n_sess, n_total, n in candidates[:15]:
        pattern = " → ".join(ng)
        print(f"### `{pattern}` ({n_sess} sessions, {n_total} total)")
        print()
        for trail in samples[ng]:
            shown = " | ".join(t[:80] + "…" if len(t) > 80 else (t or "<empty>") for t in trail)
            print(f"- {shown}")
        print()

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
