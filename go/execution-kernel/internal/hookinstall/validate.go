package hookinstall

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// ValidateAdapter checks that the given path is absolute, exists, and is executable
func ValidateAdapter(path string) error {
	if path == "" {
		return errors.New("adapter path cannot be empty")
	}

	if !filepath.IsAbs(path) {
		return errors.New("adapter path must be absolute")
	}

	info, err := os.Stat(path)
	if err != nil {
		return errors.New("adapter path does not exist")
	}

	if info.IsDir() {
		return errors.New("adapter path must be a file")
	}

	// On Windows, skip executable bit check
	if runtime.GOOS == "windows" {
		return nil
	}

	// Check executable bit
	if info.Mode().Perm()&0o111 == 0 {
		return errors.New("adapter file is not executable")
	}

	return nil
}

// ValidateAdapterShell checks that the given shell command string is not empty
func ValidateAdapterShell(cmd string) error {
	if cmd == "" {
		return errors.New("adapter shell command cannot be empty")
	}
	return nil
}