package driver

import "time"

// Tier is the capability/quality band of an agent runtime — one of the two
// primary keys for deterministic driver selection (FR-005). It is a closed
// enumeration: frontier-grade hosted models, mid-grade models, and local
// self-hosted models.
type Tier int

const (
	// TierFrontier is a frontier-grade hosted model (highest capability,
	// highest cost). Sorts first in selection — most capable wins ties of
	// nothing else.
	TierFrontier Tier = iota
	// TierMid is a mid-grade hosted model.
	TierMid
	// TierLocal is a local, operator-self-hosted model.
	TierLocal
)

// tierNames is indexed by Tier; kept in sync with the constants above.
var tierNames = [...]string{
	TierFrontier: "frontier",
	TierMid:      "mid",
	TierLocal:    "local",
}

// String renders the tier as its declared name. An out-of-range Tier
// renders as "tier(N)" rather than panicking.
func (t Tier) String() string {
	if int(t) < 0 || int(t) >= len(tierNames) {
		return "tier(" + itoa(int(t)) + ")"
	}
	return tierNames[t]
}

// Valid reports whether t is one of the declared tiers.
func (t Tier) Valid() bool { return int(t) >= 0 && int(t) < len(tierNames) }

// CostClass is a relative cost band for an agent invocation — the secondary
// key in deterministic selection, applied after Tier (FR-005). Lower values
// are cheaper and therefore preferred. It is a small ordinal, not a price:
// the absolute number is meaningless, only the ordering matters.
type CostClass int

const (
	// CostFree is a no-marginal-cost invocation (e.g. a local model on the
	// operator's own GPU).
	CostFree CostClass = iota
	// CostLow is a low relative cost band.
	CostLow
	// CostMedium is a medium relative cost band.
	CostMedium
	// CostHigh is a high relative cost band.
	CostHigh
)

// CostZero is an alias for CostFree kept for specs and routing policy text
// that name the zero-marginal-cost band explicitly.
const CostZero CostClass = CostFree

// costClassNames is indexed by CostClass; kept in sync with the constants.
var costClassNames = [...]string{
	CostFree:   "free",
	CostLow:    "low",
	CostMedium: "medium",
	CostHigh:   "high",
}

// String renders the cost class as its declared name. An out-of-range value
// renders as "cost(N)" rather than panicking.
func (c CostClass) String() string {
	if int(c) < 0 || int(c) >= len(costClassNames) {
		return "cost(" + itoa(int(c)) + ")"
	}
	return costClassNames[c]
}

// Valid reports whether c is one of the declared cost classes.
func (c CostClass) Valid() bool {
	return int(c) >= 0 && int(c) < len(costClassNames)
}

// Constraints are the operational limits a driver declares on its card
// (FR-003). They describe what an invocation needs and what may make the
// driver unready, distinct from the capability tags that describe what work
// it can do.
type Constraints struct {
	// QuotaBounded is true if the agent draws on a finite quota that can be
	// exhausted; such a driver must report not-ready when its quota is
	// spent (FR-008).
	QuotaBounded bool `json:"quota_bounded"`
	// NetworkRequired is true if the driver's agent needs outbound network
	// access to function.
	NetworkRequired bool `json:"network_required"`
	// MaxContextTokens is the largest context window the agent accepts, in
	// tokens. Zero means unspecified.
	MaxContextTokens int `json:"max_context_tokens"`
	// WorktreeRequired is true if an invocation must be given a dedicated
	// git worktree (FR-007; 070 FR-013). v1 drivers all require this.
	WorktreeRequired bool `json:"worktree_required"`
}

// ReviewMode declares a reviewer-eligible driver's review-mode contract
// (spec 094 FR-002). A driver that declares CapCodeReview SHOULD also
// populate ReviewMode so the orchestrator knows which tool name to invoke
// for the review-mode dispatch and the driver's self-declared input-byte
// budget. The orchestrator does not prescribe the prompt content — that is
// the driver's own (FR-003) — only the input/output contract.
//
// ReviewMode is an optional sidecar: a nil ReviewMode on a CapCodeReview
// driver is legal at v1 (the dispatch falls back to a conventional tool
// name) but a v1.x amendment will require it.
type ReviewMode struct {
	// ToolName is the name of the tool the driver exposes for review-mode
	// dispatch — by convention "review". The dispatch activity invokes
	// the driver's tool registry with this name.
	ToolName string `json:"tool_name"`
	// PromptTemplate is the driver's own review-mode prompt. Opaque to
	// the orchestrator; recorded in the registry for audit and operator
	// inspection (FR-003).
	PromptTemplate string `json:"prompt_template,omitempty"`
	// MaxBytesIn is the driver's self-declared upper bound on the size of
	// the PRSnapshot input it accepts (bytes). The dispatch activity may
	// truncate the snapshot at this boundary and flag the truncation in
	// the outcome; the driver may treat a truncation flag as cause for
	// abstain.
	MaxBytesIn int `json:"max_bytes_in,omitempty"`
}

// CapabilityCard is one driver's declared contract — the metadata the
// Registry routes on and the kernel enforces against (FR-003, FR-011). It
// is modeled on the A2A Agent Card and is recorded in the chitin chain at
// registration so the declared contract is itself auditable (FR-010).
//
// A card is a value, not a promise: every Capability in Capabilities must
// be a tag from the closed taxonomy (taxonomy.go). A card carrying an
// unknown tag is a registration error — the Registry rejects it (FR-015).
type CapabilityCard struct {
	// DriverID is the stable, registry-unique driver identifier; it must
	// equal AgentDriver.ID(). It is the final selection tie-breaker.
	DriverID string `json:"driver_id"`
	// Version is the driver's own version string (semver recommended).
	Version string `json:"version"`
	// AgentRuntime names the underlying agent runtime — e.g. "claude-code",
	// "codex", "hermes", "openclaw", "local-llm".
	AgentRuntime string `json:"agent_runtime"`
	// Model is the model the runtime drives — e.g. "claude-opus-4-7".
	Model string `json:"model"`
	// Capabilities is the set of capability tags this driver declares. Every
	// entry must satisfy IsKnownCapability. It is a set: a Registry treats
	// duplicate tags as one and order is not significant.
	Capabilities []Capability `json:"capabilities"`
	// Tier is the agent's capability band — the primary selection key.
	Tier Tier `json:"tier"`
	// CostClass is the agent's relative cost band — the secondary selection
	// key.
	CostClass CostClass `json:"cost_class"`
	// Constraints are the driver's declared operational limits.
	Constraints Constraints `json:"constraints"`
	// GitIdentity is the git author identifier the driver uses when it
	// authors commits or PRs — e.g., "hermes-bot", "clawta-bot". It is
	// the bridge between PR-author attribution and driver identity for the
	// no-self-review exclusion (spec 094 FR-005, R-AUTHORID). Empty for
	// drivers that do not author git artifacts on their own behalf.
	GitIdentity string `json:"git_identity,omitempty"`
	// ReviewMode is the driver's review-mode contract (spec 094 FR-002).
	// Required when the driver declares CapCodeReview and intends to be
	// dispatched as a reviewer; ignored otherwise. Nil-safe: the dispatch
	// activity falls back to a conventional default when absent.
	ReviewMode *ReviewMode `json:"review_mode,omitempty"`
}

// HasCapability reports whether the card declares capability c.
func (c CapabilityCard) HasCapability(want Capability) bool {
	for _, have := range c.Capabilities {
		if have == want {
			return true
		}
	}
	return false
}

// WorkUnit is the typed input to a driver invocation (FR-006) — what a
// driver is asked to do. It carries no free-form contract: a driver
// receives exactly these fields.
type WorkUnit struct {
	// ID is the stable work-unit identifier, unique within an orchestrator
	// run; it is the activity's correlation key.
	ID string `json:"id"`
	// SpecID is the spec the work unit belongs to — e.g. "075".
	SpecID string `json:"spec_id"`
	// TaskID is the task within the spec — e.g. "T009"; empty if the work
	// unit is not task-scoped.
	TaskID string `json:"task_id"`
	// Context is the spec/task context the agent needs to do the work — the
	// instruction payload, kept opaque to the driver layer.
	Context string `json:"context"`
	// WorktreePath is the absolute path to the dedicated git worktree the
	// invocation runs in (FR-007; 070 FR-013).
	WorktreePath string `json:"worktree_path"`
	// Deadline is the wall-clock instant by which the invocation must
	// complete; on overrun the driver returns a StatusTimeout Result. A
	// zero Deadline means no driver-enforced deadline.
	Deadline time.Time `json:"deadline"`
}

// Status is the typed outcome of a driver invocation (FR-006). It always
// carries the agent outcome — even a timeout is a typed status, never a
// hang or a bare error (FR-007, edge case "exceeds its deadline").
type Status int

const (
	// StatusUnknown is the zero value — an uninitialized Result. A driver
	// must never return this deliberately.
	StatusUnknown Status = iota
	// StatusSucceeded means the agent completed the work unit.
	StatusSucceeded
	// StatusFailed means the agent ran but did not complete the work unit;
	// see Result.Explanation. The workflow may retry per policy.
	StatusFailed
	// StatusTimeout means the invocation exceeded the WorkUnit deadline.
	// It is a typed, retryable outcome — the workflow retries per policy
	// rather than seeing a hang (FR-007).
	StatusTimeout
	// StatusQuotaExhausted means the agent's quota was spent mid-invocation.
	// It is a typed, retryable failure so the scheduler can re-route or
	// back off (edge case "quota is exhausted mid-invocation").
	StatusQuotaExhausted
)

// statusNames is indexed by Status; kept in sync with the constants.
var statusNames = [...]string{
	StatusUnknown:        "unknown",
	StatusSucceeded:      "succeeded",
	StatusFailed:         "failed",
	StatusTimeout:        "timeout",
	StatusQuotaExhausted: "quota_exhausted",
}

// String renders the status as its declared name.
func (s Status) String() string {
	if int(s) < 0 || int(s) >= len(statusNames) {
		return "status(" + itoa(int(s)) + ")"
	}
	return statusNames[s]
}

// Retryable reports whether a workflow may sensibly retry the invocation
// given this status. Timeout and quota-exhaustion are retryable; an outright
// failure or success is not.
func (s Status) Retryable() bool {
	return s == StatusTimeout || s == StatusQuotaExhausted
}

// Result is the typed output of a driver invocation (FR-006) — what a
// driver returns. There is no free-form contract: the orchestrator reads
// exactly these fields.
type Result struct {
	// WorkUnitID echoes the WorkUnit.ID this Result answers, for correlation.
	WorkUnitID string `json:"work_unit_id"`
	// DriverID is the ID of the driver that produced this Result — the
	// chosen driver, recorded for audit (FR-005 "the chosen driver ...
	// recorded").
	DriverID string `json:"driver_id"`
	// Status is the typed outcome.
	Status Status `json:"status"`
	// OutputRef is a reference to the work product — a branch name, a PR
	// URL, or an artifact path. It is a reference, not the content itself.
	OutputRef string `json:"output_ref"`
	// Explanation is a human-readable account of the outcome — why it
	// failed, what it produced, what timed out.
	Explanation string `json:"explanation"`
}

// itoa is a tiny strconv.Itoa avoiding an import in the String fallbacks.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
