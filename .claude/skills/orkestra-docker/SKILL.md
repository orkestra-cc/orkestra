---
name: orkestra-docker
description: "Use for low-level per-service Docker surgery on the Orkestra stack — restart/logs/ps/up on a single service, AIR force-rebuild of the Go backend, infra-only ops, or figuring out which docker-compose.*.yml file owns a running container. NOT for full-stack deploy/rebuild/stop (use orkestra-stack + orkestra.sh for that)."
---

# Orkestra Docker Operations

Low-level, per-service container surgery on the Orkestra stack. This skill **auto-detects** which compose file owns each service from container labels, instead of guessing from `ENV=`.

> **For full-stack lifecycle use the `orkestra-stack` skill** (deploy / rebuild / restart / stop a whole app via `orkestra.sh`). It injects `ORKESTRA_VERSION` and runs preflight; a raw `docker compose up` here does NOT, leaving the frontend footer version stale. Use *this* skill only for: AIR force-rebuild, infra-only ops, single-service restart/logs, and compose-file detection.

## Why detection is non-trivial

`.env`'s `ENV=` labels the **host environment** (`development` / `staging` / `production`). It does **not** determine the compose file in use:

- A host with `ENV=staging` runs `docker-compose.staging.yml`, which itself bind-mounts source + runs AIR (staging-acts-as-dev on this repo).
- Multiple compose projects coexist: `orkestra-infra` (Mongo + Redis + RustFS) + `orkestra-observability` (Grafana stack) + the app project (`orkestra-staging` / `orkestra-dev`).
- Different services on one host live in different files: `mongodb` from `infra.yml`, `backend` from `staging.yml`.
- **Files drift from what's running.** A container's label points at the file it was *launched* from, which may no longer define it (e.g. a leftover `orkestra-gotenberg` container whose service was since removed from `infra.yml`). The running containers are ground truth; the files on disk are not.

So: never derive the compose file from `ENV=` alone. Derive it from **container labels** — what was actually launched.

## The one gotcha that bites: service name ≠ container name, and it varies by file

The `<service>` you pass to `docker compose` is **not** the same string across files:

| File | Backend service name | Container name (stable) |
| --- | --- | --- |
| `docker-compose.dev.yml` | `orkestra-backend` | `orkestra-backend` |
| `docker-compose.staging.yml` | `backend` | `orkestra-backend` |
| `docker-compose.prod.yml` | `backend` | `orkestra-backend` |

- `docker compose ... restart <service>` needs the **service** name (`backend` on staging, `orkestra-backend` on dev).
- `docker exec` / `docker restart` / `docker logs` on a raw container needs the **container** name (`orkestra-backend` everywhere).

Getting this wrong yields `no such service` or `no such container`. The detection step below prints both so you never guess.

## MANDATORY first step — detect

Run once per session (or whenever the running stack may have changed). Fast (<50ms):

```bash
echo "=== .env label ==="; grep "^ENV=" docker/.env 2>/dev/null
(grep -qi microsoft /proc/version && echo "platform: WSL2 (AIR watcher unreliable — manual rebuild fallback)" || echo "platform: native Linux")
echo "=== Running compose projects ==="; docker compose ls 2>/dev/null
echo "=== service | container | compose file (ground truth) ==="
docker ps --format '{{.Label "com.docker.compose.service"}}  {{.Names}}  {{.Label "com.docker.compose.project.config_files"}}' 2>/dev/null \
  | awk -F'  ' '$1 != "" {printf "  %-22s %-26s %s\n", $1, $2, $3}' | sort
```

The last block is the canonical lookup: **service name + container name + compose file(s)** for every running container. Use it to pick both the `-f` flag and the `<service>`/`<container>` argument for every subsequent command.

### Find the compose file for one service

```bash
service="backend"   # or orkestra-backend on a dev host — check the detect table
compose=$(docker ps --format '{{.Label "com.docker.compose.service"}}|{{.Label "com.docker.compose.project.config_files"}}' \
  | awk -F'|' -v s="$service" '$1==s {print $2; exit}')
echo "$service -> $compose"
```

Returns a comma-separated list when a service was launched with multiple `-f` files (common for `mongodb`/`redis`). Pass each as a separate `-f` flag.

## Compose invocation

```bash
# Paths below are relative to the repo root (the working directory). ALWAYS pass
# --env-file — compose does not auto-discover .env when -f points elsewhere.
docker compose \
  -f docker/docker-compose.staging.yml \
  --env-file docker/.env <cmd> <service>
```

## Common operations

Assume you ran detection and `$compose` holds the right `-f` argument(s) and `$svc` the right service name.

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

dev/staging bind-mount source and run AIR — no image build. When AIR's inotify watcher misses a change (always on WSL2 mounts, occasionally on native Linux across user namespaces) and the running binary lags a code change by more than ~5s, rebuild inside the **container** (use the container name, `orkestra-backend`):

```bash
docker exec orkestra-backend go build -o /app/tmp/main ./cmd/server/
docker restart orkestra-backend
```

## Service catalog (current repo, ADR-0006 core-only base)

Compose files that exist: `dev`, `staging`, `prod`, `infra`, `observability`. (ADR-0006 removed the `minimal`/`full` runtime-profile files; `dev-public`/`ai-sidecar` no longer exist either.)

| Service (staging/prod name) | Container | Port (host→ctr) | Compose file |
| --- | --- | --- | --- |
| `backend` (`orkestra-backend` on dev) | `orkestra-backend` | 3000→3000 | dev / staging / prod |
| `frontend-admin` | `orkestra-frontend-admin` | 8080→5173 | dev / staging / prod |
| `client-frontend` | `orkestra-client-frontend` | 8081→5173 | dev / staging (not prod) |
| `mongodb` | `orkestra-mongodb` | 27017 | infra |
| `redis` | `orkestra-redis` | 6379 | infra |
| `rustfs` | `orkestra-rustfs` | 9100/9101 | infra |
| `grafana`/`loki`/`tempo`/`promtail`/`prometheus`/`otel-collector` | `orkestra-*` | — | observability |

Detection is always authoritative over this table — the running stack is ground truth (see the gotenberg-orphan note above).

## Fallback: bringing up a stack that isn't running

Label detection has nothing to read when no container exists. Then:

1. `grep "^ENV=" docker/.env` — host label.
2. `ls docker/docker-compose.*.yml` — files present on this host.
3. Map mode → file:

| Compose file | Use case |
| --- | --- |
| `docker-compose.dev.yml` | Local dev (AIR + Vite HMR, bind-mounted source) |
| `docker-compose.staging.yml` | Staging — on this repo also runs AIR + source mount |
| `docker-compose.prod.yml` | Production (real image build) |
| `docker-compose.infra.yml` | Shared MongoDB + Redis + RustFS |
| `docker-compose.observability.yml` | Grafana + Loki + Tempo + Promtail + Prometheus + OTel |

Prefer `orkestra.sh deploy` (the `orkestra-stack` skill) for bring-up so `ORKESTRA_VERSION` is injected.

## NEVER do this

- **NEVER** `docker restart`/`docker stop` a compose-managed container to *cycle a service* — it bypasses compose's dependency graph. Use `docker compose -f ... restart <service>`. (The AIR force-rebuild above is the one sanctioned exception: `docker restart orkestra-backend` there restarts AIR to pick up a hand-built binary, not to manage the service.)
- **NEVER** guess the compose file from `ENV=` alone — it's a host label, not a routing decision. Detect from labels.
- **NEVER** omit `--env-file docker/.env` — compose does not auto-discover `.env` when `-f` points at a file outside the current directory.
- **NEVER** `docker rm` a managed container — use `docker compose -f ... down <service>`.
- **NEVER** assume the service name — it's `backend` on staging/prod but `orkestra-backend` on dev. Read it off the detect table.
