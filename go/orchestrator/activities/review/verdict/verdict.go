package verdict

// Enum is the closed set of verdict values a reviewer may emit (spec 094
// FR-013). The set is finite by design: a four-value vocabulary keeps the
// dialectic gate's decision tree tractable and forces reviewer drivers to
// commit to a position rather than hedge with free text.
type Enum string

const (
	// Approve — the reviewer endorses merging with no caveats. Blockers
	// must be empty; concerns and recommendations may be empty.
	Approve Enum = "approve"
	// ApproveWithComments — endorses merging but flags concerns or
	// recommendations the author should consider as follow-up. Blockers
	// must be empty; at least one of concerns/recommendations is required.
	ApproveWithComments Enum = "approve-with-comments"
	// RequestChanges — does not endorse merging. Blockers must be
	// non-empty; concerns and recommendations may be empty.
	RequestChanges Enum = "request-changes"
	// Abstain — explicitly declines to render a verdict (e.g., insufficient
	// context, conflict of interest). All three lists must be empty; the
	// optional reason field may carry a free-text rationale. The aggregator
	// treats abstain as undecidable — every primary-abstain combination
	// dispatches the arbiter.
	Abstain Enum = "abstain"
)

// Valid reports whether e is one of the four declared enum values. The
// orchestrator rejects any other value at validation time as a malformed
// verdict, not as a fifth kind of outcome.
func (e Enum) Valid() bool {
	switch e {
	case Approve, ApproveWithComments, RequestChanges, Abstain:
		return true
	default:
		return false
	}
}

// IsApproveShaped reports whether the enum is one of the two approve-shaped
// values (FR-009 agreement-to-pass rule). This is the predicate the
// aggregator uses to short-circuit when both primaries endorse merging.
func (e Enum) IsApproveShaped() bool {
	return e == Approve || e == ApproveWithComments
}

// StructuredVerdict is the schema-validated output of one reviewer
// invocation (FR-013, spec 094 Key Entities). Every reviewer — primary,
// arbiter, machine, or operator — emits exactly this shape. Lists may be
// empty per the per-enum invariants (FR-014), but never nil after
// JSON unmarshalling normalizes; the validator below treats nil and empty
// as equivalent for invariant purposes.
//
// A StructuredVerdict is immutable once recorded in workflow history
// (FR-015, FR-034). A re-review produces a new invocation with its own
// verdict, never a mutation of a prior one.
type StructuredVerdict struct {
	// Verdict is the enum value. Must be one of the four declared
	// constants; any other value is rejected by Validate.
	Verdict Enum `json:"verdict"`
	// Concerns are free-text observations that are not blockers. Each
	// string must be non-empty if present.
	Concerns []string `json:"concerns"`
	// Recommendations are free-text suggestions for follow-up. Each string
	// must be non-empty if present.
	Recommendations []string `json:"recommendations"`
	// Blockers are free-text reasons the verdict is request-changes. Each
	// string must be non-empty if present; the per-enum invariants govern
	// when this list must be empty vs non-empty.
	Blockers []string `json:"blockers"`
	// Reason is an optional free-text rationale, used only with
	// verdict=abstain (per the FR-014 invariant for abstain). Other enum
	// values ignore this field.
	Reason string `json:"reason,omitempty"`
}
