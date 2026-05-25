// Package speclint hosts deterministic spec-PR consistency rules (spec 115
// FR-003). This file ships rule L04 (event taxonomy closure).
//
// Self-contained for the work-unit: this file declares the Violation and
// Severity types the rule emits. Other rule work-units (T002 lint.go, T003
// L01, T005 L03, etc.) declare the same shapes in their own files; the spec
// 115 gap-fill PR reconciles the duplicates against the single canonical
// declaration in lint.go. Keeping the types here lets `go build` and `go
// test` on this work-unit PR pass standalone.
package speclint

import (
	"fmt"
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

// frBulletRegex matches the opening line of an FR bullet: `- **FR-NNN**`.
// FRs in spec 115 are bullet items, not top-level headers, so block
// boundaries are detected by the NEXT FR bullet or a `### ` section header.
//
// sectionHeaderRegex marks the next `### ` (or higher-level) header so a
// canonical FR block does not silently swallow content from a later
// section that happens not to start with another FR.
//
// chainEventsRegex is the heuristic for "this FR is the telemetry FR":
// FR-009 in spec 115 has "Chain events" literally in its first sentence.
//
// eventDefRegex matches an event definition INSIDE the canonical block:
// `<name> { ... }` in backticks. Group 1 is the event name.
//
// eventBacktickNameRegex matches a plain backticked snake_case identifier
// with no following `{` — the shape spec 099 FR-010 uses to declare and
// reference chain events (e.g. `copilot_pr_detected`,
// `copilot_review_posted`). Without this, extractCanonicalEvents would
// miss specs that declare events as bare backticked names and L04 could
// not enforce closure for them.
//
// eventVerbRegex matches a bare prose reference to an event_type by its
// verb-suffix shape. The closed verb set (started/completed/failed/...) is
// the conventional shape across spec 113/114/115 chain events; this is
// what catches the spec 113 `pr_iteration_skipped` drift case named in
// spec 115's WHY section.
var (
	frBulletRegex          = regexp.MustCompile(`^- \*\*FR-\d{3,}\*\*`)
	sectionHeaderRegex     = regexp.MustCompile(`^#{1,3} `)
	chainEventsRegex       = regexp.MustCompile(`(?i)\bchain events\b`)
	eventDefRegex          = regexp.MustCompile("`([a-z][a-z_]+)\\s*\\{")
	eventBacktickNameRegex = regexp.MustCompile("`([a-z][a-z_]+)`")
	eventVerbRegex         = regexp.MustCompile(`\b([a-z][a-z_]+_(?:started|completed|failed|escalated|skipped|emitted|fired|dispatched|detected|posted|received|created|updated|deleted|opened|closed|merged|landed))\b`)
)

// L04EventTaxonomy asserts that every event_type referenced in spec.md or
// tasks.md appears in the canonical FR-NNN telemetry block (the FR whose
// body contains "Chain events"). The canonical block is the source of
// truth; references outside it that are not in the set are violations.
//
// Detection runs in two passes:
//
//  1. Find the canonical FR block and harvest its event names from
//     backtick-enclosed `<name> { ... }` declarations.
//  2. Scan the rest of spec.md and all of tasks.md for event references
//     in two shapes — backtick `<name> {` and bare `<name>_<verb>` — and
//     flag any reference not in the canonical set.
//
// Output is sorted by (file, line, message) so repeated runs produce
// byte-identical results — a precondition for FR-004's per-(rule, file,
// line) dedup of PR review comments.
func L04EventTaxonomy(specMD, tasksMD string) []Violation {
	canon, canonStartLine, canonEndLine := extractCanonicalEvents(specMD)

	var violations []Violation
	seen := map[string]struct{}{}
	add := func(file, name string, line int) {
		if _, ok := canon[name]; ok {
			return
		}
		key := fmt.Sprintf("%s:%d:%s", file, line, name)
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		violations = append(violations, Violation{
			Rule:     "L04",
			File:     file,
			Line:     line,
			Severity: SeverityError,
			Message:  fmt.Sprintf("event_type %q referenced but not declared in the canonical FR-NNN telemetry block (the `Chain events` FR)", name),
		})
	}

	if canon == nil {
		// No telemetry FR detected. Any event-shape reference anywhere is
		// then by definition unbacked. Emit a warning-severity hint AND
		// continue to flag refs so the author sees both the structural
		// gap and the specific offenders.
		if hasAnyEventRef(specMD) || hasAnyEventRef(tasksMD) {
			violations = append(violations, Violation{
				Rule:     "L04",
				File:     "spec.md",
				Line:     1,
				Severity: SeverityWarning,
				Message:  "L04: event_types referenced in spec.md or tasks.md but no canonical telemetry block found (expected an FR-NNN in spec.md whose body contains `Chain events`)",
			})
		}
	}

	for _, ref := range findEventRefs(specMD) {
		// Skip refs that fall inside the canonical block — those ARE
		// the declarations themselves.
		if canon != nil && ref.line >= canonStartLine && ref.line <= canonEndLine {
			continue
		}
		add("spec.md", ref.name, ref.line)
	}
	for _, ref := range findEventRefs(tasksMD) {
		add("tasks.md", ref.name, ref.line)
	}

	sort.SliceStable(violations, func(i, j int) bool {
		a, b := violations[i], violations[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Message < b.Message
	})
	return violations
}

// extractCanonicalEvents finds the FR-NNN bullet whose body contains
// "Chain events" and returns the set of event names declared inside it
// (via `<name> { ... }` patterns) plus the 1-indexed line range
// [startLine, endLine] of the FR block. Returns nil if no such FR exists.
func extractCanonicalEvents(specMD string) (map[string]struct{}, int, int) {
	lines := strings.Split(specMD, "\n")
	blocks := findFRBlocks(lines)
	for _, b := range blocks {
		body := strings.Join(lines[b.startLine-1:b.endLine], "\n")
		if !chainEventsRegex.MatchString(body) {
			continue
		}
		names := map[string]struct{}{}
		for _, m := range eventDefRegex.FindAllStringSubmatch(body, -1) {
			names[m[1]] = struct{}{}
		}
		// Spec 099-style: events declared as plain backticked names with
		// no `{` (e.g. `copilot_pr_detected`). Inside the canonical FR
		// body the over-collection risk is bounded — the FR is small
		// and its job is to enumerate events.
		for _, m := range eventBacktickNameRegex.FindAllStringSubmatch(body, -1) {
			names[m[1]] = struct{}{}
		}
		if len(names) == 0 {
			// FR mentions "chain events" but declares none. Treat as
			// not-canonical so the warning path fires and the author
			// notices the empty taxonomy.
			continue
		}
		return names, b.startLine, b.endLine
	}
	return nil, 0, 0
}

// frBlock is one FR bullet's 1-indexed line range. endLine is inclusive
// and is the last line that belongs to the bullet — usually the line
// before the next FR bullet or the next `### ` section header.
type frBlock struct {
	startLine, endLine int
}

func findFRBlocks(lines []string) []frBlock {
	var starts []int
	for i, line := range lines {
		if frBulletRegex.MatchString(line) {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return nil
	}
	var blocks []frBlock
	for i, s := range starts {
		end := len(lines) - 1
		// End-of-block: line BEFORE the next FR bullet, OR line before
		// the next `### ` (or higher) header, whichever comes first.
		if i+1 < len(starts) {
			end = starts[i+1] - 1
		}
		for j := s + 1; j <= end; j++ {
			if sectionHeaderRegex.MatchString(lines[j]) {
				end = j - 1
				break
			}
		}
		blocks = append(blocks, frBlock{startLine: s + 1, endLine: end + 1})
	}
	return blocks
}

// eventRef is one detected reference: its event-name token and the
// 1-indexed line it appeared on.
type eventRef struct {
	name string
	line int
}

// findEventRefs locates every event_type reference in src. Three shapes:
//   1. Backtick-enclosed `<name> {` — the canonical declaration shape;
//      catches in-prose declarations of events the author thinks exist.
//   2. Plain backticked `<name>` (no `{`) — spec 099-style references
//      such as `copilot_pr_detected` mentioned in prose.
//   3. Bare `<name>_<verb>` where verb is in the event-suffix set —
//      catches the prose-mention drift case (spec 115 WHY cites
//      `pr_iteration_skipped` exactly this way).
func findEventRefs(src string) []eventRef {
	var refs []eventRef
	for i, line := range strings.Split(src, "\n") {
		lineNum := i + 1
		for _, m := range eventDefRegex.FindAllStringSubmatch(line, -1) {
			refs = append(refs, eventRef{name: m[1], line: lineNum})
		}
		for _, m := range eventBacktickNameRegex.FindAllStringSubmatch(line, -1) {
			refs = append(refs, eventRef{name: m[1], line: lineNum})
		}
		for _, m := range eventVerbRegex.FindAllStringSubmatch(line, -1) {
			refs = append(refs, eventRef{name: m[1], line: lineNum})
		}
	}
	return refs
}

func hasAnyEventRef(src string) bool {
	return len(findEventRefs(src)) > 0
}
