package sidecar

import (
	"strings"
	"testing"
)

// TestRedactBytes_CamelCaseSensitiveKeys covers the Copilot finding: the
// snake/kebab-oriented key patterns missed camelCase secret fields, so values
// under authToken / sessionId / cookieValue were written to the sidecar in
// the clear.
func TestRedactBytes_CamelCaseSensitiveKeys(t *testing.T) {
	keys := []string{"authToken", "sessionId", "cookieValue", "apiKey", "accessToken", "bearerToken"}
	const secret = "super-secret-value-12345"
	for _, key := range keys {
		body := []byte(`{"` + key + `":"` + secret + `"}`)
		out, changed := RedactBytes(body)
		if !changed {
			t.Errorf("%s: expected redaction, got unchanged output", key)
		}
		if strings.Contains(string(out), secret) {
			t.Errorf("%s: secret leaked through redaction: %s", key, out)
		}
	}
}

// TestRedactBytes_EmptyInput is the empty boundary.
func TestRedactBytes_EmptyInput(t *testing.T) {
	for _, body := range [][]byte{nil, {}, []byte("")} {
		out, changed := RedactBytes(body)
		if out != nil || changed {
			t.Fatalf("empty input: expected (nil,false), got (%v,%v)", out, changed)
		}
	}
}

// TestRedactBytes_MaxSizePayload is the max boundary: a large JSON object
// with a secret must still redact without panicking or dropping the secret.
func TestRedactBytes_MaxSizePayload(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"filler":"`)
	sb.WriteString(strings.Repeat("a", 500_000))
	sb.WriteString(`","apiKey":"sk-abcdefghijklmnop"}`)
	out, changed := RedactBytes([]byte(sb.String()))
	if !changed {
		t.Fatal("max payload: expected redaction")
	}
	if strings.Contains(string(out), "sk-abcdefghijklmnop") {
		t.Fatalf("max payload: secret leaked")
	}
}

// TestRedactBytes_NonJSONFallsBackToTextRedaction is the error boundary:
// malformed / non-JSON input must not crash — it falls back to inline
// text redaction.
func TestRedactBytes_NonJSONFallsBackToTextRedaction(t *testing.T) {
	body := []byte("not json at all, bearer abcdefgh12345678 trailing text")
	out, changed := RedactBytes(body)
	if !changed {
		t.Fatal("non-json input: expected inline redaction")
	}
	if strings.Contains(string(out), "abcdefgh12345678") {
		t.Fatalf("non-json input: bearer token leaked: %s", out)
	}
}

func TestCamelToSnake(t *testing.T) {
	cases := map[string]string{
		"authToken":   "auth_Token",
		"sessionId":   "session_Id",
		"cookieValue": "cookie_Value",
		"plain":       "plain",
		"already_snake": "already_snake",
		"APIKey":      "APIKey", // acronym run left intact
	}
	for in, want := range cases {
		if got := camelToSnake(in); got != want {
			t.Errorf("camelToSnake(%q) = %q, want %q", in, got, want)
		}
	}
}
