// Package speclint implements deterministic linting of spec-kit spec
// directories per spec 115 FR-003.
//
// A spec-dir is a directory containing spec.md + tasks.md. Each rule
// (L01..L07, landing in subsequent tasks T003-T009) is a pure function
// that maps a loaded SpecDir to zero or more Violations. Rules
// self-register via Register() (typically from an init() in their own
// file), so spec_lint.go does not need to know which rules exist —
// Run() iterates whatever has been registered. This file (T002) ships
// only the loader, registry, and deterministic Run() ordering; the
// rule implementations themselves land separately.
package speclint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Severity classifies a violation's gating effect on the spec-PR iteration
// loop. Per spec 115 edge case "Linter has a bug and posts false positives":
// only `error` violations gate iteration; `warning` is informational.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Violation is one finding from one rule, addressed at a single source line.
// The shape is both the structured JSON record emitted by the spec-lint
// subcommand (FR-003) and the per-comment payload posted by PostLintViolations
// (T010 / FR-004).
type Violation struct {
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// SpecDir is the loaded spec.md + tasks.md pair plus path context that every
// rule receives. Rules MUST treat the contents as read-only.
type SpecDir struct {
	// Path is the absolute path to the spec directory.
	Path string
	// SpecMD is the byte contents of spec.md. Empty if the file is missing —
	// L01 surfaces that as a violation rather than the loader failing.
	SpecMD []byte
	// TasksMD is the byte contents of tasks.md. Empty if the file is missing.
	TasksMD []byte
}

// Load reads spec.md + tasks.md from the given directory. Missing files
// yield empty byte slices, not errors — the rules surface missing-file
// conditions as violations rather than crashing the linter. A truly bad
// directory (does not exist, not a directory) returns an error so the
// subcommand can exit with the user-error code.
func Load(specDir string) (*SpecDir, error) {
	abs, err := filepath.Abs(specDir)
	if err != nil {
		return nil, fmt.Errorf("absolutize %q: %w", specDir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", abs)
	}
	specMD, err := readOrEmpty(filepath.Join(abs, "spec.md"))
	if err != nil {
		return nil, err
	}
	tasksMD, err := readOrEmpty(filepath.Join(abs, "tasks.md"))
	if err != nil {
		return nil, err
	}
	return &SpecDir{
		Path:    abs,
		SpecMD:  specMD,
		TasksMD: tasksMD,
	}, nil
}

// readOrEmpty reads p, returning nil bytes (no error) when p does not exist
// — L01 then surfaces the missing file as a violation. Any other error
// (permission denied, IO failure, etc.) is propagated so the subcommand can
// exit with a user/runtime error instead of silently producing misleading
// lint results that look like a clean run.
func readOrEmpty(p string) ([]byte, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %q: %w", p, err)
	}
	return b, nil
}

// RuleFunc is the contract every rule satisfies — pure, side-effect-free,
// returns zero-or-more violations for the loaded spec dir.
type RuleFunc func(*SpecDir) []Violation

var registered []RuleFunc

// Register adds a rule to the run set. Called from each rule file's init().
// Registration order does not affect output: Run() sorts deterministically.
func Register(r RuleFunc) {
	registered = append(registered, r)
}

// Run executes every registered rule against the given spec dir and returns
// the aggregated violations in a deterministic order (rule, file, line,
// message). The full ordering is stated so two runs over the same spec
// produce byte-identical JSON — a precondition for FR-004's per-(rule,
// file, line) dedup of PR review comments.
func Run(s *SpecDir) []Violation {
	var out []Violation
	for _, r := range registered {
		out = append(out, r(s)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Message < out[j].Message
	})
	return out
}
