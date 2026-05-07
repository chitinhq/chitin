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

type PendingApproval struct {
	ID                    string
	Agent                 string
	RuleID                string
	ActionType            string
	ActionTarget          string
	ActionParams          string // JSON-encoded; "" when none
	Cwd                   string
	Reason                string
	Channel               string // "hermes" | "cli-only"
	TimeoutSeconds        int
	RememberWindowSeconds int
	CreatedTs             int64
	NotifiedTs            *int64
	NotifyMsgID           string
	NotifyFailedReason    string
	ResolvedTs            *int64
	Resolution            string // "approved" | "denied" | "timeout"
	ResolutionBy          string // "operator-cli" | "hermes-reply" | "timeout-watcher"
	ResolutionReason      string
	RememberGrantSeconds  *int
}

func (s *EscalateStore) InsertPending(p PendingApproval) error {
	_, err := s.db.Exec(`
		INSERT INTO pending_approvals (
			id, agent, rule_id, action_type, action_target, action_params,
			cwd, reason, channel, timeout_seconds, remember_window_seconds,
			created_ts
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
	`, p.ID, p.Agent, p.RuleID, p.ActionType, p.ActionTarget, p.ActionParams,
		p.Cwd, p.Reason, p.Channel, p.TimeoutSeconds, p.RememberWindowSeconds,
		p.CreatedTs)
	return err
}

func (s *EscalateStore) GetPending(id string) (PendingApproval, error) {
	var p PendingApproval
	var notifiedTs sql.NullInt64
	var resolvedTs sql.NullInt64
	var rememberGrant sql.NullInt64
	err := s.db.QueryRow(`
		SELECT id, agent, rule_id, action_type, action_target,
		       COALESCE(action_params, ''), cwd, reason, channel,
		       timeout_seconds, remember_window_seconds, created_ts,
		       notified_ts, COALESCE(notify_msg_id, ''),
		       COALESCE(notify_failed_reason, ''), resolved_ts,
		       COALESCE(resolution, ''), COALESCE(resolution_by, ''),
		       COALESCE(resolution_reason, ''), remember_grant_seconds
		FROM pending_approvals WHERE id = ?
	`, id).Scan(
		&p.ID, &p.Agent, &p.RuleID, &p.ActionType, &p.ActionTarget,
		&p.ActionParams, &p.Cwd, &p.Reason, &p.Channel,
		&p.TimeoutSeconds, &p.RememberWindowSeconds, &p.CreatedTs,
		&notifiedTs, &p.NotifyMsgID, &p.NotifyFailedReason,
		&resolvedTs, &p.Resolution, &p.ResolutionBy,
		&p.ResolutionReason, &rememberGrant,
	)
	if err != nil {
		return p, err
	}
	if notifiedTs.Valid {
		v := notifiedTs.Int64
		p.NotifiedTs = &v
	}
	if resolvedTs.Valid {
		v := resolvedTs.Int64
		p.ResolvedTs = &v
	}
	if rememberGrant.Valid {
		v := int(rememberGrant.Int64)
		p.RememberGrantSeconds = &v
	}
	return p, nil
}
