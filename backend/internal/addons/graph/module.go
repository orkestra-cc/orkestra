package graph

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/orkestra/backend/internal/addons/graph/handlers"
	"github.com/orkestra/backend/internal/addons/graph/repository"
	"github.com/orkestra/backend/internal/addons/graph/services"
	"github.com/orkestra/backend/internal/shared/capability"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

// Default image reference for the Memgraph container. Overridable via the
// "image" module config field. The -mage variant ships the MAGE algorithm
// library that graph/services/algorithm_service.go relies on (pagerank,
// louvain, etc.) — plain memgraph/memgraph would not be sufficient.
const defaultMemgraphImage = "memgraph/memgraph-mage:latest"

type GraphModule struct {
	module.BaseModule
	handler   *handlers.GraphHandler
	graphRepo repository.GraphRepository
	driver    neo4j.DriverWithContext

	// Retained so InfraContainers() can read the live "image" config on
	// every toggle (lets admins upgrade Memgraph without a restart).
	deps *module.Dependencies
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
		{Key: "uri", Group: "Connection", Label: "Connection URI", Type: module.FieldString, Required: true, Default: "bolt://orkestra-memgraph:7687", EnvVar: "GRAPH_URI"},
		{Key: "username", Group: "Connection", Label: "Username", Type: module.FieldString, EnvVar: "GRAPH_USERNAME"},
		{Key: "password", Group: "Connection", Label: "Password", Type: module.FieldSecret, EnvVar: "GRAPH_PASSWORD"},
		{Key: "database", Group: "Connection", Label: "Database Name", Type: module.FieldString, Default: "memgraph", EnvVar: "GRAPH_DATABASE"},
		{Key: "maxConnPool", Group: "Connection", Label: "Max Connection Pool", Type: module.FieldInt, Default: "50", EnvVar: "GRAPH_MAX_CONN_POOL"},
		{Key: "image", Group: "Container", Label: "Memgraph Image", Type: module.FieldString, Default: defaultMemgraphImage, EnvVar: "GRAPH_IMAGE",
			Description: "Docker image used when the backend manages the Memgraph container's lifecycle. Use the -mage variant to get the algorithm library."},
	}
}

func (m *GraphModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "platform", Tier: "internal", Name: "Graph Database", Icon: "project-diagram", Path: "/graph", MinRole: "administrator",
			Active: true,
			Children: []module.NavItemSpec{
				{Name: "Explorer", Icon: "search", Path: "/graph/explorer", Active: true},
				{Name: "Algorithms", Icon: "brain", Path: "/graph/algorithms", Active: true},
				{Name: "Databases", Icon: "database", Path: "/graph/databases", Active: true},
				{Name: "Vector Search", Icon: "vector-square", Path: "/graph/vector", Active: true},
			},
		},
	}
}

func (m *GraphModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "graph.query.read", Module: "graph", Description: "Run read-only Cypher and browse the graph"},
		{Key: "graph.query.write", Module: "graph", Description: "Run mutating Cypher queries"},
		{Key: "graph.admin", Module: "graph", Description: "Manage indexes, algorithms, and vector search"},
	}
}

func (m *GraphModule) Capabilities() []capability.Capability {
	return []capability.Capability{
		{
			ID:          "graph.access",
			Module:      "graph",
			Action:      "access",
			Title:       "Knowledge Graph",
			Description: "Query and browse the Memgraph knowledge graph, run MAGE algorithms, and use vector search.",
			Published:   true,
		},
	}
}

// Init wires up repositories, services, and handlers against a driver that
// has not been dialed yet. The neo4j Go driver is lazy — NewDriverWithContext
// only constructs the client, TCP only opens on VerifyConnectivity or the
// first session — so this is safe even when orkestra-memgraph isn't up yet.
// Actual connectivity is verified in Start(), after the registry has brought
// up the backend-managed container.
func (m *GraphModule) Init(deps *module.Dependencies) error {
	m.deps = deps

	graphDriver, err := database.NewGraphDriver(database.GraphDBConfig{
		URI:         deps.GetConfig("graph", "uri"),
		Username:    deps.GetConfig("graph", "username"),
		Password:    deps.GetSecret("graph", "password"),
		Database:    deps.GetConfig("graph", "database"),
		MaxConnPool: deps.GetConfigInt("graph", "maxConnPool", 50),
	})
	if err != nil {
		return err
	}
	m.driver = graphDriver

	graphDatabase := deps.GetConfig("graph", "database")
	graphURI := deps.GetConfig("graph", "uri")

	m.graphRepo = repository.NewGraphRepository(graphDriver, graphDatabase)
	graphService := services.NewGraphService(m.graphRepo, deps.Logger)
	algorithmService := services.NewAlgorithmService(m.graphRepo, deps.Logger)
	vectorService := services.NewVectorService(m.graphRepo, deps.Logger)
	m.handler = handlers.NewGraphHandler(graphService, algorithmService, vectorService, graphURI)

	// Register GraphRepository for RAG module consumption. Safe to expose
	// before Start because RAG consumers only run queries when they're
	// themselves started, and the registry starts graph first (rag
	// declares graph in Dependencies()).
	deps.Services.Register(module.ServiceGraphRepo, m.graphRepo)

	deps.Logger.Info("Graph database module initialized",
		slog.String("uri", graphURI),
		slog.String("database", graphDatabase),
	)
	return nil
}

func (m *GraphModule) RegisterRoutes(ri *module.RouteInfo) {
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.Operator.AuthMW.RequireCapability("graph.access"))
		r.Use(ri.Operator.AuthMW.RequirePermission("graph.query.read"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(api, m.handler)
	})
}

// Start verifies connectivity to Memgraph. The registry has already brought
// up orkestra-memgraph (via InfraContainers) and the container manager has
// waited for port 7687 to accept TCP connections — but Memgraph buffers the
// Bolt handshake for another second or two after accepting, so retry here.
func (m *GraphModule) Start(ctx context.Context) error {
	if m.driver == nil {
		return nil // Init hasn't run (or failed) — nothing to verify.
	}
	return database.VerifyGraphConnection(ctx, m.driver, 30*time.Second)
}

// Stop is a no-op. The driver is kept alive for the process lifetime so
// that toggling the module off and on again reuses the same connection
// pool. The pool reconnects transparently when the container comes back.
func (m *GraphModule) Stop(_ context.Context) error      { return nil }
func (m *GraphModule) HealthCheck(_ context.Context) error { return nil }

// InfraContainers declares the Memgraph container the graph module owns.
// Toggling the module on at /admin/modules creates and starts this
// container (along with its named data volume) before Start() runs;
// toggling it off stops the container but the volume is retained so data
// survives toggles. Mirrors the agents module / Hindsight pattern.
func (m *GraphModule) InfraContainers() []module.InfraContainerSpec {
	if m.deps == nil {
		return nil
	}
	image := m.deps.GetConfig("graph", "image")
	if image == "" {
		image = defaultMemgraphImage
	}

	return []module.InfraContainerSpec{{
		Name:    "orkestra-memgraph",
		Image:   image,
		Volumes: []module.InfraVolumeMount{{Name: "orkestra-memgraph-data", Target: "/var/lib/memgraph"}},
		Ports: []module.InfraPortBinding{
			{HostPort: 7687, ContainerPort: 7687, Protocol: "tcp"}, // Bolt
			{HostPort: 7444, ContainerPort: 7444, Protocol: "tcp"}, // monitoring/metrics
		},
		Network: "orkestra-network",
		HealthCheck: &module.InfraHealthCheck{
			TCPPort:  7687,
			Interval: 2 * time.Second,
			Retries:  30,
			Timeout:  5 * time.Second,
		},
		ReadyTimeout: 60 * time.Second,
		Labels:       map[string]string{"orkestra.managed": "true", "orkestra.module": "graph"},
	}}
}

