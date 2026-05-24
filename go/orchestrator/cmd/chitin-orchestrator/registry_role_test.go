package main

import "testing"

func TestBuildRegistry_RoleAllowlistsAreIndependent(t *testing.T) {
	t.Setenv("CHITIN_DRIVER_ALLOW", "")
	t.Setenv("CHITIN_DRIVER_ALLOW_IMPL", "codex")
	t.Setenv("CHITIN_DRIVER_ALLOW_REVIEW", "codex,claudecode")

	implRegistry, err := buildRegistryForRole(registryRoleImpl)
	if err != nil {
		t.Fatalf("build impl registry: %v", err)
	}
	reviewRegistry, err := buildRegistryForRole(registryRoleReview)
	if err != nil {
		t.Fatalf("build review registry: %v", err)
	}

	if got := implRegistry.Len(); got != 1 {
		t.Fatalf("impl registry has %d drivers, want 1", got)
	}
	if got := reviewRegistry.Len(); got != 2 {
		t.Fatalf("review registry has %d drivers, want 2", got)
	}
}
