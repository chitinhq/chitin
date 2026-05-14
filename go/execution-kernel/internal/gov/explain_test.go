package gov

import (
	"reflect"
	"strings"
	"testing"
)

// TestExplainMatch_ParamCheckOrderIsDeterministic covers the Copilot finding:
// ExplainMatch ranged over r.Params (a Go map) directly, so the param checks —
// and therefore the rendered explain output and near-miss list — came out in a
// different order on each run. They must now be sorted by key.
func TestExplainMatch_ParamCheckOrderIsDeterministic(t *testing.T) {
	r := Rule{
		ID:     "multi-param",
		Action: ActionMatcher{"shell.exec"},
		Effect: "deny",
		Params: map[string]string{
			"zeta":   "1",
			"alpha":  "2",
			"mike":   "3",
			"bravo":  "4",
			"yankee": "5",
		},
	}
	a := Action{Type: ActionType("shell.exec"), Params: map[string]any{}}

	want := []string{"param:alpha", "param:bravo", "param:mike", "param:yankee", "param:zeta"}

	paramNames := func(m MatchExplanation) []string {
		var names []string
		for _, c := range m.Checks {
			if strings.HasPrefix(c.Name, "param:") {
				names = append(names, c.Name)
			}
		}
		return names
	}

	// Many repeats: a map-iteration bug would surface as a different order
	// on at least one run with overwhelming probability.
	for i := 0; i < 100; i++ {
		got := paramNames(r.ExplainMatch(a, FingerprintContext{}))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: param checks not in sorted order: got %v want %v", i, got, want)
		}
	}
}
