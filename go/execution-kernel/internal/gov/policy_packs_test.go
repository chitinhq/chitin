package gov

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyPackExamples_LoadAndEvaluate(t *testing.T) {
	packPaths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "examples", "policy-packs", "*", "chitin.yaml"))
	if err != nil {
		t.Fatalf("glob policy packs: %v", err)
	}
	if len(packPaths) == 0 {
		t.Fatal("expected curated policy packs")
	}

	for _, packPath := range packPaths {
		packPath := packPath
		t.Run(filepath.Base(filepath.Dir(packPath)), func(t *testing.T) {
			policy, err := LoadPolicyFile(packPath)
			if err != nil {
				t.Fatalf("LoadPolicyFile(%s): %v", packPath, err)
			}
			if len(policy.Rules) == 0 {
				t.Fatal("policy pack must contain rules")
			}
			for _, rule := range policy.Rules {
				switch rule.Effect {
				case "allow", "deny":
				default:
					t.Fatalf("rule %s uses unsupported effect %q", rule.ID, rule.Effect)
				}
			}

			read := policy.Evaluate(Action{Type: ActFileRead, Target: "README.md"})
			if !read.Allowed {
				t.Fatalf("file.read should be allowed by curated pack, got rule=%q reason=%q", read.RuleID, read.Reason)
			}

			rm := policy.Evaluate(Action{Type: ActFileRecursiveDelete, Target: "tmp/build"})
			if rm.Allowed {
				t.Fatalf("recursive delete should be denied by curated pack, got rule=%q", rm.RuleID)
			}
		})
	}
}

func TestPolicyPackExamples_Boundaries(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		policy, err := parsePolicyYAML([]byte("id: empty\nmode: enforce\nrules: []\n"))
		if err != nil {
			t.Fatalf("empty rules policy should load: %v", err)
		}
		decision := policy.Evaluate(Action{Type: ActFileRead, Target: "README.md"})
		if decision.Allowed || decision.RuleID != "default-deny" {
			t.Fatalf("empty rules must fail closed, got allowed=%v rule=%q", decision.Allowed, decision.RuleID)
		}
	})

	t.Run("max", func(t *testing.T) {
		packPaths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "examples", "policy-packs", "*", "chitin.yaml"))
		if err != nil {
			t.Fatalf("glob policy packs: %v", err)
		}
		for _, packPath := range packPaths {
			data, err := os.ReadFile(packPath)
			if err != nil {
				t.Fatalf("read %s: %v", packPath, err)
			}
			if strings.Contains(string(data), "max_runtime_seconds") {
				t.Fatalf("%s uses unsupported bounds.max_runtime_seconds", packPath)
			}
		}
	})

	t.Run("error", func(t *testing.T) {
		cases := []struct {
			name    string
			yaml    string
			wantErr string
		}{
			{
				name: "unsupported bounds key",
				yaml: `
id: bad-bounds
mode: enforce
bounds:
  max_files_changed: 1
  max_lines_changed: 1
  max_runtime_seconds: 1
rules: []
`,
				wantErr: "max_runtime_seconds",
			},
			{
				name: "unsupported guide effect",
				yaml: `
id: bad-effect
mode: enforce
rules:
  - id: guide-rule
    action: file.write
    effect: guide
`,
				wantErr: "invariantModes",
			},
		}
		for _, tc := range cases {
			_, err := parsePolicyYAML([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("%s: expected validation error", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("%s: error %q should contain %q", tc.name, err, tc.wantErr)
			}
		}
	})
}
