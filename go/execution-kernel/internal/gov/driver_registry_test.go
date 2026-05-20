package gov

import (
	"path/filepath"
	"testing"
)

func TestLoadPolicyFile_RejectsDuplicateDrivers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chitin.yaml")
	writeFile(t, path, `
id: dup-driver-policy
mode: enforce
drivers:
  - id: copilot
    identities: [copilot]
  - id: copilot
    identities: [github-copilot]
rules:
  - id: allow-reads
    action: file.read
    effect: allow
`)

	_, err := LoadPolicyFile(path)
	if err == nil {
		t.Fatal("LoadPolicyFile must reject duplicate driver ids")
	}
}

func TestLoadWithInheritance_MergesDriversByID(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "chitin.yaml"), `
id: parent
mode: enforce
drivers:
  - id: copilot
    identities: [copilot]
  - id: gemini
    identities: [gemini]
rules:
  - id: allow-reads
    action: file.read
    effect: allow
`)
	child := filepath.Join(root, "sub")
	writeFile(t, filepath.Join(child, "chitin.yaml"), `
id: child
mode: enforce
drivers:
  - id: copilot
    identities: [copilot, github-copilot]
  - id: codex
    identities: [codex]
rules:
  - id: allow-reads
    action: file.read
    effect: allow
`)

	p, _, err := LoadWithInheritance(child)
	if err != nil {
		t.Fatalf("LoadWithInheritance: %v", err)
	}
	if len(p.Drivers) != 3 {
		t.Fatalf("len(drivers)=%d want 3", len(p.Drivers))
	}
	if p.Drivers[0].ID != "copilot" || len(p.Drivers[0].Identities) != 2 {
		t.Fatalf("copilot should be overridden by child, got %+v", p.Drivers[0])
	}
	if p.Drivers[1].ID != "gemini" {
		t.Fatalf("parent-only driver should survive merge, got %+v", p.Drivers[1])
	}
	if p.Drivers[2].ID != "codex" {
		t.Fatalf("child-only driver should append, got %+v", p.Drivers[2])
	}
}
