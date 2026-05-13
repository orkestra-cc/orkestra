# Sales Intelligence Module

_Path: `/backend/internal/addons/sales`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

[← Backend](../../../CLAUDE.md) | [☰ Module Map](../../../../../CLAUDE.md#module-map)

## Module home

This directory is a **separate Go module**
(`github.com/orkestra-cc/orkestra-addon-sales`) since Phase 5e of the
SDK split. Source lives in-tree at this path for monorepo development;
the same tree is mirrored to
[github.com/orkestra-cc/orkestra-addon-sales](https://github.com/orkestra-cc/orkestra-addon-sales)
and tagged starting from `v0.1.0`. Backend's `go.mod` carries a
`replace` directive pointing at this path so changes here take effect
without a tag bump during cross-cutting work; CI and external
consumers fetch the published version through the Go module proxy.

The `SalesConfig` struct that used to live at
`backend/internal/shared/config/config.go` moved into this addon as
`config/config.go` (still `package config`). Only the sales addon ever
imported it, and the env-var populator in shared/config was already
dead code (`module.go` builds a fresh `SalesConfig` from its own
`Settings` struct on every Init via the SDK ConfigService).

## What it does

Multi-agent AI pipeline that turns a prospect URL into a scored sales
report. Each prospect job runs five agents in sequence
(`company-research`, `competitive-analysis`, `contact-finder`,
`opportunity-scoring`, `outreach-strategy`), each backed by an LLM call
through the kernel's `iface.AIModelProvider`. Individual `skills`
(reusable mini-prompts: `icp`, `qualify`, `prep`, `proposal`,
`research`, `competitors`, `contacts`, `outreach`, `objections`,
`followup`) can also be run standalone for ad-hoc workflows.

The addon scrapes the prospect's website with `colly` (depth-limited
crawl, dedup-aware, with industry inference + tech-stack detection)
and feeds the structured result into the agent chain. Output is
persisted as `sales_reports` (Markdown + PDF artefacts) and tied back
to the originating `sales_jobs` document.

## Structure

```
sales/
├── config/
│   └── config.go              # SalesConfig DTO (relocated from shared/config in Phase 5e)
├── module.go                  # SDK Module impl: ConfigSchema, Collections, NavItems, Permissions, Capabilities
├── routes.go                  # Huma route registration (skill, prospect, job, report, prompt, settings)
├── handlers/                  # HTTP handlers per surface
├── services/
│   ├── orchestrator.go        # Job lifecycle + skill execution + LLM dispatch
│   ├── agent_executor.go      # Bounded-concurrency multi-agent runner
│   ├── prompt_loader.go       # Loads + caches prompts (DB-overridable, embedded defaults)
│   ├── scraper.go             # colly-backed website scrape
│   ├── scorer.go              # Heuristic opportunity score from agent outputs
│   ├── company_enrichment.go  # Optional Italian business-registry adapter (consumes company addon)
│   ├── report_generator.go    # Markdown → HTML → PDF (calls documents addon)
│   ├── pdf_generator.go       # PDF rendering helpers
│   ├── batch_poller.go        # Async batch-mode LLM result poller
│   ├── ai_model_provider.go   # Local re-declaration of iface.AIModelProvider for service-side typing
│   └── skill_store.go         # In-memory skill registry
├── repository/                # MongoDB CRUD (jobs, reports, prompts, settings, batches)
├── models/                    # Domain types + DTOs
├── prompts/
│   ├── agents/*.md            # Five agent prompts (embedded)
│   ├── skills/*.md            # Ten skill prompts (embedded)
│   └── embed.go               # go:embed loader
└── CLAUDE.md                  # This file
```

## Endpoints (`/v1/sales`)

| Method | Path | Purpose |
|---|---|---|
| POST | `/skills/{skill}` | Run a single skill against a URL (sync, sub-prompt timeout) |
| GET | `/skills` | List supported skills |
| POST | `/prospect/quick` | Sync quick-pass prospect analysis (3 agents, capped at quickTimeout) |
| POST | `/prospect` | Async full-pipeline prospect job (all 5 agents) |
| GET / POST | `/jobs[/{id}]` | Job lifecycle: list, get, retry, rerun-failed-agents, cancel, delete |
| GET | `/reports/{id}`, `/reports/{id}/{md,html,pdf}` | Report fetch + render |
| GET / PUT | `/prompts[/{id}]` | Operator-tier prompt overrides (DB takes precedence over embedded defaults) |
| GET / PUT | `/settings` | Per-user LLM + locale + max-tokens overrides |

## MongoDB collections

| Collection | Indexes | Purpose |
|---|---|---|
| `sales_jobs` | unique `uuid` | Async job state |
| `sales_reports` | unique `uuid` | Generated reports |
| `sales_prompts` | (none) | Operator overrides for agent/skill prompts |
| `sales_settings` | (none) | Per-user model + locale prefs |
| `sales_batches` | (none) | Async batch-mode LLM submissions |

## Configuration

All eight tunables are declared in `ConfigSchema()` and editable at
`/admin/modules/sales`; defaults match the legacy `SALES_*` env-var
defaults so existing deployments keep their behavior. Cross-module
dependency: aimodels must be installed and enabled — `sales.Dependencies()`
returns `["aimodels"]` and `Init()` reads `iface.AIModelProvider` from
the `ServiceRegistry`.

| Field | Env var | Default |
|---|---|---|
| `maxConcurrency` | `SALES_MAX_CONCURRENCY` | `5` |
| `defaultLocale` | `SALES_DEFAULT_LOCALE` | `it` |
| `skillTimeout` | `SALES_SKILL_TIMEOUT` | `5m` |
| `quickTimeout` | `SALES_QUICK_TIMEOUT` | `5m` |
| `fullTimeout` | `SALES_FULL_TIMEOUT` | `15m` |
| `scraperTimeout` | `SALES_SCRAPER_TIMEOUT` | `30s` |
| `scraperMaxDepth` | `SALES_SCRAPER_MAX_DEPTH` | `3` |
| `maxTokens` | `SALES_MAX_TOKENS` | `8192` |

## RBAC + capability

Routes sit behind the `sales.access` capability gate and require one
of `sales.job.read` (read paths), `sales.job.run` (write paths), or
`sales.admin` (prompt + settings administration).
