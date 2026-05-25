package workflows

import (
	"fmt"
	"strings"
)

// SpecLintViolation is one finding from `chitin-orchestrator spec-lint`
// surfaced to the spec-iteration driver as structured input distinct from
// Copilot review comments (spec 115 FR-006).
type SpecLintViolation struct {
	// Rule is the linter rule id (e.g. "L05").
	Rule string `json:"rule"`
	// File is the spec-relative path the violation references
	// (e.g. "spec.md" or "tasks.md").
	File string `json:"file"`
	// Line is the 1-based line number of the violation; 0 means
	// "applies to the whole file".
	Line int `json:"line"`
	// Severity is "warning" or "error". Only errors gate iteration
	// (spec 115 edge cases section); warnings are informational but the
	// driver should still address them when reasonable.
	Severity string `json:"severity"`
	// Message is the human-readable description of the violation.
	Message string `json:"message"`
}

// SpecReviewLineComment is one inline Copilot review comment on a spec PR.
// Mirrors the activities-package reviewLineComment shape but lives in the
// workflows package so this prompt builder stays pure and self-contained.
type SpecReviewLineComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

// SpecIterationPromptInput is the closed input to BuildSpecIterationPrompt.
// All fields are plain values so the helper is pure and trivial to test.
type SpecIterationPromptInput struct {
	// PRNumber is the spec pull request being iterated.
	PRNumber int
	// Round is the 1-based iteration round number for this review.
	Round int
	// SpecDir is the spec-relative directory (e.g.
	// ".specify/specs/115-spec-review-gate") so the driver can locate the
	// files it is editing.
	SpecDir string
	// SpecMD is the FULL current contents of spec.md (not a diff — spec
	// authors need full context to reason about consistency, per FR-006).
	SpecMD string
	// TasksMD is the FULL current contents of tasks.md.
	TasksMD string
	// LintViolations is the structured linter output (FR-004), surfaced
	// to the driver as a distinct section from Copilot comments.
	LintViolations []SpecLintViolation
	// ReviewBody is the top-level body of the Copilot review (may be
	// empty — some reviews are inline comments only).
	ReviewBody string
	// LineComments are the inline Copilot review comments.
	LineComments []SpecReviewLineComment
}

// BuildSpecIterationPrompt assembles the re-prompt passed to the spec-tuned
// driver in SpecIterationWorkflow (spec 115 US1, FR-006). Pure function — no
// IO, deterministic, side-effect-free — so tests can assert on the rendered
// shape and the workflow stays replay-safe.
//
// The prompt differs from BuildIterationPrompt (spec 113) in three ways:
//
//  1. Includes FULL spec.md + tasks.md as context (not a diff). A spec
//     author needs the whole spec to reason about consistency between
//     sections; a code reviewer can usually work from a diff.
//  2. Surfaces linter violations as a structured section, clearly
//     separated from Copilot's review comments. The driver should treat
//     linter findings differently — every violation MUST be addressed,
//     either by fixing the spec or by justifying an allowlist patch.
//  3. Declares the required output envelope: for each lint violation,
//     the driver either edits the spec to remove the violation OR adds
//     the offending surface to the appropriate allowlist (e.g.
//     `.specify/known-cli-surfaces.txt` for L05) with a one-line
//     justification commenting WHY this spec legitimately introduces it.
//
// The orchestrator authors the commit message itself (see the iteration
// activity), so this prompt does NOT ask the driver to write commit text —
// only to make file changes.
func BuildSpecIterationPrompt(in SpecIterationPromptInput) string {
	var b strings.Builder

	hasLint := len(in.LintViolations) > 0
	hasReview := strings.TrimSpace(in.ReviewBody) != "" || len(in.LineComments) > 0

	fmt.Fprintf(&b,
		"You are addressing review feedback on spec PR #%d (round %d).\n\n",
		in.PRNumber, in.Round)
	b.WriteString("You have a fresh worktree on the spec PR's branch. ")
	b.WriteString("This PR modifies a spec under .specify/specs/. ")
	if in.SpecDir != "" {
		fmt.Fprintf(&b, "The spec directory is %s. ", in.SpecDir)
	}
	switch {
	case hasLint && hasReview:
		b.WriteString("Two kinds of feedback are below: deterministic linter violations ")
		b.WriteString("from chitin-orchestrator spec-lint, and a Copilot review with ")
		b.WriteString("free-form comments. Treat them differently — see the OUTPUT ENVELOPE ")
		b.WriteString("section at the bottom.\n\n")
	case hasLint:
		b.WriteString("The deterministic linter (chitin-orchestrator spec-lint) flagged ")
		b.WriteString("violations below. See the OUTPUT ENVELOPE section at the bottom for ")
		b.WriteString("how to resolve each one.\n\n")
	case hasReview:
		b.WriteString("A Copilot review's comments are below. See the OUTPUT ENVELOPE ")
		b.WriteString("section at the bottom for how to address each one.\n\n")
	default:
		b.WriteString("No linter violations or review comments were attached. ")
		b.WriteString("Exit without changes.\n\n")
	}

	// Full spec.md and tasks.md as context — FR-006 requirement.
	b.WriteString("==================== CURRENT spec.md ====================\n")
	b.WriteString(in.SpecMD)
	if !strings.HasSuffix(in.SpecMD, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("==================== END spec.md ========================\n\n")

	b.WriteString("==================== CURRENT tasks.md ===================\n")
	b.WriteString(in.TasksMD)
	if !strings.HasSuffix(in.TasksMD, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("==================== END tasks.md =======================\n\n")

	// Linter violations — structured, distinct from Copilot comments.
	if len(in.LintViolations) > 0 {
		b.WriteString("LINTER VIOLATIONS (deterministic checks from spec-lint):\n")
		for i, v := range in.LintViolations {
			loc := v.File
			if v.Line > 0 {
				loc = fmt.Sprintf("%s:%d", v.File, v.Line)
			}
			fmt.Fprintf(&b, "  [%d] %s [%s] %s\n      %s\n",
				i+1, v.Rule, v.Severity, loc, strings.TrimSpace(v.Message))
		}
		b.WriteString("\n")
	}

	// Copilot review body + line comments — same shape as the spec-113
	// code-iteration prompt so a spec-author driver familiar with the
	// code-iteration template recognises the section.
	if strings.TrimSpace(in.ReviewBody) != "" {
		fmt.Fprintf(&b, "REVIEW BODY:\n%s\n\n", strings.TrimSpace(in.ReviewBody))
	}
	if len(in.LineComments) > 0 {
		b.WriteString("LINE COMMENTS:\n")
		for i, c := range in.LineComments {
			fmt.Fprintf(&b, "  [%d] %s:%d\n      %s\n",
				i+1, c.Path, c.Line, strings.TrimSpace(c.Body))
		}
		b.WriteString("\n")
	}

	// Required output envelope — FR-006: every lint violation must be
	// resolved by either fixing the spec OR patching the allowlist.
	if hasLint || hasReview {
		b.WriteString("OUTPUT ENVELOPE (required):\n")
	}
	if hasLint {
		b.WriteString("  For each LINTER VIOLATION above, you MUST take exactly one of:\n")
		b.WriteString("    (a) Edit spec.md or tasks.md to remove the violation.\n")
		b.WriteString("    (b) Patch the linter allowlist (e.g. .specify/known-cli-surfaces.txt\n")
		b.WriteString("        for L05, .specify/judgement-phrases.txt for judgement matches)\n")
		b.WriteString("        AND add a one-line comment in the spec body or allowlist file\n")
		b.WriteString("        justifying WHY this spec legitimately introduces the surface.\n")
		b.WriteString("  Leaving a lint violation unaddressed will cause the iteration round\n")
		b.WriteString("  to escalate with reason=lint_violation_unresolvable (spec 115 FR-010).\n\n")
	}
	if hasReview {
		b.WriteString("  For each REVIEW LINE COMMENT above, EITHER apply the smallest reasonable\n")
		b.WriteString("  fix OR leave the file unchanged (which is recorded as an intentional\n")
		b.WriteString("  decline). Do not refactor unrelated sections. Do not change spec scope.\n\n")
	}

	b.WriteString("After making changes, exit. Do not run tests, do not write commit messages. ")
	b.WriteString("The orchestrator will commit + push your changes as a single fixup commit.\n")
	return b.String()
}
