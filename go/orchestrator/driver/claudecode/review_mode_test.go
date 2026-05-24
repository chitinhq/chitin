package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TestInvoke_ReviewMode_ProseOutputFailsWithTruncatedRaw covers spec 109 US2
// and FR-004: when the underlying claude CLI emits prose instead of a
// StructuredVerdict JSON document, the driver wrapper detects the parse
// failure at the driver boundary, returns StatusFailed with a
// "malformed_verdict" explanation, and caps the raw output snippet at
// 1 KiB so a runaway model can't blow up the activity record.
//
// Reproduces the 2026-05-24 dogfood failure mode: claudecode review-mode
// returned 3m40s of prose, the activity classified the outcome as
// FailureMalformedJSON, and verdict aggregation halted. With T001–T003 in
// place, prose stops at the driver — typed StatusFailed, capped snippet,
// no raw payload escaping into the activity history.
func TestInvoke_ReviewMode_ProseOutputFailsWithTruncatedRaw(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")

	// Construct a prose payload longer than 1 KiB so the truncation cap is
	// exercised, not merely declared. The leading sentence is a literal
	// non-JSON character so json.Unmarshal fails on the first byte; the
	// trailing 'x' run pads past the 1024-byte cap with content that must
	// NOT appear in the truncated snippet.
	proseHead := "This is prose, not JSON. The model misunderstood the review-mode contract and wrote a natural-language review instead of a StructuredVerdict document."
	proseTailMarker := "TAIL_BEYOND_1KIB_CAP"
	padding := strings.Repeat("x", 2000)
	fullProse := proseHead + padding + proseTailMarker
	if len(fullProse) <= 1024 {
		t.Fatalf("test prose is %d bytes, want >1024 to exercise the cap", len(fullProse))
	}

	// Fake claude binary: emit the prose on stdout, exit 0 so the wrapper
	// reaches the extract/parse/validate path rather than the exec-error
	// path. Use a quoted heredoc so the shell doesn't expand $ or `.
	script := "#!/usr/bin/env bash\n" +
		"cat <<'PROSE'\n" + fullProse + "\nPROSE\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "wu-review-prose-001",
		SpecID:       "094",
		TaskID:       "review",
		// Tool is the review-mode discriminator T003 adds to WorkUnit;
		// matching reviewToolName routes Invoke through invokeReview.
		Tool:         reviewToolName,
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

	// FR-004: the explanation carries the first 1 KiB of model output after
	// a "raw: " marker. Pull out the snippet and assert the cap exactly,
	// not by proxy — a future refactor that drops the cap should fail here.
	idx := strings.Index(res.Explanation, "raw: ")
	if idx < 0 {
		t.Fatalf("explanation missing 'raw: ' marker; got %q", res.Explanation)
	}
	rawSnippet := res.Explanation[idx+len("raw: "):]
	if len(rawSnippet) > 1024 {
		t.Errorf("raw snippet len = %d, want <= 1024 (first 1 KiB of model output)", len(rawSnippet))
	}

	// The snippet is the HEAD of the prose, not a random window — verifies
	// truncation kept the first 1 KiB rather than e.g. a center slice.
	if !strings.HasPrefix(rawSnippet, "This is prose") {
		head := rawSnippet
		if len(head) > 80 {
			head = head[:80]
		}
		t.Errorf("raw snippet does not start with the prose head; got %q", head)
	}

	// And the post-cap tail marker must be absent — proving content beyond
	// 1 KiB was dropped, not merely that some shorter snippet was emitted.
	if strings.Contains(rawSnippet, proseTailMarker) {
		t.Errorf("raw snippet contains tail marker %q past the 1 KiB cap", proseTailMarker)
	}
}
