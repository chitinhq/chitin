# Phase 1 Data Model: PR Review Mechanism

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Research**: [research.md](./research.md) | **Date**: 2026-05-23

## Purpose

Codify the data shapes the PR review workflow operates on. Each entity below is a Go type in the `verdict/` or `review/` sub-package; types named in spec.md (Key Entities) map to one entity each.

Three layers of types:

1. **Workflow I/O types** (`PRReviewInput`, `ReviewGateDecision`) — the contract between `PRReviewWorkflow` and its caller (`PRMergeWorkflow`).
2. **Activity I/O types** (`ReviewerSlate`, `ReviewerInvocation`, `ReviewerOutcome`) — the contract between the workflow and its activities.
3. **Verdict types** (`StructuredVerdict`, `VerdictEnum`) — the contract between the orchestrator and the reviewer drivers, validated by `verdict.Validate(...)`.

---

## Workflow I/O

### `PRReviewInput`

Argument passed to `PRReviewWorkflow` when `PRMergeWorkflow` spawns it.

```go
type PRReviewInput struct {
    Repo        string       // owner/repo, e.g. "chitinhq/chitin"
    PRNumber    int          // GitHub PR number
    PRAuthor    string       // GitHub login of the PR's author (used for FR-005 exclusion)
    PolicyClass string       // One of governance, spec-only, impl, live-fix, bookkeeping, research-docs
    ArbiterType ArbiterType  // operator | machine — derived from spec 093 policy table
    Snapshot    PRSnapshot   // Captured at workflow start, immutable for the workflow's lifetime
}

type ArbiterType string
const (
    ArbiterOperator ArbiterType = "operator"
    ArbiterMachine  ArbiterType = "machine"
)
```

### `ReviewGateDecision`

Return value from `PRReviewWorkflow` to `PRMergeWorkflow`.

```go
type ReviewGateDecision struct {
    State           GateState  // passed | blocked | halted
    Reason          string     // Human-readable reason (e.g., "both primaries approve")
    ArbiterEngaged  bool       // True iff an arbiter was dispatched
    Primaries       [2]ReviewerOutcome
    Arbiter         *ReviewerOutcome // nil iff !ArbiterEngaged
    SnapshotHeadOID string      // PR head SHA captured at workflow start
}

type GateState string
const (
    GatePassed  GateState = "passed"
    GateBlocked GateState = "blocked"
    GateHalted  GateState = "halted"
)
```

**Maps to spec entities**: `ReviewGateDecision` (spec Key Entity) = this type. `DialecticResult` (spec Key Entity) is captured by the same struct — the "aggregate of all per-reviewer outcomes" is `Primaries` + `Arbiter`; the "gate decision derived from FR-009 through FR-012" is `State` + `Reason`.

---

## Activity I/O

### `PRSnapshot`

The PR view captured at workflow start; immutable for the workflow's lifetime. Constructed by the select-reviewers activity in its first call to `gh pr view`.

```go
type PRSnapshot struct {
    Repo         string
    PRNumber     int
    HeadOID      string                  // SHA at capture
    Title        string
    Body         string
    Author       string
    BaseRef      string                  // e.g., "main"
    Files        []PRFile                // file paths + per-file diff
    SpecArtifacts []SpecArtifact         // spec.md, plan.md, contracts/*, data-model.md, research.md when present
    CapturedAt   time.Time
}

type PRFile struct {
    Path      string
    Additions int
    Deletions int
    Diff      string // unified-diff hunks
}

type SpecArtifact struct {
    Path    string // e.g., "specs/094-pr-review-mechanism/spec.md"
    Content string // raw text
}
```

**Maps to spec entity**: `PRReviewSnapshot` (spec Key Entity) = this type, renamed `PRSnapshot` in code for brevity.

**Content hashing for telemetry**: Each reviewer invocation's telemetry event (FR-032) carries SHA-256 hashes of `Files` and `SpecArtifacts` so the snapshot can be reconstructed-or-confirmed from history without re-shipping the raw content in the OTLP stream.

### `ReviewerSlate`

Return value from `SelectReviewers` activity. The pool selection result before any dispatch.

```go
type ReviewerSlate struct {
    Primary1   DriverID // The first primary slot
    Primary2   DriverID // The second primary slot
    Arbiter    *DriverID // The third reviewer-tagged driver if available and class wants machine arbiter, else nil
    ExcludedAuthor *DriverID // The author's mapped driver id, if any (recorded for telemetry attribution)
    EligibleAfterExclusion []DriverID // The full eligible pool after author exclusion, for shortfall diagnostics
}

type DriverID string // e.g., "hermes", "openclaw"
```

The `Arbiter` field is filled only if `ArbiterType == ArbiterMachine` and the eligible pool has a third driver after both primaries and the author are excluded. For `ArbiterType == ArbiterOperator`, `Arbiter` is nil and the operator surface is used instead.

**Shortfall behaviour (FR-007)**: If `len(EligibleAfterExclusion) < 2` (can't fill both primary slots), the activity returns an error with the shortfall counts, and the workflow halts at selection per Acceptance Scenario 4.2.

### `ReviewerInvocation`

A single dispatch of a reviewer for a specific role. Produced when the workflow starts a per-reviewer activity; closed when the activity returns a `ReviewerOutcome`.

```go
type ReviewerInvocation struct {
    InvocationID string   // ULID generated at dispatch
    DriverID     DriverID // The driver assigned to this slot, or "operator" for OperatorArbiterDispatch
    Role         Role     // primary | arbiter
    SnapshotRef  string   // SHA-256 of the canonical PRSnapshot
    StartedAt    time.Time
    Outcome      ReviewerOutcome
}

type Role string
const (
    RolePrimary Role = "primary"
    RoleArbiter Role = "arbiter"
)
```

**Maps to spec entity**: `ReviewerInvocation` (spec Key Entity) = this type. `OperatorArbiterDispatch` (spec Key Entity) is `ReviewerInvocation` with `DriverID == "operator"` and `Role == RoleArbiter`.

### `ReviewerOutcome`

The terminal state of one invocation: either a validated `StructuredVerdict` or a failure.

```go
type ReviewerOutcome struct {
    InvocationID string
    DriverID     DriverID
    Role         Role
    Verdict      *StructuredVerdict // nil iff Failure != nil
    Failure      *FailureReason     // nil iff Verdict != nil
    ElapsedMS    int64
}

type FailureReason struct {
    Kind FailureKind
    Detail string // free-text diagnostic
}

type FailureKind string
const (
    FailureTimeout       FailureKind = "timeout"
    FailureError         FailureKind = "error"          // driver returned a non-zero error
    FailureMalformedJSON FailureKind = "malformed_json" // verdict could not parse
    FailureMalformedShape FailureKind = "malformed_shape" // verdict parsed but violates FR-014 invariants
    FailureCancelled     FailureKind = "cancelled"     // workflow cancelled the dispatch
)

// Convenience predicates used by the aggregator (R-AGG).
func (o ReviewerOutcome) IsApproveShaped() bool {
    return o.Verdict != nil && (o.Verdict.Verdict == VerdictApprove || o.Verdict.Verdict == VerdictApproveWithComments)
}
func (o ReviewerOutcome) IsRequestChanges() bool {
    return o.Verdict != nil && o.Verdict.Verdict == VerdictRequestChanges
}
func (o ReviewerOutcome) IsFailure() bool {
    return o.Failure != nil
}
```

---

## Verdict types

### `StructuredVerdict` (FR-013, FR-014)

```go
type StructuredVerdict struct {
    Verdict         VerdictEnum `json:"verdict"`
    Concerns        []string    `json:"concerns"`         // free-text strings
    Recommendations []string    `json:"recommendations"`  // free-text strings
    Blockers        []string    `json:"blockers"`         // free-text strings
    Reason          string      `json:"reason,omitempty"` // optional; used only for abstain
}

type VerdictEnum string
const (
    VerdictApprove              VerdictEnum = "approve"
    VerdictApproveWithComments  VerdictEnum = "approve-with-comments"
    VerdictRequestChanges       VerdictEnum = "request-changes"
    VerdictAbstain              VerdictEnum = "abstain"
)
```

**Maps to spec entity**: `StructuredVerdict` (spec Key Entity) = this type.

### `Validate` (FR-014)

Pure function in `verdict/invariants.go`. Returns either `nil` or a `FailureReason{Kind: FailureMalformedShape, Detail: ...}` describing the violated invariant. The four invariants from FR-014 are encoded as:

```go
func Validate(v StructuredVerdict) error {
    switch v.Verdict {
    case VerdictApprove:
        if len(v.Blockers) != 0 {
            return fmt.Errorf("approve verdict must have empty blockers (got %d)", len(v.Blockers))
        }
    case VerdictApproveWithComments:
        if len(v.Blockers) != 0 {
            return fmt.Errorf("approve-with-comments verdict must have empty blockers (got %d)", len(v.Blockers))
        }
        if len(v.Concerns) == 0 && len(v.Recommendations) == 0 {
            return fmt.Errorf("approve-with-comments verdict must have at least one concern or recommendation")
        }
    case VerdictRequestChanges:
        if len(v.Blockers) == 0 {
            return fmt.Errorf("request-changes verdict must have non-empty blockers")
        }
    case VerdictAbstain:
        if len(v.Concerns) != 0 || len(v.Recommendations) != 0 || len(v.Blockers) != 0 {
            return fmt.Errorf("abstain verdict must have all three lists empty")
        }
    default:
        return fmt.Errorf("invalid verdict enum value: %q", v.Verdict)
    }
    return nil
}
```

Validated verdicts are recorded immutably in workflow history (FR-034); the activity returning the verdict to the workflow has already validated it, so the workflow never observes a malformed `StructuredVerdict`.

### `Aggregate` (R-AGG → FR-009 through FR-012)

Pure function in `verdict/aggregate.go`. Called from inside `PRReviewWorkflow`. Signature:

```go
func Aggregate(p1, p2 ReviewerOutcome, arbiter *ReviewerOutcome) ReviewGateDecision
```

Decision tree (table form):

| `p1` is | `p2` is | Result without arbiter | Arbiter needed? |
|---|---|---|---|
| approve-shaped | approve-shaped | `passed`, "both primaries approve" | no |
| request-changes | request-changes | `blocked`, "both primaries request-changes" | no |
| any other combination (including any failure on either side, any abstain, mixed approve-shaped + request-changes) | | (no decision without arbiter) | yes |

When arbiter is engaged (FR-012), the gate decision derives from the arbiter's outcome:

| Arbiter is | Gate state | Reason |
|---|---|---|
| approve-shaped | `passed` | "arbiter approves" |
| `request-changes` | `blocked` | "arbiter requests changes: " + first blocker |
| `abstain` | `halted` | "arbiter abstained: " + reason |
| failure | `halted` | "arbiter failed: " + failure detail |

**Edge case** — if `Aggregate` is called with `arbiter == nil` for a case that requires arbiter, it returns `halted, "arbiter required but not dispatched"`. This is a defensive case; the workflow code is structured so this is unreachable in normal flow.

---

## Registry extension (spec 075 v1.x)

This spec adds two things to the existing driver-registry metadata:

### `reviewer` capability tag

```go
const CapabilityReviewer Capability = "reviewer"
```

Added to the existing `Capability` enum in `registry/capability.go`. Drivers declare it in their registry entry to be eligible for primary or machine-arbiter selection.

### `ReviewMode` shape on the driver entry

```go
type DriverEntry struct {
    // ... existing fields ...
    GitIdentity string         `json:"git_identity"` // exists at v1.1 — see R-AUTHORID
    ReviewMode  *ReviewMode    `json:"review_mode,omitempty"` // NEW — required iff CapabilityReviewer is declared
}

type ReviewMode struct {
    ToolName       string  `json:"tool_name"`       // The tool name the driver exposes for review (e.g., "review")
    PromptTemplate string  `json:"prompt_template"` // The driver's own review-mode prompt content (per FR-003)
    MaxBytesIn     int     `json:"max_bytes_in"`    // Driver-self-declared limit on PRSnapshot bytes
}
```

The orchestrator's responsibility is the tool's input/output contract (per `contracts/review-mode-driver-contract.md`); the driver's `PromptTemplate` is opaque to the orchestrator.

### `SelectDriver` signature extension (spec 076 v1.x)

```go
type SelectDriverInput struct {
    // ... existing fields ...
    RequireCapability  Capability `json:"require_capability,omitempty"` // NEW
    ExcludeIdentities  []DriverID `json:"exclude_identities,omitempty"` // NEW
}
```

Additive — existing callers pass neither field and get unchanged behaviour.

---

## Telemetry event shape (FR-032)

Emitted once per `ReviewerInvocation` close.

```go
type ReviewerInvocationTelemetry struct {
    // OTLP common
    Timestamp       time.Time
    WorkflowID      string
    Repo            string
    PRNumber        int

    // Reviewer-invocation specific
    InvocationID    string
    DriverID        string   // "operator" for operator-arbiter
    Role            string   // "primary" | "arbiter"
    Verdict         string   // verdict enum value, or "" if failed
    FailureKind     string   // "" if Verdict is set
    ElapsedMS       int64
    SnapshotHashRef string   // SHA-256 of the snapshot

    // Content hashes (raw text lives in workflow history, not the OTLP stream)
    ConcernsHash        string // hex(SHA-256(json(Concerns)))
    RecommendationsHash string
    BlockersHash        string
}
```

Workflow history holds the raw text (FR-033/034); the OTLP stream holds only hashes so external observers can verify integrity without storing free text twice.

---

## Persistence layout summary

| Where | What |
|---|---|
| Temporal workflow history | Full `StructuredVerdict` raw text, `ReviewerInvocation` records, `ReviewGateDecision` outcome — SYSTEM OF RECORD (FR-033). |
| OTLP telemetry stream | One `ReviewerInvocationTelemetry` event per invocation. Content hashes only. |
| Driver registry (disk) | Capability tag + `ReviewMode` per driver entry. |
| (nothing else) | No new database, no new file store, no new cache. |

---

## Index of spec.md Key Entities → this file's types

| Spec entity | Type in this file | Notes |
|---|---|---|
| `ReviewerDriver` | `DriverEntry` (registry, extended) | Adds capability tag + `ReviewMode` |
| `ReviewerPool` | `ReviewerSlate.EligibleAfterExclusion` | The filtered set after no-self-review |
| `ReviewerRole` | `Role` (`primary` \| `arbiter`) | enum |
| `ReviewerInvocation` | `ReviewerInvocation` | direct |
| `StructuredVerdict` | `StructuredVerdict` | direct, with `Validate` enforcing FR-014 |
| `DialecticResult` | `ReviewGateDecision` | merged into the gate decision struct |
| `ReviewGateDecision` | `ReviewGateDecision` | direct |
| `OperatorArbiterDispatch` | `ReviewerInvocation` w/ `DriverID == "operator"` | not a separate type — same shape, different value |
| `PRReviewSnapshot` | `PRSnapshot` | direct, with content hashing for telemetry |
