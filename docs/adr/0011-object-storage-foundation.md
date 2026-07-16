---
title: ADR-0011 — Object-storage foundation (per-domain buckets, presigned upload, SDK seam)
status: accepted
public: true
---

# ADR-0011 — Object-storage foundation (per-domain buckets, presigned upload, SDK seam)

| Field | Value |
|---|---|
| **Status** | ✅ Accepted — adopted 2026-07-16 |
| **Date** | 2026-07-16 |
| **Authors** | @salvatore.balestrino |
| **Related** | [ADR-0006](0006-collapse-to-core-only-base.md) (core-only base — the SDK seam is what lets a fork's addon consume storage); [ADR-0009](0009-core-compliance-module.md) (GDPR DSR — the Delete-on-erasure rule) |

## Context

Orkestra shipped an S3-compatible object-storage substrate early (`internal/shared/blob` + a RustFS service), but only ever grew one consumer: the user module's avatar pipeline. Everything about it was shaped for that single use:

- The `blob.Store` interface lived in `internal/shared/blob`, **not** in the SDK — an addon (which must not import another module's internals) had no sanctioned way to consume storage. The package doc itself flagged the eventual fix: *"if a second consumer arrives … the Store interface should be promoted to `pkg/sdk/iface`."*
- The security-critical upload flow (presigned PUT direct to S3, then a commit that HEAD-confirms the object and validates the key belongs to the caller) was **hard-coded inside the avatar handler**. A second consumer would copy-paste it — and the parts that are easy to get wrong (the content-type allowlist that stops an SVG XSS upload, the size cap, the commit-time key-ownership check) are exactly the parts a copy would get subtly wrong.
- There was **one bucket** (`STORAGE_BUCKET`, default `orkestra-avatars`) and **no naming convention**. A second domain (CRM member photos, future attachments) had nothing to follow.
- The default deployment was **broken**: RustFS was pinned to `:latest`, which drifted — the current image `403`s on the `/minio/health/live` path the compose healthcheck probed, so the container read `unhealthy`.

The trigger is a concrete second consumer: a fork's CRM addon wants to attach a member **photo** to a wallet pass (tenant-scoped, unlike operator-global avatars).

## Decision

Make object storage a **first-class, governed, reusable capability** on the core base:

1. **SDK seam.** Promote `blob.Store` → **`iface.ObjectStore`** (with `iface.PresignedPut`), and add **`iface.ObjectStoreProvider`**. `internal/shared/blob` keeps `type Store = iface.ObjectStore` aliases so every existing caller compiles unchanged. The provider is registered in the `ServiceRegistry` (`ServiceObjectStoreProvider`) so core modules **and** addons resolve it through the contract, never through internals.

2. **Per-domain buckets.** One S3 connection vends a **bucket-pinned** store per logical domain: bucket = `<STORAGE_BUCKET_PREFIX>-<domain>` (`orkestra-avatars`, `orkestra-crm-photos`, …). Chosen over a single shared bucket for blast-radius isolation and independent per-bucket lifecycle/retention policies. `blob.Provider` memoizes and auto-provisions (when `EnsureBucket`) each bucket on first use.

3. **Presigned direct-to-S3 upload, always.** The SPA PUTs bytes straight to storage; the backend never proxies the payload. Two backend touches only: presign (mint the URL) and commit (confirm + persist).

4. **Reusable upload helper.** `blob.UploadController` centralizes the security-critical flow. A consumer wires `{domain store, content-type allowlist, size cap, tenant-scoped KeyBuilder, OnCommit persist callback}` and gets presign + commit (mime/size validation, HEAD-confirm, commit-time key-ownership check, prior-object GC) for free. The avatar handler is refactored onto it as the reference consumer — byte-identical behavior.

5. **Tenant-scoped key convention.** `<domain>/<scope>/<entity-uuid>/<hash>.<ext>`, where `<scope>` is the tenant ID for tenant-scoped domains and `operator`/user-uuid for operator-global ones (avatars). The controller derives the caller's key prefix from the `KeyBuilder` and rejects a commit whose key falls outside it.

6. **Module owns purge.** Each consumer `Delete`s its keys on GDPR erasure (ADR-0009) — storage does not track ownership. Orphaned uncommitted uploads are swept by a per-bucket lifecycle rule (ops guidance, not code).

7. **Deployment.** RustFS is the self-hosted default, **pinned by digest** (no `:latest`), healthchecked on `/health`. Managed S3 is recommended at scale (`STORAGE_ENSURE_BUCKET=false`, pre-provisioned buckets, `STORAGE_FORCE_PATH_STYLE=false`).

The developer recipe is codified in the **`orkestra-object-storage` skill**.

## Consequences

**Enables.** Any module (core or a fork's addon) adds a user-upload capability by resolving `ObjectStoreProvider`, wiring an `UploadController`, and mounting presign/commit under its own RBAC — without reimplementing the security checks. The CRM-photo consumer (fork-side) builds directly on this.

**Config migration.** `STORAGE_BUCKET` is superseded by `STORAGE_BUCKET_PREFIX` (default `orkestra`). The default `orkestra-avatars` bucket is unchanged (prefix `orkestra` + domain `avatars`) — no data move. A deployment that set a *custom* `STORAGE_BUCKET` must set `STORAGE_BUCKET_PREFIX` instead; the backend logs a WARN on an ignored custom value.

**Back-compat.** `ServiceBlobStore` stays registered as the `avatars` bucket, so the existing avatar + auth-DSR-bundle consumers are untouched.

**Gives up.** A single shared bucket (simpler to provision) in exchange for per-domain isolation. Cross-domain helpers must name their domain.

## Alternatives considered

- **Conventions + raw `Store` only** (document a recipe, each module hand-rolls presign/commit). Rejected: leaves the security-critical validation duplicated per consumer — the exact copy-paste hazard this ADR removes.
- **Single shared bucket, domain-prefixed keys.** Rejected: no per-bucket lifecycle/retention, larger blast radius; the operational win of per-domain buckets outweighs the extra provisioning.
- **Backend-proxied upload** (bytes stream through Go). Rejected: doubles bandwidth and memory for large files; presigned direct-to-S3 is the established, cheaper pattern already proven by avatars.
- **A generic `attachments` module** owning an attachments collection. Rejected as premature (YAGNI): modules already own their entity + key persistence; a shared collection adds coupling without a demonstrated need.
