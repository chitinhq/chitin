package speclint

import (
	"os"
	"path/filepath"
)

// register.go wires each L0N rule into the speclint.Run() set. The
// individual rule files have heterogeneous signatures because they were
// each dispatched as an isolated task (T003-T009); this file adapts each
// to the uniform RuleFunc the runner expects.
//
// Without this wiring, Run() iterates an empty slice and spec-lint
// always returns "[]" — which is what happened in the first dispatch of
// spec 115 because no rule file's task included the registration step.
// Filed as part of spec 117 (file-overlap edge inference for creates):
// the registration belongs in a shared file that no single task owns,
// which the scheduler can't dispatch as a leaf.

func init() {
	Register(adaptL01)
	Register(adaptL02)
	Register(adaptL03)
	Register(adaptL04)
	Register(adaptL05)
	Register(adaptL06)
	Register(adaptL07)
}

// adaptL01 calls L01Frontmatter with the loaded spec.md content. The
// rule reports violations against the "spec.md" filename relative to
// the spec dir — operators see a stable label whether they ran the
// linter from inside the dir or from the repo root.
func adaptL01(s *SpecDir) []Violation {
	return L01Frontmatter(specMDLabel(s), string(s.SpecMD))
}

// adaptL02 calls CheckCrossRefs with the spec's path AND its containing
// .specify/specs/ root — the rule needs both to glob sibling spec dirs
// when resolving depends_on / related ids. An error from CheckCrossRefs
// is logged silently for now (the rule's "could not stat" branch); the
// fail-soft contract matches the rest of speclint.
func adaptL02(s *SpecDir) []Violation {
	specsRoot := filepath.Dir(s.Path)
	out, _ := CheckCrossRefs(s.Path, specsRoot)
	return out
}

// adaptL03 calls L03TaskFRCoverage with both file contents.
func adaptL03(s *SpecDir) []Violation {
	return L03TaskFRCoverage(string(s.SpecMD), string(s.TasksMD))
}

// adaptL04 calls L04Events with the two-file pair.
func adaptL04(s *SpecDir) []Violation {
	return L04Events(specMDLabel(s), string(s.SpecMD), tasksMDLabel(s), string(s.TasksMD))
}

// adaptL05 calls L05CLISurface with an optional allowlist loaded from
// .specify/known-cli-surfaces.txt (relative to the repo root inferred
// from the spec dir's path). Missing or unreadable allowlist file =
// empty allowlist; L05 then flags every subcommand reference that
// isn't introduced by THIS spec, which is the desired conservative
// default for a fresh operator-host.
func adaptL05(s *SpecDir) []Violation {
	var allowlist []AllowlistEntry
	if path := allowlistPath(s); path != "" {
		if content, err := os.ReadFile(path); err == nil {
			allowlist = ParseCLIAllowlist(string(content))
		}
	}
	return L05CLISurface(
		specMDLabel(s), string(s.SpecMD),
		tasksMDLabel(s), string(s.TasksMD),
		allowlist,
	)
}

// adaptL06 calls CheckL06 with the spec + tasks pair and the resolved
// depends_on spec.md contents. Resolution walks
// .specify/specs/<id>-*/spec.md siblings; failures fall through with an
// empty depSpecContents (L06 then only sees this spec's own canonical
// reason set, which is the correct restrictive fallback).
func adaptL06(s *SpecDir) []Violation {
	depSpecs := loadDepSpecContents(s)
	return CheckL06(
		specMDLabel(s), string(s.SpecMD),
		tasksMDLabel(s), string(s.TasksMD),
		depSpecs,
	)
}

// adaptL07 calls CheckL07 with the spec content.
func adaptL07(s *SpecDir) []Violation {
	return CheckL07(specMDLabel(s), string(s.SpecMD))
}

// specMDLabel returns the stable filename the rules report. Using the
// bare "spec.md" — rather than the absolute path — keeps violation
// records stable across operator hosts and PR-comment dedup (FR-004).
func specMDLabel(s *SpecDir) string { return "spec.md" }

// tasksMDLabel mirrors specMDLabel for tasks.md.
func tasksMDLabel(s *SpecDir) string { return "tasks.md" }

// allowlistPath returns the absolute path to
// .specify/known-cli-surfaces.txt by walking up from s.Path until we
// find a directory containing `.specify/`. Returns "" if no such
// ancestor exists — adaptL05 then runs L05 with an empty allowlist.
func allowlistPath(s *SpecDir) string {
	dir := s.Path
	for i := 0; i < 8; i++ { // depth cap so a pathological path doesn't loop
		candidate := filepath.Join(dir, ".specify", "known-cli-surfaces.txt")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

// loadDepSpecContents resolves the depends_on ids declared in s.SpecMD's
// frontmatter to the contents of each sibling spec.md. Missing siblings
// are silently skipped — L02 is the rule that flags unresolved
// cross-refs; L06 only needs whatever DOES resolve.
func loadDepSpecContents(s *SpecDir) []string {
	depIDs := L06DependsOnIDs(string(s.SpecMD))
	if len(depIDs) == 0 {
		return nil
	}
	specsRoot := filepath.Dir(s.Path)
	var out []string
	for _, id := range depIDs {
		matches, _ := filepath.Glob(filepath.Join(specsRoot, id+"-*", "spec.md"))
		if len(matches) != 1 {
			continue
		}
		if content, err := os.ReadFile(matches[0]); err == nil {
			out = append(out, string(content))
		}
	}
	return out
}
