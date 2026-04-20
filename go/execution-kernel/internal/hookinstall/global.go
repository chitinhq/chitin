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

// writeSettings marshals settings to JSON and writes them to path atomically
// via temp-file-and-rename (rename is atomic on POSIX). Mode is preserved
// from the existing file, defaulting to 0o600 for new files — settings.json
// may contain sensitive credentials, so it should not be world-readable.
func writeSettings(path string, s map[string]any) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", path, statErr)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".settings.json.tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}
	cleanup = false
	return nil
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
