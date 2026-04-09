# Orkestra — Architectural Assessment & Modernization Strategy

**Version**: 1.0  
**Date**: April 2026  
**Classification**: Internal — Technical Leadership

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Current Architecture Assessment](#2-current-architecture-assessment)
3. [Target Architecture: Modular Plugin System](#3-target-architecture-modular-plugin-system)
4. [Backend Modernization](#4-backend-modernization)
5. [Frontend Modernization](#5-frontend-modernization)
6. [Scalability Strategy](#6-scalability-strategy)
7. [Infrastructure & DevOps](#7-infrastructure--devops)
8. [Data Architecture](#8-data-architecture)
9. [Migration Roadmap](#9-migration-roadmap)
10. [Risk Assessment](#10-risk-assessment)
11. [Appendix: Reference Architecture Diagram](#11-appendix-reference-architecture-diagram)

---

## 1. Executive Summary

### Current State

Orkestra is a modern professional management system built across ~136 commits in approximately 3.5 months. It comprises:

- **Backend**: Monolithic Go 1.25.1 application with 14 domain modules, Huma v2 REST framework, MongoDB 8.0, Redis 8.2
- **Frontend**: React 19 + TypeScript + Vite 7 application with ~41 common components, Redux Toolkit, 17 RTK Query API slices
- **Mobile**: Flutter 3.35+ cross-platform app with Riverpod state management (early stage)
- **Infrastructure**: Docker Compose deployment across 4 compose files, interactive bash deployment script

The system handles professional business management, Italian electronic invoicing (FatturaPA/SDI), sales intelligence with AI-driven prospect analysis, AI agents (Hindsight), RAG pipeline with graph database, PDF document generation, and comprehensive reporting.

### Why Modernize Now

The monolithic architecture has served the rapid development phase well, but the system is approaching three inflection points:

| Inflection Point          | Evidence                                                                                                 |
| ------------------------- | -------------------------------------------------------------------------------------------------------- |
| **Wiring Complexity**     | `cmd/server/main.go` is 1,402 lines — every new module requires conditional init blocks and route groups |
| **Inter-Module Coupling** | RAG imports graph repository directly; Sales imports aimodels service; Agents imports RAG service        |
| **Delivery Risk**         | No CI/CD pipeline — deployment is via interactive `deploy.sh` with optional (not enforced) testing       |

### Three Strategic Pillars

```
1. MODULARIZE     →  Clean module boundaries with plugin interfaces
2. AUTOMATE       →  CI/CD pipeline, GitOps, automated quality gates
3. SCALE          →  Kubernetes orchestration for 10,000+ concurrent users
```

### Key Recommendations

1. **Do NOT rewrite** — adopt a "Strangler Fig" approach, progressively extracting clean boundaries
2. **Compile-time Module interface** in Go (not runtime plugins) — standardized lifecycle for all 14 modules
3. **NATS JetStream** event bus for decoupled inter-module communication
4. **Kubernetes** with Helm charts, replacing Docker Compose for production
5. **GitHub Actions CI/CD** with automated testing gates before any deployment
6. **Frontend monorepo** with extracted design system package

---

## 2. Current Architecture Assessment

### 2.1 Strengths — What to Preserve

| #   | Strength                            | Evidence                                                                                               |
| --- | ----------------------------------- | ------------------------------------------------------------------------------------------------------ |
| 1   | **Consistent 3-layer architecture** | Every module follows Handler → Service → Repository across all 14 modules in `internal/`               |
| 2   | **Interface-based design**          | AI Model Provider interface shared by RAG and Sales, OAuth provider factory, email service interface   |
| 3   | **OpenAPI auto-generation**         | Huma v2 generates always-in-sync API documentation at `/docs`                                          |
| 4   | **Security maturity**               | OAuth 2.1 with PKCE (Google, Apple, GitHub, Discord), RS256 JWT, 6-role RBAC hierarchy, HSTS           |
| 5   | **Feature-flag modules**            | Modules (billing, graph, RAG, agents, sales) enable/disable via config flags — already isolation-ready |
| 6   | **Multi-provider AI architecture**  | Unified AI model management across Ollama, OpenAI, Anthropic, Gemini with consumer interfaces          |
| 7   | **Italian billing integration**     | Complete FatturaPA/SDI pipeline with XML generation, webhook reception, polling, and PDF invoicing     |
| 8   | **Multi-stage Docker builds**       | Separate dev/staging/prod configurations with AIR hot-reload for development                           |
| 9   | **Comprehensive error system**      | Structured error handling with rate limiting, input sanitization, and request validation               |
| 10  | **Module-level documentation**      | CLAUDE.md files per module provide excellent developer onboarding                                      |

### 2.2 Pain Points — What Must Change

| #   | Pain Point                           | Impact                                                                                              | Location                   |
| --- | ------------------------------------ | --------------------------------------------------------------------------------------------------- | -------------------------- |
| 1   | **1,402-line wiring function**       | Adding any module requires conditional init blocks, handler declarations, and route groups          | `cmd/server/main.go`       |
| 2   | **No module registration interface** | Each module is wired differently — conditional blocks with `moduleEnabled` flags                    | `cmd/server/main.go`       |
| 3   | **Cross-module concrete coupling**   | RAG imports graph repository; Agents imports RAG service; Sales imports aimodels                    | Throughout `internal/`     |
| 4   | **No event bus**                     | All cross-module coordination is synchronous function calls                                         | Throughout `internal/`     |
| 5   | **No CI/CD pipeline**                | Interactive bash script, no automated testing gates                                                 | `deploy.sh`                |
| 6   | **Single MongoDB connection**        | All 14 modules share one `*mongo.Database`, no read replicas, no per-module timeouts                | `shared/database/mongo.go` |
| 7   | **Frontend tag proliferation**       | ~40 cache tags in single `baseApi.ts`, growing with every feature                                   | `store/api/baseApi.ts`     |
| 8   | **No automated test enforcement**    | 80% coverage target in docs, but no CI gate enforces it                                             | `CLAUDE.md` vs reality     |
| 9   | **Mixed route registration**         | User and reporting routes registered via helper functions, other modules use RegisterRoutes pattern | `cmd/server/main.go`       |
| 10  | **No API versioning strategy**       | All routes are `/v1/` with no strategy for introducing v2                                           | Route registration         |

### 2.3 Architectural Risk Score

| Area                  |   Score   | Assessment                                                              |
| --------------------- | :-------: | ----------------------------------------------------------------------- |
| Module Separation     |    3/5    | Clean per-module structure, but wiring complexity and concrete coupling |
| API Design            |    4/5    | Huma v2 + OpenAPI auto-gen is strong                                    |
| Security              |    4/5    | OAuth 2.1, multi-provider, RBAC hierarchy, HSTS — production-mature     |
| Observability         |    2/5    | Basic logging present, no metrics pipeline, no alerting, no SLOs        |
| CI/CD                 |    1/5    | No pipeline exists — critical gap                                       |
| Scalability           |    2/5    | Docker Compose only, no orchestration                                   |
| Frontend Architecture |    3/5    | Clean component separation, but single API slice with growing tags      |
| Data Architecture     |    2/5    | Single MongoDB, no read replicas, no event sourcing                     |
| Testing               |    2/5    | Tests exist but no enforcement or automated gates                       |
| Documentation         |    4/5    | CLAUDE.md per module is excellent                                       |
| **Overall**           | **2.7/5** | **Strong foundations, critical infrastructure gaps**                    |

---

## 3. Target Architecture: Modular Plugin System

### 3.1 Design Philosophy

The goal is **not** to build microservices. The goal is to transform the monolith into a **modular monolith** with clean plugin boundaries that _could_ become independent services if needed, but don't have to.

The existing feature-flag pattern (`billingEnabled`, `ragEnabled`, `salesEnabled`) already demonstrates module isolation thinking. The Module interface formalizes this into a consistent lifecycle.

### 3.2 Backend: Module Registry Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    cmd/server/main.go                     │
│              (slim ~200 lines: boot + register)           │
├─────────────────────────────────────────────────────────┤
│                    Module Registry                        │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐          │
│  │ Auth │ │ User │ │ Bill │ │Sales │ │ RAG  │  ...      │
│  └──┬───┘ └──┬───┘ └──┬───┘ └──┬───┘ └──┬───┘          │
│     │        │        │        │        │                │
├─────┴────────┴────────┴────────┴────────┴───────────────┤
│                    Shared Kernel                          │
│  ┌────────┐ ┌───────┐ ┌──────┐ ┌───────┐ ┌──────────┐  │
│  │Database│ │ Redis │ │Events│ │Config │ │Middleware│  │
│  └────────┘ └───────┘ └──────┘ └───────┘ └──────────┘  │
├─────────────────────────────────────────────────────────┤
│                    Event Bus (NATS)                       │
│  invoice.sent │ prospect.scored │ user.deleted │ ...     │
└─────────────────────────────────────────────────────────┘
```

**Core Module Interface**:

```go
// pkg/module/module.go
package module

type Module interface {
    // Identity
    Name() string
    Version() string
    Dependencies() []string  // names of modules this depends on
    Enabled(cfg *config.Config) bool  // feature-flag check

    // Lifecycle
    Init(deps *Dependencies) error
    RegisterRoutes(public, protected huma.API, mw *middleware.AuthMiddleware)
    RegisterJobs(registry *scheduler.Registry)    // optional
    RegisterEvents(bus *events.Bus)                // optional
    HealthCheck(ctx context.Context) error
    Close() error
}

// Dependencies provides shared infrastructure — no cross-module imports needed
type Dependencies struct {
    DB          *mongo.Database
    Redis       *redis.Client
    Config      *config.Config
    EventBus    *events.Bus
    Logger      *slog.Logger

    // Cross-module service interfaces (typed, not generic)
    UserLookup    UserLookup
    AIModelProvider AIModelProvider
    PDFService    PDFService
    GraphRepo     GraphRepository
}

// Cross-module interfaces — defined in shared kernel, implemented by modules
type UserLookup interface {
    GetByID(ctx context.Context, id string) (*UserInfo, error)
    GetByRole(ctx context.Context, role string) ([]UserInfo, error)
}

type AIModelProvider interface {
    GetModel(ctx context.Context, id string) (*ModelInfo, error)
    ListModels(ctx context.Context, provider string) ([]ModelInfo, error)
    InferChat(ctx context.Context, modelID string, messages []Message) (*ChatResponse, error)
}

type PDFService interface {
    GeneratePDF(ctx context.Context, templateID string, data map[string]any) ([]byte, error)
}

type GraphRepository interface {
    ExecuteQuery(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}
```

**Module Registry**:

```go
// pkg/module/registry.go
type Registry struct {
    modules   map[string]Module
    order     []string  // topologically sorted by dependencies
    deps      *Dependencies
}

func (r *Registry) Register(m Module) error {
    // Validate dependencies exist
    // Add to registry
    // Re-sort initialization order
}

func (r *Registry) InitAll(cfg *config.Config, deps *Dependencies) error {
    // Skip modules where Enabled() returns false
    // Initialize in dependency order
    // Wire cross-module interfaces (UserLookup, AIModelProvider, etc.)
}

func (r *Registry) RegisterAllRoutes(public, protected huma.API, mw *middleware.AuthMiddleware) {
    for _, name := range r.order {
        r.modules[name].RegisterRoutes(public, protected, mw)
    }
}
```

**Resulting `main.go`** (~200 lines):

```go
func main() {
    cfg := config.Load()
    db := database.Connect(cfg)
    redis := database.ConnectRedis(cfg)
    bus := events.NewBus(nats.Connect(cfg.NATS.URL))

    deps := &module.Dependencies{DB: db, Redis: redis, Config: cfg, EventBus: bus}

    registry := module.NewRegistry()
    registry.Register(auth.NewModule())
    registry.Register(user.NewModule())
    registry.Register(navigation.NewModule())
    registry.Register(reporting.NewModule())
    registry.Register(billing.NewModule())
    registry.Register(company.NewModule())
    registry.Register(documents.NewModule())
    registry.Register(graph.NewModule())
    registry.Register(aimodels.NewModule())
    registry.Register(rag.NewModule())
    registry.Register(agents.NewModule())
    registry.Register(sales.NewModule())

    registry.InitAll(cfg, deps)

    router := chi.NewRouter()
    // Global middleware...
    publicAPI := humachi.New(router, apiConfig)
    protectedAPI := humachi.New(protectedRouter, apiConfig)

    registry.RegisterAllRoutes(publicAPI, protectedAPI, authMiddleware)
    registry.RegisterAllJobs(schedulerRegistry)

    http.ListenAndServe(":3000", router)
}
```

### 3.3 Frontend: Package-Based Architecture

```
orkestra/
├── packages/
│   ├── ui/                    # Design system (from components/common/)
│   │   ├── src/
│   │   │   ├── tokens/        # Colors, spacing, typography from SCSS variables
│   │   │   ├── components/    # Avatar, Card, AdvanceTable, Button...
│   │   │   ├── hooks/         # useBreakpoints, useBulkSelect, useAdvanceTable
│   │   │   └── index.ts       # Public API
│   │   ├── package.json
│   │   └── .storybook/        # Visual documentation
│   │
│   ├── api-client/            # RTK Query foundation
│   │   ├── src/
│   │   │   ├── baseApi.ts     # Shared fetchBaseQuery config
│   │   │   ├── auth/          # Auth endpoints + slice
│   │   │   └── types.ts       # Shared API types
│   │   └── package.json
│   │
│   ├── auth/                  # Auth provider, RBAC utilities
│   │   ├── src/
│   │   │   ├── AuthProvider.tsx
│   │   │   ├── ProtectedRoute.tsx
│   │   │   ├── useAuth.ts
│   │   │   └── roleUtils.ts
│   │   └── package.json
│   │
│   └── shared-types/          # Cross-module TypeScript types
│       ├── src/
│       │   ├── user.ts
│       │   ├── billing.ts
│       │   ├── sales.ts
│       │   └── index.ts
│       └── package.json
│
├── apps/
│   └── admin/                 # Current frontend (refactored)
│       ├── src/
│       │   ├── modules/       # Domain modules (replacing pages/)
│       │   │   ├── billing/
│       │   │   │   ├── api.ts          # Billing-specific RTK Query slice
│       │   │   │   ├── pages/          # Invoice, Customer, Supplier pages
│       │   │   │   └── components/     # Billing-specific components
│       │   │   ├── sales/
│       │   │   ├── ai/
│       │   │   ├── graph/
│       │   │   ├── company/
│       │   │   └── reporting/
│       │   ├── layouts/
│       │   ├── routes/
│       │   └── App.tsx
│       └── package.json
│
├── turbo.json                 # Turborepo pipeline config
└── package.json               # Root workspace
```

---

## 4. Backend Modernization

### 4.1 Phase 1: Module Interface Extraction

**Goal**: Transform the 1,402-line `main.go` into a clean registry-based initialization.

**Step-by-step for the first module (navigation — simplest)**:

1. Create `pkg/module/module.go` with the `Module` interface
2. Create `internal/navigation/module.go`:

   ```go
   type NavigationModule struct {
       handler *handlers.NavigationHandler
       service *services.NavigationService
   }

   func NewModule() *NavigationModule { return &NavigationModule{} }

   func (m *NavigationModule) Name() string          { return "navigation" }
   func (m *NavigationModule) Version() string        { return "1.0.0" }
   func (m *NavigationModule) Dependencies() []string { return []string{"auth"} }
   func (m *NavigationModule) Enabled(_ *config.Config) bool { return true } // always enabled

   func (m *NavigationModule) Init(deps *module.Dependencies) error {
       menuConfig := config.NewMenuConfig()
       m.service = services.NewNavigationService(menuConfig)
       m.handler = handlers.NewNavigationHandler(m.service)
       return nil
   }

   func (m *NavigationModule) RegisterRoutes(public, protected huma.API, mw *middleware.AuthMiddleware) {
       huma.Register(protected, huma.Operation{
           OperationID: "get-navigation",
           Method:      http.MethodGet,
           Path:        "/v1/navigation",
           Summary:     "Get navigation menu",
           Tags:        []string{"Navigation"},
           Security:    []map[string][]string{{"bearerAuth": {}}},
       }, m.handler.GetNavigation)
   }
   ```

3. Remove navigation wiring from `main.go`, replace with `registry.Register(navigation.NewModule())`
4. Test that the navigation endpoint still works
5. Repeat for each module

**Migration order** (by dependency complexity, least coupled first):

1. `navigation` (zero data dependencies, pure config — simplest possible proof of concept)
2. `reporting` (reads from DB, no cross-module deps)
3. `company` (external API wrapper + DB, self-contained)
4. `documents` (depends on Gotenberg, consumed by billing)
5. `aimodels` (self-contained, consumed by RAG and sales)
6. `billing` (depends on documents, complex but self-contained externally)
7. `graph` (depends on Neo4j/Memgraph driver)
8. `rag` (depends on graph, aimodels, documents)
9. `sales` (depends on aimodels)
10. `agents` (depends on RAG)
11. `user` (many modules depend on it)
12. `auth` (core — many depend on it, extract last or keep as special case)

### 4.2 Phase 2: Event Bus Introduction

**Technology**: NATS JetStream

**Why NATS over alternatives**:
| Option | Verdict |
|---|---|
| **Kafka** | Too heavy for this scale — operational complexity not justified for <30 event types |
| **Redis Streams** | Already using Redis — but mixing data caching with event streaming leads to resource contention |
| **NATS JetStream** | Go-native, lightweight, durable delivery, exactly-once semantics, runs as single binary |
| **RabbitMQ** | Viable but heavier than NATS, less natural in Go ecosystem |

**Domain Events to Define**:

```go
// shared/events/events.go
const (
    // Billing lifecycle
    InvoiceSent            = "billing.invoice_sent"
    InvoiceReceived        = "billing.invoice_received"
    SDINotificationReceived = "billing.sdi_notification"
    InvoicePDFGenerated    = "billing.invoice_pdf_generated"

    // Sales Intelligence
    SalesJobCreated    = "sales.job_created"
    SalesJobCompleted  = "sales.job_completed"
    ProspectScored     = "sales.prospect_scored"
    SalesReportReady   = "sales.report_ready"

    // AI & RAG
    RAGDocumentIngested = "rag.document_ingested"
    RAGQueryCompleted   = "rag.query_completed"
    AgentConversation   = "agents.conversation_completed"
    AIModelUpdated      = "aimodels.model_updated"

    // User lifecycle
    UserCreated     = "user.created"
    UserDeleted     = "user.deleted"  // GDPR
    UserRoleChanged = "user.role_changed"

    // Documents
    TemplateSeedCompleted = "documents.templates_seeded"
    PDFGenerated          = "documents.pdf_generated"

    // Graph
    GraphQueryExecuted = "graph.query_executed"
)
```

**Event Structure**:

```go
type Event struct {
    ID        string            `json:"id"`
    Type      string            `json:"type"`
    Source    string            `json:"source"`    // module name
    Timestamp time.Time         `json:"timestamp"`
    Data      json.RawMessage   `json:"data"`
    Metadata  map[string]string `json:"metadata"` // trace_id, user_id, etc.
}
```

**Usage Pattern**:

```go
// Publisher (in billing service)
func (s *InvoiceService) SendInvoice(ctx context.Context, invoiceID string) error {
    invoice, err := s.repo.Send(ctx, invoiceID)
    if err != nil {
        return err
    }

    s.eventBus.Publish(ctx, events.InvoiceSent, InvoiceSentData{
        InvoiceID:  invoice.ID,
        CustomerID: invoice.CustomerID,
        Amount:     invoice.TotalAmount,
        SentAt:     time.Now(),
    })
    return nil
}

// Subscriber (in reporting module)
func (m *ReportingModule) RegisterEvents(bus *events.Bus) {
    bus.Subscribe(events.InvoiceSent, m.onInvoiceSent)
    bus.Subscribe(events.SalesJobCompleted, m.onSalesJobCompleted)
}
```

### 4.3 Phase 3: Service Boundary Hardening

Replace direct cross-module imports with **service interfaces defined in the shared kernel**.

**Before** (current — tight coupling):

```go
// internal/rag/services/ingestion_service.go
type IngestionService struct {
    documentRepo   *repository.DocumentRepository
    graphRepo      graphRepo.GraphRepository      // DIRECT import of graph module
    modelProvider  aimodelsSvc.AIModelProvider     // DIRECT import of aimodels module
    textExtractor  *TextExtractor                  // Depends on Gotenberg URL from documents config
}

// internal/agents/services/agent_service.go
type AgentService struct {
    projectRepo    *repository.ProjectRepository
    ragBridge      RAGBridge                       // Wraps RAG query service directly
}

// internal/sales/services/orchestrator.go
type Orchestrator struct {
    modelProvider  AIModelProvider                  // Direct aimodels dependency
}
```

**After** (decoupled — interfaces):

```go
// internal/rag/services/ingestion_service.go
type IngestionService struct {
    documentRepo   *repository.DocumentRepository
    graphRepo      module.GraphRepository    // interface from shared kernel
    modelProvider  module.AIModelProvider    // interface from shared kernel
    textExtractor  module.TextExtractor      // interface from shared kernel
}

// internal/agents/services/agent_service.go
type AgentService struct {
    projectRepo    *repository.ProjectRepository
    ragBridge      module.RAGQueryService    // interface from shared kernel
}
```

This means `rag` module has **zero imports** from `graph` or `aimodels` modules. All cross-module communication is through interfaces defined in the shared kernel, with implementations wired during module initialization.

### 4.4 Shared Kernel Evolution

Expand the current `internal/shared/` directory:

```
internal/shared/
├── config/         # (existing) Configuration loading
├── database/       # (existing) MongoDB, Redis, Graph connections
├── errors/         # (existing) Error management
├── middleware/      # (existing) HTTP middleware + auth
├── types/          # (existing) Common types
├── utils/          # (existing) Utilities
│
├── module/         # NEW: Module interface and registry
│   ├── module.go           # Module interface
│   ├── registry.go         # Module registry with dependency sorting
│   ├── dependencies.go     # Shared dependencies struct
│   └── interfaces.go       # Cross-module service interfaces
│
├── events/         # NEW: Event bus abstraction
│   ├── bus.go              # Event bus interface
│   ├── nats.go             # NATS JetStream implementation
│   ├── events.go           # Event type constants
│   └── middleware.go       # Event middleware (logging, tracing)
│
└── health/         # NEW: Health check registry
    ├── registry.go         # Health check collector
    └── handlers.go         # /health, /ready endpoints (extract from main.go)
```

---

## 5. Frontend Modernization

### 5.1 Design System Extraction

**Current state**: ~41 components in `src/components/common/`, barrel exports via `index.ts`, SCSS files with color/spacing/typography tokens embedded.

**Target**: Standalone `@orkestra/ui` package with:

| Layer          | Contents                                                             | Source                                                                   |
| -------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| **Tokens**     | Colors, spacing scale, typography scale, breakpoints                 | Extract from `assets/scss/theme/` SCSS variable files                    |
| **Primitives** | Avatar, Button, Card, Badge, Divider, Logo, Flex                     | Extract from `components/common/`                                        |
| **Composites** | AdvanceTable, PortalDropdown, FalconEditor, FalconLightBox, Calendar | Extract from `components/common/` and `components/common/advance-table/` |
| **Patterns**   | CardDropdown, ConfirmModal, ErrorBoundary, Toast                     | Extract from `components/common/`                                        |
| **Hooks**      | useBreakpoints, useBulkSelect, useAdvanceTable, useToggleStyle       | Extract from `hooks/`                                                    |

**Storybook documentation** for each component enables visual regression testing and serves as a living style guide.

### 5.2 State Management Evolution

**Current state**: Redux Toolkit for auth state and server cache (RTK Query), React Hook Form for forms, Context providers for configuration.

**Recommended split**:

| State Type       | Technology                                | Rationale                                                                                                                            |
| ---------------- | ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| **Server state** | TanStack Query (React Query)              | Already available in the project; better cache lifecycle, native Suspense support. RTK Query works but the ~40 tag system is growing |
| **Auth state**   | Zustand (lightweight) or keep Redux slice | Auth is a single global concern — doesn't need full Redux machinery                                                                  |
| **Form state**   | React Hook Form (already in use)          | Keep as-is — works well                                                                                                              |
| **UI state**     | React Context (already using providers)   | Theme, sidebar state, toast queue — keep out of Redux                                                                                |

**Migration path**: Keep RTK Query working while progressively migrating domain slices to TanStack Query. No big-bang rewrite.

### 5.3 API Slice Decomposition

**Current state**: 17 API slices all inject into a single `baseApi` with ~40 shared tags. Any cache invalidation can accidentally affect unrelated modules.

**Target**: Domain-scoped API instances:

```typescript
// packages/api-client/src/createApi.ts
export function createDomainApi(reducerPath: string) {
  return createApi({
    reducerPath,
    baseQuery: fetchBaseQuery({
      baseUrl: import.meta.env.VITE_BACKEND_URL,
      credentials: 'include',
      prepareHeaders: (headers, { getState }) => {
        const state = getState() as RootState;
        const accessToken = state.auth?.accessToken;
        if (accessToken) {
          headers.set('Authorization', `Bearer ${accessToken}`);
        }
        return headers;
      },
    }),
    endpoints: () => ({}),
  });
}

// apps/admin/src/modules/billing/api.ts
const billingApi = createDomainApi('billingApi');

export const billingEndpoints = billingApi.injectEndpoints({
  endpoints: (builder) => ({
    listInvoices: builder.query({...}),
    getInvoice: builder.query({...}),
    sendInvoice: builder.mutation({...}),
  }),
});
```

Each domain module has its own API slice with its own cache tags, its own reducer path, and its own lifecycle. Cross-module invalidation is explicit and intentional.

### 5.4 Route-Level Module Loading

```typescript
// apps/admin/src/routes/index.tsx
const routes = [
  {
    path: "/admin/billing/*",
    lazy: () => import("../modules/billing/routes"),
  },
  {
    path: "/admin/sales/*",
    lazy: () => import("../modules/sales/routes"),
  },
  {
    path: "/admin/ai/*",
    lazy: () => import("../modules/ai/routes"),
  },
  {
    path: "/admin/graph/*",
    lazy: () => import("../modules/graph/routes"),
  },
  // Each module bundles its own pages, components, and API slice
]
```

This ensures that loading the billing module does not download the sales module's code, and vice versa.

---

## 6. Scalability Strategy

### 6.1 Kubernetes Architecture

#### Phase 1: Lift and Shift (Replace Docker Compose)

```yaml
# Namespace: orkestra
Deployments:
  - orkestra-backend     (2-5 replicas, HPA on CPU/memory)
  - orkestra-frontend    (2 replicas, Nginx serving static files)

StatefulSets:
  - orkestra-mongodb     (3 replicas, replica set)
  - orkestra-redis       (3 replicas, Redis Sentinel)
  - orkestra-nats        (3 replicas, JetStream cluster)
  - orkestra-memgraph    (1 replica, graph database)

Services:
  - orkestra-backend-svc   (ClusterIP, load balances across backend pods)
  - orkestra-frontend-svc  (ClusterIP, behind Ingress)
  - orkestra-mongodb-svc   (Headless, for replica set discovery)

Ingress:
  - Traefik IngressRoute
    - app.orkestra.cc      → orkestra-frontend-svc
    - api.orkestra.cc      → orkestra-backend-svc
    - /docs                → orkestra-backend-svc (OpenAPI docs)
```

**Horizontal Pod Autoscaler**:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: orkestra-backend-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: orkestra-backend
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
```

#### Phase 2: Service Mesh (Linkerd)

**Why Linkerd over Istio**:
| Criteria | Linkerd | Istio |
|---|---|---|
| Resource overhead | ~25MB per proxy | ~100MB per proxy |
| Complexity | Minimal config | Extensive CRDs |
| Go ecosystem fit | Linkerd is written in Go/Rust | Istio is Envoy-centric |
| mTLS setup | Automatic, zero-config | Requires explicit configuration |
| Observability | Built-in golden metrics | Requires additional setup |

**Capabilities added**:

- Automatic mTLS between all services (zero-trust networking)
- Traffic splitting for canary deployments
- Retry budgets and circuit breaking
- Per-route metrics and latency percentiles

#### Phase 3: API Gateway (Traefik)

```yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: orkestra-api-gateway
spec:
  entryPoints:
    - websecure
  routes:
    - match: Host(`api.orkestra.cc`) && PathPrefix(`/api/v1`)
      kind: Rule
      services:
        - name: orkestra-backend-svc
          port: 3000
      middlewares:
        - name: rate-limit
        - name: jwt-validation
        - name: cors-headers
        - name: request-logging

    - match: Host(`app.orkestra.cc`)
      kind: Rule
      services:
        - name: orkestra-frontend-svc
          port: 80
      middlewares:
        - name: compress
        - name: security-headers
```

**Benefits of gateway-level rate limiting**:

- Remove `rateLimiter.Middleware("api:general")` from Go code
- Configurable per-route without code changes
- Protects all services uniformly
- Enables geographic-aware rate limiting

### 6.2 Database Scaling

#### MongoDB

```
┌─────────────────────────────────────────────────┐
│                  MongoDB Replica Set              │
│                                                   │
│  ┌─────────┐   ┌─────────┐   ┌─────────┐       │
│  │ Primary │   │Secondary│   │Secondary│       │
│  │ (write) │──▶│ (read)  │──▶│ (read)  │       │
│  └─────────┘   └─────────┘   └─────────┘       │
│       │              │              │             │
│       │         Reporting      Analytics          │
│       │         Queries        Queries            │
│       ▼                                           │
│  All Writes                                       │
│  (billing,                                        │
│   user mgmt,                                      │
│   sales jobs)                                     │
└─────────────────────────────────────────────────┘
```

**Read preference routing**:

```go
// For write operations (default)
collection := db.Collection("invoices")

// For reporting queries (read from secondaries)
opts := options.Collection().SetReadPreference(readpref.SecondaryPreferred())
reportCollection := db.Collection("invoices", opts)
```

**Future sharding** (when data exceeds single-node capacity):

- Shard key: `companyId` (for multi-tenancy) or `createdAt` (for time-series data)
- Only shard collections that grow unboundedly (invoices, audit logs, RAG documents, sales jobs)

#### Redis

Upgrade from standalone to **Redis Sentinel** (Phase 1) or **Redis Cluster** (Phase 3):

```
Phase 1: Redis Sentinel (automatic failover)
  - 1 master + 2 replicas + 3 sentinels
  - Reads from replicas for session validation
  - Writes to master for session creation

Phase 3: Redis Cluster (horizontal scaling)
  - 6 nodes (3 masters, 3 replicas)
  - Hash-slot distribution for even load
  - Required when session count exceeds single-node memory
```

---

## 7. Infrastructure & DevOps

### 7.1 CI/CD Pipeline (GitHub Actions)

```yaml
# .github/workflows/ci.yml
name: CI/CD Pipeline

on:
  pull_request:
    branches: [dev, main]
  push:
    branches: [dev, main]

jobs:
  # ─── Quality Gates (parallel) ───────────────────────
  backend-lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.25" }
      - run: golangci-lint run ./...

  backend-test:
    runs-on: ubuntu-latest
    services:
      mongodb: { image: mongo:8.0, ports: ["27017:27017"] }
      redis: { image: redis/redis-stack-server:latest, ports: ["6379:6379"] }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: cd backend && go test -race -coverprofile=coverage.out ./...
      - run: |
          COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}')
          echo "Total coverage: $COVERAGE"
          # Fail if below 80%

  frontend-lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: "22" }
      - run: cd frontend && npm ci && npm run lint

  frontend-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
      - run: cd frontend && npm ci && npm run test -- --coverage

  # ─── Build (after quality gates) ────────────────────
  build-backend:
    needs: [backend-lint, backend-test]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/build-push-action@v5
        with:
          context: ./backend
          push: ${{ github.event_name == 'push' }}
          tags: ghcr.io/orkestra/backend:${{ github.sha }}

  build-frontend:
    needs: [frontend-lint, frontend-test]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/build-push-action@v5
        with:
          context: ./frontend
          push: ${{ github.event_name == 'push' }}
          tags: ghcr.io/orkestra/frontend:${{ github.sha }}

  # ─── Deploy (on push to dev/main) ───────────────────
  deploy-staging:
    if: github.ref == 'refs/heads/dev'
    needs: [build-backend, build-frontend]
    runs-on: ubuntu-latest
    steps:
      - run: |
          kubectl set image deployment/orkestra-backend \
            backend=ghcr.io/orkestra/backend:${{ github.sha }}
          kubectl set image deployment/orkestra-frontend \
            frontend=ghcr.io/orkestra/frontend:${{ github.sha }}
          kubectl rollout status deployment/orkestra-backend --timeout=300s

  deploy-production:
    if: github.ref == 'refs/heads/main'
    needs: [build-backend, build-frontend]
    runs-on: ubuntu-latest
    environment: production # Requires manual approval
    steps:
      - run: |
          # Canary deployment: route 10% traffic to new version
          kubectl set image deployment/orkestra-backend-canary \
            backend=ghcr.io/orkestra/backend:${{ github.sha }}
          # Wait for health checks, then promote
```

### 7.2 GitOps with ArgoCD

```
orkestra-infra/                     # Separate repo for K8s manifests
├── base/
│   ├── backend/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── hpa.yaml
│   │   └── kustomization.yaml
│   ├── frontend/
│   ├── mongodb/
│   ├── redis/
│   ├── memgraph/
│   ├── gotenberg/
│   ├── hindsight/
│   └── nats/
├── overlays/
│   ├── development/
│   │   ├── kustomization.yaml      # 1 replica, debug logging
│   │   └── patches/
│   ├── staging/
│   │   ├── kustomization.yaml      # 2 replicas, staging DB
│   │   └── patches/
│   └── production/
│       ├── kustomization.yaml      # 3+ replicas, prod secrets
│       └── patches/
└── argocd/
    └── application.yaml            # ArgoCD Application CRD
```

**ArgoCD syncs** from Git to Kubernetes: any change to the manifests repo triggers automatic deployment.

### 7.3 Secrets Management

**Migration path**:

| Phase   | Method                       | Tools                                    |
| ------- | ---------------------------- | ---------------------------------------- |
| Current | `.env` files per environment | Manual, separate dev/staging/prod files  |
| Phase 1 | Kubernetes Secrets           | `kubectl create secret`, base64-encoded  |
| Phase 2 | External Secrets Operator    | Syncs secrets from external store to K8s |
| Phase 3 | HashiCorp Vault              | Dynamic secrets, rotation, audit trail   |

**Minimum requirements for Phase 1**:

- JWT private/public keys as K8s Secrets
- MongoDB/Redis credentials as K8s Secrets
- OAuth client secrets (Google, Apple, GitHub, Discord) as K8s Secrets
- Billing API tokens (OpenAPI.it bearer token) as K8s Secrets
- AI provider API keys (OpenAI, Anthropic, Gemini) as K8s Secrets
- Never in Git, never in container images

### 7.4 Monitoring Evolution

**Current gaps and solutions**:

| Gap                 | Solution                           | Tool                              |
| ------------------- | ---------------------------------- | --------------------------------- |
| No metrics pipeline | Prometheus + OpenTelemetry metrics | Prometheus Operator on K8s        |
| No dashboards       | Golden signals dashboards          | Grafana                           |
| No alerting         | Alert rules for SLO violations     | Alertmanager → PagerDuty/Opsgenie |
| No SLOs defined     | Define and track SLOs              | Grafana SLO dashboard             |
| Basic logging only  | Structured logging with search     | Loki + Grafana Explore            |

**Golden Signals Dashboard**:

```
┌──────────────────────────────────────────────────┐
│  LATENCY         │  ERRORS          │  TRAFFIC    │
│  p50: 12ms       │  5xx: 0.02%      │  1,247 rpm  │
│  p95: 45ms       │  4xx: 3.1%       │  ↑ 12%      │
│  p99: 120ms      │                  │             │
├──────────────────┼──────────────────┼─────────────┤
│  SATURATION      │  SLO STATUS      │  UPTIME     │
│  CPU: 34%        │  Availability:    │  99.97%     │
│  Memory: 61%     │  99.95% ✓        │  (30 days)  │
│  Connections: 45 │  Latency p99:     │             │
│                  │  99.2% ✓         │             │
└──────────────────┴──────────────────┴─────────────┘
```

**SLO Definitions**:

| SLO                 | Target                     | Measurement                            |
| ------------------- | -------------------------- | -------------------------------------- |
| Availability        | 99.9% (8.7h downtime/year) | Successful responses / total responses |
| Latency (API)       | p99 < 200ms                | OpenTelemetry trace duration           |
| Latency (RAG Query) | p99 < 5s                   | RAG query completion time              |
| Error rate          | < 0.1% 5xx                 | Prometheus counter ratio               |

---

## 8. Data Architecture

### 8.1 Event Sourcing: Selective, Not Universal

Full event sourcing for all entities is overkill. Instead, adopt **event sourcing for audit-critical flows** while keeping CRUD for the rest.

**Event-sourced entities** (high business value of history):

- Invoice state transitions (draft → sent → delivered → accepted/rejected)
- Sales job lifecycle (created → running → completed → reported)
- GDPR data operations (deletion requests, anonymization)
- RAG document lifecycle (ingested → chunked → embedded → indexed)

**CRUD entities** (standard operations, history not critical):

- Navigation config, company lookups, AI model definitions, user profiles

### 8.2 CQRS for Reporting

The reporting module (`internal/reporting/`) runs MongoDB aggregation pipelines that compete with OLTP workloads on the same primary.

**Solution**: Read model separation

```
WRITE PATH                          READ PATH
────────────                        ─────────
Backend API                         Reporting API
    │                                   │
    ▼                                   ▼
MongoDB Primary ──replication──▶ MongoDB Secondary
                                        │
                                   Materialized Views
                                   (pre-aggregated)
                                        │
                                   Dashboard Queries
```

**Materialized views** (created as MongoDB views or periodic aggregation jobs):

- `billing_monthly_summary` — Pre-computed monthly invoice totals
- `sales_pipeline_metrics` — Prospect scoring and conversion stats
- `user_document_expiry` — Upcoming document expirations across users
- `rag_usage_analytics` — RAG query frequency and response quality metrics

### 8.3 Multi-Tenancy Foundation

If SaaS distribution becomes a goal, the architecture needs tenant isolation at the data level.

**Recommended approach**: Database-per-tenant (MongoDB)

```
orkestra_tenant_001/    # Company A
  ├── invoices
  ├── users
  ├── customers
  └── ...

orkestra_tenant_002/    # Company B
  ├── invoices
  ├── users
  ├── customers
  └── ...

orkestra_shared/        # Cross-tenant data
  ├── tenant_configs
  ├── ai_models
  └── system_settings
```

**Why database-per-tenant over collection-per-tenant**:

- Stronger data isolation (no accidental cross-tenant queries)
- Independent backup and restore per tenant
- Easier GDPR compliance (drop entire database on tenant deletion)
- MongoDB handles many databases efficiently

**Middleware integration**:

```go
func TenantMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        tenantID := r.Header.Get("X-Tenant-ID") // or from JWT claims
        ctx := context.WithValue(r.Context(), TenantKey, tenantID)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func (d *Dependencies) DBForTenant(ctx context.Context) *mongo.Database {
    tenantID := ctx.Value(TenantKey).(string)
    return d.MongoClient.Database("orkestra_" + tenantID)
}
```

---

## 9. Migration Roadmap

### Phase 0: Foundation (Month 1-2)

**Goal**: Establish CI/CD and prove the Module interface pattern.

| Task                                   | Effort | Owner   | Deliverable                                       |
| -------------------------------------- | ------ | ------- | ------------------------------------------------- |
| Set up GitHub Actions CI pipeline      | 1 week | DevOps  | `.github/workflows/ci.yml` with lint + test gates |
| Set up GitHub Container Registry       | 1 day  | DevOps  | Push images on merge to dev/main                  |
| Create `pkg/module/` interface         | 3 days | Backend | Module, Registry, Dependencies types              |
| Migrate `navigation` module (simplest) | 2 days | Backend | Proof of concept                                  |
| Migrate `reporting` module             | 2 days | Backend | Second validation                                 |
| Migrate `company` module               | 2 days | Backend | Pattern solidified                                |
| Measure: main.go lines reduced         | —      | —       | Target: -300 lines                                |

**Exit criteria**: 3 modules migrated, CI pipeline running on every PR, main.go reduced by ~300 lines.

### Phase 1: Internal Modularization (Month 3-5)

**Goal**: All modules use the Module interface; event bus handles cross-module communication.

| Task                                         | Effort  | Owner    | Deliverable                            |
| -------------------------------------------- | ------- | -------- | -------------------------------------- |
| Migrate remaining 9 modules                  | 4 weeks | Backend  | All modules implement Module interface |
| Define cross-module service interfaces       | 2 weeks | Backend  | AIModelProvider, PDFService, etc.      |
| Deploy NATS JetStream                        | 1 week  | DevOps   | Docker Compose + K8s manifest          |
| Define domain events                         | 1 week  | Backend  | `shared/events/events.go`              |
| Implement event publishing for billing       | 2 weeks | Backend  | InvoiceSent, SDINotification, etc.     |
| Implement event subscriptions in reporting   | 1 week  | Backend  | Real-time metrics updates              |
| Extract `@orkestra/ui` design system package | 4 weeks | Frontend | Storybook + npm package                |
| Split `baseApi` into domain-scoped slices    | 3 weeks | Frontend | Per-module API slices                  |
| Set up Turborepo monorepo                    | 1 week  | Frontend | `turbo.json` + workspace config        |

**Exit criteria**: main.go under 250 lines, NATS running with 5+ event types, frontend builds as monorepo.

### Phase 2: Infrastructure Modernization (Month 6-8)

**Goal**: Production runs on Kubernetes with proper monitoring.

| Task                                   | Effort  | Owner  | Deliverable                           |
| -------------------------------------- | ------- | ------ | ------------------------------------- |
| Create Helm charts for all services    | 2 weeks | DevOps | `charts/` directory                   |
| Set up managed Kubernetes cluster      | 1 week  | DevOps | GKE/EKS/AKS cluster                   |
| Deploy staging to Kubernetes           | 2 weeks | DevOps | Staging environment on K8s            |
| MongoDB replica set on K8s             | 1 week  | DevOps | StatefulSet with 3 replicas           |
| Redis Sentinel on K8s                  | 1 week  | DevOps | HA Redis deployment                   |
| Memgraph on K8s                        | 1 week  | DevOps | StatefulSet with persistent volume    |
| Install Prometheus + Grafana           | 1 week  | DevOps | Golden signals dashboard              |
| Define SLOs and alerting rules         | 1 week  | DevOps | Alertmanager configuration            |
| ArgoCD for GitOps                      | 1 week  | DevOps | Automatic deployment from Git         |
| Deploy production to Kubernetes        | 2 weeks | DevOps | Production cutover with rollback plan |
| Decommission Docker Compose production | 1 day   | DevOps | After 2-week validation period        |

**Exit criteria**: Production on Kubernetes, automated deployments via GitOps, dashboards for golden signals.

### Phase 3: Scale and Harden (Month 9-12)

**Goal**: Production-hardened for 10,000+ concurrent users.

| Task                                         | Effort  | Owner      | Deliverable                                |
| -------------------------------------------- | ------- | ---------- | ------------------------------------------ |
| Install Traefik API Gateway                  | 2 weeks | DevOps     | Gateway with rate limiting, JWT validation |
| Remove application-level rate limiting       | 1 week  | Backend    | Simplify middleware stack                  |
| Install Linkerd service mesh                 | 2 weeks | DevOps     | mTLS, traffic splitting, circuit breaking  |
| MongoDB read replicas for reporting          | 1 week  | DevOps     | Secondary-preferred reads                  |
| Load testing with k6                         | 2 weeks | QA         | Performance baseline at 10,000 users       |
| Canary deployment pipeline                   | 1 week  | DevOps     | 10% → 50% → 100% rollout                   |
| Frontend bundle optimization audit           | 2 weeks | Frontend   | Per-route bundle analysis, tree shaking    |
| Multi-tenancy foundation (if SaaS confirmed) | 4 weeks | Full stack | Tenant middleware, database-per-tenant     |

**Exit criteria**: 10,000 concurrent user load test passes, p99 < 200ms, 99.9% availability measured over 30 days.

### Roadmap Timeline

```
Month:  1    2    3    4    5    6    7    8    9    10   11   12
        ├────┤    ├─────────┤    ├─────────┤    ├──────────────┤
        Phase 0   Phase 1        Phase 2        Phase 3
        CI/CD     Modularize     Kubernetes     Scale & Harden
        +Module   +Events        +Monitoring    +Gateway
        PoC       +Design Sys    +GitOps        +Mesh
```

---

## 10. Risk Assessment

| Risk                                                | Likelihood |    Impact    | Mitigation                                                                                                                                            |
| --------------------------------------------------- | :--------: | :----------: | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| Module extraction breaks cross-module queries       |    High    |    Medium    | Extract simplest modules first (navigation, reporting, company); build integration test suite before touching complex modules (rag, auth)             |
| Kubernetes learning curve delays feature work       |   Medium   |     High     | Use managed K8s (GKE/EKS); keep Docker Compose for local dev; only ops/deploy changes — application code stays the same                               |
| Event bus introduces eventual consistency bugs      |   Medium   |    Medium    | Start with non-critical events (reporting, analytics); keep synchronous calls for data integrity operations; add dead-letter queues for failed events |
| Frontend monorepo refactoring breaks existing pages |   Medium   |     High     | Component-level Storybook tests; visual regression testing (Chromatic); keep old import paths working via package aliases during migration            |
| RAG/Graph decoupling introduces latency             |    Low     |    Medium    | Interface-based communication is still in-process calls; no network overhead unless extracted to microservice                                         |
| **Migration fatigue — team stops halfway**          |  **High**  | **Critical** | **Each phase delivers independent, measurable value; Phase 0 alone (CI/CD) pays for itself; no phase depends on completing all of the next phase**    |
| NATS JetStream adds operational complexity          |    Low     |    Medium    | NATS is a single binary with minimal config; start with embedded mode (in-process) before deploying separately                                        |
| Cost increase from Kubernetes infrastructure        |   Medium   |     Low      | Managed K8s clusters cost ~$70-150/month; offset by reduced manual deployment time and faster incident response                                       |
| AI provider API key management complexity           |    Low     |    Medium    | Vault integration in Phase 2; until then, K8s Secrets with strict RBAC access controls                                                                |

### Decision Matrix: Build vs Buy vs Wait

| Component        | Recommendation           | Rationale                                                                    |
| ---------------- | ------------------------ | ---------------------------------------------------------------------------- |
| Module interface | **Build**                | Go-specific, aligned with existing feature-flag pattern, simple to implement |
| Event bus        | **Buy (NATS)**           | Production-proven, Go-native, avoids building message infrastructure         |
| API gateway      | **Buy (Traefik)**        | K8s-native, eliminates custom rate limiting from app code                    |
| Service mesh     | **Buy (Linkerd)**        | mTLS and observability cannot be reasonably built in-house                   |
| CI/CD            | **Buy (GitHub Actions)** | Already on GitHub, zero infrastructure to manage                             |
| Kubernetes       | **Buy (managed)**        | GKE/EKS/AKS eliminates control plane management                              |
| Design system    | **Build**                | Extract from existing components, specific to Orkestra's brand               |
| Multi-tenancy    | **Wait**                 | Only build if SaaS distribution is confirmed as a business goal              |

---

## 11. Appendix: Reference Architecture Diagram

### Current Architecture

```
                          ┌─────────────┐
                          │   Clients   │
                          │ Web│Mobile  │
                          └──────┬──────┘
                                 │
                          ┌──────▼──────┐
                          │  Cloudflare │
                          │   Tunnel    │
                          └──────┬──────┘
                                 │
                   ┌─────────────┼─────────────┐
                   │                            │
            ┌──────▼──────┐              ┌──────▼──────┐
            │  Frontend   │              │  Backend    │
            │  (Nginx)    │              │  (Go mono) │
            │  1 instance │              │  1 instance │
            └─────────────┘              └──────┬──────┘
                                                │
                              ┌─────────────────┼─────────────────┐
                              │                 │                  │
                       ┌──────▼──┐        ┌─────▼────┐     ┌──────▼─────┐
                       │MongoDB  │        │  Redis   │     │ Memgraph   │
                       │ single  │        │  single  │     │ (graph DB) │
                       └─────────┘        └──────────┘     └────────────┘
                                                │
                              ┌─────────────────┼─────────────────┐
                              │                 │                  │
                       ┌──────▼──┐        ┌─────▼────┐     ┌──────▼─────┐
                       │Gotenberg│        │Hindsight │     │  Ollama    │
                       │(PDF gen)│        │(AI agent)│     │ (local LLM)│
                       └─────────┘        └──────────┘     └────────────┘
```

### Target Architecture (Post Phase 3)

```
                          ┌─────────────┐
                          │   Clients   │
                          │ Web│Mobile  │
                          └──────┬──────┘
                                 │
                          ┌──────▼──────┐
                          │   Traefik   │
                          │ API Gateway │
                          │ Rate Limit  │
                          │ JWT Valid.  │
                          └──────┬──────┘
                                 │
                     ┌───────────┼───────────┐
                     │                       │
              ┌──────▼──────┐         ┌──────▼──────┐
              │  Frontend   │         │   Linkerd   │
              │  (Nginx)    │         │   Mesh      │
              │  2 replicas │         │   (mTLS)    │
              └─────────────┘         ├─────────────┤
                                      │  Backend    │
                                      │  2-10 pods  │
                                      │  (HPA auto) │
                                      │             │
                                      │ ┌─────────┐ │
                                      │ │Module   │ │
                                      │ │Registry │ │
                                      │ │Auth│User│ │
                                      │ │Bill│RAG │ │
                                      │ │Sales│...│ │
                                      │ └────┬────┘ │
                                      └──────┼──────┘
                                             │
              ┌──────────────────────────────┼──────────────────┐
              │                              │                  │
       ┌──────▼──────┐               ┌──────▼─────┐    ┌───────▼──────┐
       │  MongoDB    │               │   Redis    │    │    NATS      │
       │ ReplicaSet  │               │  Sentinel  │    │  JetStream   │
       │ 3 nodes     │               │  3 nodes   │    │  3 nodes     │
       │ R/W split   │               │  HA        │    │  Events      │
       └──────┬──────┘               └────────────┘    └──────────────┘
              │
       ┌──────▼──────┐    ┌────────────┐    ┌────────────┐
       │  Read       │    │ Memgraph   │    │ Gotenberg  │
       │  Replicas   │    │ (graph)    │    │ (PDF)      │
       │  (reporting)│    └────────────┘    └────────────┘
       └─────────────┘

    ┌──────────────────────────────────────────┐
    │           Observability Stack             │
    │  Prometheus │ Grafana │ Loki │ Tempo     │
    │  Metrics    │ Dashb.  │ Logs │ Traces    │
    │  Alertmgr   │ SLOs    │      │           │
    └──────────────────────────────────────────┘

    ┌──────────────────────────────────────────┐
    │            GitOps (ArgoCD)                │
    │  Git repo ──▶ K8s manifests ──▶ Deploy   │
    │  Helm charts │ Kustomize overlays        │
    └──────────────────────────────────────────┘
```

---

## Summary of Investment

| Phase       | Duration | Key Deliverables                                    | Independent Value                              |
| ----------- | -------- | --------------------------------------------------- | ---------------------------------------------- |
| **Phase 0** | 2 months | CI/CD pipeline, Module PoC (3 modules)              | Automated quality gates, faster PR reviews     |
| **Phase 1** | 3 months | All modules extracted, event bus, design system     | Clean architecture, reusable UI library        |
| **Phase 2** | 3 months | Kubernetes, monitoring, GitOps                      | Auto-scaling, observability, automated deploys |
| **Phase 3** | 4 months | API gateway, service mesh, load-tested at 10K users | Production-hardened, enterprise-ready          |

**Total timeline**: 12 months  
**Each phase is independently valuable** — the project benefits even if only Phase 0 is completed.

---

_This document should be reviewed quarterly and updated as the architecture evolves. The migration roadmap dates are estimates and should be adjusted based on team capacity and business priorities._
