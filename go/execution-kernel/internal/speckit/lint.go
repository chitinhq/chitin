// Package speckit provides a deterministic linter for chitin spec-kit
// specifications, intended to be called from CLI (chitin-kernel speckit-lint)
// or directly from CI tooling.
//
// The linter is intentionally mechanical: it checks structural and formatting
// rules, not semantic substance. Semantic review is the responsibility of the
// per-spec checklist and the dialectic review mechanism (spec 094).
//
// v1 checks (each produces one or more Finding records):
//
//  1. missing-required-section — spec.md lacks a required H2/H3 section
//  2. missing-frontmatter-field — spec.md lacks a required **Field**: line
//  3. wrong-h1 — spec.md's first H1 isn't "# Feature Specification: ..."
//  4. template-placeholder — leftover template scaffolding text found
//  5. needs-clarification-marker — at least one [NEEDS CLARIFICATION ...]
//  6. fr-numbering-gap — FR ids not monotonic from FR-001 with no gaps/dupes
//  7. sc-numbering-gap — same for SC ids
//  8. user-story-without-priority — story heading without (Priority: PN)
//  9. user-story-without-acceptance-scenarios — story without **Given/When/Then**
//  10. missing-checklist — checklists/requirements.md missing
//  11. unchecked-checklist-box — checklists/requirements.md has '- [ ]' boxes
package speckit

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Severity classifies a finding for downstream gating.
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
)

// Finding is one issue surfaced by the linter.
type Finding struct {
	CheckID  string   `json:"check_id"`
	Severity Severity `json:"severity"`
	File     string   `json:"file"`
	Line     int      `json:"line,omitempty"` // 1-based; 0 means whole-file
	Detail   string   `json:"detail"`
}

// requiredSections lists the H2 and H3 headers a spec.md must contain.
// Headers are matched by exact string after the leading '#' characters and
// optional trailing template annotation like "*(mandatory)*".
var requiredSections = []sectionRequirement{
	{level: 2, title: "User Scenarios & Testing"},
	{level: 3, title: "Edge Cases"},
	{level: 2, title: "Requirements"},
	{level: 2, title: "Success Criteria"},
	{level: 2, title: "Assumptions"},
}

type sectionRequirement struct {
	level int    // 2 for ##, 3 for ###
	title string // expected title text, stripped of trailing annotation
}

// requiredFrontmatterFields lists **Field**: prefixes that must appear
// near the top of spec.md (within the first 30 non-empty lines).
var requiredFrontmatterFields = []string{
	"Feature Branch",
	"Created",
	"Status",
	"Input",
}

// templatePlaceholders are strings that indicate unfilled template content.
// If any appears in spec.md, that's a finding.
var templatePlaceholders = []string{
	"[FEATURE NAME]",
	"[###-feature-name]",
	"$ARGUMENTS",
	"[Describe this user journey",
	"[Brief Title]",
	"[Explain the value and why",
	"ACTION REQUIRED:",
	"[Add more user stories",
	"[boundary condition]",
	"[error scenario]",
	"[specific capability,",
	"[key interaction,",
	"[data requirement,",
	"[behavior,",
	"[Entity 1]",
	"[Entity 2]",
	"[Measurable metric,",
	"[User satisfaction metric,",
	"[Business metric,",
	"[Assumption about target users,",
	"[Assumption about scope boundaries,",
	"[Dependency on existing system",
	"[link]",
	"[DATE]",
}

// Pre-compiled regular expressions.
var (
	reH1Feature        = regexp.MustCompile(`^#\s+Feature Specification:\s+\S`)
	reFRLine           = regexp.MustCompile(`(?m)\*\*FR-(\d{3})\*\*\s*:`)
	reSCLine           = regexp.MustCompile(`(?m)\*\*SC-(\d{3})\*\*\s*:`)
	reUserStoryHeading = regexp.MustCompile(`^(#{2,4})\s+User Story\s+\d+\b.*$`)
	rePriorityTag      = regexp.MustCompile(`\(Priority:\s*P\d+\)`)
	reGivenWhenThen    = regexp.MustCompile(`\*\*Given\*\*.*\*\*When\*\*.*\*\*Then\*\*`)
	reNeedsClarif      = regexp.MustCompile(`\[NEEDS CLARIFICATION\b`)
	reCheckedBox       = regexp.MustCompile(`^\s*-\s*\[x\]`)
	reUncheckedBox     = regexp.MustCompile(`^\s*-\s*\[ \]`)
	reHeading          = regexp.MustCompile(`^(#{1,6})\s+(.*?)(\s*\*?\(.*\)\*?)?\s*$`)
)

// Lint runs every v1 check on the spec at specDir and returns a sorted slice
// of findings (sorted by File then Line then CheckID for deterministic output).
//
// Returns an error only for IO/structural failures (spec dir missing, spec.md
// unreadable). Content findings are returned in the Findings slice, not the
// error; a structurally-empty Findings slice means the spec passes lint.
func Lint(specDir string) ([]Finding, error) {
	if _, err := os.Stat(specDir); err != nil {
		return nil, fmt.Errorf("spec dir not accessible: %w", err)
	}

	specPath := filepath.Join(specDir, "spec.md")
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("spec.md unreadable at %s: %w", specPath, err)
	}
	specText := string(specBytes)
	specLines := strings.Split(specText, "\n")

	var findings []Finding

	findings = append(findings, checkH1(specPath, specLines)...)
	findings = append(findings, checkRequiredFrontmatter(specPath, specLines)...)
	findings = append(findings, checkRequiredSections(specPath, specLines)...)
	findings = append(findings, checkTemplatePlaceholders(specPath, specLines)...)
	findings = append(findings, checkNeedsClarification(specPath, specLines)...)
	findings = append(findings, checkFRNumbering(specPath, specText)...)
	findings = append(findings, checkSCNumbering(specPath, specText)...)
	findings = append(findings, checkUserStories(specPath, specLines)...)

	// Checklist checks live in checklists/requirements.md
	findings = append(findings, checkChecklist(specDir)...)

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].CheckID < findings[j].CheckID
	})

	return findings, nil
}

// checkH1 verifies the first H1 of spec.md is "# Feature Specification: <name>".
func checkH1(specPath string, lines []string) []Finding {
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(trim, "# ") {
			continue
		}
		if !reH1Feature.MatchString(trim) {
			return []Finding{{
				CheckID:  "wrong-h1",
				Severity: SeverityError,
				File:     specPath,
				Line:     i + 1,
				Detail:   fmt.Sprintf("first H1 must match `# Feature Specification: <name>`; got %q", trim),
			}}
		}
		return nil
	}
	return []Finding{{
		CheckID:  "wrong-h1",
		Severity: SeverityError,
		File:     specPath,
		Line:     0,
		Detail:   "no H1 found; spec must begin with `# Feature Specification: <name>`",
	}}
}

// checkRequiredFrontmatter verifies each required **Field**: line appears
// within the first 30 non-empty lines.
func checkRequiredFrontmatter(specPath string, lines []string) []Finding {
	const lookahead = 30
	seen := map[string]bool{}
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		nonEmpty++
		if nonEmpty > lookahead {
			break
		}
		for _, field := range requiredFrontmatterFields {
			needle := "**" + field + "**:"
			if strings.Contains(line, needle) {
				seen[field] = true
			}
		}
	}
	var findings []Finding
	for _, field := range requiredFrontmatterFields {
		if !seen[field] {
			findings = append(findings, Finding{
				CheckID:  "missing-frontmatter-field",
				Severity: SeverityError,
				File:     specPath,
				Line:     0,
				Detail:   fmt.Sprintf("required frontmatter field `**%s**:` not found in first %d non-empty lines", field, lookahead),
			})
		}
	}
	return findings
}

// checkRequiredSections verifies every section in requiredSections appears
// at the right heading level.
func checkRequiredSections(specPath string, lines []string) []Finding {
	type seenSection struct {
		title string
		level int
	}
	seen := map[seenSection]bool{}
	for _, line := range lines {
		if m := reHeading.FindStringSubmatch(line); m != nil {
			level := len(m[1])
			title := strings.TrimSpace(m[2])
			// Strip trailing annotations the regex didn't fully eat
			// (m[3] is the annotation group, already optional).
			seen[seenSection{title: title, level: level}] = true
		}
	}
	var findings []Finding
	for _, req := range requiredSections {
		if !seen[seenSection{title: req.title, level: req.level}] {
			hashes := strings.Repeat("#", req.level)
			findings = append(findings, Finding{
				CheckID:  "missing-required-section",
				Severity: SeverityError,
				File:     specPath,
				Line:     0,
				Detail:   fmt.Sprintf("required section `%s %s` not found", hashes, req.title),
			})
		}
	}
	return findings
}

// checkTemplatePlaceholders flags any unfilled template scaffolding.
func checkTemplatePlaceholders(specPath string, lines []string) []Finding {
	var findings []Finding
	for i, line := range lines {
		for _, placeholder := range templatePlaceholders {
			if strings.Contains(line, placeholder) {
				findings = append(findings, Finding{
					CheckID:  "template-placeholder",
					Severity: SeverityError,
					File:     specPath,
					Line:     i + 1,
					Detail:   fmt.Sprintf("leftover template placeholder %q", placeholder),
				})
			}
		}
	}
	return findings
}

// checkNeedsClarification flags any [NEEDS CLARIFICATION ...] markers.
func checkNeedsClarification(specPath string, lines []string) []Finding {
	var findings []Finding
	for i, line := range lines {
		if reNeedsClarif.MatchString(line) {
			findings = append(findings, Finding{
				CheckID:  "needs-clarification-marker",
				Severity: SeverityError,
				File:     specPath,
				Line:     i + 1,
				Detail:   "[NEEDS CLARIFICATION] marker must be resolved before lint passes",
			})
		}
	}
	return findings
}

// checkFRNumbering verifies FR ids are monotonic from FR-001 with no gaps
// or duplicates.
func checkFRNumbering(specPath, specText string) []Finding {
	return checkNumberedItems(specPath, specText, reFRLine, "FR", "fr-numbering-gap")
}

// checkSCNumbering verifies SC ids are monotonic from SC-001 with no gaps
// or duplicates.
func checkSCNumbering(specPath, specText string) []Finding {
	return checkNumberedItems(specPath, specText, reSCLine, "SC", "sc-numbering-gap")
}

// checkNumberedItems implements the shared FR/SC numbering check.
func checkNumberedItems(specPath, specText string, re *regexp.Regexp, prefix, checkID string) []Finding {
	matches := re.FindAllStringSubmatch(specText, -1)
	if len(matches) == 0 {
		return nil // No items of this kind; not a finding (spec may legitimately have none).
	}
	seen := map[int]bool{}
	var nums []int
	for _, m := range matches {
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		if seen[n] {
			return []Finding{{
				CheckID:  checkID,
				Severity: SeverityError,
				File:     specPath,
				Line:     0,
				Detail:   fmt.Sprintf("%s-%03d appears more than once", prefix, n),
			}}
		}
		seen[n] = true
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for i, n := range nums {
		expected := i + 1
		if n != expected {
			return []Finding{{
				CheckID:  checkID,
				Severity: SeverityError,
				File:     specPath,
				Line:     0,
				Detail:   fmt.Sprintf("expected %s-%03d but found %s-%03d (numbering must be monotonic from %s-001 with no gaps)", prefix, expected, prefix, n, prefix),
			}}
		}
	}
	return nil
}

// checkUserStories verifies every "User Story N" heading has a Priority tag
// and that the section beneath each story contains at least one
// Given/When/Then block.
func checkUserStories(specPath string, lines []string) []Finding {
	var findings []Finding
	// Locate user story heading line indices.
	type story struct {
		startLine int    // 0-based; the heading line itself
		heading   string // raw heading text
	}
	var stories []story
	for i, line := range lines {
		if reUserStoryHeading.MatchString(line) {
			stories = append(stories, story{startLine: i, heading: strings.TrimSpace(line)})
		}
	}

	if len(stories) == 0 {
		return findings // No user stories; covered by required-section check at the section level.
	}

	for idx, s := range stories {
		// Priority check on the heading itself.
		if !rePriorityTag.MatchString(s.heading) {
			findings = append(findings, Finding{
				CheckID:  "user-story-without-priority",
				Severity: SeverityError,
				File:     specPath,
				Line:     s.startLine + 1,
				Detail:   fmt.Sprintf("user story heading missing `(Priority: PN)` tag: %s", s.heading),
			})
		}

		// Acceptance Scenarios: scan from this heading to the next user story
		// heading (or EOF) for at least one Given/When/Then line.
		var endExclusive int
		if idx+1 < len(stories) {
			endExclusive = stories[idx+1].startLine
		} else {
			endExclusive = len(lines)
		}
		sectionLines := lines[s.startLine:endExclusive]
		sectionText := strings.Join(sectionLines, "\n")
		if !reGivenWhenThen.MatchString(sectionText) {
			findings = append(findings, Finding{
				CheckID:  "user-story-without-acceptance-scenarios",
				Severity: SeverityError,
				File:     specPath,
				Line:     s.startLine + 1,
				Detail:   fmt.Sprintf("user story has no `**Given** ... **When** ... **Then** ...` line: %s", s.heading),
			})
		}
	}
	return findings
}

// checkChecklist verifies the sidecar checklist exists and has no unchecked boxes.
func checkChecklist(specDir string) []Finding {
	checklistPath := filepath.Join(specDir, "checklists", "requirements.md")
	f, err := os.Open(checklistPath)
	if err != nil {
		return []Finding{{
			CheckID:  "missing-checklist",
			Severity: SeverityError,
			File:     checklistPath,
			Line:     0,
			Detail:   "checklists/requirements.md not found; every spec must ship a quality checklist sidecar",
		}}
	}
	defer f.Close()

	var findings []Finding
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if reUncheckedBox.MatchString(line) {
			findings = append(findings, Finding{
				CheckID:  "unchecked-checklist-box",
				Severity: SeverityError,
				File:     checklistPath,
				Line:     lineNo,
				Detail:   "unchecked checklist item; must be `- [x]` or replaced with annotated text",
			})
		} else if !reCheckedBox.MatchString(line) {
			// Non-checkbox lines are fine (notes, headings, prose).
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		findings = append(findings, Finding{
			CheckID:  "missing-checklist",
			Severity: SeverityError,
			File:     checklistPath,
			Line:     0,
			Detail:   fmt.Sprintf("error reading checklist: %v", err),
		})
	}
	return findings
}
