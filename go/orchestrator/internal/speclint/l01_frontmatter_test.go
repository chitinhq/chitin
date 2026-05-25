package speclint

import (
	"sort"
	"strings"
	"testing"
)

const cleanSpec = `---
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
  - 098
---

# Spec 115

Body content.
`

func TestL01_Clean(t *testing.T) {
	got := CheckL01Frontmatter("spec.md", []byte(cleanSpec))
	if len(got) != 0 {
		t.Fatalf("clean spec produced violations: %+v", got)
	}
}

func TestL01_EmptyFile(t *testing.T) {
	got := CheckL01Frontmatter("spec.md", []byte(""))
	if len(got) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(got), got)
	}
	if got[0].Line != 1 || !strings.Contains(got[0].Message, "frontmatter not found") {
		t.Errorf("unexpected violation: %+v", got[0])
	}
}

func TestL01_NoOpeningDelimiter(t *testing.T) {
	got := CheckL01Frontmatter("spec.md", []byte("# Spec without frontmatter\n\nbody\n"))
	if len(got) != 1 || got[0].Line != 1 {
		t.Fatalf("want single line-1 violation, got %+v", got)
	}
}

func TestL01_UnterminatedFrontmatter(t *testing.T) {
	src := "---\nspec_id: 115\ntitle: foo\n\n# body, no closing ---\n"
	got := CheckL01Frontmatter("spec.md", []byte(src))
	if len(got) != 1 || got[0].Line != 1 {
		t.Fatalf("want single line-1 violation for unterminated block, got %+v", got)
	}
}

func TestL01_MalformedYAML(t *testing.T) {
	src := "---\nspec_id: 115\n  title: bad-indent\n\t- not valid\n---\n"
	got := CheckL01Frontmatter("spec.md", []byte(src))
	if len(got) != 1 {
		t.Fatalf("want exactly 1 violation, got %+v", got)
	}
	if !strings.Contains(got[0].Message, "not valid YAML") {
		t.Errorf("expected YAML parse error message, got %q", got[0].Message)
	}
	if got[0].Line != 1 {
		t.Errorf("want line=1 (opening delimiter), got %d", got[0].Line)
	}
}

func TestL01_EmptyFrontmatterBody(t *testing.T) {
	got := CheckL01Frontmatter("spec.md", []byte("---\n---\n\nbody\n"))
	if len(got) != 1 || !strings.Contains(got[0].Message, "empty") {
		t.Fatalf("want single empty-block violation, got %+v", got)
	}
}

func TestL01_AllKeysMissing(t *testing.T) {
	src := "---\nirrelevant: 1\n---\n"
	got := CheckL01Frontmatter("spec.md", []byte(src))
	// 7 required keys, all absent → 7 violations.
	if len(got) != len(l01RequiredKeys) {
		t.Fatalf("want %d violations, got %d: %+v", len(l01RequiredKeys), len(got), got)
	}
	gotKeys := make([]string, 0, len(got))
	for _, v := range got {
		if v.Severity != SeverityError {
			t.Errorf("want SeverityError, got %q", v.Severity)
		}
		if !strings.Contains(v.Message, "missing required key") {
			t.Errorf("want 'missing required key' message, got %q", v.Message)
		}
		// Extract the key from the quoted message tail.
		gotKeys = append(gotKeys, extractQuotedKey(v.Message))
	}
	wantKeys := append([]string(nil), l01RequiredKeys...)
	sort.Strings(wantKeys)
	sort.Strings(gotKeys)
	for i := range wantKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Errorf("required-keys mismatch: want %v, got %v", wantKeys, gotKeys)
			break
		}
	}
}

func TestL01_DeterministicOrder(t *testing.T) {
	src := "---\nirrelevant: 1\n---\n"
	first := CheckL01Frontmatter("spec.md", []byte(src))
	second := CheckL01Frontmatter("spec.md", []byte(src))
	if len(first) != len(second) {
		t.Fatalf("length differs across runs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Message != second[i].Message {
			t.Errorf("violation order non-deterministic at index %d: %q vs %q",
				i, first[i].Message, second[i].Message)
		}
	}
}

func TestL01_SpecIDMalformed(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"string-not-int", `spec_id: "not-a-number"`},
		{"zero", `spec_id: 0`},
		{"negative", `spec_id: -5`},
		{"float", `spec_id: 3.14`},
		{"list", "spec_id:\n  - 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := withSpecIDLine(tc.val)
			got := CheckL01Frontmatter("spec.md", []byte(src))
			if !hasViolationFor(got, "spec_id") {
				t.Fatalf("expected spec_id malformed violation, got %+v", got)
			}
		})
	}
}

func TestL01_SpecIDAcceptedShapes(t *testing.T) {
	// Bare int (the canonical form) and a quoted-string int both pass —
	// the latter mirrors how YAML 1.2 returns leading-zero spec IDs.
	for _, val := range []string{`spec_id: 115`, `spec_id: "115"`} {
		t.Run(val, func(t *testing.T) {
			src := withSpecIDLine(val)
			got := CheckL01Frontmatter("spec.md", []byte(src))
			if hasViolationFor(got, "spec_id") {
				t.Errorf("spec_id %s should be accepted, got %+v", val, got)
			}
		})
	}
}

func TestL01_StringKeysEmpty(t *testing.T) {
	for _, key := range []string{"title", "status", "owner"} {
		t.Run(key, func(t *testing.T) {
			src := strings.Replace(cleanSpec, key+": ", key+": ", 1)
			// Replace the value with whitespace only.
			src = replaceFrontmatterValue(src, key, `"   "`)
			got := CheckL01Frontmatter("spec.md", []byte(src))
			if !hasViolationFor(got, key) {
				t.Fatalf("expected %s empty-value violation, got %+v", key, got)
			}
		})
	}
}

func TestL01_CreatedBadDate(t *testing.T) {
	cases := []string{`2026/05/25`, `not-a-date`, `25-05-2026`}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			src := replaceFrontmatterValue(cleanSpec, "created", v)
			got := CheckL01Frontmatter("spec.md", []byte(src))
			if !hasViolationFor(got, "created") {
				t.Fatalf("expected created violation for %q, got %+v", v, got)
			}
		})
	}
}

func TestL01_DependsOnNotASequence(t *testing.T) {
	src := replaceFrontmatterValue(cleanSpec, "depends_on", "not-a-list")
	got := CheckL01Frontmatter("spec.md", []byte(src))
	if !hasViolationFor(got, "depends_on") {
		t.Fatalf("expected depends_on shape violation, got %+v", got)
	}
}

func TestL01_DependsOnEmptyOK(t *testing.T) {
	src := strings.Replace(cleanSpec, "depends_on:\n  - 097\n  - 113", "depends_on: []", 1)
	got := CheckL01Frontmatter("spec.md", []byte(src))
	if hasViolationFor(got, "depends_on") {
		t.Errorf("empty depends_on should be valid, got %+v", got)
	}
}

func TestL01_DependsOnNullOK(t *testing.T) {
	src := strings.Replace(cleanSpec, "depends_on:\n  - 097\n  - 113", "depends_on:", 1)
	got := CheckL01Frontmatter("spec.md", []byte(src))
	if hasViolationFor(got, "depends_on") {
		t.Errorf("null depends_on should be valid, got %+v", got)
	}
}

func TestL01_DependsOnBadElement(t *testing.T) {
	src := strings.Replace(cleanSpec, "  - 097\n", "  - foo\n", 1)
	got := CheckL01Frontmatter("spec.md", []byte(src))
	if !hasViolationFor(got, "depends_on") {
		t.Fatalf("expected depends_on element violation, got %+v", got)
	}
}

func TestL01_ViolationLineMatchesKey(t *testing.T) {
	// `created` sits on line 6 of cleanSpec. Replacing with a bad date should
	// report on line 6 too — not on the block opening line.
	src := replaceFrontmatterValue(cleanSpec, "created", "2026/05/25")
	got := CheckL01Frontmatter("spec.md", []byte(src))
	var found *Violation
	for i := range got {
		if extractQuotedKey(got[i].Message) == "created" {
			found = &got[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no created violation: %+v", got)
	}
	if found.Line != 6 {
		t.Errorf("want line=6 for created key, got %d (msg=%q)", found.Line, found.Message)
	}
}

func TestL01_LeadingBlankLinesBeforeFrontmatter(t *testing.T) {
	got := CheckL01Frontmatter("spec.md", []byte("\n\n"+cleanSpec))
	if len(got) != 0 {
		t.Fatalf("blank prelude shouldn't break parsing, got %+v", got)
	}
}

func TestL01_PrefixedTextBeforeDelimiter(t *testing.T) {
	got := CheckL01Frontmatter("spec.md", []byte("# Title before delimiter\n"+cleanSpec))
	if len(got) != 1 || !strings.Contains(got[0].Message, "frontmatter not found") {
		t.Fatalf("want no-frontmatter violation when non-blank content precedes ---, got %+v", got)
	}
}

// --- helpers ---

func withSpecIDLine(specIDLine string) string {
	// Replace `spec_id: 115` (line 2 of cleanSpec) with the test's value.
	return strings.Replace(cleanSpec, "spec_id: 115", specIDLine, 1)
}

func replaceFrontmatterValue(src, key, newValue string) string {
	lines := strings.Split(src, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(ln, key+":") {
			lines[i] = key + ": " + newValue
			return strings.Join(lines, "\n")
		}
	}
	return src
}

func hasViolationFor(vs []Violation, key string) bool {
	for _, v := range vs {
		if extractQuotedKey(v.Message) == key {
			return true
		}
	}
	return false
}

// extractQuotedKey pulls the first `"..."` token out of a violation
// message. Both the "missing required key" and "malformed" message
// shapes embed the key as a quoted token.
func extractQuotedKey(msg string) string {
	i := strings.IndexByte(msg, '"')
	if i < 0 {
		return ""
	}
	rest := msg[i+1:]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}
