package superpowers_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/spec"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/spec/adapter"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/spec/adapter/superpowers"
)

const superpowersSpec = `# Chitin Dashboard — visual replay + self-improving feedback loop

Status: spec — open

## Goal

Build the dashboard without changing kernel authority.

## Hard rules

- Kernel events remain canonical.
- OTEL stays a projection.

## Slice 1 — Capture extension

Worker captures prompt and tool I/O.

**Acceptance:**
- Prompt sidecar is written.
- Chain id links back to the run.

## Slice 2 — Replay API

Expose replay for sessions.

## Non-goals

- No live execution from the dashboard.

## Open questions

1. Should token costs be sampled or exact?
`

const planDoc = `# Adopt spec-kit; retire docs/superpowers/specs/ — Implementation Plan

**Goal:** Replace the bespoke specs workflow.

### Task 1.1: Branch + install spec-kit CLI

- [ ] Create the feature branch
- [ ] Verify supported-agent list
`

func writeDoc(t *testing.T, rel, body string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAdapter_ImplementsInterface(t *testing.T) {
	var _ adapter.SpecAdapter = &superpowers.Adapter{}
}

func TestDetect_SuperpowersMarkdown(t *testing.T) {
	a := &superpowers.Adapter{}
	path := writeDoc(t, "docs/superpowers/specs/2026-05-12-chitin-dashboard.md", superpowersSpec)
	if !a.Detect(path) {
		t.Fatalf("Detect should accept Superpowers markdown path")
	}
	other := writeDoc(t, "docs/other/2026-05-12-chitin-dashboard.md", superpowersSpec)
	if a.Detect(other) {
		t.Fatalf("Detect should reject non-Superpowers path")
	}
}

func TestDetect_DirectoryRequiresSuperpowersPath(t *testing.T) {
	a := &superpowers.Adapter{}
	root := t.TempDir()
	superpowersDir := filepath.Join(root, filepath.FromSlash("docs/superpowers/specs/2026-05-12-chitin-dashboard"))
	if err := os.MkdirAll(superpowersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(superpowersDir, "spec.md"), []byte(superpowersSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	speckitDir := filepath.Join(root, filepath.FromSlash(".specify/specs/061-unified-spec-model"))
	if err := os.MkdirAll(speckitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(speckitDir, "spec.md"), []byte(superpowersSpec), 0o644); err != nil {
		t.Fatal(err)
	}

	if !a.Detect(superpowersDir) {
		t.Fatalf("Detect should accept Superpowers directory")
	}
	if a.Detect(speckitDir) {
		t.Fatalf("Detect should reject non-Superpowers directory")
	}
}

func TestParse_SuperpowersSpecSections(t *testing.T) {
	a := &superpowers.Adapter{}
	path := writeDoc(t, "docs/superpowers/specs/2026-05-12-chitin-dashboard.md", superpowersSpec)

	result, err := a.Parse(path)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if result.SpecID != "2026-05-12-chitin-dashboard" {
		t.Fatalf("SpecID = %q", result.SpecID)
	}
	if result.Title != "Chitin Dashboard — visual replay + self-improving feedback loop" {
		t.Fatalf("Title = %q", result.Title)
	}
	if result.Status != spec.SpecStatusRatified {
		t.Fatalf("Status = %q", result.Status)
	}
	if result.SourceFramework != spec.SourceFrameworkSuperpowers {
		t.Fatalf("SourceFramework = %q", result.SourceFramework)
	}
	if len(result.Requirements) != 3 {
		t.Fatalf("requirements = %#v", result.Requirements)
	}
	if result.Requirements[1].Text != "Kernel events remain canonical" {
		t.Fatalf("second requirement = %#v", result.Requirements[1])
	}
	if len(result.Acceptance) != 2 || result.Acceptance[0].Text != "Prompt sidecar is written" {
		t.Fatalf("acceptance = %#v", result.Acceptance)
	}
	if len(result.Slices) != 2 || result.Slices[1].Scope != "Replay API" {
		t.Fatalf("slices = %#v", result.Slices)
	}
	if len(result.Boundaries) != 1 || result.Boundaries[0] != "No live execution from the dashboard" {
		t.Fatalf("boundaries = %#v", result.Boundaries)
	}
	if len(result.OpenQuestions) != 1 || result.OpenQuestions[0].ID != "Q1" {
		t.Fatalf("questions = %#v", result.OpenQuestions)
	}
}

func TestParse_DirectoryUsesDirectorySpecID(t *testing.T) {
	a := &superpowers.Adapter{}
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash("docs/superpowers/specs/2026-05-12-chitin-dashboard"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(superpowersSpec), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := a.Parse(dir)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if result.SpecID != "2026-05-12-chitin-dashboard" {
		t.Fatalf("SpecID = %q", result.SpecID)
	}
}

func TestParse_PlanCheckboxesAsSlices(t *testing.T) {
	a := &superpowers.Adapter{}
	path := writeDoc(t, "docs/superpowers/plans/2026-05-15-adopt-speckit-replace-spec-flow.md", planDoc)

	result, err := a.Parse(path)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if result.Status != spec.SpecStatusDraft {
		t.Fatalf("Status = %q", result.Status)
	}
	if len(result.Slices) != 2 || result.Slices[0].Scope != "Create the feature branch" {
		t.Fatalf("slices = %#v", result.Slices)
	}
}

func TestParse_PartialNoteDoesNotFabricateFields(t *testing.T) {
	a := &superpowers.Adapter{}
	path := writeDoc(t, "docs/superpowers/specs/2026-05-20-short-note.md", "# Short note\n\nJust a narrative note.\n")

	result, err := a.Parse(path)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if result.Title != "Short note" {
		t.Fatalf("Title = %q", result.Title)
	}
	if len(result.Requirements) != 0 || len(result.Acceptance) != 0 || len(result.Slices) != 0 {
		t.Fatalf("expected empty structural fields, got req=%#v ac=%#v slices=%#v", result.Requirements, result.Acceptance, result.Slices)
	}
}

func TestParse_PreservesExplicitQuestionIDs(t *testing.T) {
	a := &superpowers.Adapter{}
	path := writeDoc(t, "docs/superpowers/specs/2026-05-20-question-ids.md", "# Question IDs\n\n## Open questions\n\n- **Q2 — Should this retain numbering?**\n")

	result, err := a.Parse(path)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(result.OpenQuestions) != 1 || result.OpenQuestions[0].ID != "Q2" || result.OpenQuestions[0].Text != "Should this retain numbering?" {
		t.Fatalf("questions = %#v", result.OpenQuestions)
	}
}
