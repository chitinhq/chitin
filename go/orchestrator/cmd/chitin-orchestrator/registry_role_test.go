package main

import (
	"os"
	"testing"
)

func TestBuildRegistry_LegacyAllowlistAppliesToBothRoles(t *testing.T) {
	t.Setenv("CHITIN_DRIVER_ALLOW", "codex,claudecode")
	unsetEnvForTest(t, "CHITIN_DRIVER_ALLOW_IMPL")
	unsetEnvForTest(t, "CHITIN_DRIVER_ALLOW_REVIEW")

	implRegistry, err := buildRegistry("impl")
	if err != nil {
		t.Fatalf("buildRegistry(impl): %v", err)
	}
	reviewRegistry, err := buildRegistry("review")
	if err != nil {
		t.Fatalf("buildRegistry(review): %v", err)
	}

	if implRegistry.Len() != 2 {
		t.Fatalf("impl registry len=%d want 2", implRegistry.Len())
	}
	if reviewRegistry.Len() != 2 {
		t.Fatalf("review registry len=%d want 2", reviewRegistry.Len())
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	orig, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if ok {
			if err := os.Setenv(key, orig); err != nil {
				t.Fatalf("restore %s: %v", key, err)
			}
		} else if err := os.Unsetenv(key); err != nil {
			t.Fatalf("restore unset %s: %v", key, err)
		}
	})
}
