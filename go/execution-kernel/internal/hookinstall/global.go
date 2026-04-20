package hookinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// chitinTag identifies wrapper entries owned by chitin in
// ~/.claude/settings.json. Stable across binary-path changes so reinstall
// and uninstall reliably target our entries without depending on the
// adapter path string.
const chitinTag = "chitin"

func globalSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// InstallGlobal merges chitin's hook entries into ~/.claude/settings.json.
// adapterBinary is the absolute path to the Claude Code adapter CLI.
//
// Each subscribed hook event gets a wrapper entry of the form
//
//	{"_tag": "chitin", "matcher": "", "hooks": [{"type": "command", "command": adapterBinary}]}
//
// Pre-existing non-chitin entries are preserved verbatim. If a chitin
// wrapper already exists for an event, it is replaced (idempotency +
// path-change tolerance).
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

	for _, h := range SubscribedHooks {
		list := toAnySlice(hooks[h])
		list = filterOutChitin(list)
		list = append(list, chitinWrapper(adapterBinary))
		hooks[h] = list
	}
	settings["hooks"] = hooks
	return writeSettings(path, settings)
}

// UninstallGlobal removes chitin's wrapper entries (identified by
// _tag == chitinTag) from every subscribed hook event in
// ~/.claude/settings.json. Unrelated entries are preserved. Hook events
// whose lists become empty are removed; the top-level "hooks" key is
// removed if all subscribed lists become empty. Missing settings.json
// is a no-op.
func UninstallGlobal() error {
	path, err := globalSettingsPath()
	if err != nil {
		return err
	}
	settings, err := loadSettings(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	for _, h := range SubscribedHooks {
		list := toAnySlice(hooks[h])
		filtered := filterOutChitin(list)
		if len(filtered) == 0 {
			delete(hooks, h)
		} else {
			hooks[h] = filtered
		}
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	return writeSettings(path, settings)
}

func chitinWrapper(adapterBinary string) map[string]any {
	return map[string]any{
		"_tag":    chitinTag,
		"matcher": "",
		"hooks": []any{
			map[string]any{"type": "command", "command": adapterBinary},
		},
	}
}

func filterOutChitin(list []any) []any {
	out := make([]any, 0, len(list))
	for _, e := range list {
		m, ok := e.(map[string]any)
		if ok && m["_tag"] == chitinTag {
			continue
		}
		out = append(out, e)
	}
	return out
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
