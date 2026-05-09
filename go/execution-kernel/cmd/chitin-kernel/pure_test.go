package main

import "testing"

func TestSplitPositionalID(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		want  string
		rest  []string
	}{
		{"empty", []string{}, "", []string{}},
		{"position arg", []string{"sess-1", "arg2"}, "sess-1", []string{"arg2"}},
		{"flag first", []string{"-v", "arg"}, "", []string{"-v", "arg"}},
		{"double dash flag", []string{"--verbose"}, "", []string{"--verbose"}},
		{"single position", []string{"abc"}, "abc", []string{}},
		{"position with flags after", []string{"abc", "-v", "--force"}, "abc", []string{"-v", "--force"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rest := splitPositionalID(tt.args)
			if got != tt.want {
				t.Errorf("splitPositionalID(%v) id = %q, want %q", tt.args, got, tt.want)
			}
			if len(rest) != len(tt.rest) {
				t.Errorf("splitPositionalID(%v) rest = %v, want %v", tt.args, rest, tt.rest)
			}
		})
	}
}

func TestFormatCapStatusInt(t *testing.T) {
	tests := []struct {
		name   string
		spent  int64
		max    int64
		want   string
	}{
		{"uncapped", 50, 0, "50/uncapped"},
		{"negative max", 50, -1, "50/uncapped"},
		{"normal", 50, 100, "50/100"},
		{"zero spent", 0, 100, "0/100"},
		{"over budget", 150, 100, "150/100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCapStatusInt(tt.spent, tt.max)
			if got != tt.want {
				t.Errorf("formatCapStatusInt(%d, %d) = %q, want %q", tt.spent, tt.max, got, tt.want)
			}
		})
	}
}

func TestFormatCapStatusBytes(t *testing.T) {
	tests := []struct {
		name   string
		spent  int64
		max    int64
		want   string
	}{
		{"uncapped", 500, 0, "500B/uncapped"},
		{"negative max uncapped", 500, -1, "500B/uncapped"},
		{"normal bytes", 500, 1000, "500B/1.0KB"},
		{"kb", 1500, 2000, "1.5KB/2.0KB"},
		{"mb", 1500000, 2000000, "1.5MB/2.0MB"},
		{"zero spent", 0, 1000, "0B/1.0KB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCapStatusBytes(tt.spent, tt.max)
			if got != tt.want {
				t.Errorf("formatCapStatusBytes(%d, %d) = %q, want %q", tt.spent, tt.max, got, tt.want)
			}
		})
	}
}

