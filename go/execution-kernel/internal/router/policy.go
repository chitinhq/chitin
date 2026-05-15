package router

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FindChitinYaml walks up from startCwd looking for chitin.yaml.
// Returns the absolute path or "" if not found.
func FindChitinYaml(startCwd string) string {
	dir, err := filepath.Abs(startCwd)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, "chitin.yaml")
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

// extractRouterSection finds the `router:` block in chitin.yaml
// text and returns its full body (including the heading line).
// Returns "" if not present.
func extractRouterSection(yaml string) string {
	lines := strings.Split(yaml, "\n")
	startIdx := -1
	headerRe := regexp.MustCompile(`^router:\s*$`)
	for i, l := range lines {
		if headerRe.MatchString(l) {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		return ""
	}
	out := []string{lines[startIdx]}
	for i := startIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			out = append(out, line)
			continue
		}
		// End of section: a non-indented, non-comment line at column 0
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "#") {
			break
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// parseRouterSection parses the router section into a Policy.
// MVP YAML parser — handles the specific keys we declare in
// DefaultPolicy. Unknown keys ignored. Type errors fall back to
// defaults so a malformed config doesn't brick the router.
func parseRouterSection(routerYaml string) Policy {
	policy := DefaultPolicy()
	lines := strings.Split(routerYaml, "\n")

	enabledRe := regexp.MustCompile(`^\s+enabled:\s+(true|false)`)
	if m := findFirstMatch(lines, enabledRe); m != nil {
		policy.Enabled = m[1] == "true"
	}

	section := ""
	subsection := ""
	for _, rawLine := range lines {
		// Section header at 2-space indent + word
		if matched, _ := regexp.MatchString(`^\s{2}\w`, rawLine); matched {
			section = strings.TrimSuffix(strings.TrimSpace(rawLine), ":")
			subsection = ""
			continue
		}
		// Subsection header at 4-space indent + word + colon-EOL
		if matched, _ := regexp.MatchString(`^\s{4}\w.*:\s*$`, rawLine); matched {
			subsection = strings.TrimSuffix(strings.TrimSpace(rawLine), ":")
			continue
		}
		// key: value at any indent
		kvRe := regexp.MustCompile(`^\s+([a-z_]+):\s+(.+)$`)
		m := kvRe.FindStringSubmatch(rawLine)
		if m == nil {
			continue
		}
		key := m[1]
		value := strings.TrimSpace(m[2])

		if section == "heuristics" && subsection != "" {
			h := policy.Heuristics[subsection]
			switch key {
			case "enabled":
				h.Enabled = value == "true"
			case "threshold":
				if f, err := strconv.ParseFloat(value, 64); err == nil {
					h.Threshold = f
				}
			case "max_loop_count":
				if n, err := strconv.Atoi(value); err == nil {
					h.MaxLoopCount = n
				}
			case "max_stall_seconds":
				if n, err := strconv.Atoi(value); err == nil {
					h.MaxStallSeconds = n
				}
			case "warn_threshold":
				if f, err := strconv.ParseFloat(value, 64); err == nil {
					h.WarnThreshold = f
				}
			case "halt_threshold":
				if f, err := strconv.ParseFloat(value, 64); err == nil {
					h.HaltThreshold = f
				}
			case "max_turns":
				if n, err := strconv.Atoi(value); err == nil {
					h.MaxTurns = n
				}
			}
			policy.Heuristics[subsection] = h
		}
		// section == "advisor" is silently ignored. The in-gate advisor
		// was culled 2026-05-08; old chitin.yaml files may still have
		// the block, but we no longer parse it. See
		// docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md.
	}

	return policy
}

func findFirstMatch(lines []string, re *regexp.Regexp) []string {
	for _, l := range lines {
		if m := re.FindStringSubmatch(l); m != nil {
			return m
		}
	}
	return nil
}

// LoadPolicy loads chitin.yaml's router section from a starting cwd.
// Walks up parents to find chitin.yaml; falls back to DefaultPolicy
// if missing or unreadable.
func LoadPolicy(cwd string) Policy {
	path := FindChitinYaml(cwd)
	if path == "" {
		return DefaultPolicy()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultPolicy()
	}
	section := extractRouterSection(string(data))
	if section == "" {
		return DefaultPolicy()
	}
	policy := parseRouterSection(section)
	// Plugins use a real YAML parse since they're a list-of-maps
	// shape the hand-rolled parser doesn't handle. Failures here
	// just leave Plugins empty (graceful no-op — the rest of the
	// hand-rolled parse already populated heuristics).
	policy.Plugins = parsePluginsViaYAML(string(data))
	return policy
}

// parsePluginsViaYAML extracts router.plugins from chitin.yaml using
// a real YAML parser. The hand-rolled parseRouterSection above
// handles flat key:value but not list-of-maps. For tonight's
// MVP, parse the whole YAML and pluck the plugins slice.
func parsePluginsViaYAML(yamlBody string) []PluginConfig {
	var top struct {
		Router struct {
			Plugins []PluginConfig `yaml:"plugins"`
		} `yaml:"router"`
	}
	if err := yaml.Unmarshal([]byte(yamlBody), &top); err != nil {
		return nil
	}
	return top.Router.Plugins
}
