package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/orkestra/backend/internal/auth/handlers"
	"github.com/orkestra/backend/internal/auth/models"
	"github.com/orkestra/backend/internal/auth/repository"
	"github.com/orkestra/backend/internal/auth/services"
	"github.com/orkestra/backend/internal/billing"
	billingConfig "github.com/orkestra/backend/internal/billing/config"
	billingHandlers "github.com/orkestra/backend/internal/billing/handlers"
	billingJobs "github.com/orkestra/backend/internal/billing/jobs"
	billingRepo "github.com/orkestra/backend/internal/billing/repository"
	billingSvc "github.com/orkestra/backend/internal/billing/services"
	"github.com/orkestra/backend/internal/company"
	companyConfig "github.com/orkestra/backend/internal/company/config"
	companyHandlers "github.com/orkestra/backend/internal/company/handlers"
	companyRepo "github.com/orkestra/backend/internal/company/repository"
	companySvc "github.com/orkestra/backend/internal/company/services"
	"github.com/orkestra/backend/internal/graph"
	graphHandlers "github.com/orkestra/backend/internal/graph/handlers"
	graphRepo "github.com/orkestra/backend/internal/graph/repository"
	graphSvc "github.com/orkestra/backend/internal/graph/services"
	"github.com/orkestra/backend/internal/documents"
	"github.com/orkestra/backend/internal/aimodels"
	aimodelsHandlers "github.com/orkestra/backend/internal/aimodels/handlers"
	aimodelsRepo "github.com/orkestra/backend/internal/aimodels/repository"
	aimodelsSvc "github.com/orkestra/backend/internal/aimodels/services"
	"github.com/orkestra/backend/internal/agents"
	agentsHandlers "github.com/orkestra/backend/internal/agents/handlers"
	agentsRepo "github.com/orkestra/backend/internal/agents/repository"
	agentsSvc "github.com/orkestra/backend/internal/agents/services"
	"github.com/orkestra/backend/internal/sales"
	salesHandlers "github.com/orkestra/backend/internal/sales/handlers"
	salesRepo "github.com/orkestra/backend/internal/sales/repository"
	salesSvc "github.com/orkestra/backend/internal/sales/services"
	"github.com/orkestra/backend/internal/rag"
	ragHandlers "github.com/orkestra/backend/internal/rag/handlers"
	ragRepo "github.com/orkestra/backend/internal/rag/repository"
	ragSvc "github.com/orkestra/backend/internal/rag/services"
	documentsConfig "github.com/orkestra/backend/internal/documents/config"
	documentsHandlers "github.com/orkestra/backend/internal/documents/handlers"
	documentsRepo "github.com/orkestra/backend/internal/documents/repository"
	documentsSvc "github.com/orkestra/backend/internal/documents/services"
	devHandlers "github.com/orkestra/backend/internal/dev/handlers"
	"github.com/orkestra/backend/internal/navigation"
	"github.com/orkestra/backend/internal/reporting"
	reportingHandlers "github.com/orkestra/backend/internal/reporting/handlers"
	reportingRepository "github.com/orkestra/backend/internal/reporting/repository"
	reportingServices "github.com/orkestra/backend/internal/reporting/services"
	"github.com/orkestra/backend/internal/user"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/internal/shared/errors"
	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/utils"
	userHandlers "github.com/orkestra/backend/internal/user/handlers"
	userRepository "github.com/orkestra/backend/internal/user/repository"
	userServices "github.com/orkestra/backend/internal/user/services"
	navigationConfig "github.com/orkestra/backend/internal/navigation/config"
	navigationHandlers "github.com/orkestra/backend/internal/navigation/handlers"
	navigationServices "github.com/orkestra/backend/internal/navigation/services"
)

func main() {
	logger := utils.SetupLogger()
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mongoConfig := database.MongoConfig{
		URI:             cfg.Database.MongoURI,
		Database:        cfg.Database.DatabaseName,
		MaxPoolSize:     cfg.Database.MaxPoolSize,
		MinPoolSize:     cfg.Database.MinPoolSize,
		MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
		ConnectTimeout:  cfg.Database.ConnectTimeout,
	}

	db, err := database.NewMongoConnection(ctx, mongoConfig)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}

	redisConfig := database.RedisConfig{
		URL:             cfg.Redis.URL,
		MaxRetries:      cfg.Redis.MaxRetries,
		MinIdleConns:    cfg.Redis.MinIdleConns,
		MaxIdleConns:    cfg.Redis.MaxIdleConns,
		ConnMaxLifetime: cfg.Redis.ConnMaxLifetime,
		ReadTimeout:     cfg.Redis.ReadTimeout,
		WriteTimeout:    cfg.Redis.WriteTimeout,
	}

	redisClient, err := database.NewRedisConnection(ctx, redisConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	// Initialize new enhanced repositories
	authRepo, err := repository.NewAuthRepository(db)
	if err != nil {
		log.Fatalf("Failed to create auth repository: %v", err)
	}
	oauthProviderRepo := repository.NewOAuthProviderRepository(db)

	refreshTokenRepo := repository.NewRefreshTokenRepository(db)
	authSessionRepo := repository.NewAuthSessionRepository(db)

	// Initialize OAuth provider factory with configurations
	providerConfigs := map[models.OAuthProvider]*services.OAuthProviderConfig{
		models.OAuthProviderGoogle: {
			ClientID:     cfg.Auth.Google.ClientID,
			ClientSecret: cfg.Auth.Google.ClientSecret,
			Scopes:       []string{"openid", "email", "profile"},
			AdditionalConfig: map[string]string{
				"android_client_id": cfg.Auth.Google.AndroidClientID,
				"ios_client_id":     cfg.Auth.Google.IOSClientID,
			},
		},
		models.OAuthProviderApple: {
			ClientID:     cfg.Auth.Apple.ClientID,
			ClientSecret: "", // JWT-based for Apple
			Scopes:       []string{"name", "email"},
			AdditionalConfig: map[string]string{
				"team_id":          cfg.Auth.Apple.TeamID,
				"key_id":           cfg.Auth.Apple.KeyID,
				"private_key":      cfg.Auth.Apple.PrivateKey,
				"private_key_path": cfg.Auth.Apple.PrivateKeyPath,
				"redirect_url":     cfg.Auth.Apple.RedirectURL,
			},
		},
		models.OAuthProviderGitHub: {
			ClientID:     cfg.Auth.GitHub.ClientID,
			ClientSecret: cfg.Auth.GitHub.ClientSecret,
			Scopes:       []string{"user:email", "read:user"},
			AdditionalConfig: map[string]string{
				"redirect_url": cfg.Auth.GitHub.RedirectURL,
			},
		},
		models.OAuthProviderDiscord: {
			ClientID:     cfg.Auth.Discord.ClientID,
			ClientSecret: cfg.Auth.Discord.ClientSecret,
			Scopes:       []string{"identify", "email"},
			AdditionalConfig: map[string]string{
				"redirect_url": cfg.Auth.Discord.RedirectURL,
			},
		},
	}

	redisClientAdapter := database.NewRedisClientAdapter(redisClient)
	providerFactory := services.NewOAuthProviderFactory(providerConfigs, redisClientAdapter)

	// Initialize OAuth provider manager
	providerManager := services.NewOAuthProviderManager(providerFactory)
	_ = providerManager // TODO: Use in auth handlers

	// Initialize JWT services
	jwtService := services.NewJWTService(cfg.Auth.JWT.PrivateKey, cfg.Auth.JWT.PublicKey)

	// Initialize OAuth state service with Redis adapter
	redisStore := services.NewRedisOAuthStateStore(redisClientAdapter)
	oauthStateService := services.NewOAuthStateService(redisStore)

	// Initialize user management module first (required by auth service)
	userRepo := userRepository.NewUserRepository(db)
	userService := userServices.NewUserService(userRepo, oauthProviderRepo)
	userHandler := userHandlers.NewUserHandler(userService)

	// Initialize reporting module
	reportingRepo := reportingRepository.NewDeadlineRepository(db)
	reportingService := reportingServices.NewDeadlineService(reportingRepo)
	reportingHandler := reportingHandlers.NewDeadlineHandler(reportingService)

	// Initialize navigation module
	menuConfig := navigationConfig.NewMenuConfig()
	navigationService := navigationServices.NewNavigationService(menuConfig)
	navigationHandler := navigationHandlers.NewNavigationHandler(navigationService)

	// Initialize documents module (PDF generation with Gotenberg)
	// IMPORTANT: Documents module must be initialized BEFORE billing module
	// so pdfSvc can be injected into billing's InvoiceService
	var documentsTemplateHandler *documentsHandlers.TemplateHandler
	var documentsDocumentHandler *documentsHandlers.DocumentHandler
	var pdfSvc documentsSvc.PDFService // Declared here so billing module can use it
	documentsEnabled := cfg.Documents.GotenbergURL != ""

	if documentsEnabled {
		// Create documents config
		docsConfig := &documentsConfig.Config{
			GotenbergURL:  cfg.Documents.GotenbergURL,
			Timeout:       cfg.Documents.Timeout,
			RetryAttempts: cfg.Documents.RetryAttempts,
			DefaultMargins: documentsConfig.PDFMargins{
				Top:    cfg.Documents.DefaultMargins.Top,
				Bottom: cfg.Documents.DefaultMargins.Bottom,
				Left:   cfg.Documents.DefaultMargins.Left,
				Right:  cfg.Documents.DefaultMargins.Right,
			},
		}

		// Create repositories
		templateRepo := documentsRepo.NewTemplateRepository(db)
		documentRepo := documentsRepo.NewDocumentRepository(db)

		// Create services
		gotenbergClient := documentsSvc.NewGotenbergClient(docsConfig, logger)
		templateEngine := documentsSvc.NewTemplateEngine()
		templateSvc := documentsSvc.NewTemplateService(templateRepo, templateEngine, logger)
		pdfSvc = documentsSvc.NewPDFService(templateRepo, documentRepo, gotenbergClient, templateEngine, logger)

		// Create handlers
		documentsTemplateHandler = documentsHandlers.NewTemplateHandler(templateSvc)
		documentsDocumentHandler = documentsHandlers.NewDocumentHandler(pdfSvc)

		// Seed built-in templates
		if err := templateSvc.SeedBuiltInTemplates(context.Background()); err != nil {
			logger.Warn("Failed to seed built-in templates", slog.String("error", err.Error()))
		}

		logger.Info("Documents module initialized",
			slog.String("gotenbergURL", cfg.Documents.GotenbergURL),
		)
	} else {
		logger.Warn("Documents module disabled: GOTENBERG_URL not configured")
	}

	// Initialize billing module (OpenAPI SDI integration)
	var billingPollingJob *billingJobs.PollingJob
	var billingInvoiceHandler *billingHandlers.InvoiceHandler
	var billingCustomerHandler *billingHandlers.CustomerHandler
	var billingSupplierHandler *billingHandlers.SupplierHandler
	var billingCompanyHandler *billingHandlers.CompanyHandler
	var billingNotificationHandler *billingHandlers.NotificationHandler
	var billingBusinessRegistryHandler *billingHandlers.BusinessRegistryHandler
	var billingSyncHandler *billingHandlers.SyncHandler
	var billingWebhookHandler *billingHandlers.WebhookHandler
	var openAPIClient billingSvc.OpenAPIClient
	billingEnabled := cfg.Billing.OpenAPIBearerToken != ""

	if billingEnabled {
		// Create OpenAPI config from shared config
		openAPIConfig := &billingConfig.OpenAPIConfig{
			BaseURL:        cfg.Billing.OpenAPIBaseURL,
			BearerToken:    cfg.Billing.OpenAPIBearerToken,
			FiscalID:       cfg.Billing.OpenAPIFiscalID,
			RecipientCode:  cfg.Billing.OpenAPIRecipientCode,
			ApplySignature: cfg.Billing.ApplySignature,
			ApplyStorage:   cfg.Billing.ApplyStorage,
			Timeout:        cfg.Billing.Timeout,
			RetryAttempts:  cfg.Billing.RetryAttempts,
			SandboxMode:    cfg.Billing.SandboxMode,
		}

		// Create repositories
		invoiceRepo := billingRepo.NewInvoiceRepository(db)
		customerRepo := billingRepo.NewCustomerRepository(db)
		supplierRepo := billingRepo.NewSupplierRepository(db)
		companyRepo := billingRepo.NewCompanyRepository(db)
		notificationRepo := billingRepo.NewNotificationRepository(db)

		// Create OpenAPI client with Redis caching and XML builder
		openAPIClient = billingSvc.NewOpenAPIClientWithCache(openAPIConfig, logger, redisClientAdapter)
		xmlBuilder := billingSvc.NewXMLBuilder(openAPIConfig)

		// Create services (pdfSvc can be nil if documents module is disabled, xmlParser nil uses default)
		invoiceSvc := billingSvc.NewInvoiceService(invoiceRepo, customerRepo, supplierRepo, companyRepo, openAPIClient, xmlBuilder, nil, pdfSvc, logger)
		customerSvc := billingSvc.NewCustomerService(customerRepo, logger)
		supplierSvc := billingSvc.NewSupplierService(supplierRepo, logger)
		companySvc := billingSvc.NewCompanyService(companyRepo, openAPIClient, logger)
		notificationSvc := billingSvc.NewNotificationService(notificationRepo, logger)

		// Create handlers
		billingInvoiceHandler = billingHandlers.NewInvoiceHandler(invoiceSvc)
		billingCustomerHandler = billingHandlers.NewCustomerHandler(customerSvc)
		billingSupplierHandler = billingHandlers.NewSupplierHandler(supplierSvc)
		billingCompanyHandler = billingHandlers.NewCompanyHandler(companySvc)
		billingNotificationHandler = billingHandlers.NewNotificationHandler(notificationSvc)
		billingBusinessRegistryHandler = billingHandlers.NewBusinessRegistryHandler(openAPIClient)

		// Create polling job
		billingPollingJob = billingJobs.NewPollingJob(
			openAPIClient,
			invoiceRepo,
			notificationRepo,
			logger,
			cfg.Billing.PollingInterval,
		)

		// Create sync handler for manual sync endpoints
		billingSyncHandler = billingHandlers.NewSyncHandler(billingPollingJob)

		// Create webhook handler for SDI callbacks
		if cfg.Billing.WebhookURL != "" {
			billingWebhookHandler = billingHandlers.NewWebhookHandler(
				billingPollingJob,
				cfg.Billing.WebhookSecret,
				logger,
			)
		}

		logger.Info("Billing module initialized",
			slog.String("baseURL", cfg.Billing.OpenAPIBaseURL),
			slog.Bool("sandbox", cfg.Billing.SandboxMode),
		)
	} else {
		logger.Warn("Billing module disabled: OPENAPI_BILLING_BEARER_TOKEN not configured")
	}

	// Initialize company lookup module (OpenAPI Company API)
	var companyLookupHandler *companyHandlers.CompanyHandler
	companyEnabled := cfg.Company.BearerToken != ""

	if companyEnabled {
		companyCfg := &companyConfig.CompanyAPIConfig{
			BaseURL:       cfg.Company.BaseURL,
			BearerToken:   cfg.Company.BearerToken,
			Timeout:       cfg.Company.Timeout,
			RetryAttempts: cfg.Company.RetryAttempts,
			CacheTTL:      cfg.Company.CacheTTL,
		}

		companyRepository := companyRepo.NewCompanyRepository(db)
		companyClient := companySvc.NewCompanyAPIClientWithCache(companyCfg, logger, redisClientAdapter)
		companyService := companySvc.NewCompanyService(companyRepository, companyClient, logger)
		companyLookupHandler = companyHandlers.NewCompanyHandler(companyService)

		logger.Info("Company lookup module initialized",
			slog.String("baseURL", cfg.Company.BaseURL),
		)
	} else {
		logger.Warn("Company lookup module disabled: OPENAPI_COMPANY_BEARER_TOKEN not configured")
	}

	// Initialize graph database module (Memgraph)
	var graphHandler *graphHandlers.GraphHandler
	var graphRepository graphRepo.GraphRepository
	graphEnabled := cfg.Graph.Enabled

	if graphEnabled {
		graphDriver, err := database.NewGraphConnection(ctx, database.GraphDBConfig{
			URI:         cfg.Graph.URI,
			Username:    cfg.Graph.Username,
			Password:    cfg.Graph.Password,
			Database:    cfg.Graph.Database,
			MaxConnPool: cfg.Graph.MaxConnPool,
		})
		if err != nil {
			log.Fatalf("Failed to connect to graph database: %v", err)
		}
		defer database.DisconnectGraph(ctx, graphDriver)

		graphRepository = graphRepo.NewGraphRepository(graphDriver, cfg.Graph.Database)
		graphService := graphSvc.NewGraphService(graphRepository, logger)
		algorithmService := graphSvc.NewAlgorithmService(graphRepository, logger)
		vectorService := graphSvc.NewVectorService(graphRepository, logger)
		graphHandler = graphHandlers.NewGraphHandler(graphService, algorithmService, vectorService, cfg.Graph.URI)

		logger.Info("Graph database module initialized",
			slog.String("uri", cfg.Graph.URI),
			slog.String("database", cfg.Graph.Database),
		)
	} else {
		logger.Warn("Graph database module disabled: GRAPH_ENABLED not set")
	}

	// Initialize AI Models module (standalone model management)
	var aiModelHandler *aimodelsHandlers.ModelHandler
	var aiModelService aimodelsSvc.AIModelService
	aimodelsEnabled := cfg.AIModels.Enabled

	if aimodelsEnabled {
		aiModelRepository := aimodelsRepo.NewModelRepository(db)
		aiModelService = aimodelsSvc.NewModelService(aiModelRepository, aimodelsSvc.AIModelsConfig{
			OllamaBaseURL: cfg.AIModels.OllamaBaseURL,
			OpenAIAPIKey:  cfg.AIModels.OpenAIAPIKey,
			AnthropicKey:  cfg.AIModels.AnthropicKey,
			GeminiKey:     cfg.AIModels.GeminiKey,
		}, logger)

		aiModelHandler = aimodelsHandlers.NewModelHandler(aiModelService)

		// Seed default models on first startup
		if err := aiModelService.SeedDefaults(ctx); err != nil {
			logger.Warn("Failed to seed default AI models", slog.String("error", err.Error()))
		}

		logger.Info("AI Models module initialized")
	} else {
		logger.Warn("AI Models module disabled: AIMODELS_ENABLED not set")
	}

	// Initialize RAG module
	var ragDocumentHandler *ragHandlers.DocumentHandler
	var ragQueryHandler *ragHandlers.QueryHandler
	var ragStreamHandler *ragHandlers.StreamHandler
	var ragRelationshipHandler *ragHandlers.RelationshipHandler
	var ragQueryService ragSvc.QueryService // hoisted for agents module
	ragEnabled := cfg.RAG.Enabled

	if ragEnabled {
		documentRepository := ragRepo.NewDocumentRepository(db)
		relTypeRepo := ragRepo.NewRelationshipTypeRepository(db)
		relTypeService := ragSvc.NewRelationshipTypeService(relTypeRepo, logger)
		ragRelationshipHandler = ragHandlers.NewRelationshipHandler(relTypeService)

		// Use aimodels service if available, otherwise create a standalone model service for RAG
		var ragModelProvider ragSvc.AIModelProvider
		if aiModelService != nil {
			ragModelProvider = aiModelService
		} else {
			// Fallback: create a local model service for RAG when aimodels module is disabled
			modelRepository := ragRepo.NewModelRepository(db)
			localModelService := ragSvc.NewModelService(modelRepository, cfg.RAG, logger)
			if err := localModelService.SeedDefaults(ctx); err != nil {
				logger.Warn("Failed to seed default RAG models", slog.String("error", err.Error()))
			}
			ragModelProvider = localModelService
		}

		// Text extractor using Gotenberg
		textExtractor := ragSvc.NewTextExtractor(cfg.Documents.GotenbergURL)

		// Ingestion service (depends on graph repo if graph module is enabled)
		if graphEnabled {
			ingestionService := ragSvc.NewIngestionService(
				documentRepository, relTypeRepo, graphRepository, ragModelProvider, textExtractor,
				cfg.RAG.ChunkSize, cfg.RAG.ChunkOverlap, logger,
			)
			ragDocumentHandler = ragHandlers.NewDocumentHandler(ingestionService)

			// Query service
			ragQueryService = ragSvc.NewQueryService(graphRepository, ragModelProvider, cfg.RAG.DefaultTopK, logger)
			ragQueryHandler = ragHandlers.NewQueryHandler(ragQueryService)
			ragStreamHandler = ragHandlers.NewStreamHandler(ragQueryService, logger)
		}

		logger.Info("RAG module initialized")
	} else {
		logger.Warn("RAG module disabled: RAG_ENABLED not set")
	}

	// Initialize Agents module (Hindsight AI agent integration)
	var agentProjectHandler *agentsHandlers.ProjectHandler
	var agentQueryHandler *agentsHandlers.AgentHandler
	var personalAgentHandler *agentsHandlers.PersonalAgentHandler
	agentsEnabled := cfg.Agents.Enabled

	if agentsEnabled {
		projectRepo := agentsRepo.NewProjectRepository(db)
		conversationRepo := agentsRepo.NewConversationRepository(db)

		hsClient := agentsSvc.NewHindsightClient(cfg.Agents.HindsightURL, logger)

		// Create RAG bridge if RAG query service is available
		var ragBridge agentsSvc.RAGBridge
		if ragQueryService != nil {
			ragBridge = agentsSvc.NewRAGBridge(ragQueryService, cfg.RAG.DefaultTopK, logger)
		}

		projectService := agentsSvc.NewProjectService(projectRepo, hsClient, cfg.Agents.HindsightNamespace, logger)
		agentService := agentsSvc.NewAgentService(projectRepo, conversationRepo, hsClient, ragBridge, logger)

		agentProjectHandler = agentsHandlers.NewProjectHandler(projectService)
		agentQueryHandler = agentsHandlers.NewAgentHandler(agentService)

		// Personal agent — per-user auto-provisioned agent
		personalAgentService := agentsSvc.NewPersonalAgentService(projectRepo, agentService, hsClient, cfg.Agents.HindsightNamespace, logger)
		personalAgentHandler = agentsHandlers.NewPersonalAgentHandler(personalAgentService)

		logger.Info("Agents module initialized",
			slog.String("hindsightURL", cfg.Agents.HindsightURL),
		)
	} else {
		logger.Warn("Agents module disabled: AGENTS_ENABLED not set")
	}

	// Initialize Sales Intelligence module
	var salesSkillHandler *salesHandlers.SkillHandler
	var salesProspectHandler *salesHandlers.ProspectHandler
	var salesJobHandler *salesHandlers.JobHandler
	var salesReportHandler *salesHandlers.ReportHandler
	var salesPromptHandler *salesHandlers.PromptHandler
	var salesSettingsHandler *salesHandlers.SettingsHandler
	salesEnabled := cfg.Sales.Enabled

	if salesEnabled {
		salesJobRepo := salesRepo.NewJobRepository(db)

		// Sales module uses the AI model provider through a consumer interface
		var salesModelProvider salesSvc.AIModelProvider
		if aiModelService != nil {
			salesModelProvider = aiModelService
		}

		// Optional: company enrichment for Italian business registry
		var salesEnrichment salesSvc.CompanyEnrichmentService
		// TODO: wire companyService adapter when ready

		salesPromptRepo := salesRepo.NewPromptRepository(db)
		promptLoader := salesSvc.NewPromptLoader(salesPromptRepo, logger)

		// Seed default prompts from embedded files on first run
		if err := promptLoader.SeedDefaults(context.Background()); err != nil {
			logger.Warn("Failed to seed sales prompts", slog.String("error", err.Error()))
		}
		scraper := salesSvc.NewScraper(cfg.Sales, logger)
		agentExecutor := salesSvc.NewAgentExecutor(cfg.Sales.MaxConcurrency, logger)
		scorer := salesSvc.NewScorer()

		salesSettingsRepo := salesRepo.NewSettingsRepository(db)
		salesReportRepo := salesRepo.NewReportRepository(db)
		reportGen := salesSvc.NewReportGenerator(salesReportRepo, logger)

		salesBatchRepo := salesRepo.NewBatchRepository(db)

		orchestrator := salesSvc.NewOrchestrator(
			salesJobRepo, salesReportRepo, salesSettingsRepo, salesBatchRepo, salesModelProvider, promptLoader,
			scraper, agentExecutor, scorer, salesEnrichment, reportGen,
			cfg.Sales, logger,
		)

		// Start batch poller for async LLM batch results
		batchPoller := salesSvc.NewBatchPoller(
			salesBatchRepo, salesJobRepo, salesReportRepo,
			salesModelProvider, scorer, reportGen,
			30*time.Second, logger,
		)
		batchPoller.Start()

		skillStore := salesSvc.NewSkillStore()
		salesSkillHandler = salesHandlers.NewSkillHandler(orchestrator, skillStore, logger)
		salesProspectHandler = salesHandlers.NewProspectHandler(orchestrator)
		salesJobHandler = salesHandlers.NewJobHandler(orchestrator)
		salesReportHandler = salesHandlers.NewReportHandler(salesReportRepo, salesJobRepo, reportGen)
		salesPromptHandler = salesHandlers.NewPromptHandler(salesPromptRepo, promptLoader)
		salesSettingsHandler = salesHandlers.NewSettingsHandler(salesSettingsRepo)

		// Mark incomplete jobs from previous runs as failed
		if err := salesJobRepo.MarkStaleJobsFailed(context.Background()); err != nil {
			logger.Warn("Failed to mark stale sales jobs", slog.String("error", err.Error()))
		}

		logger.Info("Sales Intelligence module initialized")
	} else {
		logger.Warn("Sales Intelligence module disabled: SALES_ENABLED not set")
	}

	// Initialize auth service with all repositories
	authService, err := services.NewAuthService(&services.AuthConfig{
		AuthRepo:          authRepo,
		UserService:       userService,
		OAuthProviderRepo: oauthProviderRepo,
		RefreshTokenRepo:  refreshTokenRepo,
		AuthSessionRepo:   authSessionRepo,
		JWTService:        jwtService,
	})
	if err != nil {
		log.Fatalf("Failed to create enhanced auth service: %v", err)
	}

	// Initialize risk assessment service
	riskService := services.NewRiskAssessmentService(nil, nil, nil)
	_ = riskService // TODO: Use in auth handlers

	// Initialize error management system
	isDevelopment := cfg.Server.Environment != "production"
	errorManager := errors.NewManager(logger, isDevelopment)
	defer errorManager.Close()

	// Initialize middleware with JWT service for consistent token handling
	authMiddlewareHandler := authMiddleware.NewAuthMiddlewareWithConfig(jwtService, errorManager, cfg)
	// Set auth service for auto-refresh functionality
	authMiddlewareHandler.SetAuthService(authService)
	deviceMiddlewareHandler := authMiddleware.NewDeviceMiddleware(errorManager)

	router := chi.NewRouter()

	// Security headers middleware
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Security headers to prevent common attacks
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			// HSTS header for production (force HTTPS)
			if cfg.IsProductionLike() {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	})

	// Request body size limit middleware
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > cfg.Server.MaxBodySize {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, cfg.Server.MaxBodySize)
			next.ServeHTTP(w, r)
		})
	})

	// CORS middleware - must be before other middleware
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.Server.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Request-ID"},
		ExposedHeaders:   []string{"Link", "X-Total-Count", "X-Ratelimit-Limit", "X-Ratelimit-Remaining", "X-New-Access-Token", "X-Token-Refreshed"},
		AllowCredentials: true,
		MaxAge:           300, // 5 minutes
	}))

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	// Custom logger that excludes /health endpoint
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				middleware.Logger(next).ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	})

	// Add device information extraction middleware early in the chain
	router.Use(deviceMiddlewareHandler.ExtractDeviceInfo)

	// Add middleware to inject HTTP request into context for Huma handlers
	// Must be careful not to consume the request body as Huma needs to read it
	// Place after device middleware to avoid body consumption issues
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create a new context with the HTTP request reference
			// Don't modify the request body - Huma will handle that
			ctx := context.WithValue(r.Context(), "http_request", r)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	// Add our error handling middleware
	router.Use(errorManager.GetErrorHandler().Middleware())
	router.Use(errorManager.GetValidator().Middleware())
	router.Use(errorManager.GetRateLimiter().Middleware("api:general"))

	// Keep the default recoverer as a fallback
	router.Use(middleware.Recoverer)
	// Timeout middleware with bypass for SSE streaming endpoints
	router.Use(func(next http.Handler) http.Handler {
		timeoutHandler := middleware.Timeout(60 * time.Second)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/stream") {
				next.ServeHTTP(w, r)
				return
			}
			timeoutHandler.ServeHTTP(w, r)
		})
	})

	// Dev token routes - registered directly on main router in non-production
	// These routes are registered BEFORE the protected router mount, so they take precedence
	if !cfg.IsProduction() {
		devTokenHandler := devHandlers.NewDevTokenHandler(jwtService, cfg)
		// Register dev routes directly on the main router (no auth required)
		router.Post("/dev/token", devTokenHandler.GenerateTokenHTTP)
		router.Get("/dev/token/roles", devTokenHandler.ListRolesHTTP)
		logger.Info("Dev token routes registered",
			slog.String("environment", cfg.Server.Environment),
		)
	}

	apiConfig := huma.DefaultConfig("Orkestra API", "1.0.0")
	apiConfig.DocsPath = ""
	apiConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
		},
	}

	// Register public routes first
	publicAPI := humachi.New(router, apiConfig)

	// Create protected routes with authentication middleware
	protectedRouter := chi.NewRouter()
	protectedRouter.Use(authMiddlewareHandler.RequireAuth)
	protectedAPI := humachi.New(protectedRouter, apiConfig)

	// Initialize and register authentication handler
	authHandler := handlers.NewAuthHandler(
		authService,
		providerFactory,
		oauthStateService,
		oauthProviderRepo,
		jwtService,
		cfg,
	)
	authHandler.RegisterRoutes(publicAPI, protectedAPI, router, protectedRouter)

	// Create user management routes with role-based protection within protected router
	// Only administrator, ceo, and developer roles should have access
	protectedRouter.Group(func(r chi.Router) {
		r.Use(authMiddlewareHandler.RequireHierarchicalRole("administrator"))
		userAPI := humachi.New(r, apiConfig)
		// Register user management routes (protected with role restrictions)
		user.RegisterRoutes(userAPI, userHandler)
	})

	// Create reporting routes with role-based protection
	// Only manager, administrator, ceo, and developer roles should have access
	protectedRouter.Group(func(r chi.Router) {
		r.Use(authMiddlewareHandler.RequireHierarchicalRole("manager"))
		reportingAPI := humachi.New(r, apiConfig)
		// Register reporting routes (protected with role restrictions)
		reporting.RegisterRoutes(reportingAPI, reportingHandler)
	})

	// Navigation routes - accessible to all authenticated users
	// Role filtering happens inside the service based on user's role
	navigationAPI := humachi.New(protectedRouter, apiConfig)
	navigation.RegisterRoutes(navigationAPI, navigationHandler)

	// Billing routes - manager role and above
	// Only register if billing module is enabled
	if billingEnabled {
		protectedRouter.Group(func(r chi.Router) {
			r.Use(authMiddlewareHandler.RequireHierarchicalRole("manager"))
			billingAPI := humachi.New(r, apiConfig)
			billing.RegisterRoutes(
				billingAPI,
				billingInvoiceHandler,
				billingCustomerHandler,
				billingSupplierHandler,
				billingCompanyHandler,
				billingNotificationHandler,
				billingBusinessRegistryHandler,
				billingSyncHandler,
			)
		})
	}

	// Billing webhook routes - PUBLIC (no JWT, authenticated via webhook secret)
	if billingEnabled && billingWebhookHandler != nil {
		billing.RegisterWebhookRoutes(publicAPI, billingWebhookHandler)
		logger.Info("Billing webhook routes registered",
			slog.String("webhookURL", cfg.Billing.WebhookURL),
		)
	}

	// Documents routes - manager role and above for templates, all authenticated for PDF generation
	// Only register if documents module is enabled
	if documentsEnabled {
		protectedRouter.Group(func(r chi.Router) {
			r.Use(authMiddlewareHandler.RequireHierarchicalRole("manager"))
			documentsAPI := humachi.New(r, apiConfig)
			documents.RegisterRoutes(
				documentsAPI,
				documentsTemplateHandler,
				documentsDocumentHandler,
			)
		})
	}

	// Company lookup routes - manager role and above
	// Only register if company module is enabled (same token as billing)
	if companyEnabled {
		protectedRouter.Group(func(r chi.Router) {
			r.Use(authMiddlewareHandler.RequireHierarchicalRole("manager"))
			companyAPI := humachi.New(r, apiConfig)
			company.RegisterRoutes(companyAPI, companyLookupHandler)
		})
	}

	// Graph database routes - administrator role and above
	// Provides raw Cypher execution, restricted to trusted roles
	if graphEnabled {
		protectedRouter.Group(func(r chi.Router) {
			r.Use(authMiddlewareHandler.RequireHierarchicalRole("administrator"))
			graphAPI := humachi.New(r, apiConfig)
			graph.RegisterRoutes(graphAPI, graphHandler)
		})
	}

	// AI Models routes - administrator role and above
	if aimodelsEnabled {
		protectedRouter.Group(func(r chi.Router) {
			r.Use(authMiddlewareHandler.RequireHierarchicalRole("administrator"))
			aiModelsAPI := humachi.New(r, apiConfig)
			aimodels.RegisterRoutes(aiModelsAPI, aiModelHandler)
		})
	}

	// RAG routes - administrator role and above
	if ragEnabled {
		protectedRouter.Group(func(r chi.Router) {
			r.Use(authMiddlewareHandler.RequireHierarchicalRole("administrator"))
			ragAPI := humachi.New(r, apiConfig)
			if ragDocumentHandler != nil {
				rag.RegisterDocumentRoutes(ragAPI, ragDocumentHandler)
			}
			if ragQueryHandler != nil {
				rag.RegisterQueryRoutes(ragAPI, ragQueryHandler)
			}
			if ragStreamHandler != nil {
				rag.RegisterStreamRoute(r, ragStreamHandler)
			}
			if ragRelationshipHandler != nil {
				rag.RegisterRelationshipTypeRoutes(ragAPI, ragRelationshipHandler)
			}
		})
	}

	// Agents routes - different role levels for different operations
	if agentsEnabled {
		// Personal agent - any authenticated user (guest and above)
		protectedRouter.Group(func(r chi.Router) {
			r.Use(authMiddlewareHandler.RequireHierarchicalRole("guest"))
			personalAgentAPI := humachi.New(r, apiConfig)
			agents.RegisterPersonalAgentRoutes(personalAgentAPI, personalAgentHandler)
		})

		// Project management - manager role and above
		protectedRouter.Group(func(r chi.Router) {
			r.Use(authMiddlewareHandler.RequireHierarchicalRole("manager"))
			agentsProjectAPI := humachi.New(r, apiConfig)
			agents.RegisterProjectRoutes(agentsProjectAPI, agentProjectHandler)
		})

		// Agent querying and conversations - operator role and above
		protectedRouter.Group(func(r chi.Router) {
			r.Use(authMiddlewareHandler.RequireHierarchicalRole("operator"))
			agentsQueryAPI := humachi.New(r, apiConfig)
			agents.RegisterQueryRoutes(agentsQueryAPI, agentQueryHandler)
		})

		// Agent admin - administrator role and above
		protectedRouter.Group(func(r chi.Router) {
			r.Use(authMiddlewareHandler.RequireHierarchicalRole("administrator"))
			agentsAdminAPI := humachi.New(r, apiConfig)
			agents.RegisterAdminRoutes(agentsAdminAPI, agentQueryHandler)
		})
	}

	// Sales Intelligence routes - manager role and above
	if salesEnabled {
		protectedRouter.Group(func(r chi.Router) {
			r.Use(authMiddlewareHandler.RequireHierarchicalRole("manager"))
			salesAPI := humachi.New(r, apiConfig)
			sales.RegisterSkillRoutes(salesAPI, salesSkillHandler)
			sales.RegisterProspectRoutes(salesAPI, salesProspectHandler)
			sales.RegisterJobRoutes(salesAPI, salesJobHandler)
			sales.RegisterReportRoutes(salesAPI, salesReportHandler)
			sales.RegisterPromptRoutes(salesAPI, salesPromptHandler)
			sales.RegisterReportDownloadRoute(r, salesReportHandler)
			sales.RegisterSettingsRoutes(salesAPI, salesSettingsHandler)
		})
	}

	// Mount the protected routes
	router.Mount("/", protectedRouter)

	// Then register other protected routes with middleware
	router.Group(func(r chi.Router) {
		r.Use(authMiddlewareHandler.RequireAuth)
		// TODO: Add other protected routes here as needed
	})

	router.Route("/protected", func(r chi.Router) {
		r.Use(authMiddlewareHandler.RequireAuth)
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			userID := req.Context().Value("userID").(string)
			w.Write([]byte(fmt.Sprintf("Protected endpoint accessed by user: %s", userID)))
		})
	})

	huma.Register(publicAPI, huma.Operation{
		OperationID: "health-check",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Returns the health status of the application",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Status  string            `json:"status"`
			Time    string            `json:"time"`
			Version string            `json:"version"`
			Checks  map[string]string `json:"checks"`
		}
	}, error) {
		checks := map[string]string{}

		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if err := db.Client().Ping(ctx, nil); err != nil {
			checks["mongodb"] = "down"
		} else {
			checks["mongodb"] = "up"
		}

		if err := redisClient.Ping(ctx).Err(); err != nil {
			checks["redis"] = "down"
		} else {
			checks["redis"] = "up"
		}

		status := "healthy"
		for _, check := range checks {
			if check == "down" {
				status = "unhealthy"
				break
			}
		}

		return &struct {
			Body struct {
				Status  string            `json:"status"`
				Time    string            `json:"time"`
				Version string            `json:"version"`
				Checks  map[string]string `json:"checks"`
			}
		}{
			Body: struct {
				Status  string            `json:"status"`
				Time    string            `json:"time"`
				Version string            `json:"version"`
				Checks  map[string]string `json:"checks"`
			}{
				Status:  status,
				Time:    time.Now().UTC().Format(time.RFC3339),
				Version: "1.0.0",
				Checks:  checks,
			},
		}, nil
	})

	huma.Register(publicAPI, huma.Operation{
		OperationID: "readiness-check",
		Method:      http.MethodGet,
		Path:        "/ready",
		Summary:     "Readiness check",
		Description: "Returns whether the application is ready to accept requests",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Ready bool `json:"ready"`
		}
	}, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		ready := true

		if err := db.Client().Ping(ctx, nil); err != nil {
			ready = false
		}

		if err := redisClient.Ping(ctx).Err(); err != nil {
			ready = false
		}

		return &struct {
			Body struct {
				Ready bool `json:"ready"`
			}
		}{
			Body: struct {
				Ready bool `json:"ready"`
			}{
				Ready: ready,
			},
		}, nil
	})

	// Scalar API Documentation
	router.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		// Override HAProxy CSP for documentation page to allow Scalar CDN
		// This CSP is more permissive to allow the documentation to work
		w.Header().Set("Content-Security-Policy", "default-src 'self' https://cdn.jsdelivr.net; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://fonts.googleapis.com; connect-src 'self' http://localhost:* https://*.blacklab.cc; img-src 'self' data: https:; font-src 'self' data: https://cdn.jsdelivr.net https://fonts.gstatic.com https://fonts.googleapis.com;")
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html>
<html>
<head>
    <title>Orkestra API Documentation</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <style>
        body { margin: 0; padding: 0; }
    </style>
</head>
<body>
    <script id="api-reference" data-url="/openapi.json"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`))
	})

	// OpenAPI JSON endpoint
	router.Get("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		spec := publicAPI.OpenAPI()
		if err := json.NewEncoder(w).Encode(spec); err != nil {
			http.Error(w, "Failed to generate OpenAPI spec", http.StatusInternalServerError)
			return
		}
	})

	srv := &http.Server{
		Addr:           fmt.Sprintf(":%s", cfg.Server.Port),
		Handler:        router,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   5 * time.Minute, // RAG queries with local LLM can take 2-3 minutes
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB max header size
	}

	// Start billing polling job if enabled and polling is configured
	if billingEnabled && billingPollingJob != nil && cfg.Billing.PollingEnabled {
		pollingCtx, pollingCancel := context.WithCancel(context.Background())
		defer pollingCancel()

		go func() {
			logger.Info("Starting SDI notification polling job",
				slog.Duration("interval", cfg.Billing.PollingInterval),
			)
			billingPollingJob.Start(pollingCtx)
		}()
	} else if billingEnabled && !cfg.Billing.PollingEnabled {
		logger.Info("SDI polling job disabled - use manual sync endpoints instead (POST /v1/billing/sync)")
	}

	// Auto-configure API callbacks on OpenAPI.it for webhook reception
	if billingEnabled && cfg.Billing.WebhookURL != "" && openAPIClient != nil {
		go func() {
			configCtx, configCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer configCancel()

			authHeader := "Bearer " + cfg.Billing.WebhookSecret
			err := openAPIClient.ConfigureAPICallbacks(configCtx, billingSvc.APICallbackConfig{
				FiscalID: cfg.Billing.OpenAPIFiscalID,
				Callbacks: []billingSvc.CallbackConfig{
					{Event: "supplier-invoice", URL: cfg.Billing.WebhookURL, AuthHeader: authHeader},
					{Event: "customer-notification", URL: cfg.Billing.WebhookURL, AuthHeader: authHeader},
					{Event: "legal-storage-receipt", URL: cfg.Billing.WebhookURL, AuthHeader: authHeader},
				},
			})
			if err != nil {
				logger.Error("Failed to configure API callbacks on OpenAPI.it",
					slog.String("error", err.Error()),
					slog.String("webhookURL", cfg.Billing.WebhookURL),
				)
			} else {
				logger.Info("API callbacks configured on OpenAPI.it",
					slog.String("webhookURL", cfg.Billing.WebhookURL),
					slog.String("fiscalID", cfg.Billing.OpenAPIFiscalID),
				)
			}
		}()
	}

	// Log development mode warning
	if !cfg.IsProduction() {
		utils.PrintDevelopmentWarning(cfg.Server.Environment)
	}

	go func() {
		logger.Info("Starting server",
			slog.String("port", cfg.Server.Port),
			slog.String("environment", cfg.Server.Environment),
		)

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Failed to start server", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Stop billing polling job
	if billingEnabled && billingPollingJob != nil {
		logger.Info("Stopping SDI notification polling job")
		billingPollingJob.Stop()
	}

	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Failed to shutdown server gracefully", slog.String("error", err.Error()))
	}

	if err := database.DisconnectMongo(ctx, db); err != nil {
		logger.Error("Failed to disconnect from MongoDB", slog.String("error", err.Error()))
	}

	if err := database.DisconnectRedis(redisClient); err != nil {
		logger.Error("Failed to disconnect from Redis", slog.String("error", err.Error()))
	}

	logger.Info("Server stopped")
}



