// Package speclint implements deterministic spec-PR linter rules for
// spec 115. This file implements rule L05 — CLI surface check.
//
// L05 enforces two invariants over spec.md prose and code samples:
//
//  1. Every `gh api <path>` invocation MUST address a path under
//     `repos/<owner>/<repo>/...`. The spec-PR #1050 incident (referenced
//     in spec 115's Why section) shipped `gh api
//     /pulls/N/comments/M/replies` — an endpoint that returns 404 —
//     because no mechanical check caught it. Detection walks across
//     line breaks so wrapped invocations (`gh api\n  /pulls/...`, both
//     markdown-wrapped prose and shell `\` continuations) are still
//     validated.
//
//  2. Every `chitin-orchestrator <sub>` and `chitin-kernel <sub>`
//     subcommand mentioned MUST be either in the curated allowlist at
//     `.specify/known-cli-surfaces.txt`, OR introduced by an FR-NNN of
//     this spec (heuristic: the same subcommand mention appears
//     somewhere inside an FR body). The latter clause lets a spec
//     introduce a new subcommand without first patching the allowlist.
package speclint

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// RuleL05 is the rule identifier emitted in Violation.Rule.
const RuleL05 = "L05"

// Severity classifies a Violation's gate impact. `error` blocks
// iteration, `warning` is informational. Defined locally here so this
// rule compiles standalone; a follow-up consolidation will move
// Violation/Severity to a shared lint.go.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Violation is the structured shape every L0N rule emits, matching
// FR-003's JSON output contract.
type Violation struct {
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// L05Input bundles the parameters for RunL05.
type L05Input struct {
	// SpecFile is the path reported in Violation.File. Usually a
	// repo-relative path like "specs/115-spec-review-gate/spec.md".
	SpecFile string
	// SpecContent is the raw bytes of spec.md.
	SpecContent []byte
	// AllowlistPath points at `.specify/known-cli-surfaces.txt`. Empty
	// or non-existent means no allowlist; introduced-by-FR is still
	// honoured.
	AllowlistPath string
}

var (
	// reGhAPILoc locates every `gh api` occurrence in the full content.
	// Path-token extraction then walks forward across line breaks so a
	// wrapped invocation (the #1050 incident pattern) is validated even
	// when the endpoint sits on a subsequent line.
	reGhAPILoc = regexp.MustCompile(`gh\s+api\b`)

	// reChitinCLI matches `chitin-orchestrator <sub>` and
	// `chitin-kernel <sub>` where `<sub>` is a lower-case subcommand
	// token (skips flags like `--refresh-allowlist`). Uses `[ \t]+`
	// rather than `\s+` so it never spans a line break — keeps the
	// match per-mention now that the rule scans the full content.
	reChitinCLI = regexp.MustCompile(`chitin-(orchestrator|kernel)[ \t]+([a-z][a-z0-9-]*)`)

	// reFRHeader matches the inline `**FR-NNN**` bullet marker that
	// starts a functional-requirement body.
	reFRHeader = regexp.MustCompile(`\*\*FR-\d{3}\*\*`)

	// reSectionHdr matches a markdown ATX section header line, used to
	// terminate an FR body when a new section starts before the next
	// FR.
	reSectionHdr = regexp.MustCompile(`^#{1,6}\s`)
)

// RunL05 evaluates the CLI surface check against the given spec content
// and returns one Violation per offending mention, ordered by line.
func RunL05(in L05Input) ([]Violation, error) {
	allow, err := loadAllowlist(in.AllowlistPath)
	if err != nil {
		return nil, fmt.Errorf("L05: load allowlist: %w", err)
	}
	intro := collectIntroducedSubcommands(in.SpecContent)

	s := string(in.SpecContent)
	var violations []Violation

	// gh api: walk the full content so wrapped invocations across line
	// breaks (the #1050 incident) are caught, not silently skipped.
	for _, m := range reGhAPILoc.FindAllStringIndex(s, -1) {
		path, pathOffset := findGhAPIPathToken(s[m[1]:])
		if path == "" {
			continue
		}
		normalized := strings.TrimLeft(path, "/")
		if strings.HasPrefix(normalized, "repos/") {
			continue
		}
		absOffset := m[1] + pathOffset
		violations = append(violations, Violation{
			Rule:     RuleL05,
			File:     in.SpecFile,
			Line:     lineNumberAt(s, absOffset),
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"gh api path %q must start with 'repos/<owner>/<repo>/...'",
				path,
			),
		})
	}

	for _, m := range reChitinCLI.FindAllStringSubmatchIndex(s, -1) {
		binary := "chitin-" + s[m[2]:m[3]]
		sub := s[m[4]:m[5]]
		key := binary + " " + sub
		if allow[key] || intro[key] {
			continue
		}
		violations = append(violations, Violation{
			Rule:     RuleL05,
			File:     in.SpecFile,
			Line:     lineNumberAt(s, m[0]),
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"CLI %q is not in .specify/known-cli-surfaces.txt and is not introduced by an FR-NNN in this spec",
				key,
			),
		})
	}

	sort.SliceStable(violations, func(i, j int) bool {
		return violations[i].Line < violations[j].Line
	})
	return violations, nil
}

// findGhAPIPathToken returns the first path-shaped token (one
// containing '/') appearing after a `gh api` invocation, along with
// its byte offset into rest. It walks across line breaks so both
// markdown-wrapped prose and shell `\`-continuation invocations are
// validated, and stops at a blank line, a markdown header, a code
// fence, or another `gh ` invocation — boundaries that mean "this gh
// api had no path argument."
func findGhAPIPathToken(rest string) (string, int) {
	const maxScan = 2048
	end := len(rest)
	if end > maxScan {
		end = maxScan
	}
	lineIdx := 0
	for i := 0; i < end; {
		nl := strings.IndexByte(rest[i:end], '\n')
		var line string
		var nextI int
		if nl < 0 {
			line = rest[i:end]
			nextI = end
		} else {
			line = rest[i : i+nl]
			nextI = i + nl + 1
		}
		if lineIdx > 0 {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" ||
				strings.HasPrefix(trimmed, "#") ||
				strings.HasPrefix(trimmed, "```") ||
				strings.HasPrefix(trimmed, "gh ") {
				return "", 0
			}
		}
		scanLine := strings.TrimRight(line, " \t\\")
		for _, tok := range strings.Fields(scanLine) {
			cleaned := cleanGhToken(tok)
			if cleaned == "" || strings.HasPrefix(cleaned, "-") {
				continue
			}
			if !strings.Contains(cleaned, "/") {
				continue
			}
			tokRel := strings.Index(rest[i:nextI], cleaned)
			if tokRel < 0 {
				tokRel = 0
			}
			return cleaned, i + tokRel
		}
		i = nextI
		lineIdx++
	}
	return "", 0
}

// cleanGhToken strips quote/backtick punctuation that commonly wraps
// path tokens in markdown prose.
func cleanGhToken(tok string) string {
	tok = strings.Trim(tok, "`'\".,;:)")
	tok = strings.TrimLeft(tok, "(`'\"")
	return tok
}

// lineNumberAt returns the 1-based line number of byteOffset in s.
func lineNumberAt(s string, byteOffset int) int {
	if byteOffset > len(s) {
		byteOffset = len(s)
	}
	return strings.Count(s[:byteOffset], "\n") + 1
}

// loadAllowlist parses `.specify/known-cli-surfaces.txt`. Each
// non-blank, non-`#` line is expected to be `<binary> <subcommand>`
// (e.g. `chitin-orchestrator spec-lint`). Returns an empty map when
// path is empty or the file does not exist — those cases fall back to
// the introduced-by-FR heuristic alone.
func loadAllowlist(path string) (map[string]bool, error) {
	out := map[string]bool{}
	if path == "" {
		return out, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
	}
	return out, nil
}

// collectIntroducedSubcommands walks the spec content and returns the
// set of `chitin-(orchestrator|kernel) <sub>` keys that appear inside
// an FR-NNN body. An FR body begins at a `**FR-NNN**` marker and ends
// at the next `**FR-NNN**` marker OR a markdown section header,
// whichever comes first.
func collectIntroducedSubcommands(content []byte) map[string]bool {
	out := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var body strings.Builder
	inFR := false
	flush := func() {
		if !inFR {
			return
		}
		for _, m := range reChitinCLI.FindAllStringSubmatch(body.String(), -1) {
			out["chitin-"+m[1]+" "+m[2]] = true
		}
		body.Reset()
		inFR = false
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case reFRHeader.MatchString(line):
			flush()
			inFR = true
			body.WriteString(line)
			body.WriteByte('\n')
		case reSectionHdr.MatchString(line):
			flush()
		default:
			if inFR {
				body.WriteString(line)
				body.WriteByte('\n')
			}
		}
	}
	flush()
	return out
}
