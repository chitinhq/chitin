"""Integration test: end-to-end workflow."""
import json
import tempfile
from pathlib import Path

from argus.indexer import init_db, index_jsonl_file
from argus.detectors import run_all_detectors
from argus.reporter import generate_daily_report


def test_end_to_end_workflow():
    """Test complete workflow: index → detect → report."""
    with tempfile.TemporaryDirectory() as tmpdir:
        tmpdir = Path(tmpdir)

        # Create mock JSONL file with test data
        jsonl_file = tmpdir / "gov-decisions-2026-05-13.jsonl"
        events = [
            {"ts": "2026-05-13T08:00:00Z", "allowed": False, "rule_id": "rule1", "agent": "agent1"},
            {"ts": "2026-05-13T08:00:01Z", "allowed": False, "rule_id": "rule1", "agent": "agent1"},
            {"ts": "2026-05-13T08:00:02Z", "allowed": False, "rule_id": "rule1", "agent": "agent1"},
            {"ts": "2026-05-13T08:00:03Z", "allowed": False, "rule_id": "rule1", "agent": "agent1"},
            {"ts": "2026-05-13T08:01:00Z", "allowed": True, "rule_id": "rule2", "agent": "agent2"},
        ]

        with jsonl_file.open("w") as f:
            for event in events:
                f.write(json.dumps(event) + "\n")

        # Index
        db_path = tmpdir / "index.db"
        conn = init_db(db_path)
        inserted, skipped = index_jsonl_file(conn, jsonl_file)
        conn.close()

        assert inserted == 5
        assert skipped == 0

        # Detect
        findings = run_all_detectors(str(db_path))
        assert len(findings) > 0  # Should find deny cluster

        # Report
        report_dir = tmpdir / "reports"
        report_path = generate_daily_report(str(db_path), report_dir)

        assert report_path.exists()
        content = report_path.read_text()
        assert "Argus Observatory Report" in content
        assert "5" in content or "Total decisions" in content
