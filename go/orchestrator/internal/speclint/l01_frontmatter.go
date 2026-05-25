// Package speclint implements the deterministic spec-PR linter introduced
// by spec 115 — `chitin-orchestrator spec-lint <spec-dir>` (FR-003). Each
// rule is a pure function over a spec directory's `spec.md` + `tasks.md`
// that returns zero or more Violations; the linter command (T002) wires
// the rules together and emits structured JSON.
//
// This file contains rule L01 (frontmatter complete). Sibling files
// (l02_cross_refs.go … l07_us_test.go) carry the other six rules.
package speclint

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Severity classifies a lint Violation. `error` violations gate the
// spec-iteration loop (spec 115 FR-008); `warning` violations are
// informational and never block iteration.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Violation is the structured output contract a rule emits — one entry
// per distinct finding. The JSON tags match the FR-003 shape that the
// `chitin-orchestrator spec-lint` subcommand serializes onto stdout.
type Violation struct {
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// l01RequiredKeys is the closed set of frontmatter keys L01 enforces, in
// the canonical order used when reporting absences. Order is fixed so
// the violation stream is deterministic for golden-fixture tests.
var l01RequiredKeys = []string{
	"spec_id", "title", "status", "owner", "created", "depends_on", "related",
}

// CheckL01Frontmatter asserts that spec.md begins with a YAML
// frontmatter block delimited by `---` lines, and that every key in
// l01RequiredKeys is present and well-formed. It returns one Violation
// per distinct finding; a clean spec returns nil.
//
// Well-formedness per key:
//
//   - spec_id: positive integer (an unquoted YAML int, or a string that
//     parses as one — depends_on values like `097` come back as strings
//     under the YAML 1.2 core schema, so we accept either shape).
//   - title, status, owner: non-empty trimmed string.
//   - created: `YYYY-MM-DD` per time.Parse.
//   - depends_on, related: a (possibly empty or null) YAML sequence
//     whose elements are each a positive int or string-int.
//
// Failure modes that prevent per-key inspection — no frontmatter block,
// an unterminated one, or YAML that won't parse — each produce a
// single violation and short-circuit the remaining checks.
func CheckL01Frontmatter(specPath string, content []byte) []Violation {
	fmBody, fmContentLine, openLine, ok := extractFrontmatter(content)
	if !ok {
		return []Violation{{
			Rule: "L01", File: specPath, Line: 1, Severity: SeverityError,
			Message: "spec frontmatter not found: expected a YAML block delimited by `---` lines at the top of the file",
		}}
	}

	var raw map[string]any
	if err := yaml.Unmarshal(fmBody, &raw); err != nil {
		return []Violation{{
			Rule: "L01", File: specPath, Line: openLine, Severity: SeverityError,
			Message: fmt.Sprintf("frontmatter is not valid YAML: %v", err),
		}}
	}
	if len(raw) == 0 {
		return []Violation{{
			Rule: "L01", File: specPath, Line: openLine, Severity: SeverityError,
			Message: "frontmatter block is empty",
		}}
	}

	// Walk the YAML AST once to recover each top-level key's source line,
	// so per-key violations point at the offending line, not at the block.
	keyLines := mapKeyLines(fmBody, fmContentLine)

	var out []Violation
	for _, key := range l01RequiredKeys {
		val, present := raw[key]
		if !present {
			out = append(out, Violation{
				Rule: "L01", File: specPath, Line: openLine, Severity: SeverityError,
				Message: fmt.Sprintf("frontmatter missing required key %q", key),
			})
			continue
		}
		reason := checkL01Value(key, val)
		if reason == "" {
			continue
		}
		ln := keyLines[key]
		if ln == 0 {
			ln = openLine
		}
		out = append(out, Violation{
			Rule: "L01", File: specPath, Line: ln, Severity: SeverityError,
			Message: fmt.Sprintf("frontmatter key %q is malformed: %s", key, reason),
		})
	}
	return out
}

// checkL01Value applies the per-key well-formedness rule and returns
// the empty string when the value is acceptable; otherwise a short
// human-readable reason that names the actual shape it saw.
func checkL01Value(key string, v any) string {
	switch key {
	case "spec_id":
		n, ok := toPositiveInt(v)
		if !ok {
			return fmt.Sprintf("expected positive integer, got %s", describeValue(v))
		}
		if n <= 0 {
			return fmt.Sprintf("expected positive integer, got %d", n)
		}
		return ""
	case "title", "status", "owner":
		s, ok := v.(string)
		if !ok {
			return fmt.Sprintf("expected string, got %s", describeValue(v))
		}
		if strings.TrimSpace(s) == "" {
			return "value is empty"
		}
		return ""
	case "created":
		// yaml.v3 resolves a bare YYYY-MM-DD value as time.Time; a quoted
		// string round-trips as string. Accept either, and any string that
		// time.Parse can read as YYYY-MM-DD.
		switch x := v.(type) {
		case time.Time:
			return ""
		case string:
			if _, err := time.Parse("2006-01-02", strings.TrimSpace(x)); err != nil {
				return fmt.Sprintf("expected YYYY-MM-DD, got %q", x)
			}
			return ""
		default:
			return fmt.Sprintf("expected ISO date (YYYY-MM-DD), got %s", describeValue(v))
		}
	case "depends_on", "related":
		if v == nil {
			return ""
		}
		items, ok := v.([]any)
		if !ok {
			return fmt.Sprintf("expected sequence of spec IDs, got %s", describeValue(v))
		}
		for i, item := range items {
			n, ok := toPositiveInt(item)
			if !ok {
				return fmt.Sprintf("element [%d] is not an integer spec ID: %s", i, describeValue(item))
			}
			if n <= 0 {
				return fmt.Sprintf("element [%d] is not a positive integer: %d", i, n)
			}
		}
		return ""
	}
	return ""
}

// extractFrontmatter locates the leading `---`-delimited YAML block in
// the file. It tolerates blank lines before the opening delimiter but
// nothing else. Returns the YAML body bytes, the 1-based file line of
// the first content line inside the block (for offsetting AST line
// numbers), the line of the opening `---` (for block-level violations),
// and ok=false when no well-formed block is present.
func extractFrontmatter(content []byte) (body []byte, contentLine, openLine int, ok bool) {
	lines := bytes.Split(content, []byte("\n"))
	openIdx := -1
	for i, ln := range lines {
		if strings.TrimSpace(string(ln)) == "" {
			continue
		}
		if string(bytes.TrimRight(ln, " \t\r")) == "---" {
			openIdx = i
			break
		}
		return nil, 0, 0, false
	}
	if openIdx == -1 {
		return nil, 0, 0, false
	}
	closeIdx := -1
	for i := openIdx + 1; i < len(lines); i++ {
		if string(bytes.TrimRight(lines[i], " \t\r")) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		return nil, 0, 0, false
	}
	body = bytes.Join(lines[openIdx+1:closeIdx], []byte("\n"))
	return body, openIdx + 2, openIdx + 1, true
}

// mapKeyLines parses the frontmatter once more as a yaml.Node tree and
// returns each top-level key's 1-based file line. fmContentLine is the
// file line of the first YAML content line inside the block; node-local
// lines are 1-based, so the absolute line is node.Line + fmContentLine
// - 1. A parse failure here is silently absorbed: by the time we reach
// this helper we already know Unmarshal succeeded, but if a future
// caller changes that ordering an empty map keeps per-key violations
// pointing at the block instead of crashing.
func mapKeyLines(fmBody []byte, fmContentLine int) map[string]int {
	out := map[string]int{}
	var doc yaml.Node
	if err := yaml.Unmarshal(fmBody, &doc); err != nil {
		return out
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return out
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		out[k.Value] = k.Line + fmContentLine - 1
	}
	return out
}

// toPositiveInt accepts YAML's integer-bearing shapes — int, int64,
// uint64, float64 with no fractional part, or a string that strconv
// accepts — and returns the int. It does NOT enforce positivity; the
// caller checks the sign so the error message can name "non-positive"
// distinctly from "non-integer".
func toPositiveInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case uint64:
		return int(x), true
	case float64:
		if x == float64(int(x)) {
			return int(x), true
		}
		return 0, false
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// describeValue renders a scalar value for an error message: strings
// get quoted, other types print their Go syntax. Used in messages like
// `expected integer, got string("Draft")`.
func describeValue(v any) string {
	if v == nil {
		return "null"
	}
	if s, ok := v.(string); ok {
		return fmt.Sprintf("string(%q)", s)
	}
	return fmt.Sprintf("%T(%v)", v, v)
}
