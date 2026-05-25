// reason.go — spec 114 T008. Closed taxonomy + validation for the
// `chitin-orchestrator queue --reason KIND` filter.
//
// The taxonomy is the SOURCE-OF-TRUTH list referenced by FR-008 and
// FR-003: every value here corresponds 1-to-1 to a rule in the filter
// (T004) and to a `payload.reason` string produced by spec 113
// FR-011's `pr_iteration_escalated` events plus spec 112 US2's
// `sibling_rebase_failed` event_type. Spec 115 T017 extends the
// taxonomy with two `spec_iteration_escalated` reasons (FR-010) so
// spec-PR escalations land in the same operator queue as code-PR
// ones. Keep the lists synchronised: adding a new escalation reason
// MUST land here AND in the filter AND in scan.go's classifier.
//
// The flag itself is parsed in cmd/chitin-orchestrator/queue.go (T001);
// this file provides the validator that file calls before doing any
// chain-scan or gh-list work, so the operator gets the helpful error
// immediately rather than after a 2-second scan.

package queue

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ValidReasons is the closed FR-008 taxonomy in spec-listed order.
// Iterate this slice (not the map) when rendering — the spec order is
// meaningful (most-common operator triage path first).
//
// Spec 115 FR-010 appends two spec-PR-specific kinds carried by
// `spec_iteration_escalated`:
//
//   - design_judgement_required   — spec 115 FR-007 classifier flagged
//     a Copilot comment as judgement, not mechanical; operator
//     adjudicates rather than the driver iterating
//   - lint_violation_unresolvable — driver couldn't fix the
//     deterministic linter violation and didn't justify patching the
//     allowlist
//
// They land at the tail so the existing FR-008 order (the spec 114
// code-PR triage path) renders unchanged in `queue` output — the
// spec-PR escalations sort after the code-PR ones, matching the
// operator's morning-triage instinct of clearing the fast mechanical
// signals first.
var ValidReasons = []string{
	"iteration_cap_hit",
	"iteration_completed_with_skips",
	"human_reviewer_present",
	"sibling_rebase_failed",
	"lease_lost",
	"dialectic_request_changes",
	"stale_no_automation",
	"conflicting_persistent",
	"design_judgement_required",
	"lint_violation_unresolvable",
}

// validReasonSet is the O(1) lookup index built once at package init.
var validReasonSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(ValidReasons))
	for _, r := range ValidReasons {
		m[r] = struct{}{}
	}
	return m
}()

// ErrUnknownReason is the sentinel returned by ValidateReason for any
// non-empty input that is not in ValidReasons. Callers that need to
// distinguish "unknown kind" from other CLI errors can errors.Is it;
// the wrapped error message names the offending value and lists the
// valid kinds.
var ErrUnknownReason = errors.New("unknown reason kind")

// IsValidReason reports whether s is in the closed FR-008 taxonomy.
// The empty string is NOT valid here — callers that treat an empty
// --reason as "no filter" must short-circuit before calling this.
func IsValidReason(s string) bool {
	_, ok := validReasonSet[s]
	return ok
}

// ValidateReason returns nil for either the empty string (interpreted
// as "no --reason filter") or any value in ValidReasons. For an
// unknown non-empty value, it returns an error wrapping
// ErrUnknownReason whose message names the bad value and lists every
// valid kind in alphabetical order so the operator can grep or fix
// without consulting the spec.
//
// Inputs are matched case-sensitively against the canonical
// lowercase-snake kinds. Surrounding whitespace is trimmed so a stray
// shell-quoting space ("--reason 'iteration_cap_hit '") doesn't
// confuse the operator with an unhelpful "unknown kind".
func ValidateReason(s string) error {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	if _, ok := validReasonSet[trimmed]; ok {
		return nil
	}
	return fmt.Errorf("%w %q; valid kinds: %s", ErrUnknownReason, trimmed, validReasonsSortedCSV())
}

// validReasonsSortedCSV returns the closed taxonomy as a
// comma-separated alphabetical string for error messages. Alphabetical
// (not spec-order) so the operator can scan to find their kind without
// reading the whole list.
func validReasonsSortedCSV() string {
	sorted := make([]string, len(ValidReasons))
	copy(sorted, ValidReasons)
	sort.Strings(sorted)
	return strings.Join(sorted, ", ")
}
