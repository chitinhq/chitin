// specref.go — spec-ref resolution for spec 097 `schedule` subcommand.
//
// Implements the three-tier resolution from
// specs/097-operator-scheduler-entrypoint/research.md D9:
//   1. Exact directory-name match  — repo/specs/<spec-ref>/  or  repo/.specify/specs/<spec-ref>/
//   2. Numeric prefix match        — the unique NNN-*/ that begins with <spec-ref>
//   3. Slug match                  — the unique *-<spec-ref>/ (slug-only form)
//
// Ambiguous matches and zero matches both return an error with a sorted
// candidate list so the operator can see what to type next.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SpecRefResolution is the parsed result of a successful spec-ref resolution.
// Per data-model.md Entity 2.
type SpecRefResolution struct {
	// SpecDir is the absolute path of the resolved spec directory.
	SpecDir string
	// SpecRef is the canonical form: the directory's basename
	// (e.g., "096-operator-session-state-surface").
	SpecRef string
	// Numeric is the leading-digits portion (e.g., "096"), empty if absent.
	Numeric string
	// Slug is the trailing-after-first-dash portion
	// (e.g., "operator-session-state-surface"), empty if absent.
	Slug string
}

// SpecRefError carries a structured failure from resolveSpecRef so the
// caller can render a stable stderr line per contract.
type SpecRefError struct {
	Kind       string   // "not-found", "ambiguous"
	Ref        string   // the input that failed to resolve
	Candidates []string // sorted spec directory names — for not-found this is "available specs", for ambiguous this is "matched specs"
}

func (e *SpecRefError) Error() string {
	switch e.Kind {
	case "not-found":
		return fmt.Sprintf("no spec matching ref %q", e.Ref)
	case "ambiguous":
		return fmt.Sprintf("ref %q is ambiguous — matched %d specs", e.Ref, len(e.Candidates))
	default:
		return fmt.Sprintf("specref: %s: %s", e.Kind, e.Ref)
	}
}

// specsRootRelatives is the set of directory layouts a spec-kit repo may use
// for its specs, ordered by preference. Matches go/orchestrator/adapter/speckit
// — the canonical .specify/specs/ first, then a bare specs/.
var specsRootRelatives = []string{
	filepath.Join(".specify", "specs"),
	"specs",
}

// numericPrefixRe matches the leading NNN- digits of a spec directory name.
var numericPrefixRe = regexp.MustCompile(`^(\d+)-`)

// resolveSpecRef finds the unique spec directory in repoRoot whose form
// matches the given ref. Returns SpecRefResolution on success, or a
// *SpecRefError on miss or ambiguity (so callers can render the candidate
// list deterministically).
//
// The repoRoot MUST be absolute (resolveRepoRoot guarantees this).
func resolveSpecRef(repoRoot, ref string) (*SpecRefResolution, error) {
	if ref == "" {
		return nil, &SpecRefError{Kind: "not-found", Ref: ref}
	}

	specsRoot, dirs, err := listAllSpecDirs(repoRoot)
	if err != nil {
		return nil, err
	}

	if len(dirs) == 0 {
		return nil, &SpecRefError{Kind: "not-found", Ref: ref}
	}

	// Tier 1: exact directory-name match.
	for _, name := range dirs {
		if name == ref {
			return resolution(specsRoot, name), nil
		}
	}

	// Tier 2: numeric prefix match. If `ref` is all digits or has a leading
	// digit run, treat it as a numeric prefix. The match must be on the
	// digits portion of NNN- — "09" matches "091-..." but also "092-...",
	// which is the ambiguous case the contract expects.
	if onlyDigits(ref) {
		var matches []string
		for _, name := range dirs {
			m := numericPrefixRe.FindStringSubmatch(name)
			if m == nil {
				continue
			}
			if strings.HasPrefix(m[1], ref) {
				matches = append(matches, name)
			}
		}
		switch len(matches) {
		case 0:
			// Fall through to tier 3 — purely digit input could still
			// theoretically be a slug, though unlikely.
		case 1:
			return resolution(specsRoot, matches[0]), nil
		default:
			sort.Strings(matches)
			return nil, &SpecRefError{Kind: "ambiguous", Ref: ref, Candidates: matches}
		}
	}

	// Tier 3: slug match — the trailing-after-numeric-prefix portion.
	var slugMatches []string
	for _, name := range dirs {
		slug := slugOf(name)
		if slug == ref {
			slugMatches = append(slugMatches, name)
		}
	}
	switch len(slugMatches) {
	case 0:
		sort.Strings(dirs)
		return nil, &SpecRefError{Kind: "not-found", Ref: ref, Candidates: dirs}
	case 1:
		return resolution(specsRoot, slugMatches[0]), nil
	default:
		sort.Strings(slugMatches)
		return nil, &SpecRefError{Kind: "ambiguous", Ref: ref, Candidates: slugMatches}
	}
}

// listAllSpecDirs walks the first existing specs root under repoRoot and
// returns its child directory names. The set is the union of every
// spec-kit layout; chitin uses specs/ as a symlink to .specify/specs/ so the
// first hit is canonical.
func listAllSpecDirs(repoRoot string) (string, []string, error) {
	for _, rel := range specsRootRelatives {
		root := filepath.Join(repoRoot, rel)
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return "", nil, fmt.Errorf("reading specs root %s: %w", root, err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			// Skip well-known non-spec subdirs that happen to live alongside
			// specs (e.g., chitin's audit-2026-05-18). A spec directory always
			// starts with one or more digits followed by a dash.
			if !numericPrefixRe.MatchString(e.Name()) {
				continue
			}
			names = append(names, e.Name())
		}
		return root, names, nil
	}
	return "", nil, fmt.Errorf("no specs directory found in %s (looked for %v)", repoRoot, specsRootRelatives)
}

func resolution(specsRoot, name string) *SpecRefResolution {
	r := &SpecRefResolution{
		SpecDir: filepath.Join(specsRoot, name),
		SpecRef: name,
	}
	if m := numericPrefixRe.FindStringSubmatch(name); m != nil {
		r.Numeric = m[1]
		r.Slug = strings.TrimPrefix(name, m[0])
	}
	return r
}

// slugOf returns the part of a spec directory name after the leading
// "NNN-" — empty if no numeric prefix is present.
func slugOf(name string) string {
	if m := numericPrefixRe.FindStringSubmatch(name); m != nil {
		return strings.TrimPrefix(name, m[0])
	}
	return ""
}

func onlyDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
