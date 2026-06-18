# Architecture Decision Records

Durable, dated records of architectural decisions that shape Orkestra. One file per decision. Numbered sequentially.

## Index

| # | Title | Status | Date |
|---|-------|--------|------|
| [0001](0001-unified-tenant-model.md) | Unified Tenant model for two-tier multi-tenancy | Accepted | 2026-04-18 |
| [0002](0002-metrics-label-schema.md) | Prometheus metric label schema | Accepted | 2026-04-20 |
| [0003](0003-three-audience-host-split.md) | Three-audience API host split (operator / client / service) | Proposed | 2026-04-30 |
| [0004](0004-external-services-integration.md) | External services integration framework | Proposed | 2026-05-14 |
| [0005](0005-observability-logging-tracing-metrics.md) | Observability — logging, tracing, metrics as core platform features | Proposed | 2026-05-16 |
| [0006](0006-collapse-to-core-only-base.md) | Collapse Orkestra to a core-only base; addons become per-fork responsibility | Proposed | 2026-06-01 |
| [0007](0007-per-addon-i18n-namespaces.md) | Per-addon i18n namespaces; addon translations never touch core locale files | Proposed | 2026-06-14 |
| [0008](0008-partition-openapi-spec-per-module.md) | Partition the OpenAPI spec per module so a fork's addons never collide with core | Proposed | 2026-06-14 |
| [0009](0009-core-compliance-module.md) | Re-home the compliance plane (audit + GDPR DSR) to core | Proposed | 2026-06-18 |

## Format

Every ADR follows the shape:

- **Status** — Proposed / Accepted / Superseded-by-XXXX / Deprecated
- **Context** — what forced the decision
- **Decision** — what we chose
- **Consequences** — what changes, what we give up, what we enable
- **Alternatives considered** — rejected paths and why
