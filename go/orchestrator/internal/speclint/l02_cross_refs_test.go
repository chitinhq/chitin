package speclint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSpec creates <root>/<dirName>/spec.md with the given body and returns
// the absolute path to the spec dir. Used to stand up a minimal
// .specify/specs/ layout for L02 to glob over.
func writeSpec(t *testing.T, root, dirName, body string) string {
	t.Helper()
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write spec.md: %v", err)
	}
	return dir
}

const l02Frontmatter = `---
spec_id: 115
title: Spec PR review gate
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
%s
related:
%s
---

# Spec 115
`

func l02Spec(depends, related []string) string {
	dep := "  []"
	if len(depends) > 0 {
		var b strings.Builder
		for _, id := range depends {
			b.WriteString("  - " + id + "\n")
		}
		dep = strings.TrimRight(b.String(), "\n")
	}
	rel := "  []"
	if len(related) > 0 {
		var b strings.Builder
		for _, id := range related {
			b.WriteString("  - " + id + "\n")
		}
		rel = strings.TrimRight(b.String(), "\n")
	}
	// l02Frontmatter has two "%s" slots; depends_on goes first, related second.
	out := strings.Replace(l02Frontmatter, "%s", dep, 1)
	out = strings.Replace(out, "%s", rel, 1)
	return out
}

func TestCheckCrossRefs_CleanResolvesAllSiblings(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root, "097-watchdog", "")
	writeSpec(t, root, "113-pr-iteration", "")
	writeSpec(t, root, "094-design-judgement", "")
	writeSpec(t, root, "098-webhook-push", "")
	specDir := writeSpec(t, root, "115-spec-review-gate",
		l02Spec([]string{"097", "113"}, []string{"094", "098"}))

	got, err := CheckCrossRefs(specDir, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no violations when all refs resolve, got %#v", got)
	}
}

func TestCheckCrossRefs_EmptyListsAreClean(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root, "200-standalone",
		l02Spec(nil, nil))

	got, err := CheckCrossRefs(specDir, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no violations for empty depends_on / related, got %#v", got)
	}
}

func TestCheckCrossRefs_UnresolvedDependsOn(t *testing.T) {
	root := t.TempDir()
	// 113 exists, 999 does not — only 999 should flag.
	writeSpec(t, root, "113-pr-iteration", "")
	specDir := writeSpec(t, root, "115-spec-review-gate",
		l02Spec([]string{"113", "999"}, nil))

	got, err := CheckCrossRefs(specDir, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %#v", len(got), got)
	}
	v := got[0]
	if v.Rule != "L02" {
		t.Errorf("Rule: want L02, got %q", v.Rule)
	}
	if v.Severity != SeverityError {
		t.Errorf("Severity: want error, got %q", v.Severity)
	}
	if v.File != "spec.md" {
		t.Errorf("File: want spec.md, got %q", v.File)
	}
	if !strings.Contains(v.Message, `"999"`) {
		t.Errorf("Message should name id 999: %q", v.Message)
	}
	if !strings.Contains(v.Message, "depends_on") {
		t.Errorf("Message should name the key depends_on: %q", v.Message)
	}
	if !strings.Contains(v.Message, "does not resolve") {
		t.Errorf("Message should say 'does not resolve': %q", v.Message)
	}
}

func TestCheckCrossRefs_UnresolvedRelated(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-spec-review-gate",
		l02Spec(nil, []string{"888"}))

	got, err := CheckCrossRefs(specDir, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "related") {
		t.Errorf("Message should name the key 'related': %q", got[0].Message)
	}
	if !strings.Contains(got[0].Message, `"888"`) {
		t.Errorf("Message should name id 888: %q", got[0].Message)
	}
}

func TestCheckCrossRefs_AmbiguousID(t *testing.T) {
	// Two sibling dirs share the same numeric prefix — the id "042"
	// matches both, which is ambiguous.
	root := t.TempDir()
	writeSpec(t, root, "042-foo", "")
	writeSpec(t, root, "042-bar", "")
	specDir := writeSpec(t, root, "115-spec-review-gate",
		l02Spec([]string{"042"}, nil))

	got, err := CheckCrossRefs(specDir, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %#v", len(got), got)
	}
	v := got[0]
	if !strings.Contains(v.Message, "ambiguous") {
		t.Errorf("Message should say 'ambiguous': %q", v.Message)
	}
	if !strings.Contains(v.Message, "042-foo") || !strings.Contains(v.Message, "042-bar") {
		t.Errorf("Message should name both matching dirs: %q", v.Message)
	}
}

func TestCheckCrossRefs_NonDirSiblingIgnored(t *testing.T) {
	// A regular file named "777-something" lives next to the specs but
	// is not a directory — L02 must skip it. A genuine sibling dir
	// then satisfies the reference.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "777-stray-file"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	writeSpec(t, root, "777-real-spec", "")
	specDir := writeSpec(t, root, "115-spec-review-gate",
		l02Spec([]string{"777"}, nil))

	got, err := CheckCrossRefs(specDir, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no violations when only the dir matches, got %#v", got)
	}
}

func TestCheckCrossRefs_DefaultSpecsRootFromSpecDir(t *testing.T) {
	// Passing specsRoot="" should derive it from filepath.Dir(specDir).
	root := t.TempDir()
	writeSpec(t, root, "113-pr-iteration", "")
	specDir := writeSpec(t, root, "115-spec-review-gate",
		l02Spec([]string{"113"}, nil))

	got, err := CheckCrossRefs(specDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no violations with default specsRoot, got %#v", got)
	}
}

func TestCheckCrossRefs_MissingSpecMDIsError(t *testing.T) {
	// L02 cannot lint what isn't there. The caller (the spec-lint
	// subcommand) decides whether that's a hard error or surfaced by L01.
	root := t.TempDir()
	specDir := filepath.Join(root, "115-empty")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := CheckCrossRefs(specDir, root)
	if err == nil {
		t.Fatalf("expected error when spec.md is missing")
	}
}

func TestCheckCrossRefs_MalformedFrontmatterDefersToL01(t *testing.T) {
	// Bad frontmatter is L01's territory; L02 emits nothing rather
	// than double-reporting on the same root cause.
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-spec-review-gate", "no fence here at all\n")
	got, err := CheckCrossRefs(specDir, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected L02 to defer on malformed frontmatter, got %#v", got)
	}
}

func TestCheckCrossRefs_LinePointsAtReferencedID(t *testing.T) {
	// The violation must point at the line of the offending list item,
	// not the depends_on key itself — PR review comments need to land
	// on the right line.
	root := t.TempDir()
	// 113 exists, 999 does not — 999 lives on the second list item.
	writeSpec(t, root, "113-pr-iteration", "")
	specDir := writeSpec(t, root, "115-spec-review-gate",
		l02Spec([]string{"113", "999"}, nil))

	got, err := CheckCrossRefs(specDir, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %#v", got)
	}
	// l02Frontmatter layout:
	//   1: ---
	//   2: spec_id: 115
	//   3: title: ...
	//   4: status: ...
	//   5: owner: ...
	//   6: created: ...
	//   7: depends_on:
	//   8:   - 113
	//   9:   - 999   <-- expected
	if got[0].Line != 9 {
		t.Errorf("expected Line=9 (the '999' list item), got %d", got[0].Line)
	}
}
