# AI Models Module

Standalone module for managing AI/LLM model configurations. Supports local (Ollama, llama.cpp, vLLM) and cloud (OpenAI, Anthropic, Google Gemini) providers.

## Architecture

```
models/
  ├── model_config.go     ← ModelConfig struct + ProviderCategoryFor()
  └── dto.go              ← Huma request/response DTOs
providers/
  ├── interfaces.go       ← EmbeddingProvider + LLMProvider interfaces
  ├── types.go            ← ModelConfig, CompletionOptions, StreamChunk, RemoteModel
  ├── factory.go          ← Provider creation + TestConnectivity + FetchAvailableModels
  ├── ollama.go           ← Ollama REST API implementation
  ├── openai.go           ← OpenAI-compatible (also llama.cpp, vLLM, LocalAI)
  ├── anthropic.go        ← Anthropic Claude (LLM only, no embeddings)
  └── gemini.go           ← Google Gemini (LLM + Embedding)
repository/
  └── model_repository.go ← MongoDB: ai_models collection
services/
  └── model_service.go    ← AIModelService interface + implementation
handlers/
  └── model_handler.go    ← HTTP handlers for /v1/ai/models/*
routes.go                 ← Huma route registration
```

## Endpoints (`/v1/ai/models`)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/` | Create model config |
| GET | `/` | List models (`?type=`, `?provider=`, `?category=`) |
| GET | `/{uuid}` | Get model |
| PATCH | `/{uuid}` | Update model |
| DELETE | `/{uuid}` | Delete model |
| POST | `/{uuid}/default` | Set as default for its type |
| POST | `/{uuid}/test` | Test connectivity (persists result) |
| POST | `/{uuid}/prompt` | Quick prompt test (LLM only) |
| POST | `/fetch` | Fetch available models from provider |

## Supported Providers

| Provider | Embedding | LLM | Streaming | Auth | Model Discovery |
|----------|-----------|-----|-----------|------|-----------------|
| `ollama` | Yes | Yes | Yes | None | `/api/tags` |
| `openai` | Yes | Yes | Yes | API key or none | `ListModels` |
| `anthropic` | No | Yes | Yes | `x-api-key` header | Hardcoded list |
| `gemini` | Yes | Yes | Yes | `?key=` query param | `GET /v1beta/models` |

## Inter-Module Communication

The `AIModelService` interface is consumed by:
- **RAG module** — via `AIModelProvider` consumer interface for embedding/LLM provider resolution
- **Future modules** — any module needing AI model access

## Configuration

```
AIMODELS_ENABLED=true         # Enable module
OLLAMA_BASE_URL=...           # Default Ollama endpoint
OPENAI_API_KEY=...            # Default OpenAI key
ANTHROPIC_API_KEY=...         # Default Anthropic key
GEMINI_API_KEY=...            # Default Gemini key
```

## API Key Security

- `APIKey` field has `json:"-"` tag — never returned in API responses
- Global env var fallbacks applied when model-level key is empty
- Local providers (Ollama, llama.cpp) don't require API keys
