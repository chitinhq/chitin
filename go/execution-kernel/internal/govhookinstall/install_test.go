package govhookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectInTempDir installs/uninstalls against a fresh temp dir using
// ScopeProject — sidesteps the global $HOME path so the test can't
// touch the user's real ~/.claude/settings.json.
func projectInTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings %s: %v", path, err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("parse settings: %v\nbody=%s", err, b)
	}
	return s
}

func TestInstall_FreshSettings_AddsGovernanceHook(t *testing.T) {
	cwd := projectInTempDir(t)
	path, backup, err := Install(ScopeProject, cwd, "chitin-kernel gate evaluate --hook-stdin --agent=claude-code")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if backup != "" {
		t.Fatalf("fresh settings should not produce a backup, got %q", backup)
	}
	if !strings.HasSuffix(path, "/.claude/settings.json") {
		t.Fatalf("path=%q should end with /.claude/settings.json", path)
	}
	s := readSettings(t, path)
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("missing hooks: %+v", s)
	}
	pre, ok := hooks["PreToolUse"].([]any)
	if !ok || len(pre) != 1 {
		t.Fatalf("PreToolUse: %+v", hooks["PreToolUse"])
	}
	entry := pre[0].(map[string]any)
	if entry["_tag"] != chitinTag {
		t.Fatalf("_tag=%v want %s", entry["_tag"], chitinTag)
	}
	if entry["matcher"] == nil || !strings.Contains(entry["matcher"].(string), "Bash") {
		t.Fatalf("matcher missing Bash: %v", entry["matcher"])
	}
}

func TestInstall_PreservesNonChitinEntries(t *testing.T) {
	cwd := projectInTempDir(t)
	settingsDir := filepath.Join(cwd, ".claude")
	_ = os.MkdirAll(settingsDir, 0o755)
	settingsPath := filepath.Join(settingsDir, "settings.json")
	original := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Read",
					"hooks": []any{
						map[string]any{"type": "command", "command": "user-script.sh"},
					},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(original, "", "  ")
	_ = os.WriteFile(settingsPath, b, 0o600)

	_, _, err := Install(ScopeProject, cwd, "chitin-kernel gate evaluate --hook-stdin")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	s := readSettings(t, settingsPath)
	pre := s["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("PreToolUse count=%d want 2 (user + chitin)", len(pre))
	}
	// User entry first (preserved verbatim), chitin appended.
	user := pre[0].(map[string]any)
	if user["matcher"] != "Read" {
		t.Fatalf("user entry mutated: %+v", user)
	}
	chitin := pre[1].(map[string]any)
	if chitin["_tag"] != chitinTag {
		t.Fatalf("chitin entry not appended last: %+v", pre)
	}
}

func TestInstall_Idempotent_SecondInstallReplacesNotDuplicates(t *testing.T) {
	cwd := projectInTempDir(t)
	_, _, err := Install(ScopeProject, cwd, "old-command")
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	_, _, err = Install(ScopeProject, cwd, "new-command")
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	settingsPath := filepath.Join(cwd, ".claude", "settings.json")
	s := readSettings(t, settingsPath)
	pre := s["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("expected 1 chitin entry after reinstall, got %d", len(pre))
	}
	cmd := pre[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"]
	if cmd != "new-command" {
		t.Fatalf("reinstall did not pick up new command: %v", cmd)
	}
}

func TestInstall_FirstTouch_BacksUpExistingFile(t *testing.T) {
	cwd := projectInTempDir(t)
	settingsDir := filepath.Join(cwd, ".claude")
	_ = os.MkdirAll(settingsDir, 0o755)
	settingsPath := filepath.Join(settingsDir, "settings.json")
	originalBytes := []byte(`{"unrelated":"value"}`)
	_ = os.WriteFile(settingsPath, originalBytes, 0o600)

	_, backup, err := Install(ScopeProject, cwd, "x")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if backup == "" {
		t.Fatalf("expected backup to be created on first touch")
	}
	got, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(got) != string(originalBytes) {
		t.Fatalf("backup contents do not match original")
	}
}

func TestInstall_SecondTouch_DoesNotOverwriteBackup(t *testing.T) {
	cwd := projectInTempDir(t)
	settingsDir := filepath.Join(cwd, ".claude")
	_ = os.MkdirAll(settingsDir, 0o755)
	settingsPath := filepath.Join(settingsDir, "settings.json")
	originalBytes := []byte(`{"unrelated":"value"}`)
	_ = os.WriteFile(settingsPath, originalBytes, 0o600)

	_, backup1, _ := Install(ScopeProject, cwd, "x")
	if backup1 == "" {
		t.Fatalf("first install should produce backup")
	}
	// Second install: the backup must be preserved (no overwrite).
	_, backup2, _ := Install(ScopeProject, cwd, "y")
	if backup2 != "" {
		t.Fatalf("second install must NOT create another backup, got %q", backup2)
	}
	got, _ := os.ReadFile(backup1)
	if string(got) != string(originalBytes) {
		t.Fatalf("original backup was modified on second install")
	}
}

func TestUninstall_RemovesGovernanceEntries_PreservesOthers(t *testing.T) {
	cwd := projectInTempDir(t)
	settingsDir := filepath.Join(cwd, ".claude")
	_ = os.MkdirAll(settingsDir, 0o755)
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
	_ = os.WriteFile(settingsPath, b, 0o600)

	_, _, _ = Install(ScopeProject, cwd, "x")
	if _, err := Uninstall(ScopeProject, cwd); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	s := readSettings(t, settingsPath)
	pre := s["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("expected user entry preserved, got %d", len(pre))
	}
	if pre[0].(map[string]any)["matcher"] != "Read" {
		t.Fatalf("wrong entry preserved: %+v", pre[0])
	}
}

func TestUninstall_EmptyAfterRemoval_PrunesKeys(t *testing.T) {
	cwd := projectInTempDir(t)
	_, _, _ = Install(ScopeProject, cwd, "x") // no other hooks present
	if _, err := Uninstall(ScopeProject, cwd); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	settingsPath := filepath.Join(cwd, ".claude", "settings.json")
	s := readSettings(t, settingsPath)
	if _, has := s["hooks"]; has {
		t.Fatalf("hooks key should be removed when empty")
	}
}

func TestUninstall_MissingFileIsNoOp(t *testing.T) {
	cwd := projectInTempDir(t)
	if _, err := Uninstall(ScopeProject, cwd); err != nil {
		t.Fatalf("Uninstall on missing file should be no-op, got %v", err)
	}
}

func TestDryRun_DoesNotWriteFile(t *testing.T) {
	cwd := projectInTempDir(t)
	plan, err := DryRun(ScopeProject, cwd, "x")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if !plan.WouldWrite {
		t.Fatalf("WouldWrite=false; want true")
	}
	if _, err := os.Stat(plan.Path); !os.IsNotExist(err) {
		t.Fatalf("DryRun must not create %s; stat err=%v", plan.Path, err)
	}
}
