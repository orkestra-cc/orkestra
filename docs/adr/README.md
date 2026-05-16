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

## Format

Every ADR follows the shape:

- **Status** — Proposed / Accepted / Superseded-by-XXXX / Deprecated
- **Context** — what forced the decision
- **Decision** — what we chose
- **Consequences** — what changes, what we give up, what we enable
- **Alternatives considered** — rejected paths and why
