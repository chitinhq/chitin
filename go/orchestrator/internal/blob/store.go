// Package blob stores large driver outputs out-of-band while preserving the
// existing workflow contract.
//
// Driver Result fields remain strings. Small outputs stay inline as literal
// strings; larger outputs are written through Store and represented by a
// content-addressed blob://sha256/<hex> reference. Consumers can call Resolve
// unconditionally: literals pass through unchanged, blob refs load the full
// body from the configured store.
package blob

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	refPrefix = "blob://sha256/"
	hashLen   = 64
)

var sha256HexRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Store persists and retrieves content-addressed blobs.
type Store interface {
	Put(ctx context.Context, body []byte) (Ref, error)
	Get(ctx context.Context, ref Ref) ([]byte, error)
}

// Ref wraps a blob://sha256/<hex> URI. Its zero value is the empty reference.
type Ref struct {
	uri string
}

// ParseRef validates and returns a blob reference.
func ParseRef(s string) (Ref, error) {
	if s == "" {
		return Ref{}, nil
	}
	hash, ok := strings.CutPrefix(s, refPrefix)
	if !ok {
		return Ref{}, fmt.Errorf("blob: ref %q missing %q prefix", s, refPrefix)
	}
	if !sha256HexRE.MatchString(hash) {
		return Ref{}, fmt.Errorf("blob: ref %q has invalid sha256 hex", s)
	}
	return Ref{uri: s}, nil
}

// RefFromHash returns a Ref for a lowercase SHA-256 hex digest.
func RefFromHash(hash string) (Ref, error) {
	if !sha256HexRE.MatchString(hash) {
		return Ref{}, fmt.Errorf("blob: invalid sha256 hex %q", hash)
	}
	return Ref{uri: refPrefix + hash}, nil
}

// String renders the reference URI.
func (r Ref) String() string { return r.uri }

// IsZero reports whether r is the empty reference.
func (r Ref) IsZero() bool { return r.uri == "" }

// Hash returns the SHA-256 hex digest embedded in r.
func (r Ref) Hash() (string, error) {
	if r.uri == "" {
		return "", errors.New("blob: empty ref")
	}
	ref, err := ParseRef(r.uri)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(ref.uri, refPrefix), nil
}

// IsRef reports whether s starts with the blob reference prefix.
func IsRef(s string) bool {
	return strings.HasPrefix(s, refPrefix)
}
