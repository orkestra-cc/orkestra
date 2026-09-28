---
name: orkestra-api-test
description: "Use when calling or testing protected Orkestra backend API endpoints — minting a dev JWT (devtoken.sh / POST /dev/token), curl-ing /v1/* with a Bearer token, verifying RBAC for a role, testing operator vs client audience, or debugging 401 Unauthorized / 403 Forbidden / 421 Misdirected Request from the API. Dev tokens exist in ENV=development ONLY — not on staging."
---

# Orkestra API Testing (dev tokens)

`POST /dev/token` mints synthetic JWTs for API testing — implemented in `backend/internal/shared/devtoken/devtoken.go`, mounted on the operator root router by `cmd/server/main.go`. Tokens are for synthetic users (`<role>@orkestra.dev`, `sub` = `dev-<role>-<unix>`); nothing is written to the database. Lifetime is the server's access-token TTL — **not** caller-controllable.

All API paths are `/v1/...` — there is no `/api` prefix. The backend always listens on **port 3000**. Discover endpoints at `http://localhost:3000/docs` (Scalar UI), `/openapi.json`, or the committed spec `backend/openapi/enterprise.json` (`jq -r '.paths | keys[]' ...`).

## Golden rule: dev tokens are development-only

The endpoint mints a signed `super_admin` token to **any anonymous caller**, so it is gated on `IsProductionLike()` (`production` **or** `staging`), enforced twice: `main.go` skips the wiring and `Handler.RegisterRoutes` refuses to register. Check first:

```bash
grep -E "^ENV=" docker/.env
```

| ENV | `/dev/token` | Plain `curl http://localhost:3000/v1/...` |
| --- | --- | --- |
| `development` | registered | works — an unmatched `Host` falls through to the operator mux |
| `staging` / `production` | **does not exist** → `401 authentication required` (`INVALID_CREDENTIALS`, the request lands on the auth middleware) | **421** on everything except `/health` + `/ready` (LAN-probe carve-out) — send `-H "Host: $CONSOLE_HOST"` |

That 401 on staging is **by design** (since `f8a6da0b4`, 2026-08-01), not a broken backend — don't debug it, and don't "fix" it by flipping `ENV`.

### Calling a staging API anyway

There is no anonymous token path on staging. Options, in order:

1. **Drive the operator console in the browser** (`integrated-browser-mcp`) with the user's real session. The SPA holds the access token in memory and the refresh token in an HttpOnly cookie, so `browser_eval` can't lift a bearer out of it — act through the UI.
2. **Service account** (ADR-0014) for machine calls: `POST /v1/auth/token` with `{"grantType":"client_credentials","clientId":"sa_...","clientSecret":"sas_..."}` on the console host → `aud: service` token, accepted by the operator mux. It can never hold `administrator`/`super_admin`, has no refresh token, and can never satisfy step-up (step-up routes answer `503`).
3. **Admin writes need the human.** `/v1/admin/modules/*` mutations sit behind `RequireMFA()` + `RequireLowRisk`; the console pops a "Confirm this action" TOTP dialog (the PATCH first returns 401). Only the user can type that code — ask them. **Never** write `module_configs` straight into Mongo to skip it: that bypasses MFA and the audit trail, and ConfigService's Redis cache keeps serving the old value for up to 30s.

## Recipe: mint a token and call an endpoint (development)

```bash
./scripts/devtoken.sh administrator                    # pretty output + curl hint
TOKEN=$(./scripts/devtoken.sh admin --quiet)           # token only, shorthands work
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:3000/v1/users | jq .

# raw equivalent
TOKEN=$(curl -s -X POST http://localhost:3000/dev/token \
  -H "Content-Type: application/json" \
  -d '{"role":"administrator"}' | jq -r .accessToken)
```

⚠️ `T=$(…)` swallows the script's non-zero exit: if `$TOKEN` is empty or `null`, the next call is a puzzling 401. Check it before blaming RBAC. On a host that pins `HOST_BIND_ADDRESS` to one interface, `localhost` is refused — use that address (`ORKESTRA_API_URL=http://<addr>:3000`).

`GET /dev/token/roles` lists valid roles + reports which environment the server thinks it is in — a cheap sanity probe (401 = not a development stack).

## Request / response contract

`POST /dev/token` body (all optional except `role`):

| Field | Values | Notes |
| --- | --- | --- |
| `role` | `super_admin` \| `administrator` \| `developer` \| `manager` \| `operator` \| `guest` | highest → lowest |
| `audience` | `operator` (default) \| `client` | ADR-0003 PR-D: which JWT `aud` / surface the token is for |
| `tenantUuid` | UUID | pins the acting tenant; omitted operator tokens are auto-stamped with the platform default internal tenant (none assigned → tenant-less token) |
| ~~`expiry`~~ | — | **rejected with 400**: the token always carries the server TTL (`JWT_ACCESS_TOKEN_EXPIRY` / admin `accessTokenTTL`) |

Response: `accessToken`, `role`, `audience`, `email`, `tenant` (the stamped UUID, omitted when none), `expiresAt` + `expiresIn` (read from the token's own `exp`), `curl` (ready-to-paste command).

`scripts/devtoken.sh` wraps all of this: role shorthands (`su`/`admin`/`dev`/`mgr`/`op`), `-q` quiet, `-c` curl output, `-t` tenant UUID, `-a` audience, `-u`/`ORKESTRA_API_URL` for the base URL. There is **no** `--expiry`. Run `./scripts/devtoken.sh --help` for details.

## What a dev token cannot do

Synthetic users carry no `amr`, no `last_otp_at` and no `auth_time`, and cannot re-login. So routes gated on step-up / recent authentication (e.g. MFA enrolment — see `backend/internal/core/auth/AGENTS.md`) answer `reauthentication_required` to a dev token by construction. Exercise those with a real login, not a dev token.

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
| `401` on `/dev/token` itself | Not a development stack — the route isn't registered (see golden rule) |
| `401` | Token missing, expired, invalid — or empty because the mint failed silently |
| `401 {"code":"audience_mismatch"}` | Cross-audience token — e.g. `audience: client` token sent to the console surface. Mint the right audience, don't chase auth bugs |
| `401` / `reauthentication_required` on a write | Step-up / MFA gate — needs a real, recently authenticated session |
| `403` | Authenticated but role lacks the permission — RBAC doing its job |
| `421 Misdirected Request` | Host mux rejected the `Host` header (staging/prod). Add `-H "Host: $CONSOLE_HOST"` |
| `404` | No such route — check `/openapi.json`; remember paths are `/v1/*`, not `/api/v1/*` |
| `503` | Route belongs to a disabled module (`ModuleGate`) — or a service account hit a step-up route |

## Notes

- There is **no** "Sign in with dev token" button in the console (`frontend-admin/src` has zero references to `/dev/token`). Dev tokens are a curl/script tool only; first login on a fresh install goes through the setup wizard.
- Use the **minimum role** that exercises the code path; reserve `super_admin` (wildcard, bypasses permission checks) for isolating whether a failure is RBAC vs the handler itself.
