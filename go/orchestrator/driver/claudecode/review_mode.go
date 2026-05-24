package claudecode

import (
	"errors"
	"strings"
)

// extractVerdictJSON pulls a JSON document out of raw model output.
//
// Strategies in order, per spec 109 FR-003:
//
//	(a) Strip surrounding markdown fences (```json ... ``` or ``` ... ```).
//	(b) Extract the largest top-level balanced {...} block. Double-quoted
//	    string literals are treated as opaque so braces inside JSON
//	    strings cannot unbalance the scanner.
//	(c) When no balanced block exists, fall back to the raw input and
//	    return a non-nil error so the caller can mark the result as a
//	    malformed-shape failure.
func extractVerdictJSON(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return raw, errors.New("empty model output")
	}

	candidate := stripSurroundingFences(raw)
	if block := largestBalancedBlock(candidate); block != "" {
		return block, nil
	}
	return raw, errors.New("no JSON-shaped substring found in model output")
}

// stripSurroundingFences peels triple-backtick fences off when they wrap
// the entire (trimmed) input. Returns the original string when no
// surrounding fence is present so the brace scanner can still pick up
// JSON embedded in prose.
func stripSurroundingFences(s string) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 6 || !strings.HasPrefix(trimmed, "```") || !strings.HasSuffix(trimmed, "```") {
		return s
	}
	inner := trimmed[3 : len(trimmed)-3]
	// Drop an optional language tag on the opening line (e.g. "json").
	if newline := strings.IndexByte(inner, '\n'); newline >= 0 {
		first := strings.TrimSpace(inner[:newline])
		if first == "" || isFenceLang(first) {
			return inner[newline+1:]
		}
	}
	return inner
}

// isFenceLang reports whether s looks like a markdown code-fence info
// string (e.g. "json", "jsonc"). Only ASCII identifier characters are
// considered to avoid mistaking the first line of fence-less content
// for a language tag.
func isFenceLang(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '+':
		default:
			return false
		}
	}
	return true
}

// largestBalancedBlock returns the longest top-level {...} substring of
// s. Inner (nested) blocks are skipped because they are by definition
// smaller than the enclosing one. Returns "" when no balanced block is
// found.
func largestBalancedBlock(s string) string {
	var best string
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		end, ok := matchBrace(s, i)
		if !ok {
			continue
		}
		if candidate := s[i : end+1]; len(candidate) > len(best) {
			best = candidate
		}
		i = end
	}
	return best
}

// matchBrace finds the index of the '}' that closes the '{' at start,
// honoring nesting and skipping over double-quoted strings (with
// backslash escapes). Returns (0, false) when no matching brace exists.
func matchBrace(s string, start int) (int, bool) {
	depth := 0
	inString := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch c {
			case '\\':
				if i+1 < len(s) {
					i++
				}
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}
