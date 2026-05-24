// context_keyword_test.go — spec 106 FR-006/007 tests for the keyword
// matcher coverage + precedence rule introduced by the same spec.

package adapter_test

import (
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TestMapCapability_NewKeywordsSpec106 — FR-006. One unit test case
// per FR-001 keyword extension. Real wording sourced from spec 097's
// tasks.md (the dogfood that drove this spec).
func TestMapCapability_NewKeywordsSpec106(t *testing.T) {
	cases := []struct {
		name    string
		desc    string
		wantCap driver.Capability
	}{
		// Spec 106 FR-001 new CapTestAuthor keywords:
		{"integration test", "Integration test for the schedule round-trip", driver.CapTestAuthor},
		{"regression test", "Regression test for the kanban-dispatch zero-commit path", driver.CapTestAuthor},
		{"smoke test", "Smoke test the dispatch chain", driver.CapTestAuthor},
		{"argv parsing test", "Argv parsing test in schedule_argv_test.go", driver.CapTestAuthor},

		// Spec 106 FR-001 new CapDocsWrite keywords:
		{"operator runbook", "Operator runbook at docs/operator/scheduling.md", driver.CapDocsWrite},
		{"developer doc", "Developer doc for the orchestrator subcommands", driver.CapDocsWrite},
		{"update docs", "Update docs to mention the new subcommands", driver.CapDocsWrite},
		{"changelog entry", "CHANGELOG entry for the next release", driver.CapDocsWrite},
		{"changelog", "Add a CHANGELOG line under Unreleased", driver.CapDocsWrite},

		// Spec 106 FR-001 new CapSpecAuthor keywords:
		{"draft a spec", "Draft a spec for the new feature", driver.CapSpecAuthor},
		{"update the spec", "Update the spec to address Copilot's review", driver.CapSpecAuthor},
		{"fixture spec", "Create fixture spec for the dispatcher tests", driver.CapSpecAuthor},

		// Spec 106 FR-001 new CapCodeImplement keywords:
		{"add a (X)", "Add a helper function buildRegistry to main.go", driver.CapCodeImplement},
		{"create a (X)", "Create a worker host entry point", driver.CapCodeImplement},
		{"add the flag", "Add the flag --temporal-host to schedule", driver.CapCodeImplement},
		{"add the option", "Add the option --json to status", driver.CapCodeImplement},
		{"extract the", "Extract the registration block from main.go", driver.CapCodeImplement},
		{"construction helper", "Driver-registry construction helper buildRegistry", driver.CapCodeImplement},
		{"add the subcommand", "Add the subcommand dispatcher to main.go", driver.CapCodeImplement},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := adapter.MapCapability(c.desc)
			if !ok {
				t.Fatalf("MapCapability(%q) returned ok=false; want %s", c.desc, c.wantCap)
			}
			if got != c.wantCap {
				t.Errorf("MapCapability(%q) = %s; want %s", c.desc, got, c.wantCap)
			}
		})
	}
}

// TestMapCapability_PrecedenceOnMultiMatch — FR-007. When the description
// hits keywords from multiple capabilities, the higher-precedence
// capability wins per capabilityPrecedence
// (test.author > docs.write > spec.author > code.refactor > bulk.codegen
// > code.implement).
func TestMapCapability_PrecedenceOnMultiMatch(t *testing.T) {
	cases := []struct {
		name    string
		desc    string
		wantCap driver.Capability
	}{
		// test.author > code.implement
		{"test_vs_impl", "Implement the handler with unit tests", driver.CapTestAuthor},
		// docs.write > code.implement (catch-all loses)
		{"docs_vs_impl", "Implement the doc generator — add the runbook", driver.CapDocsWrite},
		// spec.author > code.implement
		{"spec_vs_impl", "Implement the changes to spec.md", driver.CapSpecAuthor},
		// test.author > docs.write (tests beats docs)
		{"test_vs_docs", "Update the doc and add a unit test for the runbook", driver.CapTestAuthor},
		// test.author > spec.author (tests beats specs)
		{"test_vs_spec", "Author test for the spec.md compiler", driver.CapTestAuthor},
		// code.refactor > code.implement (more specific verb)
		{"refactor_vs_impl", "Implement the change by refactoring the module", driver.CapCodeRefactor},
		// docs.write > spec.author (docs wins over spec.md mention)
		{"docs_vs_spec", "Update the doc that mentions spec.md", driver.CapDocsWrite},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := adapter.MapCapability(c.desc)
			if !ok {
				t.Fatalf("MapCapability(%q) returned ok=false; want %s", c.desc, c.wantCap)
			}
			if got != c.wantCap {
				t.Errorf("MapCapability(%q) = %s; want %s", c.desc, got, c.wantCap)
			}
		})
	}
}

// TestMapCapability_NoMatch_StillFails — sanity: spec 106 changes the
// multi-match behavior but the zero-match behavior is unchanged. A
// description with no keyword hits still returns ok=false so the caller
// marks the node NeedsClarification.
func TestMapCapability_NoMatch_StillFails(t *testing.T) {
	cases := []string{
		"Do the thing",
		"Quarterly sync with the team",
		"Investigate the issue",
	}
	for _, desc := range cases {
		_, ok := adapter.MapCapability(desc)
		if ok {
			t.Errorf("MapCapability(%q) = ok=true; want false (no keyword match)", desc)
		}
	}
}

// TestMapCapability_AddThe_NoLongerHitsTestAuthor — spec 106 removed
// "add the" from CapTestAuthor's keyword set because it false-positive-
// matched code-implementation tasks like spec 097's T002 ("Extract the
// worker-host main… so the dispatcher can call it as the no-subcommand
// default"). Asserting the regression doesn't re-open.
func TestMapCapability_AddThe_NoLongerHitsTestAuthor(t *testing.T) {
	// This description matches only the (removed) "add the" keyword.
	desc := "Add the subcommand dispatcher to main.go"
	got, ok := adapter.MapCapability(desc)
	if !ok {
		t.Fatalf("MapCapability(%q) = ok=false; want true (matches 'add the subcommand')", desc)
	}
	// Should classify as CapCodeImplement via the new "add the subcommand"
	// keyword, NOT CapTestAuthor.
	if got != driver.CapCodeImplement {
		t.Errorf("MapCapability(%q) = %s; want %s (spec 106 removed 'add the' from CapTestAuthor)",
			desc, got, driver.CapCodeImplement)
	}
}
