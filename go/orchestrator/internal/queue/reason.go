package queue

import (
	"fmt"
	"strings"
)

// FR-008 closed reason taxonomy — the union of every reason kind the
// queue ever surfaces. Used by the --reason CLI flag to narrow output
// AND by the `unknown reason` error message at the CLI boundary. Order
// matches FR-008's listed order: chain-derived first, then live-derived.
var fr008ReasonTaxonomy = []string{
	// Chain-derived (spec 113 FR-011 + spec 112 US2):
	"iteration_cap_hit",
	"iteration_completed_with_skips",
	"human_reviewer_present",
	"lease_lost",
	"sibling_rebase_failed",
	"silent_drop",
	"stale_report",
	// Live-state-derived (filter.go):
	"dialectic_request_changes",
	"stale_no_automation",
	"conflicting_persistent",
}

// fr008ReasonSet mirrors fr008ReasonTaxonomy as a lookup map for the
// validator. Built once at init time so ValidateReasonKind is allocation
// free.
var fr008ReasonSet = func() map[string]struct{} {
	out := make(map[string]struct{}, len(fr008ReasonTaxonomy))
	for _, r := range fr008ReasonTaxonomy {
		out[r] = struct{}{}
	}
	return out
}()

// ValidateReasonKind reports whether kind is in the FR-008 closed
// taxonomy. The empty string is treated as valid (no filter). Returns
// a formatted error naming every accepted value when kind is rejected,
// so the operator sees the full list at the CLI surface.
//
// Per spec 114 T008 — "validate against the closed reason taxonomy
// (FR-008); error helpfully on unknown kinds."
func ValidateReasonKind(kind string) error {
	if kind == "" {
		return nil
	}
	if _, ok := fr008ReasonSet[kind]; ok {
		return nil
	}
	return fmt.Errorf("unknown reason kind %q; valid kinds: %s",
		kind, strings.Join(fr008ReasonTaxonomy, ", "))
}

// FilterByReason returns the subset of entries whose Reason equals kind.
// kind == "" returns entries unchanged. The function does NOT validate
// kind — callers (queue.go) MUST run ValidateReasonKind first so the
// rejection happens at the CLI boundary, not inside the renderer chain.
func FilterByReason(entries []Entry, kind string) []Entry {
	if kind == "" {
		return entries
	}
	out := entries[:0:0]
	for _, e := range entries {
		if e.Reason == kind {
			out = append(out, e)
		}
	}
	return out
}

// ReasonTaxonomy returns a copy of the FR-008 reason kind list. Used by
// CLI help text and the digest job's reason-breakdown column so both
// stay in sync with the canonical list.
func ReasonTaxonomy() []string {
	out := make([]string, len(fr008ReasonTaxonomy))
	copy(out, fr008ReasonTaxonomy)
	return out
}
