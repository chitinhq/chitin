// Package chitindir resolves the .chitin state dir for a given cwd.
//
// Walk-up semantics: walk from cwd upward, stopping at workspaceBoundary (if
// given, exclusive — we do not leave the workspace); if a .chitin/ dir is
// found along the way, return it. Otherwise fall back to $HOME/.chitin/
// (orphan sessions), creating it if missing.
package chitindir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Resolve returns the absolute path to the .chitin state dir for cwd.
//
// If workspaceBoundary is non-empty, the walk stops at that boundary (the
// boundary itself IS inspected; ancestors of the boundary are NOT).
// Returns the orphan path ($HOME/.chitin) and creates it on-demand if no
// enclosing .chitin/ is found.
func Resolve(cwd, workspaceBoundary string) (string, error) {
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("abs cwd: %w", err)
	}
	absBoundary := ""
	if workspaceBoundary != "" {
		absBoundary, err = filepath.Abs(workspaceBoundary)
		if err != nil {
			return "", fmt.Errorf("abs boundary: %w", err)
		}
	}

	dir := absCwd
	for {
		candidate := filepath.Join(dir, ".chitin")
		info, statErr := os.Stat(candidate)
		if statErr == nil && info.IsDir() {
			return candidate, nil
		}
		if !errors.Is(statErr, os.ErrNotExist) && statErr != nil {
			return "", fmt.Errorf("stat %s: %w", candidate, statErr)
		}
		if absBoundary != "" && dir == absBoundary {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	orphan := filepath.Join(home, ".chitin")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		return "", fmt.Errorf("mkdir orphan: %w", err)
	}
	return orphan, nil
}
