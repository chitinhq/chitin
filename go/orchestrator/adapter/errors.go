package adapter

import "fmt"

// NeedsClarification is the sentinel a node carries — in place of a capability
// tag or a dependency edge — when a kit's artifacts leave that property
// ambiguous (FR-009, FR-014). It is deliberately a human-readable string with
// embedded spaces so it can never be mistaken for a taxonomy capability tag
// (driver.IsKnownCapability rejects it) and so it surfaces verbatim to the
// human-in-the-loop. An adapter MUST emit this marker rather than invent an
// edge or a capability.
const NeedsClarification = "NEEDS CLARIFICATION"

// MalformedArtifactError is the typed failure for an unparseable kit artifact
// (FR-010). Compilation that hits a malformed artifact MUST fail with this
// error and emit no partial DAG — the error names the file and the location
// (a 1-based line number, 0 when the fault is the whole file) plus a precise
// reason. Callers detect it with errors.As.
type MalformedArtifactError struct {
	// File is the kit artifact that failed to parse — a repo-relative path so
	// the message is stable regardless of where the repo is checked out.
	File string
	// Line is the 1-based line number of the fault. It is 0 when the fault is
	// the artifact as a whole (e.g. a required file is absent or empty).
	Line int
	// Reason is a precise, human-readable description of what was wrong.
	Reason string
}

// Error implements the error interface. It always names the file; it names
// the line only when one is known (Line > 0).
func (e *MalformedArtifactError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("malformed artifact %s:%d: %s", e.File, e.Line, e.Reason)
	}
	return fmt.Sprintf("malformed artifact %s: %s", e.File, e.Reason)
}

// malformed is a constructor for a line-scoped MalformedArtifactError.
func malformed(file string, line int, reason string) *MalformedArtifactError {
	return &MalformedArtifactError{File: file, Line: line, Reason: reason}
}

// DanglingReferenceError is the typed failure for a task that declares a
// dependency on a task id that does not exist in the same spec (FR-011).
// Compilation MUST fail with this error and emit no partial DAG. The error
// names both the referencing node and the missing target so the fault can be
// fixed without re-reading the kit. Callers detect it with errors.As.
type DanglingReferenceError struct {
	// File is the artifact the dangling reference was read from.
	File string
	// From is the task id that declared the dependency.
	From string
	// MissingTarget is the dependency task id that has no matching task.
	MissingTarget string
}

// Error implements the error interface, naming the missing target (FR-011).
func (e *DanglingReferenceError) Error() string {
	return fmt.Sprintf(
		"dangling dependency in %s: task %q depends on %q, which does not exist",
		e.File, e.From, e.MissingTarget,
	)
}

// UnrecognizedKitError is returned by the registry when a repo matches no
// registered kit's detection markers (FR-008). It is reported explicitly
// rather than guessing a kit.
type UnrecognizedKitError struct {
	// RepoPath is the repository root that matched no kit.
	RepoPath string
}

// Error implements the error interface.
func (e *UnrecognizedKitError) Error() string {
	return fmt.Sprintf("no recognized spec kit detected in %s", e.RepoPath)
}

// AmbiguousKitError is returned by the registry when a repo matches more than
// one kit's detection markers (FR-008). chitin itself carries both `.specify/`
// and `docs/superpowers/`; detection MUST require an explicit choice rather
// than pick one silently. The error lists every kit that matched so the
// operator can choose.
type AmbiguousKitError struct {
	// RepoPath is the repository root that matched multiple kits.
	RepoPath string
	// Kits is the sorted list of kit names whose markers were all present.
	Kits []string
}

// Error implements the error interface, listing the kits that matched.
func (e *AmbiguousKitError) Error() string {
	return fmt.Sprintf(
		"ambiguous spec kit in %s: %v matched — pass an explicit kit choice",
		e.RepoPath, e.Kits,
	)
}
