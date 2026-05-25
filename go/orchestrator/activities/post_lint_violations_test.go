package activities

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestBuildLintCommentBody_ShapeAndMarker asserts the rendered comment
// body carries the rule id, severity, message, and the hidden dedup
// marker. Pure-function test; no IO.
func TestBuildLintCommentBody_ShapeAndMarker(t *testing.T) {
	v := LintViolation{
		Rule:     "L05",
		File:     ".specify/specs/115-spec-review-gate/spec.md",
		Line:     132,
		Severity: "error",
		Message:  "  chitin-kernel events is not in the known CLI surface allowlist  ",
	}
	got := BuildLintCommentBody(v)

	if !strings.Contains(got, "spec-lint L05") {
		t.Errorf("body missing rule id; got:\n%s", got)
	}
	if !strings.Contains(got, "error") {
		t.Errorf("body missing severity; got:\n%s", got)
	}
	if !strings.Contains(got, "chitin-kernel events is not in the known CLI surface allowlist") {
		t.Errorf("body missing trimmed message; got:\n%s", got)
	}
	want := lintMarker(v.Rule, v.File, v.Line)
	if !strings.Contains(got, want) {
		t.Errorf("body missing dedup marker %q; got:\n%s", want, got)
	}
}

// TestParseLintMarker_RoundTrip asserts a marker built by lintMarker
// parses back to its three fields exactly.
func TestParseLintMarker_RoundTrip(t *testing.T) {
	cases := []struct {
		rule, file string
		line       int
	}{
		{"L01", "spec.md", 1},
		{"L07", ".specify/specs/115-spec-review-gate/tasks.md", 42},
		{"L04", "weird/path with spaces.md", 9999},
	}
	for _, c := range cases {
		body := "leading copy\n" + lintMarker(c.rule, c.file, c.line) + "\ntrailing"
		gotRule, gotFile, gotLine, ok := parseLintMarker(body)
		if !ok {
			t.Errorf("parse failed for %+v in body %q", c, body)
			continue
		}
		if gotRule != c.rule || gotFile != c.file || gotLine != c.line {
			t.Errorf("round trip: want (%s,%s,%d) got (%s,%s,%d)",
				c.rule, c.file, c.line, gotRule, gotFile, gotLine)
		}
	}
}

// TestParseLintMarker_NoMarker asserts a body without the chitin marker
// returns ok=false (so non-chitin review comments don't poison dedup).
func TestParseLintMarker_NoMarker(t *testing.T) {
	cases := []string{
		"",
		"plain copilot review comment",
		"<!-- some other html comment -->",
		// Marker prefix present but no suffix.
		"<!-- chitin-spec-lint:L01|x|1",
		// Suffix present but no prefix.
		"L01|x|1 -->",
		// Wrong field count.
		"<!-- chitin-spec-lint:L01|x -->",
		// Non-numeric line.
		"<!-- chitin-spec-lint:L01|x|abc -->",
	}
	for _, body := range cases {
		if _, _, _, ok := parseLintMarker(body); ok {
			t.Errorf("expected parse to fail for body %q", body)
		}
	}
}

// TestFilterErrorSeverity_OnlyErrors asserts warnings flow past and only
// error-severity violations remain.
func TestFilterErrorSeverity_OnlyErrors(t *testing.T) {
	in := []LintViolation{
		{Rule: "L01", Severity: "error"},
		{Rule: "L02", Severity: "warning"},
		{Rule: "L03", Severity: "error"},
		{Rule: "L04", Severity: ""}, // unspecified — treated as non-error
	}
	got := filterErrorSeverity(in)
	if len(got) != 2 {
		t.Fatalf("want 2 error violations, got %d: %+v", len(got), got)
	}
	if got[0].Rule != "L01" || got[1].Rule != "L03" {
		t.Errorf("error-only filter changed order or dropped wrong items: %+v", got)
	}
}

// TestDedupViolations_SkipsExisting asserts violations whose key is
// already in the existing set drop out, and order is preserved.
func TestDedupViolations_SkipsExisting(t *testing.T) {
	vs := []LintViolation{
		{Rule: "L01", File: "a.md", Line: 1, Severity: "error"},
		{Rule: "L02", File: "a.md", Line: 5, Severity: "error"},
		{Rule: "L01", File: "b.md", Line: 1, Severity: "error"},
	}
	existing := map[string]struct{}{
		violationKey("L02", "a.md", 5): {},
	}
	got := dedupViolations(vs, existing)
	if len(got) != 2 {
		t.Fatalf("want 2 after dedup, got %d: %+v", len(got), got)
	}
	if got[0].Rule != "L01" || got[0].File != "a.md" {
		t.Errorf("dedup reordered the survivors: %+v", got)
	}
	if got[1].Rule != "L01" || got[1].File != "b.md" {
		t.Errorf("dedup dropped the wrong item: %+v", got)
	}
}

// TestDedupViolations_AllDeduped asserts a full overlap returns an empty
// slice without panicking on the empty path.
func TestDedupViolations_AllDeduped(t *testing.T) {
	vs := []LintViolation{
		{Rule: "L01", File: "a.md", Line: 1, Severity: "error"},
	}
	existing := map[string]struct{}{
		violationKey("L01", "a.md", 1): {},
	}
	if got := dedupViolations(vs, existing); len(got) != 0 {
		t.Errorf("want empty after full dedup, got %+v", got)
	}
}

// TestPostLintViolations_MissingInputs asserts the input guard fires
// and returns a populated explanation without making any network call.
func TestPostLintViolations_MissingInputs(t *testing.T) {
	act := NewPostLintViolations()
	res, err := act.Execute(context.Background(), PostLintViolationsInput{
		// PRNumber: 0,
		Repo: "chitinhq/chitin",
		Violations: []LintViolation{
			{Rule: "L01", Severity: "error"},
		},
	})
	if err != nil {
		t.Fatalf("Execute must be fail-soft, returned err: %v", err)
	}
	if res.PostedCount != 0 || res.ReviewID != 0 {
		t.Errorf("missing-input guard should not post; got %+v", res)
	}
	if !strings.Contains(res.Explanation, "missing PRNumber or Repo") {
		t.Errorf("explanation should name the missing inputs, got %q", res.Explanation)
	}
}

// TestPostLintViolations_NoErrorViolations asserts a payload of only
// warnings short-circuits without fetching or posting.
func TestPostLintViolations_NoErrorViolations(t *testing.T) {
	guardCallCount := 0
	withStubbedFetchAndPost(t,
		func(ctx context.Context, repo string, pr int) (map[string]struct{}, error) {
			guardCallCount++
			return nil, nil
		},
		func(ctx context.Context, repo string, pr int, vs []LintViolation) (int64, error) {
			guardCallCount++
			return 0, nil
		})

	act := NewPostLintViolations()
	res, err := act.Execute(context.Background(), PostLintViolationsInput{
		PRNumber: 42,
		Repo:     "chitinhq/chitin",
		Violations: []LintViolation{
			{Rule: "L01", File: "x.md", Line: 1, Severity: "warning"},
			{Rule: "L02", File: "y.md", Line: 5, Severity: "warning"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if guardCallCount != 0 {
		t.Errorf("warnings-only payload must not call fetch or post; got %d calls", guardCallCount)
	}
	if res.SkippedNonError != 2 {
		t.Errorf("want SkippedNonError=2, got %d", res.SkippedNonError)
	}
	if !strings.Contains(res.Explanation, "no error-severity violations") {
		t.Errorf("explanation should name the no-error short-circuit, got %q", res.Explanation)
	}
}

// TestPostLintViolations_AllDeduped asserts a payload whose every error
// violation already has a marker triggers the all-deduped short-circuit
// without calling the POST hook.
func TestPostLintViolations_AllDeduped(t *testing.T) {
	postCalls := 0
	withStubbedFetchAndPost(t,
		func(ctx context.Context, repo string, pr int) (map[string]struct{}, error) {
			return map[string]struct{}{
				violationKey("L01", "x.md", 1): {},
				violationKey("L03", "y.md", 9): {},
			}, nil
		},
		func(ctx context.Context, repo string, pr int, vs []LintViolation) (int64, error) {
			postCalls++
			return 0, nil
		})

	act := NewPostLintViolations()
	res, err := act.Execute(context.Background(), PostLintViolationsInput{
		PRNumber: 42,
		Repo:     "chitinhq/chitin",
		Violations: []LintViolation{
			{Rule: "L01", File: "x.md", Line: 1, Severity: "error"},
			{Rule: "L03", File: "y.md", Line: 9, Severity: "error"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if postCalls != 0 {
		t.Errorf("all-deduped payload must not call POST; got %d calls", postCalls)
	}
	if res.DedupedCount != 2 || res.PostedCount != 0 {
		t.Errorf("want DedupedCount=2 PostedCount=0, got %+v", res)
	}
	if !strings.Contains(res.Explanation, "already posted") {
		t.Errorf("explanation should name the all-deduped short-circuit, got %q", res.Explanation)
	}
}

// TestPostLintViolations_PostsSurvivors asserts the happy path: a mix
// of new errors, already-posted errors, and warnings produces a POST
// carrying only the new errors, with the result counts matching.
func TestPostLintViolations_PostsSurvivors(t *testing.T) {
	var posted []LintViolation
	var postedRepo string
	var postedPR int
	withStubbedFetchAndPost(t,
		func(ctx context.Context, repo string, pr int) (map[string]struct{}, error) {
			return map[string]struct{}{
				violationKey("L02", "a.md", 5): {},
			}, nil
		},
		func(ctx context.Context, repo string, pr int, vs []LintViolation) (int64, error) {
			postedRepo = repo
			postedPR = pr
			posted = append(posted, vs...)
			return 7777, nil
		})

	act := NewPostLintViolations()
	res, err := act.Execute(context.Background(), PostLintViolationsInput{
		PRNumber: 42,
		Repo:     "chitinhq/chitin",
		Violations: []LintViolation{
			{Rule: "L01", File: "a.md", Line: 1, Severity: "error", Message: "frontmatter missing owner"},
			{Rule: "L02", File: "a.md", Line: 5, Severity: "error", Message: "already-posted"},
			{Rule: "L03", File: "b.md", Line: 9, Severity: "warning", Message: "warning-only"},
			{Rule: "L04", File: "c.md", Line: 2, Severity: "error", Message: "event taxonomy drift"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if postedRepo != "chitinhq/chitin" || postedPR != 42 {
		t.Errorf("POST hook called with wrong args: repo=%q pr=%d", postedRepo, postedPR)
	}
	if len(posted) != 2 {
		t.Fatalf("want 2 posted survivors, got %d: %+v", len(posted), posted)
	}
	if posted[0].Rule != "L01" || posted[1].Rule != "L04" {
		t.Errorf("survivors lost order or wrong items: %+v", posted)
	}
	if res.PostedCount != 2 {
		t.Errorf("want PostedCount=2, got %d", res.PostedCount)
	}
	if res.DedupedCount != 1 {
		t.Errorf("want DedupedCount=1, got %d", res.DedupedCount)
	}
	if res.SkippedNonError != 1 {
		t.Errorf("want SkippedNonError=1, got %d", res.SkippedNonError)
	}
	if res.ReviewID != 7777 {
		t.Errorf("want ReviewID=7777, got %d", res.ReviewID)
	}
}

// TestPostLintViolations_FetchFailureFailSoft asserts that a fetch
// fault on the dedup-discovery step returns a fail-soft result rather
// than propagating an activity error.
func TestPostLintViolations_FetchFailureFailSoft(t *testing.T) {
	withStubbedFetchAndPost(t,
		func(ctx context.Context, repo string, pr int) (map[string]struct{}, error) {
			return nil, errors.New("simulated gh outage")
		},
		func(ctx context.Context, repo string, pr int, vs []LintViolation) (int64, error) {
			t.Fatalf("POST hook should not be reached when fetch fails")
			return 0, nil
		})

	act := NewPostLintViolations()
	res, err := act.Execute(context.Background(), PostLintViolationsInput{
		PRNumber: 1,
		Repo:     "chitinhq/chitin",
		Violations: []LintViolation{
			{Rule: "L01", File: "a.md", Line: 1, Severity: "error"},
		},
	})
	if err != nil {
		t.Fatalf("Execute must fold fetch faults into result, got err: %v", err)
	}
	if res.PostedCount != 0 || res.ReviewID != 0 {
		t.Errorf("fetch-fault path must not record a post; got %+v", res)
	}
	if !strings.Contains(res.Explanation, "fetch existing review comments failed") ||
		!strings.Contains(res.Explanation, "simulated gh outage") {
		t.Errorf("explanation should carry the fetch fault, got %q", res.Explanation)
	}
}

// TestPostLintViolations_PostFailureFailSoft asserts that a POST fault
// after passing dedup folds into the result.
func TestPostLintViolations_PostFailureFailSoft(t *testing.T) {
	withStubbedFetchAndPost(t,
		func(ctx context.Context, repo string, pr int) (map[string]struct{}, error) {
			return nil, nil
		},
		func(ctx context.Context, repo string, pr int, vs []LintViolation) (int64, error) {
			return 0, errors.New("422 unprocessable")
		})

	act := NewPostLintViolations()
	res, err := act.Execute(context.Background(), PostLintViolationsInput{
		PRNumber: 1,
		Repo:     "chitinhq/chitin",
		Violations: []LintViolation{
			{Rule: "L01", File: "a.md", Line: 1, Severity: "error"},
		},
	})
	if err != nil {
		t.Fatalf("Execute must fold POST faults into result, got err: %v", err)
	}
	if res.PostedCount != 0 {
		t.Errorf("PostedCount must remain 0 on POST fault, got %d", res.PostedCount)
	}
	if !strings.Contains(res.Explanation, "post review failed") ||
		!strings.Contains(res.Explanation, "422 unprocessable") {
		t.Errorf("explanation should carry the POST fault, got %q", res.Explanation)
	}
}

// withStubbedFetchAndPost overrides the two package-level hooks for the
// duration of one subtest. t.Cleanup restores the originals so other
// tests run unaffected.
func withStubbedFetchAndPost(
	t *testing.T,
	fetch func(ctx context.Context, repo string, pr int) (map[string]struct{}, error),
	post func(ctx context.Context, repo string, pr int, vs []LintViolation) (int64, error),
) {
	t.Helper()
	origFetch := fetchExistingLintMarkersFn
	origPost := postLintReviewFn
	fetchExistingLintMarkersFn = fetch
	postLintReviewFn = post
	t.Cleanup(func() {
		fetchExistingLintMarkersFn = origFetch
		postLintReviewFn = origPost
	})
}
