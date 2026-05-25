package speclint

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCheckReasonTaxonomy_AgainstRealSpec115 runs L06 against the real
// spec 115 in the repository. This is a smoke test: spec 115 is the
// canonical example the rule was designed around, so it MUST be clean.
// Skipped if the spec directory isn't present (e.g. running from a
// detached test bundle).
func TestCheckReasonTaxonomy_AgainstRealSpec115(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile = <repo>/go/orchestrator/internal/speclint/l06_spec115_smoke_test.go
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	specsRoot := filepath.Join(repoRoot, "specs")
	specDir := filepath.Join(specsRoot, "115-spec-review-gate")

	if _, err := os.Stat(filepath.Join(specDir, "spec.md")); err != nil {
		t.Skipf("spec 115 not present at %s: %v", specDir, err)
	}

	got, err := CheckReasonTaxonomy(specDir, specsRoot)
	if err != nil {
		t.Fatalf("CheckReasonTaxonomy on real spec 115: %v", err)
	}
	if len(got) != 0 {
		for _, v := range got {
			t.Errorf("unexpected violation: %s:%d [%s] %s", v.File, v.Line, v.Rule, v.Message)
		}
	}
}
