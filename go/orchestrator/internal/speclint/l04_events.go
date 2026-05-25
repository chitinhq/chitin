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

const ruleL04 = "L04"

// L04Events enforces event-taxonomy closure (spec 115 FR-003 L04).
//
// Invariant: for every snake_case identifier t appearing in spec.md or
// tasks.md (outside the canonical telemetry FR), if t shares the
// 2-segment family prefix of some canonical event e, then t ∈ E where
// E is the closed set declared by the canonical FR.
//
// Rule contract — what counts as a candidate token t:
//
//   - Token shape: t matches [a-z_]+ AND contains at least one
//     underscore. The underscore requirement is intentional and tighter
//     than the spec text's bare "[a-z_]+" — it drops bare words like
//     `error`, `info`, `reason`, `comments` that match the pattern
//     trivially but are not event identifiers. Specs MUST declare event
//     names with at least one underscore (e.g. `decision_made`, not
//     `decision`); single-segment event types are out of contract for
//     this rule.
//   - Token source: t is read from either an inline backtick span
//     (`pr_iteration_skipped`) or the value of an `event_type` field in
//     JSON/YAML-shaped examples (`"event_type": "pr_iteration_skipped"`,
//     `event_type: pr_iteration_skipped`).
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
//  4. Scan spec.md (outside the canonical block) and tasks.md for
//     candidate tokens — both backticked snake_case tokens AND
//     `event_type` field values in JSON/YAML-shaped examples. A token t
//     is reported as an L04 violation iff family(t) ∈ F and t ∉ E.
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

	// eventTypeFieldRE matches an `event_type` field assignment in the
	// common JSON/YAML shapes specs include as example payloads, e.g.
	//   "event_type": "pr_iteration_skipped"
	//   event_type: pr_iteration_skipped
	//   event_type: "pr_iteration_skipped"
	// The captured group is the value (without surrounding quotes). Spec
	// 115 FR-003 (L04) and T006 say the rule catches event_type references
	// "ANYWHERE else in spec.md or tasks.md", so backtick spans alone are
	// not sufficient — quoted payload examples must be scanned too.
	eventTypeFieldRE = regexp.MustCompile(`"?event_type"?\s*:\s*"?([a-z_]+)"?`)
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

// scanEventRefs walks content line-by-line, looking for event_type
// references in two shapes:
//
//  1. Backticked snake_case tokens, e.g. `pr_iteration_skipped`.
//  2. JSON/YAML field assignments of the form `event_type: foo_bar` or
//     `"event_type": "foo_bar"`, which spec authors use in payload
//     examples (sample chain envelopes, fixture snippets).
//
// A token t is reported iff family(t) ∈ families and t ∉ canonical.
// Lines within excludeSpan are skipped (used to ignore the canonical
// FR's own body when scanning spec.md).
//
// Each (token, line) pair is reported once even if the same token
// appears multiple times on the same line — across either shape.
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
		report := func(tok string) {
			if !isEventLike(tok) {
				return
			}
			if _, ok := canonical[tok]; ok {
				return
			}
			if _, ok := families[familyOf(tok)]; !ok {
				return
			}
			key := fmt.Sprintf("%s@%d", tok, lineNo)
			if _, dup := seen[key]; dup {
				return
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
		for _, m := range backtickSpanRE.FindAllStringSubmatch(line, -1) {
			report(firstToken(m[1]))
		}
		for _, m := range eventTypeFieldRE.FindAllStringSubmatch(line, -1) {
			report(m[1])
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
