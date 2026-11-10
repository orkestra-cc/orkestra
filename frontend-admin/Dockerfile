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
FROM node:24.22.2-alpine@sha256:a34ab6d7f6f4813f4d0c5469d74685604b962fbdbf5793a7bc7b4998ecb867c8 AS builder

WORKDIR /app

# Build-time toggles that don't depend on the deployment environment.
# Anything env-specific belongs in window.__ORKESTRA_CONFIG__ — see the
# entrypoint in stage 2.
ARG VITE_PUBLIC_URL=/
ARG VITE_SENTRY_DSN
# Application version baked into the JS bundle by Vite's `define`.
# Default "dev" keeps `docker build` working without a wrapper; CI and
# orkestra.sh pass the git tag here.
ARG ORKESTRA_VERSION=dev

ENV VITE_PUBLIC_URL=${VITE_PUBLIC_URL}
ENV VITE_SENTRY_DSN=${VITE_SENTRY_DSN}
ENV ORKESTRA_VERSION=${ORKESTRA_VERSION}

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

# Self-hosted TinyMCE: the editor loads /tinymce/tinymce.min.js, which
# postinstall.js copies from node_modules into public/tinymce (gitignored).
# `npm ci --ignore-scripts` above skips that hook, so run it explicitly —
# otherwise the image ships without TinyMCE and every rich-text editor
# renders as an empty box.
RUN node postinstall.js

# Build the application, and fail it if TinyMCE did not make it into dist/.
# The build's tsc peaks above Node's default heap on a CI runner (about 2 GB
# there): pin 4 GB, as the Makefile's ADMIN_TSC_NODE_OPTIONS does.
RUN NODE_OPTIONS=--max-old-space-size=4096 npm run build && test -f dist/tinymce/tinymce.min.js

# ============================================
# Stage 2: Production
# ============================================
FROM nginx:1.31.8-alpine@sha256:713fc2ebfd92e8217db451477182ee91944b123d2b1ee9fd5a8c6e91ca6c3a11 AS production

# curl for healthcheck; gettext for envsubst in the entrypoint.
RUN apk add --no-cache curl gettext

# Copy built files from builder
COPY --from=builder /app/dist /usr/share/nginx/html

# Default nginx config: SPA fallback + /health. Mountable override via
# /etc/nginx/conf.d/default.conf is still supported — and the prod
# compose mounts docker/frontend-admin/nginx.conf, which carries the reasons
# behind each header. The cache policy here is the same: /config.js no-store,
# hashed /assets/ immutable, index.html (every SPA route) no-cache so a new
# build is picked up on the next load. Locations that declare a header
# re-state the security ones (nginx does not merge add_header across levels).
RUN echo 'server { \
    listen 80; \
    server_name localhost; \
    root /usr/share/nginx/html; \
    index index.html; \
    add_header X-Frame-Options "SAMEORIGIN" always; \
    add_header X-Content-Type-Options "nosniff" always; \
    add_header X-XSS-Protection "0" always; \
    location = /config.js { \
        add_header Cache-Control "no-store" always; \
        add_header X-Content-Type-Options "nosniff" always; \
        try_files $uri =404; \
    } \
    location ^~ /assets/ { \
        add_header Cache-Control "public, max-age=31536000, immutable"; \
        add_header X-Content-Type-Options "nosniff" always; \
        try_files $uri =404; \
    } \
    location / { \
        add_header Cache-Control "no-cache" always; \
        add_header X-Frame-Options "SAMEORIGIN" always; \
        add_header X-Content-Type-Options "nosniff" always; \
        add_header X-XSS-Protection "0" always; \
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
  debug: ${ORKESTRA_DEBUG:-false},\n\
  version: "${ORKESTRA_VERSION:-dev}",\n\
  appName: "${ORKESTRA_APP_NAME:-orkestra}",\n\
  cloneVersion: "${ORKESTRA_CLONE_VERSION:-dev}",\n\
  buildCommit: "${ORKESTRA_BUILD_COMMIT:-}",\n\
  startedAt: "$(date -u +'\''%%Y-%%m-%%d %%H:%%MZ'\'')",\n\
  cookiePrefix: "${ORKESTRA_COOKIE_PREFIX:-}"\n\
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
