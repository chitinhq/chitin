package speclint

import (
	"strings"
	"testing"
)

func TestCheckL07_HappyPath(t *testing.T) {
	// Every US has an Independent test paragraph — zero violations.
	content := strings.Join([]string{
		"## User stories",
		"",
		"### US1 (P1) — first story",
		"> as a user...",
		"",
		"**Independent test:** do the thing.",
		"",
		"### US2 (P2) — second story",
		"> as another user...",
		"",
		"**Independent test:** verify the other thing.",
		"",
		"## Next section",
	}, "\n")

	got := CheckL07("spec.md", content)
	if len(got) != 0 {
		t.Fatalf("expected no violations, got %d: %+v", len(got), got)
	}
}

func TestCheckL07_MissingInOnlyStory(t *testing.T) {
	content := strings.Join([]string{
		"### US1 (P1) — solo story",
		"> as a user...",
		"",
		"no marker here",
		"",
		"## Next section",
	}, "\n")

	got := CheckL07("spec.md", content)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	if got[0].Rule != "L07" || got[0].Line != 1 || got[0].Severity != SeverityError {
		t.Errorf("unexpected violation shape: %+v", got[0])
	}
}

func TestCheckL07_MissingInMiddleStory(t *testing.T) {
	// US2 is missing its marker — US1 and US3 have one. Only US2 should
	// violate.
	content := strings.Join([]string{
		"### US1 — first",
		"**Independent test:** ok.",
		"",
		"### US2 — broken",
		"> no marker",
		"",
		"### US3 — third",
		"**Independent test:** ok.",
		"",
	}, "\n")

	got := CheckL07("spec.md", content)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	if got[0].Line != 4 {
		t.Errorf("expected violation on line 4 (US2 header), got line %d", got[0].Line)
	}
	if !strings.Contains(got[0].Message, "US2") {
		t.Errorf("expected US2 in message, got: %s", got[0].Message)
	}
}

func TestCheckL07_LastSectionToEOF(t *testing.T) {
	// USN at end-of-file with no trailing section — must still scan to EOF
	// to find the marker.
	content := strings.Join([]string{
		"### US1 — last section",
		"> blah",
		"",
		"**Independent test:** found at EOF.",
	}, "\n")

	got := CheckL07("spec.md", content)
	if len(got) != 0 {
		t.Fatalf("expected no violations at EOF case, got: %+v", got)
	}
}

func TestCheckL07_LastSectionMissingToEOF(t *testing.T) {
	// USN at EOF with nothing after — should violate.
	content := strings.Join([]string{
		"### US1 — last section",
		"> blah",
		"no marker, no trailing section",
	}, "\n")

	got := CheckL07("spec.md", content)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
}

func TestCheckL07_MarkerBeforeHeaderDoesNotCount(t *testing.T) {
	// A marker that appears BEFORE the USN header (e.g., quoted in an
	// earlier paragraph) must not satisfy that USN's requirement.
	content := strings.Join([]string{
		"## Why",
		"",
		"We use **Independent test:** as a convention.",
		"",
		"### US1 — actual story",
		"> the story",
		"",
		"## Next section",
	}, "\n")

	got := CheckL07("spec.md", content)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	if got[0].Line != 5 {
		t.Errorf("expected violation on line 5 (US1 header), got line %d", got[0].Line)
	}
}

func TestCheckL07_NoUserStoriesNoViolations(t *testing.T) {
	content := strings.Join([]string{
		"# Spec X",
		"",
		"## Why",
		"",
		"Just prose, no user stories.",
	}, "\n")

	got := CheckL07("spec.md", content)
	if len(got) != 0 {
		t.Fatalf("expected no violations (no USN headers), got: %+v", got)
	}
}

func TestCheckL07_MultiDigitUSNumber(t *testing.T) {
	content := strings.Join([]string{
		"### US12 — twelfth story",
		"> blah",
		"",
		"## Next",
	}, "\n")

	got := CheckL07("spec.md", content)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(got))
	}
	if !strings.Contains(got[0].Message, "US12") {
		t.Errorf("expected US12 in message, got: %s", got[0].Message)
	}
}

func TestCheckL07_SubheaderDoesNotTerminateSection(t *testing.T) {
	// A `####` header is a subsection within the user story and must not
	// terminate the search bound; the marker after it should still count.
	content := strings.Join([]string{
		"### US1 — has nested heading",
		"> blah",
		"",
		"#### Details",
		"some details",
		"",
		"**Independent test:** found after subheader.",
		"",
		"## Next",
	}, "\n")

	got := CheckL07("spec.md", content)
	if len(got) != 0 {
		t.Fatalf("expected no violations (marker after #### subheader is valid), got: %+v", got)
	}
}

func TestCheckL07_RealSpec115(t *testing.T) {
	// Smoke test against the actual spec 115 fragment — it has three US
	// sections, all with Independent test markers.
	content := strings.Join([]string{
		"### US1 (P1) — Factory iterates spec-PR Copilot comments to zero or escalation",
		"",
		"> As a spec author, when I open a spec PR...",
		"",
		"**Independent test:** Open a spec PR with an obvious doc inconsistency.",
		"",
		"### US2 (P1) — Spec-specific consistency linter runs before Copilot review",
		"",
		"> As a spec author, the factory runs a deterministic linter...",
		"",
		"**Independent test:** Open a spec PR that references a non-existent CLI.",
		"",
		"### US3 (P2) — Design-judgment comments escalate, not iterate",
		"",
		"> As the operator, when Copilot leaves a comment...",
		"",
		"**Independent test:** Copilot leaves a comment about user story redundancy.",
		"",
		"## Functional requirements",
	}, "\n")

	got := CheckL07("spec.md", content)
	if len(got) != 0 {
		t.Fatalf("spec 115 fragment should pass L07, got violations: %+v", got)
	}
}
