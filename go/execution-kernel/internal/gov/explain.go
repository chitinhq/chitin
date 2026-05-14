package gov

import (
	"fmt"
	"strings"
)

// MatchCheck reports one predicate involved in rule matching.
type MatchCheck struct {
	Name    string  `json:"name"`
	Matched bool    `json:"matched"`
	Detail  string  `json:"detail,omitempty"`
	Weight  float64 `json:"weight,omitempty"`
}

// MatchExplanation describes how one rule compared against an action.
type MatchExplanation struct {
	Matched bool         `json:"matched"`
	Checks  []MatchCheck `json:"checks"`
	Passed  int          `json:"passed"`
	Failed  int          `json:"failed"`
	Score   float64      `json:"score"`
}

// ExplainMatch evaluates the same predicates used by Rule matching and returns
// a human-readable breakdown of what matched and what did not.
func (r Rule) ExplainMatch(a Action, ctx FingerprintContext) MatchExplanation {
	checks := []MatchCheck{
		{
			Name:    "action",
			Matched: r.Action.Matches(a.Type),
			Detail:  fmt.Sprintf("rule allows %s; action was %s", joinOrAny([]string(r.Action)), a.Type),
			Weight:  2,
		},
	}

	appendIdentity := func(name string, matcher IdentityMatcher, actual string) {
		if len(matcher) == 0 {
			return
		}
		checks = append(checks, MatchCheck{
			Name:    name,
			Matched: matcher.Matches(actual),
			Detail:  fmt.Sprintf("wanted %s; got %q", joinOrAny([]string(matcher)), actual),
			Weight:  1,
		})
	}
	appendIdentity("agent_instance_id", r.AgentInstanceID, ctx.AgentInstanceID)
	appendIdentity("agent_fingerprint", r.AgentFingerprint, firstNonEmpty(ctx.AgentFingerprint, ctx.Fingerprint))
	appendIdentity("driver", r.Driver, ctx.Driver)
	appendIdentity("model", r.Model, ctx.Model)
	appendIdentity("role", r.Role, ctx.Role)
	appendIdentity("station_prompt_hash", r.StationPromptHash, ctx.StationPromptHash)
	appendIdentity("skills_tools_hash", r.SkillsToolsHash, ctx.SkillsToolsHash)
	appendIdentity("soul_lens", r.SoulLens, ctx.SoulLens)
	appendIdentity("authority", r.Authority, ctx.Authority)
	appendIdentity("workflow_id", r.WorkflowID, ctx.WorkflowID)

	for key, want := range r.Params {
		got, ok := a.Params[key]
		matched := ok && fmt.Sprint(got) == want
		detail := fmt.Sprintf("wanted %s=%q; key was absent", key, want)
		if ok {
			detail = fmt.Sprintf("wanted %s=%q; got %q", key, want, fmt.Sprint(got))
		}
		checks = append(checks, MatchCheck{
			Name:    "param:" + key,
			Matched: matched,
			Detail:  detail,
			Weight:  1,
		})
	}

	if len(r.Branches) > 0 {
		detail := fmt.Sprintf("wanted branch in [%s]; got %q", strings.Join(r.Branches, ", "), a.Target)
		if a.Type == ActGitCommit && branchesContain(r.Branches, "<HEAD-implicit>") {
			detail = fmt.Sprintf("wanted branch in [%s]; current branch resolved from %q", strings.Join(r.Branches, ", "), a.Path)
		}
		checks = append(checks, MatchCheck{
			Name:    "branches",
			Matched: r.branchMatches(a),
			Detail:  detail,
			Weight:  1,
		})
	}

	if len(r.PathUnder) > 0 {
		checks = append(checks, MatchCheck{
			Name:    "path_under",
			Matched: pathUnderMatches(r.PathUnder, a.Target),
			Detail:  fmt.Sprintf("wanted target under [%s]; got %q", strings.Join(r.PathUnder, ", "), a.Target),
			Weight:  1,
		})
	}

	if r.Target != "" {
		checks = append(checks, MatchCheck{
			Name:    "target",
			Matched: strings.Contains(a.Target, r.Target),
			Detail:  fmt.Sprintf("wanted target containing %q; got %q", r.Target, a.Target),
			Weight:  1,
		})
	}

	if r.TargetRegex != "" {
		matched := false
		detail := fmt.Sprintf("wanted target matching /%s/; got %q", r.TargetRegex, a.Target)
		if r.compiledRegex != nil {
			matched = r.compiledRegex.MatchString(a.Target)
		} else {
			detail = fmt.Sprintf("target_regex %q was not compiled", r.TargetRegex)
		}
		checks = append(checks, MatchCheck{
			Name:    "target_regex",
			Matched: matched,
			Detail:  detail,
			Weight:  1,
		})
	}

	out := MatchExplanation{Checks: checks}
	var matchedWeight float64
	var totalWeight float64
	for _, check := range checks {
		weight := check.Weight
		if weight <= 0 {
			weight = 1
		}
		totalWeight += weight
		if check.Matched {
			out.Passed++
			matchedWeight += weight
			continue
		}
		out.Failed++
	}
	out.Matched = out.Failed == 0
	if totalWeight > 0 {
		out.Score = matchedWeight / totalWeight
	}
	return out
}

func joinOrAny(values []string) string {
	if len(values) == 0 {
		return "any"
	}
	return strings.Join(values, ", ")
}

func pathUnderMatches(prefixes []string, target string) bool {
	for _, p := range prefixes {
		if len(target) >= len(p) && target[:len(p)] == p {
			return true
		}
	}
	return false
}
