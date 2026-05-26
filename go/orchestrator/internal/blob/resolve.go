package blob

import (
	"context"
	"fmt"
)

// Resolve returns s as bytes unless it is a blob URI, in which case it reads
// the referenced body from store.
func Resolve(ctx context.Context, store Store, s string) ([]byte, error) {
	if !IsRef(s) {
		return []byte(s), nil
	}
	ref, err := ParseRef(s)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, fmt.Errorf("blob: cannot resolve %s without a store", ref)
	}
	return store.Get(ctx, ref)
}
