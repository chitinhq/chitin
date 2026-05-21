package ingest

import (
	"context"
	"testing"
)

// Spec 079 T012 / FR-011 / SC-005 tests for the knowledge-base projection
// activity: a kept item is surfaced; the projection has exactly one verb —
// the pipeline can write to the knowledge base and nothing else.

// TestSurfaceActivity_SurfacesKeptItem proves a kept KnowledgeItem reaches
// the knowledge base via the activity.
func TestSurfaceActivity_SurfacesKeptItem(t *testing.T) {
	kb := NewMemoryKnowledgeBase()
	act := NewSurfaceActivity(kb)

	item := KnowledgeItem{SourceRef: "https://example.com/post", Rank: 0.8, Trust: TrustOperatorSeeded}
	res, err := act.Execute(context.Background(), SurfaceInput{Item: item})
	if err != nil {
		t.Fatalf("Execute errored: %v", err)
	}
	if !res.Surfaced {
		t.Error("a well-formed item should be surfaced")
	}
	if !kb.Has("https://example.com/post") {
		t.Error("the item did not reach the knowledge base")
	}
	if kb.Len() != 1 {
		t.Errorf("knowledge base size = %d, want 1", kb.Len())
	}
}

// TestSurfaceActivity_RejectsUnanchoredItem proves the boundary guard — an
// item with no source ref is refused, never silently surfaced.
func TestSurfaceActivity_RejectsUnanchoredItem(t *testing.T) {
	act := NewSurfaceActivity(NewMemoryKnowledgeBase())
	if _, err := act.Execute(context.Background(), SurfaceInput{Item: KnowledgeItem{}}); err == nil {
		t.Error("an item with no source ref must be rejected")
	}
}

// TestSurfaceActivity_IsIdempotent proves surfacing the same source twice is
// safe — required under Temporal's at-least-once activity execution.
func TestSurfaceActivity_IsIdempotent(t *testing.T) {
	kb := NewMemoryKnowledgeBase()
	act := NewSurfaceActivity(kb)
	item := KnowledgeItem{SourceRef: "https://example.com/x", Rank: 0.7}

	for i := 0; i < 3; i++ {
		if _, err := act.Execute(context.Background(), SurfaceInput{Item: item}); err != nil {
			t.Fatalf("surface %d errored: %v", i, err)
		}
	}
	if kb.Len() != 1 {
		t.Errorf("idempotent surface left %d items, want 1", kb.Len())
	}
}

// TestSurfaceActivity_NilSinkFallsBackToLogging proves the activity is
// runnable before the real knowledge base exists — a nil sink logs and
// discards rather than crashing.
func TestSurfaceActivity_NilSinkFallsBackToLogging(t *testing.T) {
	act := NewSurfaceActivity(nil)
	res, err := act.Execute(context.Background(), SurfaceInput{
		Item: KnowledgeItem{SourceRef: "https://example.com/x"},
	})
	if err != nil {
		t.Fatalf("the logging fallback must not error: %v", err)
	}
	if !res.Surfaced {
		t.Error("the logging fallback should still report Surfaced")
	}
}
