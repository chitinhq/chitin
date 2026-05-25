// Package speclint hosts deterministic spec-PR consistency rules (spec 115
// FR-003). This file ships rule L03 (task-to-FR coverage).
//
// Self-contained for the work-unit: this file declares the Violation and
// Severity types the rule emits. Other rule work-units (T002 lint.go, T003
// L01, etc.) declare the same shapes in their own files; the spec 115
// gap-fill PR reconciles the duplicates against the single canonical
// declaration in lint.go. Keeping the types here lets `go build` and `go
// test` on this work-unit PR pass standalone.
package speclint

import (
	"regexp"
	"sort"
	"strings"
)

// Severity classifies a violation's gating effect. Mirrors the canonical
// shape in T002's lint.go so the gap-fill collapse is a pure delete-here,
// keep-there operation.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Violation is one finding from one rule at one source line. Field shape
// matches FR-003's JSON envelope so the spec-lint subcommand can marshal a
// []Violation directly.
type Violation struct {
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// specFRDeclRegex matches the canonical declaration form `**FR-NNN**` in
// spec.md. Only the bold form declares — plain prose like "(spec 113
// FR-001)" intentionally does NOT count, so a spec that cites another
// spec's FRs in its prose doesn't accidentally claim them as local.
//
// taskFRRefRegex matches a bare `FR-NNN` reference anywhere in tasks.md.
//
// crossSpecFRRefRegex matches `spec <digits> FR-NNN` so cross-spec
// citations in tasks.md (e.g. "extends spec 113 FR-010 behavior") can be
// stripped before scanning for local task FRs — otherwise FR-010 would be
// reported as an unknown reference against the current spec.
var (
	specFRDeclRegex     = regexp.MustCompile(`\*\*(FR-\d{3,})\*\*`)
	taskFRRefRegex      = regexp.MustCompile(`FR-\d{3,}`)
	crossSpecFRRefRegex = regexp.MustCompile(`spec \d+ FR-\d{3,}`)
)

// L03TaskFRCoverage asserts bidirectional coverage between spec.md FR
// declarations and tasks.md FR references:
//
//  1. Every `FR-NNN` referenced in tasks.md is declared as `**FR-NNN**` in
//     spec.md (a task pointing at no requirement is unimplementable).
//  2. Every `**FR-NNN**` declared in spec.md is referenced by at least one
//     line in tasks.md (an FR with no task is orphaned — nothing builds it).
//
// The function is pure: no IO, no shared state. Output is sorted by
// (file, line, message) so repeated runs produce byte-identical results —
// a precondition for FR-004's per-(rule, file, line) dedup of PR review
// comments.
func L03TaskFRCoverage(specMD, tasksMD string) []Violation {
	specFRs := extractSpecFRs(specMD)
	taskRefs := extractTaskFRs(tasksMD)

	var violations []Violation

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
			Severity: SeverityError,
			Message:  "tasks.md references " + fr + " but spec.md declares no such functional requirement",
		})
	}

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
			Severity: SeverityError,
			Message:  "spec.md declares " + fr + " but no task in tasks.md references it",
		})
	}

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

// extractSpecFRs scans spec.md for `**FR-NNN**` declarations and records
// the 1-indexed line number of the FIRST occurrence of each FR. Repeated
// declarations are tolerated (legal markdown) but the earliest line wins
// for violation reporting.
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

// extractTaskFRs scans tasks.md for bare `FR-NNN` references and records
// the 1-indexed line number of the first occurrence of each FR. Cross-spec
// citations (`spec <digits> FR-NNN`) are stripped first so they do not
// produce false "unknown FR" violations against the current spec.
func extractTaskFRs(tasksMD string) map[string]int {
	out := map[string]int{}
	for i, line := range strings.Split(tasksMD, "\n") {
		stripped := crossSpecFRRefRegex.ReplaceAllString(line, "")
		for _, m := range taskFRRefRegex.FindAllString(stripped, -1) {
			if _, seen := out[m]; !seen {
				out[m] = i + 1
			}
		}
	}
	return out
}
