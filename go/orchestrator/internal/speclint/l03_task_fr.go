// Package speclint hosts deterministic spec-PR consistency rules (spec 115 FR-003).
//
// Each rule is a pure function: (specMD, tasksMD string) -> []Violation. No
// network, no filesystem. The orchestrator subcommand reads files, dispatches
// to the rules, and serialises violations as the JSON envelope defined by
// spec 115 T002.
package speclint

import (
	"regexp"
	"sort"
	"strings"
)

// Violation is the shared output contract for every L0N rule (spec 115 FR-003).
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Spec FR declarations are written as `- **FR-NNN** ...` markdown bold;
// task references are bare `FR-NNN`. The capture groups normalise both
// forms back to the canonical `FR-NNN` key.
var (
	specFRDeclRegex = regexp.MustCompile(`\*\*(FR-\d{3,})\*\*`)
	taskFRRefRegex  = regexp.MustCompile(`FR-\d{3,}`)
)

// L03TaskFRCoverage asserts bidirectional coverage between spec.md FRs and
// tasks.md FR references:
//
//  1. Every `FR-NNN` referenced in tasks.md is declared as `**FR-NNN**` in
//     spec.md (otherwise the task points at nothing).
//  2. Every `**FR-NNN**` declared in spec.md is referenced by at least one
//     line in tasks.md (otherwise the FR is orphaned with no implementation).
//
// Violations are returned sorted by (file, line) for deterministic output.
func L03TaskFRCoverage(specMD, tasksMD string) []Violation {
	specFRs := extractSpecFRs(specMD)
	taskRefs := extractTaskFRs(tasksMD)

	var violations []Violation

	// Direction 1: task references that aren't declared in spec.md.
	unknownTaskFRs := make([]string, 0, len(taskRefs))
	for fr := range taskRefs {
		if _, ok := specFRs[fr]; !ok {
			unknownTaskFRs = append(unknownTaskFRs, fr)
		}
	}
	sort.Strings(unknownTaskFRs)
	for _, fr := range unknownTaskFRs {
		violations = append(violations, Violation{
			Rule:     "L03",
			File:     "tasks.md",
			Line:     taskRefs[fr],
			Severity: "error",
			Message:  "tasks.md references " + fr + " but spec.md declares no such functional requirement",
		})
	}

	// Direction 2: spec FRs with no task reference.
	orphanSpecFRs := make([]string, 0, len(specFRs))
	for fr := range specFRs {
		if _, ok := taskRefs[fr]; !ok {
			orphanSpecFRs = append(orphanSpecFRs, fr)
		}
	}
	sort.Strings(orphanSpecFRs)
	for _, fr := range orphanSpecFRs {
		violations = append(violations, Violation{
			Rule:     "L03",
			File:     "spec.md",
			Line:     specFRs[fr],
			Severity: "error",
			Message:  "spec.md declares " + fr + " but no task in tasks.md references it",
		})
	}

	// Final ordering: (file, line, message). file ASC so spec.md (orphans)
	// follows tasks.md (unknowns) alphabetically, which matches the order
	// the subcommand will report.
	sort.SliceStable(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		if violations[i].Line != violations[j].Line {
			return violations[i].Line < violations[j].Line
		}
		return violations[i].Message < violations[j].Message
	})
	return violations
}

// extractSpecFRs scans spec.md for `**FR-NNN**` declarations and records the
// 1-indexed line number of the first occurrence of each FR.
func extractSpecFRs(specMD string) map[string]int {
	out := map[string]int{}
	for i, line := range strings.Split(specMD, "\n") {
		for _, m := range specFRDeclRegex.FindAllStringSubmatch(line, -1) {
			fr := m[1]
			if _, seen := out[fr]; !seen {
				out[fr] = i + 1
			}
		}
	}
	return out
}

// extractTaskFRs scans tasks.md for bare `FR-NNN` references and records the
// 1-indexed line number of the first occurrence of each FR.
func extractTaskFRs(tasksMD string) map[string]int {
	out := map[string]int{}
	for i, line := range strings.Split(tasksMD, "\n") {
		for _, m := range taskFRRefRegex.FindAllString(line, -1) {
			if _, seen := out[m]; !seen {
				out[m] = i + 1
			}
		}
	}
	return out
}
