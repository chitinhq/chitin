package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TestInvoke_ReviewMode_MalformedProseEmitsFailed covers spec 110 US3
// (parity with spec 109 US2): when the underlying codex CLI emits prose
// instead of a StructuredVerdict JSON document, the driver wrapper detects
// the parse failure at the driver boundary, returns StatusFailed with a
// "malformed_verdict" marker in Result.Explanation, and does NOT propagate
// the raw model output as a "successful" verdict body to the activity.
//
// Reproduces the 2026-05-24 dogfood failure mode: a reviewing driver
// returned prose, the activity classified the outcome as
// FailureMalformedJSON, and verdict aggregation halted. With T001–T003 in
// place for codex, prose stops at the driver — typed StatusFailed and a
// stable error marker the activity can route on (FR-005).
//
// Discriminator: SpecID="094" / TaskID="review" mirrors the sibling T006
// codex test and what the activity dispatcher already sets
// (activities/review/dispatch_machine_reviewer.go).
func TestInvoke_ReviewMode_MalformedProseEmitsFailed(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "codex")

	// Leading sentence is literal non-JSON so json.Unmarshal fails on the
	// first byte — guarantees the extract/parse path is exercised rather
	// than the exec-error path. No balanced {...} block anywhere in the
	// payload so extractVerdictJSON falls through to the no-JSON-found
	// branch (spec 109 FR-003 (c), inherited by 110 FR-004).
	prose := "This is prose, not JSON. The model misunderstood the review-mode contract and wrote a natural-language review of the diff instead of emitting a StructuredVerdict document."

	// Fake codex binary: emit the prose on stdout, exit 0 so the wrapper
	// reaches the extract/parse/validate path. Quoted heredoc prevents the
	// shell from expanding $ or `.
	script := "#!/usr/bin/env bash\n" +
		"cat <<'PROSE'\n" + prose + "\nPROSE\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
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
}
