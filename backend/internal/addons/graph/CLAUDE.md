# Graph Database Module

Manages all Memgraph graph database operations: Cypher execution, schema browsing, MAGE graph algorithms, and vector similarity search.

## Architecture

```
handlers/graph_handler.go   ← HTTP layer (Huma v2)
  ├── services/graph_service.go       ← Core: queries, schema, browsing
  ├── services/algorithm_service.go   ← MAGE algorithms (PageRank, Louvain, etc.)
  └── services/vector_service.go      ← Vector indexes + similarity search
        └── repository/graph_repository.go  ← Bolt driver (neo4j-go-driver/v5)
```

The handler injects three service interfaces. All services share one `GraphRepository` for Memgraph access.

## Database: Memgraph

- Connected via **Bolt protocol** using `neo4j-go-driver/v5` (same driver, compatible API)
- **Single-database system** — `ListDatabases()` returns a synthetic entry
- Config: `GRAPH_ENABLED`, `GRAPH_URI`, `GRAPH_USERNAME`, `GRAPH_PASSWORD`, `GRAPH_DATABASE`
- No auth by default — uses `neo4j.NoAuth()` when username is empty

## Endpoints

### Health & Schema (`/v1/graph/`)
| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Check connectivity |
| GET | `/databases` | List databases (single entry for Memgraph) |
| GET | `/schema` | Labels, relationships, indexes, constraints with counts |

### Query & Browsing (`/v1/graph/`)
| Method | Path | Description |
|--------|------|-------------|
| POST | `/query` | Execute Cypher query |
| GET | `/nodes` | Browse nodes with label filter + pagination |
| GET | `/relationships` | Browse relationships with type filter + pagination |
| GET | `/nodes/{nodeId}/neighbors` | Neighborhood subgraph (configurable depth) |

### Algorithms (`/v1/graph/algorithms`)
| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | List available MAGE algorithms |
| POST | `/` | Run algorithm (pageRank, louvain, wcc, etc.) |

### Vector Search (`/v1/graph/vector/`)
| Method | Path | Description |
|--------|------|-------------|
| POST | `/search` | Similarity search with topK + minScore |
| GET | `/indexes` | List vector indexes |
| POST | `/indexes` | Create vector index |
| DELETE | `/indexes/{name}` | Drop vector index |

## Memgraph-Specific Patterns

### Auto-Commit Transactions

Memgraph requires certain commands to run outside managed transactions:

```go
// These MUST use ExecuteAutoCommit (not ExecuteRead/ExecuteWrite):
// - CREATE INDEX / DROP INDEX
// - CREATE VECTOR INDEX
// - SHOW INDEX INFO / SHOW CONSTRAINT INFO
err := repo.ExecuteAutoCommit(ctx, "", "CREATE VECTOR INDEX ...", nil)
```

The repository provides three execution methods:
- `ExecuteRead()` — managed read transaction
- `ExecuteWrite()` — managed write transaction
- `ExecuteAutoCommit()` — implicit transaction (for DDL/storage commands)

### Schema Introspection

Memgraph doesn't support Neo4j's `CALL db.labels()` or `SHOW INDEXES`. Instead:

```cypher
-- Labels (pure Cypher, works in managed transaction)
MATCH (n) UNWIND labels(n) AS label RETURN DISTINCT label

-- Relationship types
MATCH ()-[r]->() RETURN DISTINCT type(r) AS relationshipType

-- Indexes (auto-commit required)
SHOW INDEX INFO  -- columns: "index type", "label", "property"

-- Constraints (auto-commit required)
SHOW CONSTRAINT INFO  -- columns: "constraint type", "label", "properties"
```

### Vector Search Syntax

```cypher
-- Create index (auto-commit)
CREATE VECTOR INDEX name ON :Label(property)
  WITH CONFIG {"dimension": 4096, "capacity": 100000, "metric": "cos"}

-- Search (managed transaction)
CALL vector_search.search($indexName, $topK, $queryVector)
YIELD node, similarity
WITH node, similarity          -- required: WHERE can't follow YIELD directly
WHERE similarity >= $minScore
RETURN node, similarity
```

Supported metrics: `cos` (cosine), `l2sq` (Euclidean), `ip` (inner product).

### MAGE Algorithms

Algorithms run directly on the stored graph — no projection step (unlike Neo4j GDS):

```go
var mageAlgorithms = map[string]mageAlgorithmDef{
    "pageRank":              {Procedure: "pagerank.get", Yield: "YIELD node, rank"},
    "betweennessCentrality": {Procedure: "betweenness_centrality.get", Yield: "YIELD node, betweenness_centrality"},
    "louvain":               {Procedure: "community_detection.get", Yield: "YIELD node, community_id"},
    "labelPropagation":      {Procedure: "label_propagation.get", Yield: "YIELD node, label"},
    "wcc":                   {Procedure: "weakly_connected_components.get", Yield: "YIELD node, component_id"},
}
```

To add a new algorithm: add an entry to `mageAlgorithms` map and `ListAlgorithms()` in `algorithm_service.go`.

## Result Processing

`collectResults()` in the repository automatically:
1. Extracts column names and row data
2. Detects `dbtype.Node`, `dbtype.Relationship`, `dbtype.Path` values
3. Populates a `GraphData` struct with deduplicated nodes/relationships for visualization
4. Collects query counters (nodes created/deleted, etc.) from the summary

## Write Detection

`ExecuteQuery` in the service detects write operations by checking for keywords: `CREATE`, `MERGE`, `DELETE`, `DETACH`, `SET`, `REMOVE`, `DROP`. When `readOnly=true` is requested, write queries are rejected.

## Key Files

| File | Purpose |
|------|---------|
| `routes.go` | Huma route registration |
| `handlers/graph_handler.go` | HTTP handlers for all endpoints |
| `services/graph_service.go` | Core graph operations + schema |
| `services/algorithm_service.go` | MAGE algorithm mapping + execution |
| `services/vector_service.go` | Vector index management + search |
| `repository/graph_repository.go` | Bolt driver, result marshaling, schema queries |
| `models/models.go` | GraphNode, GraphRelationship, QueryResult |
| `models/gds.go` | AlgorithmInfo, VectorIndex, search/index requests |
| `models/dto.go` | Huma request/response DTOs |

## Adding New Features

1. **New endpoint**: Add handler method → register in `routes.go`
2. **New algorithm**: Add to `mageAlgorithms` map + `ListAlgorithms()`
3. **New vector metric**: Update `CreateVectorIndexRequest.Similarity` enum
4. **Schema DDL**: Always use `ExecuteAutoCommit`, never managed transactions
