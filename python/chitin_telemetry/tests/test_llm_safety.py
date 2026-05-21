"""Tests for chitin_telemetry.llm safety: thinking-strip, redactions, cap counting."""
from __future__ import annotations

import os
import tempfile
import time
from pathlib import Path

import pytest

from chitin_telemetry import llm, migrations
from chitin_telemetry.indexer import init_db


# ---- strip_thinking -------------------------------------------------------


def test_strip_thinking_handles_qwen3_done_thinking():
    raw = (
        "Thinking...\n"
        "1. Analyze the request.\n"
        "2. Decide what to output.\n"
        "...done thinking.\n"
        "\n"
        "hello"
    )
    assert llm.strip_thinking(raw) == "hello"


def test_strip_thinking_handles_xml_tags():
    raw = "<think>step 1\nstep 2</think>\nthe answer is 42"
    assert llm.strip_thinking(raw) == "the answer is 42"


def test_strip_thinking_handles_thinking_prefix():
    raw = "Thinking: I should output a word.\n\nhello"
    assert llm.strip_thinking(raw) == "hello"


def test_strip_thinking_returns_plain_text_unchanged():
    raw = "just an answer"
    assert llm.strip_thinking(raw) == "just an answer"


def test_strip_thinking_handles_leading_thinking_marker_without_done():
    raw = "Thinking...\n\nfinal answer line"
    # Should drop everything up to the first blank line.
    assert llm.strip_thinking(raw) == "final answer line"


def test_strip_thinking_handles_real_qwen_capture():
    # Captured from a real `ollama run qwen3.6:27b` invocation.
    raw = (
        "Thinking...\n"
        "Thinking Process:\n"
        "1.  Analyze User Input: ...\n"
        "2.  Formulate Output: hello\n"
        "...done thinking.\n"
        "\n"
        "hello"
    )
    assert llm.strip_thinking(raw) == "hello"


# ---- redactions -----------------------------------------------------------


def test_redact_openai_key():
    text = "the token is sk-abcdefghijklmnopqrstuvwxyz0123456789 do not log"
    redacted = llm._apply_redactions(text)
    assert "sk-abcdefghij" not in redacted
    assert "[REDACTED:openai_key]" in redacted


def test_redact_github_token():
    text = "Authorization: Bearer ghp_abcdefghijklmnopqrstuvwxyz01234567"
    redacted = llm._apply_redactions(text)
    assert "ghp_abcdefghij" not in redacted
    assert "[REDACTED:github_token]" in redacted


def test_redact_discord_webhook():
    text = "https://discord.com/api/webhooks/12345/aBcDeFg_hIjKlMnO"
    redacted = llm._apply_redactions(text)
    assert "aBcDeFg_hIjKlMnO" not in redacted


def test_redact_anthropic_key():
    text = "sk-ant-1234567890abcdefghijklmnop_extra"
    redacted = llm._apply_redactions(text)
    assert "[REDACTED:anthropic_key]" in redacted


# ---- cap counting ---------------------------------------------------------


def test_count_calls_today_zero_on_fresh_db():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        assert llm.count_calls_today(conn) == 0
        conn.close()


def test_count_calls_today_counts_only_successes():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        now = int(time.time())
        # 2 successful, 1 failed (NULL response)
        conn.execute(
            "INSERT INTO llm_calls (ts_unix, purpose, prompt_hash, prompt, response, retention_ts) VALUES (?, ?, ?, ?, ?, ?)",
            (now, "p", "h1", "p", "r", now + 86400),
        )
        conn.execute(
            "INSERT INTO llm_calls (ts_unix, purpose, prompt_hash, prompt, response, retention_ts) VALUES (?, ?, ?, ?, ?, ?)",
            (now, "p", "h2", "p", "r", now + 86400),
        )
        conn.execute(
            "INSERT INTO llm_calls (ts_unix, purpose, prompt_hash, prompt, response, retention_ts) VALUES (?, ?, ?, ?, ?, ?)",
            (now, "p", "h3", "p", None, now + 86400),
        )
        conn.commit()
        assert llm.count_calls_today(conn) == 2
        conn.close()


def test_sweep_expired_calls_deletes_old_rows():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        now = int(time.time())
        # one expired, one not
        conn.execute(
            "INSERT INTO llm_calls (ts_unix, purpose, prompt_hash, prompt, response, retention_ts) VALUES (?, ?, ?, ?, ?, ?)",
            (now - 100, "p", "h1", "p", "r", now - 10),
        )
        conn.execute(
            "INSERT INTO llm_calls (ts_unix, purpose, prompt_hash, prompt, response, retention_ts) VALUES (?, ?, ?, ?, ?, ?)",
            (now, "p", "h2", "p", "r", now + 86400),
        )
        conn.commit()
        deleted = llm.sweep_expired_calls(conn)
        assert deleted == 1
        remaining = conn.execute("SELECT COUNT(*) AS c FROM llm_calls").fetchone()["c"]
        assert remaining == 1
        conn.close()


# ---- prompt hash deterministic --------------------------------------------


def test_prompt_hash_deterministic():
    h1 = llm._prompt_hash("sys", "user", "m")
    h2 = llm._prompt_hash("sys", "user", "m")
    assert h1 == h2


def test_prompt_hash_distinguishes_inputs():
    h1 = llm._prompt_hash("sys", "user", "m")
    h2 = llm._prompt_hash("sys", "user2", "m")
    assert h1 != h2
