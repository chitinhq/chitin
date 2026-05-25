// spec_pr_classify_test.go — spec 115 T001: unit tests for the
// spec-PR discriminator. Pure tests via the runSpecPRFiles seam;
// no `gh` shell-out, no network.

package main

import (
	"context"
	"errors"
	"testing"
)

// withFakeFiles swaps runSpecPRFiles for the duration of a single
// test and restores the production binding on cleanup. The fake
// returns the supplied raw bytes (or err) regardless of the
// requested repo/prNumber — those values are validated upstream by
// isSpecPR before the runner is even consulted.
func withFakeFiles(t *testing.T, raw []byte, err error) {
	t.Helper()
	prev := runSpecPRFiles
	runSpecPRFiles = func(ctx context.Context, repo string, prNumber int) ([]byte, error) {
		return raw, err
	}
	t.Cleanup(func() { runSpecPRFiles = prev })
}

func TestIsSpecPR_AllSpecFiles_ReturnsTrue(t *testing.T) {
	withFakeFiles(t, []byte(`[
		{"filename":".specify/specs/115-spec-review-gate/spec.md"},
		{"filename":".specify/specs/115-spec-review-gate/tasks.md"}
	]`), nil)

	if !isSpecPR(context.Background(), "user/repo", 1051) {
		t.Errorf("isSpecPR = false, want true for an all-spec PR")
	}
}

func TestIsSpecPR_MixedFiles_ReturnsFalse(t *testing.T) {
	// One code file alongside spec files is enough to drop us into
	// spec 113's loop per the FR-001 edge case.
	withFakeFiles(t, []byte(`[
		{"filename":".specify/specs/115-spec-review-gate/spec.md"},
		{"filename":"go/orchestrator/cmd/chitin-orchestrator/spec_pr_classify.go"}
	]`), nil)

	if isSpecPR(context.Background(), "user/repo", 1051) {
		t.Errorf("isSpecPR = true, want false for a mixed code+spec PR")
	}
}

func TestIsSpecPR_AllCodeFiles_ReturnsFalse(t *testing.T) {
	withFakeFiles(t, []byte(`[
		{"filename":"go/orchestrator/cmd/chitin-orchestrator/main.go"},
		{"filename":"go/orchestrator/cmd/chitin-orchestrator/main_test.go"}
	]`), nil)

	if isSpecPR(context.Background(), "user/repo", 1051) {
		t.Errorf("isSpecPR = true, want false for an all-code PR")
	}
}

func TestIsSpecPR_RootDotSpecifyFile_ReturnsFalse(t *testing.T) {
	// `.specify/judgement-phrases.txt` is operator-editable config —
	// it lives under .specify/ but NOT under .specify/specs/NNN-*/,
	// so it is not a spec-class change.
	withFakeFiles(t, []byte(`[
		{"filename":".specify/judgement-phrases.txt"}
	]`), nil)

	if isSpecPR(context.Background(), "user/repo", 1051) {
		t.Errorf("isSpecPR = true, want false for a .specify/ root config change")
	}
}

func TestIsSpecPR_BareSpecDir_ReturnsFalse(t *testing.T) {
	// The regex requires a path strictly INSIDE NNN-*/ — a bare
	// directory string never matches. GitHub never returns a bare
	// directory in pull-files, but the guard is cheap insurance.
	withFakeFiles(t, []byte(`[
		{"filename":".specify/specs/115-spec-review-gate"}
	]`), nil)

	if isSpecPR(context.Background(), "user/repo", 1051) {
		t.Errorf("isSpecPR = true, want false for a bare spec-dir entry")
	}
}

func TestIsSpecPR_NonNumericSpecPrefix_ReturnsFalse(t *testing.T) {
	// `.specify/specs/<name>/` without the numeric prefix is not a
	// canonical spec directory — the regex requires \d+- before the
	// rest of the slug.
	withFakeFiles(t, []byte(`[
		{"filename":".specify/specs/draft-something/spec.md"}
	]`), nil)

	if isSpecPR(context.Background(), "user/repo", 1051) {
		t.Errorf("isSpecPR = true, want false for a non-numeric spec slug")
	}
}

func TestIsSpecPR_GhError_ReturnsFalse(t *testing.T) {
	withFakeFiles(t, nil, errors.New("gh: auth required"))

	if isSpecPR(context.Background(), "user/repo", 1051) {
		t.Errorf("isSpecPR = true, want false on gh error (fail-closed-on-spec-class)")
	}
}

func TestIsSpecPR_InvalidJSON_ReturnsFalse(t *testing.T) {
	withFakeFiles(t, []byte(`not-json`), nil)

	if isSpecPR(context.Background(), "user/repo", 1051) {
		t.Errorf("isSpecPR = true, want false on parse error")
	}
}

func TestIsSpecPR_EmptyFileList_ReturnsFalse(t *testing.T) {
	// A PR with zero changed files is not classifiable as
	// spec-class. Vacuous-true would let a malformed response slip
	// through; explicit-false is safer.
	withFakeFiles(t, []byte(`[]`), nil)

	if isSpecPR(context.Background(), "user/repo", 1051) {
		t.Errorf("isSpecPR = true, want false on empty file list")
	}
}

func TestIsSpecPR_PaginatedConcatenatedArrays_ReturnsTrue(t *testing.T) {
	// `gh api --paginate` on naked-array endpoints may emit each
	// page as its own top-level array. The parser must walk every
	// page, not just the first.
	withFakeFiles(t, []byte(`[
		{"filename":".specify/specs/115-spec-review-gate/spec.md"}
	]
	[
		{"filename":".specify/specs/115-spec-review-gate/tasks.md"}
	]`), nil)

	if !isSpecPR(context.Background(), "user/repo", 1051) {
		t.Errorf("isSpecPR = false, want true when --paginate splits pages")
	}
}

func TestIsSpecPR_PaginatedMixed_ReturnsFalse(t *testing.T) {
	// Page 1 all-spec, page 2 has a code file. The classifier must
	// see the code file — this is exactly why --paginate is needed.
	withFakeFiles(t, []byte(`[
		{"filename":".specify/specs/115-spec-review-gate/spec.md"}
	]
	[
		{"filename":"go/orchestrator/cmd/chitin-orchestrator/main.go"}
	]`), nil)

	if isSpecPR(context.Background(), "user/repo", 1051) {
		t.Errorf("isSpecPR = true, want false when a later page introduces code files")
	}
}

func TestIsSpecPR_EmptyRepo_ReturnsFalse(t *testing.T) {
	// Defensive guard: caller bug. The runner is not invoked.
	called := false
	prev := runSpecPRFiles
	runSpecPRFiles = func(ctx context.Context, repo string, prNumber int) ([]byte, error) {
		called = true
		return nil, nil
	}
	defer func() { runSpecPRFiles = prev }()

	if isSpecPR(context.Background(), "", 1051) {
		t.Errorf("isSpecPR = true, want false on empty repo")
	}
	if called {
		t.Errorf("runner invoked on empty repo; should short-circuit")
	}
}

func TestIsSpecPR_NonPositivePR_ReturnsFalse(t *testing.T) {
	called := false
	prev := runSpecPRFiles
	runSpecPRFiles = func(ctx context.Context, repo string, prNumber int) ([]byte, error) {
		called = true
		return nil, nil
	}
	defer func() { runSpecPRFiles = prev }()

	if isSpecPR(context.Background(), "user/repo", 0) {
		t.Errorf("isSpecPR = true, want false on prNumber=0")
	}
	if called {
		t.Errorf("runner invoked on prNumber=0; should short-circuit")
	}
}
