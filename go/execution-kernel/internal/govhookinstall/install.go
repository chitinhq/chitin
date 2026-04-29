// Package govhookinstall installs the chitin governance hook into
// Claude Code's settings.json. Distinct from internal/hookinstall, which
// installs the chain-recording adapter — the two coexist as separate
// PreToolUse entries with different tags so install/uninstall don't
// step on each other.
//
// The governance hook fires `chitin-kernel gate evaluate --hook-stdin
// --agent=claude-code` on every Bash/Edit/Write/NotebookEdit/Read/
// WebFetch/WebSearch/Task. Allow → exit 0; deny → exit 2 with a JSON
// reason that Claude Code surfaces back to the model.
//
// Both global (~/.claude/settings.json) and project (.claude/settings.json)
// scopes are supported. Each install creates a one-time backup of the
// settings file at <path>.chitin-backup-<utc-ts> the first time chitin
// touches it, so an operator always has a pre-chitin restore point.
package govhookinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// chitinTag identifies governance-hook entries owned by chitin. Distinct
// from internal/hookinstall's "chitin" tag so the two installs don't
// remove each other on uninstall.
const chitinTag = "chitin-governance"

// matcher restricts the governance hook to tool names chitin knows how
// to normalize. Future Claude Code tools we haven't modeled fall through
// (no matcher = no chitin governance for them) so we don't gate on
// unknowns. The audit log sees only matched tools.
const matcher = "Bash|Edit|Write|NotebookEdit|Read|WebFetch|WebSearch|Task|Glob|Grep|LS|TodoWrite"

// HookCommand is the literal command Claude Code spawns on PreToolUse.
// Exposed for the cmd-layer install to override (e.g. with --envelope).
const HookCommand = "chitin-kernel gate evaluate --hook-stdin --agent=claude-code"

// Scope is global (~/.claude/settings.json) or project (.claude/settings.json
// from cwd). Operators choose at install time.
type Scope int

const (
	ScopeGlobal Scope = iota
	ScopeProject
)

// Install merges the chitin governance hook into the settings file at
// the given scope, idempotently. Pre-existing chitin-governance entries
// are replaced so reinstall picks up command-string changes (e.g. when
// the operator passes a new --envelope flag). Other entries are
// preserved verbatim.
//
// command is the full hook command line. cwd is only used for ScopeProject;
// global ignores it.
//
// Returns the absolute path of the settings file that was written, plus
// the backup path (empty if no backup was taken — only the first chitin
// touch creates a backup).
func Install(scope Scope, cwd, command string) (path, backup string, err error) {
	path, err = settingsPath(scope, cwd)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir settings dir: %w", err)
	}
	settings, existed, err := loadSettings(path)
	if err != nil {
		return "", "", err
	}
	if existed {
		backup, err = ensureBackup(path)
		if err != nil {
			return "", "", err
		}
	}

	hooks := ensureHooksMap(settings)
	preToolUse := toAnySlice(hooks["PreToolUse"])
	preToolUse = filterOutChitinGovernance(preToolUse)
	preToolUse = append(preToolUse, govWrapper(command))
	hooks["PreToolUse"] = preToolUse
	settings["hooks"] = hooks

	if err := writeSettingsAtomic(path, settings); err != nil {
		return path, backup, err
	}
	return path, backup, nil
}

// Uninstall removes chitin's governance hook entries (identified by
// _tag == chitin-governance) from PreToolUse. Other entries are
// preserved. If PreToolUse becomes empty it is removed; if hooks
// becomes empty it is removed. Missing settings file is a no-op.
func Uninstall(scope Scope, cwd string) (path string, err error) {
	path, err = settingsPath(scope, cwd)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return path, nil
	}
	settings, _, err := loadSettings(path)
	if err != nil {
		return path, err
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return path, nil
	}
	preToolUse := toAnySlice(hooks["PreToolUse"])
	filtered := filterOutChitinGovernance(preToolUse)
	if len(filtered) == 0 {
		delete(hooks, "PreToolUse")
	} else {
		hooks["PreToolUse"] = filtered
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	if err := writeSettingsAtomic(path, settings); err != nil {
		return path, err
	}
	return path, nil
}

// DryRunPlan reports what Install would do without writing. Returns the
// settings path, the would-be backup path (empty if file doesn't exist
// or backup already present), the planned PreToolUse entry, and the
// number of non-chitin entries that would be preserved.
type Plan struct {
	Path           string
	Backup         string
	BackupExists   bool
	WouldWrite     bool
	PreservedCount int
	WrapperCommand string
}

// DryRun returns a Plan describing the install without performing it.
// Useful for `install claude-code-hook --dry-run`.
func DryRun(scope Scope, cwd, command string) (Plan, error) {
	path, err := settingsPath(scope, cwd)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Path: path, WrapperCommand: command, WouldWrite: true}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return plan, nil
	}
	settings, _, err := loadSettings(path)
	if err != nil {
		return plan, err
	}
	if backupPath, exists := existingBackup(path); exists {
		plan.Backup = backupPath
		plan.BackupExists = true
	} else {
		plan.Backup = backupNameFor(path)
	}
	if hooks, ok := settings["hooks"].(map[string]any); ok {
		preToolUse := toAnySlice(hooks["PreToolUse"])
		preserved := filterOutChitinGovernance(preToolUse)
		plan.PreservedCount = len(preserved)
	}
	return plan, nil
}

func govWrapper(command string) map[string]any {
	return map[string]any{
		"_tag":    chitinTag,
		"matcher": matcher,
		"hooks": []any{
			map[string]any{"type": "command", "command": command},
		},
	}
}

func filterOutChitinGovernance(list []any) []any {
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

func settingsPath(scope Scope, cwd string) (string, error) {
	switch scope {
	case ScopeGlobal:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	case ScopeProject:
		abs, err := filepath.Abs(cwd)
		if err != nil {
			return "", fmt.Errorf("abs cwd: %w", err)
		}
		return filepath.Join(abs, ".claude", "settings.json"), nil
	}
	return "", fmt.Errorf("unknown scope: %d", scope)
}

// ensureBackup writes <path>.chitin-backup-<utc-ts> if no chitin backup
// of this path exists yet. Returns the backup path (empty if a backup
// already existed and we didn't overwrite). The backup is the operator's
// pre-chitin restore point and must NOT be overwritten by reinstalls —
// that would erase the original state on second touch.
//
// File mode is preserved from the original — a 0o644 settings.json
// stays 0o644 in the backup. Defaults to 0o600 if the original is
// missing (shouldn't happen since we only call after stat succeeds).
func ensureBackup(path string) (string, error) {
	if _, exists := existingBackup(path); exists {
		return "", nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read for backup: %w", err)
	}
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	backup := backupNameFor(path)
	if err := os.WriteFile(backup, src, mode); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	return backup, nil
}

func backupNameFor(path string) string {
	ts := time.Now().UTC().Format("20060102T150405Z")
	return path + ".chitin-backup-" + ts
}

// existingBackup looks for any sibling file whose name starts with
// <basename>.chitin-backup-. Returns the first match.
func existingBackup(path string) (string, bool) {
	dir := filepath.Dir(path)
	prefix := filepath.Base(path) + ".chitin-backup-"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) > len(prefix) && e.Name()[:len(prefix)] == prefix {
			return filepath.Join(dir, e.Name()), true
		}
	}
	return "", false
}

func loadSettings(path string) (map[string]any, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, false, nil
		}
		return nil, false, err
	}
	if len(b) == 0 {
		return map[string]any{}, true, nil
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, true, fmt.Errorf("parse %s: %w", path, err)
	}
	if s == nil {
		s = map[string]any{}
	}
	return s, true, nil
}

func writeSettingsAtomic(path string, s map[string]any) error {
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
