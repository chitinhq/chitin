package sidecar

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorePutGetAndRedact(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sidecar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	body := []byte(`{"token":"abc123456789","path":"/home/red/.ssh/id_ed25519","safe":"ok"}`)
	if err := store.Put("evt-1", "tool_input", body); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("evt-1", "tool_input")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "abc123456789") {
		t.Fatalf("secret leaked: %s", got)
	}
	if strings.Contains(string(got), "/home/red/.ssh/id_ed25519") {
		t.Fatalf("path leaked: %s", got)
	}
	if !strings.Contains(string(got), redactedValue) || !strings.Contains(string(got), redactedPath) {
		t.Fatalf("expected redaction markers, got %s", got)
	}
}

func TestStorePutCapsBlobSize(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sidecar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	large := bytes.Repeat([]byte("a"), MaxBlobBytes+64)
	if err := store.Put("evt-2", "prompt", large); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("evt-2", "prompt")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > MaxBlobBytes {
		t.Fatalf("blob size=%d want <= %d", len(got), MaxBlobBytes)
	}
	if !strings.HasSuffix(string(got), truncatedLabel) {
		t.Fatalf("expected truncation marker, got suffix %q", string(got[len(got)-len(truncatedLabel):]))
	}
}

func TestStorePrunesExpiredRows(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sidecar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	oldTS := time.Now().UTC().Add(-(RetentionDays + 1) * 24 * time.Hour).Unix()
	if _, err := store.db.Exec(
		`INSERT INTO event_blobs (event_id, blob_type, blob, redacted, ts) VALUES (?, ?, ?, 0, ?)`,
		"evt-old", "prompt", []byte(`"old"`), oldTS,
	); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("evt-new", "prompt", []byte(`"new"`)); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("evt-old", "prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expired blob still present: %s", got)
	}
}

func TestDecodeBlob(t *testing.T) {
	if got := DecodeBlob([]byte(`{"ok":true}`)); got == nil {
		t.Fatal("expected decoded json object")
	}
	if got := DecodeBlob([]byte(`plain text`)); got != "plain text" {
		t.Fatalf("decode plain text=%v", got)
	}
}
