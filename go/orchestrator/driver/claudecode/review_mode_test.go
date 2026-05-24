package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TestInvoke_ReviewMode_CleanJSONEmitsValidatedVerdict covers spec 109 FR-005
// and the US1 independent test: when the claudecode review-mode invocation
// receives a clean StructuredVerdict JSON document on stdout, the driver
// emits StatusSucceeded with the validated (canonically re-serialized)
// verdict body in Result.Explanation — the field spec 094's
// DispatchMachineReviewer activity parses via verdict.ParseStructured.
//
// This guards the 2026-05-24 dogfood failure where claudecode returned prose
// and the activity classified the outcome as FailureMalformedJSON, blocking
// verdict aggregation on PR #1007. With T001-T003 in place, a clean CLI
// response is propagated to the activity in the spec-compliant shape.
//
// Discriminator: SpecID="094" / TaskID="review" mirrors what the activity
// dispatcher already sets (activities/review/dispatch_machine_reviewer.go).
// FR-001 leaves the discriminator name open ("Tool=review or whatever") —
// T003 may also introduce an explicit WorkUnit.Tool field; either signal
// should route this WorkUnit through the review-mode codepath.
func TestInvoke_ReviewMode_CleanJSONEmitsValidatedVerdict(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")
	// Canonical enum spelling is hyphenated ("approve-with-comments"); see
	// activities/review/verdict/verdict.go. The body satisfies the FR-014
	// invariant for approve-with-comments (empty blockers + at least one
	// concern or recommendation).
	cleanJSON := `{"verdict":"approve-with-comments","concerns":["nit: name shadowing on line 42"],"recommendations":["extract a helper for the duplicated branch"],"blockers":[]}`
	script := "#!/usr/bin/env bash\n" +
		"cat <<'JSON'\n" + cleanJSON + "\nJSON\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "wu-review-clean-001",
		SpecID:       "094",
		TaskID:       "review",
		WorktreePath: dir,
		Context:      `{"pr":{"repo":"chitinhq/chitin","number":1007}}`,
	}
	res, err := d.Invoke(context.Background(), wu)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res.Status != driver.StatusSucceeded {
		t.Fatalf("status = %s, want StatusSucceeded; explanation=%q", res.Status, res.Explanation)
	}

	var got verdict.StructuredVerdict
	if err := json.Unmarshal([]byte(res.Explanation), &got); err != nil {
		t.Fatalf("Result.Explanation is not parseable as StructuredVerdict JSON: %v\nexplanation=%q", err, res.Explanation)
	}
	if err := verdict.Validate(got); err != nil {
		t.Fatalf("Result.Explanation failed verdict.Validate: %v\nexplanation=%q", err, res.Explanation)
	}
	if got.Verdict != verdict.ApproveWithComments {
		t.Errorf("verdict = %q, want %q", got.Verdict, verdict.ApproveWithComments)
	}
	if len(got.Concerns) != 1 || got.Concerns[0] == "" {
		t.Errorf("concerns = %v, want one non-empty entry", got.Concerns)
	}
	if len(got.Blockers) != 0 {
		t.Errorf("blockers = %v, want empty for approve-with-comments", got.Blockers)
	}
}

// TestInvoke_ReviewMode_ProseOnlyEmitsMalformedVerdict covers spec 109 FR-004
// and the US2 independent test: when the claudecode review-mode invocation
// produces only prose (no JSON-shaped substring), the driver emits
// StatusFailed with "malformed_verdict" in Result.Explanation and the raw
// model output truncated to the first 1 KiB (1024 bytes) — the bounded
// detail the verdict-aggregation activity records as failure context
// without flooding the chain with a long un-parseable blob.
//
// This is the inverse of the clean-JSON case above: it pins the failure
// shape FR-004 mandates so spec 094's DispatchMachineReviewer activity
// (activities/review/dispatch_machine_reviewer.go) sees a uniform
// Result.Status = StatusFailed signal rather than the 2026-05-24 dogfood
// pattern (StatusSucceeded carrying un-parseable prose, classified as
// FailureMalformedJSON downstream).
//
// Discriminator: SpecID="094" / TaskID="review" mirrors the T004 sibling
// test and the activity dispatcher — same routing rationale.
func TestInvoke_ReviewMode_ProseOnlyEmitsMalformedVerdict(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")
	// 2 KiB of single-byte prose with no braces: guarantees
	// extractVerdictJSON falls into FR-003 case (c) (raw passthrough +
	// errNoJSONFound), and is twice the FR-004 1 KiB cap so the
	// truncation rule is actually exercised. ASCII 'A' keeps byte == rune
	// so the slice comparison below is unambiguous.
	const rawBytes = 2048
	const truncCap = 1024
	prose := strings.Repeat("A", rawBytes)
	script := "#!/usr/bin/env bash\n" +
		"cat <<'PROSE'\n" + prose + "\nPROSE\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "wu-review-prose-001",
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
		t.Errorf("Explanation = %q, want substring %q", res.Explanation, "malformed_verdict")
	}
	// The truncated prefix must be present (operators need a window
	// into what the model actually said).
	if !strings.Contains(res.Explanation, prose[:truncCap]) {
		t.Errorf("Explanation = %q, want first %d bytes of raw prose to appear", res.Explanation, truncCap)
	}
	// And the full untruncated prose must NOT be present — confirms the
	// cap actually fired rather than the explanation silently growing.
	if strings.Contains(res.Explanation, prose) {
		t.Errorf("Explanation carries the full %d-byte prose; FR-004 caps the raw blob at %d bytes", rawBytes, truncCap)
	}
	// Result.Explanation must not parse as a StructuredVerdict — the
	// failure branch never re-serializes a verdict body.
	var sv verdict.StructuredVerdict
	if err := json.Unmarshal([]byte(res.Explanation), &sv); err == nil {
		t.Errorf("Explanation parsed as StructuredVerdict on the failure path: %+v", sv)
	}
}
