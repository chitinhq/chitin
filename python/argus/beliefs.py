"""Belief ingestion: index what each agent BELIEVES alongside what happened.

Slice 4 sources:
    - Operator agent cards (~/.openclaw/data/agent-cards/*.json) — per-agent
      capability claims, model lists, role declarations.
    - Clawta swarm_elo table (~/.openclaw/data/clawta.db) — router's
      learned belief about (driver, model) quality.
    - Wiki frontmatter (operator-curated truths) — markdown files with
      yaml frontmatter under a configurable wiki root.

Each adapter normalizes to:
    beliefs(agent, subject, claim, ts_recorded, source_path)

Invariants:
    - Strictly read-only. Adapters NEVER write back to agent state.
    - Per-adapter opt-in: explicit source list, no generic catch-all
      (privacy boundary per spec).
    - Idempotent on (agent, subject, claim, source_path) UNIQUE.
"""
from __future__ import annotations

import hashlib
import json
import re
import sqlite3
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterable, Optional


@dataclass(frozen=True)
class Belief:
    agent: str
    subject: str
    claim: str
    ts_recorded: int
    source_path: str


def init_beliefs_table(xs_conn: sqlite3.Connection) -> None:
    """Create the beliefs table + indexes. Idempotent."""
    xs_conn.execute("""
        CREATE TABLE IF NOT EXISTS beliefs (
            id INTEGER PRIMARY KEY,
            agent TEXT NOT NULL,
            subject TEXT NOT NULL,
            claim TEXT NOT NULL,
            ts_recorded INTEGER NOT NULL,
            source_path TEXT NOT NULL,
            dedup_key TEXT UNIQUE NOT NULL
        )
    """)
    xs_conn.execute("CREATE INDEX IF NOT EXISTS idx_beliefs_agent   ON beliefs(agent)")
    xs_conn.execute("CREATE INDEX IF NOT EXISTS idx_beliefs_subject ON beliefs(subject)")
    xs_conn.execute("CREATE INDEX IF NOT EXISTS idx_beliefs_ts      ON beliefs(ts_recorded)")
    xs_conn.commit()


def _dedup_key(agent: str, subject: str, claim: str, source_path: str) -> str:
    raw = f"{agent}|{subject}|{claim}|{source_path}".encode()
    return hashlib.sha256(raw).hexdigest()[:24]


def _insert(xs_conn: sqlite3.Connection, b: Belief) -> bool:
    """Insert a Belief idempotently. Returns True iff new row."""
    try:
        xs_conn.execute(
            """
            INSERT INTO beliefs (agent, subject, claim, ts_recorded, source_path, dedup_key)
            VALUES (?, ?, ?, ?, ?, ?)
            """,
            (b.agent, b.subject, b.claim, b.ts_recorded, b.source_path,
             _dedup_key(b.agent, b.subject, b.claim, b.source_path)),
        )
        return True
    except sqlite3.IntegrityError:
        return False


# ---------------------------------------------------------------------------
# Agent card adapter
# ---------------------------------------------------------------------------

def _agent_card_beliefs(card_path: Path) -> Iterable[Belief]:
    """Parse one agent card JSON and emit Beliefs."""
    try:
        with card_path.open("r") as f:
            data = json.load(f)
    except (OSError, json.JSONDecodeError):
        return []
    if not isinstance(data, dict):
        return []
    agent = str(data.get("id") or card_path.stem)
    ts_recorded = int(card_path.stat().st_mtime)
    source = str(card_path)

    beliefs: list[Belief] = []
    desc = data.get("description")
    if isinstance(desc, str) and desc.strip():
        beliefs.append(Belief(agent, "self.description", desc.strip(),
                              ts_recorded, source))
    for cap in (data.get("capabilities") or []):
        if not isinstance(cap, dict):
            continue
        skill = cap.get("skill")
        depth = cap.get("depth")
        if isinstance(skill, str) and isinstance(depth, str):
            beliefs.append(Belief(agent, f"capability.{skill}", depth,
                                  ts_recorded, source))
    for mdl in (data.get("models") or []):
        if not isinstance(mdl, dict):
            continue
        mid = mdl.get("id")
        tier = mdl.get("tier")
        cost = mdl.get("premium_cost")
        if isinstance(mid, str):
            claim = f"tier={tier}"
            if cost is not None:
                claim += f"; premium_cost={cost}"
            beliefs.append(Belief(agent, f"model.{mid}", claim,
                                  ts_recorded, source))
    return beliefs


def ingest_agent_cards(cards_root: Path, xs_conn: sqlite3.Connection) -> dict:
    """Index every <agent-id>.json under cards_root."""
    if not cards_root.exists():
        return {"inserted": 0, "skipped": 0, "agents": []}
    inserted = skipped = 0
    agents: list[str] = []
    for path in sorted(cards_root.glob("*.json")):
        # Skip backups and disabled cards.
        if any(suffix in path.name for suffix in (".bak", ".disabled")):
            continue
        agents.append(path.stem)
        for b in _agent_card_beliefs(path):
            if _insert(xs_conn, b):
                inserted += 1
            else:
                skipped += 1
    xs_conn.commit()
    return {"inserted": inserted, "skipped": skipped, "agents": agents}


# ---------------------------------------------------------------------------
# Clawta swarm_elo adapter
# ---------------------------------------------------------------------------

def ingest_clawta_swarm_elo(clawta_db: Path, xs_conn: sqlite3.Connection) -> dict:
    """Snapshot the swarm_elo table as router's belief about each (driver, model).

    Read-only on the source — opens the clawta DB in mode=ro.
    """
    if not clawta_db.exists():
        return {"inserted": 0, "skipped": 0}
    src = sqlite3.connect(f"file:{clawta_db}?mode=ro", uri=True, timeout=2.0)
    src.row_factory = sqlite3.Row
    inserted = skipped = 0
    try:
        rows = src.execute(
            """
            SELECT driver, model, role, task_class, elo_score,
                   dispatches_count, last_updated
            FROM swarm_elo
            """
        ).fetchall()
    except sqlite3.OperationalError:
        # Schema mismatch on this box — surface zero, don't crash the pipeline.
        return {"inserted": 0, "skipped": 0, "error": "swarm_elo missing"}
    finally:
        src.close()

    for row in rows:
        subject = f"driver:{row['driver']}/model:{row['model']}"
        role = row["role"] or "<any>"
        tcls = row["task_class"] or "<any>"
        claim = (
            f"role={role}; task_class={tcls}; "
            f"elo={row['elo_score']:.1f}; dispatches={row['dispatches_count']}"
        )
        b = Belief(
            agent="router",
            subject=subject,
            claim=claim,
            ts_recorded=int(row["last_updated"]),
            source_path=str(clawta_db),
        )
        if _insert(xs_conn, b):
            inserted += 1
        else:
            skipped += 1
    xs_conn.commit()
    return {"inserted": inserted, "skipped": skipped}


# ---------------------------------------------------------------------------
# Wiki frontmatter adapter
# ---------------------------------------------------------------------------

_FRONTMATTER_RE = re.compile(r"^---\n(.*?)\n---\n", re.DOTALL)


def _wiki_beliefs(md_path: Path) -> Iterable[Belief]:
    """Parse a markdown file's yaml frontmatter as a set of operator beliefs."""
    try:
        text = md_path.read_text(errors="replace")
    except (OSError, UnicodeDecodeError):
        return []
    m = _FRONTMATTER_RE.match(text)
    ts_recorded = int(md_path.stat().st_mtime)
    source = str(md_path)
    if m is None:
        # Degraded: emit a single belief that records the article title
        # (first '# ' heading) so the article shows up in drift surveys.
        for line in text.splitlines():
            if line.startswith("# "):
                return [Belief("operator", f"wiki:{md_path.stem}",
                               f"title={line[2:].strip()}", ts_recorded, source)]
        return []
    beliefs: list[Belief] = []
    body = m.group(1)
    for line in body.splitlines():
        if ":" not in line:
            continue
        key, _, value = line.partition(":")
        key = key.strip()
        value = value.strip().strip('"').strip("'")
        if not key:
            continue
        beliefs.append(Belief(
            agent="operator",
            subject=f"wiki:{md_path.stem}/{key}",
            claim=value,
            ts_recorded=ts_recorded,
            source_path=source,
        ))
    return beliefs


def ingest_wiki(wiki_root: Path, xs_conn: sqlite3.Connection) -> dict:
    """Walk a wiki root for *.md files and ingest frontmatter as beliefs."""
    if not wiki_root.exists():
        return {"inserted": 0, "skipped": 0, "files": 0}
    inserted = skipped = files = 0
    for path in sorted(wiki_root.rglob("*.md")):
        if not path.is_file():
            continue
        files += 1
        for b in _wiki_beliefs(path):
            if _insert(xs_conn, b):
                inserted += 1
            else:
                skipped += 1
    xs_conn.commit()
    return {"inserted": inserted, "skipped": skipped, "files": files}
