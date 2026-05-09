package router

// chitin-routes.yaml — advisory routing policy loader.
//
// This sidecar maps router signals to candidate downstream handlers
// (driver/model labels). It is intentionally pure data: no subprocess,
// no peer spawn, no LLM consultation. The kernel may stamp a
// RoutingDecision string derived from this policy; Hermes, OpenClaw,
// or another chain consumer decides whether to act on that signal.
//
// Sidecar (not part of chitin.yaml) so routing candidate lists can grow
// without bloating the main policy file. Loaded from the same parent
// walk that finds chitin.yaml.

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RoutesPolicy is the full chitin-routes.yaml shape.
type RoutesPolicy struct {
	// Schema version — bumped when the file format changes
	// incompatibly. Loader rejects unknown versions.
	Version int `yaml:"version"`

	// When false, rules + routes are loaded + validated but the
	// gate does not stamp routing candidates. Default off — operator
	// opts in.
	Enabled bool `yaml:"enabled"`

	// Historical no-op retained for schema compatibility with early
	// route files. The kernel no longer spawns peers from this policy.
	SpawnTimeoutSeconds int `yaml:"spawn_timeout_seconds"`

	// Historical no-op retained for schema compatibility with early
	// route files. The kernel no longer spawns peers from this policy.
	MaxConcurrentPeers int `yaml:"max_concurrent_peers"`

	// Rules: signal+severity → which named route to use.
	Rules []RoutingRule `yaml:"rules"`

	// Routes: named optimization category → ordered candidate list.
	// routeFor() walks the candidates in order, picking the first
	// one that fits remaining quota.
	Routes map[string][]Candidate `yaml:"routes"`
}

// RoutingRule is one row in the rules table — when this signal at
// this severity fires, look up `route` in Routes for candidates.
type RoutingRule struct {
	// Operator-friendly id (used in telemetry + error messages).
	Name string `yaml:"name"`

		// Which router signal triggers this rule.
		// "floundering" | "blast_radius" | "drift".
	Signal string `yaml:"signal"`

	// Human-readable severity expression. Today: free-text shown in
	// telemetry. Future: parsed into a comparator. Until parsed,
	// the kernel matches purely on Signal.
	Severity string `yaml:"severity"`

	// Name of the route in RoutesPolicy.Routes to consult.
	Route string `yaml:"route"`

	// Advisory rate cap retained for future chain consumers. The
	// kernel's current loader validates but does not enforce it.
	MaxPerHour int `yaml:"max_per_hour"`
}

// Candidate is one (driver, model) pair in a route's candidate list.
type Candidate struct {
	Driver string `yaml:"driver"`
	Model  string `yaml:"model"`
}

// DefaultRoutesPolicy returns the disabled-by-default policy used
// when chitin-routes.yaml is absent or unreadable. Mirrors the
// "operator opts in" stance of the rest of the router config.
func DefaultRoutesPolicy() RoutesPolicy {
	return RoutesPolicy{
		Version:             1,
		Enabled:             false,
		SpawnTimeoutSeconds: 60,
		MaxConcurrentPeers:  1,
		Rules:               nil,
		Routes:              map[string][]Candidate{},
	}
}

// FindChitinRoutesYaml walks up from startCwd looking for
// chitin-routes.yaml. Returns absolute path or "" if not found.
func FindChitinRoutesYaml(startCwd string) string {
	dir, err := filepath.Abs(startCwd)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, "chitin-routes.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// LoadRoutesPolicy reads + parses chitin-routes.yaml from a starting
// cwd. Returns DefaultRoutesPolicy + non-nil err on any failure
// (missing file, parse error, validation error). Caller decides
// whether to fail closed or fall back to default — for the gate we
// fall back so a missing config doesn't brick the kernel.
func LoadRoutesPolicy(cwd string) (RoutesPolicy, error) {
	path := FindChitinRoutesYaml(cwd)
	if path == "" {
		return DefaultRoutesPolicy(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultRoutesPolicy(), fmt.Errorf("read chitin-routes.yaml: %w", err)
	}
	var p RoutesPolicy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return DefaultRoutesPolicy(), fmt.Errorf("parse chitin-routes.yaml: %w", err)
	}
	if err := ValidateRoutesPolicy(p); err != nil {
		return DefaultRoutesPolicy(), fmt.Errorf("validate chitin-routes.yaml: %w", err)
	}
	// Apply defaults to zero-value fields
	if p.SpawnTimeoutSeconds == 0 {
		p.SpawnTimeoutSeconds = 60
	}
	if p.MaxConcurrentPeers == 0 {
		p.MaxConcurrentPeers = 1
	}
	if p.Routes == nil {
		p.Routes = map[string][]Candidate{}
	}
	return p, nil
}

// ValidateRoutesPolicy enforces structural invariants the loader
// can't express via tags. Returns the FIRST violation found —
// caller fixes one at a time. The kernel falls back to
// DefaultRoutesPolicy on any violation, so one bad rule doesn't
// brick the gate.
func ValidateRoutesPolicy(p RoutesPolicy) error {
	if p.Version != 0 && p.Version != 1 {
		return fmt.Errorf("unknown schema version %d (expected 1)", p.Version)
	}
	allowedSignals := map[string]bool{
		"floundering": true, "blast_radius": true,
		"drift": true,
	}
	seenRule := map[string]bool{}
	for i, rule := range p.Rules {
		if rule.Name == "" {
			return fmt.Errorf("rule[%d]: name required", i)
		}
		if seenRule[rule.Name] {
			return fmt.Errorf("rule[%d]: duplicate name %q", i, rule.Name)
		}
		seenRule[rule.Name] = true
		if !allowedSignals[rule.Signal] {
			return fmt.Errorf("rule[%s]: unknown signal %q (expected one of floundering, blast_radius, drift)",
				rule.Name, rule.Signal)
		}
		if rule.Route == "" {
			return fmt.Errorf("rule[%s]: route required", rule.Name)
		}
		if _, ok := p.Routes[rule.Route]; !ok {
			return fmt.Errorf("rule[%s]: route %q not defined in routes", rule.Name, rule.Route)
		}
		if rule.MaxPerHour < 0 {
			return fmt.Errorf("rule[%s]: max_per_hour cannot be negative", rule.Name)
		}
	}
	for routeName, candidates := range p.Routes {
		if routeName == "" {
			return fmt.Errorf("route key cannot be empty")
		}
		if len(candidates) == 0 {
			return fmt.Errorf("route[%s]: at least one candidate required", routeName)
		}
		for j, c := range candidates {
			if c.Driver == "" {
				return fmt.Errorf("route[%s][%d]: driver required", routeName, j)
			}
			if c.Model == "" {
				return fmt.Errorf("route[%s][%d]: model required", routeName, j)
			}
		}
	}
	return nil
}
