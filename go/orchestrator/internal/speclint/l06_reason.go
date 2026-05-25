package speclint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Violation is the structured finding emitted by a speclint rule.
// It matches the JSON envelope declared by FR-003 of spec 115:
// `[{rule, file, line, severity, message}]`.
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// L06 enforces reason-taxonomy alignment for a spec directory.
//
// Invariant: every `reason: "<value>"` or `reason: ` + "`<value>`" reference
// in spec.md or tasks.md is in the union of canonical reason sets
// declared by some FR-NNN block of this spec or any of its
// depends_on specs.
//
// specDir is the absolute or relative path to a single
// .specify/specs/NNN-*/ directory. Its parent is treated as the
// specs root for resolving depends_on links.
func L06(specDir string) ([]Violation, error) {
	specPath := filepath.Join(specDir, "spec.md")
	tasksPath := filepath.Join(specDir, "tasks.md")

	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("L06: read spec.md: %w", err)
	}
	tasksBytes, err := os.ReadFile(tasksPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("L06: read tasks.md: %w", err)
		}
		tasksBytes = nil
	}

	canonical := map[string]struct{}{}
	collectCanonicalReasons(string(specBytes), canonical)

	specsRoot := filepath.Dir(specDir)
	for _, depID := range readDependsOn(string(specBytes)) {
		depPath := resolveDepSpecPath(specsRoot, depID)
		if depPath == "" {
			continue
		}
		depBytes, err := os.ReadFile(depPath)
		if err != nil {
			continue
		}
		collectCanonicalReasons(string(depBytes), canonical)
	}

	var violations []Violation
	violations = append(violations, scanReasonReferences("spec.md", string(specBytes), canonical)...)
	violations = append(violations, scanReasonReferences("tasks.md", string(tasksBytes), canonical)...)
	return violations, nil
}

var (
	frontmatterRe = regexp.MustCompile(`(?s)\A---\s*\n(.*?)\n---\s*\n`)

	// frHeaderRe marks the start of an FR-NNN block. Matches both
	// "- **FR-001**" and "* **FR-001**" list styles.
	frHeaderRe = regexp.MustCompile(`(?m)^[ \t]*[-*][ \t]+\*\*FR-\d+\*\*`)

	// canonicalBulletRe finds bullets of the shape
	//   "  - `<lower_snake>` — ..."  (em-dash, en-dash, or hyphen)
	// inside a canonical reason FR block. The separator (em-dash
	// "—", en-dash "–", or hyphen "-") guards against catching
	// backtick-quoted identifiers that aren't list-item canonicals.
	canonicalBulletRe = regexp.MustCompile("(?m)^[ \\t]+[-*][ \\t]+`([a-z][a-z0-9_]*)`[ \\t]+[—–-]")

	// reasonRefRe captures `reason: "<value>"` or reason: `<value>` —
	// the two forms the spec uses when referencing a reason value
	// inside an event payload literal.
	reasonRefRe = regexp.MustCompile("reason:[ \\t]*(?:\"([a-z][a-z0-9_]*)\"|`([a-z][a-z0-9_]*)`)")
)

// readDependsOn extracts spec IDs (zero-padded to 3 digits) from the
// `depends_on:` YAML list in the frontmatter.
func readDependsOn(body string) []string {
	m := frontmatterRe.FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	var deps []string
	inDeps := false
	for _, ln := range strings.Split(m[1], "\n") {
		trimmed := strings.TrimRight(ln, " \t\r")
		if !inDeps {
			if strings.HasPrefix(strings.TrimLeft(trimmed, " \t"), "depends_on:") {
				inDeps = true
			}
			continue
		}
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, " ") && !strings.HasPrefix(trimmed, "\t") {
			break
		}
		body := strings.TrimSpace(trimmed)
		if !strings.HasPrefix(body, "-") {
			break
		}
		val := strings.TrimSpace(strings.TrimPrefix(body, "-"))
		val = strings.Trim(val, `"'`)
		if n, err := strconv.Atoi(val); err == nil {
			deps = append(deps, fmt.Sprintf("%03d", n))
		}
	}
	return deps
}

// resolveDepSpecPath returns the spec.md path for a depends_on id, or
// "" if the dir is missing or ambiguous (L02 is responsible for
// flagging both cases).
func resolveDepSpecPath(specsRoot, id string) string {
	matches, err := filepath.Glob(filepath.Join(specsRoot, id+"-*"))
	if err != nil || len(matches) != 1 {
		return ""
	}
	p := filepath.Join(matches[0], "spec.md")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// collectCanonicalReasons walks FR blocks in body and adds each
// bullet-listed reason identifier from any block whose header line
// mentions both "canonical" and "reason" (case-insensitive).
func collectCanonicalReasons(body string, dst map[string]struct{}) {
	headers := frHeaderRe.FindAllStringIndex(body, -1)
	for i, m := range headers {
		blockStart := m[0]
		blockEnd := len(body)
		if i+1 < len(headers) {
			blockEnd = headers[i+1][0]
		}
		block := body[blockStart:blockEnd]
		headerLineEnd := strings.IndexByte(block, '\n')
		headerLine := block
		if headerLineEnd >= 0 {
			headerLine = block[:headerLineEnd]
		}
		if !containsFold(headerLine, "canonical") || !containsFold(headerLine, "reason") {
			continue
		}
		for _, b := range canonicalBulletRe.FindAllStringSubmatch(block, -1) {
			dst[b[1]] = struct{}{}
		}
	}
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// scanReasonReferences emits one violation per `reason:` reference
// whose value is absent from canonical.
func scanReasonReferences(file, body string, canonical map[string]struct{}) []Violation {
	if body == "" {
		return nil
	}
	var out []Violation
	for _, m := range reasonRefRe.FindAllStringSubmatchIndex(body, -1) {
		val := ""
		switch {
		case m[2] >= 0:
			val = body[m[2]:m[3]]
		case m[4] >= 0:
			val = body[m[4]:m[5]]
		}
		if val == "" {
			continue
		}
		if _, ok := canonical[val]; ok {
			continue
		}
		line := 1 + strings.Count(body[:m[0]], "\n")
		out = append(out, Violation{
			Rule:     "L06",
			File:     file,
			Line:     line,
			Severity: "error",
			Message:  fmt.Sprintf("reason %q is not declared by any canonical FR-NNN reason taxonomy in this spec or its depends_on", val),
		})
	}
	return out
}
