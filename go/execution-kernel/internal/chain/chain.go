// Package chain maintains the chain index: chain_id → (last_seq, last_hash).
// Single-writer. Reconciled from JSONL by `RebuildFromJSONL` before every emit (see `cmdEmit`).
package chain

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// ChainInfo is the last-known state of a chain.
type ChainInfo struct {
	ChainID  string
	LastSeq  int64
	LastHash string
}

// Index is a kernel-owned SQLite handle for chain bookkeeping.
type Index struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS chains (
	chain_id  TEXT PRIMARY KEY,
	last_seq  INTEGER NOT NULL,
	last_hash TEXT NOT NULL
);
`

func OpenIndex(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Index{db: db}, nil
}

func (i *Index) Close() error {
	return i.db.Close()
}

func (i *Index) Get(chainID string) (*ChainInfo, error) {
	row := i.db.QueryRow(`SELECT chain_id, last_seq, last_hash FROM chains WHERE chain_id = ?`, chainID)
	var info ChainInfo
	if err := row.Scan(&info.ChainID, &info.LastSeq, &info.LastHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &info, nil
}

func (i *Index) Upsert(chainID string, seq int64, hash string) error {
	_, err := i.db.Exec(
		`INSERT INTO chains (chain_id, last_seq, last_hash)
		 VALUES (?, ?, ?)
		 ON CONFLICT(chain_id) DO UPDATE SET last_seq = excluded.last_seq, last_hash = excluded.last_hash`,
		chainID, seq, hash,
	)
	return err
}

// jsonlEventRow holds the fields we need when scanning JSONL event lines.
type jsonlEventRow struct {
	ChainID  string `json:"chain_id"`
	Seq      int64  `json:"seq"`
	ThisHash string `json:"this_hash"`
}

// RebuildFromJSONL brings the index into agreement with the ground-truth JSONL
// files found in chitinDir.
//
// Precondition:  chitinDir contains zero or more files whose names match
//                events-*.jsonl; each line is either valid JSON containing
//                chain_id/seq/this_hash or is malformed and must be tolerated.
//
// Postcondition: For every chain_id that appears in any JSONL file,
//                idx.Get(chain_id) returns (last_seq, last_hash) where
//                last_seq is the maximum seq seen for that chain across all
//                files, and last_hash is the this_hash of that highest-seq
//                event.  Chains not present in any JSONL file are untouched.
//
// Invariant:     JSONL files are never modified.  The DB is monotonically
//                brought up to match JSONL: an existing row is only
//                overwritten when the JSONL evidence has a strictly higher seq.
//                Calling RebuildFromJSONL twice in a row produces the same DB
//                state (idempotent).  Concurrent invocations are safe because
//                SQLite serialises writers and the UPSERT guard (WHERE
//                excluded.last_seq > last_seq) is a monotone advance.
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

	// Build an in-memory map of chain_id → best (seq, hash) from all JSONL files.
	best := make(map[string]jsonlEventRow)

	for _, fpath := range files {
		f, err := os.Open(fpath)
		if err != nil {
			// Non-fatal: skip unreadable file.
			continue
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Bytes()
			var row jsonlEventRow
			if err := json.Unmarshal(line, &row); err != nil {
				// Tolerate malformed lines.
				continue
			}
			if row.ChainID == "" || row.ThisHash == "" {
				continue
			}
			if prev, ok := best[row.ChainID]; !ok || row.Seq > prev.Seq {
				best[row.ChainID] = row
			}
		}
		f.Close()
	}

	// Upsert into the DB, but only advance (never retreat).
	for chainID, row := range best {
		_, err := i.db.Exec(
			`INSERT INTO chains (chain_id, last_seq, last_hash)
			 VALUES (?, ?, ?)
			 ON CONFLICT(chain_id) DO UPDATE
			   SET last_seq  = excluded.last_seq,
			       last_hash = excluded.last_hash
			   WHERE excluded.last_seq > last_seq`,
			chainID, row.Seq, row.ThisHash,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
