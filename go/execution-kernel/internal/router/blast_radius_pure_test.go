package router

import (
	"testing"
)

func TestBlastRadiusStringField(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]interface{}
		key  string
		want string
	}{
		{"missing key", map[string]interface{}{"other": "val"}, "key", ""},
		{"string value", map[string]interface{}{"key": "hello"}, "key", "hello"},
		{"trimmed string", map[string]interface{}{"key": "  hello  "}, "key", "hello"},
		{"non-string value", map[string]interface{}{"key": 42}, "key", ""},
		{"empty string", map[string]interface{}{"key": ""}, "key", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stringField(tc.m, tc.key); got != tc.want {
				t.Errorf("stringField(%v, %q) = %q, want %q", tc.m, tc.key, got, tc.want)
			}
		})
	}
}