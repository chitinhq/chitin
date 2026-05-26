package main

import (
	"context"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/dag"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

type payloadFakeDriver struct {
	id   string
	card driver.CapabilityCard
}

func (f *payloadFakeDriver) ID() string                  { return f.id }
func (f *payloadFakeDriver) Card() driver.CapabilityCard { return f.card }
func (f *payloadFakeDriver) Ready(context.Context) (bool, string) {
	return true, ""
}
func (f *payloadFakeDriver) Invoke(context.Context, driver.WorkUnit) (driver.Result, error) {
	return driver.Result{}, nil
}

func TestSelectedDriverIDForPayload_WholeSpecSingleNode(t *testing.T) {
	reg := driver.NewRegistry()
	if err := reg.Register(&payloadFakeDriver{
		id: "claudecode-glm",
		card: driver.CapabilityCard{
			DriverID:     "claudecode-glm",
			AgentRuntime: "claude-code",
			Model:        "glm-5.1",
			Capabilities: []driver.Capability{driver.CapSpecImplement},
			Tier:         driver.TierLocal,
			CostClass:    driver.CostZero,
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	d := dag.New()
	if err := d.AddNode(dag.Node{
		ID:         "wu-120-whole",
		Kind:       dag.NodeKindAgent,
		Capability: string(driver.CapSpecImplement),
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	got := selectedDriverIDForPayload(context.Background(), d, reg)
	if got != "claudecode-glm" {
		t.Fatalf("selectedDriverIDForPayload = %q, want claudecode-glm", got)
	}
}
