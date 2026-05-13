"""Tests for argus CLI safety helpers."""
from argus.cli import _sanitize_readonly_select


def test_sanitize_readonly_select_accepts_single_select():
    assert _sanitize_readonly_select("SELECT * FROM events LIMIT 1;") == "SELECT * FROM events LIMIT 1"


def test_sanitize_readonly_select_rejects_mutation_and_multi_statement():
    assert _sanitize_readonly_select("DELETE FROM events") is None
    assert _sanitize_readonly_select("SELECT * FROM events; DROP TABLE events") is None
    assert _sanitize_readonly_select("PRAGMA table_info(events)") is None
