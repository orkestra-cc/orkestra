# ORKESTRA Management System

<!-- Navigation -->

[☰ Module Map](#module-map) | [🚀 Quick Start](#quick-start) | [📚 Documentation](#technical-documentation)

<!-- /Navigation -->

## Project Overview

ORKESTRA is a modern professional management system designed for professional-business management and reporting.

## System Architecture

### Architectural Approach

**ORKESTRA follows a Modular Monolith architecture** - a single deployable application with strictly defined module boundaries, designed to support optional future extraction to microservices if business requirements demand it.

**Current State**: Enhanced Modular Monolith (Microservices-Ready)
**Future Option**: Service extraction when scale/team requirements justify the complexity

### Technology Foundation

- **Backend**: Modular Monolithic Go 1.25.1+ application with Huma v2 framework
- **Frontend**: React 19 with TypeScript, modern development tools
- **Mobile**: Flutter 3.35+ cross-platform application
- **Database**: MongoDB 8.0+ with Redis 8.2 caching layer
- **Message Queue**: Kafka (event publishing capability)
- **Infrastructure**: Containerized deployment with Docker
- **Authentication**: OAuth 2.1 flows with JWT token management
- **Observability**: OpenTelemetry-ready for distributed tracing

### Core Design Principles

1. **Monorepo Architecture** - Single repository with clear module boundaries
2. **Modular Monolithic Backend** - Unified Go application with strict module isolation and interface-based communication
3. **Real-time First** - WebSocket-based live updates for critical operations
4. **Security by Default** - Comprehensive authentication, authorization, and validation
5. **Performance Oriented** - Designed for 1,000+ concurrent users with <50ms response times
6. **Test-Driven** - Minimum 80% test coverage across all modules
7. **Microservices-Ready Design** - Module boundaries and communication patterns support future service extraction if needed
8. **Event-Driven Capability** - Optional event publishing infrastructure for async communication and future event sourcing

### Architectural Evolution Path

#### Current Architecture: Enhanced Modular Monolith

The system operates as a single deployable Go application with the following characteristics:

- **Strict Module Boundaries**: Each business domain (auth, user, reporting, billing) is isolated in separate packages
- **Interface-Based Communication**: Modules interact through well-defined Go interfaces, not direct function calls
- **Independent Data Access**: Each module manages its own repository layer and data access patterns
- **Event Publishing Ready**: Infrastructure in place to publish domain events for async communication
- **Deployment Simplicity**: Single binary deployment with all benefits of monolithic architecture

#### Future Migration Option: Microservices Extraction

The architecture supports optional extraction of modules into independent microservices if business needs justify the complexity:

**Migration Triggers** (Re-evaluate when 3+ conditions are met):

- [ ] Team size exceeds 15-20 developers with frequent code conflicts
- [ ] Specific modules require drastically different scaling characteristics (10x difference)
- [ ] Deployment frequency limited by monolith coordination (>1 hour deployment cycles)
- [ ] Module-specific technology requirements emerge (different languages/frameworks needed)
- [ ] Regulatory/compliance requirements demand service-level isolation
- [ ] DevOps/SRE team capable of managing service mesh, distributed tracing, and orchestration complexity

**Migration Strategy** (When triggers are met):

1. **Extract Low-Risk Service First**: Start with reporting service (read-heavy, minimal dependencies)
2. **Gradual Traffic Shifting**: Use feature flags and canary deployments for safe rollout
3. **Strangler Fig Pattern**: New services run alongside monolith until proven stable
4. **Service-by-Service**: Extract one module at a time (1-2 weeks per service due to preparation)
5. **Preserve Interfaces**: Existing Go interfaces become gRPC/HTTP client implementations

**Current Recommendation**: **Operate as modular monolith** until migration triggers justify the 3-5x increase in operational complexity.

## Module Map

### 🏗️ **Application Modules**

- **[`/backend/`](backend/CLAUDE.md)** - Go Monolithic Server

  - Handles: Authentication, APIs, WebSockets, business logic, billing (Italian electronic invoicing)
  - Stack: Go 1.25.1+, Huma v2, MongoDB, Redis, goroutines
  - Port: 3000
  - **Billing Module**: See [billing CLAUDE.md](backend/internal/billing/CLAUDE.md) for FatturaPA/SDI integration

- **[`/frontend/`](frontend/CLAUDE.md)** - React Web Application

  - Handles: Admin dashboard, fleet management, reporting UI
  - Stack: React 19, TypeScript, Vite, Redux Toolkit, TanStack Query
  - Port: 8080 (development)

- **[`/mobile/`](mobile/CLAUDE.md)** - Flutter Mobile Application
  - Handles: Operator interfaces, real-time tracking, offline support
  - Stack: Flutter 3.35+, Dart, Provider + Riverpod, native integrations

### 🔧 **Support Modules**

- **[`/shared/`](shared/CLAUDE.md)** - Data Models & Types

  - Contains: Type definitions, validation rules, business constants
  - Used by: All application modules for consistency

- **[`/docker/`](docker/CLAUDE.md)** - Infrastructure & Containers

  - Contains: Docker configurations, environment management
  - Provides: Development and production deployment setups

- **[`/scripts/`](scripts/CLAUDE.md)** - Automation & Development Tools
  - Contains: Build scripts, deployment automation, development utilities
  - Supports: CI/CD pipelines, local development workflows

### 📚 **Documentation**

- **[`/docs/Authentication_flow.md`](docs/Authentication_flow.md)** - OAuth 2.1 Implementation Guide
  - Details: Web and mobile authentication flows, token management
  - Security: Device tracking, session management, best practices

## Quick Start

### Prerequisites

- Go 1.25.1+
- Node.js 18+
- Flutter 3.35+
- Docker & Docker Compose

### Development Setup

```bash
# Interactive deployment manager (from project root)
./deploy.sh

# Or manual docker compose commands:
cd docker
docker compose -f docker-compose.infra.yml up -d   # Infrastructure
docker compose -f docker-compose.dev.yml up -d      # Development services

# Access applications
# Frontend: http://localhost:8080
# Backend API: http://localhost:3000
# API Docs: http://localhost:3000/docs
```

**IMPORTANT**: The backend server runs automatically in Docker with AIR (hot reload). **NEVER manually start the server** - it's managed entirely through Docker Compose.

### Development Workflow

- **Deployment**: Use `./deploy.sh` from project root for interactive environment management
- **Logs**: Use `./logs.sh` from project root for interactive log viewing
- **Backend**: Runs in Docker with AIR hot reload - changes trigger automatic rebuilds
- **Frontend**: Runs in Docker with Vite hot reload - instant updates in browser
- **No Manual Server Management**: All services are containerized and auto-managed

For Docker configuration details, see [**Docker Module**](docker/CLAUDE.md).

## Technical Documentation

### API & Integration

- **OpenAPI Specification**: `/docs` endpoint with interactive documentation
- **WebSocket Events**: Real-time updates and notifications
- **Authentication**: OAuth 2.1 with device-specific security features

### Development Guidelines

- **Module-Specific Patterns**: Each module has detailed implementation guides
- **Cross-Module Communication**: Clean interfaces and dependency management
- **Testing Requirements**: Unit, integration, and end-to-end test coverage
- **Performance Standards**: Response time and scalability benchmarks

## Assistant Instructions

### 1. **Security Requirements**

- Always validate and sanitize user inputs
- Follow authentication patterns defined in [Authentication Flow](docs/Authentication_flow.md)
- Implement proper RBAC for all operations. When creating a new endpoint ask for the required permissions.
- Never expose sensitive data in logs or responses

### 2. **Architecture & Module Boundaries**

**Core Principle**: Maintain strict module isolation to support future service extraction if needed.

#### Module Communication Rules:

- **Use Interfaces**: Modules MUST communicate through Go interfaces, never direct struct method calls
- **No Cross-Module Database Access**: Each module accesses only its own repository layer
- **Dependency Injection**: All inter-module dependencies injected via constructors
- **Event Publishing**: Publish domain events for async notifications (when Kafka infrastructure available)

#### Module Extraction Readiness Requirements:

When adding new code or refactoring existing modules, ensure:

1. **Clear Interface Contracts**

   ```go
   // ✅ CORRECT: Interface-based communication
   type UserService interface {
       GetUser(ctx context.Context, userID string) (*User, error)
   }

   // ❌ WRONG: Direct struct dependency
   authService.userService.GetUser(ctx, userID)
   ```

2. **Independent Data Access**

   ```go
   // ✅ CORRECT: Module-specific repository
   userRepo := userRepository.NewUserRepository(db)

   // ❌ WRONG: Cross-module database queries
   db.Collection("users").FindOne(...) // from auth module
   ```

3. **Self-Contained Modules**

   - Each module should have: handlers → services → repositories
   - Minimal coupling between modules (aim for <3 dependencies per module)
   - Domain logic contained within module boundaries

4. **Event-Driven Communication** (When implemented)
   - Publish events for state changes that other modules need to know about
   - Subscribe to events instead of synchronous calls where appropriate
   - Use events for: user creation, document expiry updates

**When in doubt**: Ask "Could this module be extracted as a microservice tomorrow?" If answer is "no due to tight coupling," refactor to use interfaces.

### 3. **Development Workflow**

- **🚫 NEVER manually start servers** - All services run in Docker with hot reload
- **📋 Always check logs via Docker Compose** - Use `docker compose logs <service>`
- **🔄 Use hot reload** - Backend (AIR) and Frontend (Vite) auto-restart on changes
- Check relevant module CLAUDE.md before making changes
- Follow established patterns within each module
- Ensure comprehensive test coverage (minimum 80%)
- Verify cross-module compatibility for changes

### 4. **Quality Standards**

- Prioritize code maintainability and readability
- Document complex business logic and algorithms
- Follow module-specific conventions and patterns
- Ensure observability through proper logging and monitoring

**CRITICAL**: Always consult the relevant module's CLAUDE.md file for specific implementation guidelines before making any changes.

---

## Module Extraction Readiness Checklist

Use this checklist to verify that each backend module maintains microservices-ready architecture:

### Per-Module Requirements

For each module (auth, user, reporting), verify:

#### ✅ **Interface-Based Communication**

- [ ] Module exposes service interface (e.g., `type UserService interface`)
- [ ] Other modules depend on interface, not concrete implementation
- [ ] All dependencies injected via constructor (no global state)

#### ✅ **Independent Data Access**

- [ ] Module has dedicated repository layer
- [ ] No direct database queries from other modules
- [ ] Clear collection/table ownership

#### ✅ **Self-Contained Domain Logic**

- [ ] Business rules contained within module
- [ ] Module can be tested independently
- [ ] <3 dependencies on other modules (low coupling)

#### ✅ **Clear API Contracts**

- [ ] Input/output DTOs defined
- [ ] Error handling standardized
- [ ] API versioning considered (e.g., `/api/v1/users`)

#### ✅ **Event Publishing Readiness** (When Kafka added)

- [ ] Domain events identified (e.g., `UserCreated`, `DocumentExpired`)
- [ ] Event publishing points defined in service layer
- [ ] Event schemas documented

#### ✅ **Observability**

- [ ] Structured logging with context
- [ ] Module-specific metrics exposed
- [ ] Request correlation IDs propagated

### System-Wide Requirements

- [ ] OpenTelemetry tracing infrastructure ready
- [ ] Service discovery mechanism considered (for future)
- [ ] API Gateway pattern understood
- [ ] Distributed transaction strategy defined (SAGA pattern)

### Migration Readiness Indicator

**🟢 Ready for Extraction**: Module meets all ✅ requirements above
**🟡 Needs Refactoring**: Module meets 4-5 requirements
**🔴 Tightly Coupled**: Module meets <4 requirements

**Target**: All modules should be 🟢 Ready or 🟡 Needs Refactoring to maintain extraction optionality.

---

### 🔗 Module Quick Links

[Backend](backend/CLAUDE.md) | [Frontend](frontend/CLAUDE.md) | [Mobile](mobile/CLAUDE.md) | [Shared](shared/CLAUDE.md) | [Docker](docker/CLAUDE.md) | [Scripts](scripts/CLAUDE.md) | [Auth Flow & RBAC](docs/Authentication_flow.md)
