package event

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestEvent_FixtureRoundTripPreservesAllKeys is the Go side of the
// schema-drift gate (#17). The shared fixture
// libs/contracts/tests/envelope.golden.json is the contract; this
// test decodes it into Event and asserts ToMap's output contains
// every key from the fixture.
//
// What this catches:
//
//	zod adds a field but Go's Event struct doesn't get it
//	  → JSON-decode of the fixture leaves Event.X = zero-value,
//	    ToMap doesn't emit X, round-trip key set is missing X.
//
//	Go adds a field but zod schema doesn't include it
//	  → fixture would have to be updated to include the new field
//	    for THIS test to pass; the TS-side parity test would then
//	    fail because zod doesn't know about it.
//
// In both cases the drift becomes a CI failure on one side or the
// other, forcing an explicit cross-language coordination.
func TestEvent_FixtureRoundTripPreservesAllKeys(t *testing.T) {
	repoRoot, err := findRepoRootForEvent()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	fixturePath := filepath.Join(repoRoot, "libs", "contracts", "tests", "envelope.golden.json")
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// Step 1: snapshot the fixture's key set.
	var fixtureMap map[string]any
	if err := json.Unmarshal(raw, &fixtureMap); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	fixtureKeys := keys(fixtureMap)

	// Step 2: decode into Event, re-emit via ToMap.
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("decode into Event: %v", err)
	}
	emitted, err := ev.ToMap()
	if err != nil {
		t.Fatalf("ToMap: %v", err)
	}
	emittedKeys := keys(emitted)

	// Step 3: every key the fixture has must appear in the round-tripped
	// output. Extras in the round-trip are also a sign of drift (Go
	// emitting a field zod doesn't know about) — fail on those too.
	missing := setDiff(fixtureKeys, emittedKeys)
	extra := setDiff(emittedKeys, fixtureKeys)
	if len(missing) > 0 {
		t.Errorf("Event.ToMap dropped fixture keys (Go-side drift): %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("Event.ToMap emitted keys not in fixture (Go added fields without zod update): %v", extra)
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func setDiff(a, b []string) []string {
	bSet := make(map[string]struct{}, len(b))
	for _, x := range b {
		bSet[x] = struct{}{}
	}
	var out []string
	for _, x := range a {
		if _, ok := bSet[x]; !ok {
			out = append(out, x)
		}
	}
	return out
}

func findRepoRootForEvent() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(cwd, "libs", "contracts", "tests", "envelope.golden.json")); err == nil {
			return cwd, nil
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "", fmt.Errorf("envelope.golden.json not found walking up from cwd")
		}
		cwd = parent
	}
}
