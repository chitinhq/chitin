// Package workflows contains the orchestrator's Temporal workflows.
//
// This file implements the spec 115 US1 (FR-006) prompt template for the
// spec-PR iteration loop. It is a pure helper — no I/O, no driver calls,
// no state — so it can be exercised by hermetic tests AND shared between
// the workflow scheduler and the activity that ultimately invokes the
// driver.

package workflows

import (
	"fmt"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/internal/speclint"
)

// SpecIterationPromptInput is the closed shape of the data BuildSpecIterationPrompt
// needs. Caller is responsible for loading spec.md/tasks.md off disk and
// running speclint before invoking this helper — the helper itself does no I/O.
type SpecIterationPromptInput struct {
	// PRNumber is the spec PR being iterated.
	PRNumber int
	// Round is the 1-based iteration round number for this review.
	Round int
	// SpecDirName is the slug under .specify/specs/ (e.g. "115-spec-review-gate").
	// Surfaced in the prompt header so the driver knows which spec is in scope
	// even when its working directory is the repo root.
	SpecDirName string
	// SpecMD is the current contents of <SpecDirName>/spec.md. Always included
	// in full per FR-006 — the spec author needs the whole document to reason
	// about consistency, not just the diff.
	SpecMD string
	// TasksMD is the current contents of <SpecDirName>/tasks.md. Always included
	// in full for the same reason as SpecMD.
	TasksMD string
	// LintViolations are the deterministic spec-lint findings (FR-003) the
	// driver must resolve in this round. Rendered as a distinct section from
	// the Copilot comments so the driver does not confuse the two channels.
	LintViolations []speclint.Violation
	// ReviewBody is the prose body of the Copilot review (may be empty).
	ReviewBody string
	// LineComments are the Copilot review's inline comments (may be empty).
	LineComments []SpecReviewLineComment
}

// SpecReviewLineComment is one inline Copilot review comment in the shape
// the prompt needs. Mirrors activities.reviewLineComment but lives here so
// the workflows package does not have to import activities.
type SpecReviewLineComment struct {
	Path string
	Line int
	Body string
}

// BuildSpecIterationPrompt assembles the re-prompt passed to the spec-author
// driver. Pure function — no I/O, no time, no randomness — so two calls
// with the same input produce byte-identical output. Exported so the
// activity that ultimately invokes the driver AND hermetic tests can share
// the same template.
//
// FR-006 contract surfaced in the prompt body:
//
//  1. Full current spec.md + tasks.md as context (not a diff).
//  2. Linter violations as a structured input distinct from Copilot comments.
//  3. The driver MUST address each linter violation by either fixing the
//     spec/tasks file OR justifying a patch to the linter's allowlist
//     (e.g. `.specify/known-cli-surfaces.txt`). Leaving a violation
//     unaddressed escalates the round as `lint_violation_unresolvable`
//     (FR-010).
//
// As with BuildIterationPrompt in the activities package, this prompt does
// NOT ask the driver to write a commit message or run tests — the
// orchestrator authors the fixup commit itself.
func BuildSpecIterationPrompt(in SpecIterationPromptInput) string {
	var b strings.Builder

	fmt.Fprintf(&b,
		"You are addressing review feedback on SPEC PR #%d (round %d) for spec %q.\n\n",
		in.PRNumber, in.Round, in.SpecDirName)

	b.WriteString("You have a fresh worktree on the PR's branch.\n\n")

	b.WriteString("OPERATING RULES:\n")
	b.WriteString("  - The CURRENT spec.md and tasks.md are included below in full. Reason\n")
	b.WriteString("    about consistency against the whole document, not just the diff.\n")
	b.WriteString("  - Two distinct streams of feedback follow: deterministic LINTER\n")
	b.WriteString("    VIOLATIONS (mechanical checks from `chitin-orchestrator spec-lint`)\n")
	b.WriteString("    and COPILOT REVIEW COMMENTS (LLM review). They are NOT redundant —\n")
	b.WriteString("    treat them independently.\n")
	b.WriteString("  - For each LINTER VIOLATION, you MUST do exactly one of:\n")
	b.WriteString("      (a) Fix the spec/tasks file so the next spec-lint run no longer\n")
	b.WriteString("          flags it; OR\n")
	b.WriteString("      (b) Patch the linter allowlist (for L05, edit\n")
	b.WriteString("          `.specify/known-cli-surfaces.txt`; for L07-style additions,\n")
	b.WriteString("          edit the relevant allowlist file under `.specify/`) AND leave\n")
	b.WriteString("          a one-line rationale in the spec body for why the allowlist\n")
	b.WriteString("          patch is correct, so future readers can audit it.\n")
	b.WriteString("    Leaving a linter violation unaddressed fails the round and escalates\n")
	b.WriteString("    to the operator as `lint_violation_unresolvable` (spec 115 FR-010).\n")
	b.WriteString("  - For each COPILOT REVIEW COMMENT, apply the smallest reasonable fix\n")
	b.WriteString("    OR leave the file unchanged (recorded as an intentional decline).\n")
	b.WriteString("  - Do not refactor unrelated sections. Do not change task scope.\n\n")

	b.WriteString("=== CURRENT spec.md ===\n")
	b.WriteString(ensureTrailingNewline(in.SpecMD))
	b.WriteString("=== END spec.md ===\n\n")

	b.WriteString("=== CURRENT tasks.md ===\n")
	b.WriteString(ensureTrailingNewline(in.TasksMD))
	b.WriteString("=== END tasks.md ===\n\n")

	writeLintViolations(&b, in.LintViolations)
	writeCopilotReview(&b, in.ReviewBody, in.LineComments)

	b.WriteString("After making changes, exit. Do not run tests, do not write commit\n")
	b.WriteString("messages. The orchestrator will commit + push your changes as a single\n")
	b.WriteString("fixup commit.\n")

	return b.String()
}

// writeLintViolations renders the structured linter section. An empty list
// produces a "none" line rather than being omitted, so the driver always
// sees both channel headers and cannot conflate "no violations" with
// "violations forgotten by the orchestrator".
func writeLintViolations(b *strings.Builder, vs []speclint.Violation) {
	if len(vs) == 0 {
		b.WriteString("LINTER VIOLATIONS: none.\n\n")
		return
	}
	fmt.Fprintf(b, "LINTER VIOLATIONS (%d):\n", len(vs))
	for i, v := range vs {
		fmt.Fprintf(b, "  [%d] %s %s:%d (%s)\n      %s\n",
			i+1, v.Rule, v.File, v.Line, v.Severity, strings.TrimSpace(v.Message))
	}
	b.WriteString("\n")
}

// writeCopilotReview renders the Copilot review section. As with linter
// violations, empty bodies and empty comment lists still produce explicit
// "none" lines so the prompt shape is constant across rounds.
func writeCopilotReview(b *strings.Builder, body string, comments []SpecReviewLineComment) {
	body = strings.TrimSpace(body)
	if body == "" {
		b.WriteString("COPILOT REVIEW BODY: none.\n")
	} else {
		b.WriteString("COPILOT REVIEW BODY:\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if len(comments) == 0 {
		b.WriteString("COPILOT LINE COMMENTS: none.\n\n")
		return
	}
	fmt.Fprintf(b, "COPILOT LINE COMMENTS (%d):\n", len(comments))
	for i, c := range comments {
		fmt.Fprintf(b, "  [%d] %s:%d\n      %s\n",
			i+1, c.Path, c.Line, strings.TrimSpace(c.Body))
	}
	b.WriteString("\n")
}

// ensureTrailingNewline guarantees the embedded markdown section ends with
// exactly one newline before the closing fence, so a spec file that ends
// without a final newline doesn't merge with the fence line.
func ensureTrailingNewline(s string) string {
	if s == "" {
		return "\n"
	}
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
