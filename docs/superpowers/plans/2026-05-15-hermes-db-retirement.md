# Plan 4 — Retire hermes' kanban DB as the source of truth

> **For agentic workers:** This plan has both code and operational steps. The code parts can be drafted unattended; the operational parts (cron pause + file moves) require the operator. Do not execute the operational steps in a background session.

**Goal:** Make `~/.chitin/kanban/<board>/kanban.db` the single source of truth for kanban state. The hermes-side path `~/.hermes/kanban/boards/<board>/kanban.db` becomes a symlink so existing hermes-agent code and crons still find data, but every write lands in the chitin-owned file.

**Architecture:** No code change to hermes-agent (we don't own that tree). The hermes path becomes a symlink → chitin path. SQLite resolves symlinks at `open(2)`, so both hermes daemons and console-api write to the same byte stream from then on. The `chitin-kernel kanban migrate` CLI from PR #680 still works for one-shot copies (e.g. recovery from backup), but isn't part of the steady-state hot path anymore.

**Risks:**
- Hermes processes with open file handles to the original DB will keep writing to the inode that the OS still holds — those writes are lost on swap.
- Crons that fire mid-swap can race the move.
- SQLite WAL files (`*-wal`, `*-shm`) must follow the main DB or readers get stale state.

**Mitigation:** Pause the relevant crons + briefly stop the hermes daemon before the swap. WAL flush (`PRAGMA wal_checkpoint(TRUNCATE)`) before move guarantees no stale sidecar.

---

## Phase A — Code (unattended, can land before the cutover)

### Task A1: Drop the `migrate-after-write` refresh in console-api

After cutover, console-api's `POST /api/tasks/:id/status` no longer needs to call `chitin-kernel kanban migrate` because `kanban-flow` writes will land directly in the chitin DB (via the hermes path symlink).

**File:** `apps/chitin-console-api/src/server.mjs` — the `postTaskStatus` function

Before:
```js
const migrate = spawnSync('chitin-kernel', ['kanban', 'migrate', CURRENT_BOARD], {
  encoding: 'utf8',
  timeout: 30_000,
});
const refreshed = migrate.status === 0;
if (refreshed) reopen();
```

After:
```js
// Post-cutover: hermes path is a symlink to chitin DB, so writes
// from kanban-flow land here directly. No migrate refresh needed.
reopen();
const refreshed = true;
```

Tests still pass since the validation paths don't exercise migrate.

### Task A2: README — document the new shape

Update `apps/chitin-console/README.md` "Data sources" table — change `Hermes kanban` row to `Chitin kanban (~/.chitin/kanban/<board>/kanban.db; hermes path is a symlink to this).`

---

## Phase B — Operational cutover (operator-attended, ~3 minute window)

Run these as the operator on the chimera-ant box (or whatever box hosts the swarm).

### Step B1: Pre-flight checklist

- [ ] No PRs from the swarm are currently mid-review (check `gh pr list -l agent`)
- [ ] No ticket is in `in_progress` with an active worktree being modified (check `kanban-flow status <id>` on any in-flight ticket)
- [ ] You have a recent backup: `cp ~/.hermes/kanban/boards/chitin/kanban.db /tmp/kanban-chitin-pre-cutover.db`

### Step B2: Pause hermes crons

```bash
hermes cron pause 388e38b20bd5   # board-watchdog
hermes cron pause $(hermes cron list | grep -A1 'clawta-poller' | tail -1 | awk '{print $1}')
hermes cron pause $(hermes cron list | grep -A1 'hermes-clawta-bridge' | tail -1 | awk '{print $1}')
hermes cron pause $(hermes cron list | grep -A1 'autonomous-board-engine' | tail -1 | awk '{print $1}')
hermes cron pause $(hermes cron list | grep -A1 'readybench-poller' | tail -1 | awk '{print $1}')
hermes cron list   # verify all show [paused]
```

### Step B3: Stop the hermes gateway daemon (if running)

```bash
# Find any hermes gateway process holding the DB open
ps -ef | grep -E 'hermes (gateway|agent)' | grep -v grep
# If any: pkill -f 'hermes gateway' OR systemctl --user stop hermes (whichever applies)
```

### Step B4: WAL checkpoint + verify quiescence

```bash
for BOARD in chitin readybench; do
  sqlite3 ~/.hermes/kanban/boards/$BOARD/kanban.db "PRAGMA wal_checkpoint(TRUNCATE);"
done
# Confirm no `-wal`/`-shm` sidecars remain:
ls ~/.hermes/kanban/boards/*/kanban.db-* 2>&1 || echo "clean"
```

### Step B5: Fresh migrate to capture any post-#680 writes

```bash
chitin-kernel kanban migrate chitin
chitin-kernel kanban migrate readybench
```

Expect row counts matching the source after each command.

### Step B6: Swap files for symlinks

```bash
for BOARD in chitin readybench; do
  HERMES_DB=~/.hermes/kanban/boards/$BOARD/kanban.db
  CHITIN_DB=~/.chitin/kanban/$BOARD/kanban.db
  # Move original out of the way (don't delete — recoverable for 7d).
  mv "$HERMES_DB" "$HERMES_DB.pre-cutover-$(date +%Y%m%d-%H%M%S)"
  # Symlink the hermes path to chitin DB.
  ln -s "$CHITIN_DB" "$HERMES_DB"
  ls -la "$HERMES_DB"   # should show "→ /home/<user>/.chitin/kanban/<board>/kanban.db"
done
```

### Step B7: Verify reads work through both paths

```bash
for BOARD in chitin readybench; do
  echo "=== $BOARD via hermes path ==="
  sqlite3 ~/.hermes/kanban/boards/$BOARD/kanban.db "SELECT count(*) FROM tasks"
  echo "=== $BOARD via chitin path ==="
  sqlite3 ~/.chitin/kanban/$BOARD/kanban.db "SELECT count(*) FROM tasks"
done
```

Both numbers must be identical for each board.

### Step B8: Resume crons + restart hermes

```bash
hermes cron resume <each-paused-cron-id>
# Restart the gateway daemon if you stopped it
```

### Step B9: Smoke a real write through both paths

```bash
# Find a test ticket (create one via hermes kanban or use an existing
# in-flight one with operator consent).
TEST_ID=t_xxxx
# Write via kanban-flow (hits hermes path → symlink → chitin DB):
kanban-flow start "$TEST_ID"
# Read back via chitin-direct path:
sqlite3 ~/.chitin/kanban/chitin/kanban.db "SELECT status FROM tasks WHERE id='$TEST_ID'"
# Should be: in_progress
```

### Step B10: Land Phase A code

Merge the console-api `migrate-after-write` removal so writes don't burn the extra ~100ms refresh.

### Rollback

If anything goes wrong:

```bash
for BOARD in chitin readybench; do
  HERMES_DB=~/.hermes/kanban/boards/$BOARD/kanban.db
  rm "$HERMES_DB"    # delete the symlink (NOT the chitin DB)
  mv "$HERMES_DB.pre-cutover-"* "$HERMES_DB"
done
# Resume crons
```

This restores the pre-cutover state. The chitin DB stays intact and can be re-migrated next time.

---

## Phase C — Cleanup (after 1 week of clean operation)

- Remove the `.pre-cutover-*` backups
- Update `apps/chitin-console/README.md` to reflect the new path is canonical
- Optional: remove `HERMES_KANBAN_ROOT` fallback from `server.mjs` since the path is now a symlink anyway

---

## Why not change the code instead

Two alternatives were considered and rejected:

1. **Flip `kanban-flow` to write to chitin path directly.** Rejected because hermes-agent's Python code (`hermes_cli/kanban_db.py`, ~30 call sites) reads/writes the hermes path directly. The CLI flip would fork the data.
2. **Modify hermes-agent.** Rejected because that tree is not part of this repo and the cost of forking it just to retarget a path is wildly disproportionate.

The symlink swap is the only change that lets every existing kanban writer (hermes daemon, kanban-flow, console-api, clawta-poller) land bytes in the chitin-owned file without any of them knowing about it.
