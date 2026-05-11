# Tier-2 client demo SPA — production image. Two-stage build: Node builds
# the static bundle, nginx serves it. Mirrors frontend/Dockerfile (operator
# console) so deploy tooling can treat both apps uniformly.
#
# The dev container in docker-compose.dev.yml does NOT use this Dockerfile —
# it bind-mounts the source and runs `npm run dev` against the public
# node:24-alpine image. This file builds the static bundle that ships to
# staging and prod.

# ============================================
# Stage 1: Build
# ============================================
FROM node:24-alpine AS builder

WORKDIR /app

# Build-time configuration. Vite inlines anything prefixed VITE_ at build
# time, so each environment (staging/prod) needs its own image — staging
# at app.orkestra.cc, prod at app.orkestra.com.
ARG VITE_API_BASE
ARG VITE_STRIPE_PUBLISHABLE_KEY

ENV VITE_API_BASE=${VITE_API_BASE}
ENV VITE_STRIPE_PUBLISHABLE_KEY=${VITE_STRIPE_PUBLISHABLE_KEY}

COPY package*.json ./
RUN npm ci --ignore-scripts

COPY . .
RUN npm run build

# ============================================
# Stage 2: Serve
# ============================================
FROM nginx:alpine AS production

RUN apk add --no-cache curl

COPY --from=builder /app/dist /usr/share/nginx/html

# SPA fallback — every unmatched path returns index.html so React Router
# owns the routing tree client-side. /health stays a literal endpoint so
# HAProxy / k8s liveness probes don't pull the SPA bundle.
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

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:80/health || exit 1

CMD ["nginx", "-g", "daemon off;"]
