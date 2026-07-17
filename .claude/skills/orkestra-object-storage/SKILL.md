---
name: orkestra-object-storage
description: "Use when adding or reviewing a user-upload / object-storage capability in the Orkestra backend — file/image/photo/document/attachment uploads, avatars, presigned S3 URLs, RustFS/MinIO/S3, blob.Store / iface.ObjectStore, the UploadController, or per-domain buckets. Covers the SDK seam, the presign→commit security flow, the tenant-scoped key convention, GDPR Delete-on-erasure, and the RustFS/prod deployment posture (ADR-0011)."
---

# Orkestra Object Storage

Use this skill whenever a module (core **or** a fork addon under `internal/addons/<name>/`) needs to store a user-uploaded blob — an image, photo, document, or attachment. It codifies [ADR-0011](../../../docs/adr/0011-object-storage-foundation.md). The worked reference is the avatar handler (`internal/core/user/handlers/avatar_handler.go`).

## The model (never bypass it)

- **One connection, per-domain buckets.** A single `blob.Provider` (registered as `iface.ObjectStoreProvider` under `module.ServiceObjectStoreProvider`) vends a **bucket-pinned** `iface.ObjectStore` per logical **domain**. Bucket name = `<STORAGE_BUCKET_PREFIX>-<domain>` (e.g. `orkestra-crm-photos`). Pick a short lowercase `^[a-z0-9-]+$` domain slug for your feature.
- **Presigned direct-to-S3 upload, always.** The SPA PUTs bytes straight to storage; the backend never proxies the payload. The backend only mints the URL (presign) and confirms/persists (commit).
- **The `UploadController` owns the security-critical checks.** Do not hand-roll presign/commit — you will get the content-type allowlist, size cap, or commit-time key-ownership check subtly wrong.

## Recipe — adding an upload surface

1. **Resolve the provider** in your module's `Init` and get your domain's store:
   ```go
   provider, ok := module.GetTyped[iface.ObjectStoreProvider](deps.Services, module.ServiceObjectStoreProvider)
   if !ok { /* object storage not configured — degrade: your upload routes return 503 */ }
   store, err := provider.Bucket("crm-photos") // ensures orkestra-crm-photos on first use
   ```

2. **Wire an `UploadController`** with your policy:
   ```go
   ctl := blob.NewUploadController(blob.UploadConfig{
       Store:               store,
       AllowedContentTypes: map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"},
       MaxBytes:            5 * 1024 * 1024,
       KeyBuilder: func(s blob.UploadScope, ext string) string {
           // <domain>/<scope>/<entity>/<hash>.<ext> — scope is the TENANT id for
           // tenant-scoped domains (operator/user-uuid for operator-global ones).
           return fmt.Sprintf("crm-photos/%s/%s/%s.%s", s.Tenant, s.Entity, uuid.Must(uuid.NewV7()), ext)
       },
       OnCommit: func(ctx context.Context, s blob.UploadScope, key string) (previousKey string, err error) {
           return personSvc.SetPhotoKey(ctx, s.Entity, key) // persist + return the prior key for GC
       },
   })
   ```

3. **Mount presign + commit** as two Huma routes **under your own RBAC + tier**. Fill `UploadScope` from `ctxauth` (tenant + subject + entity), call `ctl.Presign(...)` / `ctl.Commit(...)`, and map the sentinels:
   - `blob.ErrContentTypeNotAllowed` → 400
   - `blob.ErrTooLarge` → 413
   - `blob.ErrKeyOutOfScope` → 400
   - `blob.ErrUploadNotFound` → 404
   The controller's commit does the HEAD-confirm, the key-prefix ownership check (derived from your `KeyBuilder`), and the prior-object GC — so a client can never promote another subject's blob.

4. **Serve reads** with `store.PresignGet(ctx, key, ttl)` (refresh on every read path; a Redis-cached wrapper already fronts the store). Never render a raw bucket URL.

5. **GDPR — you own purge.** `store.Delete(ctx, key)` every key on the subject's erasure cascade (ADR-0009 / the addon's `HardDeleteByPersonUUID` path). Storage does not track ownership for you.

## Key convention

```
<domain>/<scope>/<entity-uuid>/<hash>.<ext>
```

`<scope>` = tenant ID (tenant-scoped domains) or `operator`/user-uuid (operator-global). `<hash>` = a random UUIDv7 so keys are unguessable and collision-safe. The commit ownership check binds `<domain>/<scope>/<entity-uuid>/` to the authenticated caller.

## Deployment & ops

- **RustFS** is the self-hosted default (`docker-compose.infra.yml`), **pinned by digest** — never `:latest` (it drifts). Healthcheck probes `/health`.
- **The presign endpoint must be browser-reachable** for presigned PUTs to succeed. The browser PUTs to `STORAGE_PUBLIC_ENDPOINT` when set, else `STORAGE_ENDPOINT`. Behind a TLS proxy (Cloudflare/HAProxy), keep `STORAGE_ENDPOINT` internal (`http://rustfs:9000`, for the backend's own HEAD/Delete/Get) and set `STORAGE_PUBLIC_ENDPOINT` to the public host — presigned URLs sign only `host` and survive proxying, but SDK-signed backend ops 403 through the proxy, so they must hit the origin directly (`blob.S3Config.PublicEndpoint`). The proxy must preserve the `Host` header and answer the browser CORS preflight. See the RustFS reachability gotcha in `docker/CLAUDE.md`.
- **Production / scale**: point `STORAGE_ENDPOINT` at a managed S3, pre-provision the per-domain buckets, set `STORAGE_ENSURE_BUCKET=false` (IAM rarely grants CreateBucket) + `STORAGE_FORCE_PATH_STYLE=false`, and apply per-bucket lifecycle (expire orphaned uncommitted uploads under a `pending/`-style prefix, or unreferenced objects after N days) + backup policies.
- `STORAGE_BUCKET` is **deprecated** — use `STORAGE_BUCKET_PREFIX`.

## Out of scope (the consumer's job, not the foundation)

Image resize / thumbnail generation, malware scanning, multipart/resumable upload, CDN fronting. The store holds **raw bytes**; a consumer that needs a specific rendition (e.g. an Apple-wallet 90×90 thumbnail) processes it itself before/after upload.

## Do not

- ❌ Import `internal/shared/blob` from an addon's `module.go` wiring for the interface — resolve `iface.ObjectStoreProvider` from the ServiceRegistry. (Constructing an `UploadController` with `blob.NewUploadController` is fine — that's a shared helper, like `tenantrepo`.)
- ❌ Hand-roll presign/commit or the mime/size/ownership checks — use the `UploadController`.
- ❌ Proxy upload bytes through the backend — presign a direct PUT.
- ❌ Re-introduce a single shared bucket or a `:latest` RustFS pin.
- ❌ Forget the `Delete`-on-erasure step — an un-purged blob is a GDPR gap.
