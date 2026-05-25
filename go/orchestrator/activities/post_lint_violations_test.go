package activities

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderLintCommentBody_MarkerShape asserts the rendered body
// carries the dedup marker prefix + every field (rule/file/line) the
// parser keys on, and that the user-visible content surfaces below
// the HTML comment.
func TestRenderLintCommentBody_MarkerShape(t *testing.T) {
	v := LintViolation{
		Rule:     "L05",
		File:     ".specify/specs/115-spec-review-gate/spec.md",
		Line:    42,
		Severity: "error",
		Message:  "invented CLI surface 'chitin-kernel events'",
	}
	body := renderLintCommentBody(v)

	for _, want := range []string{
		lintCommentMarkerPrefix,
		"rule=L05",
		"file=.specify/specs/115-spec-review-gate/spec.md",
		"line=42",
		"-->",
		"**spec-lint L05**",
		"invented CLI surface",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered body missing %q\n---\n%s", want, body)
		}
	}
}

// TestParseMarker_RoundTrip asserts parseMarker recovers the same
// dedup key that markerKey produces for a freshly-rendered body. This
// is the round-trip invariant that keeps dedup stable across re-runs.
func TestParseMarker_RoundTrip(t *testing.T) {
	v := LintViolation{
		Rule: "L03", File: "spec.md", Line: 7,
		Severity: "error", Message: "FR-001 has no task",
	}
	body := renderLintCommentBody(v)
	got, ok := parseMarker(body)
	if !ok {
		t.Fatalf("parseMarker rejected freshly-rendered body:\n%s", body)
	}
	want := markerKey("L03", "spec.md", 7)
	if got != want {
		t.Errorf("round-trip key drift: got %q, want %q", got, want)
	}
}

// TestParseMarker_RejectsForeign asserts parseMarker returns ok=false
// on human / Copilot / malformed comments so they never collide with
// the dedup set.
func TestParseMarker_RejectsForeign(t *testing.T) {
	cases := map[string]string{
		"plain human":   "Consider extracting this into a helper.",
		"copilot-ish":   "**Issue**: the regex is too permissive.",
		"truncated":     lintCommentMarkerPrefix + " rule=L01 file=x",
		"missing rule":  lintCommentMarkerPrefix + " file=spec.md line=1 -->",
		"missing file":  lintCommentMarkerPrefix + " rule=L01 line=1 -->",
		"missing line":  lintCommentMarkerPrefix + " rule=L01 file=spec.md -->",
		"non-digit line": lintCommentMarkerPrefix + " rule=L01 file=spec.md line=abc -->",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := parseMarker(body); ok {
				t.Errorf("parseMarker(%q) returned ok=true; want false", body)
			}
		})
	}
}

// TestPostLintViolations_GuardsMissingInputs asserts the early guards
// return a populated Result + nil error rather than panicking or
// running a network call.
func TestPostLintViolations_GuardsMissingInputs(t *testing.T) {
	t.Setenv("PATH", "") // belt-and-suspenders: no `gh` lookup possible
	act := NewPostLintViolations()

	t.Run("no pr number", func(t *testing.T) {
		res, err := act.Execute(t.Context(), PostLintViolationsInput{
			Repo: "owner/repo",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Explanation, "missing PRNumber") {
			t.Errorf("explanation missing PRNumber hint: %q", res.Explanation)
		}
		if res.Posted != 0 || res.Skipped != 0 {
			t.Errorf("expected zero counts on guard failure: %+v", res)
		}
	})

	t.Run("no repo", func(t *testing.T) {
		res, err := act.Execute(t.Context(), PostLintViolationsInput{
			PRNumber: 1,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Explanation, "Repo") {
			t.Errorf("explanation missing Repo hint: %q", res.Explanation)
		}
	})
}

// TestPostLintViolations_NoErrorSeverityShortCircuits asserts the
// activity never reaches the network when the input carries no
// error-severity rows — warnings alone are informational per FR-004
// edge case ("only `error` violations gate the iteration").
func TestPostLintViolations_NoErrorSeverityShortCircuits(t *testing.T) {
	t.Setenv("PATH", "") // any `gh` shell-out would fail loudly
	act := NewPostLintViolations()
	res, err := act.Execute(t.Context(), PostLintViolationsInput{
		PRNumber: 42, Repo: "owner/repo",
		Violations: []LintViolation{
			{Rule: "L01", File: "a.md", Line: 1, Severity: "warning", Message: "trailing whitespace"},
			{Rule: "L02", File: "b.md", Line: 2, Severity: "WARNING", Message: "case-insensitive too"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Eligible != 0 {
		t.Errorf("expected zero eligible (only warnings provided); got %d", res.Eligible)
	}
	if res.Posted != 0 || res.Skipped != 0 {
		t.Errorf("expected zero post/skip; got %+v", res)
	}
	if !strings.Contains(res.Explanation, "no error-severity") {
		t.Errorf("explanation should name the short-circuit reason: %q", res.Explanation)
	}
}

// TestPostLintViolations_FiltersUnanchoredErrors asserts violations
// missing a file or non-positive line are dropped before the dedup /
// POST pass — the GitHub review-comments endpoint requires both, and
// posting them would fail server-side.
func TestPostLintViolations_FiltersUnanchoredErrors(t *testing.T) {
	t.Setenv("PATH", "")
	act := NewPostLintViolations()
	res, err := act.Execute(t.Context(), PostLintViolationsInput{
		PRNumber: 42, Repo: "owner/repo",
		Violations: []LintViolation{
			{Rule: "L01", File: "", Line: 1, Severity: "error", Message: "no file"},
			{Rule: "L02", File: "x.md", Line: 0, Severity: "error", Message: "no line"},
			{Rule: "L03", File: "x.md", Line: -1, Severity: "error", Message: "negative line"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Eligible != 0 {
		t.Errorf("expected zero eligible after filter; got %d", res.Eligible)
	}
}

// TestActivityName pins the registration name so a refactor that
// renames the activity gets caught — the worker registers by this
// string and Temporal workflow history references it.
func TestPostLintViolations_ActivityName(t *testing.T) {
	if got := NewPostLintViolations().ActivityName(); got != "PostLintViolations" {
		t.Errorf("ActivityName = %q; want PostLintViolations", got)
	}
}

// stubGh writes a tiny shell script named `gh` into a temp dir and
// prepends that dir to PATH for the duration of the test. The stub
// handles the two gh invocations the activity makes:
//
//   - `gh api --paginate <path>`  → echoes the content of $GH_PAGINATE_FILE
//   - `gh api --method POST --input - <path>` → reads stdin into
//     $GH_POST_CAPTURE_FILE and echoes "{}"
//
// Any other invocation exits non-zero so a regression that adds a new
// gh call without updating the stub fails loudly instead of leaking
// to the real `gh` on the dev host's PATH.
//
// Returns the path the POST body will be captured to so the test can
// read it back and assert on payload shape.
func stubGh(t *testing.T) (postCapturePath string) {
	t.Helper()
	dir := t.TempDir()
	postCapturePath = filepath.Join(dir, "post-body.json")
	paginateFile := filepath.Join(dir, "paginate-stdout.json")

	script := `#!/bin/sh
# Stub gh — handles the two calls post_lint_violations.go makes.
# argv pattern A: gh api --paginate <path>
# argv pattern B: gh api --method POST --input - <path>
if [ "$1" = "api" ] && [ "$2" = "--paginate" ]; then
    cat "$GH_PAGINATE_FILE"
    exit 0
fi
if [ "$1" = "api" ] && [ "$2" = "--method" ] && [ "$3" = "POST" ]; then
    # --input - means body on stdin; persist it for the test.
    cat > "$GH_POST_CAPTURE_FILE"
    echo "{}"
    exit 0
fi
echo "stub gh: unexpected args: $*" >&2
exit 99
`
	ghPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(ghPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write gh stub: %v", err)
	}

	t.Setenv("PATH", dir)
	t.Setenv("GH_PAGINATE_FILE", paginateFile)
	t.Setenv("GH_POST_CAPTURE_FILE", postCapturePath)

	// Default: no existing comments. Individual tests can overwrite the
	// file before calling Execute to seed dedup state.
	if err := os.WriteFile(paginateFile, []byte("[]"), 0o644); err != nil {
		t.Fatalf("seed paginate file: %v", err)
	}
	return postCapturePath
}

// TestPostLintViolations_DedupsAndPostsOrderedPayload exercises the
// activity's load-bearing behavior end-to-end via a stubbed `gh` on
// PATH: existing chitin-authored comment present for one violation
// (skip), fresh violations for two others (post), and the resulting
// review payload pinned for commit_id, event, and (file, line, rule)
// ordering. Without this, the dedup + payload-shape code in Execute
// is uncovered.
func TestPostLintViolations_DedupsAndPostsOrderedPayload(t *testing.T) {
	capturePath := stubGh(t)

	// Seed one already-posted marker so the matching violation is
	// dropped by the dedup pass. The body shape mirrors what
	// renderLintCommentBody emits in production.
	existing := []map[string]string{
		{"body": fmt.Sprintf(
			"%s rule=L05 file=spec.md line=42 -->\n\n**spec-lint L05**: prior post",
			lintCommentMarkerPrefix)},
	}
	existingRaw, err := json.Marshal(existing)
	if err != nil {
		t.Fatalf("marshal existing: %v", err)
	}
	if err := os.WriteFile(os.Getenv("GH_PAGINATE_FILE"), existingRaw, 0o644); err != nil {
		t.Fatalf("seed paginate file: %v", err)
	}

	// Three violations on input:
	//   - L05 / spec.md / 42 — dup of the seeded marker → Skipped
	//   - L01 / tasks.md / 7 — fresh
	//   - L03 / spec.md / 9 — fresh
	// Provided in jumbled order so the test pins the deterministic
	// (file, line, rule) sort the activity guarantees.
	act := NewPostLintViolations()
	res, err := act.Execute(t.Context(), PostLintViolationsInput{
		PRNumber:  1234,
		Repo:      "chitinhq/chitin",
		CommitSHA: "deadbeefcafef00d",
		Violations: []LintViolation{
			{Rule: "L01", File: "tasks.md", Line: 7, Severity: "error", Message: "missing task"},
			{Rule: "L05", File: "spec.md", Line: 42, Severity: "error", Message: "invented surface"},
			{Rule: "L03", File: "spec.md", Line: 9, Severity: "error", Message: "FR has no task"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Eligible counts every error-severity row; Skipped is the dup;
	// Posted is the two fresh ones.
	if res.Eligible != 3 {
		t.Errorf("Eligible = %d, want 3", res.Eligible)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (the seeded L05/spec.md/42 dup)", res.Skipped)
	}
	if res.Posted != 2 {
		t.Errorf("Posted = %d, want 2", res.Posted)
	}

	// Read the POST body the stub captured and pin its shape.
	body, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured POST body: %v", err)
	}
	var payload struct {
		CommitID string `json:"commit_id"`
		Event    string `json:"event"`
		Comments []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Side string `json:"side"`
			Body string `json:"body"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode captured POST body: %v\n---\n%s", err, body)
	}

	if payload.CommitID != "deadbeefcafef00d" {
		t.Errorf("commit_id = %q, want deadbeefcafef00d", payload.CommitID)
	}
	if payload.Event != "COMMENT" {
		t.Errorf("event = %q, want COMMENT", payload.Event)
	}
	if len(payload.Comments) != 2 {
		t.Fatalf("comments len = %d, want 2; payload=%s", len(payload.Comments), body)
	}
	// Deterministic order is (file, line, rule). With the two fresh
	// rows that pins: spec.md/9/L03 before tasks.md/7/L01.
	if got := payload.Comments[0]; got.Path != "spec.md" || got.Line != 9 {
		t.Errorf("comments[0] = %+v, want spec.md:9", got)
	}
	if got := payload.Comments[1]; got.Path != "tasks.md" || got.Line != 7 {
		t.Errorf("comments[1] = %+v, want tasks.md:7", got)
	}
	// Each comment body carries the marker the dedup pass keys on.
	for i, c := range payload.Comments {
		if !strings.Contains(c.Body, lintCommentMarkerPrefix) {
			t.Errorf("comments[%d].body missing dedup marker: %q", i, c.Body)
		}
		if c.Side != "RIGHT" {
			t.Errorf("comments[%d].side = %q, want RIGHT", i, c.Side)
		}
	}
}

// TestPostLintViolations_AllDupsSkipsPost asserts the activity does
// NOT make the POST call when every eligible violation matches an
// existing marker — the stub would error out (no POST handler invoked)
// only if Execute incorrectly proceeded to post.
func TestPostLintViolations_AllDupsSkipsPost(t *testing.T) {
	capturePath := stubGh(t)

	existing := []map[string]string{
		{"body": fmt.Sprintf("%s rule=L01 file=a.md line=1 -->\n\n**spec-lint L01**: x",
			lintCommentMarkerPrefix)},
	}
	raw, _ := json.Marshal(existing)
	if err := os.WriteFile(os.Getenv("GH_PAGINATE_FILE"), raw, 0o644); err != nil {
		t.Fatalf("seed paginate file: %v", err)
	}

	act := NewPostLintViolations()
	res, err := act.Execute(t.Context(), PostLintViolationsInput{
		PRNumber: 1, Repo: "x/y", CommitSHA: "abc",
		Violations: []LintViolation{
			{Rule: "L01", File: "a.md", Line: 1, Severity: "error", Message: "dup"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Posted != 0 || res.Skipped != 1 {
		t.Errorf("expected Posted=0 Skipped=1, got %+v", res)
	}
	if !strings.Contains(res.Explanation, "already posted") {
		t.Errorf("explanation should name the all-dups short-circuit: %q", res.Explanation)
	}
	// The stub would have written something to the capture path if
	// the POST was invoked; its absence is the assertion.
	if _, err := os.Stat(capturePath); !os.IsNotExist(err) {
		t.Errorf("POST body was written despite all-dups skip: stat err=%v", err)
	}
}

// TestMarkerKey_IsInjective asserts distinct (rule, file, line)
// tuples produce distinct keys — a collision would silently dedup
// two unrelated violations into one missing post.
func TestMarkerKey_IsInjective(t *testing.T) {
	keys := map[string]struct{}{}
	for _, tup := range []struct {
		rule, file string
		line       int
	}{
		{"L01", "spec.md", 1},
		{"L01", "spec.md", 2},
		{"L02", "spec.md", 1},
		{"L01", "tasks.md", 1},
	} {
		k := markerKey(tup.rule, tup.file, tup.line)
		if _, dup := keys[k]; dup {
			t.Errorf("collision at %v: key %q", tup, k)
		}
		keys[k] = struct{}{}
	}
}
