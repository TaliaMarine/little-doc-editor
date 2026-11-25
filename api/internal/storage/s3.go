// Package storage wraps S3-compatible object storage access for attachments
// (SPEC.md Decision 5: S3-compatible only — AWS S3, MinIO, or GCS via the S3
// API). Nothing here is AWS-specific beyond the SDK used as the S3 API
// client; path-style addressing (config.S3UsePathStyle) is what makes MinIO
// and most non-AWS endpoints work.
package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
)

// Storage is the attachment blob store. Two backends implement it: S3-compatible
// object storage (the default, Decision 5) and a plain directory (a
// PersistentVolume mounted into the api pod, for small self-hosted setups
// without an object store — see NewFilesystem).
type Storage interface {
	Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes an object; deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
}

// Open returns the backend selected by cfg.StorageBackend.
func Open(ctx context.Context, cfg config.Config) (Storage, error) {
	if cfg.StorageBackend == "filesystem" {
		return NewFilesystem(cfg.StorageDir)
	}
	return New(ctx, cfg)
}

type S3Storage struct {
	client *s3.Client
	bucket string
}

func New(ctx context.Context, cfg config.Config) (*S3Storage, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.S3Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("loading S3 config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		}
		o.UsePathStyle = cfg.S3UsePathStyle
		// The SDK's newer default (WhenSupported) forces a trailing checksum
		// on every request, which requires either TLS or a seekable body.
		// Attachment uploads now pass a seekable bytes.Reader, but most
		// local/MinIO setups run over plain HTTP and some S3-compatible
		// stores reject the trailing-checksum encoding outright —
		// WhenRequired matches what MinIO/S3-compatible integration guides
		// recommend for exactly this case.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	return &S3Storage{client: client, bucket: cfg.S3Bucket}, nil
}

// Put uploads content under key, returning an error if the bucket is
// unreachable or rejects the write. Callers are responsible for validating
// content type/size before calling this — it stores whatever it's given.
func (s *S3Storage) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("uploading %q: %w", key, err)
	}
	return nil
}

// Get returns the object body for key. Callers must close the returned
// reader.
func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("fetching %q: %w", key, err)
	}
	return out.Body, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("deleting %q: %w", key, err)
	}
	return nil
}
