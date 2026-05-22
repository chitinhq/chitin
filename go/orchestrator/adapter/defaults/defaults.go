// Package defaults wires the concrete spec-kit adapters into an
// adapter.Registry. It lives in its own sub-package so the generic
// adapter.Registry (which every kit sub-package imports) does not itself
// import the kit sub-packages — that would be an import cycle. The scheduler
// (spec 076) obtains a ready registry by calling Registry here; adding a kit
// is one new import and one Register line in this file and nothing else
// (FR-002, SC-001).
package defaults

import (
	"fmt"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/adapter/openspec"
	"github.com/chitinhq/chitin/go/orchestrator/adapter/speckit"
)

// Registry returns an adapter.Registry with every shipped kit adapter
// registered — the GitHub spec-kit adapter and the OpenSpec adapter. A new
// kit is added by importing its sub-package and adding one Register call
// below; the scheduler never changes (FR-002).
func Registry() (*adapter.Registry, error) {
	reg := adapter.NewRegistry()
	for _, a := range []adapter.SpecKitAdapter{
		speckit.New(),
		openspec.New(),
	} {
		if err := reg.Register(a); err != nil {
			return nil, fmt.Errorf("defaults: registering %s adapter: %w", a.Kit(), err)
		}
	}
	return reg, nil
}
