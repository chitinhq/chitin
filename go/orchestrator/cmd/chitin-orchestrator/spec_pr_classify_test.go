// spec_pr_classify_test.go — spec 115 US1 T001 tests for the spec-PR
// discriminator. Pure-function tests on allPathsUnderSpecifySpecs plus stub-
// injected tests on isSpecPR; no real gh-api shelling.

package main

import (
	"context"
	"errors"
	"testing"
)

func TestAllPathsUnderSpecifySpecs(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  bool
	}{
		{"empty changeset", nil, false},
		{"single spec.md", []string{".specify/specs/115-spec-review-gate/spec.md"}, true},
		{"single tasks.md", []string{".specify/specs/114-operator-escalation/tasks.md"}, true},
		{"nested under spec dir", []string{".specify/specs/115-foo/contracts/x.md"}, true},
		{"two specs, two dirs", []string{".specify/specs/115-x/spec.md", ".specify/specs/114-y/tasks.md"}, true},
		{"mixed spec + code", []string{".specify/specs/115-x/spec.md", "go/orchestrator/main.go"}, false},
		{"mixed spec + readme", []string{".specify/specs/115-x/spec.md", "README.md"}, false},
		{"code only", []string{"go/orchestrator/main.go"}, false},
		{"docs only", []string{"docs/runbooks/spec-115.md"}, false},
		{"non-numeric spec id", []string{".specify/specs/abc-foo/spec.md"}, false},
		{"missing dash", []string{".specify/specs/115/spec.md"}, false},
		{"file directly under specs", []string{".specify/specs/somefile.md"}, false},
		{"no trailing slash after id", []string{".specify/specs/115-foo"}, false},
		{"leading slash", []string{"/.specify/specs/115-foo/spec.md"}, false},
		{"path prefix only (suffix not match)", []string{"docs/.specify/specs/115-foo/spec.md"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := allPathsUnderSpecifySpecs(tc.files); got != tc.want {
				t.Errorf("allPathsUnderSpecifySpecs(%v) = %v; want %v", tc.files, got, tc.want)
			}
		})
	}
}

func TestIsSpecPR_AllSpecPathsClassifiesTrue(t *testing.T) {
	gotRepo, gotPR := "", 0
	stub := func(ctx context.Context, repo string, prNumber int) ([]string, error) {
		gotRepo, gotPR = repo, prNumber
		return []string{
			".specify/specs/115-spec-review-gate/spec.md",
			".specify/specs/115-spec-review-gate/tasks.md",
		}, nil
	}
	ok, err := isSpecPR(context.Background(), "chitinhq/chitin", 1050, stub)
	if err != nil {
		t.Fatalf("isSpecPR error: %v", err)
	}
	if !ok {
		t.Errorf("isSpecPR = false; want true for all-spec changeset")
	}
	if gotRepo != "chitinhq/chitin" || gotPR != 1050 {
		t.Errorf("lister called with repo=%q pr=%d; want chitinhq/chitin 1050", gotRepo, gotPR)
	}
}

func TestIsSpecPR_MixedClassClassifiesFalse(t *testing.T) {
	// Per spec 115 edge case: a PR touching both spec and code routes to the
	// code-iteration loop (spec 113), so isSpecPR must return false.
	stub := func(ctx context.Context, repo string, prNumber int) ([]string, error) {
		return []string{
			".specify/specs/115-spec-review-gate/spec.md",
			"go/orchestrator/main.go",
		}, nil
	}
	ok, err := isSpecPR(context.Background(), "chitinhq/chitin", 9, stub)
	if err != nil {
		t.Fatalf("isSpecPR error: %v", err)
	}
	if ok {
		t.Errorf("isSpecPR = true for mixed-class PR; want false")
	}
}

func TestIsSpecPR_EmptyChangesetClassifiesFalse(t *testing.T) {
	stub := func(ctx context.Context, repo string, prNumber int) ([]string, error) {
		return nil, nil
	}
	ok, err := isSpecPR(context.Background(), "chitinhq/chitin", 1, stub)
	if err != nil {
		t.Fatalf("isSpecPR error: %v", err)
	}
	if ok {
		t.Errorf("isSpecPR = true for empty changeset; want false")
	}
}

func TestIsSpecPR_ListerErrorPropagates(t *testing.T) {
	wantErr := errors.New("network unreachable")
	stub := func(ctx context.Context, repo string, prNumber int) ([]string, error) {
		return nil, wantErr
	}
	ok, err := isSpecPR(context.Background(), "chitinhq/chitin", 1, stub)
	if err == nil {
		t.Fatal("isSpecPR error = nil; want non-nil from lister failure")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("isSpecPR error = %v; want wrapping %v", err, wantErr)
	}
	if ok {
		t.Errorf("isSpecPR = true on lister error; want false (safe-default to code path)")
	}
}

func TestIsSpecPR_NilListerSelectsDefault(t *testing.T) {
	// The default lister shells out to `gh`; we don't want to actually run it
	// in a unit test, but we can assert the path: a nil lister selects
	// listPRFilesViaGH, which is what allows the production wiring to call
	// isSpecPR with `nil` and Just Work. We verify by checking that nil does
	// not panic on the dispatch decision — the gh exec will then fail in a
	// hermetic environment, which is fine: we only assert the function does
	// not nil-deref. Force PATH empty so `gh` cannot resolve regardless of
	// the host (CI images often ship gh pre-installed); t.Setenv auto-restores
	// after the test.
	t.Setenv("PATH", "")
	_, err := isSpecPR(context.Background(), "chitinhq/chitin", 1, nil)
	if err == nil {
		t.Errorf("isSpecPR with nil lister + no gh on PATH should error; got nil")
	}
}
