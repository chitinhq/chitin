package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallGlobal_WritesHooksIntoEmptyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	if err := InstallGlobal("/usr/local/bin/chitin-claude-code-adapter"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json missing: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("expected hooks map, got %T", s["hooks"])
	}
	for _, h := range SubscribedHooks {
		if _, ok := hooks[h]; !ok {
			t.Errorf("missing hook %s", h)
		}
	}
}

func TestInstallGlobal_MergesIntoExistingSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	settingsPath := filepath.Join(claudeDir, "settings.json")

	existing := map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"type": "command", "command": "/usr/local/bin/other-tool"},
			},
		},
	}
	b, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(settingsPath, b, 0o644)

	if err := InstallGlobal("/usr/local/bin/chitin-claude-code-adapter"); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var s map[string]any
	json.Unmarshal(raw, &s)
	if s["theme"] != "dark" {
		t.Errorf("theme lost, got %v", s["theme"])
	}
	hooks := s["hooks"].(map[string]any)
	pre, _ := hooks["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Errorf("expected 2 PreToolUse entries (existing + chitin), got %d", len(pre))
	}
}

func TestInstallGlobal_IsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	adapterPath := "/usr/local/bin/chitin-claude-code-adapter"

	if err := InstallGlobal(adapterPath); err != nil {
		t.Fatal(err)
	}
	if err := InstallGlobal(adapterPath); err != nil {
		t.Fatal(err)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	raw, _ := os.ReadFile(settingsPath)
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	hooks := s["hooks"].(map[string]any)
	for _, h := range SubscribedHooks {
		list, _ := hooks[h].([]any)
		chitinCount := 0
		for _, e := range list {
			m := e.(map[string]any)
			if m["command"] == adapterPath {
				chitinCount++
			}
		}
		if chitinCount != 1 {
			t.Errorf("hook %s has %d chitin entries, want 1", h, chitinCount)
		}
	}
}

func TestUninstallGlobal_RemovesOnlyChitinEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	settingsPath := filepath.Join(claudeDir, "settings.json")

	adapterPath := "/usr/local/bin/chitin-claude-code-adapter"
	existing := map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"type": "command", "command": "/usr/local/bin/other-tool"},
			},
		},
	}
	b, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(settingsPath, b, 0o644)

	if err := InstallGlobal(adapterPath); err != nil {
		t.Fatal(err)
	}
	if err := UninstallGlobal(adapterPath); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var s map[string]any
	json.Unmarshal(raw, &s)
	if s["theme"] != "dark" {
		t.Errorf("theme lost after uninstall")
	}
	hooks := s["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("want 1 PreToolUse after uninstall, got %d", len(pre))
	}
	entry := pre[0].(map[string]any)
	if entry["command"] != "/usr/local/bin/other-tool" {
		t.Errorf("uninstall removed the wrong entry: %v", entry)
	}
}
