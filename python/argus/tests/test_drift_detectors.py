"""Tests for belief-vs-reality drift detectors."""
from __future__ import annotations

import hashlib
import sqlite3
import tempfile
from pathlib import Path

from argus.cross_source_db import init_cross_source_db
from argus.beliefs import init_beliefs_table
from argus.drift_detectors import (
    detect_stale_beliefs,
    detect_belief_without_evidence,
    detect_capability_without_belief,
    run_all_drift_detectors,
)
from argus.indexer import init_db


def _insert_belief(conn, agent, subject, claim, ts_recorded, source="s"):
    dedup = hashlib.sha256(
        f"{agent}|{subject}|{claim}|{source}".encode()
    ).hexdigest()[:24]
    conn.execute(
        """
        INSERT INTO beliefs (agent, subject, claim, ts_recorded, source_path, dedup_key)
        VALUES (?, ?, ?, ?, ?, ?)
        """,
        (agent, subject, claim, ts_recorded, source, dedup),
    )
    conn.commit()


def _insert_event(conn, ts_unix, agent, action_type, action_target=""):
    """Insert a chain event with the columns the indexer schema uses."""
    import hashlib as _h
    lh = _h.sha256(f"{ts_unix}{agent}{action_type}{action_target}".encode()).hexdigest()
    conn.execute("""
        INSERT INTO events (
            line_hash, ts, ts_unix, allowed, agent, action_type, action_target
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
    """, (lh, str(ts_unix), ts_unix, 1, agent, action_type, action_target))
    conn.commit()


class TestStaleBeliefs:
    def test_old_belief_triggers(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            init_beliefs_table(conn)
            _insert_belief(conn, "agent", "capability.go", "expert", 1000)
            conn.close()
            now_ts = 1000 + 100 * 86400
            findings = detect_stale_beliefs(xs, max_age_days=90, now_ts=now_ts)
            assert len(findings) == 1
            assert findings[0].details["age_days"] == 100

    def test_fresh_belief_does_not_trigger(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            init_beliefs_table(conn)
            _insert_belief(conn, "agent", "capability.go", "expert", 1000)
            conn.close()
            now_ts = 1000 + 30 * 86400  # 30 days < 90
            findings = detect_stale_beliefs(xs, max_age_days=90, now_ts=now_ts)
            assert findings == []

    def test_no_beliefs_no_findings(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            init_beliefs_table(conn)
            conn.close()
            assert detect_stale_beliefs(xs, now_ts=99999) == []


class TestBeliefWithoutEvidence:
    def test_zero_evidence_triggers(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            xs = tmpdir / "xs.db"
            chain = tmpdir / "chain.db"
            xs_conn = init_cross_source_db(xs)
            init_beliefs_table(xs_conn)
            _insert_belief(xs_conn, "agent_x", "capability.rust", "expert", 1000)
            xs_conn.close()
            chain_conn = init_db(chain)
            chain_conn.close()  # no events
            findings = detect_belief_without_evidence(xs, chain)
            assert len(findings) == 1
            assert findings[0].details["agent"] == "agent_x"

    def test_evidence_present_no_finding(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            xs = tmpdir / "xs.db"
            chain = tmpdir / "chain.db"
            xs_conn = init_cross_source_db(xs)
            init_beliefs_table(xs_conn)
            _insert_belief(xs_conn, "agent_x", "capability.rust", "expert", 1000)
            xs_conn.close()
            chain_conn = init_db(chain)
            _insert_event(chain_conn, 1100, "agent_x", "code.rust", "rust syntax")
            chain_conn.close()
            assert detect_belief_without_evidence(xs, chain) == []


class TestCapabilityWithoutBelief:
    def test_unclaimed_action_triggers(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            xs = tmpdir / "xs.db"
            chain = tmpdir / "chain.db"
            xs_conn = init_cross_source_db(xs)
            init_beliefs_table(xs_conn)
            # agent_x only claims python.
            _insert_belief(xs_conn, "agent_x", "capability.python", "expert", 1000)
            xs_conn.close()

            chain_conn = init_db(chain)
            for i in range(6):
                _insert_event(chain_conn, 1000 + i, "agent_x", "code.rust", "")
            chain_conn.close()

            findings = detect_capability_without_belief(xs, chain, min_usage=5)
            assert len(findings) == 1
            assert findings[0].details["action_type"] == "code.rust"
            assert findings[0].details["usage_count"] == 6

    def test_claimed_skill_substring_matches(self):
        """If the chain action_type contains a claimed-skill token, no finding."""
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            xs = tmpdir / "xs.db"
            chain = tmpdir / "chain.db"
            xs_conn = init_cross_source_db(xs)
            init_beliefs_table(xs_conn)
            _insert_belief(xs_conn, "agent_x", "capability.rust", "expert", 1000)
            xs_conn.close()

            chain_conn = init_db(chain)
            for i in range(10):
                _insert_event(chain_conn, 1000 + i, "agent_x", "code.rust", "")
            chain_conn.close()

            assert detect_capability_without_belief(xs, chain, min_usage=5) == []

    def test_universal_actions_skipped(self):
        """file.read / shell.exec / bash etc. are universal and not flagged."""
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            xs = tmpdir / "xs.db"
            chain = tmpdir / "chain.db"
            xs_conn = init_cross_source_db(xs)
            init_beliefs_table(xs_conn)
            xs_conn.close()

            chain_conn = init_db(chain)
            for i in range(20):
                _insert_event(chain_conn, 1000 + i, "agent_x", "file.read", "")
            chain_conn.close()

            assert detect_capability_without_belief(xs, chain, min_usage=5) == []


def test_run_all_drift_detectors_combines():
    with tempfile.TemporaryDirectory() as tmpdir:
        tmpdir = Path(tmpdir)
        xs = tmpdir / "xs.db"
        chain = tmpdir / "chain.db"
        xs_conn = init_cross_source_db(xs)
        init_beliefs_table(xs_conn)
        _insert_belief(xs_conn, "agent_x", "capability.go", "expert", 1000)
        xs_conn.close()
        chain_conn = init_db(chain)
        chain_conn.close()

        findings = run_all_drift_detectors(xs, chain, now_ts=1000 + 200 * 86400)
        kinds = {f.detector for f in findings}
        assert "stale_belief" in kinds
        assert "belief_without_evidence" in kinds


def test_missing_databases_safe():
    """Missing DBs degrade gracefully — zero findings, no exception."""
    findings = run_all_drift_detectors(
        Path("/nonexistent/xs.db"), Path("/nonexistent/chain.db")
    )
    assert findings == []
