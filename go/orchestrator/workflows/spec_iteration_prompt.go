package workflows

import (
	"fmt"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/internal/speclint"
)

// SpecIterationPromptInput is the closed input shape for
// BuildSpecIterationPrompt (spec 115 T012 / FR-006). One PR review on a
// spec PR; the helper composes a self-contained re-prompt for the
// spec-author driver to act on.
//
// The shape intentionally keeps the linter's violations distinct from
// Copilot's comments — FR-006 requires that distinction in the prompt
// (the driver treats them with different output contracts: lint
// violations MUST be addressed either by editing the spec OR by patching
// an allowlist; Copilot comments are the same fix-or-decline contract
// as code PRs).
type SpecIterationPromptInput struct {
	// PRNumber is the spec pull request being iterated.
	PRNumber int
	// Round is the 1-based iteration round (mirrors PRIterationInput.Round).
	Round int
	// SpecDirPath is the repo-relative path of the spec directory the PR
	// touches, e.g. ".specify/specs/115-spec-review-gate". Surfaced in the
	// prompt header so the driver names the right files when editing.
	SpecDirPath string
	// SpecMD is the FULL current contents of spec.md (FR-006: not a diff —
	// the driver needs full context to reason about cross-FR consistency).
	SpecMD string
	// TasksMD is the FULL current contents of tasks.md.
	TasksMD string
	// LintViolations are the deterministic spec-lint findings (FR-003 /
	// FR-004). Order is preserved — speclint.Run already sorts
	// deterministically by (rule, file, line, message).
	LintViolations []speclint.Violation
	// ReviewBody is the Copilot review's top-level body (may be empty).
	ReviewBody string
	// LineComments are the Copilot review's inline comments.
	LineComments []SpecCopilotComment
}

// SpecCopilotComment is one inline review comment from Copilot. Shape
// matches reviewLineComment in activities/pr_iteration.go but is
// redeclared here so the workflows package does not depend on
// activities-package internals.
type SpecCopilotComment struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

// BuildSpecIterationPrompt assembles the re-prompt passed to the
// spec-author driver. Pure function — no IO, no globals — so tests can
// assert on the exact output and the workflow can call it from any
// context (including Temporal workflow code, where determinism matters).
//
// The output contract per FR-006:
//
//  1. The full spec.md + tasks.md are inlined (not a diff) so the
//     driver can reason about consistency across the whole spec when
//     editing one part of it.
//  2. Lint violations are presented as a separate, numbered section,
//     distinct from Copilot comments. The header on each section names
//     the required response contract for that input class.
//  3. For EACH lint violation the driver MUST either (a) edit the spec
//     so the violation no longer holds, OR (b) patch the relevant
//     linter allowlist (e.g. .specify/known-cli-surfaces.txt for L05)
//     in the same commit and name the patch in its post-action
//     summary. Unaddressed violations escalate as
//     `lint_violation_unresolvable` (FR-010).
//
// The orchestrator authors the commit message itself (mirrors the code-PR
// loop in activities.BuildIterationPrompt), so this prompt does NOT ask
// the driver to write one — only to make file changes and exit.
func BuildSpecIterationPrompt(in SpecIterationPromptInput) string {
	var b strings.Builder

	fmt.Fprintf(&b,
		"You are addressing review feedback on spec PR #%d (round %d).\n",
		in.PRNumber, in.Round)
	if in.SpecDirPath != "" {
		fmt.Fprintf(&b, "Spec directory: %s\n", in.SpecDirPath)
	}
	b.WriteString("\n")

	b.WriteString("You have a fresh worktree on the PR's branch. ")
	b.WriteString("Two distinct input classes follow:\n")
	b.WriteString("  1. LINT VIOLATIONS — deterministic spec-lint findings (spec 115 FR-003).\n")
	b.WriteString("  2. COPILOT REVIEW COMMENTS — judgement-overlay feedback.\n\n")

	b.WriteString("OUTPUT CONTRACT:\n")
	b.WriteString("  For each LINT VIOLATION you MUST do ONE of:\n")
	b.WriteString("    (a) FIX SPEC — edit spec.md or tasks.md so the violation no longer holds.\n")
	b.WriteString("    (b) PATCH ALLOWLIST — if the violation is legitimate (e.g., the spec\n")
	b.WriteString("        introduces a new CLI subcommand), edit the relevant linter allowlist\n")
	b.WriteString("        (typically .specify/known-cli-surfaces.txt) in the same commit so a\n")
	b.WriteString("        re-run is clean. Briefly note the justification at the top of the\n")
	b.WriteString("        file you patch.\n")
	b.WriteString("  An unaddressed lint violation escalates as `lint_violation_unresolvable`\n")
	b.WriteString("  to the operator — fix it or justify the patch.\n\n")
	b.WriteString("  For each COPILOT COMMENT, EITHER apply the smallest reasonable fix OR\n")
	b.WriteString("  leave the file unchanged (recorded as an intentional decline). Do not\n")
	b.WriteString("  refactor unrelated code. Do not change task scope.\n\n")

	if strings.TrimSpace(in.SpecMD) != "" {
		fmt.Fprintf(&b, "CURRENT SPEC (%s/spec.md):\n", trimOrSpecDir(in.SpecDirPath))
		b.WriteString(in.SpecMD)
		if !strings.HasSuffix(in.SpecMD, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if strings.TrimSpace(in.TasksMD) != "" {
		fmt.Fprintf(&b, "CURRENT TASKS (%s/tasks.md):\n", trimOrSpecDir(in.SpecDirPath))
		b.WriteString(in.TasksMD)
		if !strings.HasSuffix(in.TasksMD, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(in.LintViolations) > 0 {
		b.WriteString("LINT VIOLATIONS:\n")
		for i, v := range in.LintViolations {
			fmt.Fprintf(&b, "  [%d] %s %s:%d (%s) %s\n",
				i+1, v.Rule, v.File, v.Line, v.Severity, strings.TrimSpace(v.Message))
		}
		b.WriteString("\n")
	}

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

	b.WriteString("After making changes, exit. Do not run tests, do not write commit messages. ")
	b.WriteString("The orchestrator will commit + push your changes as a single fixup commit.\n")
	return b.String()
}

// trimOrSpecDir returns the SpecDirPath without a trailing slash, or a
// sentinel "<spec dir>" placeholder when the path was not supplied. Keeps
// the inlined header readable without forcing the caller to normalize.
func trimOrSpecDir(p string) string {
	t := strings.TrimRight(p, "/")
	if t == "" {
		return "<spec dir>"
	}
	return t
}
