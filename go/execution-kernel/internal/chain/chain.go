// Package chain maintains the chain index: chain_id → (last_seq, last_hash).
// Single-writer, rebuildable from JSONL on startup if missing.
package chain

import (
	"database/sql"
	"errors"

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
