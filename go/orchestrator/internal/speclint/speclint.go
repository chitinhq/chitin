// Package speclint contains the deterministic spec-PR consistency checks
// (spec 115 FR-003). The seven rules L01-L07 each live in their own file
// (l01_frontmatter.go … l07_us_test.go) and self-register via init() into
// the package-level registry exposed here.
//
// The chitin-orchestrator `spec-lint <spec-dir>` subcommand (T002) loads
// spec.md + tasks.md from <spec-dir>, runs every registered rule, and emits
// the merged violation list as JSON on stdout. The package itself has no
// I/O beyond reading those two files; rules are pure functions of the
// SpecPaths input and any allowlist files the rule documents.
package speclint

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Severity is the level of a single Violation. The taxonomy is closed:
// info, warning, error. The subcommand maps severity to exit code per
// FR-003 (0=clean, 2=warnings, 3=errors).
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Violation is one finding emitted by a rule. The JSON shape is the public
// contract — `chitin-orchestrator spec-lint` emits `[]Violation` on stdout
// per FR-003, and PostLintViolations (T010) reads this shape to dedup +
// post PR review comments.
type Violation struct {
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// SpecPaths is the resolved per-spec context a rule operates on. SpecDir
// is the absolute path to `.specify/specs/NNN-*/`; SpecMD and TasksMD are
// the absolute paths to the two files inside it. RepoRoot is the chitin
// repo root, which rules L02 and L05 use to resolve cross-spec refs and
// the CLI surface allowlist.
type SpecPaths struct {
	RepoRoot string
	SpecDir  string
	SpecMD   string
	TasksMD  string
}

// RuleFunc is the signature every rule implements. It returns its own
// violations (an empty slice and a nil error means the rule found no
// problems). A non-nil error means the rule itself failed to run — e.g.
// a malformed YAML frontmatter so L01 couldn't parse — and the subcommand
// surfaces that as a runtime error rather than a violation list.
type RuleFunc func(SpecPaths) ([]Violation, error)

// registry holds the registered rules in insertion order. Rule files call
// Register in their init() blocks; lint.Run iterates this slice.
var registry []registeredRule

type registeredRule struct {
	name string
	fn   RuleFunc
}

// Register adds a rule to the registry. Called from each rule's init().
// A duplicate name overwrites the previous entry — useful in tests, but
// production rule files use distinct names L01..L07.
func Register(name string, fn RuleFunc) {
	for i, r := range registry {
		if r.name == name {
			registry[i].fn = fn
			return
		}
	}
	registry = append(registry, registeredRule{name: name, fn: fn})
}

// ResetRulesForTest clears the registry and returns a restore function
// the caller defers. Tests that wire fake rules to exercise the
// subcommand path use this to keep their isolation hermetic — once the
// real T003-T009 rule files self-register in init(), tests that don't
// want them must call this to start from a clean slate.
func ResetRulesForTest() func() {
	saved := registry
	registry = nil
	return func() { registry = saved }
}

// Rules returns the registered rule names in insertion order. Exposed so
// tests can assert the rule set without poking at the registry directly.
func Rules() []string {
	names := make([]string, 0, len(registry))
	for _, r := range registry {
		names = append(names, r.name)
	}
	return names
}

// ResolveSpecPaths takes a directory the operator supplied and resolves
// the SpecPaths the rules need. It validates that spec.md and tasks.md
// both exist under specDir and infers the chitin repo root by walking up
// from specDir until it finds a `.specify` ancestor directory; if no
// such ancestor is found, RepoRoot stays empty (rules that need it must
// handle that gracefully — typically by skipping their cross-ref check).
func ResolveSpecPaths(specDir string) (SpecPaths, error) {
	abs, err := filepath.Abs(specDir)
	if err != nil {
		return SpecPaths{}, fmt.Errorf("resolve spec dir: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return SpecPaths{}, fmt.Errorf("spec dir %s: %w", abs, err)
	}
	if !info.IsDir() {
		return SpecPaths{}, fmt.Errorf("spec dir %s: not a directory", abs)
	}
	specMD := filepath.Join(abs, "spec.md")
	if _, err := os.Stat(specMD); err != nil {
		return SpecPaths{}, fmt.Errorf("spec.md missing in %s: %w", abs, err)
	}
	tasksMD := filepath.Join(abs, "tasks.md")
	if _, err := os.Stat(tasksMD); err != nil {
		return SpecPaths{}, fmt.Errorf("tasks.md missing in %s: %w", abs, err)
	}
	return SpecPaths{
		RepoRoot: inferRepoRoot(abs),
		SpecDir:  abs,
		SpecMD:   specMD,
		TasksMD:  tasksMD,
	}, nil
}

// inferRepoRoot walks up from specDir looking for a `.specify` directory.
// The chitin layout is <repo>/.specify/specs/NNN-*/, so the repo root is
// two levels above specDir if that ancestor exists. Returns empty if no
// `.specify` ancestor is found.
func inferRepoRoot(specDir string) string {
	cur := specDir
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		if filepath.Base(parent) == "specs" && filepath.Base(filepath.Dir(parent)) == ".specify" {
			return filepath.Dir(filepath.Dir(parent))
		}
		cur = parent
	}
}

// Run executes every registered rule against the given SpecPaths and
// returns the merged violation list, sorted by (file, line, rule) so the
// JSON output and any downstream dedup are deterministic. Rule errors
// short-circuit: the first rule that errors returns its error up; this
// is intentional, since a rule-internal failure is a bug, not a finding.
func Run(p SpecPaths) ([]Violation, error) {
	var all []Violation
	for _, r := range registry {
		vs, err := r.fn(p)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", r.name, err)
		}
		all = append(all, vs...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].File != all[j].File {
			return all[i].File < all[j].File
		}
		if all[i].Line != all[j].Line {
			return all[i].Line < all[j].Line
		}
		return all[i].Rule < all[j].Rule
	})
	return all, nil
}

// Worst returns the highest severity in vs, or empty string if vs is
// empty. The subcommand uses this to pick the exit code.
func Worst(vs []Violation) Severity {
	var worst Severity
	for _, v := range vs {
		if severityRank(v.Severity) > severityRank(worst) {
			worst = v.Severity
		}
	}
	return worst
}

func severityRank(s Severity) int {
	switch s {
	case SeverityError:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}
