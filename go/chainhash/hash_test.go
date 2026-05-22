package chainhash

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestParityCorpus pins HashEvent against the shared cross-language fixture
// corpus. The same corpus is consumed by libs/run-sdk/tests/hash-parity.test.ts,
// which asserts the TypeScript hashEvent produces these identical values.
func TestParityCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/parity-corpus.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var corpus []struct {
		Name         string         `json:"name"`
		Event        map[string]any `json:"event"`
		ExpectedHash string         `json:"expected_hash"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}
	if len(corpus) == 0 {
		t.Fatal("parity corpus is empty")
	}
	for _, c := range corpus {
		t.Run(c.Name, func(t *testing.T) {
			got, err := HashEvent(c.Event)
			if err != nil {
				t.Fatalf("HashEvent: %v", err)
			}
			if got != c.ExpectedHash {
				t.Errorf("hash mismatch\n  got:  %s\n  want: %s", got, c.ExpectedHash)
			}
			// Determinism: a second call yields the identical hash.
			if again, _ := HashEvent(c.Event); again != got {
				t.Errorf("non-deterministic hash: %s != %s", again, got)
			}
		})
	}
}

// TestCanonicalJSONSortsKeysAtEveryDepth verifies object keys are ordered
// lexicographically at every level of nesting, not just the top level.
func TestCanonicalJSONSortsKeysAtEveryDepth(t *testing.T) {
	in := map[string]any{
		"b": map[string]any{"y": 2, "x": 1},
		"a": []any{map[string]any{"q": true, "p": false}},
	}
	got, err := CanonicalJSON(in)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	want := `{"a":[{"p":false,"q":true}],"b":{"x":1,"y":2}}`
	if got != want {
		t.Errorf("canonical form\n  got:  %s\n  want: %s", got, want)
	}
}

// TestHashEventExcludesThisHash verifies an event's own this_hash field is
// excluded from its hash input — an event never hashes its own digest.
func TestHashEventExcludesThisHash(t *testing.T) {
	withHash := map[string]any{"chain_id": "c", "seq": 0, "this_hash": "deadbeef"}
	without := map[string]any{"chain_id": "c", "seq": 0}
	a, err := HashEvent(withHash)
	if err != nil {
		t.Fatalf("HashEvent(withHash): %v", err)
	}
	b, err := HashEvent(without)
	if err != nil {
		t.Fatalf("HashEvent(without): %v", err)
	}
	if a != b {
		t.Errorf("this_hash not excluded from hash input: %s != %s", a, b)
	}
}

// TestStrictDefaultRejectsUnsupportedType verifies the strict boundary
// behavior (research.md Decision 2 / FR-003): a value type outside the
// supported JSON set is rejected with an error, never silently encoded.
func TestStrictDefaultRejectsUnsupportedType(t *testing.T) {
	type weird struct{ X int }
	cases := []any{
		weird{X: 1},
		[]int{1, 2, 3},
		map[string]string{"k": "v"},
		complex128(1 + 2i),
	}
	for _, v := range cases {
		_, err := CanonicalJSON(v)
		if err == nil {
			t.Errorf("CanonicalJSON(%T) = nil error; want unsupported-type error", v)
			continue
		}
		if !strings.Contains(err.Error(), "unsupported type") {
			t.Errorf("CanonicalJSON(%T) error = %q; want 'unsupported type'", v, err)
		}
	}
	// HashEvent surfaces the same error when a payload value is unsupported.
	if _, err := HashEvent(map[string]any{"payload": weird{X: 9}}); err == nil {
		t.Error("HashEvent with unsupported payload type = nil error; want error")
	}
}
