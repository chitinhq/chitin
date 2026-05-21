package gov

import (
	"fmt"
	"os"
	"path/filepath"
)

// LoadWithInheritance walks from cwd upward until it finds chitin.yaml
// files, loads each, and merges them with child-wins-on-rule-id semantics
// and monotonic-strictness checks (a child cannot loosen a parent's mode).
//
// Returns the merged Policy, the ordered list of source paths that
// contributed (outermost first, innermost last), and an error if no
// policy was found or a strictness violation was detected.
func LoadWithInheritance(cwd string) (Policy, []string, error) {
	return LoadWithInheritanceWithOptions(cwd, PolicyLoadOptions{})
}

func LoadWithInheritanceWithOptions(cwd string, opts PolicyLoadOptions) (Policy, []string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return Policy{}, nil, fmt.Errorf("abs: %w", err)
	}

	var paths []string
	dir := abs
	for {
		candidate := filepath.Join(dir, "chitin.yaml")
		if _, err := os.Stat(candidate); err == nil {
			paths = append(paths, candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if len(paths) == 0 {
		return Policy{}, nil, fmt.Errorf("no_policy_found: no chitin.yaml from %s upward", abs)
	}

	// Reverse paths so outermost (root) is first, innermost (leaf) is last
	// — so child overrides parent on rule-ID collision.
	reverse(paths)

	var merged Policy
	for i, p := range paths {
		loaded, err := LoadPolicyFileWithOptions(p, opts)
		if err != nil {
			return Policy{}, paths, err
		}
		if i == 0 {
			merged = loaded
			continue
		}
		if err := checkMonotonicStrictness(merged.Mode, loaded.Mode); err != nil {
			return Policy{}, paths, fmt.Errorf("strictness_violation in %s: %w", p, err)
		}
		merged = mergePolicies(merged, loaded)
	}

	// Validate the merged result. Pre-fix this discarded the error,
	// letting any validation failure introduced by the merge survive
	// into the live Policy. The per-file LoadPolicyFile already runs
	// ApplyDefaults on each file in isolation, but propagating the
	// post-merge error closes the contract: load returns a non-nil
	// error iff the resulting Policy would not have validated had it
	// been authored as a single file.
	if err := merged.ApplyDefaults(); err != nil {
		return Policy{}, paths, fmt.Errorf("validate merged policy: %w", err)
	}
	return merged, paths, nil
}

// checkMonotonicStrictness rejects child weakening parent's mode.
// Strictness ordering: enforce > guide > monitor.
func checkMonotonicStrictness(parentMode, childMode string) error {
	rank := map[string]int{"monitor": 0, "guide": 1, "enforce": 2, "": 1}
	if rank[childMode] < rank[parentMode] {
		return fmt.Errorf("child mode=%q cannot weaken parent mode=%q", childMode, parentMode)
	}
	return nil
}

// mergePolicies merges child over parent with child-wins-on-rule-id.
// Bounds / InvariantModes merge additively; child overrides on key collision.
func mergePolicies(parent, child Policy) Policy {
	out := parent
	if child.ID != "" {
		out.ID = child.ID
	}
	if child.Mode != "" {
		out.Mode = child.Mode
	}
	if len(child.Drivers) > 0 {
		parentByID := make(map[string]int, len(out.Drivers))
		for i, driver := range out.Drivers {
			parentByID[driver.ID] = i
		}
		for _, driver := range child.Drivers {
			if idx, ok := parentByID[driver.ID]; ok {
				out.Drivers[idx] = driver
			} else {
				out.Drivers = append(out.Drivers, driver)
			}
		}
	}
	if child.Bounds.MaxFilesChanged > 0 {
		out.Bounds.MaxFilesChanged = child.Bounds.MaxFilesChanged
	}
	if child.Bounds.MaxLinesChanged > 0 {
		out.Bounds.MaxLinesChanged = child.Bounds.MaxLinesChanged
	}
	// Bounds.PerAction merges additively — child entries override parent
	// entries on key collision, parent entries survive when child has no
	// entry for that action_type. Without this merge step, a child policy
	// that sets only the global bounds (or a parent that contributes a
	// PerAction map) silently drops the per_action overrides at the merge
	// boundary: in the workspace-root inheritance case, the workspace
	// chitin.yaml has no bounds, the inner repo's chitin.yaml has both
	// global and per_action, mergePolicies(workspace, inner) used to
	// copy global but lose PerAction — so a 7000-line git push got hit
	// with the 500-line global ceiling instead of the 5000-line git.push
	// override. This also fixes the symptom that looked like a
	// chitin-router-hook bug ("ceiling of 500 firing without sentinel"):
	// it was downstream from CheckBounds, fixed here in the merge.
	if len(child.Bounds.PerAction) > 0 {
		if out.Bounds.PerAction == nil {
			out.Bounds.PerAction = make(map[string]ActionBounds, len(child.Bounds.PerAction))
		}
		for k, v := range child.Bounds.PerAction {
			out.Bounds.PerAction[k] = v
		}
	}
	// (MaxRuntimeSeconds removed from v1 — see Bounds doc.)
	// Escalation config: child overrides parent per field (only if child
	// explicitly set the value — zero means "use parent/default").
	if child.Escalation.ElevatedThreshold > 0 {
		out.Escalation.ElevatedThreshold = child.Escalation.ElevatedThreshold
	}
	if child.Escalation.HighThreshold > 0 {
		out.Escalation.HighThreshold = child.Escalation.HighThreshold
	}
	if child.Escalation.LockdownThreshold > 0 {
		out.Escalation.LockdownThreshold = child.Escalation.LockdownThreshold
	}
	if child.Escalation.MaxRetriesPerFp > 0 {
		out.Escalation.MaxRetriesPerFp = child.Escalation.MaxRetriesPerFp
	}
	out.Authority.Trusted = append(out.Authority.Trusted, child.Authority.Trusted...)
	if out.InvariantModes == nil {
		out.InvariantModes = make(map[string]string)
	}
	for k, v := range child.InvariantModes {
		out.InvariantModes[k] = v
	}

	// Child rules: override parent by ID, append new ones.
	parentByID := make(map[string]int, len(out.Rules))
	for i, r := range out.Rules {
		parentByID[r.ID] = i
	}
	for _, r := range child.Rules {
		if idx, ok := parentByID[r.ID]; ok {
			out.Rules[idx] = r
		} else {
			out.Rules = append(out.Rules, r)
		}
	}
	return out
}

func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
