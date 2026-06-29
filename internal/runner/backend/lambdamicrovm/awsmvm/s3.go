package awsmvm

import (
	"bytes"
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3API is the subset of the S3 client the stager uses (kept small so the
// stager can be unit-tested against a fake).
type s3API interface {
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// s3Presigner is the subset of the S3 presign client the stager uses.
type s3Presigner interface {
	PresignGetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*signedRequest, error)
}

// S3Stager stages task spec bundles in S3 and hands out presigned GET URLs the
// in-VM shim fetches without credentials. It implements lambdamicrovm.Stager.
// The bucket is per-workspace and passed per call.
type S3Stager struct {
	client    s3API
	presigner s3Presigner
}

// NewS3Stager builds a stager from an AWS config.
func NewS3Stager(cfg aws.Config) *S3Stager {
	client := s3.NewFromConfig(cfg)
	return &S3Stager{
		client:    client,
		presigner: presignAdapter{s3.NewPresignClient(client)},
	}
}

// Stage PUTs data under key and returns a presigned GET URL valid for ttlSeconds.
func (s *S3Stager) Stage(ctx context.Context, bucket, key string, data []byte, ttlSeconds int64) (string, error) {
	if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	}); err != nil {
		return "", err
	}
	req, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(time.Duration(ttlSeconds)*time.Second))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// Remove deletes a staged object. Best-effort cleanup.
func (s *S3Stager) Remove(ctx context.Context, bucket, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

// signedRequest mirrors the fields of the SDK's presign result the stager needs,
// so the presigner can be faked in tests.
type signedRequest struct {
	URL string
}

// presignAdapter adapts the SDK presign client to s3Presigner.
type presignAdapter struct{ c *s3.PresignClient }

func (a presignAdapter) PresignGetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*signedRequest, error) {
	out, err := a.c.PresignGetObject(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return &signedRequest{URL: out.URL}, nil
}
