package router

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()
	if p.Enabled {
		t.Fatal("DefaultPolicy should have Enabled=false")
	}
	if _, ok := p.Heuristics["blast_radius"]; !ok {
		t.Fatal("DefaultPolicy should contain blast_radius heuristic")
	}
	if _, ok := p.Heuristics["floundering"]; !ok {
		t.Fatal("DefaultPolicy should contain floundering heuristic")
	}
	br := p.Heuristics["blast_radius"]
	if !br.Enabled || br.Threshold != 0.6 {
		t.Fatalf("blast_radius: Enabled=%v Threshold=%v, want true/0.6", br.Enabled, br.Threshold)
	}
	fl := p.Heuristics["floundering"]
	if !fl.Enabled || fl.MaxLoopCount != 3 || fl.MaxStallSeconds != 600 {
		t.Fatalf("floundering: Enabled=%v MaxLoop=%d MaxStall=%d, want true/3/600",
			fl.Enabled, fl.MaxLoopCount, fl.MaxStallSeconds)
	}
	if p.Advisor.Enabled {
		t.Fatal("Advisor should be disabled in default")
	}
}

func TestFindChitinYaml_InCurrentDir(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "chitin.yaml")
	if err := os.WriteFile(yamlPath, []byte("router:\n  enabled: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	result := FindChitinYaml(dir)
	if result != yamlPath {
		t.Fatalf("expected %s, got %s", yamlPath, result)
	}
}

func TestFindChitinYaml_InParentDir(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "chitin.yaml")
	if err := os.WriteFile(yamlPath, []byte("router:\n  enabled: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dir, "sub", "project")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	result := FindChitinYaml(child)
	if result != yamlPath {
		t.Fatalf("expected %s, got %s", yamlPath, result)
	}
}

func TestFindChitinYaml_NotFound(t *testing.T) {
	dir := t.TempDir()
	result := FindChitinYaml(dir)
	if result != "" {
		t.Fatalf("expected empty string, got %s", result)
	}
}

func TestExtractRouterSection_Found(t *testing.T) {
	input := "version: \"1\"\nrouter:\n  enabled: true\n  heuristics:\n    blast_radius:\n      enabled: true\nother_key: value\n"
	section := extractRouterSection(input)
	if section == "" {
		t.Fatal("expected non-empty router section")
	}
	if !strings.Contains(section, "router:") {
		t.Fatal("expected section to contain 'router:'")
	}
	if !strings.Contains(section, "enabled: true") {
		t.Fatal("expected section to contain 'enabled: true'")
	}
}

func TestExtractRouterSection_NotFound(t *testing.T) {
	input := "version: \"1\"\nother_key: value\n"
	section := extractRouterSection(input)
	if section != "" {
		t.Fatalf("expected empty section when no router block, got %q", section)
	}
}

func TestParseRouterSection_Enabled(t *testing.T) {
	section := "router:\n  enabled: true\n  heuristics:\n    blast_radius:\n      enabled: true\n      threshold: 0.8\n    floundering:\n      enabled: false\n      max_loop_count: 5\n      max_stall_seconds: 120\n  advisor:\n    enabled: true\n    model: claude-sonnet-4-20250514\n    chain:\n      max_depth: 6\n      tier_steps: [t0, t2, t4]"
	policy := parseRouterSection(section)
	if !policy.Enabled {
		t.Fatal("expected Enabled=true")
	}
	if !policy.Heuristics["blast_radius"].Enabled {
		t.Fatal("expected blast_radius Enabled=true")
	}
	if policy.Heuristics["blast_radius"].Threshold != 0.8 {
		t.Fatalf("blast_radius Threshold=%v, want 0.8", policy.Heuristics["blast_radius"].Threshold)
	}
	if policy.Heuristics["floundering"].Enabled {
		t.Fatal("expected floundering Enabled=false")
	}
	if policy.Heuristics["floundering"].MaxLoopCount != 5 {
		t.Fatalf("floundering MaxLoopCount=%d, want 5", policy.Heuristics["floundering"].MaxLoopCount)
	}
	if policy.Heuristics["floundering"].MaxStallSeconds != 120 {
		t.Fatalf("floundering MaxStallSeconds=%d, want 120", policy.Heuristics["floundering"].MaxStallSeconds)
	}
	if !policy.Advisor.Enabled {
		t.Fatal("expected advisor Enabled=true")
	}
	if policy.Advisor.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("advisor Model=%q, want claude-sonnet-4-20250514", policy.Advisor.Model)
	}
	if policy.Advisor.Chain.MaxDepth != 6 {
		t.Fatalf("advisor chain MaxDepth=%d, want 6", policy.Advisor.Chain.MaxDepth)
	}
}

func TestParseRouterSection_DefaultsOnEmpty(t *testing.T) {
	section := "router:\n  enabled: false"
	policy := parseRouterSection(section)
	if policy.Enabled {
		t.Fatal("expected Enabled=false")
	}
	// Heuristics should come from DefaultPolicy
	if _, ok := policy.Heuristics["blast_radius"]; !ok {
		t.Fatal("expected blast_radius from defaults")
	}
}

func TestParseInlineList(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"t0, t2, t4", []string{"t0", "t2", "t4"}},
		{"t0,t2,t4", []string{"t0", "t2", "t4"}},
		{"  t0 , t2  ", []string{"t0", "t2"}},
		{`"t0", 't2'`, []string{"t0", "t2"}},
		{"", nil},
		{" , , ", nil},
	}
	for _, tt := range tests {
		got := parseInlineList(tt.input)
		if len(got) != len(tt.want) {
			t.Fatalf("parseInlineList(%q) = %v, want %v", tt.input, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseInlineList(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestFindFirstMatch(t *testing.T) {
	re := regexp.MustCompile(`^\s+enabled:\s+(true|false)`)
	lines := []string{"router:", "  enabled: true", "  other: x"}
	m := findFirstMatch(lines, re)
	if m == nil || m[1] != "true" {
		t.Fatalf("expected match 'true', got %v", m)
	}

	m = findFirstMatch([]string{"no match here"}, re)
	if m != nil {
		t.Fatalf("expected nil match, got %v", m)
	}
}

func TestLoadPolicy_FromFile(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte("version: \"1\"\nrouter:\n  enabled: true\n  heuristics:\n    blast_radius:\n      enabled: true\n      threshold: 0.75\n    floundering:\n      enabled: true\n      max_loop_count: 4\n      max_stall_seconds: 300\n  advisor:\n    enabled: false\n")
	if err := os.WriteFile(filepath.Join(dir, "chitin.yaml"), yaml, 0644); err != nil {
		t.Fatal(err)
	}
	policy := LoadPolicy(dir)
	if !policy.Enabled {
		t.Fatal("expected policy Enabled=true from file")
	}
	if policy.Heuristics["blast_radius"].Threshold != 0.75 {
		t.Fatalf("blast_radius Threshold=%v, want 0.75", policy.Heuristics["blast_radius"].Threshold)
	}
}

func TestLoadPolicy_NoFile(t *testing.T) {
	policy := LoadPolicy(t.TempDir())
	if policy.Enabled {
		t.Fatal("expected default policy (Enabled=false) when no chitin.yaml")
	}
}

func TestLoadPolicy_MalformedFile(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte("router: [")
	if err := os.WriteFile(filepath.Join(dir, "chitin.yaml"), yaml, 0644); err != nil {
		t.Fatal(err)
	}
	// Should gracefully fall back — extractRouterSection returns "" for
	// malformed YAML, so we get DefaultPolicy.
	policy := LoadPolicy(dir)
	if policy.Enabled {
		t.Fatal("expected default policy for malformed file")
	}
}

func TestLoadPolicy_NoRouterSection(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte("version: \"1\"\nsomething_else: true\n")
	if err := os.WriteFile(filepath.Join(dir, "chitin.yaml"), yaml, 0644); err != nil {
		t.Fatal(err)
	}
	policy := LoadPolicy(dir)
	if policy.Enabled {
		t.Fatal("expected default policy when no router section")
	}
}

func TestParsePluginsViaYAML(t *testing.T) {
	yaml := "router:\n  plugins:\n    - name: my-plugin\n      type: heuristic\n      runtime: python3\n      module: plugins/my_plugin.py\n      timeout_ms: 5000\n"
	plugins := parsePluginsViaYAML(yaml)
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Name != "my-plugin" {
		t.Fatalf("plugin name=%q, want my-plugin", plugins[0].Name)
	}
	if plugins[0].Runtime != "python3" {
		t.Fatalf("plugin runtime=%q, want python3", plugins[0].Runtime)
	}
	if plugins[0].TimeoutMs != 5000 {
		t.Fatalf("plugin timeout_ms=%d, want 5000", plugins[0].TimeoutMs)
	}
}

func TestParsePluginsViaYAML_Empty(t *testing.T) {
	plugins := parsePluginsViaYAML("version: 1\n")
	if plugins != nil {
		t.Fatalf("expected nil plugins for no plugins section, got %v", plugins)
	}
}

func TestParsePluginsViaYAML_InvalidYAML(t *testing.T) {
	plugins := parsePluginsViaYAML("router: [invalid")
	if plugins != nil {
		t.Fatalf("expected nil plugins for invalid YAML, got %v", plugins)
	}
}

func TestReadChainEvents_EmptySessionID(t *testing.T) {
	events := ReadChainEvents("")
	if events != nil {
		t.Fatalf("expected nil for empty session, got %v", events)
	}
}

func TestReadChainEvents_MissingFile(t *testing.T) {
	events := ReadChainEvents("nonexistent-session-id-xyz")
	if events != nil {
		t.Fatalf("expected nil for missing file, got %v", events)
	}
}