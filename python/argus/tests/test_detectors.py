"""Tests for argus.detectors with boundary conditions."""
import sqlite3
import tempfile
from datetime import datetime, timedelta, timezone
from pathlib import Path


def _utc_now():
    return datetime.now(timezone.utc)


def _from_unix(ts_unix):
    return datetime.fromtimestamp(ts_unix, timezone.utc)

import pytest

from argus import migrations
from argus.detectors import (
    detect_belief_without_evidence,
    detect_cross_agent_disagreement,
    detect_deny_cluster,
    detect_reality_without_belief,
    detect_unknown_rate_spike,
    detect_agent_failure_run,
    detect_stale_belief,
    detect_stuck_flow,
)
from argus.indexer import init_db


def _insert_event(conn, ts: str, allowed: bool, rule_id: str, agent: str = "test-agent"):
    """Helper to insert an event into the database."""
    import hashlib
    dt = datetime.fromisoformat(ts.replace("Z", "+00:00"))
    ts_unix = int(dt.timestamp())

    # Create unique hash for each event
    event_id = f"{ts}-{rule_id}-{agent}-{allowed}"
    lh = hashlib.sha256(event_id.encode()).hexdigest()

    conn.execute("""
        INSERT INTO events (
            line_hash, ts, ts_unix, allowed, rule_id, agent, action_type
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
    """, (
        lh,
        dt.isoformat(),
        ts_unix,
        int(allowed),
        rule_id,
        agent,
        "shell.exec",
    ))
    conn.commit()


def _insert_belief(conn, *, agent: str, subject: str, claim: str, ts_unix: int, source_path: str = "memory/test.md"):
    import hashlib

    payload = f"{agent}|{subject}|{claim}|{ts_unix}|{source_path}"
    conn.execute(
        """
        INSERT INTO beliefs (
            belief_hash, agent, subject, claim, ts_recorded, source_path,
            source_kind, schema_version, private, created_ts
        ) VALUES (?, ?, ?, ?, ?, ?, 'test', 'v1', 0, ?)
        """,
        (
            hashlib.sha256(payload.encode()).hexdigest(),
            agent,
            subject,
            claim,
            ts_unix,
            source_path,
            ts_unix,
        ),
    )
    conn.commit()


class TestDenyCluster:
    """Test deny_cluster detector with boundary conditions."""

    def test_deny_cluster_empty_database(self):
        """Empty database produces no findings."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)
            conn.close()

            findings = detect_deny_cluster(str(db_path))
            assert len(findings) == 0

    def test_deny_cluster_n_minus_1_does_not_trigger(self):
        """N-1 events at boundary does NOT trigger."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # Insert 3 denies within 300s (N=4 is threshold)
            for i in range(3):
                _insert_event(conn, f"2026-05-13T08:00:{i:02d}Z", False, "rule1")

            findings = detect_deny_cluster(str(db_path), window_seconds=300, threshold_count=4)
            assert len(findings) == 0
            conn.close()

    def test_deny_cluster_n_at_boundary_triggers(self):
        """N events at boundary DOES trigger."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # Insert exactly 4 denies within 300s
            for i in range(4):
                _insert_event(conn, f"2026-05-13T08:00:{i:02d}Z", False, "rule1")

            findings = detect_deny_cluster(str(db_path), window_seconds=300, threshold_count=4)
            assert len(findings) == 1
            assert findings[0].severity == "warning"
            conn.close()

    def test_deny_cluster_outside_window_boundary(self):
        """Events outside window_seconds boundary are excluded."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # Insert 4 denies, 3 within 100s, 1 just beyond
            for i in range(3):
                _insert_event(conn, f"2026-05-13T08:00:{i:02d}Z", False, "rule1")
            _insert_event(conn, "2026-05-13T08:02:00Z", False, "rule1")

            findings = detect_deny_cluster(str(db_path), window_seconds=100, threshold_count=4)
            # Should find 1 cluster of 3 (not 4), so no finding
            assert len(findings) == 0
            conn.close()

    def test_deny_cluster_multiple_clusters(self):
        """Multiple separate clusters are detected."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # First cluster: 4 denies in first 100s
            for i in range(4):
                _insert_event(conn, f"2026-05-13T08:00:{i:02d}Z", False, "rule1")

            # Gap
            # Second cluster: 4 denies in next 100s
            for i in range(4):
                _insert_event(conn, f"2026-05-13T08:05:{i:02d}Z", False, "rule2")

            findings = detect_deny_cluster(str(db_path), window_seconds=100, threshold_count=4)
            # Should find 2 clusters
            assert len(findings) >= 2
            conn.close()


class TestUnknownRateSpike:
    """Test unknown_rate_spike detector with boundary conditions."""

    def test_unknown_rate_spike_empty_database(self):
        """Empty database produces no findings."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)
            conn.close()

            findings = detect_unknown_rate_spike(str(db_path))
            assert len(findings) == 0

    def test_unknown_rate_spike_no_unknown_events(self):
        """Database with only known action_types produces no findings."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # Insert events with known action_type
            for i in range(10):
                conn.execute("""
                    INSERT INTO events (
                        line_hash, ts, ts_unix, allowed, action_type
                    ) VALUES (?, ?, ?, ?, ?)
                """, (
                    str(i),
                    f"2026-05-13T08:00:{i:02d}Z",
                    1715600000 + i,
                    1,
                    "shell.exec",
                ))
            conn.commit()

            findings = detect_unknown_rate_spike(str(db_path), threshold_percent=1.0)
            assert len(findings) == 0
            conn.close()

    def test_unknown_rate_spike_exactly_threshold_does_not_trigger(self):
        """Rate exactly at threshold does NOT trigger."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # Insert 100 events, exactly 1% unknown
            for i in range(99):
                conn.execute("""
                    INSERT INTO events (
                        line_hash, ts, ts_unix, allowed, action_type
                    ) VALUES (?, ?, ?, ?, ?)
                """, (
                    str(i),
                    f"2026-05-13T08:00:{i%60:02d}Z" if i < 60 else f"2026-05-13T08:{i//60:02d}:{i%60:02d}Z",
                    1715600000 + i,
                    1,
                    "shell.exec",
                ))

            # 1 unknown
            conn.execute("""
                INSERT INTO events (
                    line_hash, ts, ts_unix, allowed, action_type
                ) VALUES (?, ?, ?, ?, ?)
            """, (
                "100",
                "2026-05-13T09:00:00Z",
                1715603600,
                1,
                None,
            ))
            conn.commit()

            findings = detect_unknown_rate_spike(str(db_path), threshold_percent=1.0)
            # 1/100 = exactly 1%, does not exceed threshold
            assert len(findings) == 0
            conn.close()

    def test_unknown_rate_spike_above_threshold_triggers(self):
        """Rate above threshold DOES trigger."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # Insert 100 events with recent timestamps, 2% unknown
            now_ts = int(_utc_now().timestamp())
            for i in range(98):
                conn.execute("""
                    INSERT INTO events (
                        line_hash, ts, ts_unix, allowed, action_type
                    ) VALUES (?, ?, ?, ?, ?)
                """, (
                    f"hash_{i}",
                    _from_unix(now_ts - 1000 + i).isoformat(),
                    now_ts - 1000 + i,
                    1,
                    "shell.exec",
                ))

            # 2 unknown
            for i in range(2):
                conn.execute("""
                    INSERT INTO events (
                        line_hash, ts, ts_unix, allowed, action_type
                    ) VALUES (?, ?, ?, ?, ?)
                """, (
                    f"hash_unknown_{i}",
                    _from_unix(now_ts - 100 + i).isoformat(),
                    now_ts - 100 + i,
                    1,
                    None,
                ))
            conn.commit()

            findings = detect_unknown_rate_spike(str(db_path), threshold_percent=1.0)
            # 2/100 = 2% > 1%, triggers
            assert len(findings) == 1
            conn.close()


class TestAgentFailureRun:
    """Test agent_failure_run detector with boundary conditions."""

    def test_agent_failure_run_min_minus_1_does_not_trigger(self):
        """min_failures-1 events does NOT trigger."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # Insert 2 denies for agent (min_failures=3)
            for i in range(2):
                _insert_event(conn, f"2026-05-13T08:00:{i:02d}Z", False, "rule1", "agent1")

            findings = detect_agent_failure_run(str(db_path), min_failures=3)
            assert len(findings) == 0
            conn.close()

    def test_agent_failure_run_min_at_threshold_triggers(self):
        """min_failures events DOES trigger."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # Insert exactly 3 denies for agent
            for i in range(3):
                _insert_event(conn, f"2026-05-13T08:00:{i:02d}Z", False, "rule1", "agent1")

            findings = detect_agent_failure_run(str(db_path), min_failures=3)
            assert len(findings) == 1
            conn.close()

    def test_agent_failure_run_multiple_agents(self):
        """Multiple agents with failures are detected."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # Agent1: 3 denies
            for i in range(3):
                _insert_event(conn, f"2026-05-13T08:00:{i:02d}Z", False, "rule1", "agent1")

            # Agent2: 3 denies
            for i in range(3):
                _insert_event(conn, f"2026-05-13T08:05:{i:02d}Z", False, "rule1", "agent2")

            findings = detect_agent_failure_run(str(db_path), min_failures=3)
            assert len(findings) == 2
            conn.close()


class TestStuckFlow:
    """Test stuck_flow detector with boundary conditions."""

    def test_stuck_flow_recent_activity_no_alert(self):
        """Recent agent activity produces no alert."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # Insert recent event (1 minute ago, well within 3600s threshold)
            now_ts = int(_utc_now().timestamp())
            recent_ts = now_ts - 60  # 1 minute ago
            recent = _from_unix(recent_ts).isoformat().replace("+00:00", "Z")
            _insert_event(conn, recent, False, "rule1", "agent1")

            findings = detect_stuck_flow(str(db_path), min_idle_seconds=3600)
            assert len(findings) == 0
            conn.close()

    def test_stuck_flow_old_activity_triggers(self):
        """Agent with no activity for >min_idle_seconds triggers."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            # Insert old event (2 hours ago)
            now_ts = int(_utc_now().timestamp())
            old_ts_val = now_ts - (2 * 3600)  # 2 hours ago
            old_ts = _from_unix(old_ts_val).isoformat().replace("+00:00", "Z")
            _insert_event(conn, old_ts, False, "rule1", "agent1")

            findings = detect_stuck_flow(str(db_path), min_idle_seconds=3600)
            assert len(findings) == 1
            conn.close()


class TestBeliefDrift:
    def test_stale_belief_triggers_for_active_subject(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)
            migrations.apply_pending(conn)
            now_ts = int(_utc_now().timestamp())
            stale_ts = now_ts - 100 * 86400
            _insert_belief(conn, agent="hermes", subject="t_123", claim="t_123 is P50", ts_unix=stale_ts)
            conn.execute(
                """
                INSERT INTO events (line_hash, ts, ts_unix, allowed, action_target, action_type)
                VALUES ('evt1', ?, ?, 1, 't_123', 'kanban.update')
                """,
                (_from_unix(now_ts - 60).isoformat(), now_ts - 60),
            )
            conn.commit()

            findings = detect_stale_belief(str(db_path))
            conn.close()

            assert len(findings) == 1
            assert findings[0].detector == "belief_stale"

    def test_cross_agent_disagreement_dedups_until_seven_day_reminder(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)
            migrations.apply_pending(conn)
            now_ts = int(_utc_now().timestamp())
            _insert_belief(conn, agent="hermes", subject="t_456", claim="t_456 is P50", ts_unix=now_ts - 10)
            _insert_belief(conn, agent="clawta", subject="t_456", claim="t_456 is P30", ts_unix=now_ts - 5)
            conn.execute(
                """
                INSERT INTO findings (
                    finding_hash, ts_unix, detector, severity, title, body, citations
                ) VALUES ('oldhash', ?, 'belief_cross_agent_disagreement', 'critical',
                          'Cross-agent disagreement: t_456', ?, '[]')
                """,
                (now_ts - 2 * 86400, '{"subject": "t_456", "claims": ["priority:P30", "priority:P50"]}'),
            )
            conn.commit()

            suppressed = detect_cross_agent_disagreement(str(db_path))
            conn.execute("DELETE FROM findings")
            conn.execute(
                """
                INSERT INTO findings (
                    finding_hash, ts_unix, detector, severity, title, body, citations
                ) VALUES ('olderhash', ?, 'belief_cross_agent_disagreement', 'critical',
                          'Cross-agent disagreement: t_456', ?, '[]')
                """,
                (now_ts - 8 * 86400, '{"subject": "t_456", "claims": ["priority:P30", "priority:P50"]}'),
            )
            conn.commit()
            resurfaced = detect_cross_agent_disagreement(str(db_path))
            conn.close()

            assert suppressed == []
            assert len(resurfaced) == 1

    def test_cross_agent_disagreement_dedups_against_pretty_printed_body(self):
        """Regression: findings_store.persist writes indent=2 pretty JSON.

        The dedup check must match the claim set structurally, not by
        substring-matching a compact json.dumps against the multi-line body.
        """
        from argus import findings_store

        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)
            migrations.apply_pending(conn)
            now_ts = int(_utc_now().timestamp())
            _insert_belief(conn, agent="hermes", subject="t_789", claim="t_789 is P50", ts_unix=now_ts - 10)
            _insert_belief(conn, agent="clawta", subject="t_789", claim="t_789 is P30", ts_unix=now_ts - 5)
            conn.commit()

            # First run produces a finding; persist it via the real store
            # path so the body is pretty-printed exactly as in production.
            first = detect_cross_agent_disagreement(str(db_path))
            assert len(first) == 1
            inserted, _ = findings_store.persist(conn, first)
            assert inserted == 1

            stored_body = conn.execute(
                "SELECT body FROM findings WHERE detector = 'belief_cross_agent_disagreement'"
            ).fetchone()[0]
            assert "\n" in stored_body  # confirm it is pretty-printed

            # Second run within the 7-day window must suppress the duplicate.
            second = detect_cross_agent_disagreement(str(db_path))
            conn.close()
            assert second == []

    def test_belief_without_evidence_marks_deleted_ticket_as_orphan(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)
            migrations.apply_pending(conn)
            now_ts = int(_utc_now().timestamp())
            _insert_belief(conn, agent="hermes", subject="t_deleted", claim="t_deleted is P50", ts_unix=now_ts)

            findings = detect_belief_without_evidence(str(db_path))
            conn.close()

            assert len(findings) == 1
            assert findings[0].details["orphan"] is True

    def test_reality_without_belief_triggers_on_ticket_activity(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)
            migrations.apply_pending(conn)
            now_ts = int(_utc_now().timestamp())
            conn.execute(
                """
                INSERT INTO events (line_hash, ts, ts_unix, allowed, action_target, action_type)
                VALUES ('evt2', ?, ?, 1, 't_789', 'kanban.update')
                """,
                (_from_unix(now_ts - 30).isoformat(), now_ts - 30),
            )
            conn.commit()

            findings = detect_reality_without_belief(str(db_path))
            conn.close()

            assert len(findings) == 1
            assert findings[0].details["subject"] == "t_789"
