// Package verdict defines the StructuredVerdict shape that every reviewer
// invocation — primary, arbiter, machine, or operator — returns (spec 094
// FR-013), the invariant validator that enforces the per-enum contract
// (FR-014), and the pure aggregation function that turns a pair of primary
// outcomes plus an optional arbiter outcome into a ReviewGateDecision
// (FR-009 through FR-012, R-AGG).
//
// The package is intentionally Temporal-free: a verdict is a value, and
// aggregating verdicts is closed-form arithmetic on values. Keeping the
// package import-clean lets the workflow call Aggregate from inside a
// Temporal workflow function without breaching determinism, and lets the
// invariant tests run as plain `go test` with no testsuite scaffolding.
package verdict
