<div align="center">

# Orkestra AI Models addon

**Multi-provider AI/LLM model management for the [Orkestra](https://github.com/orkestra-cc/orkestra) modular monolith. One admin surface for credentials, capability declarations, and per-tenant default-model resolution — implemented once, consumed by every downstream addon (RAG, sales, agents) through the `iface.AIModelProvider` contract.**

[![Go Reference](https://pkg.go.dev/badge/github.com/orkestra-cc/orkestra-addon-aimodels.svg)](https://pkg.go.dev/github.com/orkestra-cc/orkestra-addon-aimodels)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white&style=flat-square)](https://go.dev)
[![Module](https://img.shields.io/badge/module-github.com%2Forkestra--cc%2Forkestra--addon--aimodels-blue?style=flat-square)](https://github.com/orkestra-cc/orkestra-addon-aimodels)
[![Latest tag](https://img.shields.io/github/v/tag/orkestra-cc/orkestra-addon-aimodels?sort=semver&style=flat-square)](https://github.com/orkestra-cc/orkestra-addon-aimodels/tags)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat-square)](LICENSE)

[SDK](https://github.com/orkestra-cc/orkestra-sdk) · [Monorepo](https://github.com/orkestra-cc/orkestra) · [Module docs](CLAUDE.md)

</div>

---

## What this is

A self-contained Orkestra addon implementing the `Module` interface from [`orkestra-cc/orkestra-sdk`](https://github.com/orkestra-cc/orkestra-sdk). It owns the `ai_models` MongoDB collection, exposes `/v1/ai/models/*` CRUD + connectivity-test endpoints, and registers an `AIModelService` in the kernel's `ServiceRegistry` so downstream addons can resolve embeddings / completions providers per tenant without re-implementing credential storage.

**Supported providers:**

| Provider | Embedding | LLM | Streaming | Auth | Model discovery |
|---|---|---|---|---|---|
| `ollama` | ✓ | ✓ | ✓ | none | `/api/tags` |
| `openai` (also llama.cpp, vLLM, LocalAI) | ✓ | ✓ | ✓ | API key or none | `ListModels` |
| `anthropic` | — | ✓ | ✓ | `x-api-key` | hardcoded |
| `gemini` | ✓ | ✓ | ✓ | `?key=` | `GET /v1beta/models` |

## How it ships

The same source tree lives in two places:

- **In-tree** at [`backend/internal/addons/aimodels/`](https://github.com/orkestra-cc/orkestra/tree/main/backend/internal/addons/aimodels) inside the [orkestra-cc/orkestra](https://github.com/orkestra-cc/orkestra) monorepo, where cross-module development happens.
- **Standalone** at this repository, tagged from `v0.1.0`, consumed via the Go module proxy by anything outside the monorepo.

The upstream backend's `go.mod` looks roughly like this:

```go
require github.com/orkestra-cc/orkestra-addon-aimodels v0.1.0
replace github.com/orkestra-cc/orkestra-addon-aimodels => ./internal/addons/aimodels
```

The `replace` will retire once cross-cutting addon churn settles.

## Install

```bash
go get github.com/orkestra-cc/orkestra-addon-aimodels@latest
```

Requires Go 1.25.10 or newer.

```go
import aimodels "github.com/orkestra-cc/orkestra-addon-aimodels"
```

## Boot in a host

The addon plugs into any host that implements the Orkestra SDK kernel. Register it alongside the other catalog entries:

```go
import (
    aimodels "github.com/orkestra-cc/orkestra-addon-aimodels"
    "github.com/orkestra-cc/orkestra-sdk/module"
)

reg := module.NewRegistry( /* ... */ )
reg.Register("aimodels", aimodels.NewModule())
```

The registry takes care of init order (aimodels has no hard dependencies), Mongo collection creation, route mounting (operator-tier `/v1/ai/models/*`), and module-gate middleware for runtime enable/disable via `/admin/modules`.

Consumer modules pull the service via the standard typed-getter pattern:

```go
import (
    "github.com/orkestra-cc/orkestra-sdk/iface"
    "github.com/orkestra-cc/orkestra-sdk/module"
)

svc, ok := module.GetTyped[iface.AIModelProvider](deps.Services, iface.AIModelProviderKey)
```

## Configuration

| Env var | Purpose |
|---|---|
| `OLLAMA_BASE_URL` | Default Ollama endpoint (e.g. `http://ollama:11434`) |
| `OPENAI_API_KEY` | Default OpenAI fallback when a model record's `APIKey` is empty |
| `ANTHROPIC_API_KEY` | Default Anthropic fallback |
| `GEMINI_API_KEY` | Default Gemini fallback |

All four are also editable at runtime under `/admin/modules/aimodels` with AES-256-GCM at-rest encryption for API-key fields. The admin UI's edit form is rendered from this module's `ConfigSchema()` declaration.

## API key handling

- `APIKey` fields carry `json:"-"` so they never appear in `/v1/ai/models/*` responses.
- Empty per-model keys fall back to the matching global env var.
- Local providers (Ollama, llama.cpp) don't require keys.

See [`CLAUDE.md`](CLAUDE.md) for the per-package map, endpoint table, RBAC surface, and inter-module integration details.

## Versioning

Standard Go semver. `v0.x` allows breaking changes (rare); `v1.x` will freeze the public surface alongside the rest of the Orkestra addon ecosystem.

## Contributing

Most development happens in the [upstream monorepo](https://github.com/orkestra-cc/orkestra) where this addon lives alongside the kernel and its consumers — so a change can be implemented, tested, and reviewed against real callers in one diff. PRs against this standalone repo are welcome for addon-only changes (provider additions, bug fixes scoped to this tree).

See [CONTRIBUTING.md](https://github.com/orkestra-cc/orkestra/blob/main/CONTRIBUTING.md) in the monorepo for the contributor flow.

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
