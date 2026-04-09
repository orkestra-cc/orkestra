package graph

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/addons/graph/handlers"
	"github.com/orkestra/backend/internal/addons/graph/repository"
	"github.com/orkestra/backend/internal/addons/graph/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

type GraphModule struct {
	module.BaseModule
	handler    *handlers.GraphHandler
	graphRepo  repository.GraphRepository
	disconnect func(ctx context.Context) error
}

func NewModule() *GraphModule { return &GraphModule{} }

func (m *GraphModule) Name() string                   { return "graph" }
func (m *GraphModule) DisplayName() string             { return "Graph Database" }
func (m *GraphModule) Description() string             { return "Memgraph graph database for knowledge graphs and algorithms" }
func (m *GraphModule) Category() module.ModuleCategory { return module.CategoryExternal }
func (m *GraphModule) Enabled(cfg *config.Config) bool { return cfg.Graph.Enabled }

func (m *GraphModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceGraphRepo}
}

func (m *GraphModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "uri", Label: "Connection URI", Type: module.FieldString, Required: true, Default: "bolt://memgraph:7687", EnvVar: "GRAPH_URI"},
		{Key: "username", Label: "Username", Type: module.FieldString, EnvVar: "GRAPH_USERNAME"},
		{Key: "password", Label: "Password", Type: module.FieldSecret, EnvVar: "GRAPH_PASSWORD"},
		{Key: "database", Label: "Database Name", Type: module.FieldString, Default: "memgraph", EnvVar: "GRAPH_DATABASE"},
		{Key: "maxConnPool", Label: "Max Connection Pool", Type: module.FieldInt, Default: "50", EnvVar: "GRAPH_MAX_CONN_POOL"},
	}
}

func (m *GraphModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Group: "System Administration", Name: "Graph Database", Icon: "project-diagram", Path: "/graph",
			MinRole: "administrator", Active: true,
			Children: []module.NavItemSpec{
				{Name: "Explorer", Icon: "search", Path: "/graph/explorer", Active: true},
				{Name: "Algorithms", Icon: "brain", Path: "/graph/algorithms", Active: true},
				{Name: "Databases", Icon: "database", Path: "/graph/databases", Active: true},
				{Name: "Vector Search", Icon: "vector-square", Path: "/graph/vector", Active: true},
			},
		},
	}
}

func (m *GraphModule) Init(deps *module.Dependencies) error {
	graphDriver, err := database.NewGraphConnection(context.Background(), database.GraphDBConfig{
		URI:         deps.GetConfig("graph", "uri"),
		Username:    deps.GetConfig("graph", "username"),
		Password:    deps.GetSecret("graph", "password"),
		Database:    deps.GetConfig("graph", "database"),
		MaxConnPool: deps.GetConfigInt("graph", "maxConnPool", 50),
	})
	if err != nil {
		return err
	}
	m.disconnect = func(ctx context.Context) error {
		database.DisconnectGraph(ctx, graphDriver)
		return nil
	}

	graphDatabase := deps.GetConfig("graph", "database")
	graphURI := deps.GetConfig("graph", "uri")

	m.graphRepo = repository.NewGraphRepository(graphDriver, graphDatabase)
	graphService := services.NewGraphService(m.graphRepo, deps.Logger)
	algorithmService := services.NewAlgorithmService(m.graphRepo, deps.Logger)
	vectorService := services.NewVectorService(m.graphRepo, deps.Logger)
	m.handler = handlers.NewGraphHandler(graphService, algorithmService, vectorService, graphURI)

	// Register GraphRepository for RAG module consumption
	deps.Services.Register(module.ServiceGraphRepo, m.graphRepo)

	deps.Logger.Info("Graph database module initialized",
		slog.String("uri", graphURI),
		slog.String("database", graphDatabase),
	)
	return nil
}

func (m *GraphModule) RegisterRoutes(ri *module.RouteInfo) {
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.AuthMW.RequireHierarchicalRole("administrator"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(api, m.handler)
	})
}

func (m *GraphModule) Start(_ context.Context) error      { return nil }
func (m *GraphModule) Stop(ctx context.Context) error {
	if m.disconnect != nil {
		return m.disconnect(ctx)
	}
	return nil
}
func (m *GraphModule) HealthCheck(_ context.Context) error { return nil }
