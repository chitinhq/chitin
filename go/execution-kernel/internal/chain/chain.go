// Package chain maintains the chain index: chain_id → (last_seq, last_hash).
// Single-writer. Reconciled from JSONL by `RebuildFromJSONL` before every emit (see `cmdEmit`).
package chain

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// ChainInfo is the last-known state of a chain. The EventType /
// LogicalHash / EmitTs fields support emit-time invariants:
//
//   - LastEventType: drives the session_end-is-last enforcement (#3).
//     If it equals "session_end" and a non-session_end event tries to
//     emit on the same chain, the emit is refused.
//   - LastLogicalHash: drives idempotent emit (#16). A retried event
//     whose logical content matches the chain tail's LogicalHash AND
//     was emitted within the dedup window is treated as a duplicate.
//   - LastEmitTs: paired with LastLogicalHash for the dedup window
//     check. RFC3339 string (matches event.Event.Ts format).
//
// These fields default to "" for chains seeded before the schema
// migration (see ALTER TABLE in OpenIndex). Empty values are equivalent
// to "no prior info" — the invariants gracefully no-op on legacy chains.
type ChainInfo struct {
	ChainID         string
	LastSeq         int64
	LastHash        string
	LastEventType   string
	LastLogicalHash string
	LastEmitTs      string
}

// Index is a kernel-owned SQLite handle for chain bookkeeping.
type Index struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS chains (
	chain_id          TEXT PRIMARY KEY,
	last_seq          INTEGER NOT NULL,
	last_hash         TEXT NOT NULL,
	last_event_type   TEXT NOT NULL DEFAULT '',
	last_logical_hash TEXT NOT NULL DEFAULT '',
	last_emit_ts      TEXT NOT NULL DEFAULT ''
);
`

// migrations runs idempotent ADD COLUMN statements for DBs created before
// the last_event_type / last_logical_hash / last_emit_ts columns existed.
// SQLite returns a "duplicate column" error if the column is already
// present; we swallow that specific error so re-running OpenIndex on a
// fresh DB doesn't fail.
var migrations = []string{
	`ALTER TABLE chains ADD COLUMN last_event_type   TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE chains ADD COLUMN last_logical_hash TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE chains ADD COLUMN last_emit_ts      TEXT NOT NULL DEFAULT ''`,
}

func OpenIndex(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Serialize all in-process writers through a single connection. This
	// ensures that concurrent goroutines queue at the pool level rather than
	// racing on BEGIN IMMEDIATE. For cross-process callers (separate kernel
	// subshells sharing the same DB file), the busy_timeout PRAGMA below
	// makes the SQLite driver spin-wait up to 5 s instead of returning
	// SQLITE_BUSY immediately.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	for _, stmt := range migrations {
		if _, err := db.Exec(stmt); err != nil {
			// SQLite reports "duplicate column name: X" when the column
			// is already present from a fresh-schema CREATE TABLE.
			// Swallow that specific case; surface anything else.
			if !strings.Contains(err.Error(), "duplicate column") {
				db.Close()
				return nil, err
			}
		}
	}
	return &Index{db: db}, nil
}

func (i *Index) Close() error {
	return i.db.Close()
}

func (i *Index) Get(chainID string) (*ChainInfo, error) {
	row := i.db.QueryRow(
		`SELECT chain_id, last_seq, last_hash, last_event_type, last_logical_hash, last_emit_ts
		 FROM chains WHERE chain_id = ?`, chainID)
	var info ChainInfo
	if err := row.Scan(
		&info.ChainID, &info.LastSeq, &info.LastHash,
		&info.LastEventType, &info.LastLogicalHash, &info.LastEmitTs,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &info, nil
}

// Upsert writes the chain tail atomically. Used by RebuildFromJSONL when
// reconciling from disk. The eventType/logicalHash/emitTs args may be ""
// when the caller only knows seq+hash (legacy paths); the columns default
// to "" so legacy callers stay correct.
func (i *Index) Upsert(chainID string, seq int64, hash, eventType, logicalHash, emitTs string) error {
	_, err := i.db.Exec(
		`INSERT INTO chains (chain_id, last_seq, last_hash, last_event_type, last_logical_hash, last_emit_ts)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(chain_id) DO UPDATE SET
		   last_seq          = excluded.last_seq,
		   last_hash         = excluded.last_hash,
		   last_event_type   = excluded.last_event_type,
		   last_logical_hash = excluded.last_logical_hash,
		   last_emit_ts      = excluded.last_emit_ts`,
		chainID, seq, hash, eventType, logicalHash, emitTs,
	)
	return err
}

// EmitTx holds the state of an in-progress BEGIN IMMEDIATE transaction for a
// single chain emit. Callers must call Commit or Rollback exactly once.
//
// Precondition:  The Index is open; chain_id is non-empty.
// Postcondition on Commit(seq, hash): the chains table for chain_id has
//   (last_seq = seq, last_hash = hash) and the IMMEDIATE lock is released.
// Postcondition on Rollback(): the chains table is unchanged from its state
//   at BeginEmit and the lock is released.
// Invariant: at most one BeginEmit across all OS processes sharing the same
//   DB file can be in the "acquired but not yet Commit/Rollback" state at any
//   moment — SQLite's RESERVED lock (acquired by BEGIN IMMEDIATE) enforces this.
type EmitTx struct {
	// Current is the last-known state of the chain at the time BeginEmit was
	// called. Nil if the chain has no prior events.
	Current *ChainInfo

	chainID string
	conn    *sql.Conn
	done    bool
}

// Commit upserts the chain tail and commits + releases the IMMEDIATE lock.
// eventType / logicalHash / emitTs become the new tail's metadata used by
// the next BeginEmit's invariant checks (session_end-is-last, idempotent
// dedup window).
func (tx *EmitTx) Commit(seq int64, hash, eventType, logicalHash, emitTs string) error {
	if tx.done {
		return nil
	}
	tx.done = true
	ctx := context.Background()
	_, err := tx.conn.ExecContext(ctx,
		`INSERT INTO chains (chain_id, last_seq, last_hash, last_event_type, last_logical_hash, last_emit_ts)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(chain_id) DO UPDATE SET
		   last_seq          = excluded.last_seq,
		   last_hash         = excluded.last_hash,
		   last_event_type   = excluded.last_event_type,
		   last_logical_hash = excluded.last_logical_hash,
		   last_emit_ts      = excluded.last_emit_ts`,
		tx.chainID, seq, hash, eventType, logicalHash, emitTs,
	)
	if err != nil {
		// Best-effort rollback before releasing the conn.
		_, _ = tx.conn.ExecContext(ctx, "ROLLBACK")
		tx.conn.Close()
		return err
	}
	if _, err := tx.conn.ExecContext(ctx, "COMMIT"); err != nil {
		tx.conn.Close()
		return err
	}
	return tx.conn.Close()
}

// Rollback aborts the transaction and releases the lock. Safe to call after a
// successful Commit (no-op).
func (tx *EmitTx) Rollback() error {
	if tx.done {
		return nil
	}
	tx.done = true
	ctx := context.Background()
	_, _ = tx.conn.ExecContext(ctx, "ROLLBACK")
	return tx.conn.Close()
}

// BeginEmit acquires a writer lock on the index for the given chain and returns
// the current (LastSeq, LastHash) plus a transaction handle. Callers must call
// Commit(newSeq, newHash) on success or Rollback() on any error path. Multiple
// concurrent kernel subshells will serialize at BeginEmit — the second caller
// blocks until the first calls Commit or Rollback.
//
// Precondition:  a valid Index; chainID is non-empty.
// Postcondition on Commit(seq, hash): see EmitTx.Commit.
// Postcondition on Rollback(): see EmitTx.Rollback.
// Invariant: at most one BeginEmit across all processes holding the same DB
//   file can be in the "acquired but not yet Commit/Rollback" state at any moment.
func (i *Index) BeginEmit(chainID string) (*EmitTx, error) {
	ctx := context.Background()
	conn, err := i.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		conn.Close()
		return nil, err
	}
	// Read current chain state inside the transaction.
	row := conn.QueryRowContext(ctx,
		`SELECT chain_id, last_seq, last_hash, last_event_type, last_logical_hash, last_emit_ts
		 FROM chains WHERE chain_id = ?`, chainID)
	var info ChainInfo
	var current *ChainInfo
	if err := row.Scan(
		&info.ChainID, &info.LastSeq, &info.LastHash,
		&info.LastEventType, &info.LastLogicalHash, &info.LastEmitTs,
	); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
			conn.Close()
			return nil, err
		}
		// New chain — current stays nil.
	} else {
		current = &info
	}
	return &EmitTx{Current: current, chainID: chainID, conn: conn}, nil
}

// jsonlEventRow holds the fields we need when scanning JSONL event lines.
// PrevHash is a pointer so we can distinguish "not present" (unusual) from
// "explicitly null" (chain head). EventType + Ts are read so the rebuild
// can populate the same chain-tail metadata that emit.go relies on for
// the session_end-is-last and idempotent-dedup invariants.
//
// LogicalHash is recomputed from event_type + payload at rebuild time —
// not stored on the JSONL line — so legacy events written before the
// dedup feature still get a coherent dedup hash.
type jsonlEventRow struct {
	ChainID   string          `json:"chain_id"`
	Seq       int64           `json:"seq"`
	ThisHash  string          `json:"this_hash"`
	PrevHash  *string         `json:"prev_hash"`
	EventType string          `json:"event_type"`
	Ts        string          `json:"ts"`
	Payload   json.RawMessage `json:"payload"`
}

// RebuildFromJSONL brings the index into agreement with the ground-truth JSONL
// files found in chitinDir, refusing to reconcile any chain whose linkage is
// broken.
//
// Precondition:  chitinDir contains zero or more files whose names match
//                events-*.jsonl; each line is either valid JSON containing
//                chain_id/seq/this_hash/prev_hash or is malformed and must
//                be tolerated.
//
// Postcondition: For every chain_id that appears in any JSONL file, if the
//                chain's events form a valid linked list (seqs 0..N-1,
//                prev_hash at seq 0 is null, prev_hash at seq k matches
//                this_hash at seq k-1), idx.Get(chain_id) returns
//                (N-1, this_hash at seq N-1). Chains not present in any
//                JSONL file are untouched.
//
// On any broken linkage detected — gap in seq, wrong prev_hash, non-null
// prev_hash at head, duplicate seq with conflicting this_hash — returns an
// error identifying the chain and the specific inconsistency; the DB is left
// unchanged.  Rationale: a chain whose JSONL linkage is broken is either
// corrupted or forged; continuing to emit on top of it would produce a
// divergent fork, so the correct response is to refuse rather than to guess.
//
// Invariant:     JSONL files are never modified.  The DB is monotonically
//                brought up to match validated JSONL: an existing row is
//                only overwritten when the JSONL evidence has a strictly
//                higher seq.  Calling RebuildFromJSONL twice in a row
//                produces the same DB state (idempotent).  Concurrent
//                invocations are safe because SQLite serialises writers and
//                the UPSERT guard (WHERE excluded.last_seq > last_seq) is a
//                monotone advance.
//
// Phase 2 note:  This scans all JSONL on every emit.  At Phase 1.5 volumes
//                this is acceptable.  Phase 2 can replace this with a
//                checkpoint-based incremental reconcile.
func (i *Index) RebuildFromJSONL(chitinDir string) error {
	if _, err := os.Stat(chitinDir); os.IsNotExist(err) {
		return nil
	}

	pattern := filepath.Join(chitinDir, "events-*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	// Collect all rows per chain — not just the max — so we can validate linkage.
	rowsByChain := make(map[string][]jsonlEventRow)
	for _, fpath := range files {
		f, err := os.Open(fpath)
		if err != nil {
			// Non-fatal: skip unreadable file.
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			var row jsonlEventRow
			if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
				continue // tolerate malformed lines
			}
			if row.ChainID == "" || row.ThisHash == "" {
				continue
			}
			rowsByChain[row.ChainID] = append(rowsByChain[row.ChainID], row)
		}
		f.Close()
	}

	// Sort, dedup exact duplicates, validate linkage, and upsert per chain.
	for chainID, rows := range rowsByChain {
		sort.Slice(rows, func(a, b int) bool { return rows[a].Seq < rows[b].Seq })

		// Collapse exact duplicates (same seq + same this_hash); refuse conflicts.
		deduped := rows[:0]
		for k, row := range rows {
			if k > 0 && rows[k-1].Seq == row.Seq {
				if rows[k-1].ThisHash != row.ThisHash {
					return fmt.Errorf(
						"chain %s: seq %d has conflicting this_hash: %s vs %s",
						chainID, row.Seq, rows[k-1].ThisHash, row.ThisHash,
					)
				}
				continue
			}
			deduped = append(deduped, row)
		}
		rows = deduped

		// Seqs must be 0..N-1 contiguous; linkage must hold at every step.
		for k, row := range rows {
			if row.Seq != int64(k) {
				return fmt.Errorf(
					"chain %s: expected seq %d at position %d, got %d (gap or out-of-range head)",
					chainID, k, k, row.Seq,
				)
			}
			if k == 0 {
				if row.PrevHash != nil {
					return fmt.Errorf(
						"chain %s: seq 0 must have prev_hash=null, got %q",
						chainID, *row.PrevHash,
					)
				}
				continue
			}
			if row.PrevHash == nil {
				return fmt.Errorf(
					"chain %s: seq %d has prev_hash=null (must link to seq %d)",
					chainID, row.Seq, row.Seq-1,
				)
			}
			if *row.PrevHash != rows[k-1].ThisHash {
				return fmt.Errorf(
					"chain %s: seq %d prev_hash %s does not match seq %d this_hash %s",
					chainID, row.Seq, *row.PrevHash, rows[k-1].Seq, rows[k-1].ThisHash,
				)
			}
		}

		if len(rows) == 0 {
			continue
		}
		last := rows[len(rows)-1]
		// LogicalHash recomputed from canonical (event_type, payload) so
		// the dedup-window check on next emit works even on legacy chains
		// reconciled from JSONL.
		logicalHash := LogicalHash(last.EventType, last.Payload)
		if _, err := i.db.Exec(
			`INSERT INTO chains (chain_id, last_seq, last_hash, last_event_type, last_logical_hash, last_emit_ts)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(chain_id) DO UPDATE
			   SET last_seq          = excluded.last_seq,
			       last_hash         = excluded.last_hash,
			       last_event_type   = excluded.last_event_type,
			       last_logical_hash = excluded.last_logical_hash,
			       last_emit_ts      = excluded.last_emit_ts
			   WHERE excluded.last_seq > last_seq`,
			chainID, last.Seq, last.ThisHash, last.EventType, logicalHash, last.Ts,
		); err != nil {
			return err
		}
	}
	return nil
}

// LogicalHash returns a stable hash over (event_type, payload) — the
// fields that uniquely identify a logical event regardless of when /
// how many times it was emitted. Used for idempotent-emit dedup (#16):
// retries of the same logical event yield identical LogicalHash even
// though Ts/Seq/PrevHash/ThisHash all differ.
//
// Empty event_type or empty payload yield a stable empty-prefix hash;
// callers should treat the empty string as "no logical hash available"
// and skip the dedup check.
func LogicalHash(eventType string, payload json.RawMessage) string {
	if eventType == "" {
		return ""
	}
	// Canonical-ish JSON: payload may already be canonical from
	// hash.HashEvent's pipeline; if not, json.Marshal of a sorted
	// re-decode would be more rigorous. For dedup purposes we just
	// need byte-equal across retries, which is what the kernel-emit
	// path produces (deterministic JSON encoding).
	h := sha256.New()
	h.Write([]byte(eventType))
	h.Write([]byte{0})
	h.Write(payload)
	return fmt.Sprintf("%x", h.Sum(nil))
}
