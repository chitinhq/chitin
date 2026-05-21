"""Conftest for misroute tests: pytest fixtures backed by test_misroute_infra.

All fixtures use the shared helpers and schemas from test_misroute_infra.
Every test runs inside a SAVEPOINT that is rolled back on teardown —
no persistent state leaks between tests.
"""
from __future__ import annotations

import sqlite3
from pathlib import Path

import pytest

# Import shared infrastructure from the module
from test_misroute_infra import (
    AGENT_BUS_SCHEMA,
    AGENT_CHANNELS,
    ALL_CHANNEL_IDS,
    CHANNEL_ARES,
    CHANNEL_ARGUS,
    CHANNEL_CLAWTA,
    CHANNEL_ICARUS,
    CHANNEL_SWARM,
    GATEWAY_LOG_SCHEMA,
    HERMES_CHANNEL_ID,
    KANBAN_SCHEMA_FALLBACK,
    assert_temp_path,
    make_bus_db,
    make_gateway_db,
)


# ── Fixture: test bus DB with transactional rollback ────────────────────

@pytest.fixture
def bus_db(tmp_path):
    """Provide a fresh agent-bus DB in a temp directory.

    Every test runs inside a SAVEPOINT that is rolled back on teardown,
    so no mutations persist between tests. The connection uses
    sqlite3.Row for dict-style access.

    The DB path is explicitly validated to never resolve under
    ~/.chitin or ~/.hermes.
    """
    conn = make_bus_db(tmp_path)
    conn.execute("SAVEPOINT test_isolation")
    yield conn
    # Rollback to the savepoint — all test mutations are discarded
    try:
        conn.execute("ROLLBACK TO SAVEPOINT test_isolation")
        conn.commit()
    except sqlite3.OperationalError:
        pass  # already closed or no savepoint
    conn.close()


@pytest.fixture
def gateway_db(tmp_path):
    """Provide a fresh gateway-log DB for receipt tracking.

    Same isolation guarantees as bus_db: transactional rollback on
    teardown, temp-only path enforcement.
    """
    conn = make_gateway_db(tmp_path)
    conn.execute("SAVEPOINT test_isolation")
    yield conn
    try:
        conn.execute("ROLLBACK TO SAVEPOINT test_isolation")
        conn.commit()
    except sqlite3.OperationalError:
        pass
    conn.close()


@pytest.fixture
def channel_ids():
    """Provide the 5 channel IDs as a dict keyed by agent name.

    Returns:
        dict like {"hermes": "1503438297597350062", "clawta": ..., ...}
        Also includes "ares" as alias for "hermes" for clarity.
    """
    ids = dict(AGENT_CHANNELS)
    ids["ares"] = ids["hermes"]  # alias for clarity
    return ids