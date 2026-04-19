package hookinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// globalSettingsPath returns $HOME/.claude/settings.json.
func globalSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// InstallGlobal merges chitin's hook entries into ~/.claude/settings.json.
// adapterBinary is the absolute path to the Claude Code adapter CLI.
// Pre-existing non-chitin hook entries are preserved.
func InstallGlobal(adapterBinary string) error {
	path, err := globalSettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	settings, err := loadSettings(path)
	if err != nil {
		return err
	}
	hooks := ensureHooksMap(settings)

	chitinEntry := map[string]any{"type": "command", "command": adapterBinary}
	for _, h := range SubscribedHooks {
		list := toAnySlice(hooks[h])
		if !containsAdapter(list, adapterBinary) {
			list = append(list, chitinEntry)
		}
		hooks[h] = list
	}
	settings["hooks"] = hooks
	return writeSettings(path, settings)
}

// UninstallGlobal removes entries whose command equals adapterBinary.
// Leaves unrelated hook entries intact.
func UninstallGlobal(adapterBinary string) error {
	path, err := globalSettingsPath()
	if err != nil {
		return err
	}
	settings, err := loadSettings(path)
	if err != nil {
		return err
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil // nothing to uninstall
	}
	for _, h := range SubscribedHooks {
		list := toAnySlice(hooks[h])
		filtered := make([]any, 0, len(list))
		for _, e := range list {
			m, ok := e.(map[string]any)
			if !ok {
				filtered = append(filtered, e)
				continue
			}
			if m["command"] == adapterBinary {
				continue
			}
			filtered = append(filtered, m)
		}
		if len(filtered) == 0 {
			delete(hooks, h)
		} else {
			hooks[h] = filtered
		}
	}
	settings["hooks"] = hooks
	return writeSettings(path, settings)
}

func loadSettings(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if s == nil {
		s = map[string]any{}
	}
	return s, nil
}

func writeSettings(path string, s map[string]any) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func ensureHooksMap(settings map[string]any) map[string]any {
	h, ok := settings["hooks"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return h
}

func toAnySlice(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func containsAdapter(list []any, adapterBinary string) bool {
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			if m["command"] == adapterBinary {
				return true
			}
		}
	}
	return false
}
