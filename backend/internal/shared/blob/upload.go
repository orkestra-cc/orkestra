package blob

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Sentinel errors a consuming handler maps to HTTP statuses.
var (
	ErrContentTypeNotAllowed = errors.New("blob: content type not allowed")
	ErrTooLarge              = errors.New("blob: upload exceeds size cap")
	ErrKeyOutOfScope         = errors.New("blob: key does not belong to the caller")
	ErrUploadNotFound        = errors.New("blob: uploaded object not found")
)

// UploadScope carries the resolved identity a KeyBuilder needs to build a
// caller-bound key. Modules fill the fields they use (avatars use Subject;
// tenant-scoped domains use Tenant + Entity).
type UploadScope struct {
	Tenant  string
	Subject string
	Entity  string
}

// UploadConfig configures one upload surface (one domain/bucket). The
// centralized presign+commit flow enforces the mime allowlist, size cap,
// and commit-time key-ownership check so every consumer gets them right.
type UploadConfig struct {
	Store               iface.ObjectStore
	AllowedContentTypes map[string]string // mime -> canonical extension
	MaxBytes            int64
	PresignTTL          time.Duration // default 10m
	// KeyBuilder builds the object key from the scope + extension. SAFETY
	// INVARIANT: put the caller-identifying scope segments (tenant / subject /
	// entity) BEFORE the final "/" with slash-free values, because the commit
	// ownership check derives the caller's prefix from KeyBuilder(scope, "")
	// up to the last "/". A slash-less or misordered key fails CLOSED (every
	// commit rejected as ErrKeyOutOfScope), never open — but that is a silent
	// breakage, so follow the <domain>/<scope>/<entity>/<hash>.<ext> convention.
	KeyBuilder func(scope UploadScope, ext string) string
	// OnCommit persists the committed key onto the module's entity and
	// returns the previously-stored key (or "") for the controller to GC.
	OnCommit func(ctx context.Context, scope UploadScope, key string) (previousKey string, err error)
}

// UploadController is the reusable presign→commit helper.
type UploadController struct{ cfg UploadConfig }

// NewUploadController builds a controller; PresignTTL defaults to 10m.
func NewUploadController(cfg UploadConfig) *UploadController {
	if cfg.PresignTTL == 0 {
		cfg.PresignTTL = 10 * time.Minute
	}
	return &UploadController{cfg: cfg}
}

// Presign validates the content type + declared size, builds a
// caller-scoped key, and mints a direct-to-S3 PUT URL.
func (c *UploadController) Presign(ctx context.Context, scope UploadScope, contentType string, sizeBytes int64) (*iface.PresignedPut, error) {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	ext, ok := c.cfg.AllowedContentTypes[ct]
	if !ok {
		return nil, ErrContentTypeNotAllowed
	}
	if sizeBytes <= 0 || sizeBytes > c.cfg.MaxBytes {
		return nil, ErrTooLarge
	}
	key := c.cfg.KeyBuilder(scope, ext)
	return c.cfg.Store.PresignPut(ctx, key, ct, c.cfg.PresignTTL)
}

// scopePrefix is the leading path segment(s) a caller's keys must share.
// Derived from KeyBuilder(scope, "") up to and including the last "/", so
// the ownership check tracks whatever key layout the module chose.
func (c *UploadController) scopePrefix(scope UploadScope) string {
	k := c.cfg.KeyBuilder(scope, "")
	if i := strings.LastIndex(k, "/"); i >= 0 {
		return k[:i+1]
	}
	return k
}

// DeleteObject removes an object by key (GC for a replaced/removed entity blob).
func (c *UploadController) DeleteObject(ctx context.Context, key string) error {
	return c.cfg.Store.Delete(ctx, key)
}

// PresignGetURL mints a short-lived signed GET URL for reading a stored object
// (operator download/preview). The URL host is the store's public endpoint when
// configured, so the browser can fetch it directly.
func (c *UploadController) PresignGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return c.cfg.Store.PresignGet(ctx, key, ttl)
}

// PresignGetDownloadURL mints a short-lived signed GET URL that, when fetched,
// downloads the object as an attachment named downloadAs (instead of the opaque
// object-key filename). Falls back to a plain presigned GET when downloadAs is
// blank or the configured store lacks the download-presign capability.
func (c *UploadController) PresignGetDownloadURL(ctx context.Context, key, downloadAs string, ttl time.Duration) (string, error) {
	if downloadAs != "" {
		if dl, ok := c.cfg.Store.(ObjectDownloadPresigner); ok {
			return dl.PresignGetDownload(ctx, key, downloadAs, ttl)
		}
	}
	return c.cfg.Store.PresignGet(ctx, key, ttl)
}

// Commit re-validates the key belongs to the caller, HEAD-confirms the
// object landed, persists it via OnCommit, and GCs the prior key.
func (c *UploadController) Commit(ctx context.Context, scope UploadScope, key string) error {
	key = strings.TrimSpace(key)
	if key == "" || !strings.HasPrefix(key, c.scopePrefix(scope)) {
		return ErrKeyOutOfScope
	}
	exists, err := c.cfg.Store.Exists(ctx, key)
	if err != nil {
		return err
	}
	if !exists {
		return ErrUploadNotFound
	}
	previous, err := c.cfg.OnCommit(ctx, scope, key)
	if err != nil {
		return err
	}
	if previous != "" && previous != key {
		if delErr := c.cfg.Store.Delete(ctx, previous); delErr != nil {
			slog.WarnContext(ctx, "blob: failed to delete previous object",
				slog.String("key", previous), slog.String("error", delErr.Error()))
		}
	}
	return nil
}
