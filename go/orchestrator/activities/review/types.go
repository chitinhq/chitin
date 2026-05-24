package review

import (
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
)

// ArbiterType is the class-policy choice between operator-as-arbiter and a
// third machine driver as arbiter (spec 094 FR-016). It is set by the
// merge orchestrator from the PR's policy class (spec 093 v1.1.0
// amendment) and threaded through PRReviewInput.
type ArbiterType string

const (
	// ArbiterOperator — class policy says the operator arbitrates on
	// primary disagreement (governance + spec-only at v1.1).
	ArbiterOperator ArbiterType = "operator"
	// ArbiterMachine — class policy says a third reviewer-tagged driver
	// arbitrates. Operationally degenerate at v1.1.0 ship (only two
	// reviewer-tagged drivers exist); see spec 094 Assumptions.
	ArbiterMachine ArbiterType = "machine"
)

// Valid reports whether t is one of the two declared arbiter types.
func (t ArbiterType) Valid() bool {
	return t == ArbiterOperator || t == ArbiterMachine
}

// PRReviewInput is the argument PRMergeWorkflow (spec 093) passes to
// PRReviewWorkflow (this spec) when it spawns the child. It carries the
// PR identity, the policy class hint, and the arbiter-type choice.
//
// PR snapshot capture is the FIRST activity of the workflow, not a
// parameter — the snapshot must be taken at workflow start so its content
// hash anchors the entire dialectic in audit history (FR-032, R-SNAP).
type PRReviewInput struct {
	// Repo is the GitHub repo slug, e.g. "chitinhq/chitin".
	Repo string `json:"repo"`
	// PRNumber is the GitHub PR number.
	PRNumber int `json:"pr_number"`
	// PRAuthor is the GitHub author login of the PR (used for the
	// FR-005 no-self-review exclusion via Registry.LookupByGitIdentity).
	PRAuthor string `json:"pr_author"`
	// PolicyClass is the spec 093 policy class — one of governance,
	// spec-only, impl, live-fix, bookkeeping, research-docs. The class
	// shapes telemetry attribution and is passed to reviewer drivers as a
	// hint (FR-002).
	PolicyClass string `json:"policy_class"`
	// ArbiterType is the class-policy arbiter choice. Determines whether
	// a disagreement engages the operator arbiter surface or dispatches a
	// third reviewer-tagged driver.
	ArbiterType ArbiterType `json:"arbiter_type"`
}

// ReviewerSlate is the pool-selection result returned by the
// SelectReviewers activity. It carries the three driver assignments (two
// primaries; optional arbiter) plus diagnostic fields used by the workflow
// to halt cleanly on a shortfall and by telemetry to attribute exclusions.
type ReviewerSlate struct {
	// Primary1 and Primary2 are the two driver IDs assigned to the
	// primary slots. They are picked from the deterministic pool order
	// (first two ready CapCodeReview drivers after applying the author
	// exclusion).
	Primary1 string `json:"primary1"`
	Primary2 string `json:"primary2"`
	// Arbiter is the driver ID assigned to the arbiter slot when
	// ArbiterType=machine AND a third reviewer-tagged driver is
	// available after excluding both primaries and the author. Empty
	// otherwise (including the operator-arbiter case, since the operator
	// is not a registry driver).
	Arbiter string `json:"arbiter,omitempty"`
	// ExcludedAuthor is the driver ID that was excluded for being the
	// PR author, if any. Empty if the PR was operator-authored or
	// authored by a human / unmapped identity (FR-005 / R-AUTHORID).
	// Recorded for telemetry attribution (FR-032).
	ExcludedAuthor string `json:"excluded_author,omitempty"`
	// EligibleAfterExclusion is the full ordered driver ID list that
	// remains after the author exclusion — used by the shortfall halt
	// reason and by tests to assert pool composition.
	EligibleAfterExclusion []string `json:"eligible_after_exclusion"`
}

// ReviewerInvocation is one driver dispatch within one dialectic run
// (spec 094 Key Entity "ReviewerInvocation"). The workflow accumulates two
// of these (one per primary) plus optionally a third (the arbiter) and
// hands them to verdict.Aggregate to produce the gate decision.
//
// A ReviewerInvocation closes when the dispatch activity returns; the
// terminal verdict-or-failure is captured in the embedded Outcome.
type ReviewerInvocation struct {
	// InvocationID is the per-dispatch ULID (generated at activity start).
	InvocationID string `json:"invocation_id"`
	// DriverID is the driver assigned to this slot, or the literal
	// "operator" for an operator-arbiter dispatch.
	DriverID string `json:"driver_id"`
	// Role is the slot the invocation fills (primary or arbiter).
	Role verdict.Role `json:"role"`
	// SnapshotHashRef is the SHA-256 hash of the canonical PR snapshot
	// the reviewer saw. Audit anchor — content hash is in telemetry
	// (FR-032); raw content lives in workflow history (FR-033).
	SnapshotHashRef string `json:"snapshot_hash_ref"`
	// StartedAt is the wall-clock instant the dispatch activity started.
	StartedAt time.Time `json:"started_at"`
	// Outcome is the terminal result of the invocation (verdict or
	// failure). Set when the activity returns; never mutated after.
	Outcome verdict.Outcome `json:"outcome"`
}

// PRSnapshot is the view of the PR each reviewer receives (spec 094 Key
// Entity "PRReviewSnapshot", R-SNAP). It is captured at the moment the
// workflow's first activity (CapturePRSnapshot) starts; later PR head
// movement does not invalidate in-flight reviews.
//
// The snapshot is content-hashed so the telemetry stream can reference
// "the PR state these reviewers saw" without re-shipping the diff (FR-032).
type PRSnapshot struct {
	Repo         string         `json:"repo"`
	PRNumber     int            `json:"pr_number"`
	HeadOID      string         `json:"head_oid"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Author       string         `json:"author"`
	BaseRef      string         `json:"base_ref"`
	Files        []PRFile       `json:"files"`
	SpecArtifacts []SpecArtifact `json:"spec_artifacts"`
	CapturedAt   time.Time      `json:"captured_at"`
}

// PRFile is one file-level diff entry within a PRSnapshot. Unified-diff
// text lives in Diff; the file path and add/delete line counts are top-level
// for fast filtering by the reviewer driver.
type PRFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Diff      string `json:"diff"`
}

// SpecArtifact is one in-repo spec-kit file the PR is bound to (spec.md,
// plan.md, contracts/*, data-model.md, research.md when present). Reviewer
// drivers consult these to ground their verdict in the contract the PR
// claims to satisfy.
type SpecArtifact struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
