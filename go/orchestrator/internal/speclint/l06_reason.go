// Package speclint implements the deterministic spec-PR consistency rules
// L01-L07 from spec 115 FR-003. Each rule is a pure function that takes a
// spec-directory (and, for cross-spec rules, the parent specs-root) and
// returns a slice of Violation values. The `chitin-orchestrator spec-lint`
// subcommand (spec 115 T002) composes the rules and emits the aggregated
// slice as JSON on stdout.
//
// L06 — reason taxonomy alignment.
//
// Invariant: every `reason:` value referenced in spec.md or tasks.md MUST
// appear in the canonical reason set declared by an FR-NNN of this spec OR
// one of its `depends_on:` specs. The canonical set is closed; a usage of an
// undeclared `reason` is an error-severity violation.
//
// Detection heuristic (kept narrow on purpose so false positives are rare):
//
//   - Declaration: an FR-NNN block whose first ~3 lines match the marker
//     regex `(?i)canonical[^\n]*\breason` — i.e. introduces a "canonical
//     reason" set. Within that block, every nested bullet of the form
//     `  - \`token\`` contributes `token` to the closed set.
//
//   - Usage: a `reason:` occurrence followed by an optional ASCII quote
//     (`"`, `'`, or backtick) and a lower-snake-case identifier — the shape
//     used across chain-event payloads (e.g. `reason: "design_judgement_required"`).
//
// Lines INSIDE a declaration block are masked from the usage scan so that an
// FR which illustrates a reason inline (`- \`x\` — emitted as \`reason: "x"\`...`)
// does not flag itself.
package speclint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Violation is the canonical shape every L0N rule returns and the spec-lint
// subcommand serialises to JSON (spec 115 FR-003:
// "{rule, file, line, severity, message}"). Duplicate definitions across
// rule files are coalesced into a single canonical type at gap-fill time.
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Severity values used in Violation.Severity. Per spec 115's edge-case
// "linter has a bug" clause, only `error` violations gate iteration; `warning`
// is informational. L06 violations are always `error` because an undeclared
// reason value in shipped spec text would break the iteration's chain-event
// taxonomy at runtime.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

const ruleL06 = "L06"

var (
	// reasonUsagePattern matches `reason:` followed by an optionally-quoted
	// lower-snake-case identifier. Captures the identifier. Supports double-
	// quoted, single-quoted, backticked, and unquoted forms.
	reasonUsagePattern = regexp.MustCompile("reason:\\s*[\"'`]?([a-z][a-z0-9_]*)[\"'`]?")

	// frHeaderPattern matches `- **FR-NNN**` (the canonical FR header shape).
	// Captures the three-digit id.
	frHeaderPattern = regexp.MustCompile(`(?m)^\s*-\s+\*\*FR-(\d{3})\*\*`)

	// canonicalMarkerPattern detects a "canonical reason" marker — the
	// declarative phrase that opens a reason-set FR block.
	canonicalMarkerPattern = regexp.MustCompile(`(?i)canonical[^\n]*\breason`)

	// declarationBulletPattern matches nested bullets of form
	// `  - \`token\``  inside a declaration block. The leading indent
	// distinguishes declaration members from the FR header bullet (which has
	// no leading whitespace).
	declarationBulletPattern = regexp.MustCompile("(?m)^\\s{2,}-\\s+`([a-z][a-z0-9_]*)`")

	// markdownSectionPattern detects markdown headers (##, ###, ####) that
	// terminate an FR block's continuation lines.
	markdownSectionPattern = regexp.MustCompile(`^#{2,}\s`)
)

// CheckReasonTaxonomy runs L06 against the spec at specDir. specsRoot is the
// directory containing all spec directories (used to resolve depends_on); if
// empty, it is derived as filepath.Dir(specDir).
//
// The function reads spec.md (required) and tasks.md (optional); for each
// depends_on spec it ALSO reads that spec's spec.md to merge its declared
// reasons into the canonical set. Missing depends_on directories are
// tolerated silently — L02 (cross-spec refs resolve) owns dangling-ref
// reporting.
//
// Returned violations are sorted by (File asc, Line asc) so the linter's
// JSON output is byte-stable across runs.
func CheckReasonTaxonomy(specDir, specsRoot string) ([]Violation, error) {
	if specsRoot == "" {
		specsRoot = filepath.Dir(specDir)
	}

	specPath := filepath.Join(specDir, "spec.md")
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("speclint L06: read %s: %w", specPath, err)
	}
	specText := string(specBytes)

	tasksPath := filepath.Join(specDir, "tasks.md")
	var tasksText string
	if b, err := os.ReadFile(tasksPath); err == nil {
		tasksText = string(b)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("speclint L06: read %s: %w", tasksPath, err)
	}

	selfReasons, selfDeclRanges := extractCanonicalReasons(specText)

	union := make(map[string]struct{}, len(selfReasons))
	for r := range selfReasons {
		union[r] = struct{}{}
	}
	for _, id := range dependsOnIDsForL06(specText) {
		depDir, ok := resolveDependsOnSpec(specsRoot, id)
		if !ok {
			continue
		}
		depBytes, err := os.ReadFile(filepath.Join(depDir, "spec.md"))
		if err != nil {
			continue
		}
		depReasons, _ := extractCanonicalReasons(string(depBytes))
		for r := range depReasons {
			union[r] = struct{}{}
		}
	}

	var violations []Violation
	violations = append(violations, scanReasonUsages("spec.md", specText, union, selfDeclRanges)...)
	violations = append(violations, scanReasonUsages("tasks.md", tasksText, union, nil)...)

	sort.SliceStable(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		return violations[i].Line < violations[j].Line
	})
	return violations, nil
}

// lineRange is a 1-based inclusive line range within a single file. Used to
// mask declaration blocks from the usage scan so an FR that illustrates a
// reason inline does not flag itself.
type lineRange struct {
	start int
	end   int
}

func (r lineRange) contains(line int) bool {
	return line >= r.start && line <= r.end
}

// extractCanonicalReasons walks `text` for FR-NNN blocks that open with the
// "canonical reason" marker; returns the union of declared tokens AND the
// 1-based line ranges of each declaration block (for usage-scan masking).
//
// A block extends from its FR header line up to (but not including) the
// next FR header or the next markdown section header (`##`, `###`, …),
// whichever comes first.
func extractCanonicalReasons(text string) (map[string]struct{}, []lineRange) {
	out := make(map[string]struct{})
	var ranges []lineRange

	lines := strings.Split(text, "\n")

	var headerLines []int
	for i, line := range lines {
		if frHeaderPattern.MatchString(line) {
			headerLines = append(headerLines, i)
		}
	}
	if len(headerLines) == 0 {
		return out, ranges
	}

	for hi, startIdx := range headerLines {
		endIdx := len(lines)
		if hi+1 < len(headerLines) {
			endIdx = headerLines[hi+1]
		}
		for j := startIdx + 1; j < endIdx; j++ {
			if markdownSectionPattern.MatchString(lines[j]) {
				endIdx = j
				break
			}
		}

		block := strings.Join(lines[startIdx:endIdx], "\n")

		loc := canonicalMarkerPattern.FindStringIndex(block)
		if loc == nil {
			continue
		}
		// The marker MUST be near the FR header (within ~3 lines), otherwise
		// the FR is about something else and just happens to mention
		// "canonical" and "reason" in passing.
		if strings.Count(block[:loc[0]], "\n") > 3 {
			continue
		}

		ranges = append(ranges, lineRange{start: startIdx + 1, end: endIdx})

		for _, m := range declarationBulletPattern.FindAllStringSubmatch(block, -1) {
			out[m[1]] = struct{}{}
		}
	}
	return out, ranges
}

// scanReasonUsages walks `text` line-by-line for `reason:` patterns; for each
// match whose captured identifier is not in `closed` AND whose line is not
// within any range in `mask`, emits an error-severity Violation.
func scanReasonUsages(file, text string, closed map[string]struct{}, mask []lineRange) []Violation {
	if text == "" {
		return nil
	}
	var out []Violation
	for i, line := range strings.Split(text, "\n") {
		lineNo := i + 1
		matches := reasonUsagePattern.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			continue
		}
		masked := false
		for _, r := range mask {
			if r.contains(lineNo) {
				masked = true
				break
			}
		}
		if masked {
			continue
		}

		seen := make(map[string]struct{}, len(matches))
		for _, m := range matches {
			tok := m[1]
			if _, dup := seen[tok]; dup {
				continue
			}
			seen[tok] = struct{}{}
			if _, ok := closed[tok]; ok {
				continue
			}
			out = append(out, Violation{
				Rule:     ruleL06,
				File:     file,
				Line:     lineNo,
				Severity: SeverityError,
				Message: fmt.Sprintf(
					"reason %q is not in the canonical reason set declared by an FR-NNN of this spec or its depends_on",
					tok,
				),
			})
		}
	}
	return out
}

// resolveDependsOnSpec returns the absolute path to the depends_on spec dir
// named by id (a zero-padded numeric string like "097"), iff exactly one
// match exists under specsRoot. Missing / ambiguous matches return ok=false;
// L02 reports those as separate violations.
func resolveDependsOnSpec(specsRoot, id string) (string, bool) {
	matches, err := filepath.Glob(filepath.Join(specsRoot, id+"-*"))
	if err != nil || len(matches) != 1 {
		return "", false
	}
	info, err := os.Stat(matches[0])
	if err != nil || !info.IsDir() {
		return "", false
	}
	return matches[0], true
}

// dependsOnIDsForL06 extracts depends_on ids from the YAML frontmatter of a
// spec.md. Ids are normalised to the 3-digit form (`97` → `097`) so they
// match the spec-directory naming convention. Tolerates missing /
// malformed frontmatter — returns nil rather than erroring (L01 owns
// frontmatter validity).
func dependsOnIDsForL06(specText string) []string {
	body, _, err := extractFrontmatterForL06(specText)
	if err != nil || body == "" {
		return nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return nil
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	mapping := doc.Content[0]
	seq := findSequenceForL06(mapping, "depends_on")
	if seq == nil {
		return nil
	}
	var ids []string
	for _, item := range seq.Content {
		if item.Kind != yaml.ScalarNode {
			continue
		}
		v := strings.TrimSpace(item.Value)
		if v == "" {
			continue
		}
		if isAllDigits(v) && len(v) < 3 {
			v = strings.Repeat("0", 3-len(v)) + v
		}
		ids = append(ids, v)
	}
	return ids
}

// extractFrontmatterForL06 returns the YAML body between the leading `---`
// fences plus the 1-based line on which the body begins (always 2 when a
// fence is present). The helper is named with an L06 suffix to avoid
// collisions with same-named helpers in sibling rule files; gap-fill
// consolidates them.
func extractFrontmatterForL06(text string) (string, int, error) {
	if !strings.HasPrefix(text, "---\n") && !strings.HasPrefix(text, "---\r\n") {
		return "", 0, errors.New("missing leading --- fence")
	}
	skip := 4
	if strings.HasPrefix(text, "---\r\n") {
		skip = 5
	}
	rest := text[skip:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", 0, errors.New("missing trailing --- fence")
	}
	return rest[:end], 2, nil
}

// findSequenceForL06 returns the child sequence node under key in mapping,
// or nil if the key is missing / its value isn't a sequence.
func findSequenceForL06(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := mapping.Content[i]
		v := mapping.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key && v.Kind == yaml.SequenceNode {
			return v
		}
	}
	return nil
}

// isAllDigits reports whether s is non-empty and consists entirely of ASCII
// decimal digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
