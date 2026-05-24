# Contract: spec 093 policy-table amendment v1.1.0

**Spec reference**: FR-035, FR-037 | **Research**: R-POLICYAMEND

This file describes the amendment that spec 094 requires spec 093 to make to its policy table. The amendment lands as a separate PR after both specs ratify (spec 093 v1.1.0). This file is the source-of-truth document; when the amendment ships, spec 093's `contracts/policy-table.md` is updated to incorporate the two new columns described below.

## Amendment shape

Two new columns added to spec 093's existing 6-class policy table:

| New column | Type | Purpose |
|---|---|---|
| `review_required` | bool | If true, `PRMergeWorkflow` spawns a `PRReviewWorkflow` child before attempting merge. |
| `arbiter_type` | enum (`operator` \| `machine`) | If review is dispatched and primaries disagree, this picks the arbiter route per FR-016 through FR-020. |

The existing policy table columns (e.g., `requires_signal`, `auto_resolves_pointer_files`, etc. — defined in spec 093) are unchanged. The amendment is purely additive.

## v1.1.0 column values (per-class)

| Class | `review_required` | `arbiter_type` |
|---|---|---|
| `governance` | `true` | `operator` |
| `spec-only` | `true` | `operator` |
| `impl` | `true` | `operator` (v1.1.0 — operationally degenerate; see below) |
| `live-fix` | `false` | (n/a) |
| `bookkeeping` | `false` | (n/a) |
| `research-docs` | `true` | `operator` (v1.1.0 — operationally degenerate; see below) |

**v1.1.0 operational degeneracy**: per spec 094 Assumptions ("v1 known limitation — machine arbiter not operationally viable"), at the moment spec 094 v1 ships, only two reviewer-tagged drivers exist (`hermes`, `openclaw`). With two drivers, the dialectic can fill both primary slots but cannot fill a third machine-arbiter slot after author exclusion. Therefore at v1.1.0 the `arbiter_type` for `impl` and `research-docs` is set to `operator` — every disagreement on those classes lands on the operator until a third reviewer-tagged driver (codex, copilot, gemini, local-llm) is added.

When the third driver is added (spec 094 v1.1 or later, no spec 094 code change required per SC-008), the policy table will be amended again — `impl` and `research-docs` `arbiter_type` flips to `machine`. That subsequent amendment is a policy-table-only change with no spec, no plan, no code change.

## Class invariants the amendment preserves

- `governance` MUST have `arbiter_type: operator` (FR-020). This is enforced at policy-table load time: the orchestrator refuses to start with a policy table where `governance` has any other value.
- `spec-only` MUST have `arbiter_type: operator` (FR-020). Same load-time check.
- The other four classes MAY use either value.

## YAML representation in spec 093's `contracts/policy-table.md`

After this amendment lands, the policy table file looks like (excerpt):

```yaml
policy_table:
  version: 1.1.0
  classes:
    governance:
      file_patterns: ["constitution.md", "constitution/**"]
      requires_signal: true
      auto_resolves_pointer_files: false
      # ... existing columns ...
      review_required: true       # NEW in v1.1.0
      arbiter_type: operator      # NEW in v1.1.0
    spec-only:
      file_patterns: ["specs/**", ".specify/specs/**"]
      requires_signal: false
      # ...
      review_required: true
      arbiter_type: operator
    impl:
      # ...
      review_required: true
      arbiter_type: operator      # operationally degenerate until 3rd reviewer driver exists
    live-fix:
      # ...
      review_required: false
      arbiter_type: null
    bookkeeping:
      # ...
      review_required: false
      arbiter_type: null
    research-docs:
      # ...
      review_required: true
      arbiter_type: operator      # same operational degeneracy as impl
```

(The full table — including all existing columns — lives in spec 093's `contracts/policy-table.md`. Only the additions are shown above.)

## Migration

Existing v1.0.0 policy tables in the wild (if any) are migrated by setting both new columns to safe defaults during load:

- `review_required` defaults to `false` (preserves v1.0.0 behaviour for any caller that hasn't updated their table file).
- `arbiter_type` defaults to `operator` (safest when `review_required` is true).

A v1.0.0 policy table thus continues to work unchanged. To enable review, the policy table file must be re-saved with v1.1.0's column values.

## Where this amendment lands in the codebase

Spec 093's `contracts/policy-table.md` is the canonical YAML. The Go struct mirror lives in `go/orchestrator/activities/merge/policy/policy_table.go`. The amendment adds two struct fields:

```go
type PolicyClass struct {
    // ... existing fields ...
    ReviewRequired bool        `yaml:"review_required"`
    ArbiterType    ArbiterType `yaml:"arbiter_type"`
}
```

And the load-time class-invariant check:

```go
func (p *PolicyTable) Validate() error {
    if p.Classes["governance"].ArbiterType != ArbiterOperator {
        return errors.New("governance class must use operator arbiter (FR-020)")
    }
    if p.Classes["spec-only"].ArbiterType != ArbiterOperator {
        return errors.New("spec-only class must use operator arbiter (FR-020)")
    }
    return nil
}
```

## Coordination

This amendment must be applied by a separate PR (spec 093 v1.1.0) **after** spec 094 ships. The PR has three components:

1. Update `contracts/policy-table.md` in spec 093's directory to add the two columns with the values above.
2. Update `go/orchestrator/activities/merge/policy/policy_table.go` to add the two struct fields and the `Validate()` invariant check.
3. Update `go/orchestrator/workflows/pr_merge.go` to consult `class.ReviewRequired` and, if true, spawn a `PRReviewWorkflow` child with `class.ArbiterType` in the input.

Steps 1–2 can be tested without step 3 (table loads, validates, exposes the new fields). Step 3 is the actual integration with the workflow spawned by spec 094.

The amendment is small enough to be a single PR scoped to spec 093 v1.1.0 with no spec re-open required.
