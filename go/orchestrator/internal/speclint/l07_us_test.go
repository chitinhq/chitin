package speclint

import (
	"strings"
	"testing"
)

func TestL07_RealSpec115_HasAllMarkers(t *testing.T) {
	// The real spec.md this work unit was authored against — US1, US2,
	// and US3 all have **Independent test:** paragraphs. Expect 0
	// violations. Inlined rather than read from disk so the test stays
	// pure / hermetic.
	const spec = `---
spec_id: 115
---

# foo

### US1 (P1) — story one

> as a user...

**Independent test:** open a PR.

### US2 (P1) — story two

> blurb

**Independent test:** other thing.

## Functional requirements

### FR-001 ...
`
	got := L07UserStoryTests("spec.md", spec)
	if len(got) != 0 {
		t.Fatalf("expected no violations, got %d: %+v", len(got), got)
	}
}

func TestL07_MissingMarker_ReportsOnePerStory(t *testing.T) {
	const spec = `---
spec_id: 1
---

### US1 — story one
body without the marker.

### US2 — story two
**Independent test:** present here.

### US3 — story three
body without the marker either.
`
	got := L07UserStoryTests("spec.md", spec)
	if len(got) != 2 {
		t.Fatalf("expected 2 violations, got %d: %+v", len(got), got)
	}
	// Violations should point at the US1 and US3 header lines.
	if got[0].Line == 0 || got[1].Line == 0 || got[0].Line >= got[1].Line {
		t.Fatalf("violations have wrong line ordering: %+v", got)
	}
	for _, v := range got {
		if v.Rule != "L07" || v.Severity != SeverityError || v.File != "spec.md" {
			t.Fatalf("violation has wrong metadata: %+v", v)
		}
		if !strings.Contains(v.Message, "Independent test") {
			t.Fatalf("violation message missing marker hint: %q", v.Message)
		}
	}
}

func TestL07_NoUserStories_IsSilent(t *testing.T) {
	const spec = `---
spec_id: 1
---

# Title

## Functional requirements

### FR-001 ...
body
`
	got := L07UserStoryTests("spec.md", spec)
	if len(got) != 0 {
		t.Fatalf("expected no violations on spec with no US sections, got %+v", got)
	}
}

func TestL07_UsersHeader_DoesNotMatch(t *testing.T) {
	// "### USers" must not be misclassified as a user-story header.
	const spec = `---
spec_id: 1
---

### USers and roles

no marker, but this is not a US section either.
`
	got := L07UserStoryTests("spec.md", spec)
	if len(got) != 0 {
		t.Fatalf("expected USers to not match US-header regex, got %+v", got)
	}
}

func TestL07_MultiDigitStory_Matches(t *testing.T) {
	const spec = `---
spec_id: 1
---

### US10 — tenth story

(no marker)
`
	got := L07UserStoryTests("spec.md", spec)
	if len(got) != 1 {
		t.Fatalf("expected US10 to be recognised and flagged, got %+v", got)
	}
}

func TestL07_MarkerInBlockquote_Accepted(t *testing.T) {
	// Indented / blockquoted markers still count — trim leading ws/>.
	const spec = `---
spec_id: 1
---

### US1 — story

>   **Independent test:** in a quote indent.
`
	got := L07UserStoryTests("spec.md", spec)
	if len(got) != 0 {
		t.Fatalf("expected indented/blockquoted marker to be accepted, got %+v", got)
	}
}

func TestL07_EmptyBody_BetweenTwoUSHeaders_Flags(t *testing.T) {
	const spec = `---
spec_id: 1
---

### US1 — story one
### US2 — story two
**Independent test:** belongs to US2 only.
`
	got := L07UserStoryTests("spec.md", spec)
	if len(got) != 1 {
		t.Fatalf("expected US1 to be flagged (empty body, US2's marker is out of scope), got %+v", got)
	}
	if got[0].Message == "" || !strings.Contains(got[0].Message, "US1") {
		t.Fatalf("expected violation to name US1, got %q", got[0].Message)
	}
}

func TestL07_MarkerAfterDeeperSubheading_Accepted(t *testing.T) {
	// A #### sub-heading does NOT end the US section.
	const spec = `---
spec_id: 1
---

### US1 — story

#### sub-detail

**Independent test:** still in US1's section.

## Next top-level
`
	got := L07UserStoryTests("spec.md", spec)
	if len(got) != 0 {
		t.Fatalf("expected marker after #### to count, got %+v", got)
	}
}
