# Module: Docker Infrastructure

_Path: `/docker`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

<!-- Navigation -->

[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [🚀 Quick Start](../CLAUDE.md#quick-start)

<!-- /Navigation -->

## Module Purpose

The docker module provides **containerized infrastructure and deployment configurations** for the Orkestra system across development and production environments.

- **Primary Role**: Container orchestration and environment management
- **System Integration**: Provides database, caching, and application container services
- **Architecture**: Clean separation between infrastructure and application services

## Dependencies

### Imports

- **[`/backend/`](../backend/CLAUDE.md)** - Go application containerization
- **[`/frontend/`](../frontend/CLAUDE.md)** - React application containerization

### Importers

- **[`/scripts/`](../scripts/CLAUDE.md)** - Automation scripts for container orchestration
- **Developers**: Local development environment setup
- **Production**: Deployment and scaling configurations

## AI Assistant Critical Rules

### 🚨 MANDATORY: Check ENV Variable FIRST

**BEFORE running ANY Docker command, you MUST check the current environment:**

```bash
# ALWAYS run this FIRST before any docker operations
grep "^ENV=" /home/tore/orkestra/docker/.env
```

**This determines which compose file to use:**
- `ENV=development` → Use `docker-compose.dev.yml`
- `ENV=staging` → Use `docker-compose.staging.yml`
- `ENV=production` → Use `docker-compose.prod.yml`

**⛔ NEVER assume the environment. ALWAYS check first.**

---

### 🚨 ALWAYS use `--env-file` when running Docker Compose commands:

```bash
# ✅ CORRECT - Always specify the env file
docker compose -f docker-compose.staging.yml --env-file .env up -d
docker compose -f docker-compose.staging.yml --env-file .env restart frontend
docker compose -f docker-compose.staging.yml --env-file .env logs frontend

# ❌ WRONG - Using wrong compose file without checking ENV first
docker compose -f docker-compose.dev.yml up -d
```

**Compose file selection based on ENV variable:**
- `ENV=development` → `docker-compose.dev.yml` with `--env-file .env`
- `ENV=staging` → `docker-compose.staging.yml` with `--env-file .env`
- `ENV=production` → `docker-compose.prod.yml` with `--env-file .env`

---

## Three-Stage Environment Workflow

ORKESTRA uses a three-stage DevOps workflow: **Development**, **Staging**, and **Production**.

### Environment Files

| File | Environment | Purpose |
|------|-------------|---------|
| `.env.development` | Development | Local dev with hot reload, relaxed security |
| `.env.staging` | Staging | Production-like behavior, staging credentials |
| `.env.production` | Production | Full security, production credentials |
| `.env.example` | Template | Copy to create new environment files |

### Quick Commands

```bash
# Interactive deployment manager (from project root)
./deploy.sh                        # Select environment and operation interactively

# Interactive log viewer (from project root)
./logs.sh                          # Select service and view logs interactively

# Validate environment files
./scripts/env-validate.sh all      # Validate all
./scripts/env-validate.sh staging  # Validate specific
```

### Environment Behavior Matrix

| Feature | Development | Staging | Production |
|---------|-------------|---------|------------|
| `ENV` value | `development` | `staging` | `production` |
| Log Format | Text (pretty) | JSON | JSON |
| Error Details | Exposed | Hidden | Hidden |
| Cookie Secure | `false` | `true` | `true` |
| Cookie SameSite | `lax` | `strict` | `strict` |
| Rate Limits | 1000/min | 60/min | 30/min |
| Debug Mode | Enabled | Disabled | Disabled |
| HTTPS Required | No | Yes | Yes |
| Observability | Disabled | Enabled | Enabled |

---

## Clean Docker Compose Architecture

### Core Philosophy

Separate infrastructure from applications across four compose files:

1. **`docker-compose.infra.yml`** - Infrastructure services only (MongoDB, Redis)
2. **`docker-compose.dev.yml`** - Application services in development mode with hot reload
3. **`docker-compose.staging.yml`** - Application services in staging mode (production-like)
4. **`docker-compose.prod.yml`** - Application services in production mode with optimizations

### File Organization

```
/                              # Project root
├── deploy.sh                  # Interactive deployment manager
├── logs.sh                    # Interactive log viewer
└── docker/
    ├── docker-compose.infra.yml   # Infrastructure: MongoDB, Redis
    ├── docker-compose.dev.yml     # Development: Backend, Frontend with hot reload
    ├── docker-compose.staging.yml # Staging: Production-like behavior
    ├── docker-compose.prod.yml    # Production: Optimized Backend, Frontend
    ├── .env.example               # Template for environment files
    ├── .env.development           # Development environment variables
    ├── .env.staging               # Staging environment variables
    ├── .env.production            # Production environment variables
    ├── keys/                      # JWT and OAuth keys (gitignored)
    └── mongo-init/                # MongoDB initialization scripts
```

### Environment Combinations

- **Development**: `docker-compose.infra.yml` + `docker-compose.dev.yml` + `.env.development`
- **Staging**: `docker-compose.infra.yml` + `docker-compose.staging.yml` + `.env.staging`
- **Production**: `docker-compose.infra.yml` + `docker-compose.prod.yml` + `.env.production`

**IMPORTANT**: All Docker files must remain in `/docker` directory for proper build contexts.

## Quick Start

### Prerequisites

```bash
# Create external network (required for all environments)
docker network create orkestra-network

# Copy environment template and configure (first time only)
cd docker
cp .env.example .env.development
cp .env.example .env.staging
cp .env.example .env.production
# Edit each file with appropriate values
```

### Using deploy.sh (Recommended)

```bash
# From project root - interactive deployment manager
./deploy.sh

# The script will:
# 1. Show available environments with status (✓/✗)
# 2. Prompt for environment selection
# 3. Show operation menu (Deploy, Stop, Status)
# 4. Handle all docker compose operations automatically
```

### Using logs.sh

```bash
# From project root - interactive log viewer
./logs.sh              # Interactive service selection
./logs.sh -f           # Follow logs
./logs.sh -n 50        # Show last 50 lines
./logs.sh -t           # Show timestamps
```

### Manual Docker Compose (Alternative)

```bash
# Navigate to docker directory
cd docker

# Development
docker compose -f docker-compose.infra.yml up -d
docker compose -f docker-compose.dev.yml --env-file .env.development up -d

# Production
docker compose -f docker-compose.infra.yml --env-file .env.production up -d
docker compose -f docker-compose.prod.yml --env-file .env.production up -d
```

## Service Architecture

### Infrastructure Services (`docker-compose.infra.yml`)

**Shared across all environments - start once, use everywhere**

| Service       | Port  | Purpose             | Health Check     |
| ------------- | ----- | ------------------- | ---------------- |
| **mongodb**   | 27017 | Primary database    | mongosh ping     |
| **redis**     | 6379  | Cache & sessions    | redis-cli ping   |
| **gotenberg** | 3030  | PDF generation      | curl /health     |

### Application Services

#### Development (`docker-compose.dev.yml`)

**Lightweight development with hot reload**

| Service      | Port | Purpose       | Features                     |
| ------------ | ---- | ------------- | ---------------------------- |
| **backend**  | 3000 | Go API server | Hot reload (Air), debug logs |
| **frontend** | 8080 | React web app | Vite dev server, HMR         |

#### Production (`docker-compose.prod.yml`)

**Optimized production services**

| Service      | Port | Purpose       | Features                       |
| ------------ | ---- | ------------- | ------------------------------ |
| **backend**  | 3000 | Go API server | Optimized build, health checks |
| **frontend** | 8080 | React web app | Nginx static serving           |

## Network Architecture

### Internal Communication

- **Network**: `orkestra-network` (external bridge network)
- **Service Discovery**: Docker internal DNS resolution
- **Security**: Isolated container network, no external access to internal services
- **Subnet**: 172.20.0.0/16 (configured in docker-compose.yml)

### Port Mapping Strategy

```
Infrastructure (docker-compose.infra.yml):
27017 → mongodb:27017     # Database access
6379  → redis:6379        # Cache access
3030  → gotenberg:3000    # PDF generation (Chromium-based)

Application Services (dev/prod):
3000  → backend:3000      # API server
8080  → frontend:8080     # Web application
```

### Security & Secrets Management

**Environment File Security:**

```bash
# Ensure .env files are gitignored
echo ".env*" >> .gitignore
echo "!.env.example" >> .gitignore

# Encrypt production secrets (optional)
openssl enc -aes-256-cbc -salt -in .env.prod -out .env.prod.enc
openssl enc -aes-256-cbc -d -in .env.prod.enc -out .env.prod

# Use external secret management in production
# - HashiCorp Vault
# - AWS Secrets Manager
# - Kubernetes Secrets
```

**Container Security:**

- Non-root users in production containers
- Read-only root filesystems where possible
- Resource limits to prevent DoS
- Regular security scanning with Trivy
- Minimal Alpine base images

## Environment Configuration

### Current Setup

The existing `.env` file contains development defaults. For production, create `.env.prod` with secure values.

### Development Environment Variables (`.env`)

```bash
# Infrastructure Ports
MONGO_PORT=27017              # Host port for MongoDB
REDIS_PORT=6379               # Host port for Redis

# Application Ports
BACKEND_PORT=3000             # Host port for backend API
FRONTEND_PORT=8080            # Host port for frontend

# MongoDB
MONGO_ROOT_USERNAME=admin
MONGO_ROOT_PASSWORD=changeme  # Change in production
MONGO_DATABASE=orkestra

# Redis
REDIS_PASSWORD=changeme       # Change in production

# JWT (Development defaults)
JWT_SECRET=dev-jwt-secret-key-change-in-production
JWT_REFRESH_SECRET=dev-jwt-refresh-secret-change-in-production
JWT_ACCESS_EXPIRY=15m
JWT_REFRESH_EXPIRY=7d

# URLs (Development)
FRONTEND_URL=http://localhost:8080
BACKEND_URL=http://localhost:3000
VITE_API_URL=http://localhost:3000
VITE_WS_URL=ws://localhost:3000

# Security (Development - relaxed)
CORS_ORIGINS=http://localhost:8080,http://localhost:5173
RATE_LIMIT_MAX=1000
RATE_LIMIT_WINDOW=1m

# OAuth (Development - optional)
GOOGLE_CLIENT_ID_DEV=your_dev_google_client_id
GOOGLE_CLIENT_SECRET_DEV=your_dev_google_secret
APPLE_CLIENT_ID_DEV=your_dev_apple_client_id
APPLE_TEAM_ID_DEV=your_dev_apple_team_id
APPLE_KEY_ID_DEV=your_dev_apple_key_id
APPLE_PRIVATE_KEY_DEV=your_dev_apple_private_key
```

### Production Environment Template (`.env.prod`)

```bash
# Database Credentials (CHANGE THESE)
MONGO_USERNAME=orkestra_prod_user
MONGO_PASSWORD=SECURE_MONGO_PASSWORD_HERE
MONGO_DATABASE=orkestra
REDIS_PASSWORD=SECURE_REDIS_PASSWORD_HERE

# JWT Configuration
JWT_SECRET=BASE64_ENCODED_SECRET_KEY_HERE
JWT_REFRESH_SECRET=BASE64_ENCODED_REFRESH_SECRET_HERE
JWT_ACCESS_EXPIRY=15m
JWT_REFRESH_EXPIRY=7d

# OAuth Credentials
GOOGLE_CLIENT_ID=your_production_google_client_id
GOOGLE_CLIENT_SECRET=your_production_google_client_secret
APPLE_CLIENT_ID=your_production_apple_client_id
APPLE_TEAM_ID=your_apple_team_id
APPLE_KEY_ID=your_apple_key_id
APPLE_PRIVATE_KEY=your_apple_private_key

# Production URLs
FRONTEND_URL=https://orkestra.cc
BACKEND_URL=https://api.orkestra.cc
WS_URL=wss://io.orkestra.cc
CORS_ORIGINS=https://orkestra.cc

# Security Settings
RATE_LIMIT_MAX=100
RATE_LIMIT_WINDOW=15m

# Email Configuration
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your_email@gmail.com
SMTP_PASSWORD=your_app_password
SMTP_FROM=noreply@orkestra.cc

# AWS S3 Storage
S3_BUCKET=orkestra-prod-uploads
S3_REGION=us-east-1
AWS_ACCESS_KEY_ID=your_access_key
AWS_SECRET_ACCESS_KEY=your_secret_key
```

## Monitoring & Observability

### Infrastructure Monitoring

With the simplified infrastructure, monitoring focuses on essential services:

- **MongoDB**: Monitor connection health, query performance, disk usage
- **Redis**: Monitor memory usage, cache hit rates, connection counts
- **Application Services**: Monitor via application-level instrumentation

### External Monitoring Options

For production environments, consider external monitoring services:

- **DataDog**: Comprehensive APM and infrastructure monitoring
- **New Relic**: Application performance monitoring
- **Prometheus + Grafana**: Self-hosted metrics and alerting
- **Cloud Provider Solutions**: AWS CloudWatch, GCP Operations, Azure Monitor

### Basic Health Checks

```bash
# Check MongoDB health
docker exec orkestra-mongodb mongosh --eval "db.adminCommand('ping')"

# Check Redis health
docker exec orkestra-redis redis-cli ping

# Check Gotenberg health (PDF service)
curl http://localhost:3030/health

# Check application health endpoints
curl http://localhost:3000/health  # Backend health
curl http://localhost:8080         # Frontend availability
```

## Backup & Restore

### Manual Backup Commands

**MongoDB Backup:**

```bash
#!/bin/bash
# Create timestamped backup
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="./backups/mongo_${TIMESTAMP}"

# Run mongodump
docker exec orkestra-mongodb mongodump \
  --uri="mongodb://admin:changeme@localhost:27017" \
  --out=/tmp/backup \
  --gzip

# Copy to host
docker cp orkestra-mongodb:/tmp/backup ${BACKUP_DIR}

# Upload to S3 (optional)
# aws s3 cp ${BACKUP_DIR} s3://orkestra-backups/mongo/${TIMESTAMP}/ --recursive
```

**Redis Backup:**

```bash
#!/bin/bash
# Force Redis to save current dataset
docker exec orkestra-redis redis-cli --pass changeme BGSAVE
sleep 5

# Copy RDB file
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
docker cp orkestra-redis:/data/dump.rdb ./backups/redis_${TIMESTAMP}.rdb
```

### Restore Procedures

**MongoDB Restore:**

```bash
# Stop application services
docker compose -f docker-compose.dev.yml down    # Or prod.yml
docker compose -f docker-compose.prod.yml down

# Restore from backup
docker exec orkestra-mongodb mongorestore \
  --uri="mongodb://admin:changeme@localhost:27017" \
  --gzip \
  /path/to/backup/directory

# Restart services
docker compose -f docker-compose.dev.yml up -d   # Or prod.yml
docker compose -f docker-compose.prod.yml up -d
```

**Redis Restore:**

```bash
# Stop Redis
docker compose stop redis

# Copy backup file
docker cp ./backups/redis_backup.rdb orkestra-redis:/data/dump.rdb

# Start Redis
docker compose start redis
```

## Common Operations

### Service Management

```bash
# Navigate to docker directory first
cd docker

# Start infrastructure only (shared by all environments)
docker compose -f docker-compose.infra.yml up -d

# Start development environment
docker compose -f docker-compose.dev.yml up -d

# Start production environment
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d

# View logs for specific services
docker compose -f docker-compose.dev.yml logs -f orkestra-backend orkestra-frontend

# Restart a specific service
docker compose -f docker-compose.dev.yml restart orkestra-backend

# Scale production backend
docker compose -f docker-compose.prod.yml up -d --scale backend=3

# Stop application services (keep infrastructure running)
docker compose -f docker-compose.dev.yml down
docker compose -f docker-compose.prod.yml down

# Stop infrastructure (when completely done)
docker compose -f docker-compose.infra.yml down
```

### Troubleshooting

```bash
# Check service health
docker compose ps

# View detailed logs
docker compose logs --tail=100 backend

# Execute commands in containers
docker exec -it orkestra-backend sh
docker exec -it orkestra-mongodb mongosh

# Check network connectivity
docker network inspect orkestra-network

# Monitor resource usage
docker stats
```

### Development Workflow

```bash
# Start infrastructure (once per development session)
docker compose -f docker-compose.infra.yml up -d

# Hot reload development (recommended)
docker compose -f docker-compose.dev.yml up -d

# Rebuild after Dockerfile changes
docker compose -f docker-compose.dev.yml up -d --build backend

# Clean rebuild (remove cache)
docker compose -f docker-compose.dev.yml build --no-cache backend

# Update dependencies and rebuild
docker compose -f docker-compose.dev.yml pull
```

## Log Management - CRITICAL FOR DEVELOPMENT

### 🚫 NEVER manually start servers

The backend and frontend run automatically in Docker with hot reload. **ALWAYS** use Docker Compose commands to check logs and status.

### ✅ Essential Logging Commands

#### Backend Server Logs

```bash
# View backend logs (MOST IMPORTANT - use this to check server status)
docker compose logs backend

# Follow backend logs in real-time (for debugging)
docker compose logs -f backend

# View recent backend logs with timestamps
docker compose logs -t --tail=50 backend

# Search backend logs for errors
docker compose logs backend | grep -i error
```

#### Frontend Logs

```bash
# View frontend logs
docker compose logs frontend

# Follow frontend logs in real-time
docker compose logs -f frontend

# Frontend build errors
docker compose logs frontend | grep -i error
```

#### Infrastructure Logs

```bash
# Database logs
docker compose -f docker-compose.infra.yml logs mongodb

# Cache logs
docker compose -f docker-compose.infra.yml logs redis

# All infrastructure logs
docker compose -f docker-compose.infra.yml logs
```

#### Service Status

```bash
# Check all service health
docker compose ps

# Check infrastructure status
docker compose -f docker-compose.infra.yml ps

# View resource usage
docker stats
```

### 🔄 Hot Reload Status

```bash
# Backend: AIR hot reload logs
docker compose logs backend | grep -i "watching\|building\|reload"

# Frontend: Vite HMR logs
docker compose logs frontend | grep -i "hmr\|updated\|ready"
```

### 🚨 Troubleshooting

```bash
# Restart specific service (rarely needed)
docker compose restart backend

# Rebuild service after Dockerfile changes
docker compose up -d --build backend

# View complete container information
docker inspect orkestra-backend-dev

# Access container shell for debugging (emergency only)
docker exec -it orkestra-backend-dev sh
docker compose -f docker-compose.dev.yml up -d --build

# Quick restart of application services (keep infrastructure running)
docker compose -f docker-compose.dev.yml restart
```

## Security Best Practices

### Container Security

1. **Alpine base images** - Minimal attack surface
2. **Non-root users** - Run containers as unprivileged users
3. **Image scanning** - Use Trivy/Snyk for vulnerability scanning
4. **Resource limits** - CPU/memory limits to prevent DoS
5. **Read-only filesystems** - Mount as read-only where possible
6. **Secrets management** - Never hardcode secrets in images
7. **Network isolation** - Use custom bridge networks
8. **Regular updates** - Keep base images and dependencies current

### Production Security Checklist

- [ ] Change all default passwords in `.env.prod`
- [ ] Use strong, unique passwords (>20 characters)
- [ ] Enable Docker Content Trust for image signing
- [ ] Configure firewall rules (only expose necessary ports)
- [ ] Set up SSL certificates for Nginx
- [ ] Enable audit logging for containers
- [ ] Implement backup encryption
- [ ] Configure log rotation and retention
- [ ] Set up intrusion detection
- [ ] Regular security scanning

---

## Module-Specific Guidelines

- **Environment Separation**: Maintain clear separation between infrastructure and application services
- **Security**: Use secure defaults and environment-specific configurations
- **Scalability**: Design for horizontal scaling in production environments
- **Monitoring**: Implement comprehensive health checks and logging
- **Backup**: Ensure data persistence and backup strategies for databases
- **Documentation**: Maintain clear instructions for development and production setup

---

### Related Guides

- [Project Overview](../CLAUDE.md) - System architecture and design principles
- [Backend Containerization](../backend/CLAUDE.md) - Go API server configuration
- [Frontend Containerization](../frontend/CLAUDE.md) - React application setup
- [Documents Module](../backend/internal/addons/documents/CLAUDE.md) - PDF generation with Gotenberg
- [Deployment Scripts](../scripts/CLAUDE.md) - Automation and deployment orchestration
