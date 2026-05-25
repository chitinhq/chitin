package speclint

import (
	"strings"
	"testing"
)

// specWithCanonical is the spec.md fragment used across L04 tests. The
// canonical FR-009 declares four events whose trailing-verb suffixes
// (started, completed, failed, escalated) form the closed suffix set.
const specWithCanonical = `---
spec_id: 999
title: Test spec
---

# Spec 999

## Functional requirements

- **FR-001** Some unrelated requirement that references ` + "`pr_number`" + ` field.
- **FR-009** Chain events (closed taxonomy):
  - ` + "`foo_started { pr_number }`" + `
  - ` + "`foo_completed { pr_number, round, fixup_sha }`" + `
  - ` + "`foo_failed { pr_number, failure_kind }`" + `
  - ` + "`foo_escalated { pr_number, reason }`" + `

## Edge cases

- Some edge case here.
`

func TestL04Events_NoFreelanceReferences(t *testing.T) {
	tasks := "- T001 Run the loop until `foo_completed` fires.\n"
	got := L04Events("spec.md", specWithCanonical, "tasks.md", tasks)
	if len(got) != 0 {
		t.Fatalf("expected no violations, got %d: %#v", len(got), got)
	}
}

func TestL04Events_FreelanceEventInSpec(t *testing.T) {
	spec := specWithCanonical + "\nReferences `bar_started` here in prose.\n"
	got := L04Events("spec.md", spec, "tasks.md", "")
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %#v", len(got), got)
	}
	v := got[0]
	if v.Rule != "L04" {
		t.Errorf("Rule = %q, want L04", v.Rule)
	}
	if v.File != "spec.md" {
		t.Errorf("File = %q, want spec.md", v.File)
	}
	if v.Severity != SeverityError {
		t.Errorf("Severity = %q, want %q", v.Severity, SeverityError)
	}
	if !strings.Contains(v.Message, `"bar_started"`) {
		t.Errorf("Message missing offending event: %q", v.Message)
	}
	if !strings.Contains(v.Message, "foo_completed") {
		t.Errorf("Message missing canonical list: %q", v.Message)
	}
}

func TestL04Events_FreelanceEventInTasks(t *testing.T) {
	tasks := "- T001 Wait for `bar_started` event.\n"
	got := L04Events("spec.md", specWithCanonical, "tasks.md", tasks)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %#v", len(got), got)
	}
	if got[0].File != "tasks.md" {
		t.Errorf("File = %q, want tasks.md", got[0].File)
	}
	if got[0].Line != 1 {
		t.Errorf("Line = %d, want 1", got[0].Line)
	}
}

func TestL04Events_FieldNamesIgnored(t *testing.T) {
	// pr_number, fixup_sha, lint_violations_count all match [a-z_]+
	// with at least one underscore but their last segments aren't in
	// the canonical verb suffix set, so the rule must stay silent on
	// them. Without this guard the rule would false-positive on every
	// payload field referenced in narrative prose.
	tasks := strings.Join([]string{
		"- T001 Field `pr_number` is the PR id.",
		"- T002 Field `fixup_sha` is the head sha.",
		"- T003 Counter `lint_violations_count` tracks total.",
	}, "\n")
	got := L04Events("spec.md", specWithCanonical, "tasks.md", tasks)
	if len(got) != 0 {
		t.Fatalf("expected no violations on field-name tokens, got %d: %#v", len(got), got)
	}
}

func TestL04Events_ReasonValuesIgnored(t *testing.T) {
	// Reason values from spec 115 FR-010: their last segments (hit,
	// present, required, unresolvable, lost) are not in the canonical
	// event verb suffix set, so L04 must stay silent. Without this
	// guard L04 would collide with L06's domain.
	tasks := strings.Join([]string{
		"- T001 Reason `iteration_cap_hit` triggers escalation.",
		"- T002 Reason `human_reviewer_present` is closed-set.",
		"- T003 Reason `design_judgement_required` ditto.",
		"- T004 Reason `lint_violation_unresolvable` ditto.",
	}, "\n")
	got := L04Events("spec.md", specWithCanonical, "tasks.md", tasks)
	if len(got) != 0 {
		t.Fatalf("expected no violations on reason-value tokens, got %d: %#v", len(got), got)
	}
}

func TestL04Events_CanonicalBodyExcluded(t *testing.T) {
	// The four canonical event names appear inside the FR-009 body.
	// They must NOT be flagged as references to themselves.
	got := L04Events("spec.md", specWithCanonical, "tasks.md", "")
	if len(got) != 0 {
		t.Fatalf("canonical body produced violations: %#v", got)
	}
}

func TestL04Events_NoCanonicalBlockMeansNoOp(t *testing.T) {
	spec := `---
spec_id: 0
---

# Spec 0

## Functional requirements

- **FR-001** This spec declares no telemetry. References ` + "`bar_started`" + ` in prose.
`
	got := L04Events("spec.md", spec, "tasks.md", "")
	if len(got) != 0 {
		t.Fatalf("expected no-op when no Chain events FR exists, got %#v", got)
	}
}

func TestL04Events_FirstChainEventsFRWins(t *testing.T) {
	// When two FR blocks contain "Chain events", the first one is
	// canonical. The second is scanned as references and may produce
	// violations if it introduces suffix-matching events not in the
	// first.
	spec := `---
spec_id: 999
---

## Functional requirements

- **FR-009** Chain events:
  - ` + "`foo_completed`" + `
- **FR-010** Chain events appendix:
  - ` + "`bar_completed`" + `
`
	got := L04Events("spec.md", spec, "tasks.md", "")
	if len(got) != 1 {
		t.Fatalf("expected 1 violation from second FR block, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "bar_completed") {
		t.Errorf("violation should name the freelance event: %q", got[0].Message)
	}
}

func TestL04Events_DedupsSameTokenSameLine(t *testing.T) {
	tasks := "- T001 See `bar_started` and again `bar_started`.\n"
	got := L04Events("spec.md", specWithCanonical, "tasks.md", tasks)
	if len(got) != 1 {
		t.Fatalf("expected dedup to collapse to 1 violation, got %d: %#v", len(got), got)
	}
}

func TestL04Events_ViolationsSortedDeterministically(t *testing.T) {
	tasks := strings.Join([]string{
		"- T001 References `bar_started`.",   // line 1
		"- T002 References `baz_completed`.", // line 2
		"- T003 References `qux_failed`.",    // line 3
	}, "\n")
	got := L04Events("spec.md", specWithCanonical, "tasks.md", tasks)
	if len(got) != 3 {
		t.Fatalf("expected 3 violations, got %d: %#v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Line > got[i].Line {
			t.Fatalf("violations out of order at index %d: %#v", i, got)
		}
	}
}

func TestL04Events_GoldenL04FailShape(t *testing.T) {
	// Replicates the spec 115 T020 golden l04_fail fixture: canonical
	// FR-002 declares foo_started + foo_completed; FR-001 references
	// the freelance bar_started in prose. The golden test asserts ≥1
	// error-severity L04 violation; pin that contract here so future
	// edits to L04Events can't silently regress the fixture.
	spec := `---
spec_id: 100
title: L04 fail
---

# L04 fail fixture

` + "`**FR-002**`" + ` is the canonical block (contains ` + "`Chain\nevents`" + `).
It declares ` + "`foo_started`" + ` and ` + "`foo_completed`" + `.
` + "`**FR-001**`" + ` references ` + "`bar_started`" + ` outside it.

## Functional requirements

- **FR-001** The kernel emits ` + "`bar_started`" + ` to mark a phase.
- **FR-002** Chain events (closed taxonomy):
  - ` + "`foo_started { pr_number }`" + `
  - ` + "`foo_completed { pr_number }`" + `
`
	got := L04Events("spec.md", spec, "tasks.md", "")
	found := false
	for _, v := range got {
		if v.Rule == "L04" && v.Severity == SeverityError && strings.Contains(v.Message, `"bar_started"`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ≥1 L04 error violation naming bar_started, got %#v", got)
	}
}

func TestLastSegment(t *testing.T) {
	cases := map[string]string{
		"foo_completed":         "completed",
		"spec_iteration_failed": "failed",
		"single":                "single",
		"two_word":              "word",
	}
	for in, want := range cases {
		if got := lastSegment(in); got != want {
			t.Errorf("lastSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsEventLike(t *testing.T) {
	yes := []string{"foo_started", "spec_lint_completed", "a_b"}
	no := []string{"FooStarted", "started", "info", "snake_WITH_caps", "has spaces", "has-dash"}
	for _, s := range yes {
		if !isEventLike(s) {
			t.Errorf("isEventLike(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isEventLike(s) {
			t.Errorf("isEventLike(%q) = true, want false", s)
		}
	}
}

func TestFirstToken(t *testing.T) {
	cases := map[string]string{
		"foo_started":                 "foo_started",
		"foo_started { pr_number }":   "foo_started",
		"foo_started  multiple words": "foo_started",
		"   leading whitespace":       "leading",
		"foo_started\twith\ttab":      "foo_started",
	}
	for in, want := range cases {
		if got := firstToken(in); got != want {
			t.Errorf("firstToken(%q) = %q, want %q", in, got, want)
		}
	}
}
