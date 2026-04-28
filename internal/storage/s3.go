package storage

import (
	"context"
	"io"

	"goro/internal/config"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3 struct {
	client *minio.Client
	bucket string
}

func New(cfg config.S3Config) (*S3, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, err
		}
	}

	return &S3{client: client, bucket: cfg.Bucket}, nil
}

func (s *S3) UploadFile(ctx context.Context, objectName, filePath, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err := s.client.FPutObject(ctx, s.bucket, objectName, filePath, minio.PutObjectOptions{ContentType: contentType})
	return err
}

// GetObject fetches an object from MinIO and returns a ReadCloser along with
// the object's size in bytes.  The caller is responsible for closing the
// returned ReadCloser.
func (s *S3) GetObject(ctx context.Context, objectName string) (io.ReadCloser, int64, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	stat, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, 0, err
	}
	return obj, stat.Size, nil
}

// DeleteVideoObjects removes all objects stored under videos/{publicID}/ from
// the bucket.  It lists objects with that prefix and deletes them in a single
// batch call.  No error is returned when no objects are found.
func (s *S3) DeleteVideoObjects(ctx context.Context, publicID string) error {
	prefix := "videos/" + publicID + "/"

	// Collect all object names first so that any listing error is detected
	// before we attempt deletion.
	var toDelete []minio.ObjectInfo
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return obj.Err
		}
		toDelete = append(toDelete, obj)
	}
	if len(toDelete) == 0 {
		return nil
	}

	objectsCh := make(chan minio.ObjectInfo, len(toDelete))
	for _, obj := range toDelete {
		objectsCh <- obj
	}
	close(objectsCh)

	for err := range s.client.RemoveObjects(ctx, s.bucket, objectsCh, minio.RemoveObjectsOptions{}) {
		if err.Err != nil {
			return err.Err
		}
	}
	return nil
}
