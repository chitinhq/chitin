package speclint

import (
	"strings"
	"testing"
)

func TestL03_CleanSpecAndTasks_NoViolations(t *testing.T) {
	spec := strings.Join([]string{
		"## Functional requirements",
		"",
		"- **FR-001** First requirement.",
		"- **FR-002** Second requirement.",
	}, "\n")
	tasks := strings.Join([]string{
		"- [ ] T001 Implement FR-001 in foo.go",
		"- [ ] T002 Implement FR-002 in bar.go",
	}, "\n")

	got := L03TaskFRCoverage(spec, tasks)
	if len(got) != 0 {
		t.Fatalf("expected no violations, got %d: %+v", len(got), got)
	}
}

func TestL03_TaskReferencesUnknownFR_ReportsViolationOnTasksMd(t *testing.T) {
	spec := "- **FR-001** First.\n"
	tasks := strings.Join([]string{
		"- [ ] T001 Implement FR-001",
		"- [ ] T002 Wire up FR-999 to the bus",
	}, "\n")

	got := L03TaskFRCoverage(spec, tasks)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.Rule != "L03" {
		t.Errorf("rule: got %q, want L03", v.Rule)
	}
	if v.File != "tasks.md" {
		t.Errorf("file: got %q, want tasks.md", v.File)
	}
	if v.Severity != SeverityError {
		t.Errorf("severity: got %q, want error", v.Severity)
	}
	if v.Line != 2 {
		t.Errorf("line: got %d, want 2 (the T002 ref line)", v.Line)
	}
	if !strings.Contains(v.Message, "FR-999") {
		t.Errorf("message should name FR-999, got %q", v.Message)
	}
}

func TestL03_OrphanSpecFR_ReportsViolationOnSpecMd(t *testing.T) {
	spec := strings.Join([]string{
		"- **FR-001** Has a task.",
		"- **FR-002** Has no task.",
	}, "\n")
	tasks := "- [ ] T001 Implement FR-001 only\n"

	got := L03TaskFRCoverage(spec, tasks)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.Rule != "L03" {
		t.Errorf("rule: got %q, want L03", v.Rule)
	}
	if v.File != "spec.md" {
		t.Errorf("file: got %q, want spec.md", v.File)
	}
	if v.Severity != SeverityError {
		t.Errorf("severity: got %q, want error", v.Severity)
	}
	if v.Line != 2 {
		t.Errorf("line: got %d, want 2 (FR-002 declaration line)", v.Line)
	}
	if !strings.Contains(v.Message, "FR-002") {
		t.Errorf("message should name FR-002, got %q", v.Message)
	}
}

func TestL03_BothDirections_DeterministicOrder(t *testing.T) {
	// File-ASC ordering: "spec.md" < "tasks.md" lexically, so spec.md
	// orphans must precede tasks.md unknowns. Inside each file, line ASC.
	spec := strings.Join([]string{
		"- **FR-001** orphan one",
		"- **FR-003** orphan two",
	}, "\n")
	tasks := strings.Join([]string{
		"- [ ] T001 references FR-555",
		"- [ ] T002 references FR-777",
	}, "\n")

	got := L03TaskFRCoverage(spec, tasks)
	if len(got) != 4 {
		t.Fatalf("expected 4 violations, got %d: %+v", len(got), got)
	}
	wantOrder := []struct {
		file, fr string
		line     int
	}{
		{"spec.md", "FR-001", 1},
		{"spec.md", "FR-003", 2},
		{"tasks.md", "FR-555", 1},
		{"tasks.md", "FR-777", 2},
	}
	for i, w := range wantOrder {
		v := got[i]
		if v.File != w.file || v.Line != w.line || !strings.Contains(v.Message, w.fr) {
			t.Errorf("violation[%d]: got %+v, want file=%s line=%d containing %s",
				i, v, w.file, w.line, w.fr)
		}
	}
}

func TestL03_EmptyInputs_NoViolations(t *testing.T) {
	if got := L03TaskFRCoverage("", ""); len(got) != 0 {
		t.Errorf("empty in, empty out — got %+v", got)
	}
}

func TestL03_RepeatedDeclarationAndReference_RecordFirstLine(t *testing.T) {
	spec := strings.Join([]string{
		"- **FR-001** Declared once.",
		"- **FR-001** Repeated declaration (legal markdown, lint accepts).",
		"- **FR-002** Orphan.",
	}, "\n")
	tasks := strings.Join([]string{
		"- [ ] T001 References FR-001 here.",
		"- [ ] T002 Also references FR-001.",
	}, "\n")

	got := L03TaskFRCoverage(spec, tasks)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation (orphan FR-002), got %d: %+v", len(got), got)
	}
	if got[0].Line != 3 {
		t.Errorf("FR-002 is declared on line 3, violation reported line %d", got[0].Line)
	}
}

func TestL03_CrossSpecTaskFRReferencesIgnored(t *testing.T) {
	// tasks.md routinely cites other specs' FRs for context, e.g.
	// "extends spec 113 FR-010 behavior". Those are NOT local task
	// references — they must not produce "unknown FR" violations
	// against the current spec.
	spec := strings.Join([]string{
		"- **FR-001** Local requirement.",
	}, "\n")
	tasks := strings.Join([]string{
		"- [ ] T001 Implement FR-001",
		"- [ ] T002 [US1] Honors the taxonomy from spec 113 FR-010 and spec 114 FR-008",
	}, "\n")

	got := L03TaskFRCoverage(spec, tasks)
	if len(got) != 0 {
		t.Fatalf("cross-spec FR references must be ignored, got %d violations: %+v", len(got), got)
	}
}

func TestL03_PlainFRInSpecBodyIgnored(t *testing.T) {
	// Spec prose like "extends spec 113 FR-001" must NOT be treated as a
	// local declaration. Only the **FR-NNN** bold form declares — otherwise
	// L03 would silently absolve every orphan an author happened to
	// mention in their narrative.
	spec := strings.Join([]string{
		"## Why",
		"This spec extends spec 113 FR-001 behavior.",
		"",
		"- **FR-001** Real declaration.",
	}, "\n")
	tasks := "- [ ] T001 Implement FR-001\n"

	got := L03TaskFRCoverage(spec, tasks)
	if len(got) != 0 {
		t.Fatalf("expected no violations, got %+v", got)
	}
}

func TestL03_FourDigitFRSupported(t *testing.T) {
	// The regex floor is 3 digits; longer ids must also resolve.
	spec := "- **FR-1001** Big-number requirement.\n"
	tasks := "- [ ] T001 Implement FR-1001\n"

	if got := L03TaskFRCoverage(spec, tasks); len(got) != 0 {
		t.Fatalf("FR-1001 should resolve cleanly, got %+v", got)
	}
}
