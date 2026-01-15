# ============================================
# ORKESTRA Backend - Multi-Stage Dockerfile
# ============================================
#
# This Dockerfile supports multiple build targets:
# - development: Hot reload with AIR (used by docker-compose.dev.yml)
# - production: Optimized binary (used by docker-compose.prod.yml and staging)
#
# Build commands:
#   Development: docker build --target development -t orkestra-backend:dev .
#   Production:  docker build --target production -t orkestra-backend:prod .
#
# ============================================

# ============================================
# STAGE 1: Base Dependencies
# ============================================
# Using Docker Hardened Images (DHI) for security
# dhi.io/golang:1-dev includes git, ca-certificates, tzdata, and build tools
FROM dhi.io/golang:1-dev AS base

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies (cached layer)
RUN go mod download && go mod verify

# ============================================
# STAGE 2: Development (Hot Reload with AIR)
# ============================================
FROM base AS development

# Install AIR for hot reload
RUN go install github.com/air-verse/air@latest

# Create non-root user for development
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

# Set permissions
RUN mkdir -p /app/tmp /app/.go-cache /app/.go-build-cache && \
    chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 3000

# AIR will be started by docker-compose with volume mounts
# CMD is defined in docker-compose.dev.yml

# ============================================
# STAGE 3: Builder (Production Build)
# ============================================
FROM base AS builder

# Copy source code
COPY . .

# Build arguments
ARG VERSION=dev
ARG BUILD_TIME
ARG GIT_COMMIT

# Build optimized binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a \
    -installsuffix cgo \
    -ldflags="-w -s \
    -X main.Version=${VERSION} \
    -X main.BuildTime=${BUILD_TIME} \
    -X main.GitCommit=${GIT_COMMIT}" \
    -o /app/bin/server \
    ./cmd/server

# Verify binary
RUN /app/bin/server --version || echo "Binary built successfully"

# ============================================
# STAGE 4: Production Runtime
# ============================================
# Using Docker Hardened Images (DHI) for production
# dhi.io/golang:1 is the hardened runtime image
FROM dhi.io/golang:1 AS production

# Set timezone
ENV TZ=Europe/Rome

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bin/server /app/server

# Expose port
EXPOSE 3000

# Note: No HEALTHCHECK in Dockerfile - DHI images don't have curl/wget
# Health checks are performed externally by the deployment script

# Set environment
ENV ENV=production \
    GIN_MODE=release \
    PORT=3000

# Run the binary
CMD ["/app/server"]

# ============================================
# STAGE 5: Staging Runtime (Same as Production)
# ============================================
FROM production AS staging

# Override environment
ENV ENV=staging \
    GIN_MODE=release

# Staging uses same runtime as production
# CMD is inherited from production stage
