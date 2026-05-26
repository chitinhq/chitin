package blob

import (
	"context"
	"fmt"
)

// InlineThreshold is the maximum body size kept inline in Result fields.
const InlineThreshold = 1_048_576

// Externalize applies the inline-vs-blob policy for driver result strings.
func Externalize(ctx context.Context, store Store, body []byte) (string, error) {
	if len(body) <= InlineThreshold || store == nil {
		return string(body), nil
	}
	ref, err := store.Put(ctx, body)
	if err != nil {
		return "", fmt.Errorf("blob: externalize: %w", err)
	}
	return ref.String(), nil
}
