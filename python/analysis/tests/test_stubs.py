"""Tests that the debt and souls stubs produce valid empty findings.

Foundation-generalization proof: if the stubs plug into the same writers
and produce valid JSON/markdown, the foundation is reusable.
"""
import json
import subprocess
import sys


def test_debt_stub_writes_valid_empty_json(tmp_path):
    out_dir = tmp_path / "out"
    result = subprocess.run(
        [sys.executable, "-m", "analysis.debt",
         "--out-dir", str(out_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stderr
    json_path = next(out_dir.glob("debt-*.json"))
    body = json.loads(json_path.read_text())
    assert body["stream"] == "debt"
    assert body["patterns"] == []
    md_path = next(out_dir.glob("debt-*.md"))
    assert "Debt Analysis" in md_path.read_text()


def test_souls_stub_writes_valid_empty_json(tmp_path):
    out_dir = tmp_path / "out"
    result = subprocess.run(
        [sys.executable, "-m", "analysis.souls",
         "--out-dir", str(out_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stderr
    json_path = next(out_dir.glob("souls-*.json"))
    body = json.loads(json_path.read_text())
    assert body["stream"] == "souls"
    assert body["patterns"] == []
    md_path = next(out_dir.glob("souls-*.md"))
    assert "Souls Analysis" in md_path.read_text()
