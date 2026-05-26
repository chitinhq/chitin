package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

type recordingStore struct {
	puts   int
	bodies [][]byte
	data   map[Ref][]byte
}

func newRecordingStore() *recordingStore {
	return &recordingStore{data: map[Ref][]byte{}}
}

func (s *recordingStore) Put(ctx context.Context, body []byte) (Ref, error) {
	s.puts++
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	sum := sha256.Sum256(body)
	ref, err := NewRef(hex.EncodeToString(sum[:]))
	if err != nil {
		return "", err
	}
	s.data[ref] = append([]byte(nil), body...)
	return ref, nil
}

func (s *recordingStore) Get(ctx context.Context, ref Ref) ([]byte, error) {
	return append([]byte(nil), s.data[ref]...), nil
}

func TestExternalizeThresholdPolicy(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		body     []byte
		wantBlob bool
	}{
		{name: "1 KiB inline", body: bytes.Repeat([]byte("a"), 1024)},
		{name: "2.5 MiB blob", body: bytes.Repeat([]byte("b"), 2_621_440), wantBlob: true},
		{name: "exact threshold inline", body: bytes.Repeat([]byte("c"), InlineThreshold)},
		{name: "threshold plus one blob", body: bytes.Repeat([]byte("d"), InlineThreshold+1), wantBlob: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newRecordingStore()
			got, err := Externalize(ctx, store, tt.body)
			if err != nil {
				t.Fatalf("Externalize: %v", err)
			}
			if tt.wantBlob {
				if !IsRef(got) {
					t.Fatalf("Externalize returned %q, want blob ref", got)
				}
				if len(got) != len("blob://sha256/")+64 {
					t.Fatalf("ref length = %d, want %d", len(got), len("blob://sha256/")+64)
				}
				if store.puts != 1 {
					t.Fatalf("Put calls = %d, want 1", store.puts)
				}
				if !bytes.Equal(store.bodies[0], tt.body) {
					t.Fatal("Put body did not match original bytes")
				}
				return
			}
			if got != string(tt.body) {
				t.Fatal("Externalize did not return literal inline body")
			}
			if store.puts != 0 {
				t.Fatalf("Put calls = %d, want 0", store.puts)
			}
		})
	}
}

func TestExternalizeNilStorePassesThrough(t *testing.T) {
	body := bytes.Repeat([]byte("x"), InlineThreshold+1)
	got, err := Externalize(context.Background(), nil, body)
	if err != nil {
		t.Fatalf("Externalize: %v", err)
	}
	if got != string(body) {
		t.Fatal("nil store should pass through literally")
	}
}
