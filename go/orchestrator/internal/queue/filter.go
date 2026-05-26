package queue

import (
	"strings"
	"time"
)

// FR-003 thresholds — pinned in code (not config) because the queue's
// utility hinges on operators trusting that "stale" and "conflicting"
// mean the same thing every time they read the queue. Drift here would
// invalidate the SC-001 "median queue size over 7 days" metric.
const (
	// staleNoAutomationThreshold is the FR-003 cutoff for the
	// stale_no_automation rule: a chitin-authored PR whose head hasn't
	// received an orchestrator commit in this long is escalated.
	staleNoAutomationThreshold = 24 * time.Hour
	// conflictingPersistentThreshold is the FR-003 cutoff for the
	// conflicting_persistent rule: a PR whose Mergeable=CONFLICTING
	// state has persisted past this window is escalated.
	conflictingPersistentThreshold = 1 * time.Hour
)

// chitinAuthoredBranchPrefix marks PR head refs the factory authored.
// stale_no_automation only fires on chitin-authored PRs — a human PR
// being old is not an escalation, it is normal review backlog.
const chitinAuthoredBranchPrefix = "chitin/wu/"

// liveRuleOrder is the deterministic priority of the live-state rules.
// Matters when a single PR matches more than one live rule: the FIRST
// match wins so the chain reason rendered to the operator is stable
// across invocations. Mirrors FR-003's listed-in-spec order.
var liveRuleOrder = []string{
	"dialectic_request_changes",
	"stale_no_automation",
	"conflicting_persistent",
}

// Build composes []Entry from the chain scan results and the live PR
// snapshot per FR-003. A PR surfaces in at most one Entry — when both a
// chain event and a live rule match the same PR, the chain reason wins
// (the chain carries explicit operator-action signals; the live rules
// are inferences from PR state). Result order: chain-derived entries in
// PR-number order, then live-rule-derived entries in PR-number order.
//
// Pure function — no IO. Callers (queue.go cmd, operator-digest job)
// supply scan + fetchLive outputs and a wall-clock "now" so the rule
// thresholds are deterministic.
func Build(chain map[int][]EscalationEvent, live []LivePR, now time.Time) []Entry {
	// 1. Chain-derived entries: one per PR that has at least one
	// escalation event in window. Reason = the FIRST event for that PR
	// (Scan preserves file/line order; for multi-event PRs the operator
	// triages with the earliest signal as the entry-point).
	liveByPR := indexLiveByPR(live)
	var out []Entry
	chainPRs := sortedKeys(chain)
	for _, pr := range chainPRs {
		events := chain[pr]
		if len(events) == 0 {
			continue
		}
		if pr == 0 {
			for i := range events {
				ev := events[i]
				out = append(out, makeEntry(0, ev.Reason, nil, &ev))
			}
			continue
		}
		first := events[0]
		out = append(out, makeEntry(pr, first.Reason, liveByPR[pr], &first))
	}

	// 2. Live-rule-derived entries: each open PR is evaluated against
	// the three live rules in liveRuleOrder. Skip PRs already in `out`
	// from the chain pass (no double-surfacing).
	chainSet := map[int]bool{}
	for _, e := range out {
		chainSet[e.PRNumber] = true
	}
	var liveOut []Entry
	for _, p := range live {
		if chainSet[p.Number] {
			continue
		}
		reason, ok := matchLiveRules(p, now)
		if !ok {
			continue
		}
		liveOut = append(liveOut, makeEntry(p.Number, reason, &p, nil))
	}

	// Sort the two halves separately by PR number for deterministic
	// output. The two halves are concatenated chain-first so an operator
	// scanning top-down sees explicit chain signals before inferred state.
	sortEntriesByPRNumber(out)
	sortEntriesByPRNumber(liveOut)
	return append(out, liveOut...)
}

// matchLiveRules evaluates the three FR-003 live rules in liveRuleOrder
// and returns the FIRST matching reason kind. Returns ("", false) when
// no live rule matches — the PR is clean and should be hidden (FR-004).
func matchLiveRules(p LivePR, now time.Time) (string, bool) {
	for _, rule := range liveRuleOrder {
		switch rule {
		case "dialectic_request_changes":
			if hasRequestChangesReview(p.Reviews) {
				return rule, true
			}
		case "stale_no_automation":
			// A chitin-authored PR is stale when the most recent
			// orchestrator commit is older than the threshold OR the PR
			// has no orchestrator commits at all. The second case is
			// gated by the PR itself being older than the threshold —
			// otherwise a freshly-opened chitin PR (no commits yet by
			// design) would surface immediately, drowning the queue.
			if !isChitinAuthored(p.HeadRefName) {
				continue
			}
			noAutomationEver := p.LastAutomatedCommitAt == nil
			autoIsStale := p.LastAutomatedCommitAt != nil &&
				now.Sub(*p.LastAutomatedCommitAt) > staleNoAutomationThreshold
			prItselfIsOld := now.Sub(p.UpdatedAt) > staleNoAutomationThreshold
			if autoIsStale || (noAutomationEver && prItselfIsOld) {
				return rule, true
			}
		case "conflicting_persistent":
			if strings.EqualFold(p.Mergeable, "CONFLICTING") &&
				now.Sub(p.UpdatedAt) > conflictingPersistentThreshold {
				return rule, true
			}
		}
	}
	return "", false
}

// hasRequestChangesReview reports whether any review on this PR carries
// the GitHub REQUEST_CHANGES state. The dialectic-class escalation fires
// when at least one reviewer (Copilot or human) is actively blocking;
// resolved-then-re-approved is signalled by a later APPROVED review,
// which is NOT what FR-003 asks about — the rule fires on the CURRENT
// presence of any REQUEST_CHANGES verdict regardless of subsequent
// reviews. Matches `gh pr list --json reviews` shape.
func hasRequestChangesReview(reviews []Review) bool {
	for _, r := range reviews {
		if strings.EqualFold(r.State, "REQUEST_CHANGES") ||
			strings.EqualFold(r.State, "CHANGES_REQUESTED") {
			return true
		}
	}
	return false
}

// isChitinAuthored reports whether headRef matches the factory's
// chitin/wu/<spec>-<task>-<suffix> convention. Used to gate
// stale_no_automation per the rule definition: only chitin-authored PRs
// escalate on lack of recent auto-action; a stale human PR is normal
// backlog and is not the queue's concern.
func isChitinAuthored(headRef string) bool {
	return strings.HasPrefix(headRef, chitinAuthoredBranchPrefix)
}

// makeEntry constructs one Entry from a (PRNumber, reason, optional
// live snapshot, optional triggering chain event). The PR may not be
// in the live snapshot (e.g. closed between scan and live fetch) — in
// that case Title/URL/UpdatedAt remain zero and the renderer shows the
// PR number only. SpecRef and LastAutoActionAt come from the live
// snapshot when present.
func makeEntry(prNumber int, reason string, live *LivePR, trig *EscalationEvent) Entry {
	e := Entry{
		PRNumber: prNumber,
		Reason:   reason,
	}
	if live != nil {
		e.Title = live.Title
		e.SpecRef = live.SpecRef
		e.UpdatedAt = live.UpdatedAt
		if live.LastAutomatedCommitAt != nil {
			e.LastAutoActionAt = *live.LastAutomatedCommitAt
		}
	}
	if trig != nil {
		e.TriggeringEvent = trig
		if trig.Reason == "silent_drop" {
			if trig.PRNumber == 0 {
				e.PRNumber = 0
			}
			if trig.TaskID != "" {
				e.TaskID = trig.TaskID
			}
			if trig.SpecRef != "" {
				e.SpecRef = trig.SpecRef
			}
			// No live PR snapshot to anchor AGE; fall back to the event's
			// own timestamp so the row renders with a real age + non-zero JSON.
			if live == nil && !trig.Ts.IsZero() {
				e.UpdatedAt = trig.Ts
			}
		}
	}
	return e
}

// indexLiveByPR returns a lookup by PR number. Used to join chain events
// to their corresponding live PR snapshot so the entry carries
// title/spec-ref/timestamps even on the chain-derived branch.
func indexLiveByPR(live []LivePR) map[int]*LivePR {
	out := make(map[int]*LivePR, len(live))
	for i := range live {
		out[live[i].Number] = &live[i]
	}
	return out
}

// sortedKeys returns the integer keys of m sorted ascending. Used for
// deterministic iteration order over chain results.
func sortedKeys(m map[int][]EscalationEvent) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Tiny insertion sort — keys is bounded by the number of PRs that
	// have escalation events in the window (~tens to low hundreds at the
	// operator-host scale this queue serves).
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

// sortEntriesByPRNumber sorts s in place by PRNumber ascending. Same
// in-place insertion-sort rationale as sortedKeys — small slices, no
// allocation overhead.
func sortEntriesByPRNumber(s []Entry) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].PRNumber > s[j].PRNumber; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
