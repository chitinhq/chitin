package gov

import (
	"io/ioutil"
	"os"
	"path/filepath"
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
