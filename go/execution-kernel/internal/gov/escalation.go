package gov

import (
	"database/sql"
	"fmt"
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

// OpenCounter opens/creates the SQLite DB at dbPath with WAL mode.
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
			locked_ts TEXT
		);
	`); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Counter{db: db}, nil
}

// Close the underlying DB.
func (c *Counter) Close() error {
	return c.db.Close()
}

// RecordDenial increments counters for (agent, fp) by weight. If total
// denials reach the lockdown threshold (10), marks the agent locked.
func (c *Counter) RecordDenial(agent, fp string, weight int) {
	if weight <= 0 {
		weight = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := c.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	_, _ = tx.Exec(`
		INSERT INTO denials (agent, action_fp, count, first_ts, last_ts)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(agent, action_fp) DO UPDATE SET
			count = count + excluded.count,
			last_ts = excluded.last_ts
	`, agent, fp, weight, now, now)

	_, _ = tx.Exec(`
		INSERT INTO agent_state (agent, total, locked)
		VALUES (?, ?, 0)
		ON CONFLICT(agent) DO UPDATE SET total = total + excluded.total
	`, agent, weight)

	var total int
	_ = tx.QueryRow(`SELECT total FROM agent_state WHERE agent = ?`, agent).Scan(&total)
	if total >= 10 {
		_, _ = tx.Exec(`UPDATE agent_state SET locked = 1, locked_ts = ? WHERE agent = ?`, now, agent)
	}

	_ = tx.Commit()
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
func (c *Counter) Lockdown(agent string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = c.db.Exec(`
		INSERT INTO agent_state (agent, total, locked, locked_ts)
		VALUES (?, 10, 1, ?)
		ON CONFLICT(agent) DO UPDATE SET locked = 1, locked_ts = excluded.locked_ts
	`, agent, now)
}

// Reset clears all denial counters and the locked flag for an agent.
func (c *Counter) Reset(agent string) {
	_, _ = c.db.Exec(`DELETE FROM denials WHERE agent = ?`, agent)
	_, _ = c.db.Exec(`DELETE FROM agent_state WHERE agent = ?`, agent)
}
