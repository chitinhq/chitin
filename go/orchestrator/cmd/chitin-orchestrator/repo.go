// repo.go — repo-root resolution helper for spec 097 subcommands.
//
// The repo root is resolved in flag → env → `git rev-parse --show-toplevel`
// from cwd order, matching the convention documented in
// specs/097-operator-scheduler-entrypoint/research.md D6.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveRepoRoot resolves the chitin repo root for spec-ref resolution.
// Priority:
//  1. flagRepoRoot (the value of --repo-root, possibly empty)
//  2. $CHITIN_REPO_ROOT
//  3. `git rev-parse --show-toplevel` from the current working directory
//
// Returns an absolute path or an error if none of the three sources yields
// a valid directory. Caller surfaces the error as runtime (exit 2) per
// FR-011.
func resolveRepoRoot(flagRepoRoot string) (string, error) {
	candidate := flagRepoRoot
	if candidate == "" {
		candidate = os.Getenv("CHITIN_REPO_ROOT")
	}
	if candidate == "" {
		out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		if err != nil {
			return "", fmt.Errorf("cannot determine repo root: --repo-root unset, $CHITIN_REPO_ROOT unset, and `git rev-parse --show-toplevel` failed: %w", err)
		}
		candidate = strings.TrimSpace(string(out))
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("cannot absolutize repo root %q: %w", candidate, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("repo root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repo root %q is not a directory", abs)
	}
	return abs, nil
}
