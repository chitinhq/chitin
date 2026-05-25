package speclint

import (
	"strings"
	"testing"
)

// specWithCanonical is the minimal spec.md fragment used across L04
// tests. The canonical FR-009 declares four events sharing the
// `pr_iteration` family.
const specWithCanonical = `---
spec_id: 999
title: Test spec
---

# Spec 999

## Functional requirements

- **FR-001** Some unrelated requirement that references ` + "`pr_number`" + ` field.
- **FR-009** Chain events (closed taxonomy):
  - ` + "`pr_iteration_round_started { pr_number, round }`" + `
  - ` + "`pr_iteration_completed { pr_number, round, fixup_sha }`" + `
  - ` + "`pr_iteration_failed { pr_number, failure_kind }`" + `
  - ` + "`pr_iteration_escalated { pr_number, reason }`" + `

## Edge cases

- Some edge case here.
`

func TestL04Events_NoFreelanceReferences(t *testing.T) {
	tasks := "- T001 Run the loop until `pr_iteration_completed` fires.\n"
	got := L04Events("spec.md", specWithCanonical, "tasks.md", tasks)
	if len(got) != 0 {
		t.Fatalf("expected no violations, got %d: %#v", len(got), got)
	}
}

func TestL04Events_FreelanceEventInSpec(t *testing.T) {
	spec := specWithCanonical + "\nReferences `pr_iteration_skipped` here in prose.\n"
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
	if !strings.Contains(v.Message, `"pr_iteration_skipped"`) {
		t.Errorf("Message missing offending event: %q", v.Message)
	}
	if !strings.Contains(v.Message, "pr_iteration_completed") {
		t.Errorf("Message missing canonical list: %q", v.Message)
	}
}

func TestL04Events_FreelanceEventInTasks(t *testing.T) {
	tasks := "- T001 Wait for `pr_iteration_skipped` event.\n"
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

func TestL04Events_OutOfFamilyTokensIgnored(t *testing.T) {
	// `lint_violation_count`, `replies_posted`, `design_judgement_required`
	// all match [a-z_]+ and have underscores, but none share a 2-segment
	// family with the canonical `pr_iteration_*` set, so they are silent.
	tasks := strings.Join([]string{
		"- T001 Field `lint_violation_count` is tracked.",
		"- T002 Counter `replies_posted` increments per reply.",
		"- T003 Reason `design_judgement_required` is closed-set.",
	}, "\n")
	got := L04Events("spec.md", specWithCanonical, "tasks.md", tasks)
	if len(got) != 0 {
		t.Fatalf("expected no violations, got %d: %#v", len(got), got)
	}
}

func TestL04Events_CanonicalBodyItselfIsExcluded(t *testing.T) {
	// The four canonical event names appear inside the FR-009 body. They
	// must NOT be flagged as references to themselves.
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

- **FR-001** This spec declares no telemetry. References ` + "`pr_iteration_skipped`" + ` in prose.
`
	got := L04Events("spec.md", spec, "tasks.md", "")
	if len(got) != 0 {
		t.Fatalf("expected rule to no-op when no Chain events FR exists, got %#v", got)
	}
}

func TestL04Events_FirstChainEventsFRIsCanonical(t *testing.T) {
	// When two FR blocks contain "Chain events", the first one wins. The
	// second one's contents are scanned as references and may produce
	// violations if they introduce family-shared events not in the first.
	spec := `---
spec_id: 999
---

## Functional requirements

- **FR-009** Chain events:
  - ` + "`pr_iteration_completed`" + `
- **FR-010** Chain events appendix:
  - ` + "`pr_iteration_skipped`" + `
`
	got := L04Events("spec.md", spec, "tasks.md", "")
	if len(got) != 1 {
		t.Fatalf("expected 1 violation from second FR block, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "pr_iteration_skipped") {
		t.Errorf("violation should name the freelance event: %q", got[0].Message)
	}
}

func TestL04Events_DedupsSameTokenSameLine(t *testing.T) {
	tasks := "- T001 See `pr_iteration_skipped` and again `pr_iteration_skipped`.\n"
	got := L04Events("spec.md", specWithCanonical, "tasks.md", tasks)
	if len(got) != 1 {
		t.Fatalf("expected dedup to collapse to 1 violation, got %d: %#v", len(got), got)
	}
}

func TestL04Events_ViolationsSortedDeterministically(t *testing.T) {
	tasks := strings.Join([]string{
		"- T001 References `pr_iteration_skipped`.",            // line 1
		"- T002 References `pr_iteration_paused`.",             // line 2
		"- T003 References `pr_iteration_aborted`.",            // line 3
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

func TestFamilyOf(t *testing.T) {
	cases := map[string]string{
		"pr_iteration_completed":       "pr_iteration",
		"pr_iteration_round_started":   "pr_iteration",
		"spec_lint_completed":          "spec_lint",
		"two_word":                     "two_word",
		"single":                       "single",
	}
	for in, want := range cases {
		if got := familyOf(in); got != want {
			t.Errorf("familyOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsEventLike(t *testing.T) {
	yes := []string{"pr_iteration_completed", "spec_lint_completed", "a_b"}
	no := []string{"PrIterationCompleted", "error", "info", "snake_case_WITH_caps", "has spaces", "has-dash"}
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
