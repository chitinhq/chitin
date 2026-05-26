package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type recordingSink struct {
	events []BlobWrittenPayload
}

func (s *recordingSink) BlobWritten(_ context.Context, payload BlobWrittenPayload) {
	s.events = append(s.events, payload)
}

func TestFSStorePutGetPathLayoutAndIdempotency(t *testing.T) {
	dir := t.TempDir()
	sink := &recordingSink{}
	store, err := NewFSStore(dir, WithEventSink(sink))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("hello blob store")
	hash := sha256Hex(body)
	wantRef := "blob://sha256/" + hash

	ref, err := store.Put(context.Background(), body)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if ref.String() != wantRef {
		t.Fatalf("ref = %q, want %q", ref.String(), wantRef)
	}
	path := filepath.Join(dir, hash[:2], hash[2:]+".blob")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read blob path: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("on-disk body = %q, want %q", got, body)
	}
	resolved, err := store.Get(context.Background(), ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(resolved, body) {
		t.Fatalf("Get body = %q, want %q", resolved, body)
	}
	if tmpFiles, _ := filepath.Glob(filepath.Join(dir, hash[:2], "*.tmp")); len(tmpFiles) != 0 {
		t.Fatalf("tmp files left after success: %v", tmpFiles)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}
	if sink.events[0] != (BlobWrittenPayload{Ref: wantRef, SizeBytes: len(body), SHA256: hash}) {
		t.Fatalf("event = %+v", sink.events[0])
	}

	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	ref2, err := store.Put(context.Background(), body)
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if ref2.String() != ref.String() {
		t.Fatalf("second ref = %q, want %q", ref2.String(), ref.String())
	}
	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Fatalf("idempotent Put rewrote destination: before=%s after=%s", infoBefore.ModTime(), infoAfter.ModTime())
	}
	if len(sink.events) != 1 {
		t.Fatalf("events after idempotent Put = %d, want 1", len(sink.events))
	}
}

func TestFSStoreNoValidBlobOnSimulatedCrash(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("crash before rename")
	crashErr := errors.New("simulated crash")
	store.afterTempWrite = func() error { return crashErr }

	_, err = store.Put(context.Background(), body)
	if !errors.Is(err, crashErr) {
		t.Fatalf("Put err = %v, want simulated crash", err)
	}
	hash := sha256Hex(body)
	if _, err := os.Stat(filepath.Join(dir, hash[:2], hash[2:]+".blob")); !os.IsNotExist(err) {
		t.Fatalf("valid blob path exists after simulated crash: %v", err)
	}
}

func TestFSStoreDefaultDirHonorsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(blobDirEnv, dir)
	if got := DefaultDir(); got != dir {
		t.Fatalf("DefaultDir = %q, want %q", got, dir)
	}
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
