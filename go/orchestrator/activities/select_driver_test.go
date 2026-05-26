package activities

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/driver/claudecodeglm"
)

// fakeDriver is a minimal driver.AgentDriver for the SelectDriver activity
// tests — a fixed card and a fixed ready state.
type fakeDriver struct {
	id   string
	card driver.CapabilityCard
}

func (f *fakeDriver) ID() string                           { return f.id }
func (f *fakeDriver) Card() driver.CapabilityCard          { return f.card }
func (f *fakeDriver) Ready(context.Context) (bool, string) { return true, "" }
func (f *fakeDriver) Invoke(context.Context, driver.WorkUnit) (driver.Result, error) {
	return driver.Result{}, nil
}

// newFakeDriver builds a ready fake driver declaring the given capability.
func newFakeDriver(id string, cap driver.Capability) *fakeDriver {
	return &fakeDriver{
		id: id,
		card: driver.CapabilityCard{
			DriverID:     id,
			AgentRuntime: "fake",
			Capabilities: []driver.Capability{cap},
			Tier:         driver.TierMid,
			CostClass:    driver.CostLow,
		},
	}
}

// TestSelectDriver_RoutesByCapability proves FR-007: the activity routes a
// node to a registered, ready, capability-matching driver.
func TestSelectDriver_RoutesByCapability(t *testing.T) {
	reg := driver.NewRegistry()
	if err := reg.Register(newFakeDriver("impl-driver", driver.CapCodeImplement)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	act := NewDriverSelector(reg)

	res, err := act.Execute(context.Background(), SelectDriverInput{
		NodeID:     "n1",
		Capability: string(driver.CapCodeImplement),
	})
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if res.Unroutable {
		t.Fatalf("node n1 must be routable; got unroutable: %s", res.Reason)
	}
	if res.DriverID != "impl-driver" {
		t.Errorf("selected driver = %q, want impl-driver", res.DriverID)
	}
	if res.Reason == "" {
		t.Error("a routed selection must carry a non-empty reason for the audit record")
	}
}

// TestSelectDriver_BlockedUnroutable proves FR-010: a node whose capability no
// driver satisfies yields an Unroutable result naming the missing capability —
// never an error, so the rest of the frontier can still proceed.
func TestSelectDriver_BlockedUnroutable(t *testing.T) {
	reg := driver.NewRegistry()
	if err := reg.Register(newFakeDriver("impl-driver", driver.CapCodeImplement)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	act := NewDriverSelector(reg)

	res, err := act.Execute(context.Background(), SelectDriverInput{
		NodeID:     "n2",
		Capability: string(driver.CapResearchWeb),
	})
	if err != nil {
		t.Fatalf("Execute on a blocked-unroutable node must NOT error; got %v", err)
	}
	if !res.Unroutable {
		t.Fatalf("node n2 must be blocked-unroutable; got driver %q", res.DriverID)
	}
	if res.MissingCapability != string(driver.CapResearchWeb) {
		t.Errorf("missing capability = %q, want %q", res.MissingCapability, driver.CapResearchWeb)
	}
}

func TestSelectDriver_SkipsNotReadyClaudeCodeGLM(t *testing.T) {
	dir := t.TempDir()
	ollama := writeActivityShim(t, dir, "ollama")
	claude := writeActivityShim(t, dir, "claude")

	reg := driver.NewRegistry()
	if err := reg.Register(claudecodeglm.New(
		claudecodeglm.WithOllamaCommand(ollama),
		claudecodeglm.WithClaudeCommand(claude),
		claudecodeglm.WithBaseURL("http://127.0.0.1:1"),
	)); err != nil {
		t.Fatalf("Register claudecode-glm: %v", err)
	}
	act := NewDriverSelector(reg)

	res, err := act.Execute(context.Background(), SelectDriverInput{
		NodeID:     "wu-120-whole",
		Capability: string(driver.CapSpecImplement),
	})
	if err != nil {
		t.Fatalf("Execute must return blocked-unroutable as a result, not an error: %v", err)
	}
	if !res.Unroutable {
		t.Fatalf("ready=false claudecode-glm should be skipped; got driver %q", res.DriverID)
	}
	if res.MissingCapability != string(driver.CapSpecImplement) {
		t.Fatalf("MissingCapability = %q, want %q", res.MissingCapability, driver.CapSpecImplement)
	}
}

// TestSelectDriver_NoRegistryErrors proves a misconfigured activity (no
// registry bound) returns an error rather than silently mis-routing.
func TestSelectDriver_NoRegistryErrors(t *testing.T) {
	act := NewDriverSelector(nil)
	if _, err := act.Execute(context.Background(), SelectDriverInput{
		NodeID: "n3", Capability: string(driver.CapCodeImplement),
	}); err == nil {
		t.Fatal("Execute with no registry bound must error, got nil")
	}
}

func writeActivityShim(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write shim %s: %v", name, err)
	}
	return path
}

// TestSelectDriver_Deterministic proves selection is deterministic — 100
// repeated selections over a fixed registry yield the identical driver.
func TestSelectDriver_Deterministic(t *testing.T) {
	reg := driver.NewRegistry()
	for _, id := range []string{"d-alpha", "d-bravo", "d-charlie"} {
		if err := reg.Register(newFakeDriver(id, driver.CapCodeImplement)); err != nil {
			t.Fatalf("Register(%s): %v", id, err)
		}
	}
	act := NewDriverSelector(reg)

	first, err := act.Execute(context.Background(), SelectDriverInput{
		NodeID: "n", Capability: string(driver.CapCodeImplement),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for i := 0; i < 100; i++ {
		got, err := act.Execute(context.Background(), SelectDriverInput{
			NodeID: "n", Capability: string(driver.CapCodeImplement),
		})
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if got.DriverID != first.DriverID {
			t.Fatalf("iteration %d: selection drifted — %q != %q", i, got.DriverID, first.DriverID)
		}
	}
}
