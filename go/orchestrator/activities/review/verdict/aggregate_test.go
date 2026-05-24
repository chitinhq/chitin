package verdict

import (
	"strings"
	"testing"
)

// makeVerdict is a tiny constructor that bypasses Validate. Aggregate
// trusts its inputs (the dispatch activity has already validated), so
// tests can construct any verdict shape — including malformed ones if
// the test is explicitly probing the aggregator's behaviour on
// "shouldn't happen" inputs.
func makeVerdict(e Enum, blockers ...string) *StructuredVerdict {
	return &StructuredVerdict{Verdict: e, Blockers: blockers}
}

func makeOutcome(role Role, v *StructuredVerdict) Outcome {
	return Outcome{
		InvocationID: "01",
		DriverID:     "test-driver",
		Role:         role,
		Verdict:      v,
	}
}

func makeFailureOutcome(role Role, kind FailureKind) Outcome {
	return Outcome{
		InvocationID: "01",
		DriverID:     "test-driver",
		Role:         role,
		Failure:      &Failure{Kind: kind, Detail: "test"},
	}
}

// TestAggregate_BothApprove covers rule 1 (FR-010).
func TestAggregate_BothApprove(t *testing.T) {
	cases := []struct{ a, b Enum }{
		{Approve, Approve},
		{Approve, ApproveWithComments},
		{ApproveWithComments, Approve},
		{ApproveWithComments, ApproveWithComments},
	}
	for _, tc := range cases {
		p1 := makeOutcome(RolePrimary, makeVerdict(tc.a))
		p2 := makeOutcome(RolePrimary, makeVerdict(tc.b))
		got := Aggregate(p1, p2, nil)
		if got.State != GatePassed {
			t.Errorf("Aggregate(%s, %s, nil).State = %s, want %s", tc.a, tc.b, got.State, GatePassed)
		}
		if got.ArbiterEngaged {
			t.Errorf("Aggregate(%s, %s, nil).ArbiterEngaged = true, want false (rule 1 short-circuit)", tc.a, tc.b)
		}
		if got.Reason != "both primaries approve" {
			t.Errorf("Aggregate(%s, %s, nil).Reason = %q, want %q", tc.a, tc.b, got.Reason, "both primaries approve")
		}
	}
}

// TestAggregate_BothRequestChanges covers rule 2 (FR-011).
func TestAggregate_BothRequestChanges(t *testing.T) {
	p1 := makeOutcome(RolePrimary, makeVerdict(RequestChanges, "no tests"))
	p2 := makeOutcome(RolePrimary, makeVerdict(RequestChanges, "no plan"))
	got := Aggregate(p1, p2, nil)
	if got.State != GateBlocked {
		t.Errorf("Aggregate(reqchg, reqchg, nil).State = %s, want %s", got.State, GateBlocked)
	}
	if got.ArbiterEngaged {
		t.Error("Aggregate(reqchg, reqchg, nil).ArbiterEngaged = true, want false (rule 2 short-circuit)")
	}
	if got.Reason != "both primaries request-changes" {
		t.Errorf("Aggregate(reqchg, reqchg, nil).Reason = %q", got.Reason)
	}
}

// TestAggregate_ArbiterRequired covers rule 3 — every other primary
// combination requires arbiter dispatch. If arbiter is nil, the function
// returns GateHalted with the defensive reason.
func TestAggregate_ArbiterRequired_NilArbiter(t *testing.T) {
	cases := []struct {
		name string
		p1   Outcome
		p2   Outcome
	}{
		{"approve_vs_request_changes",
			makeOutcome(RolePrimary, makeVerdict(Approve)),
			makeOutcome(RolePrimary, makeVerdict(RequestChanges, "x"))},
		{"approve_with_comments_vs_request_changes",
			makeOutcome(RolePrimary, makeVerdict(ApproveWithComments)),
			makeOutcome(RolePrimary, makeVerdict(RequestChanges, "x"))},
		{"request_changes_vs_approve",
			makeOutcome(RolePrimary, makeVerdict(RequestChanges, "x")),
			makeOutcome(RolePrimary, makeVerdict(Approve))},
		{"approve_vs_abstain",
			makeOutcome(RolePrimary, makeVerdict(Approve)),
			makeOutcome(RolePrimary, makeVerdict(Abstain))},
		{"abstain_vs_request_changes",
			makeOutcome(RolePrimary, makeVerdict(Abstain)),
			makeOutcome(RolePrimary, makeVerdict(RequestChanges, "x"))},
		{"both_abstain",
			makeOutcome(RolePrimary, makeVerdict(Abstain)),
			makeOutcome(RolePrimary, makeVerdict(Abstain))},
		{"approve_vs_timeout",
			makeOutcome(RolePrimary, makeVerdict(Approve)),
			makeFailureOutcome(RolePrimary, FailureTimeout)},
		{"request_changes_vs_malformed",
			makeOutcome(RolePrimary, makeVerdict(RequestChanges, "x")),
			makeFailureOutcome(RolePrimary, FailureMalformedShape)},
		{"both_fail",
			makeFailureOutcome(RolePrimary, FailureTimeout),
			makeFailureOutcome(RolePrimary, FailureError)},
	}
	for _, tc := range cases {
		got := Aggregate(tc.p1, tc.p2, nil)
		if got.State != GateHalted {
			t.Errorf("Aggregate(%s).State = %s, want %s (nil arbiter)", tc.name, got.State, GateHalted)
		}
		if got.Reason != "arbiter required but not dispatched" {
			t.Errorf("Aggregate(%s).Reason = %q, want defensive halt reason", tc.name, got.Reason)
		}
	}
}

// TestAggregate_ArbiterApproveSupersedes covers rule 4: any approve-shaped
// arbiter passes the gate regardless of the primary disagreement.
func TestAggregate_ArbiterApproveSupersedes(t *testing.T) {
	p1 := makeOutcome(RolePrimary, makeVerdict(Approve))
	p2 := makeOutcome(RolePrimary, makeVerdict(RequestChanges, "x"))
	for _, e := range []Enum{Approve, ApproveWithComments} {
		arb := makeOutcome(RoleArbiter, makeVerdict(e))
		got := Aggregate(p1, p2, &arb)
		if got.State != GatePassed {
			t.Errorf("Aggregate(approve, reqchg, arb=%s).State = %s, want passed", e, got.State)
		}
		if !got.ArbiterEngaged {
			t.Errorf("Aggregate(approve, reqchg, arb=%s).ArbiterEngaged = false, want true", e)
		}
		if got.Reason != "arbiter approves" {
			t.Errorf("Aggregate(approve, reqchg, arb=%s).Reason = %q", e, got.Reason)
		}
	}
}

// TestAggregate_ArbiterRequestChanges covers rule 5: arbiter's first blocker
// is named in the reason so the operator sees the dispositive concern.
func TestAggregate_ArbiterRequestChanges(t *testing.T) {
	p1 := makeOutcome(RolePrimary, makeVerdict(Approve))
	p2 := makeOutcome(RolePrimary, makeVerdict(RequestChanges, "p2-blocker"))
	arb := makeOutcome(RoleArbiter, makeVerdict(RequestChanges, "arb-blocker-1", "arb-blocker-2"))
	got := Aggregate(p1, p2, &arb)
	if got.State != GateBlocked {
		t.Errorf("State = %s, want blocked", got.State)
	}
	if !strings.Contains(got.Reason, "arb-blocker-1") {
		t.Errorf("Reason = %q, want to contain first arbiter blocker", got.Reason)
	}
	if strings.Contains(got.Reason, "arb-blocker-2") {
		t.Errorf("Reason = %q, must name ONLY the first arbiter blocker", got.Reason)
	}
}

// TestAggregate_ArbiterAbstain covers rule 6.
func TestAggregate_ArbiterAbstain(t *testing.T) {
	p1 := makeOutcome(RolePrimary, makeVerdict(Approve))
	p2 := makeOutcome(RolePrimary, makeVerdict(RequestChanges, "x"))
	abstainVerdict := &StructuredVerdict{Verdict: Abstain, Reason: "no spec context"}
	arb := Outcome{
		InvocationID: "01", DriverID: "operator", Role: RoleArbiter,
		Verdict: abstainVerdict,
	}
	got := Aggregate(p1, p2, &arb)
	if got.State != GateHalted {
		t.Errorf("State = %s, want halted", got.State)
	}
	if !strings.Contains(got.Reason, "abstained") || !strings.Contains(got.Reason, "no spec context") {
		t.Errorf("Reason = %q, want to mention abstention + abstain reason", got.Reason)
	}
}

// TestAggregate_ArbiterAbstain_NoReason covers rule 6 when the abstain
// verdict carries no optional reason — should still halt cleanly.
func TestAggregate_ArbiterAbstain_NoReason(t *testing.T) {
	p1 := makeOutcome(RolePrimary, makeVerdict(Approve))
	p2 := makeOutcome(RolePrimary, makeVerdict(RequestChanges, "x"))
	arb := makeOutcome(RoleArbiter, &StructuredVerdict{Verdict: Abstain})
	got := Aggregate(p1, p2, &arb)
	if got.State != GateHalted {
		t.Errorf("State = %s, want halted", got.State)
	}
	if got.Reason != "arbiter abstained" {
		t.Errorf("Reason = %q, want %q", got.Reason, "arbiter abstained")
	}
}

// TestAggregate_ArbiterFailure covers rule 7 across every failure kind.
func TestAggregate_ArbiterFailure(t *testing.T) {
	p1 := makeOutcome(RolePrimary, makeVerdict(Approve))
	p2 := makeOutcome(RolePrimary, makeVerdict(RequestChanges, "x"))
	for _, kind := range []FailureKind{
		FailureTimeout, FailureError, FailureMalformedJSON, FailureMalformedShape, FailureCancelled,
	} {
		arb := makeFailureOutcome(RoleArbiter, kind)
		got := Aggregate(p1, p2, &arb)
		if got.State != GateHalted {
			t.Errorf("kind=%s: State = %s, want halted", kind, got.State)
		}
		if !strings.Contains(got.Reason, string(kind)) {
			t.Errorf("kind=%s: Reason = %q, want to contain kind", kind, got.Reason)
		}
	}
}

// TestAggregate_FullCartesian sweeps the cartesian product of primary
// outcomes to ensure every cell either passes through rules 1–2 or falls
// to the arbiter case (rule 3). This is the SC-009 / FR-009 closure
// invariant: no primary combination silently misroutes.
func TestAggregate_FullCartesian(t *testing.T) {
	// Eight primary outcome kinds: 4 verdicts + 4 distinct failures.
	// (FailureCancelled is also a kind but its workflow lifecycle is
	// special-cased upstream; we still test the aggregator handles it.)
	primaryOutcomes := []struct {
		name string
		o    Outcome
	}{
		{"approve", makeOutcome(RolePrimary, makeVerdict(Approve))},
		{"approve_with_comments", makeOutcome(RolePrimary, makeVerdict(ApproveWithComments))},
		{"request_changes", makeOutcome(RolePrimary, makeVerdict(RequestChanges, "x"))},
		{"abstain", makeOutcome(RolePrimary, makeVerdict(Abstain))},
		{"failure_timeout", makeFailureOutcome(RolePrimary, FailureTimeout)},
		{"failure_error", makeFailureOutcome(RolePrimary, FailureError)},
		{"failure_malformed_json", makeFailureOutcome(RolePrimary, FailureMalformedJSON)},
		{"failure_malformed_shape", makeFailureOutcome(RolePrimary, FailureMalformedShape)},
	}

	arbiterApprove := makeOutcome(RoleArbiter, makeVerdict(Approve))

	for _, p1 := range primaryOutcomes {
		for _, p2 := range primaryOutcomes {
			// Without arbiter:
			got := Aggregate(p1.o, p2.o, nil)
			switch {
			case p1.o.IsApproveShaped() && p2.o.IsApproveShaped():
				if got.State != GatePassed || got.ArbiterEngaged {
					t.Errorf("(%s, %s, nil): expected passed, no arbiter", p1.name, p2.name)
				}
			case p1.o.IsRequestChanges() && p2.o.IsRequestChanges():
				if got.State != GateBlocked || got.ArbiterEngaged {
					t.Errorf("(%s, %s, nil): expected blocked, no arbiter", p1.name, p2.name)
				}
			default:
				if got.State != GateHalted {
					t.Errorf("(%s, %s, nil): expected halted (defensive), got %s", p1.name, p2.name, got.State)
				}
			}
			// With arbiter approve: should always pass unless rules 1-2 already
			// short-circuit it.
			got2 := Aggregate(p1.o, p2.o, &arbiterApprove)
			switch {
			case p1.o.IsApproveShaped() && p2.o.IsApproveShaped():
				if got2.ArbiterEngaged {
					t.Errorf("(%s, %s, +arb): rule 1 must short-circuit before arbiter", p1.name, p2.name)
				}
			case p1.o.IsRequestChanges() && p2.o.IsRequestChanges():
				if got2.ArbiterEngaged {
					t.Errorf("(%s, %s, +arb): rule 2 must short-circuit before arbiter", p1.name, p2.name)
				}
			default:
				if !got2.ArbiterEngaged || got2.State != GatePassed {
					t.Errorf("(%s, %s, +approve_arb): expected passed with arbiter engaged, got %+v", p1.name, p2.name, got2)
				}
			}
		}
	}
}
