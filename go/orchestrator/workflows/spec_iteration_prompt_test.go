package workflows

import (
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/internal/speclint"
)

// TestBuildSpecIterationPrompt_ShapeAndContent asserts the prompt template
// includes the PR header, the full spec.md + tasks.md (FR-006), every
// lint violation in its own distinct section (FR-006), every Copilot
// comment with file+line+body, the fix-or-allowlist output contract for
// lint findings, and the post-action exit instruction. Pure-function
// test — no IO.
func TestBuildSpecIterationPrompt_ShapeAndContent(t *testing.T) {
	in := SpecIterationPromptInput{
		PRNumber:    1234,
		Round:       2,
		SpecDirPath: ".specify/specs/115-spec-review-gate",
		SpecMD:      "# Spec 115\n\nFull spec body goes here.\n",
		TasksMD:     "- [ ] T001 first task\n- [ ] T002 second task\n",
		LintViolations: []speclint.Violation{
			{Rule: "L05", File: "spec.md", Line: 78, Severity: speclint.SeverityError, Message: "unknown chitin-kernel subcommand: events"},
			{Rule: "L04", File: "spec.md", Line: 182, Severity: speclint.SeverityWarning, Message: "event_type pr_iteration_skipped not in canonical telemetry"},
		},
		ReviewBody: "Mostly good — a couple of inline notes.",
		LineComments: []SpecCopilotComment{
			{ID: 1, Path: "spec.md", Line: 17, Body: "Consider naming this US explicitly."},
			{ID: 2, Path: "tasks.md", Line: 9, Body: "Should T003 land before T002?"},
		},
	}

	got := BuildSpecIterationPrompt(in)

	// PR header + round + spec dir
	for _, want := range []string{"PR #1234", "round 2", ".specify/specs/115-spec-review-gate"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q; got:\n%s", want, got)
		}
	}

	// Full spec + tasks inlined (FR-006 explicit: full context, not a diff)
	if !strings.Contains(got, "CURRENT SPEC") || !strings.Contains(got, "Full spec body goes here.") {
		t.Errorf("prompt missing full spec.md content; got:\n%s", got)
	}
	if !strings.Contains(got, "CURRENT TASKS") || !strings.Contains(got, "T002 second task") {
		t.Errorf("prompt missing full tasks.md content; got:\n%s", got)
	}

	// Lint violations section present, distinct from Copilot comments
	lintIdx := strings.Index(got, "LINT VIOLATIONS:")
	commentsIdx := strings.Index(got, "LINE COMMENTS:")
	if lintIdx < 0 || commentsIdx < 0 {
		t.Fatalf("prompt missing distinct LINT VIOLATIONS / LINE COMMENTS sections; got:\n%s", got)
	}
	if lintIdx > commentsIdx {
		t.Errorf("LINT VIOLATIONS must appear before LINE COMMENTS so the driver reads them first; got:\n%s", got)
	}

	// Each lint violation surfaces with rule + file + line + severity + message
	for _, v := range in.LintViolations {
		for _, want := range []string{v.Rule, v.File, string(v.Severity), v.Message} {
			if !strings.Contains(got, want) {
				t.Errorf("prompt missing lint field %q; got:\n%s", want, got)
			}
		}
	}

	// FR-006 output contract: fix-spec OR patch-allowlist, with escalation kind named
	for _, want := range []string{"FIX SPEC", "PATCH ALLOWLIST", "lint_violation_unresolvable"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing FR-006 output-contract token %q; got:\n%s", want, got)
		}
	}

	// Each Copilot comment surfaces with file+line+body
	for _, c := range in.LineComments {
		if !strings.Contains(got, c.Path+":") {
			t.Errorf("prompt missing comment file marker %q; got:\n%s", c.Path, got)
		}
		if !strings.Contains(got, c.Body) {
			t.Errorf("prompt missing comment body %q; got:\n%s", c.Body, got)
		}
	}

	// Closing instruction — driver must exit, not run tests, not write commits
	for _, want := range []string{"Do not run tests", "single fixup commit"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing closing instruction %q; got:\n%s", want, got)
		}
	}
}

// TestBuildSpecIterationPrompt_OmitsEmptySections asserts every optional
// section is omitted entirely when its source is empty / whitespace — so
// a Copilot review with only a body (no inline comments) doesn't produce
// a hollow LINE COMMENTS: header, and a PR with no lint violations
// doesn't produce a hollow LINT VIOLATIONS: header.
func TestBuildSpecIterationPrompt_OmitsEmptySections(t *testing.T) {
	in := SpecIterationPromptInput{
		PRNumber:    7,
		Round:       1,
		SpecDirPath: ".specify/specs/115-spec-review-gate",
		SpecMD:      "# Spec 115\n",
		TasksMD:     "- [ ] T001\n",
		ReviewBody:  "  \n\t  ", // whitespace only
	}
	got := BuildSpecIterationPrompt(in)
	for _, banned := range []string{"LINT VIOLATIONS:", "REVIEW BODY:", "LINE COMMENTS:"} {
		if strings.Contains(got, banned) {
			t.Errorf("prompt should omit %q when its source is empty; got:\n%s", banned, got)
		}
	}
	// Output contract still surfaces even with no violations — the contract
	// is a static promise to the driver, not gated on the current input
	// (the driver may still encounter violations on its next round).
	for _, want := range []string{"FIX SPEC", "PATCH ALLOWLIST"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt should always state the lint output contract; missing %q in:\n%s", want, got)
		}
	}
}

// TestBuildSpecIterationPrompt_LintOrderingIsStable asserts the helper
// preserves the order it receives violations in — speclint.Run sorts
// deterministically and the prompt must not reshuffle, so the driver's
// edit ordering matches the audit log's ordering.
func TestBuildSpecIterationPrompt_LintOrderingIsStable(t *testing.T) {
	in := SpecIterationPromptInput{
		PRNumber: 1,
		Round:    1,
		SpecMD:   "x",
		LintViolations: []speclint.Violation{
			{Rule: "L01", File: "spec.md", Line: 1, Severity: speclint.SeverityError, Message: "alpha"},
			{Rule: "L02", File: "spec.md", Line: 5, Severity: speclint.SeverityError, Message: "beta"},
			{Rule: "L03", File: "tasks.md", Line: 9, Severity: speclint.SeverityWarning, Message: "gamma"},
		},
	}
	got := BuildSpecIterationPrompt(in)
	aIdx := strings.Index(got, "alpha")
	bIdx := strings.Index(got, "beta")
	gIdx := strings.Index(got, "gamma")
	if !(aIdx < bIdx && bIdx < gIdx) {
		t.Errorf("lint violations should appear in input order alpha < beta < gamma; got positions %d, %d, %d", aIdx, bIdx, gIdx)
	}
}
