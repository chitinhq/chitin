// Package blob stores large driver outputs outside Temporal activity results.
//
// Driver results continue to carry strings: small bodies are kept inline, and
// large bodies are replaced with content-addressed blob://sha256/<hex>
// references. Consumers can call Resolve unconditionally to turn either shape
// back into bytes.
package blob

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	schemePrefix = "blob://sha256/"
	hashHexLen   = 64
)

// Ref is a content-addressed blob URI. Its zero value is the empty reference.
type Ref string

// NewRef builds a Ref from a SHA-256 hex digest.
func NewRef(sha256Hex string) (Ref, error) {
	if err := validateSHA256Hex(sha256Hex); err != nil {
		return "", err
	}
	return Ref(schemePrefix + strings.ToLower(sha256Hex)), nil
}

// ParseRef validates s as a blob://sha256/<hex> reference.
func ParseRef(s string) (Ref, error) {
	if s == "" {
		return "", nil
	}
	if !strings.HasPrefix(s, schemePrefix) {
		return "", fmt.Errorf("blob: ref %q missing %q prefix", s, schemePrefix)
	}
	if err := validateSHA256Hex(strings.TrimPrefix(s, schemePrefix)); err != nil {
		return "", err
	}
	return Ref(strings.ToLower(s)), nil
}

// String returns the URI form of r.
func (r Ref) String() string { return string(r) }

// SHA256 returns the lowercase SHA-256 hex digest embedded in r.
func (r Ref) SHA256() (string, error) {
	if r == "" {
		return "", nil
	}
	ref, err := ParseRef(string(r))
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(string(ref), schemePrefix), nil
}

// IsRef reports whether s has the blob URI prefix. It does not validate the
// digest; Resolve performs validation before reading.
func IsRef(s string) bool {
	return strings.HasPrefix(s, schemePrefix)
}

// Store is the durable content-addressed blob storage contract.
type Store interface {
	Put(ctx context.Context, body []byte) (Ref, error)
	Get(ctx context.Context, ref Ref) ([]byte, error)
}

func validateSHA256Hex(s string) error {
	if len(s) != hashHexLen {
		return fmt.Errorf("blob: sha256 digest length = %d, want %d", len(s), hashHexLen)
	}
	if _, err := hex.DecodeString(s); err != nil {
		return fmt.Errorf("blob: invalid sha256 digest %q: %w", s, err)
	}
	return nil
}
