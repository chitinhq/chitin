package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_BoardConfigFromConfig(t *testing.T) {
	home := t.TempDir()
	writeCLIConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	stdout, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home}, "board-config", "chitin", "repo")
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "chitinhq/chitin" {
		t.Fatalf("stdout=%q, want chitinhq/chitin", stdout)
	}
}

func TestCLI_BoardConfigEnvOverridesConfig(t *testing.T) {
	home := t.TempDir()
	writeCLIConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	stdout, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{
		"HOME=" + home,
		"KANBAN_BOARD_REPO=override/repo",
	}, "board-config", "chitin", "repo")
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "override/repo" {
		t.Fatalf("stdout=%q, want override/repo", stdout)
	}
}

func TestCLI_BoardConfigMissingFieldExit2(t *testing.T) {
	home := t.TempDir()
	writeCLIConfig(t, home, "chitin", `{"default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home}, "board-config", "chitin", "repo")
	if code != 2 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "missing field: repo") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestCLI_BoardConfigUnknownBoardExit3(t *testing.T) {
	home := t.TempDir()
	writeCLIConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home}, "board-config", "readybench", "repo")
	if code != 3 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "unknown board: readybench") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestCLI_BoardConfigEnvSetUnknownBoardExit3(t *testing.T) {
	home := t.TempDir()
	writeCLIConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{
		"HOME=" + home,
		"KANBAN_BOARD_REPO=override/repo",
	}, "board-config", "readybench", "repo")
	if code != 3 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "unknown board: readybench") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestCLI_BoardConfigNoBoardsInitializedExit3(t *testing.T) {
	home := t.TempDir()

	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home}, "board-config", "chitin", "repo")
	if code != 3 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "no boards initialized") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestCLI_BoardConfigEnvSetNoBoardsInitializedExit3(t *testing.T) {
	home := t.TempDir()

	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{
		"HOME=" + home,
		"KANBAN_BOARD_REPO=override/repo",
	}, "board-config", "chitin", "repo")
	if code != 3 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "no boards initialized") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestCLI_BoardConfigEnvSetMissingConfigExit2(t *testing.T) {
	home := t.TempDir()
	boardDir := filepath.Join(home, ".hermes", "kanban", "boards", "chitin")
	if err := os.MkdirAll(boardDir, 0o755); err != nil {
		t.Fatalf("mkdir board dir: %v", err)
	}

	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{
		"HOME=" + home,
		"KANBAN_BOARD_REPO=override/repo",
	}, "board-config", "chitin", "repo")
	if code != 2 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "missing config:") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestCLI_BoardConfigOptionalLookupStillFailsForMissingRequiredField(t *testing.T) {
	home := t.TempDir()
	writeCLIConfig(t, home, "chitin", `{"default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	_, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home}, "board-config", "chitin", "chitin_yaml")
	if code != 2 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "missing field: repo") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestCLI_BoardConfigSoulMapDefault(t *testing.T) {
	home := t.TempDir()
	writeCLIConfig(t, home, "chitin", `{"repo":"chitinhq/chitin","default_branch":"main","workspace_root":"~/workspace/chitin","kernel_bin":"chitin-kernel"}`)

	stdout, stderr, code := runCLIWithEnv(t, t.TempDir(), []string{"HOME=" + home}, "board-config", "chitin", "soul_map")
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	want := `{"correctness":"knuth","architecture":"davinci","dispatch":"sun-tzu","research":"socrates","default":"sun-tzu"}`
	if strings.TrimSpace(stdout) != want {
		t.Fatalf("stdout=%q, want %q", stdout, want)
	}
}

func writeCLIConfig(t *testing.T, home, slug, raw string) {
	t.Helper()
	dir := filepath.Join(home, ".hermes", "kanban", "boards", slug)
	writeFileForCLI(t, filepath.Join(dir, "config.json"), raw)
}
