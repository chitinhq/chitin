package govhookinstall

import "testing"

func TestToAnySlice(t *testing.T) {
	if got := toAnySlice(nil); got != nil {
		t.Errorf("toAnySlice(nil) = %v, want nil", got)
	}
	if got := toAnySlice("not a slice"); got != nil {
		t.Errorf("toAnySlice(string) = %v, want nil", got)
	}
	if got := toAnySlice(42); got != nil {
		t.Errorf("toAnySlice(int) = %v, want nil", got)
	}
	input := []any{"a", "b", "c"}
	if got := toAnySlice(input); len(got) != 3 || got[0] != "a" {
		t.Errorf("toAnySlice([]any) = %v, want [a b c]", got)
	}
}

func TestSettingsPath_Global(t *testing.T) {
	path, err := settingsPath(ScopeGlobal, "/any/cwd")
	if err != nil {
		t.Fatalf("settingsPath Global: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path for Global scope")
	}
}

func TestSettingsPath_Project(t *testing.T) {
	path, err := settingsPath(ScopeProject, "/tmp")
	if err != nil {
		t.Fatalf("settingsPath Project: %v", err)
	}
	if path != "/tmp/.claude/settings.json" {
		t.Errorf("settingsPath Project = %q, want /tmp/.claude/settings.json", path)
	}
}

func TestSettingsPath_InvalidScope(t *testing.T) {
	_, err := settingsPath(Scope(999), "/tmp")
	if err == nil {
		t.Error("expected error for invalid scope")
	}
}