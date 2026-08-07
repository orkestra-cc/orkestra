# Deploy health check + env-driven environment URLs

**Date:** 2026-08-07
**Status:** Approved, ready for implementation

## Problem

Two defects in `orkestra.sh`, both found while redeploying the staging stack.

### 1. The health-check seam has never had a script behind it

`orkestra.sh` looks for a health-check script in two locations:

```sh
if   [ -f "$DOCKER_DIR/health-check.sh" ];        then health_script="$DOCKER_DIR/health-check.sh"
elif [ -f "$PROJECT_ROOT/scripts/health-check.sh" ]; then health_script="$PROJECT_ROOT/scripts/health-check.sh"
fi
```

Neither file exists, and `git log --all -- '*health-check.sh'` returns nothing — **the
script has never existed in the repository's history.** The branch is dead by
construction, and it fails differently per environment:

- **staging** — `p_warn "Health check script not found, skipping..."`, then the deploy
  reports `Deployment successful`. The success banner is not evidence of health; a
  deploy that leaves the backend crash-looping still exits 0.
- **production** — the retry loop hits `p_err "Health check script not found"` and
  `break`s with `health_ok=false`, so `die` fires unconditionally. **A production
  deploy can never succeed.** It restarts the containers, then aborts before the
  post-deployment steps (image tagging, metadata).

A third, smaller bug sits in the same block: production captures the check output to
`/tmp/health_check_output.txt` but `die` never prints it, so the failure is opaque.

### 2. Environment URLs are hardcoded and wrong for staging

`set_env_config()` hardcodes `FRONTEND_URL`/`BACKEND_URL` per environment. Staging
gets `https://stage.orkestra.com`, which is not the staging host. The correct values
already exist in `docker/.env` (`FRONTEND_URL=https://staging.orkestra.cc`,
`BACKEND_URL=https://staging-api.orkestra.cc`) and `docker-compose.staging.yml`
already consumes them as `${FRONTEND_URL:-https://staging.orkestra.cc}`. Only
`orkestra.sh` disagrees. The hardcoded backend URL is also wrong in shape — the API
lives on its own host, not on a `/api` path.

## Design

### `scripts/health-check.sh` (new)

**Contract:** `bash scripts/health-check.sh <env> [scope]`

- `<env>` — `development` | `staging` | `production`. Already passed by both call sites.
- `[scope]` — new, optional, default `all`. The `status` command passes no scope and
  keeps working unchanged.
- **Exit 0** when every in-scope service is healthy; **exit 1** on the first in-scope
  failure. Out-of-scope failures print a warning and never affect the exit code.

**Stack resolution.** Reads `APP_NAME` from `docker/.env` (default `orkestra`) and
composes container names as `${APP_NAME}-<svc>-${ENV}`. It never assumes which
checkout it is inspecting: one host commonly runs several stacks from different
clones at once (an upstream checkout on `staging`, a fork's on `development`), and
`APP_NAME` is per-checkout and arbitrary, so a guessed name inspects the wrong
repository's containers. Sources `scripts/env-file.sh` for `env_get` rather than
reimplementing env parsing.

**Checks.**

| Service | Verification |
| --- | --- |
| `backend` | container running + Docker health status + `GET 127.0.0.1:$BACKEND_PORT/health` → 200 |
| `frontend-admin` | container running + `GET 127.0.0.1:$FRONTEND_PORT/health` → 200 |
| `client-frontend` | same on `$CLIENT_FRONTEND_PORT`; **skipped entirely** when the container does not exist |
| `mongodb`, `redis`, `rustfs` | Docker health status only, no HTTP |

`frontend-admin` defines no Docker healthcheck, so the HTTP probe is its only real
signal — this is why container-level checking alone was rejected. Its `/health`
returns the resolved `version`, which also proves `ORKESTRA_VERSION` was injected.

**Scope → in-scope services.**

| Scope | Hard-fails on |
| --- | --- |
| `backend` | `backend` |
| `frontend-admin` | `frontend-admin` |
| `frontend-admin+backend` | both |
| `infra` | `mongodb`, `redis`, `rustfs` |
| `all` (default) | every service present |

Everything outside the scope is still probed, but downgraded to a warning. This
attributes failures correctly — a backend-only deploy must not fail because of a
pre-existing, unrelated frontend problem — while still surfacing collateral damage.
It is not hypothetical: on the machine this was written on, a `client-frontend`
container had been `unhealthy` for 7 days (the known Vite IPv6 healthcheck trap),
and a whole-stack hard-fail policy would have blocked every deploy on that stack.

**Retries.** Container state is read once. HTTP probes retry 8 times at 3s intervals
(~21s), because Vite reports `ready in 12007 ms` on a cold start and a refused
connection fails instantly — a shorter budget just races the frontend's boot. (The
first draft used 3 attempts, which is only ~6s of real waiting and lost that race in
testing.) A healthy service answers on the first try, so the budget only costs time
when something is genuinely down. The existing 5×10s production retry loop stays in
`orkestra.sh` — the script itself is single-shot, so the two do not compound.

### `orkestra.sh` (modified)

1. **Env-driven URLs.** In `set_env_config()`, after the `case`, override
   `FRONTEND_URL`/`BACKEND_URL` from `docker/.env` when those keys are set. Correct
   the staging defaults to `https://staging.orkestra.cc` and
   `https://staging-api.orkestra.cc`.

   The **production default stays as-is** (`gestionale.orkestra.com`). It disagrees
   with `docker-compose.prod.yml` (`CONSOLE_HOST: console.orkestra.com`), but which
   one is the live domain cannot be verified from this checkout, and silently
   rewriting a production URL is worse than leaving a documented inconsistency.
   Making the value env-driven means production behaviour is unchanged unless
   `docker/.env` says otherwise — and if it does, that wins.

2. **Pass the scope.** Both call sites become
   `bash "$health_script" "$ENV" "$DEPLOY_SCOPE"`.

3. **Print the captured output on production failure**, so a failed production
   health check is diagnosable instead of silent.

### Documentation

Per the repo's commit doc-hygiene rule, the same commit updates:

- `scripts/CLAUDE.md` — document the new script, its contract and exit codes.
- the `orkestra-stack` skill — it currently describes health verification that never
  ran, and tells the reader to trust the deploy's success banner.

## Out of scope (addressed in a follow-up commit)

A separate stack-ownership documentation defect: the docs used a downstream fork's
container names as the canonical worked example and labelled them "this checkout's
dev stack" — false for the upstream checkout, false for any fresh clone, and it
leaked a private fork's identity into the base repo. Fixed immediately after this
change: names genericised to the `docker/.env.example` defaults, the false claim
removed, and `docker/CLAUDE.md` gained an explicit warning that a running container
does not belong to the checkout you are standing in.

## Testing

`scripts/` already carries shell tests (`test-env-file.sh`, `test-orkestra-helpers.sh`,
`test-init-delegation.sh`). Verification for this change:

1. Run `scripts/health-check.sh staging all` against the live, known-good staging
   stack — must exit 0 and report backend, frontend-admin and client-frontend healthy.
2. Run `scripts/health-check.sh staging backend` — must exit 0.
3. Stop `frontend-admin`, re-run scope `backend` (expect exit 0 with a warning) and
   scope `all` (expect exit 1). Restart it afterwards.
4. Confirm `set_env_config` resolves the staging URLs from `docker/.env` — the deploy
   banner must print `https://staging.orkestra.cc`, not `stage.orkestra.com`.
5. `bash -n` on both changed shell files.
