package main

import (
	"os"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

func TestBuildRegistry_UsesRoleSpecificAllowlists(t *testing.T) {
	clearDriverAllowEnv(t)
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
	assertRegistryIDs(t, reviewRegistry, []string{"claudecode", "codex"})
}

func TestBuildRegistry_RoleAllowlistFallsBackToLegacyAllowlist(t *testing.T) {
	clearDriverAllowEnv(t)
	t.Setenv("CHITIN_DRIVER_ALLOW", "codex claudecode")

	implRegistry, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry(impl): %v", err)
	}
	reviewRegistry, err := buildRegistry("review")
	if err != nil {
		t.Fatalf("buildRegistry(review): %v", err)
	}

	assertRegistryIDs(t, implRegistry, []string{"claudecode", "codex"})
	assertRegistryIDs(t, reviewRegistry, []string{"claudecode", "codex"})
}

func TestBuildRegistry_RoleSpecificAllowlistOverridesLegacyAllowlist(t *testing.T) {
	clearDriverAllowEnv(t)
	t.Setenv("CHITIN_DRIVER_ALLOW", "codex claudecode")
	t.Setenv("CHITIN_DRIVER_ALLOW_IMPL", "codex")

	registry, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry(impl): %v", err)
	}

	assertRegistryIDs(t, registry, []string{"codex"})
}

func TestBuildRegistry_CanRegisterClaudeCodeGLM(t *testing.T) {
	clearDriverAllowEnv(t)
	t.Setenv("CHITIN_DRIVER_ALLOW_IMPL", "claudecode-glm")

	registry, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry(impl): %v", err)
	}

	assertRegistryIDs(t, registry, []string{"claudecode-glm"})
}

func TestBuildRegistry_ImplAllowlistDoesNotBleedIntoReviewFallback(t *testing.T) {
	clearDriverAllowEnv(t)
	t.Setenv("CHITIN_DRIVER_ALLOW", "codex claudecode")
	t.Setenv("CHITIN_DRIVER_ALLOW_IMPL", "codex")

	registry, err := buildRegistry("review")
	if err != nil {
		t.Fatalf("buildRegistry(review): %v", err)
	}

	assertRegistryIDs(t, registry, []string{"claudecode", "codex"})
}

func TestBuildRegistry_UnknownRoleFails(t *testing.T) {
	if _, err := buildRegistry("audit"); err == nil {
		t.Fatal("buildRegistry(audit) err = nil, want error")
	}
}

func assertRegistryIDs(t *testing.T, registry *driver.Registry, want []string) {
	t.Helper()
	got := make([]string, 0, len(registry.Drivers()))
	for _, d := range registry.Drivers() {
		got = append(got, d.ID())
	}
	if len(got) != len(want) {
		t.Fatalf("registered IDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("registered IDs = %v, want %v", got, want)
		}
	}
}

func clearDriverAllowEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"CHITIN_DRIVER_ALLOW",
		"CHITIN_DRIVER_ALLOW_IMPL",
		"CHITIN_DRIVER_ALLOW_REVIEW",
	}
	for _, key := range keys {
		key := key
		old, existed := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(key, old)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}
