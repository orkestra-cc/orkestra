# Module: Backend - Orkestra Modular Monolithic Go Server

_Path: `/backend`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

<!-- Navigation -->

[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [🚀 Quick Start](../CLAUDE.md#quick-start)

<!-- /Navigation -->

## Module Purpose

The backend serves as the **monolithic Go server** handling all API operations, real-time communications, and business logic for the Orkestra system.

- **Primary Role**: Unified API server for all client applications
- **System Integration**: Central hub connecting frontend, mobile, and external services
- **Architecture**: Single Go application with modular internal structure

**CRITICAL**: This is a monolithic application, not microservices. All modules run within a single Go application on port 3000, utilizing goroutines for concurrent processing.

## Dependencies

### Imports

- **[`/shared/`](../shared/CLAUDE.md)** - Data models, types, and validation rules
- **[`/docker/`](../docker/CLAUDE.md)** - Infrastructure services (MongoDB, Redis)

### Importers

- **[`/frontend/`](../frontend/CLAUDE.md)** - Consumes REST APIs and WebSocket events
- **[`/mobile/`](../mobile/CLAUDE.md)** - Consumes mobile-optimized APIs

## Module-Specific AI Guidelines

### Development Workflow - CRITICAL

- **🚫 NEVER manually start the server** - The development server runs automatically in Docker with AIR (hot reload)
- **🐳 Always use Docker Compose** - The backend runs in a containerized environment with live reload
- **📋 Check logs via Docker Compose** - Use `docker compose logs backend` to view server logs
- **🔄 Automatic rebuilds** - AIR detects code changes and automatically rebuilds/restarts the server
- **🚪 No manual server management** - The server is managed entirely through Docker Compose

### Code Standards

- **Code Organization**: Maintain strict module separation within the monolithic structure
- **API Standards**: Always generate new OpenAPI 3.1 specs when DTOs or routes change
- **Performance Focus**: Optimize for 1,000+ concurrent users with <50ms response times
- **Security First**: Validate all inputs, sanitize outputs, follow RBAC patterns
- **Real-time Support**: Preserve WebSocket functionality for live updates
- **Testing**: Maintain minimum 80% coverage with comprehensive integration tests
- **Documentation**: Auto-generate API docs via Huma v2 framework

## Technology Stack

- **Language**: Go 1.25.1+
- **Framework**: Huma v2 (OpenAPI-first)
- **Database**: MongoDB 8.0+ (primary datastore)
- **Cache**: Redis 8.2 (sessions, hot data)
- **Message Queue**: RabbitMQ 3 (async tasks)
- **Object Storage**: MinIO (S3-compatible)
- **Real-time**: Native WebSocket implementation
- **Monitoring**: OpenTelemetry + SigNoz
- **Development**: AIR (hot reload) + Docker Compose for automated server management

## Project Structure

```
backend/
├── cmd/
│   └── server/
│       └── main.go              # Application entry point
├── internal/
│   ├── auth/                    # Authentication module
│   │   ├── handler.go           # HTTP handlers
│   │   ├── service.go           # Business logic
│   │   ├── repository.go        # Data access
│   │   └── jwt.go               # JWT utilities
│   ├── user/                    # User management module
│   │   ├── handler.go
│   │   ├── service.go
│   │   ├── repository.go
│   │   └── rbac.go              # Role-based access control
│   ├── operator/                # Operator/driver module
│   │   ├── handler.go
│   │   ├── service.go
│   │   └── repository.go
│   ├── tracking/                # Real-time tracking module
│   │   ├── handler.go
│   │   ├── service.go
│   │   ├── repository.go
│   │   └── websocket.go         # WebSocket handler
│   ├── task/                    # Task management module
│   │   ├── handler.go
│   │   ├── service.go
│   │   └── repository.go
│   ├── reporting/               # Analytics & reporting module
│   │   ├── handler.go
│   │   ├── service.go
│   │   ├── repository.go
│   │   └── aggregations.go      # MongoDB aggregations
│   ├── shared/                  # Shared components
│   │   ├── database/            # Database connections
│   │   ├── middleware/          # HTTP middleware
│   │   ├── errors/              # Error handling
│   │   ├── validators/          # Input validation
│   │   └── utils/               # Utility functions
│   └── config/                  # Configuration management
├── pkg/                         # Exportable packages
│   ├── models/                  # Data models
│   ├── types/                   # Shared types
│   └── constants/               # Application constants
├── migrations/                  # Database migrations
├── tests/                       # Integration tests
├── go.mod
├── go.sum
├── Dockerfile
├── Makefile
└── .env.example

```

## Module Architecture

### Core Principles

1. **Single Process**: All modules run in one Go application
2. **Goroutine Concurrency**: Each module uses goroutines for parallel processing
3. **Interface-Based**: Modules communicate through interfaces
4. **Dependency Injection**: Clean dependency management
5. **Repository Pattern**: Separation of data access logic
6. **API Standards**: Automatic OpenAPI 3.1 generation via Huma v2

## Performance Requirements

### Targets

- **Concurrent Users**: 1,000+
- **API Response Time**: < 50ms (p99)
- **WebSocket Connections**: 1,000 simultaneous
- **Tracking Updates**: 5-second intervals
- **Database Queries**: < 30ms (p95)

### Optimization Techniques

1. **Connection pooling** for all external services
2. **Goroutine worker pools** for concurrent processing
3. **Redis caching** for frequently accessed data
4. **MongoDB indexes** on all query fields
5. **Batch processing** for bulk operations
6. **Response pagination** for list endpoints

## Security Checklist

- [ ] JWT authentication implemented
- [ ] Refresh token rotation
- [ ] RBAC with role hierarchy
- [ ] Input validation on all endpoints
- [ ] SQL/NoSQL injection prevention
- [ ] XSS protection
- [ ] CSRF tokens for state-changing operations
- [ ] Sensitive data encryption
- [ ] Audit logging for critical operations
- [ ] Dependencies regularly updated
- [ ] Secrets management (never in code)

### API Documentation & Health Checks

- **`/docs`** - Interactive API documentation (Scalar interface)
- **`/openapi.json`** - OpenAPI 3.1 specification (auto-generated by Huma v2)
- `/health` - Overall health status
- `/ready` - Readiness probe
- `/metrics` - Prometheus metrics

**IMPORTANT**: OpenAPI specs are automatically generated from code. When adding/modifying endpoints:

1. Use Huma v2 `huma.Register()` for auto-documentation
2. Specs update automatically on server restart
3. Visit `/docs` for interactive testing and documentation

## Development Environment

### AIR Hot Reload System

The backend runs in Docker with **AIR (Live Reload)** which provides:

- **Automatic detection** of Go file changes (`.go`, `.tpl`, `.tmpl`, `.html`)
- **Instant rebuilds** when code is modified
- **Zero-downtime restarts** for development
- **Build error reporting** in real-time

### Docker Development Workflow

#### ✅ **DO** - Recommended Development Practices

```bash
# View server logs (ALWAYS use this to check server status)
docker compose logs backend

# Follow logs in real-time
docker compose logs -f backend

# Check all services status
docker compose ps

# Restart if needed (rarely required)
docker compose restart backend
```

#### 🚫 **DON'T** - Avoid These Practices

```bash
# NEVER manually start the server
go run ./cmd/server/main.go

# NEVER build and run locally
go build ./cmd/server/ && ./server

# NEVER try to manage the server process manually
./server &
```

### Development Configuration

- **Port**: 3000 (exposed via Docker)
- **Hot Reload**: Enabled by default in development
- **Config**: Uses `.env` file for environment variables
- **Logs**: Accessible via `docker compose logs backend`
- **Restart Policy**: Automatic on file changes

## Common Pitfalls to Avoid

1. **Don't split into microservices** - Keep it monolithic
2. **Don't skip input validation** - Validate everything
3. **Don't ignore context cancellation** - Respect context in all operations
4. **Don't forget indexes** - MongoDB performance depends on proper indexing
5. **Don't hardcode secrets** - Use environment variables
6. **Don't ignore errors** - Handle all errors appropriately
7. **Don't block goroutines** - Use channels and select statements properly
8. **Don't forget transactions** - Use them for data consistency
9. **Don't cache sensitive data** - Be careful what goes in Redis

---

### Related Guides

- [Project Overview](../CLAUDE.md) - System architecture and design principles
- [Shared Types](../shared/CLAUDE.md) - Data models and validation rules
- [Docker Infrastructure](../docker/CLAUDE.md) - Database and infrastructure setup
- [Authentication Flow](../docs/Authentication_flow.md) - OAuth 2.1 implementation details
