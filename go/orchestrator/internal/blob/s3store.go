package blob

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// S3Store is the future S3-compatible blob store implementation.
type S3Store struct {
	Config aws.Config
	Bucket string
	Prefix string
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
