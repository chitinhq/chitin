package speckit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/spec"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/spec/adapter"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/spec/adapter/speckit"
)

func writeFixture(t *testing.T, dirName, content string) string {
	t.Helper()
	dir := t.TempDir()
	specDir := filepath.Join(dir, dirName)
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specFile := filepath.Join(specDir, "spec.md")
	if err := os.WriteFile(specFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return specDir
}

func TestDetect_ValidSpecKitDir(t *testing.T) {
	a := &speckit.Adapter{}
	dir := writeFixture(t, "020-sdd-tdd-enforcement", "# 020 — Title\n")
	if !a.Detect(dir) {
		t.Errorf("Detect should return true for valid spec-kit dir %q", dir)
	}
}

func TestDetect_InvalidDir(t *testing.T) {
	a := &speckit.Adapter{}
	dir := t.TempDir()
	if a.Detect(dir) {
		t.Errorf("Detect should return false for dir without spec.md")
	}
}

func TestDetect_NonNumericPrefix(t *testing.T) {
	a := &speckit.Adapter{}
	dir := writeFixture(t, "random-name", "# Title\n")
	if a.Detect(dir) {
		t.Errorf("Detect should return false for non-spec-kit directory name")
	}
}

func TestDetect_ICPrefix(t *testing.T) {
	a := &speckit.Adapter{}
	dir := writeFixture(t, "020-ic-001-test", "# ic-001 — Title\n")
	if !a.Detect(dir) {
		t.Errorf("Detect should accept ic-NNN style prefixes")
	}
}

func TestParse_SpecTitle(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantTitle string
	}{
		{
			"spec number format",
			"# Spec 050: Mini MCP — dispatch\n\nSome text",
			"Mini MCP — dispatch",
		},
		{
			"dash format",
			"# 020 — Chitin enforces SDD + TDD\n\nSome text",
			"Chitin enforces SDD + TDD",
		},
		{
			"feature spec format",
			"# Feature Specification: Drift Guard — elimination\n\nText",
			"Drift Guard — elimination",
		},
	}

	a := &speckit.Adapter{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeFixture(t, "050-mini-mcp-spec-dispatch", tt.content)
			result, err := a.Parse(dir)
			if err != nil {
				t.Fatalf("Parse returned unexpected error: %v", err)
			}
			if result.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", result.Title, tt.wantTitle)
			}
		})
	}
}

func TestParse_Status(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    spec.SpecStatus
	}{
		{
			"shipped status",
			"# Spec 050: Title\n\n**Status**: shipped\n",
			spec.SpecStatusRatified,
		},
		{
			"draft status",
			"# Spec 050: Title\n\n**Status**: draft\n",
			spec.SpecStatusDraft,
		},
		{
			"ratified status",
			"# Spec 050: Title\n\n**Status**: ratified\n",
			spec.SpecStatusRatified,
		},
		{
			"no status defaults to draft",
			"# 020 — Title\n\nSome text without status marker.",
			spec.SpecStatusDraft,
		},
	}

	a := &speckit.Adapter{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeFixture(t, "020-sdd-tdd", tt.content)
			result, err := a.Parse(dir)
			if err != nil {
				t.Fatalf("Parse returned unexpected error: %v", err)
			}
			if result.Status != tt.want {
				t.Errorf("Status = %q, want %q", result.Status, tt.want)
			}
		})
	}
}

func TestParse_Requirements(t *testing.T) {
	content := "# 061 — Unified spec model\n\n" +
		"### R1 — the normalized model is the only upward contract\n\n" +
		"L2–L7 consume `UnifiedSpec` exclusively.\n\n" +
		"### R2 — the adapter interface\n\n" +
		"An adapter is a pure function.\n\n" +
		"**R3 — spec-kit adapter**\n"

	dir := writeFixture(t, "061-unified-spec-model", content)
	a := &speckit.Adapter{}
	result, err := a.Parse(dir)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(result.Requirements) < 3 {
		t.Fatalf("Expected at least 3 requirements, got %d", len(result.Requirements))
	}
	if result.Requirements[0].ID != "R1" {
		t.Errorf("First requirement ID = %q, want R1", result.Requirements[0].ID)
	}
	if result.Requirements[1].ID != "R2" {
		t.Errorf("Second requirement ID = %q, want R2", result.Requirements[1].ID)
	}
	if result.Requirements[2].ID != "R3" {
		t.Errorf("Third requirement ID = %q, want R3", result.Requirements[2].ID)
	}
}

func TestParse_Acceptance(t *testing.T) {
	content := "# 061 — Unified spec model\n\n" +
		"**AC1** — UnifiedSpec schema is defined and documented.\n\n" +
		"### AC2 — The spec-kit adapter parses all specs.\n\n" +
		"**AC3** — detect routes to exactly one adapter.\n"

	dir := writeFixture(t, "061-unified-spec-model", content)
	a := &speckit.Adapter{}
	result, err := a.Parse(dir)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(result.Acceptance) < 2 {
		t.Fatalf("Expected at least 2 acceptance criteria, got %d", len(result.Acceptance))
	}
	if result.Acceptance[0].ID != "AC1" {
		t.Errorf("First AC ID = %q, want AC1", result.Acceptance[0].ID)
	}
}

func TestParse_BoundaryCases(t *testing.T) {
	content := "# 061 — Title\n\n" +
		"## Boundary cases\n\n" +
		"1. **Spec with no requirements** — may have empty requirements.\n" +
		"2. **Ambiguous spec_id** — raises typed error.\n"

	dir := writeFixture(t, "061-unified-spec-model", content)
	a := &speckit.Adapter{}
	result, err := a.Parse(dir)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(result.Boundaries) < 2 {
		t.Fatalf("Expected at least 2 boundaries, got %d", len(result.Boundaries))
	}
}

func TestParse_OpenQuestions(t *testing.T) {
	content := "# 061 — Title\n\n" +
		"## Open questions\n\n" +
		"- **Q1 — model owner** (charter Q1). Does UnifiedSpec live as a Go type?\n\n" +
		"- **Q2 — adapter location**. One package, one registry?\n"

	dir := writeFixture(t, "061-unified-spec-model", content)
	a := &speckit.Adapter{}
	result, err := a.Parse(dir)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(result.OpenQuestions) < 1 {
		t.Fatalf("Expected at least 1 open question, got %d", len(result.OpenQuestions))
	}
	if result.OpenQuestions[0].ID != "Q1" {
		t.Errorf("First question ID = %q, want Q1", result.OpenQuestions[0].ID)
	}
}

func TestParse_Slices(t *testing.T) {
	content := "# 061 — Title\n\n" +
		"### R1 — model is the contract\n\n" +
		"### R2 — adapter interface\n\n" +
		"### R3 — spec-kit adapter\n\n" +
		"## Slice plan\n\n" +
		"- **Slice 1** — UnifiedSpec schema + adapter interface + spec-kit adapter. R1, R2, R3, R6.\n\n" +
		"- **Slice 2** — Superpowers adapter (R5). R5.\n"

	dir := writeFixture(t, "061-unified-spec-model", content)
	a := &speckit.Adapter{}
	result, err := a.Parse(dir)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(result.Slices) < 2 {
		t.Fatalf("Expected at least 2 slices, got %d", len(result.Slices))
	}
	if result.Slices[0].ID != "Slice 1" {
		t.Errorf("First slice ID = %q, want 'Slice 1'", result.Slices[0].ID)
	}
	if len(result.Slices[0].RequirementIDs) < 3 {
		t.Errorf("Slice 1 should link to at least 3 requirements, got %d: %v",
			len(result.Slices[0].RequirementIDs), result.Slices[0].RequirementIDs)
	}
}

func TestParse_MalformedFile(t *testing.T) {
	a := &speckit.Adapter{}
	dir := t.TempDir()
	_, err := a.Parse(dir)
	if err == nil {
		t.Fatal("Parse should return error for missing spec.md")
	}
	if _, ok := err.(*adapter.ParseError); !ok {
		t.Errorf("Error should be *adapter.ParseError, got %T", err)
	}
}

func TestParse_DirWithoutSpecID(t *testing.T) {
	content := "# Some random doc\n"
	dir := writeFixture(t, "random-name", content)
	a := &speckit.Adapter{}
	_, err := a.Parse(dir)
	if err == nil {
		t.Fatal("Parse should return error for directory without spec-id pattern")
	}
}

func TestParse_SpecIDExtraction(t *testing.T) {
	tests := []struct {
		dirName string
		wantID  string
	}{
		{"020-sdd-tdd-enforcement", "020"},
		{"075-icarus-local-llm-driver", "075"},
		{"062-spec-build-attribution", "062"},
		{"728-dispatch-default-branch-fix", "728"},
	}

	a := &speckit.Adapter{}
	for _, tt := range tests {
		t.Run(tt.dirName, func(t *testing.T) {
			content := "# " + tt.dirName + " — Test\n"
			dir := writeFixture(t, tt.dirName, content)
			result, err := a.Parse(dir)
			if err != nil {
				t.Fatalf("Parse returned unexpected error: %v", err)
			}
			if result.SpecID != tt.wantID {
				t.Errorf("SpecID = %q, want %q", result.SpecID, tt.wantID)
			}
		})
	}
}

func TestAdapter_ImplementsInterface(t *testing.T) {
	var _ adapter.SpecAdapter = &speckit.Adapter{}
}

func TestRegistry(t *testing.T) {
	a := &speckit.Adapter{}
	if a.Framework() != spec.SourceFrameworkSpecKit {
		t.Errorf("Framework() = %q, want %q", a.Framework(), spec.SourceFrameworkSpecKit)
	}
}

func TestDetectAdapters(t *testing.T) {
	dir := writeFixture(t, "020-sdd-tdd", "# 020 — Title\n")
	matches := adapter.DetectAdapters(dir)
	found := false
	for _, m := range matches {
		if m.Framework() == spec.SourceFrameworkSpecKit {
			found = true
		}
	}
	if !found {
		t.Errorf("DetectAdapters should find speckit adapter for %q, got %d matches", dir, len(matches))
	}
}

func TestParse_FullSpec(t *testing.T) {
	content := "# 061 — Unified spec model + framework adapters (L1)\n\n" +
		"**Status**: draft 2026-05-19\n\n" +
		"### R1 — the normalized model is the only upward contract\n\n" +
		"L2–L7 consume `UnifiedSpec` exclusively.\n\n" +
		"### R2 — the adapter interface\n\n" +
		"An adapter is a pure function parse(source) -> UnifiedSpec\n" +
		"plus a detect(path) -> bool.\n\n" +
		"### R3 — spec-kit adapter (reference implementation)\n\n" +
		"The spec-kit / house-format adapter parses .specify/specs/.\n\n" +
		"**AC1** — UnifiedSpec schema is defined and documented.\n\n" +
		"**AC2** — spec-kit adapter parses all 53 existing specs.\n\n" +
		"**AC3** — detect correctly routes a path to exactly one adapter.\n\n" +
		"## Boundary cases\n\n" +
		"1. **Spec with no requirements** — may be valid if it has slices.\n" +
		"2. **Ambiguous spec_id** — raises typed error.\n" +
		"3. **Malformed source** — raises typed error naming file and section.\n\n" +
		"## Slice plan\n\n" +
		"- **Slice 1** — UnifiedSpec schema + adapter interface + spec-kit adapter. R1, R2, R3.\n\n" +
		"- **Slice 2** — Superpowers adapter (R5).\n\n" +
		"## Open questions\n\n" +
		"- **Q1 — model owner** — JSON Schema canonical?\n\n" +
		"- **Q2 — adapter location** — One package, one registry?\n"

	dir := writeFixture(t, "061-unified-spec-model", content)
	a := &speckit.Adapter{}
	result, err := a.Parse(dir)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if result.SpecID != "061" {
		t.Errorf("SpecID = %q, want 061", result.SpecID)
	}
	if result.SourceFramework != spec.SourceFrameworkSpecKit {
		t.Errorf("SourceFramework = %q, want spec-kit", result.SourceFramework)
	}
	if result.Title == "" {
		t.Error("Title should not be empty")
	}
	if len(result.Requirements) < 3 {
		t.Errorf("Expected >= 3 requirements, got %d", len(result.Requirements))
	}
	if len(result.Acceptance) < 2 {
		t.Errorf("Expected >= 2 acceptance criteria, got %d", len(result.Acceptance))
	}
	if len(result.Boundaries) < 2 {
		t.Errorf("Expected >= 2 boundaries, got %d", len(result.Boundaries))
	}
	if len(result.Slices) < 1 {
		t.Errorf("Expected >= 1 slice, got %d", len(result.Slices))
	}
	if len(result.OpenQuestions) < 1 {
		t.Errorf("Expected >= 1 open question, got %d", len(result.OpenQuestions))
	}
}