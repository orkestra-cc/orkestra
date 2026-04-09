# RAG Module

Retrieval-Augmented Generation system for ISO norm compliance. Manages AI model configs, document ingestion into the knowledge graph, and question answering with source citations.

## Architecture

```
handlers/
  ├── model_handler.go      ← AI model CRUD + connectivity testing
  ├── document_handler.go   ← Document upload + ingestion
  └── query_handler.go      ← RAG query execution
services/
  ├── model_service.go      ← Model management, provider factory, seeding
  ├── ingestion_service.go  ← Full pipeline: extract → parse → chunk → embed → graph
  ├── query_service.go      ← Vector search → context expansion → LLM
  ├── markdown_parser.go    ← Goldmark-based CommonMark AST → StructuralNode tree
  ├── structural_parser.go  ← Regex-based parser for ISO/legal documents
  ├── chunker.go            ← Text chunking respecting structural boundaries
  └── text_extractor.go     ← PDF/text/markdown extraction
providers/
  ├── embedding.go          ← EmbeddingProvider interface
  ├── llm.go                ← LLMProvider interface
  ├── ollama.go             ← Ollama implementation (REST API)
  ├── openai.go             ← OpenAI + compatible APIs (llama.cpp, vLLM)
  └── factory.go            ← Provider creation + model fetching
repository/
  ├── model_repository.go   ← MongoDB: rag_models collection
  └── document_repository.go ← MongoDB: rag_documents collection
```

## Data Storage Split

| Data | Storage | Why |
|------|---------|-----|
| Model configs | MongoDB `rag_models` | CRUD metadata |
| Document metadata + status | MongoDB `rag_documents` | Status tracking, file info |
| Document graph nodes | Memgraph `:RagDocument` | Graph traversal |
| Chunk nodes + embeddings | Memgraph `:RagChunk` | Vector search + context |
| Relationships | Memgraph edges | Graph RAG context expansion (intra + cross-document) |

## Endpoints

### Models (`/v1/rag/models`)
| Method | Path | Description |
|--------|------|-------------|
| POST | `/` | Create model config |
| GET | `/` | List models (`?type=embedding\|llm`) |
| GET | `/{uuid}` | Get model |
| PATCH | `/{uuid}` | Update model |
| DELETE | `/{uuid}` | Delete model |
| POST | `/{uuid}/default` | Set as default for its type |
| POST | `/{uuid}/test` | Test provider connectivity |
| POST | `/fetch` | Fetch available models from provider URL |

### Documents (`/v1/rag/documents`)
| Method | Path | Description |
|--------|------|-------------|
| POST | `/` | Upload + ingest (multipart form: file, title, isoStandard, version) |
| GET | `/` | List documents (`?status=...&isoStandard=...`) |
| GET | `/{uuid}` | Get document details |
| PATCH | `/{uuid}` | Update metadata (title, isoStandard, version) |
| GET | `/{uuid}/chunks` | Get document chunks |
| GET | `/{uuid}/sections` | Get document section tree |
| GET | `/{uuid}/relations` | Get cross-document similarity links and related doc summaries |
| POST | `/{uuid}/reprocess` | Clear graph nodes for re-ingestion |
| DELETE | `/{uuid}` | Delete from MongoDB + Memgraph |

### Query (`/v1/rag/query`)
| Method | Path | Description |
|--------|------|-------------|
| POST | `/` | Ask question, get answer with sources |

## AI Provider System

Two provider interfaces with multiple implementations:

### EmbeddingProvider
```go
type EmbeddingProvider interface {
    Embed(ctx, text) ([]float64, error)
    EmbedBatch(ctx, texts) ([][]float64, error)
    Dimensions() int
    ModelName() string
}
```

### LLMProvider
```go
type LLMProvider interface {
    Complete(ctx, prompt, opts) (string, error)
    StreamComplete(ctx, prompt, opts) (<-chan StreamChunk, error)
    ModelName() string
}
```

### Supported Providers

| Provider | Config | Notes |
|----------|--------|-------|
| `openai` | BaseURL + optional APIKey | Works with OpenAI cloud, llama.cpp, vLLM, LocalAI |
| `ollama` | BaseURL | Ollama REST API (`/api/embeddings`, `/api/generate`) |

The `openai` provider supports custom base URLs via `openai.NewClientWithConfig()`, making it compatible with any OpenAI-compatible API server.

**Current defaults**: llama.cpp at `192.168.88.52` — embedding on `:8082/v1`, LLM on `:8081/v1`.

### API Key Security

- `APIKey` field has `json:"-"` tag — never returned in API responses
- Global fallback: `OPENAI_API_KEY` env var applied when model-level key is empty
- Local servers (llama.cpp, Ollama) don't require API keys

## Document Ingestion Pipeline

Runs asynchronously in a background goroutine (17 steps):

```
1.  Create MongoDB record (status: "pending")
2.  Update status → "processing"
3.  Extract text (TextExtractor: PDF via Gotenberg, plain text/markdown as-is)
4.  Parse document structure:
    - Markdown (.md) → goldmark CommonMark AST → StructuralNode tree (markdown_parser.go)
    - PDF/TXT        → regex-based line-by-line parser (structural_parser.go)
5.  Chunk using structural boundaries (chunker.go: ChunkStructured)
6.  Generate embeddings (EmbeddingProvider.Embed per chunk)
7.  Create :RagDocument node in Memgraph
8.  Create :RagSection nodes from structural tree
9.  Create HAS_SECTION relationships (document → top-level sections)
10. Create CONTAINS relationships (section → child sections)
11. Create NEXT_SECTION relationships (sequential siblings)
12. Create :RagChunk nodes with embeddings
13. Create CONTAINS relationships (section → chunks)
14. Create NEXT relationships between sequential chunks
15. Extract definitions → :RagDefinition nodes + DEFINES edges
16. Resolve cross-references → REFERENCES edges
17. Compute intra-document SIMILAR_TO edges (cosine > 0.85 threshold)
18. Compute cross-document SIMILAR_TO edges via vector search index
    - For each new chunk, find top 3 similar chunks from OTHER documents
    - Uses Memgraph vector_search (no LLM calls — embedding-only)
    - Edges tagged with `crossDocument: true` property
19. Create vector + property indexes
20. Update status → "completed"
```

### Memgraph Graph Schema

```cypher
(:RagDocument {uuid, title, isoStandard, version, documentCategory, docType, chunkCount, createdAt})
(:RagSection {uuid, documentUuid, nodeType, numbering, title, depth, fullPath, position})
(:RagChunk {uuid, documentUuid, text, position, nodeType, numbering, fullPath, requirementLevel, depth, sectionUuid, embedding})
(:RagDefinition {uuid, documentUuid, term, definition, embedding})

(RagDocument)-[:HAS_SECTION]->(RagSection)
(RagSection)-[:CONTAINS]->(RagSection)
(RagSection)-[:CONTAINS]->(RagChunk)
(RagSection)-[:NEXT_SECTION]->(RagSection)
(RagChunk)-[:NEXT]->(RagChunk)
(RagDocument)-[:HAS_DEFINITION]->(RagDefinition)
(RagDefinition)-[:DEFINES]->(RagChunk)
(RagChunk)-[:REFERENCES {referenceText}]->(RagSection)
(RagChunk)-[:SIMILAR_TO {similarity, crossDocument?}]->(RagChunk)
```

### Structural Parsing

Two parsers produce the same `StructuralNode` tree — the downstream chunking, embedding, and graph creation pipeline is shared.

**Markdown Parser** (`markdown_parser.go`) — used for `.md` files:
- Uses **goldmark** (Go CommonMark reference implementation) to parse into a proper AST
- Heading levels (`#` count) map to tree depth: `#` → clause (depth 1), `##`+ → subclause (depth 2+)
- Extracts leading numbering from headings (e.g., `## 4.1 Understanding` → numbering `4.1`, title `Understanding`)
- Handles: headings, paragraphs, fenced/indented code blocks (`code_block` node type), ordered/unordered/nested lists (`list_item`), blockquotes (`blockquote`), GFM tables (rendered as pipe-delimited text), thematic breaks, inline formatting (bold, italic, code spans, links, images)
- Text before any heading becomes a "Preamble" node
- Detects requirement language (SHALL/SHOULD/MAY) on all nodes

**ISO/Legal Parser** (`structural_parser.go`) — used for `.pdf`, `.txt` files:
- State-machine parser processes text line-by-line with ~15 regex patterns
- Supports ISO standards (Clause, Section, Annex) AND Italian legal docs (TITOLO, CAPO, SEZIONE, Articolo)
- Builds a hierarchical `StructuralNode` tree
- Detects requirement language: SHALL/SHOULD/MAY (EN) + deve/dovrebbe/può (IT)
- Extracts cross-references: "see 4.1.3", "Art. 12", "Annex A", etc.

### Chunking

**Tree → Chunks** (`chunker.go`):
- Leaf-first: each leaf node becomes a chunk (128–1024 chars)
- Merges small adjacent siblings under same parent
- Splits oversized nodes at sentence boundaries
- Every chunk inherits `fullPath` metadata (e.g., "Clause 4 > 4.1 > 4.1.2")
- No overlap needed — context recovered via graph traversal

### Relationship Extraction (`relationship_extractor.go`)

- **Definitions**: Parsed from "Terms and definitions" sections → `RagDefinition` nodes
- **Cross-references**: Regex-based, resolved to target `RagSection` by numbering
- **Similarity (intra-doc)**: Pairwise cosine between non-adjacent chunks within same doc, edges for pairs > 0.85
- **Similarity (cross-doc)**: Vector search finds top 3 similar chunks from other documents per new chunk. Uses embedding index, no LLM calls. Edges tagged `crossDocument: true`

## RAG Query Pipeline

```
1. Embed question → default EmbeddingProvider
2. Vector search with optional filters (requirementLevel, nodeType)
3. Context expansion:
   - "vector" mode: prev/next chunks via NEXT edges
   - "graph" mode: section hierarchy + cross-refs + definitions + cross-document similar chunks via SIMILAR_TO edges
4. Fetch document metadata → title, isoStandard from :RagDocument nodes
5. Build richer prompt with structural paths + requirement levels + definitions
6. LLM generation → default LLMProvider.Complete() with 5-minute timeout
7. Return → answer + source citations + timing metadata
```

### System Prompt

The LLM is instructed as an ISO compliance expert. `/no_think` is appended to disable reasoning mode in models like Qwen 3.5.

### Timeouts

- LLM generation uses a dedicated `context.WithTimeout(5 * time.Minute)` — separate from the HTTP request context
- HTTP server WriteTimeout set to 5 minutes to accommodate local LLM inference
- Embedding calls use standard HTTP client timeout (120s)

## Configuration

```
RAG_ENABLED=true              # Enable/disable module
OLLAMA_BASE_URL=...           # Default Ollama endpoint
OPENAI_API_KEY=...            # Default OpenAI API key
RAG_CHUNK_SIZE=512            # Default chunk size (chars)
RAG_CHUNK_OVERLAP=50          # Default overlap (chars)
RAG_DEFAULT_TOP_K=10          # Default vector search results
```

## Default Model Seeding

On first startup (when `rag_models` collection is empty), seeds two models:
- **Embedding**: Qwen3-Embedding-8B at `192.168.88.52:8082/v1` (4096d)
- **LLM**: Qwen3.5-9B at `192.168.88.52:8081/v1`

Both use the `openai` provider (llama.cpp is OpenAI-compatible).

## Adding a New Provider

1. Implement `EmbeddingProvider` and/or `LLMProvider` in `providers/`
2. Add a case to `NewEmbeddingProvider`/`NewLLMProvider` in `factory.go`
3. Add a case to `TestConnectivity` and `FetchAvailableModels` in `factory.go`
4. Update `ModelConfig.Provider` enum in DTOs
