// session.go — Counter API extensions for spec 096 operator session-state.
//
// The pre-spec-096 API offered Lockdown() (sticky kill-switch that wipes
// the row partially) and Reset() (destructive — deletes the row and the
// denial history). Spec 096 adds a softer sibling, Unlock(), that
// preserves audit history; an operator-CLI lock that distinguishes itself
// from auto-escalation via the source field on the chain event; and
// read-only Status queries.
//
// Chain emission lives in the CLI subcommand layer (cmd/chitin-kernel/
// session_*.go) — this file is gov-package-pure and never reaches outside
// gov.db. The CLI is responsible for emitting session_locked /
// session_unlocked chain events after a successful Counter call.

package gov

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoAgent is returned by Counter.Status / Counter.LockEpoch when no
// agent_state row exists for the given agent. CLI handlers map this to
// exit-code 1 with the spec'd stderr text.
var ErrNoAgent = errors.New("no agent_state row")

// AgentStatus is the read-only snapshot Counter.Status returns. Fields
// match data-model.md Entity 1 (the extended agent_state row) plus the
// derived `level` for operator readability.
type AgentStatus struct {
	Agent     string
	Locked    bool
	LockedTs  string // RFC3339, empty if never locked
	UnlockTs  string // RFC3339, empty if never unlocked
	LockEpoch int
	Total     int
	Level     string // normal | elevated | high | lockdown — matches Counter.Level()
}

// UnlockResult is what Counter.Unlock returns to its caller. The CLI
// layer uses this to (a) decide which message to print and (b) emit a
// session_unlocked chain event with locked_ts_before + total_at_unlock
// for forensic snapshot (data-model.md Entity 5).
type UnlockResult struct {
	// Idempotent is true when the agent was already unlocked at the
	// moment Counter.Unlock ran. The chain event IS still emitted (for
	// forensic completeness — operator action happened) but lock_epoch
	// is NOT advanced (spec 096 D5).
	Idempotent      bool
	LockEpochAfter  int
	LockedTsBefore  string // empty if never previously locked
	TotalAtUnlock   int    // lifetime denial total at the moment of unlock
}

// LockResult is what Counter.OperatorLock returns. Distinguished from
// the raw Counter.Lockdown() Go API because we want the CLI to know the
// new epoch for both stdout and the chain event payload.
type LockResult struct {
	LockEpochAfter int
}

// Unlock transitions an agent from locked → unlocked, preserving the
// audit trail (denial counters and denial_events are untouched).
//
// Idempotent: unlocking an already-unlocked agent succeeds but does NOT
// advance lock_epoch. Returns UnlockResult.Idempotent=true so the caller
// can render the spec'd "(was already unlocked)" suffix.
//
// Returns ErrNoAgent when no agent_state row exists for `agent` — the
// CLI maps this to exit-code 1.
func (c *Counter) Unlock(agent string) (UnlockResult, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := c.db.Begin()
	if err != nil {
		return UnlockResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var (
		locked         int
		lockedTs       sql.NullString
		lockEpochBefore int
		total          int
	)
	err = tx.QueryRow(`SELECT locked, locked_ts, lock_epoch, total FROM agent_state WHERE agent = ?`, agent).
		Scan(&locked, &lockedTs, &lockEpochBefore, &total)
	if errors.Is(err, sql.ErrNoRows) {
		return UnlockResult{}, ErrNoAgent
	}
	if err != nil {
		return UnlockResult{}, fmt.Errorf("read agent_state: %w", err)
	}

	if locked == 0 {
		// Idempotent path — DO NOT issue an UPDATE, DO NOT advance epoch.
		// Per spec 096 D5: chain event is still emitted (caller's job)
		// for forensic completeness; we just don't mutate state.
		if err := tx.Commit(); err != nil {
			return UnlockResult{}, fmt.Errorf("commit (idempotent): %w", err)
		}
		return UnlockResult{
			Idempotent:     true,
			LockEpochAfter: lockEpochBefore,
			LockedTsBefore: lockedTs.String,
			TotalAtUnlock:  total,
		}, nil
	}

	// Non-idempotent unlock: clear locked, set unlock_ts, advance epoch.
	// Read-back the new epoch in the same transaction (D10 — read-after-
	// write so the emitted event's epoch matches gov.db post-commit).
	if _, err := tx.Exec(`UPDATE agent_state SET locked = 0, unlock_ts = ?, lock_epoch = lock_epoch + 1 WHERE agent = ?`, now, agent); err != nil {
		return UnlockResult{}, fmt.Errorf("update agent_state: %w", err)
	}
	var epochAfter int
	if err := tx.QueryRow(`SELECT lock_epoch FROM agent_state WHERE agent = ?`, agent).Scan(&epochAfter); err != nil {
		return UnlockResult{}, fmt.Errorf("read epoch after update: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return UnlockResult{}, fmt.Errorf("commit: %w", err)
	}

	return UnlockResult{
		Idempotent:     false,
		LockEpochAfter: epochAfter,
		LockedTsBefore: lockedTs.String,
		TotalAtUnlock:  total,
	}, nil
}

// OperatorLock is the CLI-facing wrapper around the existing Lockdown()
// semantics, returning the post-transition lock_epoch so the caller can
// emit the chain event with the same epoch gov.db now holds (D10).
//
// Bootstrap-locks an unseen agent — INSERTs `(agent, total=10, locked=1,
// locked_ts=NOW, lock_epoch=1)`. Re-locks an already-locked agent —
// advances epoch by 1 and updates locked_ts (lock-of-locked is NOT
// idempotent per contracts/lock-subcommand.md).
func (c *Counter) OperatorLock(agent string) (LockResult, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := c.db.Begin()
	if err != nil {
		return LockResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO agent_state (agent, total, locked, locked_ts, lock_epoch)
		VALUES (?, 10, 1, ?, 1)
		ON CONFLICT(agent) DO UPDATE SET
			locked = 1,
			locked_ts = excluded.locked_ts,
			lock_epoch = lock_epoch + 1,
			total = MAX(total, 10)
	`, agent, now); err != nil {
		return LockResult{}, fmt.Errorf("upsert agent_state: %w", err)
	}
	var epochAfter int
	if err := tx.QueryRow(`SELECT lock_epoch FROM agent_state WHERE agent = ?`, agent).Scan(&epochAfter); err != nil {
		return LockResult{}, fmt.Errorf("read epoch after upsert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return LockResult{}, fmt.Errorf("commit: %w", err)
	}
	return LockResult{LockEpochAfter: epochAfter}, nil
}

// Status returns a snapshot of one agent's state. Returns ErrNoAgent
// when no row exists for `agent`.
func (c *Counter) Status(agent string) (*AgentStatus, error) {
	var (
		total     int
		locked    int
		lockedTs  sql.NullString
		unlockTs  sql.NullString
		lockEpoch int
	)
	err := c.db.QueryRow(`SELECT total, locked, locked_ts, unlock_ts, lock_epoch FROM agent_state WHERE agent = ?`, agent).
		Scan(&total, &locked, &lockedTs, &unlockTs, &lockEpoch)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoAgent
	}
	if err != nil {
		return nil, fmt.Errorf("read agent_state: %w", err)
	}
	return &AgentStatus{
		Agent:     agent,
		Locked:    locked == 1,
		LockedTs:  lockedTs.String,
		UnlockTs:  unlockTs.String,
		LockEpoch: lockEpoch,
		Total:     total,
		Level:     deriveLevel(total, locked),
	}, nil
}

// StatusAll returns every agent's snapshot, sorted by agent ASCII.
// Determinism per FR-009 — operators must be able to diff successive
// snapshots.
func (c *Counter) StatusAll() ([]AgentStatus, error) {
	rows, err := c.db.Query(`SELECT agent, total, locked, locked_ts, unlock_ts, lock_epoch FROM agent_state ORDER BY agent ASC`)
	if err != nil {
		return nil, fmt.Errorf("query agent_state: %w", err)
	}
	defer rows.Close()
	var out []AgentStatus
	for rows.Next() {
		var (
			agent     string
			total     int
			locked    int
			lockedTs  sql.NullString
			unlockTs  sql.NullString
			lockEpoch int
		)
		if err := rows.Scan(&agent, &total, &locked, &lockedTs, &unlockTs, &lockEpoch); err != nil {
			return nil, fmt.Errorf("scan agent_state row: %w", err)
		}
		out = append(out, AgentStatus{
			Agent:     agent,
			Locked:    locked == 1,
			LockedTs:  lockedTs.String,
			UnlockTs:  unlockTs.String,
			LockEpoch: lockEpoch,
			Total:     total,
			Level:     deriveLevel(total, locked),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent_state: %w", err)
	}
	return out, nil
}

// LockEpoch returns just the lock_epoch for the named agent — cheaper
// than full Status when the caller only needs the generation counter.
// Returns ErrNoAgent when no row exists.
func (c *Counter) LockEpoch(agent string) (int, error) {
	var lockEpoch int
	err := c.db.QueryRow(`SELECT lock_epoch FROM agent_state WHERE agent = ?`, agent).Scan(&lockEpoch)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoAgent
	}
	if err != nil {
		return 0, fmt.Errorf("read lock_epoch: %w", err)
	}
	return lockEpoch, nil
}

// deriveLevel computes the same escalation level Counter.Level() returns
// from raw total + locked values. Pulled into a shared helper so Status
// / StatusAll produce identical Level strings without a per-row round
// trip through Counter.Level().
func deriveLevel(total, locked int) string {
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
