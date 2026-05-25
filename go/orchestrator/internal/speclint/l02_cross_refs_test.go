package speclint

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// writeSpec creates <specsRoot>/<name>/spec.md with the given body. It also
// creates the parent dir for each sibling spec the test wants to exist.
func writeSpec(t *testing.T, specsRoot, name, body string) string {
	t.Helper()
	dir := filepath.Join(specsRoot, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return dir
}

func mustEmpty(t *testing.T, vs []Violation, label string) {
	t.Helper()
	if len(vs) != 0 {
		t.Fatalf("%s: expected no violations, got %d: %+v", label, len(vs), vs)
	}
}

func TestCheckCrossRefs_ResolvesCleanly(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-foo", `---
id: 115
depends_on:
  - 113
related:
  - 114
---

# body
`)
	writeSpec(t, root, "113-bar", "---\nid: 113\n---\n")
	writeSpec(t, root, "114-baz", "---\nid: 114\n---\n")

	vs, err := CheckCrossRefs(specDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustEmpty(t, vs, "clean resolve")
}

func TestCheckCrossRefs_DanglingReference(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-foo", `---
depends_on:
  - 999
---
`)

	vs, err := CheckCrossRefs(specDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(vs), vs)
	}
	v := vs[0]
	if v.Rule != "L02" || v.Severity != SeverityError || v.File != "spec.md" {
		t.Errorf("unexpected violation envelope: %+v", v)
	}
	if !strings.Contains(v.Message, `"999"`) || !strings.Contains(v.Message, "does not resolve") {
		t.Errorf("message missing id/text: %q", v.Message)
	}
	// `depends_on:` is line 2; `- 999` is line 3 of spec.md.
	if v.Line != 3 {
		t.Errorf("want line 3, got %d", v.Line)
	}
}

func TestCheckCrossRefs_AmbiguousReference(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-foo", `---
related:
  - 200
---
`)
	writeSpec(t, root, "200-one", "---\nid: 200\n---\n")
	writeSpec(t, root, "200-two", "---\nid: 200\n---\n")

	vs, err := CheckCrossRefs(specDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(vs), vs)
	}
	v := vs[0]
	if v.Severity != SeverityError {
		t.Errorf("want error severity, got %s", v.Severity)
	}
	if !strings.Contains(v.Message, "ambiguous") {
		t.Errorf("message should call out ambiguity: %q", v.Message)
	}
	if !strings.Contains(v.Message, "200-one") || !strings.Contains(v.Message, "200-two") {
		t.Errorf("message should list matching dir names deterministically: %q", v.Message)
	}
}

func TestCheckCrossRefs_NonDirectorySiblingIgnored(t *testing.T) {
	// A file like `097-notes.md` next to the specs tree must not count as
	// a sibling spec — only directories satisfy L02.
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-foo", `---
depends_on:
  - 97
---
`)
	if err := os.WriteFile(filepath.Join(root, "97-notes.md"), []byte("stray"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}

	vs, err := CheckCrossRefs(specDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vs) != 1 || !strings.Contains(vs[0].Message, "does not resolve") {
		t.Fatalf("non-dir match must not satisfy rule; got %+v", vs)
	}
}

func TestCheckCrossRefs_TrailingSlashSpecDir(t *testing.T) {
	// Regression for review comment [1]: filepath.Dir on a path with a
	// trailing slash returned the wrong specsRoot. After normalization both
	// "...115-foo" and "...115-foo/" must derive the same specsRoot.
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-foo", `---
depends_on:
  - 113
---
`)
	writeSpec(t, root, "113-bar", "---\nid: 113\n---\n")

	vsNoSlash, err := CheckCrossRefs(specDir, "")
	if err != nil {
		t.Fatalf("no-slash: %v", err)
	}
	vsSlash, err := CheckCrossRefs(specDir+string(filepath.Separator), "")
	if err != nil {
		t.Fatalf("trailing-slash: %v", err)
	}
	if !reflect.DeepEqual(vsNoSlash, vsSlash) {
		t.Fatalf("trailing slash changed result: %+v vs %+v", vsNoSlash, vsSlash)
	}
	mustEmpty(t, vsSlash, "trailing slash should still resolve")
}

func TestCheckCrossRefs_RejectsUnsafeIDs(t *testing.T) {
	// Regression for review comment [2]: ids containing path separators or
	// glob metacharacters must never reach filepath.Glob. L02 stays silent
	// and lets L01 report shape violations.
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-foo", `---
depends_on:
  - "../999"
  - "*"
  - "11[3-5]"
  - "abc"
---
`)

	vs, err := CheckCrossRefs(specDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustEmpty(t, vs, "unsafe ids must be skipped, not globbed")
}

func TestCheckCrossRefs_NoFrontmatter(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-foo", "# just a heading, no frontmatter\n")

	vs, err := CheckCrossRefs(specDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustEmpty(t, vs, "no frontmatter defers to L01")
}

func TestCheckCrossRefs_MalformedFrontmatterDefersToL01(t *testing.T) {
	// Unterminated `---` block: extractFrontmatter returns an error;
	// CheckCrossRefs intentionally swallows it so L01 emits the single
	// canonical "frontmatter incomplete" finding.
	root := t.TempDir()
	specDir := writeSpec(t, root, "115-foo", "---\ndepends_on:\n  - 113\n")

	vs, err := CheckCrossRefs(specDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustEmpty(t, vs, "malformed frontmatter is L01's job")
}

func TestCheckCrossRefs_ExplicitSpecsRootOverridesDerivation(t *testing.T) {
	// When the caller passes specsRoot explicitly, it must be honored even
	// if filepath.Dir(specDir) would point elsewhere.
	outer := t.TempDir()
	specsRoot := filepath.Join(outer, "specs")
	if err := os.MkdirAll(specsRoot, 0o755); err != nil {
		t.Fatalf("mkdir specsRoot: %v", err)
	}
	// Put the spec being linted somewhere other than under specsRoot.
	stagedDir := filepath.Join(outer, "staged", "115-foo")
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		t.Fatalf("mkdir stagedDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagedDir, "spec.md"), []byte("---\ndepends_on:\n  - 113\n---\n"), 0o644); err != nil {
		t.Fatalf("write staged spec: %v", err)
	}
	writeSpec(t, specsRoot, "113-bar", "---\nid: 113\n---\n")

	vs, err := CheckCrossRefs(stagedDir, specsRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustEmpty(t, vs, "explicit specsRoot should resolve cleanly")
}

func TestCheckCrossRefs_SpecFileMissing(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "115-foo") // no spec.md written
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	vs, err := CheckCrossRefs(specDir, "")
	if err == nil {
		t.Fatalf("expected read error for missing spec.md, got nil (vs=%+v)", vs)
	}
}

func TestMatchingSpecDirs_SortedAndDirsOnly(t *testing.T) {
	root := t.TempDir()
	// Create two matching dirs in reverse-lex order plus a file that
	// matches the pattern but must be filtered out.
	for _, name := range []string{"200-zzz", "200-aaa"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "200-file.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write decoy file: %v", err)
	}

	got, err := matchingSpecDirs(root, "200")
	if err != nil {
		t.Fatalf("matchingSpecDirs: %v", err)
	}
	gotNames := make([]string, len(got))
	for i, p := range got {
		gotNames[i] = filepath.Base(p)
	}
	want := []string{"200-aaa", "200-zzz"}
	if !sort.StringsAreSorted(gotNames) {
		t.Errorf("results not sorted: %v", gotNames)
	}
	if !reflect.DeepEqual(gotNames, want) {
		t.Errorf("got %v, want %v", gotNames, want)
	}
}

func TestMatchingSpecDirs_NoMatches(t *testing.T) {
	root := t.TempDir()
	got, err := matchingSpecDirs(root, "404")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want no matches, got %v", got)
	}
}

func TestIsNumericID(t *testing.T) {
	cases := map[string]bool{
		"":           false,
		"115":        true,
		"0":          true,
		"007":        true,
		"abc":        false,
		"11a":        false,
		"../115":     false,
		"*":          false,
		"11[3-5]":    false,
		"115-foo":    false,
		" 115":       false, // leading space — caller already TrimSpaces, but defense-in-depth
		"115\n":      false,
	}
	for in, want := range cases {
		if got := isNumericID(in); got != want {
			t.Errorf("isNumericID(%q) = %v, want %v", in, got, want)
		}
	}
}
