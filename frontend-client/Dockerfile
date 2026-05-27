# Tier-2 client demo SPA — production image.
#
# Runtime config: VITE_API_BASE / VITE_STRIPE_PUBLISHABLE_KEY are no longer
# baked at build time. The SPA reads window.__ORKESTRA_CONFIG__ from /config.js,
# which the nginx entrypoint regenerates from container env vars
# (ORKESTRA_API_BASE, ORKESTRA_STRIPE_PUBLISHABLE_KEY) on container start.
# One image works in staging and prod — set the ORKESTRA_* vars on the
# container instead of rebuilding per environment.

# ============================================
# Stage 1: Build
# ============================================
FROM node:24-alpine AS builder

WORKDIR /app

# Application version baked into the JS bundle by Vite's `define`.
# Default "dev" keeps `docker build` working standalone; CI and
# orkestra.sh pass the git tag here.
ARG ORKESTRA_VERSION=dev
ENV ORKESTRA_VERSION=${ORKESTRA_VERSION}

COPY package*.json ./
RUN npm ci --ignore-scripts

COPY . .
RUN npm run build

# ============================================
# Stage 2: Serve
# ============================================
FROM nginx:alpine AS production

# curl for healthcheck; gettext for envsubst in the entrypoint.
RUN apk add --no-cache curl gettext

COPY --from=builder /app/dist /usr/share/nginx/html

# SPA fallback + /health endpoint.
RUN printf 'server {\n\
    listen 80;\n\
    server_name _;\n\
    root /usr/share/nginx/html;\n\
    index index.html;\n\
    location /health {\n\
        access_log off;\n\
        return 200 "healthy";\n\
        add_header Content-Type text/plain;\n\
    }\n\
    location / {\n\
        try_files $uri $uri/ /index.html;\n\
    }\n\
}\n' > /etc/nginx/conf.d/default.conf

# Runtime-config entrypoint: rewrites /usr/share/nginx/html/config.js from
# container env vars before nginx forks. Slots into nginx:alpine's
# /docker-entrypoint.d/ hook so we don't have to override ENTRYPOINT.
RUN printf '#!/bin/sh\n\
set -e\n\
cat > /usr/share/nginx/html/config.js <<EOF\n\
window.__ORKESTRA_CONFIG__ = {\n\
  apiBase: "${ORKESTRA_API_BASE:-http://api.localhost:3000}",\n\
  stripePublishableKey: "${ORKESTRA_STRIPE_PUBLISHABLE_KEY:-}"\n\
};\n\
EOF\n\
' > /docker-entrypoint.d/10-write-config.sh \
 && chmod +x /docker-entrypoint.d/10-write-config.sh

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:80/health || exit 1

CMD ["nginx", "-g", "daemon off;"]
