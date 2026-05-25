// lint_golden_test.go — hermetic golden-fixture tests for spec 115 T020.
//
// Each fixture under `testdata/fixtures/<name>/.specify/specs/100-*/` is a
// minimal spec.md+tasks.md pair crafted to trigger exactly one rule's fail
// case (plus a `clean` baseline and an `l02_pass` cross-ref fixture). The
// test loads each spec dir via the speclint.Load + Run public API, then
// asserts three contracts from spec 115 FR-003:
//
//  1. Pass/fail coverage — each rule's pass case yields no L<NN> violation;
//     each fail case yields ≥1 error-severity L<NN> violation.
//  2. Structured JSON output shape — every Violation marshals to an object
//     with the keys {rule, file, line, severity, message} of the documented
//     types; an empty violation slice marshals to "[]" (not "null").
//  3. Exit code mapping — clean → 0, warnings-only → 2, any-error → 3. The
//     mapping is replicated here as a helper (cmd/chitin-orchestrator imports
//     speclint, so importing the cmd's specLintExitCode would be circular).
//     If FR-003's exit-code semantics change, this helper and the cmd's
//     mirror must update together.
//
// Lives alongside PR #1082's lint_test.go (basic loader tests) as a
// separate file to keep the merge trivial under the spec-115 sibling-
// rebase model.
package speclint

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// goldenCase describes a single golden-fixture lint scenario.
//
// Every fixture directory follows:
//
//	testdata/fixtures/<fixture>/.specify/specs/100-*/   ← spec under test
//	                                       (200-sibling/, only for l02_pass)
//
// wantViolatesRule == "" marks a clean / pass-case fixture (no rule should
// fire). Otherwise we require at least one violation tagged with that rule
// and severity — the existence check is one-sided rather than exact-match
// because a single fixture may incidentally trip an adjacent rule; the
// hermetic guarantee we need is "the rule under test fires", not "no other
// rule ever fires".
type goldenCase struct {
	fixture          string
	wantViolatesRule string
	wantSeverity     Severity
	wantExitCode     int
}

var goldenCases = []goldenCase{
	{fixture: "clean", wantExitCode: 0},
	{fixture: "l01_fail", wantViolatesRule: "L01", wantSeverity: SeverityError, wantExitCode: 3},
	{fixture: "l02_fail", wantViolatesRule: "L02", wantSeverity: SeverityError, wantExitCode: 3},
	{fixture: "l02_pass", wantExitCode: 0},
	{fixture: "l03_fail", wantViolatesRule: "L03", wantSeverity: SeverityError, wantExitCode: 3},
	{fixture: "l04_fail", wantViolatesRule: "L04", wantSeverity: SeverityError, wantExitCode: 3},
	{fixture: "l05_fail", wantViolatesRule: "L05", wantSeverity: SeverityError, wantExitCode: 3},
	{fixture: "l06_fail", wantViolatesRule: "L06", wantSeverity: SeverityError, wantExitCode: 3},
	{fixture: "l07_fail", wantViolatesRule: "L07", wantSeverity: SeverityError, wantExitCode: 3},
}

func TestGoldenFixtures(t *testing.T) {
	for _, tc := range goldenCases {
		tc := tc
		t.Run(tc.fixture, func(t *testing.T) {
			specDir := resolveFixtureSpecDir(t, tc.fixture)
			s, err := Load(specDir)
			if err != nil {
				t.Fatalf("Load(%q): %v", specDir, err)
			}
			vs := Run(s)
			assertGoldenViolations(t, vs, tc)
			assertJSONShape(t, vs)
			assertExitCode(t, vs, tc.wantExitCode)
		})
	}
}

// resolveFixtureSpecDir picks the `100-*` spec dir under a fixture's
// workspace. For l02_pass the workspace also contains `200-sibling/` as a
// resolution target — we deliberately glob for the `100-*` prefix so we
// always pick the spec under test.
func resolveFixtureSpecDir(t *testing.T, fixture string) string {
	t.Helper()
	pattern := filepath.Join("testdata", "fixtures", fixture, ".specify", "specs", "100-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %q: %v", pattern, err)
	}
	if len(matches) != 1 {
		t.Fatalf("fixture %q: want exactly one spec-under-test dir matching %q, got %v",
			fixture, pattern, matches)
	}
	return matches[0]
}

func assertGoldenViolations(t *testing.T, vs []Violation, tc goldenCase) {
	t.Helper()
	if tc.wantViolatesRule == "" {
		if len(vs) != 0 {
			t.Errorf("clean fixture %q produced %d violation(s); want zero: %+v",
				tc.fixture, len(vs), vs)
		}
		return
	}
	for _, v := range vs {
		if v.Rule == tc.wantViolatesRule && v.Severity == tc.wantSeverity {
			return
		}
	}
	t.Errorf("fixture %q: no violation matched (rule=%s severity=%s); got %+v",
		tc.fixture, tc.wantViolatesRule, tc.wantSeverity, vs)
}

// assertJSONShape verifies spec 115 FR-003's structured-output contract:
// the violations slice marshals to a JSON array whose elements each have
// {rule, file, line, severity, message} with the documented types. Empty
// slice must marshal to "[]" (not "null") so PostLintViolations (T010)
// parses stdout uniformly across clean and non-clean runs.
func assertJSONShape(t *testing.T, vs []Violation) {
	t.Helper()
	body, err := json.Marshal(vs)
	if err != nil {
		t.Fatalf("marshal violations: %v", err)
	}
	if len(vs) == 0 {
		if string(body) != "[]" {
			t.Errorf("empty violations slice should marshal to %q, got %q (nil slice marshals to \"null\" — Run() must return a non-nil empty slice for clean runs)", "[]", string(body))
		}
		return
	}
	var generic []map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		t.Fatalf("unmarshal violations: %v\nbody=%s", err, body)
	}
	requiredKeys := []string{"rule", "file", "line", "severity", "message"}
	for i, obj := range generic {
		for _, k := range requiredKeys {
			if _, ok := obj[k]; !ok {
				t.Errorf("violation[%d] missing key %q: %v", i, k, obj)
			}
		}
		assertStringKey(t, obj, "rule", i)
		assertStringKey(t, obj, "file", i)
		assertStringKey(t, obj, "severity", i)
		assertStringKey(t, obj, "message", i)
		if _, ok := obj["line"].(float64); !ok {
			t.Errorf("violation[%d].line not a JSON number: %#v", i, obj["line"])
		}
	}
}

func assertStringKey(t *testing.T, obj map[string]any, key string, idx int) {
	t.Helper()
	v, ok := obj[key].(string)
	if !ok {
		t.Errorf("violation[%d].%s not a string: %#v", idx, key, obj[key])
		return
	}
	if strings.TrimSpace(v) == "" {
		t.Errorf("violation[%d].%s is empty/whitespace", idx, key)
	}
}

// exitCodeForViolations mirrors specLintExitCode() in
// cmd/chitin-orchestrator/spec_lint.go without importing it (cmd imports
// speclint, so the reverse import would cycle). Both implementations of
// FR-003's exit-code mapping must stay aligned.
func exitCodeForViolations(vs []Violation) int {
	if len(vs) == 0 {
		return 0
	}
	for _, v := range vs {
		if v.Severity == SeverityError {
			return 3
		}
	}
	return 2
}

func assertExitCode(t *testing.T, vs []Violation, want int) {
	t.Helper()
	got := exitCodeForViolations(vs)
	if got != want {
		t.Errorf("exit code = %d, want %d (violations=%+v)", got, want, vs)
	}
}

// TestExitCodeMapping_Synthetic pins the mapping on synthetic violation
// inputs — guarantees the helper used above stays a faithful mirror of
// the cmd's specLintExitCode() even when no rules are registered.
func TestExitCodeMapping_Synthetic(t *testing.T) {
	cases := []struct {
		name string
		vs   []Violation
		want int
	}{
		{"empty", nil, 0},
		{"empty-slice", []Violation{}, 0},
		{"single-warning", []Violation{{Rule: "L01", Severity: SeverityWarning}}, 2},
		{"single-error", []Violation{{Rule: "L01", Severity: SeverityError}}, 3},
		{"warning-then-error", []Violation{
			{Rule: "L01", Severity: SeverityWarning},
			{Rule: "L02", Severity: SeverityError},
		}, 3},
		{"error-then-warning", []Violation{
			{Rule: "L01", Severity: SeverityError},
			{Rule: "L02", Severity: SeverityWarning},
		}, 3},
		{"two-warnings", []Violation{
			{Rule: "L01", Severity: SeverityWarning},
			{Rule: "L02", Severity: SeverityWarning},
		}, 2},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeForViolations(tc.vs); got != tc.want {
				t.Errorf("exit code = %d, want %d", got, tc.want)
			}
		})
	}
}
