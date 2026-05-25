// Package queue computes the spec 114 operator escalation surface.
//
// filter.go composes the live PR list (live.go) and the escalation-event
// index (scan.go) into the "needs operator" set per spec 114 FR-003. The
// rule taxonomy is the closed set declared in FR-008 (eight code-PR
// kinds) extended by spec 115 FR-010 (two spec-PR kinds) for a current
// total of ten — each with a single rule that returns
// `(matched bool, reason string)` so that downstream formatters
// (FR-005, FR-006, FR-007) can show WHY a PR is in the queue. The
// authoritative list of kinds is queue.ValidReasons (reason.go).
//
// Membership invariant (FR-003): a PR is in the queue iff AT LEAST ONE
// rule matches. When more than one rule matches, the FIRST match in the
// canonical FR-003 declaration order wins — that order is stable and
// puts chain-event signals (the strongest evidence the driver actually
// gave up on the PR) ahead of live-state heuristics (which can fire on
// transient GitHub state). This is the property that makes the queue
// readable to a human: the reason column reports the most actionable
// cause, not a noisy live-state symptom of a deeper chain event.
//
// Hiding invariant (FR-004): the filter does NOT compute the hidden set.
// FR-004 reads "iff NONE of the above hold AND ..." — a PR with zero
// matching rules is, by construction, not in the output. So the absence
// of an entry IS the hiding outcome; no separate hide pass is needed.
package queue

import (
	"sort"
	"strings"
	"time"
)

// staleReviewAge is the FR-003 `stale_no_automation` threshold: a PR
// whose latest review submission is older than this and which has had
// no orchestrator-authored commit since that review is treated as
// stalled and surfaced to the operator.
const staleReviewAge = 24 * time.Hour

// conflictingMinAge is the FR-003 `conflicting_persistent` debounce:
// `mergeable == "CONFLICTING"` is the transient state GitHub reports
// in the seconds after a merge, so the rule fires only when the PR
// has been sitting in CONFLICTING for longer than this. UpdatedAt is
// the closest signal `gh pr list` exposes; a fresher signal would
// require per-PR API roundtrips and would blow SC-002's 2-second
// budget for v1.
const conflictingMinAge = time.Hour

// iteratingLabel is the spec 113 marker that a PR is being actively
// iterated by the comment-respond loop. Its presence suppresses the
// human-reviewer-present fallback (FR-003 third bullet), because spec
// 113 is, by construction, the system already handling the human
// review on that PR.
const iteratingLabelPrefix = "chitin-iterating"

// botReviewerLogins is the closed set of GitHub login suffixes / exact
// names that we treat as automated reviewers — their reviews never
// trigger the `human_reviewer_present` fallback. The Copilot reviewer
// (spec 113's trigger) is the load-bearing entry; the rest are common
// GitHub Apps that show up on chitin PRs and would otherwise produce
// false "human reviewer" matches.
var botReviewerLogins = map[string]struct{}{
	"copilot":                       {},
	"copilot-pull-request-reviewer": {},
	"github-actions":                {},
	"dependabot":                    {},
	"renovate":                      {},
	"chitin-orchestrator":           {},
}

// QueueEntry is one PR that passed at least one FR-003 rule. Reason
// carries the canonical FR-008 reason kind from the rule that matched
// first. Event is the triggering chain event when the matching rule is
// chain-event-derived (the four `pr_iteration_escalated` reasons and
// `sibling_rebase_failed`); it is nil for live-state-derived rules
// (`dialectic_request_changes`, `stale_no_automation`,
// `conflicting_persistent`) so that callers can distinguish "this is
// what the driver said" from "this is what GitHub state implies".
type QueueEntry struct {
	PR     LivePR
	Reason string
	Event  *EscalationEvent
}

// ruleFn is the FR-003 rule shape: given a live PR, the chain events
// indexed for it, and the comparison "now", return whether the PR
// matches the rule and the canonical reason string (FR-008) when it
// does. A rule MUST return the same reason string every time it
// matches; the formatters key off it.
type ruleFn func(pr LivePR, events []EscalationEvent, now time.Time) (matched bool, reason string)

// rules is the ordered list of FR-003 rules. Order is the canonical
// FR-003 declaration order (chain-event reasons first, live-state
// reasons last) and is the tie-break for PRs that match more than one
// rule. Keeping it as a package-level slice makes the priority order
// reviewable in one place and lets tests assert the order is stable.
//
// Spec 115 T017 appends two spec-PR-specific rules at the tail. They
// match `spec_iteration_escalated` events (scan.go FR-009) and
// surface spec-PR escalations in the same queue as code-PR ones. The
// tail position preserves spec 114's FR-008 ordering invariant: a PR
// that matches BOTH a code-PR rule AND a spec-PR rule (which only
// happens on mixed-class PRs, an edge case the spec 115 discriminator
// routes to the code path anyway) keeps the original code-PR reason
// as its primary, which is what spec 114's operator expects.
var rules = []struct {
	reason string
	fn     ruleFn
}{
	{"iteration_cap_hit", ruleIterationCapHit},
	{"iteration_completed_with_skips", ruleIterationCompletedWithSkips},
	{"human_reviewer_present", ruleHumanReviewerPresent},
	{"sibling_rebase_failed", ruleSiblingRebaseFailed},
	{"lease_lost", ruleLeaseLost},
	{"dialectic_request_changes", ruleDialecticRequestChanges},
	{"stale_no_automation", ruleStaleNoAutomation},
	{"conflicting_persistent", ruleConflictingPersistent},
	{"design_judgement_required", ruleDesignJudgementRequired},
	{"lint_violation_unresolvable", ruleLintViolationUnresolvable},
}

// Filter composes prs and events into the FR-003 "needs operator" set.
//
// Each PR is evaluated against the rules in FR-003 declaration order;
// the first matching rule wins. PRs in the input but in no rule are
// dropped from the output (FR-004 hiding by absence).
//
// Output ordering is by PR number ascending. The deterministic order
// stabilises the formatter outputs (FR-005/006/007) so digests look the
// same across runs given the same input — the property SC-001's
// median-queue-size measurement relies on.
//
// now is the comparison time for the time-based rules
// (`stale_no_automation`, `conflicting_persistent`); injected for
// hermetic testing.
func Filter(prs []LivePR, events map[int][]EscalationEvent, now time.Time) []QueueEntry {
	out := make([]QueueEntry, 0, len(prs))
	for _, pr := range prs {
		entry, ok := EvaluatePR(pr, events[pr.Number], now)
		if !ok {
			continue
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].PR.Number < out[j].PR.Number
	})
	return out
}

// FilterByReason narrows Filter's output to entries whose Reason equals
// reason. Used by the `--reason KIND` CLI flag (FR-008). reason is
// expected to be one of the canonical reason kinds; an unknown value
// returns an empty slice — flag-level validation lives in T008, not
// here.
//
// Surrounding whitespace is trimmed to match ValidateReason's tolerance
// of stray shell-quoting spaces ("--reason 'iteration_cap_hit '"):
// without trimming, such an input would validate but then quietly
// produce an empty result here.
func FilterByReason(entries []QueueEntry, reason string) []QueueEntry {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return entries
	}
	out := make([]QueueEntry, 0, len(entries))
	for _, e := range entries {
		if e.Reason == reason {
			out = append(out, e)
		}
	}
	return out
}

// EvaluatePR runs the FR-003 rules against one PR and returns the first
// matching (entry, true) pair. When no rule matches, returns the zero
// entry and false. Exported so tests can pinpoint individual rule
// outcomes without going through Filter.
func EvaluatePR(pr LivePR, events []EscalationEvent, now time.Time) (QueueEntry, bool) {
	for _, r := range rules {
		matched, reason := r.fn(pr, events, now)
		if !matched {
			continue
		}
		return QueueEntry{
			PR:     pr,
			Reason: reason,
			Event:  latestEventByReason(events, reason),
		}, true
	}
	return QueueEntry{}, false
}

// ---- chain-event-backed rules (FR-003 bullets 1-5) -----------------

func ruleIterationCapHit(_ LivePR, events []EscalationEvent, _ time.Time) (bool, string) {
	if hasEscalationReason(events, "iteration_cap_hit") {
		return true, "iteration_cap_hit"
	}
	return false, ""
}

func ruleIterationCompletedWithSkips(_ LivePR, events []EscalationEvent, _ time.Time) (bool, string) {
	if hasEscalationReason(events, "iteration_completed_with_skips") {
		return true, "iteration_completed_with_skips"
	}
	return false, ""
}

// ruleHumanReviewerPresent implements the FR-003 bullet 3 dual signal:
// either the spec-113 driver emitted `pr_iteration_escalated` with
// reason `human_reviewer_present` (the system tried and gave up), OR
// — without spec 113 having engaged on this PR — a non-bot reviewer is
// present (so no automated handler ever ran). The iterating-label
// check encodes the "without spec 113 deployed" qualifier: if 113 is
// iterating on this PR, its outcome (handled or escalated) is the
// authoritative signal, and the bare-reviewer fallback would be noise.
func ruleHumanReviewerPresent(pr LivePR, events []EscalationEvent, _ time.Time) (bool, string) {
	if hasEscalationReason(events, "human_reviewer_present") {
		return true, "human_reviewer_present"
	}
	if hasLabelPrefix(pr.Labels, iteratingLabelPrefix) {
		return false, ""
	}
	for _, r := range pr.Reviews {
		if !isBotReviewer(r) {
			return true, "human_reviewer_present"
		}
	}
	return false, ""
}

func ruleSiblingRebaseFailed(_ LivePR, events []EscalationEvent, _ time.Time) (bool, string) {
	for _, e := range events {
		if e.EventType == "sibling_rebase_failed" {
			return true, "sibling_rebase_failed"
		}
	}
	return false, ""
}

func ruleLeaseLost(_ LivePR, events []EscalationEvent, _ time.Time) (bool, string) {
	if hasEscalationReason(events, "lease_lost") {
		return true, "lease_lost"
	}
	return false, ""
}

// ---- spec 115 chain-event-backed rules (FR-010) --------------------

// ruleDesignJudgementRequired fires when scan.go observed a
// `spec_iteration_escalated` event whose payload reason is
// `design_judgement_required` (spec 115 FR-008): every Copilot comment
// in the round classified as design-judgement, so the workflow skipped
// driver dispatch and handed the round to the operator. The reason
// kind is unique to `spec_iteration_escalated` — `pr_iteration_escalated`
// never produces it — so a bare reason check is sufficient and
// consistent with how the code-PR rules above check by reason rather
// than re-deriving the event_type.
func ruleDesignJudgementRequired(_ LivePR, events []EscalationEvent, _ time.Time) (bool, string) {
	if hasEscalationReason(events, "design_judgement_required") {
		return true, "design_judgement_required"
	}
	return false, ""
}

// ruleLintViolationUnresolvable fires when scan.go observed a
// `spec_iteration_escalated` event whose payload reason is
// `lint_violation_unresolvable` (spec 115 FR-010): the spec-tuned
// driver couldn't fix a deterministic linter violation and didn't
// justify patching the `.specify/known-cli-surfaces.txt` /
// `.specify/judgement-phrases.txt` allowlist. The operator's
// resolution is usually a one-line allowlist patch or a quick spec
// fix the driver couldn't reach.
func ruleLintViolationUnresolvable(_ LivePR, events []EscalationEvent, _ time.Time) (bool, string) {
	if hasEscalationReason(events, "lint_violation_unresolvable") {
		return true, "lint_violation_unresolvable"
	}
	return false, ""
}

// ---- live-state-backed rules (FR-003 bullets 6-8) ------------------

// ruleDialecticRequestChanges fires when a spec-094 dialectic verdict
// has been recorded as a CHANGES_REQUESTED PR review. The dialectic
// invariant (verdict/invariants.go) guarantees that request-changes
// verdicts carry non-empty Blockers, so the GitHub-side review state
// is the load-bearing signal — we do not need to refetch the verdict
// to confirm Blockers.
func ruleDialecticRequestChanges(pr LivePR, _ []EscalationEvent, _ time.Time) (bool, string) {
	for _, r := range pr.Reviews {
		if strings.EqualFold(r.State, "CHANGES_REQUESTED") {
			return true, "dialectic_request_changes"
		}
	}
	return false, ""
}

// ruleStaleNoAutomation fires when a PR has at least one review whose
// most recent submission is older than 24h and no orchestrator commit
// has landed since that review. PRs with zero reviews are NOT in scope
// — FR-004 explicitly hides "no review at all yet (still authoring)".
func ruleStaleNoAutomation(pr LivePR, _ []EscalationEvent, now time.Time) (bool, string) {
	latestReview, ok := mostRecentReviewTime(pr.Reviews)
	if !ok {
		return false, ""
	}
	if now.Sub(latestReview) <= staleReviewAge {
		return false, ""
	}
	if pr.LastAutomatedCommit.IsZero() {
		return true, "stale_no_automation"
	}
	if pr.LastAutomatedCommit.Before(latestReview) {
		return true, "stale_no_automation"
	}
	return false, ""
}

// ruleConflictingPersistent fires only when CONFLICTING has persisted
// past the conflictingMinAge debounce — protecting the queue from the
// transient post-merge CONFLICTING state GitHub briefly reports while
// it recomputes mergeability. UpdatedAt is the freshest proxy for
// "how long has this state held" available from `gh pr list`.
func ruleConflictingPersistent(pr LivePR, _ []EscalationEvent, now time.Time) (bool, string) {
	if !strings.EqualFold(pr.Mergeable, "CONFLICTING") {
		return false, ""
	}
	if pr.UpdatedAt.IsZero() {
		return false, ""
	}
	if now.Sub(pr.UpdatedAt) < conflictingMinAge {
		return false, ""
	}
	return true, "conflicting_persistent"
}

// ---- shared helpers -------------------------------------------------

// hasEscalationReason returns true when events contains any entry with
// the given canonical reason. Used by the four
// `pr_iteration_escalated` rules; sibling_rebase_failed has its own
// inline event_type check.
func hasEscalationReason(events []EscalationEvent, reason string) bool {
	for _, e := range events {
		if e.Reason == reason {
			return true
		}
	}
	return false
}

// hasLabelPrefix returns true when any label name starts with prefix.
// The `chitin-iterating` family is the only multi-label use today (
// `chitin-iterating/active` is the canonical one but variants may
// emerge), so we match by prefix to be forward-compatible.
func hasLabelPrefix(labels []Label, prefix string) bool {
	for _, l := range labels {
		if strings.HasPrefix(l.Name, prefix) {
			return true
		}
	}
	return false
}

// isBotReviewer returns true for reviews whose author is a known
// automated reviewer or whose login carries the GitHub `[bot]` suffix.
// Author-association isn't reliable for this — Copilot reviews can
// arrive as NONE or as MEMBER depending on installation — so we key
// off the login string directly.
func isBotReviewer(r Review) bool {
	login := strings.ToLower(strings.TrimSpace(r.Author.Login))
	if login == "" {
		return false
	}
	if strings.HasSuffix(login, "[bot]") {
		return true
	}
	if _, ok := botReviewerLogins[login]; ok {
		return true
	}
	return false
}

// mostRecentReviewTime returns the submittedAt of the latest review in
// reviews and true; or the zero time and false when reviews is empty
// or every entry has the zero submittedAt.
func mostRecentReviewTime(reviews []Review) (time.Time, bool) {
	var latest time.Time
	var found bool
	for _, r := range reviews {
		if r.SubmittedAt.IsZero() {
			continue
		}
		if r.SubmittedAt.After(latest) {
			latest = r.SubmittedAt
			found = true
		}
	}
	return latest, found
}

// latestEventByReason returns a pointer to the most recent event in
// events whose canonical reason equals reason, or nil when none
// matches. Used to populate QueueEntry.Event so the JSON formatter
// (FR-007) can surface the raw triggering payload alongside the row.
// Tie-break on equal Ts is by RunID lexicographic order to keep the
// pick stable across re-runs of the scanner.
func latestEventByReason(events []EscalationEvent, reason string) *EscalationEvent {
	var pick *EscalationEvent
	for i := range events {
		e := &events[i]
		if e.Reason != reason {
			continue
		}
		if pick == nil {
			pick = e
			continue
		}
		if e.Ts.After(pick.Ts) {
			pick = e
			continue
		}
		if e.Ts.Equal(pick.Ts) && e.RunID > pick.RunID {
			pick = e
		}
	}
	return pick
}
