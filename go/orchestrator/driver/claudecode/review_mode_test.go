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

// TestInvoke_ReviewMode_VerdictValidationFailureSurfacesError covers spec 109
// FR-006 (and the US2 verdict-shape-failure path): when the claudecode
// review-mode invocation returns syntactically valid JSON whose body
// violates one of the spec 094 FR-014 per-enum invariants — here, the
// classic "verdict=approve with non-empty blockers" contradiction — the
// driver must NOT propagate the malformed body to the verdict activity.
// Instead it routes the outcome to StatusFailed and surfaces the validator's
// invariant name + detail in Result.Explanation so the workflow's failure
// classification (FailureMalformedVerdict, distinct from FailureMalformedJSON)
// has the post-mortem detail attached without a separate audit fetch.
//
// The body below trips the "approve_blockers_must_be_empty" invariant from
// activities/review/verdict/invariants.go — it is parseable as
// StructuredVerdict but Validate rejects it because approve mandates an
// empty blockers list (FR-014.1).
//
// Pairs with TestInvoke_ReviewMode_CleanJSONEmitsValidatedVerdict (T004,
// the success path) and the T006 prose-only test (FailureMalformedJSON,
// distinct error class): together they pin the three FR-005/FR-006 outcomes
// the dispatch_machine_reviewer activity classifies on (succeeded / malformed
// JSON / invariant-violated verdict body).
func TestInvoke_ReviewMode_VerdictValidationFailureSurfacesError(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")
	// verdict=approve + non-empty blockers violates FR-014.1 ("approve ⇒
	// blockers empty"). Validate returns a *ValidationError whose Invariant
	// is "approve_blockers_must_be_empty" and Detail names the count.
	invalidJSON := `{"verdict":"approve","concerns":[],"recommendations":[],"blockers":["secret committed in config.yaml"]}`
	script := "#!/usr/bin/env bash\n" +
		"cat <<'JSON'\n" + invalidJSON + "\nJSON\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "wu-review-invalid-001",
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
	// T003 specifies the explanation prefix "malformed_verdict: ..." so the
	// activity dispatcher can match on a stable token without parsing the
	// validator's invariant name format.
	if !strings.Contains(res.Explanation, "malformed_verdict") {
		t.Errorf("explanation missing %q prefix: %q", "malformed_verdict", res.Explanation)
	}
	// The validator's invariant name is the load-bearing post-mortem signal —
	// it tells an operator exactly which FR-014 rule the reviewer violated.
	// Either the invariant token or its human-readable detail is acceptable;
	// requiring both would over-constrain the explanation format.
	hasInvariant := strings.Contains(res.Explanation, "approve_blockers_must_be_empty")
	hasDetail := strings.Contains(res.Explanation, "approve verdict must have empty blockers")
	if !hasInvariant && !hasDetail {
		t.Errorf("explanation missing validator error (invariant %q or detail %q): %q",
			"approve_blockers_must_be_empty",
			"approve verdict must have empty blockers",
			res.Explanation)
	}
	// Sanity check the contradiction is the actual violation: Validate over
	// the same body should reject for the same reason. If this changes (e.g.
	// the verdict package relaxes the invariant) the test author should pick
	// a different violation rather than weaken the assertion above.
	var parsed verdict.StructuredVerdict
	if err := json.Unmarshal([]byte(invalidJSON), &parsed); err != nil {
		t.Fatalf("test fixture is not parseable JSON: %v", err)
	}
	if err := verdict.Validate(parsed); err == nil {
		t.Fatalf("test fixture unexpectedly passed verdict.Validate; pick a different invariant violation")
	}
}
