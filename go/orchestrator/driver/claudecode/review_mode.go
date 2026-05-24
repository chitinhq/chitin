package claudecode

import (
	"errors"
	"strings"
)

// errNoJSONFound signals that extractVerdictJSON could not locate a balanced
// {...} block in the input. Callers receive the original raw string alongside
// this error (per spec 109 FR-003 (c)) so they can surface it to the verdict
// activity as malformed output.
var errNoJSONFound = errors.New("no JSON-shaped substring in driver output")

// extractVerdictJSON pulls the StructuredVerdict body out of a model's raw
// stdout. Per spec 109 FR-003 it applies, in order:
//
//	(a) strip a surrounding ```json ... ``` (or ``` ... ```) markdown fence,
//	(b) return the LARGEST balanced {...} block found in the remaining text,
//	(c) fall back to the unmodified raw string when no balanced block exists.
//
// On (a)+(b) success the returned string is the extracted JSON document and
// err is nil. On (c) the returned string is the original raw input and err
// wraps errNoJSONFound so the caller can route to malformed-output handling
// without re-parsing.
func extractVerdictJSON(raw string) (string, error) {
	body := stripMarkdownFence(raw)
	if block, ok := largestBalancedBraces(body); ok {
		return block, nil
	}
	return raw, errNoJSONFound
}

// stripMarkdownFence removes a single outer ``` ... ``` (optionally
// language-tagged, e.g. ```json) fence if one wraps the trimmed input.
// Returns the input untouched when no such fence is present so the brace
// scanner can still find inline JSON.
func stripMarkdownFence(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "```") {
		return s
	}
	rest := strings.TrimPrefix(trimmed, "```")
	// Drop the optional language tag on the opening fence's line.
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		firstLine := strings.TrimSpace(rest[:nl])
		if firstLine == "" || isLangTag(firstLine) {
			rest = rest[nl+1:]
		}
	}
	if idx := strings.LastIndex(rest, "```"); idx >= 0 {
		rest = rest[:idx]
	}
	return strings.TrimSpace(rest)
}

func isLangTag(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '+':
		default:
			return false
		}
	}
	return true
}

// largestBalancedBraces scans s for top-level balanced {...} blocks,
// ignoring braces that appear inside double-quoted JSON string literals,
// and returns the longest such block. Returns ("", false) when no balanced
// block exists.
func largestBalancedBraces(s string) (string, bool) {
	var (
		best     string
		depth    int
		startIdx = -1
		inString bool
		escape   bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if inString {
			switch c {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				startIdx = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && startIdx >= 0 {
				candidate := s[startIdx : i+1]
				if len(candidate) > len(best) {
					best = candidate
				}
				startIdx = -1
			}
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}
