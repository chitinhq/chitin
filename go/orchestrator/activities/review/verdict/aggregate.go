package verdict

// GateState is the terminal state of one dialectic execution (spec 094
// Key Entity "ReviewGateDecision"). The set is closed: every Aggregate
// call returns one of these three values plus a reason naming why.
type GateState string

const (
	// GatePassed — review approved; the parent merge workflow may
	// proceed. May or may not have engaged the arbiter (FR-010, FR-012).
	GatePassed GateState = "passed"
	// GateBlocked — review denied; the parent must halt or seek an
	// operator override (FR-011, FR-012 on request-changes arbiter).
	GateBlocked GateState = "blocked"
	// GateHalted — no decisive verdict reachable (arbiter abstained,
	// pool exhausted, all primaries failed). Operator escalation
	// required (spec 094 edge cases, R-AGG).
	GateHalted GateState = "halted"
)

// Decision is the aggregator's verdict on one dialectic execution. The
// parent merge workflow consumes State + Reason; ArbiterEngaged is the
// truthful audit flag describing whether the third slot dispatched.
type Decision struct {
	State          GateState `json:"state"`
	Reason         string    `json:"reason"`
	ArbiterEngaged bool      `json:"arbiter_engaged"`
}

// Aggregate is the closed-form decision rule for the dialectic gate
// (spec 094 FR-009 through FR-012, research decision R-AGG). It is a
// pure function — no I/O, no clocks, no randomness — so the workflow
// may call it from inside a deterministic Temporal workflow function
// without breaching determinism.
//
// Decision tree (applied in order):
//
//  1. Both primaries approve-shaped → passed (no arbiter needed).
//  2. Both primaries request-changes → blocked (no arbiter needed).
//  3. Any other primary combination — disagreement, any abstain, any
//     failure on either primary — requires the arbiter. If arbiter is
//     nil, return GateHalted with the "arbiter required but not
//     dispatched" reason (defensive case; the workflow caller is
//     responsible for ensuring arbiter is dispatched before calling
//     Aggregate on the arbiter case).
//  4. Arbiter approve-shaped → passed.
//  5. Arbiter request-changes → blocked, reason names the first blocker.
//  6. Arbiter abstain → halted, reason names the abstain reason.
//  7. Arbiter failure → halted, reason names the failure detail.
//
// The order matters: rules 1–3 are evaluated before 4–7 because the
// arbiter only matters in case 3. Rules 4–7 are exhaustive on the
// arbiter outcome.
func Aggregate(p1, p2 Outcome, arbiter *Outcome) Decision {
	// Rule 1 — both primaries approve-shaped.
	if p1.IsApproveShaped() && p2.IsApproveShaped() {
		return Decision{
			State:          GatePassed,
			Reason:         "both primaries approve",
			ArbiterEngaged: false,
		}
	}
	// Rule 2 — both primaries request-changes.
	if p1.IsRequestChanges() && p2.IsRequestChanges() {
		return Decision{
			State:          GateBlocked,
			Reason:         "both primaries request-changes",
			ArbiterEngaged: false,
		}
	}
	// Rule 3 — arbiter required.
	if arbiter == nil {
		return Decision{
			State:          GateHalted,
			Reason:         "arbiter required but not dispatched",
			ArbiterEngaged: false,
		}
	}
	// Rules 4–7 — arbiter case.
	switch {
	case arbiter.IsApproveShaped():
		return Decision{
			State:          GatePassed,
			Reason:         "arbiter approves",
			ArbiterEngaged: true,
		}
	case arbiter.IsRequestChanges():
		first := arbiter.Verdict.Blockers[0] // FR-014 guarantees non-empty.
		return Decision{
			State:          GateBlocked,
			Reason:         "arbiter requests changes: " + first,
			ArbiterEngaged: true,
		}
	case arbiter.IsAbstain():
		reason := "arbiter abstained"
		if arbiter.Verdict.Reason != "" {
			reason += ": " + arbiter.Verdict.Reason
		}
		return Decision{
			State:          GateHalted,
			Reason:         reason,
			ArbiterEngaged: true,
		}
	case arbiter.IsFailure():
		return Decision{
			State:          GateHalted,
			Reason:         "arbiter failed: " + string(arbiter.Failure.Kind) + ": " + arbiter.Failure.Detail,
			ArbiterEngaged: true,
		}
	default:
		// Defensive: shouldn't be reachable if Outcome's IsX predicates
		// are exhaustive. Halt loudly so a regression surfaces.
		return Decision{
			State:          GateHalted,
			Reason:         "arbiter outcome is neither verdict nor failure",
			ArbiterEngaged: true,
		}
	}
}
