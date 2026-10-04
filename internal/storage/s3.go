package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config describes an S3-compatible bucket (AWS S3, MinIO, Cloudflare R2, Backblaze B2, ...).
type S3Config struct {
	Endpoint  string // "s3.amazonaws.com", "minio:9000" or a full URL
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	Prefix    string // optional folder inside the bucket
	Insecure  bool   // plain HTTP (a MinIO on your own network)
	PathStyle bool   // bucket in the path rather than the host name (MinIO, most self-hosted)
}

// S3 stores blobs in a bucket. Keys are content hashes, exactly as on disk, so a
// bucket can be copied to a disk (or the other way round) without changing the database.
type S3 struct {
	c      *minio.Client
	bucket string
	prefix string
	tmp    *FS // local scratch space for uploads in progress
}

// NewS3 connects to the bucket and creates it when it doesn't exist yet.
func NewS3(ctx context.Context, cfg S3Config, tmp *FS) (*S3, error) {
	endpoint, insecure := cfg.Endpoint, cfg.Insecure
	switch {
	case strings.HasPrefix(endpoint, "https://"):
		endpoint = strings.TrimPrefix(endpoint, "https://")
	case strings.HasPrefix(endpoint, "http://"):
		endpoint, insecure = strings.TrimPrefix(endpoint, "http://"), true
	}
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" || cfg.Bucket == "" {
		return nil, errors.New("S3 storage needs DOCVETA_S3_ENDPOINT and DOCVETA_S3_BUCKET")
	}
	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	c, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: !insecure, Region: cfg.Region, BucketLookup: lookup})
	if err != nil {
		return nil, fmt.Errorf("S3: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ok, err := c.BucketExists(cctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("S3: can't reach bucket %q at %s: %w", cfg.Bucket, endpoint, err)
	}
	if !ok {
		if err := c.MakeBucket(cctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("S3: bucket %q doesn't exist and couldn't be created: %w", cfg.Bucket, err)
		}
	}
	p := strings.Trim(cfg.Prefix, "/")
	if p != "" {
		p += "/"
	}
	return &S3{c: c, bucket: cfg.Bucket, prefix: p, tmp: tmp}, nil
}

func (s *S3) object(key string) string { return s.prefix + key }

func isMissing(err error) bool {
	if err == nil {
		return false
	}
	code := minio.ToErrorResponse(err).Code
	return code == "NoSuchKey" || code == "NoSuchBucket" || code == "NotFound"
}

func (s *S3) Stage() (*Staged, error) { return s.tmp.NewStaged(s.commit) }

// commit uploads the staged file (multipart for big ones, handled by the client) and
// removes the local copy. Content that is already in the bucket isn't uploaded again.
func (s *S3) commit(key, tmpPath string) error {
	defer os.Remove(tmpPath)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	if _, err := s.c.StatObject(ctx, s.bucket, s.object(key), minio.StatObjectOptions{}); err == nil {
		return nil
	}
	_, err := s.c.FPutObject(ctx, s.bucket, s.object(key), tmpPath, minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

func (s *S3) Open(ctx context.Context, key string) (io.ReadSeekCloser, int64, error) {
	if !validKey(key) {
		return nil, 0, ErrNotFound
	}
	obj, err := s.c.GetObject(ctx, s.bucket, s.object(key), minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	st, err := obj.Stat() // GetObject is lazy: this is where a missing object shows up
	if err != nil {
		obj.Close()
		if isMissing(err) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	return obj, st.Size, nil
}

func (s *S3) Exists(ctx context.Context, key string) (bool, error) {
	if !validKey(key) {
		return false, nil
	}
	_, err := s.c.StatObject(ctx, s.bucket, s.object(key), minio.StatObjectOptions{})
	if isMissing(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return ErrNotFound
	}
	err := s.c.RemoveObject(ctx, s.bucket, s.object(key), minio.RemoveObjectOptions{})
	if isMissing(err) {
		return nil
	}
	return err
}

func (s *S3) Walk(ctx context.Context, fn func(key string, mod time.Time) error) error {
	for obj := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s.object("sha256/"), Recursive: true}) {
		if obj.Err != nil {
			return obj.Err
		}
		if err := fn(strings.TrimPrefix(obj.Key, s.prefix), obj.LastModified); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// FreeBytes: a bucket has no meaningful free space.
func (s *S3) FreeBytes() int64 { return -1 }
