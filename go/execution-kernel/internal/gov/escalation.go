package gov

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Counter tracks per-agent escalation state backed by SQLite.
// Key invariants:
//   - Lockdown is sticky across sessions (survives Close/Open).
//   - Counter keyed on (agent, action_fp); total denials per agent drive
//     the level ladder.
//   - Weighted denials (e.g. self-modification) bump total by >1.
type Counter struct {
	db *sql.DB
}

const denialEventRetention = 7 * 24 * time.Hour

// OpenCounter opens/creates the SQLite DB at dbPath with WAL mode.
//
// Schema notes (spec 096):
//   - A fresh database is created with the full extended `agent_state`
//     schema in one shot (the CREATE TABLE below includes `unlock_ts`
//     and `lock_epoch`).
//   - A pre-existing database created before spec 096 is migrated by
//     migrateAgentStateSchema(), which uses PRAGMA table_info to detect
//     missing columns and ALTER TABLE them in place. Idempotent — safe
//     to re-run on every kernel invocation.
func OpenCounter(dbPath string) (*Counter, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS denials (
			agent TEXT NOT NULL,
			action_fp TEXT NOT NULL,
			count INTEGER NOT NULL DEFAULT 0,
			first_ts TEXT NOT NULL,
			last_ts TEXT NOT NULL,
			PRIMARY KEY (agent, action_fp)
		);
		CREATE TABLE IF NOT EXISTS agent_state (
			agent TEXT PRIMARY KEY,
			total INTEGER NOT NULL DEFAULT 0,
			locked INTEGER NOT NULL DEFAULT 0,
			locked_ts TEXT,
			unlock_ts TEXT,
			lock_epoch INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS denial_events (
			agent TEXT NOT NULL,
			action_type TEXT NOT NULL,
			action_fp TEXT NOT NULL,
			ts_unix INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_denial_events_agent_type_ts
			ON denial_events(agent, action_type, ts_unix);
	`); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err := migrateAgentStateSchema(db); err != nil {
		return nil, fmt.Errorf("migrate agent_state: %w", err)
	}
	return &Counter{db: db}, nil
}

// migrateAgentStateSchema brings a pre-spec-096 agent_state table up to
// the post-spec-096 shape by adding the unlock_ts and lock_epoch columns
// if they're absent. Idempotent — uses PRAGMA table_info to detect what's
// already present.
//
// Order of operations matters: SQLite ALTER TABLE ADD COLUMN is fast
// (metadata-only) but each column is its own statement. We check both
// columns up front and add the missing ones; a SIGKILL between the two
// adds leaves a half-migrated DB that the NEXT invocation of this
// function finishes. No data is rewritten — adds use a backward-
// compatible default (NULL for unlock_ts, 0 for lock_epoch).
func migrateAgentStateSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(agent_state)`)
	if err != nil {
		return fmt.Errorf("pragma table_info: %w", err)
	}
	defer rows.Close()
	present := map[string]bool{}
	for rows.Next() {
		var (
			cid       int
			name      string
			typ       string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("scan table_info row: %w", err)
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table_info: %w", err)
	}

	if !present["unlock_ts"] {
		if _, err := db.Exec(`ALTER TABLE agent_state ADD COLUMN unlock_ts TEXT`); err != nil {
			// Tolerate the concurrent-migration race: two kernel processes
			// can both pass the PRAGMA table_info check and both attempt
			// the ALTER. SQLite serializes writes, so the second one's
			// ALTER returns "duplicate column name" — same as if the
			// migration had already run. Treat as success.
			if !isDuplicateColumnErr(err, "unlock_ts") {
				return fmt.Errorf("add unlock_ts: %w", err)
			}
		}
	}
	if !present["lock_epoch"] {
		if _, err := db.Exec(`ALTER TABLE agent_state ADD COLUMN lock_epoch INTEGER NOT NULL DEFAULT 0`); err != nil {
			if !isDuplicateColumnErr(err, "lock_epoch") {
				return fmt.Errorf("add lock_epoch: %w", err)
			}
		}
	}
	return nil
}

// isDuplicateColumnErr matches the SQLite error returned when ALTER TABLE
// ADD COLUMN names a column that already exists. Used by
// migrateAgentStateSchema to tolerate the concurrent-migration race
// (two processes seeing the same "column missing" state and both
// issuing the ALTER; SQLite serializes them so the second one races
// into "duplicate column name" — semantically equivalent to "migration
// already done", so we treat it as success).
func isDuplicateColumnErr(err error, column string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// modernc.org/sqlite reports this as `duplicate column name: <col>`;
	// some forks also report `column "<col>" already exists`.
	return (strings.Contains(msg, "duplicate column name") && strings.Contains(msg, column)) ||
		(strings.Contains(msg, "already exists") && strings.Contains(msg, column))
}

// Close the underlying DB.
func (c *Counter) Close() error {
	return c.db.Close()
}

// RecordDenial increments counters for (agent, fp) by weight. If total
// denials reach the lockdown threshold (10), marks the agent locked.
// Returns a non-nil error if the underlying SQLite transaction fails so
// callers can surface (or at minimum log) the failure rather than
// silently dropping the escalation count — a silent drop would let an
// agent past the lockdown threshold without ever locking.
func (c *Counter) RecordDenial(agent, fp string, weight int) error {
	return c.RecordActionDenial(agent, "", fp, weight)
}

// RecordActionDenial increments the aggregate escalation counters and records
// one timestamped denial event for windowed cascade detection.
func (c *Counter) RecordActionDenial(agent, actionType, fp string, weight int) error {
	if weight <= 0 {
		weight = 1
	}
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339)

	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO denials (agent, action_fp, count, first_ts, last_ts)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(agent, action_fp) DO UPDATE SET
			count = count + excluded.count,
			last_ts = excluded.last_ts
	`, agent, fp, weight, nowText, nowText); err != nil {
		return fmt.Errorf("upsert denial: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO agent_state (agent, total, locked)
		VALUES (?, ?, 0)
		ON CONFLICT(agent) DO UPDATE SET total = total + excluded.total
	`, agent, weight); err != nil {
		return fmt.Errorf("upsert agent_state: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO denial_events (agent, action_type, action_fp, ts_unix)
		VALUES (?, ?, ?, ?)
	`, agent, actionType, fp, now.Unix()); err != nil {
		return fmt.Errorf("insert denial event: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM denial_events WHERE ts_unix < ?`, now.Add(-denialEventRetention).Unix()); err != nil {
		return fmt.Errorf("prune expired denial events: %w", err)
	}

	var total int
	var wasLocked int
	if err := tx.QueryRow(`SELECT total, locked FROM agent_state WHERE agent = ?`, agent).Scan(&total, &wasLocked); err != nil {
		return fmt.Errorf("read agent_state.total: %w", err)
	}
	if total >= 10 && wasLocked == 0 {
		// Spec 096 FR-005: lock_epoch advances on every lock transition,
		// including the auto-escalation path. Only advance when the
		// agent was not already locked — re-running RecordActionDenial
		// against an already-locked agent must not bump the epoch.
		if _, err := tx.Exec(`UPDATE agent_state SET locked = 1, locked_ts = ?, lock_epoch = lock_epoch + 1 WHERE agent = ?`, nowText, agent); err != nil {
			return fmt.Errorf("set locked: %w", err)
		}
	} else if total >= 10 {
		// Already locked — keep the existing behavior of touching
		// locked_ts so the most-recent-denial timestamp stays fresh,
		// but don't advance the epoch.
		if _, err := tx.Exec(`UPDATE agent_state SET locked_ts = ? WHERE agent = ?`, nowText, agent); err != nil {
			return fmt.Errorf("set locked: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// PruneActionDenialsBefore drops windowed denial events older than the
// maximum cascade window. Aggregate denial counters remain lifetime-spanning;
// only timestamped events used for recent-behavior detection are pruned.
func (c *Counter) PruneActionDenialsBefore(beforeUnix int64) error {
	if _, err := c.db.Exec(`DELETE FROM denial_events WHERE ts_unix < ?`, beforeUnix); err != nil {
		return fmt.Errorf("prune denial events: %w", err)
	}
	return nil
}

// CountActionDenialsSince returns timestamped denials for an agent/action type
// in the trailing window. It is intentionally separate from the aggregate
// denials table: the ladder stays lifetime-spanning, while cascade detection
// is recent behavior.
func (c *Counter) CountActionDenialsSince(agent, actionType string, sinceUnix int64) (int, error) {
	var count int
	if err := c.db.QueryRow(`
		SELECT COUNT(*)
		FROM denial_events
		WHERE agent = ? AND action_type = ? AND ts_unix >= ?
	`, agent, actionType, sinceUnix).Scan(&count); err != nil {
		return 0, fmt.Errorf("count denial events: %w", err)
	}
	return count, nil
}

// Level returns the escalation level for an agent: normal | elevated |
// high | lockdown. Thresholds are hard-coded for v1; config-driven
// thresholds are a v2 concern.
func (c *Counter) Level(agent string) string {
	var total int
	var locked int
	_ = c.db.QueryRow(`SELECT total, locked FROM agent_state WHERE agent = ?`, agent).
		Scan(&total, &locked)
	if locked == 1 {
		return "lockdown"
	}
	switch {
	case total >= 10:
		return "lockdown"
	case total >= 7:
		return "high"
	case total >= 3:
		return "elevated"
	default:
		return "normal"
	}
}

// IsLocked returns true if the agent is in lockdown.
func (c *Counter) IsLocked(agent string) bool {
	return c.Level(agent) == "lockdown"
}

// Lockdown forces an agent into lockdown immediately (operator kill-switch).
// Advances lock_epoch on every invocation (spec 096 R6), including
// repeated calls against an already-locked agent — re-locking is an
// audit-meaningful operator action, distinct from no-op.
func (c *Counter) Lockdown(agent string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = c.db.Exec(`
		INSERT INTO agent_state (agent, total, locked, locked_ts, lock_epoch)
		VALUES (?, 10, 1, ?, 1)
		ON CONFLICT(agent) DO UPDATE SET locked = 1, locked_ts = excluded.locked_ts, lock_epoch = lock_epoch + 1
	`, agent, now)
}

// Reset clears all denial counters and the locked flag for an agent.
func (c *Counter) Reset(agent string) {
	_, _ = c.db.Exec(`DELETE FROM denials WHERE agent = ?`, agent)
	_, _ = c.db.Exec(`DELETE FROM agent_state WHERE agent = ?`, agent)
	_, _ = c.db.Exec(`DELETE FROM denial_events WHERE agent = ?`, agent)
}
