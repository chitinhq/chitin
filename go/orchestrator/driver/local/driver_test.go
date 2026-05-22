package local

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

func TestCardDeclaresLocalLLMContract(t *testing.T) {
	d := New(WithBaseURL("http://127.0.0.1:8080"))
	card := d.Card()

	if d.ID() != "local" {
		t.Fatalf("ID() = %q, want local", d.ID())
	}
	if card.DriverID != d.ID() {
		t.Fatalf("card DriverID = %q, want %q", card.DriverID, d.ID())
	}
	if card.Tier != driver.TierLocal {
		t.Fatalf("tier = %s, want local", card.Tier)
	}
	if card.CostClass != driver.CostFree {
		t.Fatalf("cost class = %s, want free", card.CostClass)
	}
	for _, cap := range []driver.Capability{driver.CapCodeImplement, driver.CapCodeRefactor, driver.CapBulkCodegen} {
		if !card.HasCapability(cap) {
			t.Fatalf("card missing capability %q", cap)
		}
	}
	if !card.Constraints.WorktreeRequired {
		t.Fatalf("constraints = %+v, want governed worktree required", card.Constraints)
	}
	if card.Constraints.QuotaBounded {
		t.Fatalf("constraints = %+v, want a non-quota-bounded local driver", card.Constraints)
	}
}

func TestReadyReportsUnconfiguredEndpoint(t *testing.T) {
	d := New(WithBaseURL(""))

	ready, reason := d.Ready(context.Background())
	if ready {
		t.Fatal("Ready() = true for an unconfigured endpoint, want false")
	}
	if !strings.Contains(reason, baseURLEnv) {
		t.Fatalf("Ready() reason = %q, want it to name the %s env var", reason, baseURLEnv)
	}
}

func TestReadyReportsUnreachableEndpoint(t *testing.T) {
	// A reserved-for-documentation address that does not accept connections;
	// a short client timeout keeps the unreachable probe from idling.
	d := New(
		WithBaseURL("http://192.0.2.1:9"),
		WithHTTPClient(&http.Client{Timeout: 500 * time.Millisecond}),
	)

	ready, reason := d.Ready(context.Background())
	if ready {
		t.Fatal("Ready() = true for an unreachable endpoint, want false")
	}
	if !strings.Contains(reason, "not reachable") {
		t.Fatalf("Ready() reason = %q, want it to explain the endpoint was not reachable", reason)
	}
}

func TestReadyReportsReachableEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("probe path = %q, want /v1/models", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := New(WithBaseURL(srv.URL))
	ready, reason := d.Ready(context.Background())
	if !ready {
		t.Fatalf("Ready() = false for a reachable endpoint, reason = %q", reason)
	}
	if reason != "" {
		t.Fatalf("Ready() reason = %q, want empty on the happy path", reason)
	}
}
