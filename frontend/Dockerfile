# ORKESTRA Frontend Dockerfile
# Multi-stage build for React/Vite application

# ============================================
# Stage 1: Build
# ============================================
FROM node:24-alpine AS builder

WORKDIR /app

# Build arguments for environment-specific configuration
ARG VITE_API_URL
ARG VITE_BACKEND_URL
ARG VITE_WS_URL
ARG VITE_ENV=production
ARG VITE_DEBUG=false
ARG VITE_SENTRY_DSN

# Set environment variables for build
ENV VITE_API_URL=${VITE_API_URL}
ENV VITE_BACKEND_URL=${VITE_BACKEND_URL}
ENV VITE_WS_URL=${VITE_WS_URL}
ENV VITE_ENV=${VITE_ENV}
ENV VITE_DEBUG=${VITE_DEBUG}
ENV VITE_SENTRY_DSN=${VITE_SENTRY_DSN}

# Copy package files first for better caching
COPY package*.json ./

# Install dependencies
RUN npm ci --ignore-scripts

# Copy source code
COPY . .

# Build the application
RUN npm run build

# ============================================
# Stage 2: Production
# ============================================
FROM nginx:alpine AS production

# Install curl for health checks
RUN apk add --no-cache curl

# Copy built files from builder
COPY --from=builder /app/dist /usr/share/nginx/html

# Copy nginx configuration (will be mounted as volume in docker-compose)
# Default nginx config as fallback
RUN echo 'server { \
    listen 80; \
    server_name localhost; \
    root /usr/share/nginx/html; \
    index index.html; \
    location / { \
        try_files $uri $uri/ /index.html; \
    } \
    location /health { \
        access_log off; \
        return 200 "healthy"; \
        add_header Content-Type text/plain; \
    } \
}' > /etc/nginx/conf.d/default.conf

# Expose port
EXPOSE 80

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:80/health || exit 1

# Run nginx
CMD ["nginx", "-g", "daemon off;"]
