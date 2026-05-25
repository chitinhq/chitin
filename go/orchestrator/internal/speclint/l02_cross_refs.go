// Package speclint implements deterministic consistency lints for chitin
// specs — the `.specify/specs/NNN-*/spec.md` + `tasks.md` pair. Each rule
// LNN is one named function returning a slice of Violation; the
// `chitin-orchestrator spec-lint` subcommand (spec 115 FR-003) aggregates
// the rules into a single JSON document, with the named exit codes
// 0=clean, 2=warnings, 3=errors.
//
// Every rule is pure: it reads the spec dir's files and may stat siblings
// in `.specify/specs/`, but never opens a network connection, mutates a
// file, or shells out. This keeps the lint cheap enough to run on every
// spec-PR open (FR-004) and trivially testable from hermetic fixtures.
package speclint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Severity values used in Violation.Severity. Only `error` violations gate
// the iteration (spec 115 edge case "Linter has a bug and posts false
// positives"); `warning` is informational.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// RuleL02 is the rule id for cross-spec ref resolution (spec 115 FR-003 L02).
const RuleL02 = "L02"

// Violation is the structured output of a lint rule (spec 115 FR-003). The
// JSON tags match the shape declared in T002:
// `[{rule, file, line, severity, message}]`.
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// L02CheckCrossRefs is rule L02 (spec 115 FR-003): every spec_id listed in
// the spec.md frontmatter's `depends_on:` or `related:` sequence MUST glob-
// resolve to exactly one directory under specsRoot.
//
// specMdPath is the spec.md file to read. specsRoot is the directory whose
// children are spec dirs of the form `<id>-<slug>/` — typically the repo's
// `.specify/specs`. The returned file path on each Violation is specMdPath
// verbatim; the line is the file-line of the offending list item in the
// frontmatter (1-based).
//
// The rule treats only `error` severity: a dangling or ambiguous spec ref
// is a genuine inconsistency the spec author must address (either by
// removing the ref, by fixing the id, or — if the target spec is being
// introduced concurrently — by ensuring the sibling spec dir exists in the
// same PR). A missing or absent frontmatter is NOT this rule's concern:
// rule L01 covers frontmatter completeness; L02 falls through silently so
// the operator sees one L01 violation rather than seven cascade failures.
//
// An error is returned only when the rule cannot decide (unreadable file,
// malformed YAML, glob failure). That signals an infrastructure bug, not a
// spec authoring problem.
func L02CheckCrossRefs(specMdPath, specsRoot string) ([]Violation, error) {
	body, err := os.ReadFile(specMdPath)
	if err != nil {
		return nil, fmt.Errorf("speclint L02: read %s: %w", specMdPath, err)
	}
	fm, lineOffset, err := extractFrontmatter(body)
	if err != nil {
		return nil, fmt.Errorf("speclint L02: %s: %w", specMdPath, err)
	}
	if fm == "" {
		// No frontmatter (or unterminated marker) — L01 will surface that.
		return nil, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		return nil, fmt.Errorf("speclint L02: parse frontmatter in %s: %w", specMdPath, err)
	}

	var violations []Violation
	for _, field := range []string{"depends_on", "related"} {
		for _, item := range findListField(&doc, field) {
			id := strings.TrimSpace(item.Value)
			line := item.Line + lineOffset
			if id == "" {
				violations = append(violations, Violation{
					Rule:     RuleL02,
					File:     specMdPath,
					Line:     line,
					Severity: SeverityError,
					Message:  fmt.Sprintf("%s: empty spec id", field),
				})
				continue
			}
			matches, err := filepath.Glob(filepath.Join(specsRoot, id+"-*"))
			if err != nil {
				return nil, fmt.Errorf("speclint L02: glob %s under %s: %w", id, specsRoot, err)
			}
			dirs := dirMatches(matches)
			switch len(dirs) {
			case 1:
				// resolved cleanly
			case 0:
				violations = append(violations, Violation{
					Rule:     RuleL02,
					File:     specMdPath,
					Line:     line,
					Severity: SeverityError,
					Message: fmt.Sprintf(
						"%s: spec %s has no matching directory under %s",
						field, id, specsRoot,
					),
				})
			default:
				bases := make([]string, len(dirs))
				for i, d := range dirs {
					bases[i] = filepath.Base(d)
				}
				violations = append(violations, Violation{
					Rule:     RuleL02,
					File:     specMdPath,
					Line:     line,
					Severity: SeverityError,
					Message: fmt.Sprintf(
						"%s: spec %s matches %d directories under %s (%s)",
						field, id, len(dirs), specsRoot, strings.Join(bases, ", "),
					),
				})
			}
		}
	}
	return violations, nil
}

// dirMatches narrows a glob result to entries that are directories. A spec
// id ought to resolve to a directory; a file with the same prefix (a stray
// `097-notes.md` next to the specs tree, say) is not a valid sibling spec
// and must not satisfy the rule.
func dirMatches(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			continue
		}
		out = append(out, p)
	}
	return out
}

// extractFrontmatter returns the YAML body between the leading `---`
// marker and the matching closing `---` line, plus the offset to add to a
// yaml.Node.Line for that body to recover its 1-based line number in the
// original file.
//
// Returns ("", 0, nil) when the file has no leading `---` marker —
// L01's frontmatter-completeness check owns that diagnosis. Returns a
// non-nil error only when the marker opens but is never closed: that is a
// hard malformation a glob check cannot recover from.
func extractFrontmatter(body []byte) (content string, lineOffset int, err error) {
	text := string(body)
	var rest string
	switch {
	case strings.HasPrefix(text, "---\n"):
		rest = text[len("---\n"):]
	case strings.HasPrefix(text, "---\r\n"):
		rest = text[len("---\r\n"):]
	default:
		return "", 0, nil
	}
	// File line 1 is the opening "---"; YAML content begins at file line 2,
	// so yaml.Node.Line=1 maps to file line 2 → offset = 1.
	lineOffset = 1

	lines := strings.Split(rest, "\n")
	end := -1
	for i, line := range lines {
		if strings.TrimRight(line, "\r") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", 0, fmt.Errorf("frontmatter not terminated by `---`")
	}
	return strings.Join(lines[:end], "\n"), lineOffset, nil
}

// findListField returns the items of the top-level mapping field `key`
// when that field is a YAML sequence. Returns nil for any other shape
// (scalar, mapping, missing) — those are L01's concern, not L02's.
func findListField(doc *yaml.Node, key string) []*yaml.Node {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := mapping.Content[i]
		v := mapping.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key && v.Kind == yaml.SequenceNode {
			return v.Content
		}
	}
	return nil
}
