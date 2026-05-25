package speclint

import (
	"reflect"
	"strings"
	"testing"
)

func TestCheckL06_EmptyInputs(t *testing.T) {
	got := CheckL06("spec.md", "", "tasks.md", "", nil)
	if got != nil {
		t.Fatalf("expected nil violations for empty inputs, got %+v", got)
	}
}

func TestCheckL06_NoCanonicalNoReferences(t *testing.T) {
	spec := "## Why\n\nPlain prose with no reason references.\n"
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if got != nil {
		t.Fatalf("expected nil violations, got %+v", got)
	}
}

func TestCheckL06_CanonicalMatchedReferences(t *testing.T) {
	spec := `## Functional requirements

- **FR-010** Canonical ` + "`reason`" + ` strings for ` + "`x_escalated`" + `:
  - ` + "`iteration_cap_hit`" + ` — cap reached
  - ` + "`design_judgement_required`" + ` — escalate to operator

## Edge cases

Workflow emits ` + "`x_escalated { reason: \"iteration_cap_hit\" }`" + ` when…
Operator triage uses ` + "`reason: \"design_judgement_required\"`" + ` to filter.
`
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if got != nil {
		t.Fatalf("expected nil violations, got %+v", got)
	}
}

func TestCheckL06_UnknownReferenceFlaggedInSpec(t *testing.T) {
	spec := `## Functional requirements

- **FR-010** Canonical ` + "`reason`" + ` strings:
  - ` + "`iteration_cap_hit`" + ` — cap reached

## Edge cases

Emits ` + "`x { reason: \"never_declared\" }`" + ` on weird state.
`
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.Rule != "L06" || v.File != "spec.md" || v.Severity != "error" {
		t.Errorf("violation header wrong: %+v", v)
	}
	if !strings.Contains(v.Message, `"never_declared"`) {
		t.Errorf("expected message to quote the offending value, got %q", v.Message)
	}
}

func TestCheckL06_BacktickQuotedReferenceForm(t *testing.T) {
	spec := `## Functional requirements

- **FR-010** Canonical ` + "`reason`" + ` strings:
  - ` + "`iteration_cap_hit`" + ` — cap reached

## Notes

Operator filter: reason: ` + "`mystery_reason`" + ` should not exist.
`
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, `"mystery_reason"`) {
		t.Errorf("expected message to mention mystery_reason, got %q", got[0].Message)
	}
}

func TestCheckL06_NewlineWrappedReferenceIsDetected(t *testing.T) {
	spec := `## Functional requirements

- **FR-010** Canonical ` + "`reason`" + ` strings:
  - ` + "`iteration_cap_hit`" + ` — cap reached

## Edge cases

Factory emits ` + "`x { reason:" + `
"undeclared_value" }` + "`" + ` when wrapped.
`
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation from wrapped form, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, `"undeclared_value"`) {
		t.Errorf("expected wrapped value to be captured, got %q", got[0].Message)
	}
}

func TestCheckL06_ReferenceLineIsValueLineNotKeywordLine(t *testing.T) {
	// `reason:` on line 5, value on line 6. Reporter should point at line 6.
	spec := "## Functional requirements\n\n" +
		"- **FR-010** Canonical `reason` strings:\n" +
		"  - `ok_value` — fine\n" +
		"Wrap: `x { reason:\n" +
		"\"bad_value\" }`\n"
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(got))
	}
	if got[0].Line != 6 {
		t.Errorf("expected line 6 (value line), got %d", got[0].Line)
	}
}

func TestCheckL06_ReferenceInTasksFlagged(t *testing.T) {
	spec := `## Functional requirements

- **FR-010** Canonical ` + "`reason`" + ` strings:
  - ` + "`iteration_cap_hit`" + ` — cap reached
`
	tasks := "- [ ] T015 assert event has reason: \"not_in_taxonomy\" on cap path\n"
	got := CheckL06("spec.md", spec, "tasks.md", tasks, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation from tasks, got %d: %+v", len(got), got)
	}
	if got[0].File != "tasks.md" {
		t.Errorf("expected violation file=tasks.md, got %q", got[0].File)
	}
	if !strings.Contains(got[0].Message, `"not_in_taxonomy"`) {
		t.Errorf("expected message to mention not_in_taxonomy, got %q", got[0].Message)
	}
}

func TestCheckL06_DepSpecContributesToCanonical(t *testing.T) {
	dep := `## Functional requirements

- **FR-011** Canonical ` + "`reason`" + ` strings for ` + "`pr_escalated`" + `:
  - ` + "`iteration_cap_hit`" + ` — cap reached
  - ` + "`human_reviewer_present`" + ` — non-allowlisted reviewer
`
	spec := `## Functional requirements

- **FR-010** Canonical ` + "`reason`" + ` strings extending FR-011:
  - ` + "`design_judgement_required`" + ` — escalate to operator

## Edge cases

Inherits ` + "`{ reason: \"iteration_cap_hit\" }`" + ` from dep.
Also emits ` + "`{ reason: \"design_judgement_required\" }`" + ` locally.
`
	got := CheckL06("spec.md", spec, "tasks.md", "", []string{dep})
	if got != nil {
		t.Fatalf("expected nil violations when dep supplies canonical, got %+v", got)
	}
}

func TestCheckL06_DepUnknownStillFlagged(t *testing.T) {
	dep := `## Functional requirements

- **FR-011** Canonical ` + "`reason`" + ` strings:
  - ` + "`iteration_cap_hit`" + ` — cap reached
`
	spec := `## Edge cases

Refers to ` + "`{ reason: \"absent_everywhere\" }`" + `.
`
	got := CheckL06("spec.md", spec, "tasks.md", "", []string{dep})
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
}

func TestCheckL06_MultipleViolationsEachReported(t *testing.T) {
	spec := "## Functional requirements\n\n" +
		"- **FR-010** Canonical `reason` strings:\n" +
		"  - `known` — fine\n\n" +
		"## Edge cases\n\n" +
		"emits `{ reason: \"first_bad\" }`.\n" +
		"emits `{ reason: \"second_bad\" }`.\n" +
		"emits `{ reason: \"first_bad\" }` again.\n"
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if len(got) != 3 {
		t.Fatalf("expected 3 violations (each occurrence flagged), got %d: %+v", len(got), got)
	}
	values := []string{}
	for _, v := range got {
		// pull the quoted value out of the message
		start := strings.Index(v.Message, `"`)
		end := strings.LastIndex(v.Message, `"`)
		if start >= 0 && end > start {
			values = append(values, v.Message[start+1:end])
		}
	}
	want := []string{"first_bad", "second_bad", "first_bad"}
	if !reflect.DeepEqual(values, want) {
		t.Errorf("expected violation values %v in document order, got %v", want, values)
	}
}

func TestCheckL06_NonCanonicalFRBlockIgnored(t *testing.T) {
	// FR-001 has backticked snake_case bullets but header doesn't say
	// "canonical reason" — must not contribute to canonical set.
	spec := "## Functional requirements\n\n" +
		"- **FR-001** Event taxonomy for routing:\n" +
		"  - `not_a_reason_value` — first event\n" +
		"  - `also_not_a_reason` — second event\n\n" +
		"- **FR-010** Canonical `reason` strings:\n" +
		"  - `valid_reason` — fine\n\n" +
		"## Edge cases\n\n" +
		"emits `{ reason: \"not_a_reason_value\" }`.\n"
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation (FR-001 bullets must not count as canonical), got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, `"not_a_reason_value"`) {
		t.Errorf("expected message to mention not_a_reason_value, got %q", got[0].Message)
	}
}

func TestCheckL06_H2BoundsCanonicalCollection(t *testing.T) {
	// Section AFTER the canonical FR-010 has a backticked snake_case
	// bullet shaped like a canonical entry. The H2 boundary must stop
	// the canonical-bullet sweep so that bullet isn't pulled into the set.
	spec := "## Functional requirements\n\n" +
		"- **FR-010** Canonical `reason` strings:\n" +
		"  - `legit_reason` — fine\n\n" +
		"## Edge cases\n\n" +
		"- `phantom_reason` — a backticked bullet outside the FR block\n\n" +
		"emits `{ reason: \"phantom_reason\" }` from the edge-case bullet.\n"
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation (phantom_reason must not be canonical despite the H2-separated bullet), got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, `"phantom_reason"`) {
		t.Errorf("expected message to mention phantom_reason, got %q", got[0].Message)
	}
}

func TestCheckL06_BacktickedReasonCodeSpanNotRef(t *testing.T) {
	// The L06 description in spec 115 contains ``every `reason:` string``
	// — a code-span around `reason:` itself, NOT a reference. Must not
	// flag anything.
	spec := "## Functional requirements\n\n" +
		"- **FR-010** Canonical `reason` strings:\n" +
		"  - `iteration_cap_hit` — cap reached\n\n" +
		"- **L06 — reason taxonomy alignment**: every `reason:` string\n" +
		"  referenced must be canonical.\n"
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if got != nil {
		t.Fatalf("expected nil violations (the prose `reason:` code span is not a value reference), got %+v", got)
	}
}

func TestCheckL06_EventShapeBulletIsNotCanonical(t *testing.T) {
	// Bullets like ``- `event_name { field, field }` `` close the
	// backtick on `}` — not on a separator — so they must NOT be
	// captured as canonical reasons. (FR-009 telemetry bullets follow
	// this shape.)
	spec := "## Functional requirements\n\n" +
		"- **FR-010** Canonical `reason` strings:\n" +
		"  - `valid_one` — fine\n\n" +
		"### Telemetry\n\n" +
		"- **FR-009** Chain events:\n" +
		"  - `event_name { pr_number, reason }`\n" +
		"  - `other_event { fields }`\n\n" +
		"## Edge cases\n\n" +
		"emits `{ reason: \"event_name\" }`.\n"
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation (event_name must not be canonical), got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, `"event_name"`) {
		t.Errorf("expected message to mention event_name, got %q", got[0].Message)
	}
}

func TestCheckL06_BareReasonFieldInJSONShapeNotARef(t *testing.T) {
	// `pr_iteration_skipped { pr_number, reason }` uses `reason` as a
	// bare field name (no colon, no value). Must not be flagged.
	spec := "## Functional requirements\n\n" +
		"- **FR-010** Canonical `reason` strings:\n" +
		"  - `valid` — fine\n\n" +
		"- **FR-009** Events:\n" +
		"  - `pr_iteration_skipped { pr_number, reason }`\n"
	got := CheckL06("spec.md", spec, "tasks.md", "", nil)
	if got != nil {
		t.Fatalf("expected nil violations (bare reason field name is not a reference), got %+v", got)
	}
}

func TestL06DependsOnIDs_BlockList(t *testing.T) {
	content := "---\n" +
		"spec_id: 115\n" +
		"title: example\n" +
		"depends_on:\n" +
		"  - 097\n" +
		"  - 113\n" +
		"related:\n" +
		"  - 094\n" +
		"---\n" +
		"\n" +
		"# Body\n"
	got := L06DependsOnIDs(content)
	want := []string{"097", "113"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestL06DependsOnIDs_InlineFlowList(t *testing.T) {
	content := "---\n" +
		"spec_id: 115\n" +
		"depends_on: [97, 113]\n" +
		"related: []\n" +
		"---\n"
	got := L06DependsOnIDs(content)
	want := []string{"097", "113"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestL06DependsOnIDs_QuotedInlineEntries(t *testing.T) {
	content := "---\n" +
		"depends_on: [\"113\", '97']\n" +
		"---\n"
	got := L06DependsOnIDs(content)
	want := []string{"113", "097"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestL06DependsOnIDs_EmptyInlineList(t *testing.T) {
	content := "---\n" +
		"depends_on: []\n" +
		"---\n"
	got := L06DependsOnIDs(content)
	if got != nil {
		t.Errorf("expected nil for empty inline list, got %v", got)
	}
}

func TestL06DependsOnIDs_NoFrontmatter(t *testing.T) {
	if got := L06DependsOnIDs("# Just a title\n"); got != nil {
		t.Errorf("expected nil with no frontmatter, got %v", got)
	}
}

func TestL06DependsOnIDs_NoDependsOnKey(t *testing.T) {
	content := "---\n" +
		"spec_id: 115\n" +
		"title: noop\n" +
		"---\n"
	if got := L06DependsOnIDs(content); got != nil {
		t.Errorf("expected nil when depends_on absent, got %v", got)
	}
}

func TestL06DependsOnIDs_NonIntegerEntriesSkipped(t *testing.T) {
	// L02 is responsible for flagging malformed cross-refs; L06 just
	// ignores entries it can't parse so it doesn't false-positive when
	// L02 has already reported.
	content := "---\n" +
		"depends_on:\n" +
		"  - 113\n" +
		"  - not-a-number\n" +
		"  - 097\n" +
		"---\n"
	got := L06DependsOnIDs(content)
	want := []string{"113", "097"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestL06DependsOnIDs_BlockListEndsAtTopLevelKey(t *testing.T) {
	content := "---\n" +
		"depends_on:\n" +
		"  - 113\n" +
		"related:\n" +
		"  - 094\n" +
		"---\n"
	got := L06DependsOnIDs(content)
	want := []string{"113"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCheckL06_RealSpec115PositiveFixture asserts the rule passes
// against spec 115's own content + spec 113's canonical reason set as a
// dep. Spec 115's only `reason:` value-reference is
// `"design_judgement_required"`, which is declared canonical in spec
// 115's own FR-010. Spec 113 contributes `iteration_cap_hit`,
// `human_reviewer_present`, `lease_lost`, `iteration_completed_with_skips`.
// Spec 115 references none of those outside of FR-010 itself, so
// linting must come back clean.
func TestCheckL06_RealSpec115PositiveFixture(t *testing.T) {
	spec := realSpec115Excerpt
	dep113 := realSpec113Excerpt

	got := CheckL06("spec.md", spec, "tasks.md", "", []string{dep113})
	if got != nil {
		t.Fatalf("spec 115 must lint clean against itself + spec 113, got violations: %+v", got)
	}
}

// realSpec115Excerpt is the minimal slice of spec 115 needed to exercise
// L06: the canonical FR-010 declaration plus the two prose references
// that wrap `{ reason: "design_judgement_required" }`. Trimmed for
// stability — adding or removing canonical entries elsewhere in the
// real file should not affect this test.
const realSpec115Excerpt = "" +
	"## User stories\n" +
	"\n" +
	"### US3 (P2)\n" +
	"\n" +
	"**Independent test:** Copilot leaves a comment ...\n" +
	"a heuristic — see FR-007), emits `spec_iteration_escalated { reason:\n" +
	"\"design_judgement_required\" }` without dispatching a driver round.\n" +
	"\n" +
	"## Functional requirements\n" +
	"\n" +
	"- **FR-008** When all of a Copilot review's comments classify as\n" +
	"  design-judgement, the workflow skips driver dispatch and emits\n" +
	"  `spec_iteration_escalated { reason: \"design_judgement_required\" }`\n" +
	"  immediately. When some are mechanical and some are judgement, the\n" +
	"  workflow iterates the mechanical ones AND escalates the judgement\n" +
	"  ones in the same round.\n" +
	"\n" +
	"- **FR-010** Canonical `reason` strings for `spec_iteration_escalated`\n" +
	"  (closed set — extends spec 113 FR-011's vocabulary with the\n" +
	"  spec-specific kinds):\n" +
	"  - `iteration_cap_hit` — same semantics as spec 113\n" +
	"  - `human_reviewer_present` — same semantics\n" +
	"  - `lease_lost` — same semantics\n" +
	"  - `design_judgement_required` — FR-008 classification fired\n" +
	"  - `lint_violation_unresolvable` — driver couldn't fix the lint\n" +
	"    violation and didn't justify patching the allowlist\n" +
	"\n" +
	"## Success criteria\n" +
	"\n" +
	"- **SC-001** Re-running the scenario produces a single fixup.\n"

const realSpec113Excerpt = "" +
	"## Functional requirements\n" +
	"\n" +
	"- **FR-011** Canonical `reason` strings used in `pr_iteration_escalated`\n" +
	"  events. The set is closed; spec 114's queue-filter taxonomy (FR-008)\n" +
	"  MUST be the same vocabulary string-for-string:\n" +
	"  - `iteration_cap_hit` — FR-008 cap reached with ≥1 unaddressed comment\n" +
	"  - `human_reviewer_present` — FR-009 non-allowlisted reviewer detected\n" +
	"  - `lease_lost` — force-push lost its lease\n" +
	"  - `iteration_completed_with_skips` — round completed cleanly but skip>0\n" +
	"\n" +
	"## Success criteria\n"
