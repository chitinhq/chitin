package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const testAdapter = "/usr/local/bin/chitin-claude-code-adapter"

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}

func writeJSON(t *testing.T, path string, m map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func settingsPathFor(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".claude", "settings.json")
}

// chitinEntryFor walks the hook list for event h and returns chitin's wrapper
// (the entry with _tag==chitin). nil if absent.
func chitinEntryFor(settings map[string]any, h string) map[string]any {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	list, ok := hooks[h].([]any)
	if !ok {
		return nil
	}
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if m["_tag"] == chitinTag {
			return m
		}
	}
	return nil
}

func TestInstallGlobal_WritesWrappedHooksIntoMissingFile(t *testing.T) {
	path := settingsPathFor(t)

	if err := InstallGlobal(testAdapter); err != nil {
		t.Fatal(err)
	}

	s := readJSON(t, path)
	for _, h := range SubscribedHooks {
		entry := chitinEntryFor(s, h)
		if entry == nil {
			t.Errorf("hook %s: missing chitin wrapper", h)
			continue
		}
		if entry["matcher"] != "" {
			t.Errorf("hook %s: want matcher=\"\", got %v", h, entry["matcher"])
		}
		inner, ok := entry["hooks"].([]any)
		if !ok || len(inner) != 1 {
			t.Errorf("hook %s: want 1 inner hook, got %v", h, entry["hooks"])
			continue
		}
		cmd, _ := inner[0].(map[string]any)
		if cmd["type"] != "command" || cmd["command"] != testAdapter {
			t.Errorf("hook %s: wrong inner hook: %v", h, cmd)
		}
	}
}

func TestInstallGlobal_PreservesExistingEntriesAndTopLevelKeys(t *testing.T) {
	path := settingsPathFor(t)
	writeJSON(t, path, map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/usr/local/bin/other-tool"},
					},
				},
			},
		},
	})

	if err := InstallGlobal(testAdapter); err != nil {
		t.Fatal(err)
	}

	s := readJSON(t, path)
	if s["theme"] != "dark" {
		t.Errorf("theme lost: %v", s["theme"])
	}
	hooks := s["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("PreToolUse: want 2 entries (existing + chitin), got %d", len(pre))
	}
	// Verify the existing other-tool entry is preserved untouched.
	found := false
	for _, e := range pre {
		m, _ := e.(map[string]any)
		if m["matcher"] == "Bash" {
			inner, _ := m["hooks"].([]any)
			cmd, _ := inner[0].(map[string]any)
			if cmd["command"] == "/usr/local/bin/other-tool" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("existing non-chitin entry corrupted or removed")
	}
}

func TestInstallGlobal_Idempotent(t *testing.T) {
	path := settingsPathFor(t)

	if err := InstallGlobal(testAdapter); err != nil {
		t.Fatal(err)
	}
	if err := InstallGlobal(testAdapter); err != nil {
		t.Fatal(err)
	}

	s := readJSON(t, path)
	hooks := s["hooks"].(map[string]any)
	for _, h := range SubscribedHooks {
		list, _ := hooks[h].([]any)
		chitinCount := 0
		for _, e := range list {
			m, _ := e.(map[string]any)
			if m["_tag"] == chitinTag {
				chitinCount++
			}
		}
		if chitinCount != 1 {
			t.Errorf("hook %s: want 1 chitin entry after double-install, got %d", h, chitinCount)
		}
	}
}

func TestInstallGlobal_ReplacesStaleChitinEntryOnPathChange(t *testing.T) {
	path := settingsPathFor(t)

	if err := InstallGlobal("/old/path/adapter"); err != nil {
		t.Fatal(err)
	}
	if err := InstallGlobal("/new/path/adapter"); err != nil {
		t.Fatal(err)
	}

	s := readJSON(t, path)
	for _, h := range SubscribedHooks {
		entry := chitinEntryFor(s, h)
		if entry == nil {
			t.Errorf("hook %s: chitin entry missing after re-install", h)
			continue
		}
		inner, _ := entry["hooks"].([]any)
		cmd, _ := inner[0].(map[string]any)
		if cmd["command"] != "/new/path/adapter" {
			t.Errorf("hook %s: want /new/path/adapter, got %v", h, cmd["command"])
		}
	}
}

func TestInstallGlobal_MalformedJSONReturnsError(t *testing.T) {
	path := settingsPathFor(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := InstallGlobal(testAdapter)
	if err == nil {
		t.Fatal("want error on malformed JSON, got nil")
	}
}

func TestInstallGlobal_HandlesNullHooksKey(t *testing.T) {
	path := settingsPathFor(t)
	writeJSON(t, path, map[string]any{"theme": "dark", "hooks": nil})

	if err := InstallGlobal(testAdapter); err != nil {
		t.Fatal(err)
	}

	s := readJSON(t, path)
	if s["theme"] != "dark" {
		t.Errorf("theme lost")
	}
	for _, h := range SubscribedHooks {
		if chitinEntryFor(s, h) == nil {
			t.Errorf("hook %s: chitin entry missing after install into null-hooks settings", h)
		}
	}
}

func TestUninstallGlobal_RemovesOnlyChitinEntries(t *testing.T) {
	path := settingsPathFor(t)
	writeJSON(t, path, map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/usr/local/bin/other-tool"},
					},
				},
			},
		},
	})

	if err := InstallGlobal(testAdapter); err != nil {
		t.Fatal(err)
	}
	if err := UninstallGlobal(); err != nil {
		t.Fatal(err)
	}

	s := readJSON(t, path)
	if s["theme"] != "dark" {
		t.Errorf("theme lost after uninstall")
	}
	hooks, _ := s["hooks"].(map[string]any)
	pre, _ := hooks["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("want 1 PreToolUse entry after uninstall, got %d", len(pre))
	}
	entry, _ := pre[0].(map[string]any)
	if entry["matcher"] != "Bash" {
		t.Errorf("uninstall removed the wrong entry: %v", entry)
	}
	for _, h := range SubscribedHooks {
		if chitinEntryFor(s, h) != nil {
			t.Errorf("hook %s: chitin entry still present after uninstall", h)
		}
	}
}

func TestUninstallGlobal_MissingFileIsNoOp(t *testing.T) {
	_ = settingsPathFor(t) // sets HOME; no file created

	if err := UninstallGlobal(); err != nil {
		t.Errorf("uninstall on missing file should be no-op, got: %v", err)
	}
}

func TestInvariant_UninstallOfInstallRestoresOriginal(t *testing.T) {
	cases := []struct {
		name    string
		initial map[string]any
	}{
		{
			name: "empty-settings",
			initial: map[string]any{},
		},
		{
			name: "theme-only",
			initial: map[string]any{"theme": "dark", "fontSize": 14.0},
		},
		{
			name: "existing-unrelated-hook",
			initial: map[string]any{
				"theme": "dark",
				"hooks": map[string]any{
					"PreToolUse": []any{
						map[string]any{
							"matcher": "Bash",
							"hooks": []any{
								map[string]any{"type": "command", "command": "/other"},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := settingsPathFor(t)
			if len(tc.initial) > 0 {
				writeJSON(t, path, tc.initial)
			}

			// Snapshot the expected final state: same as initial (written through
			// the same JSON encoder so trailing-newline / map-ordering differences
			// don't cause false failures).
			var want map[string]any
			if len(tc.initial) == 0 {
				want = map[string]any{}
			} else {
				writeJSON(t, path, tc.initial)
				want = readJSON(t, path)
			}

			if err := InstallGlobal(testAdapter); err != nil {
				t.Fatal(err)
			}
			if err := UninstallGlobal(); err != nil {
				t.Fatal(err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				if len(tc.initial) == 0 {
					return // OK: started missing, ended missing-or-empty is fine
				}
				t.Fatalf("read after roundtrip: %v", err)
			}
			var gotMap map[string]any
			if err := json.Unmarshal(got, &gotMap); err != nil {
				t.Fatal(err)
			}
			// After uninstall, hooks map may be empty; normalize before comparing.
			if h, ok := gotMap["hooks"].(map[string]any); ok && len(h) == 0 {
				delete(gotMap, "hooks")
			}
			if w, ok := want["hooks"].(map[string]any); ok && len(w) == 0 {
				delete(want, "hooks")
			}
			if !reflect.DeepEqual(gotMap, want) {
				t.Errorf("roundtrip mismatch:\n  got:  %v\n  want: %v", gotMap, want)
			}
		})
	}
}
