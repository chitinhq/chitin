// pr_eligibility_test.go — spec 099 slice 3 tests for the PR
// eligibility logic. Pure-function tests; no HTTP or chain side
// effects (those live in factory_listen_pr_test.go).

package main

import (
	"testing"
)

func mkLabels(names ...string) []struct {
	Name string `json:"name"`
} {
	out := make([]struct {
		Name string `json:"name"`
	}, len(names))
	for i, n := range names {
		out[i].Name = n
	}
	return out
}

func TestCheckPREligibility_OpenedWithLabel_IsEligible(t *testing.T) {
	p := &prPayload{Action: "opened"}
	p.PullRequest.Labels = mkLabels("chitin-dispatch", "kind/feat")
	p.PullRequest.Body = "Closes #42\n\nDoes the thing."

	got := checkPREligibility("pull_request", p)
	if !got.Eligible {
		t.Errorf("Eligible = false, want true; reasons=%v", got.Reasons)
	}
	if got.IssueNumber != 42 {
		t.Errorf("IssueNumber = %d, want 42", got.IssueNumber)
	}
}

func TestCheckPREligibility_MissingLabel_NotEligible(t *testing.T) {
	p := &prPayload{Action: "opened"}
	p.PullRequest.Labels = mkLabels("kind/feat")
	p.PullRequest.Body = "Closes #42"

	got := checkPREligibility("pull_request", p)
	if got.Eligible {
		t.Errorf("Eligible = true, want false")
	}
	if !containsReason(got.Reasons, "missing_label") {
		t.Errorf("reasons should include missing_label; got %v", got.Reasons)
	}
}

func TestCheckPREligibility_IneligibleAction_NotEligible(t *testing.T) {
	p := &prPayload{Action: "closed"}
	p.PullRequest.Labels = mkLabels("chitin-dispatch")

	got := checkPREligibility("pull_request", p)
	if got.Eligible {
		t.Errorf("Eligible = true, want false")
	}
	if !containsReason(got.Reasons, "not_draft_or_ready") {
		t.Errorf("reasons should include not_draft_or_ready; got %v", got.Reasons)
	}
}

func TestCheckPREligibility_EligibleActionsCovered(t *testing.T) {
	// opened, ready_for_review, reopened, synchronize all qualify per FR-007.
	for _, action := range []string{"opened", "ready_for_review", "reopened", "synchronize"} {
		t.Run(action, func(t *testing.T) {
			p := &prPayload{Action: action}
			p.PullRequest.Labels = mkLabels("chitin-dispatch")
			p.PullRequest.Body = "Closes #1"

			got := checkPREligibility("pull_request", p)
			if !got.Eligible {
				t.Errorf("%q: Eligible = false, want true; reasons=%v", action, got.Reasons)
			}
		})
	}
}

func TestCheckPREligibility_LabelPresentButNoClosesRef_StillEligible(t *testing.T) {
	// Per FR-007 condition 3 note: missing Closes reference does NOT
	// make the PR ineligible — review fires with spec_ref="unknown".
	p := &prPayload{Action: "opened"}
	p.PullRequest.Labels = mkLabels("chitin-dispatch")
	p.PullRequest.Body = "Implements the thing without a Closes reference."

	got := checkPREligibility("pull_request", p)
	if !got.Eligible {
		t.Errorf("Eligible = false, want true; reasons=%v", got.Reasons)
	}
	if got.SpecRef != "unknown" {
		t.Errorf("SpecRef = %q, want \"unknown\"", got.SpecRef)
	}
	if !containsReason(got.Reasons, "no_closes_reference") {
		t.Errorf("reasons should note no_closes_reference; got %v", got.Reasons)
	}
}

func TestCheckPREligibility_PullRequestReview_NotEligible(t *testing.T) {
	// Review events are ignored on this code path — they're noise for
	// the "should we start a review?" decision.
	got := checkPREligibility("pull_request_review", &prPayload{})
	if got.Eligible {
		t.Errorf("Eligible = true, want false for pull_request_review")
	}
	if !containsReason(got.Reasons, "event_type_ignored") {
		t.Errorf("reasons should be event_type_ignored; got %v", got.Reasons)
	}
}

func TestCheckPREligibility_IssueComment_EligibleWhenLabelPresent(t *testing.T) {
	p := &prPayload{Action: "created"}
	p.Issue.Labels = mkLabels("chitin-dispatch")
	p.Issue.Body = "Closes #100"

	got := checkPREligibility("issue_comment", p)
	if !got.Eligible {
		t.Errorf("Eligible = false, want true; reasons=%v", got.Reasons)
	}
	if got.IssueNumber != 100 {
		t.Errorf("IssueNumber = %d, want 100", got.IssueNumber)
	}
}

func TestCheckPREligibility_UnknownEventType_NotEligible(t *testing.T) {
	got := checkPREligibility("workflow_run", &prPayload{Action: "completed"})
	if got.Eligible {
		t.Errorf("unknown event type should not be eligible")
	}
}

func TestParseClosesReference_Variants(t *testing.T) {
	cases := []struct {
		body    string
		wantNum int
		wantOk  bool
	}{
		{"Closes #42", 42, true},
		{"closes #42", 42, true},
		{"closed #42", 42, true},
		{"fix #99", 99, true},
		{"fixes #99", 99, true},
		{"fixed #99", 99, true},
		{"resolve #7", 7, true},
		{"resolves #7", 7, true},
		{"resolved #7", 7, true},
		{"PR text\n\nCloses #123\n\nMore body.", 123, true},
		{"enclosed #42", 0, false},     // word-boundary: must not match
		{"foreclose #42", 0, false},    // word-boundary
		{"see #42 for context", 0, false},
		{"", 0, false},
		{"Closes#42", 0, false}, // requires whitespace per pattern
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			n, ok := parseClosesReference(tc.body)
			if n != tc.wantNum || ok != tc.wantOk {
				t.Errorf("parseClosesReference(%q) = (%d, %v); want (%d, %v)",
					tc.body, n, ok, tc.wantNum, tc.wantOk)
			}
		})
	}
}

func containsReason(reasons []string, target string) bool {
	for _, r := range reasons {
		if r == target {
			return true
		}
	}
	return false
}
