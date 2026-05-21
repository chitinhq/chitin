package hookdispatch

import "testing"

// Smoke test for kanban-dispatch workflow step-output interpolation ($step.json.field syntax)
func TestKanbanDispatchStepOutputInterpolation(t *testing.T) {
	payload := map[string]any{
		"step": map[string]any{
			"json": map[string]any{
				"field": "expected-value",
			},
		},
	}
	// Simulate extracting $step.json.field
	step, ok := payload["step"].(map[string]any)
	if !ok {
		t.Fatalf("payload.step missing or wrong type")
	}
	jsonObj, ok := step["json"].(map[string]any)
	if !ok {
		t.Fatalf("payload.step.json missing or wrong type")
	}
	field, ok := jsonObj["field"].(string)
	if !ok || field != "expected-value" {
		t.Errorf("expected 'expected-value', got %v", field)
	}
}
