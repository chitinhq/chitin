// Package adapter is the kit-agnostic bridge of the orchestrator (spec 077).
// Specs arrive in different kits — GitHub spec-kit, OpenSpec, superpowers —
// each with its own files and conventions; the Spec-DAG Scheduler (spec 076)
// consumes exactly one shape, the normalized Work-Unit DAG. This package
// closes that gap: a single SpecKitAdapter interface, one concrete adapter
// per kit under a sub-package (speckit/, openspec/), and a Registry that
// detects which kit a repo uses. Adding a kit is adding one adapter — zero
// changes to the scheduler or orchestrator core (FR-002, SC-001).
//
// Compilation is a pure, deterministic, side-effect-free transform: spec
// files in, an in-memory dag.DAG out. The package takes no wall clock, opens
// no network connection, and writes no file — the same spec tree always
// compiles to the same DAG (plan.md Constraints). It runs inside a Temporal
// activity (Compile in compile.go); the dag package it produces is itself
// pure (spec 076), so the determinism that matters is provable by `go test`
// without a Temporal harness.
//
// The DAG schema (go/orchestrator/dag) and the capability taxonomy
// (go/orchestrator/driver) are imported, never redefined — spec 076 owns the
// output contract, spec 075 owns the closed capability vocabulary. Where a
// kit's artifacts leave a dependency or a capability ambiguous, an adapter
// emits the NeedsClarification marker (errors.go) rather than inventing one.
package adapter

import "github.com/chitinhq/chitin/go/orchestrator/dag"

// SpecKitAdapter is the uniform interface every spec kit is reached through
// (FR-001). The scheduler obtains a Work-Unit DAG ONLY by calling Compile on
// an adapter; it never reads a kit's files directly. A new kit is a new
// implementation of this interface and a registration line — nothing else
// (FR-002).
//
// Both methods are pure, deterministic, and side-effect-free with respect to
// the orchestrator's state: they read the repo on disk and return a value.
// They take no wall clock, perform no network I/O, and write nothing.
type SpecKitAdapter interface {
	// Kit returns the stable, lowercase name of the kit this adapter handles
	// — e.g. "speckit", "openspec". It is the registry key and the value an
	// operator passes to resolve an explicitly-chosen kit (FR-008).
	Kit() string

	// Detect reports whether repoPath is a repository that uses this
	// adapter's kit, by the presence of the kit's marker files (e.g.
	// `.specify/` for spec-kit). It reads only enough of the filesystem to
	// answer the question and never compiles. A non-nil error signals an I/O
	// fault probing the repo — not "kit absent", which is (false, nil).
	Detect(repoPath string) (bool, error)

	// Compile turns the spec identified by specRef within repoPath into a
	// normalized Work-Unit DAG (FR-003, FR-004). specRef names the spec
	// directory within the kit's layout — for spec-kit, the `NNN-name`
	// directory under `.specify/specs/` (or `specs/`). An empty specRef asks
	// the adapter to compile every spec the repo contains into one DAG.
	//
	// The returned DAG conforms to spec 076: one node per work unit, edges
	// from the kit's declared ordering, node IDs unique within the DAG.
	// Compile fails — returning a nil DAG — on a malformed artifact
	// (*MalformedArtifactError, FR-010) or a dangling dependency reference
	// (*DanglingReferenceError, FR-011); it never returns a partial DAG.
	Compile(repoPath, specRef string) (*dag.DAG, error)
}
