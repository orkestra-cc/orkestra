<div align="center">

# Orkestra Sales Intelligence addon

**Multi-agent AI prospect-analysis pipeline for the [Orkestra](https://github.com/orkestra-cc/orkestra) modular monolith. Scrapes a target URL with `colly`, runs five sequenced agents through the kernel's `iface.AIModelProvider` (company research → competitive analysis → contact finder → opportunity scoring → outreach strategy), scores the result, and renders a Markdown/HTML/PDF report. Bring-your-own LLM (Ollama / OpenAI / Anthropic / Gemini) via the [`aimodels`](https://github.com/orkestra-cc/orkestra-addon-aimodels) addon.**

[![Go Reference](https://pkg.go.dev/badge/github.com/orkestra-cc/orkestra-addon-sales.svg)](https://pkg.go.dev/github.com/orkestra-cc/orkestra-addon-sales)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white&style=flat-square)](https://go.dev)
[![Module](https://img.shields.io/badge/module-github.com%2Forkestra--cc%2Forkestra--addon--sales-blue?style=flat-square)](https://github.com/orkestra-cc/orkestra-addon-sales)
[![Latest tag](https://img.shields.io/github/v/tag/orkestra-cc/orkestra-addon-sales?sort=semver&style=flat-square)](https://github.com/orkestra-cc/orkestra-addon-sales/tags)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat-square)](LICENSE)

[SDK](https://github.com/orkestra-cc/orkestra-sdk) · [AI Models addon](https://github.com/orkestra-cc/orkestra-addon-aimodels) · [Monorepo](https://github.com/orkestra-cc/orkestra) · [Module docs](CLAUDE.md)

</div>

---

## What this is

A self-contained Orkestra addon implementing the `Module` interface from [`orkestra-cc/orkestra-sdk`](https://github.com/orkestra-cc/orkestra-sdk). It owns five MongoDB collections (`sales_jobs`, `sales_reports`, `sales_prompts`, `sales_settings`, `sales_batches`), exposes `/v1/sales/*` job + report endpoints, and ships embedded default prompts for five agents and ten standalone skills.

**Agents** (run sequentially per prospect job):

| Agent | Purpose |
|---|---|
| `company-research` | Synthesises a structured company brief from scraped content |
| `competitive-analysis` | Names rival players + key differentiators |
| `contact-finder` | Surfaces likely decision-makers + outreach handles |
| `opportunity-scoring` | Numeric fit score with rationale |
| `outreach-strategy` | First-touch outreach plan tailored to the score |

**Skills** (reusable mini-prompts; can be run ad-hoc via `/v1/sales/skills/{name}`):

`icp`, `qualify`, `prep`, `proposal`, `research`, `competitors`, `contacts`, `outreach`, `objections`, `followup`.

Both prompt sets are DB-overridable: operators can edit prompts at `/admin` (under the sales surface) and the loader prefers the DB row when present, falling back to the embedded default.

## How it ships

The same source tree lives in two places:

- **In-tree** at [`backend/internal/addons/sales/`](https://github.com/orkestra-cc/orkestra/tree/main/backend/internal/addons/sales) inside the [orkestra-cc/orkestra](https://github.com/orkestra-cc/orkestra) monorepo, where cross-module development happens.
- **Standalone** at this repository, tagged from `v0.1.0`, consumed via the Go module proxy by anything outside the monorepo.

The upstream backend's `go.mod` looks roughly like this:

```go
require github.com/orkestra-cc/orkestra-addon-sales v0.1.0
replace github.com/orkestra-cc/orkestra-addon-sales => ./internal/addons/sales
```

The `replace` will retire once cross-cutting addon churn settles.

## Install

```bash
go get github.com/orkestra-cc/orkestra-addon-sales@latest
```

Requires Go 1.25.10 or newer.

```go
import sales "github.com/orkestra-cc/orkestra-addon-sales"
```

## Boot in a host

The addon plugs into any host that implements the Orkestra SDK kernel. Register it alongside the other catalog entries (and make sure [`aimodels`](https://github.com/orkestra-cc/orkestra-addon-aimodels) is also registered — sales declares it as a hard dependency):

```go
import (
    aimodels "github.com/orkestra-cc/orkestra-addon-aimodels"
    sales "github.com/orkestra-cc/orkestra-addon-sales"
    "github.com/orkestra-cc/orkestra-sdk/module"
)

reg := module.NewModuleRegistry(logger) // your kernel's *slog.Logger
reg.Register(aimodels.NewModule())      // produces iface.AIModelProvider
reg.Register(sales.NewModule())         // depends on aimodels — registry topo-sorts at Init time
```

The registry takes care of init order (`sales` waits for `aimodels`), Mongo collection creation, route mounting on the operator surface, RBAC enforcement (`sales.job.read` / `sales.job.run` / `sales.admin`), and module-gate middleware for runtime enable/disable via `/admin/modules`.

## Configuration

All eight runtime tunables are declared in the addon's `ConfigSchema()` and editable at `/admin/modules/sales`; defaults match the legacy `SALES_*` env-var defaults so existing deployments keep their behavior.

| Env var (seeds first-install) / admin field | Purpose | Default |
|---|---|---|
| `SALES_MAX_CONCURRENCY` | Max parallel agent LLM calls per job | `5` |
| `SALES_DEFAULT_LOCALE` | Default prompt locale | `it` |
| `SALES_SKILL_TIMEOUT` | Per-skill LLM call timeout | `5m` |
| `SALES_QUICK_TIMEOUT` | `/prospect/quick` sync deadline | `5m` |
| `SALES_FULL_TIMEOUT` | `/prospect` full-pipeline deadline | `15m` |
| `SALES_SCRAPER_TIMEOUT` | Per-request scrape timeout | `30s` |
| `SALES_SCRAPER_MAX_DEPTH` | Max subpage depth | `3` |
| `SALES_MAX_TOKENS` | Max output tokens per LLM call | `8192` |

## What it depends on

- [`orkestra-cc/orkestra-sdk`](https://github.com/orkestra-cc/orkestra-sdk) — the kernel contract.
- [`orkestra-cc/orkestra-addon-aimodels`](https://github.com/orkestra-cc/orkestra-addon-aimodels) at runtime — sales reads `iface.AIModelProvider` from the kernel's `ServiceRegistry` to mint per-tenant LLM clients (Ollama, OpenAI, Anthropic, or Gemini, depending on the user's `sales_settings`).
- `github.com/gocolly/colly/v2` for the website scraper.
- MongoDB for persistence.
- *(Optional)* the documents addon for PDF rendering — when present, reports get a downloadable PDF; otherwise Markdown + HTML are still available.

See [`CLAUDE.md`](CLAUDE.md) for the per-package map, the agent + skill catalogues, MongoDB indexes, the batch-poller flow for async LLM responses, and the relocation note for the `SalesConfig` struct (formerly at `shared/config`, moved into this addon in Phase 5e of the SDK split).

## Versioning

Standard Go semver. `v0.x` allows breaking changes (rare); `v1.x` will freeze the public surface alongside the rest of the Orkestra addon ecosystem.

## Contributing

Most development happens in the [upstream monorepo](https://github.com/orkestra-cc/orkestra) where this addon lives alongside the kernel and its consumers — so a change can be implemented, tested, and reviewed against real callers in one diff. PRs against this standalone repo are welcome for addon-only changes (new agents, new skills, scraper improvements scoped to this tree).

See [CONTRIBUTING.md](https://github.com/orkestra-cc/orkestra/blob/main/CONTRIBUTING.md) in the monorepo for the contributor flow.

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
