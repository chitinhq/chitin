package speclint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSpec stages a minimal spec dir on disk: <root>/<id>-<slug>/spec.md
// with the given body. The spec dirs SIBLING to specDir under specsRoot
// are what L02 globs against, so callers stage those too.
func writeSpec(t *testing.T, specsRoot, dirName, body string) string {
	t.Helper()
	d := filepath.Join(specsRoot, dirName)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", d, err)
	}
	p := filepath.Join(d, "spec.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return d
}

// TestL02_AllRefsResolve asserts the happy path: every depends_on / related
// id has exactly one matching sibling directory, so no violations.
func TestL02_AllRefsResolve(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "097-already-here", "")
	writeSpec(t, root, "113-also-here", "")
	writeSpec(t, root, "094-related-here", "")
	specDir := writeSpec(t, root, "115-spec-review-gate", `---
spec_id: 115
title: Spec PR review gate
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 097
  - 113
related:
  - 094
---

# body
`)
	v, err := L02CrossRefs(specDir)
	if err != nil {
		t.Fatalf("L02: %v", err)
	}
	if len(v) != 0 {
		t.Fatalf("expected no violations, got %+v", v)
	}
}

// TestL02_DanglingRef asserts a depends_on id whose directory does not
// exist produces one error-severity violation pointing at the source line.
func TestL02_DanglingRef(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "097-already-here", "")
	// 113 deliberately missing.
	specDir := writeSpec(t, root, "115-spec-review-gate", `---
spec_id: 115
depends_on:
  - 097
  - 113
---
`)
	v, err := L02CrossRefs(specDir)
	if err != nil {
		t.Fatalf("L02: %v", err)
	}
	if len(v) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(v), v)
	}
	if v[0].Rule != "L02" || v[0].Severity != SeverityError {
		t.Errorf("unexpected rule/severity: %+v", v[0])
	}
	if v[0].File != "spec.md" || v[0].Line != 5 {
		t.Errorf("expected spec.md:5 (the `  - 113` line), got %s:%d", v[0].File, v[0].Line)
	}
	if !strings.Contains(v[0].Message, "depends_on") || !strings.Contains(v[0].Message, "113") {
		t.Errorf("message should name the key and id: %s", v[0].Message)
	}
}

// TestL02_AmbiguousPrefix asserts that two sibling directories both starting
// with the same id prefix are flagged. This protects the linter from a
// silent-fail mode where a glob accidentally resolves to the wrong dir.
func TestL02_AmbiguousPrefix(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "097-first", "")
	writeSpec(t, root, "097-second", "")
	specDir := writeSpec(t, root, "115-spec-review-gate", `---
depends_on:
  - 097
---
`)
	v, err := L02CrossRefs(specDir)
	if err != nil {
		t.Fatalf("L02: %v", err)
	}
	if len(v) != 1 {
		t.Fatalf("expected 1 violation, got %+v", v)
	}
	if !strings.Contains(v[0].Message, "097-first") || !strings.Contains(v[0].Message, "097-second") {
		t.Errorf("message should list both matches: %s", v[0].Message)
	}
}

// TestL02_RelatedAndDependsOnOrdering asserts the emission tie-breaker:
// depends_on violations come before related violations regardless of how
// they were interleaved in the source. This keeps the same spec's lint
// output stable across re-runs.
func TestL02_RelatedAndDependsOnOrdering(t *testing.T) {
	root := t.TempDir()
	// Stage none of the referenced ids — both keys produce violations.
	specDir := writeSpec(t, root, "115-spec-review-gate", `---
related:
  - 094
depends_on:
  - 097
---
`)
	v, err := L02CrossRefs(specDir)
	if err != nil {
		t.Fatalf("L02: %v", err)
	}
	if len(v) != 2 {
		t.Fatalf("expected 2 violations, got %+v", v)
	}
	if !strings.Contains(v[0].Message, "depends_on") {
		t.Errorf("depends_on must come first, got %s", v[0].Message)
	}
	if !strings.Contains(v[1].Message, "related") {
		t.Errorf("related must come second, got %s", v[1].Message)
	}
}

// TestL02_NoFrontmatter asserts L02 stays silent when there is no
// frontmatter at all — that's L01's finding, not L02's.
func TestL02_NoFrontmatter(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-spec-review-gate", "# Spec 115\n\nno frontmatter at all\n")
	v, err := L02CrossRefs(specDir)
	if err != nil {
		t.Fatalf("L02: %v", err)
	}
	if len(v) != 0 {
		t.Fatalf("expected silence when frontmatter is absent, got %+v", v)
	}
}

// TestL02_EmptyLists asserts depends_on: / related: with no list items
// produces no violations — the spec author legitimately has no upstream
// refs.
func TestL02_EmptyLists(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-spec-review-gate", `---
depends_on:
related:
---
`)
	v, err := L02CrossRefs(specDir)
	if err != nil {
		t.Fatalf("L02: %v", err)
	}
	if len(v) != 0 {
		t.Fatalf("expected no violations for empty lists, got %+v", v)
	}
}

// TestL02_SpecMDMissing asserts the rule returns an I/O error when spec.md
// is absent — caller (the lint dispatcher) decides whether to surface as a
// per-file error or a top-level failure.
func TestL02_SpecMDMissing(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "115-spec-review-gate")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := L02CrossRefs(specDir); err == nil {
		t.Fatalf("expected error when spec.md is missing")
	}
}

// TestL02_QuotedIDs asserts ids written with surrounding quotes (a YAML
// shape some authors use) still parse — depends_on:\n  - "097".
func TestL02_QuotedIDs(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "097-here", "")
	specDir := writeSpec(t, root, "115-spec-review-gate", `---
depends_on:
  - "097"
---
`)
	v, err := L02CrossRefs(specDir)
	if err != nil {
		t.Fatalf("L02: %v", err)
	}
	if len(v) != 0 {
		t.Fatalf("expected quoted ids to resolve, got %+v", v)
	}
}
