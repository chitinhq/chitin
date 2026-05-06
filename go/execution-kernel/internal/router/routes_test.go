package router

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultRoutesPolicy(t *testing.T) {
	p := DefaultRoutesPolicy()
	if p.Enabled {
		t.Fatal("default must be disabled — operator opts in")
	}
	if p.SpawnTimeoutSeconds != 60 {
		t.Errorf("default spawn_timeout_seconds: got %d want 60", p.SpawnTimeoutSeconds)
	}
	if p.MaxConcurrentPeers != 1 {
		t.Errorf("default max_concurrent_peers: got %d want 1", p.MaxConcurrentPeers)
	}
	if p.Routes == nil {
		t.Error("default Routes must be non-nil empty map")
	}
}

func TestLoadRoutesPolicyMissing(t *testing.T) {
	dir := t.TempDir()
	p, err := LoadRoutesPolicy(dir)
	if err != nil {
		t.Fatalf("missing chitin-routes.yaml should not error: %v", err)
	}
	if p.Enabled {
		t.Error("missing file → default disabled")
	}
}

func TestLoadRoutesPolicyValid(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
enabled: true
spawn_timeout_seconds: 90
max_concurrent_peers: 2
rules:
  - name: floundering-loop
    signal: floundering
    severity: ">= 2 loops"
    route: patch_quality
    max_per_hour: 10
routes:
  patch_quality:
    - driver: copilot
      model: gpt-5.4
    - driver: claude
      model: claude-opus-4-6
`
	if err := os.WriteFile(filepath.Join(dir, "chitin-routes.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadRoutesPolicy(dir)
	if err != nil {
		t.Fatalf("valid yaml should load: %v", err)
	}
	if !p.Enabled {
		t.Error("expected enabled=true")
	}
	if p.SpawnTimeoutSeconds != 90 {
		t.Errorf("spawn_timeout_seconds: got %d want 90", p.SpawnTimeoutSeconds)
	}
	if p.MaxConcurrentPeers != 2 {
		t.Errorf("max_concurrent_peers: got %d want 2", p.MaxConcurrentPeers)
	}
	if len(p.Rules) != 1 || p.Rules[0].Name != "floundering-loop" {
		t.Errorf("rules not parsed: %+v", p.Rules)
	}
	if len(p.Routes["patch_quality"]) != 2 {
		t.Errorf("route candidates not parsed: %+v", p.Routes)
	}
	if p.Routes["patch_quality"][0].Driver != "copilot" {
		t.Errorf("first candidate driver: got %q want copilot", p.Routes["patch_quality"][0].Driver)
	}
}

func TestLoadRoutesPolicyParentWalk(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	yaml := `version: 1
enabled: true
routes:
  cheap+stable:
    - driver: copilot
      model: gpt-4.1
`
	if err := os.WriteFile(filepath.Join(root, "chitin-routes.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadRoutesPolicy(deep)
	if err != nil {
		t.Fatalf("parent walk should find file: %v", err)
	}
	if !p.Enabled {
		t.Error("found file should populate Enabled")
	}
}

func TestLoadRoutesPolicyInvalidYaml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chitin-routes.yaml"), []byte("not: valid: yaml: at: all:::"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadRoutesPolicy(dir)
	if err == nil {
		t.Error("malformed yaml should return error")
	}
	if p.Enabled {
		t.Error("error should not leak partial parse — must default disabled")
	}
}

func TestValidateUnknownSignal(t *testing.T) {
	p := RoutesPolicy{
		Version: 1,
		Routes: map[string][]Candidate{
			"foo": {{Driver: "copilot", Model: "gpt-4.1"}},
		},
		Rules: []RoutingRule{{
			Name: "bad", Signal: "telepathy", Route: "foo",
		}},
	}
	if err := ValidateRoutesPolicy(p); err == nil {
		t.Error("unknown signal should fail validation")
	}
}

func TestValidateRuleReferencesMissingRoute(t *testing.T) {
	p := RoutesPolicy{
		Version: 1,
		Rules: []RoutingRule{{
			Name: "x", Signal: "floundering", Route: "missing",
		}},
		Routes: map[string][]Candidate{
			"other": {{Driver: "copilot", Model: "gpt-4.1"}},
		},
	}
	if err := ValidateRoutesPolicy(p); err == nil {
		t.Error("rule.route not in routes map should fail validation")
	}
}

func TestValidateDuplicateRuleName(t *testing.T) {
	p := RoutesPolicy{
		Version: 1,
		Routes:  map[string][]Candidate{"r": {{Driver: "x", Model: "y"}}},
		Rules: []RoutingRule{
			{Name: "dup", Signal: "floundering", Route: "r"},
			{Name: "dup", Signal: "blast_radius", Route: "r"},
		},
	}
	if err := ValidateRoutesPolicy(p); err == nil {
		t.Error("duplicate rule name should fail validation")
	}
}

func TestValidateEmptyCandidateList(t *testing.T) {
	p := RoutesPolicy{
		Version: 1,
		Routes:  map[string][]Candidate{"empty": {}},
	}
	if err := ValidateRoutesPolicy(p); err == nil {
		t.Error("empty candidate list should fail validation")
	}
}

func TestValidateMissingDriverOrModel(t *testing.T) {
	p := RoutesPolicy{
		Version: 1,
		Routes: map[string][]Candidate{
			"x": {{Driver: "copilot"}}, // missing model
		},
	}
	if err := ValidateRoutesPolicy(p); err == nil {
		t.Error("missing model should fail validation")
	}
}

func TestValidateUnknownVersion(t *testing.T) {
	p := RoutesPolicy{Version: 99}
	if err := ValidateRoutesPolicy(p); err == nil {
		t.Error("unknown schema version should fail validation")
	}
}

func TestExampleFileValidates(t *testing.T) {
	// The committed chitin-routes.example.yaml at the repo root must
	// always pass validation — it's documentation operators copy from.
	repoRoot, err := filepath.Abs("../../../..")
	if err != nil {
		t.Skipf("can't resolve repo root: %v", err)
	}
	example := filepath.Join(repoRoot, "chitin-routes.example.yaml")
	if _, err := os.Stat(example); err != nil {
		t.Skipf("no example file at %s; skipping (CI may run from a worktree)", example)
	}
	data, err := os.ReadFile(example)
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	// Write to a temp dir under a name the loader recognizes.
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "chitin-routes.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRoutesPolicy(tmp); err != nil {
		t.Errorf("chitin-routes.example.yaml must validate: %v", err)
	}
}
