package blob

import (
	"context"
	"errors"
)

// S3Store is the future S3-compatible blob store implementation. The AWS SDK
// is intentionally NOT imported here yet — the real S3 wiring will pull it in
// when implemented behind a build tag or sibling file so non-S3 builds don't
// pay the dependency cost.
type S3Store struct {
	Bucket   string
	Prefix   string
	Region   string
	Endpoint string
}

// Put satisfies Store. Live S3 wiring is intentionally deferred.
func (s *S3Store) Put(ctx context.Context, body []byte) (Ref, error) {
	return Ref{}, errors.New("s3 blob store not implemented")
}

// Get satisfies Store. Live S3 wiring is intentionally deferred.
func (s *S3Store) Get(ctx context.Context, ref Ref) ([]byte, error) {
	return nil, errors.New("s3 blob store not implemented")
}

var _ Store = (*S3Store)(nil)
