package gov

import (
	"strings"
	"testing"
	"time"
)

func TestNewULID_LengthAndAlphabet(t *testing.T) {
	id, err := newULID()
	if err != nil {
		t.Fatalf("newULID: %v", err)
	}
	if len(id) != 26 {
		t.Fatalf("len=%d want 26 (%q)", len(id), id)
	}
	for i, c := range id {
		if !strings.ContainsRune(crockfordAlphabet, c) {
			t.Fatalf("char %d=%q not in Crockford alphabet (id=%q)", i, c, id)
		}
	}
}

func TestNewULID_TimeSortable(t *testing.T) {
	a, err := newULID()
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	// Sleep past the 1ms timestamp resolution so the prefix differs.
	time.Sleep(2 * time.Millisecond)
	b, err := newULID()
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if a >= b {
		t.Fatalf("expected a<b lexicographic order, got a=%q b=%q", a, b)
	}
	// Time prefix is the first 10 chars; rest is random.
	if a[:10] >= b[:10] {
		t.Fatalf("time prefix not strictly increasing: a=%q b=%q", a[:10], b[:10])
	}
}

func TestNewULID_NoCollisionsBurst(t *testing.T) {
	const n = 5000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id, err := newULID()
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("collision at iter %d: %q", i, id)
		}
		seen[id] = struct{}{}
	}
}

// TestNewULID_FirstCharBounded asserts the canonical ULID encoding's
// "two leading zero bits" invariant: the first char encodes 5 bits of
// which only the bottom 3 are non-zero (top of the 48-bit timestamp).
// The first char's alphabet index must therefore be < 8 — i.e. in '0'..'7'.
func TestNewULID_FirstCharBounded(t *testing.T) {
	id, err := newULID()
	if err != nil {
		t.Fatalf("newULID: %v", err)
	}
	idx := strings.IndexRune(crockfordAlphabet, rune(id[0]))
	if idx < 0 || idx >= 8 {
		t.Fatalf("first char index=%d (char=%q), want < 8 per ULID spec", idx, id[0])
	}
}
