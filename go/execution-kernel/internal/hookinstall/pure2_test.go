package hookinstall

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
	input := []any{"a", "b"}
	if got := toAnySlice(input); len(got) != 2 || got[0] != "a" {
		t.Errorf("toAnySlice([]any) = %v, want [a b]", got)
	}
}

func TestGlobalSettingsPath(t *testing.T) {
	path, err := globalSettingsPath()
	if err != nil {
		t.Fatalf("globalSettingsPath: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}