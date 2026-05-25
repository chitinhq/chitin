// Package speclint implements the deterministic spec PR linter rules (spec
// 115 FR-003). This file is rule L05 — the CLI surface check: every backticked
// `gh api <path>` must use the `repos/<owner>/<repo>/...` form, and every
// backticked `chitin-orchestrator <sub>` or `chitin-kernel <sub>` must either
// be in the operator-curated allowlist at `.specify/known-cli-surfaces.txt`
// or be introduced by an FR-NNN in this spec (heuristic: the FR body that
// names the CLI invocation also contains the word "subcommand").
package speclint

import (
	"fmt"
	"regexp"
	"strings"
)

// Severity tags a violation's gating level. Error-severity violations gate
// the iteration loop; warning-severity ones are informational (spec 115 edge
// cases).
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Violation is the structured output shape shared by every L0N rule (the
// JSON contract `spec-lint` emits per spec 115 FR-003 + T002).
type Violation struct {
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

var (
	// All three regexes anchor inside a leading backtick: chitin spec text
	// uses backticks for CLI references, and anchoring there both filters out
	// prose mentions ("the gh api endpoint") and gives a single place to put
	// the @-prefix exclusion for `@chitin-orchestrator iterate` notation.
	ghApiRE        = regexp.MustCompile("`gh\\s+api\\s+([^\\s`'\"\\)]+)")
	orchestratorRE = regexp.MustCompile("`(@?)chitin-orchestrator\\s+([a-z][a-z0-9-]*)")
	kernelRE       = regexp.MustCompile("`(@?)chitin-kernel\\s+([a-z][a-z0-9-]*)")

	// FR markers appear as `- **FR-NNN** ...` bullets at the start of a line.
	// `[ \t>*-]*` consumes leading bullet/blockquote chars without crossing
	// newlines, which a `\s` class would.
	frHeaderRE = regexp.MustCompile(`(?m)^[ \t>*-]*\*\*FR-(\d+)\*\*`)

	// Any ##/### heading terminates the current FR body even if no next FR
	// follows (e.g., last FR in a "Functional requirements" section).
	sectionHeaderRE = regexp.MustCompile(`(?m)^#{1,3}\s`)
)

// CheckL05 runs the CLI surface check against `spec.md` content. `file` is
// the path to surface in violations (typically "spec.md"); `allowlist` is the
// parsed contents of `.specify/known-cli-surfaces.txt` — pass nil if the file
// is missing or empty (the FR-introduction heuristic still applies).
func CheckL05(file, content string, allowlist []string) []Violation {
	allow := make(map[string]struct{}, len(allowlist))
	for _, e := range allowlist {
		if k := strings.TrimSpace(e); k != "" {
			allow[k] = struct{}{}
		}
	}

	introduced := introducedSubcommands(parseFRBlocks(content))

	var vs []Violation

	for _, ref := range findGhApiRefs(content) {
		if strings.HasPrefix(ref.path, "repos/") {
			continue
		}
		vs = append(vs, Violation{
			Rule:     "L05",
			File:     file,
			Line:     ref.line,
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"`gh api %s`: path must start with `repos/<owner>/<repo>/`",
				ref.path,
			),
		})
	}

	for _, ref := range findCliRefs(content, "chitin-orchestrator", orchestratorRE) {
		if v, ok := checkCliRef(ref, file, allow, introduced); ok {
			vs = append(vs, v)
		}
	}
	for _, ref := range findCliRefs(content, "chitin-kernel", kernelRE) {
		if v, ok := checkCliRef(ref, file, allow, introduced); ok {
			vs = append(vs, v)
		}
	}

	return vs
}

// ParseAllowlist parses `.specify/known-cli-surfaces.txt` contents into the
// list of "<binary> <subcommand>" entries CheckL05 expects. Blank lines and
// `#` comment lines are skipped; surrounding whitespace is trimmed.
func ParseAllowlist(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

type ghApiRef struct {
	line int
	path string
}

type cliRef struct {
	line       int
	binary     string
	subcommand string
	atPrefix   bool
}

func findGhApiRefs(content string) []ghApiRef {
	var refs []ghApiRef
	for _, m := range ghApiRE.FindAllStringSubmatchIndex(content, -1) {
		refs = append(refs, ghApiRef{
			line: lineAt(content, m[0]),
			path: content[m[2]:m[3]],
		})
	}
	return refs
}

func findCliRefs(content, binary string, re *regexp.Regexp) []cliRef {
	var refs []cliRef
	for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
		refs = append(refs, cliRef{
			line:       lineAt(content, m[0]),
			binary:     binary,
			subcommand: content[m[4]:m[5]],
			atPrefix:   m[3]-m[2] == 1, // capture group 1 length 1 means "@"
		})
	}
	return refs
}

func checkCliRef(ref cliRef, file string, allow map[string]struct{}, introduced map[string]bool) (Violation, bool) {
	if ref.atPrefix {
		return Violation{}, false
	}
	key := ref.binary + " " + ref.subcommand
	if _, ok := allow[key]; ok {
		return Violation{}, false
	}
	if introduced[key] {
		return Violation{}, false
	}
	return Violation{
		Rule:     "L05",
		File:     file,
		Line:     ref.line,
		Severity: SeverityError,
		Message: fmt.Sprintf(
			"`%s %s`: subcommand not in `.specify/known-cli-surfaces.txt` and not introduced by an FR-NNN in this spec",
			ref.binary, ref.subcommand,
		),
	}, true
}

type frBlock struct {
	id   string
	body string
}

// parseFRBlocks splits the spec into per-FR-NNN bodies. Each block runs from
// the FR marker line up to the next FR marker OR the next ##/### heading,
// whichever comes first — so an FR doesn't bleed into the next section.
func parseFRBlocks(content string) []frBlock {
	matches := frHeaderRE.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}
	headings := sectionHeaderRE.FindAllStringIndex(content, -1)

	var blocks []frBlock
	for i, m := range matches {
		start := m[0]
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		for _, h := range headings {
			if h[0] > start && h[0] < end {
				end = h[0]
				break
			}
		}
		blocks = append(blocks, frBlock{
			id:   "FR-" + content[m[2]:m[3]],
			body: content[start:end],
		})
	}
	return blocks
}

// introducedSubcommands returns the set of "<binary> <sub>" keys whose backticked
// CLI invocation appears inside an FR-NNN body that ALSO contains the word
// "subcommand" — the L05 heuristic for "this spec introduces this surface."
func introducedSubcommands(blocks []frBlock) map[string]bool {
	out := map[string]bool{}
	for _, b := range blocks {
		if !strings.Contains(strings.ToLower(b.body), "subcommand") {
			continue
		}
		for _, m := range orchestratorRE.FindAllStringSubmatch(b.body, -1) {
			if m[1] == "@" {
				continue
			}
			out["chitin-orchestrator "+m[2]] = true
		}
		for _, m := range kernelRE.FindAllStringSubmatch(b.body, -1) {
			if m[1] == "@" {
				continue
			}
			out["chitin-kernel "+m[2]] = true
		}
	}
	return out
}

func lineAt(content string, offset int) int {
	return 1 + strings.Count(content[:offset], "\n")
}
