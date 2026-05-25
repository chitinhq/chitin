// Package speclint hosts the deterministic spec-PR consistency rules
// from spec 115 FR-003. Each rule is a pure function: in-memory
// content -> []Violation. No network, no filesystem. The
// `chitin-orchestrator spec-lint` subcommand reads files and dispatches.
package speclint

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Violation + SeverityError live in lint.go (canonical home). L06 used
// to declare both locally because every rule task was dispatched in
// isolation; consolidated here.

const ruleL06 = "L06"

// l06FRHeaderPattern marks the start of an FR-NNN list-item header.
// Matches "- **FR-001**", "* **FR-001**", with optional leading
// whitespace. Multi-line (?m) so ^ anchors to each line start.
var l06FRHeaderPattern = regexp.MustCompile(`(?m)^[ \t]*[-*][ \t]+\*\*FR-\d+\*\*`)

// l06CanonicalBulletPattern matches a bullet of the shape
//
//	"  - `<lower_snake>` <sep>"
//
// inside a canonical-reason FR block. The trailing separator (em-dash,
// en-dash, colon, or hyphen) keeps backticked words used elsewhere in
// the spec from being mistaken for canonical entries — event-shape
// bullets like ``  - `event { ... }`  `` end the backtick on `}` not
// on a separator, so they don't match.
var l06CanonicalBulletPattern = regexp.MustCompile("(?m)^[ \\t]+[-*][ \\t]+`([a-z][a-z0-9_]*)`[ \\t]+[—–:\\-]")

// l06ReasonRefPattern captures `reason: "<value>"` or `reason: ` + "`<value>`".
// Allows a single wrapped newline + indent between `reason:` and the
// value, so prose that wraps mid-event-shape literal is still
// recognised (e.g. spec 115 lines 97-98 wrap `{ reason:` / `"foo" }`).
// Tolerates CRLF line endings (`\r?\n`) for files checked out on
// platforms with non-LF defaults. The leading `\b` keeps `treason:` /
// other suffix-of-`reason` words from matching.
var l06ReasonRefPattern = regexp.MustCompile("\\breason:[ \\t]*(?:\\r?\\n[ \\t]*)?(?:\"([a-z][a-z0-9_]*)\"|`([a-z][a-z0-9_]*)`)")

// l06FrontmatterPattern matches the YAML frontmatter block at the
// start of a Markdown file. Tolerates CRLF (`\r?\n`) so frontmatter
// parsing doesn't silently break on non-LF checkouts.
var l06FrontmatterPattern = regexp.MustCompile(`(?s)\A---[ \t]*\r?\n(.*?)\r?\n---[ \t]*\r?\n`)

// CheckL06 enforces reason-taxonomy alignment for a single spec
// directory's contents.
//
// Invariant: every `reason: "<value>"` (or `reason: ` + "`<value>`")
// reference in specContent or tasksContent MUST appear in the union of
// canonical reason sets declared by an FR-NNN block of specContent or
// any depSpecContents.
//
// A "canonical reason FR block" is identified by the first-line
// heuristic: the FR-NNN header line contains both "canonical" and
// "reason" (case-insensitive). The block extends until the next
// FR-NNN header or the next H2 (`## `) header, whichever comes first.
// Each indented bullet of the shape ``  - `<value>` <sep>`` inside
// that block contributes its backticked identifier to the canonical
// set.
//
// The function is pure. specPath / tasksPath are reported verbatim in
// Violation.File. tasksContent may be "" (no tasks.md). depSpecContents
// may be nil/empty.
//
// Boundaries explicitly handled:
//   - No FR blocks declare a canonical reason set -> empty canonical ->
//     every reason: reference becomes a violation.
//   - tasksContent == "" -> tasks.md scan is skipped entirely.
//   - A reference value present in canonical -> no violation, even if
//     it appears repeatedly.
//   - Reference value wrapped across one newline -> detected.
//
// Violation ordering: spec violations first (in document order by
// match-start offset), then tasks violations (same ordering).
func CheckL06(specPath, specContent, tasksPath, tasksContent string, depSpecContents []string) []Violation {
	canonical := map[string]struct{}{}
	l06CollectCanonicalReasons(specContent, canonical)
	for _, dep := range depSpecContents {
		l06CollectCanonicalReasons(dep, canonical)
	}

	var violations []Violation
	violations = append(violations, l06ScanReasonReferences(specPath, specContent, canonical)...)
	if tasksContent != "" {
		violations = append(violations, l06ScanReasonReferences(tasksPath, tasksContent, canonical)...)
	}
	return violations
}

// L06DependsOnIDs extracts spec ids (zero-padded to 3 digits) from the
// depends_on YAML list in content's frontmatter. Returns nil if the
// frontmatter is absent or has no depends_on key.
//
// Exported so the spec-lint subcommand can resolve dep spec.md paths
// before invoking CheckL06. Accepts both YAML block-list form
// (`depends_on:\n  - 113`) and inline-flow form (`depends_on: [113]`).
// Quoted ids (`"113"` / `'113'`) are tolerated.
//
// Non-integer entries (e.g. ids that aren't a bare number) are
// silently skipped — L01 / L02 are responsible for flagging
// malformed frontmatter and unresolved cross-refs.
func L06DependsOnIDs(content string) []string {
	m := l06FrontmatterPattern.FindStringSubmatch(content)
	if m == nil {
		return nil
	}
	var ids []string
	inDeps := false
	for _, ln := range strings.Split(m[1], "\n") {
		trimmed := strings.TrimRight(ln, " \t\r")
		if !inDeps {
			leftTrim := strings.TrimLeft(trimmed, " \t")
			if !strings.HasPrefix(leftTrim, "depends_on:") {
				continue
			}
			rest := strings.TrimSpace(strings.TrimPrefix(leftTrim, "depends_on:"))
			if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
				// Inline flow list: depends_on: [113, 097]
				inner := strings.Trim(rest, "[]")
				for _, part := range strings.Split(inner, ",") {
					part = strings.TrimSpace(part)
					part = strings.Trim(part, `"'`)
					if n, err := strconv.Atoi(part); err == nil {
						ids = append(ids, fmt.Sprintf("%03d", n))
					}
				}
				continue
			}
			// Block list opens on next indented line.
			inDeps = true
			continue
		}
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, " ") && !strings.HasPrefix(trimmed, "\t") {
			break
		}
		body := strings.TrimSpace(trimmed)
		if !strings.HasPrefix(body, "-") {
			break
		}
		val := strings.TrimSpace(strings.TrimPrefix(body, "-"))
		val = strings.Trim(val, `"'`)
		if n, err := strconv.Atoi(val); err == nil {
			ids = append(ids, fmt.Sprintf("%03d", n))
		}
	}
	return ids
}

// l06CollectCanonicalReasons walks FR blocks in content and adds each
// bullet-listed reason identifier from any block whose first header
// line contains both "canonical" and "reason" (case-insensitive). A
// block is bounded by the next FR-NNN header or the next H2 header
// (`## ...`), whichever comes first — the H2 bound prevents
// success-criteria / scope / edge-case sections of the spec (which
// may contain unrelated backticked snake_case bullets) from leaking
// into the canonical set.
func l06CollectCanonicalReasons(content string, dst map[string]struct{}) {
	if content == "" {
		return
	}
	lines := strings.Split(content, "\n")

	var frHeaderLines []int
	for i, ln := range lines {
		if l06FRHeaderPattern.MatchString(ln) {
			frHeaderLines = append(frHeaderLines, i)
		}
	}

	for j, headerIdx := range frHeaderLines {
		headerLine := lines[headerIdx]
		if !l06ContainsFold(headerLine, "canonical") || !l06ContainsFold(headerLine, "reason") {
			continue
		}

		blockEnd := len(lines)
		if j+1 < len(frHeaderLines) {
			blockEnd = frHeaderLines[j+1]
		}
		for k := headerIdx + 1; k < blockEnd; k++ {
			if strings.HasPrefix(lines[k], "## ") {
				blockEnd = k
				break
			}
		}

		block := strings.Join(lines[headerIdx:blockEnd], "\n")
		for _, b := range l06CanonicalBulletPattern.FindAllStringSubmatch(block, -1) {
			dst[b[1]] = struct{}{}
		}
	}
}

func l06ContainsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// l06ScanReasonReferences emits one violation per `reason:` reference
// whose value is absent from canonical. The reported line is the line
// of the value itself (not the line of the `reason:` keyword) so the
// operator's editor jumps straight to the offending string when the
// reference wraps across a newline.
func l06ScanReasonReferences(file, content string, canonical map[string]struct{}) []Violation {
	if content == "" {
		return nil
	}
	matches := l06ReasonRefPattern.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]Violation, 0, len(matches))
	for _, m := range matches {
		// m layout: [full-start, full-end,
		//            dq-val-start, dq-val-end,
		//            bt-val-start, bt-val-end]
		var (
			val      string
			valStart int
		)
		switch {
		case m[2] >= 0:
			val = content[m[2]:m[3]]
			valStart = m[2]
		case m[4] >= 0:
			val = content[m[4]:m[5]]
			valStart = m[4]
		}
		if val == "" {
			continue
		}
		if _, ok := canonical[val]; ok {
			continue
		}
		line := 1 + strings.Count(content[:valStart], "\n")
		out = append(out, Violation{
			Rule:     ruleL06,
			File:     file,
			Line:     line,
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"reason %q is not declared by any canonical FR-NNN reason taxonomy in this spec or its depends_on",
				val,
			),
		})
	}
	return out
}
