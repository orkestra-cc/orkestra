# ORKESTRA Frontend Dockerfile
# Multi-stage build for React/Vite application
#
# Runtime config: VITE_API_URL/VITE_BACKEND_URL/VITE_WS_URL are NOT baked
# at build time. The SPA reads window.__ORKESTRA_CONFIG__ from /config.js,
# which the nginx entrypoint regenerates from env vars on container start.
# One image works in dev / staging / prod — set ORKESTRA_API_URL,
# ORKESTRA_WS_URL, ORKESTRA_ENV, ORKESTRA_DEBUG on the container.

# ============================================
# Stage 1: Build
# ============================================
FROM node:24-alpine AS builder

WORKDIR /app

# Build-time toggles that don't depend on the deployment environment.
# Anything env-specific belongs in window.__ORKESTRA_CONFIG__ — see the
# entrypoint in stage 2.
ARG VITE_PUBLIC_URL=/
ARG VITE_SENTRY_DSN

ENV VITE_PUBLIC_URL=${VITE_PUBLIC_URL}
ENV VITE_SENTRY_DSN=${VITE_SENTRY_DSN}

# Copy package files first for better caching
COPY package*.json ./

# Install dependencies
RUN npm ci --ignore-scripts

# Copy source code
COPY . .

# Seed public/config.js from the example template so `npm run build`
# always finds a runtime-config file to ship into dist/. The file is
# gitignored (each env regenerates its own), so build contexts produced
# from a clean checkout — including CI — would otherwise be missing it.
# The nginx entrypoint at /docker-entrypoint.d/10-write-config.sh
# overwrites this baked default at container start, so dev defaults never
# leak into a running staging/prod container.
RUN cp -n public/config.example.js public/config.js

# Build the application
RUN npm run build

# ============================================
# Stage 2: Production
# ============================================
FROM nginx:alpine AS production

# curl for healthcheck; gettext for envsubst in the entrypoint.
RUN apk add --no-cache curl gettext

# Copy built files from builder
COPY --from=builder /app/dist /usr/share/nginx/html

# Default nginx config: SPA fallback + /health. Mountable override via
# /etc/nginx/conf.d/default.conf is still supported.
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

# Runtime-config entrypoint: rewrites /usr/share/nginx/html/config.js
# from ORKESTRA_* env vars before nginx forks. Drops into the
# docker-entrypoint.d/ hook nginx:alpine already runs at start so we
# don't have to override ENTRYPOINT.
RUN printf '#!/bin/sh\n\
set -e\n\
cat > /usr/share/nginx/html/config.js <<EOF\n\
window.__ORKESTRA_CONFIG__ = {\n\
  apiUrl: "${ORKESTRA_API_URL:-http://console.localhost:3000}",\n\
  wsUrl: "${ORKESTRA_WS_URL:-ws://console.localhost:3000/ws}",\n\
  env: "${ORKESTRA_ENV:-production}",\n\
  debug: ${ORKESTRA_DEBUG:-false}\n\
};\n\
EOF\n\
' > /docker-entrypoint.d/10-write-config.sh \
 && chmod +x /docker-entrypoint.d/10-write-config.sh

# Expose port
EXPOSE 80

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:80/health || exit 1

# Run nginx (inherits the default entrypoint that scans /docker-entrypoint.d/).
CMD ["nginx", "-g", "daemon off;"]
