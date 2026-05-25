// Package speclint implements the deterministic spec-PR consistency rules
// L01-L07 from spec 115 FR-003. Each rule is a pure function over the
// in-memory spec.md and tasks.md text and returns a slice of Violations —
// no network, no filesystem. The `chitin-orchestrator spec-lint`
// subcommand (spec 115 T002) loads the two files and invokes the rules;
// this package stays I/O-free so the rules are trivially testable.
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
// l04_events.go builds in isolation; sibling rule files declare the same
// shape and the merge into main consolidates them.
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Severity values are a closed set: error gates iteration, warning is
// informational (spec 115 edge case: "Only `error` violations gate the
// iteration").
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

const ruleL04 = "L04"

// L04Events enforces event-taxonomy closure (spec 115 FR-003 L04).
//
// Invariant: for every backticked snake_case identifier t appearing in
// spec.md or tasks.md (outside the canonical telemetry FR), if t shares
// the 2-segment family prefix of some canonical event e, then t ∈ E
// where E is the closed set declared by the canonical FR.
//
// Algorithm:
//
//  1. Locate the FR-NNN block in spec.md whose body literally contains
//     the phrase "Chain events" — this is the canonical telemetry block.
//  2. Extract the closed set E by collecting backticked tokens within the
//     block that match [a-z_]+ and contain at least one underscore. (Bare
//     words like `error` or `info` are filtered out — they are severity
//     values, not event identifiers.)
//  3. Compute the family set F = {family(e) | e ∈ E}, where family(e)
//     is the first two underscore-separated segments of e (or the full
//     name when e has fewer than two underscores).
//  4. Scan spec.md (outside the canonical block) and tasks.md for backticked
//     snake_case tokens. A token t is reported as an L04 violation iff
//     family(t) ∈ F and t ∉ E.
//
// The family-prefix guard is the noise filter that separates real event
// drift (e.g. `pr_iteration_skipped` against canonical `pr_iteration_*`,
// the exact bug spec 115's Why section cites from #1050) from incidental
// snake_case tokens like `pr_number` or `fixup_sha` that share a single
// segment with the canonical set but are field names, not event names.
//
// Returns nil when spec.md has no FR with "Chain events" in its body, or
// when the canonical FR declares no event-shaped identifiers — neither
// is a violation of the rule, just a no-op that lets the linter run
// successfully against specs that have no telemetry surface.
func L04Events(specPath, specContent, tasksPath, tasksContent string) []Violation {
	block, blockSpan := findCanonicalEventsFR(specContent)
	if block == "" {
		return nil
	}

	canonical := extractEventNames(block)
	if len(canonical) == 0 {
		return nil
	}
	families := computeFamilies(canonical)
	canonicalList := sortedKeys(canonical)

	var violations []Violation
	violations = append(violations,
		scanEventRefs(specPath, specContent, canonical, families, canonicalList, blockSpan)...)
	violations = append(violations,
		scanEventRefs(tasksPath, tasksContent, canonical, families, canonicalList, lineSpan{})...)

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

// lineSpan denotes [start, end] inclusive (1-based) line numbers within a
// file. A zero-value span matches no line.
type lineSpan struct {
	start, end int
}

func (s lineSpan) contains(line int) bool {
	return s.start > 0 && line >= s.start && line <= s.end
}

var (
	// frBulletRE matches a top-level FR bullet at column 0, e.g.
	// "- **FR-009** Chain events ...". Specs in this repo use bullet form;
	// indented continuation lines belong to the same FR block.
	frBulletRE = regexp.MustCompile(`^- \*\*FR-\d+\*\*`)

	// sectionHeadRE matches a markdown heading (#, ##, ### …) at column 0.
	// FR blocks never cross a heading boundary.
	sectionHeadRE = regexp.MustCompile(`^#{1,6} `)

	// eventTokenRE is the spec 115 L04 token shape: [a-z_]+ literally.
	eventTokenRE = regexp.MustCompile(`^[a-z_]+$`)

	// backtickSpanRE matches the contents of one inline backtick span.
	backtickSpanRE = regexp.MustCompile("`([^`]+)`")
)

// findCanonicalEventsFR returns the body text of the first FR-NNN bullet
// in content whose body contains the phrase "Chain events", together with
// the [start, end] line span (1-based, inclusive) the block occupies.
// Returns ("", lineSpan{}) when no such block exists.
//
// A bullet's body extends from its opening line through every following
// line up to (but not including) the next FR bullet or markdown heading.
func findCanonicalEventsFR(content string) (string, lineSpan) {
	lines := strings.Split(content, "\n")
	n := len(lines)
	for i := 0; i < n; {
		if !frBulletRE.MatchString(lines[i]) {
			i++
			continue
		}
		startLine := i + 1
		j := i + 1
		for j < n {
			if frBulletRE.MatchString(lines[j]) || sectionHeadRE.MatchString(lines[j]) {
				break
			}
			j++
		}
		body := strings.Join(lines[i:j], "\n")
		if strings.Contains(body, "Chain events") {
			return body, lineSpan{start: startLine, end: j}
		}
		i = j
	}
	return "", lineSpan{}
}

// extractEventNames returns the set of event_type identifiers found in
// inline-backtick spans within block. The first whitespace- or brace-
// delimited token inside each backtick span is the candidate; it is kept
// only when it matches [a-z_]+ and contains at least one underscore.
//
// Containing an underscore is the lightweight filter that drops bare
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

// firstToken returns the first whitespace- or brace-delimited token of s,
// trimmed of surrounding whitespace. For a backtick span like
// "spec_lint_completed { pr_number, ... }" it returns "spec_lint_completed".
func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t{"); i >= 0 {
		s = s[:i]
	}
	return s
}

func isEventLike(tok string) bool {
	if !eventTokenRE.MatchString(tok) {
		return false
	}
	return strings.Contains(tok, "_")
}

// computeFamilies returns the set of family prefixes for the events in
// canonical. family(e) is the first two underscore-separated segments of
// e (e.g. "spec_iteration" for "spec_iteration_completed"); when e has
// fewer than two underscores, family(e) is e itself.
func computeFamilies(canonical map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for e := range canonical {
		out[familyOf(e)] = struct{}{}
	}
	return out
}

func familyOf(e string) string {
	parts := strings.SplitN(e, "_", 3)
	if len(parts) >= 3 {
		return parts[0] + "_" + parts[1]
	}
	return e
}

// scanEventRefs walks content line-by-line, looking for backticked
// snake_case tokens. A token t is reported iff family(t) ∈ families and
// t ∉ canonical. Lines within excludeSpan are skipped (used to ignore the
// canonical FR's own body when scanning spec.md).
//
// Each (token, line) pair is reported once even if the same backtick span
// appears multiple times on the same line.
func scanEventRefs(
	file, content string,
	canonical, families map[string]struct{},
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
			if _, ok := families[familyOf(tok)]; !ok {
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
