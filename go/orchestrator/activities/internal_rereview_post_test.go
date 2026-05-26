package activities

import (
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
)

// TestReviewEventFor proves the verdict→gh-event mapping. Both
// approve-shaped values collapse to APPROVE; abstain becomes COMMENT
// (not silently dropped — the operator wants the abstention on the
// timeline); request-changes becomes REQUEST_CHANGES; any unknown enum
// returns empty so the activity short-circuits to no_review_event_for_verdict.
func TestReviewEventFor(t *testing.T) {
	cases := []struct {
		in   verdict.Enum
		want string
	}{
		{verdict.Approve, "APPROVE"},
		{verdict.ApproveWithComments, "APPROVE"},
		{verdict.RequestChanges, "REQUEST_CHANGES"},
		{verdict.Abstain, "COMMENT"},
		{verdict.Enum("maybe"), ""},
		{verdict.Enum(""), ""},
	}
	for _, tc := range cases {
		if got := reviewEventFor(tc.in); got != tc.want {
			t.Errorf("reviewEventFor(%q) = %q, want %q", string(tc.in), got, tc.want)
		}
	}
}

// TestEventFlag proves the gh-event→CLI-flag mapping. gh pr review takes
// --approve / --request-changes / --comment, not the GraphQL event names;
// the default fallback is --comment so a future GraphQL event we forget
// to wire still posts something rather than nothing.
func TestEventFlag(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"APPROVE", "approve"},
		{"REQUEST_CHANGES", "request-changes"},
		{"COMMENT", "comment"},
		{"PENDING", "comment"}, // fallback
		{"", "comment"},        // fallback
	}
	for _, tc := range cases {
		if got := eventFlag(tc.in); got != tc.want {
			t.Errorf("eventFlag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRenderReviewBody_Approve proves the rendered body carries the
// reviewer attribution, confidence, the verdict, and the canonical JSON
// in the fenced block — and that a clean approve renders without the
// concerns/recommendations/blockers headers.
func TestRenderReviewBody_Approve(t *testing.T) {
	v := verdict.StructuredVerdict{Verdict: verdict.Approve, Confidence: verdict.ConfidenceHigh}
	canonical := `{"verdict":"approve","concerns":null,"recommendations":null,"blockers":null,"confidence":"high"}`
	got := renderReviewBody("codex", v, canonical)

	mustContain := []string{
		"`codex`",                                       // reviewer attribution
		"verdict: `approve`",                            // verdict surface
		"confidence: `high`",                            // confidence surface
		canonical,                                       // canonical JSON in fenced block
		"StructuredVerdict JSON (spec 094)",             // fenced section header
		"Posted by the Chitin orchestrator (spec 116",   // footer
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("body missing %q\n--- body ---\n%s", want, got)
		}
	}
	mustNotContain := []string{"**Blockers:**", "**Concerns:**", "**Recommendations:**", "**Reason:**"}
	for _, no := range mustNotContain {
		if strings.Contains(got, no) {
			t.Errorf("body unexpectedly contains %q (clean approve should omit empty sections)\n--- body ---\n%s", no, got)
		}
	}
}

// TestRenderReviewBody_RequestChanges proves a request-changes verdict
// renders every blocker as a bullet under the Blockers header AND that
// the confidence value defaults to medium when omitted.
func TestRenderReviewBody_RequestChanges(t *testing.T) {
	v := verdict.StructuredVerdict{
		Verdict:  verdict.RequestChanges,
		Blockers: []string{"missing nil-check on payload", "test asserts wrong field"},
	}
	got := renderReviewBody("claudecode", v, "{}")

	if !strings.Contains(got, "confidence: `medium`") {
		t.Errorf("body missing default-medium confidence rendering\n--- body ---\n%s", got)
	}
	if !strings.Contains(got, "**Blockers:**") {
		t.Errorf("body missing Blockers header\n--- body ---\n%s", got)
	}
	if !strings.Contains(got, "- missing nil-check on payload") {
		t.Errorf("body missing blocker 1 bullet\n--- body ---\n%s", got)
	}
	if !strings.Contains(got, "- test asserts wrong field") {
		t.Errorf("body missing blocker 2 bullet\n--- body ---\n%s", got)
	}
}

// TestRenderReviewBody_AbstainWithReason proves the optional reason
// field renders when populated. An abstain verdict typically carries
// the reviewer's rationale in Reason; rendering it here is the
// operator's only signal.
func TestRenderReviewBody_AbstainWithReason(t *testing.T) {
	v := verdict.StructuredVerdict{
		Verdict: verdict.Abstain,
		Reason:  "insufficient context — fixup touches code I don't have access to",
	}
	got := renderReviewBody("codex", v, "{}")
	if !strings.Contains(got, "**Reason:**") {
		t.Errorf("body missing Reason header\n--- body ---\n%s", got)
	}
	if !strings.Contains(got, "insufficient context") {
		t.Errorf("body missing reason text\n--- body ---\n%s", got)
	}
}
