package speclint

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckL06_RealOnDiskSpec115 reads the actual spec 115 + tasks 115
// from the worktree and runs L06 against them with spec 113 + 097 as
// deps (the depends_on declared by spec 115's frontmatter). Acts as a
// final integration sanity check.
func TestCheckL06_RealOnDiskSpec115(t *testing.T) {
	// Locate the repo root by walking up from this file until we see
	// the .specify/specs/115-* directory.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	var repoRoot string
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "specs", "115-spec-review-gate")
		if _, err := os.Stat(candidate); err == nil {
			repoRoot = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if repoRoot == "" {
		t.Skip("repo root not found from cwd; skipping on-disk smoke")
	}

	specDir := filepath.Join(repoRoot, "specs", "115-spec-review-gate")
	specBytes, err := os.ReadFile(filepath.Join(specDir, "spec.md"))
	if err != nil {
		t.Fatalf("read spec.md: %v", err)
	}
	tasksBytes, err := os.ReadFile(filepath.Join(specDir, "tasks.md"))
	if err != nil {
		t.Fatalf("read tasks.md: %v", err)
	}

	depIDs := L06DependsOnIDs(string(specBytes))
	var depContents []string
	for _, id := range depIDs {
		matches, err := filepath.Glob(filepath.Join(repoRoot, "specs", id+"-*"))
		if err != nil || len(matches) != 1 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(matches[0], "spec.md"))
		if err != nil {
			continue
		}
		depContents = append(depContents, string(b))
	}

	got := CheckL06("specs/115-spec-review-gate/spec.md", string(specBytes),
		"specs/115-spec-review-gate/tasks.md", string(tasksBytes), depContents)
	if len(got) > 0 {
		t.Errorf("on-disk spec 115 should lint clean against its declared deps %v, got %d violations:", depIDs, len(got))
		for _, v := range got {
			t.Errorf("  %s:%d %s — %s", v.File, v.Line, v.Severity, v.Message)
		}
	}
}
