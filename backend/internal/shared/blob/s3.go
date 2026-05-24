package blob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
)

// isNotFound returns true for the 404 / NoSuchKey / NotFound family
// of S3 errors. Multiple typed shapes are emitted depending on the
// op (HeadObject → types.NotFound, GetObject → types.NoSuchKey,
// HeadBucket → types.NotFound or a 404 APIError); collapsing them
// here keeps the call sites tidy.
func isNotFound(err error) bool {
	var nf *types.NotFound
	var nsk *types.NoSuchKey
	var nsb *types.NoSuchBucket
	if errors.As(err, &nf) || errors.As(err, &nsk) || errors.As(err, &nsb) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey", "NoSuchBucket", "404":
			return true
		}
	}
	return false
}

// S3Config captures the connection params an S3-compatible store
// needs. Endpoint + ForcePathStyle are the knobs that make this work
// against RustFS / MinIO / Backblaze in addition to AWS S3 — leave
// Endpoint empty for native AWS.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKey       string
	SecretKey       string
	ForcePathStyle  bool
	EnsureBucket    bool
	RequestPresigns bool
}

// s3Store is the AWS SDK v2 implementation of Store. Pinned to one
// bucket per process — multi-bucket setups should construct one Store
// per bucket.
type s3Store struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
}

// NewS3 constructs a Store backed by an S3-compatible endpoint. When
// cfg.EnsureBucket is true the constructor probes the bucket and
// attempts a CreateBucket on miss — useful for self-hosted RustFS /
// MinIO where the operator may not have pre-created it. AWS-side
// constructors should leave EnsureBucket=false because IAM rarely
// grants CreateBucket to the service role.
func NewS3(ctx context.Context, cfg S3Config) (Store, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("blob: bucket is required")
	}
	region := cfg.Region
	if region == "" {
		// RustFS / MinIO don't care about region but the SDK rejects
		// an empty value at sign time; pick a sane placeholder so
		// self-hosted deploys don't have to fill in a meaningless var.
		region = "us-east-1"
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey, cfg.SecretKey, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("blob: load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.ForcePathStyle
	})

	s := &s3Store{
		client:    client,
		presigner: s3.NewPresignClient(client),
		bucket:    cfg.Bucket,
	}

	if cfg.EnsureBucket {
		if err := s.ensureBucket(ctx); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// ensureBucket is best-effort: a 404 / NotFound triggers a create.
// Any other error (auth, network) propagates so the operator sees a
// real failure on boot rather than silent fallback. Re-running against
// an already-existing bucket is a no-op (the SDK returns
// BucketAlreadyOwnedByYou which we treat as success).
func (s *s3Store) ensureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}
	if !isNotFound(err) {
		return fmt.Errorf("blob: head bucket %q: %w", s.bucket, err)
	}
	_, createErr := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	if createErr == nil {
		return nil
	}
	var owned *types.BucketAlreadyOwnedByYou
	var taken *types.BucketAlreadyExists
	if errors.As(createErr, &owned) || errors.As(createErr, &taken) {
		return nil
	}
	return fmt.Errorf("blob: create bucket %q: %w", s.bucket, createErr)
}

func (s *s3Store) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (*PresignedPut, error) {
	if key == "" {
		return nil, errors.New("blob: key is required")
	}
	if contentType == "" {
		return nil, errors.New("blob: contentType is required")
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	in := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}
	req, err := s.presigner.PresignPutObject(ctx, in, func(opts *s3.PresignOptions) {
		opts.Expires = ttl
	})
	if err != nil {
		return nil, fmt.Errorf("blob: presign put: %w", err)
	}
	headers := map[string]string{
		"Content-Type": contentType,
	}
	return &PresignedPut{
		URL:       req.URL,
		Headers:   headers,
		Key:       key,
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}

func (s *s3Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", errors.New("blob: key is required")
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	in := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	req, err := s.presigner.PresignGetObject(ctx, in, func(opts *s3.PresignOptions) {
		opts.Expires = ttl
	})
	if err != nil {
		return "", fmt.Errorf("blob: presign get: %w", err)
	}
	return req.URL, nil
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		return nil
	}
	if isNotFound(err) {
		return nil
	}
	return fmt.Errorf("blob: delete %q: %w", key, err)
}

func (s *s3Store) Exists(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("blob: head %q: %w", key, err)
}
