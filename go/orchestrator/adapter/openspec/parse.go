package openspec

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
)

// delta is one requirement change parsed from an OpenSpec spec delta file.
type delta struct {
	// kind is the change-kind — "ADDED", "MODIFIED", or "REMOVED" (FR-007).
	kind string
	// area is the capability area the delta lives under — the delta file's
	// directory name (e.g. "auth"), used as a stable ordering and grouping
	// key.
	area string
	// title is the requirement's title — the `### Requirement:` heading text.
	title string
	// body is the requirement's body text, used for capability mapping.
	body string
	// lineNo is the 1-based line the requirement heading sat on.
	lineNo int
}

var (
	// sectionRe matches an OpenSpec delta section header:
	// "## ADDED Requirements", "## MODIFIED Requirements",
	// "## REMOVED Requirements".
	sectionRe = regexp.MustCompile(`^##\s+(ADDED|MODIFIED|REMOVED)\s+Requirements\b`)
	// requirementRe matches a requirement heading within a section:
	// "### Requirement: Users can log in".
	requirementRe = regexp.MustCompile(`^###\s+Requirement:\s*(.+?)\s*$`)
)

// parseDeltas parses one OpenSpec spec delta file and returns its requirement
// changes (FR-006, FR-007). file is the repo-relative path, used to label any
// *adapter.MalformedArtifactError. The capability area is taken from the
// delta file's parent directory name.
//
// parseDeltas fails — returning nil — when the file declares a change section
// with no requirements under it, or a requirement heading outside any
// section: a malformed delta must never compile to a partial DAG (FR-010). A
// file with no change sections at all is not malformed — it simply yields no
// deltas, and the caller decides whether the change as a whole is empty.
func parseDeltas(file, content string) ([]delta, error) {
	area := filepath.Base(filepath.Dir(file))
	if area == "." || area == string(filepath.Separator) {
		area = "spec"
	}

	var deltas []delta
	currentKind := ""
	sectionLine := 0
	sectionCount := 0
	var current *delta
	var bodyLines []string

	flush := func() {
		if current != nil {
			current.body = strings.TrimSpace(strings.Join(bodyLines, "\n"))
			deltas = append(deltas, *current)
			current = nil
			bodyLines = nil
		}
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lineNo := i + 1

		if m := sectionRe.FindStringSubmatch(line); m != nil {
			flush()
			if currentKind != "" && sectionCount == 0 {
				return nil, &adapter.MalformedArtifactError{
					File: file, Line: sectionLine,
					Reason: "change section declares no `### Requirement:` entries",
				}
			}
			currentKind = m[1]
			sectionLine = lineNo
			sectionCount = 0
			continue
		}

		if m := requirementRe.FindStringSubmatch(line); m != nil {
			if currentKind == "" {
				return nil, &adapter.MalformedArtifactError{
					File: file, Line: lineNo,
					Reason: "`### Requirement:` heading is not inside an ADDED/MODIFIED/REMOVED section",
				}
			}
			flush()
			sectionCount++
			current = &delta{
				kind: currentKind, area: area,
				title: m[1], lineNo: lineNo,
			}
			continue
		}

		if current != nil {
			bodyLines = append(bodyLines, line)
		}
	}
	flush()

	if currentKind != "" && sectionCount == 0 {
		return nil, &adapter.MalformedArtifactError{
			File: file, Line: sectionLine,
			Reason: "change section declares no `### Requirement:` entries",
		}
	}
	return deltas, nil
}
