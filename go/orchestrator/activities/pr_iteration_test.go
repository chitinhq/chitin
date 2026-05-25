package activities

import (
	"context"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/worktree"
)

// TestBuildIterationPrompt_ShapeAndContent asserts the prompt template
// includes: PR number, round, review body when present, every line
// comment with file+line+body, and the post-action exit instruction.
// Pure-function test; no driver / no IO.
func TestBuildIterationPrompt_ShapeAndContent(t *testing.T) {
	in := IteratePRReviewInput{
		PRNumber: 1234,
		Round:    2,
	}
	rc := reviewContext{
		Body: "Looks mostly good but a couple of things to address.",
		LineComments: []reviewLineComment{
			{ID: 1, Path: "foo.go", Line: 42, Body: "Consider extracting this into a helper."},
			{ID: 2, Path: "bar/baz.go", Line: 17, Body: "Typo: \"recieve\"."},
		},
	}

	got := BuildIterationPrompt(in, rc)

	// PR header
	if !strings.Contains(got, "PR #1234") {
		t.Errorf("prompt missing PR number; got:\n%s", got)
	}
	if !strings.Contains(got, "round 2") {
		t.Errorf("prompt missing round number; got:\n%s", got)
	}
	// Review body
	if !strings.Contains(got, "REVIEW BODY:") || !strings.Contains(got, "couple of things") {
		t.Errorf("prompt missing review body; got:\n%s", got)
	}
	// Every line comment surfaces with file+line+body
	for _, c := range rc.LineComments {
		fileLine := c.Path + ":"
		if !strings.Contains(got, fileLine) {
			t.Errorf("prompt missing file marker %q; got:\n%s", fileLine, got)
		}
		if !strings.Contains(got, c.Body) {
			t.Errorf("prompt missing comment body %q; got:\n%s", c.Body, got)
		}
	}
	// Closing instruction — driver must exit, not run tests
	if !strings.Contains(got, "Do not run tests") {
		t.Errorf("prompt missing exit instruction; got:\n%s", got)
	}
}

// TestBuildIterationPrompt_OmitsEmptyBody asserts the review-body section
// is omitted entirely when the body is empty (Copilot sometimes leaves a
// review with only line comments and no top-level body).
func TestBuildIterationPrompt_OmitsEmptyBody(t *testing.T) {
	in := IteratePRReviewInput{PRNumber: 1, Round: 1}
	rc := reviewContext{
		Body: "  \n\t  ", // whitespace only
		LineComments: []reviewLineComment{
			{ID: 1, Path: "x.go", Line: 1, Body: "fix this"},
		},
	}
	got := BuildIterationPrompt(in, rc)
	if strings.Contains(got, "REVIEW BODY:") {
		t.Errorf("prompt should omit REVIEW BODY section on empty body; got:\n%s", got)
	}
	if !strings.Contains(got, "fix this") {
		t.Errorf("prompt should still include line comments; got:\n%s", got)
	}
}

// TestBuildIterationPrompt_OmitsEmptyComments asserts the line-comments
// section is omitted when no line comments are present (some Copilot
// reviews are just a summary with no inline comments).
func TestBuildIterationPrompt_OmitsEmptyComments(t *testing.T) {
	in := IteratePRReviewInput{PRNumber: 1, Round: 1}
	rc := reviewContext{Body: "Approving with a note: please add tests next time."}
	got := BuildIterationPrompt(in, rc)
	if strings.Contains(got, "LINE COMMENTS:") {
		t.Errorf("prompt should omit LINE COMMENTS section when empty; got:\n%s", got)
	}
	if !strings.Contains(got, "please add tests") {
		t.Errorf("prompt should still include review body; got:\n%s", got)
	}
}

// TestIteratePRReview_NoManagerOrRegistry asserts the guard: a nil
// Manager or Registry returns a populated result with a clear
// explanation rather than panicking.
func TestIteratePRReview_NoManagerOrRegistry(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "1")
	act := NewIteratePRReview(nil, nil)
	res, err := act.Execute(context.Background(), IteratePRReviewInput{
		PRNumber: 1, PRBranch: "x", TargetRepo: "y", Repo: "z",
	})
	if err != nil {
		t.Fatalf("Execute must be fail-soft, returned err: %v", err)
	}
	if res.PushedFixup {
		t.Error("expected PushedFixup=false with nil Manager/Registry")
	}
	if !strings.Contains(res.Explanation, "no Manager or Registry bound") {
		t.Errorf("explanation should name the missing deps, got %q", res.Explanation)
	}
}

// TestIteratePRReview_MissingInputs asserts the input guard fires after
// the Manager/Registry-bound check. Uses real (empty) constructions of
// both so the previous guard doesn't short-circuit.
func TestIteratePRReview_MissingInputs(t *testing.T) {
	t.Setenv("CHITIN_DISABLE_CHAIN_EMIT", "1")
	mgr, err := worktree.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	reg := driver.NewRegistry()
	act := NewIteratePRReview(mgr, reg)
	res, err := act.Execute(context.Background(), IteratePRReviewInput{
		PRNumber: 1,
		PRBranch: "", // missing
	})
	if err != nil {
		t.Fatalf("Execute must be fail-soft, returned err: %v", err)
	}
	if !strings.Contains(res.Explanation, "missing PRBranch") {
		t.Errorf("explanation should name the missing inputs, got %q", res.Explanation)
	}
}
