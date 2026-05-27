---
name: docker
description: Execute Docker container operations with correct environment detection. Use for restart, logs, up, down, and other docker compose commands.
---

# Docker Operations Skill

This skill ensures correct container operations by **auto-detecting** which compose file owns each service, instead of guessing from a single env var.

## Why detection is non-trivial

The `.env` file's `ENV=` value labels the **host environment** (`development` / `staging` / `production`). It does **not** uniquely determine the compose file in use:

- A host with `ENV=staging` can run `docker-compose.staging.yml` (which itself bind-mounts source + runs AIR — staging-acts-as-dev pattern on this repo).
- Multiple compose projects can run side-by-side: `orkestra-infra` (shared MongoDB + Redis) + `orkestra-observability` (Grafana stack) + a profile stack (`orkestra-staging` / `orkestra-full` / etc.) all coexist.
- Different SERVICES on the same host may live in different compose files (e.g. `mongodb` from `infra.yml`, `backend` from `staging.yml`).

So: never derive the compose file from `ENV=` alone. Derive it from **container labels** — what was actually launched.

## MANDATORY First Step — detect

Run this **once per session** (or whenever the running stack may have changed). It prints a structured summary and is fast (<50ms):

```bash
echo "=== Host ===" \
  && hostname \
  && (grep -qi microsoft /proc/version && echo "platform: WSL2 (AIR file-watcher unreliable — manual rebuild fallback may be needed)" || echo "platform: native Linux") \
  && echo "" \
  && echo "=== .env label ===" \
  && grep "^ENV=" /home/tore/orkestra/docker/.env 2>/dev/null \
  && echo "" \
  && echo "=== Running compose projects ===" \
  && docker compose ls 2>/dev/null \
  && echo "" \
  && echo "=== Service -> compose file (ground truth) ===" \
  && docker ps --format '{{.Label "com.docker.compose.service"}}  {{.Names}}  {{.Label "com.docker.compose.project.config_files"}}' 2>/dev/null \
       | awk -F'  ' '$1 != "" {printf "  %-22s %-26s %s\n", $1, $2, $3}' \
       | sort
```

The last block is the canonical lookup: it lists every running container's **service name** + **container name** + **compose file(s) it was launched from**. Use that table when picking the `-f` flag for any subsequent command.

## Detection cheatsheet

### Find the compose file for one service

```bash
service="backend"
compose=$(docker ps --format '{{.Label "com.docker.compose.service"}}|{{.Label "com.docker.compose.project.config_files"}}' \
  | awk -F'|' -v s="$service" '$1==s {print $2; exit}')
echo "$service -> $compose"
```

Returns a comma-separated list when a service was launched with multiple `-f` files (common for `mongodb` / `redis` which combine `infra.yml` + a profile file). Pass each as a separate `-f` flag when re-running compose against the same service.

### Build a compose invocation from the detected file(s)

```bash
# Single file
docker compose -f /home/tore/orkestra/docker/docker-compose.staging.yml --env-file /home/tore/orkestra/docker/.env <cmd> <service>

# Multi-file (mongodb / redis / shared infra)
docker compose \
  -f /home/tore/orkestra/docker/docker-compose.infra.yml \
  -f /home/tore/orkestra/docker/docker-compose.full.yml \
  --env-file /home/tore/orkestra/docker/.env <cmd> <service>
```

Working directory: `/home/tore/orkestra/docker` (or use absolute paths, as above).

## Fallback: bringing up a stack that isn't running

When no container exists yet, label-based detection has nothing to read. Fall back to:

1. `grep "^ENV=" /home/tore/orkestra/docker/.env` — host environment label.
2. `ls /home/tore/orkestra/docker/docker-compose.*.yml` — which compose files exist on this host.
3. Map the desired profile / mode to its compose file:

| Compose file                    | Use case                                            |
| ------------------------------- | --------------------------------------------------- |
| `docker-compose.dev.yml`        | Local dev (Chainguard images, AIR, Vite HMR)        |
| `docker-compose.dev-public.yml` | Dev with Alpine fallback (no dhi.io subscription)   |
| `docker-compose.staging.yml`    | Staging — on this repo, also runs AIR + source mount |
| `docker-compose.prod.yml`       | Production                                          |
| `docker-compose.infra.yml`      | Shared MongoDB + Redis (+ Gotenberg)                |
| `docker-compose.minimal.yml`    | Runtime profile: core only (no addons pre-enabled), GHCR image |
| `docker-compose.full.yml`       | Runtime profile: every non-dev addon pre-enabled, GHCR image   |
| `docker-compose.observability.yml` | Grafana + Loki + Tempo + Promtail + Prom + OTel  |
| `docker-compose.ai-sidecar.yml` | Split AI service (port 3100)                        |

## Common operations

Each example assumes you've already run the detection step and have `$compose` set to the right `-f` argument(s).

### Restart a service

```bash
docker compose -f "$compose" --env-file /home/tore/orkestra/docker/.env restart <service>
```

### Tail logs

```bash
docker compose -f "$compose" --env-file /home/tore/orkestra/docker/.env logs --tail 100 <service>
```

### Follow logs (stream)

Use `Monitor` rather than `Bash` for streaming so output reaches you live:

```bash
docker compose -f "$compose" --env-file /home/tore/orkestra/docker/.env logs -f <service>
```

### Status of the active stack

```bash
docker compose -f "$compose" --env-file /home/tore/orkestra/docker/.env ps
```

### Rebuild and restart one service

```bash
docker compose -f "$compose" --env-file /home/tore/orkestra/docker/.env up -d --build <service>
```

### Force-rebuild the Go backend when AIR misses a change

AIR's inotify-based file watcher is unreliable on WSL2 mounts and occasionally also misses changes on native Linux when the source directory is bind-mounted across user namespaces. If `git pull` lands a code change and the running binary doesn't reflect it within ~5 s:

```bash
docker exec orkestra-backend go build -o /app/tmp/main ./cmd/server/
docker restart orkestra-backend
```

(Container name is `orkestra-backend` on dev/staging compose; adjust if your stack uses a different naming scheme — check `docker ps`.)

## Service catalog (current repo)

Built-in services likely to appear in `docker ps`:

- **`backend`** → Go API server (port 3000)
- **`frontend-admin`** → Tier-1 React SPA (port 8080)
- **`client-frontend`** → Tier-2 React SPA (port 8081)
- **`mongodb`** → MongoDB 8.0 (shared, from `infra.yml`)
- **`redis`** → Redis 8.2 (shared, from `infra.yml`)
- **`gotenberg`** → PDF rendering (from `infra.yml`)
- **`rustfs`** → S3-compatible blob store for avatars
- **`ai-service`** → split AI sidecar (only when `ai-sidecar.yml` is up)
- **`grafana` / `loki` / `tempo` / `promtail` / `prometheus` / `otel-collector`** → observability stack (from `observability.yml`)

## NEVER do this

- **NEVER** run `docker restart <container>` or `docker stop <container>` directly when the container is compose-managed — it bypasses compose's dependency graph and leaves the project in an inconsistent state. Always `docker compose -f ... restart` instead.
- **NEVER** guess the compose file from `ENV=` alone — it's a label, not a routing decision. Use the detection step.
- **NEVER** omit `--env-file /home/tore/orkestra/docker/.env`. Compose has no automatic `.env` discovery when `-f` points at an absolute path.
- **NEVER** delete a managed container manually (`docker rm`). Use `docker compose -f ... down <service>`.
- **NEVER** mix profiles unintentionally — bringing up `docker-compose.full.yml` while `docker-compose.staging.yml` is already running creates two backend instances and port collisions.
