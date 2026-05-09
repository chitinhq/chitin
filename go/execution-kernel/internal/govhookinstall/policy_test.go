package govhookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsPath_GlobalScope(t *testing.T) {
	// We can't easily mock os.UserHomeDir, but we can at least verify
	// the path suffix is correct by testing ScopeProject.
	t.Run("project scope", func(t *testing.T) {
		p, err := settingsPath(ScopeProject, "/tmp/myproject")
		if err != nil {
			t.Fatal(err)
		}
		expected := filepath.Join("/tmp/myproject", ".claude", "settings.json")
		if p != expected {
			t.Errorf("project path=%q, want %q", p, expected)
		}
	})

	t.Run("unknown scope", func(t *testing.T) {
		_, err := settingsPath(Scope(99), "/tmp")
		if err == nil {
			t.Fatal("expected error for unknown scope")
		}
	})
}

func TestDryRun_ExistingSettings(t *testing.T) {
	cwd := t.TempDir()
	settingsDir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
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
	if err := os.WriteFile(settingsPath, b, 0600); err != nil {
		t.Fatal(err)
	}

	plan, err := DryRun(ScopeProject, cwd, "chitin-kernel gate evaluate --hook-stdin")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if !plan.WouldWrite {
		t.Error("expected WouldWrite=true")
	}
	if plan.PreservedCount != 1 {
		t.Errorf("PreservedCount=%d, want 1", plan.PreservedCount)
	}
	// File should not have been modified
	s := readSettings(t, settingsPath)
	pre := s["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Errorf("DryRun should not modify file, but PreToolUse count=%d", len(pre))
	}
}

func TestDryRun_ExistingSettingsWithBackup(t *testing.T) {
	cwd := t.TempDir()
	settingsDir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	b, _ := json.MarshalIndent(map[string]any{}, "", "  ")
	if err := os.WriteFile(settingsPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	// Create a prior backup
	backupPath := settingsPath + ".chitin-backup-20260509T040000Z"
	if err := os.WriteFile(backupPath, b, 0600); err != nil {
		t.Fatal(err)
	}

	plan, err := DryRun(ScopeProject, cwd, "cmd")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if !plan.BackupExists {
		t.Error("expected BackupExists=true when backup already present")
	}
	if plan.Backup != backupPath {
		t.Errorf("Backup=%q, want %q", plan.Backup, backupPath)
	}
}

func TestDryRun_ExistingSettingsNoBackup(t *testing.T) {
	cwd := t.TempDir()
	settingsDir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	b, _ := json.MarshalIndent(map[string]any{}, "", "  ")
	if err := os.WriteFile(settingsPath, b, 0600); err != nil {
		t.Fatal(err)
	}

	plan, err := DryRun(ScopeProject, cwd, "cmd")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if plan.BackupExists {
		t.Error("expected BackupExists=false when no backup exists")
	}
	// Backup should be a would-be path (not yet on disk)
	if plan.Backup == "" {
		t.Error("expected non-empty Backup path for DryRun of existing file")
	}
}

func TestFilterOutChitinGovernance(t *testing.T) {
	entries := []any{
		map[string]any{"matcher": "Read", "hooks": []any{"x"}},
		map[string]any{"_tag": chitinTag, "matcher": "Bash", "hooks": []any{"y"}},
		map[string]any{"matcher": "Write", "hooks": []any{"z"}},
	}
	filtered := filterOutChitinGovernance(entries)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 entries after filtering, got %d", len(filtered))
	}
}

func TestGovWrapper(t *testing.T) {
	w := govWrapper("test-cmd")
	if w["_tag"] != chitinTag {
		t.Errorf("_tag=%v, want %q", w["_tag"], chitinTag)
	}
	if w["matcher"] == nil {
		t.Error("matcher should not be nil")
	}
	hooks, ok := w["hooks"].([]any)
	if !ok || len(hooks) != 1 {
		t.Errorf("hooks: expected 1-entry slice, got %v", w["hooks"])
	}
	cmd := hooks[0].(map[string]any)
	if cmd["command"] != "test-cmd" {
		t.Errorf("command=%v, want test-cmd", cmd["command"])
	}
}

func TestEnsureHooksMap_NilSettings(t *testing.T) {
	settings := map[string]any{}
	h := ensureHooksMap(settings)
	if h == nil {
		t.Fatal("expected non-nil empty map")
	}
}

func TestEnsureHooksMap_ExistingHooks(t *testing.T) {
	existing := map[string]any{"PreToolUse": []any{"x"}}
	settings := map[string]any{"hooks": existing}
	h := ensureHooksMap(settings)
	if _, ok := h["PreToolUse"]; !ok {
		t.Error("expected PreToolUse to be preserved")
	}
}