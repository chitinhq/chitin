package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

type recordingEmitter struct {
	payloads []BlobWrittenPayload
}

func (e *recordingEmitter) EmitBlobWritten(ctx context.Context, payload BlobWrittenPayload) error {
	e.payloads = append(e.payloads, payload)
	return nil
}

func TestFSStorePutGetLayoutAndIdempotency(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	emitter := &recordingEmitter{}
	store := NewFSStore(WithDir(dir), WithEmitter(emitter))
	body := []byte("the complete driver transcript")

	ref1, err := store.Put(ctx, body)
	if err != nil {
		t.Fatalf("Put first: %v", err)
	}
	ref2, err := store.Put(ctx, body)
	if err != nil {
		t.Fatalf("Put second: %v", err)
	}
	if ref1 != ref2 {
		t.Fatalf("refs differ: %s != %s", ref1, ref2)
	}

	got, err := store.Get(ctx, ref1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("Get body did not round-trip")
	}

	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	wantPath := filepath.Join(dir, hash[:2], hash[2:]+".blob")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected sharded blob at %s: %v", wantPath, err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, hash[:2], "*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("tmp files left behind: %v", matches)
	}
	if len(emitter.payloads) != 1 {
		t.Fatalf("blob_written events = %d, want 1", len(emitter.payloads))
	}
	wantPayload := BlobWrittenPayload{Ref: ref1.String(), SizeBytes: len(body), SHA256: hash}
	if emitter.payloads[0] != wantPayload {
		t.Fatalf("payload = %+v, want %+v", emitter.payloads[0], wantPayload)
	}
}

func TestFSStoreDefaultDirHonorsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(BlobDirEnv, dir)
	if got := DefaultDir(); got != dir {
		t.Fatalf("DefaultDir = %q, want %q", got, dir)
	}
}
