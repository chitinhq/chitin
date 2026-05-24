package main

import (
	"context"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

func TestValidateForDispatch_EmptyDAGIsValid(t *testing.T) {
	reg, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	errs := ValidateForDispatch(context.Background(), dag.New(), reg)
	if len(errs) != 0 {
		t.Errorf("expected empty DAG to pass validation, got %v", errs)
	}
}

func TestValidateForDispatch_AllValid(t *testing.T) {
	reg, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	d := dag.New()
	for _, id := range []string{"a", "b", "c"} {
		if err := d.AddNode(dag.Node{
			ID:         id,
			Kind:       dag.NodeKindAgent,
			Capability: "code.implement",
		}); err != nil {
			t.Fatalf("AddNode(%s): %v", id, err)
		}
	}
	errs := ValidateForDispatch(context.Background(), d, reg)
	if len(errs) != 0 {
		t.Errorf("expected all-valid DAG to pass, got %v", errs)
	}
}

func TestValidateForDispatch_NeedsClarification(t *testing.T) {
	reg, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	d := dag.New()
	if err := d.AddNode(dag.Node{
		ID:         "ambig",
		Kind:       dag.NodeKindAgent,
		Capability: adapter.NeedsClarification,
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	errs := ValidateForDispatch(context.Background(), d, reg)
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 validation error, got %d: %v", len(errs), errs)
	}
	if errs[0].Kind != "needs_clarification" {
		t.Errorf("Kind = %q, want %q", errs[0].Kind, "needs_clarification")
	}
	if errs[0].NodeID != "ambig" {
		t.Errorf("NodeID = %q, want %q", errs[0].NodeID, "ambig")
	}
}

func TestValidateForDispatch_UnroutableCapability(t *testing.T) {
	reg, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	d := dag.New()
	if err := d.AddNode(dag.Node{
		ID:         "weird",
		Kind:       dag.NodeKindAgent,
		Capability: "code.compile", // not in the taxonomy
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	errs := ValidateForDispatch(context.Background(), d, reg)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Kind != "unroutable" {
		t.Errorf("Kind = %q, want %q", errs[0].Kind, "unroutable")
	}
}

func TestValidateForDispatch_MissingCapability(t *testing.T) {
	reg, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	d := dag.New()
	if err := d.AddNode(dag.Node{
		ID:         "empty",
		Kind:       dag.NodeKindAgent,
		Capability: "",
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	errs := ValidateForDispatch(context.Background(), d, reg)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Kind != "missing_capability" {
		t.Errorf("Kind = %q, want %q", errs[0].Kind, "missing_capability")
	}
}

func TestValidateForDispatch_DeterministicNodeNeedsCommand(t *testing.T) {
	reg, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	d := dag.New()
	if err := d.AddNode(dag.Node{
		ID:      "det-empty",
		Kind:    dag.NodeKindDeterministic,
		Command: "", // empty Command on a deterministic node = invalid
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	errs := ValidateForDispatch(context.Background(), d, reg)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Kind != "missing_capability" {
		t.Errorf("Kind = %q, want %q", errs[0].Kind, "missing_capability")
	}
}

func TestValidateForDispatch_DeterministicNodeWithCommandIsValid(t *testing.T) {
	reg, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	d := dag.New()
	if err := d.AddNode(dag.Node{
		ID:      "det-ok",
		Kind:    dag.NodeKindDeterministic,
		Command: "go",
		Args:    []string{"test", "./..."},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	errs := ValidateForDispatch(context.Background(), d, reg)
	if len(errs) != 0 {
		t.Errorf("expected deterministic node with command to pass, got %v", errs)
	}
}

func TestValidateForDispatch_MultipleErrorsReported(t *testing.T) {
	reg, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	d := dag.New()
	for _, n := range []dag.Node{
		{ID: "a", Kind: dag.NodeKindAgent, Capability: "code.implement"}, // valid
		{ID: "b", Kind: dag.NodeKindAgent, Capability: adapter.NeedsClarification},
		{ID: "c", Kind: dag.NodeKindAgent, Capability: "code.implement"}, // valid
		{ID: "d", Kind: dag.NodeKindAgent, Capability: ""},
	} {
		if err := d.AddNode(n); err != nil {
			t.Fatalf("AddNode(%s): %v", n.ID, err)
		}
	}
	errs := ValidateForDispatch(context.Background(), d, reg)
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
	// Errors come back in DAG node order, deterministic.
	if errs[0].NodeID != "b" || errs[1].NodeID != "d" {
		t.Errorf("unexpected error order: %v", errs)
	}
}
