package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// FindingKind classifies an analyzed observation by its shape (spec 078 Key
// Entities: Finding). It is a closed enumeration — every analysis pass emits
// exactly one of these kinds.
type FindingKind string

const (
	// FindingRecurringFailure is a failure pattern that repeats across the
	// telemetry window — the same Signature failing more than once. It is the
	// canonical input to a spec proposal (spec 078 US1 acceptance scenario 1).
	FindingRecurringFailure FindingKind = "recurring_failure"
	// FindingMissedOpportunity is an observation that the platform could have
	// done better — work that succeeded but slowly, or a manual step that a
	// deterministic node could absorb.
	FindingMissedOpportunity FindingKind = "missed_opportunity"
	// FindingRegression marks a previously-approved-and-implemented proposal
	// whose intent later failed in new telemetry (spec 078 FR-016).
	//
	// TODO(spec-078-US3): regression detection — correlating new failing
	// telemetry against the spec a prior proposal targeted — is implemented in
	// analysis.go under Phase 5, T026. The kind is declared here so the
	// Finding type is complete for every story.
	FindingRegression FindingKind = "regression"
)

// Finding is an analyzed observation derived from a TelemetryWindow — a
// recurring failure, a missed opportunity, or a regression — carrying the
// specific telemetry records that evidence it (spec 078 Key Entities:
// Finding, FR-004).
//
// A Finding is a pure value produced by the analysis passes (analysis.go). It
// is the input to proposal synthesis: every spec proposal carries the Finding
// that grounds it, so the operator reviews a claim with its proof.
type Finding struct {
	// Kind is the shape of the observation.
	Kind FindingKind `json:"kind"`
	// SpecRef is the chitin spec the observation is about — the spec a
	// proposal derived from this finding would target (spec 078 FR-003). A
	// finding with no SpecRef cannot become a proposal: a proposal MUST name a
	// spec (see proposal.go / SynthesizeProposal).
	SpecRef string `json:"spec_ref"`
	// Signature is the stable, normalized fingerprint of WHAT was observed —
	// the failing command, the error class, the denied category. It is the
	// core of the finding's identity: two findings with the same Kind,
	// SpecRef, and Signature are the SAME finding (see Identity / SameAs).
	Signature string `json:"signature"`
	// Summary is a one-line human-readable description of the observation.
	Summary string `json:"summary"`
	// Occurrences is how many telemetry records evidence this finding — for a
	// recurring failure, how many times the signature recurred. It is a count,
	// never a heuristic score (spec 078 Out of Scope: no ranking optimizer).
	Occurrences int `json:"occurrences"`
	// Category is the gate-able action category a proposal addressing this
	// finding would propose work within (spec 078 FR-007). Synthesis refuses a
	// finding whose Category is not in the closed gate-able set.
	Category GateableCategory `json:"category"`
	// Evidence is the specific telemetry records that ground the finding —
	// carried verbatim so the operator can follow each back to raw telemetry
	// (spec 078 FR-004). It is ordered deterministically by the analysis pass.
	Evidence []TelemetryRecord `json:"evidence"`
}

// Identity returns the stable identity of a finding — a hex digest over its
// Kind, SpecRef, and Signature. Two findings with the same identity describe
// the SAME underlying observation, even when their evidence sets differ
// (because the failure recurred and accumulated new records).
//
// Identity is what duplicate suppression keys on: a recurring finding whose
// proposal is still pending must NOT be re-queued — its new evidence is
// attached to the existing pending proposal instead (spec 078 FR-014, SC-006).
// Hashing — rather than concatenating — yields a fixed-width key safe to use
// as a map key and as a proposal id stem regardless of signature length.
func (f Finding) Identity() string {
	h := sha256.New()
	// A NUL separator keeps the three fields unambiguous — no field value can
	// contain a NUL, so ("a","b","c") never collides with ("ab","","c").
	h.Write([]byte(f.Kind))
	h.Write([]byte{0})
	h.Write([]byte(f.SpecRef))
	h.Write([]byte{0})
	h.Write([]byte(f.Signature))
	return hex.EncodeToString(h.Sum(nil)[:16]) // 16 bytes → 32 hex chars.
}

// SameAs reports whether two findings describe the same underlying
// observation — the duplicate-matching predicate. It compares Identity, so it
// is independent of the two findings' evidence sets and occurrence counts.
func (f Finding) SameAs(other Finding) bool {
	return f.Identity() == other.Identity()
}

// IsProposable reports whether a finding can become a spec proposal at all. A
// finding MUST name a spec (a proposal is a change against a NAMED spec —
// FR-003) and MUST carry a gate-able category (a proposal is refused outside
// the gate-able set — FR-007). A finding failing either is dropped at
// synthesis, never forced into a vague or ungated proposal.
func (f Finding) IsProposable() bool {
	return f.SpecRef != "" && IsGateable(f.Category)
}

// SortFindings orders a slice of findings deterministically: SpecRef, then
// Kind, then Signature as the final stable tie-breaker. The analysis passes
// call it so a cycle's findings — and therefore its proposals — are emitted
// in a replay-stable order (spec 078 plan Constraints: workflow determinism).
func SortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.SpecRef != b.SpecRef {
			return a.SpecRef < b.SpecRef
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Signature < b.Signature
	})
}

// MergeEvidence folds a recurring finding's new evidence into an existing
// finding, de-duplicating by telemetry-record ID, and returns the merged
// finding. This is the duplicate-suppression primitive (spec 078 FR-014): when
// a still-pending finding recurs, its fresh evidence is ATTACHED to the
// existing finding rather than a duplicate finding being created.
//
// The merge is pure and deterministic: the result's evidence is the union of
// both inputs' records keyed by ID, sorted by the canonical record ordering,
// and Occurrences is recomputed as the merged evidence count. The receiver's
// identity fields (Kind, SpecRef, Signature, Category, Summary) are kept — a
// merge never changes WHICH finding this is, only how much evidence it has.
func (f Finding) MergeEvidence(recurrence Finding) Finding {
	seen := make(map[string]struct{}, len(f.Evidence)+len(recurrence.Evidence))
	merged := make([]TelemetryRecord, 0, len(f.Evidence)+len(recurrence.Evidence))
	for _, ev := range append(append([]TelemetryRecord(nil), f.Evidence...), recurrence.Evidence...) {
		if _, dup := seen[ev.ID]; dup {
			continue
		}
		seen[ev.ID] = struct{}{}
		merged = append(merged, ev)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		a, b := merged[i], merged[j]
		if !a.Timestamp.Equal(b.Timestamp) {
			return a.Timestamp.Before(b.Timestamp)
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.ID < b.ID
	})
	out := f
	out.Evidence = merged
	out.Occurrences = len(merged)
	return out
}
