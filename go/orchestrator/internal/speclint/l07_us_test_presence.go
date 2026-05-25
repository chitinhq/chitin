package speclint

import (
	"fmt"
	"regexp"
	"strings"
)

// Severity is the per-violation level reported by every L0N rule. Declared
// locally in this rule file; sibling L0N work-unit branches declare the same
// type and the gap-fill integration step on main picks one canonical
// location.
type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Violation is the structured output shape per FR-003. Same caveat as
// Severity: declared locally here, deduplicated at integration.
type Violation struct {
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

var (
	// `### US1`, `### US12 (P1) — title`, etc. The trailing token after
	// `US` must be at least one digit; the rest of the line is free-form.
	usHeaderRE = regexp.MustCompile(`^###\s+US(\d+)\b`)

	// Any markdown header at level 1-3 marks the end of a US section.
	// We bound on `###` or shallower (`##`, `#`) — deeper headers (`####`)
	// are sub-sections WITHIN the user story and don't terminate it.
	sectionBoundaryRE = regexp.MustCompile(`^#{1,3}\s`)

	// `**Independent test:**` — exact phrase. Case-sensitive on purpose:
	// the spec template prescribes this exact form, and accepting variants
	// would let style drift in.
	independentTestRE = regexp.MustCompile(`\*\*Independent test:\*\*`)
)

// CheckL07 runs rule L07 (user-story test presence) against a spec.md
// body. For every `### USN` header, an `**Independent test:**` paragraph
// must appear within that user story's section.
//
// Invariant: a violation is emitted iff a `### USN` header exists at
// line L AND no line in (L, nextBoundary) matches `**Independent test:**`,
// where nextBoundary is the line number of the next `###`/`##`/`#` header
// or one past EOF if none follows.
//
// specPath is the path used in the emitted Violation.File; content is the
// raw spec.md body. The function is pure — no I/O.
func CheckL07(specPath, content string) []Violation {
	type usHeader struct {
		number int
		line   int // 1-indexed
	}

	var headers []usHeader
	var boundaries []int // 1-indexed line numbers of any `###`/`##`/`#` header

	// strings.Split (vs bufio.Scanner) has no max-line-size cap — a spec.md
	// with one pathologically long line still scans correctly. Used for both
	// the header/boundary pass and the bounded marker scan below.
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lineNo := i + 1
		if sectionBoundaryRE.MatchString(line) {
			boundaries = append(boundaries, lineNo)
		}
		if m := usHeaderRE.FindStringSubmatch(line); m != nil {
			n := 0
			fmt.Sscanf(m[1], "%d", &n)
			headers = append(headers, usHeader{number: n, line: lineNo})
		}
	}
	totalLines := len(lines)

	var out []Violation
	for _, h := range headers {
		end := totalLines + 1
		for _, b := range boundaries {
			if b > h.line {
				end = b
				break
			}
		}

		found := false
		// h.line is the USN header itself; start the marker search on the
		// line after it. Bound is exclusive of `end` (the next section's
		// header line).
		for i := h.line; i < end-1 && i < len(lines); i++ {
			if independentTestRE.MatchString(lines[i]) {
				found = true
				break
			}
		}

		if !found {
			out = append(out, Violation{
				Rule:     "L07",
				File:     specPath,
				Line:     h.line,
				Severity: SeverityError,
				Message:  fmt.Sprintf("US%d is missing an `**Independent test:**` paragraph in its section", h.number),
			})
		}
	}

	return out
}
