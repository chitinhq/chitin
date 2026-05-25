package speclint

import (
	"strings"
	"testing"
)

const goodFrontmatter = `---
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
`

func TestL01Frontmatter_Clean(t *testing.T) {
	got := L01Frontmatter("spec.md", goodFrontmatter)
	if len(got) != 0 {
		t.Fatalf("expected no violations on well-formed frontmatter, got %#v", got)
	}
}

func TestL01Frontmatter_CleanWithEmptyLists(t *testing.T) {
	// depends_on / related declared as explicit empty lists are well-formed.
	content := `---
spec_id: 200
title: A standalone spec
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

# Spec 200
`
	got := L01Frontmatter("spec.md", content)
	if len(got) != 0 {
		t.Fatalf("expected no violations on empty-list frontmatter, got %#v", got)
	}
}

func TestL01Frontmatter_NoOpeningFence(t *testing.T) {
	content := "# Spec 115\n\nsome content\n"
	got := L01Frontmatter("spec.md", content)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "missing") {
		t.Errorf("expected 'missing' in message, got %q", got[0].Message)
	}
	if got[0].Severity != SeverityError {
		t.Errorf("expected error severity, got %q", got[0].Severity)
	}
	// Line MUST be 1-based even when no fence is present; 0 would leak
	// past downstream consumers (PR-comment renderers, IDE jump-to-line).
	if got[0].Line != 1 {
		t.Errorf("expected Line=1 for missing-fence violation, got %d", got[0].Line)
	}
}

func TestL01Frontmatter_UnterminatedFence(t *testing.T) {
	content := `---
spec_id: 115
title: Stuff

# the closing fence is missing
`
	got := L01Frontmatter("spec.md", content)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "unterminated") {
		t.Errorf("expected 'unterminated' in message, got %q", got[0].Message)
	}
	if got[0].Line != 1 {
		t.Errorf("expected Line=1 for unterminated-fence violation, got %d", got[0].Line)
	}
}

func TestL01Frontmatter_MissingKey(t *testing.T) {
	// Each required key, dropped one at a time, surfaces exactly one
	// "missing required key" violation naming the dropped key.
	for _, key := range requiredKeysL01 {
		t.Run(key, func(t *testing.T) {
			content := dropKey(goodFrontmatter, key)
			got := L01Frontmatter("spec.md", content)
			if len(got) != 1 {
				t.Fatalf("expected 1 violation for missing %q, got %d: %#v", key, len(got), got)
			}
			if !strings.Contains(got[0].Message, "missing required key") {
				t.Errorf("expected 'missing required key' in message, got %q", got[0].Message)
			}
			if !strings.Contains(got[0].Message, key) {
				t.Errorf("expected key %q named in message %q", key, got[0].Message)
			}
		})
	}
}

func TestL01Frontmatter_SpecIDNotInt(t *testing.T) {
	// A quoted scalar resolves to !!str and must fail.
	content := strings.Replace(goodFrontmatter, "spec_id: 115", `spec_id: "115"`, 1)
	got := L01Frontmatter("spec.md", content)
	if len(got) != 1 || !strings.Contains(got[0].Message, "must be an integer") {
		t.Fatalf("expected 'must be an integer' for quoted spec_id, got %#v", got)
	}
}

func TestL01Frontmatter_SpecIDNonPositive(t *testing.T) {
	cases := []string{"0", "-1"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			content := strings.Replace(goodFrontmatter, "spec_id: 115", "spec_id: "+v, 1)
			got := L01Frontmatter("spec.md", content)
			if len(got) != 1 || !strings.Contains(got[0].Message, "positive integer") {
				t.Fatalf("expected 'positive integer' for spec_id=%s, got %#v", v, got)
			}
		})
	}
}

func TestL01Frontmatter_EmptyStringFields(t *testing.T) {
	cases := map[string]string{
		"title":  `title: ""`,
		"status": `status: ""`,
		"owner":  `owner: ""`,
	}
	for key, replacement := range cases {
		t.Run(key, func(t *testing.T) {
			content := replaceKeyLine(goodFrontmatter, key, replacement)
			got := L01Frontmatter("spec.md", content)
			if len(got) != 1 || !strings.Contains(got[0].Message, "non-empty string") {
				t.Fatalf("expected 'non-empty string' for empty %s, got %#v", key, got)
			}
		})
	}
}

func TestL01Frontmatter_StringFieldsRejectNonStringScalars(t *testing.T) {
	// YAML-typed scalars (title: 123 → !!int, owner: true → !!bool) must
	// be rejected. Without the !!str tag check they pass ScalarNode + non-
	// null and silently masquerade as strings.
	cases := map[string]string{
		"title-int":   "title: 123",
		"status-bool": "status: true",
		"owner-int":   "owner: 42",
	}
	for name, replacement := range cases {
		t.Run(name, func(t *testing.T) {
			key := strings.SplitN(replacement, ":", 2)[0]
			content := replaceKeyLine(goodFrontmatter, key, replacement)
			got := L01Frontmatter("spec.md", content)
			if len(got) != 1 || !strings.Contains(got[0].Message, "non-empty string") {
				t.Fatalf("expected 'non-empty string' for %s, got %#v", name, got)
			}
		})
	}
}

func TestL01Frontmatter_CreatedBadFormat(t *testing.T) {
	cases := []string{
		"created: tomorrow",
		"created: 2026/05/25",
		"created: 25-05-2026",
	}
	for _, replacement := range cases {
		t.Run(replacement, func(t *testing.T) {
			content := replaceKeyLine(goodFrontmatter, "created", replacement)
			got := L01Frontmatter("spec.md", content)
			if len(got) != 1 || !strings.Contains(got[0].Message, "YYYY-MM-DD") {
				t.Fatalf("expected 'YYYY-MM-DD' message for %q, got %#v", replacement, got)
			}
		})
	}
}

func TestL01Frontmatter_DependsOnScalar(t *testing.T) {
	// A bare "depends_on:" parses to a null scalar; rejected so authors
	// write "[]" explicitly when there are no dependencies.
	content := strings.Replace(goodFrontmatter,
		"depends_on:\n  - 097\n  - 113",
		"depends_on:",
		1,
	)
	got := L01Frontmatter("spec.md", content)
	if len(got) != 1 || !strings.Contains(got[0].Message, "YAML sequence") {
		t.Fatalf("expected 'YAML sequence' for bare depends_on, got %#v", got)
	}
}

func TestL01Frontmatter_RelatedScalar(t *testing.T) {
	content := strings.Replace(goodFrontmatter,
		"related:\n  - 094\n  - 098",
		"related: 094",
		1,
	)
	got := L01Frontmatter("spec.md", content)
	if len(got) != 1 || !strings.Contains(got[0].Message, "YAML sequence") {
		t.Fatalf("expected 'YAML sequence' for scalar related, got %#v", got)
	}
}

func TestL01Frontmatter_LineNumbersPointIntoSpec(t *testing.T) {
	// The "title" key lives on source line 3 of goodFrontmatter (the
	// opening fence is line 1, spec_id line 2, title line 3). A violation
	// on title must report line 3, not the YAML-body-relative line.
	content := replaceKeyLine(goodFrontmatter, "title", `title: ""`)
	got := L01Frontmatter("spec.md", content)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %#v", got)
	}
	if got[0].Line != 3 {
		t.Errorf("expected violation on line 3 (the title line), got line %d", got[0].Line)
	}
	if got[0].File != "spec.md" {
		t.Errorf("expected File=spec.md, got %q", got[0].File)
	}
	if got[0].Rule != "L01" {
		t.Errorf("expected Rule=L01, got %q", got[0].Rule)
	}
}

func TestL01Frontmatter_OrderingIsDeterministic(t *testing.T) {
	// When every required key is missing, violations are reported in
	// requiredKeysL01 order — operators and tests see a stable sequence.
	content := "---\nfoo: bar\n---\n"
	got := L01Frontmatter("spec.md", content)
	if len(got) != len(requiredKeysL01) {
		t.Fatalf("expected %d violations, got %d: %#v", len(requiredKeysL01), len(got), got)
	}
	for i, key := range requiredKeysL01 {
		if !strings.Contains(got[i].Message, key) {
			t.Errorf("violation[%d]: expected key %q in message, got %q", i, key, got[i].Message)
		}
	}
}

func TestL01Frontmatter_NonMappingBody(t *testing.T) {
	// A YAML sequence (or a scalar) at the top level is not a mapping —
	// L01 reports that distinctly from a missing-key cascade.
	content := "---\n- a\n- b\n---\n"
	got := L01Frontmatter("spec.md", content)
	if len(got) != 1 || !strings.Contains(got[0].Message, "not a mapping") {
		t.Fatalf("expected 'not a mapping' violation, got %#v", got)
	}
}

// dropKey returns goodFrontmatter with the given top-level frontmatter
// key (and its value lines, when multi-line) removed. Used to assert
// each required key independently surfaces a "missing" violation.
func dropKey(s, key string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, key+":") {
			skipping = true
			continue
		}
		if skipping {
			// Continuation lines start with whitespace (the YAML "- 097"
			// indented children). A non-indented line ends the block.
			if len(ln) > 0 && (ln[0] == ' ' || ln[0] == '\t') {
				continue
			}
			skipping = false
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// replaceKeyLine swaps the single line that begins with "<key>:" for
// `replacement`. It does not touch continuation lines, so callers using
// it on list-valued keys must replace the whole block themselves.
func replaceKeyLine(s, key, replacement string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), key+":") {
			lines[i] = replacement
			return strings.Join(lines, "\n")
		}
	}
	return s
}
