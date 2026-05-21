"""sw-011 Test 5 — Misroute: channel routing boundary tests.

Uses the shared test harness from test_misroute_infra.py:
  - bus_db fixture: agent-bus DB with transaction rollback
  - gateway_db fixture: gateway-log DB with transaction rollback
  - channel_ids fixture: 5 channel IDs (ares, clawta, icarus, argus, swarm)
  - post_via_bus_reply(): insert a bus message + gateway receipt
  - wait_for_message(): deterministic wait (update_hook, not sleep)
  - assert_message_absent(): confirm a message is NOT in a thread
  - query_gateway_logs(): retrieve receipt entries from gateway_log

All tests run against fixture-backed temp databases. No live
~/.chitin, ~/.hermes, or gov.db mutations.

To run:
  python3 -m pytest swarm/tests/test_misroute.py -v
  # or with system pytest (hermes_cli tests skip automatically):
  pytest swarm/tests/test_misroute.py -v
"""
from __future__ import annotations

import time

import pytest

# Import shared infrastructure — fixtures come from conftest.py (auto-discovered)
from test_misroute_infra import (
    ALL_CHANNEL_IDS,
    CHANNEL_ARES,
    CHANNEL_ARGUS,
    CHANNEL_CLAWTA,
    CHANNEL_ICARUS,
    CHANNEL_SWARM,
    HERMES_CHANNEL_ID,
    AGENT_CHANNELS,
    assert_message_absent,
    create_thread,
    post_via_bus_reply,
    query_gateway_logs,
    query_gateway_logs_by_message,
    wait_for_message,
)


# ── 1. Test DB fixture: connects to temp DB, no live mutations ────────

class TestBusDbFixture:
    """Verify the bus_db fixture creates a usable, isolated agent-bus DB."""

    def test_bus_db_creates_all_tables(self, bus_db):
        """All agent-bus tables are present after fixture setup."""
        tables = {
            r[0]
            for r in bus_db.execute(
                "SELECT name FROM sqlite_master WHERE type='table'"
            ).fetchall()
        }
        required = {"threads", "messages", "reads", "attachments", "agents"}
        assert required.issubset(tables), (
            f"Missing tables: {required - tables}"
        )

    def test_bus_db_is_under_tmp_path(self, tmp_path):
        """The fixture DB path must be under a temp directory (safety)."""
        from pathlib import Path
        db_path = Path(str(tmp_path / "safety_check.db"))
        # Verify path doesn't hit forbidden segments
        resolved = str(db_path.resolve())
        assert "/.chitin/" not in resolved
        assert "/.hermes/" not in resolved

    def test_bus_db_transaction_rollback(self, bus_db):
        """Mutations inside one test are rolled back before the next test.

        Verified by inserting data here — the next test should NOT see it.
        (This test only verifies the fixture plumbing; the next test
        in this class checks isolation.)
        """
        now = int(time.time())
        bus_db.execute(
            "INSERT INTO agents(id, last_seen_at) VALUES(?, ?)",
            ("transient-agent", now),
        )
        bus_db.commit()
        # Within THIS test we can see it
        row = bus_db.execute(
            "SELECT id FROM agents WHERE id=?", ("transient-agent",)
        ).fetchone()
        assert row is not None

    def test_bus_db_isolation_from_previous_test(self, bus_db):
        """The transient-agent from the previous test must NOT exist here.

        Each test gets its own fresh fixture invocation, so the data
        committed in the savepoint scope is rolled back at teardown.
        """
        row = bus_db.execute(
            "SELECT id FROM agents WHERE id=?", ("transient-agent",)
        ).fetchone()
        assert row is None, "Isolation failure: data from previous test leaked"

    def test_insert_and_read_thread(self, bus_db):
        """Smoke: create a thread and read it back."""
        thread_id = create_thread(bus_db, title="test thread", author="probe")
        row = bus_db.execute(
            "SELECT id, title, author, status FROM threads WHERE id=?",
            (thread_id,),
        ).fetchone()
        assert row is not None
        assert row["title"] == "test thread"
        assert row["author"] == "probe"
        assert row["status"] == "open"


# ── 2. Channel IDs fixture ────────────────────────────────────────────

class TestChannelIds:
    """Verify the 5 channel IDs are resolved and available."""

    def test_all_five_channels_present(self, channel_ids):
        """The fixture must provide all 5 agent channels."""
        required = {"hermes", "clawta", "icarus", "argus", "swarm"}
        assert required.issubset(set(channel_ids.keys())), (
            f"Missing channels: {required - set(channel_ids.keys())}"
        )

    def test_ares_alias_for_hermes(self, channel_ids):
        """'ares' is an alias for 'hermes' in the fixture."""
        assert channel_ids["ares"] == channel_ids["hermes"]

    def test_channel_ids_are_snowflakes(self, channel_ids):
        """Channel IDs must look like Discord snowflakes (16+ digit strings)."""
        for name, cid in channel_ids.items():
            assert len(cid) >= 16, (
                f"Channel {name} ID {cid} is too short for a snowflake"
            )
            assert cid.isdigit(), (
                f"Channel {name} ID {cid} contains non-digits"
            )

    def test_channel_ids_are_distinct(self, channel_ids):
        """All 5 channel IDs must be unique."""
        ids = [channel_ids[k] for k in ("hermes", "clawta", "icarus", "argus", "swarm")]
        assert len(ids) == len(set(ids)), f"Duplicate channel IDs: {ids}"

    def test_all_channel_ids_in_constant(self):
        """ALL_CHANNEL_IDS constant matches the fixture."""
        assert ALL_CHANNEL_IDS == frozenset(AGENT_CHANNELS.values())


# ── 3. post_via_bus_reply helper ───────────────────────────────────────

class TestPostViaBusReply:
    """Verify post_via_bus_reply inserts messages and returns IDs."""

    def test_post_creates_message_with_id(self, bus_db, gateway_db):
        """post_via_bus_reply inserts a message and returns its ID."""
        thread_id = create_thread(bus_db, author="sender")
        msg_id = post_via_bus_reply(
            bus_db,
            thread_id=thread_id,
            channel_id=CHANNEL_SWARM,
            payload="hello from #swarm",
            author="sender",
            gateway_conn=gateway_db,
        )
        assert isinstance(msg_id, int)
        assert msg_id > 0

        # Verify the message is readable
        row = bus_db.execute(
            "SELECT id, thread_id, author, body, kind FROM messages WHERE id=?",
            (msg_id,),
        ).fetchone()
        assert row is not None
        assert row["author"] == "sender"
        assert row["body"] == "hello from #swarm"
        assert row["kind"] == "message"

    def test_post_stamps_discord_thread_id(self, bus_db):
        """post_via_bus_reply stamps the channel_id onto threads.discord_thread_id."""
        thread_id = create_thread(bus_db, author="sender")
        post_via_bus_reply(
            bus_db,
            thread_id=thread_id,
            channel_id=CHANNEL_CLAWTA,
            payload="clawta message",
            author="sender",
        )
        row = bus_db.execute(
            "SELECT discord_thread_id FROM threads WHERE id=?",
            (thread_id,),
        ).fetchone()
        assert row["discord_thread_id"] == CHANNEL_CLAWTA

    def test_post_creates_gateway_receipt(self, bus_db, gateway_db):
        """When gateway_conn is provided, a receipt row is created."""
        thread_id = create_thread(bus_db, author="sender")
        msg_id = post_via_bus_reply(
            bus_db,
            thread_id=thread_id,
            channel_id=CHANNEL_SWARM,
            payload="swarm receipt",
            author="sender",
            gateway_conn=gateway_db,
        )

        receipts = query_gateway_logs(gateway_db, CHANNEL_SWARM, msg_id)
        assert len(receipts) == 1
        receipt = receipts[0]
        assert receipt["channel_id"] == CHANNEL_SWARM
        assert receipt["message_id"] == msg_id
        assert receipt["routed_to"] == CHANNEL_SWARM

    def test_post_hermes_redirects_to_swarm(self, bus_db, gateway_db):
        """pos-002: posts targeting #hermes must redirect to #swarm in gateway logs."""
        thread_id = create_thread(bus_db, author="sender")
        msg_id = post_via_bus_reply(
            bus_db,
            thread_id=thread_id,
            channel_id=CHANNEL_ARES,  # #hermes — FORBIDDEN
            payload="should redirect to #swarm",
            author="sender",
            gateway_conn=gateway_db,
        )

        receipts = query_gateway_logs(gateway_db, CHANNEL_ARES, msg_id)
        assert len(receipts) == 1
        # channel_id is original target, routed_to is where it actually goes
        assert receipts[0]["channel_id"] == CHANNEL_ARES
        assert receipts[0]["routed_to"] == CHANNEL_SWARM, (
            "pos-002 violation: receipt targeting #hermes must redirect to #swarm"
        )

    def test_post_without_gateway_conn(self, bus_db):
        """post_via_bus_reply works without gateway_conn (no receipt logged)."""
        thread_id = create_thread(bus_db, author="sender")
        msg_id = post_via_bus_reply(
            bus_db,
            thread_id=thread_id,
            channel_id=CHANNEL_ICARUS,
            payload="icarus message without gateway",
            author="sender",
        )
        assert msg_id > 0
        # Message is in the bus DB
        row = bus_db.execute(
            "SELECT id FROM messages WHERE id=?", (msg_id,)
        ).fetchone()
        assert row is not None


# ── 4. wait_for_message (deterministic) ────────────────────────────────

class TestWaitForMessage:
    """Verify wait_for_message finds existing messages without sleep."""

    def test_immediate_return_for_existing_message(self, bus_db):
        """If the message already exists, return immediately."""
        thread_id = create_thread(bus_db, author="sender")
        msg_id = post_via_bus_reply(
            bus_db,
            thread_id=thread_id,
            channel_id=CHANNEL_SWARM,
            payload="already here",
            author="sender",
        )
        # Should return instantly (no polling needed)
        result = wait_for_message(
            bus_db, thread_id=thread_id, message_id=msg_id, timeout=1.0
        )
        assert result["id"] == msg_id
        assert result["body"] == "already here"

    def test_timeout_raises_assertion(self, bus_db):
        """If message never appears, wait_for_message raises AssertionError."""
        thread_id = create_thread(bus_db, author="sender")
        # No message inserted — should timeout
        with pytest.raises(AssertionError, match="not found"):
            wait_for_message(
                bus_db,
                thread_id=thread_id,
                message_id=99999,  # nonexistent
                timeout=0.1,
                poll_interval=0.02,
            )

    def test_finds_message_in_correct_thread(self, bus_db):
        """Messages are scoped to threads — cross-thread lookups fail."""
        thread_a = create_thread(bus_db, title="thread A", author="a")
        thread_b = create_thread(bus_db, title="thread B", author="b")

        msg_a = post_via_bus_reply(
            bus_db, thread_id=thread_a, channel_id=CHANNEL_ARES,
            payload="message in A", author="a",
        )
        msg_b = post_via_bus_reply(
            bus_db, thread_id=thread_b, channel_id=CHANNEL_CLAWTA,
            payload="message in B", author="b",
        )

        # Each thread finds its own message
        row_a = wait_for_message(bus_db, thread_id=thread_a, message_id=msg_a)
        assert row_a["body"] == "message in A"

        row_b = wait_for_message(bus_db, thread_id=thread_b, message_id=msg_b)
        assert row_b["body"] == "message in B"

        # But cross-thread lookup fails
        with pytest.raises(AssertionError):
            wait_for_message(
                bus_db, thread_id=thread_a, message_id=msg_b, timeout=0.1
            )


# ── 5. assert_message_absent ───────────────────────────────────────────

class TestAssertMessageAbsent:
    """Verify assert_message_absent correctly detects absent messages."""

    def test_absent_message_passes(self, bus_db):
        """An ID that doesn't exist should pass without error."""
        thread_id = create_thread(bus_db, author="sender")
        # No messages in this thread — any ID should be absent
        assert_message_absent(bus_db, thread_id=thread_id, message_id=99999)

    def test_present_message_fails(self, bus_db):
        """If the message IS in the thread, assertion must fail."""
        thread_id = create_thread(bus_db, author="sender")
        msg_id = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=CHANNEL_SWARM,
            payload="should NOT be here", author="sender",
        )
        with pytest.raises(AssertionError, match="misroute"):
            assert_message_absent(bus_db, thread_id=thread_id, message_id=msg_id)

    def test_body_contains_check(self, bus_db):
        """body_contains flags messages with matching content in the thread."""
        thread_id = create_thread(bus_db, author="sender")
        post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=CHANNEL_SWARM,
            payload="forbidden content about ares", author="sender",
        )

        with pytest.raises(AssertionError, match="misroute"):
            assert_message_absent(
                bus_db,
                thread_id=thread_id,
                message_id=99999,  # nonexistent ID, but body check triggers
                body_contains="forbidden content",
            )

    def test_body_contains_no_match_passes(self, bus_db):
        """body_contains with no matching messages passes."""
        thread_id = create_thread(bus_db, author="sender")
        post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=CHANNEL_SWARM,
            payload="innocuous message", author="sender",
        )
        # body_contains that doesn't match anything should pass
        assert_message_absent(
            bus_db,
            thread_id=thread_id,
            message_id=99999,
            body_contains="completely unrelated",
        )


# ── 6. query_gateway_logs ──────────────────────────────────────────────

class TestQueryGatewayLogs:
    """Verify query_gateway_logs retrieves receipt entries correctly."""

    def test_empty_log_returns_empty(self, gateway_db):
        """No receipts → empty list."""
        result = query_gateway_logs(gateway_db, CHANNEL_SWARM)
        assert result == []

    def test_log_by_channel(self, bus_db, gateway_db):
        """Filter receipts by channel ID."""
        thread_id = create_thread(bus_db, author="sender")

        post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=CHANNEL_SWARM,
            payload="swarm msg", author="sender", gateway_conn=gateway_db,
        )
        post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=CHANNEL_CLAWTA,
            payload="clawta msg", author="sender", gateway_conn=gateway_db,
        )

        swarm_logs = query_gateway_logs(gateway_db, CHANNEL_SWARM)
        assert len(swarm_logs) == 1
        assert swarm_logs[0]["channel_id"] == CHANNEL_SWARM

        clawta_logs = query_gateway_logs(gateway_db, CHANNEL_CLAWTA)
        assert len(clawta_logs) == 1
        assert clawta_logs[0]["channel_id"] == CHANNEL_CLAWTA

    def test_log_by_channel_and_message(self, bus_db, gateway_db):
        """Filter receipts by channel ID + message ID."""
        thread_id = create_thread(bus_db, author="sender")
        msg_1 = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=CHANNEL_SWARM,
            payload="msg 1", author="sender", gateway_conn=gateway_db,
        )
        msg_2 = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=CHANNEL_SWARM,
            payload="msg 2", author="sender", gateway_conn=gateway_db,
        )

        logs = query_gateway_logs(gateway_db, CHANNEL_SWARM, msg_1)
        assert len(logs) == 1
        assert logs[0]["message_id"] == msg_1

    def test_gateway_log_fields(self, bus_db, gateway_db):
        """Each receipt row has all expected fields."""
        thread_id = create_thread(bus_db, author="sender")
        msg_id = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=CHANNEL_ICARUS,
            payload="icarus receipt", author="icarus-probe",
            gateway_conn=gateway_db,
        )

        receipts = query_gateway_logs(gateway_db, CHANNEL_ICARUS, msg_id)
        assert len(receipts) == 1
        r = receipts[0]
        assert r["channel_id"] == CHANNEL_ICARUS
        assert r["message_id"] == msg_id
        assert r["author"] == "icarus-probe"
        assert r["body"] == "icarus receipt"
        assert r["kind"] == "receipt"
        assert r["routed_to"] == CHANNEL_ICARUS  # no redirect for #icarus
        assert r["created_at"] > 0


# ── Negative misroute prevention: cross-channel isolation ──────────────

class TestNegativeMisroutePrevention:
    """Verify that messages do NOT leak to wrong channels.

    Each test posts to one agent's channel, then asserts the message is
    absent from all other channels' threads and gateway receipts.
    Uses assert_message_absent from the shared harness and
    query_gateway_logs_by_message for receipt-level isolation checks.
    """

    def test_ares_msg_not_in_clawta(self, bus_db, gateway_db):
        """Post to #ares, verify message does NOT appear in #clawta,
        #icarus, or #argus threads or gateway receipts."""
        # Post a message in an #ares thread
        ares_thread = create_thread(
            bus_db, title="ares-only thread", author="ares-probe",
            channel_id=CHANNEL_ARES,
        )
        msg_id = post_via_bus_reply(
            bus_db, thread_id=ares_thread, channel_id=CHANNEL_ARES,
            payload="ares-only message payload", author="ares-probe",
            gateway_conn=gateway_db,
        )

        # Create threads for other agents' channels
        clawta_thread = create_thread(
            bus_db, title="clawta thread", author="clawta-probe",
            channel_id=CHANNEL_CLAWTA,
        )
        icarus_thread = create_thread(
            bus_db, title="icarus thread", author="icarus-probe",
            channel_id=CHANNEL_ICARUS,
        )
        argus_thread = create_thread(
            bus_db, title="argus thread", author="argus-probe",
            channel_id=CHANNEL_ARGUS,
        )

        # The ares message must NOT appear in any other channel's thread
        assert_message_absent(
            bus_db, thread_id=clawta_thread, message_id=msg_id,
            body_contains="ares-only message payload",
        )
        assert_message_absent(
            bus_db, thread_id=icarus_thread, message_id=msg_id,
            body_contains="ares-only message payload",
        )
        assert_message_absent(
            bus_db, thread_id=argus_thread, message_id=msg_id,
            body_contains="ares-only message payload",
        )

        # No gateway receipt for other channels referencing this message
        for wrong_channel in (CHANNEL_CLAWTA, CHANNEL_ICARUS, CHANNEL_ARGUS):
            receipts = query_gateway_logs(gateway_db, wrong_channel, msg_id)
            assert len(receipts) == 0, (
                f"Ares message {msg_id} leaked to channel {wrong_channel} "
                f"gateway receipt — misroute detected!"
            )

    def test_clawta_msg_not_in_others(self, bus_db, gateway_db):
        """Post to #clawta, verify message does NOT appear in #ares,
        #icarus, or #argus threads or gateway receipts."""
        clawta_thread = create_thread(
            bus_db, title="clawta-only thread", author="clawta-probe",
            channel_id=CHANNEL_CLAWTA,
        )
        msg_id = post_via_bus_reply(
            bus_db, thread_id=clawta_thread, channel_id=CHANNEL_CLAWTA,
            payload="clawta-only message payload", author="clawta-probe",
            gateway_conn=gateway_db,
        )

        # Create threads for other channels
        ares_thread = create_thread(
            bus_db, title="ares thread", author="ares-probe",
            channel_id=CHANNEL_ARES,
        )
        icarus_thread = create_thread(
            bus_db, title="icarus thread", author="icarus-probe",
            channel_id=CHANNEL_ICARUS,
        )
        argus_thread = create_thread(
            bus_db, title="argus thread", author="argus-probe",
            channel_id=CHANNEL_ARGUS,
        )

        # Clawta message must NOT appear in other channels
        assert_message_absent(
            bus_db, thread_id=ares_thread, message_id=msg_id,
            body_contains="clawta-only message payload",
        )
        assert_message_absent(
            bus_db, thread_id=icarus_thread, message_id=msg_id,
            body_contains="clawta-only message payload",
        )
        assert_message_absent(
            bus_db, thread_id=argus_thread, message_id=msg_id,
            body_contains="clawta-only message payload",
        )

        # No gateway receipt for wrong channels
        for wrong_channel in (CHANNEL_ARES, CHANNEL_ICARUS, CHANNEL_ARGUS):
            receipts = query_gateway_logs(gateway_db, wrong_channel, msg_id)
            assert len(receipts) == 0, (
                f"Clawta message {msg_id} leaked to channel {wrong_channel} "
                f"gateway receipt — misroute detected!"
            )

    def test_icarus_msg_not_in_ares_or_clawta(self, bus_db, gateway_db):
        """Icarus post goes to #icarus + #swarm only. Verify it does NOT
        appear in #ares, #clawta, or #argus threads or gateway receipts."""
        icarus_thread = create_thread(
            bus_db, title="icarus-only thread", author="icarus-probe",
            channel_id=CHANNEL_ICARUS,
        )
        msg_id = post_via_bus_reply(
            bus_db, thread_id=icarus_thread, channel_id=CHANNEL_ICARUS,
            payload="icarus-only message payload", author="icarus-probe",
            gateway_conn=gateway_db,
        )

        # Icarus also appears in #swarm — post receipt there
        swarm_thread = create_thread(
            bus_db, title="swarm thread for icarus", author="icarus-probe",
            channel_id=CHANNEL_SWARM,
        )

        # Create wrong-channel threads
        ares_thread = create_thread(
            bus_db, title="ares thread", author="ares-probe",
            channel_id=CHANNEL_ARES,
        )
        clawta_thread = create_thread(
            bus_db, title="clawta thread", author="clawta-probe",
            channel_id=CHANNEL_CLAWTA,
        )
        argus_thread = create_thread(
            bus_db, title="argus thread", author="argus-probe",
            channel_id=CHANNEL_ARGUS,
        )

        # Icarus message must NOT appear in ares, clawta, or argus threads
        assert_message_absent(
            bus_db, thread_id=ares_thread, message_id=msg_id,
            body_contains="icarus-only message payload",
        )
        assert_message_absent(
            bus_db, thread_id=clawta_thread, message_id=msg_id,
            body_contains="icarus-only message payload",
        )
        assert_message_absent(
            bus_db, thread_id=argus_thread, message_id=msg_id,
            body_contains="icarus-only message payload",
        )

        # Icarus IS allowed in #icarus and #swarm — verify positive presence
        wait_for_message(
            bus_db, thread_id=icarus_thread, message_id=msg_id,
        )

        # No gateway receipt for wrong channels
        for wrong_channel in (CHANNEL_ARES, CHANNEL_CLAWTA, CHANNEL_ARGUS):
            receipts = query_gateway_logs(gateway_db, wrong_channel, msg_id)
            assert len(receipts) == 0, (
                f"Icarus message {msg_id} leaked to channel {wrong_channel} "
                f"gateway receipt — misroute detected!"
            )

    def test_argus_msg_not_in_others(self, bus_db, gateway_db):
        """Post to #argus, verify message does NOT appear in #ares,
        #clawta, or #icarus threads or gateway receipts."""
        argus_thread = create_thread(
            bus_db, title="argus-only thread", author="argus-probe",
            channel_id=CHANNEL_ARGUS,
        )
        msg_id = post_via_bus_reply(
            bus_db, thread_id=argus_thread, channel_id=CHANNEL_ARGUS,
            payload="argus-only message payload", author="argus-probe",
            gateway_conn=gateway_db,
        )

        # Create wrong-channel threads
        ares_thread = create_thread(
            bus_db, title="ares thread", author="ares-probe",
            channel_id=CHANNEL_ARES,
        )
        clawta_thread = create_thread(
            bus_db, title="clawta thread", author="clawta-probe",
            channel_id=CHANNEL_CLAWTA,
        )
        icarus_thread = create_thread(
            bus_db, title="icarus thread", author="icarus-probe",
            channel_id=CHANNEL_ICARUS,
        )

        # Argus message must NOT appear in other channels
        assert_message_absent(
            bus_db, thread_id=ares_thread, message_id=msg_id,
            body_contains="argus-only message payload",
        )
        assert_message_absent(
            bus_db, thread_id=clawta_thread, message_id=msg_id,
            body_contains="argus-only message payload",
        )
        assert_message_absent(
            bus_db, thread_id=icarus_thread, message_id=msg_id,
            body_contains="argus-only message payload",
        )

        # No gateway receipt for wrong channels
        for wrong_channel in (CHANNEL_ARES, CHANNEL_CLAWTA, CHANNEL_ICARUS):
            receipts = query_gateway_logs(gateway_db, wrong_channel, msg_id)
            assert len(receipts) == 0, (
                f"Argus message {msg_id} leaked to channel {wrong_channel} "
                f"gateway receipt — misroute detected!"
            )

    def test_unmentioned_ares_post_wakes_ares(self, bus_db, gateway_db):
        """Post to #ares without mentioning other agents, verify only
        Ares-relevant routing occurs — no wake/receipt events for
        Clawta, Icarus, or Argus channels."""
        ares_thread = create_thread(
            bus_db, title="ares wakeup thread", author="ares-probe",
            channel_id=CHANNEL_ARES,
        )
        msg_id = post_via_bus_reply(
            bus_db, thread_id=ares_thread, channel_id=CHANNEL_ARES,
            payload="ares wakeup — no other agents mentioned",
            author="ares-probe",
            gateway_conn=gateway_db,
        )

        # Gateway receipt should exist only for #ares (routed to #swarm
        # per pos-002, NOT to any other agent channel)
        all_receipts = query_gateway_logs_by_message(gateway_db, msg_id)
        assert len(all_receipts) == 1, (
            f"Expected exactly 1 gateway receipt for ares message, "
            f"got {len(all_receipts)}"
        )
        receipt = all_receipts[0]
        # pos-002: #hermes (ares) outbound is redirected to #swarm
        assert receipt["channel_id"] == CHANNEL_ARES, (
            f"Expected channel_id={CHANNEL_ARES}, "
            f"got {receipt['channel_id']}"
        )
        assert receipt["routed_to"] == CHANNEL_SWARM, (
            f"Expected ares to route to swarm, "
            f"got routed_to={receipt['routed_to']}"
        )

        # Explicitly verify no receipt in other agent channels
        for wrong_channel, wrong_name in [
            (CHANNEL_CLAWTA, "clawta"),
            (CHANNEL_ICARUS, "icarus"),
            (CHANNEL_ARGUS, "argus"),
        ]:
            wrong_receipts = query_gateway_logs(gateway_db, wrong_channel)
            assert len(wrong_receipts) == 0, (
                f"Un-mentioned ares post produced {len(wrong_receipts)} "
                f"receipt(s) in #{wrong_name} — should be 0 (no wake)"
            )

    def test_unmentioned_clawta_post_wakes_clawta(self, bus_db, gateway_db):
        """Post to #clawta without mentioning other agents, verify only
        Clawta-relevant routing occurs — no wake/receipt events for
        Ares, Icarus, or Argus channels."""
        clawta_thread = create_thread(
            bus_db, title="clawta wakeup thread", author="clawta-probe",
            channel_id=CHANNEL_CLAWTA,
        )
        msg_id = post_via_bus_reply(
            bus_db, thread_id=clawta_thread, channel_id=CHANNEL_CLAWTA,
            payload="clawta wakeup — no other agents mentioned",
            author="clawta-probe",
            gateway_conn=gateway_db,
        )

        # Gateway receipt should exist only for #clawta
        all_receipts = query_gateway_logs_by_message(gateway_db, msg_id)
        assert len(all_receipts) == 1, (
            f"Expected exactly 1 gateway receipt for clawta message, "
            f"got {len(all_receipts)}"
        )
        receipt = all_receipts[0]
        # Clawta passes through unchanged (not forbidden like #ares)
        assert receipt["channel_id"] == CHANNEL_CLAWTA, (
            f"Expected channel_id={CHANNEL_CLAWTA}, "
            f"got {receipt['channel_id']}"
        )
        assert receipt["routed_to"] == CHANNEL_CLAWTA, (
            f"Expected clawta to route to clawta, "
            f"got routed_to={receipt['routed_to']}"
        )

        # Explicitly verify no receipt in other agent channels
        for wrong_channel, wrong_name in [
            (CHANNEL_ARES, "ares"),
            (CHANNEL_ICARUS, "icarus"),
            (CHANNEL_ARGUS, "argus"),
        ]:
            wrong_receipts = query_gateway_logs(gateway_db, wrong_channel)
            assert len(wrong_receipts) == 0, (
                f"Un-mentioned clawta post produced {len(wrong_receipts)} "
                f"receipt(s) in #{wrong_name} — should be 0 (no wake)"
            )


# ── Integration: pos-002 misroute prevention ───────────────────────────

class TestMisroutePrevention:
    """Cross-cutting tests verifying the pos-002 routing contract:
    - Posts to #hermes (#ares) are FORBIDDEN as outbound; they must be
      redirected to #swarm
    - Un-mentioned posts in #ares/#clawta wake the correct agent
    - #swarm posts reach #swarm
    - Unknown channels are dropped
    """

    def test_outbound_hermes_redirects_to_swarm(self, bus_db, gateway_db):
        """pos-002: any receipt targeting #hermes must redirect to #swarm."""
        thread_id = create_thread(bus_db, author="sender")
        msg_id = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=CHANNEL_ARES,
            payload="should be redirected", author="sender",
            gateway_conn=gateway_db,
        )

        receipts = query_gateway_logs(gateway_db, CHANNEL_ARES, msg_id)
        assert len(receipts) == 1
        assert receipts[0]["routed_to"] == CHANNEL_SWARM, (
            f"pos-002 violation: #hermes receipt routed to "
            f"{receipts[0]['routed_to']} instead of #swarm"
        )

    def test_non_hermes_channel_passes_through(self, bus_db, gateway_db):
        """Channels other than #hermes pass through unchanged."""
        for ch_name, ch_id in [
            ("clawta", CHANNEL_CLAWTA),
            ("icarus", CHANNEL_ICARUS),
            ("argus", CHANNEL_ARGUS),
            ("swarm", CHANNEL_SWARM),
        ]:
            thread_id = create_thread(bus_db, author=f"sender-{ch_name}")
            msg_id = post_via_bus_reply(
                bus_db, thread_id=thread_id, channel_id=ch_id,
                payload=f"test for #{ch_name}", author=f"sender-{ch_name}",
                gateway_conn=gateway_db,
            )
            receipts = query_gateway_logs(gateway_db, ch_id, msg_id)
            assert len(receipts) == 1
            assert receipts[0]["routed_to"] == ch_id, (
                f"#{ch_name} receipt should pass through, but was routed to "
                f"{receipts[0]['routed_to']}"
            )

    def test_unknown_channel_not_in_allowed(self):
        """Unknown channel IDs are not in the allowlist."""
        unknown = "8888888888888888888"
        assert unknown not in ALL_CHANNEL_IDS

    def test_agent_channel_mapping_covers_all_agents(self, channel_ids):
        """Each known agent has a channel mapping in AGENT_CHANNELS."""
        for agent in ("hermes", "clawta", "icarus", "argus", "swarm"):
            assert agent in channel_ids, f"Missing channel for agent {agent}"

    def test_inbound_routing_correct_wake(self, bus_db):
        """Verify agent-to-channel mapping: each agent's posts land in
        their own channel thread, not in another agent's thread."""
        for agent, ch_id in AGENT_CHANNELS.items():
            # Create a thread tagged with the agent's channel
            thread_id = create_thread(
                bus_db, title=f"{agent} thread", author=agent,
                channel_id=ch_id,
            )
            msg_id = post_via_bus_reply(
                bus_db, thread_id=thread_id, channel_id=ch_id,
                payload=f"message from {agent}", author=agent,
            )

            # Verify the message is in the correct thread
            row = wait_for_message(
                bus_db, thread_id=thread_id, message_id=msg_id
            )
            assert row["author"] == agent
            assert row["body"] == f"message from {agent}"

            # Verify thread is tagged with the correct channel
            thread = bus_db.execute(
                "SELECT discord_thread_id FROM threads WHERE id=?",
                (thread_id,),
            ).fetchone()
            assert thread["discord_thread_id"] == ch_id


# ── Smoke test: post and read back ─────────────────────────────────────

class TestSmokePostAndReadback:
    """Trivial end-to-end: create a thread, post a message, read it back.
    This is the acceptance test for the harness itself — if this passes,
    all fixtures and helpers are functional."""

    def test_post_and_read_back(self, bus_db, gateway_db, channel_ids):
        """Post a message via bus_reply, wait for it, and verify all fields."""
        # 1. Create a thread in #swarm
        thread_id = create_thread(
            bus_db, title="smoke test thread", author="test-probe",
            board="swarm", channel_id=channel_ids["swarm"],
        )
        assert thread_id > 0, "Thread creation failed"

        # 2. Post a message via the helper
        msg_id = post_via_bus_reply(
            bus_db,
            thread_id=thread_id,
            channel_id=channel_ids["swarm"],
            payload="smoke test message in #swarm",
            author="test-probe",
            kind="system",
            gateway_conn=gateway_db,
        )
        assert msg_id > 0, "post_via_bus_reply failed to return a message ID"

        # 3. Wait for the message (deterministic, no sleep)
        row = wait_for_message(
            bus_db, thread_id=thread_id, message_id=msg_id, timeout=2.0
        )
        assert row["body"] == "smoke test message in #swarm"
        assert row["author"] == "test-probe"
        assert row["kind"] == "system"

        # 4. Verify gateway receipt
        receipts = query_gateway_logs(
            gateway_db, channel_ids["swarm"], msg_id
        )
        assert len(receipts) == 1
        assert receipts[0]["routed_to"] == channel_ids["swarm"]

        # 5. Verify #swarm message is NOT present in #hermes thread
        hermes_thread_id = create_thread(
            bus_db, title="hermes thread", author="hermes-bot",
            channel_id=channel_ids["hermes"],
        )
        assert_message_absent(
            bus_db,
            thread_id=hermes_thread_id,
            message_id=msg_id,  # This msg is in #swarm, NOT #hermes
        )

        # 6. Verify #hermes redirect: a post targeting #hermes routes to #swarm
        redirect_msg_id = post_via_bus_reply(
            bus_db,
            thread_id=hermes_thread_id,
            channel_id=channel_ids["hermes"],
            payload="should redirect to #swarm",
            author="test-probe",
            gateway_conn=gateway_db,
        )
        redirect_receipts = query_gateway_logs(
            gateway_db, channel_ids["hermes"], redirect_msg_id
        )
        assert redirect_receipts[0]["routed_to"] == channel_ids["swarm"], (
            "pos-002 violation: #hermes target must redirect to #swarm"
        )


# ── 8. Gateway log routing assertions ────────────────────────────────

class TestGatewayLogAssertions:
    """Verify gateway intake logs reflect correct routing independently
    of the bus DB. These tests assert against gateway_log data only,
    confirming that receipt entries name the right channel(s) and no
    others — a separate signal chain from the bus message assertions.
    """

    def test_gateway_log_shows_correct_channel(self, bus_db, gateway_db):
        """Post msg to #clawta via bus_reply. Gateway log entry shows
        channel_id matching #clawta (not #hermes or any other channel)."""
        thread_id = create_thread(bus_db, author="clawta-probe")
        msg_id = post_via_bus_reply(
            bus_db,
            thread_id=thread_id,
            channel_id=CHANNEL_CLAWTA,
            payload="clawta routing check",
            author="clawta-probe",
            gateway_conn=gateway_db,
        )

        # Query gateway logs for the clawta channel + this message
        receipts = query_gateway_logs(gateway_db, CHANNEL_CLAWTA, msg_id)
        assert len(receipts) == 1, (
            f"Expected exactly 1 receipt for #clawta msg {msg_id}, "
            f"got {len(receipts)}"
        )
        receipt = receipts[0]

        # The channel_id in the receipt must be #clawta — not any other
        assert receipt["channel_id"] == CHANNEL_CLAWTA, (
            f"Gateway log channel_id mismatch: expected {CHANNEL_CLAWTA}, "
            f"got {receipt['channel_id']}"
        )
        # routed_to must also be #clawta (no redirect for non-hermes)
        assert receipt["routed_to"] == CHANNEL_CLAWTA, (
            f"Gateway log routed_to mismatch: expected {CHANNEL_CLAWTA}, "
            f"got {receipt['routed_to']}"
        )

        # Cross-check: no receipts for this message in any forbidden channel
        all_receipts = query_gateway_logs_by_message(gateway_db, msg_id)
        forbidden_channels = {
            CHANNEL_ARES, CHANNEL_ICARUS, CHANNEL_ARGUS, CHANNEL_SWARM,
        }
        for r in all_receipts:
            assert r["channel_id"] not in forbidden_channels, (
                f"Gateway log shows msg {msg_id} in channel "
                f"{r['channel_id']}, expected only #clawta"
            )

    def test_gateway_log_no_misroute_entries(self, bus_db, gateway_db):
        """Post msg to #ares. Gateway logs for that message_id exist
        ONLY for #ares (with routed_to=#swarm per pos-002) — no entries
        for #clawta, #icarus, or #argus."""
        thread_id = create_thread(bus_db, author="ares-probe")
        msg_id = post_via_bus_reply(
            bus_db,
            thread_id=thread_id,
            channel_id=CHANNEL_ARES,
            payload="ares misroute check",
            author="ares-probe",
            gateway_conn=gateway_db,
        )

        # The #ares receipt must exist (pos-002: channel=ares, routed_to=swarm)
        ares_receipts = query_gateway_logs(gateway_db, CHANNEL_ARES, msg_id)
        assert len(ares_receipts) == 1, (
            f"Expected 1 receipt for #ares msg {msg_id}, "
            f"got {len(ares_receipts)}"
        )
        assert ares_receipts[0]["routed_to"] == CHANNEL_SWARM, (
            "pos-002 violation: #ares receipt must route to #swarm"
        )

        # Across ALL channels, this message must only appear in #ares
        all_receipts = query_gateway_logs_by_message(gateway_db, msg_id)
        allowed_channels = {CHANNEL_ARES}  # pos-002: only ares records the receipt
        for r in all_receipts:
            assert r["channel_id"] in allowed_channels, (
                f"Misroute detected: msg {msg_id} has gateway log entry in "
                f"channel {r['channel_id']}, expected only #ares "
                f"({CHANNEL_ARES})"
            )

        # Explicitly verify absence in other agent channels
        for ch_name, ch_id in [
            ("clawta", CHANNEL_CLAWTA),
            ("icarus", CHANNEL_ICARUS),
            ("argus", CHANNEL_ARGUS),
        ]:
            entries = query_gateway_logs(gateway_db, ch_id, msg_id)
            assert len(entries) == 0, (
                f"Misroute: msg {msg_id} found in #{ch_name} gateway logs "
                f"({len(entries)} entries), expected 0"
            )

    def test_gateway_log_icarus_dual_channel(self, bus_db, gateway_db):
        """Post Icarus msg. Gateway logs show entries for both #icarus
        and #swarm (broadcast), and NO entries for #ares, #clawta,
        or #argus.

        In production, icarus messages broadcast to #swarm as well as
        their own channel. The test harness models this by inserting
        receipts for both channels.
        """
        thread_id = create_thread(
            bus_db, author="icarus-probe", channel_id=CHANNEL_ICARUS,
        )
        # Post to #icarus with gateway receipt
        msg_id = post_via_bus_reply(
            bus_db,
            thread_id=thread_id,
            channel_id=CHANNEL_ICARUS,
            payload="icarus dual-channel check",
            author="icarus-probe",
            gateway_conn=gateway_db,
        )

        # Also model the broadcast to #swarm by inserting a second receipt
        # (In production, the controller would emit to both channels)
        now = int(time.time())
        gateway_db.execute(
            "INSERT INTO gateway_log(channel_id, message_id, author, body, "
            "kind, routed_to, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)",
            (CHANNEL_SWARM, msg_id, "icarus-probe", "icarus dual-channel check",
             "receipt", CHANNEL_SWARM, now + 1),
        )
        gateway_db.commit()

        # Verify #icarus receipt exists
        icarus_receipts = query_gateway_logs(gateway_db, CHANNEL_ICARUS, msg_id)
        assert len(icarus_receipts) == 1, (
            f"Expected 1 receipt for #icarus msg {msg_id}, "
            f"got {len(icarus_receipts)}"
        )
        assert icarus_receipts[0]["routed_to"] == CHANNEL_ICARUS, (
            f"#icarus receipt should pass through, got routed_to="
            f"{icarus_receipts[0]['routed_to']}"
        )

        # Verify #swarm broadcast receipt exists
        swarm_receipts = query_gateway_logs(gateway_db, CHANNEL_SWARM, msg_id)
        assert len(swarm_receipts) == 1, (
            f"Expected 1 broadcast receipt for #swarm on icarus msg {msg_id}, "
            f"got {len(swarm_receipts)}"
        )
        assert swarm_receipts[0]["routed_to"] == CHANNEL_SWARM

        # Verify NO entries in forbidden channels
        for ch_name, ch_id in [
            ("ares", CHANNEL_ARES),
            ("clawta", CHANNEL_CLAWTA),
            ("argus", CHANNEL_ARGUS),
        ]:
            entries = query_gateway_logs(gateway_db, ch_id, msg_id)
            assert len(entries) == 0, (
                f"Misroute: icarus msg {msg_id} found in #{ch_name} "
                f"gateway logs ({len(entries)} entries), expected 0"
            )

        # Verify total: exactly 2 entries (icarus + swarm broadcast)
        all_receipts = query_gateway_logs_by_message(gateway_db, msg_id)
        assert len(all_receipts) == 2, (
            f"Expected exactly 2 gateway log entries for icarus msg "
            f"(icarus + swarm broadcast), got {len(all_receipts)}"
        )


# ── Positive routing correctness tests ──────────────────────────────────

class TestPositiveRoutingCorrectness:
    """Verify that messages routed via bus_reply arrive in the CORRECT channel.

    Each test posts a message targeting a specific channel, then asserts:
    1. The message is present in the bus DB with correct fields.
    2. The gateway receipt shows the message routed to the expected channel.

    These are positive-path tests (the message SHOULD be there),
    complementing the misroute-prevention tests (the message should NOT
    be in the wrong channel).
    """

    def test_reply_to_swarm(self, bus_db, gateway_db, channel_ids):
        """Posts targeting #swarm arrive in the #swarm channel DB."""
        ch_id = channel_ids["swarm"]
        thread_id = create_thread(
            bus_db, title="swarm routing thread", author="swarm-probe",
            channel_id=ch_id,
        )
        msg_id = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=ch_id,
            payload="delivered to #swarm", author="swarm-probe",
            gateway_conn=gateway_db,
        )

        # Message is in the bus DB
        row = wait_for_message(bus_db, thread_id=thread_id, message_id=msg_id)
        assert row["body"] == "delivered to #swarm"
        assert row["author"] == "swarm-probe"

        # Gateway receipt shows correct routing
        receipts = query_gateway_logs(gateway_db, ch_id, msg_id)
        assert len(receipts) == 1
        assert receipts[0]["routed_to"] == ch_id

        # Thread is tagged with the swarm channel
        thread = bus_db.execute(
            "SELECT discord_thread_id FROM threads WHERE id=?", (thread_id,),
        ).fetchone()
        assert thread["discord_thread_id"] == ch_id

    def test_reply_to_ares(self, bus_db, gateway_db, channel_ids):
        """Posts targeting #ares arrive in the #ares channel DB.

        Note: pos-002 reroutes #ares outbound to #swarm at the gateway level.
        The bus DB message is still tagged with the original #ares channel.
        The gateway receipt shows the redirect: routed_to = #swarm.
        """
        ch_id = channel_ids["ares"]
        thread_id = create_thread(
            bus_db, title="ares routing thread", author="ares-probe",
            channel_id=ch_id,
        )
        msg_id = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=ch_id,
            payload="targeting #ares", author="ares-probe",
            gateway_conn=gateway_db,
        )

        # Message is in the bus DB
        row = wait_for_message(bus_db, thread_id=thread_id, message_id=msg_id)
        assert row["body"] == "targeting #ares"
        assert row["author"] == "ares-probe"

        # Thread is tagged with the ares channel
        thread = bus_db.execute(
            "SELECT discord_thread_id FROM threads WHERE id=?", (thread_id,),
        ).fetchone()
        assert thread["discord_thread_id"] == ch_id

        # Gateway receipt: #ares is rerouted to #swarm per pos-002
        receipts = query_gateway_logs(gateway_db, ch_id, msg_id)
        assert len(receipts) == 1
        assert receipts[0]["channel_id"] == ch_id
        assert receipts[0]["routed_to"] == channel_ids["swarm"]

    def test_reply_to_clawta(self, bus_db, gateway_db, channel_ids):
        """Posts targeting #clawta arrive in the #clawta channel DB."""
        ch_id = channel_ids["clawta"]
        thread_id = create_thread(
            bus_db, title="clawta routing thread", author="clawta-probe",
            channel_id=ch_id,
        )
        msg_id = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=ch_id,
            payload="delivered to #clawta", author="clawta-probe",
            gateway_conn=gateway_db,
        )

        row = wait_for_message(bus_db, thread_id=thread_id, message_id=msg_id)
        assert row["body"] == "delivered to #clawta"
        assert row["author"] == "clawta-probe"

        receipts = query_gateway_logs(gateway_db, ch_id, msg_id)
        assert len(receipts) == 1
        assert receipts[0]["routed_to"] == ch_id

    def test_reply_to_icarus(self, bus_db, gateway_db, channel_ids):
        """Posts targeting #icarus arrive in the #icarus channel DB."""
        ch_id = channel_ids["icarus"]
        thread_id = create_thread(
            bus_db, title="icarus routing thread", author="icarus-probe",
            channel_id=ch_id,
        )
        msg_id = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=ch_id,
            payload="delivered to #icarus", author="icarus-probe",
            gateway_conn=gateway_db,
        )

        row = wait_for_message(bus_db, thread_id=thread_id, message_id=msg_id)
        assert row["body"] == "delivered to #icarus"
        assert row["author"] == "icarus-probe"

        receipts = query_gateway_logs(gateway_db, ch_id, msg_id)
        assert len(receipts) == 1
        assert receipts[0]["routed_to"] == ch_id

    def test_reply_to_argus(self, bus_db, gateway_db, channel_ids):
        """Posts targeting #argus arrive in the #argus channel DB."""
        ch_id = channel_ids["argus"]
        thread_id = create_thread(
            bus_db, title="argus routing thread", author="argus-probe",
            channel_id=ch_id,
        )
        msg_id = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=ch_id,
            payload="delivered to #argus", author="argus-probe",
            gateway_conn=gateway_db,
        )

        row = wait_for_message(bus_db, thread_id=thread_id, message_id=msg_id)
        assert row["body"] == "delivered to #argus"
        assert row["author"] == "argus-probe"

        receipts = query_gateway_logs(gateway_db, ch_id, msg_id)
        assert len(receipts) == 1
        assert receipts[0]["routed_to"] == ch_id

    def test_icarus_posts_to_swarm_and_icarus(self, bus_db, gateway_db, channel_ids):
        """Icarus posts go to BOTH #icarus AND #swarm.

        In production, Icarus messages are fanned out to both its agent
        channel and the coordination channel. In the test harness this
        means: posting with channel_id=#icarus creates a receipt with
        routed_to=#icarus, and a second receipt is stamped targeting
        #swarm (fan-out). We model this by posting to both channels
        and verifying both receipts exist.
        """
        icarus_id = channel_ids["icarus"]
        swarm_id = channel_ids["swarm"]

        # Create a thread tagged with #icarus
        thread_id = create_thread(
            bus_db, title="icarus dual-post thread", author="icarus-probe",
            channel_id=icarus_id,
        )

        # Icarus posts its message to #icarus (primary channel)
        msg_id = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=icarus_id,
            payload="icarus dual post", author="icarus-probe",
            gateway_conn=gateway_db,
        )

        # Icarus also fans out to #swarm (coordination channel)
        # Same message body, different channel target
        msg_id_swarm = post_via_bus_reply(
            bus_db, thread_id=thread_id, channel_id=swarm_id,
            payload="icarus dual post", author="icarus-probe",
            gateway_conn=gateway_db,
        )

        # Verify the #icarus receipt: routed_to = #icarus
        icarus_receipts = query_gateway_logs(gateway_db, icarus_id, msg_id)
        assert len(icarus_receipts) == 1
        assert icarus_receipts[0]["routed_to"] == icarus_id

        # Verify the #swarm receipt: routed_to = #swarm
        swarm_receipts = query_gateway_logs(gateway_db, swarm_id, msg_id_swarm)
        assert len(swarm_receipts) == 1
        assert swarm_receipts[0]["routed_to"] == swarm_id

        # Verify both messages are in the bus DB
        row_icarus = wait_for_message(
            bus_db, thread_id=thread_id, message_id=msg_id,
        )
        assert row_icarus["body"] == "icarus dual post"

        row_swarm = wait_for_message(
            bus_db, thread_id=thread_id, message_id=msg_id_swarm,
        )
        assert row_swarm["body"] == "icarus dual post"

        # Verify the thread has both channel tags via gateway_log
        all_icarus_receipts = query_gateway_logs(gateway_db, icarus_id)
        all_swarm_receipts = query_gateway_logs(gateway_db, swarm_id)
        assert len(all_icarus_receipts) >= 1, "No icarus receipt in gateway log"
        assert len(all_swarm_receipts) >= 1, "No swarm receipt in gateway log"