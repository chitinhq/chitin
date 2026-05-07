// Package gov: PendingApprovalStore + RememberGrants for the operator-
// approval escalation effect. Spec:
// docs/superpowers/specs/2026-05-07-operator-approval-escalation-design.md
package gov

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

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

// ErrAlreadyResolved is returned when a Resolve* call targets a row
// whose resolved_ts is already set. Caller (CLI / hermes-reply parser)
// surfaces this as "pending_already_resolved" to the operator.
var ErrAlreadyResolved = fmt.Errorf("pending_already_resolved")

func (s *EscalateStore) ResolveApprove(id, by string, grantSeconds int) error {
	return s.resolve(id, "approved", by, "", &grantSeconds)
}

func (s *EscalateStore) ResolveDeny(id, by, reason string) error {
	return s.resolve(id, "denied", by, reason, nil)
}

func (s *EscalateStore) ResolveTimeout(id string) error {
	return s.resolve(id, "timeout", "timeout-watcher", "", nil)
}

func (s *EscalateStore) resolve(id, resolution, by, reason string, grantSeconds *int) error {
	now := nowUnix()
	res, err := s.db.Exec(`
		UPDATE pending_approvals
		SET resolved_ts = ?, resolution = ?, resolution_by = ?,
		    resolution_reason = ?, remember_grant_seconds = ?
		WHERE id = ? AND resolved_ts IS NULL
	`, now, resolution, by, reason, nullableInt(grantSeconds), id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrAlreadyResolved
	}
	return nil
}

// nowUnix is a hook for tests to override time.Now().Unix().
var nowUnix = func() int64 { return timeNow().Unix() }

// timeNow is a hook for tests to override time.Now().
var timeNow = time.Now

func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// ListUnresolved returns all pending_approvals rows where resolved_ts
// IS NULL, ordered by created_ts ASC. Used by the CLI's `pending list`.
func (s *EscalateStore) ListUnresolved() ([]PendingApproval, error) {
	rows, err := s.db.Query(`
		SELECT id FROM pending_approvals
		WHERE resolved_ts IS NULL
		ORDER BY created_ts ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingApproval
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		p, err := s.GetPending(id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// HasUnexpiredGrant returns true if there's a row in remember_grants
// for (rule_id, agent) whose expires_ts > now.
func (s *EscalateStore) HasUnexpiredGrant(ruleID, agent string) bool {
	now := nowUnix()
	var count int
	_ = s.db.QueryRow(`
		SELECT COUNT(*) FROM remember_grants
		WHERE rule_id = ? AND agent = ? AND expires_ts > ?
	`, ruleID, agent, now).Scan(&count)
	return count > 0
}

// InsertGrant writes a new grant row. ON CONFLICT replaces — so re-
// approving the same (rule, agent) extends the window from now,
// not from the original grant_ts.
func (s *EscalateStore) InsertGrant(ruleID, agent string, windowSeconds int) error {
	now := nowUnix()
	_, err := s.db.Exec(`
		INSERT INTO remember_grants (rule_id, agent, granted_ts, expires_ts)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (rule_id, agent) DO UPDATE SET
			granted_ts = excluded.granted_ts,
			expires_ts = excluded.expires_ts
	`, ruleID, agent, now, now+int64(windowSeconds))
	return err
}

// SweepExpiredGrants deletes all rows whose expires_ts <= now.
// Returns the count removed.
func (s *EscalateStore) SweepExpiredGrants() (int, error) {
	now := nowUnix()
	res, err := s.db.Exec(`DELETE FROM remember_grants WHERE expires_ts <= ?`, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// WaitPollInterval is how often Wait checks the row for resolution.
// Exported as a var so tests can override (default: 2s).
var WaitPollInterval = 2 * time.Second

// Resolution is what Wait returns: the outcome of a single escalation
// from row insertion through resolution-or-timeout. The gate uses
// OutcomeRuleID to stamp the chain decision.
type Resolution struct {
	EscalationID         string
	Approved             bool
	OperatorReason       string
	GrantedWindowSeconds int
}

// OutcomeRuleID returns the chain rule_id that the gate should stamp
// on the resolved Decision: "escalate-approved" on approve,
// "escalate-timeout" when no operator reason is recorded (the watcher
// path), or "escalate-denied" otherwise.
func (r Resolution) OutcomeRuleID() string {
	if r.Approved {
		return "escalate-approved"
	}
	if r.OperatorReason == "" {
		return "escalate-timeout"
	}
	return "escalate-denied"
}

// WaitArgs bundles the inputs Wait needs. NotifyFn is mockable so
// tests don't depend on a live hermes-gateway; OnInsert is an optional
// hook fired (synchronously) after the row lands so a test thread can
// safely call Resolve* without polling for the generated ID.
type WaitArgs struct {
	RuleID   string
	Agent    string
	Action   Action
	Reason   string
	Config   EscalateConfig
	NotifyFn func(id string, p PendingApproval) error // pass real notifyHermes from caller
	OnInsert func(id string)                          // optional test hook
}

// Wait inserts a pending_approvals row, fires notify in the
// background (when channel is "hermes"), then polls every
// WaitPollInterval until the row is resolved or its deadline passes.
// On deadline, Wait stamps the row with ResolveTimeout and returns a
// non-Approved Resolution. Caller blocks for the full duration.
func (s *EscalateStore) Wait(args WaitArgs) (Resolution, error) {
	id, err := newULID()
	if err != nil {
		return Resolution{}, fmt.Errorf("ulid: %w", err)
	}
	now := nowUnix()

	paramsJSON := ""
	if args.Action.Params != nil {
		if b, err := json.Marshal(args.Action.Params); err == nil {
			paramsJSON = string(b)
		}
	}

	row := PendingApproval{
		ID: id, Agent: args.Agent, RuleID: args.RuleID,
		ActionType: string(args.Action.Type), ActionTarget: args.Action.Target,
		ActionParams: paramsJSON, Cwd: args.Action.Path, Reason: args.Reason,
		Channel: args.Config.Channel, TimeoutSeconds: args.Config.TimeoutSeconds,
		RememberWindowSeconds: args.Config.RememberWindowSeconds,
		CreatedTs:             now,
	}
	if err := s.InsertPending(row); err != nil {
		return Resolution{EscalationID: id}, err
	}
	if args.OnInsert != nil {
		args.OnInsert(id)
	}

	// Fire notify in the background; failures get stamped on the row
	// (future task) but don't fail the Wait — the CLI fallback still
	// works while operator catches up out-of-band.
	if args.Config.Channel == "hermes" && args.NotifyFn != nil {
		go func() { _ = args.NotifyFn(id, row) }()
	}

	deadline := time.Unix(now, 0).Add(time.Duration(args.Config.TimeoutSeconds) * time.Second)
	ticker := time.NewTicker(WaitPollInterval)
	defer ticker.Stop()
	for {
		<-ticker.C
		got, err := s.GetPending(id)
		if err != nil {
			return Resolution{EscalationID: id}, err
		}
		if got.ResolvedTs != nil {
			grant := 0
			if got.RememberGrantSeconds != nil {
				grant = *got.RememberGrantSeconds
			}
			return Resolution{
				EscalationID:         id,
				Approved:             got.Resolution == "approved",
				OperatorReason:       got.ResolutionReason,
				GrantedWindowSeconds: grant,
			}, nil
		}
		if timeNow().After(deadline) {
			_ = s.ResolveTimeout(id)
			return Resolution{EscalationID: id, Approved: false}, nil
		}
	}
}

// ListUnresolvedPastDeadline returns rows whose
// (created_ts + timeout_seconds) < nowSec. Used by the sweeper.
func (s *EscalateStore) ListUnresolvedPastDeadline(nowSec int64) ([]PendingApproval, error) {
	rows, err := s.db.Query(`
		SELECT id FROM pending_approvals
		WHERE resolved_ts IS NULL
		AND (created_ts + timeout_seconds) < ?
		ORDER BY created_ts ASC
	`, nowSec)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingApproval
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		p, err := s.GetPending(id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// SweepStale resolves all unresolved rows whose deadline has passed.
// Called at gate startup (recovers orphaned rows from a crashed
// kernel) and optionally on a timer. Returns count resolved.
func (s *EscalateStore) SweepStale() (int, error) {
	now := nowUnix()
	stale, err := s.ListUnresolvedPastDeadline(now)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, p := range stale {
		if err := s.ResolveTimeout(p.ID); err == nil {
			count++
		}
	}
	return count, nil
}
