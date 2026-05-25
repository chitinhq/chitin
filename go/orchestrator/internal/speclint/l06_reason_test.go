package speclint

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// writeSpec writes spec.md (+ optionally tasks.md) under a fresh subdirectory
// of root and returns the spec dir path.
func writeSpec(t *testing.T, root, name, specMD, tasksMD string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(specMD), 0o644); err != nil {
		t.Fatalf("write spec.md: %v", err)
	}
	if tasksMD != "" {
		if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte(tasksMD), 0o644); err != nil {
			t.Fatalf("write tasks.md: %v", err)
		}
	}
	return dir
}

// canonicalSpec mirrors the FR-010-style declaration this rule was designed
// around. Spec id 200 declares a 3-element closed set.
const canonicalSpec = `---
spec_id: 200
title: Canonical decl
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

# Spec 200

## Functional requirements

- **FR-010** Canonical ` + "`reason`" + ` strings for ` + "`some_event`" + `
  (closed set):
  - ` + "`alpha`" + ` — first
  - ` + "`beta`" + ` — second
  - ` + "`gamma`" + ` — third

## Other

- **FR-011** Something unrelated.
`

func TestCheckReasonTaxonomy_AllUsagesDeclared(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root, "200-canonical", canonicalSpec+`
## Edge cases

- emits an event with ` + "`reason: \"alpha\"`" + `
- escalates with ` + "`reason: \"beta\"`" + `
`, `---
description: tasks
---
- [ ] T001 emit ` + "`{ reason: \"gamma\" }`" + ` on completion
`)

	got, err := CheckReasonTaxonomy(specDir, "")
	if err != nil {
		t.Fatalf("CheckReasonTaxonomy: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 violations, got %d: %+v", len(got), got)
	}
}

func TestCheckReasonTaxonomy_UndeclaredReason(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root, "200-canonical", canonicalSpec+`
## Edge cases

- emits ` + "`reason: \"alpha\"`" + ` on success
- emits ` + "`reason: \"undeclared_kind\"`" + ` on failure
`, "")

	got, err := CheckReasonTaxonomy(specDir, "")
	if err != nil {
		t.Fatalf("CheckReasonTaxonomy: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	v := got[0]
	if v.Rule != "L06" || v.File != "spec.md" || v.Severity != SeverityError {
		t.Errorf("violation shape unexpected: %+v", v)
	}
	if !contains(v.Message, "undeclared_kind") {
		t.Errorf("message should name the offending reason; got %q", v.Message)
	}
}

func TestCheckReasonTaxonomy_DependsOnReasons(t *testing.T) {
	root := t.TempDir()
	// Base spec declares "delta".
	writeSpec(t, root, "100-base", `---
spec_id: 100
title: Base
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

## Functional requirements

- **FR-005** Canonical `+"`reason`"+` values for `+"`base_event`"+`
  (closed set):
  - `+"`delta`"+` — only one
`, "")

	// Dependent spec declares "alpha" and depends_on 100, then uses "delta"
	// (from depends_on) and "alpha" (self) — both should resolve.
	depSpec := `---
spec_id: 201
title: Dep
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 100
related: []
---

## Functional requirements

- **FR-010** Canonical `+"`reason`"+` strings for `+"`some_event`"+`
  (closed set):
  - `+"`alpha`"+` — local

## Edge cases

- emits `+"`reason: \"alpha\"`"+`
- also emits `+"`reason: \"delta\"`"+` (from depends_on)
`
	depDir := writeSpec(t, root, "201-dep", depSpec, "")

	got, err := CheckReasonTaxonomy(depDir, root)
	if err != nil {
		t.Fatalf("CheckReasonTaxonomy: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 violations (delta declared in depends_on=100), got %d: %+v", len(got), got)
	}
}

func TestCheckReasonTaxonomy_NoCanonicalDeclaration(t *testing.T) {
	// Spec has no canonical reason FR but uses a `reason:` value — that
	// usage cannot be in the (empty) closed set, so it's a violation.
	root := t.TempDir()
	specDir := writeSpec(t, root, "300-noset", `---
spec_id: 300
title: NoSet
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

## Edge cases

- emits `+"`reason: \"orphan\"`"+`
`, "")

	got, err := CheckReasonTaxonomy(specDir, "")
	if err != nil {
		t.Fatalf("CheckReasonTaxonomy: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(got), got)
	}
	if !contains(got[0].Message, "orphan") {
		t.Errorf("expected violation to name 'orphan'; got %q", got[0].Message)
	}
}

func TestCheckReasonTaxonomy_DeclarationLinesAreMasked(t *testing.T) {
	// The declaration block itself contains `reason: "alpha"` inline as an
	// illustration. Masking should prevent it from being scanned.
	root := t.TempDir()
	specDir := writeSpec(t, root, "400-inline", `---
spec_id: 400
title: Inline
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on: []
related: []
---

## Functional requirements

- **FR-010** Canonical `+"`reason`"+` set for `+"`evt`"+`
  (closed set):
  - `+"`alpha`"+` — emitted as `+"`reason: \"alpha\"`"+` in chain
`, "")

	got, err := CheckReasonTaxonomy(specDir, "")
	if err != nil {
		t.Fatalf("CheckReasonTaxonomy: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("declaration-line usage should be masked; got %d: %+v", len(got), got)
	}
}

func TestCheckReasonTaxonomy_TasksMDScanned(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root, "500-tasks", canonicalSpec, `---
description: tasks
---
- [ ] T001 emit `+"`reason: \"alpha\"`"+`           // declared
- [ ] T002 emit `+"`reason: \"not_declared\"`"+`   // undeclared
`)

	got, err := CheckReasonTaxonomy(specDir, "")
	if err != nil {
		t.Fatalf("CheckReasonTaxonomy: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 violation from tasks.md, got %d: %+v", len(got), got)
	}
	if got[0].File != "tasks.md" {
		t.Errorf("expected violation in tasks.md, got %q", got[0].File)
	}
	if !contains(got[0].Message, "not_declared") {
		t.Errorf("message should name 'not_declared'; got %q", got[0].Message)
	}
}

func TestCheckReasonTaxonomy_SortedOutput(t *testing.T) {
	// Multiple violations across both files should sort by (file asc, line asc).
	root := t.TempDir()
	specDir := writeSpec(t, root, "600-multi", canonicalSpec+`
## Edge cases

- `+"`reason: \"undeclared_b\"`"+`
- `+"`reason: \"undeclared_a\"`"+`
`, `---
description: tasks
---
- T001 `+"`reason: \"task_bad\"`"+`
`)

	got, err := CheckReasonTaxonomy(specDir, "")
	if err != nil {
		t.Fatalf("CheckReasonTaxonomy: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 violations, got %d: %+v", len(got), got)
	}
	// Verify the (file asc, line asc) ordering.
	for i := 1; i < len(got); i++ {
		if got[i-1].File > got[i].File {
			t.Errorf("file order broken at index %d: %+v", i, got)
		}
		if got[i-1].File == got[i].File && got[i-1].Line > got[i].Line {
			t.Errorf("line order broken at index %d: %+v", i, got)
		}
	}
}

func TestCheckReasonTaxonomy_MissingSpecMD(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "700-empty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := CheckReasonTaxonomy(dir, "")
	if err == nil {
		t.Fatalf("expected error for missing spec.md, got nil")
	}
}

func TestExtractCanonicalReasons_SkipsUnrelatedFRBlocks(t *testing.T) {
	// FR-009 mentions `reason` in a field listing but is NOT a canonical
	// declaration — must not contribute tokens.
	text := `## Functional requirements

- **FR-009** Chain events:
  - ` + "`event_a { pr_number, reason }`" + ` (just listing the field)
  - ` + "`event_b { pr_number, x }`" + `

- **FR-010** Canonical ` + "`reason`" + ` strings for ` + "`evt`" + `:
  - ` + "`good`" + ` — first
`

	reasons, _ := extractCanonicalReasons(text)
	want := map[string]struct{}{"good": {}}
	if !reflect.DeepEqual(reasons, want) {
		// Sort keys for stable error message.
		var got []string
		for k := range reasons {
			got = append(got, k)
		}
		sort.Strings(got)
		t.Fatalf("expected only {good}; got %v", got)
	}
}

func TestExtractCanonicalReasons_MarkerMustBeNearHeader(t *testing.T) {
	// An FR that mentions "canonical" / "reason" far down in its body
	// should NOT count as a declaration.
	text := `- **FR-001** Some unrelated requirement.
  Line 2.
  Line 3.
  Line 4.
  Line 5 — and here we describe the canonical reason format used elsewhere.
  - ` + "`should_not_capture`" + `
`
	reasons, ranges := extractCanonicalReasons(text)
	if len(reasons) != 0 {
		t.Errorf("expected no declared reasons; got %v", reasons)
	}
	if len(ranges) != 0 {
		t.Errorf("expected no declaration ranges; got %v", ranges)
	}
}

func TestDependsOnIDsForL06_PadsToThreeDigits(t *testing.T) {
	text := `---
spec_id: 999
title: T
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 7
  - 97
  - 113
related: []
---
`
	got := dependsOnIDsForL06(text)
	want := []string{"007", "097", "113"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v; want %v", got, want)
	}
}

func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
