package adapter

import (
	"errors"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

// fakeAdapter is a test SpecKitAdapter whose Detect result and Compile output
// are fixed at construction.
type fakeAdapter struct {
	kit      string
	detect   bool
	detasErr error
}

func (f *fakeAdapter) Kit() string { return f.kit }

func (f *fakeAdapter) Detect(string) (bool, error) {
	return f.detect, f.detasErr
}

func (f *fakeAdapter) Compile(string, string) (*dag.DAG, error) {
	return dag.New(), nil
}

// TestRegisterRejectsDuplicateKit asserts a kit name is unique.
func TestRegisterRejectsDuplicateKit(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&fakeAdapter{kit: "k1"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(&fakeAdapter{kit: "k1"}); err == nil {
		t.Error("expected duplicate-kit registration to fail")
	}
}

// TestRegisterRejectsEmptyAndNil asserts the registration contract.
func TestRegisterRejectsEmptyAndNil(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Error("expected nil-adapter registration to fail")
	}
	if err := r.Register(&fakeAdapter{kit: ""}); err == nil {
		t.Error("expected empty-kit registration to fail")
	}
}

// TestResolveSingleKit asserts a repo matching exactly one kit resolves to
// that adapter (FR-008).
func TestResolveSingleKit(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&fakeAdapter{kit: "speckit", detect: true})
	_ = r.Register(&fakeAdapter{kit: "openspec", detect: false})

	a, err := r.Resolve("/repo", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if a.Kit() != "speckit" {
		t.Errorf("resolved kit = %q, want speckit", a.Kit())
	}
}

// TestResolveNoKit asserts a repo matching no kit reports it explicitly
// (FR-008).
func TestResolveNoKit(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&fakeAdapter{kit: "speckit", detect: false})

	_, err := r.Resolve("/repo", "")
	var uErr *UnrecognizedKitError
	if !errors.As(err, &uErr) {
		t.Fatalf("error %T is not *UnrecognizedKitError", err)
	}
}

// TestResolveAmbiguousRequiresChoice asserts a repo matching more than one
// kit fails without an explicit choice and succeeds with one — never picking
// silently (FR-008).
func TestResolveAmbiguousRequiresChoice(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&fakeAdapter{kit: "speckit", detect: true})
	_ = r.Register(&fakeAdapter{kit: "openspec", detect: true})

	// No explicit choice → AmbiguousKitError listing both kits.
	_, err := r.Resolve("/repo", "")
	var aErr *AmbiguousKitError
	if !errors.As(err, &aErr) {
		t.Fatalf("error %T is not *AmbiguousKitError", err)
	}
	if len(aErr.Kits) != 2 {
		t.Errorf("ambiguous error lists %v, want both kits", aErr.Kits)
	}

	// Explicit choice → the chosen adapter.
	a, err := r.Resolve("/repo", "openspec")
	if err != nil {
		t.Fatalf("Resolve with explicit kit: %v", err)
	}
	if a.Kit() != "openspec" {
		t.Errorf("explicit choice resolved to %q, want openspec", a.Kit())
	}

	// Explicit choice naming an undetected kit → rejected.
	if _, err := r.Resolve("/repo", "nonsuch"); err == nil {
		t.Error("expected an explicit choice of an undetected kit to fail")
	}
}

// TestDetectKitsPropagatesIOError asserts a probe I/O fault aborts detection
// rather than being treated as "kit absent".
func TestDetectKitsPropagatesIOError(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&fakeAdapter{kit: "speckit", detasErr: errors.New("disk gone")})
	if _, err := r.DetectKits("/repo"); err == nil {
		t.Error("expected a detection I/O error to propagate")
	}
}
