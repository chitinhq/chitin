package activities

import (
	"strings"
	"testing"
)

// TestBuildInternalReviewPrompt_HappyShape proves the prompt includes
// every invariant claimed by the activity's contract: the verdict schema
// inline, the original review body, every line comment, and the fixup
// diff. A missing section would silently degrade the re-reviewer's
// signal — a verdict produced from a half-prompt is worse than no
// verdict because the operator can't tell which case it was.
func TestBuildInternalReviewPrompt_HappyShape(t *testing.T) {
	in := DispatchInternalReviewInput{
		PRNumber:    1234,
		FixupAuthor: "codex",
	}
	orig := reviewContext{
		Body: "Two nits and a real concern.",
		LineComments: []reviewLineComment{
			{Path: "foo.go", Line: 42, Body: "magic constant; extract"},
			{Path: "bar.go", Line: 7, Body: "missing error wrap"},
		},
	}
	diff := "diff --git a/foo.go b/foo.go\n@@ -42 +42 @@\n-const X = 7\n+const MagicXThreshold = 7\n"
	got := BuildInternalReviewPrompt(in, diff, orig)

	mustContain := []string{
		"PR #1234",                                // PR identity
		`driver "codex"`,                          // author exclusion provenance
		"StructuredVerdict",                       // schema label
		"approve|approve-with-comments",           // verdict shape
		"confidence",                              // spec 116 field
		"high|medium|low",                         // confidence shape
		"Two nits and a real concern.",            // original body
		"foo.go:42",                               // line comment 1 location
		"magic constant; extract",                 // line comment 1 body
		"bar.go:7",                                // line comment 2 location
		"missing error wrap",                      // line comment 2 body
		"FIXUP COMMIT DIFF",                       // diff label
		"MagicXThreshold",                         // diff content
		"emit ONLY the StructuredVerdict JSON",    // exit instruction
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing required substring %q\n--- prompt ---\n%s\n--------------", want, got)
		}
	}
}

// TestBuildInternalReviewPrompt_OmitsEmptyOriginalBody proves the
// prompt doesn't render an empty "ORIGINAL COPILOT REVIEW BODY:"
// header when the original review carried only line comments and no
// summary text — keeps the prompt tight and avoids confusing the
// reviewer with a vacant section.
func TestBuildInternalReviewPrompt_OmitsEmptyOriginalBody(t *testing.T) {
	orig := reviewContext{
		Body: "",
		LineComments: []reviewLineComment{
			{Path: "x.go", Line: 1, Body: "rename this"},
		},
	}
	got := BuildInternalReviewPrompt(DispatchInternalReviewInput{PRNumber: 1}, "diff", orig)
	if strings.Contains(got, "ORIGINAL COPILOT REVIEW BODY") {
		t.Errorf("prompt rendered empty REVIEW BODY section; should omit\n--- prompt ---\n%s", got)
	}
}

// TestBuildInternalReviewPrompt_OmitsEmptyLineComments proves the prompt
// doesn't render an empty "ORIGINAL COPILOT LINE COMMENTS" header when
// the original review carried only a summary body.
func TestBuildInternalReviewPrompt_OmitsEmptyLineComments(t *testing.T) {
	orig := reviewContext{
		Body:         "Big-picture concern about the auth boundary.",
		LineComments: nil,
	}
	got := BuildInternalReviewPrompt(DispatchInternalReviewInput{PRNumber: 1}, "diff", orig)
	if strings.Contains(got, "ORIGINAL COPILOT LINE COMMENTS") {
		t.Errorf("prompt rendered empty LINE COMMENTS section; should omit\n--- prompt ---\n%s", got)
	}
}

// TestTruncateForReview proves the 1 KiB cap fires only at the
// threshold — short strings pass through verbatim, long strings get
// clipped to exactly the cap. The cap matches the spec 109/110 driver
// wrappers; drift would break log-line size assumptions.
func TestTruncateForReview(t *testing.T) {
	short := strings.Repeat("x", 100)
	if got := truncateForReview(short); got != short {
		t.Errorf("short string altered; got len %d want %d", len(got), len(short))
	}
	exact := strings.Repeat("x", 1024)
	if got := truncateForReview(exact); got != exact {
		t.Errorf("exact-cap string altered; got len %d want %d", len(got), len(exact))
	}
	long := strings.Repeat("x", 2048)
	got := truncateForReview(long)
	if len(got) != 1024 {
		t.Errorf("long string len = %d, want 1024", len(got))
	}
}
