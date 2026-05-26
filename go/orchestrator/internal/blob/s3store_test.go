package blob

func compileS3Store() {
	var _ Store = (*S3Store)(nil)
}
