// Package speclint implements deterministic spec-PR linter rules for
// spec 115. This file implements rule L05 — CLI surface check.
//
// L05 enforces two invariants over spec.md prose and code samples:
//
//  1. Every `gh api <path>` invocation MUST address a path under
//     `repos/<owner>/<repo>/...`. The spec-PR #1050 incident (referenced
//     in spec 115's Why section) shipped `gh api
//     /pulls/N/comments/M/replies` — an endpoint that returns 404 —
//     because no mechanical check caught it.
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
	// reGhAPI captures the argument tail after `gh api`. Path-token
	// extraction happens in the body so multi-flag invocations like
	// `gh api -X POST repos/...` still find the real path.
	reGhAPI = regexp.MustCompile(`gh\s+api\b([^\n]*)`)

	// reChitinCLI matches `chitin-orchestrator <sub>` and
	// `chitin-kernel <sub>` where `<sub>` is a lower-case subcommand
	// token (skips flags like `--refresh-allowlist`).
	reChitinCLI = regexp.MustCompile(`chitin-(orchestrator|kernel)\s+([a-z][a-z0-9-]*)`)

	// reFRHeader matches the inline `**FR-NNN**` bullet marker that
	// starts a functional-requirement body.
	reFRHeader = regexp.MustCompile(`\*\*FR-\d{3}\*\*`)

	// reSectionHdr matches a markdown ATX section header line, used to
	// terminate an FR body when a new section starts before the next
	// FR.
	reSectionHdr = regexp.MustCompile(`^#{1,6}\s`)
)

// RunL05 evaluates the CLI surface check against the given spec content
// and returns one Violation per offending mention.
func RunL05(in L05Input) ([]Violation, error) {
	allow, err := loadAllowlist(in.AllowlistPath)
	if err != nil {
		return nil, fmt.Errorf("L05: load allowlist: %w", err)
	}
	intro := collectIntroducedSubcommands(in.SpecContent)

	var violations []Violation
	scanner := bufio.NewScanner(bytes.NewReader(in.SpecContent))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		for _, m := range reGhAPI.FindAllStringSubmatch(line, -1) {
			path := firstPathToken(m[1])
			if path == "" {
				continue
			}
			normalized := strings.TrimLeft(path, "/")
			if !strings.HasPrefix(normalized, "repos/") {
				violations = append(violations, Violation{
					Rule:     RuleL05,
					File:     in.SpecFile,
					Line:     lineNo,
					Severity: SeverityError,
					Message: fmt.Sprintf(
						"gh api path %q must start with 'repos/<owner>/<repo>/...'",
						path,
					),
				})
			}
		}

		for _, m := range reChitinCLI.FindAllStringSubmatch(line, -1) {
			binary := "chitin-" + m[1]
			sub := m[2]
			key := binary + " " + sub
			if allow[key] || intro[key] {
				continue
			}
			violations = append(violations, Violation{
				Rule:     RuleL05,
				File:     in.SpecFile,
				Line:     lineNo,
				Severity: SeverityError,
				Message: fmt.Sprintf(
					"CLI %q is not in .specify/known-cli-surfaces.txt and is not introduced by an FR-NNN in this spec",
					key,
				),
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("L05: scan spec: %w", err)
	}
	return violations, nil
}

// firstPathToken returns the first whitespace-separated token in rest
// that looks like an API path — i.e. contains a `/`. Tokens are
// stripped of surrounding quote/backtick characters. Returns "" if no
// path-shaped token is present (e.g. `gh api ...` template prose).
func firstPathToken(rest string) string {
	for _, tok := range strings.Fields(rest) {
		tok = strings.Trim(tok, "`'\".,;:)")
		tok = strings.TrimLeft(tok, "(`'\"")
		if strings.HasPrefix(tok, "-") {
			continue
		}
		if strings.Contains(tok, "/") {
			return tok
		}
	}
	return ""
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
