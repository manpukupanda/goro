package storage

type S3 struct{}

func New() *S3 {
	return &S3{}
}
