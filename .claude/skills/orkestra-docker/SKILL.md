---
name: orkestra-docker
description: "Use for low-level per-service Docker surgery on the Orkestra stack — restart/logs/ps/up on a single service, AIR force-rebuild of the Go backend, infra-only ops, or figuring out which docker-compose.*.yml file owns a running container. NOT for full-stack deploy/rebuild/stop (use orkestra-stack + orkestra.sh for that)."
---

# Orkestra Docker Operations

Low-level, per-service container surgery on the Orkestra stack. This skill **auto-detects** which compose file(s) and stack own each service from container labels, instead of guessing from `ENV=`.

> **For full-stack lifecycle use the `orkestra-stack` skill** (deploy / rebuild / restart / stop a whole app via `orkestra.sh`). It injects `ORKESTRA_VERSION` and runs preflight; a raw `docker compose up` here does NOT, leaving the frontend footer version stale. Use *this* skill only for: AIR force-rebuild, infra-only ops, single-service restart/logs, and compose-file/stack detection.

## Multi-stack model (read this first)

Every checkout × environment combination is **one Compose project**: `STACK=${APP_NAME}-${ENV}` (e.g. `orkestra-development`), computed from `docker/.env` and exported as `COMPOSE_PROJECT_NAME` by `orkestra.sh`. Full design: [`docker/CLAUDE.md` § Multi-Stack Model](../../../docker/CLAUDE.md#multi-stack-model).

- **Containers**: `${APP_NAME}-<svc>-${ENV}` — e.g. `orkestra-backend-development`.
- **Network**: `${STACK}_default` — Compose's own per-project default network. There is no shared `orkestra-network` bridge anymore.
- **Service names are uniform** across dev/staging/prod: `backend`, `frontend-admin`, `client-frontend` (app), `mongodb`, `redis`, `rustfs` (infra), `grafana`/`loki`/`tempo`/`promtail`/`prometheus`/`otel-collector` (observability). There is no dev-only service-name variant anymore.
- **One project spans multiple files**: a stack's project is `docker-compose.infra.yml` + `docker-compose.{dev,staging,prod}.yml` + (opt-in) `docker-compose.observability.yml`, all under the same `${STACK}` project — not separate `orkestra-infra` / `orkestra-observability` projects.

## Why detection is non-trivial

`.env`'s `ENV=` labels the **host environment** (`development` / `staging` / `production`). It does **not** by itself tell you the compose file(s) in use, or which stack a given running container belongs to:

- A host with `ENV=staging` runs `docker-compose.staging.yml`, which itself bind-mounts source + runs AIR (staging-acts-as-dev on this repo).
- **Multiple stacks can run concurrently on one host** (different `APP_NAME` and/or `ENV`), each its own Compose project (`${APP_NAME}-${ENV}`). `docker/.env`'s `APP_NAME`/`ENV` tells you *this checkout's* stack identity, not what else is running.
- Within one stack's project, different services still come from different files: `mongodb` from `infra.yml`, `backend` from `dev.yml`/`staging.yml`/`prod.yml`, `grafana` from the opt-in `observability.yml` overlay.
- **Files drift from what's running.** A container's label points at the file(s) it was *launched* from, which may no longer define it (e.g. a leftover container whose service was since removed from a compose file). The running containers are ground truth; the files on disk are not.

So: never derive the compose file or stack identity from `ENV=` alone. Derive both from **container labels** — what was actually launched. Every container also carries an `orkestra.stack=${APP_NAME}-${ENV}` label, which is the fastest way to scope a `docker ps`/`docker inspect` to one stack when several are running.

## Service name vs container name

Both are now predictable and uniform in shape, but they are **not interchangeable**:

- **Service name** (`backend`, `mongodb`, …) — same string on dev/staging/prod, same string regardless of `APP_NAME`. Use it with `docker compose ... <cmd> <service>`.
- **Container name** (`${APP_NAME}-<svc>-${ENV}`, e.g. `orkestra-backend-development`) — stack-namespaced. Use it with a raw `docker exec` / `docker restart` / `docker inspect` / `docker logs` that doesn't go through `docker compose`.

Getting this wrong yields `no such service` (passed a container name to `docker compose`) or `no such container` (passed a bare service name to raw `docker`). The detection step below prints both so you never guess.

## MANDATORY first step — detect

Run once per session (or whenever the running stack may have changed). Fast (<50ms):

```bash
echo "=== .env identity ==="; grep -E "^(APP_NAME|ENV)=" docker/.env 2>/dev/null
(grep -qi microsoft /proc/version && echo "platform: WSL2 (AIR watcher unreliable — manual rebuild fallback)" || echo "platform: native Linux")
echo "=== Running compose projects (= stacks) ==="; docker compose ls 2>/dev/null
echo "=== service | container | stack | compose file (ground truth) ==="
docker ps --format '{{.Label "com.docker.compose.service"}}  {{.Names}}  {{.Label "orkestra.stack"}}  {{.Label "com.docker.compose.project.config_files"}}' 2>/dev/null \
  | awk -F'  ' '$1 != "" {printf "  %-18s %-38s %-30s %s\n", $1, $2, $3, $4}' | sort
```

The last block is the canonical lookup: **service name + container name + stack + compose file(s)** for every running container across every stack on the host. Use it to pick both the `-f` flag(s) and the `<service>`/`<container>` argument for every subsequent command — and to confirm you're targeting the right stack when more than one is up.

### Find the compose file for one service (in the current stack)

```bash
service="backend"
compose=$(docker ps --format '{{.Label "com.docker.compose.service"}}|{{.Label "orkestra.stack"}}|{{.Label "com.docker.compose.project.config_files"}}' \
  | awk -F'|' -v s="$service" -v stack="$STACK" '$1==s && (stack=="" || $2==stack) {print $3; exit}')
echo "$service -> $compose"
```

Returns a comma-separated list when a service was launched with multiple `-f` files (common for `mongodb`/`redis`, and for any app service when the observability overlay is active). Pass each as a separate `-f` flag.

## Compose invocation

```bash
# Paths below are relative to the repo root (the working directory). ALWAYS pass
# --env-file — compose does not auto-discover .env when -f points elsewhere.
# Also set COMPOSE_PROJECT_NAME (or -p) to the stack's project name so you don't
# accidentally create/attach to a second, differently-named project.
APP_NAME="$(grep -E '^APP_NAME=' docker/.env | cut -d= -f2-)"
ENV_VAL="$(grep -E '^ENV=' docker/.env | cut -d= -f2-)"
export COMPOSE_PROJECT_NAME="${APP_NAME}-${ENV_VAL}"

docker compose \
  -f docker/docker-compose.infra.yml \
  -f docker/docker-compose.staging.yml \
  --env-file docker/.env <cmd> <service>
```

## Common operations

Assume you ran detection and `$compose` holds the right `-f` argument(s) and `$svc` the right service name (`COMPOSE_PROJECT_NAME` set per above).

```bash
# Restart a service
docker compose -f "$compose" --env-file docker/.env restart "$svc"

# Tail logs
docker compose -f "$compose" --env-file docker/.env logs --tail 100 "$svc"

# Follow logs — run in the background (Bash run_in_background) or via Monitor so output streams to you
docker compose -f "$compose" --env-file docker/.env logs -f "$svc"

# Status of the active stack
docker compose -f "$compose" --env-file docker/.env ps

# Rebuild + restart one service (only meaningful on stacks with a real build: context — prod/GHCR)
docker compose -f "$compose" --env-file docker/.env up -d --build "$svc"
```

### Force-rebuild the Go backend when AIR misses a change

dev/staging bind-mount source and run AIR — no image build. When AIR's inotify watcher misses a change (always on WSL2 mounts, occasionally on native Linux across user namespaces) and the running binary lags a code change by more than ~5s, rebuild inside the **container** (use the stack-namespaced container name — worked example below uses the shipped defaults, `orkestra`/`development`; resolve your own from `docker/.env`):

```bash
docker exec orkestra-backend-development go build -o /app/tmp/main ./cmd/server/
docker restart orkestra-backend-development
```

## Service catalog (current repo, ADR-0006 core-only base)

Compose files that exist: `dev`, `staging`, `prod`, `infra`, `observability`. (ADR-0006 removed the `minimal`/`full` runtime-profile files; `dev-public`/`ai-sidecar` no longer exist either.)

| Service (uniform across dev/staging/prod) | Container (`${APP_NAME}-<svc>-${ENV}`) | Port (host→ctr) | Compose file |
| --- | --- | --- | --- |
| `backend` | `${APP_NAME}-backend-${ENV}` | `${BACKEND_PORT}`→3000 | dev / staging / prod |
| `frontend-admin` | `${APP_NAME}-frontend-admin-${ENV}` | `${FRONTEND_PORT}`→5173 (dev/staging) or →80 (prod) | dev / staging / prod |
| `client-frontend` | `${APP_NAME}-client-frontend-${ENV}` | `${CLIENT_FRONTEND_PORT}`→5173 | dev / staging (not prod) |
| `mongodb` | `${APP_NAME}-mongodb-${ENV}` | `${MONGO_PORT}`→27017 | infra |
| `redis` | `${APP_NAME}-redis-${ENV}` | `${REDIS_PORT}`→6379 | infra |
| `rustfs` | `${APP_NAME}-rustfs-${ENV}` | `${RUSTFS_API_PORT}`/`${RUSTFS_CONSOLE_PORT}` | infra |
| `grafana`/`loki`/`tempo`/`promtail`/`prometheus`/`otel-collector` | `${APP_NAME}-<svc>-${ENV}` | per `.env` (observability block) | observability (opt-in overlay) |

Detection is always authoritative over this table — the running stack is ground truth (files drift, see above).

## Fallback: bringing up a stack that isn't running

Label detection has nothing to read when no container exists. Then:

1. `grep -E "^(APP_NAME|ENV)=" docker/.env` — this checkout's stack identity.
2. `ls docker/docker-compose.*.yml` — files present on this host.
3. Map mode → file:

| Compose file | Use case |
| --- | --- |
| `docker-compose.dev.yml` | Local dev (AIR + Vite HMR, bind-mounted source) |
| `docker-compose.staging.yml` | Staging — on this repo also runs AIR + source mount |
| `docker-compose.prod.yml` | Production (real image build) |
| `docker-compose.infra.yml` | This stack's own MongoDB + Redis + RustFS (isolated per stack, not shared) |
| `docker-compose.observability.yml` | Grafana + Loki + Tempo + Promtail + Prometheus + OTel, layered into the same stack project |

Prefer `orkestra.sh deploy` (the `orkestra-stack` skill) for bring-up so `ORKESTRA_VERSION` is injected and `COMPOSE_PROJECT_NAME` is set correctly.

## NEVER do this

- **NEVER** `docker restart`/`docker stop` a compose-managed container to *cycle a service* — it bypasses compose's dependency graph. Use `docker compose -f ... restart <service>`. (The AIR force-rebuild above is the one sanctioned exception: restarting the container there picks up a hand-built binary, not to manage the service.)
- **NEVER** guess the compose file or stack from `ENV=` alone — it's a host label, not a routing decision, and multiple stacks (different `APP_NAME`) can coexist. Detect from labels (`orkestra.stack`, `com.docker.compose.*`).
- **NEVER** omit `--env-file docker/.env` — compose does not auto-discover `.env` when `-f` points at a file outside the current directory.
- **NEVER** `docker rm` a managed container — use `docker compose -f ... down <service>`.
- **NEVER** pass a bare service name (`backend`) to a raw `docker exec`/`docker restart`/`docker inspect` — those need the full stack-namespaced container name (`${APP_NAME}-backend-${ENV}`). Read it off the detect table.
- **NEVER** assume there is a shared `orkestra-network` to join — it was removed. Each stack has its own `${STACK}_default` network.
