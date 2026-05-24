package verdict

// FailureKind enumerates the terminal not-a-verdict outcomes a reviewer
// invocation may produce (spec 094 FR-028, edge cases). Each kind is
// preserved in workflow history so the post-mortem trail attributes the
// missing verdict to the exact failure mode.
type FailureKind string

const (
	// FailureTimeout — the activity exceeded its per-reviewer time bound
	// (FR-026 default 30 minutes).
	FailureTimeout FailureKind = "timeout"
	// FailureError — the driver returned a non-zero error from its review
	// tool (driver-side crash, panic, RPC error).
	FailureError FailureKind = "error"
	// FailureMalformedJSON — the driver's output could not be parsed as
	// JSON conforming to StructuredVerdict's field set.
	FailureMalformedJSON FailureKind = "malformed_json"
	// FailureMalformedShape — the driver's output parsed as JSON but the
	// resulting StructuredVerdict failed Validate() (an FR-014 invariant
	// was violated).
	FailureMalformedShape FailureKind = "malformed_shape"
	// FailureCancelled — the workflow cancelled the dispatch mid-flight
	// (e.g., the parent received a re-review or abort signal).
	FailureCancelled FailureKind = "cancelled"
)

// Valid reports whether k is one of the declared failure kinds.
func (k FailureKind) Valid() bool {
	switch k {
	case FailureTimeout, FailureError, FailureMalformedJSON, FailureMalformedShape, FailureCancelled:
		return true
	default:
		return false
	}
}

// Failure carries the kind plus a human-readable diagnostic. The detail is
// recorded in workflow history alongside the failure kind so an operator
// reading the audit can see both the category and the specific reason.
type Failure struct {
	Kind   FailureKind `json:"kind"`
	Detail string      `json:"detail"`
}

// Role names the slot a reviewer invocation fills within one dialectic.
// Two primary slots plus optionally one arbiter slot — the structure of
// the gate (spec 094 FR-008, FR-012).
type Role string

const (
	// RolePrimary — one of the two primaries dispatched in parallel.
	RolePrimary Role = "primary"
	// RoleArbiter — the single arbiter dispatched on primary disagreement.
	RoleArbiter Role = "arbiter"
)

// Outcome is the terminal state of one reviewer invocation: either a
// validated StructuredVerdict or a Failure, never both, never neither.
// The invariant Verdict != nil XOR Failure != nil is enforced by the
// constructor predicates below; the Aggregate function relies on it.
type Outcome struct {
	// InvocationID is the ULID generated when the dispatch activity started.
	InvocationID string `json:"invocation_id"`
	// DriverID names the driver that produced this outcome — a real
	// driver's ID (e.g., "hermes", "openclaw") or the literal "operator"
	// for an operator-arbiter dispatch.
	DriverID string `json:"driver_id"`
	// Role is the slot this outcome fills.
	Role Role `json:"role"`
	// Verdict is set on a successful invocation; nil on a Failure.
	Verdict *StructuredVerdict `json:"verdict,omitempty"`
	// Failure is set on a failed invocation; nil on success.
	Failure *Failure `json:"failure,omitempty"`
	// ElapsedMS is the wall-clock duration from dispatch start to close.
	ElapsedMS int64 `json:"elapsed_ms"`
}

// IsFailure reports whether this outcome is a failure rather than a verdict.
func (o Outcome) IsFailure() bool { return o.Failure != nil }

// IsApproveShaped reports whether this outcome is a verdict in
// {approve, approve-with-comments}. A failure is never approve-shaped.
func (o Outcome) IsApproveShaped() bool {
	return o.Verdict != nil && o.Verdict.Verdict.IsApproveShaped()
}

// IsRequestChanges reports whether this outcome is a verdict of
// request-changes. A failure is never request-changes.
func (o Outcome) IsRequestChanges() bool {
	return o.Verdict != nil && o.Verdict.Verdict == RequestChanges
}

// IsAbstain reports whether this outcome is a verdict of abstain.
func (o Outcome) IsAbstain() bool {
	return o.Verdict != nil && o.Verdict.Verdict == Abstain
}
