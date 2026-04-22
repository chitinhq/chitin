package hookinstall

import (
	"os"
	"runtime"
	"testing"
)

func TestValidateAdapter(t *testing.T) {
	// Test absolute path, exists, executable → pass
	t.Run("absolute path exists executable", func(t *testing.T) {
		// Create a temporary executable file
		tmpFile, err := os.CreateTemp("", "test-adapter")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpFile.Name())

		// Make it executable
		if runtime.GOOS != "windows" {
			if err := os.Chmod(tmpFile.Name(), 0o755); err != nil {
				t.Fatal(err)
			}
		}

		absPath := tmpFile.Name()
		if err := ValidateAdapter(absPath); err != nil {
			t.Errorf("Expected no error for valid executable file, got: %v", err)
		}
	})

	// Test relative path → reject with clear error
	t.Run("relative path", func(t *testing.T) {
		if err := ValidateAdapter("relative/path"); err == nil {
			t.Error("Expected error for relative path, got nil")
		}
	})

	// Test non-existent path → reject
	t.Run("non-existent path", func(t *testing.T) {
		if err := ValidateAdapter("/this/path/does/not/exist"); err == nil {
			t.Error("Expected error for non-existent path, got nil")
		}
	})

	// Test non-executable file → reject
	t.Run("non-executable file", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "test-adapter")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpFile.Name())

		// Don't make it executable
		absPath := tmpFile.Name()
		if err := ValidateAdapter(absPath); err == nil {
			t.Error("Expected error for non-executable file, got nil")
		}
	})

	// Test empty string → reject
	t.Run("empty string", func(t *testing.T) {
		if err := ValidateAdapter(""); err == nil {
			t.Error("Expected error for empty string, got nil")
		}
	})
}

func TestValidateAdapterShell(t *testing.T) {
	// Test shell metacharacters → pass
	t.Run("shell with metacharacters", func(t *testing.T) {
		if err := ValidateAdapterShell("ls -la && echo 'hello'"); err != nil {
			t.Errorf("Expected no error for shell command with metacharacters, got: %v", err)
		}
	})

	// Test empty string → reject
	t.Run("empty string", func(t *testing.T) {
		if err := ValidateAdapterShell(""); err == nil {
			t.Error("Expected error for empty string, got nil")
		}
	})
}