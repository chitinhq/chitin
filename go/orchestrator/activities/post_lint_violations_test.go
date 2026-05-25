package activities

import (
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
