package blob

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

type recordingStore struct {
	puts [][]byte
	ref  Ref
	err  error
}

func (s *recordingStore) Put(_ context.Context, body []byte) (Ref, error) {
	s.puts = append(s.puts, append([]byte(nil), body...))
	return s.ref, s.err
}

func (s *recordingStore) Get(_ context.Context, _ Ref) ([]byte, error) {
	return nil, nil
}

func TestExternalizeThreshold(t *testing.T) {
	ref, err := RefFromHash(strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		body     []byte
		wantBlob bool
	}{
		{name: "small inline", body: bytes.Repeat([]byte("a"), 1024)},
		{name: "large externalized", body: bytes.Repeat([]byte("b"), 2_621_440), wantBlob: true},
		{name: "exact threshold inline", body: bytes.Repeat([]byte("c"), InlineThreshold)},
		{name: "threshold plus one externalized", body: bytes.Repeat([]byte("d"), InlineThreshold+1), wantBlob: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingStore{ref: ref}
			got, err := Externalize(context.Background(), store, tc.body)
			if err != nil {
				t.Fatalf("Externalize: %v", err)
			}
			if tc.wantBlob {
				if got != ref.String() {
					t.Fatalf("Externalize returned %q, want %q", got, ref.String())
				}
				if len(store.puts) != 1 {
					t.Fatalf("Put calls = %d, want 1", len(store.puts))
				}
				if !bytes.Equal(store.puts[0], tc.body) {
					t.Fatal("Put body differed from input")
				}
				return
			}
			if got != string(tc.body) {
				t.Fatalf("Externalize returned non-literal output")
			}
			if len(store.puts) != 0 {
				t.Fatalf("Put calls = %d, want 0", len(store.puts))
			}
		})
	}
}

func TestExternalizeNilStorePassesThrough(t *testing.T) {
	body := bytes.Repeat([]byte("x"), InlineThreshold+1)
	got, err := Externalize(context.Background(), nil, body)
	if err != nil {
		t.Fatalf("Externalize nil store: %v", err)
	}
	if got != string(body) {
		t.Fatal("nil store must pass through literal body")
	}
}
