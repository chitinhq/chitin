"""Test suite for spec 022: Dispatch Readiness Contract.

Validates R1–R5 from .specify/specs/022-dispatch-readiness-contract/spec.md.

R1: Single source of truth — watchdog consumes board_resolver.spec_dir_for_board
R2: Board config requires spec_source field
R3: kanban-flow ready rejects NULL assignee
R3: kanban-flow ready accepts valid assignees
R4: Readiness runbook exists with all 5 checks
R5: Watchdog post includes spec-root decision telemetry
"""

import json
import os
import re
import sqlite3
import subprocess
import textwrap
from pathlib import Path
from unittest.mock import patch

import pytest

# ---------------------------------------------------------------------------
# Repo root resolution
# ---------------------------------------------------------------------------
REPO_ROOT = Path(__file__).resolve().parent.parent.parent
SWARM_BIN = REPO_ROOT / "swarm" / "bin"
SCRIPTS = REPO_ROOT / "scripts"
DOCS = REPO_ROOT / "docs" / "governance-setup-extras"

BOARD_RESOLVER = SWARM_BIN / "board_resolver.py"
WATCHDOG = SWARM_BIN / "board-watchdog-bounded.py"
KANBAN_FLOW = SCRIPTS / "kanban-flow"
RUNBOOK = DOCS / "dispatch-readiness.md"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _read_py(path: Path) -> str:
    return path.read_text()


# ===========================================================================
# R1: Single source of truth — watchdog imports and calls spec_dir_for_board
# ===========================================================================

class TestR1SingleSourceOfTruth:
    """R1: board-watchdog-bounded.py must consume board_resolver.spec_dir_for_board
    and must NOT contain a hardcoded WORKSPACE_ROOT/.specify/specs path
    construction for spec_root."""

    def test_watchdog_imports_spec_dir_for_board(self):
        """Watchdog imports spec_dir_for_board from board_resolver."""
        source = _read_py(WATCHDOG)
        assert "from board_resolver import" in source
        assert "spec_dir_for_board" in source

    def test_watchdog_no_hardcoded_spec_root(self):
        """No hardcoded WORKSPACE_ROOT/.specify/specs or CHITIN_REPO/.specify/specs
        construction for spec_root remains in the watchdog source."""
        source = _read_py(WATCHDOG)
        # Match patterns like: WORKSPACE_ROOT / ".specify" / "specs"
        # or: CHITIN_REPO / ".specify" / "specs"
        # but NOT inside comments/deprecation notes
        lines = source.splitlines()
        for i, line in enumerate(lines):
            # Skip comment-only lines
            stripped = line.lstrip()
            if stripped.startswith("#"):
                continue
            # Check for hardcoded spec root construction
            assert (
                '"/.specify" / "specs"' not in line
                and '".specify" / "specs"' not in line
            ), (
                f"R1 violation at line {i+1}: hardcoded spec root path found: {line.strip()}\n"
                "All spec-root resolution must go through board_resolver.spec_dir_for_board"
            )

    def test_watchdog_no_boards_dict_with_spec_root(self):
        """The old BOARDS dict with spec_root entries must not exist."""
        source = _read_py(WATCHDOG)
        # The BOARDS dict should be a simple tuple of board names, not a dict
        # with spec_root/db keys
        assert '"spec_root"' not in source or "spec_root" not in source.split("BOARDS")[0].split("\n")[-1] if "BOARDS" in source else True
        # More precise: BOARDS should be a tuple, not a dict
        boards_match = re.search(r'BOARDS\s*=\s*(\[.*?\]|\(.*?\))', source, re.DOTALL)
        if boards_match:
            val = boards_match.group(1)
            # Should be a simple iterable of strings, not a dict
            assert "{" not in val, f"BOARDS should be a tuple/list of board names, not a dict. Got: {val[:200]}"

    def test_watchdog_calls_spec_dir_for_board_in_classify(self):
        """classify() function calls spec_dir_for_board(board) instead of
        indexing a BOARDS dict."""
        source = _read_py(WATCHDOG)
        # Find the classify function
        classify_match = re.search(
            r'def classify\(board.*?\):.*?(?=\ndef |\Z)',
            source, re.DOTALL
        )
        assert classify_match, "classify function not found in watchdog"
        classify_body = classify_match.group(0)
        assert "spec_dir_for_board" in classify_body, (
            "R1 violation: classify() must call spec_dir_for_board(board) "
            "instead of BOARDS[board]['spec_root']"
        )

    def test_watchdog_calls_spec_dir_for_board_in_connect(self):
        """connect() resolves DB path through board_resolver.resolve_db()."""
        source = _read_py(WATCHDOG)
        connect_match = re.search(
            r'def connect\(board.*?\):.*?(?=\ndef |\Z)',
            source, re.DOTALL
        )
        assert connect_match, "connect function not found in watchdog"
        connect_body = connect_match.group(0)
        assert "resolve_db" in connect_body, (
            "R1 violation: connect() must use board_resolver.resolve_db(board) "
            "instead of BOARDS[board]['db']"
        )


# ===========================================================================
# R2: Board config requires spec_source
# ===========================================================================

class TestR2ExplicitSpecSource:
    """R2: Board config schema gains an explicit spec_source field.
    board_resolver reads it and no longer falls back to owned_orgs default-set."""

    def test_chitin_board_config_has_spec_source(self, tmp_path):
        """The chitin board config must declare spec_source."""
        from board_resolver import board_config
        # We check that the actual board config has the key
        cfg = board_config("chitin")
        assert "spec_source" in cfg, (
            "R2 violation: chitin board config must include 'spec_source' key. "
            f"Got keys: {sorted(cfg.keys())}"
        )

    def test_spec_dir_for_board_repo_source(self, tmp_path, monkeypatch):
        """When spec_source='repo', spec_dir_for_board returns workspace/.specify/specs."""
        # Create a temp board config
        config_dir = tmp_path / "boards" / "testboard"
        config_dir.mkdir(parents=True)
        (config_dir / "config.json").write_text(json.dumps({
            "spec_source": "repo",
            "workspace_root": str(tmp_path / "ws"),
        }))

        monkeypatch.setenv("KANBAN_BOARDS_DIR", str(tmp_path / "boards"))
        monkeypatch.setenv("KANBAN_BOARD", "testboard")

        # Reimport to pick up new env
        import importlib
        import board_resolver as br
        importlib.reload(br)

        result = br.spec_dir_for_board("testboard")
        assert result == Path(tmp_path / "ws") / ".specify" / "specs"

    def test_spec_dir_for_board_workspace_overlay_source(self, tmp_path, monkeypatch):
        """When spec_source='workspace_overlay', spec_dir_for_board returns
        workspace overlay path."""
        config_dir = tmp_path / "boards" / "testboard2"
        config_dir.mkdir(parents=True)
        (config_dir / "config.json").write_text(json.dumps({
            "spec_source": "workspace_overlay",
        }))

        monkeypatch.setenv("KANBAN_BOARDS_DIR", str(tmp_path / "boards"))
        monkeypatch.setenv("KANBAN_BOARD", "testboard2")
        monkeypatch.setenv("WORKSPACE_ROOT", str(tmp_path / "ws"))

        import importlib
        import board_resolver as br
        importlib.reload(br)

        result = br.spec_dir_for_board("testboard2")
        assert result == Path(tmp_path / "ws") / ".specify" / "specs"

    def test_owned_orgs_default_set_removed(self):
        """The owned_orgs() function must NOT include a hardcoded
        {'chitinhq', 'red'} default. Only config and env var values."""
        source = _read_py(BOARD_RESOLVER)
        # The old default line looked like: orgs = {"chitinhq", "red"}
        # It must not appear as a set literal with both names
        # Check that no non-comment line contains a hardcoded default-set
        # assignment with both org names.
        for i, line in enumerate(source.splitlines(), 1):
            stripped = line.lstrip()
            if stripped.startswith('#'):
                continue
            # Skip lines inside docstrings (heuristic: triple-quote delimiters)
            if '"""' in stripped or "'''" in stripped:
                continue
            assert not (
                '"chitinhq"' in stripped and '"red"' in stripped
                and '=' in stripped
                and 'owned_orgs' not in stripped.split('=')[0]
            ), (
                f"R2 violation at line {i}: owned_orgs() must not contain "
                f"hardcoded default set. Only config/env values. Line: {stripped[:100]}"
            )

    def test_spec_dir_for_board_deprecation_warning_missing_source(self, tmp_path, monkeypatch):
        """When spec_source is missing from config, spec_dir_for_board should
        emit a deprecation warning but still return a path."""
        config_dir = tmp_path / "boards" / "testboard3"
        config_dir.mkdir(parents=True)
        (config_dir / "config.json").write_text(json.dumps({}))

        monkeypatch.setenv("KANBAN_BOARDS_DIR", str(tmp_path / "boards"))
        monkeypatch.setenv("KANBAN_BOARD", "testboard3")

        import importlib
        import board_resolver as br
        importlib.reload(br)

        import warnings
        with warnings.catch_warnings(record=True) as w:
            warnings.simplefilter("always")
            result = br.spec_dir_for_board("testboard3")
            # Should still return a valid path
            assert isinstance(result, Path)
            # Should have emitted a deprecation warning
            deprecation_warnings = [x for x in w if issubclass(x.category, DeprecationWarning)]
            assert len(deprecation_warnings) >= 1, (
                "R2: Missing spec_source should emit a DeprecationWarning"
            )


# ===========================================================================
# R3: kanban-flow ready rejects NULL assignee
# ===========================================================================

class TestR3AssigneeGate:
    """R3: kanban-flow ready rejects tickets with no assignee. Error message
    names the valid set + the routing-lane alternative."""

    @pytest.fixture
    def kanban_db(self, tmp_path):
        """Create a minimal kanban DB for testing."""
        db_path = tmp_path / "kanban.db"
        con = sqlite3.connect(str(db_path))
        con.execute("""
            CREATE TABLE IF NOT EXISTS tasks (
                id TEXT PRIMARY KEY,
                title TEXT NOT NULL,
                body TEXT,
                assignee TEXT,
                status TEXT NOT NULL DEFAULT 'triage',
                priority INTEGER NOT NULL DEFAULT 0,
                created_by TEXT,
                created_at INTEGER NOT NULL
            )
        """)
        con.execute("""
            CREATE TABLE IF NOT EXISTS task_comments (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                task_id TEXT NOT NULL,
                author TEXT,
                body TEXT,
                created_at INTEGER NOT NULL
            )
        """)
        con.execute("""
            CREATE TABLE IF NOT EXISTS task_events (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                task_id TEXT NOT NULL,
                run_id INTEGER,
                kind TEXT NOT NULL,
                payload TEXT,
                created_at INTEGER NOT NULL
            )
        """)
        now = int(__import__("time").time())
        # Use hex-only ticket ID to match kanban-flow's t_[a-f0-9]+ validation
        con.execute(
            "INSERT INTO tasks (id, title, assignee, status, created_at) "
            "VALUES (?, ?, ?, ?, ?)",
            ("t_a0b1c2d3", "Test ticket", None, "triage", now),
        )
        con.commit()
        con.close()
        return db_path

    def test_ready_rejects_null_assignee(self, kanban_db, tmp_path):
        """kanban-flow ready must reject a ticket where assignee IS NULL
        and no --assignee override is given."""
        env = os.environ.copy()
        env["KANBAN_DB"] = str(kanban_db)
        env["KANBAN_BOARD"] = "test"

        result = subprocess.run(
            [str(KANBAN_FLOW), "ready", "t_a0b1c2d3"],
            capture_output=True,
            text=True,
            env=env,
            timeout=10,
        )
        assert result.returncode != 0, (
            "R3 violation: kanban-flow ready must reject tickets with NULL assignee"
        )
        # Error message must name the valid values
        output = result.stdout + result.stderr
        valid_values = ["codex", "copilot", "claude-code", "gemini", "clawta", "red"]
        found = sum(1 for v in valid_values if v in output)
        assert found >= 3, (
            f"R3 violation: error message must name valid assignee values. "
            f"Got: {output[:200]}"
        )

    @pytest.mark.parametrize("assignee", [
        "codex", "copilot", "claude-code", "gemini", "clawta", "red"
    ])
    def test_ready_accepts_valid_assignees(self, kanban_db, tmp_path, assignee):
        """kanban-flow ready must accept each valid assignee via --assignee."""
        env = os.environ.copy()
        env["KANBAN_DB"] = str(kanban_db)
        env["KANBAN_BOARD"] = "test"

        # Reset the ticket to triage for each param
        con = sqlite3.connect(str(kanban_db))
        con.execute("UPDATE tasks SET status='triage', assignee=NULL WHERE id='t_a0b1c2d3'")
        con.commit()
        con.close()

        result = subprocess.run(
            [str(KANBAN_FLOW), "ready", "t_a0b1c2d3", "--assignee", assignee],
            capture_output=True,
            text=True,
            env=env,
            timeout=10,
        )
        assert result.returncode == 0, (
            f"R3 violation: kanban-flow ready must accept assignee={assignee}. "
            f"Exit code: {result.returncode}. Output: {result.stdout + result.stderr}"
        )


# ===========================================================================
# R4: Readiness runbook exists with all 5 checks
# ===========================================================================

class TestR4Runbook:
    """R4: docs/governance-setup-extras/dispatch-readiness.md exists and
    documents all 5 dispatch-readiness checks."""

    def test_readiness_runbook_exists(self):
        """The runbook file must exist."""
        assert RUNBOOK.exists(), (
            f"R4 violation: runbook not found at {RUNBOOK}"
        )

    @pytest.mark.parametrize("check_text", [
        "invariants_and_boundaries",
        "spec-kit entry",
        "assignee",
        "Blocked until",
        "tracking-epic",
    ])
    def test_runbook_contains_all_5_checks(self, check_text):
        """The runbook must document each of the 5 readiness checks."""
        content = RUNBOOK.read_text().lower()
        # Use partial matching since exact phrasing may vary
        alternatives = {
            "invariants_and_boundaries": ["invariants_and_boundaries", "invariants and boundaries", "invariants"],
            "spec-kit entry": ["spec-kit", "spec entry", "spec root"],
            "assignee": ["assignee"],
            "Blocked until": ["blocked until", "blocked_until", "unresolved"],
            "tracking-epic": ["tracking-epic", "tracking epic", "tracking_epic"],
        }
        alts = alternatives.get(check_text, [check_text])
        found = any(alt.lower() in content for alt in alts)
        assert found, (
            f"R4 violation: runbook missing check '{check_text}'. "
            f"Searched for: {alts}"
        )


# ===========================================================================
# R5: Watchdog post includes spec-root decision telemetry
# ===========================================================================

class TestR5WatchdogTelemetry:
    """R5: Watchdog Discord post MUST include resolution-decision telemetry:
    'spec root: <path> (source: repo|workspace_overlay|owned_orgs)'"""

    def test_watchdog_report_includes_spec_root_decision(self, tmp_path, monkeypatch):
        """When the watchdog runs, its output must include spec root and source
        lines for each board."""
        # We check the source code contains the telemetry output
        source = _read_py(WATCHDOG)

        # The report_board function must output spec root telemetry
        assert "spec root:" in source, (
            "R5 violation: watchdog must emit 'spec root:' telemetry line"
        )
        assert "source:" in source, (
            "R5 violation: watchdog must emit 'source:' in telemetry line"
        )

    def test_watchdog_uses_spec_dir_for_board_for_telemetry(self):
        """The telemetry line must use spec_dir_for_board output, not a
        hardcoded path."""
        source = _read_py(WATCHDOG)
        report_match = re.search(
            r'def report_board\(board[^)]*\)[^:]*:.*?(?=\ndef [a-z_])',
            source, re.DOTALL
        )
        assert report_match, "report_board function not found in watchdog"
        report_body = report_match.group(0)
        # Must call spec_dir_for_board in the report function
        assert "spec_dir_for_board" in report_body, (
            "R5 violation: report_board must call spec_dir_for_board for telemetry"
        )