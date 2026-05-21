"""Log and Discord ingestion for Argus Slice 3."""
from __future__ import annotations

import json
import os
import re
import sqlite3
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterable, Protocol

from chitin_telemetry import llm, migrations
from chitin_telemetry.indexer import (
    checkpoint_key,
    env_path,
    file_inode,
    init_db,
    insert_source_event,
    _parse_ts_unix,
    _split_complete_lines,
)

DEFAULT_PATTERN_TIMEOUT_SECONDS = int(os.environ.get("ARGUS_PATTERN_TIMEOUT_SECONDS", "5"))
DEFAULT_PATTERN_HOURLY_TOKEN_BUDGET = int(os.environ.get("ARGUS_PATTERN_HOURLY_TOKEN_BUDGET", "12000"))
DEFAULT_DISCORD_PAGE_SIZE = int(os.environ.get("ARGUS_DISCORD_PAGE_SIZE", "50"))
DISCORD_API = os.environ.get("ARGUS_DISCORD_API", "https://discord.com/api/v10")
DEFAULT_ARES_CHANNEL_ID = os.environ.get("ARGUS_DISCORD_ARES_CHANNEL_ID", "1503438297597350062")
DEFAULT_CLAWTA_CHANNEL_ID = os.environ.get("ARGUS_DISCORD_CLAWTA_CHANNEL_ID", "1503439202719760405")

_TS_RE = re.compile(r"(?P<ts>\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))")
_TICKET_RE = re.compile(r"\b(t_[0-9a-f]{8})\b")
_JSON_RE = re.compile(r"\{.*\}", re.DOTALL)


@dataclass(frozen=True)
class PatternResult:
    """Structured outcome for one free-form line.

    `unparsed_reason` is set when the line is genuinely unparseable and
    should be recorded as an `unparsed` event (the cursor advances past
    it). `transient` is set when extraction was *skipped* for a transient
    reason — GPU busy, circuit open, daily cap, or hourly token budget
    exhausted. A transient skip must NOT advance the cursor and must NOT
    write an `unparsed` row: the line is left to be retried once the
    transient condition clears.
    """

    matched: bool
    kind: str | None = None
    subject: str | None = None
    action_target: str | None = None
    reason: str | None = None
    payload: dict | None = None
    unparsed_reason: str | None = None
    transient: bool = False
    transient_reason: str | None = None


@dataclass(frozen=True)
class DiscordMessage:
    """Minimal Discord message record."""

    id: str
    channel_id: str
    content: str
    timestamp: str
    author: str | None = None


class DiscordRateLimited(Exception):
    """Raised when Discord asks the ingester to back off."""

    def __init__(self, retry_after_seconds: float):
        super().__init__(f"discord_rate_limited:{retry_after_seconds}")
        self.retry_after_seconds = retry_after_seconds


class DiscordTranscriptClient(Protocol):
    """Fetch a channel transcript page."""

    def fetch_channel_messages(
        self,
        channel_id: str,
        *,
        after: str | None = None,
        limit: int = DEFAULT_DISCORD_PAGE_SIZE,
    ) -> list[DiscordMessage]:
        ...


def _utc_now_iso() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat()


def _payload(raw_line: str, extra: dict | None = None) -> dict:
    payload = {"raw": raw_line}
    if extra:
        payload.update(extra)
    return payload


def _extract_timestamp(raw_line: str, fallback_ts: str | None = None) -> str:
    match = _TS_RE.search(raw_line)
    if match:
        ts = match.group("ts")
        return ts.replace("Z", "+00:00")
    return fallback_ts or _utc_now_iso()


def _ticket_id(text: str) -> str | None:
    match = _TICKET_RE.search(text)
    return match.group(1) if match else None


def _extract_json_blob(text: str) -> dict | None:
    match = _JSON_RE.search(text)
    if not match:
        return None
    try:
        return json.loads(match.group(0))
    except json.JSONDecodeError:
        return None


def _pattern_budget_used(conn: sqlite3.Connection) -> int:
    since_ts = int(time.time()) - 3600
    row = conn.execute(
        """
        SELECT COALESCE(SUM(COALESCE(tokens_in, 0) + COALESCE(tokens_out, 0)), 0)
        FROM llm_calls
        WHERE purpose = 'pattern_extract' AND ts_unix >= ?
        """,
        (since_ts,),
    ).fetchone()
    return int(row[0]) if row else 0


# Transient skip reasons mean "we never actually looked at this line" —
# the LLM was unavailable (GPU busy / circuit open / daily cap) or the
# hourly token budget was exhausted. These conditions clear on their own,
# so the line must be retried rather than checkpointed as `unparsed`.
def _is_transient_skip(skipped_reason: str | None) -> bool:
    if not skipped_reason:
        return False
    if skipped_reason == "budget_exceeded":
        return True
    if skipped_reason == "circuit_open":
        return True
    if skipped_reason == "daily_cap":
        return True
    if skipped_reason.startswith("gpu:"):
        return True
    return False


def _extract_via_qwen(
    conn: sqlite3.Connection,
    *,
    source: str,
    raw_line: str,
    timeout_seconds: int,
) -> PatternResult:
    system = (
        "You classify one observability line into a canonical event. "
        "Return compact JSON only. Allowed kinds: hermes_standup, "
        "openclaw_dispatch, openclaw_workflow_failure, discord_clawta_announce, none. "
        "Schema: {\"kind\": str, \"subject\": str|null, \"reason\": str|null, "
        "\"action_target\": str|null, \"payload\": object}. "
        "Use kind='none' when the line is not one of the allowed kinds."
    )
    user = json.dumps({"source": source, "line": raw_line}, ensure_ascii=True)
    result = llm.call(
        conn,
        purpose="pattern_extract",
        system=system,
        user=user,
        timeout=timeout_seconds,
        bypass_cap=True,
    )
    if not result.ok or not result.text:
        # A transient skip (GPU busy / circuit open / daily cap) means the
        # LLM was never consulted — the line is not unparseable, it just
        # hasn't been looked at yet. Surface it as transient so the caller
        # leaves the cursor where it is and retries the line later.
        if _is_transient_skip(result.skipped_reason):
            return PatternResult(
                matched=False,
                transient=True,
                transient_reason=result.skipped_reason,
            )
        reason = result.skipped_reason or result.error or "pattern_extract_failed"
        return PatternResult(matched=False, unparsed_reason=reason)
    try:
        payload = json.loads(result.text)
    except json.JSONDecodeError:
        return PatternResult(matched=False, unparsed_reason="bad_json")
    kind = payload.get("kind")
    if kind in (None, "none", ""):
        return PatternResult(matched=False)
    return PatternResult(
        matched=True,
        kind=kind,
        subject=payload.get("subject"),
        action_target=payload.get("action_target"),
        reason=payload.get("reason"),
        payload=payload.get("payload") or {},
    )


def extract_pattern(
    conn: sqlite3.Connection,
    *,
    source: str,
    raw_line: str,
    timeout_seconds: int = DEFAULT_PATTERN_TIMEOUT_SECONDS,
    hourly_token_budget: int = DEFAULT_PATTERN_HOURLY_TOKEN_BUDGET,
) -> PatternResult:
    """Bounded qwen-backed extractor for one free-form line."""
    if _pattern_budget_used(conn) >= hourly_token_budget:
        # Hourly token budget is exhausted — this is transient. The budget
        # window rolls forward, so the line must be retried rather than
        # checkpointed as `unparsed` and skipped forever.
        return PatternResult(
            matched=False,
            transient=True,
            transient_reason="budget_exceeded",
        )
    return _extract_via_qwen(
        conn,
        source=source,
        raw_line=raw_line,
        timeout_seconds=timeout_seconds,
    )


def _record_unparsed(
    conn: sqlite3.Connection,
    *,
    source: str,
    source_ref: str,
    ts: str,
    raw_line: str,
    reason: str,
) -> None:
    insert_source_event(
        conn,
        source=source,
        kind="unparsed",
        ts=ts,
        source_ref=source_ref,
        subject=None,
        payload=_payload(raw_line, {"unparsed_reason": reason}),
        action_target=source_ref,
        reason=reason,
    )


def _lines_with_byte_offsets(
    data: bytes, start_offset: int
) -> list[tuple[str, int]]:
    """Split `data` into complete lines, each tagged with the byte offset
    *immediately after* that line.

    Mirrors `indexer._split_complete_lines` but keeps per-line byte
    boundaries so a tail can checkpoint partway through a batch (needed
    when a transient LLM skip forces us to stop without advancing past
    the un-attempted line). A trailing partial line (no newline) is
    dropped, same as `_split_complete_lines`.
    """
    if not data:
        return []
    if data.endswith(b"\n"):
        complete = data
    else:
        nl = data.rfind(b"\n")
        if nl < 0:
            return []
        complete = data[: nl + 1]

    out: list[tuple[str, int]] = []
    pos = start_offset
    for raw in complete.splitlines(keepends=True):
        pos += len(raw)
        out.append((raw.decode("utf-8", errors="replace").rstrip("\n"), pos))
    return out


def ingest_log_file(
    conn: sqlite3.Connection,
    *,
    source: str,
    file_path: Path,
    timeout_seconds: int = DEFAULT_PATTERN_TIMEOUT_SECONDS,
    hourly_token_budget: int = DEFAULT_PATTERN_HOURLY_TOKEN_BUDGET,
) -> tuple[int, int]:
    """Tail one rotating log file into canonical events."""
    if not file_path.exists():
        return 0, 0
    migrations.apply_pending(conn)

    ck_key = checkpoint_key(source, str(file_path))
    checkpoint = migrations.get_checkpoint(conn, ck_key)
    offset = int(checkpoint["offset"]) if checkpoint else 0
    inode = file_inode(file_path)
    if checkpoint:
        prev_inode = checkpoint["inode"]
        if prev_inode is not None and inode is not None and int(prev_inode) != int(inode):
            offset = 0
    try:
        size = file_path.stat().st_size
    except FileNotFoundError:
        return 0, 0
    if size < offset:
        offset = 0

    with file_path.open("rb") as fh:
        fh.seek(offset)
        data = fh.read()
    lines, next_offset = _split_complete_lines(data, offset)
    line_offsets = _lines_with_byte_offsets(data, offset)

    inserted = 0
    unparsed = 0
    # `committed_offset` advances only past lines we have fully resolved
    # (parsed, recorded unparsed, or skipped as non-matching). A transient
    # LLM skip stops the loop here so the un-attempted line — and every
    # line after it — is retried on the next pass.
    committed_offset = offset
    for idx, (raw_line, line_end_offset) in enumerate(line_offsets, start=1):
        if not raw_line.strip():
            committed_offset = line_end_offset
            continue
        ts = _extract_timestamp(raw_line)
        source_ref = f"{file_path}:{offset + idx}"
        result = extract_pattern(
            conn,
            source=source,
            raw_line=raw_line,
            timeout_seconds=timeout_seconds,
            hourly_token_budget=hourly_token_budget,
        )
        if result.transient:
            # LLM unavailable / budget exhausted — do NOT advance the
            # cursor and do NOT write an unparsed row. Stop the batch so
            # this line is retried once the transient condition clears.
            break
        if result.unparsed_reason:
            _record_unparsed(
                conn,
                source=source,
                source_ref=source_ref,
                ts=ts,
                raw_line=raw_line,
                reason=result.unparsed_reason,
            )
            unparsed += 1
            committed_offset = line_end_offset
            continue
        if not result.matched or not result.kind:
            committed_offset = line_end_offset
            continue
        if insert_source_event(
            conn,
            source=source,
            kind=result.kind,
            ts=ts,
            source_ref=source_ref,
            subject=result.subject,
            payload=_payload(raw_line, result.payload),
            action_target=result.action_target,
            reason=result.reason,
        ):
            inserted += 1
        committed_offset = line_end_offset
    else:
        # No transient break — the whole batch resolved. Advance to the
        # safe next offset (covers any trailing partial line correctly).
        committed_offset = next_offset

    next_offset = committed_offset

    migrations.upsert_checkpoint(
        conn,
        source_key=ck_key,
        source=source,
        path=str(file_path),
        offset=next_offset,
        inode=inode,
        meta={"unparsed": unparsed},
    )
    return inserted, unparsed


def ingest_log_directory(
    conn: sqlite3.Connection,
    *,
    source: str,
    log_dir: Path,
    timeout_seconds: int = DEFAULT_PATTERN_TIMEOUT_SECONDS,
    hourly_token_budget: int = DEFAULT_PATTERN_HOURLY_TOKEN_BUDGET,
) -> tuple[int, int]:
    """Ingest every `*.log` file in one source directory."""
    if not log_dir.exists():
        return 0, 0
    inserted = 0
    unparsed = 0
    for file_path in sorted(p for p in log_dir.glob("*.log") if p.is_file()):
        file_inserted, file_unparsed = ingest_log_file(
            conn,
            source=source,
            file_path=file_path,
            timeout_seconds=timeout_seconds,
            hourly_token_budget=hourly_token_budget,
        )
        inserted += file_inserted
        unparsed += file_unparsed
    return inserted, unparsed


def _read_openclaw_cfg(config_path: Path) -> dict:
    if not config_path.exists():
        return {}
    try:
        return json.loads(config_path.read_text())
    except (OSError, json.JSONDecodeError):
        return {}


class OpenClawDiscordClient:
    """Discord transcript fetcher using the operator's openclaw token."""

    def __init__(self, config_path: Path | None = None):
        self.config_path = config_path or env_path("ARGUS_OPENCLAW_CONFIG", "~/.openclaw/openclaw.json")
        cfg = _read_openclaw_cfg(self.config_path)
        self.token = (
            os.environ.get("OPENCLAW_DISCORD_TOKEN")
            or (((cfg.get("channels") or {}).get("discord") or {}).get("token"))
        )

    def fetch_channel_messages(
        self,
        channel_id: str,
        *,
        after: str | None = None,
        limit: int = DEFAULT_DISCORD_PAGE_SIZE,
    ) -> list[DiscordMessage]:
        if not self.token:
            return []
        query = {"limit": str(limit)}
        if after:
            query["after"] = after
        url = f"{DISCORD_API}/channels/{channel_id}/messages?{urllib.parse.urlencode(query)}"
        req = urllib.request.Request(
            url,
            headers={
                "Authorization": f"Bot {self.token}",
                "User-Agent": "argus-discord-ingest/0.1",
            },
            method="GET",
        )
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                payload = json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            if exc.code == 429:
                body = json.loads(exc.read().decode("utf-8"))
                raise DiscordRateLimited(float(body.get("retry_after", 1.0)))
            raise
        if not isinstance(payload, list):
            return []
        messages = [
            DiscordMessage(
                id=str(item["id"]),
                channel_id=channel_id,
                content=item.get("content") or "",
                timestamp=(item.get("timestamp") or "").replace("Z", "+00:00"),
                author=((item.get("author") or {}).get("username")),
            )
            for item in payload
            if isinstance(item, dict) and item.get("id") and item.get("timestamp")
        ]
        messages.sort(key=lambda msg: msg.id)
        return messages


def ingest_discord_channel(
    conn: sqlite3.Connection,
    *,
    channel_id: str,
    channel_name: str,
    client: DiscordTranscriptClient,
    page_size: int = DEFAULT_DISCORD_PAGE_SIZE,
) -> tuple[int, int]:
    """Fetch a Discord channel transcript incrementally."""
    migrations.apply_pending(conn)
    ck_key = checkpoint_key("discord", channel_name)
    checkpoint = migrations.get_checkpoint(conn, ck_key)
    after = checkpoint["cursor"] if checkpoint and checkpoint["cursor"] else None
    inserted = 0
    unparsed = 0
    try:
        messages = client.fetch_channel_messages(channel_id, after=after, limit=page_size)
    except DiscordRateLimited as exc:
        migrations.upsert_checkpoint(
            conn,
            source_key=ck_key,
            source="discord",
            path=channel_name,
            cursor=after,
            meta={"retry_after_seconds": exc.retry_after_seconds},
        )
        return 0, 0

    last_cursor = after
    for msg in messages:
        result = extract_pattern(conn, source="discord", raw_line=msg.content)
        source_ref = f"discord:{channel_name}:{msg.id}"
        if result.transient:
            # LLM unavailable / budget exhausted — do NOT advance the
            # cursor past this message and do NOT write an unparsed row.
            # Stop here so this message is re-fetched on the next pass.
            break
        if result.unparsed_reason:
            _record_unparsed(
                conn,
                source="discord",
                source_ref=source_ref,
                ts=msg.timestamp,
                raw_line=msg.content,
                reason=result.unparsed_reason,
            )
            unparsed += 1
            last_cursor = msg.id
            continue
        if result.matched and result.kind:
            if insert_source_event(
                conn,
                source="discord",
                kind=result.kind,
                ts=msg.timestamp,
                source_ref=source_ref,
                subject=result.subject or _ticket_id(msg.content),
                payload=_payload(msg.content, {"channel": channel_name, "author": msg.author}),
                action_target=channel_name,
                reason=result.reason,
            ):
                inserted += 1
        last_cursor = msg.id
    migrations.upsert_checkpoint(
        conn,
        source_key=ck_key,
        source="discord",
        path=channel_name,
        cursor=last_cursor,
        meta={"channel_id": channel_id, "unparsed": unparsed},
    )
    return inserted, unparsed


def ingest_discord_transcripts(
    conn: sqlite3.Connection,
    *,
    client: DiscordTranscriptClient | None = None,
) -> tuple[int, int]:
    """Ingest `#ares` and `#clawta` transcripts."""
    client = client or OpenClawDiscordClient()
    inserted = 0
    unparsed = 0
    for channel_name, channel_id in (
        ("ares", DEFAULT_ARES_CHANNEL_ID),
        ("clawta", DEFAULT_CLAWTA_CHANNEL_ID),
    ):
        c_inserted, c_unparsed = ingest_discord_channel(
            conn,
            channel_id=channel_id,
            channel_name=channel_name,
            client=client,
        )
        inserted += c_inserted
        unparsed += c_unparsed
    return inserted, unparsed


def index_all_sources(
    *,
    db_path: Path,
    decisions_dir: Path,
    hermes_logs_dir: Path | None = None,
    openclaw_logs_dir: Path | None = None,
    discord_client: DiscordTranscriptClient | None = None,
) -> Path:
    """Batch index chain decisions plus Slice 3 sources."""
    from chitin_telemetry.indexer import index_jsonl_file, _decision_files

    conn = init_db(db_path)
    migrations.apply_pending(conn)
    try:
        for file_path in _decision_files(decisions_dir):
            index_jsonl_file(conn, file_path)
        ingest_log_directory(
            conn,
            source="hermes",
            log_dir=hermes_logs_dir or env_path("ARGUS_HERMES_LOGS_DIR", "~/.hermes/logs"),
        )
        ingest_log_directory(
            conn,
            source="openclaw",
            log_dir=openclaw_logs_dir or env_path("ARGUS_OPENCLAW_LOGS_DIR", "~/.openclaw/logs"),
        )
        ingest_discord_transcripts(conn, client=discord_client)
    finally:
        conn.close()
    return db_path


def follow_all_sources(
    *,
    db_path: Path,
    decisions_dir: Path,
    poll_seconds: float = 1.0,
    hermes_logs_dir: Path | None = None,
    openclaw_logs_dir: Path | None = None,
    discord_client: DiscordTranscriptClient | None = None,
) -> Path:
    """Continuously index chain + logs + Discord without source coupling."""
    from chitin_telemetry.indexer import _decision_files, index_jsonl_file_from_offset

    conn = init_db(db_path)
    migrations.apply_pending(conn)
    offsets: dict[Path, int] = {}
    try:
        while True:
            try:
                current = list(_decision_files(decisions_dir))
                for file_path in current:
                    offset = offsets.get(file_path, 0)
                    inode = file_inode(file_path)
                    if inode is None:
                        continue
                    try:
                        size = file_path.stat().st_size
                    except FileNotFoundError:
                        continue
                    if size < offset:
                        offset = 0
                    _, _, next_offset = index_jsonl_file_from_offset(conn, file_path, offset)
                    offsets[file_path] = next_offset
            except Exception:
                pass
            try:
                ingest_log_directory(
                    conn,
                    source="hermes",
                    log_dir=hermes_logs_dir or env_path("ARGUS_HERMES_LOGS_DIR", "~/.hermes/logs"),
                )
            except Exception:
                pass
            try:
                ingest_log_directory(
                    conn,
                    source="openclaw",
                    log_dir=openclaw_logs_dir or env_path("ARGUS_OPENCLAW_LOGS_DIR", "~/.openclaw/logs"),
                )
            except Exception:
                pass
            try:
                ingest_discord_transcripts(conn, client=discord_client)
            except Exception:
                pass
            time.sleep(poll_seconds)
    finally:
        conn.close()
    return db_path
