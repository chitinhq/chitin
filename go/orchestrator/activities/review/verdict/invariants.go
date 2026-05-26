package verdict

import (
	"errors"
	"fmt"
)

// ValidationError is returned by Validate when a StructuredVerdict violates
// one of the four per-enum invariants from spec 094 FR-014. Each instance
// names the violated invariant in its Detail field — the orchestrator
// records the detail in workflow history so the malformed verdict can be
// post-mortemed without a separate audit fetch.
type ValidationError struct {
	// Invariant identifies which FR-014 rule failed.
	Invariant string
	// Detail is the human-readable reason — e.g., "approve verdict must
	// have empty blockers (got 2)".
	Detail string
}

// Error renders the validation error as "<invariant>: <detail>" so the
// invariant name is always present in the message.
func (e *ValidationError) Error() string {
	return e.Invariant + ": " + e.Detail
}

// newError is a small helper to keep the Validate body uncluttered. It
// returns a *ValidationError so callers can errors.As() on it.
func newError(invariant, format string, args ...any) error {
	return &ValidationError{Invariant: invariant, Detail: fmt.Sprintf(format, args...)}
}

// ErrUnknownEnum is returned when Validate sees an enum value that is not
// one of the four declared constants. Callers use errors.Is() to detect it.
var ErrUnknownEnum = errors.New("verdict: unknown enum value")

// Validate enforces the four per-enum invariants from spec 094 FR-014:
//
//  1. approve ⇒ blockers empty.
//  2. approve-with-comments ⇒ blockers empty AND (concerns OR recommendations non-empty).
//  3. request-changes ⇒ blockers non-empty.
//  4. abstain ⇒ concerns, recommendations, blockers all empty.
//
// It also rejects any unknown enum value (wrapping ErrUnknownEnum) and any
// list entry that is the empty string — empty strings carry no audit value,
// so the schema treats them as malformed rather than equivalent to "no
// entry."
//
// Returns nil on a valid verdict, a *ValidationError otherwise.
func Validate(v StructuredVerdict) error {
	if !v.Verdict.Valid() {
		return fmt.Errorf("%w: %q", ErrUnknownEnum, string(v.Verdict))
	}
	if !v.Confidence.Valid() {
		return newError("confidence_invalid",
			"confidence must be one of high|medium|low (or empty for default-medium); got %q",
			string(v.Confidence))
	}
	if err := requireNonEmptyEntries("concerns", v.Concerns); err != nil {
		return err
	}
	if err := requireNonEmptyEntries("recommendations", v.Recommendations); err != nil {
		return err
	}
	if err := requireNonEmptyEntries("blockers", v.Blockers); err != nil {
		return err
	}
	switch v.Verdict {
	case Approve:
		if len(v.Blockers) != 0 {
			return newError("approve_blockers_must_be_empty",
				"approve verdict must have empty blockers (got %d)", len(v.Blockers))
		}
	case ApproveWithComments:
		if len(v.Blockers) != 0 {
			return newError("approve_with_comments_blockers_must_be_empty",
				"approve-with-comments verdict must have empty blockers (got %d)", len(v.Blockers))
		}
		if len(v.Concerns) == 0 && len(v.Recommendations) == 0 {
			return newError("approve_with_comments_requires_concerns_or_recommendations",
				"approve-with-comments verdict must have at least one concern or recommendation")
		}
	case RequestChanges:
		if len(v.Blockers) == 0 {
			return newError("request_changes_requires_blockers",
				"request-changes verdict must have non-empty blockers")
		}
	case Abstain:
		if len(v.Concerns) != 0 || len(v.Recommendations) != 0 || len(v.Blockers) != 0 {
			return newError("abstain_lists_must_be_empty",
				"abstain verdict must have empty concerns/recommendations/blockers (got %d/%d/%d)",
				len(v.Concerns), len(v.Recommendations), len(v.Blockers))
		}
	}
	return nil
}

// requireNonEmptyEntries enforces that every string in a verdict list is
// non-empty. Empty strings in a blocker/concern/recommendation list carry
// no information and are treated as a schema violation, not as missing.
func requireNonEmptyEntries(field string, entries []string) error {
	for i, s := range entries {
		if s == "" {
			return newError(field+"_entry_must_be_non_empty",
				"%s[%d] must be a non-empty string", field, i)
		}
	}
	return nil
}
