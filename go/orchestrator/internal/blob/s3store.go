package blob

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Store locks the future S3-compatible blob-store shape without wiring live
// AWS calls in this spec.
type S3Store struct {
	Client *s3.Client
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
