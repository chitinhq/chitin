package speclint

import (
	"strings"
	"testing"
)

// canonicalFR returns the FR-009 telemetry block as it appears in spec 115
// — the realistic shape the rule is built against.
func canonicalFR() string {
	return strings.Join([]string{
		"### Telemetry",
		"",
		"- **FR-009** Chain events (closed taxonomy):",
		"  - `spec_lint_completed { pr_number, rule_violations }`",
		"  - `spec_iteration_round_started { pr_number, round }`",
		"  - `spec_iteration_completed { pr_number, round, fixup_sha }`",
		"  - `spec_iteration_failed { pr_number, round, failure_kind }`",
		"  - `spec_iteration_escalated { pr_number, reason }`",
		"  - `spec_iteration_skipped { pr_number, reason }`",
	}, "\n")
}

func TestL04_AllEventsDeclared_NoViolations(t *testing.T) {
	spec := canonicalFR() + "\n\n" + strings.Join([]string{
		"## US1",
		"Chain emits `spec_iteration_round_started` then `spec_iteration_completed`.",
		"Escalation: `spec_iteration_escalated { reason: \"design_judgement_required\" }`.",
	}, "\n")
	tasks := strings.Join([]string{
		"- [ ] T001 emit spec_iteration_failed on driver crash",
		"- [ ] T002 dispatch spec_iteration_skipped when lease lost",
	}, "\n")

	got := L04EventTaxonomy(spec, tasks)
	if len(got) != 0 {
		t.Fatalf("expected no violations, got %d: %+v", len(got), got)
	}
}

func TestL04_UnknownEventInProseSpec_ReportsViolation(t *testing.T) {
	// Bare prose mention of an event that isn't in the canonical set —
	// this is the spec 113 `pr_iteration_skipped` drift case spec 115's
	// WHY section calls out.
	spec := canonicalFR() + "\n\n" + strings.Join([]string{
		"## Why",                                            // line 11
		"Spec 113 had a pr_iteration_skipped event that...", // line 12
	}, "\n")

	got := L04EventTaxonomy(spec, "")
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.Rule != "L04" {
		t.Errorf("rule: got %q, want L04", v.Rule)
	}
	if v.File != "spec.md" {
		t.Errorf("file: got %q, want spec.md", v.File)
	}
	if v.Severity != SeverityError {
		t.Errorf("severity: got %q, want error", v.Severity)
	}
	if !strings.Contains(v.Message, "pr_iteration_skipped") {
		t.Errorf("message should name the offending event, got %q", v.Message)
	}
}

func TestL04_UnknownEventInBacktickDefShape_ReportsViolation(t *testing.T) {
	// Backtick `<name> {` reference outside the canonical block —
	// the spec author wrote a new event_type as if it existed.
	spec := canonicalFR() + "\n\n" + strings.Join([]string{
		"## Edge cases",
		"On replay we emit `spec_iteration_replayed { pr_number }`.",
	}, "\n")

	got := L04EventTaxonomy(spec, "")
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "spec_iteration_replayed") {
		t.Errorf("message should name spec_iteration_replayed, got %q", got[0].Message)
	}
}

func TestL04_UnknownEventInTasksMd_ReportsViolationOnTasksFile(t *testing.T) {
	// pr_iteration_failed is spec 113's event family — leaks into a
	// spec 115 task as a copy-paste drift. Valid event-shape, not in
	// THIS spec's canonical set, so flagged.
	spec := canonicalFR()
	tasks := strings.Join([]string{
		"- [ ] T001 emit spec_iteration_completed",
		"- [ ] T002 emit pr_iteration_failed on driver crash",
	}, "\n")

	got := L04EventTaxonomy(spec, tasks)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.File != "tasks.md" {
		t.Errorf("file: got %q, want tasks.md", v.File)
	}
	if v.Line != 2 {
		t.Errorf("line: got %d, want 2", v.Line)
	}
	if !strings.Contains(v.Message, "pr_iteration_failed") {
		t.Errorf("message should name pr_iteration_failed, got %q", v.Message)
	}
}

func TestL04_NoCanonicalBlockButEventRefsExist_EmitsWarningAndErrors(t *testing.T) {
	// No FR with "Chain events" — the warning fires AND each ref is
	// flagged so the author sees both the structural gap and the
	// specific tokens that prompted the rule.
	spec := strings.Join([]string{
		"## Why",
		"This spec mentions some_workflow_completed in prose.",
	}, "\n")

	got := L04EventTaxonomy(spec, "")
	if len(got) != 2 {
		t.Fatalf("expected warning + 1 error, got %d: %+v", len(got), got)
	}
	// Sorted by (file, line, message). Both are on file=spec.md;
	// line=1 (warning) comes first, then line=2 (the event ref).
	if got[0].Severity != SeverityWarning {
		t.Errorf("first violation should be the warning, got %+v", got[0])
	}
	if got[1].Severity != SeverityError || !strings.Contains(got[1].Message, "some_workflow_completed") {
		t.Errorf("second violation should error on some_workflow_completed, got %+v", got[1])
	}
}

func TestL04_NoCanonicalBlockAndNoRefs_Silent(t *testing.T) {
	spec := strings.Join([]string{
		"## Why",
		"This spec has no telemetry surface at all.",
	}, "\n")

	got := L04EventTaxonomy(spec, "")
	if len(got) != 0 {
		t.Fatalf("no telemetry + no refs = silent, got %+v", got)
	}
}

func TestL04_EventInsideCanonicalBlockNotDoubleReported(t *testing.T) {
	// Canonical block lists `spec_iteration_completed`. The rule must
	// NOT flag the very declaration as a violation just because the
	// bare-token shape ALSO matches `spec_iteration_completed`.
	spec := canonicalFR()

	got := L04EventTaxonomy(spec, "")
	if len(got) != 0 {
		t.Fatalf("declarations inside the canonical block must not self-flag, got %+v", got)
	}
}

func TestL04_RepeatedSameEventOnSameLine_DedupedByKey(t *testing.T) {
	// Same unknown event referenced twice in one line — emit one
	// violation, not two. (file, line, name) is the dedup key.
	spec := canonicalFR() + "\n\n## US1\n" +
		"loop: pr_iteration_started then pr_iteration_started again"

	got := L04EventTaxonomy(spec, "")
	if len(got) != 1 {
		t.Fatalf("expected 1 violation (dedup), got %d: %+v", len(got), got)
	}
}

func TestL04_DeterministicOrder_FileThenLine(t *testing.T) {
	// Three unknown events using realistic spec 113/114-family verb
	// suffixes: one in spec.md (after canonical), two in tasks.md.
	// spec.md < tasks.md lexically, so spec wins ordering.
	spec := canonicalFR() + "\n\n## Edge\n" +
		"emit pr_iteration_started here"
	tasks := strings.Join([]string{
		"- [ ] T001 emit pr_iteration_completed",
		"- [ ] T002 emit pr_iteration_failed",
	}, "\n")

	got := L04EventTaxonomy(spec, tasks)
	if len(got) != 3 {
		t.Fatalf("expected 3 violations, got %d: %+v", len(got), got)
	}
	wantOrder := []struct {
		file string
		name string
	}{
		{"spec.md", "pr_iteration_started"},
		{"tasks.md", "pr_iteration_completed"},
		{"tasks.md", "pr_iteration_failed"},
	}
	for i, w := range wantOrder {
		if got[i].File != w.file || !strings.Contains(got[i].Message, w.name) {
			t.Errorf("violation[%d]: got file=%s msg=%q, want file=%s containing %s",
				i, got[i].File, got[i].Message, w.file, w.name)
		}
	}
}

func TestL04_FRWithoutChainEvents_NotTreatedAsCanonical(t *testing.T) {
	// An FR that doesn't say "Chain events" in its body — even if it
	// happens to declare backtick `name {` patterns — does NOT count
	// as the telemetry FR. The heuristic is the literal phrase.
	spec := strings.Join([]string{
		"### Some other section",
		"- **FR-001** Some requirement that declares `whatever_completed { x }`.",
		"",
		"### Telemetry",
		"- **FR-009** Chain events:",
		"  - `spec_iteration_completed { pr_number }`",
		"",
		"## US1",
		"emits spec_iteration_completed",
	}, "\n")

	got := L04EventTaxonomy(spec, "")
	// `whatever_completed` is NOT in the canonical set — it's an event
	// reference outside the telemetry FR, so it IS a violation.
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "whatever_completed") {
		t.Errorf("violation should name whatever_completed, got %q", got[0].Message)
	}
}

func TestL04_CanonicalFRWithChainEventsButNoDeclarations_FallsThrough(t *testing.T) {
	// FR mentions "chain events" but lists none. extractCanonicalEvents
	// returns nil → warning path fires for any refs found elsewhere.
	spec := strings.Join([]string{
		"### Telemetry",
		"- **FR-009** Chain events: see follow-up spec.",
		"",
		"## US1",
		"emits spec_iteration_completed",
	}, "\n")

	got := L04EventTaxonomy(spec, "")
	if len(got) < 2 {
		t.Fatalf("expected warning + at least one error, got %d: %+v", len(got), got)
	}
	if got[0].Severity != SeverityWarning {
		t.Errorf("first violation should be the warning, got %+v", got[0])
	}
}
