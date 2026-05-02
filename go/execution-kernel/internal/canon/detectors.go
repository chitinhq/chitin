package canon

import (
	"regexp"
	"strings"
)

// Bypass detectors: structured predicates over a parsed Pipeline / Command
// that callers (gov.classifyShellCommand) use to identify the dangerous-
// pattern *class* rather than match against a raw command string. Closing
// the bypass surface for issues #58, #59, #60, #61, #62.
//
// Invariants:
//   - Each detector takes ONLY the canonical Command/Pipeline (no raw cmd
//     string except where the AST loses information — heredocs, redirects).
//   - A True result means "the canonical form of this command matches the
//     pattern class," not "this exact spelling matches a regex." The caller
//     pairs the True result with a closed-enum action type for policy.
//   - Each detector covers a class, not a literal: variations in whitespace,
//     quoting, env-prefix, leading flags must all yield the same answer.
//
// Known limits (filed as future work, see swarm-backlog `canon-ast-upgrade`):
//   - Subshell descent `(rm -rf /)` — current canon splitter doesn't recurse.
//   - Process substitution `bash <(curl ...)` — tokens get mangled; partial
//     coverage via ContainsProcSubstFetch at the raw-string level.
//   - Command substitution `$(cmd)` — same as subshell.
//   - Heredoc destinations `cat > path <<EOF` — partial via WriteDestinations.

// IsRecursiveDelete reports whether this command, when executed, would
// invoke rm with a recursive flag — regardless of flag spelling, whitespace,
// quoting, or splitting.
//
// Closes #58. Catches:
//   `rm -rf X`, `rm  -rf X` (multi-space), `rm '-rf' X` (quoted),
//   `rm -r -f X` (split flags), `rm --recursive --force X` (long form),
//   `rm -fr X` (reordered).
func IsRecursiveDelete(c Command) bool {
	if c.Tool != "rm" {
		return false
	}
	for flag := range c.Flags {
		// Long-form recursive flag.
		if flag == "recursive" || flag == "R" {
			return true
		}
		// Short-form: any flag whose name contains 'r' (handles -r, -rf,
		// -fr, -rfv etc. — combined-short-flag expansion already split
		// these in parseOne, so each individual flag entry is one char).
		if len(flag) == 1 && (flag == "r") {
			return true
		}
	}
	return false
}

// IsBareGitPush reports whether this command is `git push` with neither an
// explicit branch arg nor an explicit `HEAD:` ref — meaning the destination
// is implicit (relies on upstream-tracking config). Without resolving the
// current branch, gov can't verify protected-branch status, so the safer
// stance is to treat as suspicious.
//
// Closes #60. Catches:
//   `git push`, `git push origin`, `git push -u origin HEAD`,
//   `git push --set-upstream`, `git -C /tmp push`.
func IsBareGitPush(c Command) bool {
	if c.Tool != "git" || c.Action != "push" {
		return false
	}
	// Explicit branch refspec — `HEAD:branch` or `localBranch:remoteBranch`.
	// The colon is the disambiguator: it forces an explicit destination.
	for _, a := range c.Args {
		if strings.Contains(a, ":") {
			return false
		}
	}
	// At least 2 positional args means [remote, branch]. Bare `HEAD`
	// (without a colon) is implicit — it relies on upstream-tracking
	// config, so still bare from a verifiability standpoint.
	if len(c.Args) >= 2 && c.Args[1] != "HEAD" {
		return false
	}
	return true
}

// IsInfraDestroy reports whether this command invokes a destructive infra
// operation (terraform destroy, kubectl delete ns/namespace) — with leading
// global flags or env-prefix already stripped by canon.
//
// Closes #62. Catches:
//   `terraform destroy`, `terraform -chdir=./infra destroy`,
//   `env TF_LOG=1 terraform destroy`,
//   `kubectl delete ns x`, `kubectl --context=prod delete ns x`,
//   `KUBECONFIG=/tmp/k kubectl delete namespace x`.
func IsInfraDestroy(c Command) (tool string, ok bool) {
	if c.Tool == "terraform" && c.Action == "destroy" {
		return "terraform", true
	}
	if c.Tool == "kubectl" && c.Action == "delete" {
		// First positional after `delete` should be ns or namespace.
		if len(c.Args) >= 1 && (c.Args[0] == "ns" || c.Args[0] == "namespace") {
			return "kubectl", true
		}
	}
	return "", false
}

// WriteDestinations extracts the destination path(s) of any shell command
// whose effect is to write a file — regardless of whether the write is
// expressed as a redirect (>, >>), a tee, or a copy/move (cp, mv).
//
// Operates on the raw command string because canon's tokenizer treats `>`
// as a positional, losing the redirect-vs-arg distinction. For governance
// it doesn't matter that the parse is regex-based: we only need the
// destination path for self-mod rule matching.
//
// Closes #59 (with the caller mapping each destination back through the
// self-mod target_regex). Catches:
//   `echo X > path`, `cat >> path`, `tee path`, `tee -a path`,
//   `cp src path`, `mv src path`, heredoc destinations
//   `cat > path <<EOF`.
func WriteDestinations(raw string) []string {
	var dests []string
	// Redirect: > or >> followed by optional whitespace then a path token.
	// The (?:^|[^>0-9]) prefix guards against `2>&1` (fd redirect) and
	// `>>` (caught by separate alt). Capture: not-whitespace-not-redirect-
	// not-pipe-not-semicolon.
	for _, m := range reRedirect.FindAllStringSubmatch(raw, -1) {
		dest := unquote(strings.TrimSpace(m[1]))
		if dest != "" && !strings.HasPrefix(dest, "&") {
			dests = append(dests, dest)
		}
	}
	// tee [-a] path
	for _, m := range reTee.FindAllStringSubmatch(raw, -1) {
		dest := unquote(strings.TrimSpace(m[1]))
		if dest != "" {
			dests = append(dests, dest)
		}
	}
	// cp/mv (last positional after flags). Conservative regex: matches
	// only the cp/mv at command-position (start of line or after &&/||/;/|).
	for _, m := range reCpMv.FindAllStringSubmatch(raw, -1) {
		dest := unquote(strings.TrimSpace(m[2]))
		if dest != "" {
			dests = append(dests, dest)
		}
	}
	return dests
}

// IsRemoteCodeExec reports whether this pipeline fetches a script from the
// network and then pipes/sources it through a shell — the curl|bash class.
//
// Closes #61 for the pipe form. Process-substitution `bash <(curl ...)` is
// detected via ContainsProcSubstFetch (which inspects the raw string)
// because canon's tokenizer mangles the `<(curl` prefix.
//
// Catches:
//   `curl URL | bash`, `curl URL | sh`, `wget -qO- URL | bash`,
//   `wget URL | zsh`, plus the two-stage form
//   `curl URL -o /tmp/x.sh && bash /tmp/x.sh` (caught via && between a
//   network-fetch segment and a bash segment).
func IsRemoteCodeExec(p Pipeline) bool {
	// Pipe / && form, both orderings:
	//   - fetch | bash    (segments: fetch, bash with Op=Pipe)
	//   - bash <(fetch)   (segments: bash, fetch with Op=Pipe — AST emits
	//                      the proc-subst inner as Pipe-connected after
	//                      the launcher)
	// We accept either ordering: any adjacent (fetch, shell-launcher)
	// pair connected by Pipe / && / || is suspicious. A non-adjacent
	// fetch+launcher in the same pipeline isn't enough — must be
	// directly connected.
	for i := 1; i < len(p.Segments); i++ {
		seg := p.Segments[i]
		prev := p.Segments[i-1]
		if seg.Op != OpPipe && seg.Op != OpAnd && seg.Op != OpOr {
			continue
		}
		if isShellLauncher(seg.Command.Tool) && isNetworkFetch(prev.Command.Tool) {
			return true
		}
		if isNetworkFetch(seg.Command.Tool) && isShellLauncher(prev.Command.Tool) {
			return true
		}
	}
	// Two-stage form: any segment is `bash /path/to/script.sh` after a
	// network fetch with `-o /tmp/x.sh` earlier in the chain.
	hasNetworkFetchToFile := false
	for _, seg := range p.Segments {
		if isNetworkFetch(seg.Command.Tool) {
			if _, ok := seg.Command.Flags["output"]; ok {
				hasNetworkFetchToFile = true
			}
			if _, ok := seg.Command.Flags["o"]; ok {
				hasNetworkFetchToFile = true
			}
		}
		if hasNetworkFetchToFile && isShellLauncher(seg.Command.Tool) && len(seg.Command.Args) > 0 {
			return true
		}
	}
	return false
}

// ContainsProcSubstFetch reports whether the raw string contains a process-
// substitution fetch pattern: `bash <(curl ...)`, `sh <(wget ...)`. This is
// the gap canon's tokenizer can't close (it mangles `<(curl` into one
// token); regex over the raw input catches it as a v1-grade band-aid.
// A future canon-AST upgrade (mvdan/sh) will subsume this detector.
//
// Closes the proc-subst variant of #61.
func ContainsProcSubstFetch(raw string) bool {
	return reProcSubstFetch.MatchString(raw)
}

func isShellLauncher(tool string) bool {
	switch tool {
	case "bash", "sh", "zsh", "ash", "dash":
		return true
	}
	return false
}

func isNetworkFetch(tool string) bool {
	switch tool {
	case "curl", "wget", "fetch":
		return true
	}
	return false
}

// unquote strips a single layer of matched single OR double quotes.
// Idempotent on already-unquoted strings.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
		return s[1 : len(s)-1]
	}
	return s
}

var (
	// reRedirect captures the destination of `>` or `>>`. Negative lookahead
	// for digits (Go regex doesn't support lookahead, so we use a positive
	// alternation) handles `2>&1` style fd redirects: the `&` prefix is
	// excluded in WriteDestinations after capture.
	reRedirect = regexp.MustCompile(`(?:^|[\s])>>?\s*([^\s|;&<>]+)`)
	// reTee captures the path arg of `tee` (with optional flags between).
	reTee = regexp.MustCompile(`\btee\b(?:\s+-\S+)*\s+([^\s|;&<>]+)`)
	// reCpMv captures the destination (last positional) of `cp`/`mv` at
	// command position. Conservative: requires cp/mv at start of segment.
	reCpMv = regexp.MustCompile(`(?:^|[;&|]\s*)(cp|mv)\b(?:\s+-\S+)*\s+\S+\s+([^\s|;&<>]+)`)
	// reProcSubstFetch matches `bash <(curl|wget ...)`-style process
	// substitution. Conservative: only the launcher-immediately-before
	// proc-subst pattern.
	reProcSubstFetch = regexp.MustCompile(`\b(?:bash|sh|zsh)\s*<\(\s*(?:curl|wget|fetch)\b`)
)
