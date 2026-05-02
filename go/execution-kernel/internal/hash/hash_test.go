package hash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalJSON_SortsKeys(t *testing.T) {
	input := map[string]any{"b": 1, "a": 2}
	got, err := CanonicalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":2,"b":1}`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestCanonicalJSON_NoWhitespace(t *testing.T) {
	input := map[string]any{"a": []any{1.0, 2.0, 3.0}}
	got, err := CanonicalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":[1,2,3]}`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestCanonicalJSON_NestedSort(t *testing.T) {
	input := map[string]any{"z": map[string]any{"b": 1, "a": 2}}
	got, err := CanonicalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"z":{"a":2,"b":1}}`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSha256Hex_Empty(t *testing.T) {
	got := Sha256Hex("")
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSha256Hex_ABC(t *testing.T) {
	got := Sha256Hex("abc")
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestHashEvent_ExcludesThisHash(t *testing.T) {
	e1 := map[string]any{"a": 1.0, "b": 2.0, "this_hash": "xyz"}
	e2 := map[string]any{"a": 1.0, "b": 2.0, "this_hash": "abc"}
	h1, _ := HashEvent(e1)
	h2, _ := HashEvent(e2)
	if h1 != h2 {
		t.Errorf("expected equal hashes when only this_hash differs: %q vs %q", h1, h2)
	}
}

// TestCanonicalJSON_EventGolden asserts byte-equality against a SHARED
// fixture at libs/contracts/tests/canonical.golden.{json,txt} that the
// TS canonicalJSON implementation also reads. Closes #7 + #15: the two
// implementations can no longer drift silently on opposite-side test
// suites — a divergence on either side fails its own goldensuite.
//
// Fixture source-of-truth lives under libs/contracts/tests/ because
// the contracts library is the cross-language schema home; the Go and
// TS test suites both load from there.
func TestCanonicalJSON_EventGolden(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	inputBytes, err := os.ReadFile(filepath.Join(repoRoot, "libs", "contracts", "tests", "canonical.golden.json"))
	if err != nil {
		t.Fatalf("read input fixture: %v", err)
	}
	wantBytes, err := os.ReadFile(filepath.Join(repoRoot, "libs", "contracts", "tests", "canonical.golden.txt"))
	if err != nil {
		t.Fatalf("read want fixture: %v", err)
	}
	want := strings.TrimRight(string(wantBytes), "\n")

	var event map[string]any
	if err := json.Unmarshal(inputBytes, &event); err != nil {
		t.Fatalf("parse input fixture: %v", err)
	}
	got, err := CanonicalJSON(event)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("golden canonical JSON mismatch (Go side):\ngot:  %s\nwant: %s", got, want)
	}
	var rt map[string]any
	if err := json.Unmarshal([]byte(got), &rt); err != nil {
		t.Fatalf("canonical JSON did not parse: %v", err)
	}
}

// findRepoRoot walks up from the test's working dir until it finds a
// directory containing both `libs/contracts` and `go/execution-kernel`.
// Used by the shared-fixture goldensuite so the test works regardless
// of `go test` cwd.
func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(cwd, "libs", "contracts")); err == nil {
			if _, err := os.Stat(filepath.Join(cwd, "go", "execution-kernel")); err == nil {
				return cwd, nil
			}
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "", fmt.Errorf("repo root not found walking up from cwd")
		}
		cwd = parent
	}
}
