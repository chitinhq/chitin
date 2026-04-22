package gov

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Policy is the merged rule set evaluated on every gate call.
// Loaded from YAML; LoadWithInheritance merges parent chitin.yaml
// files into a single Policy before evaluation.
type Policy struct {
	ID             string            `yaml:"id"`
	Name           string            `yaml:"name,omitempty"`
	Mode           string            `yaml:"mode,omitempty"` // monitor | enforce | guide; default guide
	Pack           string            `yaml:"pack,omitempty"`
	InvariantModes map[string]string `yaml:"invariantModes,omitempty"` // ruleID → mode
	Bounds         Bounds            `yaml:"bounds,omitempty"`
	Escalation     EscalationConfig  `yaml:"escalation,omitempty"`
	Rules          []Rule            `yaml:"rules"`
}

// Rule is one entry in the policy. Evaluated top-to-bottom; first match wins.
type Rule struct {
	ID               string        `yaml:"id"`
	Action           ActionMatcher `yaml:"action"` // single type OR list of types
	Effect           string        `yaml:"effect"` // deny | allow
	Target           string        `yaml:"target,omitempty"`       // substring match on Action.Target
	TargetRegex      string        `yaml:"target_regex,omitempty"` // regex match on Action.Target
	Branches         []string      `yaml:"branches,omitempty"`     // for git.push — match if Action.Target ∈ list
	PathUnder        []string      `yaml:"path_under,omitempty"`   // for file.* — match if Action.Target begins with any
	Reason           string        `yaml:"reason,omitempty"`
	Suggestion       string        `yaml:"suggestion,omitempty"`
	CorrectedCommand string        `yaml:"correctedCommand,omitempty"`
	EscalationWeight int           `yaml:"escalation_weight,omitempty"` // default 1
}

// Bounds are the blast-radius ceilings checked for push-shaped actions.
type Bounds struct {
	MaxFilesChanged   int `yaml:"max_files_changed"`
	MaxLinesChanged   int `yaml:"max_lines_changed"`
	MaxRuntimeSeconds int `yaml:"max_runtime_seconds"`
}

// EscalationConfig overrides the default escalation thresholds.
type EscalationConfig struct {
	ElevatedThreshold  int `yaml:"elevated_threshold"`  // default 3
	HighThreshold      int `yaml:"high_threshold"`      // default 7
	LockdownThreshold  int `yaml:"lockdown_threshold"`  // default 10
	MaxRetriesPerFp    int `yaml:"max_retries_per_action"` // default 3
}

// Decision is the result of evaluating an Action against a Policy.
type Decision struct {
	Allowed          bool   `json:"allowed"`
	Mode             string `json:"mode"` // monitor | enforce | guide
	RuleID           string `json:"rule_id"`
	Reason           string `json:"reason,omitempty"`
	Suggestion       string `json:"suggestion,omitempty"`
	CorrectedCommand string `json:"corrected_command,omitempty"`
	Escalation       string `json:"escalation,omitempty"` // normal | elevated | high | lockdown
	Action           Action `json:"-"`
	Ts               string `json:"ts"`
}

// ActionMatcher is a yaml.Unmarshaler that accepts either a single
// action type string or a list of strings.
type ActionMatcher []string

func (a *ActionMatcher) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*a = []string{node.Value}
		return nil
	}
	if node.Kind == yaml.SequenceNode {
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		*a = list
		return nil
	}
	return fmt.Errorf("action must be string or list of strings, got %v", node.Kind)
}

// Matches returns true if the given ActionType appears in the matcher.
func (a ActionMatcher) Matches(t ActionType) bool {
	s := string(t)
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}

// LoadPolicyFile reads and parses a single chitin.yaml.
func LoadPolicyFile(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("read policy: %w", err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Policy{}, fmt.Errorf("parse policy %s: %w", path, err)
	}
	p.ApplyDefaults()
	return p, nil
}

// ApplyDefaults fills in unset fields with their baseline values.
func (p *Policy) ApplyDefaults() {
	if p.Mode == "" {
		p.Mode = "guide"
	}
	if p.Escalation.ElevatedThreshold == 0 {
		p.Escalation.ElevatedThreshold = 3
	}
	if p.Escalation.HighThreshold == 0 {
		p.Escalation.HighThreshold = 7
	}
	if p.Escalation.LockdownThreshold == 0 {
		p.Escalation.LockdownThreshold = 10
	}
	if p.Escalation.MaxRetriesPerFp == 0 {
		p.Escalation.MaxRetriesPerFp = 3
	}
	for i := range p.Rules {
		if p.Rules[i].EscalationWeight == 0 {
			p.Rules[i].EscalationWeight = 1
		}
	}
}

// Evaluate walks the rule list top-to-bottom. First deny match wins;
// otherwise first allow match; otherwise default deny (fail-closed).
func (p Policy) Evaluate(a Action) Decision {
	for _, r := range p.Rules {
		if r.matches(a) {
			mode := p.Mode
			if m, ok := p.InvariantModes[r.ID]; ok {
				mode = m
			}
			return Decision{
				Allowed:          r.Effect == "allow",
				Mode:             mode,
				RuleID:           r.ID,
				Reason:           r.Reason,
				Suggestion:       r.Suggestion,
				CorrectedCommand: r.CorrectedCommand,
				Action:           a,
			}
		}
	}
	// Fail-closed default
	return Decision{
		Allowed: false,
		Mode:    p.Mode,
		RuleID:  "default-deny",
		Reason:  "no matching allow rule; policy default is deny",
		Action:  a,
	}
}

func (r Rule) matches(a Action) bool {
	if !r.Action.Matches(a.Type) {
		return false
	}
	// Branch condition: Action.Target must be in the list
	if len(r.Branches) > 0 {
		inList := false
		for _, b := range r.Branches {
			if a.Target == b {
				inList = true
				break
			}
		}
		if !inList {
			return false
		}
	}
	// PathUnder: Action.Target must begin with one of the prefixes
	if len(r.PathUnder) > 0 {
		under := false
		for _, p := range r.PathUnder {
			if len(a.Target) >= len(p) && a.Target[:len(p)] == p {
				under = true
				break
			}
		}
		if !under {
			return false
		}
	}
	// Target substring
	if r.Target != "" {
		if !containsFold(a.Target, r.Target) {
			return false
		}
	}
	// TargetRegex
	if r.TargetRegex != "" {
		re, err := regexp.Compile(r.TargetRegex)
		if err != nil {
			return false
		}
		if !re.MatchString(a.Target) {
			return false
		}
	}
	return true
}

func containsFold(haystack, needle string) bool {
	return regexp.MustCompile(regexp.QuoteMeta(needle)).MatchString(haystack)
}
