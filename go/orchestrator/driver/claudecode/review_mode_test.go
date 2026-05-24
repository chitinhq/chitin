package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TestInvoke_ReviewMode_ValidationFailureSurfacesInExplanation covers
// spec 109 FR-004 and the T007 case: the model emits parseable JSON, but
// the JSON violates one of the spec 094 FR-014 per-enum invariants
// (here, verdict=approve with non-empty blockers — invariant
// approve_blockers_must_be_empty). The driver MUST emit StatusFailed
// and the validation invariant name MUST appear in Result.Explanation so
// DispatchMachineReviewer can classify the outcome as
// FailureMalformedShape without re-parsing, and operators can attribute
// the malformed verdict to a specific model behavior.
//
// This is distinct from the T006 prose-only case: the JSON is well-
// formed, the closed shape is what fails. Without driver-side validation
// the malformed verdict would only be caught downstream in the
// aggregator, losing per-driver attribution.
//
// Discriminator: SpecID="094" / TaskID="review" mirrors what the
// DispatchMachineReviewer activity already sets (see
// activities/review/dispatch_machine_reviewer.go); the driver routes
// review-mode invocations through the StructuredVerdict path on this
// signal.
func TestInvoke_ReviewMode_ValidationFailureSurfacesInExplanation(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")
	// approve with non-empty blockers — well-formed JSON, but violates
	// the approve_blockers_must_be_empty invariant from
	// activities/review/verdict/invariants.go.
	invalidVerdict := `{"verdict":"approve","concerns":[],"recommendations":[],"blockers":["missing nil check on line 88"]}`
	script := "#!/usr/bin/env bash\n" +
		"cat <<'JSON'\n" + invalidVerdict + "\nJSON\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "wu-review-invariant-001",
		SpecID:       "094",
		TaskID:       "review",
		WorktreePath: dir,
		Context:      `{"pr":{"repo":"chitinhq/chitin","number":1007}}`,
	}
	res, err := d.Invoke(context.Background(), wu)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res.Status != driver.StatusFailed {
		t.Fatalf("status = %s, want StatusFailed; explanation=%q", res.Status, res.Explanation)
	}
	if !strings.Contains(res.Explanation, "malformed_verdict") {
		t.Errorf("explanation = %q, want to contain \"malformed_verdict\"", res.Explanation)
	}
	// The invariant name from verdict.ValidationError.Invariant must reach
	// the explanation so the failure attributes to the specific rule
	// violated (FR-004) — not a generic "validation failed" string.
	if !strings.Contains(res.Explanation, "approve_blockers_must_be_empty") {
		t.Errorf("explanation = %q, want to contain validation invariant name \"approve_blockers_must_be_empty\"", res.Explanation)
	}
}
