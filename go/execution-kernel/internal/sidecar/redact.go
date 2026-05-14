package sidecar

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

const (
	redactedValue = "[REDACTED]"
	redactedPath  = "[REDACTED_PATH]"
)

var (
	sensitiveKeyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(^|[_-])(api[_-]?key|access[_-]?token|auth|authorization|bearer|cookie|password|passwd|secret|session|signature|token)($|[_-])`),
	}
	inlineSecretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._\-+=/]{8,}`),
		regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|password|secret|session|token)\s*[:=]\s*['"]?[A-Za-z0-9._\-+=/]{8,}`),
		regexp.MustCompile(`(?i)\b(sk-[A-Za-z0-9]{12,}|ghp_[A-Za-z0-9]{12,}|xox[baprs]-[A-Za-z0-9-]{10,})\b`),
	}
	sensitivePathPattern = regexp.MustCompile(`(?i)(/home/[^/\s]+|/Users/[^/\s]+|C:\\Users\\[^\\\s]+)[:/\\](?:\.ssh|\.aws|\.gnupg|\.config|Library/Application Support)(?:[/\\][^\s"']*)?`)
)

func RedactBytes(body []byte) ([]byte, bool) {
	if len(body) == 0 {
		return nil, false
	}
	var value any
	if err := json.Unmarshal(body, &value); err == nil {
		redacted := redactValue("", value)
		out, marshalErr := json.Marshal(redacted)
		if marshalErr != nil {
			text := redactText(string(body))
			return []byte(text), text != string(body)
		}
		return out, !bytes.Equal(out, body)
	}
	text := redactText(string(body))
	return []byte(text), text != string(body)
}

func redactValue(key string, value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for childKey, childValue := range v {
			if sensitiveKey(childKey) {
				out[childKey] = redactedValue
				continue
			}
			out[childKey] = redactValue(childKey, childValue)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = redactValue(key, v[i])
		}
		return out
	case string:
		if sensitiveKey(key) {
			return redactedValue
		}
		return redactText(v)
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	for _, pattern := range sensitiveKeyPatterns {
		if pattern.MatchString(key) {
			return true
		}
	}
	return false
}

func redactText(s string) string {
	s = sensitivePathPattern.ReplaceAllString(s, redactedPath)
	for _, pattern := range inlineSecretPatterns {
		s = pattern.ReplaceAllStringFunc(s, func(match string) string {
			if strings.Contains(strings.ToLower(match), "bearer ") {
				parts := strings.SplitN(match, " ", 2)
				if len(parts) == 2 {
					return parts[0] + " " + redactedValue
				}
			}
			if idx := strings.IndexAny(match, ":="); idx >= 0 {
				return match[:idx+1] + " " + redactedValue
			}
			return redactedValue
		})
	}
	return s
}
