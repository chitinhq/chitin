// Package speclint hosts the deterministic spec-PR linter (spec 115).
// Each rule (L01-L07) is a pure function over the spec.md + tasks.md
// pair; the design-judgement classifier (FR-007) lives alongside the
// rule set because it shares the same "regex over text" shape and gets
// composed into the spec-iteration workflow the same way.
package speclint

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Class is the closed two-value taxonomy ClassifyDesignJudgement returns
// for each Copilot review comment (spec 115 FR-007 / FR-008). Mechanical
// comments iterate through the spec-iteration driver loop; DesignJudgement
// comments escalate to the operator without dispatching a driver round.
type Class string

const (
	// Mechanical — no judgement phrase matched. The comment can be
	// addressed by a driver round (e.g., "this CLI subcommand doesn't
	// exist", "this cross-ref is broken").
	Mechanical Class = "mechanical"
	// DesignJudgement — at least one judgement phrase matched. The
	// comment requires the operator (e.g., "is US3 really P2?",
	// "should this be split"). Escalation reason is
	// `design_judgement_required` (FR-010).
	DesignJudgement Class = "design_judgement"
)

// JudgementPhrasesPath is the canonical operator-editable file relative
// to the repo root. The factory loads it once per workflow start; an
// edit on disk takes effect on the next workflow.
const JudgementPhrasesPath = ".specify/judgement-phrases.txt"

// ClassifyDesignJudgement returns DesignJudgement iff body matches any
// of the supplied regex phrases, else Mechanical. The function is pure —
// no I/O, no globals, no allocation beyond what regexp.MatchString does
// internally. False positives are preferred over false negatives per
// spec 115 SC-003: an escalated mechanical comment costs the operator
// one read; an un-escalated judgement comment burns a driver round and
// still ends up needing the operator.
func ClassifyDesignJudgement(body string, phrases []*regexp.Regexp) Class {
	for _, p := range phrases {
		if p.MatchString(body) {
			return DesignJudgement
		}
	}
	return Mechanical
}

// DefaultJudgementPhrases returns the FR-007 baseline regex set used
// when `.specify/judgement-phrases.txt` is absent or empty. Mirrors the
// initial allowlist T019 commits so behaviour matches whether the file
// is present or not. Each pattern is case-insensitive via the `(?i)`
// inline flag so Copilot's natural-English casing variations all hit.
func DefaultJudgementPhrases() []*regexp.Regexp {
	patterns := []string{
		`(?i)\bconsider\b`,
		`(?i)\bmight want\b`,
		`(?i)\bis this really\b`,
		`(?i)\bcould be\b`,
		`(?i)\bshould this be split\b`,
		`(?i)\bshould this be merged\b`,
		`(?i)\bP[123]\b`,
		`(?i)\bin scope\b`,
		`(?i)\bout of scope\b`,
	}
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		// MustCompile is safe: these are constant patterns under test.
		out = append(out, regexp.MustCompile(p))
	}
	return out
}

// LoadJudgementPhrases reads the operator-editable phrase file. Each
// non-blank non-comment line is one regex (compiled as-is — callers
// add `(?i)` if they want case-insensitive). Lines beginning with `#`
// are comments; trailing/leading whitespace is trimmed.
//
// If path does not exist, returns the DefaultJudgementPhrases set and
// nil error — the file is optional, and the defaults match T019.
// A compile error on any line is returned with the line number so the
// operator can find the bad pattern.
func LoadJudgementPhrases(path string) ([]*regexp.Regexp, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultJudgementPhrases(), nil
		}
		return nil, fmt.Errorf("speclint: open %s: %w", path, err)
	}
	defer f.Close()

	var out []*regexp.Regexp
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		re, err := regexp.Compile(line)
		if err != nil {
			return nil, fmt.Errorf("speclint: %s:%d invalid regex %q: %w", path, lineNo, line, err)
		}
		out = append(out, re)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("speclint: scan %s: %w", path, err)
	}
	if len(out) == 0 {
		return DefaultJudgementPhrases(), nil
	}
	return out, nil
}
