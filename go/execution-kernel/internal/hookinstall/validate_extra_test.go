package hookinstall

import (
	"os"
	"runtime"
	"testing"
)

func TestToAnySlice(t *testing.T) {
	// nil input
	if result := toAnySlice(nil); result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}

	// []any input
	input := []any{"a", 1, true}
	result := toAnySlice(input)
	if len(result) != 3 || result[0] != "a" || result[1] != 1 || result[2] != true {
		t.Errorf("expected %v, got %v", input, result)
	}

	// empty []any
	result = toAnySlice([]any{})
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %v", result)
	}

	// wrong type (e.g., string)
	result = toAnySlice("not a slice")
	if result != nil {
		t.Errorf("expected nil for non-slice input, got %v", result)
	}

	// int input
	result = toAnySlice(42)
	if result != nil {
		t.Errorf("expected nil for int input, got %v", result)
	}
}

func TestValidateAdapter_DirPath(t *testing.T) {
	// Directories should fail with "must be a file"
	tmpDir := t.TempDir()
	err := ValidateAdapter(tmpDir)
	if err == nil || err.Error() != "adapter path must be a file" {
		t.Errorf("expected 'must be a file' error, got %v", err)
	}
}

func TestValidateAdapter_NotExecutable(t *testing.T) {
	// Non-executable file should fail on non-Windows
	if runtime.GOOS == "windows" {
		t.Skip("executable bit check skipped on Windows")
	}
	tmpFile, err := os.CreateTemp("", "adapter-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	err = ValidateAdapter(tmpFile.Name())
	if err == nil || err.Error() != "adapter file is not executable" {
		t.Errorf("expected 'not executable' error, got %v", err)
	}
}

func TestValidateAdapter_ExecutableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bit check skipped on Windows")
	}
	tmpFile, err := os.CreateTemp("", "adapter-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()
	os.Chmod(tmpFile.Name(), 0o755)

	err = ValidateAdapter(tmpFile.Name())
	if err != nil {
		t.Errorf("expected nil error for executable file, got %v", err)
	}
}