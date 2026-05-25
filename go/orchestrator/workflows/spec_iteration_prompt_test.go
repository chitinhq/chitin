package workflows

import (
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/internal/speclint"
)

func TestBuildSpecIterationPrompt_PureAndDeterministic(t *testing.T) {
	in := SpecIterationPromptInput{
		PRNumber:    1050,
		Round:       1,
		SpecDirName: "115-spec-review-gate",
		SpecMD:      "# Spec 115\nfoo bar\n",
		TasksMD:     "- [ ] T001 do the thing\n",
		LintViolations: []speclint.Violation{
			{Rule: "L05", File: "spec.md", Line: 78, Severity: speclint.SeverityError,
				Message: "chitin-kernel events not in known-cli-surfaces.txt"},
		},
		ReviewBody: "Looks mostly good — see inline.",
		LineComments: []SpecReviewLineComment{
			{Path: "spec.md", Line: 113, Body: "this endpoint returns 404"},
		},
	}

	a := BuildSpecIterationPrompt(in)
	b := BuildSpecIterationPrompt(in)
	if a != b {
		t.Fatalf("BuildSpecIterationPrompt is not deterministic across calls with identical input")
	}
}

func TestBuildSpecIterationPrompt_Shape(t *testing.T) {
	in := SpecIterationPromptInput{
		PRNumber:    1050,
		Round:       2,
		SpecDirName: "115-spec-review-gate",
		SpecMD:      "# Spec 115 body\n",
		TasksMD:     "tasks body\n",
		LintViolations: []speclint.Violation{
			{Rule: "L01", File: "spec.md", Line: 1, Severity: speclint.SeverityError,
				Message: "frontmatter missing 'owner'"},
			{Rule: "L05", File: "spec.md", Line: 78, Severity: speclint.SeverityError,
				Message: "chitin-kernel events not in known-cli-surfaces.txt"},
		},
		ReviewBody: "Looks mostly good — see inline.",
		LineComments: []SpecReviewLineComment{
			{Path: "spec.md", Line: 113, Body: "this endpoint returns 404"},
			{Path: "tasks.md", Line: 4, Body: "T002 is missing the [P] marker"},
		},
	}
	out := BuildSpecIterationPrompt(in)

	mustContain := []string{
		// Header carries PR + round + spec-dir slug.
		"SPEC PR #1050 (round 2)",
		`"115-spec-review-gate"`,
		// Full spec.md + tasks.md included verbatim, fenced so the driver
		// cannot mistake them for prompt instructions.
		"=== CURRENT spec.md ===",
		"# Spec 115 body",
		"=== END spec.md ===",
		"=== CURRENT tasks.md ===",
		"tasks body",
		"=== END tasks.md ===",
		// FR-006 envelope: lint and Copilot are distinct channels.
		"LINTER VIOLATIONS (2):",
		"L01 spec.md:1 (error)",
		"L05 spec.md:78 (error)",
		"COPILOT REVIEW BODY:",
		"Looks mostly good",
		"COPILOT LINE COMMENTS (2):",
		"spec.md:113",
		"tasks.md:4",
		// FR-006 envelope: required output contract — fix OR justify
		// allowlist patch, else escalate as lint_violation_unresolvable.
		"you MUST do exactly one of",
		"Fix the spec/tasks file",
		"Patch the linter allowlist",
		"lint_violation_unresolvable",
		// Orchestrator owns the commit + push, not the driver.
		"do not write commit",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing required substring %q\n--- prompt ---\n%s", want, out)
		}
	}
}

// TestBuildSpecIterationPrompt_EmptyChannels — when neither the linter nor
// Copilot produced anything, the prompt still renders both section headers
// with an explicit "none." line. Constant prompt shape protects the driver
// from confusing "no findings" with "findings forgotten by the orchestrator".
func TestBuildSpecIterationPrompt_EmptyChannels(t *testing.T) {
	out := BuildSpecIterationPrompt(SpecIterationPromptInput{
		PRNumber:    42,
		Round:       1,
		SpecDirName: "115-spec-review-gate",
		SpecMD:      "spec\n",
		TasksMD:     "tasks\n",
	})
	for _, want := range []string{
		"LINTER VIOLATIONS: none.",
		"COPILOT REVIEW BODY: none.",
		"COPILOT LINE COMMENTS: none.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("empty-channel prompt missing %q\n--- prompt ---\n%s", want, out)
		}
	}
}

// TestBuildSpecIterationPrompt_TrailingNewline — spec.md / tasks.md without
// a final newline must still close cleanly: the "=== END" fence sits on its
// own line, not merged onto the spec's last line.
func TestBuildSpecIterationPrompt_TrailingNewline(t *testing.T) {
	out := BuildSpecIterationPrompt(SpecIterationPromptInput{
		PRNumber:    7,
		Round:       1,
		SpecDirName: "115-spec-review-gate",
		SpecMD:      "no trailing newline",
		TasksMD:     "no trailing newline either",
	})
	if !strings.Contains(out, "no trailing newline\n=== END spec.md ===") {
		t.Errorf("spec.md without trailing newline merged with fence:\n%s", out)
	}
	if !strings.Contains(out, "no trailing newline either\n=== END tasks.md ===") {
		t.Errorf("tasks.md without trailing newline merged with fence:\n%s", out)
	}
}
