// Package speclint implements the deterministic spec-PR consistency
// rules L01-L07 from spec 115 FR-003. Each rule is a pure function over
// the in-memory spec.md and tasks.md text and returns a slice of
// Violations — no network, no filesystem. The `chitin-orchestrator
// spec-lint` subcommand (spec 115 T002) loads the two files and invokes
// the rules; this package stays I/O-free so the rules are trivially
// testable.
package speclint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Violation is the canonical shape every L0N rule returns and the
// spec-lint subcommand serialises to JSON (spec 115 FR-003:
// "{rule, file, line, severity, message}"). Declared in this file so
// l04_events.go builds in isolation under the spec-115 work-unit model;
// sibling rule files declare the same shape and the merge into main
// consolidates them.
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Closed severity set. error gates iteration; warning is informational
// (spec 115 edge case: "Only `error` violations gate the iteration").
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

const ruleL04 = "L04"

// L04Events enforces event-taxonomy closure (spec 115 FR-003 L04).
//
// Invariant: for every backticked snake_case token t appearing in
// spec.md (outside the canonical "Chain events" FR-NNN body) or
// tasks.md, if t shares the trailing verb segment of some canonical
// event e — i.e. lastSegment(t) ∈ {lastSegment(e) | e ∈ E} — then
// t MUST be in the canonical set E; otherwise an L04 violation is
// reported against the line where t appears.
//
// Algorithm:
//
//  1. Locate the first FR-NNN bullet in spec.md whose body literally
//     contains the phrase "Chain events" — this is the canonical
//     telemetry block.
//  2. Extract the closed set E of canonical event names from inline-
//     backtick spans within the block. The candidate is the first
//     whitespace- or brace-delimited token of each span; it is kept iff
//     it matches [a-z_]+ and contains at least one underscore.
//  3. Compute the suffix set S = {lastSegment(e) | e ∈ E}.
//  4. Scan spec.md outside the canonical block, and all of tasks.md,
//     for backticked event-shaped tokens. Report any token t whose
//     lastSegment(t) ∈ S but t ∉ E.
//
// The suffix-shape guard is the noise filter that separates real event
// drift (e.g. `bar_started` against canonical `foo_started` /
// `foo_completed`, the exact failure shape the golden L04 fixture
// exercises) from incidental snake_case tokens like `pr_number`,
// `fixup_sha`, `iteration_cap_hit` which match [a-z_]+ trivially but
// are field names or reason values, not event identifiers.
//
// Returns nil when spec.md has no FR with "Chain events" in its body,
// or when the canonical FR declares no event-shaped tokens — neither is
// a violation of the rule, just a silent pass for specs with no
// telemetry surface.
func L04Events(specPath, specContent, tasksPath, tasksContent string) []Violation {
	block, blockSpan := findCanonicalEventsFR(specContent)
	if block == "" {
		return nil
	}
	canonical := extractEventNames(block)
	if len(canonical) == 0 {
		return nil
	}
	suffixes := suffixSet(canonical)
	canonicalList := sortedKeys(canonical)

	var out []Violation
	out = append(out, scanForFreelance(specPath, specContent, canonical, suffixes, canonicalList, blockSpan)...)
	out = append(out, scanForFreelance(tasksPath, tasksContent, canonical, suffixes, canonicalList, lineSpan{})...)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// lineSpan is a 1-based inclusive [start, end] line range. The zero
// value matches no line.
type lineSpan struct{ start, end int }

func (s lineSpan) contains(line int) bool {
	return s.start > 0 && line >= s.start && line <= s.end
}

var (
	// frBulletRE matches a top-level FR bullet at column 0
	// (e.g. "- **FR-009** ..."). Indented continuation lines belong to
	// the same FR block; nested bullets do not start a new FR.
	frBulletRE = regexp.MustCompile(`^- \*\*FR-\d+\*\*`)

	// sectionHeadRE matches any markdown heading at column 0. An FR's
	// body never crosses a heading boundary.
	sectionHeadRE = regexp.MustCompile(`^#{1,6} `)

	// eventTokenRE is the spec 115 FR-003 L04 token shape: [a-z_]+ as
	// stated literally in the rule.
	eventTokenRE = regexp.MustCompile(`^[a-z_]+$`)

	// backtickSpanRE matches the contents of one inline backtick span.
	backtickSpanRE = regexp.MustCompile("`([^`]+)`")
)

// findCanonicalEventsFR returns the body text and 1-based inclusive
// [start, end] line span of the first FR-NNN bullet in content whose
// body contains the phrase "Chain events". Returns ("", lineSpan{})
// when no such block exists.
//
// A bullet's body extends from its opening line through every following
// line up to (but not including) the next FR bullet or markdown
// heading.
func findCanonicalEventsFR(content string) (string, lineSpan) {
	lines := strings.Split(content, "\n")
	n := len(lines)
	for i := 0; i < n; {
		if !frBulletRE.MatchString(lines[i]) {
			i++
			continue
		}
		start := i + 1
		j := i + 1
		for j < n && !frBulletRE.MatchString(lines[j]) && !sectionHeadRE.MatchString(lines[j]) {
			j++
		}
		body := strings.Join(lines[i:j], "\n")
		if strings.Contains(body, "Chain events") {
			return body, lineSpan{start: start, end: j}
		}
		i = j
	}
	return "", lineSpan{}
}

// extractEventNames returns the set of canonical event_type identifiers
// found in inline-backtick spans within block. For each span, the
// candidate is the first whitespace- or brace-delimited token; it is
// kept iff it matches [a-z_]+ and contains at least one underscore.
//
// The underscore requirement is the lightweight filter that drops bare
// words like `error`, `info`, `reason`, `comments` which match [a-z_]+
// trivially but are not event identifiers.
func extractEventNames(block string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, m := range backtickSpanRE.FindAllStringSubmatch(block, -1) {
		tok := firstToken(m[1])
		if !isEventLike(tok) {
			continue
		}
		out[tok] = struct{}{}
	}
	return out
}

// firstToken returns the first whitespace- or brace-delimited token of
// s, trimmed of surrounding whitespace. For a span like
// "foo_started { pr_number, round }" it returns "foo_started".
func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t{"); i >= 0 {
		s = s[:i]
	}
	return s
}

func isEventLike(tok string) bool {
	return eventTokenRE.MatchString(tok) && strings.Contains(tok, "_")
}

// suffixSet returns the set of last underscore-segments of canonical.
// lastSegment("spec_iteration_completed") = "completed". The suffix set
// is the trailing-verb fingerprint that L04 uses to distinguish
// event-shaped tokens from field names and reason values which share
// snake_case shape but never end in the canonical event verbs.
func suffixSet(canonical map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for e := range canonical {
		out[lastSegment(e)] = struct{}{}
	}
	return out
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "_"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// scanForFreelance walks content line-by-line, reporting any backticked
// event-shaped token whose lastSegment is in suffixes but which is not
// itself in canonical. Lines within excludeSpan are skipped (used to
// ignore the canonical FR's own body when scanning spec.md). Each
// (token, line) pair is reported at most once.
func scanForFreelance(
	file, content string,
	canonical, suffixes map[string]struct{},
	canonicalList string,
	excludeSpan lineSpan,
) []Violation {
	var out []Violation
	seen := make(map[string]struct{})
	for idx, line := range strings.Split(content, "\n") {
		lineNo := idx + 1
		if excludeSpan.contains(lineNo) {
			continue
		}
		for _, m := range backtickSpanRE.FindAllStringSubmatch(line, -1) {
			tok := firstToken(m[1])
			if !isEventLike(tok) {
				continue
			}
			if _, ok := canonical[tok]; ok {
				continue
			}
			if _, ok := suffixes[lastSegment(tok)]; !ok {
				continue
			}
			key := fmt.Sprintf("%s@%d", tok, lineNo)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, Violation{
				Rule:     ruleL04,
				File:     file,
				Line:     lineNo,
				Severity: SeverityError,
				Message: fmt.Sprintf(
					"event %q is not in the canonical taxonomy (declared events: %s)",
					tok, canonicalList,
				),
			})
		}
	}
	return out
}

func sortedKeys(m map[string]struct{}) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
