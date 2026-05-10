package govhookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDryRun_ExistingSettings_WithHooks(t *testing.T) {
	cwd := projectInTempDir(t)
	settingsDir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	original := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Read",
					"hooks":   []any{map[string]any{"type": "command", "command": "user.sh"}},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(original, "", "  ")
	if err := os.WriteFile(settingsPath, b, 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := DryRun(ScopeProject, cwd, "chitin-kernel gate evaluate --hook-stdin")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if !plan.WouldWrite {
		t.Error("WouldWrite=false; want true")
	}
	// Should count 1 preserved (non-chitin) hook
	if plan.PreservedCount != 1 {
		t.Errorf("PreservedCount=%d, want 1", plan.PreservedCount)
	}
}

func TestDryRun_ExistingSettings_DetectsExistingChitinBackup(t *testing.T) {
	cwd := projectInTempDir(t)
	settingsDir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Create a backup file matching the pattern
	backupPath := settingsPath + ".chitin-backup-20260509T120000Z"
	if err := os.WriteFile(backupPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := DryRun(ScopeProject, cwd, "x")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if !plan.BackupExists {
		t.Error("BackupExists=false; want true (backup should be detected)")
	}
	if plan.Backup == "" {
		t.Error("expected non-empty Backup path when backup exists")
	}
}

func TestDryRun_ExistingSettings_NoExistingBackup(t *testing.T) {
	cwd := projectInTempDir(t)
	settingsDir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := DryRun(ScopeProject, cwd, "x")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if plan.BackupExists {
		t.Error("BackupExists=true; want false (no backup should exist)")
	}
	if plan.Backup == "" {
		t.Error("expected non-empty Backup name (planned backup path)")
	}
}

func TestDryRun_SettingsPath_ScopeProject(t *testing.T) {
	cwd := projectInTempDir(t)
	plan, err := DryRun(ScopeProject, cwd, "x")
	if err != nil {
		t.Fatalf("DryRun ScopeProject: %v", err)
	}
	expected := filepath.Join(cwd, ".claude", "settings.json")
	if plan.Path != expected {
		t.Errorf("Path=%q, want %q", plan.Path, expected)
	}
}

func TestDryRun_ChitinEntriesNotCountedAsPreserved(t *testing.T) {
	cwd := projectInTempDir(t)
	settingsDir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	// Settings with an existing chitin entry and a user entry
	original := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"_tag":    chitinTag,
					"matcher": "Bash",
					"hooks":   []any{map[string]any{"type": "command", "command": "old-chitin"}},
				},
				map[string]any{
					"matcher": "Read",
					"hooks":   []any{map[string]any{"type": "command", "command": "user.sh"}},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(original, "", "  ")
	if err := os.WriteFile(settingsPath, b, 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := DryRun(ScopeProject, cwd, "chitin-kernel gate evaluate --hook-stdin")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	// Only 1 preserved (the non-chitin entry) — chitin entries are filtered
	if plan.PreservedCount != 1 {
		t.Errorf("PreservedCount=%d, want 1 (only non-chitin entries)", plan.PreservedCount)
	}
}

func TestSettingsPath_GlobalScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := settingsPath(ScopeGlobal, "")
	if err != nil {
		t.Fatalf("settingsPath Global: %v", err)
	}
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".claude", "settings.json")
	if path != expected {
		t.Errorf("Global path=%q, want %q", path, expected)
	}
}

func TestSettingsPath_InvalidScope(t *testing.T) {
	_, err := settingsPath(Scope(99), "/tmp")
	if err == nil {
		t.Error("expected error for invalid scope")
	}
}

func TestWriteSettingsAtomic_CreateNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	r := map[string]any{"key": "value"}
	if err := writeSettingsAtomic(path, r); err != nil {
		t.Fatalf("writeSettingsAtomic: %v", err)
	}
	s := readSettings(t, path)
	if s["key"] != "value" {
		t.Errorf("expected key=value, got %v", s["key"])
	}
}

func TestWriteSettingsAtomic_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	r1 := map[string]any{"version": 1}
	if err := writeSettingsAtomic(path, r1); err != nil {
		t.Fatal(err)
	}
	r2 := map[string]any{"version": 2}
	if err := writeSettingsAtomic(path, r2); err != nil {
		t.Fatalf("writeSettingsAtomic overwrite: %v", err)
	}
	s := readSettings(t, path)
	if s["version"] != float64(2) {
		t.Errorf("expected version=2, got %v", s["version"])
	}
}