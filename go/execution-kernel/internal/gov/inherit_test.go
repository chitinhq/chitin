package gov

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadWithInheritance_WalksParents(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "chitin.yaml"), `
id: root-policy
mode: enforce
rules:
  - id: root-deny
    action: shell.exec
    effect: deny
    target: "rm -rf"
    reason: "root"
`)
	leaf := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}

	p, sources, err := LoadWithInheritance(leaf)
	if err != nil {
		t.Fatalf("LoadWithInheritance: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("sources: got %d want 1", len(sources))
	}
	if p.ID != "root-policy" {
		t.Errorf("ID: got %q", p.ID)
	}
}

func TestLoadWithInheritance_ChildOverridesOnID(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "chitin.yaml"), `
id: parent
mode: enforce
rules:
  - id: shared-rule
    action: shell.exec
    effect: deny
    target: "rm"
    reason: "parent"
`)
	child := filepath.Join(root, "sub")
	writeFile(t, filepath.Join(child, "chitin.yaml"), `
id: child
mode: enforce
rules:
  - id: shared-rule
    action: shell.exec
    effect: deny
    target: "rm"
    reason: "child-overridden"
`)
	p, _, err := LoadWithInheritance(child)
	if err != nil {
		t.Fatalf("LoadWithInheritance: %v", err)
	}
	var found *Rule
	for i := range p.Rules {
		if p.Rules[i].ID == "shared-rule" {
			found = &p.Rules[i]
			break
		}
	}
	if found == nil {
		t.Fatal("shared-rule not in merged policy")
	}
	if found.Reason != "child-overridden" {
		t.Errorf("child should override parent on id collision, got reason=%q", found.Reason)
	}
}

func TestLoadWithInheritance_NoPolicyFound(t *testing.T) {
	dir := t.TempDir()
	_, _, err := LoadWithInheritance(dir)
	if err == nil {
		t.Fatal("expected error when no chitin.yaml found up the tree")
	}
}

// TestLoadWithInheritance_RejectsMalformedRegex pins that a child rule
// with a malformed target_regex causes LoadWithInheritance to fail and
// the error names the offending rule_id. Pre-fix, the post-merge
// ApplyDefaults() return value was discarded; per-file load catches
// per-file regex errors today, but propagating the merge-time error
// closes the contract that the merged Policy is fully revalidated.
func TestLoadWithInheritance_RejectsMalformedRegex(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "chitin.yaml"), `
id: parent
mode: enforce
rules:
  - id: parent-rule
    action: shell.exec
    effect: deny
    target: "rm"
    reason: "parent"
`)
	child := filepath.Join(root, "sub")
	writeFile(t, filepath.Join(child, "chitin.yaml"), `
id: child
mode: enforce
rules:
  - id: bad-regex-rule
    action: shell.exec
    effect: deny
    target_regex: "("
    reason: "bad"
`)
	_, _, err := LoadWithInheritance(child)
	if err == nil {
		t.Fatal("LoadWithInheritance must reject malformed regex")
	}
	if !strings.Contains(err.Error(), "bad-regex-rule") {
		t.Errorf("error should name the offending rule_id, got: %v", err)
	}
}

func TestLoadWithInheritance_MonotonicStrictness(t *testing.T) {
	// Parent is mode:enforce. Child tries mode:monitor.
	// Child CANNOT weaken — merge should reject.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "chitin.yaml"), `
id: parent
mode: enforce
rules: []
`)
	child := filepath.Join(root, "sub")
	writeFile(t, filepath.Join(child, "chitin.yaml"), `
id: child
mode: monitor
rules: []
`)
	_, _, err := LoadWithInheritance(child)
	if err == nil {
		t.Fatal("child:monitor under parent:enforce should fail strictness check")
	}
}

// TestMergePolicies_PerActionMergedAdditively pins the fix for the
// silent-drop bug observed in the workspace-root inheritance case
// (operator-side `git push` from /home/red/workspace/chitin/ hit
// "exceeds ceiling of 500" instead of the per_action git.push:5000
// override). Pre-fix, mergePolicies copied the global Bounds fields
// but never copied child.Bounds.PerAction into out.Bounds.PerAction —
// so when the parent (workspace-root chitin.yaml) had no bounds at
// all, the merge produced out.Bounds with the child's globals but
// PerAction=nil, and effectiveBounds("git.push") fell back to the
// 500-line global instead of the 5000-line override.
//
// Boundaries:
//  1. parent has no PerAction, child has one — child's PerAction wins.
//  2. parent has PerAction, child has none — parent survives.
//  3. both have PerAction with overlapping keys — child overrides on
//     collision, parent-only keys survive.
func TestMergePolicies_PerActionMergedAdditively(t *testing.T) {
	t.Run("parent_empty_child_has_perAction", func(t *testing.T) {
		parent := Policy{}
		child := Policy{Bounds: Bounds{
			MaxLinesChanged: 500,
			PerAction: map[string]ActionBounds{
				"git.push": {MaxLinesChanged: 5000},
			},
		}}
		out := mergePolicies(parent, child)
		eff := out.Bounds.effectiveBounds("git.push")
		if eff.MaxLinesChanged != 5000 {
			t.Errorf("expected per_action override to survive merge, got effective MaxLinesChanged=%d", eff.MaxLinesChanged)
		}
	})
	t.Run("parent_has_perAction_child_empty", func(t *testing.T) {
		parent := Policy{Bounds: Bounds{
			MaxLinesChanged: 500,
			PerAction: map[string]ActionBounds{
				"git.push": {MaxLinesChanged: 5000},
			},
		}}
		child := Policy{}
		out := mergePolicies(parent, child)
		eff := out.Bounds.effectiveBounds("git.push")
		if eff.MaxLinesChanged != 5000 {
			t.Errorf("parent per_action should survive when child has none, got %d", eff.MaxLinesChanged)
		}
	})
	t.Run("both_have_perAction_child_wins_on_collision", func(t *testing.T) {
		parent := Policy{Bounds: Bounds{
			MaxLinesChanged: 500,
			PerAction: map[string]ActionBounds{
				"git.push":         {MaxLinesChanged: 1000},
				"github.pr.create": {MaxLinesChanged: 2000},
			},
		}}
		child := Policy{Bounds: Bounds{
			PerAction: map[string]ActionBounds{
				"git.push": {MaxLinesChanged: 5000},
			},
		}}
		out := mergePolicies(parent, child)
		// Child wins on collision.
		if effPush := out.Bounds.effectiveBounds("git.push"); effPush.MaxLinesChanged != 5000 {
			t.Errorf("child should override parent on git.push, got %d", effPush.MaxLinesChanged)
		}
		// Parent-only key survives.
		if effPR := out.Bounds.effectiveBounds("github.pr.create"); effPR.MaxLinesChanged != 2000 {
			t.Errorf("parent-only github.pr.create should survive merge, got %d", effPR.MaxLinesChanged)
		}
	})
}

func TestMergePolicies_WorktreeRequirementMergedAdditively(t *testing.T) {
	t.Run("parent_requirement_survives_child_without_worktree", func(t *testing.T) {
		parent := Policy{Worktree: WorktreeConfig{
			Mode:       "guide",
			RequireFor: ActionMatcher{string(ActFileWrite)},
		}}
		child := Policy{}
		out := mergePolicies(parent, child)
		if !out.Worktree.RequireFor.Matches(ActFileWrite) {
			t.Fatalf("parent worktree.require_for should survive child without worktree: %+v", out.Worktree)
		}
		if out.Worktree.Mode != "guide" {
			t.Fatalf("mode=%q want guide", out.Worktree.Mode)
		}
	})
	t.Run("child_requirement_adds_action_and_overrides_mode", func(t *testing.T) {
		parent := Policy{Worktree: WorktreeConfig{
			Mode:           "guide",
			RequireFor:     ActionMatcher{string(ActFileWrite)},
			ProtectedRoots: []string{"/repo"},
		}}
		child := Policy{Worktree: WorktreeConfig{
			Mode:           "enforce",
			RequireFor:     ActionMatcher{string(ActGitCommit)},
			ProtectedRoots: []string{"/repo-child"},
		}}
		out := mergePolicies(parent, child)
		if !out.Worktree.RequireFor.Matches(ActFileWrite) || !out.Worktree.RequireFor.Matches(ActGitCommit) {
			t.Fatalf("worktree.require_for should merge additively, got %+v", out.Worktree.RequireFor)
		}
		if out.Worktree.Mode != "enforce" {
			t.Fatalf("child mode should override parent, got %q", out.Worktree.Mode)
		}
		if len(out.Worktree.ProtectedRoots) != 2 {
			t.Fatalf("protected roots should merge additively, got %+v", out.Worktree.ProtectedRoots)
		}
	})
}

// TestLoadWithInheritance_PerActionSurvivesWorkspaceMerge is the e2e
// version of the merge fix: a parent chitin.yaml with no bounds, and
// a child chitin.yaml with both a global ceiling AND a per_action
// override. After the fix, effectiveBounds(git.push) honors the
// child's per_action override.
func TestLoadWithInheritance_PerActionSurvivesWorkspaceMerge(t *testing.T) {
	root := t.TempDir()
	// Workspace-root: no bounds (mirrors /home/red/workspace/chitin.yaml).
	writeFile(t, filepath.Join(root, "chitin.yaml"), `
id: workspace-root
mode: enforce
rules: []
`)
	// Inner repo: global 500 + per_action git.push:5000.
	inner := filepath.Join(root, "inner")
	writeFile(t, filepath.Join(inner, "chitin.yaml"), `
id: inner-repo
mode: enforce
rules: []
bounds:
  max_lines_changed: 500
  per_action:
    git.push:
      max_lines_changed: 5000
`)
	p, _, err := LoadWithInheritance(inner)
	if err != nil {
		t.Fatalf("LoadWithInheritance: %v", err)
	}
	eff := p.Bounds.effectiveBounds("git.push")
	if eff.MaxLinesChanged != 5000 {
		t.Errorf("per_action git.push override should survive workspace merge, got effective MaxLinesChanged=%d (PerAction=%+v)",
			eff.MaxLinesChanged, p.Bounds.PerAction)
	}
	// A 600-line push: over the 500 global default, under the 5000
	// per_action override. Pre-fix this would fail with "exceeds
	// ceiling of 500" (the symptom from operator's git push); post-
	// fix it must pass because the override is honored.
	a := Action{Type: ActGitPush, Target: "fix"}
	d := evaluateBoundsFromStats(a, p, eff, 5, 300, 300)
	if !d.Allowed {
		t.Errorf("600-line push should pass under 5000-line per_action override (was hitting 500-line global), got %+v", d)
	}
	// A 6000-line push: over the 5000 override. Confirms the override
	// is the active ceiling — not silently raised to infinity.
	d2 := evaluateBoundsFromStats(a, p, eff, 50, 3000, 3000)
	if d2.Allowed {
		t.Errorf("6000-line push should fail under 5000-line override, got %+v", d2)
	}
	if d2.Reason == "" || !strings.Contains(d2.Reason, "5000") {
		t.Errorf("denial reason should reference the 5000 ceiling (proves override took effect, not the 500 default), got reason=%q", d2.Reason)
	}
}
