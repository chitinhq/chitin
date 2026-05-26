package driver

// Capability is a tag from the closed capability taxonomy (FR-015). It is
// the shared vocabulary between a driver's CapabilityCard (what an agent
// can do) and a work unit's requirement (what a piece of work needs). The
// taxonomy is closed: every tag a card or a requirement names must be one
// of the constants below — IsKnownCapability rejects anything else, and the
// Registry treats an unknown tag on a card as a registration error.
type Capability string

const (
	// CapCodeImplement — write or modify production code to satisfy a spec
	// or task.
	CapCodeImplement Capability = "code.implement"
	// CapCodeReview — review a code change for correctness, safety, and
	// structure.
	CapCodeReview Capability = "code.review"
	// CapCodeRefactor — restructure existing code without changing behavior.
	CapCodeRefactor Capability = "code.refactor"
	// CapResearchWeb — research a question against the open web.
	CapResearchWeb Capability = "research.web"
	// CapResearchX — research against X (formerly Twitter).
	CapResearchX Capability = "research.x"
	// CapDocsWrite — author or update documentation, runbooks, and guides.
	CapDocsWrite Capability = "docs.write"
	// CapSpecAuthor — author a spec, plan, or task list (the spec-kit
	// artifacts).
	CapSpecAuthor Capability = "spec.author"
	// CapBulkCodegen — generate code in bulk — scaffolding, boilerplate,
	// repetitive transforms across many files.
	CapBulkCodegen Capability = "bulk.codegen"
	// CapTestAuthor — author tests for existing or planned code.
	CapTestAuthor Capability = "test.author"
	// CapBrowserAutomate — drive a browser to test or operate a web UI.
	CapBrowserAutomate Capability = "browser.automate"
	// CapSpecImplement — implement an entire spec (every task in
	// tasks.md) in a single driver invocation. Spec 119's whole-spec
	// dispatch mode routes against this capability; only T4-tier drivers
	// (opus-4.7-class, gpt-5.5-codex-class) should declare it, because
	// the per-invocation workload is much larger than a single-task
	// CapCodeImplement invocation. Distinct from CapCodeImplement so
	// existing per-task dispatch routing stays unchanged.
	CapSpecImplement Capability = "code.spec-implement"
)

// knownCapabilities is the closed set of taxonomy tags. It is the single
// source of truth for IsKnownCapability and KnownCapabilities; every
// Capability constant above must appear here.
var knownCapabilities = map[Capability]struct{}{
	CapCodeImplement:   {},
	CapCodeReview:      {},
	CapCodeRefactor:    {},
	CapResearchWeb:     {},
	CapResearchX:       {},
	CapDocsWrite:       {},
	CapSpecAuthor:      {},
	CapBulkCodegen:     {},
	CapTestAuthor:      {},
	CapBrowserAutomate: {},
	CapSpecImplement:   {},
}

// IsKnownCapability reports whether tag is a member of the closed capability
// taxonomy (FR-015). A capability card declaring a tag for which this
// returns false is rejected at registration with an "unknown capability"
// error — the taxonomy is never silently extended.
func IsKnownCapability(tag string) bool {
	_, ok := knownCapabilities[Capability(tag)]
	return ok
}

// KnownCapabilities returns the taxonomy tags in a deterministic order
// (lexical by tag string). It is for diagnostics and tests; it allocates a
// fresh slice on every call and never exposes the backing map. The lexical
// sort means callers — including error messages — get a stable list with
// no dependence on map iteration order.
func KnownCapabilities() []Capability {
	out := make([]Capability, 0, len(knownCapabilities))
	for c := range knownCapabilities {
		out = append(out, c)
	}
	sortCapabilities(out)
	return out
}

// sortCapabilities sorts a slice of Capability lexically in place. Kept
// local (insertion sort over a tiny, fixed taxonomy) so taxonomy.go stays
// import-free; determinism, not speed, is the goal.
func sortCapabilities(cs []Capability) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j-1] > cs[j]; j-- {
			cs[j-1], cs[j] = cs[j], cs[j-1]
		}
	}
}
