package storage

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"wsapi-video/internal/config"
)

type S3 struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

func newS3(c config.Config) *S3 {
	client := s3.NewFromConfig(aws.Config{Region: c.S3Region, Credentials: credentials.NewStaticCredentialsProvider(c.S3AccessKey, c.S3SecretKey, "")}, func(o *s3.Options) {
		o.UsePathStyle = true
		if c.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(c.S3Endpoint)
		}
	})
	return &S3{client: client, presign: s3.NewPresignClient(client), bucket: c.S3Bucket}
}
func (s *S3) Put(ctx context.Context, key string, r io.Reader, contentType string) error {
	// SDK checksums/retries need a seekable body; large media stays off the heap.
	f, e := os.CreateTemp("", "studio-s3-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, e := io.Copy(f, r)
	if e != nil {
		return e
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return e
	}
	_, e = s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), Body: f, ContentLength: aws.Int64(n), ContentType: aws.String(contentType)})
	return e
}
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, e := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if e != nil {
		return nil, e
	}
	return out.Body, nil
}
func (s *S3) SignedURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	out, e := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}, func(o *s3.PresignOptions) { o.Expires = expires })
	if e != nil {
		return "", e
	}
	return out.URL, nil
}
func (s *S3) Delete(ctx context.Context, key string) error {
	_, e := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return e
}
