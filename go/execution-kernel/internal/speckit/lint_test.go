package speckit

import (
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDir returns the absolute path to a named testdata fixture.
func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

// findingCheckIDs extracts the CheckID values from a slice of Findings.
func findingCheckIDs(findings []Finding) []string {
	ids := make([]string, len(findings))
	for i, f := range findings {
		ids[i] = f.CheckID
	}
	return ids
}

// hasCheckID returns true if findings contain at least one with the given check id.
func hasCheckID(findings []Finding, id string) bool {
	for _, f := range findings {
		if f.CheckID == id {
			return true
		}
	}
	return false
}

func TestLint_Good_ReturnsNoFindings(t *testing.T) {
	findings, err := Lint(fixtureDir(t, "good"))
	if err != nil {
		t.Fatalf("Lint returned unexpected error: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected zero findings, got %d: %v", len(findings), findingCheckIDs(findings))
	}
}

func TestLint_MissingSection_ReportsMissingRequiredSection(t *testing.T) {
	findings, err := Lint(fixtureDir(t, "missing-section"))
	if err != nil {
		t.Fatalf("Lint returned unexpected error: %v", err)
	}
	if !hasCheckID(findings, "missing-required-section") {
		t.Fatalf("expected 'missing-required-section' finding, got %v", findingCheckIDs(findings))
	}
}

func TestLint_Placeholder_ReportsTemplatePlaceholder(t *testing.T) {
	findings, err := Lint(fixtureDir(t, "placeholder"))
	if err != nil {
		t.Fatalf("Lint returned unexpected error: %v", err)
	}
	if !hasCheckID(findings, "template-placeholder") {
		t.Fatalf("expected 'template-placeholder' finding, got %v", findingCheckIDs(findings))
	}
}

func TestLint_FRGap_ReportsFRNumberingGap(t *testing.T) {
	findings, err := Lint(fixtureDir(t, "fr-gap"))
	if err != nil {
		t.Fatalf("Lint returned unexpected error: %v", err)
	}
	if !hasCheckID(findings, "fr-numbering-gap") {
		t.Fatalf("expected 'fr-numbering-gap' finding, got %v", findingCheckIDs(findings))
	}
}

func TestLint_NeedsClarification_ReportsNeedsClarificationMarker(t *testing.T) {
	findings, err := Lint(fixtureDir(t, "needs-clarification"))
	if err != nil {
		t.Fatalf("Lint returned unexpected error: %v", err)
	}
	if !hasCheckID(findings, "needs-clarification-marker") {
		t.Fatalf("expected 'needs-clarification-marker' finding, got %v", findingCheckIDs(findings))
	}
}

func TestLint_NoPriorityStory_ReportsUserStoryWithoutPriority(t *testing.T) {
	findings, err := Lint(fixtureDir(t, "no-priority-story"))
	if err != nil {
		t.Fatalf("Lint returned unexpected error: %v", err)
	}
	if !hasCheckID(findings, "user-story-without-priority") {
		t.Fatalf("expected 'user-story-without-priority' finding, got %v", findingCheckIDs(findings))
	}
}

func TestLint_NoGivenWhenThen_ReportsUserStoryWithoutAcceptanceScenarios(t *testing.T) {
	findings, err := Lint(fixtureDir(t, "no-given-when-then"))
	if err != nil {
		t.Fatalf("Lint returned unexpected error: %v", err)
	}
	if !hasCheckID(findings, "user-story-without-acceptance-scenarios") {
		t.Fatalf("expected 'user-story-without-acceptance-scenarios' finding, got %v", findingCheckIDs(findings))
	}
}

func TestLint_NoEdgeCases_ReportsMissingEdgeCases(t *testing.T) {
	findings, err := Lint(fixtureDir(t, "no-edge-cases"))
	if err != nil {
		t.Fatalf("Lint returned unexpected error: %v", err)
	}
	// Edge Cases is a required section; it should be reported by the same
	// missing-required-section check.
	if !hasCheckID(findings, "missing-required-section") {
		t.Fatalf("expected 'missing-required-section' finding for Edge Cases, got %v", findingCheckIDs(findings))
	}
	// And the finding's Detail should name "Edge Cases" specifically.
	foundEdgeCases := false
	for _, f := range findings {
		if f.CheckID == "missing-required-section" && strings.Contains(f.Detail, "Edge Cases") {
			foundEdgeCases = true
			break
		}
	}
	if !foundEdgeCases {
		t.Fatalf("expected missing-required-section detail to name 'Edge Cases', got findings: %+v", findings)
	}
}

func TestLint_NoChecklist_ReportsMissingChecklist(t *testing.T) {
	findings, err := Lint(fixtureDir(t, "no-checklist"))
	if err != nil {
		t.Fatalf("Lint returned unexpected error: %v", err)
	}
	if !hasCheckID(findings, "missing-checklist") {
		t.Fatalf("expected 'missing-checklist' finding, got %v", findingCheckIDs(findings))
	}
}

func TestLint_UncheckedBoxes_ReportsUncheckedChecklistBox(t *testing.T) {
	findings, err := Lint(fixtureDir(t, "unchecked-boxes"))
	if err != nil {
		t.Fatalf("Lint returned unexpected error: %v", err)
	}
	if !hasCheckID(findings, "unchecked-checklist-box") {
		t.Fatalf("expected 'unchecked-checklist-box' finding, got %v", findingCheckIDs(findings))
	}
}

func TestLint_NonexistentDir_ReturnsError(t *testing.T) {
	_, err := Lint(fixtureDir(t, "this-fixture-does-not-exist"))
	if err == nil {
		t.Fatalf("expected error for nonexistent spec dir, got nil")
	}
}

// TestLint_RealSpec093 runs the linter against the actual repo's spec 093 to
// confirm a known-good real-world spec passes. This is a smoke test, not a
// unit test; if it fails, fix spec 093 (or the linter).
func TestLint_RealSpec093(t *testing.T) {
	repoSpec := "../../../../.specify/specs/093-merge-queue-orchestrator"
	findings, err := Lint(repoSpec)
	if err != nil {
		// If the spec dir does not exist (e.g., test run outside repo), skip.
		t.Skipf("spec 093 not reachable from test cwd: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("spec 093 has lint findings (fix spec or amend linter): %+v", findings)
	}
}

// TestLint_RealSpec094 runs the linter against the actual repo's spec 094.
func TestLint_RealSpec094(t *testing.T) {
	repoSpec := "../../../../.specify/specs/094-pr-review-mechanism"
	findings, err := Lint(repoSpec)
	if err != nil {
		t.Skipf("spec 094 not reachable from test cwd: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("spec 094 has lint findings (fix spec or amend linter): %+v", findings)
	}
}
