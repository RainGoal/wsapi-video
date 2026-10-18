package storage

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"wsapi-video/internal/config"
)

// Dynamic resolves the current private storage credentials on each operation.
// The settings service forbids changing the location once any asset exists.
type Dynamic struct {
	load  func(context.Context) (config.Config, error)
	lock  func(context.Context, config.Config) error
	mu    sync.Mutex
	key   [32]byte
	store Store
}

func NewDynamic(load func(context.Context) (config.Config, error), lock ...func(context.Context, config.Config) error) *Dynamic {
	d := &Dynamic{load: load}
	if len(lock) > 0 {
		d.lock = lock[0]
	}
	return d
}
func (d *Dynamic) current(ctx context.Context) (Store, error) {
	c, err := d.load(ctx)
	if err != nil {
		return nil, err
	}
	if c.StorageDriver == "s3" && (c.S3Bucket == "" || c.S3AccessKey == "" || c.S3SecretKey == "") {
		return nil, errors.New("private storage is not configured")
	}
	if d.lock != nil {
		if err = d.lock(ctx, c); err != nil {
			return nil, err
		}
	}
	key := sha256.Sum256([]byte(c.StorageDriver + "\x00" + c.StorageDir + "\x00" + c.PublicURL + "\x00" + c.AppSecret + "\x00" + c.S3Endpoint + "\x00" + c.S3Region + "\x00" + c.S3Bucket + "\x00" + c.S3AccessKey + "\x00" + c.S3SecretKey))
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.store != nil && d.key == key {
		return d.store, nil
	}
	store, err := New(c)
	if err != nil {
		return nil, err
	}
	d.store = store
	d.key = key
	return store, nil
}
func (d *Dynamic) Put(ctx context.Context, k string, r io.Reader, t string) error {
	s, e := d.current(ctx)
	if e != nil {
		return e
	}
	return s.Put(ctx, k, r, t)
}
func (d *Dynamic) Open(ctx context.Context, k string) (io.ReadCloser, error) {
	s, e := d.current(ctx)
	if e != nil {
		return nil, e
	}
	return s.Open(ctx, k)
}
func (d *Dynamic) SignedURL(ctx context.Context, k string, t time.Duration) (string, error) {
	s, e := d.current(ctx)
	if e != nil {
		return "", e
	}
	return s.SignedURL(ctx, k, t)
}
func (d *Dynamic) Delete(ctx context.Context, k string) error {
	s, e := d.current(ctx)
	if e != nil {
		return e
	}
	return s.Delete(ctx, k)
}

// Check is intentionally read-only: HeadBucket cannot prove Put/Get/Delete permissions.
func Check(ctx context.Context, c config.Config) error {
	if c.StorageDriver == "local" {
		info, err := os.Stat(c.StorageDir)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("local storage root is not a directory")
		}
		return nil
	}
	s := newS3(c)
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.S3Bucket)})
	return err
}
