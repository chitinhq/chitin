// Package adapter defines the SpecAdapter interface and the global registry
// that maps source frameworks to concrete adapter implementations.
//
// An adapter is a pure function: parse(source) → UnifiedSpec, plus
// detect(path) → bool. Adapters are stateless, deterministic (same source
// ⇒ same UnifiedSpec), and side-effect-free. Registration is a single list
// so adding a new framework is one entry (R2, spec 061).
package adapter

import (
	"fmt"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/spec"
)

// ParseError is a typed error raised when an adapter fails to parse a spec
// file. It carries the file path and the section that failed, satisfying
// boundary case 3 (spec 061): "parse raises a typed error naming the file
// and the failed section; it never returns a half-populated model."
type ParseError struct {
	Path    string
	Section string
	Err     error
}

func (e *ParseError) Error() string {
	if e.Section != "" {
		return fmt.Sprintf("spec parse error in %s (section %s): %v", e.Path, e.Section, e.Err)
	}
	return fmt.Sprintf("spec parse error in %s: %v", e.Path, e.Err)
}

func (e *ParseError) Unwrap() error { return e.Err }

// DuplicateIDError is raised when two spec directories share the same spec_id
// prefix, satisfying boundary case 2 (spec 061): "the adapter surfaces the
// collision as an error, never silently picks one."
type DuplicateIDError struct {
	ID    string
	Paths []string
}

func (e *DuplicateIDError) Error() string {
	return fmt.Sprintf("duplicate spec_id %q found in: %v", e.ID, e.Paths)
}

// SpecAdapter is the interface that all framework adapters must implement.
// R2 (spec 061): "An adapter is a pure function parse(source) → UnifiedSpec
// plus a detect(path) → bool."
type SpecAdapter interface {
	// Detect returns true if the adapter recognises the file/directory at path
	// as a spec in its framework's format.
	Detect(path string) bool

	// Parse reads the spec at path and returns a fully-populated UnifiedSpec.
	// On malformed input, Parse returns a *ParseError — never a
	// half-populated UnifiedSpec (boundary case 3).
	Parse(path string) (*spec.UnifiedSpec, error)

	// Framework returns the SourceFramework constant for this adapter.
	Framework() spec.SourceFramework
}

// registry holds all registered adapters, keyed by SourceFramework.
var registry = map[spec.SourceFramework]SpecAdapter{}

// Register adds an adapter to the global registry. Panics if an adapter for
// the same framework is already registered.
func Register(a SpecAdapter) {
	fw := a.Framework()
	if _, dup := registry[fw]; dup {
		panic(fmt.Sprintf("adapter already registered for framework %q", fw))
	}
	registry[fw] = a
}

// Lookup returns the adapter for the given framework, or nil if none is
// registered.
func Lookup(fw spec.SourceFramework) SpecAdapter {
	return registry[fw]
}

// All returns all registered adapters.
func All() map[spec.SourceFramework]SpecAdapter {
	return registry
}

// DetectAdapters returns all adapters whose Detect method returns true for
// the given path. If more than one adapter detects the path, that is a
// configuration error — the caller should raise it.
func DetectAdapters(path string) []SpecAdapter {
	var matches []SpecAdapter
	for _, a := range registry {
		if a.Detect(path) {
			matches = append(matches, a)
		}
	}
	return matches
}