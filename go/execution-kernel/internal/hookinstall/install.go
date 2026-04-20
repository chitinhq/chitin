// Package hookinstall writes and removes per-session Claude Code hook settings overlays.
package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SubscribedHooks is the canonical list of Claude Code hook event names
// that chitin forwards to the kernel. Shared between session-scoped and
// global installs.
var SubscribedHooks = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PreCompact",
	"SubagentStop",
	"SessionEnd",
}

// Install writes .chitin/sessions/<session>/settings.json registering adapterBinary for all subscribed hooks.
func Install(chitinDir, sessionID, adapterBinary string) error {
	sessionDir := filepath.Join(chitinDir, "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}
	hooks := make(map[string]any, len(SubscribedHooks))
	for _, h := range SubscribedHooks {
		hooks[h] = []any{
			map[string]any{
				"type":    "command",
				"command": adapterBinary,
			},
		}
	}
	settings := map[string]any{
		"hooks": hooks,
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(sessionDir, "settings.json"), b, 0o644)
}

// Uninstall removes the session overlay directory.
func Uninstall(chitinDir, sessionID string) error {
	sessionDir := filepath.Join(chitinDir, "sessions", sessionID)
	return os.RemoveAll(sessionDir)
}
