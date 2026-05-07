// Package gov: PendingApprovalStore + RememberGrants for the operator-
// approval escalation effect. Spec:
// docs/superpowers/specs/2026-05-07-operator-approval-escalation-design.md
package gov

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type EscalateStore struct {
	db *sql.DB
}

func OpenEscalateStore(dbPath string) (*EscalateStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("WAL: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS pending_approvals (
			id              TEXT PRIMARY KEY,
			agent           TEXT NOT NULL,
			rule_id         TEXT NOT NULL,
			action_type     TEXT NOT NULL,
			action_target   TEXT NOT NULL,
			action_params   TEXT,
			cwd             TEXT NOT NULL,
			reason          TEXT NOT NULL,
			channel         TEXT NOT NULL,
			timeout_seconds INTEGER NOT NULL,
			remember_window_seconds INTEGER NOT NULL,
			created_ts      INTEGER NOT NULL,
			notified_ts     INTEGER,
			notify_msg_id   TEXT,
			notify_failed_reason TEXT,
			resolved_ts     INTEGER,
			resolution      TEXT,
			resolution_by   TEXT,
			resolution_reason TEXT,
			remember_grant_seconds INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_unresolved ON pending_approvals (resolved_ts) WHERE resolved_ts IS NULL;
		CREATE TABLE IF NOT EXISTS remember_grants (
			rule_id    TEXT NOT NULL,
			agent      TEXT NOT NULL,
			granted_ts INTEGER NOT NULL,
			expires_ts INTEGER NOT NULL,
			PRIMARY KEY (rule_id, agent)
		);
		CREATE INDEX IF NOT EXISTS idx_remember_unexpired ON remember_grants (expires_ts);
	`); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &EscalateStore{db: db}, nil
}

func (s *EscalateStore) Close() error { return s.db.Close() }
