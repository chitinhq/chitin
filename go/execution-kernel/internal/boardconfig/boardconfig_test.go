package boardconfig

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFieldFromConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeBoardConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	got, err := ResolveField("chitin", "repo")
	if err != nil {
		t.Fatalf("ResolveField: %v", err)
	}
	if got != "chitinhq/chitin" {
		t.Fatalf("repo = %q, want chitinhq/chitin", got)
	}
}

func TestResolveFieldEnvOverridesConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KANBAN_BOARD_REPO", "override/repo")
	writeBoardConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	got, err := ResolveField("chitin", "repo")
	if err != nil {
		t.Fatalf("ResolveField: %v", err)
	}
	if got != "override/repo" {
		t.Fatalf("repo = %q, want override/repo", got)
	}
}

func TestResolveFieldMissingRequiredField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeBoardConfig(t, home, "chitin", `{"default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	_, err := ResolveField("chitin", "repo")
	var missingErr MissingFieldError
	if !errors.As(err, &missingErr) {
		t.Fatalf("want MissingFieldError, got %v", err)
	}
	if missingErr.Field != "repo" {
		t.Fatalf("missing field = %q, want repo", missingErr.Field)
	}
}

func TestResolveFieldUnknownBoard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeBoardConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	_, err := ResolveField("readybench", "repo")
	var boardErr UnknownBoardError
	if !errors.As(err, &boardErr) {
		t.Fatalf("want UnknownBoardError, got %v", err)
	}
	if boardErr.Slug != "readybench" {
		t.Fatalf("slug = %q, want readybench", boardErr.Slug)
	}
}

func TestResolveFieldEnvDoesNotSynthesizeUnknownBoard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KANBAN_BOARD_REPO", "override/repo")
	writeBoardConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	_, err := ResolveField("readybench", "repo")
	var boardErr UnknownBoardError
	if !errors.As(err, &boardErr) {
		t.Fatalf("want UnknownBoardError, got %v", err)
	}
	if boardErr.Slug != "readybench" {
		t.Fatalf("slug = %q, want readybench", boardErr.Slug)
	}
}

func TestResolveFieldNoBoardsInitialized(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := ResolveField("chitin", "repo")
	if !errors.Is(err, ErrNoBoardsInitialized) {
		t.Fatalf("want ErrNoBoardsInitialized, got %v", err)
	}
}

func TestResolveFieldEnvDoesNotSynthesizeNoBoardsInitialized(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KANBAN_BOARD_REPO", "override/repo")

	_, err := ResolveField("chitin", "repo")
	if !errors.Is(err, ErrNoBoardsInitialized) {
		t.Fatalf("want ErrNoBoardsInitialized, got %v", err)
	}
}

func TestResolveFieldMissingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	boardDir := filepath.Join(home, ".hermes", "kanban", "boards", "chitin")
	if err := os.MkdirAll(boardDir, 0o755); err != nil {
		t.Fatalf("mkdir board dir: %v", err)
	}

	_, err := ResolveField("chitin", "repo")
	var configErr MissingConfigError
	if !errors.As(err, &configErr) {
		t.Fatalf("want MissingConfigError, got %v", err)
	}
}

func TestResolveFieldEnvDoesNotSynthesizeMissingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KANBAN_BOARD_REPO", "override/repo")
	boardDir := filepath.Join(home, ".hermes", "kanban", "boards", "chitin")
	if err := os.MkdirAll(boardDir, 0o755); err != nil {
		t.Fatalf("mkdir board dir: %v", err)
	}

	_, err := ResolveField("chitin", "repo")
	var configErr MissingConfigError
	if !errors.As(err, &configErr) {
		t.Fatalf("want MissingConfigError, got %v", err)
	}
}

func TestResolveFieldOptionalDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeBoardConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	got, err := ResolveField("chitin", "chitin_yaml")
	if err != nil {
		t.Fatalf("ResolveField: %v", err)
	}
	if got != "chitin.yaml" {
		t.Fatalf("chitin_yaml = %q, want chitin.yaml", got)
	}
}

func TestResolveFieldSoulMapDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeBoardConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	got, err := ResolveField("chitin", "soul_map")
	if err != nil {
		t.Fatalf("ResolveField: %v", err)
	}
	want := `{"correctness":"knuth","architecture":"davinci","dispatch":"sun-tzu","research":"socrates","default":"sun-tzu"}`
	if got != want {
		t.Fatalf("soul_map = %q, want %q", got, want)
	}
}

func TestResolveFieldOptionalDefaultStillRequiresCompleteConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeBoardConfig(t, home, "chitin", `{"default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	_, err := ResolveField("chitin", "chitin_yaml")
	var missingErr MissingFieldError
	if !errors.As(err, &missingErr) {
		t.Fatalf("want MissingFieldError, got %v", err)
	}
	if missingErr.Field != "repo" {
		t.Fatalf("missing field = %q, want repo", missingErr.Field)
	}
}

func TestResolveFieldEnvCanCompleteRequiredConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KANBAN_BOARD_REPO", "override/repo")
	writeBoardConfig(t, home, "chitin", `{"default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	got, err := ResolveField("chitin", "chitin_yaml")
	if err != nil {
		t.Fatalf("ResolveField: %v", err)
	}
	if got != "chitin.yaml" {
		t.Fatalf("chitin_yaml = %q, want chitin.yaml", got)
	}
}

func TestResolveFieldRejectsPathTraversalSlug(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeBoardConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"/abs/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	for _, slug := range []string{"", ".", "..", "../etc", "chitin/../etc", `chitin\evil`, "with/slash"} {
		_, err := ResolveField(slug, "repo")
		var invalid InvalidSlugError
		if !errors.As(err, &invalid) {
			t.Fatalf("slug %q: want InvalidSlugError, got %v", slug, err)
		}
	}
}

func TestResolveFieldExpandsTildeInWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeBoardConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	got, err := ResolveField("chitin", "workspace_root")
	if err != nil {
		t.Fatalf("ResolveField: %v", err)
	}
	want := filepath.Join(home, "workspace", "chitin")
	if got != want {
		t.Fatalf("workspace_root = %q, want %q", got, want)
	}
}

func TestResolveFieldExpandsTildeFromEnvOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KANBAN_BOARD_WORKSPACE_ROOT", "~/other-root")
	writeBoardConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"/abs/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	got, err := ResolveField("chitin", "workspace_root")
	if err != nil {
		t.Fatalf("ResolveField: %v", err)
	}
	want := filepath.Join(home, "other-root")
	if got != want {
		t.Fatalf("workspace_root = %q, want %q", got, want)
	}
}

func TestResolveFieldDoesNotExpandTildeForNonPathFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeBoardConfig(t, home, "chitin", `{"repo":"~/literal","default_branch":"main","workspace_root":"/abs","kernel_bin":"chitin-kernel"}`)

	got, err := ResolveField("chitin", "repo")
	if err != nil {
		t.Fatalf("ResolveField: %v", err)
	}
	if got != "~/literal" {
		t.Fatalf("repo = %q, want ~/literal", got)
	}
}

func writeBoardConfig(t *testing.T, home, slug, raw string) {
	t.Helper()
	dir := filepath.Join(home, ".hermes", "kanban", "boards", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir board dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
