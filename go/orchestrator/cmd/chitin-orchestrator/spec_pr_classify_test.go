// spec_pr_classify_test.go — spec 115 T001 tests for the spec-PR
// discriminator helper. Pure-function tests over a fake
// specFilesLister; the gh-shell path is exercised via the
// fake-gh-on-PATH pattern (matches copilot_dispatch_test.go).

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeSpecFilesLister struct {
	files []string
	err   error
}

func (f fakeSpecFilesLister) listPRFiles(_ context.Context, _ int) ([]string, error) {
	return f.files, f.err
}

func TestIsSpecPRWith_AllFilesUnderSpecDir_True(t *testing.T) {
	lister := fakeSpecFilesLister{files: []string{
		".specify/specs/115-spec-review-gate/spec.md",
		".specify/specs/115-spec-review-gate/tasks.md",
		".specify/specs/115-spec-review-gate/contracts/iteration.md",
	}}
	if !isSpecPRWith(context.Background(), 1057, lister) {
		t.Fatalf("isSpecPRWith = false, want true: all files under .specify/specs/<N>-*/")
	}
}

func TestIsSpecPRWith_MixedSpecAndCode_False(t *testing.T) {
	// FR-001 edge case: a PR that ALSO modifies code falls out of the
	// spec-class branch and runs through spec 113's code-PR loop.
	lister := fakeSpecFilesLister{files: []string{
		".specify/specs/115-spec-review-gate/spec.md",
		"go/orchestrator/cmd/chitin-orchestrator/factory_listen.go",
	}}
	if isSpecPRWith(context.Background(), 1057, lister) {
		t.Fatalf("isSpecPRWith = true, want false: mixed spec+code PR is not spec-class")
	}
}

func TestIsSpecPRWith_AllFilesOutsideSpecDir_False(t *testing.T) {
	lister := fakeSpecFilesLister{files: []string{
		"README.md",
		"go/orchestrator/main.go",
	}}
	if isSpecPRWith(context.Background(), 1057, lister) {
		t.Fatalf("isSpecPRWith = true, want false: no spec files")
	}
}

func TestIsSpecPRWith_SpecifyButNotSpecsDir_False(t *testing.T) {
	// Files under .specify/ that are NOT in a spec dir (allowlists,
	// templates, judgement phrases) should NOT classify the PR as a
	// spec-class PR — only spec.md/tasks.md changes qualify per FR-001.
	lister := fakeSpecFilesLister{files: []string{
		".specify/known-cli-surfaces.txt",
		".specify/judgement-phrases.txt",
	}}
	if isSpecPRWith(context.Background(), 1057, lister) {
		t.Fatalf("isSpecPRWith = true, want false: .specify/ allowlist files are not spec content")
	}
}

func TestIsSpecPRWith_SpecDirWithoutNumericPrefix_False(t *testing.T) {
	// `.specify/specs/foo/...` (no NNN- prefix) is not a real spec dir;
	// the regex requires \d+- per FR-001.
	lister := fakeSpecFilesLister{files: []string{
		".specify/specs/foo/spec.md",
	}}
	if isSpecPRWith(context.Background(), 1057, lister) {
		t.Fatalf("isSpecPRWith = true, want false: non-numeric spec dir")
	}
}

func TestIsSpecPRWith_EmptyChangeset_False(t *testing.T) {
	// A zero-file changeset can't be "wholly contained" in spec dirs —
	// it isn't contained anywhere. Treat as not-spec-class so we
	// fail-safe to the code-PR path.
	lister := fakeSpecFilesLister{files: nil}
	if isSpecPRWith(context.Background(), 1057, lister) {
		t.Fatalf("isSpecPRWith = true, want false: empty changeset is not spec-class")
	}
}

func TestIsSpecPRWith_GhError_False(t *testing.T) {
	// When the gh-api call fails we can't classify, so default to
	// not-spec-class — the code-PR loop is the existing behavior and a
	// safer fallback than silently dropping a real code review.
	lister := fakeSpecFilesLister{err: errors.New("gh api: 404")}
	if isSpecPRWith(context.Background(), 1057, lister) {
		t.Fatalf("isSpecPRWith = true, want false on gh error")
	}
}

func TestIsSpecPRWith_MultipleSpecDirsInOnePR_True(t *testing.T) {
	// A PR touching two different specs is still spec-class — the
	// discriminator gates on "wholly under any spec dir," not "single
	// spec dir."
	lister := fakeSpecFilesLister{files: []string{
		".specify/specs/115-spec-review-gate/spec.md",
		".specify/specs/116-multi-driver/spec.md",
	}}
	if !isSpecPRWith(context.Background(), 1057, lister) {
		t.Fatalf("isSpecPRWith = false, want true: multi-spec PR is still spec-class")
	}
}

// TestDefaultSpecFilesLister_ParsesGhAPIOutput exercises the default
// lister against a fake `gh` binary on PATH. Matches a real
// `gh api repos/.../pulls/N/files` response shape.
func TestDefaultSpecFilesLister_ParsesGhAPIOutput(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "gh")
	// Real gh api output is a JSON array of file objects. The lister
	// reads .filename off each.
	body := `[{"filename":".specify/specs/115-spec-review-gate/spec.md","status":"added"},` +
		`{"filename":".specify/specs/115-spec-review-gate/tasks.md","status":"modified"}]`
	script := "#!/usr/bin/env bash\ncat <<'EOF'\n" + body + "\nEOF\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	files, err := defaultSpecFilesLister{}.listPRFiles(context.Background(), 1057)
	if err != nil {
		t.Fatalf("listPRFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	if files[0] != ".specify/specs/115-spec-review-gate/spec.md" {
		t.Errorf("files[0] = %q, want spec.md path", files[0])
	}
	if files[1] != ".specify/specs/115-spec-review-gate/tasks.md" {
		t.Errorf("files[1] = %q, want tasks.md path", files[1])
	}
}

func TestDefaultSpecFilesLister_GhExitError(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "gh")
	script := "#!/usr/bin/env bash\necho 'gh: 404 Not Found' >&2\nexit 1\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := defaultSpecFilesLister{}.listPRFiles(context.Background(), 1057)
	if err == nil {
		t.Fatalf("listPRFiles returned nil error, want non-nil on gh exit 1")
	}
}
