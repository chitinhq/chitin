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

// TestInvoke_ReviewMode_OmittedListFieldsBecomeEmptyArrays addresses the
// Copilot review comment on PR #1041: when the model omits an empty list
// field (e.g. an Approve verdict with no concerns), json.Unmarshal leaves
// the slice nil; without normalization, json.Marshal then emits `null` for
// that field — which conflicts with the StructuredVerdict schema (arrays)
// and surprises downstream consumers. The fix in reviewResult normalizes
// nil slices to empty before re-marshaling; this test guards the
// invariant by feeding a verdict that omits two of the three list fields
// and asserting all three round-trip as `[]`, never `null`.
func TestInvoke_ReviewMode_OmittedListFieldsBecomeEmptyArrays(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")
	// Approve verdict with NO concerns / recommendations / blockers — the
	// model omits all three list fields entirely. Approve is the only enum
	// that passes Validate with all three empty (FR-014).
	cleanJSON := `{"verdict":"approve"}`
	script := "#!/usr/bin/env bash\n" +
		"cat <<'JSON'\n" + cleanJSON + "\nJSON\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "wu-review-omit-001",
		SpecID:       "094",
		TaskID:       "review",
		WorktreePath: dir,
		Context:      `{"pr":{"repo":"chitinhq/chitin","number":1}}`,
	}
	res, err := d.Invoke(context.Background(), wu)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res.Status != driver.StatusSucceeded {
		t.Fatalf("status = %s, want StatusSucceeded; explanation=%q", res.Status, res.Explanation)
	}

	// The canonical re-serialized body must carry `[]` for every list
	// field. A bare `"concerns": null` (or any of the three) means the
	// normalization step regressed.
	for _, field := range []string{"concerns", "recommendations", "blockers"} {
		if !strings.Contains(res.Explanation, `"`+field+`":[]`) {
			t.Errorf("expected %q to appear as empty array in canonical JSON, got: %s",
				field, res.Explanation)
		}
		if strings.Contains(res.Explanation, `"`+field+`":null`) {
			t.Errorf("field %q canonicalized as null (should be []): %s",
				field, res.Explanation)
		}
	}

	// Sanity: still parseable + validates as Approve.
	var got verdict.StructuredVerdict
	if err := json.Unmarshal([]byte(res.Explanation), &got); err != nil {
		t.Fatalf("parse canonical: %v", err)
	}
	if got.Verdict != verdict.Approve {
		t.Errorf("verdict = %q, want %q", got.Verdict, verdict.Approve)
	}
}

// TestInvoke_ReviewMode_MarkdownFencedJSONEmitsValidatedVerdict covers spec
// 109 T005 / FR-003 (a): claudecode commonly wraps its structured output in
// a ```json … ``` markdown fence even when the prompt forbids it. The
// review-mode post-processor MUST strip that fence, recover the JSON body,
// validate it, and emit StatusSucceeded with the canonically re-serialized
// verdict in Result.Explanation — identical to the clean-JSON path.
//
// Without this, every fenced response (a frequent claudecode shape) would
// trip the brace scanner's prose-only fallback and surface as
// malformed_verdict, even though the model produced a valid verdict.
func TestInvoke_ReviewMode_MarkdownFencedJSONEmitsValidatedVerdict(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")
	// Same approve-with-comments body as the clean-JSON test, but wrapped
	// in a ```json fence the way claudecode tends to emit it.
	cleanJSON := `{"verdict":"approve-with-comments","concerns":["nit: name shadowing on line 42"],"recommendations":["extract a helper for the duplicated branch"],"blockers":[]}`
	fenced := "```json\n" + cleanJSON + "\n```"
	script := "#!/usr/bin/env bash\n" +
		"cat <<'FENCED'\n" + fenced + "\nFENCED\n" +
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

	// The canonical re-serialized body must NOT carry the fence markers.
	// A regression that forgot to strip the fence would leave backticks
	// in res.Explanation (and json.Unmarshal would fail below).
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

// TestInvoke_ReviewMode_ProseOnlyEmitsMalformedVerdict covers spec 109 T006:
// when the claudecode review-mode invocation returns plain prose with no
// JSON-shaped substring, extractVerdictJSON's fallback branch (FR-003 (c))
// fires, and the driver MUST surface the failure as
// Result{Status: StatusFailed, Explanation: "malformed_verdict: ..."} with
// the raw model output truncated to 1 KiB so a runaway response cannot
// flood the verdict activity's event payload or the Temporal history.
//
// The probe is shaped so that truncation behavior is directly observable:
// a head marker sits at the start of the prose and survives any sensible
// truncation; a tail marker is placed past byte 1024 and MUST be dropped
// if truncation is correct. A test that asserts only "head marker present"
// would silently pass an implementation that forgot to truncate at all,
// so we assert both halves of the contract.
func TestInvoke_ReviewMode_ProseOnlyEmitsMalformedVerdict(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")

	const (
		headMarker = "REVIEW_PROSE_HEAD"
		tailMarker = "REVIEW_PROSE_TAIL_BEYOND_1KIB"
	)
	// No `{` / `}` so the brace scanner finds nothing and the driver
	// reaches the FR-003 (c) fallback.
	prose := headMarker + " " + strings.Repeat("a", 1500) + " " + tailMarker + " " + strings.Repeat("z", 200)
	if len(prose) <= 1024 {
		t.Fatalf("test setup: prose len %d <= 1 KiB, can't verify truncation", len(prose))
	}
	if off := strings.Index(prose, tailMarker); off < 1024 {
		t.Fatalf("test setup: tail marker at byte %d, must be >=1024 to prove truncation", off)
	}

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
		t.Errorf("explanation missing 'malformed_verdict' marker; got %q", res.Explanation)
	}
	if !strings.Contains(res.Explanation, headMarker) {
		t.Errorf("explanation missing head marker %q; truncation may have dropped the prose entirely; got %q",
			headMarker, res.Explanation)
	}
	if strings.Contains(res.Explanation, tailMarker) {
		t.Errorf("explanation contains tail marker %q which sits beyond 1 KiB; truncation did not fire; got %q",
			tailMarker, res.Explanation)
	}
}
