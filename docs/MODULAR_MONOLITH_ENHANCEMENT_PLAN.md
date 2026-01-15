# Modular Monolith Enhancement Plan
**ORKESTRA System - Microservices-Ready Architecture**

---

## Executive Summary

This plan outlines the incremental enhancement of ORKESTRA's backend from a well-structured monolith to a **microservices-ready modular monolith**. The goal is to maintain extraction optionality without introducing unnecessary complexity.

**Timeline**: 8-12 weeks (can be executed in parallel with feature development)
**Risk Level**: Low (all changes are additive, non-breaking)
**Deployment Impact**: Zero downtime (changes deployed incrementally)

---

## Current State Assessment

### ✅ Already Implemented
- Service interfaces exist (`UserService`, `AuthService`, etc.)
- Repository pattern with clean data access
- Dependency injection in `main.go`
- Module-specific packages (`/internal/auth`, `/internal/user`, etc.)
- Docker-based infrastructure (MongoDB, Redis)

### 🔄 Needs Enhancement
- Standardize service client interfaces across all modules
- Add event publishing infrastructure (Kafka)
- Implement distributed tracing (OpenTelemetry)
- Create service client factory pattern
- Add feature flags for local/remote service switching

---

## Phase 1: Service Client Interface Layer (Week 1-2)

### Objective
Create standardized client interfaces for all modules to support future local/remote switching.

### 1.1 Create Service Client Package Structure

**New Directory**: `/backend/internal/clients/`

```
backend/internal/clients/
├── user_client.go          # User service client interface
├── auth_client.go          # Auth service client interface
├── reporting_client.go     # Reporting service client interface
├── factory.go              # Client factory (local/remote decision)
└── types.go                # Shared client types
```

### 1.2 User Service Client Interface

**File**: `/backend/internal/clients/user_client.go`

```go
package clients

import (
	"context"
	"github.com/orkestra/backend/internal/user/models"
	"github.com/orkestra/backend/internal/user/services"
)

// UserServiceClient defines the interface for user operations
// This interface supports both local (direct) and remote (gRPC) implementations
type UserServiceClient interface {
	// Core operations needed by other modules
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	CreateUserFromOAuth(ctx context.Context, input *models.CreateUserInput) (*models.User, error)
	UpdateUserLastLogin(ctx context.Context, id string) error
	ValidateUserActive(ctx context.Context, id string) (bool, error)

	// OAuth operations
	AddOAuthLinkToUser(ctx context.Context, userUUID string, link models.OAuthLink) error
	GetUserOAuthLinks(ctx context.Context, userUUID string) ([]models.OAuthLink, error)
}

// LocalUserServiceClient wraps the actual user service for local calls
type LocalUserServiceClient struct {
	userService services.UserService
}

func NewLocalUserServiceClient(userService services.UserService) UserServiceClient {
	return &LocalUserServiceClient{
		userService: userService,
	}
}

// Implement interface methods by delegating to userService
func (c *LocalUserServiceClient) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	return c.userService.GetUserByID(ctx, id)
}

func (c *LocalUserServiceClient) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	user, err := c.userService.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	// Convert UserManagementResponse to User model
	return c.userService.GetUserByID(ctx, user.UUID)
}

// ... implement remaining methods
```

### 1.3 Reporting Service Client Interface

**File**: `/backend/internal/clients/reporting_client.go`

```go
package clients

import (
	"context"
	"github.com/orkestra/backend/internal/reporting/models"
	"github.com/orkestra/backend/internal/reporting/services"
)

// ReportingServiceClient defines the interface for reporting operations
type ReportingServiceClient interface {
	GetDeadlines(ctx context.Context, filters *models.DeadlineFilters) (*models.DeadlineReport, error)
}

type LocalReportingServiceClient struct {
	deadlineService services.DeadlineService
}

func NewLocalReportingServiceClient(deadlineService services.DeadlineService) ReportingServiceClient {
	return &LocalReportingServiceClient{
		deadlineService: deadlineService,
	}
}

// Implement interface methods...
```

### 1.4 Client Factory with Service Discovery

**File**: `/backend/internal/clients/factory.go`

```go
package clients

import (
	"fmt"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/user/services"
	reportingServices "github.com/orkestra/backend/internal/reporting/services"
)

// ServiceMode defines how services communicate
type ServiceMode string

const (
	ServiceModeLocal  ServiceMode = "local"  // Direct function calls (current)
	ServiceModeRemote ServiceMode = "remote" // gRPC calls (future)
)

// ClientFactory creates service clients based on configuration
type ClientFactory struct {
	config *config.Config

	// Local service references (populated in local mode)
	userService      services.UserService
	reportingService reportingServices.DeadlineService
}

func NewClientFactory(cfg *config.Config) *ClientFactory {
	return &ClientFactory{
		config: cfg,
	}
}

// SetLocalServices configures the factory with local service instances
func (f *ClientFactory) SetLocalServices(
	userService services.UserService,
	reportingService reportingServices.DeadlineService,
) {
	f.userService = userService
	f.reportingService = reportingService
}

// CreateUserClient creates a user service client
func (f *ClientFactory) CreateUserClient() (UserServiceClient, error) {
	mode := f.getServiceMode("user")

	switch mode {
	case ServiceModeLocal:
		if f.userService == nil {
			return nil, fmt.Errorf("user service not initialized")
		}
		return NewLocalUserServiceClient(f.userService), nil

	case ServiceModeRemote:
		// Future: Return gRPC client
		// return NewRemoteUserServiceClient(f.config.Services.User.Address)
		return nil, fmt.Errorf("remote mode not yet implemented")

	default:
		return nil, fmt.Errorf("unknown service mode: %s", mode)
	}
}

// CreateReportingClient creates a reporting service client
func (f *ClientFactory) CreateReportingClient() (ReportingServiceClient, error) {
	mode := f.getServiceMode("reporting")

	switch mode {
	case ServiceModeLocal:
		if f.reportingService == nil {
			return nil, fmt.Errorf("reporting service not initialized")
		}
		return NewLocalReportingServiceClient(f.reportingService), nil

	case ServiceModeRemote:
		// Future: Return gRPC client
		return nil, fmt.Errorf("remote mode not yet implemented")

	default:
		return nil, fmt.Errorf("unknown service mode: %s", mode)
	}
}

// getServiceMode determines if a service should use local or remote communication
func (f *ClientFactory) getServiceMode(serviceName string) ServiceMode {
	// Check environment variable first
	// Example: USER_SERVICE_MODE=remote
	// envKey := fmt.Sprintf("%s_SERVICE_MODE", strings.ToUpper(serviceName))
	// if mode := os.Getenv(envKey); mode != "" {
	//     return ServiceMode(mode)
	// }

	// For now, always use local mode
	// This will be configurable via feature flags later
	return ServiceModeLocal
}
```

### 1.5 Update Configuration

**File**: `/backend/internal/shared/config/config.go` (add section)

```go
// Services configuration for service discovery (future use)
type ServicesConfig struct {
	User struct {
		Mode    string `env:"USER_SERVICE_MODE" envDefault:"local"`
		Address string `env:"USER_SERVICE_ADDRESS" envDefault:"user-service:3002"`
	}
	Reporting struct {
		Mode    string `env:"REPORTING_SERVICE_MODE" envDefault:"local"`
		Address string `env:"REPORTING_SERVICE_ADDRESS" envDefault:"reporting-service:3004"`
	}
}

type Config struct {
	// ... existing fields ...
	Services ServicesConfig
}
```

### 1.6 Refactor main.go to Use Client Factory

**File**: `/backend/cmd/server/main.go` (modified sections)

```go
import (
	// ... existing imports ...
	"github.com/orkestra/backend/internal/clients"
)

func main() {
	// ... existing initialization ...

	// Initialize services (existing code)
	userRepo := userRepository.NewUserRepository(db)
	userService := userServices.NewUserService(userRepo, oauthProviderRepo)

	reportingRepo := reportingRepository.NewDeadlineRepository(db)
	reportingService := reportingServices.NewDeadlineService(reportingRepo)

	// NEW: Create client factory
	clientFactory := clients.NewClientFactory(cfg)
	clientFactory.SetLocalServices(userService, reportingService)

	// NEW: Create service clients (currently local, future remote-capable)
	userClient, err := clientFactory.CreateUserClient()
	if err != nil {
		log.Fatalf("Failed to create user client: %v", err)
	}

	// Auth service now uses client interface instead of direct service
	authService, err := services.NewAuthService(&services.AuthConfig{
		AuthRepo:          authRepo,
		UserClient:        userClient, // Changed from UserService
		OAuthProviderRepo: oauthProviderRepo,
		RefreshTokenRepo:  refreshTokenRepo,
		AuthSessionRepo:   authSessionRepo,
		JWTService:        jwtService,
	})

	// ... rest of initialization ...
}
```

**Deliverables**:
- [ ] Create `/backend/internal/clients/` package
- [ ] Implement client interfaces for all 3 modules
- [ ] Update `main.go` to use client factory (local mode only)
- [ ] Update auth service to accept client interface
- [ ] Run tests to ensure no regression
- [ ] Update documentation

**Success Criteria**: Application runs identically, but now supports future remote clients.

---

## Phase 2: Event Publishing Infrastructure (Week 3-4)

### Objective
Add Kafka infrastructure and event publishing capability without requiring consumption.

### 2.1 Add Kafka to Docker Infrastructure

**File**: `/docker/docker-compose.infra.yml` (append)

```yaml
  zookeeper:
    image: confluentinc/cp-zookeeper:7.6.0
    container_name: orkestra-zookeeper
    restart: unless-stopped
    environment:
      ZOOKEEPER_CLIENT_PORT: 2181
      ZOOKEEPER_TICK_TIME: 2000
    networks:
      - orkestra-network
    healthcheck:
      test: ["CMD", "nc", "-z", "localhost", "2181"]
      interval: 10s
      timeout: 5s
      retries: 5

  kafka:
    image: confluentinc/cp-kafka:7.6.0
    container_name: orkestra-kafka
    restart: unless-stopped
    depends_on:
      zookeeper:
        condition: service_healthy
    ports:
      - "${KAFKA_PORT:-9092}:9092"
    environment:
      KAFKA_BROKER_ID: 1
      KAFKA_ZOOKEEPER_CONNECT: 'zookeeper:2181'
      KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: PLAINTEXT:PLAINTEXT,PLAINTEXT_HOST:PLAINTEXT
      KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://kafka:29092,PLAINTEXT_HOST://localhost:9092
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
      KAFKA_TRANSACTION_STATE_LOG_MIN_ISR: 1
      KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: 1
      KAFKA_AUTO_CREATE_TOPICS_ENABLE: 'true'
    networks:
      - orkestra-network
    healthcheck:
      test: ["CMD", "kafka-broker-api-versions", "--bootstrap-server", "localhost:9092"]
      interval: 10s
      timeout: 10s
      retries: 5

  kafka-ui:
    image: provectuslabs/kafka-ui:latest
    container_name: orkestra-kafka-ui
    restart: unless-stopped
    depends_on:
      - kafka
    ports:
      - "${KAFKA_UI_PORT:-8082}:8080"
    environment:
      KAFKA_CLUSTERS_0_NAME: local
      KAFKA_CLUSTERS_0_BOOTSTRAPSERVERS: kafka:29092
      KAFKA_CLUSTERS_0_ZOOKEEPER: zookeeper:2181
    networks:
      - orkestra-network
```

**Update `.env`**:
```bash
# Kafka Configuration
KAFKA_PORT=9092
KAFKA_UI_PORT=8082
KAFKA_BROKERS=localhost:9092
```

### 2.2 Create Event Publishing Infrastructure

**New Directory**: `/backend/internal/events/`

```
backend/internal/events/
├── publisher.go         # Kafka event publisher
├── subscriber.go        # Event consumer (future)
├── events.go           # Event definitions
├── schemas.go          # Event schemas
└── topics.go           # Topic constants
```

**File**: `/backend/internal/events/topics.go`

```go
package events

// Topic constants for Kafka
const (
	// User domain events
	TopicUserCreated      = "user.created"
	TopicUserUpdated      = "user.updated"
	TopicUserDeleted      = "user.deleted"
	TopicUserOAuthLinked  = "user.oauth.linked"

	// Auth domain events
	TopicUserLoggedIn       = "auth.user.logged_in"
	TopicSessionCreated     = "auth.session.created"
	TopicSecurityEvent      = "auth.security.event"

	// Document domain events
	TopicDocumentExpired    = "document.expired"
	TopicDocumentExpiringSoon = "document.expiring_soon"
)
```

**File**: `/backend/internal/events/events.go`

```go
package events

import (
	"encoding/json"
	"time"
	"github.com/google/uuid"
)

// BaseEvent contains common fields for all events
type BaseEvent struct {
	EventID   string    `json:"event_id"`
	EventType string    `json:"event_type"`
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
	Source    string    `json:"source"` // e.g., "user-service", "monolith"
}

// UserCreatedEvent is published when a new user is created
type UserCreatedEvent struct {
	BaseEvent
	UserID       string            `json:"user_id"`
	Email        string            `json:"email"`
	Role         string            `json:"role"`
	OAuthProvider string           `json:"oauth_provider,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// UserUpdatedEvent is published when user data changes
type UserUpdatedEvent struct {
	BaseEvent
	UserID       string            `json:"user_id"`
	UpdatedFields []string         `json:"updated_fields"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// DocumentExpiringEvent is published for expiring documents
type DocumentExpiringEvent struct {
	BaseEvent
	EntityType   string    `json:"entity_type"` // "user"
	EntityID     string    `json:"entity_id"`
	DocumentType string    `json:"document_type"`
	ExpiryDate   time.Time `json:"expiry_date"`
	DaysUntilExpiry int    `json:"days_until_expiry"`
}

// NewBaseEvent creates a base event with common fields
func NewBaseEvent(eventType, source string) BaseEvent {
	return BaseEvent{
		EventID:   uuid.New().String(),
		EventType: eventType,
		Timestamp: time.Now().UTC(),
		Version:   "1.0",
		Source:    source,
	}
}

// ToJSON serializes event to JSON
func ToJSON(event interface{}) ([]byte, error) {
	return json.Marshal(event)
}
```

**File**: `/backend/internal/events/publisher.go`

```go
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/segmentio/kafka-go"
)

// Publisher publishes events to Kafka
type Publisher interface {
	Publish(ctx context.Context, topic string, event interface{}) error
	PublishBatch(ctx context.Context, topic string, events []interface{}) error
	Close() error
}

// KafkaPublisher implements Publisher using Kafka
type KafkaPublisher struct {
	writer *kafka.Writer
	logger *slog.Logger
}

// NewKafkaPublisher creates a new Kafka event publisher
func NewKafkaPublisher(brokers []string, logger *slog.Logger) Publisher {
	return &KafkaPublisher{
		writer: &kafka.Writer{
			Addr:                   kafka.TCP(brokers...),
			Balancer:               &kafka.LeastBytes{},
			AllowAutoTopicCreation: true,
			Async:                  false, // Synchronous for reliability
		},
		logger: logger,
	}
}

// Publish publishes a single event to Kafka
func (p *KafkaPublisher) Publish(ctx context.Context, topic string, event interface{}) error {
	// Serialize event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		p.logger.Error("Failed to serialize event",
			slog.String("topic", topic),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// Create Kafka message
	message := kafka.Message{
		Topic: topic,
		Value: data,
	}

	// Publish to Kafka
	err = p.writer.WriteMessages(ctx, message)
	if err != nil {
		p.logger.Error("Failed to publish event",
			slog.String("topic", topic),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("failed to publish event: %w", err)
	}

	p.logger.Info("Event published successfully",
		slog.String("topic", topic),
	)

	return nil
}

// PublishBatch publishes multiple events in a single operation
func (p *KafkaPublisher) PublishBatch(ctx context.Context, topic string, events []interface{}) error {
	messages := make([]kafka.Message, len(events))

	for i, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("failed to serialize event %d: %w", i, err)
		}

		messages[i] = kafka.Message{
			Topic: topic,
			Value: data,
		}
	}

	err := p.writer.WriteMessages(ctx, messages...)
	if err != nil {
		p.logger.Error("Failed to publish batch",
			slog.String("topic", topic),
			slog.Int("count", len(events)),
			slog.String("error", err.Error()),
		)
		return fmt.Errorf("failed to publish batch: %w", err)
	}

	p.logger.Info("Batch published successfully",
		slog.String("topic", topic),
		slog.Int("count", len(events)),
	)

	return nil
}

// Close closes the Kafka writer
func (p *KafkaPublisher) Close() error {
	return p.writer.Close()
}

// NoOpPublisher is a publisher that does nothing (for testing/feature flag)
type NoOpPublisher struct{}

func NewNoOpPublisher() Publisher {
	return &NoOpPublisher{}
}

func (p *NoOpPublisher) Publish(ctx context.Context, topic string, event interface{}) error {
	return nil // Do nothing
}

func (p *NoOpPublisher) PublishBatch(ctx context.Context, topic string, events []interface{}) error {
	return nil // Do nothing
}

func (p *NoOpPublisher) Close() error {
	return nil
}
```

### 2.3 Add Event Publishing to Services

**Example**: `/backend/internal/user/services/user_service.go` (modify CreateUser)

```go
type userService struct {
	userRepo          repository.UserRepository
	oauthProviderRepo authRepository.OAuthProviderRepository
	eventPublisher    events.Publisher // NEW
}

func NewUserService(
	userRepo repository.UserRepository,
	oauthProviderRepo authRepository.OAuthProviderRepository,
	eventPublisher events.Publisher, // NEW
) UserService {
	return &userService{
		userRepo:          userRepo,
		oauthProviderRepo: oauthProviderRepo,
		eventPublisher:    eventPublisher, // NEW
	}
}

func (s *userService) CreateUser(ctx context.Context, input *models.CreateUserInput) (*models.UserManagementResponse, error) {
	// ... existing creation logic ...

	user, err := s.userRepo.Create(ctx, newUser)
	if err != nil {
		return nil, err
	}

	// NEW: Publish event (optional - doesn't fail if publisher unavailable)
	if s.eventPublisher != nil {
		event := events.UserCreatedEvent{
			BaseEvent: events.NewBaseEvent(events.TopicUserCreated, "monolith"),
			UserID:    user.UUID,
			Email:     user.Email,
			Role:      user.Role,
		}

		// Fire and forget (or log error without failing the request)
		go func() {
			if err := s.eventPublisher.Publish(context.Background(), events.TopicUserCreated, event); err != nil {
				// Log error but don't fail user creation
				slog.Error("Failed to publish user created event", slog.String("error", err.Error()))
			}
		}()
	}

	return toUserManagementResponse(user), nil
}
```

### 2.4 Update main.go with Event Publisher

```go
func main() {
	// ... existing initialization ...

	// NEW: Initialize event publisher (optional - controlled by feature flag)
	var eventPublisher events.Publisher
	if cfg.Features.EventPublishing {
		eventPublisher = events.NewKafkaPublisher(
			[]string{cfg.Kafka.Brokers},
			logger,
		)
		defer eventPublisher.Close()
		logger.Info("Event publishing enabled")
	} else {
		eventPublisher = events.NewNoOpPublisher()
		logger.Info("Event publishing disabled (using no-op publisher)")
	}

	// Pass event publisher to services
	userService := userServices.NewUserService(userRepo, oauthProviderRepo, eventPublisher)

	// ... rest of initialization ...
}
```

### 2.5 Add Feature Flag Configuration

**Update**: `/backend/internal/shared/config/config.go`

```go
type FeaturesConfig struct {
	EventPublishing bool `env:"FEATURE_EVENT_PUBLISHING" envDefault:"false"`
}

type KafkaConfig struct {
	Brokers string `env:"KAFKA_BROKERS" envDefault:"localhost:9092"`
}

type Config struct {
	// ... existing fields ...
	Features FeaturesConfig
	Kafka    KafkaConfig
}
```

**Deliverables**:
- [ ] Add Kafka to `docker-compose.infra.yml`
- [ ] Create `/backend/internal/events/` package
- [ ] Implement event publisher with no-op fallback
- [ ] Add event publishing to user service (example)
- [ ] Add feature flag for event publishing
- [ ] Test with feature flag off (default behavior)
- [ ] Test with feature flag on (events published to Kafka)

**Success Criteria**: Application works with events disabled; events published when enabled without affecting performance.

---

## Phase 3: OpenTelemetry Distributed Tracing (Week 5-6)

### Objective
Add distributed tracing infrastructure to support future microservices debugging.

### 3.1 Add Jaeger to Docker Infrastructure

**File**: `/docker/docker-compose.infra.yml` (append)

```yaml
  jaeger:
    image: jaegertracing/all-in-one:1.52
    container_name: orkestra-jaeger
    restart: unless-stopped
    ports:
      - "${JAEGER_UI_PORT:-16686}:16686"      # UI
      - "${JAEGER_OTLP_PORT:-4317}:4317"      # OTLP gRPC
      - "${JAEGER_OTLP_HTTP_PORT:-4318}:4318" # OTLP HTTP
    environment:
      COLLECTOR_OTLP_ENABLED: 'true'
    networks:
      - orkestra-network
```

**Update `.env`**:
```bash
# Tracing Configuration
JAEGER_UI_PORT=16686
JAEGER_OTLP_PORT=4317
JAEGER_ENDPOINT=http://localhost:4318/v1/traces
OTEL_SERVICE_NAME=orkestra-backend
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
```

### 3.2 Create Tracing Package

**New Directory**: `/backend/internal/observability/`

```
backend/internal/observability/
├── tracing.go          # OpenTelemetry setup
├── middleware.go       # HTTP tracing middleware
└── context.go          # Context helpers
```

**File**: `/backend/internal/observability/tracing.go`

```go
package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

// InitTracer initializes OpenTelemetry tracing
func InitTracer(serviceName, endpoint string) (trace.TracerProvider, error) {
	// Create OTLP exporter
	exporter, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(), // Use TLS in production
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource with service information
	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	return tp, nil
}

// Tracer returns a tracer for the given name
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
```

**File**: `/backend/internal/observability/middleware.go`

```go
package observability

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware adds OpenTelemetry tracing to HTTP requests
func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract trace context from headers
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		// Start span
		tracer := otel.Tracer("http-server")
		ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path,
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.url", r.URL.String()),
				attribute.String("http.scheme", r.URL.Scheme),
				attribute.String("http.host", r.Host),
			),
		)
		defer span.End()

		// Call next handler with traced context
		next.ServeHTTP(w, r.WithContext(ctx))

		// Add response status to span
		span.SetAttributes(attribute.Int("http.status_code", 200)) // Could capture from response writer
	})
}
```

### 3.3 Add Tracing to Services

**Example**: Add tracing to user service methods

```go
import "go.opentelemetry.io/otel"

func (s *userService) GetUser(ctx context.Context, id string) (*models.UserManagementResponse, error) {
	// Start span
	tracer := otel.Tracer("user-service")
	ctx, span := tracer.Start(ctx, "GetUser")
	defer span.End()

	span.SetAttributes(attribute.String("user.id", id))

	// Existing logic...
	user, err := s.userRepo.GetByUUID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return toUserManagementResponse(user), nil
}
```

### 3.4 Update main.go with Tracing

```go
import "github.com/orkestra/backend/internal/observability"

func main() {
	// ... existing initialization ...

	// NEW: Initialize OpenTelemetry (if enabled)
	if cfg.Observability.TracingEnabled {
		tp, err := observability.InitTracer(
			cfg.Observability.ServiceName,
			cfg.Observability.OTLPEndpoint,
		)
		if err != nil {
			log.Fatalf("Failed to initialize tracer: %v", err)
		}
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				logger.Error("Error shutting down tracer provider", slog.String("error", err.Error()))
			}
		}()
		logger.Info("Distributed tracing enabled")
	}

	// ... router setup ...

	// Add tracing middleware
	router.Use(observability.TracingMiddleware)

	// ... rest of initialization ...
}
```

### 3.5 Update Configuration

```go
type ObservabilityConfig struct {
	TracingEnabled bool   `env:"OTEL_TRACING_ENABLED" envDefault:"false"`
	ServiceName    string `env:"OTEL_SERVICE_NAME" envDefault:"orkestra-backend"`
	OTLPEndpoint   string `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:"localhost:4318"`
}

type Config struct {
	// ... existing fields ...
	Observability ObservabilityConfig
}
```

**Deliverables**:
- [ ] Add Jaeger to `docker-compose.infra.yml`
- [ ] Create `/backend/internal/observability/` package
- [ ] Implement tracing middleware
- [ ] Add tracing to one service (example)
- [ ] Update `main.go` with tracer initialization
- [ ] Test with tracing disabled (default)
- [ ] Test with tracing enabled (view traces in Jaeger UI)

**Success Criteria**: Traces visible in Jaeger UI when enabled; no performance impact when disabled.

---

## Phase 4: Documentation & Testing (Week 7-8)

### 4.1 Create Proto Definitions (Future gRPC)

**New Directory**: `/backend/proto/`

```
backend/proto/
├── user/v1/
│   └── user.proto
├── reporting/v1/
│   └── reporting.proto
└── README.md
```

**Example**: `/backend/proto/user/v1/user.proto`

```protobuf
syntax = "proto3";

package user.v1;

option go_package = "github.com/orkestra/backend/proto/user/v1;userv1";

// UserService defines user operations
service UserService {
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
  rpc CreateUser(CreateUserRequest) returns (CreateUserResponse);
  rpc UpdateUser(UpdateUserRequest) returns (UpdateUserResponse);
}

message GetUserRequest {
  string user_id = 1;
}

message GetUserResponse {
  User user = 1;
}

message User {
  string id = 1;
  string email = 2;
  string name = 3;
  string role = 4;
}

// ... more definitions
```

### 4.2 Update Backend CLAUDE.md

Add section on microservices readiness status and migration preparedness.

### 4.3 Create Migration Runbook

**New File**: `/docs/MICROSERVICES_MIGRATION_RUNBOOK.md`

Document step-by-step process for extracting first service (reporting service).

### 4.4 Integration Tests

Create tests that verify:
- Client factory creates correct client types
- Event publishing is optional and non-blocking
- Tracing doesn't affect application behavior
- Services work with and without features enabled

**Deliverables**:
- [ ] Create proto definitions for all services
- [ ] Update all documentation
- [ ] Create migration runbook
- [ ] Write integration tests
- [ ] Load test with features enabled/disabled

**Success Criteria**: System is fully documented and tested; team understands migration path.

---

## Rollout Strategy

### Week 1-2: Phase 1
- ✅ Create client interfaces
- ✅ Update main.go
- ✅ Deploy to staging
- ✅ Run full test suite
- ✅ Deploy to production (zero risk - identical behavior)

### Week 3-4: Phase 2
- ✅ Add Kafka to infrastructure
- ✅ Deploy Kafka (standalone, not used yet)
- ✅ Add event publishing with feature flag OFF
- ✅ Deploy to staging
- ✅ Enable feature flag in staging, verify events
- ✅ Deploy to production with feature flag OFF
- ✅ (Optional) Enable in production after observation period

### Week 5-6: Phase 3
- ✅ Add Jaeger to infrastructure
- ✅ Add tracing code with feature flag OFF
- ✅ Deploy to staging
- ✅ Enable tracing in staging, verify traces
- ✅ Deploy to production with feature flag OFF
- ✅ (Optional) Enable in production for debugging

### Week 7-8: Phase 4
- ✅ Documentation
- ✅ Testing
- ✅ Team training

---

## Risk Mitigation

### Technical Risks

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| Performance degradation | Low | High | Feature flags allow instant rollback |
| Kafka availability issues | Medium | Medium | NoOp publisher fallback |
| Tracing overhead | Low | Low | Disabled by default, sampling in production |
| Interface changes break code | Low | Medium | Interfaces match existing services exactly |

### Operational Risks

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| Deployment complexity | Low | Low | All changes are additive |
| Team knowledge gap | Medium | Medium | Documentation + training sessions |
| Infrastructure costs | Low | Medium | Kafka/Jaeger minimal resources when not used |

---

## Success Metrics

### Phase 1 (Client Interfaces)
- ✅ All services use client factory
- ✅ Zero performance regression
- ✅ 100% test coverage maintained

### Phase 2 (Events)
- ✅ Events published to Kafka (when enabled)
- ✅ <1ms overhead per event publish (async)
- ✅ No failures when Kafka unavailable

### Phase 3 (Tracing)
- ✅ Traces visible in Jaeger
- ✅ <5ms overhead per request with tracing
- ✅ Full request chain visibility

### Phase 4 (Documentation)
- ✅ All modules documented
- ✅ Migration runbook complete
- ✅ Team trained on new patterns

---

## Future State Readiness

After completing this plan, extracting a service requires:

1. **Copy module code** to new repository (1 day)
2. **Add gRPC server** wrapper (1-2 days)
3. **Implement remote client** in client factory (1 day)
4. **Update config** to point to remote service (1 hour)
5. **Deploy and test** (2-3 days)
6. **Gradual traffic shift** (1 week)

**Total: 1-2 weeks per service** (vs 6-12 months from traditional monolith)

---

## Conclusion

This plan transforms ORKESTRA from a well-structured monolith to a **microservices-ready modular monolith** in 8-12 weeks with:

- ✅ **Zero breaking changes** - All enhancements are additive
- ✅ **Low risk** - Feature flags enable instant rollback
- ✅ **Optional complexity** - Features only add overhead when enabled
- ✅ **Future optionality** - Can extract services in 1-2 weeks when needed
- ✅ **No premature optimization** - Maintain monolith benefits until microservices are justified

**Recommendation**: Execute this plan incrementally alongside normal feature development. Each phase is independently valuable even if you never migrate to microservices.
