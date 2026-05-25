package activities

import (
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// buildRegistryWith returns a Registry pre-loaded with the named drivers.
// Each driver is a ready fake with no declared capability — the pool
// resolver only looks at id presence, never at capability, so the cards
// can be empty.
func buildRegistryWith(t *testing.T, ids ...string) *driver.Registry {
	t.Helper()
	r := driver.NewRegistry()
	for _, id := range ids {
		d := &fakeDriver{id: id, card: driver.CapabilityCard{DriverID: id}}
		if err := r.Register(d); err != nil {
			t.Fatalf("registering %q: %v", id, err)
		}
	}
	return r
}

// TestResolveRereviewerDriver_DefaultPoolPicksFirstNonAuthor proves the
// happy path: with both default drivers registered and codex as author,
// the resolver returns claudecode (the next non-author entry in default
// pool order).
func TestResolveRereviewerDriver_DefaultPoolPicksFirstNonAuthor(t *testing.T) {
	t.Setenv(internalRereviewPoolEnv, "")
	reg := buildRegistryWith(t, "codex", "claudecode")
	sel := resolveRereviewerDriver(reg, "codex")
	if sel.DriverID != "claudecode" {
		t.Fatalf("DriverID = %q, want claudecode (default pool [codex, claudecode] minus author=codex)", sel.DriverID)
	}
	if sel.Reason != "" {
		t.Errorf("Reason = %q, want empty on success", sel.Reason)
	}
}

// TestResolveRereviewerDriver_DefaultPoolFirstEntryAuthorSkips proves
// that when the FIRST entry in the default pool is the author, the
// resolver advances to the next.
func TestResolveRereviewerDriver_DefaultPoolFirstEntryAuthorSkips(t *testing.T) {
	t.Setenv(internalRereviewPoolEnv, "")
	reg := buildRegistryWith(t, "codex", "claudecode")
	sel := resolveRereviewerDriver(reg, "claudecode")
	if sel.DriverID != "codex" {
		t.Fatalf("DriverID = %q, want codex (skip author=claudecode, fall through to codex)", sel.DriverID)
	}
}

// TestResolveRereviewerDriver_EnvOverridesDefault proves the env var
// wins over the default list AND that the priority order matches the
// env's declared order (first listed wins).
func TestResolveRereviewerDriver_EnvOverridesDefault(t *testing.T) {
	t.Setenv(internalRereviewPoolEnv, "claudecode,gemini,codex")
	reg := buildRegistryWith(t, "codex", "claudecode", "gemini")
	sel := resolveRereviewerDriver(reg, "")
	if sel.DriverID != "claudecode" {
		t.Fatalf("DriverID = %q, want claudecode (env order claudecode,gemini,codex)", sel.DriverID)
	}
}

// TestResolveRereviewerDriver_EnvDropsUnregistered proves the resolver
// silently skips env entries that aren't in the registry (operator
// listed a driver that didn't load).
func TestResolveRereviewerDriver_EnvDropsUnregistered(t *testing.T) {
	t.Setenv(internalRereviewPoolEnv, "phantom,codex,claudecode")
	reg := buildRegistryWith(t, "codex", "claudecode")
	sel := resolveRereviewerDriver(reg, "")
	if sel.DriverID != "codex" {
		t.Fatalf("DriverID = %q, want codex (phantom not registered, codex is first registered)", sel.DriverID)
	}
}

// TestResolveRereviewerDriver_RegistryMissingDriversYieldsNoPool proves
// the no_pool_configured reason fires when the env has a non-author
// entry but it isn't registered (the operator listed an unloaded driver).
func TestResolveRereviewerDriver_RegistryMissingDriversYieldsNoPool(t *testing.T) {
	t.Setenv(internalRereviewPoolEnv, "phantom")
	reg := buildRegistryWith(t, "codex", "claudecode")
	sel := resolveRereviewerDriver(reg, "codex")
	if sel.DriverID != "" {
		t.Fatalf("DriverID = %q, want empty", sel.DriverID)
	}
	if sel.Reason != PoolReasonNoPool {
		t.Errorf("Reason = %q, want %q", sel.Reason, PoolReasonNoPool)
	}
}

// TestResolveRereviewerDriver_EmptyAfterExclusion proves the
// empty_after_author_exclusion reason fires when every pool entry IS the
// author (single-driver operator-host with that driver authoring the
// fixup).
func TestResolveRereviewerDriver_EmptyAfterExclusion(t *testing.T) {
	t.Setenv(internalRereviewPoolEnv, "codex,codex")
	reg := buildRegistryWith(t, "codex")
	sel := resolveRereviewerDriver(reg, "codex")
	if sel.DriverID != "" {
		t.Fatalf("DriverID = %q, want empty", sel.DriverID)
	}
	if sel.Reason != PoolReasonEmptyAfterExclusion {
		t.Errorf("Reason = %q, want %q", sel.Reason, PoolReasonEmptyAfterExclusion)
	}
}

// TestResolveRereviewerDriver_NilRegistry proves the no_registry_bound
// reason fires when called with a nil registry (defensive — shouldn't
// happen in production but the resolver doesn't crash).
func TestResolveRereviewerDriver_NilRegistry(t *testing.T) {
	sel := resolveRereviewerDriver(nil, "codex")
	if sel.DriverID != "" {
		t.Fatalf("DriverID = %q, want empty", sel.DriverID)
	}
	if sel.Reason != PoolReasonNoRegistry {
		t.Errorf("Reason = %q, want %q", sel.Reason, PoolReasonNoRegistry)
	}
}

// TestConfiguredPool_TokenizesAndStrips proves the env parser strips
// whitespace and empty entries (operator may write "codex, claudecode "
// with stray spaces, expects it to work).
func TestConfiguredPool_TokenizesAndStrips(t *testing.T) {
	t.Setenv(internalRereviewPoolEnv, " codex ,, claudecode , ")
	got := configuredPool()
	want := []string{"codex", "claudecode"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestConfiguredPool_FallsBackToDefault proves an unset env yields the
// default pool — and the returned slice is a fresh copy (mutating it
// can't poison subsequent calls).
func TestConfiguredPool_FallsBackToDefault(t *testing.T) {
	t.Setenv(internalRereviewPoolEnv, "")
	got := configuredPool()
	if len(got) != len(defaultInternalRereviewPool) {
		t.Fatalf("len = %d, want %d", len(got), len(defaultInternalRereviewPool))
	}
	for i, id := range defaultInternalRereviewPool {
		if got[i] != id {
			t.Errorf("[%d] = %q, want %q", i, got[i], id)
		}
	}
	// Mutate the returned slice — the resolver should still see the
	// original default on the next call.
	got[0] = "mutated"
	again := configuredPool()
	if again[0] == "mutated" {
		t.Errorf("configuredPool() returns alias to defaultInternalRereviewPool; mutation leaked")
	}
}

// TestPoolSelection_String renders both branches.
func TestPoolSelection_String(t *testing.T) {
	cases := []struct {
		in   PoolSelection
		want string
	}{
		{PoolSelection{DriverID: "codex"}, "selected=codex"},
		{PoolSelection{Reason: PoolReasonEmptyAfterExclusion}, "empty reason=empty_after_author_exclusion"},
	}
	for _, tc := range cases {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("%+v.String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}
