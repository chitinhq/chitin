// Package superpowers registers the Superpowers markdown adapter on import.
package superpowers

import "github.com/chitinhq/chitin/go/execution-kernel/internal/spec/adapter"

func init() {
	adapter.Register(&Adapter{})
}
