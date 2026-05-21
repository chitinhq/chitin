package adapter

import (
	"context"

	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

// CompileRequest is the typed input to the spec→DAG compile activity. It
// names the repo, the spec within it, and — when the repo uses more than one
// kit — the operator's explicit kit choice (FR-008). It carries no wall
// clock and no scheduling state: compilation depends only on the spec files
// on disk.
type CompileRequest struct {
	// RepoPath is the repository root to compile a spec from.
	RepoPath string
	// SpecRef names the spec within the kit's layout — for spec-kit, the
	// `NNN-name` directory. An empty SpecRef compiles every spec in the repo
	// into one DAG.
	SpecRef string
	// Kit is the explicitly-chosen kit name. It MUST be set when the repo
	// uses more than one kit; for a single-kit repo it may be left empty and
	// detection resolves the adapter (FR-008).
	Kit string
}

// CompileResult is the typed output of the compile activity: the normalized
// Work-Unit DAG and the kit it was compiled from. The DAG conforms to spec
// 076 and is handed to the scheduler unchanged.
type CompileResult struct {
	// Kit is the name of the kit the spec was compiled from — recorded so the
	// scheduler's audit trail names the source kit.
	Kit string
	// DAG is the normalized Work-Unit DAG. It is never nil on success.
	DAG *dag.DAG
}

// Compile is the spec→DAG compile activity entrypoint (FR-001, FR-003). It is
// a pure, deterministic, side-effect-free transform: it resolves the repo's
// adapter through the registry, invokes the adapter's Compile, validates the
// resulting DAG is acyclic, and returns it. It takes no wall clock, performs
// no network I/O, and writes nothing — the same request against the same
// spec files always yields the same DAG.
//
// The ctx parameter is accepted so Compile slots directly in as a Temporal
// activity (the scheduler's compile step, 076 FR-001); the transform itself
// neither reads nor cancels on it — compilation is fast and bounded by the
// spec's size, not by an external deadline.
//
// Compile fails — returning a nil DAG — when:
//
//   - the repo matches no kit (*UnrecognizedKitError) or matches several
//     with no explicit choice (*AmbiguousKitError) — FR-008;
//   - a kit artifact is malformed (*MalformedArtifactError) — FR-010;
//   - a task names a dependency that does not exist
//     (*DanglingReferenceError) — FR-011;
//   - the compiled graph is not acyclic — the error names the cycle.
//
// It never returns a partial DAG.
func Compile(_ context.Context, reg *Registry, req CompileRequest) (*CompileResult, error) {
	a, err := reg.Resolve(req.RepoPath, req.Kit)
	if err != nil {
		return nil, err
	}
	compiled, err := a.Compile(req.RepoPath, req.SpecRef)
	if err != nil {
		return nil, err
	}
	// FR-003: the output contract is an acyclic DAG. An adapter that emits a
	// cycle is a bug in the adapter, not a malformed input; surface it here
	// so no scheduler ever receives a non-acyclic graph. DAG.Acyclic also
	// catches dangling edges and names the offending cycle in its error.
	if err := compiled.Acyclic(); err != nil {
		return nil, err
	}
	return &CompileResult{Kit: a.Kit(), DAG: compiled}, nil
}
