---
name: orkestra-api-test
description: "Use when calling or testing protected Orkestra backend API endpoints — minting a dev JWT (devtoken.sh / POST /dev/token), curl-ing /v1/* with a Bearer token, verifying RBAC for a role, testing operator vs client audience, or debugging 401 Unauthorized / 403 Forbidden / 421 Misdirected Request from the API."
---

# Orkestra API Testing (dev tokens)

`POST /dev/token` mints synthetic JWTs for API testing — implemented in `backend/internal/shared/devtoken/devtoken.go`, mounted on the operator root router, **refuses to register in production**. Tokens are for synthetic users (`<role>@orkestra.dev`); nothing is written to the database. Default expiry 15m, min 1m, max 24h.

All API paths are `/v1/...` — there is no `/api` prefix. The backend always listens on **port 3000**. Discover endpoints at `http://localhost:3000/docs` (Scalar UI), `/openapi.json`, or the committed spec `backend/openapi/enterprise.json` (`jq -r '.paths | keys[]' ...`).

## Golden rule: check ENV before curl-ing anything

The host mux (ADR-0003) dispatches by `Host` header. In `development` an unmatched host falls through to the operator mux; in `staging`/`production` it returns **421 Misdirected Request**. So:

```bash
grep -E "^(ENV|CONSOLE_HOST)=" docker/.env
```

| ENV | Plain `curl http://localhost:3000/...` | What to do |
| --- | --- | --- |
| `development` | works (dev fallthrough) | nothing special |
| `staging` | **421** on everything except `/health` + `/ready` (LAN-probe carve-out) | add `-H "Host: $CONSOLE_HOST"` to **every** request — token mint *and* API calls |

**`devtoken.sh` does not send a Host header**, so on a staging stack reached via `localhost` it dies with `Failed to extract token from response / 421 Misdirected Request`. That is the Host mux, not a broken backend — use the raw-curl recipe below. (Don't be fooled by its `/health` preflight passing: `/health` is exempt.)

## Recipe: mint a token and call an endpoint

Dev (`ENV=development`) — use the script:

```bash
./scripts/devtoken.sh administrator                    # pretty output + curl hint
TOKEN=$(./scripts/devtoken.sh admin --quiet)           # token only, shorthands work
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:3000/v1/users | jq .
```

Staging via localhost — raw curl with Host header (verified recipe):

```bash
HOST=$(grep "^CONSOLE_HOST=" docker/.env | cut -d= -f2)
TOKEN=$(curl -s -X POST http://localhost:3000/dev/token \
  -H "Host: $HOST" -H "Content-Type: application/json" \
  -d '{"role":"administrator"}' | jq -r .accessToken)
curl -s -H "Host: $HOST" -H "Authorization: Bearer $TOKEN" \
  http://localhost:3000/v1/users | jq .
```

`GET /dev/token/roles` lists valid roles + reports which environment the server thinks it is in — a cheap sanity probe.

## Request / response contract

`POST /dev/token` body (all optional except `role`):

| Field | Values | Notes |
| --- | --- | --- |
| `role` | `super_admin` \| `administrator` \| `developer` \| `manager` \| `operator` \| `guest` | highest → lowest |
| `audience` | `operator` (default) \| `client` | ADR-0003 PR-D: which JWT `aud` / surface the token is for |
| `expiry` | Go duration `"15m"`, `"1h"`, `"24h"` | default 15m; rejects <1m or >24h |
| `tenantUuid` | UUID | pins the acting tenant; omitted operator tokens are auto-stamped with the default internal tenant so tenant-scoped reads work |

Response: `accessToken`, `role`, `audience`, `email`, `tenant` (the stamped UUID), `expiresAt`, `expiresIn`, `curl` (ready-to-paste command).

`scripts/devtoken.sh` wraps all of this: role shorthands (`su`/`admin`/`dev`/`mgr`/`op`), `-q` quiet, `-c` curl output, `-e` expiry, `-t` tenant UUID, `-a` audience, `-u`/`ORKESTRA_API_URL` for the base URL. Run `./scripts/devtoken.sh --help` for details.

## Verifying RBAC

Test both directions — the role that should pass **and** a lower role that should be rejected:

```bash
# administrator → 200
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $ADMIN_TOKEN" http://localhost:3000/v1/users
# guest → 403 (RBAC working)
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $GUEST_TOKEN" http://localhost:3000/v1/users
```

Admin surfaces (`/v1/admin/...`) require `administrator`; `/v1/navigation` accepts any authenticated user. When asked to test a new endpoint, check its required permission in the handler rather than guessing from the path.

## Reading failures

| Response | Meaning |
| --- | --- |
| `401` | Token missing, expired, or invalid |
| `401 {"code":"audience_mismatch"}` | Cross-audience token — e.g. `audience: client` token sent to the console surface. Mint the right audience, don't chase auth bugs |
| `403` | Authenticated but role lacks the permission — RBAC doing its job |
| `421 Misdirected Request` | Host mux rejected the `Host` header (staging/prod). Add `-H "Host: $CONSOLE_HOST"` |
| `404` | No such route — check `/openapi.json`; remember paths are `/v1/*`, not `/api/v1/*` |
| `503` | Route belongs to a disabled module (`ModuleGate`) |

## Notes

- Dev tokens work in **development and staging only** — the handler double-gates against production.
- The operator console login page has a "Sign in with dev token" affordance backed by the same endpoint (first login on a fresh install).
- Use the **minimum role** that exercises the code path; reserve `super_admin` (wildcard, bypasses permission checks) for isolating whether a failure is RBAC vs the handler itself.
