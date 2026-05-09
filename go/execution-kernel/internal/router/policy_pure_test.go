package router

import (
	"regexp"
	"testing"
)

func TestExtractRouterSection(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			"present",
			"router:\n  enabled: true\nother:\n  key: val",
			"router:\n  enabled: true",
		},
		{
			"absent",
			"other:\n  key: val\n",
			"",
		},
		{
			"router with subsections",
			"router:\n  enabled: true\n  heuristics:\n    drift:\n      enabled: true\nother: x",
			"router:\n  enabled: true\n  heuristics:\n    drift:\n      enabled: true",
		},
		{
			"empty router section",
			"router:\nother: x",
			"router:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRouterSection(tt.yaml)
			if got != tt.want {
				t.Errorf("extractRouterSection(%q) = %q, want %q", tt.yaml, got, tt.want)
			}
		})
	}
}

func TestParseRouterSection(t *testing.T) {
	section := `router:
  enabled: false
  heuristics:
    drift:
      enabled: true
      threshold: 0.75
      max_stall_seconds: 120
    floundering:
      enabled: true
      max_loop_count: 5
  advisor:
    enabled: true
    model: "claude-sonnet-4-20250514"
    when: [drift, floundering]
    chain:
      max_depth: 10
      tier_steps: [T0, T1, T2]`

	policy := parseRouterSection(section)

	if policy.Enabled {
		t.Error("Enabled should be false")
	}
	drift, ok := policy.Heuristics["drift"]
	if !ok {
		t.Fatal("drift heuristic not found")
	}
	if !drift.Enabled {
		t.Error("drift.Enabled should be true")
	}
	if drift.Threshold != 0.75 {
		t.Errorf("drift.Threshold = %v, want 0.75", drift.Threshold)
	}
	if drift.MaxStallSeconds != 120 {
		t.Errorf("drift.MaxStallSeconds = %d, want 120", drift.MaxStallSeconds)
	}
	fl, ok := policy.Heuristics["floundering"]
	if !ok {
		t.Fatal("floundering heuristic not found")
	}
	if !fl.Enabled {
		t.Error("floundering.Enabled should be true")
	}
	if fl.MaxLoopCount != 5 {
		t.Errorf("floundering.MaxLoopCount = %d, want 5", fl.MaxLoopCount)
	}
	if !policy.Advisor.Enabled {
		t.Error("Advisor.Enabled should be true")
	}
	if policy.Advisor.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Advisor.Model = %q, want %q", policy.Advisor.Model, "claude-sonnet-4-20250514")
	}
	if policy.Advisor.Chain.MaxDepth != 10 {
		t.Errorf("Advisor.Chain.MaxDepth = %d, want 10", policy.Advisor.Chain.MaxDepth)
	}
}

func TestParseInlineList(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"drift, floundering", []string{"drift", "floundering"}},
		{"T0, T1, T2", []string{"T0", "T1", "T2"}},
		{"single", []string{"single"}},
		{"", nil},
		{"  a , b  , c  ", []string{"a", "b", "c"}},
		{`"a", 'b'`, []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseInlineList(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseInlineList(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseInlineList(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFindFirstMatch(t *testing.T) {
	re := regexp.MustCompile(`^\s+enabled:\s+(true|false)`)
	lines := []string{
		"router:",
		"  enabled: true",
		"  heuristics:",
	}
	m := findFirstMatch(lines, re)
	if m == nil || m[1] != "true" {
		t.Errorf("findFirstMatch = %v, want [matched, true]", m)
	}

	// No match
	m = findFirstMatch([]string{"other:", "  key: val"}, re)
	if m != nil {
		t.Errorf("findFirstMatch should return nil for no match, got %v", m)
	}
}

func TestParsePluginsViaYAML(t *testing.T) {
	yamlBody := `router:
  plugins:
    - name: my-plugin
      module: /usr/local/bin/my-plugin
      enabled: true
`
	plugins := parsePluginsViaYAML(yamlBody)
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Name != "my-plugin" {
		t.Errorf("plugin name = %q, want %q", plugins[0].Name, "my-plugin")
	}
	if plugins[0].Module != "/usr/local/bin/my-plugin" {
		t.Errorf("plugin path = %q, want %q", plugins[0].Module, "/usr/local/bin/my-plugin")
	}
}

func TestParsePluginsViaYAML_Empty(t *testing.T) {
	plugins := parsePluginsViaYAML("other: stuff\n")
	if plugins != nil {
		t.Errorf("expected nil plugins for YAML without router.plugins, got %v", plugins)
	}
}

func TestParsePluginsViaYAML_Invalid(t *testing.T) {
	plugins := parsePluginsViaYAML("::not yaml::\n")
	if plugins != nil {
		t.Errorf("expected nil plugins for invalid YAML, got %v", plugins)
	}
}