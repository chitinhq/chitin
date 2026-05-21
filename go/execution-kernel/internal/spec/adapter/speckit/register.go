// Package speckit registers the spec-kit/house adapter on import.
// Import with a blank identifier to trigger the init function:
//
//	import _ "github.com/chitinhq/chitin/go/execution-kernel/internal/spec/adapter/speckit"
package speckit

import "github.com/chitinhq/chitin/go/execution-kernel/internal/spec/adapter"

func init() {
	adapter.Register(&Adapter{})
}