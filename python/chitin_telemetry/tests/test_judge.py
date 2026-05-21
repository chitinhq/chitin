"""Tests for chitin_telemetry.judge — deterministic checks before LLM judge."""
from __future__ import annotations

import tempfile
from pathlib import Path

from chitin_telemetry import judge, migrations
from chitin_telemetry.indexer import init_db


def test_extract_citations_finds_tickets_and_shas():
    text = "see t_abc123 and commit a1b2c3d for context; also PR 42 and #99"
    found = judge.extract_citations(text)
    assert "t_abc123" in found
    assert "a1b2c3d" in found
    assert any("42" in s for s in found)
    assert "#99" in found


def test_citation_check_passes_when_subset():
    expected = ["t_abc123", "lockdown"]
    ok, hallucinated = judge._citation_check(
        "ticket t_abc123 violated lockdown", expected
    )
    assert ok
    assert hallucinated == []


def test_citation_check_rejects_hallucinated_tickets():
    expected = ["t_abc123"]
    ok, hallucinated = judge._citation_check(
        "ticket t_abc123 plus a fake t_deadbeef one", expected
    )
    assert not ok
    assert "t_deadbeef" in hallucinated


def test_judge_rejects_response_with_hallucinated_citation():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        # Both must be in the citation-regex shape (lowercase hex, 6-12 chars).
        verdict = judge.judge(
            conn,
            purpose="narrate_recent",
            prompt="summarize",
            response="t_aaaa111 happened next to t_bbbbbbb",
            expected_citations=["t_aaaa111"],
        )
        assert verdict.verdict == "reject"
        assert verdict.reason == "hallucinated_citations"
        assert "t_bbbbbbb" in verdict.hallucinated_citations
        conn.close()


def test_judge_rejects_empty_response():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        verdict = judge.judge(
            conn,
            purpose="narrate_recent",
            prompt="x",
            response="",
            expected_citations=[],
        )
        assert verdict.verdict == "reject"
        assert verdict.reason.startswith("structural:")
        conn.close()


def test_judge_passes_when_response_clean_and_judge_sampled_out():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        # sample_p=0 means we always sample OUT the LLM call.
        verdict = judge.judge(
            conn,
            purpose="narrate_recent",
            prompt="x",
            response="all quiet today.",
            expected_citations=[],
            sample_p=0.0,
        )
        assert verdict.verdict == "pass"
        assert verdict.judge_skipped
        conn.close()


def test_parse_judge_json_strips_fences():
    parsed = judge._parse_judge_json('```json\n{"verdict": "pass"}\n```')
    assert parsed == {"verdict": "pass"}


def test_parse_judge_json_handles_surrounding_prose():
    parsed = judge._parse_judge_json('blah blah {"verdict": "reject"} ok')
    assert parsed == {"verdict": "reject"}


def test_parse_judge_json_returns_none_on_garbage():
    assert judge._parse_judge_json("no json here") is None
