from __future__ import annotations

import importlib.util
import sqlite3
import tempfile
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "workflows" / "_swarm_elo.py"


def load_module():
    spec = importlib.util.spec_from_loader("swarm_elo", SourceFileLoader("swarm_elo", str(SCRIPT)))
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class SwarmEloTests(unittest.TestCase):
    def test_open_db_migrates_legacy_schema(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db = Path(tmpdir) / "clawta.db"
            conn = sqlite3.connect(db)
            conn.executescript(
                """
                CREATE TABLE swarm_elo (
                    id INTEGER PRIMARY KEY,
                    driver TEXT NOT NULL,
                    model TEXT NOT NULL,
                    task_class TEXT,
                    elo_score REAL NOT NULL,
                    dispatches_count INTEGER NOT NULL DEFAULT 0,
                    last_dispatch_id TEXT,
                    last_updated INTEGER NOT NULL,
                    UNIQUE(driver, model, task_class)
                );

                CREATE TABLE swarm_dispatch_scores (
                    id INTEGER PRIMARY KEY,
                    ticket_id TEXT NOT NULL,
                    pr_url TEXT,
                    driver TEXT NOT NULL,
                    model TEXT NOT NULL,
                    task_class TEXT,
                    code_quality INTEGER,
                    test_coverage INTEGER,
                    scope_adherence INTEGER,
                    efficiency INTEGER,
                    review_friendliness INTEGER,
                    total_score INTEGER,
                    judge_model TEXT NOT NULL,
                    judge_reasoning TEXT,
                    scored_at INTEGER NOT NULL
                );

                INSERT INTO swarm_elo (
                    driver, model, task_class, elo_score, dispatches_count, last_dispatch_id, last_updated
                ) VALUES ('codex', 'gpt-5.5', 'feature', 1512.5, 3, 't_old', 1234);
                """
            )
            conn.commit()
            conn.close()

            elo = load_module()
            elo.DB_PATH = db
            migrated = elo.open_db()

            row = migrated.execute(
                "SELECT driver, model, role, task_class, complexity_bucket, dispatches_count, first_scored_at "
                "FROM swarm_elo"
            ).fetchone()
            self.assertEqual(
                dict(row),
                {
                    "driver": "codex",
                    "model": "gpt-5.5",
                    "role": "",
                    "task_class": "feature",
                    "complexity_bucket": "",
                    "dispatches_count": 3,
                    "first_scored_at": 1234,
                },
            )

            cols = {
                col[1]
                for col in migrated.execute("PRAGMA table_info(swarm_dispatch_scores)").fetchall()
            }
            self.assertTrue({"role", "capabilities_json", "pr_outcome", "ci_outcome", "review_outcome"} <= cols)

    def test_record_score_and_update_elo_use_rich_dimensions(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            elo = load_module()
            elo.DB_PATH = Path(tmpdir) / "clawta.db"
            conn = elo.open_db()

            score_id = elo.record_score(
                conn,
                "t_12345678",
                "https://github.com/chitinhq/chitin/pull/123",
                "codex",
                "gpt-5.5",
                "feature",
                {
                    "code_quality": 5,
                    "test_coverage": 4,
                    "scope_adherence": 5,
                    "efficiency": 4,
                    "review_friendliness": 5,
                },
                "clawta-glm-5.1",
                "Solid patch.",
                role="programmer",
                complexity_bucket="medium",
                capabilities=["database", "testing"],
                pr_outcome="open",
                ci_outcome="pending",
                review_outcome="pending",
                pr_created_at=100,
                pr_updated_at=120,
            )
            new_elo = elo.update_elo(
                conn,
                "codex",
                "gpt-5.5",
                "feature",
                23,
                last_dispatch_id="t_12345678",
                role="programmer",
                complexity_bucket="medium",
                capabilities=["testing", "database"],
                pr_outcome="open",
                ci_outcome="pending",
                review_outcome="pending",
                scored_at=200,
            )

            self.assertGreater(score_id, 0)
            self.assertGreater(new_elo, elo.BASE_ELO)

            row = conn.execute(
                "SELECT role, complexity_bucket, capabilities_key, pr_outcome, ci_outcome, review_outcome "
                "FROM swarm_dispatch_scores WHERE id = ?",
                (score_id,),
            ).fetchone()
            self.assertEqual(
                dict(row),
                {
                    "role": "programmer",
                    "complexity_bucket": "medium",
                    "capabilities_key": "database,testing",
                    "pr_outcome": "open",
                    "ci_outcome": "pending",
                    "review_outcome": "pending",
                },
            )

            elo_row = elo.lookup_elo(
                conn,
                "codex",
                "gpt-5.5",
                "feature",
                role="programmer",
                complexity_bucket="medium",
                capabilities=["database", "testing"],
                pr_outcome="open",
                ci_outcome="pending",
                review_outcome="pending",
            )
            self.assertIsNotNone(elo_row)
            assert elo_row is not None
            self.assertEqual(elo_row["dispatches_count"], 1)

    def test_aggregate_scores_by_driver_model_and_task_class(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            elo = load_module()
            elo.DB_PATH = Path(tmpdir) / "clawta.db"
            conn = elo.open_db()

            elo.update_elo(
                conn, "codex", "gpt-5.5", "feature", 20,
                role="programmer", complexity_bucket="small",
                capabilities=["database"], pr_outcome="open",
                ci_outcome="pending", review_outcome="pending", scored_at=100,
            )
            elo.update_elo(
                conn, "codex", "gpt-5.5", "feature", 18,
                role="programmer", complexity_bucket="medium",
                capabilities=["database", "testing"], pr_outcome="open",
                ci_outcome="passed", review_outcome="approved", scored_at=120,
            )
            elo.update_elo(
                conn, "claude-code", "sonnet", "bugfix", 24,
                role="programmer", complexity_bucket="small",
                capabilities=["testing"], pr_outcome="open",
                ci_outcome="passed", review_outcome="approved", scored_at=140,
            )

            rows = elo.aggregate_scores(conn, group_by="driver_model_task_class", limit=10)
            first = rows[0]
            self.assertTrue({"driver", "model", "task_class", "weighted_elo", "dispatches_count"} <= set(first.keys()))
            target = next(row for row in rows if row["driver"] == "codex" and row["model"] == "gpt-5.5")
            self.assertEqual(target["task_class"], "feature")
            self.assertEqual(target["dispatches_count"], 2)


if __name__ == "__main__":
    unittest.main()
