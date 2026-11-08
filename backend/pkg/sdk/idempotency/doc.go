// Package idempotency provides an Idempotency-Key HTTP middleware for
// mutating endpoints, so a client that retries a queued operation (mobile
// offline sync) never applies it twice.
//
// A module mounts Middleware on its mutating routes and declares the store
// collection with CollectionSpec in its own Collections(); each module owns
// its own collection (named with the module prefix). The store is
// fail-closed: if it is unavailable the request is refused (503) and the
// handler is not executed.
//
// Semantics per (scope, key), where scope is the caller's tenant and user:
//   - first request: executes the handler and stores the response;
//   - same key and same request fingerprint after completion: the stored
//     response is replayed with "Idempotent-Replayed: true";
//   - same key while the first request runs: 409 with Retry-After;
//   - same key with a different method, URI or body: 422.
//
// Responses with status >= 500, panics and empty responses are not stored
// (the key is released so a retry executes again). Responses larger than
// 256 KiB are recorded as done but cannot be replayed (409).
package idempotency
