// schedule_whole_spec.go — spec 119 whole-spec dispatch helper.
//
// When `chitin-orchestrator schedule <spec-ref>` runs in --whole-spec
// mode (the new default), this file replaces the per-task DAG produced
// by speckit.CompileSpec with a SINGLE-NODE DAG whose one work unit
// carries the entire spec as its instruction payload. The scheduler
// workflow then dispatches that one work unit to a T4 driver that
// declares CapSpecImplement (claudecode opus-4.7 / codex gpt-5.x-codex).
//
// Rationale (spec 119 Why): per-task dispatch was the right shape for
// T1-T2 drivers; for T4 it fragments coherent work and produces the
// cross-task coherence failures the May 25 spec 114 + 115 re-dispatches
// surfaced. Whole-spec dispatch keeps the multi-agent thesis where it
// earns its keep (dialectic review, cross-project orchestration) and
// stops abusing it for within-spec implementation.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/dag"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// buildWholeSpecDAG replaces the per-task DAG with a single-node DAG
// per spec 119 FR-002. The single node:
//   - Has ID `wu-<spec-ref>-whole` (stable across re-dispatches of the
//     same spec; deterministic for Temporal dedup if reused later)
//   - Carries Capability=CapSpecImplement so the scheduler routes it
//     to a T4 driver
//   - Carries the full spec.md + tasks.md + plan.md text in Description
//     (the Node field the scheduler hands to the driver as the prompt;
//     keeping the convention consistent with per-task nodes)
//   - Sets WorktreeRequired=true so the activity mints a dedicated
//     worktree the driver edits in
//
// Returns the new DAG (one node, zero edges) plus the count of unchecked
// tasks for the scheduler_started telemetry. specDir is the absolute
// path to .specify/specs/<spec-ref>/ (the resolution.SpecDir).
func buildWholeSpecDAG(specRef, specDir, targetRepo, baseRef string) (*dag.DAG, int, error) {
	prompt, taskCount, err := renderWholeSpecPrompt(specRef, specDir)
	if err != nil {
		return nil, 0, err
	}
	node := dag.Node{
		ID:               wholeSpecNodeID(specRef),
		SpecRef:          specRef,
		TaskRef:          "", // FR-002: whole-spec is not task-scoped
		Kind:             dag.NodeKindAgent,
		Capability:       string(driver.CapSpecImplement),
		Description:      prompt,
		Priority:         0,
		TargetRepo:       targetRepo,
		BaseRef:          baseRef,
		WorktreeRequired: true,
	}
	d := dag.New()
	if err := d.AddNode(node); err != nil {
		return nil, 0, fmt.Errorf("whole-spec AddNode: %w", err)
	}
	return d, taskCount, nil
}

// wholeSpecNodeID returns the stable single-node ID for a spec ref.
// Format: `wu-<spec-ref>-whole`. The trailing `-whole` distinguishes
// the node from per-task IDs (`wu-<spec-ref>-T001`, etc.) so chain
// readers can grep either shape.
func wholeSpecNodeID(specRef string) string {
	return "wu-" + specRef + "-whole"
}

// renderWholeSpecPrompt reads spec.md + tasks.md + plan.md (if present)
// from specDir and assembles the whole-spec invocation prompt per
// FR-003. Returns the prompt string and the count of unchecked tasks
// (for the scheduler_started telemetry).
//
// The prompt's shape is deliberately simple: the driver gets the full
// spec text and is told to deliver every task. No summarization, no
// truncation — driver MUST see the authoritative source.
//
// Missing plan.md is tolerated (the section is omitted, no error).
// Missing spec.md or tasks.md is a fatal config error (the spec can't
// be implemented if either is absent).
func renderWholeSpecPrompt(specRef, specDir string) (string, int, error) {
	specMD, err := os.ReadFile(filepath.Join(specDir, "spec.md"))
	if err != nil {
		return "", 0, fmt.Errorf("read spec.md for %s: %w", specRef, err)
	}
	tasksMD, err := os.ReadFile(filepath.Join(specDir, "tasks.md"))
	if err != nil {
		return "", 0, fmt.Errorf("read tasks.md for %s: %w", specRef, err)
	}
	// plan.md is best-effort — not every spec has one (especially older
	// or smaller specs). Missing-file is fine; any other read error is
	// fatal so the operator notices a permission / filesystem fault.
	var planMD []byte
	planPath := filepath.Join(specDir, "plan.md")
	if _, statErr := os.Stat(planPath); statErr == nil {
		planMD, err = os.ReadFile(planPath)
		if err != nil {
			return "", 0, fmt.Errorf("read plan.md for %s: %w", specRef, err)
		}
	} else if !os.IsNotExist(statErr) {
		return "", 0, fmt.Errorf("stat plan.md for %s: %w", specRef, statErr)
	}

	unchecked := extractUncheckedTaskIDs(string(tasksMD))
	taskCount := len(unchecked)

	var b strings.Builder
	fmt.Fprintf(&b, "You are implementing spec %s in a SINGLE coherent invocation.\n\n", specRef)
	fmt.Fprintf(&b, "Your job: deliver every unchecked task in tasks.md as one cohesive change set, "+
		"open ONE PR, and update tasks.md to mark each completed task `[x]`. Spec 119's whole-spec "+
		"dispatch trusts you with the full context so cross-task coherence (shared types, helpers, "+
		"file layout) emerges naturally — NOT fragmented across N driver invocations.\n\n")
	fmt.Fprintf(&b, "UNCHECKED TASKS (%d):\n", taskCount)
	for _, id := range unchecked {
		fmt.Fprintf(&b, "  - %s\n", id)
	}
	b.WriteString("\n---\n\n# spec.md\n\n")
	b.Write(specMD)
	b.WriteString("\n\n---\n\n# tasks.md\n\n")
	b.Write(tasksMD)
	if len(planMD) > 0 {
		b.WriteString("\n\n---\n\n# plan.md\n\n")
		b.Write(planMD)
	}
	b.WriteString("\n\n---\n\n")
	b.WriteString("DELIVERABLE CHECKLIST:\n")
	b.WriteString("  1. Implement every unchecked task above.\n")
	b.WriteString("  2. Update tasks.md to mark each completed task `[x]`.\n")
	b.WriteString("  3. Run `go test ./...` (or the language's equivalent) and ensure it passes.\n")
	b.WriteString("  4. Commit + push to a feature branch and open ONE PR titled\n")
	b.WriteString("     `feat(" + specRef + "): <one-line summary>`.\n")
	b.WriteString("  5. The PR description should list every task you completed, mention any\n")
	b.WriteString("     deferred items with a follow-up justification.\n")
	return b.String(), taskCount, nil
}

// uncheckedTaskPattern matches `- [ ] T000` style task lines that the
// spec-kit tasks.md format uses. Captures the task id (T followed by
// digits). The pattern is anchored to the line start (after optional
// whitespace) so a "completed" task that mentions another task id in
// its prose body doesn't match.
var uncheckedTaskPattern = regexp.MustCompile(`(?m)^- \[ \] (T\d+)\b`)

// extractUncheckedTaskIDs returns the ordered list of task ids whose
// checkboxes are still `[ ]` in tasksMD. Used by the prompt and by the
// scheduler_started telemetry so operators see how many tasks the
// whole-spec invocation is being asked to deliver.
func extractUncheckedTaskIDs(tasksMD string) []string {
	matches := uncheckedTaskPattern.FindAllStringSubmatch(tasksMD, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}
