"""Tests for gov-decisions JSONL loaders."""
from datetime import datetime, timezone

import pytest

from chitin_telemetry.loaders import LoadResult, Window, load_gov_decisions, parse_window_str


@pytest.fixture
def decisions_dir(tmp_path, fixtures_dir):
    src = fixtures_dir / "gov-decisions-fixture.jsonl"
    dst = tmp_path / "gov-decisions-2026-04-25.jsonl"
    dst.write_text(src.read_text())
    return tmp_path


def test_load_with_full_window(decisions_dir):
    window = Window(
        since=datetime(2026, 4, 25, 0, 0, tzinfo=timezone.utc),
        until=datetime(2026, 4, 26, 0, 0, tzinfo=timezone.utc),
    )
    result = load_gov_decisions(decisions_dir, window)
    assert isinstance(result, LoadResult)
    assert result.files_read == 1
    assert result.parse_errors == 2
    assert len(result.decisions) == 8


def test_load_with_narrow_window_excludes_outside(decisions_dir):
    window = Window(
        since=datetime(2026, 4, 25, 9, 0, tzinfo=timezone.utc),
        until=datetime(2026, 4, 25, 11, 0, tzinfo=timezone.utc),
    )
    result = load_gov_decisions(decisions_dir, window)
    envelope_ids = {d.envelope_id for d in result.decisions}
    assert envelope_ids == {"env_004", "env_005"}


def test_load_with_empty_dir(tmp_path):
    window = Window(
        since=datetime(2026, 4, 25, tzinfo=timezone.utc),
        until=datetime(2026, 4, 26, tzinfo=timezone.utc),
    )
    result = load_gov_decisions(tmp_path, window)
    assert result.files_read == 0
    assert result.decisions == []
    assert result.parse_errors == 0


def test_load_with_missing_dir(tmp_path):
    window = Window(
        since=datetime(2026, 4, 25, tzinfo=timezone.utc),
        until=datetime(2026, 4, 26, tzinfo=timezone.utc),
    )
    result = load_gov_decisions(tmp_path / "does-not-exist", window)
    assert result.files_read == 0
    assert result.decisions == []


def test_load_skips_directory_whose_name_matches_pattern(tmp_path, fixtures_dir):
    """Regression for Copilot review: a dir named like a JSONL file must not crash."""
    src = fixtures_dir / "gov-decisions-fixture.jsonl"
    (tmp_path / "gov-decisions-2026-04-25.jsonl").write_text(src.read_text())
    # Adversarial: a directory whose name matches the pattern.
    (tmp_path / "gov-decisions-2026-04-26.jsonl").mkdir()
    window = Window(
        since=datetime(2026, 4, 25, tzinfo=timezone.utc),
        until=datetime(2026, 4, 27, tzinfo=timezone.utc),
    )
    result = load_gov_decisions(tmp_path, window)
    # Only the real file is read; the directory is silently skipped.
    assert result.files_read == 1


def test_load_skips_non_matching_filenames(tmp_path, fixtures_dir):
    src = fixtures_dir / "gov-decisions-fixture.jsonl"
    (tmp_path / "gov-decisions-2026-04-25.jsonl").write_text(src.read_text())
    (tmp_path / "other-file.jsonl").write_text("ignored\n")
    (tmp_path / "gov-decisions.txt").write_text("ignored\n")
    window = Window(
        since=datetime(2026, 4, 25, tzinfo=timezone.utc),
        until=datetime(2026, 4, 26, tzinfo=timezone.utc),
    )
    result = load_gov_decisions(tmp_path, window)
    assert result.files_read == 1


def test_parse_window_str_days():
    now = datetime(2026, 4, 30, 12, 0, tzinfo=timezone.utc)
    w = parse_window_str("7d", now)
    assert w.since == datetime(2026, 4, 23, 12, 0, tzinfo=timezone.utc)
    assert w.until == now


def test_parse_window_str_hours():
    now = datetime(2026, 4, 30, 12, 0, tzinfo=timezone.utc)
    w = parse_window_str("24h", now)
    assert w.since == datetime(2026, 4, 29, 12, 0, tzinfo=timezone.utc)


def test_parse_window_str_invalid_raises():
    now = datetime(2026, 4, 30, tzinfo=timezone.utc)
    with pytest.raises(ValueError):
        parse_window_str("7y", now)
