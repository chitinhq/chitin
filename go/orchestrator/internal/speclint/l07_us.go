// Package speclint implements the deterministic spec-PR consistency rules
// L01-L07 from spec 115 FR-003. Each rule is a pure function over
// in-memory file contents and returns a slice of Violations — no network,
// no filesystem.
package speclint

import (
	"fmt"
	"regexp"
	"strings"
)

// Violation is the canonical shape every L0N rule returns and the
// spec-lint subcommand (spec 115 T002) serialises to JSON
// ("{rule, file, line, severity, message}").
//
// Each LNN rule file independently declares these types; the integrator
// (spec 115 T002) dedupes against a single canonical declaration. This
// lets each work-unit PR build standalone.
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"` // 1-based
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

const ruleL07 = "L07"

// independentTestMarker is the literal prefix L07 looks for inside each
// user-story section. Spec 115's own spec.md uses this exact marker on
// every US section, and the FR-003 text quotes it verbatim.
const independentTestMarker = "**Independent test:**"

// usHeaderRe matches a level-3 markdown header that names a user story:
// "### US<N>" where N is one or more digits. The trailing `\b` rejects
// "### USers ..." but accepts "### US10 — ..." and "### US1 (P1)".
//
// Anchored at line start; the caller iterates line-by-line so multiline
// is unnecessary.
var usHeaderRe = regexp.MustCompile(`^### US([0-9]+)\b`)

// headingRe matches any ATX-style markdown heading at depth 1-6. L07
// uses it to find the end of a user-story section: the section runs from
// its US header up to (but not including) the next heading at depth ≤ 3
// — either another user story or a higher-level structural break like
// "## Functional requirements".
var headingRe = regexp.MustCompile(`^(#{1,6})\s`)

// L07UserStoryTests checks that every "### US<N>" header in spec.md is
// followed within its section by an "**Independent test:**" paragraph.
//
// Section bounds: a US section starts at its header line and ends at the
// next heading of depth ≤ 3 (the next US header, or a structural ## /
// # heading like "## Functional requirements"), exclusive — or EOF.
// Deeper sub-headers (####+) stay inside the section. This is a strict
// reading of FR-003 / the spec template's intent: each US contains its
// own independent-test paragraph and does not borrow one from a sibling.
//
// The marker check is a literal substring search at the start of any
// non-blank line in the section's body. This is permissive enough to
// allow alternative spellings of the surrounding prose ("**Independent
// test:** Open a spec PR ..." on one line, or "**Independent test:**\n
// long paragraph ..." with the body on the next line) while still
// requiring the canonical bold marker exactly as FR-003 names it.
//
// Returns one error-severity Violation per US section that lacks the
// marker, pointing at the header's line so the operator can navigate
// straight to the offending story. Returns nil when every story has its
// test paragraph (and when the spec has no US sections at all — L07 is
// silent on specs that simply don't use the user-story convention).
func L07UserStoryTests(specPath, content string) []Violation {
	lines := strings.Split(content, "\n")

	// First pass: find every US header line and the boundary (exclusive)
	// where its section ends. Two passes keep the boundary logic separate
	// from the marker search and avoid an inner loop that re-scans for
	// the next heading on each match.
	type usSection struct {
		number   string
		line     int // 1-based, header line
		bodyFrom int // 0-based index into lines, first body line
		bodyTo   int // 0-based index into lines, exclusive
	}
	var sections []usSection
	for i, raw := range lines {
		m := usHeaderRe.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		// bodyTo: scan forward for the next heading of depth ≤ 3.
		bodyTo := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if h := headingRe.FindStringSubmatch(lines[j]); h != nil && len(h[1]) <= 3 {
				bodyTo = j
				break
			}
		}
		sections = append(sections, usSection{
			number:   m[1],
			line:     i + 1,
			bodyFrom: i + 1,
			bodyTo:   bodyTo,
		})
	}

	var violations []Violation
	for _, s := range sections {
		if hasMarker(lines[s.bodyFrom:s.bodyTo]) {
			continue
		}
		violations = append(violations, Violation{
			Rule:     ruleL07,
			File:     specPath,
			Line:     s.line,
			Severity: SeverityError,
			Message:  fmt.Sprintf("user story US%s has no %s paragraph", s.number, independentTestMarker),
		})
	}
	return violations
}

// hasMarker returns true iff any line in body, after trimming leading
// whitespace, begins with the independent-test marker. Trimming leading
// whitespace tolerates blockquote / list indentation; the marker itself
// is checked as an exact prefix so a stray "Independent test:" without
// the surrounding ** does not pass.
func hasMarker(body []string) bool {
	for _, raw := range body {
		if strings.HasPrefix(strings.TrimLeft(raw, " \t>"), independentTestMarker) {
			return true
		}
	}
	return false
}
