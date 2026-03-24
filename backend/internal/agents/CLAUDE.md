# Agents Module

AI agent system powered by [Hindsight](https://hindsight.vectorize.io/) persistent memory. Each project gets a scoped Hindsight memory bank and queries the existing graphRAG for document retrieval.

## Architecture

```
handlers/
  ├── project_handler.go    ← Project CRUD endpoints
  └── agent_handler.go      ← Agent query + conversation endpoints
services/
  ├── project_service.go    ← Project CRUD + Hindsight bank lifecycle
  ├── agent_service.go      ← Query orchestration: RAG → Retain → Reflect
  ├── hindsight_client.go   ← Thin wrapper around Hindsight Go SDK
  └── rag_bridge.go         ← Consumer interface for scoped RAG queries
models/
  ├── project.go            ← Project MongoDB model
  ├── conversation.go       ← Conversation + Message models
  ├── role_config.go        ← Persona definitions + RBAC validation
  └── dto.go                ← Huma request/response DTOs
repository/
  ├── project_repository.go       ← MongoDB: agent_projects
  └── conversation_repository.go  ← MongoDB: agent_conversations
routes.go                   ← RegisterProjectRoutes, RegisterQueryRoutes, RegisterAdminRoutes
```

## Data Storage

| Data | Storage | Collection |
|------|---------|------------|
| Project metadata | MongoDB | `agent_projects` |
| Conversations | MongoDB | `agent_conversations` |
| Agent memory | Hindsight | Bank per project |
| Document chunks | Memgraph | Via RAG module |

## Query Orchestration Flow

```
1. Validate project + resolve persona
2. Merge project AgentSettings with persona defaults (system prompt, directives, disposition)
3. RAG Phase: QueryWithScope(question, project.documentUUIDs)
4. Retain Phase (async): Store RAG results in Hindsight bank
5. Reflect Phase: Hindsight combines persistent memory + RAG context + persona directives
6. Capture token usage from Hindsight response (inputTokens, outputTokens, totalTokens)
7. Save conversation + metadata to MongoDB
8. Return answer + sources + metadata + token counts
```

## Personas

Query-time behavior profiles (not RBAC roles). Users select any persona at or below their system role level.

| Persona | Focus | Min RBAC |
|---------|-------|----------|
| developer | Technical details, raw data | developer |
| administrator | Comprehensive, compliance | administrator |
| auditor | Evidence-based, compliance | administrator |
| manager | Summaries, business impact | manager |
| guest | General overviews | guest |

## Endpoints

### Projects (`/v1/agents/projects`) — manager role
| Method | Path | Description |
|--------|------|-------------|
| POST | `/` | Create project + Hindsight bank |
| GET | `/` | List projects |
| GET | `/{uuid}` | Get project |
| PATCH | `/{uuid}` | Update project |
| DELETE | `/{uuid}` | Delete project + bank |
| POST | `/{uuid}/documents` | Add documents |
| DELETE | `/{uuid}/documents` | Remove documents |
| PATCH | `/{uuid}/filters` | Update filters |
| GET | `/{uuid}/settings` | Get agent settings |
| PATCH | `/{uuid}/settings` | Update agent settings |

### Query (`/v1/agents/projects`) — operator role
| Method | Path | Description |
|--------|------|-------------|
| POST | `/{uuid}/query` | Query agent |
| POST | `/{uuid}/conversations` | New conversation |
| GET | `/{uuid}/conversations` | List conversations |
| GET | `/conversations/{uuid}` | Get conversation |
| DELETE | `/conversations/{uuid}` | Delete conversation |

### Admin (`/v1/agents`) — administrator role
| Method | Path | Description |
|--------|------|-------------|
| GET | `/projects/{uuid}/bank` | Hindsight bank info |
| GET | `/health` | Hindsight health check |

## Configuration

```
AGENTS_ENABLED=true
HINDSIGHT_URL=http://hindsight:8888
HINDSIGHT_NAMESPACE=orkestra
```

## Per-Project Agent Settings

Stored in `AgentSettings` on the Project model. All optional — unset fields fall back to persona defaults.

| Setting | Effect |
|---------|--------|
| `systemPrompt` | Overrides persona's default system context |
| `directives` | Extra rules merged on top of persona directives |
| `skepticism` | 1=trusting, 5=strict to docs (0=persona default) |
| `literalism` | 1=creative, 5=literal (0=persona default) |
| `empathy` | 1=detached, 5=helpful/warm (0=persona default) |
| `temperature` | "precise", "balanced", "creative" |
| `language` | Force response language (e.g. "en", "it") |
| `maxTokens` | Response length budget |

## Token Usage Tracking

Each assistant message records Hindsight Reflect token usage:
- `inputTokens` — question + RAG context + directives sent to LLM
- `outputTokens` — generated response tokens
- `totalTokens` — sum

Token data flows: Hindsight API response → `MsgMeta` → MongoDB → frontend display.

## Graceful Degradation

If Hindsight is unreachable, queries fall back to RAG-only mode (skip Retain + Reflect, use RAG answer directly).
