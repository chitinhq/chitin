package sidecar

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const (
	MaxBlobBytes   = 256 * 1024
	RetentionDays  = 30
	truncatedLabel = "\n[TRUNCATED]"
)

var (
	ErrInvalidBlobType = errors.New("sidecar: invalid blob type")
	ErrMissingEventID  = errors.New("sidecar: event_id is required")
)

var validBlobTypes = map[string]struct{}{
	"prompt":         {},
	"thinking":       {},
	"tool_input":     {},
	"tool_output":    {},
	"model_response": {},
}

const schema = `
CREATE TABLE IF NOT EXISTS event_blobs (
	event_id TEXT NOT NULL,
	blob_type TEXT NOT NULL,
	blob BLOB NOT NULL,
	redacted BOOL NOT NULL DEFAULT 0,
	ts INTEGER NOT NULL,
	PRIMARY KEY (event_id, blob_type)
);
CREATE TABLE IF NOT EXISTS event_aliases (
	alias TEXT PRIMARY KEY,
	event_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_event_blobs_ts ON event_blobs(ts);
`

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir sidecar dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Put(eventID, blobType string, body []byte) error {
	if s == nil || s.db == nil {
		return errors.New("sidecar: store is not open")
	}
	if eventID == "" {
		return ErrMissingEventID
	}
	if _, ok := validBlobTypes[blobType]; !ok {
		return ErrInvalidBlobType
	}
	if err := s.pruneExpired(time.Now().UTC()); err != nil {
		return err
	}
	redactedBody, changed := RedactBytes(body)
	cappedBody, truncated := capBlob(redactedBody)
	if truncated {
		changed = true
	}
	_, err := s.db.Exec(
		`INSERT INTO event_blobs (event_id, blob_type, blob, redacted, ts)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(event_id, blob_type) DO UPDATE SET
		   blob = excluded.blob,
		   redacted = excluded.redacted,
		   ts = excluded.ts`,
		eventID, blobType, cappedBody, changed, time.Now().UTC().Unix(),
	)
	return err
}

func (s *Store) Get(eventID, blobType string) ([]byte, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sidecar: store is not open")
	}
	if eventID == "" {
		return nil, ErrMissingEventID
	}
	if _, ok := validBlobTypes[blobType]; !ok {
		return nil, ErrInvalidBlobType
	}
	var blob []byte
	err := s.db.QueryRow(
		`SELECT blob FROM event_blobs WHERE event_id = ? AND blob_type = ?`,
		eventID, blobType,
	).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return blob, err
}

func (s *Store) GetAll(eventID string) (map[string][]byte, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("sidecar: store is not open")
	}
	if eventID == "" {
		return nil, ErrMissingEventID
	}
	rows, err := s.db.Query(
		`SELECT blob_type, blob FROM event_blobs WHERE event_id = ? ORDER BY blob_type`,
		eventID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var blobType string
		var blob []byte
		if err := rows.Scan(&blobType, &blob); err != nil {
			return nil, err
		}
		out[blobType] = blob
	}
	return out, rows.Err()
}

func (s *Store) PutAlias(alias, eventID string) error {
	if s == nil || s.db == nil {
		return errors.New("sidecar: store is not open")
	}
	if alias == "" || eventID == "" {
		return ErrMissingEventID
	}
	_, err := s.db.Exec(
		`INSERT INTO event_aliases (alias, event_id)
		 VALUES (?, ?)
		 ON CONFLICT(alias) DO UPDATE SET event_id = excluded.event_id`,
		alias, eventID,
	)
	return err
}

func (s *Store) ResolveEventID(id string) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("sidecar: store is not open")
	}
	if id == "" {
		return "", ErrMissingEventID
	}
	var eventID string
	err := s.db.QueryRow(`SELECT event_id FROM event_aliases WHERE alias = ?`, id).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return id, nil
	}
	return eventID, err
}

func (s *Store) pruneExpired(now time.Time) error {
	cutoff := now.Add(-RetentionDays * 24 * time.Hour).Unix()
	_, err := s.db.Exec(`DELETE FROM event_blobs WHERE ts < ?`, cutoff)
	return err
}

func capBlob(body []byte) ([]byte, bool) {
	if len(body) <= MaxBlobBytes {
		return body, false
	}
	limit := MaxBlobBytes - len(truncatedLabel)
	if limit < 0 {
		limit = 0
	}
	body = append(bytes.Clone(body[:limit]), truncatedLabel...)
	return body, true
}

func DecodeBlob(blob []byte) any {
	if len(blob) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(blob, &v); err == nil {
		return v
	}
	return string(blob)
}
