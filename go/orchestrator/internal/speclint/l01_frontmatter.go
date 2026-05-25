// Package speclint implements the deterministic spec-PR consistency rules
// L01-L07 from spec 115 FR-003. Each rule is a pure function that takes
// in-memory file contents and returns a slice of Violations — no network,
// no filesystem. The `chitin-orchestrator spec-lint` subcommand (spec 115
// T002) reads spec.md/tasks.md from disk and invokes the rules; this
// package itself stays I/O-free so the rules are trivially testable.
package speclint

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Violation is the canonical shape every L0N rule returns and the
// spec-lint subcommand serialises to JSON (spec 115 FR-003:
// "{rule, file, line, severity, message}").
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"` // 1-based; line of the opening "---" fence when the violation has no narrower location
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

const ruleL01 = "L01"

// requiredKeysL01 is the closed list of frontmatter keys L01 enforces, in
// the order operators see violations reported. Ordering is fixed so the
// linter's JSON output is byte-stable across runs over the same spec.
var requiredKeysL01 = []string{
	"spec_id",
	"title",
	"status",
	"owner",
	"created",
	"depends_on",
	"related",
}

// L01Frontmatter checks that spec.md begins with a well-formed YAML
// frontmatter block whose top-level mapping defines every key in
// requiredKeysL01, each well-formed:
//
//   - spec_id    : positive integer (YAML !!int)
//   - title      : non-empty string (YAML !!str — not a bare !!int / !!bool)
//   - status     : non-empty string (YAML !!str)
//   - owner      : non-empty string (YAML !!str)
//   - created    : YYYY-MM-DD date string parseable by time.Parse(time.DateOnly, …)
//   - depends_on : YAML sequence (may be empty — write "[]" for an empty list)
//   - related    : YAML sequence (may be empty — write "[]" for an empty list)
//
// The function is pure. specPath is propagated to Violation.File so
// operators can navigate from the linter output to the offending file;
// content is the spec.md text. Returns nil when the frontmatter passes.
func L01Frontmatter(specPath, content string) []Violation {
	yamlText, baseLine, err := extractFrontmatter(content)
	if err != nil {
		return []Violation{{
			Rule:     ruleL01,
			File:     specPath,
			Line:     baseLine,
			Severity: SeverityError,
			Message:  fmt.Sprintf("frontmatter %s", err.Error()),
		}}
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlText), &root); err != nil {
		return []Violation{{
			Rule:     ruleL01,
			File:     specPath,
			Line:     baseLine,
			Severity: SeverityError,
			Message:  fmt.Sprintf("frontmatter is not valid YAML: %v", err),
		}}
	}

	mapping := topLevelMapping(&root)
	if mapping == nil {
		return []Violation{{
			Rule:     ruleL01,
			File:     specPath,
			Line:     baseLine,
			Severity: SeverityError,
			Message:  "frontmatter top-level node is not a mapping",
		}}
	}

	keys := indexMapping(mapping)

	var violations []Violation
	for _, k := range requiredKeysL01 {
		node, present := keys[k]
		if !present {
			violations = append(violations, Violation{
				Rule:     ruleL01,
				File:     specPath,
				Line:     baseLine,
				Severity: SeverityError,
				Message:  fmt.Sprintf("frontmatter missing required key %q", k),
			})
			continue
		}
		if v := validateL01Field(specPath, k, node, baseLine); v != nil {
			violations = append(violations, *v)
		}
	}
	return violations
}

// extractFrontmatter returns the YAML body between the first two "---"
// fence lines and the 1-based source line of the opening fence. When the
// fence is missing or unterminated, baseLine is clamped to 1 (the top of
// the file) so Violation.Line never leaks a 0 to downstream tooling.
func extractFrontmatter(content string) (string, int, error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return "", 1, errors.New("missing — file must start with '---' fence")
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			return strings.Join(lines[1:i], "\n"), 1, nil
		}
	}
	return "", 1, errors.New("unterminated — no closing '---' fence found")
}

// topLevelMapping returns the mapping node at the root of a parsed YAML
// document, or nil when the document is empty, a scalar, or a sequence
// at the top level.
func topLevelMapping(root *yaml.Node) *yaml.Node {
	if root == nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil
	}
	body := root.Content[0]
	if body.Kind != yaml.MappingNode {
		return nil
	}
	return body
}

// indexMapping returns a map from the key string of every top-level
// mapping entry to its value node. yaml.v3 stores mapping entries as
// alternating key/value nodes in Content; we walk them pairwise. The
// first occurrence of a key wins — duplicate keys are out of L01's scope.
func indexMapping(mapping *yaml.Node) map[string]*yaml.Node {
	out := make(map[string]*yaml.Node, len(mapping.Content)/2)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := mapping.Content[i]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		if _, dup := out[k.Value]; dup {
			continue
		}
		out[k.Value] = mapping.Content[i+1]
	}
	return out
}

// validateL01Field reports the violation a field's node fails to satisfy,
// or nil when the field is well-formed. yaml.v3's Node.Line is 1-based
// within the YAML body we fed the parser; we add baseLine (the line of
// the opening "---") so violations point at the line in the source
// spec.md, not the line in the extracted body.
func validateL01Field(specPath, key string, node *yaml.Node, baseLine int) *Violation {
	line := node.Line + baseLine
	if node.Line == 0 {
		line = baseLine
	}

	mk := func(msg string) *Violation {
		return &Violation{
			Rule:     ruleL01,
			File:     specPath,
			Line:     line,
			Severity: SeverityError,
			Message:  fmt.Sprintf("frontmatter key %q %s", key, msg),
		}
	}

	switch key {
	case "spec_id":
		// !!int is yaml.v3's implicit tag for a lexically valid integer
		// scalar; a quoted "115" resolves to !!str and fails here.
		if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
			return mk("must be an integer")
		}
		var n int
		if err := node.Decode(&n); err != nil {
			return mk(fmt.Sprintf("is not a valid integer: %v", err))
		}
		if n <= 0 {
			return mk("must be a positive integer")
		}
	case "title", "status", "owner":
		// Require an explicit !!str tag so YAML-typed scalars (title: 123
		// → !!int, owner: true → !!bool) can't silently satisfy the
		// "non-empty string" constraint.
		if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
			return mk("must be a non-empty string")
		}
		if strings.TrimSpace(node.Value) == "" {
			return mk("must be a non-empty string")
		}
	case "created":
		if node.Kind != yaml.ScalarNode || node.Tag == "!!null" {
			return mk("must be a YYYY-MM-DD date string")
		}
		if _, err := time.Parse(time.DateOnly, node.Value); err != nil {
			return mk("must be a YYYY-MM-DD date string")
		}
	case "depends_on", "related":
		// A bare "depends_on:" with no value parses as a null scalar, not
		// a sequence — that is ambiguous (null vs empty list) and rejected
		// so authors write the empty list explicitly as "[]".
		if node.Kind != yaml.SequenceNode {
			return mk("must be a YAML sequence (use '[]' for an empty list)")
		}
	}
	return nil
}
