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

// TestInvoke_ReviewMode_MarkdownFencedJSONEmitsValidatedVerdict covers spec
// 109 FR-003 (a): when the claudecode CLI wraps its StructuredVerdict body
// in a ```json ... ``` markdown fence — the most common shape returned by
// chat-tuned models that have been instructed to "respond in JSON" — the
// driver's output post-processor (extractVerdictJSON) strips the fence and
// the driver still emits StatusSucceeded with a canonical, validate-able
// verdict in Result.Explanation.
//
// This test is the fenced-input twin of
// TestInvoke_ReviewMode_CleanJSONEmitsValidatedVerdict. Together they pin
// the contract that the verdict activity (verdict.ParseStructured) sees
// the same payload regardless of whether the model chose to fence its
// output.
func TestInvoke_ReviewMode_MarkdownFencedJSONEmitsValidatedVerdict(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")
	// request-changes — exercises a different FR-014 branch than the
	// clean-JSON test (non-empty blockers required). The body lands inside
	// a ```json ... ``` fence so the post-processor has work to do.
	fencedJSON := "```json\n" +
		`{"verdict":"request-changes","concerns":[],"recommendations":[],"blockers":["missing input validation on /api/users"]}` +
		"\n```"
	script := "#!/usr/bin/env bash\n" +
		"cat <<'JSON'\n" + fencedJSON + "\nJSON\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "wu-review-fenced-001",
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

	// The post-processor must strip the fence — any residual backticks in
	// the propagated payload would break the downstream JSON parse in
	// verdict.ParseStructured.
	if strings.Contains(res.Explanation, "```") {
		t.Errorf("Result.Explanation still contains markdown fence markers: %q", res.Explanation)
	}

	var got verdict.StructuredVerdict
	if err := json.Unmarshal([]byte(res.Explanation), &got); err != nil {
		t.Fatalf("Result.Explanation is not parseable as StructuredVerdict JSON: %v\nexplanation=%q", err, res.Explanation)
	}
	if err := verdict.Validate(got); err != nil {
		t.Fatalf("Result.Explanation failed verdict.Validate: %v\nexplanation=%q", err, res.Explanation)
	}
	if got.Verdict != verdict.RequestChanges {
		t.Errorf("verdict = %q, want %q", got.Verdict, verdict.RequestChanges)
	}
	if len(got.Blockers) != 1 || got.Blockers[0] == "" {
		t.Errorf("blockers = %v, want one non-empty entry for request-changes", got.Blockers)
	}
}
