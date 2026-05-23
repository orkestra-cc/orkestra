# Roadmap

What's actively in flight, what's coming next, what we know we'll do but haven't started, and what we've consciously deferred.

This document moves with the project — see `git log ROADMAP.md` for the change history. For shipped work, [`CHANGELOG.md`](CHANGELOG.md) is the canonical record. For the per-release planning detail, [GitHub Projects](https://github.com/orkestra-cc/orkestra/projects) is where day-to-day state lives.

## Now (in flight, Q2 2026)

### Tier-2 client demo SPA (`frontend-client/`)

A sibling React 19 SPA that demonstrates the client-tier surface — Stripe Checkout-backed signup, subscription management, AI services consumption. Lives at `app.orkestra.cc`. Five phases planned, three landed. Active development.

**Tracked in:** [`frontend-client/`](frontend-client/) + MEMORY entries.

### Marketing addon

Five-phase build-out: contact base + CSV importer (Phase 1) → storicizzazione + scoring (Phase 2) → advanced imports / adapters (Phase 3) → card lifecycle (Phase 4) → engagement triggers (Phase 5). Phases 1–4 are on `dev` awaiting promotion; Phase 5 is the immediate next.

**Tracked in:** [`backend/internal/addons/marketing/CLAUDE.md`](backend/internal/addons/marketing/CLAUDE.md) + per-phase implementation plans in [`docs/plans/marketing-addon/`](docs/plans/marketing-addon/).

### Fork-readiness epic

The work that produced this `ROADMAP.md` itself. Seven phases (most landed): env template + `make init`, public-image dev compose, deployment + onboarding docs, mobile fork-readiness, this governance pass, smoke test. **You're reading the output of Phase 6.**

**Tracked in:** GitHub Discussions thread (link TBD when Discussions are enabled) + the `chore(release)` commit titles in [`CHANGELOG.md`](CHANGELOG.md).

## Next (committed, not yet started)

### Helm chart for Kubernetes deployments

[Operating Orkestra → Kubernetes overview](https://docs.orkestra.cc/operating/deployment/kubernetes-overview) ships hand-written YAML today. A maintained Helm chart with sensible `values.yaml` defaults, optional dependencies (cert-manager, ingress-nginx), and parity with the SKU profile pattern is on the list.

**Open call:** if you're already running Orkestra on K8s and have a chart in flight, please open an issue or PR so we can converge.

### AI sidecar production-ready

The AI module chain (`graph`, `aimodels`, `rag`, `agents`) can run as a separate `cmd/ai-service` binary, controlled by `AI_SERVICE_URL` on the monolith (see [backend/CLAUDE.md "AI Service Sidecar"](backend/CLAUDE.md)). The split works end-to-end in dev. Production-readiness items remaining: independent scaling examples, dedicated CI matrix, deploy patterns for both Compose and K8s, staging-environment proof-of-life.

### Public-image build matrix

[Phase 3 of fork-readiness](docs/site/architecture/dev-images.mdx) added a public-image dev compose. The next step is a `backend/Dockerfile.public` so `make build-*` profile-image builds don't require a Chainguard subscription either. Forkers can build their own images locally without `dhi.io` access.

### Algolia DocSearch crawler stabilization

The crawler is configured, runs nightly, but coverage on freshly-deployed pages is occasionally patchy (Phase 0–4 docs may not all be indexed when you read this). Adjust the crawl config in the Algolia dashboard or schedule a manual re-index.

## Later (known but not committed)

### External-services framework (ADR-0004 implementation)

[ADR-0004](docs/adr/0004-external-services-integration.md) defines a formal pattern for slotting self-hosted external services (octo-stt, n8n, docling, crawl4ai, rustfs) into Orkestra's control plane. The ADR is proposed; the broker module `external_services` and the four classification axes still need to be implemented.

### Discussions, RFC threads, contributor-day cadence

[GitHub Discussions](https://github.com/orkestra-cc/orkestra/discussions) needs to be enabled (Settings → General → Features → Discussions). Once on, we'll seed categories (Q&A, Ideas, Show and tell, Announcements, Polls), document the conventions in [CONTRIBUTING.md](CONTRIBUTING.md), and use it as the primary surface for asynchronous design discussion.

A recurring (quarterly?) contributor-day Zoom / Meet, with the BDFL + active contributors, is a nice-to-have once contributor headcount warrants it.

### Compliance addon — GDPR DSR pipelines, SOC2 evidence

The `compliance` addon today ships the platform audit log. The next two surfaces are: automated DSR (Data Subject Request) intake + fulfilment pipelines (export-my-data, delete-my-data) and SOC2 evidence collection (per-control evidence, audit-ready packaging).

### Identity addon — per-tenant BYO OIDC + SCIM 2.0

The `identity` addon today ships the stubs (per-tenant OIDC entry, SCIM endpoints). Wiring them to a working flow (test it against Okta, Azure AD, JumpCloud) is on the list.

## Deferred / consciously not doing

### Multi-region active-active

Orkestra is designed for single-region operation. Multi-region (active-active or warm-standby) is real work — Mongo cross-region replication, Redis CRDTs or shared session store, audience-host DNS failover, etc. Not on the roadmap because nobody's asked. If you need this, open an issue with your use case.

### Built-in object storage

The `documents` addon uses Gotenberg for PDF rendering; output streams back to the client. We don't ship a built-in S3-compatible object store. Operators who need long-term object storage integrate S3 / Spaces / R2 at the application layer.

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
