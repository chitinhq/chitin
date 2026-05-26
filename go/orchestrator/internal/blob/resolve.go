package blob

import (
	"context"
	"fmt"
	"regexp"
)

var refTokenRE = regexp.MustCompile(`blob://sha256/[0-9a-f]{64}`)

// Resolve returns literal inputs unchanged and resolves blob refs to bytes.
func Resolve(ctx context.Context, store Store, s string) ([]byte, error) {
	if !IsRef(s) {
		return []byte(s), nil
	}
	if store == nil {
		return nil, fmt.Errorf("blob: cannot resolve %q without a store", s)
	}
	ref, err := ParseRef(s)
	if err != nil {
		return nil, err
	}
	return store.Get(ctx, ref)
}

// ResolveText replaces blob ref tokens in s with their bodies.
func ResolveText(ctx context.Context, store Store, s string) (string, error) {
	return ResolveTextWithCap(ctx, store, s, 0)
}

// ResolveTextWithCap is like ResolveText but truncates each resolved blob body
// to capBytes, replacing the tail with a hint pointing back at the original
// ref so an operator can open the full body manually. capBytes <= 0 disables
// truncation. Used for size-bounded sinks (e.g. Discord summaries) so a
// multi-MiB transcript does not allocate a giant string only to be truncated
// downstream.
func ResolveTextWithCap(ctx context.Context, store Store, s string, capBytes int) (string, error) {
	var firstErr error
	out := refTokenRE.ReplaceAllStringFunc(s, func(tok string) string {
		if firstErr != nil {
			return tok
		}
		body, err := Resolve(ctx, store, tok)
		if err != nil {
			firstErr = err
			return tok
		}
		if capBytes > 0 && len(body) > capBytes {
			return string(body[:capBytes]) + "… (truncated; full body at " + tok + ")"
		}
		return string(body)
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}
