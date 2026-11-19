// Package storage keeps uploaded files in any S3-compatible object storage
// (SeaweedFS in Docker Compose, S3 or GCS in production).
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var ErrNotFound = errors.New("object not found")

type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type S3 struct {
	client *minio.Client
	bucket string
}

// Connect opens the client and creates the bucket on first start.
func Connect(ctx context.Context, c Config) (*S3, error) {
	client, err := minio.New(c.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure: c.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("object storage client: %w", err)
	}
	exists, err := client.BucketExists(ctx, c.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, c.Bucket, minio.MakeBucketOptions{}); err != nil {
			// Another replica may have created it meanwhile.
			if exists, _ := client.BucketExists(ctx, c.Bucket); !exists {
				return nil, fmt.Errorf("create bucket: %w", err)
			}
		}
	}
	return &S3{client: client, bucket: c.Bucket}, nil
}

func (s *S3) Put(ctx context.Context, key, contentType string, data []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *S3) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

// Object is an open stored file; the caller closes Body.
type Object struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64
}

func (s *S3) Get(ctx context.Context, key string) (Object, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return Object{}, err
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return Object{}, ErrNotFound
		}
		return Object{}, err
	}
	return Object{Body: obj, ContentType: info.ContentType, Size: info.Size}, nil
}
