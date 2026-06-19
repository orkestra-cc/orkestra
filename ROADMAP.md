# Roadmap

What's actively in flight, what's coming next, what we know we'll do but haven't started, and what we've consciously deferred.

This document moves with the project — see `git log ROADMAP.md` for the change history. For shipped work, [`CHANGELOG.md`](CHANGELOG.md) is the canonical record. For the per-release planning detail, [GitHub Projects](https://github.com/orkestra-cc/orkestra/projects) is where day-to-day state lives.

## Now (in flight, Q2 2026)

### Core-only base ([ADR-0006](docs/adr/0006-collapse-to-core-only-base.md)) — shipping as v0.3.0

The big one, and what you're reading the output of. Orkestra collapsed to a **core-only base**: the eight core modules (`user`, `auth`, `authz`, `tenant`, `notification`, `navigation`, `logging`, `compliance`) plus the `Module` extension seam they're built on — nothing else. All fourteen verticals (billing, payments, subscriptions, documents, company, graph, aimodels, rag, agents, sales, marketing, compliance, identity, dev) left the monorepo at v0.3.0; their last in-tree state is preserved as archived `orkestra-cc/orkestra-addon-*` snapshots for forks to crib from. (`compliance` was later re-homed to core in v0.3.9 per [ADR-0009](docs/adr/0009-core-compliance-module.md), which is why core is eight modules today.) The multi-repo SDK split was reverted — `pkg/sdk` is back to an **in-tree package** of the single backend Go module (no satellite `go.mod`, no `go.work`, no `replace`). A fork that needs a vertical builds it on top, against the in-tree SDK contract, using the same `Module` + `cmd/server/catalog_<name>.go` path the core itself uses. **v0.3.0 is the first release of the core-only base, and it is breaking.**

**Tracked in:** [ADR-0006](docs/adr/0006-collapse-to-core-only-base.md).

### Tier-2 client demo SPA (`frontend-client/`)

A sibling React 19 SPA that demonstrates the client-tier surface. Post-ADR-0006 it's a thin **login + account + billing-identity** skeleton — the Stripe Checkout, subscription, and transaction flows left with the addons. The two-tier tenancy *data* model survives in the `tenant` module; a fork that sells services to external clients rebuilds the *consumption* layer (catalog → subscribe → Stripe → entitlement) on top of it.

**Tracked in:** [`frontend-client/`](frontend-client/).

## Next (committed, not yet started)

### Helm chart for Kubernetes deployments

[Operating Orkestra → Kubernetes overview](https://docs.orkestra.cc/operating/deployment/kubernetes-overview) ships hand-written YAML today. A maintained Helm chart with sensible `values.yaml` defaults and optional dependencies (cert-manager, ingress-nginx) is on the list.

**Open call:** if you're already running Orkestra on K8s and have a chart in flight, please open an issue or PR so we can converge.

### Public production image build path

ADR-0006 already folded dev onto a public `golang:alpine` base ([`docker/Dockerfile.dev-backend`](docker/Dockerfile.dev-backend), with a `GO_BASE` build-arg for forks that have a Chainguard subscription). The remaining step is the same treatment for the **production** `backend/Dockerfile` so a fork without `dhi.io` access can build prod images too. (The old per-SKU `make build-*` matrix is gone — there is a single `make build`.)

### Algolia DocSearch crawler stabilization

The crawler is configured, runs nightly, but coverage on freshly-deployed pages is occasionally patchy (Phase 0–4 docs may not all be indexed when you read this). Adjust the crawl config in the Algolia dashboard or schedule a manual re-index.

## Later (known but not committed)

### External-services framework (ADR-0004 implementation)

[ADR-0004](docs/adr/0004-external-services-integration.md) defines a formal pattern for slotting self-hosted external services (octo-stt, n8n, docling, crawl4ai, rustfs) into Orkestra's control plane. The ADR is proposed; the broker module `external_services` and the four classification axes still need to be implemented.

### Discussions, RFC threads, contributor-day cadence

[GitHub Discussions](https://github.com/orkestra-cc/orkestra/discussions) is enabled. Remaining: seed categories (Q&A, Ideas, Show and tell, Announcements, Polls), document the conventions in [CONTRIBUTING.md](CONTRIBUTING.md), and use it as the primary surface for asynchronous design discussion.

A recurring (quarterly?) contributor-day Zoom / Meet, with the BDFL + active contributors, is a nice-to-have once contributor headcount warrants it.

### Rebuilding the archived verticals

ADR-0006 moved the fourteen verticals out of the base, but the seams that let a fork rebuild them stayed: the empty `optionalModules` catalog, the `Module` + `cmd/server/catalog_<name>.go` extension path, and the nil-by-default `AuditSink` / `KMSProvider` setters on the core services (the hooks the old `compliance` addon wired). The archived `orkestra-cc/orkestra-addon-*` snapshots preserve the last in-tree state of each vertical — compliance (platform audit log, GDPR DSR pipelines, SOC2 evidence), identity (per-tenant BYO OIDC + SCIM 2.0), billing/payments/subscriptions, and the rest — for a fork to crib from. The base itself does not carry these on its roadmap; the [addon-authoring guide](docs/site/sdk/build-your-first-addon.mdx) is the entry point.

## Deferred / consciously not doing

### Multi-region active-active

Orkestra is designed for single-region operation. Multi-region (active-active or warm-standby) is real work — Mongo cross-region replication, Redis CRDTs or shared session store, audience-host DNS failover, etc. Not on the roadmap because nobody's asked. If you need this, open an issue with your use case.

### Built-in object storage

The base ships RustFS in [`docker-compose.infra.yml`](docker/docker-compose.infra.yml) as the S3-compatible target for avatars (`internal/shared/blob`), with defaults that swap cleanly to AWS S3 / Spaces / R2 in production. We don't ship a managed object-storage *service* beyond that dev convenience — operators who need durable long-term object storage point `STORAGE_*` at their own bucket.

### iOS / desktop / web platform scaffolds for mobile

The Flutter app currently has Android scaffold only. iOS and other platforms can be added via `flutter create --platforms=<name> .` — but a production iOS build needs a real Apple Developer account + signing identity, which is operator-specific. We document the path; we don't pre-scaffold.

### Bundled telemetry vendor

The observability stack ([ADR-0005](docs/adr/0005-observability-logging-tracing-metrics.md)) ships a self-hosted Tempo + Prometheus + Loki + Grafana profile, plus an OTLP-fanout path that works with any compliant vendor (Honeycomb, Datadog, Grafana Cloud, Axiom, New Relic). We don't bundle a specific vendor — operators pick.

### Backwards-compat shims for pre-1.0 API changes

Pre-1.0, the API contract is allowed to break across MINOR versions if the change is well-justified. Backwards-compat shims that complicate the codebase are deferred until 1.0. Operators should pin to specific patch versions in production.

## How to influence this roadmap

- **Already coding it?** Open a draft PR. Lazy consensus + ADR if architectural.
- **Have an opinion on priority?** Open a [Discussion](https://github.com/orkestra-cc/orkestra/discussions) under "Ideas".
- **Found a blocker for your fork?** Open an issue with the `enhancement` label.
- **Want to take on a "Later" item?** Comment on the relevant tracking issue (or open one). The BDFL coordinates ownership.

The BDFL revisits this roadmap before every `dev → main` promotion. If you're not seeing a real-world need addressed here, file it.

## See also

- [`CHANGELOG.md`](CHANGELOG.md) — what shipped
- [`GOVERNANCE.md`](GOVERNANCE.md) — how decisions get made
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — practical contributor guide
- [`docs/adr/`](docs/adr/) — Architecture Decision Records
