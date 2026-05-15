"""Tests for argus CLI safety helpers."""
from argus.cli import _sanitize_readonly_select, parse_args


def test_sanitize_readonly_select_accepts_single_select():
    assert _sanitize_readonly_select("SELECT * FROM events LIMIT 1;") == "SELECT * FROM events LIMIT 1"


def test_sanitize_readonly_select_rejects_mutation_and_multi_statement():
    assert _sanitize_readonly_select("DELETE FROM events") is None
    assert _sanitize_readonly_select("SELECT * FROM events; DROP TABLE events") is None
    assert _sanitize_readonly_select("PRAGMA table_info(events)") is None


def test_ingest_beliefs_accepts_db_path_after_subcommand():
    args = parse_args(["ingest-beliefs", "--wiki", "--db-path", "/tmp/argus-test.db"])

    assert args.cmd == "ingest-beliefs"
    assert args.wiki is True
    assert args.db_path == "/tmp/argus-test.db"
