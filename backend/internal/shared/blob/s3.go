package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	Endpoint string
	// PublicEndpoint, when set, is the browser-reachable endpoint baked into
	// presigned PUT/GET URLs — leaving Endpoint free to be a docker-internal
	// host (e.g. http://rustfs:9000) the SPA can't reach. Server-side ops
	// (Put/Get/Delete/HEAD/ensureBucket) always use Endpoint. This split
	// matters behind a TLS-terminating proxy (Cloudflare): presigned URLs sign
	// only `host` and survive proxying, whereas SDK-signed requests sign more
	// headers and 403 through the proxy — so they must hit the origin directly.
	// Empty → presigns use Endpoint (single-endpoint deploys, unchanged).
	PublicEndpoint  string
	Region          string
	Bucket          string
	AccessKey       string
	SecretKey       string
	ForcePathStyle  bool
	EnsureBucket    bool
	RequestPresigns bool
	// CORSAllowedOrigins are the browser origins permitted to PUT/GET this
	// bucket directly. A presigned browser upload is a cross-origin request
	// to the storage host, and only the bucket's own CORS policy can allow
	// it — the backend is not in that request's path and cannot add the
	// header. Empty (the default) means the policy is left untouched, so
	// deployments whose IAM lacks s3:PutBucketCORS are unaffected.
	CORSAllowedOrigins []string
}

// s3Store is the AWS SDK v2 implementation of Store. Pinned to one
// bucket per process — multi-bucket setups should construct one Store
// per bucket.
type s3Store struct {
	client      *s3.Client
	presigner   *s3.PresignClient
	bucket      string
	corsOrigins []string
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

	// The presigner signs against PublicEndpoint when it differs from
	// Endpoint, so browser-facing URLs point at the public host while the
	// direct-op client stays on the internal endpoint. Presigning is offline
	// (no network call), so constructing a second client here is cheap.
	//
	// It is also a SEPARATE client for a second reason: response checksum
	// validation must be off on it. Since aws-sdk-go-v2 defaults
	// ResponseChecksumValidation to WhenSupported, a presigned GET carries
	// `x-amz-checksum-mode` inside X-Amz-SignedHeaders — and a signed header
	// is one the recipient MUST send. The recipients here are a browser
	// opening the URL and a plain fetch of it; neither sends it, so the
	// signature never matches and every presigned download answers 403
	// SignatureDoesNotMatch. Response checksums are the caller's own
	// integrity check, not an authorization control, so dropping them here
	// costs nothing; keeping them makes presigned GETs unusable by the only
	// clients they exist for. The direct-op client above keeps validation on,
	// because it sends the header itself.
	presignEndpoint := cfg.Endpoint
	if cfg.PublicEndpoint != "" {
		presignEndpoint = cfg.PublicEndpoint
	}
	presignBase := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if presignEndpoint != "" {
			o.BaseEndpoint = aws.String(presignEndpoint)
		}
		o.UsePathStyle = cfg.ForcePathStyle
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	s := &s3Store{
		client:      client,
		presigner:   s3.NewPresignClient(presignBase),
		bucket:      cfg.Bucket,
		corsOrigins: cfg.CORSAllowedOrigins,
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
		// Existing bucket: still (re)apply the CORS policy. It lives in the
		// bucket, so a fresh volume — or a policy edited by hand — would
		// otherwise leave browser uploads silently broken until someone
		// remembered. Applying every boot makes it self-healing.
		s.applyCORS(ctx)
		return nil
	}
	if !isNotFound(err) {
		return fmt.Errorf("blob: head bucket %q: %w", s.bucket, err)
	}
	_, createErr := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	if createErr == nil {
		s.applyCORS(ctx)
		return nil
	}
	var owned *types.BucketAlreadyOwnedByYou
	var taken *types.BucketAlreadyExists
	if errors.As(createErr, &owned) || errors.As(createErr, &taken) {
		s.applyCORS(ctx)
		return nil
	}
	return fmt.Errorf("blob: create bucket %q: %w", s.bucket, createErr)
}

// applyCORS puts the bucket CORS policy that lets configured browser origins
// run the presigned upload. Best-effort by design: storage that refuses the
// call (a managed S3 whose IAM withholds s3:PutBucketCORS, or an
// implementation without bucket CORS) still serves every server-side
// operation and every download, so refusing to boot over it would trade a
// broken upload for a broken deployment. It warns instead — loudly enough to
// explain a browser upload that dies at the preflight.
func (s *s3Store) applyCORS(ctx context.Context) {
	if len(s.corsOrigins) == 0 {
		return
	}
	_, err := s.client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket: aws.String(s.bucket),
		CORSConfiguration: &types.CORSConfiguration{
			CORSRules: []types.CORSRule{{
				AllowedOrigins: s.corsOrigins,
				// PUT for the presigned upload, GET/HEAD for a presigned read
				// issued from script. DELETE is deliberately absent: detaching
				// goes through the API, which deletes server-side.
				AllowedMethods: []string{"PUT", "GET", "HEAD"},
				AllowedHeaders: []string{"*"},
				ExposeHeaders:  []string{"ETag"},
				MaxAgeSeconds:  aws.Int32(3000),
			}},
		},
	})
	if err != nil {
		slog.Warn("blob: could not set bucket CORS policy — browser uploads to this bucket will fail at the preflight",
			slog.String("bucket", s.bucket),
			slog.Any("origins", s.corsOrigins),
			slog.String("error", err.Error()))
	}
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

// Put streams body to the bucket under key, overwriting any existing
// object. ContentType is pinned on the stored object so a later
// PresignGet serves it with the right mime. A non-seekable body is
// buffered by the SDK to compute the request signature.
func (s *s3Store) Put(ctx context.Context, key, contentType string, body io.Reader) error {
	if key == "" {
		return errors.New("blob: key is required")
	}
	if contentType == "" {
		return errors.New("blob: contentType is required")
	}
	if body == nil {
		return errors.New("blob: body is required")
	}
	if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
		Body:        body,
	}); err != nil {
		return fmt.Errorf("blob: put: %w", err)
	}
	return nil
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

// PresignGetDownload mints a presigned GET whose response forces an attachment
// download named downloadAs, via the signed response-content-disposition query
// parameter (honored by S3-compatible stores incl. RustFS). A blank downloadAs
// presigns without a disposition — i.e. it behaves like PresignGet.
func (s *s3Store) PresignGetDownload(ctx context.Context, key, downloadAs string, ttl time.Duration) (string, error) {
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
	if cd := contentDispositionAttachment(downloadAs); cd != "" {
		in.ResponseContentDisposition = aws.String(cd)
	}
	req, err := s.presigner.PresignGetObject(ctx, in, func(opts *s3.PresignOptions) {
		opts.Expires = ttl
	})
	if err != nil {
		return "", fmt.Errorf("blob: presign get download: %w", err)
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
