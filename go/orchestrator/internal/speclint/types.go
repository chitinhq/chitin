// Package speclint implements the deterministic spec-PR consistency linter
// described in spec 115 FR-003. Each rule (L01–L07) is one file; each rule
// reads a spec directory (`.specify/specs/NNN-*/`) and returns a slice of
// Violation values. The `chitin-orchestrator spec-lint` subcommand (spec 115
// T002) composes the rules and emits the aggregated slice as JSON on stdout.
//
// Rules are pure: no network, no mutation. The only I/O is reading the spec
// directory the caller passes in and, for L02, globbing the spec's siblings
// to resolve cross-references.
package speclint

// Severity values used in Violation.Severity. The closed set lives here so
// callers can switch on the strings without inventing new values.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Violation is one finding emitted by a spec-lint rule. The shape is the
// per-element schema of the JSON array `chitin-orchestrator spec-lint` writes
// to stdout (spec 115 FR-003: `[{rule, file, line, severity, message}]`).
//
// Rule is the rule id ("L01"…"L07"). File is the spec-relative filename the
// violation points at — "spec.md" or "tasks.md" — so the orchestrator can
// dedup posted PR review comments by (rule, file, line) per FR-004. Line is
// 1-based within File. Severity is one of the Severity* constants. Message is
// human-readable text intended to be posted verbatim as a PR review comment.
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}
