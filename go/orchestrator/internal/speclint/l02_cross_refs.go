package speclint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// CheckCrossRefs runs rule L02 (cross-spec refs resolve) against the spec at
// specDir. For every id listed under `depends_on:` and `related:` in the
// spec.md frontmatter, it globs `<specsRoot>/<id>-*` and emits an
// error-severity Violation when the number of matching directories is not
// exactly one — both zero matches (dangling reference) and >1 matches
// (ambiguous id) fail the rule.
//
// specDir is the absolute or repo-relative path to the spec being linted
// (e.g. ".specify/specs/115-spec-review-gate"). specsRoot is the directory
// holding all spec directories (e.g. ".specify/specs"); if empty, it is
// derived as filepath.Dir(specDir), which is the standard layout.
//
// The function is pure with respect to inputs aside from reading spec.md and
// statting matched sibling paths — no network, no mutation.
//
// Failure modes:
//   - spec.md does not exist or is unreadable: returned as a non-nil error;
//     the caller (the orchestrator command) decides whether that is a lint
//     failure or a hard error.
//   - spec.md has no YAML frontmatter, or the frontmatter is malformed: L02
//     defers to L01 (frontmatter complete) and returns an empty slice. L01
//     owns frontmatter validity; L02 only acts on what it can parse.
//   - depends_on / related keys are absent, empty, or have non-scalar
//     entries: each unparseable entry is silently skipped; entry-shape
//     validation is L01's job.
func CheckCrossRefs(specDir, specsRoot string) ([]Violation, error) {
	if specsRoot == "" {
		specsRoot = filepath.Dir(specDir)
	}
	specPath := filepath.Join(specDir, "spec.md")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("speclint L02: read %s: %w", specPath, err)
	}

	body, bodyStart, err := extractFrontmatter(string(raw))
	if err != nil {
		// Malformed frontmatter — L01's territory. L02 emits nothing.
		return nil, nil
	}
	if body == "" {
		return nil, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		// Same logic as malformed framing — L01 reports it.
		return nil, nil
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	mapping := doc.Content[0]

	var violations []Violation
	for _, key := range []string{"depends_on", "related"} {
		seq := findSequence(mapping, key)
		if seq == nil {
			continue
		}
		for _, item := range seq.Content {
			if item.Kind != yaml.ScalarNode {
				continue
			}
			id := strings.TrimSpace(item.Value)
			if id == "" {
				continue
			}
			// yaml.v3 reports 1-based lines within the body we parsed. The
			// body starts at spec.md line bodyStart, so the spec.md line of
			// this item is bodyStart + (item.Line - 1).
			line := bodyStart + item.Line - 1

			dirs, err := matchingSpecDirs(specsRoot, id)
			if err != nil {
				return nil, fmt.Errorf("speclint L02: glob %s/%s-*: %w", specsRoot, id, err)
			}
			switch len(dirs) {
			case 1:
				// Resolved cleanly.
			case 0:
				violations = append(violations, Violation{
					Rule:     "L02",
					File:     "spec.md",
					Line:     line,
					Severity: SeverityError,
					Message: fmt.Sprintf(
						"%s id %q does not resolve: no directory matching %s/%s-* found",
						key, id, specsRoot, id,
					),
				})
			default:
				names := make([]string, 0, len(dirs))
				for _, d := range dirs {
					names = append(names, filepath.Base(d))
				}
				violations = append(violations, Violation{
					Rule:     "L02",
					File:     "spec.md",
					Line:     line,
					Severity: SeverityError,
					Message: fmt.Sprintf(
						"%s id %q is ambiguous: %d directories match %s-* (%s) — each spec id must resolve to exactly one directory",
						key, id, len(dirs), id, strings.Join(names, ", "),
					),
				})
			}
		}
	}
	return violations, nil
}

// matchingSpecDirs returns the subdirectories of specsRoot whose names begin
// with "<id>-". Non-directory matches are filtered out. The slice is sorted
// lexically so the resulting Violation messages are deterministic across
// platforms (filepath.Glob is already documented to return lexical order,
// but the explicit sort makes the invariant local to this function).
func matchingSpecDirs(specsRoot, id string) ([]string, error) {
	pattern := filepath.Join(specsRoot, id+"-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, m := range matches {
		info, statErr := os.Stat(m)
		if statErr != nil {
			continue
		}
		if info.IsDir() {
			dirs = append(dirs, m)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// extractFrontmatter returns the body of the leading YAML frontmatter block
// (the text between the opening `---` on line 1 and the next `---` line) and
// the 1-based spec.md line at which that body begins.
//
// If the file does not start with `---` on its first non-empty line, the
// function returns ("", 0, nil) — there is simply no frontmatter, which is
// L01's problem, not L02's. If `---` opens a block that never closes, it
// returns a non-nil error so the caller knows the frontmatter is malformed.
func extractFrontmatter(content string) (string, int, error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", 0, nil
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			body := strings.Join(lines[1:i], "\n")
			// bodyStart = 2 because the body's first line sits at spec.md
			// line 2 (the opening --- is on line 1). The math doesn't depend
			// on i; it only depends on the opening marker's position.
			return body, 2, nil
		}
	}
	return "", 0, errors.New("frontmatter block has no closing `---`")
}

// findSequence returns the yaml.Node for the top-level mapping entry with the
// given key when that entry is a sequence; otherwise nil. It does not recurse
// — depends_on / related are top-level by spec.
func findSequence(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k, v := mapping.Content[i], mapping.Content[i+1]
		if k.Value == key && v.Kind == yaml.SequenceNode {
			return v
		}
	}
	return nil
}
