package speclint

import (
	"fmt"
	"regexp"
	"strings"
)

// Classification labels a Copilot review comment for spec-PR iteration
// routing per spec 115 FR-007 / FR-008.
//
// Invariant: ClassifyDesignJudgement returns DesignJudgement iff at
// least one regex in the supplied JudgementPhrases matches the comment
// body (case-insensitive). Every other comment classifies as Mechanical.
type Classification string

const (
	// Mechanical comments are addressable by a driver — typo fixes,
	// missing FR cross-references, taxonomy violations, etc.
	Mechanical Classification = "mechanical"

	// DesignJudgement comments require operator attention — scope
	// debates, priority calls, restructuring suggestions. The workflow
	// emits spec_iteration_escalated { reason: "design_judgement_required" }
	// for these (FR-008) rather than dispatching a driver.
	DesignJudgement Classification = "design_judgement"
)

// JudgementPhrases is the compiled regex set loaded from
// .specify/judgement-phrases.txt. Constructed once per workflow run via
// ParseJudgementPhrases; passed by reference to ClassifyDesignJudgement
// for each comment in a Copilot review.
//
// Opaque on purpose — callers should not depend on the internal slice
// shape so the parser can change (e.g., add anchored vs unanchored
// regex modes) without breaking T014's wiring.
type JudgementPhrases struct {
	patterns []*regexp.Regexp
}

// ParseJudgementPhrases compiles the contents of
// .specify/judgement-phrases.txt into a regex set.
//
// Format: one regex per line. Blank lines and `#` comments are ignored.
// Each line is compiled with the case-insensitive flag (?i) prepended,
// matching the spec 115 T019 seed file's intent ("consider" should
// match "Consider this" the same as "consider this").
//
// Returns an error on the first malformed regex, naming the offending
// line. The file is operator-editable per FR-007; surfacing parse
// errors loudly is safer than silently dropping a phrase the operator
// thought they had added — a missed DesignJudgement classification
// could send a scope debate to a driver that then commits opinionated
// spec changes.
func ParseJudgementPhrases(content string) (*JudgementPhrases, error) {
	var out []*regexp.Regexp
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		re, err := regexp.Compile("(?i)" + line)
		if err != nil {
			return nil, fmt.Errorf(".specify/judgement-phrases.txt line %d: invalid regex %q: %w", i+1, line, err)
		}
		out = append(out, re)
	}
	return &JudgementPhrases{patterns: out}, nil
}

// Count returns the number of compiled phrases. Provided so tests can
// assert parser behavior (blank/comment line handling) without touching
// the unexported slice — keeping JudgementPhrases opaque per its doc.
func (p *JudgementPhrases) Count() int {
	if p == nil {
		return 0
	}
	return len(p.patterns)
}

// ClassifyDesignJudgement returns DesignJudgement when comment matches
// any compiled phrase in phrases; Mechanical otherwise.
//
// Pure function — no IO. A nil or empty phrase set yields Mechanical
// for every comment, which is the safe-for-driver-dispatch default
// (mechanical fixes are cheap to revert; an escalation that should
// have been a fix wastes operator attention but loses no data).
//
// Boundary contracts:
//   - phrases == nil               -> Mechanical
//   - len(phrases.patterns) == 0   -> Mechanical
//   - comment == ""                -> Mechanical (guarded explicitly;
//     an operator-added regex like `.*` or `^$` could otherwise match
//     an empty body and silently escalate)
//   - multiple patterns match      -> DesignJudgement (single-bit
//     result; short-circuit on first hit)
func ClassifyDesignJudgement(comment string, phrases *JudgementPhrases) Classification {
	if comment == "" {
		return Mechanical
	}
	if phrases == nil || len(phrases.patterns) == 0 {
		return Mechanical
	}
	for _, p := range phrases.patterns {
		if p.MatchString(comment) {
			return DesignJudgement
		}
	}
	return Mechanical
}
