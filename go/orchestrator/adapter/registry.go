package adapter

import (
	"fmt"
	"sort"
)

// Registry is the set of available SpecKitAdapters and the per-repo kit
// detector (FR-008). It answers two questions: "which kit does this repo
// use?" (Detect) and "give me the adapter for it" (Resolve / AdapterFor).
//
// The registry is in-memory and populated at orchestrator startup. It holds
// no datastore. A Registry value must be created with NewRegistry; the zero
// value is not usable. Register all adapters at startup, then treat the
// Registry as read-only.
type Registry struct {
	// byKit holds adapters keyed by their stable kit name. It is only ever
	// used for lookup and membership — never iterated for ordering. Every
	// ordered result comes from an explicit sort (determinism).
	byKit map[string]SpecKitAdapter
}

// NewRegistry returns an empty Registry ready for Register calls.
func NewRegistry() *Registry {
	return &Registry{byKit: make(map[string]SpecKitAdapter)}
}

// Register admits one adapter into the registry. It returns an error if the
// adapter is nil, declares an empty kit name, or names a kit already
// registered — kit names are the registry's primary key and must be unique.
func (r *Registry) Register(a SpecKitAdapter) error {
	if a == nil {
		return fmt.Errorf("adapter: cannot register a nil adapter")
	}
	kit := a.Kit()
	if kit == "" {
		return fmt.Errorf("adapter: cannot register an adapter with an empty kit name")
	}
	if _, exists := r.byKit[kit]; exists {
		return fmt.Errorf("adapter: kit %q already registered", kit)
	}
	r.byKit[kit] = a
	return nil
}

// Len reports how many adapters are registered.
func (r *Registry) Len() int { return len(r.byKit) }

// AdapterFor returns the registered adapter for the named kit, or
// (nil, false) if no adapter is registered under that name.
func (r *Registry) AdapterFor(kit string) (SpecKitAdapter, bool) {
	a, ok := r.byKit[kit]
	return a, ok
}

// Kits returns every registered kit name, sorted lexically. The slice is
// freshly allocated; the order has no dependence on map iteration.
func (r *Registry) Kits() []string {
	out := make([]string, 0, len(r.byKit))
	for kit := range r.byKit {
		out = append(out, kit)
	}
	sort.Strings(out)
	return out
}

// DetectKits runs every registered adapter's Detect against repoPath and
// returns the sorted list of kit names whose markers are present. An empty
// result means no recognized kit; a result of length > 1 means the repo uses
// more than one kit and a caller must require an explicit choice (FR-008).
//
// A detection probe that fails with an I/O error aborts DetectKits — a
// repo whose layout cannot be read is reported, not silently treated as
// "kit absent".
func (r *Registry) DetectKits(repoPath string) ([]string, error) {
	var detected []string
	for _, kit := range r.Kits() { // sorted — deterministic probe order
		ok, err := r.byKit[kit].Detect(repoPath)
		if err != nil {
			return nil, fmt.Errorf("adapter: detecting kit %q in %s: %w", kit, repoPath, err)
		}
		if ok {
			detected = append(detected, kit)
		}
	}
	return detected, nil
}

// Resolve selects the one adapter for repoPath (FR-008). It runs detection
// and then applies the explicit-choice rule:
//
//   - zero kits detected            → *UnrecognizedKitError.
//   - exactly one kit detected      → that kit's adapter.
//   - more than one kit detected,
//     and chosenKit is empty        → *AmbiguousKitError listing the matches.
//   - more than one kit detected,
//     and chosenKit names a match   → the chosen kit's adapter.
//   - chosenKit names a kit not
//     among the detected matches    → an error rejecting the choice.
//
// chosenKit lets an operator disambiguate a repo that genuinely uses two
// kits — chitin itself carries both `.specify/` and `docs/superpowers/`. The
// registry never picks for them.
func (r *Registry) Resolve(repoPath, chosenKit string) (SpecKitAdapter, error) {
	detected, err := r.DetectKits(repoPath)
	if err != nil {
		return nil, err
	}
	switch len(detected) {
	case 0:
		return nil, &UnrecognizedKitError{RepoPath: repoPath}
	case 1:
		if chosenKit != "" && chosenKit != detected[0] {
			return nil, fmt.Errorf(
				"adapter: explicit kit %q not detected in %s — detected %q",
				chosenKit, repoPath, detected[0],
			)
		}
		return r.byKit[detected[0]], nil
	default:
		if chosenKit == "" {
			return nil, &AmbiguousKitError{RepoPath: repoPath, Kits: detected}
		}
		for _, kit := range detected {
			if kit == chosenKit {
				return r.byKit[kit], nil
			}
		}
		return nil, fmt.Errorf(
			"adapter: explicit kit %q is not among the kits detected in %s (%v)",
			chosenKit, repoPath, detected,
		)
	}
}
