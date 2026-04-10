# Orkestra

Professional business and service management platform — electronic invoicing (FatturaPA/SDI), AI sales intelligence, RAG pipeline, and document generation. Go backend, React frontend, Flutter mobile, fully modular.

## Architecture at a glance

- **Backend** — Go 1.25, Huma v2 (OpenAPI-first), self-contained modules with lifecycle (`Init`, `RegisterRoutes`, `Start`, `Stop`, `HealthCheck`). Three core modules (auth, user, navigation) always load; every other feature is an opt-in addon registered in `backend/cmd/server/catalog.go`.
- **Frontend** — React 19 + Vite 7 + TypeScript 5.9 strict. Navigation is fetched dynamically from the backend, so the UI reflects whatever modules are enabled.
- **Mobile** — Flutter 3.35 + Riverpod (early-stage).
- **Data** — MongoDB 8 + Redis 8. Optional Memgraph knowledge graph.
- **Auth** — OAuth 2.1 (Google, Apple, GitHub, Discord), RS256 JWT, 6-role RBAC hierarchy.

See [CLAUDE.md](CLAUDE.md) for the full module catalog and architecture notes, and [docs/Architecture_Modernization_Report.md](docs/Architecture_Modernization_Report.md) for the modernization roadmap.

## Quick start — minimal profile

The minimal profile boots Orkestra with just core modules (auth, user, navigation, dev token generator) on any Docker host. Four containers, public base images only, roughly 500 MB of RAM.

```bash
cd docker
docker network create orkestra-network   # first time only
docker compose -f docker-compose.minimal.yml --env-file .env.minimal up -d
```

Wait ~30 seconds for the backend to come up, then:

- Frontend — http://localhost:8050
- Backend API — http://localhost:3050
- OpenAPI docs — http://localhost:3050/docs
- Health check — http://localhost:3050/health

Ports are deliberately non-standard (3050/8050/27050/6350) so the minimal stack can run alongside the full dev stack or other unrelated Docker projects without colliding. Override them in `docker/.env.minimal` if you want the classic 3000/8080/27017/6379.

Generate an administrator token for first login:

```bash
ORKESTRA_API_URL=http://localhost:3050 ./scripts/devtoken.sh administrator
```

### Enabling more modules

Edit `docker/.env.minimal` and add module names to `MODULES`. Dependencies are auto-included.

```bash
# Enable billing + documents (documents is a dependency, auto-included)
MODULES=dev,billing
```

Then restart the backend:

```bash
docker compose -f docker-compose.minimal.yml --env-file .env.minimal up -d --force-recreate backend
```

Addons with external dependencies (Gotenberg for PDFs, Memgraph for graph, Hindsight for agents) will need their infrastructure to be reachable — either add those services to the compose file or point the module at a remote host via the admin API (`GET/PATCH /v1/admin/modules`).

## Full development stack

For hot-reload Go development with AIR and the full addon fleet (Gotenberg, Hindsight, etc.), use the dev compose. Note that this stack uses Chainguard hardened images (`dhi.io/*`) and requires registry access.

```bash
./deploy.sh                                     # interactive deployment manager
# or manually:
cd docker
docker compose -f docker-compose.infra.yml up -d
docker compose -f docker-compose.dev.yml up -d
```

## Adding a new module

### Backend

1. Create `backend/internal/addons/<name>/module.go` implementing the `Module` interface
2. Register it in `backend/cmd/server/catalog.go` under `optionalModules`
3. Declare `Collections()`, `NavItems()`, `ConfigSchema()`, `Dependencies()`
4. Use `shared/iface` interfaces for cross-module dependencies — never import another module's `services/` or `repository/` package directly

See `backend/CLAUDE.md` for details.

### Frontend

The React app reads the enabled modules from `/v1/navigation` at boot and only renders routes for modules the backend has turned on. Add per-module UI under `frontend/src/modules/<name>/` (see `frontend/CLAUDE.md`).

## Repository layout

```
orkestra/
├── backend/                 # Go modular monolith
│   ├── cmd/
│   │   ├── server/          # Monolith binary
│   │   └── ai-service/      # Optional AI sidecar
│   ├── internal/
│   │   ├── core/            # Always loaded: auth, user, navigation
│   │   ├── addons/          # Optional: billing, documents, rag, agents, ...
│   │   └── shared/          # Module interface, config, middleware, interfaces
│   ├── Dockerfile           # Chainguard hardened images (dev + prod)
│   └── Dockerfile.minimal   # Public images for the minimal profile
├── frontend/                # React 19 + Vite 7
├── mobile/                  # Flutter 3.35
├── docker/                  # Compose files + env templates
├── docs/                    # Architecture and modernization docs
└── scripts/                 # devtoken.sh, env-validate.sh, ...
```

## License

Proprietary — all rights reserved.
