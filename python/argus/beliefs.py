"""Belief ingestion and normalization for Argus.

Adapters are read-only and explicit opt-in. They normalize persisted
agent memory and wiki/graph sources into the `beliefs` table.
"""
from __future__ import annotations

import hashlib
import json
import re
import sqlite3
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, Optional


_HEADING_RE = re.compile(r"^(#{1,6})\s+(.+?)\s*$")
_FRONTMATTER_RE = re.compile(r"\A---\n(.*?)\n---\n?", re.DOTALL)
_PRIVATE_PATH_RE = re.compile(r"(^|/)(dm|dms|direct-messages|personal)(/|$)", re.IGNORECASE)
_SENTENCE_BREAK_RE = re.compile(r"[.!?\n]")
_SUBJECT_TICKET_RE = re.compile(r"\bt_[a-z0-9]+\b", re.IGNORECASE)


@dataclass(frozen=True)
class Belief:
    agent: str
    subject: str
    claim: str
    ts_recorded: int
    source_path: str
    source_kind: str
    schema_version: str = "v1"
    private: bool = False


@dataclass(frozen=True)
class IngestAlert:
    adapter: str
    message: str


@dataclass(frozen=True)
class IngestResult:
    inserted: int
    skipped: int
    alerts: tuple[IngestAlert, ...]


def _belief_hash(belief: Belief) -> str:
    payload = "|".join([
        belief.agent,
        belief.subject,
        belief.claim,
        str(belief.ts_recorded),
        belief.source_path,
        belief.source_kind,
        belief.schema_version,
    ])
    return hashlib.sha256(payload.encode()).hexdigest()


def _insert_beliefs(conn: sqlite3.Connection, beliefs: Iterable[Belief]) -> tuple[int, int]:
    inserted = 0
    skipped = 0
    created_ts = int(time.time())
    for belief in beliefs:
        try:
            conn.execute(
                """
                INSERT INTO beliefs (
                    belief_hash, agent, subject, claim, ts_recorded,
                    source_path, source_kind, schema_version, private, created_ts
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    _belief_hash(belief),
                    belief.agent,
                    belief.subject,
                    belief.claim,
                    belief.ts_recorded,
                    belief.source_path,
                    belief.source_kind,
                    belief.schema_version,
                    int(belief.private),
                    created_ts,
                ),
            )
            inserted += 1
        except sqlite3.IntegrityError:
            skipped += 1
    conn.commit()
    return inserted, skipped


def _iter_markdownish_files(root: Path) -> Iterable[Path]:
    if root.is_file():
        yield root
        return
    if not root.exists():
        return
    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue
        if path.suffix.lower() not in {".md", ".markdown", ".txt", ".json"}:
            continue
        yield path


def _parse_frontmatter(text: str) -> tuple[dict[str, str], str]:
    match = _FRONTMATTER_RE.match(text)
    if not match:
        return {}, text
    frontmatter: dict[str, str] = {}
    for raw_line in match.group(1).splitlines():
        line = raw_line.strip()
        if not line or ":" not in line:
            continue
        key, value = line.split(":", 1)
        frontmatter[key.strip()] = value.strip().strip("'\"")
    return frontmatter, text[match.end():]


def _subject_from_text(source_path: str, text: str, *, fallback: str) -> str:
    ticket = _SUBJECT_TICKET_RE.search(text)
    if ticket:
        return ticket.group(0)
    first = _SENTENCE_BREAK_RE.split(text.strip(), maxsplit=1)[0].strip(" -:`#")
    if first:
        first = re.sub(r"\s+", " ", first)
        return first[:120]
    stem = Path(source_path).stem.replace("_", " ").strip()
    return stem or fallback


def _ts_from_path(path: Path) -> int:
    try:
        return _normalize_ts(path.stat().st_mtime)
    except OSError:
        return int(time.time())


def _normalize_ts(value: object) -> int:
    try:
        ts = float(value)
    except (TypeError, ValueError):
        return int(time.time())
    if ts > 10_000_000_000:
        ts /= 1000.0
    return int(ts)


def _is_private_path(path: str | Path) -> bool:
    return bool(_PRIVATE_PATH_RE.search(str(path)))


def _beliefs_from_markdown_text(
    *,
    agent: str,
    path: Path,
    text: str,
    source_kind: str,
    schema_version: str = "v1",
) -> list[Belief]:
    beliefs: list[Belief] = []
    frontmatter, body = _parse_frontmatter(text)
    ts_recorded = _ts_from_path(path)
    private = _is_private_path(path)

    subject = frontmatter.get("title") or _subject_from_text(str(path), body, fallback=agent)
    for key in ("title", "summary", "status", "owner", "priority"):
        value = frontmatter.get(key)
        if value:
            beliefs.append(
                Belief(
                    agent=agent,
                    subject=subject,
                    claim=f"{key}: {value}",
                    ts_recorded=ts_recorded,
                    source_path=str(path),
                    source_kind=source_kind,
                    schema_version=schema_version,
                    private=private,
                )
            )

    current_heading = ""
    paragraph_lines: list[str] = []

    def flush_paragraph() -> None:
        if not paragraph_lines:
            return
        paragraph = " ".join(line.strip() for line in paragraph_lines).strip()
        paragraph_lines.clear()
        if len(paragraph) < 12:
            return
        paragraph_subject = subject
        if current_heading and not _SUBJECT_TICKET_RE.fullmatch(subject):
            paragraph_subject = _subject_from_text(
                str(path),
                current_heading,
                fallback=subject,
            )
        claim = paragraph if not current_heading else f"{current_heading}: {paragraph}"
        beliefs.append(
            Belief(
                agent=agent,
                subject=paragraph_subject,
                claim=claim[:800],
                ts_recorded=ts_recorded,
                source_path=str(path),
                source_kind=source_kind,
                schema_version=schema_version,
                private=private,
            )
        )

    for raw_line in body.splitlines():
        line = raw_line.rstrip()
        heading_match = _HEADING_RE.match(line)
        if heading_match:
            flush_paragraph()
            current_heading = heading_match.group(2).strip()
            continue
        if not line.strip():
            flush_paragraph()
            continue
        paragraph_lines.append(line)
    flush_paragraph()
    return beliefs


def _beliefs_from_json_text(
    *,
    agent: str,
    path: Path,
    text: str,
    source_kind: str,
) -> list[Belief]:
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return []
    ts_recorded = _ts_from_path(path)
    private = _is_private_path(path)
    beliefs: list[Belief] = []
    if isinstance(data, dict):
        for key, value in data.items():
            if isinstance(value, (str, int, float, bool)):
                claim = f"{key}: {value}"
                beliefs.append(
                    Belief(
                        agent=agent,
                        subject=Path(path).stem,
                        claim=claim[:800],
                        ts_recorded=ts_recorded,
                        source_path=str(path),
                        source_kind=source_kind,
                        private=private,
                    )
                )
    return beliefs


def _read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8", errors="replace")


def ingest_hermes_memory(conn: sqlite3.Connection, roots: Optional[list[Path]] = None) -> IngestResult:
    roots = roots or [
        Path.home() / ".hermes" / "memories",
        Path.home() / ".hermes" / "memory",
    ]
    beliefs: list[Belief] = []
    alerts: list[IngestAlert] = []
    for root in roots:
        if not root.exists():
            continue
        for path in _iter_markdownish_files(root):
            if _is_private_path(path):
                continue
            try:
                text = _read_text(path)
            except OSError as exc:
                alerts.append(IngestAlert("hermes", f"skip unreadable {path}: {exc}"))
                continue
            if path.suffix.lower() == ".json":
                beliefs.extend(_beliefs_from_json_text(agent="hermes", path=path, text=text, source_kind="hermes_memory"))
            else:
                beliefs.extend(_beliefs_from_markdown_text(agent="hermes", path=path, text=text, source_kind="hermes_memory"))
    inserted, skipped = _insert_beliefs(conn, beliefs)
    return IngestResult(inserted=inserted, skipped=skipped, alerts=tuple(alerts))


def _meta_schema_version(mem_conn: sqlite3.Connection) -> str:
    try:
        row = mem_conn.execute("SELECT value FROM meta WHERE key = 'schema_version'").fetchone()
    except sqlite3.DatabaseError:
        return "legacy"
    if not row:
        return "legacy"
    value = row[0]
    return str(value) if value is not None else "legacy"


def ingest_openclaw_memory_db(
    conn: sqlite3.Connection,
    *,
    agent: str,
    db_path: Path,
    source_kind: str,
) -> IngestResult:
    if not db_path.exists():
        return IngestResult(inserted=0, skipped=0, alerts=())
    alerts: list[IngestAlert] = []
    try:
        mem_conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
        mem_conn.row_factory = sqlite3.Row
    except sqlite3.DatabaseError as exc:
        return IngestResult(
            inserted=0,
            skipped=0,
            alerts=(IngestAlert(agent, f"needs operator key or valid sqlite for {db_path}: {exc}"),),
        )

    beliefs: list[Belief] = []
    try:
        schema_version = _meta_schema_version(mem_conn)
        rows = mem_conn.execute(
            """
            SELECT path, text, updated_at
            FROM chunks
            WHERE source = 'memory'
            ORDER BY updated_at DESC
            """
        ).fetchall()
        for row in rows:
            source_path = row["path"]
            if _is_private_path(source_path):
                continue
            claim = str(row["text"] or "").strip()
            if len(claim) < 12:
                continue
            belief = Belief(
                agent=agent,
                subject=_subject_from_text(source_path, claim, fallback=agent),
                claim=claim[:800],
                ts_recorded=_normalize_ts(row["updated_at"] or time.time()),
                source_path=source_path,
                source_kind=source_kind,
                schema_version=schema_version,
                private=False,
            )
            beliefs.append(belief)
    except sqlite3.DatabaseError as exc:
        alerts.append(IngestAlert(agent, f"needs operator key or readable schema for {db_path}: {exc}"))
    finally:
        mem_conn.close()

    inserted, skipped = _insert_beliefs(conn, beliefs)
    return IngestResult(inserted=inserted, skipped=skipped, alerts=tuple(alerts))


def ingest_openclaw_agent_dir(
    conn: sqlite3.Connection,
    *,
    agent: str,
    roots: list[Path],
) -> IngestResult:
    beliefs: list[Belief] = []
    alerts: list[IngestAlert] = []
    for root in roots:
        if not root.exists():
            continue
        for path in _iter_markdownish_files(root):
            if _is_private_path(path):
                continue
            try:
                text = _read_text(path)
            except OSError as exc:
                alerts.append(IngestAlert(agent, f"skip unreadable {path}: {exc}"))
                continue
            if path.suffix.lower() == ".json":
                beliefs.extend(_beliefs_from_json_text(agent=agent, path=path, text=text, source_kind="openclaw_agent"))
            else:
                beliefs.extend(_beliefs_from_markdown_text(agent=agent, path=path, text=text, source_kind="openclaw_agent"))
    inserted, skipped = _insert_beliefs(conn, beliefs)
    return IngestResult(inserted=inserted, skipped=skipped, alerts=tuple(alerts))


def ingest_wiki_graph(conn: sqlite3.Connection, roots: Optional[list[Path]] = None) -> IngestResult:
    roots = roots or [
        Path.cwd() / "graphify-out" / "wiki",
        Path.cwd() / "wiki",
        Path.home() / ".openclaw" / "data" / "wiki",
    ]
    beliefs: list[Belief] = []
    alerts: list[IngestAlert] = []
    for root in roots:
        if not root.exists():
            continue
        for path in _iter_markdownish_files(root):
            if _is_private_path(path):
                continue
            try:
                text = _read_text(path)
            except OSError as exc:
                alerts.append(IngestAlert("wiki", f"skip unreadable {path}: {exc}"))
                continue
            beliefs.extend(
                _beliefs_from_markdown_text(
                    agent="wiki",
                    path=path,
                    text=text,
                    source_kind="wiki_graph",
                )
            )
    inserted, skipped = _insert_beliefs(conn, beliefs)
    return IngestResult(inserted=inserted, skipped=skipped, alerts=tuple(alerts))


def ingest_beliefs(
    conn: sqlite3.Connection,
    *,
    include_hermes: bool = False,
    include_clawta: bool = False,
    include_openclaw_agent: bool = False,
    include_wiki: bool = False,
) -> IngestResult:
    inserted = 0
    skipped = 0
    alerts: list[IngestAlert] = []

    if include_hermes:
        result = ingest_hermes_memory(conn)
        inserted += result.inserted
        skipped += result.skipped
        alerts.extend(result.alerts)

    if include_clawta:
        result = ingest_openclaw_memory_db(
            conn,
            agent="clawta",
            db_path=Path.home() / ".openclaw" / "memory" / "clawta.sqlite",
            source_kind="clawta_memory",
        )
        inserted += result.inserted
        skipped += result.skipped
        alerts.extend(result.alerts)

    if include_openclaw_agent:
        glm_results = [
            ingest_openclaw_memory_db(
                conn,
                agent="glm-agent",
                db_path=Path.home() / ".openclaw" / "memory" / "glm-agent.sqlite",
                source_kind="openclaw_agent_memory",
            ),
            ingest_openclaw_agent_dir(
                conn,
                agent="glm-agent",
                roots=[
                    Path.home() / ".openclaw" / "agents" / "glm-agent" / "agent",
                    Path.home() / ".openclaw" / "data" / "agents" / "glm-agent",
                ],
            ),
        ]
        for result in glm_results:
            inserted += result.inserted
            skipped += result.skipped
            alerts.extend(result.alerts)

    if include_wiki:
        result = ingest_wiki_graph(conn)
        inserted += result.inserted
        skipped += result.skipped
        alerts.extend(result.alerts)

    return IngestResult(inserted=inserted, skipped=skipped, alerts=tuple(alerts))
