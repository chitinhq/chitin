// Package speckit implements the spec-kit/house adapter (R3, spec 061).
//
// It parses .specify/specs/NNN-slug/spec.md files into UnifiedSpec objects.
// The adapter handles the observed variation in spec.md format:
//
//   - Title line: `# Spec NNN: Title`, `# NNN — Title`,
//     `# Feature Specification: Title — subtitle`, etc.
//   - Requirements: `### RN — …` or `**RN —** …` headings.
//   - Acceptance criteria: `**ACN** …` or `### ACN` headings.
//   - Boundary cases: `## Boundary cases` section.
//   - Open questions: `## Open questions` section with `- **QN — …**`.
//   - Slices: `## Slice plan` or `## Slice plan` with `- **Slice N** — …`.
package speckit

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/spec"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/spec/adapter"
)

// specIDPattern extracts the NNN (or ic-NNN, sw-NNN) prefix from a directory
// name like "020-sdd-tdd-enforcement" or "036-ic-001-icarus-local-llm-driver".
var specIDPattern = regexp.MustCompile(`^(\d{3,}|ic-\d+|sw-\d+)`)

// Title extraction patterns.
var titlePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^#\s+Spec\s+\S+\s*:\s+(.+)$`),            // "# Spec NNN: Title"
	regexp.MustCompile(`^#\s+\S+\s+—\s+(.+)$`),                    // "# NNN — Title"
	regexp.MustCompile(`^#\s+Feature\s+Specification:\s+(.+)$`),    // "# Feature Specification: ..."
	regexp.MustCompile(`^#\s+Scripts\s+Classification:\s+(.+)$`),  // "# Scripts Classification: ..."
	regexp.MustCompile(`^#\s+\w+:\s+(.+)$`),                        // "# Dispatch: Title"
	regexp.MustCompile(`^#\s+(.+)$`),                                // Fallback
}

// Section parsing patterns.
var requireHeading = regexp.MustCompile(`^(?:###\s+)?\*{0,2}([RCQ]\d+)\s*[—\-]+\s*(.+?)\*{0,2}$`)
var acceptanceHeading = regexp.MustCompile(`^\*{0,2}(AC\d+)\*{0,2}[\s:]*[—\-]?\s*(.+?)\*{0,2}$`)
var sliceHeading = regexp.MustCompile(`^\*{0,2}(Slice\s+\d+)\*{0,2}\s*[—\-]+\s*(.+)$`)
var questionPattern = regexp.MustCompile(`^\s*[-*]\s+\*{0,2}(Q\d+)\s*[—\-]+\s*(.+?)\*{0,2}(?:\s*[—\-]+\s*(.+?))?\s*$`)
var questionHeadingPattern = regexp.MustCompile(`^###\s+(Q\d+)\s*[—\-]+\s*(.+)$`)
var numberedBoundary = regexp.MustCompile(`^\d+\.\s+(.+)$`)
var boldCleaner = regexp.MustCompile(`\*\*(.+?)\*\*`)
var codeCleaner = regexp.MustCompile("`(.*?)`")

// Adapter implements spec-kit/house format parsing.
type Adapter struct{}

// Framework returns the spec-kit source framework constant.
func (a *Adapter) Framework() spec.SourceFramework {
	return spec.SourceFrameworkSpecKit
}

// Detect returns true if path looks like a spec-kit directory
// (.specify/specs/NNN-slug/spec.md).
func (a *Adapter) Detect(path string) bool {
	base := filepath.Base(path)
	if !specIDPattern.MatchString(base) {
		return false
	}
	info, err := os.Stat(filepath.Join(path, "spec.md"))
	if err != nil || info.IsDir() {
		return false
	}
	return true
}

// Parse reads the spec.md at path and returns a fully-populated UnifiedSpec.
func (a *Adapter) Parse(path string) (*spec.UnifiedSpec, error) {
	specDir := filepath.Clean(path)
	if filepath.Base(specDir) == "spec.md" {
		specDir = filepath.Dir(specDir)
	}

	specFile := filepath.Join(specDir, "spec.md")
	data, err := os.ReadFile(specFile)
	if err != nil {
		return nil, &adapter.ParseError{
			Path:    specFile,
			Section: "file-read",
			Err:     fmt.Errorf("read: %w", err),
		}
	}

	dirName := filepath.Base(specDir)
	sidMatch := specIDPattern.FindString(dirName)
	if sidMatch == "" {
		return nil, &adapter.ParseError{
			Path:    specFile,
			Section: "spec_id",
			Err:     fmt.Errorf("directory %q does not match spec-id pattern", dirName),
		}
	}

	doc := string(data)
	title := extractTitle(doc)
	parsedStatus := extractStatus(doc)
	reqs := extractRequirements(doc)
	acs := extractAcceptance(doc)
	bounds := extractBoundaries(doc)
	slices := extractSlices(doc, reqs)
	questions := extractQuestions(doc)

	return &spec.UnifiedSpec{
		SpecID:          sidMatch,
		Title:           title,
		Status:          parsedStatus,
		SourceFramework: spec.SourceFrameworkSpecKit,
		SourcePath:       specFile,
		Requirements:    reqs,
		Acceptance:      acs,
		Boundaries:      bounds,
		Slices:          slices,
		OpenQuestions:   questions,
	}, nil
}

func extractTitle(doc string) string {
	scanner := bufio.NewScanner(strings.NewReader(doc))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "# ") {
			continue
		}
		for _, pat := range titlePatterns {
			if m := pat.FindStringSubmatch(line); m != nil {
				return strings.TrimSpace(m[len(m)-1])
			}
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "# "))
	}
	return ""
}

func extractStatus(doc string) spec.SpecStatus {
	lower := strings.ToLower(doc)
	switch {
	case strings.Contains(lower, "**status**: shipped"):
		return spec.SpecStatusRatified
	case strings.Contains(lower, "**status**: ratified"):
		return spec.SpecStatusRatified
	case strings.Contains(lower, "**status**: superseded"):
		return spec.SpecStatusSuperseded
	case strings.Contains(lower, "**status**: draft"):
		return spec.SpecStatusDraft
	case strings.Contains(lower, "ratified"):
		return spec.SpecStatusRatified
	default:
		return spec.SpecStatusDraft
	}
}

func extractRequirements(doc string) []spec.Requirement {
	var reqs []spec.Requirement
	for _, line := range strings.Split(doc, "\n") {
		stripped := strings.TrimSpace(line)
		target := stripped
		if strings.HasPrefix(target, "### ") {
			target = target[4:]
		}
		m := requireHeading.FindStringSubmatch(target)
		if m == nil {
			continue
		}
		id := m[1]
		text := cleanMarkdown(strings.TrimSpace(m[2]))
		if id != "" && text != "" && strings.HasPrefix(id, "R") {
			reqs = append(reqs, spec.Requirement{ID: id, Text: text})
		}
	}
	return reqs
}

func extractAcceptance(doc string) []spec.AcceptanceCriterion {
	var acs []spec.AcceptanceCriterion
	for _, line := range strings.Split(doc, "\n") {
		stripped := strings.TrimSpace(line)
		m := acceptanceHeading.FindStringSubmatch(stripped)
		if m == nil {
			continue
		}
		id := m[1]
		text := cleanMarkdown(strings.TrimSpace(m[2]))
		if id != "" && text != "" {
			acs = append(acs, spec.AcceptanceCriterion{ID: id, Text: text})
		}
	}
	return acs
}

func extractBoundaries(doc string) []string {
	lines := strings.Split(doc, "\n")
	inBounds := false
	var bounds []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			heading := strings.ToLower(strings.TrimPrefix(trimmed, "## "))
			if strings.HasPrefix(heading, "boundary") {
				inBounds = true
				continue
			}
			if inBounds {
				break
			}
			continue
		}
		if !inBounds {
			continue
		}
		if m := numberedBoundary.FindStringSubmatch(trimmed); m != nil {
			bounds = append(bounds, cleanListItem(m[1]))
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			item := trimmed[2:]
			bounds = append(bounds, cleanListItem(item))
		}
	}
	return bounds
}

func extractSlices(doc string, reqs []spec.Requirement) []spec.Slice {
	lines := strings.Split(doc, "\n")
	inSlices := false
	var slices []spec.Slice
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			heading := strings.ToLower(strings.TrimPrefix(trimmed, "## "))
			if strings.HasPrefix(heading, "slice") {
				inSlices = true
				continue
			}
			if inSlices {
				break
			}
			continue
		}
		if !inSlices {
			continue
		}
		// Strip bullet prefix
		target := trimmed
		if strings.HasPrefix(target, "- ") {
			target = target[2:]
		} else if strings.HasPrefix(target, "* ") {
			target = target[2:]
		}
		m := sliceHeading.FindStringSubmatch(target)
		if m != nil {
			id := m[1]
			scope := cleanMarkdown(strings.TrimSpace(m[2]))
			reqIDs := linkSlicesToReqs(scope, reqs)
			slices = append(slices, spec.Slice{
				ID:             id,
				Scope:          scope,
				RequirementIDs: reqIDs,
			})
		}
	}
	return slices
}

func extractQuestions(doc string) []spec.Question {
	lines := strings.Split(doc, "\n")
	inQuestions := false
	var questions []spec.Question
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			heading := strings.ToLower(strings.TrimPrefix(trimmed, "## "))
			if strings.Contains(heading, "open question") {
				inQuestions = true
				continue
			}
			if inQuestions {
				break
			}
			continue
		}
		if !inQuestions {
			continue
		}
		if m := questionPattern.FindStringSubmatch(trimmed); m != nil {
			id := m[1]
			text := cleanMarkdown(strings.TrimSpace(m[2]))
			questions = append(questions, spec.Question{ID: id, Text: text})
			continue
		}
		if m := questionHeadingPattern.FindStringSubmatch(trimmed); m != nil {
			id := m[1]
			text := cleanMarkdown(strings.TrimSpace(m[2]))
			questions = append(questions, spec.Question{ID: id, Text: text})
		}
	}
	return questions
}

func linkSlicesToReqs(scope string, reqs []spec.Requirement) []string {
	var linked []string
	for _, r := range reqs {
		if strings.Contains(scope, r.ID) {
			linked = append(linked, r.ID)
		}
	}
	return linked
}

func cleanMarkdown(s string) string {
	s = strings.TrimSpace(s)
	s = boldCleaner.ReplaceAllString(s, "$1")
	s = codeCleaner.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

func cleanListItem(s string) string {
	s = strings.TrimSpace(s)
	s = boldCleaner.ReplaceAllString(s, "$1")
	s = strings.TrimRight(s, ".")
	return strings.TrimSpace(s)
}