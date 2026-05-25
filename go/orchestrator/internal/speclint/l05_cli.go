// Package speclint implements the deterministic spec linter described in
// spec 115 — a per-rule, pure-function lint suite that the
// `chitin-orchestrator spec-lint` command runs against a spec.md / tasks.md
// pair and posts as PR review comments.
//
// Each rule lives in its own file (l01_frontmatter.go, l02_cross_refs.go,
// l03_task_fr.go, l04_events.go, l05_cli.go, l06_reason.go, l07_us_test.go)
// and returns []Finding. The shared Finding / Severity types and the
// allowlist parser live here in l05_cli.go because L05 is the first rule
// to need them; they are part of the package's exported API so that
// callers (e.g. cmd/spec-lint) can construct allowlists and consume
// findings, and so that sibling rule files in the same package may
// freely use them.
package speclint

import (
	"regexp"
	"strings"
)

// Severity classifies a finding for downstream gating. Mirrors the
// `severity` field in the JSON output of `chitin-orchestrator spec-lint`
// (spec 115 FR-003: structured JSON of {rule, file, line, severity, message}).
type Severity string

const (
	// SeverityError is a gate-failing violation: the iteration loop
	// must address it before merge.
	SeverityError Severity = "error"
	// SeverityWarning is informational; surfaced but does not block.
	SeverityWarning Severity = "warning"
)

// Finding is one violation surfaced by a lint rule. Field tags match
// the FR-003 output contract: [{rule, file, line, severity, message}].
type Finding struct {
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// AllowlistEntry is one (binary, subcommand) pair from
// .specify/known-cli-surfaces.txt.
type AllowlistEntry struct {
	Binary     string
	Subcommand string
}

// ParseCLIAllowlist parses the contents of .specify/known-cli-surfaces.txt.
// Format: one entry per line, `<binary> <subcommand>`, whitespace-separated.
// Blank lines and `#` comments are ignored.
func ParseCLIAllowlist(content string) []AllowlistEntry {
	var out []AllowlistEntry
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out = append(out, AllowlistEntry{Binary: fields[0], Subcommand: fields[1]})
	}
	return out
}

// Regexes used by L05. All anchored on backticks to avoid catching
// the binary names as prose nouns ("the chitin-orchestrator service").
var (
	// `gh api ...` — capture every argument inside the backticked
	// span. The path is then extracted by walking the tokens and
	// skipping known flag forms (see extractGhAPIPath).
	reGhAPIBlock = regexp.MustCompile("`gh api\\s+([^`]*)`")

	// `chitin-orchestrator <sub>` / `chitin-kernel <sub>` — subcommand
	// is a lowercase, dash-separated word. Flags (`--help`) are excluded
	// by the leading [a-z] character class.
	reChitinOrchSub = regexp.MustCompile("`chitin-orchestrator\\s+([a-z][a-z0-9-]*)")
	reChitinKernSub = regexp.MustCompile("`chitin-kernel\\s+([a-z][a-z0-9-]*)")

	// FR-NNN body delimiter — start of an FR-NNN paragraph in spec.md.
	reFRMarker = regexp.MustCompile(`\*\*FR-\d{3}\*\*`)

	// Section heading at start of line — terminates an FR body if it
	// appears before the next FR-NNN marker.
	reSectionHead = regexp.MustCompile(`(?m)^#{1,6}\s`)
)

// ghAPIValueFlags is the set of `gh api` flags that consume the next
// argument as their value. Anything not in this set is treated as a
// boolean flag (single token) when its name starts with `-`.
var ghAPIValueFlags = map[string]bool{
	"-X": true, "--method": true,
	"-F": true, "--field": true,
	"-f": true, "--raw-field": true,
	"-H": true, "--header": true,
	"-q": true, "--jq": true,
	"-i": true, "--include": true,
	"-p": true, "--preview": true,
	"--hostname": true,
	"--input":    true,
	"--template": true,
	"--cache":    true,
}

// L05CLISurface runs rule L05 (CLI surface check) against a spec.md +
// tasks.md pair.
//
// It enforces two invariants per spec 115 FR-003:
//
//  1. Every `gh api <path>` reference uses the `repos/...` form (no
//     legacy un-scoped paths, no `/repos/...` with a leading slash, no
//     non-repos endpoints).
//
//  2. Every `chitin-orchestrator <sub>` / `chitin-kernel <sub>` reference
//     names a subcommand that is either (a) in the operator-curated
//     allowlist, or (b) introduced by an FR-NNN body of THIS spec.
//     Detection of (b) is heuristic: the subcommand appears inside an
//     FR-NNN body that also contains the word "subcommand".
//
// specMD / tasksMD are the file contents; specFile / tasksFile are the
// labels embedded in returned findings (typically "spec.md" / "tasks.md",
// or full paths the caller prefers to surface in PR comments).
//
// Returns one Finding per violation. Each finding has Rule="L05" and
// Severity=SeverityError. Returns nil if both files are empty.
func L05CLISurface(specFile, specMD, tasksFile, tasksMD string, allowlist []AllowlistEntry) []Finding {
	introduced := extractIntroducedSubcommands(specMD)
	allowed := allowlistSet(allowlist)

	var findings []Finding
	findings = append(findings, checkGhAPIPaths(specFile, specMD)...)
	findings = append(findings, checkGhAPIPaths(tasksFile, tasksMD)...)
	findings = append(findings, checkChitinSubcommands(specFile, specMD, allowed, introduced)...)
	findings = append(findings, checkChitinSubcommands(tasksFile, tasksMD, allowed, introduced)...)
	return findings
}

// checkGhAPIPaths flags any `gh api <path>` reference whose path does
// not start with `repos/`. Flag forms like `gh api -X POST repos/...`
// are handled by walking the captured args and skipping flag tokens
// before extracting the endpoint path.
func checkGhAPIPaths(file, text string) []Finding {
	if text == "" {
		return nil
	}
	var findings []Finding
	for _, m := range reGhAPIBlock.FindAllStringSubmatchIndex(text, -1) {
		// m[0] = full-match start (opening backtick), m[2]:m[3] = args after `gh api `.
		args := text[m[2]:m[3]]
		path := extractGhAPIPath(args)
		if path == "" {
			// No positional path token (e.g., the span was nothing but
			// flags). Not a violation L05 can speak to.
			continue
		}
		if strings.HasPrefix(path, "repos/") {
			continue
		}
		findings = append(findings, Finding{
			Rule:     "L05",
			File:     file,
			Line:     lineNumberAt(text, m[0]),
			Severity: SeverityError,
			Message:  "`gh api " + path + "` must use `repos/<owner>/<repo>/...` form",
		})
	}
	return findings
}

// extractGhAPIPath returns the first non-flag token in a `gh api`
// argument string. Tokens that begin with `-` are treated as flags;
// flags listed in ghAPIValueFlags also consume the following token as
// their value. Long-form `--flag=value` packs the value into one token
// and is skipped as a single flag. Returns "" if no positional path
// token is present.
func extractGhAPIPath(args string) string {
	tokens := strings.Fields(args)
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if strings.HasPrefix(t, "-") {
			if strings.Contains(t, "=") {
				// --flag=value form: value is part of the same token.
				continue
			}
			if ghAPIValueFlags[t] && i+1 < len(tokens) {
				i++ // consume the flag's value token
			}
			continue
		}
		return t
	}
	return ""
}

// checkChitinSubcommands flags references to chitin-orchestrator and
// chitin-kernel subcommands that are neither allowlisted nor introduced
// by an FR-NNN body of this spec.
func checkChitinSubcommands(file, text string, allowed, introduced map[string]bool) []Finding {
	if text == "" {
		return nil
	}
	var findings []Finding
	findings = append(findings, scanBinarySubs(file, text, "chitin-orchestrator", reChitinOrchSub, allowed, introduced)...)
	findings = append(findings, scanBinarySubs(file, text, "chitin-kernel", reChitinKernSub, allowed, introduced)...)
	return findings
}

// scanBinarySubs is the per-binary scan shared by chitin-orchestrator
// and chitin-kernel. Duplicate (binary, subcommand) references at
// different lines each emit their own finding so the iteration loop
// can address them in place.
func scanBinarySubs(file, text, binary string, re *regexp.Regexp, allowed, introduced map[string]bool) []Finding {
	var findings []Finding
	for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
		sub := text[m[2]:m[3]]
		key := binary + " " + sub
		if allowed[key] || introduced[key] {
			continue
		}
		findings = append(findings, Finding{
			Rule:     "L05",
			File:     file,
			Line:     lineNumberAt(text, m[0]),
			Severity: SeverityError,
			Message:  "`" + binary + " " + sub + "` is not in the CLI allowlist and is not introduced by an FR-NNN of this spec",
		})
	}
	return findings
}

// extractIntroducedSubcommands returns the set of (binary, subcommand)
// pairs that this spec.md declares as new. The heuristic: a subcommand
// reference appears inside an FR-NNN body that also contains the word
// "subcommand" (a strong signal the FR is defining the surface).
//
// Returned keys are "<binary> <subcommand>" strings.
func extractIntroducedSubcommands(specMD string) map[string]bool {
	out := map[string]bool{}
	if specMD == "" {
		return out
	}
	starts := reFRMarker.FindAllStringIndex(specMD, -1)
	if len(starts) == 0 {
		return out
	}
	for i, s := range starts {
		bodyStart := s[0]
		var bodyEnd int
		if i+1 < len(starts) {
			bodyEnd = starts[i+1][0]
		} else {
			bodyEnd = len(specMD)
		}
		body := specMD[bodyStart:bodyEnd]
		// Cap the body at the next section heading if one appears
		// before the next FR (e.g., end of the Functional Requirements
		// section).
		if cut := reSectionHead.FindStringIndex(body); cut != nil && cut[0] > 0 {
			body = body[:cut[0]]
		}
		if !strings.Contains(strings.ToLower(body), "subcommand") {
			continue
		}
		for _, sm := range reChitinOrchSub.FindAllStringSubmatch(body, -1) {
			out["chitin-orchestrator "+sm[1]] = true
		}
		for _, sm := range reChitinKernSub.FindAllStringSubmatch(body, -1) {
			out["chitin-kernel "+sm[1]] = true
		}
	}
	return out
}

// allowlistSet converts an AllowlistEntry slice into a set keyed by
// "<binary> <subcommand>".
func allowlistSet(entries []AllowlistEntry) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		out[e.Binary+" "+e.Subcommand] = true
	}
	return out
}

// lineNumberAt returns the 1-based line number containing the byte at
// offset in text.
func lineNumberAt(text string, offset int) int {
	if offset < 0 {
		return 1
	}
	if offset > len(text) {
		offset = len(text)
	}
	return strings.Count(text[:offset], "\n") + 1
}
