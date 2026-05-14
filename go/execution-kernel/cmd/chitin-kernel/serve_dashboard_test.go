package main

import (
	"strings"
	"testing"
)

func TestDashboardRenderArea_BoundariesEmptyMaxErrorStack(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "empty",
			want: "No cost data in this session.",
		},
		{
			name: "max",
			want: "const maxY = Math.max(...points.map((point) => point.cumulative_usd || 0), 0.0001);",
		},
		{
			name: "error",
			want: "Dashboard load failed",
		},
		{
			name: "stacked lower upper bands",
			want: "const lowerValue = drivers.slice(0, driverIndex).reduce((sum, name) => sum + (driverCosts[name] || 0), 0);",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(dashboardHTML, tt.want) {
				t.Fatalf("dashboardHTML missing %q boundary contract", tt.want)
			}
		})
	}
}
