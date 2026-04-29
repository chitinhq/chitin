package gov

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CostDelta is the unit of debit against a BudgetEnvelope.
//
// Real-time enforcement fires on ToolCalls and InputBytes (deterministic,
// observable at PreToolUse time). USD is informational/best-effort —
// per-token rates are partly fictional for Copilot CLI's flat-rate model,
// so the BudgetUSD cap never denies; real $USD reconciliation is deferred
// to OTEL ingest. See spec §"Why calls + bytes, not $USD".
type CostDelta struct {
	USD         float64
	InputBytes  int64
	OutputBytes int64
	ToolCalls   int64
}

// BudgetLimits is the cap configuration on an envelope.
//
// MaxToolCalls=0 or MaxInputBytes=0 means "no cap on this dimension".
// BudgetUSD is informational only — it is recorded and surfaced by
// `envelope inspect`/`envelope tail --stats` but never causes denial.
type BudgetLimits struct {
	MaxToolCalls  int64
	MaxInputBytes int64
	BudgetUSD     float64
}

// EnvelopeState is a read-only snapshot of an envelope row.
type EnvelopeState struct {
	ID           string
	CreatedAt    string
	ClosedAt     string
	Limits       BudgetLimits
	SpentCalls   int64
	SpentBytes   int64
	SpentUSD     float64
	LastSpendAt  string
}

// BudgetStore wraps a *sql.DB pinned at ~/.chitin/gov.db with the
// envelope-specific tables migrated. Cross-process atomicity comes from
// sqlite WAL; no flock at the application level.
type BudgetStore struct {
	db *sql.DB
}

// BudgetEnvelope is a handle to one envelope row. Spend is the only
// mutator that crosses processes; under WAL it is a single transactional
// UPDATE keyed on (id) and the row's current state.
type BudgetEnvelope struct {
	ID     string
	store  *BudgetStore
	Limits BudgetLimits
}

// Sentinel errors. Callers compare with errors.Is.
var (
	ErrEnvelopeExhausted = errors.New("envelope exhausted")
	ErrEnvelopeClosed    = errors.New("envelope closed")
	ErrEnvelopeNotFound  = errors.New("envelope not found")
)

// OpenBudgetStore opens or creates ~/.chitin/gov.db with WAL mode and
// ensures the envelope tables exist. Safe to call concurrently with
// OpenCounter on the same dbPath; both use WAL.
func OpenBudgetStore(dbPath string) (*BudgetStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if err := migrateBudget(db); err != nil {
		return nil, err
	}
	return &BudgetStore{db: db}, nil
}

// Close the underlying DB.
func (s *BudgetStore) Close() error { return s.db.Close() }

// DB returns the underlying *sql.DB. Exported for tests and for callers
// that want to share a single connection pool with the Counter.
func (s *BudgetStore) DB() *sql.DB { return s.db }

// migrateBudget is additive: adds envelope tables to an existing gov.db
// next to the v1 escalation counter tables. Each CREATE is idempotent;
// schema_version row records what's been applied so future migrations
// can branch without re-checking shape.
func migrateBudget(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			component TEXT PRIMARY KEY,
			version   INTEGER NOT NULL,
			applied_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS envelopes (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			closed_at TEXT,
			max_tool_calls INTEGER NOT NULL DEFAULT 0,
			max_input_bytes INTEGER NOT NULL DEFAULT 0,
			budget_usd REAL NOT NULL DEFAULT 0,
			spent_calls INTEGER NOT NULL DEFAULT 0,
			spent_bytes INTEGER NOT NULL DEFAULT 0,
			spent_usd REAL NOT NULL DEFAULT 0,
			last_spend_at TEXT
		);
		CREATE TABLE IF NOT EXISTS envelope_grants (
			envelope_id TEXT NOT NULL REFERENCES envelopes(id),
			granted_at TEXT NOT NULL,
			delta_calls INTEGER NOT NULL DEFAULT 0,
			delta_bytes INTEGER NOT NULL DEFAULT 0,
			delta_usd REAL NOT NULL DEFAULT 0,
			reason TEXT
		);
		CREATE INDEX IF NOT EXISTS envelope_grants_id_idx ON envelope_grants(envelope_id);
	`); err != nil {
		return fmt.Errorf("migrate budget schema: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		INSERT INTO schema_version (component, version, applied_at)
		VALUES ('budget', 1, ?)
		ON CONFLICT(component) DO NOTHING
	`, now); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return nil
}

// Create allocates a new envelope row with a fresh ULID.
func (s *BudgetStore) Create(limits BudgetLimits) (*BudgetEnvelope, error) {
	id, err := newULID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`
		INSERT INTO envelopes (id, created_at, max_tool_calls, max_input_bytes, budget_usd)
		VALUES (?, ?, ?, ?, ?)
	`, id, now, limits.MaxToolCalls, limits.MaxInputBytes, limits.BudgetUSD); err != nil {
		return nil, fmt.Errorf("create envelope: %w", err)
	}
	return &BudgetEnvelope{ID: id, store: s, Limits: limits}, nil
}

// Load returns the envelope handle for id, or ErrEnvelopeNotFound if no
// row exists. Limits are read from the row, so a stale handle held by a
// shim sees the latest cap after an operator-grant.
func (s *BudgetStore) Load(id string) (*BudgetEnvelope, error) {
	var calls, bytes int64
	var usd float64
	err := s.db.QueryRow(`
		SELECT max_tool_calls, max_input_bytes, budget_usd FROM envelopes WHERE id = ?
	`, id).Scan(&calls, &bytes, &usd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEnvelopeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load envelope: %w", err)
	}
	return &BudgetEnvelope{
		ID:     id,
		store:  s,
		Limits: BudgetLimits{MaxToolCalls: calls, MaxInputBytes: bytes, BudgetUSD: usd},
	}, nil
}

// Spend debits CostDelta and returns:
//   - nil on success
//   - ErrEnvelopeClosed if closed_at is set (sticky)
//   - ErrEnvelopeExhausted if the debit would breach MaxToolCalls or
//     MaxInputBytes (and atomically sets closed_at)
//
// Cross-process correctness: a single `UPDATE … WHERE id=? AND
// closed_at IS NULL AND new_calls<=cap AND new_bytes<=cap` returns
// rowsAffected=0 when any predicate fails. We then read state to
// distinguish "closed" from "would exhaust" so the caller gets a precise
// error. The cap check is in the same statement as the increment, so
// two concurrent processes cannot both succeed past the cap.
func (e *BudgetEnvelope) Spend(d CostDelta) error {
	if d.ToolCalls < 0 || d.InputBytes < 0 {
		return fmt.Errorf("negative spend: %+v", d)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	// First, atomically debit if-and-only-if closed_at IS NULL and the
	// post-debit values fit within caps. cap=0 means "uncapped" — we
	// short-circuit that with "OR max_tool_calls=0".
	res, err := e.store.db.Exec(`
		UPDATE envelopes
		SET spent_calls = spent_calls + ?,
		    spent_bytes = spent_bytes + ?,
		    spent_usd = spent_usd + ?,
		    last_spend_at = ?
		WHERE id = ?
		  AND closed_at IS NULL
		  AND (max_tool_calls = 0 OR spent_calls + ? <= max_tool_calls)
		  AND (max_input_bytes = 0 OR spent_bytes + ? <= max_input_bytes)
	`,
		d.ToolCalls, d.InputBytes, d.USD, now, e.ID,
		d.ToolCalls, d.InputBytes,
	)
	if err != nil {
		return fmt.Errorf("envelope spend: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("envelope spend rows: %w", err)
	}
	if n == 1 {
		return nil
	}

	// Debit failed — figure out why for a precise error. Read closed_at
	// + current spent state in one query.
	var closedAt sql.NullString
	var calls, bytes, maxCalls, maxBytes int64
	err = e.store.db.QueryRow(`
		SELECT closed_at, spent_calls, spent_bytes, max_tool_calls, max_input_bytes
		FROM envelopes WHERE id = ?
	`, e.ID).Scan(&closedAt, &calls, &bytes, &maxCalls, &maxBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrEnvelopeNotFound
	}
	if err != nil {
		return fmt.Errorf("envelope state read: %w", err)
	}
	if closedAt.Valid {
		return ErrEnvelopeClosed
	}
	// Cap breach — sticky-close so siblings deny on next call without
	// having to recompute the breach themselves.
	if _, err := e.store.db.Exec(`
		UPDATE envelopes SET closed_at = ? WHERE id = ? AND closed_at IS NULL
	`, now, e.ID); err != nil {
		return fmt.Errorf("envelope sticky-close: %w", err)
	}
	return fmt.Errorf("%w: id=%s calls=%d/%d bytes=%d/%d",
		ErrEnvelopeExhausted, e.ID, calls, maxCalls, bytes, maxBytes)
}

// Inspect returns a read-only snapshot.
func (e *BudgetEnvelope) Inspect() (EnvelopeState, error) {
	var st EnvelopeState
	st.ID = e.ID
	var closedAt sql.NullString
	var lastSpend sql.NullString
	err := e.store.db.QueryRow(`
		SELECT created_at, closed_at, max_tool_calls, max_input_bytes, budget_usd,
		       spent_calls, spent_bytes, spent_usd, last_spend_at
		FROM envelopes WHERE id = ?
	`, e.ID).Scan(
		&st.CreatedAt, &closedAt,
		&st.Limits.MaxToolCalls, &st.Limits.MaxInputBytes, &st.Limits.BudgetUSD,
		&st.SpentCalls, &st.SpentBytes, &st.SpentUSD, &lastSpend,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return st, ErrEnvelopeNotFound
	}
	if err != nil {
		return st, fmt.Errorf("envelope inspect: %w", err)
	}
	if closedAt.Valid {
		st.ClosedAt = closedAt.String
	}
	if lastSpend.Valid {
		st.LastSpendAt = lastSpend.String
	}
	return st, nil
}

// Grant raises caps and reopens the envelope (clears closed_at) inside
// one transaction, then logs the grant in envelope_grants. Negative or
// zero deltas are accepted but discouraged — caller intent is "raise".
func (e *BudgetEnvelope) Grant(deltaCalls, deltaBytes int64, deltaUSD float64, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := e.store.db.Begin()
	if err != nil {
		return fmt.Errorf("grant begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		UPDATE envelopes
		SET max_tool_calls = max_tool_calls + ?,
		    max_input_bytes = max_input_bytes + ?,
		    budget_usd = budget_usd + ?,
		    closed_at = NULL
		WHERE id = ?
	`, deltaCalls, deltaBytes, deltaUSD, e.ID); err != nil {
		return fmt.Errorf("grant update: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO envelope_grants
		(envelope_id, granted_at, delta_calls, delta_bytes, delta_usd, reason)
		VALUES (?, ?, ?, ?, ?, ?)
	`, e.ID, now, deltaCalls, deltaBytes, deltaUSD, reason); err != nil {
		return fmt.Errorf("grant insert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("grant commit: %w", err)
	}
	// Refresh local Limits view.
	st, err := e.Inspect()
	if err != nil {
		return err
	}
	e.Limits = st.Limits
	return nil
}

// CloseEnvelope marks the envelope closed. Subsequent Spends fail with
// ErrEnvelopeClosed. Idempotent.
func (e *BudgetEnvelope) CloseEnvelope() error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := e.store.db.Exec(`
		UPDATE envelopes SET closed_at = ? WHERE id = ? AND closed_at IS NULL
	`, now, e.ID); err != nil {
		return fmt.Errorf("close envelope: %w", err)
	}
	return nil
}

// List returns recent envelopes ordered by creation time descending.
// Limit caps the result count; pass 0 for no limit.
func (s *BudgetStore) List(limit int) ([]EnvelopeState, error) {
	q := `SELECT id, created_at, closed_at, max_tool_calls, max_input_bytes, budget_usd,
	             spent_calls, spent_bytes, spent_usd, last_spend_at
	      FROM envelopes ORDER BY id DESC`
	args := []any{}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list envelopes: %w", err)
	}
	defer rows.Close()
	var out []EnvelopeState
	for rows.Next() {
		var st EnvelopeState
		var closedAt, lastSpend sql.NullString
		if err := rows.Scan(
			&st.ID, &st.CreatedAt, &closedAt,
			&st.Limits.MaxToolCalls, &st.Limits.MaxInputBytes, &st.Limits.BudgetUSD,
			&st.SpentCalls, &st.SpentBytes, &st.SpentUSD, &lastSpend,
		); err != nil {
			return nil, fmt.Errorf("list scan: %w", err)
		}
		if closedAt.Valid {
			st.ClosedAt = closedAt.String
		}
		if lastSpend.Valid {
			st.LastSpendAt = lastSpend.String
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
