package speclint

import (
	"os"
	"testing"
)

// TestL07_AgainstActualSpec115 reads the real spec.md from this repo
// and asserts L07 reports zero violations. This catches drift between
// the rule and the spec template the team actually writes against.
//
// The test path is relative to this file's directory (go/orchestrator/
// internal/speclint) — Go test invocations run with the package dir
// as cwd.
func TestL07_AgainstActualSpec115(t *testing.T) {
	const path = "../../../../specs/115-spec-review-gate/spec.md"
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real spec not reachable from test cwd (%v) — skipping integration check", err)
	}
	got := L07UserStoryTests(path, string(content))
	if len(got) != 0 {
		t.Fatalf("L07 flagged real spec 115 (it shouldn't — every US has a marker): %+v", got)
	}
}
