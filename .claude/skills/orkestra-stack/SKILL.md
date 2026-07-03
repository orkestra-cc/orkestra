---
name: orkestra-stack
description: Deploy, rebuild, restart, stop, and inspect the Orkestra stack via orkestra.sh — the sanctioned lifecycle tool. Use for any "rebuild / redeploy / restart the backend or frontend", "bring the stack up/down", "deploy to staging", or "tail the logs" request. Prefer this over raw docker compose for full-stack lifecycle because orkestra.sh injects ORKESTRA_VERSION, runs preflight, and resolves the right compose file from ENV.
---

# Orkestra Stack Lifecycle (orkestra.sh)

`orkestra.sh` is the **sanctioned** way to deploy / restart / stop / inspect the stack. The project rule *"never start servers manually"* means: do not hand-roll `docker run` / `docker compose up`; drive lifecycle through `orkestra.sh`. For low-level, per-service compose surgery (force-rebuilding the AIR binary, infra-only ops, label detection) drop down to the **`orkestra-docker`** skill instead.

## Golden rule: use orkestra.sh, not raw `docker compose up`, for deploys

`orkestra.sh` computes `ORKESTRA_VERSION` from `git describe --tags --always --dirty` and exports it to compose on **every invocation**. A raw `docker compose up -d --build` does NOT — which silently leaves the frontend footer version stale or `"dev"`. The footer version is baked into the Vite dev-server at **frontend container (re)start** from `ORKESTRA_VERSION` (the container has no git), so the only way to refresh it is an orkestra.sh redeploy of the frontend.

## CLI surface (non-interactive)

```bash
# ENV comes from docker/.env (check it first); prefix to override: ENV=staging ./orkestra.sh ...
./orkestra.sh deploy --scope SCOPE [--rebuild] [--yes]
./orkestra.sh stop [--with-infra]
./orkestra.sh status
./orkestra.sh logs <service> [-f] [-n N] [-t]
./orkestra.sh observability up|down|reset|status|info|logs
./orkestra.sh init [--force] [--yes]      # first-time .env + JWT keys scaffold
./orkestra.sh -v                          # version + capabilities
```

Always check the target environment before deploying:

```bash
grep "^ENV=" docker/.env    # development | staging | production
```

`--yes` makes it non-interactive (required when driving from an agent). Run deploys in the background — they can take minutes — and read the output file when notified.

## Scopes — backend and frontend are SEPARATE

`--scope` values: `all` | `backend` | `frontend-admin` | `frontend-admin+backend` | `infra`.

**This is the #1 gotcha:** `--scope backend` restarts only the backend. The frontend container keeps running with whatever version/code it had at its last restart. To update everything use `--scope all`; to update both apps without touching infra use `--scope frontend-admin+backend`.

| You want to… | Command (ENV from docker/.env) |
| --- | --- |
| Pick up new backend code | `./orkestra.sh deploy --scope backend --yes` |
| Refresh frontend (incl. footer version) | `./orkestra.sh deploy --scope frontend-admin --yes` |
| Update both apps | `./orkestra.sh deploy --scope frontend-admin+backend --yes` |
| Full redeploy | `./orkestra.sh deploy --scope all --yes` |
| Stop apps (keep infra) | `./orkestra.sh stop --yes` |
| Stop everything | `./orkestra.sh stop --with-infra --yes` |

## Staging/dev stack is AIR-mounted — what `--rebuild` actually does

On this repo `docker-compose.staging.yml` (and dev) **bind-mount the source** and run AIR (backend) / Vite (frontend) — there is no `build:` context for those services. Consequences:

- `--rebuild` prints `level=warning msg="No services to build"` and then just **recreates the container**. That is expected, not an error. The container restart triggers AIR to recompile `/app/tmp/main` from the mounted (current-HEAD) source, and Vite to re-evaluate its config.
- So for these stacks you usually don't even need `--rebuild` — a plain `deploy --scope ...` recreates the container and recompiles. Use `--rebuild` only on stacks with a real image build (prod / GHCR profiles).

## Verifying a deploy

After the deploy reports success:

```bash
# Backend healthy + which binary is running
docker ps --filter name=orkestra-backend --format '{{.Status}}'
docker exec orkestra-backend sh -c 'ls -la --time-style=full-iso /app/tmp/main'   # mtime should be ~now

# Frontend resolved version (dev-server /health exposes APP_VERSION)
curl -s http://localhost:8080/health | jq .       # {"version":"0.3.6-3-g54f9090a", ...}
```

Notes:
- `ORKESTRA_VERSION` shaped like `0.3.6-3-g<sha>` means `main` has N commits after the last tag (e.g. release-workflow CHANGELOG + housekeeping merges). That's `git describe` working as intended, not a bug.
- After a **frontend** redeploy the browser still serves the cached bundle — tell the user to **hard-refresh (Ctrl+Shift+R)** to see the new footer version.
- `build-errors.log` inside the backend container can be a stale file — check its mtime before treating "exit status 1" lines as current failures.

## When to use the `orkestra-docker` skill instead

- Force-rebuilding the Go binary when AIR misses a change: `docker exec orkestra-backend go build -o /app/tmp/main ./cmd/server/ && docker compose ... restart backend`.
- Infra-only ops, multi-compose detection, inspecting which compose file owns a service.
- Anything `orkestra.sh` doesn't expose.

## Pushing the version into the footer (mechanism, for reference)

`frontend-admin/vite.config.js::resolveAppVersion()` priority: `GITHUB_REF_NAME` → `ORKESTRA_VERSION` → `git describe` → `"dev"`. The result becomes `import.meta.env.VITE_APP_VERSION`, read by `src/config.ts`, rendered by the footer with a cosmetic `v` prefix. Containers have no git, so `ORKESTRA_VERSION` (set by orkestra.sh) is the only live path.
