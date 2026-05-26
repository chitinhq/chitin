package blob

import "testing"

func TestS3StoreSatisfiesStore(t *testing.T) {
	var _ Store = (*S3Store)(nil)
}
