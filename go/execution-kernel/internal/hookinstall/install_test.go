package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstall_CreatesSettingsOverlay(t *testing.T) {
	dir := t.TempDir()
	chitinDir := filepath.Join(dir, ".chitin")
	os.MkdirAll(filepath.Join(chitinDir, "sessions"), 0o755)
	sessionID := "sess-xyz"
	adapterBin := "/opt/chitin/adapter-cc"

	if err := Install(chitinDir, sessionID, adapterBin); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chitinDir, "sessions", sessionID, "settings.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatal(err)
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("expected hooks map, got %T", settings["hooks"])
	}
	wantHooks := []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PreCompact", "SubagentStop", "SessionEnd"}
	for _, h := range wantHooks {
		if _, ok := hooks[h]; !ok {
			t.Errorf("missing hook %s", h)
		}
	}
}

func TestUninstall_RemovesOverlay(t *testing.T) {
	dir := t.TempDir()
	chitinDir := filepath.Join(dir, ".chitin")
	os.MkdirAll(filepath.Join(chitinDir, "sessions"), 0o755)
	sessionID := "sess-xyz"
	Install(chitinDir, sessionID, "/opt/adapter")
	if err := Uninstall(chitinDir, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(chitinDir, "sessions", sessionID)); !os.IsNotExist(err) {
		t.Errorf("session dir still exists: %v", err)
	}
}
