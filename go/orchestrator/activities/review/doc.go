// Package review hosts the activities and shared types of the PR review
// workflow (spec 094). The verdict sub-package owns the StructuredVerdict
// schema, its FR-014 invariants, and the Aggregate function; this package
// owns the side-effecting activities (driver dispatch, telemetry emit,
// operator notification) and the I/O types passed between the workflow and
// those activities.
//
// Activities here are Temporal activities — they perform I/O (driver tool
// invocation, OTLP telemetry write, Discord notify, GitHub comment
// polling) and therefore cannot be called from inside workflow code. The
// PRReviewWorkflow function (in the parent workflows/ package) is what
// orchestrates these activities into the dialectic gate.
package review
