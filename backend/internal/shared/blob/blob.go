// Package blob provides a minimal S3-compatible object-storage seam for
// user-uploaded blobs. Today the only consumer is the user module's
// avatar pipeline; if a second consumer arrives (documents migrating
// off Mongo bytes, marketing attachments) the Store interface should
// be promoted to pkg/sdk/iface so external addons can satisfy it.
//
// The package is intentionally tiny: a pre-signed PUT + GET surface,
// a Delete, and a HEAD-style Exists. Anything richer (multi-part
// upload, server-side resize, range reads) belongs in a higher layer.
//
// Production default backend is RustFS (Apache-2.0, S3-compatible),
// declared in docker-compose.infra.yml. Any S3-API-compatible target
// (AWS S3, MinIO, Backblaze B2) works through the same s3.New
// constructor — pick the right endpoint + path-style flag.
package blob

import (
	"context"
	"errors"
	"time"
)

// ErrObjectNotFound is returned by Exists when the key does not exist
// in the configured bucket. PresignGet still returns a URL for missing
// keys (the GET happens at the SPA later) — callers that need a
// pre-flight check before promoting a stored key should use Exists.
var ErrObjectNotFound = errors.New("blob: object not found")

// PresignedPut groups the upload URL with any headers the SPA must
// echo on the PUT for the signature to validate. S3-compatible
// signers require the Content-Type header to match what was signed,
// so callers must forward Headers verbatim.
type PresignedPut struct {
	URL       string
	Headers   map[string]string
	Key       string
	ExpiresAt time.Time
}

// Store is the minimal blob-storage seam. Implementations are
// expected to be safe for concurrent use.
type Store interface {
	// PresignPut returns a URL the SPA can PUT to directly, bypassing
	// the backend. The signer pins the content-type so a client can't
	// upload a different mime than what was signed; size limits are
	// the caller's responsibility (the signer typically cannot enforce
	// Content-Length on a presigned PUT).
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (*PresignedPut, error)
	// PresignGet returns a short-lived URL the SPA can GET to render
	// the blob. Callers should refresh on every read path so the URL
	// in flight is never close to expiry; a Redis-cached wrapper lives
	// in cache.go for hot paths.
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	// Delete removes an object. Missing keys do not error — Delete
	// is idempotent so a retry on a half-failed commit is safe.
	Delete(ctx context.Context, key string) error
	// Exists is a HEAD-style probe. Returns false / nil when the
	// object is missing (without raising), or true / nil when it
	// exists. Network or auth errors propagate.
	Exists(ctx context.Context, key string) (bool, error)
}
