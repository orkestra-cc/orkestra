package blob

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// domainRe constrains a storage domain slug so it can never produce a
// surprising bucket name.
var domainRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// ProviderConfig is the single shared S3 connection a Provider vends
// per-domain buckets from. S3.Bucket is ignored — the domain determines
// the bucket (<BucketPrefix>-<domain>).
type ProviderConfig struct {
	S3           S3Config
	BucketPrefix string // default "orkestra"
	Redis        *redis.Client
	Cache        CachedConfig
}

// Provider implements iface.ObjectStoreProvider: one connection, a
// bucket-pinned (optionally Redis-cached) ObjectStore per domain, memoized.
type Provider struct {
	cfg    ProviderConfig
	mu     sync.Mutex
	stores map[string]iface.ObjectStore
}

// NewProvider builds a Provider. BucketPrefix defaults to "orkestra".
func NewProvider(cfg ProviderConfig) *Provider {
	if cfg.BucketPrefix == "" {
		cfg.BucketPrefix = "orkestra"
	}
	return &Provider{cfg: cfg, stores: map[string]iface.ObjectStore{}}
}

func (p *Provider) bucketName(domain string) string {
	return p.cfg.BucketPrefix + "-" + domain
}

// Bucket returns the ObjectStore for a domain, constructing + caching it on
// first use. Concurrency-safe. A malformed domain is rejected before any
// bucket is provisioned.
func (p *Provider) Bucket(domain string) (iface.ObjectStore, error) {
	if !domainRe.MatchString(domain) {
		return nil, fmt.Errorf("blob: invalid storage domain %q (want %s)", domain, domainRe.String())
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.stores[domain]; ok {
		return s, nil
	}
	s3cfg := p.cfg.S3
	s3cfg.Bucket = p.bucketName(domain)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base, err := NewS3(ctx, s3cfg)
	if err != nil {
		return nil, fmt.Errorf("blob: provision bucket %q: %w", s3cfg.Bucket, err)
	}
	// NewCached namespaces the presigned-GET cache per domain and degrades
	// to `base` unchanged when Redis is nil, so this is always safe to call.
	cc := p.cfg.Cache
	cc.KeyPrefix = cc.KeyPrefix + domain + ":"
	store := NewCached(base, p.cfg.Redis, cc)
	p.stores[domain] = store
	return store, nil
}
