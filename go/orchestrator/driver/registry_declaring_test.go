// registry_declaring_test.go — spec 105 FR-003 tests for the
// DriversDeclaring(cap) method: returns drivers whose CARD declares
// the capability, without filtering on operational Ready state.
//
// Distinction from DriversFor:
//   DriversFor:        cap declared + Ready (operational dispatch shortlist)
//   DriversDeclaring:  cap declared only (taxonomic coverage check)
//
// The validator (ValidateForDispatch) should use DriversDeclaring — a
// non-Ready driver at scheduling time may be Ready by the time the
// work hits its activity slot. Coverage is taxonomic.

package driver_test

import (
	"context"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// declaringFakeDriver lets tests set Ready independently of the card.
type declaringFakeDriver struct {
	id          string
	caps        []driver.Capability
	readyResult bool
}

func (d *declaringFakeDriver) ID() string { return d.id }
func (d *declaringFakeDriver) Card() driver.CapabilityCard {
	return driver.CapabilityCard{
		DriverID:     d.id,
		AgentRuntime: d.id,
		Model:        d.id,
		Capabilities: d.caps,
		Tier:         driver.TierFrontier,
		CostClass:    driver.CostMedium,
	}
}
func (d *declaringFakeDriver) Ready(ctx context.Context) (bool, string) {
	return d.readyResult, ""
}
func (d *declaringFakeDriver) Invoke(ctx context.Context, _ driver.WorkUnit) (driver.Result, error) {
	return driver.Result{}, nil
}

func TestDriversDeclaring_ReturnsAllDeclarers_IgnoresReady(t *testing.T) {
	reg := driver.NewRegistry()
	a := &declaringFakeDriver{id: "a", caps: []driver.Capability{driver.CapDocsWrite}, readyResult: true}
	b := &declaringFakeDriver{id: "b", caps: []driver.Capability{driver.CapDocsWrite}, readyResult: false}
	c := &declaringFakeDriver{id: "c", caps: []driver.Capability{driver.CapCodeImplement}, readyResult: true}
	for _, d := range []driver.AgentDriver{a, b, c} {
		if err := reg.Register(d); err != nil {
			t.Fatalf("register %s: %v", d.ID(), err)
		}
	}

	got := reg.DriversDeclaring(driver.CapDocsWrite)
	if len(got) != 2 {
		t.Fatalf("got %d declarers; want 2 (a and b regardless of Ready)", len(got))
	}
	ids := []string{got[0].ID(), got[1].ID()}
	if ids[0] != "a" || ids[1] != "b" {
		t.Errorf("declarers in unexpected order: %v; want [a b]", ids)
	}
}

func TestDriversDeclaring_ReturnsEmpty_WhenNoDeclarers(t *testing.T) {
	reg := driver.NewRegistry()
	d := &declaringFakeDriver{id: "x", caps: []driver.Capability{driver.CapCodeImplement}, readyResult: true}
	_ = reg.Register(d)

	got := reg.DriversDeclaring(driver.CapTestAuthor)
	if len(got) != 0 {
		t.Errorf("got %d declarers; want 0 (no driver declares test.author)", len(got))
	}
}

func TestDriversFor_StillFiltersOnReady_RegressionGuard(t *testing.T) {
	// DriversFor (operational shortlist for SelectDriver) MUST keep
	// filtering on Ready — that semantic is unchanged.
	reg := driver.NewRegistry()
	a := &declaringFakeDriver{id: "a", caps: []driver.Capability{driver.CapDocsWrite}, readyResult: true}
	b := &declaringFakeDriver{id: "b", caps: []driver.Capability{driver.CapDocsWrite}, readyResult: false}
	_ = reg.Register(a)
	_ = reg.Register(b)

	got := reg.DriversFor(context.Background(), driver.CapDocsWrite)
	if len(got) != 1 || got[0].ID() != "a" {
		t.Errorf("DriversFor returned %v; want [a] only (b not Ready)", got)
	}
}
