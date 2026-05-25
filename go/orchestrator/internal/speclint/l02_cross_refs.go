// Package speclint hosts the deterministic consistency checks chitin runs
// against spec PRs (spec 115 FR-003). Each L0N rule is one file, returns
// the shared Violation type, and is pure: same inputs always produce the
// same []Violation, no network, no clock, no other process state.
package speclint

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Severity classifies a Violation. Only "error" gates iteration; "warning"
// is informational (spec 115 edge cases — "Linter has a bug and posts
// false positives"). L02 always emits "error" because a broken cross-ref
// means the spec literally points at nothing.
type Severity string

const (
	// SeverityError gates the iteration loop.
	SeverityError Severity = "error"
	// SeverityWarning is informational; the iteration loop does not gate on it.
	SeverityWarning Severity = "warning"
)

// Violation is one finding from a lint rule — the structured-JSON shape
// spec 115 FR-003 names as the linter's stdout contract.
type Violation struct {
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// listItemRe matches one item in the YAML block-list shape every chitin spec
// uses for depends_on: / related: — a hyphen, optional surrounding quotes,
// and an integer id. The id is captured as written so the message can
// quote it back to the operator. The whole-string anchor (^...$ with
// multiline) keeps inline garbage from being picked up as an id.
var listItemRe = regexp.MustCompile(`^\s*-\s*"?(\d+)"?\s*$`)

// keyRe matches a top-level YAML mapping key with NO value on the same
// line — i.e., the start of a block-style value (the only shape chitin's
// frontmatter uses for depends_on / related). Inline-list forms like
// `depends_on: [097, 113]` are intentionally not handled — no chitin spec
// uses them, and accepting them silently would hide drift.
var keyRe = regexp.MustCompile(`^([a-z_]+)\s*:\s*$`)

// crossRefRoots names the frontmatter keys L02 walks. Order matters: the
// emission tie-breaker is (key-order, source-line) — depends_on findings
// come before related findings even when interleaved in the file.
var crossRefRoots = []string{"depends_on", "related"}

// crossRef is one id reference pulled from the frontmatter, tagged with
// the key it came from and the line it appeared on so a violation can
// point precisely at it.
type crossRef struct {
	key  string
	id   string
	line int
}

// L02CrossRefs runs rule L02 — every id listed under depends_on: / related:
// in spec.md's frontmatter resolves to exactly one sibling spec directory
// under the parent of specDir. Zero matches is an error (the spec
// references a sibling that doesn't exist). More than one match is also
// an error (an ambiguous prefix — should never occur in a healthy specs
// root, but checked so the linter is precise about its own input).
//
// specDir is the directory holding this spec's spec.md (e.g.
// ".specify/specs/115-spec-review-gate"). Its parent is the specs root
// the glob runs against. The linter does not hard-code ".specify/specs/"
// so the same code works on this repo's "specs/" layout and on
// downstream consumers using the canonical path.
//
// A missing frontmatter is NOT an L02 violation — L01 owns that finding;
// reporting it twice would just spam the PR.
func L02CrossRefs(specDir string) ([]Violation, error) {
	specMD := filepath.Join(specDir, "spec.md")
	f, err := os.Open(specMD)
	if err != nil {
		return nil, fmt.Errorf("speclint L02: open %s: %w", specMD, err)
	}
	defer f.Close()

	refs := collectCrossRefs(f)
	if len(refs) == 0 {
		return nil, nil
	}

	specsRoot := filepath.Dir(specDir)
	out := make([]Violation, 0, len(refs))
	for _, ref := range refs {
		matches, err := filepath.Glob(filepath.Join(specsRoot, ref.id+"-*"))
		if err != nil {
			return nil, fmt.Errorf("speclint L02: glob for spec id %s: %w", ref.id, err)
		}
		switch len(matches) {
		case 1:
			// Resolved cleanly.
		case 0:
			out = append(out, Violation{
				Rule:     "L02",
				File:     "spec.md",
				Line:     ref.line,
				Severity: SeverityError,
				Message: fmt.Sprintf(
					"%s references spec %s, but no directory %s-* exists under %s",
					ref.key, ref.id, ref.id, specsRoot,
				),
			})
		default:
			sort.Strings(matches)
			rel := make([]string, len(matches))
			for i, m := range matches {
				rel[i] = filepath.Base(m)
			}
			out = append(out, Violation{
				Rule:     "L02",
				File:     "spec.md",
				Line:     ref.line,
				Severity: SeverityError,
				Message: fmt.Sprintf(
					"%s references spec %s, but %d directories match %s-* under %s: %s",
					ref.key, ref.id, len(matches), ref.id, specsRoot, strings.Join(rel, ", "),
				),
			})
		}
	}
	return out, nil
}

// collectCrossRefs walks the leading YAML frontmatter of a markdown spec
// and returns every id listed under crossRefRoots, tagged with its source
// key and line. It is a deliberately narrow parser — chitin frontmatter
// is always "key:\n  - 097" shape for these fields — and emits in
// (key-order, source-line) order so callers get a stable result.
//
// If the document has no leading frontmatter (no opening "---" delimiter)
// the result is empty. A frontmatter that opens but never closes is
// treated as if every subsequent line is still frontmatter; this matches
// the lenient behavior the rest of the linter takes when given malformed
// input (L01 is where shape problems get flagged).
func collectCrossRefs(r io.Reader) []crossRef {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		inFrontmatter bool
		sawOpen       bool
		currentKey    string
		byKey         = map[string][]crossRef{}
		lineNo        int
	)

	for sc.Scan() {
		lineNo++
		line := strings.TrimRight(sc.Text(), "\r")

		if !sawOpen {
			if line == "---" {
				sawOpen, inFrontmatter = true, true
				continue
			}
			// First non-empty, non-`---` line means there is no frontmatter
			// at all — bail out without recording anything.
			if strings.TrimSpace(line) != "" {
				return nil
			}
			continue
		}

		if !inFrontmatter {
			break
		}

		if line == "---" {
			inFrontmatter = false
			break
		}

		if m := keyRe.FindStringSubmatch(line); m != nil {
			currentKey = m[1]
			continue
		}

		if currentKey == "" {
			continue
		}

		if !isCrossRefKey(currentKey) {
			continue
		}

		if m := listItemRe.FindStringSubmatch(line); m != nil {
			byKey[currentKey] = append(byKey[currentKey], crossRef{
				key:  currentKey,
				id:   m[1],
				line: lineNo,
			})
			continue
		}

		// A non-indented line that wasn't another `key:` ends the value;
		// fall through and let the next iteration pick up the new key.
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			currentKey = ""
		}
	}

	var out []crossRef
	for _, key := range crossRefRoots {
		out = append(out, byKey[key]...)
	}
	return out
}

// isCrossRefKey reports whether key is one of the frontmatter list-keys
// L02 inspects. Linear scan because crossRefRoots is fixed at two entries.
func isCrossRefKey(key string) bool {
	for _, k := range crossRefRoots {
		if k == key {
			return true
		}
	}
	return false
}
