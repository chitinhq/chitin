package main

import (
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestSplitPositionalID(t *testing.T) {
	// Empty args → empty id
	id, rest := splitPositionalID(nil)
	if id != "" {
		t.Errorf("expected empty id, got %q", id)
	}
	if len(rest) != 0 {
		t.Errorf("expected empty rest, got %v", rest)
	}

	// First arg is a flag → no id
	id, rest = splitPositionalID([]string{"-flag", "value"})
	if id != "" {
		t.Errorf("expected empty id for flag, got %q", id)
	}

	// First arg is positional → id + rest
	id, rest = splitPositionalID([]string{"env-123", "--verbose"})
	if id != "env-123" {
		t.Errorf("expected id=env-123, got %q", id)
	}
	if len(rest) != 1 || rest[0] != "--verbose" {
		t.Errorf("expected rest=[--verbose], got %v", rest)
	}
}

func TestFmtGrantDelta(t *testing.T) {
	cases := []struct {
		name   string
		calls  int64
		bytes  int64
		usd    float64
		reason string
		want   string
	}{
		{"all zero", 0, 0, 0, "", "grant"},
		{"calls only", 5, 0, 0, "", "grant calls=+5"},
		{"bytes only", 0, 1024, 0, "", "grant bytes=+1024"},
		{"usd only", 0, 0, 0.5, "", "grant usd=+0.500000"},
		{"all set", 10, 2048, 1.5, "burst", "grant calls=+10 bytes=+2048 usd=+1.500000 (burst)"},
		{"reason only", 0, 0, 0, "topup", "grant (topup)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := fmtGrantDelta(c.calls, c.bytes, c.usd, c.reason)
			if got != c.want {
				t.Errorf("fmtGrantDelta(%d, %d, %f, %q) = %q, want %q",
					c.calls, c.bytes, c.usd, c.reason, got, c.want)
			}
		})
	}
}

func TestHumanBytes_ExtraCases(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{1, "1B"},
		{1500, "1.5KB"},
		{1000000, "1.0MB"},
		{1500000000, "1.5GB"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			got := humanBytes(c.n)
			if got != c.want {
				t.Errorf("humanBytes(%d) = %q, want %q", c.n, got, c.want)
			}
		})
	}
}

func TestFormatCapStatusInt(t *testing.T) {
	// Uncapped
	got := formatCapStatusInt(5, 0)
	if got != "5/uncapped" {
		t.Errorf("formatCapStatusInt(5, 0) = %q, want %q", got, "5/uncapped")
	}
	// Capped
	got = formatCapStatusInt(3, 10)
	if got != "3/10" {
		t.Errorf("formatCapStatusInt(3, 10) = %q, want %q", got, "3/10")
	}
}

func TestFormatCapStatusBytes(t *testing.T) {
	// Uncapped
	got := formatCapStatusBytes(1024, 0)
	if got != "1.0KB/uncapped" {
		t.Errorf("formatCapStatusBytes(1024, 0) = %q, want %q", got, "1.0KB/uncapped")
	}
	// Capped
	got = formatCapStatusBytes(512, 2048)
	if got != "512B/2.0KB" {
		t.Errorf("formatCapStatusBytes(512, 2048) = %q, want %q", got, "512B/2.0KB")
	}
}

func TestFormatStats(t *testing.T) {
	st := gov.EnvelopeState{
		ID: "env-test",
		Limits: gov.BudgetLimits{
			MaxToolCalls:  100,
			MaxInputBytes: 10000,
		},
		SpentCalls: 5,
		SpentBytes: 512,
		SpentUSD:   0.10,
	}
	got := formatStats(st, 3)
	// Should contain envelope id, calls, bytes, denials
	if got == "" {
		t.Error("formatStats returned empty string")
	}
	if got[:7] != "[stats]" {
		t.Errorf("formatStats should start with [stats], got %q", got[:7])
	}
}