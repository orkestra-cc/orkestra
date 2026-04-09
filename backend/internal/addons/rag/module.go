package rag

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/addons/rag/handlers"
	"github.com/orkestra/backend/internal/addons/rag/repository"
	"github.com/orkestra/backend/internal/addons/rag/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

type RAGModule struct {
	module.BaseModule
	documentHandler     *handlers.DocumentHandler
	queryHandler        *handlers.QueryHandler
	streamHandler       *handlers.StreamHandler
	relationshipHandler *handlers.RelationshipHandler
	internalHandler     *handlers.InternalHandler
}

func NewModule() *RAGModule { return &RAGModule{} }

func (m *RAGModule) Name() string                   { return "rag" }
func (m *RAGModule) DisplayName() string             { return "RAG Pipeline" }
func (m *RAGModule) Description() string             { return "Document ingestion, embedding, and retrieval-augmented generation" }
func (m *RAGModule) Category() module.ModuleCategory { return module.CategoryToggleable }
func (m *RAGModule) Enabled(cfg *config.Config) bool { return cfg.RAG.Enabled }
func (m *RAGModule) Dependencies() []string          { return []string{"graph", "aimodels"} }

func (m *RAGModule) ProvidedServices() []module.ServiceKey  { return []module.ServiceKey{module.ServiceRAGQuery} }
func (m *RAGModule) RequiredServices() []module.ServiceKey  { return []module.ServiceKey{module.ServiceGraphRepo} }
func (m *RAGModule) OptionalServices() []module.ServiceKey  { return []module.ServiceKey{module.ServiceAIModelProvider} }

func (m *RAGModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: "rag_documents", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
		{Name: "rag_models"},
		{Name: "rag_relationship_types"},
	}
}

func (m *RAGModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Group: "AI", Name: "Documents", Icon: "file-alt", Path: "/graph/documents", MinRole: "administrator", Active: true},
		{Group: "AI", Name: "Relationships", Icon: "project-diagram", Path: "/graph/relationships", MinRole: "administrator", Active: true},
		{Group: "AI", Name: "RAG Query", Icon: "search", Path: "/graph/rag", MinRole: "administrator", Active: true},
	}
}

func (m *RAGModule) Init(deps *module.Dependencies) error {
	cfg := deps.Config

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
		localModelService := services.NewModelService(modelRepository, cfg.RAG, deps.Logger)
		if err := localModelService.SeedDefaults(context.Background()); err != nil {
			deps.Logger.Warn("Failed to seed default RAG models", slog.String("error", err.Error()))
		}
		ragModelProvider = localModelService
	}

	// Text extractor using Gotenberg
	textExtractor := services.NewTextExtractor(cfg.Documents.GotenbergURL)

	// Ingestion + query services require graph repository
	if graphProvider, ok := module.GetTyped[iface.GraphProvider](deps.Services, module.ServiceGraphRepo); ok {
		ingestionService := services.NewIngestionService(
			documentRepository, relTypeRepo, graphProvider, ragModelProvider, textExtractor,
			cfg.RAG.ChunkSize, cfg.RAG.ChunkOverlap, deps.Logger,
		)
		m.documentHandler = handlers.NewDocumentHandler(ingestionService)

		ragQueryService := services.NewQueryService(graphProvider, ragModelProvider, cfg.RAG.DefaultTopK, deps.Logger)
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
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.AuthMW.RequireHierarchicalRole("administrator"))
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

func (m *RAGModule) Start(_ context.Context) error      { return nil }
func (m *RAGModule) Stop(_ context.Context) error       { return nil }
func (m *RAGModule) HealthCheck(_ context.Context) error { return nil }
