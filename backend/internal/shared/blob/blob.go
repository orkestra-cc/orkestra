// Package blob provides a minimal S3-compatible object-storage seam for
// user-uploaded blobs. Today the only consumer is the user module's
// avatar pipeline; if a second consumer arrives (documents migrating
// off Mongo bytes, marketing attachments) the Store interface should
// be promoted to pkg/sdk/iface so external addons can satisfy it.
//
// The package is intentionally tiny: a pre-signed PUT + GET surface,
// a server-side Put (for payloads the backend assembles itself, e.g.
// a GDPR DSR export bundle), a Delete, and a HEAD-style Exists.
// Anything richer (multi-part upload, server-side resize, range
// reads) belongs in a higher layer.
//
// Production default backend is RustFS (Apache-2.0, S3-compatible),
// declared in docker-compose.infra.yml. Any S3-API-compatible target
// (AWS S3, MinIO, Backblaze B2) works through the same s3.New
// constructor — pick the right endpoint + path-style flag.
package blob

import (
	"errors"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ErrObjectNotFound is returned by Exists when the key does not exist
// in the configured bucket. PresignGet still returns a URL for missing
// keys (the GET happens at the SPA later) — callers that need a
// pre-flight check before promoting a stored key should use Exists.
var ErrObjectNotFound = errors.New("blob: object not found")

// PresignedPut is re-exported from the SDK — the canonical definition
// lives in iface.PresignedPut. The alias keeps existing in-tree callers
// stable while addons consume the type through the SDK seam.
type PresignedPut = iface.PresignedPut

// Store is the bucket-pinned blob-storage seam. The canonical definition
// lives in the SDK (iface.ObjectStore); this alias keeps every existing
// internal/shared/blob caller compiling unchanged.
type Store = iface.ObjectStore
