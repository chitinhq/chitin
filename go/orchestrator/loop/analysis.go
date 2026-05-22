package loop

import "sort"

// Analyzer turns a TelemetryWindow into Findings (spec 078 Key Entities:
// Finding; FR-001). It is the loop's analysis step.
//
// Analyzer is an INTERFACE so the analysis step is pluggable: US1 ships the
// DeterministicAnalyzer below — a pure, mappable, zero-frontier-token pass that
// the spec calls for as the deterministic-review tier (spec 078 FR-008, US2).
// Later a small-local-model analyzer (FR-010) plugs in behind the same seam.
//
// Analyze MUST be deterministic: the same window yields the same findings in
// the same order, every call. The loop workflow relies on this — the analysis
// step runs inside (or is driven from) a Temporal workflow, so a
// non-deterministic analyzer would break replay (spec 078 plan Constraints).
type Analyzer interface {
	// Analyze detects findings in a telemetry window. It returns the findings
	// in a deterministic order (SortFindings); an empty or unreachable window
	// yields no findings — a valid empty cycle, never an error.
	Analyze(window TelemetryWindow) []Finding
}

// recurringFailureThreshold is the number of times one failure signature must
// appear within a window before the loop treats it as a RECURRING failure
// worth a finding. A single failure is noise; a repeat is a pattern (spec 078
// US1: "analyzes it for recurring failures"). Two is the smallest count that
// is, by definition, a recurrence.
const recurringFailureThreshold = 2

// DeterministicAnalyzer is the US1 analysis pass — a pure, deterministic
// detector with zero frontier-token cost (spec 078 FR-008, the
// deterministic-review tier US2 anticipates). It runs two passes over a
// telemetry window:
//
//   - recurring-failure detection: groups failure-outcome records by
//     (SpecRef, Signature) and emits a FindingRecurringFailure for every group
//     whose count reaches recurringFailureThreshold (US1 acceptance scenario 1).
//   - missed-opportunity detection: a deliberately conservative pass that, in
//     this US1 slice, surfaces nothing — the recurring-failure arc is the
//     irreducible core. The pass exists so the Analyzer contract is complete.
//
// It carries no state and no Temporal type — it is exhaustively unit-testable
// by plain `go test` (spec 078 plan Structure Decision: keep analysis pure).
type DeterministicAnalyzer struct {
	// CategoryFor maps a finding's failure signature/kind to the gate-able
	// action category a proposal addressing it would propose work within
	// (spec 078 FR-007). It is injected so the closed category mapping is a
	// configuration of the analyzer, not hard-coded into detection. A nil
	// CategoryFor falls back to defaultCategoryFor.
	CategoryFor func(rec TelemetryRecord) GateableCategory
}

// NewDeterministicAnalyzer returns the US1 deterministic analyzer. A nil
// categoryFor uses defaultCategoryFor — the conservative default mapping.
func NewDeterministicAnalyzer(categoryFor func(TelemetryRecord) GateableCategory) *DeterministicAnalyzer {
	return &DeterministicAnalyzer{CategoryFor: categoryFor}
}

// Analyze runs the deterministic analysis passes over a window and returns the
// findings in canonical order. An empty window yields no findings — a valid
// empty cycle (spec 078 edge case: empty / unreachable window).
func (a *DeterministicAnalyzer) Analyze(window TelemetryWindow) []Finding {
	if window.Empty() {
		return nil
	}
	findings := a.detectRecurringFailures(window)
	findings = append(findings, a.detectMissedOpportunities(window)...)
	SortFindings(findings)
	return findings
}

// detectRecurringFailures groups the window's failure records by
// (SpecRef, Signature) and emits one FindingRecurringFailure per group whose
// occurrence count reaches the recurrence threshold. The grouping iterates the
// window's SORTED records, so the per-group evidence — and the group set — is
// deterministic regardless of ingest order (spec 078 FR-001, US1 scenario 1).
func (a *DeterministicAnalyzer) detectRecurringFailures(window TelemetryWindow) []Finding {
	// failureGroup keys on (SpecRef, Signature) — two records of the identical
	// underlying failure share both, so they land in the same group.
	type groupKey struct{ specRef, signature string }
	groups := map[groupKey][]TelemetryRecord{}
	var order []groupKey // first-seen order, over the SORTED records.

	for _, rec := range window.Sorted() {
		if !isFailure(rec) || rec.Signature == "" {
			continue
		}
		k := groupKey{specRef: rec.SpecRef, signature: rec.Signature}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], rec)
	}

	var findings []Finding
	for _, k := range order {
		evidence := groups[k]
		if len(evidence) < recurringFailureThreshold {
			continue // a single failure is noise, not a recurrence.
		}
		findings = append(findings, Finding{
			Kind:        FindingRecurringFailure,
			SpecRef:     k.specRef,
			Signature:   k.signature,
			Summary:     recurringFailureSummary(k.signature, len(evidence)),
			Occurrences: len(evidence),
			Category:    a.categoryFor(evidence[0]),
			Evidence:    evidence, // already in canonical order — window.Sorted().
		})
	}
	return findings
}

// detectMissedOpportunities is the US1 placeholder for missed-opportunity
// detection. The irreducible US1 arc is recurring-failure → proposal; the
// missed-opportunity pass exists so the Analyzer contract is whole, but in
// this slice it surfaces nothing — the spec's measurable US1 outcome (SC-001)
// is about a recurring failure, and a noisy opportunity pass would dilute it.
//
// TODO(spec-078-US2): expand missed-opportunity detection — slow-but-passing
// work, manual steps a deterministic node could absorb — once the
// deterministic-review tier (US2) makes a richer analysis pass affordable.
func (a *DeterministicAnalyzer) detectMissedOpportunities(_ TelemetryWindow) []Finding {
	return nil
}

// categoryFor resolves the gate-able category for a finding's lead record,
// using the injected CategoryFor or the conservative default.
func (a *DeterministicAnalyzer) categoryFor(rec TelemetryRecord) GateableCategory {
	if a.CategoryFor != nil {
		return a.CategoryFor(rec)
	}
	return defaultCategoryFor(rec)
}

// defaultCategoryFor is the conservative default mapping from a telemetry
// record to a gate-able action category (spec 078 FR-007). It maps each
// telemetry source to the net-positive, gate-able category whose work a
// proposal addressing that source would entail. A source with no clear
// gate-able category maps to the empty category — which IsGateable rejects, so
// synthesis refuses to produce a proposal rather than guess (spec 078 FR-007,
// edge case: dangerous / ungated category).
func defaultCategoryFor(rec TelemetryRecord) GateableCategory {
	switch rec.Source {
	case SourceCI, SourceRunHistory:
		// A recurring CI / run failure → propose generated code to fix it.
		return CategoryCodeGeneration
	case SourcePR:
		// A recurring PR-stage failure → propose tighter PR review.
		return CategoryPRReview
	case SourceBench:
		// A recurring bench regression → propose more e2e coverage.
		return CategoryE2ETestAuthoring
	case SourceGovernance:
		// A recurring governance denial → review against the deterministic
		// spec the denial referenced.
		return CategoryReviewDeterministicSpec
	case SourceAgent:
		// A recurring agent failure → peer review of the work it produced.
		return CategoryPeerReview
	default:
		// No clear gate-able category — return empty; synthesis refuses it.
		return ""
	}
}

// isFailure reports whether a telemetry record represents a failure outcome —
// the records the recurring-failure pass groups on. It treats the canonical
// failure-shaped outcome strings as failures.
func isFailure(rec TelemetryRecord) bool {
	switch rec.Outcome {
	case "failure", "failed", "denied", "error", "timeout":
		return true
	default:
		return false
	}
}

// recurringFailureSummary builds the deterministic one-line summary for a
// recurring-failure finding.
func recurringFailureSummary(signature string, count int) string {
	return "recurring failure: " + signature + " (" + itoa(count) + " occurrences)"
}

// SuppressDuplicates folds a cycle's freshly-detected findings against the set
// of findings whose proposals are still PENDING from earlier cycles, applying
// the spec-078 FR-014 duplicate rule:
//
//   - a fresh finding that matches a still-pending finding (same Identity) is
//     NOT emitted as a new finding; its evidence is merged into the pending
//     finding, which is returned in `updated` so the loop can attach the new
//     evidence to the existing pending proposal (FR-014, SC-006).
//   - a fresh finding that matches NO pending finding is genuinely new and is
//     returned in `fresh`.
//
// The function is pure and deterministic: `pending` is the loop's read-model
// of still-open findings (US1 supplies it; US3 carries it across cycles).
// Both result slices are returned in canonical SortFindings order.
func SuppressDuplicates(detected, pending []Finding) (fresh, updated []Finding) {
	// Index the pending findings by identity for O(1) match lookup.
	pendingByID := make(map[string]Finding, len(pending))
	for _, p := range pending {
		pendingByID[p.Identity()] = p
	}

	for _, d := range detected {
		if existing, isDup := pendingByID[d.Identity()]; isDup {
			// Recurrence of a still-pending finding — attach new evidence to
			// the existing one rather than re-queue a duplicate (FR-014).
			merged := existing.MergeEvidence(d)
			pendingByID[d.Identity()] = merged // keep the merge if it recurs again this cycle.
			continue
		}
		fresh = append(fresh, d)
	}

	// `updated` is every pending finding whose evidence actually grew this
	// cycle — those, and only those, need their pending proposal re-touched.
	for _, original := range pending {
		merged := pendingByID[original.Identity()]
		if len(merged.Evidence) > len(original.Evidence) {
			updated = append(updated, merged)
		}
	}

	SortFindings(fresh)
	SortFindings(updated)
	return fresh, updated
}

// RejectedSet is the loop's record of operator-rejected proposals, keyed by
// the grounding finding's Identity (spec 078 FR-015). The loop consults it so
// it does not re-propose an identical change without new evidence.
//
// US1 (this slice) supplies the RejectedSet to a cycle as input — the cycle is
// on-demand. Carrying the rejected set forward across scheduled cycles is US3.
//
// TODO(spec-078-US3): persist the rejected set across Continue-As-New so a
// continuous loop honors a rejection cycle after cycle (FR-015, T024).
type RejectedSet struct {
	// byIdentity maps a finding Identity to the evidence count the proposal
	// carried when the operator rejected it. A later finding with the SAME
	// identity is re-proposable ONLY if it now carries MORE evidence —
	// "without new evidence" (FR-015) is made concrete as "more records".
	byIdentity map[string]int
}

// NewRejectedSet builds a RejectedSet from a slice of rejected proposals.
func NewRejectedSet(rejected []SpecProposal) *RejectedSet {
	rs := &RejectedSet{byIdentity: map[string]int{}}
	for _, p := range rejected {
		if p.Status != StatusProposalRejected {
			continue
		}
		id := p.Finding.Identity()
		// Keep the largest evidence count seen for this identity — a finding
		// must beat the most-evidenced rejection to be re-proposable.
		if n := len(p.Finding.Evidence); n > rs.byIdentity[id] {
			rs.byIdentity[id] = n
		}
	}
	return rs
}

// AllowsReProposal reports whether a finding may be proposed despite a prior
// rejection of the same change. A finding never rejected is always allowed. A
// finding matching a rejection is allowed only if it now carries strictly more
// evidence than the rejected proposal did — new evidence (spec 078 FR-015).
func (rs *RejectedSet) AllowsReProposal(f Finding) bool {
	if rs == nil || rs.byIdentity == nil {
		return true
	}
	rejectedEvidence, wasRejected := rs.byIdentity[f.Identity()]
	if !wasRejected {
		return true
	}
	return len(f.Evidence) > rejectedEvidence
}

// FilterReProposable drops from `findings` every finding the RejectedSet
// forbids (a prior rejection, no new evidence). The result is sorted.
func (rs *RejectedSet) FilterReProposable(findings []Finding) []Finding {
	var allowed []Finding
	for _, f := range findings {
		if rs.AllowsReProposal(f) {
			allowed = append(allowed, f)
		}
	}
	SortFindings(allowed)
	return allowed
}

// itoa renders a non-negative int as decimal — a tiny strconv.Itoa avoiding an
// import for the one summary-building use here. Occurrence counts are
// non-negative by construction.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// sortStrings sorts a string slice ascending in place — used where a tiny
// local sort is clearer than threading sort.Strings through callers.
func sortStrings(s []string) { sort.Strings(s) }
