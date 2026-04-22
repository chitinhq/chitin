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
		loaded, err := LoadPolicyFile(p)
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

	merged.ApplyDefaults()
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
	if child.Bounds.MaxFilesChanged > 0 {
		out.Bounds.MaxFilesChanged = child.Bounds.MaxFilesChanged
	}
	if child.Bounds.MaxLinesChanged > 0 {
		out.Bounds.MaxLinesChanged = child.Bounds.MaxLinesChanged
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
