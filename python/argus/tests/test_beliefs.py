"""Tests for the belief-ingestion adapters."""
from __future__ import annotations

import json
import sqlite3
import tempfile
from pathlib import Path

from argus.beliefs import (
    init_beliefs_table,
    ingest_agent_cards,
    ingest_clawta_swarm_elo,
    ingest_wiki,
)
from argus.cross_source_db import init_cross_source_db


class TestAgentCards:
    def test_empty_directory(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs_conn = init_cross_source_db(Path(tmpdir) / "xs.db")
            init_beliefs_table(xs_conn)
            try:
                result = ingest_agent_cards(Path(tmpdir) / "empty", xs_conn)
            finally:
                xs_conn.close()
            assert result["inserted"] == 0
            assert result["agents"] == []

    def test_minimal_card(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            cards = Path(tmpdir) / "cards"
            cards.mkdir()
            (cards / "agent_x.json").write_text(json.dumps({
                "id": "agent_x",
                "description": "Test agent.",
                "capabilities": [{"skill": "go", "depth": "expert"}],
                "models": [{"id": "m1", "tier": "T2", "premium_cost": 0.5}],
            }))
            xs_conn = init_cross_source_db(Path(tmpdir) / "xs.db")
            init_beliefs_table(xs_conn)
            try:
                result = ingest_agent_cards(cards, xs_conn)
                rows = xs_conn.execute(
                    "SELECT agent, subject, claim FROM beliefs ORDER BY subject"
                ).fetchall()
            finally:
                xs_conn.close()
            assert result["inserted"] == 3  # description + capability + model
            subjects = {r[1] for r in rows}
            assert "self.description" in subjects
            assert "capability.go" in subjects
            assert "model.m1" in subjects

    def test_idempotent_on_replay(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            cards = Path(tmpdir) / "cards"
            cards.mkdir()
            (cards / "a.json").write_text(json.dumps({
                "id": "a", "capabilities": [{"skill": "x", "depth": "strong"}],
            }))
            xs_conn = init_cross_source_db(Path(tmpdir) / "xs.db")
            init_beliefs_table(xs_conn)
            try:
                r1 = ingest_agent_cards(cards, xs_conn)
                r2 = ingest_agent_cards(cards, xs_conn)
            finally:
                xs_conn.close()
            assert r1["inserted"] == 1
            assert r2["inserted"] == 0
            assert r2["skipped"] == 1

    def test_skips_backup_and_disabled_cards(self):
        """*.bak, *.disabled cards must not produce beliefs."""
        with tempfile.TemporaryDirectory() as tmpdir:
            cards = Path(tmpdir) / "cards"
            cards.mkdir()
            (cards / "real.json").write_text(json.dumps({"id": "real"}))
            (cards / "real.json.bak").write_text(json.dumps({"id": "real_bak"}))
            (cards / "old.disabled.json").write_text(json.dumps({"id": "old"}))
            xs_conn = init_cross_source_db(Path(tmpdir) / "xs.db")
            init_beliefs_table(xs_conn)
            try:
                result = ingest_agent_cards(cards, xs_conn)
            finally:
                xs_conn.close()
            assert result["agents"] == ["real"]

    def test_malformed_card_skipped(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            cards = Path(tmpdir) / "cards"
            cards.mkdir()
            (cards / "broken.json").write_text("not valid json{")
            (cards / "good.json").write_text(json.dumps({
                "id": "good", "capabilities": [{"skill": "x", "depth": "strong"}],
            }))
            xs_conn = init_cross_source_db(Path(tmpdir) / "xs.db")
            init_beliefs_table(xs_conn)
            try:
                result = ingest_agent_cards(cards, xs_conn)
            finally:
                xs_conn.close()
            assert result["inserted"] == 1
            assert set(result["agents"]) == {"broken", "good"}  # files seen; broken yields zero beliefs


class TestClawtaSwarmElo:
    def test_missing_db_zero_inserts(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs_conn = init_cross_source_db(Path(tmpdir) / "xs.db")
            init_beliefs_table(xs_conn)
            try:
                result = ingest_clawta_swarm_elo(Path(tmpdir) / "absent.db", xs_conn)
            finally:
                xs_conn.close()
            assert result["inserted"] == 0

    def test_swarm_elo_rows_become_beliefs(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            clawta = tmpdir / "clawta.db"
            src = sqlite3.connect(str(clawta))
            src.executescript("""
                CREATE TABLE swarm_elo (
                    id INTEGER PRIMARY KEY,
                    driver TEXT NOT NULL,
                    model TEXT NOT NULL,
                    role TEXT NOT NULL DEFAULT '',
                    task_class TEXT NOT NULL DEFAULT '',
                    complexity_bucket TEXT NOT NULL DEFAULT '',
                    capabilities_key TEXT NOT NULL DEFAULT '',
                    pr_outcome TEXT NOT NULL DEFAULT '',
                    ci_outcome TEXT NOT NULL DEFAULT '',
                    review_outcome TEXT NOT NULL DEFAULT '',
                    elo_score REAL NOT NULL,
                    dispatches_count INTEGER NOT NULL DEFAULT 0,
                    last_dispatch_id TEXT,
                    first_scored_at INTEGER NOT NULL,
                    last_updated INTEGER NOT NULL
                );
                INSERT INTO swarm_elo (driver, model, role, task_class, elo_score,
                                       dispatches_count, first_scored_at, last_updated)
                VALUES
                  ('claude-code', 'opus-4-7', 'refactor', '', 1530.0, 3, 1000, 2000),
                  ('codex', 'gpt-5.5', '', '', 1500.0, 1, 1000, 2000);
            """)
            src.close()

            xs_conn = init_cross_source_db(tmpdir / "xs.db")
            init_beliefs_table(xs_conn)
            try:
                result = ingest_clawta_swarm_elo(clawta, xs_conn)
                rows = xs_conn.execute(
                    "SELECT agent, subject, claim FROM beliefs ORDER BY subject"
                ).fetchall()
            finally:
                xs_conn.close()
            assert result["inserted"] == 2
            assert {r[0] for r in rows} == {"router"}
            subjects = {r[1] for r in rows}
            assert any("claude-code" in s for s in subjects)
            assert any("codex" in s for s in subjects)


class TestWiki:
    def test_missing_root_zero(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs_conn = init_cross_source_db(Path(tmpdir) / "xs.db")
            init_beliefs_table(xs_conn)
            try:
                result = ingest_wiki(Path(tmpdir) / "missing", xs_conn)
            finally:
                xs_conn.close()
            assert result["files"] == 0

    def test_frontmatter_extracted(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            wiki = tmpdir / "wiki"
            wiki.mkdir()
            (wiki / "topic.md").write_text(
                "---\n"
                "title: Topic\n"
                "owner: red\n"
                "---\n"
                "# Topic\n\nBody.\n"
            )
            xs_conn = init_cross_source_db(tmpdir / "xs.db")
            init_beliefs_table(xs_conn)
            try:
                result = ingest_wiki(wiki, xs_conn)
                rows = xs_conn.execute(
                    "SELECT subject, claim FROM beliefs ORDER BY subject"
                ).fetchall()
            finally:
                xs_conn.close()
            assert result["files"] == 1
            assert result["inserted"] == 2
            claims = dict(rows)
            assert claims["wiki:topic/title"] == "Topic"
            assert claims["wiki:topic/owner"] == "red"

    def test_no_frontmatter_degrades_to_title(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            wiki = tmpdir / "wiki"
            wiki.mkdir()
            (wiki / "no_fm.md").write_text("# Plain Article\n\nBody only.\n")
            xs_conn = init_cross_source_db(tmpdir / "xs.db")
            init_beliefs_table(xs_conn)
            try:
                result = ingest_wiki(wiki, xs_conn)
                rows = xs_conn.execute(
                    "SELECT subject, claim FROM beliefs"
                ).fetchall()
            finally:
                xs_conn.close()
            assert result["inserted"] == 1
            assert rows[0][1] == "title=Plain Article"
