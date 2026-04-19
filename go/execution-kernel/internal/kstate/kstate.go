// Package kstate manages the .chitin/ directory layout: init, wipe, paths.
package kstate

import (
	"os"
	"path/filepath"
)

// Init creates the .chitin/ directory and required subpaths. Idempotent unless force==true,
// in which case the directory is wiped first.
func Init(chitinDir string, force bool) error {
	if force {
		if err := os.RemoveAll(chitinDir); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(chitinDir, "sessions"), 0o755); err != nil {
		return err
	}
	cp := filepath.Join(chitinDir, "transcript_checkpoint.json")
	if _, err := os.Stat(cp); os.IsNotExist(err) {
		if err := os.WriteFile(cp, []byte("{}"), 0o644); err != nil {
			return err
		}
	}
	return nil
}
