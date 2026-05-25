package workflows

import (
	"strings"
	"testing"
)

// TestBuildSpecIterationPrompt_ShapeAndContent asserts the rendered
// prompt carries every load-bearing element required by spec 115 FR-006:
// PR + round header, full spec.md + tasks.md contents (verbatim, not a
// diff), linter-violation block distinct from Copilot comments, and the
// fix-or-allowlist output envelope.
func TestBuildSpecIterationPrompt_ShapeAndContent(t *testing.T) {
	in := SpecIterationPromptInput{
		PRNumber: 1050,
		Round:    2,
		SpecDir:  ".specify/specs/115-spec-review-gate",
		SpecMD: "# Spec 115\n\n## Why\n\nMARKER-SPECBODY-XYZ — keeps the renderer honest.\n",
		TasksMD: "- [ ] T001 MARKER-TASKBODY-QQQ implement thing\n",
		LintViolations: []SpecLintViolation{
			{Rule: "L05", File: "spec.md", Line: 78, Severity: "error",
				Message: "chitin-kernel events: subcommand not in allowlist"},
			{Rule: "L04", File: "spec.md", Line: 0, Severity: "warning",
				Message: "event_type spec_iteration_skipped not in canonical telemetry block"},
		},
		ReviewBody: "Looks mostly good but a couple of mechanical issues remain.",
		LineComments: []SpecReviewLineComment{
			{Path: "spec.md", Line: 110, Body: "lease_lost reason — where is the FR that produces it?"},
		},
	}

	got := BuildSpecIterationPrompt(in)

	// Header carries PR + round.
	if !strings.Contains(got, "PR #1050") {
		t.Errorf("prompt missing PR number; got:\n%s", got)
	}
	if !strings.Contains(got, "round 2") {
		t.Errorf("prompt missing round number; got:\n%s", got)
	}
	if !strings.Contains(got, in.SpecDir) {
		t.Errorf("prompt missing spec dir %q; got:\n%s", in.SpecDir, got)
	}

	// Full spec.md and tasks.md inline (FR-006: full, not a diff).
	if !strings.Contains(got, "MARKER-SPECBODY-XYZ") {
		t.Errorf("prompt missing full spec.md body marker; got:\n%s", got)
	}
	if !strings.Contains(got, "MARKER-TASKBODY-QQQ") {
		t.Errorf("prompt missing full tasks.md body marker; got:\n%s", got)
	}
	if !strings.Contains(got, "CURRENT spec.md") || !strings.Contains(got, "END spec.md") {
		t.Errorf("prompt missing spec.md section markers; got:\n%s", got)
	}
	if !strings.Contains(got, "CURRENT tasks.md") || !strings.Contains(got, "END tasks.md") {
		t.Errorf("prompt missing tasks.md section markers; got:\n%s", got)
	}

	// Linter section — distinct from Copilot, with rule+severity+location.
	if !strings.Contains(got, "LINTER VIOLATIONS") {
		t.Errorf("prompt missing LINTER VIOLATIONS section; got:\n%s", got)
	}
	if !strings.Contains(got, "L05") || !strings.Contains(got, "error") || !strings.Contains(got, "spec.md:78") {
		t.Errorf("prompt missing L05 violation details; got:\n%s", got)
	}
	if !strings.Contains(got, "L04") || !strings.Contains(got, "warning") {
		t.Errorf("prompt missing L04 violation details; got:\n%s", got)
	}
	// Line=0 violation should render WITHOUT a :0 suffix.
	if strings.Contains(got, "spec.md:0") {
		t.Errorf("line=0 violation should not render :0 suffix; got:\n%s", got)
	}

	// Copilot review carried separately.
	if !strings.Contains(got, "REVIEW BODY:") || !strings.Contains(got, "couple of mechanical issues") {
		t.Errorf("prompt missing REVIEW BODY content; got:\n%s", got)
	}
	if !strings.Contains(got, "LINE COMMENTS:") || !strings.Contains(got, "lease_lost reason") {
		t.Errorf("prompt missing LINE COMMENTS content; got:\n%s", got)
	}

	// Output envelope — FR-006 (a) fix or (b) allowlist + justification.
	if !strings.Contains(got, "OUTPUT ENVELOPE") {
		t.Errorf("prompt missing OUTPUT ENVELOPE section; got:\n%s", got)
	}
	if !strings.Contains(got, "Edit spec.md or tasks.md") {
		t.Errorf("prompt missing fix-the-spec option; got:\n%s", got)
	}
	if !strings.Contains(got, ".specify/known-cli-surfaces.txt") {
		t.Errorf("prompt missing allowlist file reference; got:\n%s", got)
	}
	if !strings.Contains(got, "justifying") {
		t.Errorf("prompt missing justification requirement; got:\n%s", got)
	}
	if !strings.Contains(got, "lint_violation_unresolvable") {
		t.Errorf("prompt missing escalation reason for unaddressed lint; got:\n%s", got)
	}

	// Closing — driver exits, no commit message.
	if !strings.Contains(got, "Do not run tests") {
		t.Errorf("prompt missing exit instruction; got:\n%s", got)
	}
}

// TestBuildSpecIterationPrompt_OmitsEmptyReviewSections asserts the
// Copilot review body and line-comments sections are omitted entirely
// when empty. A spec PR can carry zero Copilot comments and only linter
// findings (or vice-versa); the prompt should not advertise empty
// sections to the driver.
func TestBuildSpecIterationPrompt_OmitsEmptyReviewSections(t *testing.T) {
	in := SpecIterationPromptInput{
		PRNumber: 7,
		Round:    1,
		SpecMD:   "# Spec 999\n",
		TasksMD:  "tasks\n",
		LintViolations: []SpecLintViolation{
			{Rule: "L01", File: "spec.md", Line: 1, Severity: "error",
				Message: "frontmatter missing spec_id"},
		},
		ReviewBody:   "   \n\t ", // whitespace only
		LineComments: nil,
	}

	got := BuildSpecIterationPrompt(in)

	if strings.Contains(got, "REVIEW BODY:") {
		t.Errorf("prompt should omit REVIEW BODY on empty body; got:\n%s", got)
	}
	if strings.Contains(got, "LINE COMMENTS:") {
		t.Errorf("prompt should omit LINE COMMENTS when none present; got:\n%s", got)
	}
	if !strings.Contains(got, "LINTER VIOLATIONS") {
		t.Errorf("prompt should still surface lint violations; got:\n%s", got)
	}
	if !strings.Contains(got, "OUTPUT ENVELOPE") {
		t.Errorf("prompt should still carry output envelope; got:\n%s", got)
	}
}

// TestBuildSpecIterationPrompt_OmitsEmptyLintSection asserts the linter
// block is omitted entirely when no violations were found. A Copilot-only
// round (no lint findings) should not advertise an empty linter section.
func TestBuildSpecIterationPrompt_OmitsEmptyLintSection(t *testing.T) {
	in := SpecIterationPromptInput{
		PRNumber:   1,
		Round:      1,
		SpecMD:     "spec\n",
		TasksMD:    "tasks\n",
		ReviewBody: "Please clarify the wording in §Why.",
	}
	got := BuildSpecIterationPrompt(in)
	if strings.Contains(got, "LINTER VIOLATIONS") {
		t.Errorf("prompt should omit linter section when no violations; got:\n%s", got)
	}
	if !strings.Contains(got, "Please clarify") {
		t.Errorf("prompt should still carry review body; got:\n%s", got)
	}
}

// TestBuildSpecIterationPrompt_NoFeedback asserts the rendered prompt
// for the no-lint / no-review case is internally consistent: it tells
// the driver to exit without changes, omits both feedback sections and
// the output envelope, and does NOT carry the "After making changes,
// exit…" closing sentence that would contradict the exit-without-changes
// instruction.
func TestBuildSpecIterationPrompt_NoFeedback(t *testing.T) {
	in := SpecIterationPromptInput{
		PRNumber: 9,
		Round:    1,
		SpecMD:   "# Spec\n",
		TasksMD:  "tasks\n",
	}
	got := BuildSpecIterationPrompt(in)

	if !strings.Contains(got, "Exit without changes") {
		t.Errorf("prompt should tell driver to exit without changes; got:\n%s", got)
	}
	if strings.Contains(got, "After making changes, exit") {
		t.Errorf("prompt should not carry the post-edit exit sentence when there is nothing to address; got:\n%s", got)
	}
	if strings.Contains(got, "LINTER VIOLATIONS") {
		t.Errorf("prompt should omit linter section on no-feedback round; got:\n%s", got)
	}
	if strings.Contains(got, "REVIEW BODY:") || strings.Contains(got, "LINE COMMENTS:") {
		t.Errorf("prompt should omit review sections on no-feedback round; got:\n%s", got)
	}
	if strings.Contains(got, "OUTPUT ENVELOPE") {
		t.Errorf("prompt should omit output envelope on no-feedback round; got:\n%s", got)
	}
}

// TestBuildSpecIterationPrompt_IsPure asserts the helper is deterministic
// — calling it twice with identical input yields byte-identical output.
// Pure functions in workflows package must be replay-safe.
func TestBuildSpecIterationPrompt_IsPure(t *testing.T) {
	in := SpecIterationPromptInput{
		PRNumber: 42,
		Round:    1,
		SpecMD:   "a\n",
		TasksMD:  "b\n",
		LintViolations: []SpecLintViolation{
			{Rule: "L02", File: "spec.md", Line: 5, Severity: "error", Message: "msg"},
		},
		LineComments: []SpecReviewLineComment{
			{Path: "spec.md", Line: 9, Body: "comment"},
		},
	}
	a := BuildSpecIterationPrompt(in)
	b := BuildSpecIterationPrompt(in)
	if a != b {
		t.Errorf("BuildSpecIterationPrompt is not deterministic; first call diverges from second")
	}
}
