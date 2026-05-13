package rag

import (
	"context"
	"log/slog"
	"os"
	"strconv"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra-cc/orkestra-sdk/capability"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra-cc/orkestra-sdk/modulegate"
	"github.com/orkestra/backend/internal/addons/rag/handlers"
	"github.com/orkestra/backend/internal/addons/rag/repository"
	"github.com/orkestra/backend/internal/addons/rag/services"
	"github.com/orkestra/backend/internal/shared/config"
)

// Settings mirrors the rag ConfigSchema 1:1. OllamaBaseURL/OpenAIAPIKey are
// duplicated with aimodels' schema but kept here because rag's standalone
// model-service fallback path (when aimodels is disabled) reads them.
type Settings struct {
	OllamaBaseURL string `module:"ollamaBaseURL"`
	OpenAIAPIKey  string `module:"openaiAPIKey"`
	ChunkSize     int    `module:"chunkSize"`
	ChunkOverlap  int    `module:"chunkOverlap"`
	DefaultTopK   int    `module:"defaultTopK"`
}

type RAGModule struct {
	module.BaseModule
	documentHandler     *handlers.DocumentHandler
	queryHandler        *handlers.QueryHandler
	streamHandler       *handlers.StreamHandler
	relationshipHandler *handlers.RelationshipHandler
	internalHandler     *handlers.InternalHandler
}

func NewModule() *RAGModule { return &RAGModule{} }

func (m *RAGModule) Name() string        { return "rag" }
func (m *RAGModule) DisplayName() string { return "RAG Pipeline" }
func (m *RAGModule) Description() string {
	return "Document ingestion, embedding, and retrieval-augmented generation"
}
func (m *RAGModule) Category() module.ModuleCategory { return module.CategoryToggleable }

// Enabled gates first-boot activation on RAG_ENABLED — same semantic as
// before, but sourced directly from the env var so this method has no
// dependency on shared/config. After first boot the persisted enabled
// flag in module_configs is authoritative.
func (m *RAGModule) Enabled() bool {
	v, _ := strconv.ParseBool(os.Getenv("RAG_ENABLED"))
	return v
}
func (m *RAGModule) Dependencies() []string { return []string{"graph", "aimodels"} }

func (m *RAGModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceRAGQuery}
}
func (m *RAGModule) RequiredServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceGraphRepo}
}
func (m *RAGModule) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceAIModelProvider}
}

// ConfigSchema declares the per-module runtime tunables. Fields previously
// read from cfg.RAG (shared/config) move here so they can be adjusted at
// /admin/modules without a restart. Defaults match the env-var defaults in
// shared/config so existing deployments keep their behaviour.
func (m *RAGModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "ollamaBaseURL", Label: "Ollama Base URL", Type: module.FieldString, Default: "http://localhost:11434", EnvVar: "OLLAMA_BASE_URL"},
		{Key: "openaiAPIKey", Label: "OpenAI API Key (fallback)", Type: module.FieldSecret, EnvVar: "OPENAI_API_KEY",
			Description: "Used only when aimodels is disabled and the standalone rag model service falls back to it."},
		{Key: "chunkSize", Label: "Chunk Size", Type: module.FieldInt, Default: "512", EnvVar: "RAG_CHUNK_SIZE",
			Description: "Default text chunk size in characters."},
		{Key: "chunkOverlap", Label: "Chunk Overlap", Type: module.FieldInt, Default: "50", EnvVar: "RAG_CHUNK_OVERLAP",
			Description: "Overlap between chunks in characters."},
		{Key: "defaultTopK", Label: "Default Top-K", Type: module.FieldInt, Default: "10", EnvVar: "RAG_DEFAULT_TOP_K",
			Description: "Default number of results for vector search. Also consumed by the agents module's RAG bridge."},
	}
}

func (m *RAGModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: "rag_documents", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
		{Name: "rag_models"},
		{Name: "rag_relationship_types"},
	}
}

func (m *RAGModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "shared", Section: "Tools", Name: "Knowledge Base", Icon: "file-alt", Path: "/graph/documents", Active: true},
		{Realm: "shared", Section: "Tools", Name: "Relationships", Icon: "project-diagram", Path: "/graph/relationships", Active: true},
		{Realm: "shared", Section: "Tools", Name: "RAG Query", Icon: "search", Path: "/graph/rag", Active: true},
	}
}

func (m *RAGModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "rag.document.read", Module: "rag", Description: "List and view ingested documents"},
		{Key: "rag.document.ingest", Module: "rag", Description: "Upload and ingest new documents"},
		{Key: "rag.document.delete", Module: "rag", Description: "Delete documents and their graph data"},
		{Key: "rag.query", Module: "rag", Description: "Run RAG queries"},
		{Key: "rag.admin", Module: "rag", Description: "Manage relationship types and model configs"},
	}
}

func (m *RAGModule) Capabilities() []capability.Capability {
	return []capability.Capability{
		{
			ID:          "rag.access",
			Module:      "rag",
			Action:      "access",
			Title:       "Retrieval-Augmented Generation",
			Description: "Ingest documents into the knowledge graph and run grounded RAG queries.",
			Published:   true,
		},
	}
}

func (m *RAGModule) Init(deps *module.Dependencies) error {
	var settings Settings
	if err := deps.ConfigService.UnmarshalModule(context.Background(), m.Name(), &settings); err != nil {
		return err
	}
	if settings.ChunkSize <= 0 {
		settings.ChunkSize = 512
	}
	if settings.ChunkOverlap < 0 {
		settings.ChunkOverlap = 50
	}
	if settings.DefaultTopK <= 0 {
		settings.DefaultTopK = 10
	}

	// Build the legacy RAGConfig (shared/config type) from Settings so
	// services.NewModelService keeps its signature unchanged. Enabled is
	// true by construction — Init only runs when the module is enabled.
	ragCfg := config.RAGConfig{
		Enabled:       true,
		OllamaBaseURL: settings.OllamaBaseURL,
		OpenAIAPIKey:  settings.OpenAIAPIKey,
		ChunkSize:     settings.ChunkSize,
		ChunkOverlap:  settings.ChunkOverlap,
		DefaultTopK:   settings.DefaultTopK,
	}

	documentRepository := repository.NewDocumentRepository(deps.DB)
	relTypeRepo := repository.NewRelationshipTypeRepository(deps.DB)
	relTypeService := services.NewRelationshipTypeService(relTypeRepo, deps.Logger)
	m.relationshipHandler = handlers.NewRelationshipHandler(relTypeService)

	// Use aimodels service if available, otherwise create a standalone model service
	var ragModelProvider services.AIModelProvider
	if mp, ok := module.GetTyped[iface.AIModelProvider](deps.Services, module.ServiceAIModelProvider); ok {
		ragModelProvider = mp
	} else {
		modelRepository := repository.NewModelRepository(deps.DB)
		localModelService := services.NewModelService(modelRepository, ragCfg, deps.Logger)
		if err := localModelService.SeedDefaults(context.Background()); err != nil {
			deps.Logger.Warn("Failed to seed default RAG models", slog.String("error", err.Error()))
		}
		ragModelProvider = localModelService
	}

	// Text extractor uses Gotenberg from the documents module. gotenbergURL
	// is declared in documents' ConfigSchema (PR-1a-2) so reading it via
	// deps.ConfigService is a narrow cross-addon coupling — rag asks for
	// one knob without importing documents' package.
	textExtractor := services.NewTextExtractor(deps.GetConfig("documents", "gotenbergURL"))

	// Ingestion + query services require graph repository
	if graphProvider, ok := module.GetTyped[iface.GraphProvider](deps.Services, module.ServiceGraphRepo); ok {
		ingestionService := services.NewIngestionService(
			documentRepository, relTypeRepo, graphProvider, ragModelProvider, textExtractor,
			settings.ChunkSize, settings.ChunkOverlap, deps.Logger,
		)
		m.documentHandler = handlers.NewDocumentHandler(ingestionService)

		ragQueryService := services.NewQueryService(graphProvider, ragModelProvider, settings.DefaultTopK, deps.Logger)
		m.queryHandler = handlers.NewQueryHandler(ragQueryService)
		m.streamHandler = handlers.NewStreamHandler(ragQueryService, deps.Logger)
		m.internalHandler = handlers.NewInternalHandler(ragQueryService)

		// Register RAGQueryService for agents module consumption
		deps.Services.Register(module.ServiceRAGQuery, ragQueryService)
	}

	deps.Logger.Info("RAG module initialized")
	return nil
}

func (m *RAGModule) RegisterRoutes(ri *module.RouteInfo) {
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(modulegate.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.Operator.AuthMW.RequireCapability("rag.access"))
		r.Use(ri.Operator.AuthMW.RequirePermission("rag.document.read"))
		api := humachi.New(r, ri.APIConfig)
		if m.documentHandler != nil {
			RegisterDocumentRoutes(api, m.documentHandler)
		}
		if m.queryHandler != nil {
			RegisterQueryRoutes(api, m.queryHandler)
		}
		if m.streamHandler != nil {
			RegisterStreamRoute(r, m.streamHandler)
		}
		if m.relationshipHandler != nil {
			RegisterRelationshipTypeRoutes(api, m.relationshipHandler)
		}
		if m.internalHandler != nil {
			RegisterInternalRoutes(api, m.internalHandler)
		}
	})
}

func (m *RAGModule) Start(_ context.Context) error       { return nil }
func (m *RAGModule) Stop(_ context.Context) error        { return nil }
func (m *RAGModule) HealthCheck(_ context.Context) error { return nil }
