package speclint

import (
	"regexp"
	"strings"
)

const ruleL07 = "L07"

// l07USHeaderPattern matches a user-story heading: `### US1`, `### US12`,
// etc. The numeric suffix is captured so the violation message names the
// story. Tolerates surrounding whitespace and an optional " — Title" tail.
// Multi-line so ^ anchors per-line.
var l07USHeaderPattern = regexp.MustCompile(`(?m)^###[ \t]+(US\d+)\b`)

// l07IndependentTestPattern matches the `**Independent test:**` marker
// that spec 115 FR-003 L07 requires within each US section. The text is
// case-insensitive (some specs write `**Independent Test:**`); the
// trailing colon is required so the marker can't be confused with prose
// that mentions "independent test" in passing.
var l07IndependentTestPattern = regexp.MustCompile(`(?i)\*\*independent test:\*\*`)

// l07NextHeaderPattern bounds a section: the next `### US` header OR the
// next `## ` (H2) header marks the end of the current US section. ^
// anchored per-line.
var l07NextHeaderPattern = regexp.MustCompile(`(?m)^(?:###[ \t]+US\d+|##[ \t])`)

// CheckL07 enforces that every `### USN` user-story header in specContent
// has a `**Independent test:**` paragraph inside its section.
//
// Invariant: for each US header, scan from the line after that header
// until the next US header OR the next H2 header (whichever comes first)
// for an `**Independent test:**` marker. Missing marker -> one
// SeverityError Violation naming the user story.
//
// Pure function — no IO. specPath is reported verbatim in Violation.File.
// Returns nil when specContent has no US headers at all (specs without
// user-story sections are out of L07's scope).
//
// Boundary contracts:
//   - No US headers in specContent -> empty result.
//   - US section has the marker -> no violation for that US.
//   - US section runs to end of file without another header -> the
//     scan window extends to EOF.
//   - Multiple US sections, mixed pass/fail -> violations only for the
//     missing ones, in document order.
func CheckL07(specPath, specContent string) []Violation {
	if specContent == "" {
		return nil
	}
	headers := l07USHeaderPattern.FindAllStringSubmatchIndex(specContent, -1)
	if len(headers) == 0 {
		return nil
	}

	var out []Violation
	for i, h := range headers {
		// h[0]:h[1] = full match; h[2]:h[3] = USN capture.
		usName := specContent[h[2]:h[3]]
		sectionStart := h[1] // position just after the header line's match
		sectionEnd := len(specContent)
		// The next bounding header could be any of the remaining USN
		// matches OR the first H2 after the current header. Search the
		// substring after sectionStart for the nearest bound.
		rest := specContent[sectionStart:]
		if loc := l07NextHeaderPattern.FindStringIndex(rest); loc != nil {
			sectionEnd = sectionStart + loc[0]
		}
		// In the unusual case where the next match in `headers` is
		// actually closer than the H2 scan (it shouldn't be, since the
		// pattern catches both), prefer the earlier of the two so we
		// don't accidentally scan past the next US.
		if i+1 < len(headers) {
			nextUSStart := headers[i+1][0]
			if nextUSStart < sectionEnd {
				sectionEnd = nextUSStart
			}
		}
		section := specContent[sectionStart:sectionEnd]
		if l07IndependentTestPattern.MatchString(section) {
			continue
		}
		out = append(out, Violation{
			Rule:     ruleL07,
			File:     specPath,
			Line:     lineNumberAt(specContent, h[0]) + 1,
			Severity: SeverityError,
			Message:  "user story " + strings.ToUpper(usName) + " missing `**Independent test:**` paragraph (spec 115 L07)",
		})
	}
	return out
}
