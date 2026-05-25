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

// TestInvoke_ReviewMode_ProseOnlyEmitsMalformedVerdict covers spec 109 FR-003
// fallback (c) end-to-end: when the claudecode review-mode invocation returns
// only prose — no JSON-shaped substring — the driver must NOT propagate that
// prose to the verdict activity as a "successful" review. Instead it emits
// StatusFailed with "malformed_verdict" in Result.Explanation, and truncates
// the raw stdout it surfaces to at most 1 KiB so a runaway model response can
// never blow up the activity's audit log (FR-004).
//
// This is the exact 2026-05-24 dogfood failure mode: claudecode replied
// "The PR looks fine to me, ship it" with no JSON at all, and the activity
// silently classified it as a malformed verdict — but only because spec 094's
// ParseStructured rejected the prose. With T003's review-mode dispatch in
// place, that classification happens in the driver, with the raw output
// preserved (bounded) for operator triage.
func TestInvoke_ReviewMode_ProseOnlyEmitsMalformedVerdict(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")
	// Generate >1 KiB of prose with NO `{` or `}` characters so the brace
	// scanner returns errNoJSONFound. The leading marker lets us assert the
	// preserved-but-truncated prefix lands in the explanation, while the
	// trailing marker lets us assert the tail was dropped by the 1 KiB cap.
	const headMarker = "PROSE-HEAD-MARKER:"
	const tailMarker = ":PROSE-TAIL-MARKER"
	filler := strings.Repeat("the pr looks fine to me ship it. ", 64) // ~2 KiB
	proseOutput := headMarker + " " + filler + tailMarker
	if len(proseOutput) <= 1024 {
		t.Fatalf("test bug: prose fixture is %d bytes, need >1024 to exercise truncation", len(proseOutput))
	}
	script := "#!/usr/bin/env bash\n" +
		"cat <<'PROSE'\n" + proseOutput + "\nPROSE\n" +
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
		t.Errorf("explanation missing %q sentinel: %q", "malformed_verdict", res.Explanation)
	}
	if !strings.Contains(res.Explanation, headMarker) {
		t.Errorf("explanation should preserve the leading raw-output bytes for triage; head marker %q not found in %q", headMarker, res.Explanation)
	}
	if strings.Contains(res.Explanation, tailMarker) {
		t.Errorf("explanation contains tail marker %q — raw output was not truncated to 1 KiB", tailMarker)
	}
	// FR-004 cap: the surfaced raw output must not exceed 1 KiB. The
	// explanation may also include a fixed prefix/suffix from the driver
	// (e.g. "malformed_verdict: ..."), so we allow modest overhead but
	// reject anything that suggests the full prose round-tripped.
	const maxRawBytes = 1024
	const overheadAllowance = 256
	if len(res.Explanation) > maxRawBytes+overheadAllowance {
		t.Errorf("explanation is %d bytes, want <= %d (1 KiB raw + driver framing)", len(res.Explanation), maxRawBytes+overheadAllowance)
	}
}
