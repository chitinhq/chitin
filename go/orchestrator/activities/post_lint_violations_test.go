package activities

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBuildLintCommentBody_HasMarkerAndMessage asserts the dedup marker
// is the first line of the comment body and the (rule, severity, message)
// triple lands on the next line. The marker shape is the load-bearing
// contract — fetchExistingLintMarkers scans for it byte-for-byte.
func TestBuildLintCommentBody_HasMarkerAndMessage(t *testing.T) {
	got := buildLintCommentBody(LintViolation{
		Rule:     "L05",
		File:     "spec.md",
		Line:     42,
		Severity: "error",
		Message:  "gh api path must start with `repos/`",
	})

	// Marker is the dedup anchor — must match the regex exactly.
	if !lintMarkerRe.MatchString(got) {
		t.Errorf("body must carry a parseable lint marker; got:\n%s", got)
	}
	// Marker payload check — fields appear in the expected order.
	if !strings.Contains(got, "rule=L05") ||
		!strings.Contains(got, "file=spec.md") ||
		!strings.Contains(got, "line=42") {
		t.Errorf("marker missing one of (rule, file, line); got:\n%s", got)
	}
	// Operator-facing payload — severity + rule + message.
	if !strings.Contains(got, "spec-lint L05") ||
		!strings.Contains(got, "error") ||
		!strings.Contains(got, "repos/") {
		t.Errorf("body missing rule/severity/message; got:\n%s", got)
	}
}

// TestLintMarkerKey_RoundTrip asserts the dedup key produced when
// building a body matches the key derived from parsing that same body
// back via the regex. Round-trip stability is what makes idempotent
// re-runs possible.
func TestLintMarkerKey_RoundTrip(t *testing.T) {
	v := LintViolation{Rule: "L03", File: "tasks.md", Line: 7, Severity: "error", Message: "FR-099 referenced but not defined"}
	body := buildLintCommentBody(v)
	m := lintMarkerRe.FindStringSubmatch(body)
	if len(m) != 4 {
		t.Fatalf("marker did not parse; body:\n%s", body)
	}
	wantKey := lintMarkerKey("L03", "tasks.md", 7)
	gotKey := lintMarkerKey(m[1], m[2], 7) // line parsed elsewhere; we test the string form
	if wantKey != gotKey {
		t.Errorf("key mismatch: want %q got %q", wantKey, gotKey)
	}
	// And the parsed line digit must round-trip too.
	if m[3] != "7" {
		t.Errorf("parsed line digit %q != 7", m[3])
	}
}

// TestExecute_GuardsOnMissingRepo asserts the input guard fires without
// touching gh.
func TestExecute_GuardsOnMissingRepo(t *testing.T) {
	act := NewPostLintViolations()
	res, err := act.Execute(context.Background(), PostLintViolationsInput{
		PRNumber:   1,
		Violations: []LintViolation{{Rule: "L01", File: "spec.md", Line: 1, Severity: "error", Message: "x"}},
	})
	if err != nil {
		t.Fatalf("activity must be fail-soft; got err: %v", err)
	}
	if res.Posted != 0 {
		t.Errorf("nothing should post without Repo; got Posted=%d", res.Posted)
	}
	if !strings.Contains(res.Explanation, "missing Repo or PRNumber") {
		t.Errorf("explanation should name the missing input; got %q", res.Explanation)
	}
}

// TestExecute_AllWarnings_NoPost asserts that warning-severity violations
// are tallied but never posted (FR-004 — warnings are informational).
func TestExecute_AllWarnings_NoPost(t *testing.T) {
	act := NewPostLintViolations()
	res, err := act.Execute(context.Background(), PostLintViolationsInput{
		Repo:     "owner/repo",
		PRNumber: 10,
		Violations: []LintViolation{
			{Rule: "L02", File: "spec.md", Line: 1, Severity: "warning", Message: "soft"},
			{Rule: "L02", File: "spec.md", Line: 2, Severity: "warning", Message: "soft"},
		},
	})
	if err != nil {
		t.Fatalf("activity must be fail-soft; got err: %v", err)
	}
	if res.Posted != 0 {
		t.Errorf("warnings must not post; got Posted=%d", res.Posted)
	}
	if res.SkippedWarning != 2 {
		t.Errorf("expected SkippedWarning=2; got %d", res.SkippedWarning)
	}
	if !strings.Contains(res.Explanation, "no error-severity violations") {
		t.Errorf("explanation should name the all-warnings outcome; got %q", res.Explanation)
	}
}

// TestExecute_UnknownSeverity_TreatedAsWarning asserts safety: a severity
// the linter didn't declare doesn't gate iteration. Better to under-post
// than to flood a PR with comments tagged "unknown".
func TestExecute_UnknownSeverity_TreatedAsWarning(t *testing.T) {
	act := NewPostLintViolations()
	res, err := act.Execute(context.Background(), PostLintViolationsInput{
		Repo:     "owner/repo",
		PRNumber: 11,
		Violations: []LintViolation{
			{Rule: "L02", File: "spec.md", Line: 1, Severity: "fatal", Message: "x"},
		},
	})
	if err != nil {
		t.Fatalf("activity must be fail-soft; got err: %v", err)
	}
	if res.Posted != 0 {
		t.Errorf("unknown severity must not post; got Posted=%d", res.Posted)
	}
	if res.SkippedWarning != 1 {
		t.Errorf("unknown severity counted as warning; got SkippedWarning=%d", res.SkippedWarning)
	}
}

// TestExecute_DedupAgainstExistingMarker asserts that an error-severity
// violation whose (rule, file, line) key already appears in a chitin-
// authored comment on the PR is suppressed.
//
// Uses a stub `gh` binary on disk (script writes a canned comments JSON
// to stdout). The stub is wired in via CHITIN_GH_BIN so neither the real
// gh CLI nor the network is touched.
func TestExecute_DedupAgainstExistingMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-stub stub uses POSIX shell; Windows CI not exercised here")
	}

	dir := t.TempDir()
	// Stub returns one existing chitin comment matching (L01, spec.md, 1).
	existing := []map[string]any{{
		"id":   int64(1),
		"body": "<!-- chitin-spec-lint:rule=L01 file=spec.md line=1 -->\n**spec-lint L01 (error)** — already posted",
	}}
	existingJSON, _ := json.Marshal(existing)
	// Stub returns a review object on POST so postLintReview can decode
	// the id; the test asserts on Posted/SkippedDuplicate so the value
	// doesn't matter beyond being valid JSON with an id field.
	postedReview := map[string]any{"id": int64(9999)}
	postedJSON, _ := json.Marshal(postedReview)

	stub := writeGhStub(t, dir, string(existingJSON), string(postedJSON))
	t.Setenv("CHITIN_GH_BIN", stub)
	// Help the stub's `exec.LookPath` find the stub regardless of PATH.
	t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))

	act := NewPostLintViolations()
	res, err := act.Execute(context.Background(), PostLintViolationsInput{
		Repo:     "owner/repo",
		PRNumber: 42,
		Violations: []LintViolation{
			// Already exists — should dedup.
			{Rule: "L01", File: "spec.md", Line: 1, Severity: "error", Message: "frontmatter incomplete"},
			// New — should post.
			{Rule: "L03", File: "tasks.md", Line: 5, Severity: "error", Message: "FR-099 missing"},
		},
	})
	if err != nil {
		t.Fatalf("activity must be fail-soft; got err: %v", err)
	}
	if res.Posted != 1 {
		t.Errorf("expected exactly one new post; got Posted=%d (%s)", res.Posted, res.Explanation)
	}
	if res.SkippedDuplicate != 1 {
		t.Errorf("expected one dedup skip; got SkippedDuplicate=%d", res.SkippedDuplicate)
	}
	if res.ReviewID != 9999 {
		t.Errorf("expected ReviewID=9999 from stub response; got %d", res.ReviewID)
	}
}

// TestExecute_AllDuplicates_NoPost asserts that when every error
// violation matches an existing marker, no POST is attempted.
func TestExecute_AllDuplicates_NoPost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-stub uses POSIX shell")
	}

	dir := t.TempDir()
	existing := []map[string]any{{
		"id":   int64(1),
		"body": "<!-- chitin-spec-lint:rule=L01 file=spec.md line=1 -->\nbody",
	}}
	existingJSON, _ := json.Marshal(existing)
	stub := writeGhStub(t, dir, string(existingJSON), `{"id":7}`)
	t.Setenv("CHITIN_GH_BIN", stub)
	t.Setenv("PATH", filepath.Dir(stub)+string(os.PathListSeparator)+os.Getenv("PATH"))

	act := NewPostLintViolations()
	res, err := act.Execute(context.Background(), PostLintViolationsInput{
		Repo:     "owner/repo",
		PRNumber: 42,
		Violations: []LintViolation{
			{Rule: "L01", File: "spec.md", Line: 1, Severity: "error", Message: "dup"},
		},
	})
	if err != nil {
		t.Fatalf("activity must be fail-soft; got err: %v", err)
	}
	if res.Posted != 0 {
		t.Errorf("expected Posted=0 when every violation is a duplicate; got %d", res.Posted)
	}
	if res.SkippedDuplicate != 1 {
		t.Errorf("expected SkippedDuplicate=1; got %d", res.SkippedDuplicate)
	}
	if res.ReviewID != 0 {
		t.Errorf("expected no ReviewID (no POST attempted); got %d", res.ReviewID)
	}
}

// writeGhStub drops a POSIX-shell stub that mimics two `gh api` calls:
//
//   - LIST  (`gh api --paginate repos/.../pulls/N/comments?...`): write
//     listJSON to stdout.
//   - POST  (`gh api --method POST ... --input <file> repos/.../reviews`):
//     write postJSON to stdout.
//
// The stub discriminates on the presence of `--method` or `--paginate` in
// its argv. Returns the absolute stub path.
func writeGhStub(t *testing.T, dir, listJSON, postJSON string) string {
	t.Helper()
	stub := filepath.Join(dir, "gh")
	// printf '%s' (not echo) — dash's echo interprets \n inside JSON
	// string literals as a real newline, corrupting the payload.
	body := `#!/bin/sh
for a in "$@"; do
  case "$a" in
    --method) printf '%s' '` + postJSON + `'; exit 0;;
    --paginate) printf '%s' '` + listJSON + `'; exit 0;;
  esac
done
printf '%s' '[]'
exit 0
`
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return stub
}
