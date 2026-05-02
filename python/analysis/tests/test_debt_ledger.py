"""Tests for the debt-ledger loader (PR #142).

Covers `load_ledger` + `filter_by_status` + `filter_by_severity` from
`analysis.debt`. Uses the schema established by PR #137's
`docs/debt-ledger.md`.
"""
from __future__ import annotations

from pathlib import Path

import pytest

from analysis.debt import (
    DebtEntry,
    filter_by_severity,
    filter_by_status,
    load_ledger,
)


# ─── fixture builders ────────────────────────────────────────────────────


def write_ledger(tmp_path: Path, *yaml_blocks: str) -> Path:
    """Write a debt-ledger.md-shaped file with the given yaml-fenced blocks
    interleaved with the doc's typical separator (`---`)."""
    parts = ["# Debt Ledger\n\nIntro prose.\n\n## Entries\n\n"]
    for block in yaml_blocks:
        parts.append("---\n\n```yaml\n")
        parts.append(block)
        parts.append("\n```\n\n")
    path = tmp_path / "debt-ledger.md"
    path.write_text("".join(parts))
    return path


def make_block(**overrides: object) -> str:
    """Build a well-formed yaml block with minimum-viable defaults; pass
    overrides to test specific fields."""
    fields: dict[str, object] = {
        "id": "sample-debt",
        "discovered_at": "2026-05-02T16:35:00Z",
        "discovered_by": "operator",
        "severity": "medium",
        "category": "code-debt",
        "file": "apps/foo.ts",
        "status": "open",
        "description": "what's wrong + why",
    }
    fields.update(overrides)
    lines = []
    for k, v in fields.items():
        if k == "description":
            lines.append(f"{k}: |\n  {v}")
        else:
            lines.append(f"{k}: {v}")
    return "\n".join(lines)


# ─── load_ledger ─────────────────────────────────────────────────────────


def test_returns_empty_list_when_file_missing(tmp_path):
    """Missing ledger file must not raise — matches loaders.load_gov_decisions."""
    assert load_ledger(tmp_path / "nope.md") == []


def test_parses_a_well_formed_entry(tmp_path):
    path = write_ledger(tmp_path, make_block())
    entries = load_ledger(path)
    assert len(entries) == 1
    e = entries[0]
    assert e.id == "sample-debt"
    assert e.severity == "medium"
    assert e.category == "code-debt"
    assert e.file == "apps/foo.ts"
    assert e.status == "open"
    assert e.shipped_in is None
    assert e.description == "what's wrong + why"
    assert e.discovered_at == "2026-05-02T16:35:00Z"
    assert e.discovered_by == "operator"


def test_parses_multiple_entries_in_order(tmp_path):
    path = write_ledger(
        tmp_path,
        make_block(id="first", severity="low"),
        make_block(id="second", severity="high"),
        make_block(id="third", severity="blocking"),
    )
    entries = load_ledger(path)
    assert [e.id for e in entries] == ["first", "second", "third"]


def test_skips_block_with_missing_required_field(tmp_path, capsys):
    """Missing `severity` → skipped, parse_errors += 1, stderr warning fired."""
    bad = make_block().replace("severity: medium\n", "")
    good = make_block(id="good")
    path = write_ledger(tmp_path, bad, good)
    entries = load_ledger(path)
    assert [e.id for e in entries] == ["good"]
    captured = capsys.readouterr()
    assert "1 malformed entries skipped" in captured.err


def test_skips_malformed_yaml(tmp_path, capsys):
    """A yaml-fenced block that doesn't parse as yaml → skipped + warning."""
    path = write_ledger(tmp_path, "id: ok\n  : : malformed", make_block(id="good"))
    entries = load_ledger(path)
    assert [e.id for e in entries] == ["good"]
    captured = capsys.readouterr()
    assert "malformed" in captured.err


def test_no_warning_on_clean_load(tmp_path, capsys):
    """No parse_errors → no stderr noise."""
    path = write_ledger(tmp_path, make_block())
    load_ledger(path)
    captured = capsys.readouterr()
    assert captured.err == ""


def test_shipped_in_optional(tmp_path):
    """`shipped_in` is optional; missing → None."""
    path = write_ledger(tmp_path, make_block(shipped_in="123"))
    entries = load_ledger(path)
    assert entries[0].shipped_in == "123"


def test_returns_empty_when_no_yaml_fences_in_file(tmp_path):
    """A debt-ledger.md with intro prose but no entries yet → empty list."""
    path = tmp_path / "empty-ledger.md"
    path.write_text("# Debt Ledger\n\nNo entries yet.\n")
    assert load_ledger(path) == []


# ─── filter_by_status ────────────────────────────────────────────────────


def test_filter_by_status_returns_only_matching(tmp_path):
    path = write_ledger(
        tmp_path,
        make_block(id="open-1", status="open"),
        make_block(id="claimed-1", status="claimed"),
        make_block(id="shipped-1", status="shipped"),
        make_block(id="open-2", status="open"),
    )
    entries = load_ledger(path)
    assert [e.id for e in filter_by_status(entries, "open")] == ["open-1", "open-2"]
    assert [e.id for e in filter_by_status(entries, "claimed")] == ["claimed-1"]
    assert [e.id for e in filter_by_status(entries, "shipped")] == ["shipped-1"]


def test_filter_by_status_empty_when_no_match(tmp_path):
    path = write_ledger(tmp_path, make_block(status="open"))
    entries = load_ledger(path)
    assert filter_by_status(entries, "shipped") == []


# ─── filter_by_severity ──────────────────────────────────────────────────


def test_filter_by_severity_threshold(tmp_path):
    path = write_ledger(
        tmp_path,
        make_block(id="b", severity="blocking"),
        make_block(id="h", severity="high"),
        make_block(id="m", severity="medium"),
        make_block(id="l", severity="low"),
    )
    entries = load_ledger(path)
    assert [e.id for e in filter_by_severity(entries, "high")] == ["b", "h"]
    assert [e.id for e in filter_by_severity(entries, "medium")] == ["b", "h", "m"]
    assert [e.id for e in filter_by_severity(entries, "low")] == ["b", "h", "m", "l"]
    assert [e.id for e in filter_by_severity(entries, "blocking")] == ["b"]


def test_filter_by_severity_unknown_severity_raises():
    with pytest.raises(ValueError, match="unknown severity"):
        filter_by_severity([], "catastrophic")


def test_filter_by_severity_skips_entries_with_unknown_severity(tmp_path):
    """An entry with a severity outside the canonical four is gracefully
    excluded by ranking it 0 — caller should clean the data, not the
    helper."""
    path = write_ledger(
        tmp_path,
        make_block(id="weird", severity="catastrophic"),
        make_block(id="normal", severity="high"),
    )
    entries = load_ledger(path)
    out = filter_by_severity(entries, "low")
    # 'weird' has severity 'catastrophic' → maps to 0 → below threshold
    assert [e.id for e in out] == ["normal"]
