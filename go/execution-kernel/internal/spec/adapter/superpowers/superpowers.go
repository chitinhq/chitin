// Package superpowers implements the Superpowers markdown adapter (R5, spec 061).
//
// Superpowers documents are often plans or design notes, not strict specs. This
// adapter normalizes only explicit or section-backed structure and leaves
// missing fields empty.
package superpowers

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

var dateStemPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-.+`)
var explicitReq = regexp.MustCompile(`^(?:###\s+)?\*{0,2}(R\d+)\s*[—\-]+\s*(.+?)\*{0,2}$`)
var explicitAC = regexp.MustCompile(`^\*{0,2}(AC\d+)\*{0,2}[\s:]*[—\-]?\s*(.+?)\*{0,2}$`)
var sliceHead = regexp.MustCompile(`(?i)^#{2,3}\s+(Slice\s+\d+|Milestone\s+M\d+|M\d+)\s*[—:-]\s*(.+)$`)
var numberItem = regexp.MustCompile(`^\d+\.\s+(.+)$`)
var questionItem = regexp.MustCompile(`^(?:(?:[-*]|\d+\.)\s+)?\*{0,2}(?:Q(\d+)\s*[—\-])?\s*(.+?)\*{0,2}$`)
var boldCleaner = regexp.MustCompile(`\*\*(.+?)\*\*`)
var codeCleaner = regexp.MustCompile("`(.*?)`")

var requirementSections = map[string]bool{
	"goal": true, "goals": true, "hard rules": true, "invariants": true,
	"invariant": true, "decision": true, "in scope": true, "scope": true,
}
var acceptanceSections = map[string]bool{
	"acceptance": true, "success criteria": true, "done-condition": true,
	"done condition": true, "verification": true,
}
var boundarySections = map[string]bool{
	"non-goals": true, "non goals": true, "out of scope": true,
	"edge cases": true, "risks": true,
}

// Adapter parses Superpowers markdown documents.
type Adapter struct{}

func (a *Adapter) Framework() spec.SourceFramework {
	return spec.SourceFrameworkSuperpowers
}

func (a *Adapter) Detect(path string) bool {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		if isSuperpowersDir(path) && (fileExists(filepath.Join(path, "spec.md")) || fileExists(filepath.Join(path, "plan.md"))) {
			return true
		}
		return false
	}
	if strings.ToLower(filepath.Ext(path)) != ".md" {
		return false
	}
	return isSuperpowersMarkdown(path)
}

func (a *Adapter) Parse(path string) (*spec.UnifiedSpec, error) {
	source, err := resolveSource(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, &adapter.ParseError{Path: source, Section: "file-read", Err: fmt.Errorf("read: %w", err)}
	}

	doc := string(data)
	reqs := extractRequirements(doc)
	return &spec.UnifiedSpec{
		SpecID:          specIDForPath(path, source),
		Title:           extractTitle(doc),
		Status:          extractStatus(doc),
		SourceFramework: spec.SourceFrameworkSuperpowers,
		SourcePath:      source,
		Requirements:    reqs,
		Acceptance:      extractAcceptance(doc),
		Boundaries:      extractSectionItems(doc, boundarySections),
		Slices:          extractSlices(doc, reqs),
		OpenQuestions:   extractQuestions(doc),
	}, nil
}

func resolveSource(path string) (string, error) {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		for _, name := range []string{"spec.md", "plan.md"} {
			candidate := filepath.Join(path, name)
			if fileExists(candidate) {
				return candidate, nil
			}
		}
		return "", &adapter.ParseError{Path: path, Section: "file-read", Err: fmt.Errorf("directory has no spec.md or plan.md")}
	}
	if err != nil {
		return "", &adapter.ParseError{Path: path, Section: "file-read", Err: err}
	}
	if strings.ToLower(filepath.Ext(path)) != ".md" {
		return "", &adapter.ParseError{Path: path, Section: "file-read", Err: fmt.Errorf("expected markdown file")}
	}
	return path, nil
}

func specIDForPath(path, source string) string {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return filepath.Base(path)
	}
	return strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
}

func isSuperpowersDir(path string) bool {
	return hasSuperpowersSegment(path) && dateStemPattern.MatchString(filepath.Base(filepath.Clean(path)))
}

func isSuperpowersMarkdown(path string) bool {
	return hasSuperpowersSegment(path) && dateStemPattern.MatchString(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
}

func hasSuperpowersSegment(path string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "docs" && parts[i+1] == "superpowers" {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func extractTitle(doc string) string {
	scanner := bufio.NewScanner(strings.NewReader(doc))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") {
			return cleanMarkdown(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func extractStatus(doc string) spec.SpecStatus {
	lower := strings.ToLower(doc)
	statusText := lower
	for _, line := range strings.Split(lower, "\n") {
		if strings.Contains(line, "status:") {
			statusText = line
			break
		}
	}
	switch {
	case strings.Contains(statusText, "superseded"):
		return spec.SpecStatusSuperseded
	case strings.Contains(statusText, "implemented"),
		strings.Contains(statusText, "shipped"),
		strings.Contains(statusText, "ratified"),
		strings.Contains(statusText, "amended"),
		strings.Contains(statusText, "open"):
		return spec.SpecStatusRatified
	default:
		return spec.SpecStatusDraft
	}
}

func extractRequirements(doc string) []spec.Requirement {
	var reqs []spec.Requirement
	for _, line := range strings.Split(doc, "\n") {
		target := strings.TrimSpace(line)
		if strings.HasPrefix(target, "### ") {
			target = target[4:]
		}
		if m := explicitReq.FindStringSubmatch(target); m != nil {
			reqs = append(reqs, spec.Requirement{ID: m[1], Text: cleanMarkdown(m[2])})
		}
	}
	if len(reqs) > 0 {
		return reqs
	}
	items := inlineLabelItems(doc, map[string]bool{"goal": true})
	items = append(items, extractSectionItems(doc, requirementSections)...)
	for i, item := range items {
		reqs = append(reqs, spec.Requirement{ID: fmt.Sprintf("R%d", i+1), Text: item})
	}
	return reqs
}

func extractAcceptance(doc string) []spec.AcceptanceCriterion {
	var acs []spec.AcceptanceCriterion
	for _, line := range strings.Split(doc, "\n") {
		if m := explicitAC.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			acs = append(acs, spec.AcceptanceCriterion{ID: m[1], Text: cleanMarkdown(m[2])})
		}
	}
	if len(acs) > 0 {
		return acs
	}
	items := inlineLabelFollowingBullets(doc, map[string]bool{"acceptance": true})
	items = append(items, extractSectionItems(doc, acceptanceSections)...)
	for i, item := range items {
		acs = append(acs, spec.AcceptanceCriterion{ID: fmt.Sprintf("AC%d", i+1), Text: item})
	}
	return acs
}

func extractSlices(doc string, reqs []spec.Requirement) []spec.Slice {
	var slices []spec.Slice
	for _, line := range strings.Split(doc, "\n") {
		if m := sliceHead.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			scope := cleanMarkdown(m[2])
			slices = append(slices, spec.Slice{ID: m[1], Scope: scope, RequirementIDs: linkReqs(scope, reqs)})
		}
	}
	if len(slices) > 0 {
		return slices
	}
	for i, item := range checkboxItems(doc) {
		slices = append(slices, spec.Slice{ID: fmt.Sprintf("Task %d", i+1), Scope: item})
	}
	return slices
}

func extractQuestions(doc string) []spec.Question {
	items := extractSectionItems(doc, map[string]bool{"open questions": true, "open items": true})
	var questions []spec.Question
	for i, item := range items {
		id := fmt.Sprintf("Q%d", i+1)
		text := item
		if m := questionItem.FindStringSubmatch(item); m != nil {
			if m[1] != "" {
				id = "Q" + m[1]
			}
			text = m[2]
		}
		questions = append(questions, spec.Question{ID: id, Text: cleanMarkdown(text)})
	}
	return questions
}

func extractSectionItems(doc string, wanted map[string]bool) []string {
	var found []string
	var paragraph []string
	active := false
	flush := func() {
		if len(paragraph) == 0 {
			return
		}
		text := cleanMarkdown(strings.Join(paragraph, " "))
		if text != "" {
			found = append(found, text)
		}
		paragraph = nil
	}
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			active = wanted[cleanHeading(strings.TrimPrefix(trimmed, "## "))]
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			flush()
			if active {
				active = false
			}
			continue
		}
		if !active {
			continue
		}
		if trimmed == "" {
			flush()
			continue
		}
		if item, ok := listItemText(trimmed); ok {
			flush()
			found = append(found, item)
			continue
		}
		paragraph = append(paragraph, trimmed)
	}
	flush()
	return found
}

func inlineLabelItems(doc string, wanted map[string]bool) []string {
	var items []string
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		if idx := strings.Index(trimmed, ":"); idx > 0 {
			label := cleanHeading(trimmed[:idx])
			if wanted[label] {
				text := cleanMarkdown(strings.TrimSpace(trimmed[idx+1:]))
				if text != "" {
					items = append(items, text)
				}
			}
		}
	}
	return items
}

func inlineLabelFollowingBullets(doc string, wanted map[string]bool) []string {
	var items []string
	active := false
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		if idx := strings.Index(trimmed, ":"); idx > 0 {
			tail := strings.TrimSpace(trimmed[idx+1:])
			if tail == "" || tail == "**" {
				label := cleanHeading(trimmed[:idx])
				active = wanted[label]
				continue
			}
		}
		if active && strings.HasPrefix(trimmed, "#") {
			active = false
			continue
		}
		if !active || trimmed == "" {
			continue
		}
		if item, ok := listItemText(trimmed); ok {
			items = append(items, item)
			continue
		}
		active = false
	}
	return items
}

func checkboxItems(doc string) []string {
	var items []string
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ] ") || strings.HasPrefix(trimmed, "- [x] ") {
			items = append(items, cleanMarkdown(trimmed[6:]))
		}
	}
	return items
}

func listItemText(line string) (string, bool) {
	if m := numberItem.FindStringSubmatch(line); m != nil {
		return cleanMarkdown(m[1]), true
	}
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return cleanMarkdown(line[2:]), true
	}
	return "", false
}

func linkReqs(scope string, reqs []spec.Requirement) []string {
	var ids []string
	for _, req := range reqs {
		if strings.Contains(scope, req.ID) {
			ids = append(ids, req.ID)
		}
	}
	return ids
}

func cleanHeading(s string) string {
	s = strings.ToLower(cleanMarkdown(s))
	s = strings.Trim(s, "*")
	if idx := strings.Index(s, "("); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	return strings.Trim(strings.TrimSpace(s), " :")
}

func cleanMarkdown(s string) string {
	s = strings.TrimSpace(s)
	s = boldCleaner.ReplaceAllString(s, "$1")
	s = codeCleaner.ReplaceAllString(s, "$1")
	return strings.Trim(strings.TrimSpace(s), ".")
}
