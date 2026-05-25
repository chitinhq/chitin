// pr_eligibility.go — spec 099 slice 3: PR event eligibility checks
// for the factory-listen /webhook/pr route.
//
// FR-007 invariant: a pull-request event is eligible for the Copilot
// review path when ALL of:
//
//   1. action ∈ {opened, ready_for_review, reopened, synchronize}
//   2. PR carries the `chitin-dispatch` label
//   3. PR body contains a `Closes #N` reference; N may or may not
//      cross-resolve to a known copilot_dispatched issue
//
// Conditions 1 + 2 are necessary even without condition 3 — if (1+2)
// hold but (3) fails, the PR is still detected (slice 4 emits
// copilot_pr_detected with spec_ref="unknown") and review fires.

package main

import (
	"regexp"
	"strings"
)

// prPayload is the subset of GitHub's pull_request / issue_comment /
// pull_request_review webhook payload that this listener consumes. We
// only pull fields needed for eligibility + telemetry; the full body is
// captured separately for the FR-013 copilot_pr_activity event.
type prPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		URL       string `json:"html_url"`
		Draft     bool   `json:"draft"`
		Merged    bool   `json:"merged"`
		Body      string `json:"body"`
		Commits   int    `json:"commits"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
		Changed   int    `json:"changed_files"`
		Head      struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"pull_request"`
	// For issue_comment events the PR object is at .issue and labels
	// arrive on the issue, not the PR.
	Issue struct {
		Number int `json:"number"`
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"issue"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	// Review carries the pull_request_review payload's review object —
	// state, body, id, author login. Populated for pull_request_review
	// events; zero for pull_request / issue_comment events. (Spec 113 US1.)
	Review struct {
		ID    int64  `json:"id"`
		State string `json:"state"`
		Body  string `json:"body"`
		User  struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"review"`
}

// prEligibility is the closed result shape of checking a PR event for
// the Copilot review path. The eligibility check is pure — no IO, no
// chain queries — so the same logic can be used in both the route
// handler and (future) replay tooling.
type prEligibility struct {
	Eligible    bool
	Reasons     []string // populated when !Eligible (route reports first)
	SpecRef     string   // recovered from "Closes #N" + chain lookup (TODO slice 4); "unknown" if not recoverable
	IssueNumber int      // recovered from "Closes #N"; 0 if absent
}

// chitinDispatchLabel is the marker label Copilot must carry forward
// from the dispatch issue per FR-007. The dispatch issue gets the label
// from `chitin-orchestrator schedule --driver copilot` (slice 2);
// GitHub propagates labels from the closed issue to the PR's display
// but not to the PR object itself — Copilot's GitHub integration is
// expected to apply the label on PR open (verified in production smoke
// per quickstart.md step 2). Manual label apply also works.
const chitinDispatchLabel = "chitin-dispatch"

// eligibleActions enumerates the pull_request actions that should
// trigger eligibility evaluation. Other actions (closed, locked,
// labeled, unlabeled, etc.) flow through the always-on
// copilot_pr_activity telemetry stream but do not start a review.
var eligibleActions = map[string]struct{}{
	"opened":           {},
	"ready_for_review": {},
	"reopened":         {},
	"synchronize":      {},
}

// closesReferencePattern matches GitHub's auto-close issue references
// in PR bodies. Recognized keywords (case-insensitive): close, closes,
// closed, fix, fixes, fixed, resolve, resolves, resolved.
// Match is anchored on a word boundary so "encloses" / "preclude" /
// etc. don't false-positive.
var closesReferencePattern = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s+#(\d+)\b`)

// checkPREligibility computes the eligibility result for a parsed
// pr-event payload. Pure function; no IO.
//
// eventType matches GitHub's X-GitHub-Event header value: one of
// "pull_request", "pull_request_review", "issue_comment".
//
// For issue_comment events on an issue, the eligibility is computed
// over the issue's labels + body — used so PR-thread comments can
// re-trigger review eligibility if the underlying PR was originally
// missing a label.
func checkPREligibility(eventType string, p *prPayload) prEligibility {
	switch eventType {
	case "pull_request":
		return checkPullRequestEvent(p)
	case "pull_request_review":
		// Review events don't change PR draft/label state but they're
		// noise for the "should we start a review?" decision. Mark
		// non-eligible with a clear reason; the telemetry stream still
		// captures the activity.
		return prEligibility{Eligible: false, Reasons: []string{"event_type_ignored"}}
	case "issue_comment":
		return checkIssueCommentEvent(p)
	default:
		return prEligibility{Eligible: false, Reasons: []string{"event_type_ignored"}}
	}
}

func checkPullRequestEvent(p *prPayload) prEligibility {
	r := prEligibility{}
	if _, ok := eligibleActions[p.Action]; !ok {
		r.Reasons = append(r.Reasons, "not_draft_or_ready")
		return r
	}
	if !hasLabel(p.PullRequest.Labels, chitinDispatchLabel) {
		r.Reasons = append(r.Reasons, "missing_label")
	}
	issueNum, found := parseClosesReference(p.PullRequest.Body)
	if !found {
		// Per FR-007 condition 3: missing Closes reference does NOT
		// make the PR ineligible — review still fires with
		// spec_ref="unknown" (handled slice 4). Record the absence so
		// the response body surfaces it.
		r.Reasons = append(r.Reasons, "no_closes_reference")
		r.SpecRef = "unknown"
	} else {
		r.IssueNumber = issueNum
		// SpecRef recovery via chain lookup of copilot_dispatched(issue_number=N)
		// is slice 4. For now leave SpecRef empty; slice 4 fills it.
	}
	// Eligibility = action OK + label present. Reasons may contain
	// "no_closes_reference" but that's informational, not blocking.
	r.Eligible = hasLabel(p.PullRequest.Labels, chitinDispatchLabel)
	return r
}

func checkIssueCommentEvent(p *prPayload) prEligibility {
	r := prEligibility{}
	// issue_comment fires "created" / "edited" / "deleted". The
	// orchestrator only cares about "created" for now — re-evaluating
	// eligibility after a comment lets operators add the label via
	// /command in a thread.
	if p.Action != "created" {
		r.Reasons = append(r.Reasons, "not_draft_or_ready")
		return r
	}
	if !hasLabel(p.Issue.Labels, chitinDispatchLabel) {
		r.Reasons = append(r.Reasons, "missing_label")
	}
	issueNum, found := parseClosesReference(p.Issue.Body)
	if found {
		r.IssueNumber = issueNum
	} else {
		r.SpecRef = "unknown"
		r.Reasons = append(r.Reasons, "no_closes_reference")
	}
	r.Eligible = hasLabel(p.Issue.Labels, chitinDispatchLabel)
	return r
}

func hasLabel(labels []struct {
	Name string `json:"name"`
}, target string) bool {
	for _, l := range labels {
		if strings.EqualFold(l.Name, target) {
			return true
		}
	}
	return false
}

// parseClosesReference returns the first issue number referenced via a
// "Closes #N" (or fix/resolve variant) in the body. Returns (0, false)
// if no recognized reference is found.
func parseClosesReference(body string) (int, bool) {
	m := closesReferencePattern.FindStringSubmatch(body)
	if len(m) < 2 {
		return 0, false
	}
	var n int
	for _, c := range m[1] {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, n > 0
}

// prResponse is the JSON the /webhook/pr route returns per
// contracts/factory-listen-pr-events.md.
type prResponse struct {
	Received      bool   `json:"received"`
	EventType     string `json:"event_type"`
	Action        string `json:"action"`
	PRNumber      int    `json:"pr_number"`
	Eligible      bool   `json:"eligible"`
	ReviewStarted bool   `json:"review_started"`
	ReviewRunID   string `json:"review_run_id,omitempty"`
	DedupSkipped  bool   `json:"dedup_skipped"`
	SkippedReason string `json:"skipped_reason,omitempty"`
	// Spec 112 US2 — sibling-rebase dispatch summary, populated on a
	// chitin-authored merge that has open siblings.
	SiblingRebaseDispatched int   `json:"sibling_rebase_dispatched,omitempty"`
	SiblingRebaseSiblings   int   `json:"sibling_rebase_siblings,omitempty"`
	SiblingRebasePRs        []int `json:"sibling_rebase_prs,omitempty"`
	// Spec 113 US1 — PR iteration dispatch summary, populated on a Copilot
	// review against a chitin/wu/* branch.
	PRIterationDispatched bool   `json:"pr_iteration_dispatched,omitempty"`
	PRIterationWorkflowID string `json:"pr_iteration_workflow_id,omitempty"`
}
