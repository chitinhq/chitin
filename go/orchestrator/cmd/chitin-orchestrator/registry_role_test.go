package main

import (
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

func TestBuildRegistry_UsesTieredRoleAllowlists(t *testing.T) {
	t.Setenv("CHITIN_DRIVER_ALLOW", "")
	t.Setenv("CHITIN_DRIVER_ALLOW_IMPL", "codex")
	t.Setenv("CHITIN_DRIVER_ALLOW_REVIEW", "codex,claudecode")

	implRegistry, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry(impl): %v", err)
	}
	reviewRegistry, err := buildRegistry("review")
	if err != nil {
		t.Fatalf("buildRegistry(review): %v", err)
	}

	assertRegistryIDs(t, implRegistry, []string{"codex"})
	assertRegistryIDs(t, reviewRegistry, []string{"codex", "claudecode"})
}

func TestBuildRegistry_RolesFallBackToLegacyAllowlist(t *testing.T) {
	t.Setenv("CHITIN_DRIVER_ALLOW", "codex,claudecode")
	t.Setenv("CHITIN_DRIVER_ALLOW_IMPL", "")
	t.Setenv("CHITIN_DRIVER_ALLOW_REVIEW", "")

	implRegistry, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry(impl): %v", err)
	}
	reviewRegistry, err := buildRegistry("review")
	if err != nil {
		t.Fatalf("buildRegistry(review): %v", err)
	}

	assertRegistryIDs(t, implRegistry, []string{"codex", "claudecode"})
	assertRegistryIDs(t, reviewRegistry, []string{"codex", "claudecode"})
}

func assertRegistryIDs(t *testing.T, registry *driver.Registry, want []string) {
	t.Helper()
	if registry.Len() != len(want) {
		t.Fatalf("registry.Len() = %d, want %d", registry.Len(), len(want))
	}
	for _, id := range want {
		if _, ok := registry.Driver(id); !ok {
			t.Fatalf("registry missing driver %q", id)
		}
	}
}
