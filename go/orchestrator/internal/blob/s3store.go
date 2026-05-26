package blob

import (
	"context"
	"errors"
)

// S3Store locks the future S3-compatible blob-store shape without wiring live
// AWS calls in this spec. Client is intentionally opaque (any) so the AWS SDK
// is not pulled into the orchestrator module until the implementation lands;
// the real type lands alongside the Put/Get bodies.
type S3Store struct {
	Client any
	Bucket string
	Prefix string
}

func (s *S3Store) Put(ctx context.Context, body []byte) (Ref, error) {
	return "", errors.New("s3 blob store not implemented")
}

func (s *S3Store) Get(ctx context.Context, ref Ref) ([]byte, error) {
	return nil, errors.New("s3 blob store not implemented")
}

var _ Store = (*S3Store)(nil)
