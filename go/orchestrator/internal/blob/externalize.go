package blob

import "context"

// InlineThreshold is the maximum body size carried inline in a driver result.
const InlineThreshold = 1 << 20

// Externalize applies the inline-vs-blob policy for driver result fields.
func Externalize(ctx context.Context, store Store, body []byte) (string, error) {
	if len(body) <= InlineThreshold || store == nil {
		return string(body), nil
	}
	ref, err := store.Put(ctx, body)
	if err != nil {
		return "", err
	}
	return ref.String(), nil
}
