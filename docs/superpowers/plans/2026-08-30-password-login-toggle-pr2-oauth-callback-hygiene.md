# Password-Login Toggle — PR 2: OAuth Callback Hygiene — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every web OAuth callback redirect to the SPA of the tier that started the flow, through one closed wire contract that carries no access token, refresh token, email or user ID; require a provider-verified email before any local email lookup; read the auto-link and provider toggles strictly from one consistent config read with per-provider failure granularity; and rework the operator console's callback page so it scrubs the URL before its first await, awaits a confirmed session, keeps the MFA challenge out of router state and honours only a fresh, sanitized return target.

**Architecture (v4.3):** Client-tier web OAuth completes through a **one-shot relay**: every provider callback lives on the operator host and cannot set a cookie for the client API host, so for `tier=client` the callback does the IdP half only (atomic state take, tier/provider cross-check, exchange, user info), stores an encrypted 60-second relay record and redirects to `GET {CLIENT_API_URL}/v1/auth/client/oauth/complete?relay=<id>`, where the client handler takes the record atomically, **requires** the state cookie its own host set at start, completes the application half, sets its own refresh cookie and redirects to the client SPA. The OAuth state itself becomes one-shot (`Take`), bound to the endpoint's provider, and the cross-host binding exception becomes a deferral that only that relay can discharge. The backend gains one additive SDK accessor (`ModuleConfigService.ActiveConfigRequiredModule` → `ActiveConfigView`, a single consistent read of a required module's active profile with every stored secret decrypt-checked). On top of it `services` gains the pure `ProviderStructurallyConfigured` predicate, the strict `OAuthConfigResolver.OAuthWebProviderUsable` / `UsableWebProviders` resolution, the strict `AuthPolicyService.OAuthAutoLinkByEmailEnabled` accessor and two sentinels (`ErrOAuthEmailUnverified`, `ErrAuthPolicyUnavailable`). `HandleOAuthCallbackWithLinking` refuses an unverified email for any unlinked identity **before** the email lookup. In `handlers`, every provider callback becomes a thin wrapper around one `completeOAuthCallback` flow that resolves trust (state → binding → tier) before it chooses a destination, and every SPA redirect is built by one `oauthLoginCallbackURL` / `oauthLinkReturnURL` pair in a single file that a structural AST test polices for forbidden parameters. The GitHub callback moves from Huma to the same raw handler (it never set the refresh cookie), the unregistered Huma Apple callback (`access_token=` in the URL) is deleted. `frontend-admin` extracts `MfaVerifyPanel` from `LoginMfaVerify`, rewrites `SocialAuthCallback` around a synchronously captured, immediately scrubbed outcome, an awaited `getSession` fetch and an allowlisted error-code → i18n map, and turns the OAuth return-target stash into a ten-minute take-and-delete record.

**Tech Stack:** Go 1.25.13, Huma v2 + chi, `go/ast` structural tests, `httptest`, fakes embedding the service interfaces; MongoDB 8 (`MONGO_TEST_URI`-guarded tests unchanged — this PR adds none); React 19 / RTK Query / react-router 8, Vitest + RTL + MSW (`onUnhandledRequest: 'error'`), i18n EN/IT parity test.

**Spec:** `docs/superpowers/specs/2026-08-29-password-login-toggle-design.md` **v4.3** (v4.1 + the PR 2 decisions and the four blocking review findings recorded in its §0 on 2026-08-30, committed on this branch). This plan implements the **PR 2** row of §7: §4.10 "Backend prerequisite and safe callback contract" + the `SocialAuthCallback` / `LoginMfaVerify` row; §4.4 "Structural, not has-a-client-ID" (`OAuthWebProviderUsable`, `ProviderStructurallyConfigured`, failure granularity), "Auto-link configuration" (strict `OAuthAutoLinkByEmailEnabled`) and "Verified email is mandatory before any email lookup"; §4.3's `ErrOAuthEmailUnverified` + `errcode.AuthOAuthEmailUnverified`; §5 rows 21, 22, 27, 28, 29; the §6 lines "OAuth auto-link tests", "handlers/oauth_callback_redirect_test.go", the provider-list / OAuth-start granularity sentence of `config_validation_test.go`, and the frontend-admin callback sentences. **It does not implement** the password toggle, the validator invariant, break-glass, `/policy` fields or `frontend-client` (PRs 3–4). Every file:line below is at `7574368a`; earlier tasks shift lines, so anchor on the code, not the number.

**Depends on:** nothing (§7: PR 2 "Depends on: —"). PR 1 is already merged into `dev`; this plan uses two of its artefacts — `ErrRequiredConfigMissing` / `GetRawValueRequiredModule` and `ErrConfigSecretUnreadable` / `secretPresent` semantics — without changing them.

## Global Constraints

- **Branch:** `git switch -c feat/auth-oauth-callback-hygiene dev` (`dev` = `7574368a`, 25 commits ahead of `origin/dev`, unpushed). PR 2 targets `dev`. PR 3 branches from `dev` after PR 2 merges.
- **Closed callback wire contract (spec §4.10), verbatim:** success query `?success=true&provider=<allowlisted-provider>`; failure query `?success=false&error=<allowlisted-stable-code>`; MFA continuation **fragment** `#requiresMfa=true&mfaToken=<one-shot-id>&webauthnAvailable=<bool>`. Parameter order inside a query/fragment is not part of the contract (`url.Values.Encode` sorts keys); the SPA parses, never string-matches.
- **Failure allowlist, closed:** `oauth_access_denied`, `oauth_signup_disabled`, `oauth_link_disabled`, `auth.oauth_email_unverified`, `oauth_provider_unavailable`, `oauth_login_failed`. Anything else collapses to `oauth_login_failed`. Account status and local lookup results are never encoded.
- **No callback URL may contain `access_token`, `refresh_token`, `email` or `user_id`** — not as a query key, a fragment key or a literal — in login mode, MFA mode or link mode. Success authentication is recovered only from the audience-scoped HttpOnly refresh cookie.
- **Every callback redirect sets `Referrer-Policy: no-referrer`** (and `Cache-Control: no-store`, deviation 7).
- **Trust before destination:** a missing, invalid, expired, replayed or unbound state is a terminal generic **400 with no redirect** (no trusted tier exists yet); only after a valid one-shot state is the IdP `error` interpreted, the code required, the provider resolved, or the application consulted — and each of those failures redirects to the **configured tier SPA** with an allowlisted code. The state cookie is cleared on every valid-state terminal outcome.
- **One-shot state, bound to provider, tier and browser (spec §4.10 v4.3):** the Redis row is consumed with the store's atomic `Take` (never `Get` + deferred delete); state resolution checks signature/expiry → browser binding → atomic take → `tier` → **`provider` against the endpoint's provider** → link-mode pair, every mismatch a generic 400, all before the IdP `error`, the code or any profile is read.
- **Client-tier flows never complete on the operator host.** A response from `console.*` cannot set a cookie for `api.*` (RFC 6265 §5.3 step 6 / §4.1.2.3) and the cross-tier isolation model has no shared parent domain, so for `tier=client` (login mode only — the link route is operator-only) the callback stores a one-shot **relay record** (tier, provider, the state's CSRF nonce, user-info map, provider tokens, security context, device info; encrypted at rest with `utils.EncryptOAuthToken`; `OAuthRelayTTL = 60s`; id from `GenerateOAuthCSRF`) and redirects to `{CLIENT_API_URL}/v1/auth/client/oauth/complete?relay=<id>`. That endpoint takes the record atomically, **requires** the `orkestra_oauth_state` cookie its host set at start to equal the record's nonce (constant time), refuses a missing/foreign cookie, a replay, a link-mode or wrong-tier record with a terminal 400 and no redirect, clears the cookie, then runs the shared completion and sets the refresh cookie on its own host. The cross-host "accept without binding" path is gone: a cross-host callback is *deferred* for a client-tier login and rejected for anything else.
- **`CLIENT_API_URL`** is the client API's public origin (new process-scoped env, `Server.Client.PublicURL`), falling back to `https://` + `CLIENT_API_HOST` in production-like environments and `http://` + host in development, empty when no client surface exists — then a client-tier state is refused at the callback.
- **Per-tier SPA URL:** operator handler → `OPERATOR_FRONTEND_URL`, client handler → `CLIENT_FRONTEND_URL`, each falling back to `FRONTEND_URL` — exactly the rule `module.go:1051-1053` / `1217-1219` already applies for `tierBundleDeps.frontendURL`; the handler receives that resolved value (`SetSPAURL`) rather than re-deriving it. The `Origin` header is never read to build a redirect or a stored `RedirectURI`.
- **Verified email before any email lookup:** for an identity with no existing `(provider, providerID)` link, `userInfo["email_verified"] == true` (a Go `bool`) is required before `GetUserByEmail`, before the auto-link policy is consulted, and before either the auto-link or signup branch; false/missing returns `ErrOAuthEmailUnverified` — 403 `auth.oauth_email_unverified` on JSON surfaces, `error=auth.oauth_email_unverified` on the web redirect — identically whether or not a local account exists. An existing provider-ID link logs in as today.
- **Strict boolean parsing** (spec §4.2 / §4.4): only canonical, case-insensitive `true` / `false` after trimming; an absent key gets the **schema default** (`oauthAutoLinkByEmail` → `true`; every `{provider}Enabled{Admin,Client}` → `false`); a present malformed or empty value is an error (auto-link) or makes that one provider unusable (toggle). `readBool` is never used on these keys.
- **Failure granularity (spec §4.4):** a **document-level** failure (missing `auth` document, repository error, a stored secret that cannot be decrypted) makes `GET /v1/auth/{tier}/providers` and web OAuth start return **503 `auth.policy_unavailable`**; a **per-provider** defect (malformed toggle, missing structural field, unreadable Apple key file) makes only that provider unusable — omitted from the list, **403 `auth.oauth_provider_disabled`** on its OAuth start, and a WARN naming the offending **key** (never a value).
- **Structural predicate, verbatim from §4.4:** `structural(p) := effectiveClientId(p) ≠ "" ∧ effectiveRedirectURL(p) ≠ "" ∧ secrets(p)` with `secrets(google|github|discord) := secretPresent[p+"ClientSecret"]` and `secrets(apple) := effectiveTeamId ≠ "" ∧ effectiveKeyId ≠ "" ∧ (secretPresent["applePrivateKey"] ∨ readableNonEmptyFile(effectivePrivateKeyPath))`. A path-backed Apple key counts only when the path identifies a readable regular file with non-empty content.
- **One config read per decision:** provider listing, OAuth start and the callback resolve everything — toggles, non-secret fields, secret presence **and the decrypted secret the provider is built from** — out of ONE `ActiveConfigView`; no check-then-reread.
- **SDK self-containment:** no file under `backend/pkg/sdk/` may import `backend/internal/*`. Verify with `grep -rn "internal/" backend/pkg/sdk/ --include="*.go"` before every commit — doc-comment hits only. The `Module` interface stays frozen; the only SDK change is additive (Task 1).
- **No secret value ever enters a log line, an error message, a redirect URL or a response.** WARNs carry key names only. The pure predicate receives secret **presence**, never a value.
- **Link mode keeps its own contract:** `/user/security?tab=oauth&link=success|failed&provider=<p>[&code=<allowlisted>]` — same SPA URL rule, same headers, same forbidden-field rule; never routed through the login callback state machine.
- **SPA rules (frontend-admin only; `orkestra-frontend-admin` skill applies):** RTK Query only (`authApi.endpoints.getSession.initiate`), `t()` for every string with EN **and** IT keys (parity test), path aliases without `@/`, react-router 8 (`react-router`), `useRef`/`useState` for component-memory state — never `location.state` for the OAuth MFA challenge, never raw callback text in JSX. The callback parser is **closed**: `provider` ∈ `google|apple|github|discord`, `webauthnAvailable` exactly `true`/`false`, `mfaToken` non-empty, and an MFA fragment combined with any query outcome — or `success=true` combined with `error` — is the generic failure. The OAuth return target is a `{target, createdAt}` record in `sessionStorage`, taken-and-deleted on every callback outcome **inside a layout effect, never during render**, and honoured only within **10 minutes** and after `sanitizeReturnTo`.
- **Docs move in the same commit as the code** (repo rule, `feedback_commit_doc_hygiene`): `backend/internal/core/auth/CLAUDE.md`, `docs/site/architecture/authentication-flow.mdx`, `docs/site/modules/core/auth.mdx`, `backend/pkg/sdk/CLAUDE.md`, `docs/site/sdk/config-service.mdx`, `frontend-admin/CLAUDE.md`, `backend/openapi/enterprise.json` — each in the task that changes what it documents; Task 10 is the cross-cutting sweep, not the first time they are touched.
- **Test commands** (absolute paths — `cd` drifts the shell between calls): backend `go test ./internal/core/auth/... ./pkg/sdk/module/... -count=1` from `/home/tore/orkestra/backend` after every step, `go vet ./...` before every commit (a `go build` does not compile `_test.go`); full gate `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true' make -C /home/tore/orkestra ci-backend` (0 SKIP with the `ork-errquality-ci-mongo` helper up). Frontend `cd /home/tore/orkestra/frontend-admin && npx vitest run src/components/authentication src/utils && npm run typecheck && npm run lint`; full gate `make -C /home/tore/orkestra ci-frontend-admin`. OpenAPI: `make -C /home/tore/orkestra/backend openapi-dump` (self-configures from `docker/.env` against the staging infra on `localhost:27017/6379` — `grep "^ENV=" docker/.env` first; the local stack is `orkestra-public-*-staging`). Docs render: fresh clone of `orkestra-docs`, `npm ci`, `MONOREPO_LOCAL_PATH=/home/tore/orkestra npm run sync` (**full** sync, not `sync:site`), `CI=true npm run build`.
- **Never start servers manually**; never `git push --tags`; never `--amend`; stage by path, never `git add -A`; commit with the `Claude-Session:` trailer the SDD run uses; the `conventional-pre-commit` hook rejects non-conventional subjects.
- **errquality (CI):** no `err.Error()` as a client-facing detail, no detail that merely repeats the status, no 4xx from the `default:` branch of an `errors.Is` switch. `mapOAuthError`'s default stays 500; the web mapping's default is the generic redirect code, not an HTTP error.

## Findings against `7574368a` that spec v4.1 did not state (recorded in v4.2 §0)

The spec was verified at `98911486`; four things differ at `7574368a` or were not visible from the spec's vantage. Each is folded into a numbered deviation below and approved in spec v4.2 §0, so the executor never decides it alone.

- **F1 — GitHub public-profile email is marked verified by assumption.** `services/github_oauth_service.go:185-192`: `email := user.Email; if email == "" { … getPrimaryEmail … } else { emailVerified = true // Email from profile is considered verified }`. §4.4 says GitHub "already sources the address correctly" and that "a public profile `email` is never marked verified by assumption" — the second sentence is the *rule*, and the code violates it whenever the profile carries a public email (a free-text field the user may set to any string). §6's test line ("GitHub ignores public-profile email and selects only the primary verified address returned by `/user/emails`") is what this plan implements (deviation 8).
- **F2 — The GitHub web callback never sets the refresh cookie.** `HandleGitHubCallback` (`auth_handler.go:1366-1439`) is a Huma handler that returns only a `Location` header ("Huma handlers can't set cookies directly"). After a GitHub login the SPA's `/v1/auth/session` finds no cookie and answers `authenticated:false` — the flow is broken today. Moving GitHub onto the shared raw handler fixes it as a side effect of "one callback implementation" (deviation 3).
- **F3 — `HandleAppleCallback` (Huma, `access_token=` in the URL, `auth_handler.go:1292-1363`) is not registered anywhere** (`RegisterOAuthRoutes` mounts only `HandleAppleCallbackHTTP`). It is dead code that would leak a token if ever mounted; the spec's "explicitly removes the legacy Apple Huma callback's `access_token=` query" is satisfied by deleting it and letting the structural scan keep it out (deviation 3).
- **F5 — The operator-host callback cannot set the client tier's refresh cookie.** `HandleGoogleCallbackHTTP` (`auth_handler.go:1002-1008`) writes `Set-Cookie … Domain=<CLIENT_COOKIE_DOMAIN>` from a response served by the operator host; RFC 6265 §5.3 step 6 makes the browser reject it, and `docs/site/operating/cookie-hardening-cross-tier.mdx:47-60` forbids the shared parent domain that would let it through. Client-tier web OAuth is broken today; the v4.2 plan preserved it. Fixed by the relay (deviations 17–21).
- **F6 — The cross-host binding exception is the client tier's normal path.** `oauth_state_binding.go:95-104` accepts a callback with no state cookie whenever `StartHost` differs from the callback host — every client-tier flow — so login/link CSRF was not prevented there. Fixed by deferring the check to the relay endpoint on the start host.
- **F7 — The OAuth state is not one-shot.** `oauth_state_service.go:157-187` reads with `Get` and deletes in a goroutine; two concurrent callbacks can both read it. The store already implements `Take` (Redis `GETDEL`, `:246`; memory store `:330`). Fixed by moving `ValidateOAuthState` onto `Take`, with a concurrent one-winner test and a replay test against the real service.
- **F8 — State and provider are not bound.** `resolveStateForCallback` (`auth_handler.go:824-860`) cross-checks tier, mode and link UUID but never `stateInfo.Provider` against the endpoint's provider. Fixed inside state resolution, before the IdP `error`, the code or any profile is read.
- **F4 — Apple `form_post` vs the `SameSite=Lax` state cookie (out of scope, reported).** Apple returns the callback as a cross-site `POST`; browsers do not attach a `SameSite=Lax` cookie to a cross-site POST, so `verifyOAuthStateBinding` rejects a same-host Apple flow ("no state cookie presented on the starting host"). Only the cross-host tier split (client tier) passes. Pre-existing, unrelated to this PR's contract, **not fixed here** — it needs its own decision (a `None` cookie for Apple only, or a GET-redirect bounce). Recorded so the staging round-trip in §7 step 7 is not misread as a PR 2 regression.

## Declared deviations from spec v4.1 (all approved in v4.2 §0 — read before executing)

1. **An additive SDK accessor lands in PR 2:** `ModuleConfigService.ActiveConfigRequiredModule(ctx, name) (*ActiveConfigView, error)` + the `ActiveConfigView` type + exported `NewActiveConfigView` (Task 1). §7's PR 2 row lists no SDK change, but §4.4 demands "runtime resolution comes from one required config read" and "OAuth start builds the provider from the same resolved value rather than checking and then rereading"; today `OAuthConfigResolver.Get` issues one `FindByName` per key (`GetValue` / `GetSecret`, `config_service.go:1084,1158`) and `GetSecret` silently falls back to env on a decrypt failure — neither "one read" nor "undecryptable document → 503" is expressible without a new accessor. Cost if wrong: an SDK surface a fork could depend on; it is additive and read-only.
2. **`ErrAuthPolicyUnavailable`, `errcode.AuthPolicyUnavailable` (503), `strictBool` and `configValueReader.GetRawValueRequiredModule` land in PR 2** (Task 2). The spec files them under §4.2/§4.3 (PR 3) but the strict auto-link accessor and the provider-toggle parser need them now; PR 3 reuses them unchanged. Cost if wrong: none — PR 3 would have added the same symbols.
3. **The GitHub callback becomes a raw chi handler (`HandleGitHubCallbackHTTP`) sharing `completeOAuthCallback`; the Huma operation `github-oauth-callback` leaves the OpenAPI document; `HandleAppleCallback`, `OAuthCallbackRequest`, `OAuthCallbackResponse`, `resolveOAuthMFAPartialRedirect` and `resolveOAuthLinkRedirect` are deleted** (Task 7, F2 + F3). Cost if wrong: a consumer generating a client from `enterprise.json` loses one operation that no client ever called (the IdP calls it); `openapi-check` sees the regenerated file in the same commit.
4. **The Apple dev-only "missing state" fallback (`auth_handler.go:1155-1184`) is removed.** Trust-before-destination admits no exception: a missing state is a terminal 400 in every environment. Cost if wrong: an Apple Service ID misconfigured to omit `state` fails loudly in dev instead of being papered over — which is what the fallback's own warning already told the developer to fix.
5. **The callback re-resolves provider usability through `OAuthWebProviderUsable`**, not only the start endpoint. A provider disabled or blanked between start and callback is refused with `oauth_provider_unavailable`; the provider is built from the same resolved config that answered the check. Cost if wrong: an operator flipping a provider off mid-flow sees one refused callback — the intended effect.
6. **Web-callback mapping of config uncertainty:** `ErrAuthPolicyUnavailable` (strict auto-link read) and a document-level resolver failure redirect with `oauth_provider_unavailable`; the JSON surfaces (mobile ID-token endpoints, `GET /providers`, OAuth start) return **503 `auth.policy_unavailable`**. §4.4 says "config uncertainty returns 503 before lookup" and §4.10 says every valid-state failure "redirect[s] … with an allowlisted coarse code"; a 302 cannot carry a 503, and the allowlist has no better word than "unavailable". Both surfaces fail closed before lookup/link/token issuance. Cost if wrong: the copy the user sees says "temporarily unavailable" — which is true.
7. **`Cache-Control: no-store` is set alongside `Referrer-Policy: no-referrer` on every callback redirect.** The redirect response carries `Set-Cookie` and a `Location` with a one-shot MFA id; `no-store` keeps both out of shared caches. Cost if wrong: none.
8. **GitHub `GetUserInfo` takes the address and its verified bit ONLY from `/user/emails`** (primary verified first, then any verified — the existing `getPrimaryEmail`); the public-profile `email` survives only as an **unverified** fallback when the endpoint yields nothing, and the callback then refuses to auto-link or sign up (F1). Cost if wrong: a GitHub user whose only address is unverified can no longer sign up via GitHub — the rule §4.4 states.
9. **SPA: only the OAuth path is router-state-free.** `MfaVerifyPanel` is extracted from `LoginMfaVerify`; `SocialAuthCallback` renders it locally from component memory. The password path's `LoginMfaVerify` page keeps reading `location.state` (a value that never travels in a URL) — `EmailPasswordForm` is PR 3's file. Cost if wrong: none for PR 2's threat (URL/history/referrer leakage of `mfaToken`).
10. **OAuth default landing becomes `DEFAULT_POST_LOGIN`** (`/user/dashboard`), replacing the hard-coded `/user/profile` — `frontend-admin/CLAUDE.md` already forbids hard-coding the post-login destination. Cost if wrong: a different first page after social login.
11. **Mobile ID-token endpoints keep the permissive `OAuthProviderEnabled` gate** (spec §4.4: "Native/mobile provider semantics remain outside this web-only change"); they gain only the two new sentinel mappings (unverified → 403 code, policy unavailable → 503 code) because they share `HandleOAuthCallbackWithLinking`. Cost if wrong: none — the JSON surface now names both outcomes instead of answering 500.
12. **Link-mode `code` allowlist gains `access_denied` and `provider_unavailable`** next to the existing `already_linked`, `duplicate_provider`, `invalid_userinfo`, `internal`, so link mode shares the trust-before-destination flow without inventing free-text codes. Cost if wrong: two more stable strings the `/user/security` page may map.
13. **`TestOAuthCallback_PropagatesEmailVerifiedFromIdP` is rewritten** (`gates_test.go:607-656`): its `claim_false` / `claim_missing` cases assert a user is created unverified, the exact behaviour §4.4 forbids. Cost if wrong: none — the spec decides.
14. **Provider tokens are stored uniformly** by one `providerTokens(*services.TokenResponse)`: Discord and GitHub now also copy empty `IDToken` / `RefreshToken` fields. No behavioural change (empty strings were already what a missing field stored).
15. **Two in-tree interfaces:** `OAuthConfigResolver` reads through a narrow `activeConfigReader` (`GetValue`, `GetSecret`, `ActiveConfigRequiredModule`) and `AuthHandler.oauthResolver` is typed as the new `services.OAuthResolver` interface. `NewOAuthConfigResolver(*module.ModuleConfigService)` and `NewAuthHandler(…)` keep their signatures (a concrete pointer satisfies the interface), so `module.go` does not change for this. Cost if wrong: none; it is what makes the handler and the resolver testable without Mongo.
17. **`ValidateOAuthState` consumes the row with the store's atomic `Take`** and drops the deferred-delete goroutine; the error text becomes "not found, expired or already used". In-tree change to the service; `MemoryOAuthStateStore` already implements `Take`, so the one-winner test runs against the real service.
18. **The relay record lives in the OAuth state store** (`oauth:relay:<id>`, `OAuthRelayTTL` 60 s) as `utils.EncryptOAuthToken(JSON)` — the same AES-256-GCM helper the provider tokens already use at rest — under an id minted by `GenerateOAuthCSRF`; `OAuthStateService` gains `StoreOAuthRelay` / `TakeOAuthRelay`. Cost if wrong: one more short-lived Redis key class per client-tier login.
19. **`CLIENT_API_URL` is a new process-scoped env** (`Server.Client.PublicURL`) with a derived fallback; `docker/.env.example` ships `http://api.localhost:3000` for the dev stack because the dev `CLIENT_API_HOST` has no port. Cost if wrong: a wrong derived origin sends the relay to the wrong host — the relay then 404s and no token is minted; the override is documented.
20. **`verifyOAuthStateBinding` returns `(deferred bool, err error)`**; the deferral is accepted only for a client-tier login state (the link route is mounted on the operator side only, `module.go:1643`), and a client-tier login is relayed **even when a cookie happens to be present on the operator host**, because the api.* cookie can only be set on api.*. The existing `TestOAuthStateBinding_AllowsCrossHostTierSplit` becomes `_DefersCrossHostTierSplit`.
21. **The relay endpoint is a raw chi route on the client mux** (`RegisterOAuthRelayRoute(ri.ClientRouter)`), mounted only when a client surface exists; it does not appear in OpenAPI (the browser is redirected to it, no client calls it).
16. **`StoreOAuthStateRequest.RedirectURI` is populated from `spaURL()` only** (`+ "/auth/callback"` for login, `+ "/user/security"` for link), never from the `Origin` header (§4.10 "populated only from the configured tier SPA … never concatenated from request input"). Nothing reads it back today; it stays for stored-state compatibility.

## File Structure

**Backend — `backend/pkg/sdk/module/`**

| File | Responsibility | Task |
|---|---|---|
| `config_active_view.go` (new) | `ActiveConfigView` (`Raw`, `Effective`, `Secret`, `SecretPresent`, `Revision`, `Module`), `NewActiveConfigView` | 1 |
| `config_service.go` | `ActiveConfigRequiredModule` next to `GetRawValueRequiredModule` | 1 |
| `config_active_view_test.go` (new) | view semantics, missing doc, repo error, undecryptable secret, live-schema fallbacks | 1 |

**Backend — `backend/internal/core/auth/services/`**

| File | Responsibility | Task |
|---|---|---|
| `auth_policy_service.go` | `ErrAuthPolicyUnavailable`, `strictBool`, `providerToggleKey`, `configValueReader.GetRawValueRequiredModule`, `OAuthAutoLinkByEmailEnabled` (replaces `OAuthAutoLinkByEmail`) | 2 |
| `auth_policy_service_test.go` | `stubReader.GetRawValueRequiredModule`, strict accessor tests | 2 |
| `auth_service.go` | `ErrOAuthEmailUnverified`; verified-email-before-lookup + strict auto-link in `HandleOAuthCallbackWithLinking` | 2, 4 |
| `oauth_provider_usability.go` (new) | `ProviderStructuralFields`, `KeyFileProbe`, `ReadableNonEmptyFile`, `ProviderStructurallyConfigured`, `OAuthResolver` interface, `activeConfigReader`, `OAuthWebProviderUsable`, `UsableWebProviders`, `providerConfigFrom` | 3 |
| `oauth_config_resolver.go` | reader interface + logger/probe fields; `Get` refactored onto `providerConfigFrom` | 3 |
| `oauth_provider_usability_test.go` (new) | predicate table, probe, resolver granularity, WARN names key only | 3 |
| `github_oauth_service.go` | email + verified bit from `/user/emails` only | 4 |
| `oauth_state_service.go` | `ValidateOAuthState` on `Take`; `OAuthRelayRecord`, `OAuthRelayTTL`, `StoreOAuthRelay` / `TakeOAuthRelay` | 6 |
| `oauth_state_service_test.go` (new) | replay rejected, N concurrent → one winner, relay round-trip / one-shot / expiry | 6 |
| `github_oauth_service_test.go` (new) | RoundTripper-driven `GetUserInfo` cases | 4 |
| `gates_fakes_test.go`, `gates_test.go`, `oauth_inactive_user_test.go` | lookup counter; unverified/policy tests; rewritten IdP-verified test; `email_verified:true` + policy on the email-matched inactive test | 4 |

**Backend — `backend/internal/core/auth/handlers/`**

| File | Responsibility | Task |
|---|---|---|
| `oauth_callback_redirect.go` (new) | allowlists, `oauthLoginResult` + constructors, `SetSPAURL` / `spaURL`, `oauthLoginCallbackURL`, `writeOAuthLoginRedirect`, `oauthLinkReturnURL`, `writeOAuthLinkRedirect`, `relayCompleteURL`, `writeRelayRedirect`, `setCallbackRedirectHeaders`, `oauthLoginErrorCode`, `sanitizeIdPError` | 5 |
| `oauth_callback_redirect_test.go` (new) | builder behaviour (green at its own commit) | 5 |
| `oauth_state_binding.go` | `verifyOAuthStateBinding` → `(deferred, err)`; `verifyRelayBinding` | 6 |
| `oauth_state_binding_test.go` | deferral instead of acceptance; relay binding | 6 |
| `auth_handler.go` | `resolveStateForCallback(ctx, raw, provider)` → `*stateResolution` with the provider cross-check (6); `spaBaseURL`; `oauthResolver services.OAuthResolver`; four thin callback handlers + `HandleOAuthRelayCompleteHTTP` + `RegisterOAuthRelayRoute`; deletions (deviation 3); `finishOAuthLinkRedirect` on the builder; `RegisterOAuthRoutes` GitHub raw (7); `InitiateOAuthLogin` / `InitiateOAuthLink` on `OAuthWebProviderUsable` + `spaURL`; `ListOAuthProviders` on `UsableWebProviders`; `oauthErrorResponseFor` / `mapOAuthError` for the two sentinels (8) | 6, 7, 8 |
| `oauth_callback_flow.go` (new) | `oauthCallbackParams`, `queryCallbackParams`, `formCallbackParams`, `oauthExchange`, `exchangeWithUserInfo`, `exchangeAppleIDToken`, `userInfoMap`, `providerTokens`, `completeOAuthCallback`, `finishOAuthCompletion` | 7 |
| `oauth_callback_flow_test.go` (new) | harness with fakes; tier routing, trust-before-destination, provider mismatch, relay deferral + relay endpoint, allowlist mapping, MFA fragment, link mode, Apple form-post, GitHub cookie, exact-key no-PII scan of every `Location` | 7 |
| `oauth_callback_scan_test.go` (new) | structural AST scan (forbidden params, callback literals confined to the builder file) | 7 |
| `structured_logging_safety_test.go` | parse both handler files; new function targets | 7 |
| `oauth_providers_handler_test.go` (new) | list 503 / omission; start 503 / 403 / stored `RedirectURI` from `spaURL` | 8 |
| `error_mapping_test.go` | delete the deleted helpers' tests (7); two new `mapOAuthError` rows (8) | 7, 8 |

**Backend — elsewhere**

| File | Responsibility | Task |
|---|---|---|
| `internal/shared/errcode/codes.go`, `codes_test.go` | `AuthPolicyUnavailable`, `AuthOAuthEmailUnverified` + golden rows | 2 |
| `internal/core/auth/module.go` | `SetSPAURL(opDeps.frontendURL)` / `SetSPAURL(clDeps.frontendURL)`; `RegisterOAuthRelayRoute(ri.ClientRouter)` | 7 |
| `internal/shared/config/config.go` | `AudienceConfig.PublicURL`, `CLIENT_API_URL` + derived fallback | 7 |
| `docker/.env.example` | `CLIENT_API_URL` | 7 |
| `docs/site/operating/cookie-hardening-cross-tier.mdx` | "Why OAuth relays to the client API host" | 7 |
| `openapi/enterprise.json` | regenerated (GitHub Huma op removed) | 7 |
| `pkg/sdk/CLAUDE.md`, `docs/site/sdk/config-service.mdx` | `ActiveConfigRequiredModule` | 1 |
| `internal/core/auth/CLAUDE.md` | owns-table rows, Resolver API, endpoint rows, callback contract + relay section, invariants, rules | 3, 4, 6, 7, 8, 10 |
| `docs/site/architecture/authentication-flow.mdx`, `docs/site/modules/core/auth.mdx` | web-flow steps 1/3/4/5, `/providers` line, callback contract + relay paragraph | 10 |

**Frontend — `frontend-admin/src/`**

| File | Responsibility | Task |
|---|---|---|
| `utils/socialAuthUtils.ts` | `stashOAuthReturnTo`, `takeOAuthReturnTo`, `OAUTH_RETURN_TO_TTL_MS`; `initiateSocialLogin` uses the stash | 9 |
| `utils/socialAuthUtils.test.ts` (new) | stash/take/TTL/sanitize | 9 |
| `utils/oauthCallbackParams.ts` (new) + `.test.ts` | closed `parseOAuthCallback`, `OAUTH_PROVIDERS`, `OAUTH_CALLBACK_ERROR_KEYS` | 9 |
| `components/authentication/MfaVerifyPanel.tsx` (new) | the verify form as a prop-driven panel | 9 |
| `components/authentication/LoginMfaVerify.tsx` | thin wrapper over the panel (password path) | 9 |
| `components/authentication/SocialAuthCallback.tsx` | capture → take + scrub in a layout effect → awaited session / local MFA / allowlisted error | 9 |
| `components/authentication/SocialAuthCallback.test.tsx` (new) | scrub-before-await, success, stash TTL, MFA local, signed-out, unavailable+retry, allowlist | 9 |
| `locales/en.json`, `locales/it.json` | `auth.social.callback.*` | 9 |
| `CLAUDE.md` | callback contract, return-target TTL, `MfaVerifyPanel` | 9 |

---

### Task 1: SDK — `ActiveConfigRequiredModule` and `ActiveConfigView`

**Files:**
- Create: `backend/pkg/sdk/module/config_active_view.go`
- Modify: `backend/pkg/sdk/module/config_service.go` (after `GetRawValueRequiredModule`, line 1154)
- Create: `backend/pkg/sdk/module/config_active_view_test.go`
- Modify: `backend/pkg/sdk/CLAUDE.md` (the `RequirePersistedConfig` bullet, line 349), `docs/site/sdk/config-service.mdx` ("Required documents", line 99)

**Interfaces:**
- Consumes: `fakeConfigRepo` (`recordlist_fake_repo_test.go` — `docs`, `findErr`), `withEncryptionKey(t)` + `testEncryptionKey` (`config_unmarshal_test.go`), `encryptSecret` / `decryptSecret` (`secrets.go`), `nonSecretValues` (`config_lanes.go:93`), `schemaFallbackValue` (`config_snapshot.go:88`), `ErrRequiredConfigMissing` (`config_service.go:525`), `ErrConfigSecretUnreadable` (`config_snapshot.go:15`), `schemaFor(name, doc)` (`config_service.go:70`).
- Produces: `type ActiveConfigView struct{…}` with `Module() string`, `Revision() int64`, `Raw(key) (string, bool)`, `Effective(key) string`, `Secret(key) string`, `SecretPresent(key) bool`; `func NewActiveConfigView(module string, schema []ConfigField, values, secrets map[string]string, revision int64) *ActiveConfigView`; `func (s *ModuleConfigService) ActiveConfigRequiredModule(ctx context.Context, name string) (*ActiveConfigView, error)`. Consumed by Task 3.

- [ ] **Step 1: Write the failing tests**

Create `backend/pkg/sdk/module/config_active_view_test.go`:

```go
package module

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// activeViewModule is the "auth"-shaped module the view tests register so
// the LIVE schema (not the stored one) supplies fallbacks and secret-ness.
type activeViewModule struct{ BaseModule }

var activeViewSchema = []ConfigField{
	{Key: "googleClientId", Type: FieldString},
	{Key: "googleClientSecret", Type: FieldSecret},
	{Key: "googleRedirectURL", Type: FieldString, Default: "https://default.example/cb"},
	{Key: "applePrivateKey", Type: FieldSecret, EnvVar: "TEST_ACTIVE_VIEW_APPLE_KEY"},
	{Key: "googleEnabledAdmin", Type: FieldBool, Default: "false"},
}

func (activeViewModule) Name() string                { return "auth" }
func (activeViewModule) Init(*Dependencies) error    { return nil }
func (activeViewModule) ConfigSchema() []ConfigField { return activeViewSchema }

func newActiveViewService(t *testing.T) (*ModuleConfigService, *fakeConfigRepo) {
	t.Helper()
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{activeViewModule{}})
	return svc, repo
}

func activeViewDoc(values, encrypted map[string]string, revision int64) *ModuleConfig {
	return &ModuleConfig{
		ModuleName: "auth", ActiveEnvironment: "production",
		// A deliberately STALE stored schema: the live one must win.
		ConfigSchema:   []ConfigField{{Key: "googleClientId", Type: FieldString}},
		ConfigValues:   map[string]string{}, EncryptedValues: map[string]string{},
		ConfigRevision: revision,
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: values, EncryptedValues: encrypted},
			"sandbox":    {ConfigValues: map[string]string{"googleClientId": "sandbox-id"}, EncryptedValues: map[string]string{}},
		},
	}
}

func TestActiveConfigRequiredModule_MissingDocumentIsAnError(t *testing.T) {
	svc, _ := newActiveViewService(t)
	_, err := svc.ActiveConfigRequiredModule(context.Background(), "auth")
	if !errors.Is(err, ErrRequiredConfigMissing) {
		t.Fatalf("err = %v, want ErrRequiredConfigMissing", err)
	}
}

func TestActiveConfigRequiredModule_RepositoryErrorPropagates(t *testing.T) {
	svc, repo := newActiveViewService(t)
	repo.findErr = errors.New("mongo down")
	_, err := svc.ActiveConfigRequiredModule(context.Background(), "auth")
	if err == nil || !strings.Contains(err.Error(), "mongo down") {
		t.Fatalf("err = %v, want the repository error", err)
	}
}

func TestActiveConfigRequiredModule_UndecryptableSecretIsDocumentLevel(t *testing.T) {
	svc, repo := newActiveViewService(t)
	repo.docs["auth"] = activeViewDoc(map[string]string{"googleClientId": "id"}, map[string]string{"googleClientSecret": "not-base64-ciphertext!"}, 3)
	_, err := svc.ActiveConfigRequiredModule(context.Background(), "auth")
	if !errors.Is(err, ErrConfigSecretUnreadable) {
		t.Fatalf("err = %v, want ErrConfigSecretUnreadable", err)
	}
	if !strings.Contains(err.Error(), "googleClientSecret") {
		t.Fatalf("the error must name the key: %v", err)
	}
	if strings.Contains(err.Error(), "not-base64-ciphertext!") {
		t.Fatalf("the error must not carry the stored value: %v", err)
	}
}

func TestActiveConfigRequiredModule_ViewSemantics(t *testing.T) {
	svc, repo := newActiveViewService(t)
	t.Setenv("TEST_ACTIVE_VIEW_APPLE_KEY", "env-pem")
	secret, _ := encryptSecret("shh")
	repo.docs["auth"] = activeViewDoc(
		map[string]string{
			"googleClientId":     "live-id",
			"googleRedirectURL":  "", // present but empty → Effective falls back to the schema Default
			"googleClientSecret": "plaintext-that-must-be-stripped",
			"googleEnabledAdmin": "true",
		},
		map[string]string{"googleClientSecret": secret, "applePrivateKey": ""},
		7,
	)
	view, err := svc.ActiveConfigRequiredModule(context.Background(), "auth")
	if err != nil {
		t.Fatal(err)
	}
	if view.Module() != "auth" || view.Revision() != 7 {
		t.Fatalf("module/revision = %q/%d", view.Module(), view.Revision())
	}
	if v, ok := view.Raw("googleClientId"); !ok || v != "live-id" {
		t.Fatalf("Raw(googleClientId) = %q,%v", v, ok)
	}
	if _, ok := view.Raw("googleEnabledClient"); ok {
		t.Fatal("an absent key must report absent, never a default")
	}
	if v, ok := view.Raw("googleRedirectURL"); !ok || v != "" {
		t.Fatalf("Raw must preserve present-but-empty: %q,%v", v, ok)
	}
	if got := view.Effective("googleRedirectURL"); got != "https://default.example/cb" {
		t.Fatalf("Effective(googleRedirectURL) = %q, want the LIVE schema default", got)
	}
	if got := view.Effective("googleClientId"); got != "live-id" {
		t.Fatalf("Effective(googleClientId) = %q", got)
	}
	if _, ok := view.Raw("googleClientSecret"); ok {
		t.Fatal("a plaintext under a schema-secret key must be stripped from the non-secret view")
	}
	if got := view.Secret("googleClientSecret"); got != "shh" {
		t.Fatalf("Secret(googleClientSecret) = %q, want the decrypted stored value", got)
	}
	if !view.SecretPresent("googleClientSecret") {
		t.Fatal("a non-empty decrypted secret is present")
	}
	if got := view.Secret("applePrivateKey"); got != "env-pem" {
		t.Fatalf("an empty stored ciphertext must fall back to EnvVar/Default like GetSecret: got %q", got)
	}
	if view.SecretPresent("githubClientSecret") {
		t.Fatal("an undeclared, unstored secret is absent")
	}
	if got := view.Effective("googleClientSecret"); got != "" {
		t.Fatalf("Effective must never surface a secret: %q", got)
	}
}

func TestActiveConfigRequiredModule_ReadsTheActiveProfileOnly(t *testing.T) {
	svc, repo := newActiveViewService(t)
	doc := activeViewDoc(map[string]string{"googleClientId": "prod-id"}, map[string]string{}, 1)
	doc.ActiveEnvironment = "sandbox"
	repo.docs["auth"] = doc
	view, err := svc.ActiveConfigRequiredModule(context.Background(), "auth")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := view.Raw("googleClientId"); v != "sandbox-id" {
		t.Fatalf("Raw(googleClientId) = %q, want the ACTIVE profile's value", v)
	}
}

func TestNewActiveConfigView_StripsSecretsAndCopies(t *testing.T) {
	values := map[string]string{"googleClientId": "id", "googleClientSecret": "leak"}
	secrets := map[string]string{"googleClientSecret": "shh"}
	view := NewActiveConfigView("auth", activeViewSchema, values, secrets, 0)
	if _, ok := view.Raw("googleClientSecret"); ok {
		t.Fatal("constructor must strip schema-secret keys from values")
	}
	secrets["googleClientSecret"] = "mutated"
	if view.Secret("googleClientSecret") != "shh" {
		t.Fatal("the view must own a copy of the secrets map")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/sdk/module/ -run 'ActiveConfig' -count=1`
Expected: FAIL — `undefined: ActiveConfigView`, `svc.ActiveConfigRequiredModule undefined`.

- [ ] **Step 3: Implement the view and the accessor**

Create `backend/pkg/sdk/module/config_active_view.go`:

```go
package module

import (
	"context"
	"fmt"
)

// ActiveConfigView is ONE consistent read of a required module's active
// profile: the non-secret values (schema-secret keys stripped), every stored
// secret already decrypted, and the document revision they were read at.
//
// It exists because a decision that spans several keys — "is this OAuth
// provider enabled, structurally complete, and what secret do I build it
// from?" — must not be assembled from N independent reads, each of which
// could observe a different document, and because a stored secret that no
// longer decrypts is a DOCUMENT-level outage, not a per-key "fall back to
// the env var" (which is what GetSecret does). The view is immutable and
// safe to share for the duration of one request.
type ActiveConfigView struct {
	module   string
	schema   []ConfigField
	values   map[string]string
	secrets  map[string]string
	revision int64
}

// NewActiveConfigView builds a view from already-resolved maps. Exported for
// tests and for a fork's fakes; production views come from
// ModuleConfigService.ActiveConfigRequiredModule. secrets are plaintext
// stored values keyed by schema key (an entry present with "" means "stored
// and decrypts to empty"); values are stripped of every schema-secret key by
// the constructor, so a legacy plaintext under a secret key can never be
// read back as a value. Both maps are copied.
func NewActiveConfigView(module string, schema []ConfigField, values, secrets map[string]string, revision int64) *ActiveConfigView {
	v := &ActiveConfigView{
		module:   module,
		schema:   schema,
		values:   nonSecretValues(schema, values),
		secrets:  make(map[string]string, len(secrets)),
		revision: revision,
	}
	if v.values == nil {
		v.values = map[string]string{}
	}
	for k, s := range secrets {
		v.secrets[k] = s
	}
	return v
}

// Module returns the module name the view was read for.
func (v *ActiveConfigView) Module() string { return v.module }

// Revision returns the document's configRevision at read time.
func (v *ActiveConfigView) Revision() int64 { return v.revision }

// Raw reports the stored non-secret value and whether the key is present —
// the GetRawValue contract: ("", false) is absent, (v, true) is present and
// v may legitimately be "". A schema-secret key is never present here.
func (v *ActiveConfigView) Raw(key string) (string, bool) {
	s, ok := v.values[key]
	return s, ok
}

// Effective is the GetValue rule for a non-secret key: a present non-empty
// stored value, else the schema's EnvVar-then-Default fallback, else "".
// A schema-secret key always answers "" — secrets are read via Secret.
func (v *ActiveConfigView) Effective(key string) string {
	if s, ok := v.values[key]; ok && s != "" {
		return s
	}
	for _, f := range v.schema {
		if f.Key != key {
			continue
		}
		if f.Type == FieldSecret {
			return ""
		}
		return schemaFallbackValue(f)
	}
	return ""
}

// Secret is the GetSecret rule: a stored secret (even one that decrypts to
// ""), else the schema's EnvVar-then-Default fallback, else "". The
// constructor's callers store only NON-EMPTY ciphertexts, so — exactly like
// GetSecret — a key whose ciphertext was cleared to "" falls back.
func (v *ActiveConfigView) Secret(key string) string {
	if s, ok := v.secrets[key]; ok {
		return s
	}
	for _, f := range v.schema {
		if f.Key == key && f.Type == FieldSecret {
			return schemaFallbackValue(f)
		}
	}
	return ""
}

// SecretPresent reports whether Secret(key) is non-empty — the "presence"
// the structural predicates consume. The value itself never leaves the view
// except through Secret.
func (v *ActiveConfigView) SecretPresent(key string) bool { return v.Secret(key) != "" }

// ActiveConfigRequiredModule reads a module's document ONCE and returns a
// consistent ActiveConfigView of its active profile. Like
// GetRawValueRequiredModule it treats a missing document as the ERROR
// outcome (ErrRequiredConfigMissing) and never calls GetConfig's lazy-seed
// path. Every non-empty stored ciphertext in the active profile is decrypted
// up front: one that cannot be decrypted fails the whole read with
// ErrConfigSecretUnreadable naming the key, because a caller governing
// credentials must never build a provider from an env-var fallback while the
// operator believes the stored secret is in force. Fallbacks (EnvVar/Default)
// and secret-ness come from the LIVE schema of the registered module, not
// the stored copy, so a schema that gained a key after the document was
// written still answers correctly.
func (s *ModuleConfigService) ActiveConfigRequiredModule(ctx context.Context, name string) (*ActiveConfigView, error) {
	doc, err := s.repo.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("%w: %q", ErrRequiredConfigMissing, name)
	}
	secrets := map[string]string{}
	for key, enc := range doc.ActiveEncryptedValues() {
		if enc == "" {
			continue
		}
		plain, err := decryptSecret(enc)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrConfigSecretUnreadable, key)
		}
		secrets[key] = plain
	}
	return NewActiveConfigView(name, s.schemaFor(name, doc), doc.ActiveConfigValues(), secrets, doc.ConfigRevision), nil
}
```

The file above already contains `ActiveConfigRequiredModule`; do **not** add a second copy to `config_service.go` — instead add a one-line pointer comment under `GetRawValueRequiredModule` (line 1154):

```go
// ActiveConfigRequiredModule (config_active_view.go) is the multi-key
// counterpart: one read, every secret decrypt-checked, live-schema fallbacks.
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /home/tore/orkestra/backend && go test ./pkg/sdk/module/ -count=1 && go vet ./pkg/sdk/... && grep -rn "internal/" pkg/sdk/ --include="*.go" | grep -v '^\S*:\s*//' | grep -v '_test.go' || true`
Expected: `ok`, vet clean, the grep prints nothing but doc-comment hits.

- [ ] **Step 5: Document the accessor**

In `backend/pkg/sdk/CLAUDE.md`, after the `RequirePersistedConfig` bullet (ends "…`cmd/server/admin_wiring.go`)."), add:

```markdown
- **`ActiveConfigRequiredModule(ctx, name)` is the multi-key strict reader** —
  ONE repository read returning an immutable `ActiveConfigView` of the active
  profile: `Raw(key)` (presence-aware, schema-secret keys stripped),
  `Effective(key)` (GetValue's present-non-empty-else-EnvVar/Default rule),
  `Secret(key)` / `SecretPresent(key)` (GetSecret's rule, decrypted once at
  read time) and `Revision()`. A missing document is `ErrRequiredConfigMissing`
  and a stored ciphertext that no longer decrypts fails the WHOLE read with
  `ErrConfigSecretUnreadable` naming the key — a document-level outage, never
  a silent env-var fallback. Fallbacks and secret-ness come from the LIVE
  schema. `auth` reads every OAuth provider decision (toggle, structural
  fields, the secret the provider is built from) out of one view so a check
  and the value it guards can never observe two different documents.
  `NewActiveConfigView` is exported for a fork's fakes.
```

In `docs/site/sdk/config-service.mdx`, at the end of "Required documents" (after "…so boot seeding runs."), add:

```markdown

A module that must decide something across several keys at once — the auth module's "is this OAuth provider enabled *and* structurally complete, and which secret do I build it from" — reads them through `ActiveConfigRequiredModule`, which returns one consistent, read-once view of the active profile with every stored secret decrypted up front. A secret that no longer decrypts fails that read as an outage instead of quietly substituting the environment-variable fallback the way single-key reads do.
```

- [ ] **Step 6: Commit**

```bash
cd /home/tore/orkestra && git add backend/pkg/sdk/module/config_active_view.go backend/pkg/sdk/module/config_active_view_test.go backend/pkg/sdk/module/config_service.go backend/pkg/sdk/CLAUDE.md docs/site/sdk/config-service.mdx
git commit -m "feat(sdk): ActiveConfigRequiredModule — one consistent, decrypt-checked read of a required module's active profile"
```

---

### Task 2: Sentinels, error codes and the strict auto-link accessor

**Files:**
- Modify: `backend/internal/core/auth/services/auth_policy_service.go` (`configValueReader` lines 48-64; `OAuthProviderEnabled` lines 478-500; `OAuthAutoLinkByEmail` lines 628-638)
- Modify: `backend/internal/core/auth/services/auth_policy_service_test.go` (`stubReader` lines 15-45; `TestOAuthAutoLinkByEmail_DefaultsTrue` line 690)
- Modify: `backend/internal/core/auth/services/auth_service.go` (sentinel block, after `ErrOAuthLinkDisabled` line 60)
- Modify: `backend/internal/shared/errcode/codes.go` (after `AuthOAuthProviderDisabled`, line 65), `codes_test.go` (`goldenCodes`)

**Interfaces:**
- Consumes: `module.ErrRequiredConfigMissing`, `ModuleConfigService.GetRawValueRequiredModule` (PR 1).
- Produces: `var ErrAuthPolicyUnavailable`, `var ErrOAuthEmailUnverified`; `func strictBool(raw string) (bool, error)`; `func providerToggleKey(audience PolicyAudience, provider string) (key string, known bool)`; `configValueReader.GetRawValueRequiredModule(ctx, moduleName, key string) (string, bool, error)`; `func (s *AuthPolicyService) OAuthAutoLinkByEmailEnabled(ctx context.Context) (bool, error)` (replaces `OAuthAutoLinkByEmail`); `errcode.AuthPolicyUnavailable = "auth.policy_unavailable"`, `errcode.AuthOAuthEmailUnverified = "auth.oauth_email_unverified"`. Consumed by Tasks 3–8.

- [ ] **Step 1: Write the failing tests**

In `backend/internal/core/auth/services/auth_policy_service_test.go`, replace the `stubReader` type + its two methods (lines 15-45) with:

```go
// stubReader satisfies the configValueReader interface used by
// AuthPolicyService. Tests inject keyed values directly so no Mongo
// or Redis is required.
type stubReader struct {
	values map[string]string
	// rawErr, when set, makes GetRawValue and GetRawValueRequiredModule
	// report a failed read. It stands in for an unreachable module_configs
	// collection.
	rawErr error
	// requiredMissing models the auth document being absent: the strict
	// readers must report it as an outage, never as "absent key".
	requiredMissing bool
}

func (s *stubReader) GetValue(_ context.Context, _, key string) string {
	if s == nil {
		return ""
	}
	return s.values[key]
}

// GetRawValue mirrors ModuleConfigService.GetRawValue's presence contract: a
// map lookup naturally distinguishes "key present with empty value" (ok=true)
// from "key absent" (ok=false), so a nil/missing-key map value expresses
// "absent" and an explicit empty-string entry expresses "operator cleared it".
//
// A non-nil rawErr is the THIRD state the real accessor reports and the two
// above cannot express: the read failed, so nothing is known about the key.
func (s *stubReader) GetRawValue(_ context.Context, _, key string) (string, bool, error) {
	if s == nil {
		return "", false, nil
	}
	if s.rawErr != nil {
		return "", false, s.rawErr
	}
	v, ok := s.values[key]
	return v, ok, nil
}

// GetRawValueRequiredModule mirrors the strict accessor: a missing document
// is the ERROR outcome (module.ErrRequiredConfigMissing), never "absent".
func (s *stubReader) GetRawValueRequiredModule(_ context.Context, _, key string) (string, bool, error) {
	if s == nil {
		return "", false, errors.New("nil reader")
	}
	if s.rawErr != nil {
		return "", false, s.rawErr
	}
	if s.requiredMissing {
		return "", false, module.ErrRequiredConfigMissing
	}
	v, ok := s.values[key]
	return v, ok, nil
}
```

Add `"errors"` and `"github.com/orkestra/backend/pkg/sdk/module"` to that file's imports. Replace `TestOAuthAutoLinkByEmail_DefaultsTrue` (line 690-702) with:

```go
func TestOAuthAutoLinkByEmailEnabled_Strict(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		policy  *AuthPolicyService
		want    bool
		wantErr bool
	}{
		{"nil policy is an outage, never true", nil, false, true},
		{"absent key → schema default true", newPolicy(nil), true, false},
		{"explicit false", newPolicy(map[string]string{"oauthAutoLinkByEmail": "false"}), false, false},
		{"canonical true with case and space", newPolicy(map[string]string{"oauthAutoLinkByEmail": " TRUE "}), true, false},
		{"malformed value is an error, not the default", newPolicy(map[string]string{"oauthAutoLinkByEmail": "treu"}), false, true},
		{"present-empty is an error", newPolicy(map[string]string{"oauthAutoLinkByEmail": ""}), false, true},
		{"readBool's '1'/'yes' are NOT accepted", newPolicy(map[string]string{"oauthAutoLinkByEmail": "yes"}), false, true},
		{"read failure", &AuthPolicyService{cs: &stubReader{rawErr: errors.New("mongo down")}}, false, true},
		{"missing auth document", &AuthPolicyService{cs: &stubReader{requiredMissing: true}}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.policy.OAuthAutoLinkByEmailEnabled(ctx)
			if tc.wantErr {
				if !errors.Is(err, ErrAuthPolicyUnavailable) {
					t.Fatalf("err = %v, want ErrAuthPolicyUnavailable", err)
				}
				if got {
					t.Fatal("an error must never come with true")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %v, %v; want %v, nil", got, err, tc.want)
			}
		})
	}
}

func TestStrictBool(t *testing.T) {
	for raw, want := range map[string]bool{"true": true, "TRUE": true, " False ": false, "false": false} {
		got, err := strictBool(raw)
		if err != nil || got != want {
			t.Errorf("strictBool(%q) = %v, %v; want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "1", "0", "yes", "no", "treu", "t"} {
		if _, err := strictBool(raw); err == nil {
			t.Errorf("strictBool(%q) must reject", raw)
		}
	}
}

func TestProviderToggleKey(t *testing.T) {
	if k, ok := providerToggleKey(PolicyAudienceOperator, " GitHub "); !ok || k != "githubEnabledAdmin" {
		t.Fatalf("got %q,%v", k, ok)
	}
	if k, ok := providerToggleKey(PolicyAudienceClient, "apple"); !ok || k != "appleEnabledClient" {
		t.Fatalf("got %q,%v", k, ok)
	}
	if _, ok := providerToggleKey(PolicyAudienceOperator, "facebook"); ok {
		t.Fatal("unknown provider must not resolve to a key")
	}
}
```

In `backend/internal/shared/errcode/codes_test.go`, add two rows to `goldenCodes` after `"AuthOAuthProviderDisabled"`:

```go
	"AuthPolicyUnavailable":              "auth.policy_unavailable",
	"AuthOAuthEmailUnverified":           "auth.oauth_email_unverified",
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/services/ -run 'OAuthAutoLink|StrictBool|ProviderToggleKey' -count=1; go test ./internal/shared/errcode/ -count=1`
Expected: services FAIL to compile (`undefined: strictBool`, `ErrAuthPolicyUnavailable`, `OAuthAutoLinkByEmailEnabled`); errcode FAIL (`TestCodesMatchGoldenSnapshot`: golden names a const that does not exist).

- [ ] **Step 3: Add the codes and sentinels**

In `backend/internal/shared/errcode/codes.go`, after `AuthOAuthProviderDisabled` (line 65):

```go
// AuthPolicyUnavailable signals that an admin-managed sign-in policy — or
// the auth configuration document it lives in — could not be read or
// parsed. The decision fails closed, never open, so the caller retries
// later rather than being granted a permissive default. 503.
const AuthPolicyUnavailable = "auth.policy_unavailable"

// AuthOAuthEmailUnverified signals that an OAuth identity with no existing
// link presented an email the identity provider did not mark verified, so
// it may neither auto-link to a local account nor sign up. Returned before
// any local email lookup and identically whether or not such an account
// exists — it must not become an account-existence oracle. 403 on JSON
// surfaces; the same string is the web callback's `error=` code.
const AuthOAuthEmailUnverified = "auth.oauth_email_unverified"
```

In `backend/internal/core/auth/services/auth_service.go`, after `ErrOAuthLinkDisabled` (line 60):

```go
	// ErrOAuthEmailUnverified signals that an OAuth identity with no
	// existing (provider, providerID) link arrived with an email the IdP
	// did not mark verified. It is returned BEFORE the local email lookup,
	// so the caller learns nothing about whether an account exists.
	ErrOAuthEmailUnverified = errors.New("oauth provider did not verify the email")
```

In `backend/internal/core/auth/services/auth_policy_service.go`:

(a) add `"errors"` and `"fmt"` to the imports; after the `const (...)` block that ends with `defaultPasswordResetTokenTTL` add:

```go
// ErrAuthPolicyUnavailable is returned by every STRICT policy accessor when
// the answer cannot be established: nil service, nil reader, missing auth
// document, repository failure, or a present value that is not a canonical
// boolean. Callers map it to 503 auth.policy_unavailable and never
// substitute a default — an outage must not re-enable anything.
var ErrAuthPolicyUnavailable = errors.New("auth policy unavailable")

// strictBool accepts only canonical, case-insensitive "true" / "false" after
// trimming. It deliberately does NOT accept readBool's "1"/"yes": an
// out-of-band "treu" or "" must surface as an error, never as a default.
func strictBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("not a canonical boolean")
}

// providerToggleKey maps (audience, provider) to the schema key of the
// per-surface enable toggle. Unknown provider names resolve to nothing —
// admin-managed lookups never fall through to "allow" for a typo.
func providerToggleKey(audience PolicyAudience, provider string) (string, bool) {
	suffix := "Admin"
	if audience == PolicyAudienceClient {
		suffix = "Client"
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "google":
		return "googleEnabled" + suffix, true
	case "apple":
		return "appleEnabled" + suffix, true
	case "github":
		return "githubEnabled" + suffix, true
	case "discord":
		return "discordEnabled" + suffix, true
	}
	return "", false
}
```

(b) extend `configValueReader` (line 48-64) with the strict method, keeping the two existing ones:

```go
	// GetRawValueRequiredModule is GetRawValue for a module whose document
	// must exist: a missing document is the ERROR outcome
	// (module.ErrRequiredConfigMissing), never "absent". The strict
	// accessors below read through it so an outage can never be mistaken
	// for "the operator said nothing here".
	GetRawValueRequiredModule(ctx context.Context, moduleName, key string) (string, bool, error)
```

(c) rewrite `OAuthProviderEnabled` (lines 478-500) onto the shared key mapper — behaviour unchanged (this permissive accessor is what the mobile ID-token endpoints keep using; the web path moves to the strict resolver in Task 3):

```go
// OAuthProviderEnabled reports whether the given provider is exposed
// on the audience's surface. Defaults to true when unset so existing
// deployments preserve behaviour after the schema migration. Unknown
// provider names always return false — admin-managed lookups do not
// fall through to "allow" for typos.
//
// PERMISSIVE by design and kept for the native/mobile ID-token endpoints
// only. The web flow (provider list, OAuth start, callback) uses
// OAuthConfigResolver.OAuthWebProviderUsable, which parses the same key
// strictly with the schema default (false) for an absent key.
func (s *AuthPolicyService) OAuthProviderEnabled(ctx context.Context, audience PolicyAudience, provider string) bool {
	key, known := providerToggleKey(audience, provider)
	if !known {
		return false
	}
	if s == nil || s.cs == nil {
		return true
	}
	return readBool(s.cs.GetValue(ctx, "auth", key), true)
}
```

(d) replace `OAuthAutoLinkByEmail` (lines 628-638) with:

```go
// OAuthAutoLinkByEmailEnabled reports whether the OAuth callback may
// auto-attach a provider to an existing account with the same VERIFIED
// email. STRICT (spec §4.4): a nil service, a missing auth document, a
// read failure or a malformed/empty present value is ErrAuthPolicyUnavailable
// — the callback then answers 503 before any lookup, link or token
// issuance. An absent key means the schema default, true (possible only for
// a fork that skips boot seeding).
func (s *AuthPolicyService) OAuthAutoLinkByEmailEnabled(ctx context.Context) (bool, error) {
	if s == nil || s.cs == nil {
		return false, fmt.Errorf("%w: policy service not wired", ErrAuthPolicyUnavailable)
	}
	raw, ok, err := s.cs.GetRawValueRequiredModule(ctx, "auth", "oauthAutoLinkByEmail")
	if err != nil {
		return false, fmt.Errorf("%w: read oauthAutoLinkByEmail: %w", ErrAuthPolicyUnavailable, err)
	}
	if !ok {
		return true, nil
	}
	v, err := strictBool(raw)
	if err != nil {
		return false, fmt.Errorf("%w: oauthAutoLinkByEmail is not a canonical boolean", ErrAuthPolicyUnavailable)
	}
	return v, nil
}
```

The only in-tree caller of the removed `OAuthAutoLinkByEmail` is `auth_service.go:1980`; Task 4 rewrites that branch. To keep the package compiling **now**, change that line to:

```go
			autoLink, autoLinkErr := s.policy.OAuthAutoLinkByEmailEnabled(ctx)
			if autoLinkErr != nil {
				return nil, autoLinkErr
			}
			if !autoLink {
				return nil, ErrOAuthLinkDisabled
			}
```

(Task 4 moves this read above the lookup; the interim placement keeps `TestOAuthCallback_AutoLinkDisabled_ReturnsErr` green.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/... ./internal/shared/errcode/ -count=1 && go vet ./...`
Expected: `ok` for every package. If `oauth_inactive_user_test.go`'s `RejectsInactiveEmailMatchedUser` fails with `ErrAuthPolicyUnavailable` (its `authService` literal has no `policy`), add `policy: newPolicy(nil),` to that struct literal (line 117) — Task 4 touches the same test again.

- [ ] **Step 5: Commit**

```bash
cd /home/tore/orkestra && git add backend/internal/core/auth/services/auth_policy_service.go backend/internal/core/auth/services/auth_policy_service_test.go backend/internal/core/auth/services/auth_service.go backend/internal/core/auth/services/oauth_inactive_user_test.go backend/internal/shared/errcode/codes.go backend/internal/shared/errcode/codes_test.go
git commit -m "feat(auth): strict OAuthAutoLinkByEmailEnabled, ErrAuthPolicyUnavailable / ErrOAuthEmailUnverified sentinels and their error codes"
```

---

### Task 3: Provider usability — `ProviderStructurallyConfigured`, `OAuthWebProviderUsable`, `UsableWebProviders`

**Files:**
- Create: `backend/internal/core/auth/services/oauth_provider_usability.go`
- Modify: `backend/internal/core/auth/services/oauth_config_resolver.go` (struct + constructor lines 14-23; `Get` lines 29-104)
- Create: `backend/internal/core/auth/services/oauth_provider_usability_test.go`
- Modify: `backend/internal/core/auth/CLAUDE.md` ("Resolver API" table, line 402-411)

**Interfaces:**
- Consumes: `module.ActiveConfigView` (Task 1), `strictBool`, `providerToggleKey`, `ErrAuthPolicyUnavailable` (Task 2), `OAuthProviderConfig` (`oauth_provider_interface.go:99`).
- Produces: `type ProviderStructuralFields struct{ClientID, RedirectURL string; SecretPresent bool; TeamID, KeyID, PrivateKeyPath string}`; `type KeyFileProbe func(path string) bool`; `func ReadableNonEmptyFile(path string) bool`; `func ProviderStructurallyConfigured(p models.OAuthProvider, f ProviderStructuralFields, probe KeyFileProbe) (missing string, ok bool)`; `type OAuthResolver interface{ Get; RedirectURL; MobileAudience; ConfiguredProviders; OAuthWebProviderUsable(ctx, audience PolicyAudience, p models.OAuthProvider) (*OAuthProviderConfig, bool, error); UsableWebProviders(ctx, audience PolicyAudience) ([]models.OAuthProvider, error) }`; `var WebProviderOrder = []models.OAuthProvider{Google, Apple, GitHub, Discord}`. Consumed by Tasks 7–8.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/services/oauth_provider_usability_test.go`:

```go
package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func TestProviderStructurallyConfigured(t *testing.T) {
	full := ProviderStructuralFields{ClientID: "id", RedirectURL: "https://x/cb", SecretPresent: true, TeamID: "team", KeyID: "key"}
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	cases := []struct {
		name    string
		p       models.OAuthProvider
		f       ProviderStructuralFields
		probe   KeyFileProbe
		missing string
	}{
		{"google complete", models.OAuthProviderGoogle, full, no, ""},
		{"google no client id", models.OAuthProviderGoogle, ProviderStructuralFields{RedirectURL: "u", SecretPresent: true}, no, "googleClientId"},
		{"github no redirect", models.OAuthProviderGitHub, ProviderStructuralFields{ClientID: "id", SecretPresent: true}, no, "githubRedirectURL"},
		{"discord no secret", models.OAuthProviderDiscord, ProviderStructuralFields{ClientID: "id", RedirectURL: "u"}, no, "discordClientSecret"},
		{"apple inline key", models.OAuthProviderApple, full, no, ""},
		{"apple path-backed key", models.OAuthProviderApple, ProviderStructuralFields{ClientID: "id", RedirectURL: "u", TeamID: "t", KeyID: "k", PrivateKeyPath: "/k.p8"}, yes, ""},
		{"apple unreadable path and no inline key", models.OAuthProviderApple, ProviderStructuralFields{ClientID: "id", RedirectURL: "u", TeamID: "t", KeyID: "k", PrivateKeyPath: "/k.p8"}, no, "applePrivateKey"},
		{"apple nil probe never counts a path", models.OAuthProviderApple, ProviderStructuralFields{ClientID: "id", RedirectURL: "u", TeamID: "t", KeyID: "k", PrivateKeyPath: "/k.p8"}, nil, "applePrivateKey"},
		{"apple no team", models.OAuthProviderApple, ProviderStructuralFields{ClientID: "id", RedirectURL: "u", KeyID: "k", SecretPresent: true}, no, "appleTeamId"},
		{"apple no key id", models.OAuthProviderApple, ProviderStructuralFields{ClientID: "id", RedirectURL: "u", TeamID: "t", SecretPresent: true}, no, "appleKeyId"},
		{"unknown provider", models.OAuthProvider("facebook"), full, yes, "provider"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			missing, ok := ProviderStructurallyConfigured(tc.p, tc.f, tc.probe)
			if ok != (tc.missing == "") || missing != tc.missing {
				t.Fatalf("got missing=%q ok=%v, want missing=%q", missing, ok, tc.missing)
			}
		})
	}
}

func TestReadableNonEmptyFile(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "key.p8")
	if err := os.WriteFile(full, []byte("-----BEGIN PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.p8")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !ReadableNonEmptyFile(full) {
		t.Error("a readable non-empty regular file counts")
	}
	if ReadableNonEmptyFile(empty) {
		t.Error("an empty file does not count")
	}
	if ReadableNonEmptyFile(dir) {
		t.Error("a directory does not count")
	}
	if ReadableNonEmptyFile(filepath.Join(dir, "missing.p8")) {
		t.Error("a missing file does not count")
	}
	if ReadableNonEmptyFile("") {
		t.Error("an empty path does not count")
	}
}

// fakeActiveConfigReader hands the resolver a prebuilt view (or an error).
type fakeActiveConfigReader struct {
	view *module.ActiveConfigView
	err  error
}

func (f *fakeActiveConfigReader) GetValue(context.Context, string, string) string  { return "" }
func (f *fakeActiveConfigReader) GetSecret(context.Context, string, string) string { return "" }
func (f *fakeActiveConfigReader) ActiveConfigRequiredModule(context.Context, string) (*module.ActiveConfigView, error) {
	return f.view, f.err
}

var usabilitySchema = []module.ConfigField{
	{Key: "googleEnabledAdmin", Type: module.FieldBool, Default: "false"},
	{Key: "googleEnabledClient", Type: module.FieldBool, Default: "false"},
	{Key: "githubEnabledAdmin", Type: module.FieldBool, Default: "false"},
	{Key: "appleEnabledAdmin", Type: module.FieldBool, Default: "false"},
	{Key: "discordEnabledAdmin", Type: module.FieldBool, Default: "false"},
	{Key: "googleClientId", Type: module.FieldString},
	{Key: "googleClientSecret", Type: module.FieldSecret},
	{Key: "googleRedirectURL", Type: module.FieldString},
	{Key: "githubClientId", Type: module.FieldString},
	{Key: "githubClientSecret", Type: module.FieldSecret},
	{Key: "githubRedirectURL", Type: module.FieldString},
	{Key: "appleClientId", Type: module.FieldString},
	{Key: "appleTeamId", Type: module.FieldString},
	{Key: "appleKeyId", Type: module.FieldString},
	{Key: "applePrivateKey", Type: module.FieldSecret},
	{Key: "applePrivateKeyPath", Type: module.FieldString},
	{Key: "appleRedirectURL", Type: module.FieldString},
}

func usabilityResolver(view *module.ActiveConfigView, err error, probe KeyFileProbe) (*OAuthConfigResolver, *bytes.Buffer) {
	var buf bytes.Buffer
	r := &OAuthConfigResolver{
		cs:     &fakeActiveConfigReader{view: view, err: err},
		logger: slog.New(slog.NewTextHandler(&buf, nil)),
		probe:  probe,
	}
	return r, &buf
}

func googleView(values map[string]string, secrets map[string]string) *module.ActiveConfigView {
	base := map[string]string{"googleEnabledAdmin": "true", "googleClientId": "g-id", "googleRedirectURL": "https://console/cb"}
	for k, v := range values {
		base[k] = v
	}
	sec := map[string]string{"googleClientSecret": "g-secret-value"}
	for k, v := range secrets {
		sec[k] = v
	}
	return module.NewActiveConfigView("auth", usabilitySchema, base, sec, 1)
}

func TestOAuthWebProviderUsable_DocumentLevelFailureIsAnError(t *testing.T) {
	r, _ := usabilityResolver(nil, errors.New("mongo down"), nil)
	_, _, err := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderGoogle)
	if !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("err = %v, want ErrAuthPolicyUnavailable", err)
	}
	if _, err := r.UsableWebProviders(context.Background(), PolicyAudienceOperator); !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("list: err = %v, want ErrAuthPolicyUnavailable", err)
	}
	var nilResolver *OAuthConfigResolver
	if _, _, err := nilResolver.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderGoogle); !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("nil resolver: err = %v", err)
	}
}

func TestOAuthWebProviderUsable_UsableProviderReturnsResolvedConfig(t *testing.T) {
	r, _ := usabilityResolver(googleView(nil, nil), nil, nil)
	cfg, ok, err := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderGoogle)
	if err != nil || !ok || cfg == nil {
		t.Fatalf("got cfg=%v ok=%v err=%v", cfg, ok, err)
	}
	if cfg.ClientID != "g-id" || cfg.ClientSecret != "g-secret-value" || cfg.AdditionalConfig["redirect_url"] != "https://console/cb" {
		t.Fatalf("config must be built from the SAME view: %+v", cfg)
	}
}

func TestOAuthWebProviderUsable_PerProviderDefectsAreNotErrors(t *testing.T) {
	structural := map[string]string{"googleClientId": "g-id", "googleRedirectURL": "https://console/cb"}
	secret := map[string]string{"googleClientSecret": "g-secret-value"}
	cases := []struct {
		name    string
		view    *module.ActiveConfigView
		wantKey string // expected in the WARN; "" = no WARN at all
	}{
		{"absent toggle → schema default false, no WARN", module.NewActiveConfigView("auth", usabilitySchema, structural, secret, 1), ""},
		{"toggle false", googleView(map[string]string{"googleEnabledAdmin": "false"}, nil), ""},
		{"malformed toggle names the key", googleView(map[string]string{"googleEnabledAdmin": "treu"}, nil), "googleEnabledAdmin"},
		{"readBool-style '1' is malformed", googleView(map[string]string{"googleEnabledAdmin": "1"}, nil), "googleEnabledAdmin"},
		{"present-empty toggle is malformed", googleView(map[string]string{"googleEnabledAdmin": ""}, nil), "googleEnabledAdmin"},
		{"missing client id", googleView(map[string]string{"googleClientId": ""}, nil), "googleClientId"},
		{"missing redirect", googleView(map[string]string{"googleRedirectURL": ""}, nil), "googleRedirectURL"},
		{"missing secret", googleView(nil, map[string]string{"googleClientSecret": ""}), "googleClientSecret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, logs := usabilityResolver(tc.view, nil, nil)
			cfg, ok, err := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderGoogle)
			if err != nil || ok || cfg != nil {
				t.Fatalf("got cfg=%v ok=%v err=%v; want (nil,false,nil)", cfg, ok, err)
			}
			if tc.wantKey == "" {
				if strings.Contains(logs.String(), "level=WARN") {
					t.Fatalf("no WARN expected: %s", logs.String())
				}
				return
			}
			if !strings.Contains(logs.String(), tc.wantKey) {
				t.Fatalf("WARN must name %q: %s", tc.wantKey, logs.String())
			}
			if strings.Contains(logs.String(), "g-secret-value") || strings.Contains(logs.String(), "g-id") {
				t.Fatalf("WARN must carry key names only: %s", logs.String())
			}
		})
	}
}

func TestOAuthWebProviderUsable_AudienceIsolation(t *testing.T) {
	view := googleView(map[string]string{"googleEnabledAdmin": "false", "googleEnabledClient": "true"}, nil)
	r, _ := usabilityResolver(view, nil, nil)
	if _, ok, _ := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderGoogle); ok {
		t.Fatal("operator surface must be off")
	}
	if _, ok, _ := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceClient, models.OAuthProviderGoogle); !ok {
		t.Fatal("client surface must be on")
	}
}

func TestOAuthWebProviderUsable_ApplePathBackedKeyUsesProbe(t *testing.T) {
	values := map[string]string{
		"appleEnabledAdmin": "true", "appleClientId": "a-id", "appleTeamId": "team", "appleKeyId": "key",
		"appleRedirectURL": "https://console/apple", "applePrivateKeyPath": "/etc/apple/key.p8",
	}
	view := module.NewActiveConfigView("auth", usabilitySchema, values, nil, 1)
	var probed string
	r, _ := usabilityResolver(view, nil, func(p string) bool { probed = p; return true })
	cfg, ok, err := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderApple)
	if err != nil || !ok {
		t.Fatalf("got ok=%v err=%v", ok, err)
	}
	if probed != "/etc/apple/key.p8" || cfg.AdditionalConfig["private_key_path"] != "/etc/apple/key.p8" {
		t.Fatalf("probe path %q, cfg %+v", probed, cfg.AdditionalConfig)
	}
	r, logs := usabilityResolver(view, nil, func(string) bool { return false })
	if _, ok, _ := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderApple); ok {
		t.Fatal("unreadable key file must make apple unusable")
	}
	if !strings.Contains(logs.String(), "applePrivateKey") {
		t.Fatalf("WARN must name applePrivateKey: %s", logs.String())
	}
}

func TestUsableWebProviders_ListsOnlyUsableInCanonicalOrder(t *testing.T) {
	values := map[string]string{
		"discordEnabledAdmin": "true", // enabled but no client id → omitted
		"githubEnabledAdmin":  "treu", // malformed → omitted, WARN
		"googleEnabledAdmin":  "true", "googleClientId": "g", "googleRedirectURL": "u",
		"appleEnabledAdmin": "true", "appleClientId": "a", "appleTeamId": "t", "appleKeyId": "k", "appleRedirectURL": "u",
	}
	secrets := map[string]string{"googleClientSecret": "s", "applePrivateKey": "pem"}
	r, logs := usabilityResolver(module.NewActiveConfigView("auth", usabilitySchema, values, secrets, 1), nil, nil)
	got, err := r.UsableWebProviders(context.Background(), PolicyAudienceOperator)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != models.OAuthProviderGoogle || got[1] != models.OAuthProviderApple {
		t.Fatalf("got %v, want [google apple]", got)
	}
	if !strings.Contains(logs.String(), "githubEnabledAdmin") || !strings.Contains(logs.String(), "discordClientId") {
		t.Fatalf("WARNs must name both defects: %s", logs.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core/auth/services/ -run 'ProviderStructurally|ReadableNonEmpty|OAuthWebProviderUsable|UsableWebProviders' -count=1`
Expected: FAIL to compile — `undefined: ProviderStructuralFields`, `r.logger undefined`, …

- [ ] **Step 3: Implement the predicate and the strict resolver**

Create `backend/internal/core/auth/services/oauth_provider_usability.go`:

```go
package services

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// ProviderStructuralFields are the effective (fallback-resolved) values the
// web OAuth flow needs for one provider. Secrets travel as PRESENCE only —
// the predicate never sees a secret value, so it can be logged, tested and
// reused by the PR 3 validator without a secret crossing any boundary.
type ProviderStructuralFields struct {
	ClientID    string
	RedirectURL string
	// SecretPresent is the client secret for google/github/discord and the
	// inline PEM (applePrivateKey) for apple.
	SecretPresent bool
	// Apple only.
	TeamID         string
	KeyID          string
	PrivateKeyPath string
}

// KeyFileProbe reports whether path names a readable regular file with
// non-empty content. Injected so the pure predicate is testable without a
// filesystem; ReadableNonEmptyFile is the production probe. A nil probe
// never counts a path.
type KeyFileProbe func(path string) bool

// ReadableNonEmptyFile is the production KeyFileProbe (spec §4.4: "a
// path-backed Apple key counts only when the path identifies a readable
// regular file with non-empty content"). PEM validity is operational
// validation, not structure.
func ReadableNonEmptyFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 1)
	n, err := f.Read(buf)
	return n == 1 && (err == nil || err == io.EOF)
}

// ProviderStructurallyConfigured is the single pure predicate behind
// "structurally configured" (spec §4.4): every field the web flow needs is
// present. It returns the schema key of the FIRST missing field so a WARN or
// a validation error can name it. Credential correctness, PEM validity and
// IdP reachability are outside it by design.
//
//	structural(p) := clientId ≠ "" ∧ redirectURL ≠ "" ∧ secrets(p)
//	secrets(google|github|discord) := clientSecret present
//	secrets(apple) := teamId ≠ "" ∧ keyId ≠ "" ∧ (inline key present ∨ readable key file)
func ProviderStructurallyConfigured(p models.OAuthProvider, f ProviderStructuralFields, probe KeyFileProbe) (missing string, ok bool) {
	prefix := string(p)
	switch p {
	case models.OAuthProviderGoogle, models.OAuthProviderGitHub, models.OAuthProviderDiscord, models.OAuthProviderApple:
	default:
		return "provider", false
	}
	if f.ClientID == "" {
		return prefix + "ClientId", false
	}
	if f.RedirectURL == "" {
		return prefix + "RedirectURL", false
	}
	if p != models.OAuthProviderApple {
		if !f.SecretPresent {
			return prefix + "ClientSecret", false
		}
		return "", true
	}
	if f.TeamID == "" {
		return "appleTeamId", false
	}
	if f.KeyID == "" {
		return "appleKeyId", false
	}
	if !f.SecretPresent && (probe == nil || !probe(f.PrivateKeyPath)) {
		return "applePrivateKey", false
	}
	return "", true
}

// WebProviderOrder is the order GET /v1/auth/{tier}/providers advertises.
var WebProviderOrder = []models.OAuthProvider{
	models.OAuthProviderGoogle,
	models.OAuthProviderApple,
	models.OAuthProviderGitHub,
	models.OAuthProviderDiscord,
}

// OAuthResolver is the resolver surface AuthHandler consumes. The concrete
// *OAuthConfigResolver satisfies it; tests inject a fake.
type OAuthResolver interface {
	Get(ctx context.Context, p models.OAuthProvider) (*OAuthProviderConfig, bool)
	RedirectURL(ctx context.Context, p models.OAuthProvider) string
	MobileAudience(ctx context.Context, p models.OAuthProvider, platform string) string
	ConfiguredProviders(ctx context.Context) []models.OAuthProvider
	// OAuthWebProviderUsable resolves ONE provider for ONE surface from ONE
	// config read. (cfg, true, nil) is usable and cfg is what the provider
	// is built from; (nil, false, nil) is a per-provider defect (toggle
	// off/absent/malformed, structural field missing) — already WARNed by
	// key; a non-nil error is a document-level outage (missing document,
	// repository error, undecryptable secret) and maps to 503.
	OAuthWebProviderUsable(ctx context.Context, audience PolicyAudience, p models.OAuthProvider) (*OAuthProviderConfig, bool, error)
	// UsableWebProviders is OAuthWebProviderUsable over WebProviderOrder from
	// a single read, returning the usable ones in canonical order.
	UsableWebProviders(ctx context.Context, audience PolicyAudience) ([]models.OAuthProvider, error)
}

var _ OAuthResolver = (*OAuthConfigResolver)(nil)

// activeConfigReader is the slice of ModuleConfigService the resolver
// depends on. The two legacy accessors serve Get/RedirectURL/MobileAudience
// (the mobile path); the strict web path reads only through
// ActiveConfigRequiredModule.
type activeConfigReader interface {
	GetValue(ctx context.Context, moduleName, key string) string
	GetSecret(ctx context.Context, moduleName, key string) string
	ActiveConfigRequiredModule(ctx context.Context, name string) (*module.ActiveConfigView, error)
}

// OAuthWebProviderUsable implements OAuthResolver.
func (r *OAuthConfigResolver) OAuthWebProviderUsable(ctx context.Context, audience PolicyAudience, p models.OAuthProvider) (*OAuthProviderConfig, bool, error) {
	view, err := r.activeView(ctx)
	if err != nil {
		return nil, false, err
	}
	return r.usableFromView(view, audience, p)
}

// UsableWebProviders implements OAuthResolver.
func (r *OAuthConfigResolver) UsableWebProviders(ctx context.Context, audience PolicyAudience) ([]models.OAuthProvider, error) {
	view, err := r.activeView(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.OAuthProvider, 0, len(WebProviderOrder))
	for _, p := range WebProviderOrder {
		if _, ok, _ := r.usableFromView(view, audience, p); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *OAuthConfigResolver) activeView(ctx context.Context) (*module.ActiveConfigView, error) {
	if r == nil || r.cs == nil {
		return nil, fmt.Errorf("%w: oauth config resolver not wired", ErrAuthPolicyUnavailable)
	}
	view, err := r.cs.ActiveConfigRequiredModule(ctx, "auth")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAuthPolicyUnavailable, err)
	}
	return view, nil
}

// usableFromView is the per-provider decision over an already-read view:
// strict toggle (absent → schema default false; malformed → unusable +
// WARN naming the key), then the structural predicate over the same view,
// then the provider config built from that same view.
func (r *OAuthConfigResolver) usableFromView(view *module.ActiveConfigView, audience PolicyAudience, p models.OAuthProvider) (*OAuthProviderConfig, bool, error) {
	key, known := providerToggleKey(audience, string(p))
	if !known {
		return nil, false, nil
	}
	on := false
	if raw, present := view.Raw(key); present {
		v, err := strictBool(raw)
		if err != nil {
			r.log().Warn("oauth provider toggle is not a canonical boolean; provider treated as unusable",
				slog.String("provider", string(p)), slog.String("key", key))
			return nil, false, nil
		}
		on = v
	}
	if !on {
		return nil, false, nil
	}
	fields := ProviderStructuralFields{
		ClientID:       view.Effective(string(p) + "ClientId"),
		RedirectURL:    view.Effective(string(p) + "RedirectURL"),
		SecretPresent:  view.SecretPresent(string(p) + "ClientSecret"),
		TeamID:         view.Effective("appleTeamId"),
		KeyID:          view.Effective("appleKeyId"),
		PrivateKeyPath: view.Effective("applePrivateKeyPath"),
	}
	if p == models.OAuthProviderApple {
		fields.SecretPresent = view.SecretPresent("applePrivateKey")
	}
	if missing, ok := ProviderStructurallyConfigured(p, fields, r.probe); !ok {
		r.log().Warn("oauth provider enabled but structurally incomplete; omitted",
			slog.String("provider", string(p)), slog.String("missing", missing))
		return nil, false, nil
	}
	cfg, _ := providerConfigFrom(p, view.Effective, view.Secret)
	return cfg, true, nil
}

func (r *OAuthConfigResolver) log() *slog.Logger {
	if r != nil && r.logger != nil {
		return r.logger
	}
	return slog.Default()
}
```

Rewrite `backend/internal/core/auth/services/oauth_config_resolver.go` so the struct carries the reader interface, a logger and the probe, and `Get` is a thin wrapper over one config builder shared with the strict path:

```go
package services

import (
	"context"
	"log/slog"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// OAuthConfigResolver builds a per-provider OAuthProviderConfig from the live
// module_configs document so admin-panel edits take effect without a restart.
// It keeps no process-local cache. Two read paths coexist: Get /
// RedirectURL / MobileAudience / ConfiguredProviders are the legacy per-key
// reads the mobile ID-token endpoints still use; OAuthWebProviderUsable /
// UsableWebProviders (oauth_provider_usability.go) are the strict, one-read
// web path.
type OAuthConfigResolver struct {
	cs     activeConfigReader
	logger *slog.Logger
	probe  KeyFileProbe
}

// NewOAuthConfigResolver wires the resolver to the running ConfigService.
// Passing a nil service is valid — every legacy lookup then returns
// (nil, false) and the strict path returns ErrAuthPolicyUnavailable.
func NewOAuthConfigResolver(cs *module.ModuleConfigService) *OAuthConfigResolver {
	r := &OAuthConfigResolver{logger: slog.Default(), probe: ReadableNonEmptyFile}
	if cs != nil {
		r.cs = cs
	}
	return r
}

// providerConfigFrom builds the OAuthProviderConfig for p from two accessor
// functions — GetValue/GetSecret closures on the legacy path, the view's
// Effective/Secret on the strict path — so both paths share one field map.
// ok is false when the client ID is empty (the legacy "not configured").
func providerConfigFrom(p models.OAuthProvider, get, sec func(string) string) (*OAuthProviderConfig, bool) {
	switch p {
	case models.OAuthProviderGoogle:
		id := get("googleClientId")
		if id == "" {
			return nil, false
		}
		return &OAuthProviderConfig{
			ClientID:     id,
			ClientSecret: sec("googleClientSecret"),
			Scopes:       []string{"openid", "email", "profile"},
			AdditionalConfig: map[string]string{
				"redirect_url":      get("googleRedirectURL"),
				"android_client_id": get("googleAndroidClientId"),
				"ios_client_id":     get("googleIOSClientId"),
			},
		}, true
	case models.OAuthProviderApple:
		id := get("appleClientId")
		if id == "" {
			return nil, false
		}
		return &OAuthProviderConfig{
			ClientID:     id,
			ClientSecret: "",
			Scopes:       []string{"name", "email"},
			AdditionalConfig: map[string]string{
				"team_id":           get("appleTeamId"),
				"key_id":            get("appleKeyId"),
				"private_key":       sec("applePrivateKey"),
				"private_key_path":  get("applePrivateKeyPath"),
				"redirect_url":      get("appleRedirectURL"),
				"ios_client_id":     get("appleIOSClientId"),
				"android_client_id": get("appleAndroidClientId"),
			},
		}, true
	case models.OAuthProviderGitHub:
		id := get("githubClientId")
		if id == "" {
			return nil, false
		}
		return &OAuthProviderConfig{
			ClientID:     id,
			ClientSecret: sec("githubClientSecret"),
			Scopes:       []string{"user:email", "read:user"},
			AdditionalConfig: map[string]string{
				"redirect_url": get("githubRedirectURL"),
			},
		}, true
	case models.OAuthProviderDiscord:
		id := get("discordClientId")
		if id == "" {
			return nil, false
		}
		return &OAuthProviderConfig{
			ClientID:     id,
			ClientSecret: sec("discordClientSecret"),
			Scopes:       []string{"identify", "email"},
			AdditionalConfig: map[string]string{
				"redirect_url": get("discordRedirectURL"),
			},
		}, true
	}
	return nil, false
}

// Get returns the current config for a provider, or (nil, false) if the
// client ID has not been set. Legacy per-key path (mobile). The web flow
// must use OAuthWebProviderUsable.
func (r *OAuthConfigResolver) Get(ctx context.Context, p models.OAuthProvider) (*OAuthProviderConfig, bool) {
	if r == nil || r.cs == nil {
		return nil, false
	}
	get := func(k string) string { return r.cs.GetValue(ctx, "auth", k) }
	sec := func(k string) string { return r.cs.GetSecret(ctx, "auth", k) }
	return providerConfigFrom(p, get, sec)
}
```

Keep `RedirectURL`, `MobileAudience` and `ConfiguredProviders` exactly as they are (lines 107-152). Delete nothing else.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/... -count=1 && go vet ./...`
Expected: `ok`. (`module.go` still passes `*module.ModuleConfigService` to `NewOAuthConfigResolver` — unchanged signature.)

- [ ] **Step 5: Document the resolver API**

In `backend/internal/core/auth/CLAUDE.md`, replace the "Resolver API" table (lines 406-411) with:

```markdown
| Method | Returns |
|---|---|
| `OAuthWebProviderUsable(ctx, audience, provider)` | `(*OAuthProviderConfig, bool, error)` — **the web path.** ONE `ActiveConfigRequiredModule` read; strict toggle (`{provider}Enabled{Admin,Client}`: absent → schema default `false`, malformed → unusable + WARN naming the key) then `ProviderStructurallyConfigured` over the same view, then the config the provider is built from — no check-then-reread. `(nil, false, nil)` is a per-provider defect; a non-nil error (missing document, repository error, undecryptable stored secret) is document-level and maps to **503 `auth.policy_unavailable`** |
| `UsableWebProviders(ctx, audience)` | `([]models.OAuthProvider, error)` — the above over `WebProviderOrder` (google, apple, github, discord) from one read; served by `GET /v1/auth/{tier}/providers` |
| `Get(ctx, provider)` | `(*OAuthProviderConfig, bool)` — **legacy per-key path, mobile only**; `false` means client ID is empty. Every key is a separate `module_configs` read and a decrypt failure silently falls back to the env var — which is why the web flow does not use it |
| `RedirectURL(ctx, provider)` | Web callback URL, or `""` (legacy) |
| `MobileAudience(ctx, provider, platform)` | Platform-specific client ID for mobile ID-token validation; falls back to the web client ID when `platform` is unknown |
| `ConfiguredProviders(ctx)` | Legacy "has a client ID" list; no longer serves `/providers` |

`services.OAuthResolver` is the interface `AuthHandler` holds (the concrete resolver satisfies it; tests inject a fake). `ProviderStructurallyConfigured(p, fields, probe)` is the single exported pure predicate — `clientId ≠ "" ∧ redirectURL ≠ "" ∧ secret present` (apple: `teamId ∧ keyId ∧ (inline key ∨ readable non-empty key file)`) — and receives secret **presence** only; the PR 3 validator reuses it against the target snapshot.
```

- [ ] **Step 6: Commit**

```bash
cd /home/tore/orkestra && git add backend/internal/core/auth/services/oauth_provider_usability.go backend/internal/core/auth/services/oauth_provider_usability_test.go backend/internal/core/auth/services/oauth_config_resolver.go backend/internal/core/auth/CLAUDE.md
git commit -m "feat(auth): strict one-read OAuth provider usability (ProviderStructurallyConfigured, OAuthWebProviderUsable, UsableWebProviders)"
```

---

### Task 4: Verified email before any lookup; strict auto-link before lookup; GitHub email from `/user/emails` only

**Files:**
- Modify: `backend/internal/core/auth/services/auth_service.go` (`HandleOAuthCallbackWithLinking`, the `else` branch lines 1904-1985)
- Modify: `backend/internal/core/auth/services/github_oauth_service.go` (lines 185-192)
- Create: `backend/internal/core/auth/services/github_oauth_service_test.go`
- Modify: `backend/internal/core/auth/services/gates_fakes_test.go` (`gateUserFake` lines 32-46, `GetUserByEmail` line 72), `gates_test.go` (lines 607-656 and new tests), `oauth_inactive_user_test.go` (line 117-121)
- Modify: `backend/internal/core/auth/CLAUDE.md` ("OAuth signup trusts the IdP's `email_verified` claim" invariant, line 647; "OAuth Providers" policy row, line 184)

**Interfaces:**
- Consumes: `ErrOAuthEmailUnverified`, `OAuthAutoLinkByEmailEnabled`, `ErrAuthPolicyUnavailable` (Task 2).
- Produces: the ordering contract of `HandleOAuthCallbackWithLinking` for an unlinked identity — `email required → verified bit → auto-link policy → GetUserByEmail → link | signup`; `gateUserFake.getByEmailCalls int`.

- [ ] **Step 1: Write the failing tests**

In `backend/internal/core/auth/services/gates_fakes_test.go`, add a counter to `gateUserFake` (after `updateUserErr`, line 46):

```go
	// getByEmailCalls counts GetUserByEmail — the lookup §4.4 forbids before
	// the verified-email and auto-link-policy checks.
	getByEmailCalls int
```

and increment it as the first statement inside `GetUserByEmail` (line 72, after the lock):

```go
	f.getByEmailCalls++
```

In `backend/internal/core/auth/services/gates_test.go`, replace `TestOAuthCallback_PropagatesEmailVerifiedFromIdP` (lines 607-656) with:

```go
func TestOAuthCallback_NewUser_RequiresVerifiedEmail(t *testing.T) {
	// §4.4: an unlinked identity is matched or created only when the IdP
	// vouches for the address. claim_true still lands EmailVerified=true so
	// the user is not asked to confirm what the IdP confirmed; false or
	// missing is refused BEFORE the email lookup and creates nothing.
	env := newOAuthGatesEnv(t, PolicyAudienceOperator, nil)
	env.users.seed(activeUser("seed-ev@example.com", "x"))
	env.claimer.claimed = map[string]bool{"seed": true}
	env.users.createFromOAuthAbortErr = errors.New("stop here, flag captured")

	_, _ = env.auth.HandleOAuthCallbackWithLinking(
		context.Background(), authModels.OAuthProviderGoogle,
		map[string]any{"provider_id": "g-verified", "email": "verified@example.com", "name": "V", "email_verified": true},
		nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
	)
	created := env.users.byEmail["verified@example.com"]
	if created == nil || !created.EmailVerified {
		t.Fatalf("a verified IdP email must create a verified user: %+v", created)
	}

	for name, claim := range map[string]map[string]any{
		"claim_false":   {"provider_id": "g-unverified", "email": "unverified@example.com", "name": "U", "email_verified": false},
		"claim_missing": {"provider_id": "g-missing", "email": "missing@example.com", "name": "M"},
		"claim_string":  {"provider_id": "g-string", "email": "string@example.com", "name": "S", "email_verified": "true"},
	} {
		t.Run(name, func(t *testing.T) {
			env := newOAuthGatesEnv(t, PolicyAudienceOperator, nil)
			_, err := env.auth.HandleOAuthCallbackWithLinking(
				context.Background(), authModels.OAuthProviderGoogle, claim,
				nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
			)
			if !errors.Is(err, ErrOAuthEmailUnverified) {
				t.Fatalf("err = %v, want ErrOAuthEmailUnverified", err)
			}
			if env.users.getByEmailCalls != 0 {
				t.Fatalf("GetUserByEmail was called %d times; must be 0 before the verified check", env.users.getByEmailCalls)
			}
			if len(env.users.createdUsers) != 0 {
				t.Fatal("no user may be created")
			}
		})
	}
}

func TestOAuthCallback_UnverifiedEmail_SameAnswerForKnownAndUnknownAccount(t *testing.T) {
	// The refusal must not be an account-existence oracle: identical error,
	// zero lookups, whether or not a local account with that email exists.
	for name, seedKnown := range map[string]bool{"known": true, "unknown": false} {
		t.Run(name, func(t *testing.T) {
			env := newOAuthGatesEnv(t, PolicyAudienceOperator, nil)
			if seedKnown {
				env.users.seed(activeUser("probe@example.com", "x"))
			}
			_, err := env.auth.HandleOAuthCallbackWithLinking(
				context.Background(), authModels.OAuthProviderGoogle,
				map[string]any{"provider_id": "g-probe", "email": "probe@example.com", "name": "P", "email_verified": false},
				nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
			)
			if !errors.Is(err, ErrOAuthEmailUnverified) || env.users.getByEmailCalls != 0 {
				t.Fatalf("err = %v, lookups = %d", err, env.users.getByEmailCalls)
			}
		})
	}
}

func TestOAuthCallback_AutoLinkPolicyUnavailable_FailsClosedBeforeLookup(t *testing.T) {
	for name, reader := range map[string]*stubReader{
		"read failure":     {rawErr: errors.New("mongo down")},
		"missing document": {requiredMissing: true},
		"malformed value":  {values: map[string]string{"oauthAutoLinkByEmail": "treu"}},
	} {
		t.Run(name, func(t *testing.T) {
			env := newOAuthGatesEnv(t, PolicyAudienceOperator, nil)
			env.policy.cs = reader
			env.users.seed(activeUser("existing@example.com", "x"))
			_, err := env.auth.HandleOAuthCallbackWithLinking(
				context.Background(), authModels.OAuthProviderGoogle,
				map[string]any{"provider_id": "g-existing", "email": "existing@example.com", "name": "E", "email_verified": true},
				nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
			)
			if !errors.Is(err, ErrAuthPolicyUnavailable) {
				t.Fatalf("err = %v, want ErrAuthPolicyUnavailable", err)
			}
			if env.users.getByEmailCalls != 0 || len(env.users.createdUsers) != 0 {
				t.Fatalf("lookups = %d, created = %d; must both be 0", env.users.getByEmailCalls, len(env.users.createdUsers))
			}
		})
	}
}

func TestOAuthCallback_NilPolicy_FailsClosed(t *testing.T) {
	// A service wired without a policy cannot establish the auto-link
	// rule; it must not fall open to the legacy "always link".
	env := newOAuthGatesEnv(t, PolicyAudienceOperator, nil)
	env.auth.SetPolicy(nil)
	env.users.seed(activeUser("existing@example.com", "x"))
	_, err := env.auth.HandleOAuthCallbackWithLinking(
		context.Background(), authModels.OAuthProviderGoogle,
		map[string]any{"provider_id": "g-existing", "email": "existing@example.com", "name": "E", "email_verified": true},
		nil, &authModels.SecurityContext{}, &authModels.DeviceInfo{},
	)
	if !errors.Is(err, ErrAuthPolicyUnavailable) || env.users.getByEmailCalls != 0 {
		t.Fatalf("err = %v, lookups = %d", err, env.users.getByEmailCalls)
	}
}
```

Update `TestOAuthCallback_AutoLinkDisabled_ReturnsErr` (line 678) and `TestOAuthCallback_RegistrationDisabled_ReturnsErr` (line 659), `TestOAuthCallback_SignupDisabled_ReturnsErr` (539), `TestOAuthCallback_OperatorDefaultRoleGuest` (555), `TestOAuthCallback_ClientDefaultRoleReadsPolicy` (582): add `"email_verified": true` to each `userInfo` map — those tests exercise the lookup/signup branches, which now require the bit. In `TestOAuthCallback_AutoLinkDisabled_ReturnsErr` additionally assert the lookup happened exactly once after the policy read:

```go
	if env.users.getByEmailCalls != 1 {
		t.Fatalf("lookups = %d, want exactly 1 (policy is read BEFORE the lookup, the refusal comes after it)", env.users.getByEmailCalls)
	}
```

In `oauth_inactive_user_test.go` `TestHandleOAuthCallbackWithLinking_RejectsInactiveEmailMatchedUser` (line 117-121): add `policy: newPolicy(nil),` to the `authService` literal (if Task 2 did not already) and `"email_verified": true,` to the userInfo map. Leave `RejectsInactiveLinkedUser` **unchanged** — its userInfo carries no `email_verified` and it must keep passing: that is the proof that an existing provider-ID link ignores the bit (add that sentence as a comment above the test).

Create `backend/internal/core/auth/services/github_oauth_service_test.go`:

```go
package services

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// githubRoundTripper answers api.github.com by path so GetUserInfo can be
// exercised without the network. The service hard-codes its URLs; the
// http.Client transport is the seam.
type githubRoundTripper struct {
	profile string
	emails  string
	status  map[string]int
}

func (rt githubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	body, code := "", http.StatusOK
	switch r.URL.Path {
	case "/user":
		body = rt.profile
	case "/user/emails":
		body = rt.emails
	default:
		code = http.StatusNotFound
	}
	if c, ok := rt.status[r.URL.Path]; ok {
		code = c
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

func githubService(rt http.RoundTripper) *githubOAuthService {
	return &githubOAuthService{config: &OAuthProviderConfig{ClientID: "cid"}, httpClient: &http.Client{Transport: rt}}
}

const githubProfile = `{"id": 42, "login": "octo", "name": "Octo", "email": "public-profile@example.com", "avatar_url": "https://a"}`

func TestGitHubGetUserInfo_PrimaryVerifiedFromEmailsEndpoint(t *testing.T) {
	svc := githubService(githubRoundTripper{profile: githubProfile,
		emails: `[{"email":"other@example.com","primary":false,"verified":true},{"email":"primary@example.com","primary":true,"verified":true}]`})
	info, err := svc.GetUserInfo(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if info.Email != "primary@example.com" || !info.EmailVerified {
		t.Fatalf("got %q verified=%v; the primary verified address wins over the public profile", info.Email, info.EmailVerified)
	}
}

func TestGitHubGetUserInfo_FallsBackToAnyVerified(t *testing.T) {
	svc := githubService(githubRoundTripper{profile: githubProfile,
		emails: `[{"email":"unverified-primary@example.com","primary":true,"verified":false},{"email":"second@example.com","primary":false,"verified":true}]`})
	info, err := svc.GetUserInfo(context.Background(), "tok")
	if err != nil || info.Email != "second@example.com" || !info.EmailVerified {
		t.Fatalf("got %+v err=%v; a non-primary verified address is still one GitHub verified", info, err)
	}
}

func TestGitHubGetUserInfo_PublicProfileEmailIsNeverVerifiedByAssumption(t *testing.T) {
	cases := map[string]githubRoundTripper{
		"no verified address": {profile: githubProfile, emails: `[{"email":"x@example.com","primary":true,"verified":false}]`},
		"emails endpoint 401": {profile: githubProfile, emails: `{}`, status: map[string]int{"/user/emails": 401}},
		"emails endpoint empty": {profile: githubProfile, emails: `[]`},
	}
	for name, rt := range cases {
		t.Run(name, func(t *testing.T) {
			info, err := githubService(rt).GetUserInfo(context.Background(), "tok")
			if err != nil {
				t.Fatal(err)
			}
			if info.Email != "public-profile@example.com" || info.EmailVerified {
				t.Fatalf("got %q verified=%v; the profile email survives only as an UNVERIFIED fallback", info.Email, info.EmailVerified)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/services/ -run 'OAuthCallback|GitHubGetUserInfo|HandleOAuthCallbackWithLinking' -count=1`
Expected: `TestOAuthCallback_NewUser_RequiresVerifiedEmail/claim_false` FAIL ("err = <nil>, want ErrOAuthEmailUnverified" or a created user), `TestOAuthCallback_UnverifiedEmail_*` FAIL, `TestOAuthCallback_AutoLinkPolicyUnavailable_*` FAIL (lookups = 1), `TestGitHubGetUserInfo_PrimaryVerifiedFromEmailsEndpoint` FAIL (got `public-profile@example.com`).

- [ ] **Step 3: Reorder the callback service and fix GitHub**

In `backend/internal/core/auth/services/auth_service.go`, replace the `else` branch of `if existingProvider != nil { … } else { … }` in `HandleOAuthCallbackWithLinking` (from `// No existing provider - check if user exists by email` through the closing brace of `else { … user = convertUserResponseToAuthModel(userResponse) }`) with:

```go
	} else {
		// No existing (provider, providerID) link. §4.4 — three things are
		// decided BEFORE the local email lookup, in this order:
		//
		//  1. The IdP must vouch for the address. A false/missing/non-bool
		//     email_verified is refused now, identically whether or not a
		//     local account exists (no account-existence oracle), and
		//     before either the auto-link or the signup branch.
		//  2. The auto-link policy must be establishable. A read failure,
		//     a missing auth document or a malformed value is an outage
		//     (503) — evaluated before the lookup so it cannot depend on
		//     account state either.
		//  3. Only then the lookup, and the branch it selects.
		if verified, _ := userInfo["email_verified"].(bool); !verified {
			return nil, ErrOAuthEmailUnverified
		}
		autoLink, err := s.policy.OAuthAutoLinkByEmailEnabled(ctx)
		if err != nil {
			return nil, err
		}
		userResponse, err := s.userService.GetUserByEmail(ctx, email)
		if err != nil {
			// Signup gates. Two toggles must both allow the new account:
			// the audience-scoped registration kill switch (the umbrella
			// "Allow signups" toggle, shared with password signup) and
			// the OAuth-specific gate. Unlike the password Register()
			// path there is no first-user bypass here — operators
			// bootstrap a fresh install via the password flow, which
			// retains its own kill-switch bypass for that case.
			if s.policy != nil {
				if !s.policy.RegistrationAllowed(ctx, s.audience) {
					return nil, ErrOAuthSignupDisabled
				}
				if !s.policy.OAuthAllowSignup(ctx, s.audience) {
					return nil, ErrOAuthSignupDisabled
				}
			}
			// Create new user via UserService
			newUUID := models.GenerateUUIDv7()

			// Atomic first-admin claim (replaces the former count-based race).
			// If the sentinel is already taken by another concurrent signup,
			// fall through to the tier-default role: "guest" (lowest system
			// role) for operator-tier signups so a fresh OAuth callback
			// can't grant itself elevated privileges by default; for
			// client-tier signups, the admin-configurable defaultRoleClient
			// (falls back to "operator" when unset, matching today's
			// password-path behaviour).
			role := "guest"
			if s.audience == PolicyAudienceClient && s.policy != nil {
				role = s.policy.DefaultClientRole(ctx)
			}
			claimed := false
			if s.firstAdminClaimer != nil {
				c, err := s.firstAdminClaimer.ClaimFirstAdmin(ctx, newUUID)
				if err == nil && c {
					claimed = true
					role = "super_admin"
				}
			}

			// The IdP verified the address (checked above), so the account
			// lands verified: nobody is asked to confirm what the IdP just
			// confirmed.
			createInput := &iface.CreateUserInput{
				UUID:          newUUID,
				Email:         email,
				FullName:      userInfo["name"].(string),
				Role:          role,
				EmailVerified: true,
			}

			userModel, err := s.userService.CreateUserFromOAuth(ctx, createInput)
			if err != nil {
				if claimed && s.firstAdminClaimer != nil {
					_ = s.firstAdminClaimer.Release(ctx, newUUID)
				}
				return nil, fmt.Errorf("failed to create user: %w", err)
			}
			user = convertUserModelToAuthModel(userModel)
		} else {
			// oauthAutoLinkByEmail gate (read strictly above). The callback
			// found an existing Orkestra account by a VERIFIED matching
			// email — auto-linking is convenient but lets whoever controls
			// a matching IdP identity enter a password account. When off,
			// refuse here so linking happens from an authenticated
			// settings page instead.
			if !autoLink {
				return nil, ErrOAuthLinkDisabled
			}
			user = convertUserResponseToAuthModel(userResponse)
		}
	}
```

(The interim block Task 2 inserted at the old line 1980 is replaced by this — make sure only one `OAuthAutoLinkByEmailEnabled` call remains in the function.) Keep `userInfo["name"].(string)` exactly as it was — changing that panic-on-missing behaviour is out of scope.

In `backend/internal/core/auth/services/github_oauth_service.go`, replace lines 185-192 (`// Get primary email if not available in profile` … `}`) with:

```go
	// §4.4 / §6: the address and its verified bit come ONLY from
	// /user/emails (primary verified first, then any verified — see
	// getPrimaryEmail). The public-profile `email` is a free-text field the
	// user may set to any string, so it is never marked verified by
	// assumption; it survives only as an UNVERIFIED fallback when the
	// endpoint yields nothing, and the callback then refuses to auto-link
	// or sign up with it.
	email, emailVerified := s.getPrimaryEmail(ctx, accessToken)
	if email == "" {
		email = user.Email
		emailVerified = false
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/... -count=1 && go vet ./...`
Expected: `ok`.

- [ ] **Step 5: Document the invariant**

In `backend/internal/core/auth/CLAUDE.md` replace the "OAuth signup trusts the IdP's `email_verified` claim" bullet (line 647) with:

```markdown
- **A provider-verified email is mandatory before any email lookup.** Every provider populates `OAuthUserInfo.EmailVerified` from its own signal (Google/Apple `email_verified`, Discord `verified`, GitHub **only** from `/user/emails` — primary verified first, then any verified; the public-profile `email` is never marked verified by assumption and survives only as an unverified fallback). The handlers forward it as `email_verified` in the `userInfoMap`. `HandleOAuthCallbackWithLinking` decides three things **before** `GetUserByEmail` for an identity with no existing `(provider, providerID)` link, in this order: the bit must be `true` (else `ErrOAuthEmailUnverified` → 403 `auth.oauth_email_unverified` / web `error=auth.oauth_email_unverified`, identically whether or not a local account exists — it must not become an account-existence oracle); the auto-link policy must be establishable (strict `OAuthAutoLinkByEmailEnabled`: read failure, missing document or malformed value → `ErrAuthPolicyUnavailable` → 503 before lookup, link or token issuance; a nil policy is the same outage, never the legacy "always link"); only then the lookup, after which a found account auto-links only when the policy is on, and a new account lands `EmailVerified=true` without re-asking. An existing provider-ID link logs in as today regardless of the bit (`oauth_inactive_user_test.go` pins it). Regression tests: `gates_test.go` `TestOAuthCallback_NewUser_RequiresVerifiedEmail`, `_UnverifiedEmail_SameAnswerForKnownAndUnknownAccount`, `_AutoLinkPolicyUnavailable_FailsClosedBeforeLookup`, `github_oauth_service_test.go`.
```

In the "OAuth Providers" policy-table row (line 184), replace the sentence starting "Phase 10: `oauthAutoLinkByEmail` (default true) gates…" with:

```markdown
Phase 10: `oauthAutoLinkByEmail` (default true) gates auto-attaching a provider to an existing account whose email matches the IdP's **verified** address; read strictly by `OAuthAutoLinkByEmailEnabled` (absent → true; malformed/unreadable → 503 `auth.policy_unavailable` before any lookup). When off, the callback returns `ErrOAuthLinkDisabled` (`error=oauth_link_disabled`) and the user must initiate linking from authenticated settings.
```

- [ ] **Step 6: Commit**

```bash
cd /home/tore/orkestra && git add backend/internal/core/auth/services/auth_service.go backend/internal/core/auth/services/github_oauth_service.go backend/internal/core/auth/services/github_oauth_service_test.go backend/internal/core/auth/services/gates_fakes_test.go backend/internal/core/auth/services/gates_test.go backend/internal/core/auth/services/oauth_inactive_user_test.go backend/internal/core/auth/CLAUDE.md
git commit -m "fix(auth): require a provider-verified email and an establishable auto-link policy before any local email lookup; GitHub email from /user/emails only"
```

---

### Task 5: The closed callback contract — one builder file (green on its own)

**Files:**
- Create: `backend/internal/core/auth/handlers/oauth_callback_redirect.go`
- Modify: `backend/internal/core/auth/handlers/auth_handler.go` (add the `spaBaseURL` field to `AuthHandler`, after `blobStore` line 87)
- Create: `backend/internal/core/auth/handlers/oauth_callback_redirect_test.go`

**Interfaces:**
- Consumes: `errcode.AuthOAuthEmailUnverified` (Task 2), `services.ErrOAuthSignupDisabled`, `ErrOAuthLinkDisabled`, `ErrOAuthEmailUnverified`, `ErrAuthPolicyUnavailable`, `ErrInvalidCredentials`; `config.Server.Client.PublicURL` (Task 7 adds the field — this task reads it through a helper that Task 7 wires; see Step 3).
- Produces: constants `OAuthCallbackErrAccessDenied … OAuthCallbackErrLoginFailed`, `oauthLinkCode*`, `oauthRelayCompletePath`; `type oauthLoginResult`; `oauthLoginSuccess(p)`, `oauthLoginFailure(p, code)`, `oauthLoginMFA(p, token, webauthn)`; `SetSPAURL`, `spaURL`; `oauthLoginCallbackURL`, `writeOAuthLoginRedirect`; `oauthLinkReturnURL`, `writeOAuthLinkRedirect`; `relayCompleteURL(id) (string, bool)`, `writeRelayRedirect`; `setCallbackRedirectHeaders`; `oauthLoginErrorCode(err) (code, outcome string)`; `sanitizeIdPError`. Consumed by Tasks 7–8. The structural scan test lives in Task 7 (`oauth_callback_scan_test.go`), where the legacy literals it forbids are actually removed — this task's commit is green.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/handlers/oauth_callback_redirect_test.go`:

```go
package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
)

func redirectHandler(spa string) *AuthHandler {
	cfg := &config.Config{}
	cfg.Server.FrontendURL = "https://legacy.example"
	h := &AuthHandler{config: cfg}
	if spa != "" {
		h.SetSPAURL(spa)
	}
	return h
}

// parseCallback splits a built URL into (base, query, fragment) so tests
// assert on the CONTRACT, not on parameter order.
func parseCallback(t *testing.T, raw string) (string, url.Values, url.Values) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	frag, err := url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatalf("parse fragment %q: %v", u.Fragment, err)
	}
	return u.Scheme + "://" + u.Host + u.Path, u.Query(), frag
}

func TestOAuthLoginCallbackURL_Success(t *testing.T) {
	h := redirectHandler("https://app.example/")
	base, q, frag := parseCallback(t, h.oauthLoginCallbackURL(oauthLoginSuccess(models.OAuthProviderGoogle)))
	if base != "https://app.example/auth/callback" {
		t.Fatalf("base = %q (trailing slash must be trimmed)", base)
	}
	if len(q) != 2 || q.Get("success") != "true" || q.Get("provider") != "google" {
		t.Fatalf("query = %v, want exactly success=true&provider=google", q)
	}
	if len(frag) != 0 {
		t.Fatalf("success carries no fragment: %v", frag)
	}
}

func TestOAuthLoginCallbackURL_FailureAllowlist(t *testing.T) {
	h := redirectHandler("https://console.example")
	for _, code := range []string{
		OAuthCallbackErrAccessDenied, OAuthCallbackErrSignupDisabled, OAuthCallbackErrLinkDisabled,
		OAuthCallbackErrEmailUnverified, OAuthCallbackErrProviderUnavailable, OAuthCallbackErrLoginFailed,
	} {
		_, q, frag := parseCallback(t, h.oauthLoginCallbackURL(oauthLoginFailure(models.OAuthProviderGitHub, code)))
		if len(q) != 2 || q.Get("success") != "false" || q.Get("error") != code || len(frag) != 0 {
			t.Fatalf("code %q: query = %v frag = %v", code, q, frag)
		}
	}
	for _, bad := range []string{"", "internal: mongo down", "invalid_credentials", "<script>", "auth.registration_disabled"} {
		_, q, _ := parseCallback(t, h.oauthLoginCallbackURL(oauthLoginFailure(models.OAuthProviderGitHub, bad)))
		if q.Get("error") != OAuthCallbackErrLoginFailed {
			t.Fatalf("%q must collapse to the generic code, got %q", bad, q.Get("error"))
		}
	}
	if OAuthCallbackErrEmailUnverified != "auth.oauth_email_unverified" {
		t.Fatalf("the web code must equal the errcode constant: %q", OAuthCallbackErrEmailUnverified)
	}
}

func TestOAuthLoginCallbackURL_MFAInFragmentOnly(t *testing.T) {
	h := redirectHandler("https://console.example")
	raw := h.oauthLoginCallbackURL(oauthLoginMFA(models.OAuthProviderApple, "challenge-1", true))
	base, q, frag := parseCallback(t, raw)
	if base != "https://console.example/auth/callback" || len(q) != 0 {
		t.Fatalf("MFA continuation must carry NO query: base=%q q=%v", base, q)
	}
	if len(frag) != 3 || frag.Get("requiresMfa") != "true" || frag.Get("mfaToken") != "challenge-1" || frag.Get("webauthnAvailable") != "true" {
		t.Fatalf("fragment = %v", frag)
	}
	if !strings.Contains(raw, "#") || strings.Contains(raw[:strings.Index(raw, "#")], "challenge-1") {
		t.Fatalf("the one-shot id must live after the '#': %q", raw)
	}
	_, _, frag = parseCallback(t, h.oauthLoginCallbackURL(oauthLoginMFA(models.OAuthProviderApple, "c", false)))
	if frag.Get("webauthnAvailable") != "false" {
		t.Fatalf("webauthnAvailable is always explicit: %v", frag)
	}
}

func TestSPAURL_PerTierValueThenLegacyFallback(t *testing.T) {
	if got := redirectHandler("").spaURL(); got != "https://legacy.example" {
		t.Fatalf("no per-tier value → FRONTEND_URL, got %q", got)
	}
	if got := redirectHandler("  https://app.example//  ").spaURL(); got != "https://app.example" {
		t.Fatalf("trim + trailing slashes, got %q", got)
	}
	var nilConfig AuthHandler
	if got := nilConfig.spaURL(); got != "" {
		t.Fatalf("no config, no value → empty, got %q", got)
	}
}

func TestWriteOAuthLoginRedirect_HeadersAndStatus(t *testing.T) {
	h := redirectHandler("https://app.example")
	rec := httptest.NewRecorder()
	h.writeOAuthLoginRedirect(rec, httptest.NewRequest(http.MethodGet, "/cb", nil), oauthLoginSuccess(models.OAuthProviderDiscord))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", rec.Header())
	}
	if !strings.HasPrefix(rec.Header().Get("Location"), "https://app.example/auth/callback?") {
		t.Fatalf("Location = %q", rec.Header().Get("Location"))
	}
}

func TestOAuthLinkReturnURL(t *testing.T) {
	h := redirectHandler("https://console.example")
	base, q, _ := parseCallback(t, h.oauthLinkReturnURL(models.OAuthProviderGoogle, true, ""))
	if base != "https://console.example/user/security" || q.Get("tab") != "oauth" || q.Get("link") != "success" || q.Get("provider") != "google" || q.Has("code") {
		t.Fatalf("success: base=%q q=%v", base, q)
	}
	for _, code := range []string{oauthLinkCodeAlreadyLinked, oauthLinkCodeDuplicateProvider, oauthLinkCodeInvalidUserInfo, oauthLinkCodeAccessDenied, oauthLinkCodeProviderUnavailable, oauthLinkCodeInternal} {
		_, q, _ := parseCallback(t, h.oauthLinkReturnURL(models.OAuthProviderGoogle, false, code))
		if q.Get("link") != "failed" || q.Get("code") != code {
			t.Fatalf("code %q: q=%v", code, q)
		}
	}
	_, q, _ = parseCallback(t, h.oauthLinkReturnURL(models.OAuthProviderGoogle, false, "mongo: no documents"))
	if q.Get("code") != oauthLinkCodeInternal {
		t.Fatalf("unknown link code must collapse to internal: %v", q)
	}
	rec := httptest.NewRecorder()
	h.writeOAuthLinkRedirect(rec, httptest.NewRequest(http.MethodGet, "/cb", nil), models.OAuthProviderGoogle, false, oauthLinkCodeAccessDenied)
	if rec.Code != http.StatusFound || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("link redirect: %d %v", rec.Code, rec.Header())
	}
}

func TestRelayCompleteURL(t *testing.T) {
	h := redirectHandler("https://console.example")
	if _, ok := h.relayCompleteURL("relay-1"); ok {
		t.Fatal("no client API origin → no relay destination")
	}
	h.config.Server.Client.PublicURL = "https://api.example/"
	got, ok := h.relayCompleteURL("relay-1")
	if !ok || got != "https://api.example/v1/auth/client/oauth/complete?relay=relay-1" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if _, ok := h.relayCompleteURL(""); ok {
		t.Fatal("an empty relay id must not build a destination")
	}
	rec := httptest.NewRecorder()
	h.writeRelayRedirect(rec, httptest.NewRequest(http.MethodGet, "/cb", nil), got)
	if rec.Code != http.StatusFound || rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("relay redirect: %d %v", rec.Code, rec.Header())
	}
}

func TestOAuthLoginErrorCode(t *testing.T) {
	cases := []struct {
		err         error
		code, outcm string
	}{
		{services.ErrOAuthSignupDisabled, OAuthCallbackErrSignupDisabled, "signup_disabled"},
		{services.ErrOAuthLinkDisabled, OAuthCallbackErrLinkDisabled, "link_disabled"},
		{services.ErrOAuthEmailUnverified, OAuthCallbackErrEmailUnverified, "email_unverified"},
		{services.ErrAuthPolicyUnavailable, OAuthCallbackErrProviderUnavailable, "policy_unavailable"},
		{services.ErrInvalidCredentials, OAuthCallbackErrLoginFailed, "invalid_credentials"},
		{errors.New("user u-1 <secret@example.com> inactive"), OAuthCallbackErrLoginFailed, "internal_error"},
	}
	for _, tc := range cases {
		code, outcome := oauthLoginErrorCode(tc.err)
		if code != tc.code || outcome != tc.outcm {
			t.Errorf("%v → %q/%q, want %q/%q", tc.err, code, outcome, tc.code, tc.outcm)
		}
	}
}

func TestSanitizeIdPError(t *testing.T) {
	if got := sanitizeIdPError("access_denied"); got != "access_denied" {
		t.Fatalf("got %q", got)
	}
	for _, raw := range []string{"", "Access Denied", "<script>", strings.Repeat("a", 65), "user u-1 secret@example.com"} {
		if got := sanitizeIdPError(raw); got != "unrecognized" {
			t.Fatalf("%q → %q, want unrecognized", raw, got)
		}
	}
}
```

`TestRelayCompleteURL` references `config.Server.Client.PublicURL`, which Task 7 adds to `config.go`. To keep Task 5 self-contained, **add the field now** (Task 7 wires its env var): in `backend/internal/shared/config/config.go` `AudienceConfig` (after `FrontendURL`, line 102) add

```go
	// PublicURL is the public origin of this audience's API (scheme +
	// host, no path). The auth module redirects a client-tier OAuth
	// callback to `{Client.PublicURL}/v1/auth/client/oauth/complete` so
	// the refresh cookie is set by the host that owns it. Read from
	// CLIENT_API_URL; empty means no client surface.
	PublicURL string
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/handlers/ -run 'OAuthLoginCallbackURL|SPAURL|WriteOAuthLoginRedirect|OAuthLinkReturnURL|RelayCompleteURL|OAuthLoginErrorCode|SanitizeIdPError' -count=1`
Expected: FAIL to compile (`undefined: oauthLoginSuccess`, …).

- [ ] **Step 3: Implement the builder file**

Add to the `AuthHandler` struct in `auth_handler.go` (after `blobStore blob.Store`, line 87):

```go
	// spaBaseURL is THIS tier's SPA origin, resolved once by module.go
	// (OPERATOR_FRONTEND_URL / CLIENT_FRONTEND_URL → FRONTEND_URL) and
	// handed over with SetSPAURL. Every browser-facing redirect a callback
	// issues — login success/failure, MFA continuation, link return — is
	// built on it (oauth_callback_redirect.go); the Origin header is never
	// consulted. Empty falls back to config.Server.FrontendURL.
	spaBaseURL string
```

Create `backend/internal/core/auth/handlers/oauth_callback_redirect.go`:

```go
package handlers

// The SPA-facing OAuth callback contract (spec §4.10) lives in this one
// file. Nothing else in the package may build a /auth/callback or
// /user/security URL — TestCallbackURLBuilders_StructuralScan enforces it —
// and no builder here may ever carry an access token, a refresh token, an
// email or a user id. The wire shape is CLOSED:
//
//	success:  {spa}/auth/callback?success=true&provider=<google|apple|github|discord>
//	failure:  {spa}/auth/callback?success=false&error=<allowlisted code>
//	MFA:      {spa}/auth/callback#requiresMfa=true&mfaToken=<one-shot id>&webauthnAvailable=<true|false>
//	link:     {spa}/user/security?tab=oauth&link=success|failed&provider=<p>[&code=<allowlisted>]
//	relay:    {client api}/v1/auth/client/oauth/complete?relay=<one-shot id>  (client tier only)
//
// The MFA continuation travels in the FRAGMENT so the five-minute one-shot
// challenge id never reaches a server log, a reverse proxy or a Referer;
// every redirect additionally sets Referrer-Policy: no-referrer and
// Cache-Control: no-store. Session bootstrap after success is recovered
// only from the audience-scoped HttpOnly refresh cookie. The relay id is a
// single-use, browser-bound handle like the IdP code, never a credential.

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
)

const (
	oauthCallbackPath      = "/auth/callback"
	oauthLinkReturnPath    = "/user/security"
	oauthRelayCompletePath = "/v1/auth/client/oauth/complete"

	// Login-callback failure codes — the closed allowlist of spec §4.10.
	OAuthCallbackErrAccessDenied        = "oauth_access_denied"
	OAuthCallbackErrSignupDisabled      = "oauth_signup_disabled"
	OAuthCallbackErrLinkDisabled        = "oauth_link_disabled"
	OAuthCallbackErrEmailUnverified     = errcode.AuthOAuthEmailUnverified
	OAuthCallbackErrProviderUnavailable = "oauth_provider_unavailable"
	OAuthCallbackErrLoginFailed         = "oauth_login_failed"

	// Link-mode result codes on /user/security?tab=oauth&link=failed.
	oauthLinkCodeAlreadyLinked       = "already_linked"
	oauthLinkCodeDuplicateProvider   = "duplicate_provider"
	oauthLinkCodeInvalidUserInfo     = "invalid_userinfo"
	oauthLinkCodeAccessDenied        = "access_denied"
	oauthLinkCodeProviderUnavailable = "provider_unavailable"
	oauthLinkCodeInternal            = "internal"
)

var oauthCallbackErrorAllowlist = map[string]bool{
	OAuthCallbackErrAccessDenied:        true,
	OAuthCallbackErrSignupDisabled:      true,
	OAuthCallbackErrLinkDisabled:        true,
	OAuthCallbackErrEmailUnverified:     true,
	OAuthCallbackErrProviderUnavailable: true,
	OAuthCallbackErrLoginFailed:         true,
}

var oauthLinkCodeAllowlist = map[string]bool{
	oauthLinkCodeAlreadyLinked:       true,
	oauthLinkCodeDuplicateProvider:   true,
	oauthLinkCodeInvalidUserInfo:     true,
	oauthLinkCodeAccessDenied:        true,
	oauthLinkCodeProviderUnavailable: true,
	oauthLinkCodeInternal:            true,
}

// oauthLoginResult is the outcome a login callback hands the SPA. Exactly
// one shape is populated; the constructors below are the only way to build
// one so a caller cannot combine a success with an MFA token.
type oauthLoginResult struct {
	Provider          models.OAuthProvider
	Success           bool
	ErrorCode         string
	RequiresMFA       bool
	MFAToken          string
	WebAuthnAvailable bool
}

func oauthLoginSuccess(p models.OAuthProvider) oauthLoginResult {
	return oauthLoginResult{Provider: p, Success: true}
}

func oauthLoginFailure(p models.OAuthProvider, code string) oauthLoginResult {
	return oauthLoginResult{Provider: p, ErrorCode: code}
}

func oauthLoginMFA(p models.OAuthProvider, token string, webauthnAvailable bool) oauthLoginResult {
	return oauthLoginResult{Provider: p, RequiresMFA: true, MFAToken: token, WebAuthnAvailable: webauthnAvailable}
}

// SetSPAURL records this tier's SPA origin (see the spaBaseURL field).
func (h *AuthHandler) SetSPAURL(u string) {
	h.spaBaseURL = strings.TrimRight(strings.TrimSpace(u), "/")
}

// spaURL is the sole post-callback destination origin for this tier.
func (h *AuthHandler) spaURL() string {
	if h.spaBaseURL != "" {
		return h.spaBaseURL
	}
	if h.config != nil {
		return strings.TrimRight(h.config.Server.FrontendURL, "/")
	}
	return ""
}

// oauthLoginCallbackURL renders the closed login-callback contract.
func (h *AuthHandler) oauthLoginCallbackURL(res oauthLoginResult) string {
	base := h.spaURL() + oauthCallbackPath
	switch {
	case res.RequiresMFA:
		frag := url.Values{}
		frag.Set("requiresMfa", "true")
		frag.Set("mfaToken", res.MFAToken)
		frag.Set("webauthnAvailable", strconv.FormatBool(res.WebAuthnAvailable))
		return base + "#" + frag.Encode()
	case res.Success:
		q := url.Values{}
		q.Set("success", "true")
		q.Set("provider", string(res.Provider))
		return base + "?" + q.Encode()
	default:
		code := res.ErrorCode
		if !oauthCallbackErrorAllowlist[code] {
			code = OAuthCallbackErrLoginFailed
		}
		q := url.Values{}
		q.Set("success", "false")
		q.Set("error", code)
		return base + "?" + q.Encode()
	}
}

// writeOAuthLoginRedirect is the ONLY writer of a login-callback redirect.
func (h *AuthHandler) writeOAuthLoginRedirect(w http.ResponseWriter, r *http.Request, res oauthLoginResult) {
	setCallbackRedirectHeaders(w)
	http.Redirect(w, r, h.oauthLoginCallbackURL(res), http.StatusFound)
}

// oauthLinkReturnURL renders the link-mode return contract. Kept separate
// from the login builder on purpose: link mode never mints tokens and has
// its own page to land on.
func (h *AuthHandler) oauthLinkReturnURL(p models.OAuthProvider, ok bool, code string) string {
	q := url.Values{}
	q.Set("tab", "oauth")
	q.Set("provider", string(p))
	if ok {
		q.Set("link", "success")
	} else {
		if !oauthLinkCodeAllowlist[code] {
			code = oauthLinkCodeInternal
		}
		q.Set("link", "failed")
		q.Set("code", code)
	}
	return h.spaURL() + oauthLinkReturnPath + "?" + q.Encode()
}

// writeOAuthLinkRedirect is the ONLY writer of a link-mode redirect.
func (h *AuthHandler) writeOAuthLinkRedirect(w http.ResponseWriter, r *http.Request, p models.OAuthProvider, ok bool, code string) {
	setCallbackRedirectHeaders(w)
	http.Redirect(w, r, h.oauthLinkReturnURL(p, ok, code), http.StatusFound)
}

// relayCompleteURL renders the client API's relay endpoint for a one-shot
// relay id. ok is false when no client surface is configured — the caller
// then refuses the client-tier flow instead of guessing a host.
func (h *AuthHandler) relayCompleteURL(id string) (string, bool) {
	if h.config == nil || id == "" {
		return "", false
	}
	base := strings.TrimRight(strings.TrimSpace(h.config.Server.Client.PublicURL), "/")
	if base == "" {
		return "", false
	}
	q := url.Values{}
	q.Set("relay", id)
	return base + oauthRelayCompletePath + "?" + q.Encode(), true
}

// writeRelayRedirect is the ONLY writer of the relay redirect.
func (h *AuthHandler) writeRelayRedirect(w http.ResponseWriter, r *http.Request, target string) {
	setCallbackRedirectHeaders(w)
	http.Redirect(w, r, target, http.StatusFound)
}

func setCallbackRedirectHeaders(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}

// oauthLoginErrorCode maps an application error to the coarse allowlisted
// code the SPA may see plus the structured `outcome` for the server log.
// Raw error text never reaches a URL; the default is the generic code, not
// an HTTP error (errquality R3 does not apply to a redirect code).
func oauthLoginErrorCode(err error) (code, outcome string) {
	switch {
	case errors.Is(err, services.ErrOAuthSignupDisabled):
		return OAuthCallbackErrSignupDisabled, "signup_disabled"
	case errors.Is(err, services.ErrOAuthLinkDisabled):
		return OAuthCallbackErrLinkDisabled, "link_disabled"
	case errors.Is(err, services.ErrOAuthEmailUnverified):
		return OAuthCallbackErrEmailUnverified, "email_unverified"
	case errors.Is(err, services.ErrAuthPolicyUnavailable):
		return OAuthCallbackErrProviderUnavailable, "policy_unavailable"
	case errors.Is(err, services.ErrInvalidCredentials):
		return OAuthCallbackErrLoginFailed, "invalid_credentials"
	}
	return OAuthCallbackErrLoginFailed, "internal_error"
}

var idpErrorToken = regexp.MustCompile(`^[a-z_]{1,64}$`)

// sanitizeIdPError reduces the IdP's `error` parameter to a plain OAuth
// error token for the log line; anything else is "unrecognized". The value
// is never copied to the SPA URL — the SPA sees oauth_access_denied.
func sanitizeIdPError(raw string) string {
	if idpErrorToken.MatchString(raw) {
		return raw
	}
	return "unrecognized"
}
```

- [ ] **Step 4: Run the tests**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/... ./internal/shared/config/... -count=1 && go vet ./...`
Expected: `ok` everywhere — this commit is green.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/auth/handlers/oauth_callback_redirect.go backend/internal/core/auth/handlers/oauth_callback_redirect_test.go backend/internal/core/auth/handlers/auth_handler.go backend/internal/shared/config/config.go
git commit -m "feat(auth): closed OAuth callback contract — one per-tier builder file, allowlisted codes, MFA in the fragment, relay destination"
```

---

### Task 6: One-shot state, provider binding, deferred browser binding, relay records

**Files:**
- Modify: `backend/internal/core/auth/services/oauth_state_service.go` (`OAuthStateService` interface lines 18-27; `ValidateOAuthState` lines 157-190; new relay types/methods)
- Create: `backend/internal/core/auth/services/oauth_state_service_test.go`
- Modify: `backend/internal/core/auth/handlers/oauth_state_binding.go` (`verifyOAuthStateBinding` lines 91-124; new `verifyRelayBinding`)
- Modify: `backend/internal/core/auth/handlers/oauth_state_binding_test.go` (all six tests; new relay tests)
- Modify: `backend/internal/core/auth/handlers/auth_handler.go` (`resolveStateForCallback` lines 814-860 and its five call sites 915, 1034, 1188, 1295, 1367)
- Modify: `backend/internal/core/auth/CLAUDE.md` ("OAuth state is 10 minutes in Redis" and "OAuth state is bound to the browser" invariants, lines 667-668)

**Interfaces:**
- Consumes: `OAuthStateStore.Take` (`oauth_state_service.go:84`, Redis `GETDEL` / memory store), `GenerateOAuthCSRF`, `utils.EncryptOAuthToken` / `DecryptOAuthToken`, `NewMemoryOAuthStateStore`.
- Produces: `type OAuthRelayRecord struct{Tier string; Provider models.OAuthProvider; CSRF, Mode, LinkUserUUID string; UserInfo map[string]interface{}; Tokens *models.OAuthProviderTokens; SecurityContext *models.SecurityContext; DeviceInfo *models.DeviceInfo; CreatedAt, ExpiresAt time.Time}`; `const OAuthRelayTTL = 60 * time.Second`; `OAuthStateService.StoreOAuthRelay(ctx, *OAuthRelayRecord) (string, error)`, `TakeOAuthRelay(ctx, id string) (*OAuthRelayRecord, error)`; `verifyOAuthStateBinding(r, claims) (deferred bool, err error)`; `verifyRelayBinding(r, csrf string) error`; `type stateResolution struct{info *services.OAuthStateInfo; claims *services.OAuthStateClaims; bindingDeferred bool}`; `func (h *AuthHandler) resolveStateForCallback(ctx, raw string, provider models.OAuthProvider) (*stateResolution, error)`. Consumed by Task 7.

- [ ] **Step 1: Write the failing service tests**

Create `backend/internal/core/auth/services/oauth_state_service_test.go`:

```go
package services

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
)

const stateTestEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newStateServiceForTest(t *testing.T) OAuthStateService {
	t.Helper()
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", stateTestEncryptionKey)
	return NewOAuthStateService(NewMemoryOAuthStateStore())
}

func TestValidateOAuthState_IsOneShot(t *testing.T) {
	svc := newStateServiceForTest(t)
	ctx := context.Background()
	if _, err := svc.StoreOAuthState(ctx, &StoreOAuthStateRequest{Provider: models.OAuthProviderGoogle, Tier: AudienceClient, State: "nonce-1"}); err != nil {
		t.Fatal(err)
	}
	first, err := svc.ValidateOAuthState(ctx, "nonce-1")
	if err != nil || first == nil || first.Provider != models.OAuthProviderGoogle {
		t.Fatalf("first validation: %+v %v", first, err)
	}
	if _, err := svc.ValidateOAuthState(ctx, "nonce-1"); err == nil {
		t.Fatal("a replayed state must be refused — the first validation consumed it")
	}
}

func TestValidateOAuthState_ConcurrentPresentationsHaveOneWinner(t *testing.T) {
	svc := newStateServiceForTest(t)
	ctx := context.Background()
	if _, err := svc.StoreOAuthState(ctx, &StoreOAuthStateRequest{Provider: models.OAuthProviderGoogle, State: "nonce-race"}); err != nil {
		t.Fatal(err)
	}
	const n = 64
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := svc.ValidateOAuthState(ctx, "nonce-race"); err == nil {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("winners = %d, want exactly 1 (Get-then-Delete would let several through)", wins.Load())
	}
}

func TestValidateOAuthState_ExpiredIsRefused(t *testing.T) {
	svc := newStateServiceForTest(t)
	ctx := context.Background()
	if _, err := svc.StoreOAuthState(ctx, &StoreOAuthStateRequest{Provider: models.OAuthProviderGoogle, State: "nonce-old", ExpiryDuration: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := svc.ValidateOAuthState(ctx, "nonce-old"); err == nil {
		t.Fatal("an expired state must be refused")
	}
}

func TestOAuthRelay_RoundTripIsEncryptedAndOneShot(t *testing.T) {
	svc := newStateServiceForTest(t)
	store := NewMemoryOAuthStateStore()
	svc = NewOAuthStateService(store)
	ctx := context.Background()
	rec := &OAuthRelayRecord{
		Tier: AudienceClient, Provider: models.OAuthProviderGitHub, CSRF: "nonce-9",
		UserInfo:        map[string]interface{}{"email": "u@example.com", "provider_id": "gh-1", "email_verified": true, "name": "U"},
		Tokens:          &models.OAuthProviderTokens{AccessToken: "idp-at", TokenType: "Bearer"},
		SecurityContext: &models.SecurityContext{IPAddress: "203.0.113.5"},
		DeviceInfo:      &models.DeviceInfo{DeviceID: "dev-1"},
	}
	id, err := svc.StoreOAuthRelay(ctx, rec)
	if err != nil || len(id) < 32 {
		t.Fatalf("id=%q err=%v", id, err)
	}
	raw, err := store.Get(ctx, "oauth:relay:"+id)
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"u@example.com", "idp-at", "nonce-9", "gh-1"} {
		if string(raw) != "" && containsBytes(raw, plain) {
			t.Fatalf("relay record stored in clear: contains %q", plain)
		}
	}
	got, err := svc.TakeOAuthRelay(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != AudienceClient || got.Provider != models.OAuthProviderGitHub || got.CSRF != "nonce-9" ||
		got.UserInfo["email"] != "u@example.com" || got.UserInfo["email_verified"] != true ||
		got.Tokens == nil || got.Tokens.AccessToken != "idp-at" || got.DeviceInfo.DeviceID != "dev-1" || got.SecurityContext.IPAddress != "203.0.113.5" {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if _, err := svc.TakeOAuthRelay(ctx, id); err == nil {
		t.Fatal("a relay id is single-use")
	}
	if _, err := svc.TakeOAuthRelay(ctx, "never-stored"); err == nil {
		t.Fatal("an unknown relay id must be refused")
	}
}

func TestOAuthRelay_ExpiresWithTTL(t *testing.T) {
	if OAuthRelayTTL != 60*time.Second {
		t.Fatalf("OAuthRelayTTL = %v, want 60s (spec §4.10)", OAuthRelayTTL)
	}
	svc := newStateServiceForTest(t)
	ctx := context.Background()
	id, err := svc.StoreOAuthRelay(ctx, &OAuthRelayRecord{Tier: AudienceClient, Provider: models.OAuthProviderGoogle, CSRF: "n", ExpiresAt: time.Now().Add(-time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TakeOAuthRelay(ctx, id); err == nil {
		t.Fatal("a record past its own ExpiresAt must be refused even if the store still holds it")
	}
}

func containsBytes(b []byte, s string) bool {
	return len(s) > 0 && len(b) >= len(s) && func() bool {
		for i := 0; i+len(s) <= len(b); i++ {
			if string(b[i:i+len(s)]) == s {
				return true
			}
		}
		return false
	}()
}
```

Update `backend/internal/core/auth/handlers/oauth_state_binding_test.go` — every call site now reads `deferred, err := verifyOAuthStateBinding(…)`; the four "reject" tests additionally assert `!deferred`; `TestOAuthStateBinding_AcceptsMatchingCookie` asserts `deferred == false && err == nil`; replace `TestOAuthStateBinding_AllowsCrossHostTierSplit` (line 70-79) with:

```go
func TestOAuthStateBinding_DefersCrossHostTierSplit(t *testing.T) {
	// ADR-0003: the client tier starts its flow on api.* but every
	// provider callback lands on console.* (one registered redirect URI
	// per provider). The cookie set by api.* is not sent to console.*, so
	// binding cannot be verified HERE — it is deferred to the relay
	// endpoint on api.*, which requires the cookie. It is never accepted.
	r := callbackRequest("console.example.com", "")

	deferred, err := verifyOAuthStateBinding(r, stateClaims("nonce-abc", "api.example.com"))
	if err != nil || !deferred {
		t.Fatalf("a cross-host tier-split callback must be DEFERRED, not bound or rejected: deferred=%v err=%v", deferred, err)
	}
}

func TestVerifyRelayBinding(t *testing.T) {
	// The relay endpoint runs on the start host, so the cookie is REQUIRED.
	r := httptest.NewRequest(http.MethodGet, "/v1/auth/client/oauth/complete?relay=x", nil)
	r.Host = "api.example.com"
	if err := verifyRelayBinding(r, "nonce-abc"); err == nil {
		t.Fatal("a relay without the start-host state cookie must be refused")
	}
	r.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "victims-own-nonce"})
	if err := verifyRelayBinding(r, "nonce-abc"); err == nil {
		t.Fatal("a foreign nonce must be refused")
	}
	r2 := httptest.NewRequest(http.MethodGet, "/v1/auth/client/oauth/complete?relay=x", nil)
	r2.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: "nonce-abc"})
	if err := verifyRelayBinding(r2, "nonce-abc"); err != nil {
		t.Fatalf("matching nonce must bind: %v", err)
	}
	if err := verifyRelayBinding(r2, ""); err == nil {
		t.Fatal("a record without a nonce can never bind")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/services/ -run 'ValidateOAuthState|OAuthRelay' -count=1; go test ./internal/core/auth/handlers/ -run 'OAuthStateBinding|VerifyRelayBinding' -count=1`
Expected: services — `TestValidateOAuthState_IsOneShot` and `_ConcurrentPresentationsHaveOneWinner` FAIL (replay accepted; many winners), relay tests FAIL to compile; handlers FAIL to compile (`verifyOAuthStateBinding` returns one value).

- [ ] **Step 3: Implement**

In `backend/internal/core/auth/services/oauth_state_service.go`:

(a) extend the `OAuthStateService` interface (line 18-27):

```go
	// StoreOAuthRelay persists the IdP half of a client-tier login (see
	// OAuthRelayRecord) under a fresh one-shot id for OAuthRelayTTL and
	// returns the id. The record is encrypted at rest.
	StoreOAuthRelay(ctx context.Context, rec *OAuthRelayRecord) (string, error)
	// TakeOAuthRelay atomically returns and removes a relay record. A
	// second call with the same id, an unknown id or a record past its
	// ExpiresAt is an error.
	TakeOAuthRelay(ctx context.Context, id string) (*OAuthRelayRecord, error)
```

(b) add after `OAuthStateInfo` (line 75):

```go
// OAuthRelayRecord carries the IdP half of a client-tier web login from the
// operator-host callback — the only place the provider's redirect URI
// points — to the client API host, the only host that can set the client
// refresh cookie. It holds everything the application half needs and the
// state's CSRF nonce so the relay endpoint can verify the browser binding
// against the cookie the client API host set at start. Stored encrypted
// (utils.EncryptOAuthToken) under oauth:relay:<id> for OAuthRelayTTL.
type OAuthRelayRecord struct {
	Tier            string                     `json:"tier"`
	Provider        models.OAuthProvider       `json:"provider"`
	CSRF            string                     `json:"csrf"`
	Mode            string                     `json:"mode,omitempty"`
	LinkUserUUID    string                     `json:"linkUserUuid,omitempty"`
	UserInfo        map[string]interface{}     `json:"userInfo"`
	Tokens          *models.OAuthProviderTokens `json:"tokens,omitempty"`
	SecurityContext *models.SecurityContext    `json:"securityContext,omitempty"`
	DeviceInfo      *models.DeviceInfo         `json:"deviceInfo,omitempty"`
	CreatedAt       time.Time                  `json:"createdAt"`
	ExpiresAt       time.Time                  `json:"expiresAt"`
}

// OAuthRelayTTL bounds the hop between the operator-host callback and the
// client API host: one browser redirect, so seconds, not minutes.
const OAuthRelayTTL = 60 * time.Second
```

(c) replace the body of `ValidateOAuthState` (lines 157-190) with the atomic take — the deferred-delete goroutine is gone:

```go
func (s *oAuthStateService) ValidateOAuthState(ctx context.Context, state string) (*OAuthStateInfo, error) {
	if state == "" {
		return nil, fmt.Errorf("OAuth state is required")
	}
	// ONE-SHOT: Take (Redis GETDEL) returns the row to exactly one caller.
	// The previous Get + deferred Delete let two concurrent callbacks both
	// read the same state — the replay window the signed JWT alone cannot
	// close, because the JWT is valid for ten minutes.
	stateData, err := s.store.Take(ctx, s.buildStateKey(state))
	if err != nil {
		return nil, fmt.Errorf("OAuth state not found, expired or already used: %w", err)
	}
	var stateInfo OAuthStateInfo
	if err := json.Unmarshal(stateData, &stateInfo); err != nil {
		return nil, fmt.Errorf("failed to deserialize OAuth state: %w", err)
	}
	// Belt and braces: the store's TTL should have evicted it already.
	if time.Now().After(stateInfo.ExpiresAt) {
		return nil, fmt.Errorf("OAuth state has expired")
	}
	return &stateInfo, nil
}
```

(d) add the relay methods (after `CleanupExpiredStates`):

```go
func (s *oAuthStateService) StoreOAuthRelay(ctx context.Context, rec *OAuthRelayRecord) (string, error) {
	if rec == nil || rec.CSRF == "" || rec.Tier == "" || rec.Provider == "" {
		return "", fmt.Errorf("oauth relay: tier, provider and csrf are required")
	}
	id, err := GenerateOAuthCSRF()
	if err != nil {
		return "", fmt.Errorf("oauth relay: mint id: %w", err)
	}
	now := time.Now()
	rec.CreatedAt = now
	if rec.ExpiresAt.IsZero() {
		rec.ExpiresAt = now.Add(OAuthRelayTTL)
	}
	plain, err := json.Marshal(rec)
	if err != nil {
		return "", fmt.Errorf("oauth relay: serialize: %w", err)
	}
	// The record carries the IdP tokens and the user's email: encrypted at
	// rest with the same AES-256-GCM helper the provider tokens use.
	sealed, err := utils.EncryptOAuthToken(string(plain))
	if err != nil {
		return "", fmt.Errorf("oauth relay: encrypt: %w", err)
	}
	if err := s.store.Set(ctx, s.buildRelayKey(id), []byte(sealed), OAuthRelayTTL); err != nil {
		return "", fmt.Errorf("oauth relay: store: %w", err)
	}
	return id, nil
}

func (s *oAuthStateService) TakeOAuthRelay(ctx context.Context, id string) (*OAuthRelayRecord, error) {
	if id == "" {
		return nil, fmt.Errorf("oauth relay: id is required")
	}
	sealed, err := s.store.Take(ctx, s.buildRelayKey(id))
	if err != nil {
		return nil, fmt.Errorf("oauth relay not found, expired or already used: %w", err)
	}
	plain, err := utils.DecryptOAuthToken(string(sealed))
	if err != nil {
		return nil, fmt.Errorf("oauth relay: decrypt: %w", err)
	}
	var rec OAuthRelayRecord
	if err := json.Unmarshal([]byte(plain), &rec); err != nil {
		return nil, fmt.Errorf("oauth relay: deserialize: %w", err)
	}
	if time.Now().After(rec.ExpiresAt) {
		return nil, fmt.Errorf("oauth relay has expired")
	}
	return &rec, nil
}

func (s *oAuthStateService) buildRelayKey(id string) string {
	return fmt.Sprintf("oauth:relay:%s", id)
}
```

Add `"github.com/orkestra/backend/internal/shared/utils"` to the file's imports (`auth_service.go` in the same package already imports it).

In `backend/internal/core/auth/handlers/oauth_state_binding.go` replace `verifyOAuthStateBinding` (lines 91-124) with:

```go
// verifyOAuthStateBinding checks that the callback is being made by the
// browser that started the flow.
//
// Three outcomes. (false, nil): the cookie matched — bound. (_, err): a
// foreign cookie, a missing cookie on the starting host, or a state with no
// StartHost — rejected. (true, nil): DEFERRED — the ADR-0003 tier split puts
// client-tier starts on `api.*` while every provider callback lands on
// `console.*`, so the cookie set at start cannot reach this request; the
// caller may continue ONLY by handing the flow to the relay endpoint on the
// start host, which requires that cookie (verifyRelayBinding). A cross-host
// callback is never simply accepted: before v4.3 it was, which made the
// "exception" the client tier's normal path and left login CSRF open there.
func verifyOAuthStateBinding(r *http.Request, claims *services.OAuthStateClaims) (deferred bool, err error) {
	if claims == nil || claims.CSRF == "" {
		return false, ErrOAuthStateNotBound
	}

	cookie, cerr := r.Cookie(OAuthStateCookieName)
	if cerr == nil && cookie.Value != "" {
		if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(claims.CSRF)) == 1 {
			return false, nil
		}
		// A cookie that names a different flow is the clearest signal
		// there is: this browser started something else.
		return false, fmt.Errorf("%w: state nonce does not match the browser's cookie", ErrOAuthStateNotBound)
	}

	if claims.StartHost == "" {
		return false, fmt.Errorf("%w: state carries no start host", ErrOAuthStateNotBound)
	}
	if sameHost(claims.StartHost, r.Host) {
		return false, fmt.Errorf("%w: no state cookie presented on the starting host", ErrOAuthStateNotBound)
	}
	return true, nil
}

// verifyRelayBinding is the relay endpoint's check: it runs on the host
// that set the state cookie at start, so the cookie is REQUIRED and must
// equal the relay record's nonce. Fails closed on every other shape.
func verifyRelayBinding(r *http.Request, csrf string) error {
	if csrf == "" {
		return ErrOAuthStateNotBound
	}
	cookie, err := r.Cookie(OAuthStateCookieName)
	if err != nil || cookie.Value == "" {
		return fmt.Errorf("%w: no state cookie presented to the relay", ErrOAuthStateNotBound)
	}
	if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(csrf)) != 1 {
		return fmt.Errorf("%w: relay nonce does not match the browser's cookie", ErrOAuthStateNotBound)
	}
	return nil
}
```

Drop the now-unused `slog` import from that file if nothing else uses it.

In `backend/internal/core/auth/handlers/auth_handler.go` replace `resolveStateForCallback` (lines 814-860) with:

```go
// stateResolution is what a callback learns from a valid state.
type stateResolution struct {
	info   *services.OAuthStateInfo
	claims *services.OAuthStateClaims
	// bindingDeferred is true for a client-tier LOGIN flow: its state cookie
	// lives on the client API host and its refresh cookie can only be set
	// there, so the operator-host callback must hand the flow to the relay
	// endpoint (which verifies the cookie) instead of completing it. Never
	// true for the operator/legacy tiers or for link mode.
	bindingDeferred bool
}

// resolveStateForCallback validates the signed-state JWT presented to a
// callback, binds it to the browser, consumes the one-shot Redis row and
// cross-checks it against the JWT and the endpoint. Order (spec §4.10 v4.3):
// signature/expiry → browser binding → atomic take → tier → PROVIDER →
// link-mode pair. Every failure is one generic error so a caller renders
// the same 400 whatever the reason — an attacker must not learn which
// check rejected them. All of it runs before the IdP `error`, the code or
// any profile is looked at.
func (h *AuthHandler) resolveStateForCallback(ctx context.Context, raw string, provider models.OAuthProvider) (*stateResolution, error) {
	if len(h.stateSecret) == 0 {
		return nil, fmt.Errorf("oauth state secret not configured")
	}
	claims, err := services.ValidateOAuthStateToken(h.stateSecret, raw)
	if err != nil {
		return nil, fmt.Errorf("invalid OAuth state: %w", err)
	}
	r, ok := ctx.Value("http_request").(*http.Request)
	if !ok || r == nil {
		return nil, fmt.Errorf("%w: no request available to verify against", ErrOAuthStateNotBound)
	}
	deferred, err := verifyOAuthStateBinding(r, claims)
	if err != nil {
		return nil, err
	}
	clientLogin := claims.Tier == services.AudienceClient && claims.Mode != services.OAuthStateModeLink
	if deferred && !clientLogin {
		return nil, fmt.Errorf("%w: a cross-host callback can only be relayed for a client-tier login", ErrOAuthStateNotBound)
	}
	// A client-tier login ALWAYS completes on the client API host — even
	// when a cookie happens to be present here, the api.* refresh cookie
	// cannot be set from the operator host.
	deferred = clientLogin

	stateInfo, err := h.oauthStateService.ValidateOAuthState(ctx, claims.CSRF) // atomic one-shot take
	if err != nil {
		return nil, fmt.Errorf("OAuth state not found, expired or already used: %w", err)
	}
	if stateInfo.Tier != claims.Tier {
		return nil, fmt.Errorf("OAuth state tier mismatch")
	}
	if stateInfo.Provider != provider {
		return nil, fmt.Errorf("OAuth state provider mismatch")
	}
	// Cross-check the link-mode pair (mode + linkUserUUID) — if either
	// side stamped a link flow but the other didn't, treat it as a forged
	// state. Both empty (login) or both populated (link with matching
	// UUIDs) is fine.
	if stateInfo.Mode != claims.Mode || stateInfo.LinkUserUUID != claims.LinkUserUUID {
		return nil, fmt.Errorf("OAuth state mode mismatch")
	}
	return &stateResolution{info: stateInfo, claims: claims, bindingDeferred: deferred}, nil
}
```

Adapt the five existing call sites so the package compiles — `stateInfo, claims, err := h.resolveStateForCallback(ctx, state)` becomes `res, err := h.resolveStateForCallback(ctx, state, models.OAuthProvider<X>)` followed by `stateInfo, claims := res.info, res.claims` — at `HandleGoogleCallbackHTTP` (915, Google), `HandleDiscordCallbackHTTP` (1034, Discord), `HandleAppleCallbackHTTP` (1188, Apple; the variables are already declared there, assign instead), `HandleAppleCallback` (1295, Apple), `HandleGitHubCallback` (1367, GitHub). Their behaviour is otherwise unchanged (they still cannot relay); Task 7 replaces all five.

- [ ] **Step 4: Run the tests**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/... -count=1 && go vet ./...`
Expected: `ok`, including the concurrency test (run it with `-race -count=5` once: `go test ./internal/core/auth/services/ -run ConcurrentPresentations -race -count=5`).

- [ ] **Step 5: Document**

In `backend/internal/core/auth/CLAUDE.md` replace the two invariant bullets at lines 667-668 ("OAuth state is 10 minutes in Redis." and "OAuth state is bound to the browser that started the flow.") with:

```markdown
- **OAuth state is 10 minutes in Redis and ONE-SHOT.** `ValidateOAuthState` consumes the row with the store's atomic `Take` (Redis `GETDEL`); a `Get` followed by a deferred delete — the shape it had until v4.3 — let two concurrent callbacks both read one state. Exactly one presentation proceeds; a replay is a generic 400. `oauth_state_service_test.go` pins the one-winner race on the in-memory store.
- **OAuth state is bound to the endpoint's provider, the tier, the link-mode pair and the browser, in that order after the signature.** `resolveStateForCallback(ctx, raw, provider)` runs signature/expiry → browser binding → atomic take → `tier` → `provider` (`stateInfo.Provider` must equal the callback's provider — a Google state presented to the Discord callback is a 400) → `mode`/`linkUserUUID`, every mismatch the same generic error, all before the IdP `error`, the code or any profile is read.
- **The browser binding is verified where the cookie lives.** The signed state + one-shot row prove a callback belongs to a flow *we* started, not that it belongs to *this browser*: without a binding, login CSRF (an attacker-started flow finished by a victim's browser lands the victim in the attacker's session; in `mode=link` it attaches the victim's IdP identity to the attacker's account) is open. Both start endpoints therefore also drop the CSRF nonce into an HttpOnly `orkestra_oauth_state` cookie (SameSite=Lax — Strict would suppress the top-level redirect back from the IdP) and the callback requires the two to match (`handlers/oauth_state_binding.go`). Fails closed: a mismatched cookie, an absent cookie on the starting host, or a state with no `shost` claim are all rejected. The ADR-0003 tier split puts client-tier starts on `api.*` while every provider callback lands on `console.*`, so that cookie cannot reach the callback — such a callback is **deferred**, never accepted: the operator host does the IdP half only and hands the flow to `GET /v1/auth/client/oauth/complete` on the client API host through a one-shot relay record; the relay endpoint **requires** the cookie (`verifyRelayBinding`) and fails closed without it. The SPA must call the start endpoint with `credentials: 'include'` or the cookie is never stored — `frontend-admin`'s `socialAuthUtils.ts` and RTK `baseApi` both do.
```

- [ ] **Step 6: Commit**

```bash
cd /home/tore/orkestra && git add backend/internal/core/auth/services/oauth_state_service.go backend/internal/core/auth/services/oauth_state_service_test.go backend/internal/core/auth/handlers/oauth_state_binding.go backend/internal/core/auth/handlers/oauth_state_binding_test.go backend/internal/core/auth/handlers/auth_handler.go backend/internal/core/auth/CLAUDE.md
git commit -m "fix(auth): OAuth state is one-shot (atomic Take) and bound to the endpoint's provider; cross-host browser binding is deferred to a relay record, never skipped"
```

---

### Task 7: One callback flow — trust before destination, the client-tier relay, four thin handlers, GitHub raw, wiring, `CLIENT_API_URL`, OpenAPI, structural scan

**Files:**
- Create: `backend/internal/core/auth/handlers/oauth_callback_flow.go`
- Create: `backend/internal/core/auth/handlers/oauth_callback_flow_test.go`
- Create: `backend/internal/core/auth/handlers/oauth_callback_scan_test.go`
- Modify: `backend/internal/core/auth/handlers/auth_handler.go` — field type `oauthResolver services.OAuthResolver` (line 33) + `NewAuthHandler` parameter type (line 177); delete `oauthSignupDisabled` (242-247), `writeOAuthCallbackError` (293-297), `redirectOAuthSignupDisabled` (299-311), `finishOAuthMFAPartialRedirect` (693-733), `resolveOAuthMFAPartialRedirect` (735-765), `resolveOAuthLinkRedirect` (767-812), `OAuthCallbackRequest` / `OAuthCallbackResponse` (877-889), `HandleGoogleCallbackHTTP` (896-1018), `HandleDiscordCallbackHTTP` (1020-1121), `HandleAppleCallbackHTTP` (1123-1290), `HandleAppleCallback` (1292-1363), `HandleGitHubCallback` (1365-1439); rewrite `finishOAuthLinkRedirect` (635-691); `RegisterOAuthRoutes` (2448-2470); `InitiateOAuthLogin` / `InitiateOAuthLink` `RedirectURI` (449-462, 575-585); reword the `RegisterOAuthLinkRoute` Huma `Description` (2539)
- Modify: `backend/internal/core/auth/handlers/structured_logging_safety_test.go` (lines 15-25)
- Modify: `backend/internal/core/auth/handlers/error_mapping_test.go` (delete `TestErrorMapping_WriteOAuthCallbackErrorStaysNeutralAndSanitized` 167-216, `TestOAuthSignupDisabled_MatchesSentinel` 335-345, `TestRedirectOAuthSignupDisabled_*` 347-375)
- Modify: `backend/internal/shared/config/config.go` (`CLIENT_API_URL` after the `config.Server = ServerConfig{…}` literal, line ~254)
- Modify: `docker/.env.example` (after `CLIENT_API_HOST=api.localhost`, line 87)
- Modify: `backend/internal/core/auth/module.go` (after lines 1079 and 1245; inside the `if ri.ClientRouter != nil` block at 1683-1686)
- Regenerate: `backend/openapi/enterprise.json`
- Modify: `backend/internal/core/auth/CLAUDE.md` ("What it owns"; endpoint rows 448-452; new "### OAuth callback contract" section after the state-dispatch section; rules), `docs/site/operating/cookie-hardening-cross-tier.mdx` (new section before "## Verifying")

**Interfaces:**
- Consumes: Task 5 builders; Task 6 `resolveStateForCallback`, `verifyRelayBinding`, `StoreOAuthRelay` / `TakeOAuthRelay`, `OAuthRelayRecord`; `services.OAuthResolver` (Task 3); `clearOAuthStateCookie`, `refreshCookieMaxAge`; `utils.SetRefreshTokenCookie(w, name, token, maxAgeSeconds, domain, secure)`.
- Produces: `oauthCallbackParams`, `queryCallbackParams`, `formCallbackParams`, `oauthExchange`, `exchangeWithUserInfo()`, `exchangeAppleIDToken()`, `userInfoMap`, `providerTokens`; `func (h *AuthHandler) completeOAuthCallback(w, r, provider, params, exchange)`; `func (h *AuthHandler) finishOAuthCompletion(w, r, target *AuthHandler, provider, userInfo, tokens, secCtx, devInfo)`; `HandleGitHubCallbackHTTP`; `HandleOAuthRelayCompleteHTTP`; `RegisterOAuthRelayRoute(router chi.Router)`; `config.AudienceConfig.PublicURL` populated from `CLIENT_API_URL`. Consumed by Task 8 (harness reuse).

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/handlers/oauth_callback_flow_test.go`:

```go
package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- fakes: each embeds the interface so an unexpected call panics loudly ---

type fakeStateService struct {
	services.OAuthStateService
	info      *services.OAuthStateInfo
	err       error
	validated int
	stored    []*services.StoreOAuthStateRequest
	// relay: the last stored record, handed out exactly once by TakeOAuthRelay.
	relay      *services.OAuthRelayRecord
	relayTaken bool
	relayErr   error
}

func (f *fakeStateService) ValidateOAuthState(context.Context, string) (*services.OAuthStateInfo, error) {
	f.validated++
	if f.err != nil {
		return nil, f.err
	}
	return f.info, nil
}

func (f *fakeStateService) StoreOAuthState(_ context.Context, req *services.StoreOAuthStateRequest) (*services.OAuthStateInfo, error) {
	f.stored = append(f.stored, req)
	return &services.OAuthStateInfo{State: req.State, Tier: req.Tier, Provider: req.Provider, RedirectURI: req.RedirectURI}, nil
}

func (f *fakeStateService) StoreOAuthRelay(_ context.Context, rec *services.OAuthRelayRecord) (string, error) {
	if f.relayErr != nil {
		return "", f.relayErr
	}
	f.relay = rec
	f.relayTaken = false
	return "relay-1", nil
}

func (f *fakeStateService) TakeOAuthRelay(_ context.Context, id string) (*services.OAuthRelayRecord, error) {
	if id != "relay-1" || f.relay == nil || f.relayTaken {
		return nil, errors.New("oauth relay not found, expired or already used")
	}
	f.relayTaken = true
	return f.relay, nil
}

type fakeResolver struct {
	cfg    *services.OAuthProviderConfig
	usable bool
	err    error
	list   []models.OAuthProvider
	calls  int
}

func (f *fakeResolver) Get(context.Context, models.OAuthProvider) (*services.OAuthProviderConfig, bool) {
	return f.cfg, f.cfg != nil
}
func (f *fakeResolver) RedirectURL(context.Context, models.OAuthProvider) string {
	if f.cfg == nil {
		return ""
	}
	return f.cfg.AdditionalConfig["redirect_url"]
}
func (f *fakeResolver) MobileAudience(context.Context, models.OAuthProvider, string) string { return "" }
func (f *fakeResolver) ConfiguredProviders(context.Context) []models.OAuthProvider   { return f.list }
func (f *fakeResolver) OAuthWebProviderUsable(context.Context, services.PolicyAudience, models.OAuthProvider) (*services.OAuthProviderConfig, bool, error) {
	f.calls++
	if f.err != nil {
		return nil, false, f.err
	}
	if !f.usable {
		return nil, false, nil
	}
	return f.cfg, true, nil
}
func (f *fakeResolver) UsableWebProviders(context.Context, services.PolicyAudience) ([]models.OAuthProvider, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.list, nil
}

type fakeProvider struct {
	services.OAuthProviderInterface
	token       *services.TokenResponse
	exchangeErr error
	info        *services.UserInfo
	infoErr     error
	exchanges   int
}

func (p *fakeProvider) ExchangeCodeForToken(context.Context, *services.CodeExchangeRequest) (*services.TokenResponse, error) {
	p.exchanges++
	return p.token, p.exchangeErr
}
func (p *fakeProvider) GetUserInfo(context.Context, string) (*services.UserInfo, error) {
	return p.info, p.infoErr
}
func (p *fakeProvider) ValidateIDToken(context.Context, *services.IDTokenValidationRequest) (*services.UserInfo, error) {
	return p.info, p.infoErr
}
func (p *fakeProvider) GetClientID() string { return "client-id" }
func (p *fakeProvider) GetAuthURL(state, _, redirect string) string {
	return "https://idp.example/authorize?state=" + url.QueryEscape(state) + "&redirect_uri=" + url.QueryEscape(redirect)
}

type fakeFactory struct {
	prov services.OAuthProviderInterface
	err  error
}

func (f fakeFactory) CreateProvider(models.OAuthProvider, *services.OAuthProviderConfig) (services.OAuthProviderInterface, error) {
	return f.prov, f.err
}
func (f fakeFactory) GetSupportedProviders() []models.OAuthProvider { return nil }

type fakeAuthService struct {
	services.AuthService
	resp      *models.TokenResponse
	err       error
	calls     int
	lastInfo  map[string]interface{}
	linkErr   error
	linkCalls int
}

func (f *fakeAuthService) HandleOAuthCallbackWithLinking(_ context.Context, _ models.OAuthProvider, info map[string]interface{}, _ *models.OAuthProviderTokens, _ *models.SecurityContext, _ *models.DeviceInfo) (*models.TokenResponse, error) {
	f.calls++
	f.lastInfo = info
	return f.resp, f.err
}
func (f *fakeAuthService) SelfLinkOAuthFromCallback(context.Context, string, iface.OAuthProvider, map[string]interface{}, *models.OAuthProviderTokens) error {
	f.linkCalls++
	return f.linkErr
}

type fakeJWT struct {
	services.JWTService
	ttl time.Duration
}

func (f fakeJWT) RefreshTokenTTL() time.Duration { return f.ttl }

// --- harness ---

type callbackHarness struct {
	dispatcher *AuthHandler // operator-mux instance that owns the callback routes
	operator   *AuthHandler
	client     *AuthHandler
	state      *fakeStateService
	resolver   *fakeResolver
	provider   *fakeProvider
	opAuth     *fakeAuthService
	clAuth     *fakeAuthService
	secret     []byte
}

const (
	callbackHost  = "console.example"
	clientAPIHost = "api.example"
)

func newCallbackHarness(t *testing.T) *callbackHarness {
	t.Helper()
	cfg := &config.Config{}
	cfg.Server.FrontendURL = "https://legacy.example"
	cfg.Server.Client.Host = clientAPIHost
	cfg.Server.Client.PublicURL = "https://" + clientAPIHost
	cfg.Auth.Cookie.Name = "orkestra_cookie"
	cfg.Auth.Cookie.Secure = true

	provider := &fakeProvider{
		token: &services.TokenResponse{AccessToken: "idp-at", RefreshToken: "idp-rt", TokenType: "Bearer", ExpiresIn: 3600, IDToken: "idp-idt", Scope: []string{"email"}},
		info:  &services.UserInfo{ProviderID: "g-1", Email: "u@example.com", EmailVerified: true, Name: "U", Picture: "https://p"},
	}
	resolver := &fakeResolver{
		cfg:    &services.OAuthProviderConfig{ClientID: "cid", ClientSecret: "csecret", AdditionalConfig: map[string]string{"redirect_url": "https://console.example/v1/auth/oauth/google/callback"}},
		usable: true,
	}
	state := &fakeStateService{info: &services.OAuthStateInfo{Provider: models.OAuthProviderGoogle}}
	mkAuth := func() *fakeAuthService {
		return &fakeAuthService{resp: &models.TokenResponse{
			AccessToken: "orkestra-at", RefreshToken: "orkestra-rt", TokenType: "Bearer", ExpiresIn: 900,
			User: &iface.UserManagementResponse{ID: "u-1", Email: "u@example.com"},
		}}
	}
	opAuth, clAuth := mkAuth(), mkAuth()
	secret := []byte("0123456789abcdef0123456789abcdef")

	mk := func(tier, spa, cookieDomain string, svc services.AuthService, ttl time.Duration) *AuthHandler {
		h := NewAuthHandler(svc, fakeFactory{prov: provider}, resolver, state, nil, fakeJWT{ttl: ttl}, cfg, cookieDomain)
		h.SetTier(tier)
		h.SetStateSecret(secret)
		h.SetSPAURL(spa)
		return h
	}
	// Distinct refresh TTLs per tier so a test can prove the cookie's
	// Max-Age comes from the TARGET tier's JWT service.
	operator := mk(services.AudienceOperator, "https://console.example", "console.example", opAuth, 7*24*time.Hour)
	client := mk(services.AudienceClient, "https://app.example", clientAPIHost, clAuth, 3*24*time.Hour)
	operator.SetTierDispatch(map[string]*AuthHandler{services.AudienceOperator: operator, services.AudienceClient: client})
	return &callbackHarness{dispatcher: operator, operator: operator, client: client, state: state, resolver: resolver, provider: provider, opAuth: opAuth, clAuth: clAuth, secret: secret}
}

type callbackOpts struct {
	tier      string
	mode      string // "" or services.OAuthStateModeLink
	linkUUID  string
	startHost string // defaults to callbackHost (same host); use clientAPIHost for a client-tier flow
	query     string // extra query, e.g. "&error=access_denied"
	noCode    bool
	noState   bool
	badState  bool
	form      bool // Apple form-post
	path      string
	cookie    bool // present the state cookie on THIS request
	provider  models.OAuthProvider
}

const testCSRF = "nonce-1"

func (hx *callbackHarness) request(t *testing.T, o callbackOpts) *http.Request {
	t.Helper()
	startHost := o.startHost
	if startHost == "" {
		startHost = callbackHost
	}
	var signed string
	var err error
	if o.mode == services.OAuthStateModeLink {
		signed, err = services.SignOAuthLinkStateToken(hx.secret, o.tier, testCSRF, o.linkUUID, startHost, 10*time.Minute)
	} else {
		signed, err = services.SignOAuthStateToken(hx.secret, o.tier, testCSRF, startHost, 10*time.Minute)
	}
	if err != nil {
		t.Fatal(err)
	}
	if o.badState {
		signed += "tampered"
	}
	hx.state.info.Tier = o.tier
	hx.state.info.Mode = o.mode
	hx.state.info.LinkUserUUID = o.linkUUID
	if o.provider != "" {
		hx.state.info.Provider = o.provider
	}

	values := url.Values{}
	if !o.noState {
		values.Set("state", signed)
	}
	if !o.noCode {
		values.Set("code", "abc")
	}
	extra, _ := url.ParseQuery(strings.TrimPrefix(o.query, "&"))
	for k, vs := range extra {
		values[k] = vs
	}
	path := o.path
	if path == "" {
		path = "/v1/auth/oauth/google/callback"
	}
	var r *http.Request
	if o.form {
		r = httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(http.MethodGet, path+"?"+values.Encode(), nil)
	}
	r.Host = callbackHost
	if o.cookie {
		r.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: testCSRF})
	}
	// setupMiddleware stashes the raw request for resolveStateForCallback.
	return r.WithContext(context.WithValue(r.Context(), "http_request", r))
}

// relayRequest is the browser arriving at the client API host's relay
// endpoint after the operator-host redirect.
func relayRequest(id, cookie string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/auth/client/oauth/complete?relay="+url.QueryEscape(id), nil)
	r.Host = clientAPIHost
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: OAuthStateCookieName, Value: cookie})
	}
	return r.WithContext(context.WithValue(r.Context(), "http_request", r))
}

func location(t *testing.T, rec *httptest.ResponseRecorder) (string, url.Values, url.Values) {
	t.Helper()
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatalf("no Location; status=%d body=%q", rec.Code, rec.Body.String())
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	frag, _ := url.ParseQuery(u.Fragment)
	return u.Scheme + "://" + u.Host + u.Path, u.Query(), frag
}

// assertNoPII checks the EXACT parameter names of the query and the
// fragment against the forbidden set, and every value against the marker
// strings the harness uses — a substring match on "email" would trip on
// the allowlisted auth.oauth_email_unverified code.
func assertNoPII(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	frag, _ := url.ParseQuery(u.Fragment)
	forbidden := map[string]bool{"access_token": true, "refresh_token": true, "email": true, "user_id": true}
	markers := []string{"u@example.com", "u-1", "orkestra-rt", "orkestra-at", "idp-at", "idp-rt", "idp-idt", "csecret", "abc"}
	for part, vals := range map[string]url.Values{"query": u.Query(), "fragment": frag} {
		for key, vs := range vals {
			if forbidden[key] {
				t.Errorf("Location %s carries forbidden parameter %q", part, key)
			}
			for _, v := range vs {
				for _, m := range markers {
					if v == m || strings.Contains(v, "@") {
						t.Errorf("Location %s parameter %q carries marker/PII value %q", part, key, v)
					}
				}
			}
		}
	}
	for _, m := range []string{"u@example.com", "orkestra-rt", "orkestra-at", "idp-at", "idp-rt"} {
		if strings.Contains(u.Path, m) {
			t.Errorf("Location path carries %q", m)
		}
	}
}

func assertCallbackHeaders(t *testing.T, rec *httptest.ResponseRecorder, expectStateCookieCleared bool) {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body %q)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", rec.Header())
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == OAuthStateCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if cleared != expectStateCookieCleared {
		t.Fatalf("state cookie cleared = %v, want %v", cleared, expectStateCookieCleared)
	}
}

func refreshCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "orkestra_cookie" {
			return c
		}
	}
	return nil
}

func assertNoRefreshCookie(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if c := refreshCookie(rec); c != nil {
		t.Fatalf("no refresh cookie may be written here: %+v", c)
	}
}

// --- operator tier: inline completion ---

func TestCallback_OperatorTierCompletesInline(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true}))
	assertCallbackHeaders(t, rec, true)
	base, q, frag := location(t, rec)
	if base != "https://console.example/auth/callback" {
		t.Fatalf("operator flow must land on OPERATOR_FRONTEND_URL: %q", base)
	}
	if len(q) != 2 || q.Get("success") != "true" || q.Get("provider") != "google" || len(frag) != 0 {
		t.Fatalf("q=%v frag=%v", q, frag)
	}
	c := refreshCookie(rec)
	if c == nil || c.Value != "orkestra-rt" || c.Domain != "console.example" || !c.HttpOnly || !c.Secure {
		t.Fatalf("refresh cookie = %+v", c)
	}
	if c.MaxAge != int((7 * 24 * time.Hour).Seconds()) {
		t.Fatalf("Max-Age = %d, want the OPERATOR tier's refresh TTL", c.MaxAge)
	}
	if hx.opAuth.calls != 1 || hx.clAuth.calls != 0 {
		t.Fatalf("operator=%d client=%d", hx.opAuth.calls, hx.clAuth.calls)
	}
	if hx.opAuth.lastInfo["email_verified"] != true || hx.opAuth.lastInfo["provider_id"] != "g-1" {
		t.Fatalf("userinfo map = %v", hx.opAuth.lastInfo)
	}
	assertNoPII(t, rec)
}

// --- client tier: relay ---

func TestCallback_ClientTierDefersToRelay_NoCookieNoTokenHere(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	// The browser carries no state cookie on console.* — it lives on api.*.
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))
	assertCallbackHeaders(t, rec, false) // the cookie is not here, so it is not cleared here
	if got := rec.Header().Get("Location"); got != "https://api.example/v1/auth/client/oauth/complete?relay=relay-1" {
		t.Fatalf("Location = %q", got)
	}
	assertNoRefreshCookie(t, rec)
	if hx.clAuth.calls != 0 || hx.opAuth.calls != 0 {
		t.Fatal("the operator host must not run the application half of a client-tier flow")
	}
	rec2 := hx.state.relay
	if rec2 == nil || rec2.Tier != services.AudienceClient || rec2.Provider != models.OAuthProviderGoogle || rec2.CSRF != testCSRF ||
		rec2.UserInfo["provider_id"] != "g-1" || rec2.UserInfo["email_verified"] != true || rec2.Tokens == nil || rec2.Tokens.AccessToken != "idp-at" {
		t.Fatalf("relay record = %+v", rec2)
	}
	assertNoPII(t, rec)
}

func TestCallback_ClientTierRelaysEvenWithACookieOnConsole(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost, cookie: true}))
	assertCallbackHeaders(t, rec, false)
	if !strings.HasPrefix(rec.Header().Get("Location"), "https://api.example/v1/auth/client/oauth/complete?relay=") {
		t.Fatalf("a client-tier login always relays: %q", rec.Header().Get("Location"))
	}
	assertNoRefreshCookie(t, rec)
}

func TestCallback_ClientTierWithoutClientSurfaceIsRefused(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.dispatcher.config.Server.Client.PublicURL = ""
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))
	assertCallbackHeaders(t, rec, false)
	base, q, _ := location(t, rec)
	if base != "https://app.example/auth/callback" || q.Get("error") != OAuthCallbackErrProviderUnavailable {
		t.Fatalf("base=%q q=%v", base, q)
	}
	if hx.provider.exchanges != 0 || hx.state.relay != nil {
		t.Fatal("nothing is exchanged or stored when the relay has no destination")
	}
}

func TestRelayComplete_BindsCookieAndSetsClientCookie(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.dispatcher.HandleGoogleCallbackHTTP(httptest.NewRecorder(), hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))

	rec := httptest.NewRecorder()
	hx.client.HandleOAuthRelayCompleteHTTP(rec, relayRequest("relay-1", testCSRF))
	assertCallbackHeaders(t, rec, true) // the cookie lives here → cleared here
	base, q, frag := location(t, rec)
	if base != "https://app.example/auth/callback" || q.Get("success") != "true" || q.Get("provider") != "google" || len(frag) != 0 {
		t.Fatalf("base=%q q=%v frag=%v", base, q, frag)
	}
	c := refreshCookie(rec)
	if c == nil || c.Value != "orkestra-rt" || c.Domain != clientAPIHost || !c.HttpOnly || !c.Secure {
		t.Fatalf("refresh cookie = %+v; must be set by the CLIENT host with the client domain", c)
	}
	if c.MaxAge != int((3 * 24 * time.Hour).Seconds()) {
		t.Fatalf("Max-Age = %d, want the CLIENT tier's refresh TTL", c.MaxAge)
	}
	if hx.clAuth.calls != 1 || hx.opAuth.calls != 0 {
		t.Fatalf("client=%d operator=%d", hx.clAuth.calls, hx.opAuth.calls)
	}
	if hx.clAuth.lastInfo["provider_id"] != "g-1" || hx.clAuth.lastInfo["email_verified"] != true {
		t.Fatalf("relayed userinfo = %v", hx.clAuth.lastInfo)
	}
	assertNoPII(t, rec)
}

func TestRelayComplete_RefusalsAre400WithoutRedirectOrToken(t *testing.T) {
	cases := map[string]func(hx *callbackHarness) *http.Request{
		"missing relay id":        func(hx *callbackHarness) *http.Request { return relayRequest("", testCSRF) },
		"unknown relay id":        func(hx *callbackHarness) *http.Request { return relayRequest("relay-9", testCSRF) },
		"no state cookie":         func(hx *callbackHarness) *http.Request { return relayRequest("relay-1", "") },
		"foreign nonce (CSRF)":    func(hx *callbackHarness) *http.Request { return relayRequest("relay-1", "attackers-nonce") },
		"link-mode record":        func(hx *callbackHarness) *http.Request { hx.state.relay.Mode = services.OAuthStateModeLink; return relayRequest("relay-1", testCSRF) },
		"operator-tier record":    func(hx *callbackHarness) *http.Request { hx.state.relay.Tier = services.AudienceOperator; return relayRequest("relay-1", testCSRF) },
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			hx := newCallbackHarness(t)
			hx.dispatcher.HandleGoogleCallbackHTTP(httptest.NewRecorder(), hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))
			rec := httptest.NewRecorder()
			hx.client.HandleOAuthRelayCompleteHTTP(rec, arrange(hx))
			if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
				t.Fatalf("status=%d loc=%q; want a terminal 400 with no redirect", rec.Code, rec.Header().Get("Location"))
			}
			assertNoRefreshCookie(t, rec)
			if hx.clAuth.calls != 0 {
				t.Fatal("no token may be minted")
			}
		})
	}
}

func TestRelayComplete_IsOneShot(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.dispatcher.HandleGoogleCallbackHTTP(httptest.NewRecorder(), hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))
	first := httptest.NewRecorder()
	hx.client.HandleOAuthRelayCompleteHTTP(first, relayRequest("relay-1", testCSRF))
	if first.Code != http.StatusFound {
		t.Fatalf("first: %d", first.Code)
	}
	second := httptest.NewRecorder()
	hx.client.HandleOAuthRelayCompleteHTTP(second, relayRequest("relay-1", testCSRF))
	if second.Code != http.StatusBadRequest || refreshCookie(second) != nil {
		t.Fatalf("a replayed relay must be a 400 with no cookie: %d", second.Code)
	}
	if hx.clAuth.calls != 1 {
		t.Fatalf("application half ran %d times, want 1", hx.clAuth.calls)
	}
}

func TestRelayComplete_ApplicationErrorMapsToAllowlist(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.clAuth.err = services.ErrOAuthEmailUnverified
	hx.dispatcher.HandleGoogleCallbackHTTP(httptest.NewRecorder(), hx.request(t, callbackOpts{tier: services.AudienceClient, startHost: clientAPIHost}))
	rec := httptest.NewRecorder()
	hx.client.HandleOAuthRelayCompleteHTTP(rec, relayRequest("relay-1", testCSRF))
	assertCallbackHeaders(t, rec, true)
	base, q, _ := location(t, rec)
	if base != "https://app.example/auth/callback" || q.Get("success") != "false" || q.Get("error") != OAuthCallbackErrEmailUnverified {
		t.Fatalf("base=%q q=%v", base, q)
	}
	assertNoRefreshCookie(t, rec)
	assertNoPII(t, rec)
}

// --- trust before destination ---

func TestCallback_TrustBeforeDestination(t *testing.T) {
	cases := map[string]callbackOpts{
		"missing state":                     {tier: services.AudienceClient, startHost: clientAPIHost, noState: true},
		"tampered state":                    {tier: services.AudienceClient, startHost: clientAPIHost, badState: true},
		"no browser binding on same host":   {tier: services.AudienceOperator, cookie: false},
		"IdP error with an invalid state":   {tier: services.AudienceClient, startHost: clientAPIHost, badState: true, query: "&error=access_denied"},
		"cross-host operator-tier state":    {tier: services.AudienceOperator, startHost: clientAPIHost, cookie: false},
		"cross-host link-mode state":        {tier: services.AudienceClient, mode: services.OAuthStateModeLink, linkUUID: "u-1", startHost: clientAPIHost, cookie: false},
		"state stored for another provider": {tier: services.AudienceOperator, cookie: true, provider: models.OAuthProviderDiscord},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			hx := newCallbackHarness(t)
			rec := httptest.NewRecorder()
			hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, o))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if rec.Header().Get("Location") != "" {
				t.Fatalf("no redirect may be issued before trust is established: %q", rec.Header().Get("Location"))
			}
			if hx.provider.exchanges != 0 || hx.opAuth.calls != 0 || hx.clAuth.calls != 0 || hx.state.relay != nil {
				t.Fatal("nothing downstream may run")
			}
			for _, c := range rec.Result().Cookies() {
				if c.Name == OAuthStateCookieName {
					t.Fatal("an untrusted request must not touch the state cookie")
				}
			}
		})
	}
}

func TestCallback_ReplayedOrUnknownState_400(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.state.err = errors.New("OAuth state not found, expired or already used")
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true}))
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("status=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestCallback_ValidStateThenIdPDenial(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true, noCode: true, query: "&error=access_denied&error_description=The+user+u-1+%3Cu%40example.com%3E+said+no"}))
	assertCallbackHeaders(t, rec, true)
	base, q, _ := location(t, rec)
	if base != "https://console.example/auth/callback" || q.Get("success") != "false" || q.Get("error") != OAuthCallbackErrAccessDenied {
		t.Fatalf("base=%q q=%v", base, q)
	}
	assertNoPII(t, rec)
	if hx.resolver.calls != 0 || hx.provider.exchanges != 0 {
		t.Fatal("a denial ends the flow before the provider is resolved")
	}
	if hx.state.validated != 1 {
		t.Fatal("the state must be consumed before the IdP error is interpreted")
	}
}

func TestCallback_MissingCode_Generic(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true, noCode: true}))
	assertCallbackHeaders(t, rec, true)
	_, q, _ := location(t, rec)
	if q.Get("error") != OAuthCallbackErrLoginFailed {
		t.Fatalf("q=%v", q)
	}
}

func TestCallback_ProviderProblemsAreUnavailable(t *testing.T) {
	cases := map[string]func(hx *callbackHarness){
		"config document unreadable": func(hx *callbackHarness) { hx.resolver.err = errors.New("mongo down") },
		"provider disabled mid-flow": func(hx *callbackHarness) { hx.resolver.usable = false },
		"exchange failed":            func(hx *callbackHarness) { hx.provider.exchangeErr = errors.New("idp 500 for u@example.com") },
		"userinfo failed":            func(hx *callbackHarness) { hx.provider.infoErr = errors.New("idp userinfo 502") },
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			hx := newCallbackHarness(t)
			arrange(hx)
			rec := httptest.NewRecorder()
			hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true}))
			assertCallbackHeaders(t, rec, true)
			_, q, _ := location(t, rec)
			if q.Get("error") != OAuthCallbackErrProviderUnavailable {
				t.Fatalf("q=%v", q)
			}
			assertNoPII(t, rec)
			if hx.opAuth.calls != 0 {
				t.Fatal("the application must not be consulted")
			}
		})
	}
}

func TestCallback_ApplicationErrorsMapToAllowlist(t *testing.T) {
	// The default logger is captured so the raw error text (which carries a
	// marker email and user id) is proven absent from logs as well as URLs.
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	cases := map[error]string{
		services.ErrOAuthSignupDisabled:                              OAuthCallbackErrSignupDisabled,
		services.ErrOAuthLinkDisabled:                                OAuthCallbackErrLinkDisabled,
		services.ErrOAuthEmailUnverified:                             OAuthCallbackErrEmailUnverified,
		services.ErrAuthPolicyUnavailable:                            OAuthCallbackErrProviderUnavailable,
		services.ErrInvalidCredentials:                               OAuthCallbackErrLoginFailed,
		errors.New("failed to create user u-1 u@example.com: dup key"): OAuthCallbackErrLoginFailed,
	}
	for err, want := range cases {
		t.Run(err.Error(), func(t *testing.T) {
			logs.Reset()
			hx := newCallbackHarness(t)
			hx.opAuth.err = err
			rec := httptest.NewRecorder()
			hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true}))
			assertCallbackHeaders(t, rec, true)
			_, q, _ := location(t, rec)
			if q.Get("success") != "false" || q.Get("error") != want {
				t.Fatalf("q=%v want error=%s", q, want)
			}
			assertNoRefreshCookie(t, rec)
			assertNoPII(t, rec)
			if strings.Contains(logs.String(), "u@example.com") || strings.Contains(logs.String(), "dup key") {
				t.Fatalf("raw error text reached the logs: %s", logs.String())
			}
			if !strings.Contains(logs.String(), `"msg":"oauth callback failed"`) || !strings.Contains(logs.String(), `"outcome":"`) {
				t.Fatalf("sanitized log line with a stable outcome expected: %s", logs.String())
			}
		})
	}
}

func TestCallback_MFAPartial_FragmentOnly_NoCookie(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.opAuth.resp = &models.TokenResponse{RequiresMFA: true, MFAToken: "challenge-1", WebAuthnAvailable: true, User: &iface.UserManagementResponse{ID: "u-1", Email: "u@example.com"}}
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, cookie: true}))
	assertCallbackHeaders(t, rec, true)
	base, q, frag := location(t, rec)
	if base != "https://console.example/auth/callback" || len(q) != 0 {
		t.Fatalf("base=%q q=%v (MFA carries no query)", base, q)
	}
	if frag.Get("requiresMfa") != "true" || frag.Get("mfaToken") != "challenge-1" || frag.Get("webauthnAvailable") != "true" || len(frag) != 3 {
		t.Fatalf("frag=%v", frag)
	}
	assertNoRefreshCookie(t, rec)
	assertNoPII(t, rec)
}

func TestCallback_LinkMode_OwnContract(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, mode: services.OAuthStateModeLink, linkUUID: "u-1", cookie: true}))
	assertCallbackHeaders(t, rec, true)
	base, q, _ := location(t, rec)
	if base != "https://console.example/user/security" || q.Get("tab") != "oauth" || q.Get("link") != "success" || q.Get("provider") != "google" {
		t.Fatalf("base=%q q=%v", base, q)
	}
	if hx.opAuth.linkCalls != 1 || hx.opAuth.calls != 0 || refreshCookie(rec) != nil {
		t.Fatalf("link=%d login=%d cookie=%v", hx.opAuth.linkCalls, hx.opAuth.calls, refreshCookie(rec))
	}
	assertNoPII(t, rec)

	for err, code := range map[error]string{
		services.ErrOAuthLinkClaimedByOther:                 oauthLinkCodeAlreadyLinked,
		services.ErrOAuthLinkAlreadyExists:                  oauthLinkCodeDuplicateProvider,
		services.ErrOAuthLinkInvalidUserInfo:                oauthLinkCodeInvalidUserInfo,
		errors.New("persist user link: u-1 u@example.com"): oauthLinkCodeInternal,
	} {
		hx := newCallbackHarness(t)
		hx.opAuth.linkErr = err
		rec := httptest.NewRecorder()
		hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, mode: services.OAuthStateModeLink, linkUUID: "u-1", cookie: true}))
		_, q, _ := location(t, rec)
		if q.Get("link") != "failed" || q.Get("code") != code {
			t.Fatalf("%v: q=%v want code=%s", err, q, code)
		}
		assertNoPII(t, rec)
	}

	hx = newCallbackHarness(t)
	rec = httptest.NewRecorder()
	hx.dispatcher.HandleGoogleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, mode: services.OAuthStateModeLink, linkUUID: "u-1", cookie: true, noCode: true, query: "&error=access_denied"}))
	_, q, _ = location(t, rec)
	if q.Get("link") != "failed" || q.Get("code") != oauthLinkCodeAccessDenied {
		t.Fatalf("link-mode denial: q=%v", q)
	}
}

func TestCallback_AppleFormPost(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleAppleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, form: true, path: "/v1/auth/oauth/apple/callback", cookie: true, provider: models.OAuthProviderApple}))
	assertCallbackHeaders(t, rec, true)
	base, q, _ := location(t, rec)
	if base != "https://console.example/auth/callback" || q.Get("provider") != "apple" || q.Get("success") != "true" {
		t.Fatalf("base=%q q=%v", base, q)
	}
	assertNoPII(t, rec)

	// No dev-only fallback: a missing state is a terminal 400 everywhere.
	rec = httptest.NewRecorder()
	hx.dispatcher.HandleAppleCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, form: true, path: "/v1/auth/oauth/apple/callback", noState: true, cookie: true, provider: models.OAuthProviderApple}))
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Fatalf("status=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestCallback_GitHubSetsRefreshCookie(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	hx.dispatcher.HandleGitHubCallbackHTTP(rec, hx.request(t, callbackOpts{tier: services.AudienceOperator, path: "/v1/auth/oauth/github/callback", cookie: true, provider: models.OAuthProviderGitHub}))
	assertCallbackHeaders(t, rec, true)
	_, q, _ := location(t, rec)
	if q.Get("provider") != "github" || q.Get("success") != "true" {
		t.Fatalf("q=%v", q)
	}
	if c := refreshCookie(rec); c == nil || c.Value != "orkestra-rt" {
		t.Fatalf("GitHub must set the refresh cookie like every other provider: %+v", c)
	}
}

func TestCallback_DiscordAndLegacyTier(t *testing.T) {
	hx := newCallbackHarness(t)
	rec := httptest.NewRecorder()
	// tier "" (pre-cutover state) self-handles on the dispatcher, i.e. the operator SPA, inline.
	hx.dispatcher.HandleDiscordCallbackHTTP(rec, hx.request(t, callbackOpts{tier: "", path: "/v1/auth/oauth/discord/callback", cookie: true, provider: models.OAuthProviderDiscord}))
	assertCallbackHeaders(t, rec, true)
	base, q, _ := location(t, rec)
	if base != "https://console.example/auth/callback" || q.Get("provider") != "discord" {
		t.Fatalf("base=%q q=%v", base, q)
	}
}

func TestInitiateOAuthLogin_StoredRedirectURIComesFromSPAURLNotOrigin(t *testing.T) {
	hx := newCallbackHarness(t)
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/client/oauth/login", nil)
	r.Host = clientAPIHost
	r.Header.Set("Origin", "https://evil.example")
	ctx := context.WithValue(r.Context(), "http_request", r)
	req := &OAuthLoginRequest{}
	req.Body.Provider = models.OAuthProviderGoogle
	if _, err := hx.client.InitiateOAuthLogin(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(hx.state.stored) != 1 || hx.state.stored[0].RedirectURI != "https://app.example/auth/callback" {
		t.Fatalf("stored = %+v; RedirectURI must come from the configured tier SPA, never from Origin", hx.state.stored)
	}
}
```

Create `backend/internal/core/auth/handlers/oauth_callback_scan_test.go`:

```go
package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestCallbackURLBuilders_StructuralScan polices the contract at the
// syntax-tree level across every non-test file of this package:
//   - no string literal that names a callback path (/auth/callback,
//     /user/security) may live outside oauth_callback_redirect.go — every
//     redirect goes through the builders;
//   - inside the builder file, no url.Values key may be access_token,
//     refresh_token, email or user_id, whether set via .Set/.Add or as a
//     composite-literal key;
//   - no string literal anywhere may mention a callback path together with
//     one of those names (the legacy fmt.Sprintf shape).
func TestCallbackURLBuilders_StructuralScan(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{"access_token": true, "refresh_token": true, "email": true, "user_id": true}
	callbackPaths := []string{"/auth/callback", "/user/security"}
	const builderFile = "oauth_callback_redirect.go"

	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filename[strings.LastIndex(filename, "/")+1:]
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BasicLit:
					if node.Kind != token.STRING {
						return true
					}
					lit, _ := strconv.Unquote(node.Value)
					for _, p := range callbackPaths {
						if !strings.Contains(lit, p) {
							continue
						}
						if base != builderFile {
							t.Errorf("%s: callback path literal %q outside %s", fset.Position(node.Pos()), lit, builderFile)
						}
						for name := range forbidden {
							if strings.Contains(lit, name) {
								t.Errorf("%s: callback literal %q carries forbidden parameter %q", fset.Position(node.Pos()), lit, name)
							}
						}
					}
				case *ast.CallExpr:
					if base != builderFile {
						return true
					}
					sel, ok := node.Fun.(*ast.SelectorExpr)
					if !ok || (sel.Sel.Name != "Set" && sel.Sel.Name != "Add") || len(node.Args) == 0 {
						return true
					}
					if key, ok := node.Args[0].(*ast.BasicLit); ok && key.Kind == token.STRING {
						k, _ := strconv.Unquote(key.Value)
						if forbidden[k] {
							t.Errorf("%s: builder sets forbidden parameter %q", fset.Position(node.Pos()), k)
						}
					}
				case *ast.CompositeLit:
					if base != builderFile {
						return true
					}
					for _, elt := range node.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						if key, ok := kv.Key.(*ast.BasicLit); ok && key.Kind == token.STRING {
							k, _ := strconv.Unquote(key.Value)
							if forbidden[k] {
								t.Errorf("%s: builder literal keys forbidden parameter %q", fset.Position(node.Pos()), k)
							}
						}
					}
				}
				return true
			})
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/handlers/ -run 'TestCallback_|TestRelayComplete_|TestInitiateOAuthLogin_Stored|StructuralScan' -count=1`
Expected: FAIL to compile — `NewAuthHandler` rejects `*fakeResolver` (parameter is `*services.OAuthConfigResolver`), `HandleGitHubCallbackHTTP` / `HandleOAuthRelayCompleteHTTP` undefined; once compiling, the scan names every legacy literal in `auth_handler.go`.

- [ ] **Step 3: Configuration — `CLIENT_API_URL`**

In `backend/internal/shared/config/config.go`, right after the `config.Server = ServerConfig{…}` literal closes (before `config.Database = …`):

```go
	// CLIENT_API_URL is the client API's public origin — where the auth
	// module relays a client-tier OAuth callback so the refresh cookie is
	// set by the host that owns it (spec §4.10). Derived from
	// CLIENT_API_HOST when unset: https in production-like environments,
	// http in development. Empty when no client surface exists.
	config.Server.Client.PublicURL = getEnv("CLIENT_API_URL", derivedPublicURL(config.Server.Client.Host, config.IsProductionLike()))
```

and add the helper at file scope:

```go
// derivedPublicURL builds "scheme://host" for an audience whose public
// origin was not configured explicitly. The scheme follows the environment
// (a production-like deployment terminates TLS in front of the API).
func derivedPublicURL(host string, secure bool) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.Contains(host, "://") {
		return strings.TrimRight(host, "/")
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return scheme + "://" + host
}
```

(`config.go` already imports `strings`; if not, add it.) In `docker/.env.example` after `CLIENT_API_HOST=api.localhost` (line 87):

```bash
# Public origin (scheme + host) of the client API — the destination of the
# OAuth relay that completes a client-tier social login on the host that
# owns the client refresh cookie. Derived from CLIENT_API_HOST when empty
# (https on staging/production, http in development); set it explicitly
# when the API is reached through a proxy on a different port or scheme.
CLIENT_API_URL=http://api.localhost:3000
```

Then check the backend service reads the whole file: `grep -n 'env_file' docker/docker-compose.dev.yml docker/docker-compose.staging.yml docker/docker-compose.prod.yml` — if the backend service uses `env_file: .env` nothing else changes; if it enumerates variables under `environment:`, add `CLIENT_API_URL=${CLIENT_API_URL}` beside `CLIENT_API_HOST` in each file.

- [ ] **Step 4: Write the shared flow and the relay endpoint**

Create `backend/internal/core/auth/handlers/oauth_callback_flow.go`:

```go
package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/utils"
)

// oauthCallbackParams are the IdP-supplied parameters every provider
// callback reads: GET query for Google/Discord/GitHub, form-post for Apple.
type oauthCallbackParams struct {
	State    string
	Code     string
	IdPError string
}

func queryCallbackParams(r *http.Request) oauthCallbackParams {
	q := r.URL.Query()
	return oauthCallbackParams{State: q.Get("state"), Code: q.Get("code"), IdPError: q.Get("error")}
}

func formCallbackParams(r *http.Request) (oauthCallbackParams, error) {
	if err := r.ParseForm(); err != nil {
		return oauthCallbackParams{}, err
	}
	return oauthCallbackParams{State: r.FormValue("state"), Code: r.FormValue("code"), IdPError: r.FormValue("error")}, nil
}

// oauthExchange turns an authorization code into the provider's userinfo
// map plus the tokens to store. Each provider supplies one; everything
// else about a callback is shared by completeOAuthCallback.
type oauthExchange func(ctx context.Context, prov services.OAuthProviderInterface, cfg *services.OAuthProviderConfig, code string) (map[string]interface{}, *models.OAuthProviderTokens, error)

// exchangeWithUserInfo is the code-exchange + userinfo-endpoint path
// Google, Discord and GitHub share. The redirect URI presented at exchange
// is the provider's backend callback from the SAME resolved config the
// usability check answered with.
func exchangeWithUserInfo() oauthExchange {
	return func(ctx context.Context, prov services.OAuthProviderInterface, cfg *services.OAuthProviderConfig, code string) (map[string]interface{}, *models.OAuthProviderTokens, error) {
		tok, err := prov.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{Code: code, RedirectURI: cfg.AdditionalConfig["redirect_url"]})
		if err != nil {
			return nil, nil, err
		}
		info, err := prov.GetUserInfo(ctx, tok.AccessToken)
		if err != nil {
			return nil, nil, err
		}
		return userInfoMap(info), providerTokens(tok), nil
	}
}

// exchangeAppleIDToken is Apple's path: no userinfo endpoint, the identity
// comes from the ID token returned by the exchange.
func exchangeAppleIDToken() oauthExchange {
	return func(ctx context.Context, prov services.OAuthProviderInterface, cfg *services.OAuthProviderConfig, code string) (map[string]interface{}, *models.OAuthProviderTokens, error) {
		tok, err := prov.ExchangeCodeForToken(ctx, &services.CodeExchangeRequest{Code: code, RedirectURI: cfg.AdditionalConfig["redirect_url"]})
		if err != nil {
			return nil, nil, err
		}
		info, err := prov.ValidateIDToken(ctx, &services.IDTokenValidationRequest{IDToken: tok.IDToken, AccessToken: tok.AccessToken, Audience: prov.GetClientID()})
		if err != nil {
			return nil, nil, err
		}
		return userInfoMap(info), providerTokens(tok), nil
	}
}

// userInfoMap is the shape HandleOAuthCallbackWithLinking consumes. The
// email_verified bit is copied as a Go bool — the service refuses anything
// else for an unlinked identity — and it survives the relay's JSON round
// trip as a bool.
func userInfoMap(info *services.UserInfo) map[string]interface{} {
	return map[string]interface{}{
		"email":          info.Email,
		"name":           info.Name,
		"picture":        info.Picture,
		"provider_id":    info.ProviderID,
		"email_verified": info.EmailVerified,
	}
}

func providerTokens(tok *services.TokenResponse) *models.OAuthProviderTokens {
	return &models.OAuthProviderTokens{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenType:    tok.TokenType,
		ExpiresIn:    int(tok.ExpiresIn),
		Scopes:       tok.Scope,
		IDToken:      tok.IDToken,
	}
}

// completeOAuthCallback is the ONE implementation behind every provider's
// web callback, on the operator host. Its order is the spec's
// trust-before-destination rule (§4.10 v4.3):
//
//  1. Prove the state is ours, one-shot (atomic take), for THIS provider,
//     and bound to this browser — or, for a client-tier login, deferrable
//     to the relay on the client API host. Until then there is no trusted
//     tier, hence no SPA to send anyone to: every failure is a terminal
//     generic 400 with no redirect.
//  2. Dispatch to the tier-bound handler; evict the nonce cookie if it
//     lives on this host.
//  3. Only now interpret the IdP's `error`, require the code, resolve the
//     provider strictly from one config read, and exchange. Every failure
//     from here redirects to the tier SPA with an allowlisted coarse code;
//     raw text stays in sanitized log fields.
//  4. Link mode returns through its own builder. An operator/legacy login
//     completes inline (finishOAuthCompletion). A client-tier login is
//     handed to the client API host through a one-shot relay record — the
//     refresh cookie for api.* can only be set there, and the browser
//     binding can only be verified there.
func (h *AuthHandler) completeOAuthCallback(w http.ResponseWriter, r *http.Request, provider models.OAuthProvider, params oauthCallbackParams, exchange oauthExchange) {
	ctx := r.Context()
	logger := slog.Default()

	if params.State == "" {
		logger.Warn("oauth callback rejected", slog.String("provider", string(provider)), slog.String("outcome", "missing_state"))
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}
	res, err := h.resolveStateForCallback(ctx, params.State, provider)
	if err != nil {
		logger.Warn("oauth callback rejected", slog.String("provider", string(provider)), slog.String("outcome", "invalid_state"))
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}

	target := h.dispatchTarget(res.claims.Tier)
	if !res.bindingDeferred {
		// The nonce cookie lives on this host: evict it on every terminal
		// outcome from here on. A deferred flow's cookie lives on the
		// client API host and is cleared by the relay endpoint.
		w.Header().Add("Set-Cookie", clearOAuthStateCookie(h.config.Auth.Cookie.Secure))
	}
	linkMode := res.claims.Mode == services.OAuthStateModeLink

	fail := func(loginCode, linkCode, outcome string) {
		logger.Warn("oauth callback failed",
			slog.String("provider", string(provider)),
			slog.String("tier", res.claims.Tier),
			slog.Bool("link_mode", linkMode),
			slog.Bool("relayed", res.bindingDeferred),
			slog.String("outcome", outcome))
		if linkMode {
			target.writeOAuthLinkRedirect(w, r, provider, false, linkCode)
			return
		}
		target.writeOAuthLoginRedirect(w, r, oauthLoginFailure(provider, loginCode))
	}

	// A deferred flow needs somewhere to go. Decide that before spending
	// the IdP code on an exchange nobody can complete.
	if res.bindingDeferred {
		if _, ok := h.relayCompleteURL("probe"); !ok {
			fail(OAuthCallbackErrProviderUnavailable, oauthLinkCodeProviderUnavailable, "relay_unavailable")
			return
		}
	}

	if params.IdPError != "" {
		logger.Info("oauth callback denied by provider",
			slog.String("provider", string(provider)),
			slog.String("idp_error", sanitizeIdPError(params.IdPError)))
		fail(OAuthCallbackErrAccessDenied, oauthLinkCodeAccessDenied, "idp_denied")
		return
	}
	if params.Code == "" {
		fail(OAuthCallbackErrLoginFailed, oauthLinkCodeInternal, "missing_code")
		return
	}

	cfg, usable, err := h.oauthResolver.OAuthWebProviderUsable(ctx, target.policyAudience(), provider)
	if err != nil {
		fail(OAuthCallbackErrProviderUnavailable, oauthLinkCodeProviderUnavailable, "config_unavailable")
		return
	}
	if !usable {
		fail(OAuthCallbackErrProviderUnavailable, oauthLinkCodeProviderUnavailable, "provider_unusable")
		return
	}
	prov, err := h.oauthFactory.CreateProvider(provider, cfg)
	if err != nil {
		fail(OAuthCallbackErrProviderUnavailable, oauthLinkCodeProviderUnavailable, "provider_construct_failed")
		return
	}
	userInfo, oauthTokens, err := exchange(ctx, prov, cfg, params.Code)
	if err != nil {
		fail(OAuthCallbackErrProviderUnavailable, oauthLinkCodeProviderUnavailable, "exchange_failed")
		return
	}

	if linkMode {
		h.finishOAuthLinkRedirect(w, r, target, provider, userInfo, oauthTokens, res.claims.LinkUserUUID)
		return
	}

	if res.bindingDeferred {
		id, err := h.oauthStateService.StoreOAuthRelay(ctx, &services.OAuthRelayRecord{
			Tier:            res.claims.Tier,
			Provider:        provider,
			CSRF:            res.claims.CSRF,
			UserInfo:        userInfo,
			Tokens:          oauthTokens,
			SecurityContext: res.info.SecurityContext,
			DeviceInfo:      res.info.DeviceInfo,
		})
		if err != nil {
			fail(OAuthCallbackErrLoginFailed, oauthLinkCodeInternal, "relay_store_failed")
			return
		}
		dest, _ := h.relayCompleteURL(id)
		logger.Info("oauth callback relayed to the client api host", slog.String("provider", string(provider)))
		h.writeRelayRedirect(w, r, dest)
		return
	}

	h.finishOAuthCompletion(w, r, target, provider, userInfo, oauthTokens, res.info.SecurityContext, res.info.DeviceInfo)
}

// finishOAuthCompletion is the application half of a login: it runs on the
// host that owns the target tier's cookie — the operator host for
// operator/legacy flows, the client API host (relay endpoint) for client
// flows. Everything it writes is the target tier's: authService, cookie
// name/domain/secure, refresh TTL, SPA URL.
func (h *AuthHandler) finishOAuthCompletion(
	w http.ResponseWriter,
	r *http.Request,
	target *AuthHandler,
	provider models.OAuthProvider,
	userInfo map[string]interface{},
	oauthTokens *models.OAuthProviderTokens,
	securityCtx *models.SecurityContext,
	deviceInfo *models.DeviceInfo,
) {
	ctx := r.Context()
	tokenResponse, err := target.authService.HandleOAuthCallbackWithLinking(ctx, provider, userInfo, oauthTokens, securityCtx, deviceInfo)
	if err != nil {
		code, outcome := oauthLoginErrorCode(err)
		slog.Default().Warn("oauth callback failed",
			slog.String("provider", string(provider)),
			slog.String("tier", target.tier),
			slog.String("outcome", outcome))
		target.writeOAuthLoginRedirect(w, r, oauthLoginFailure(provider, code))
		return
	}
	if tokenResponse.RequiresMFA {
		target.writeOAuthLoginRedirect(w, r, oauthLoginMFA(provider, tokenResponse.MFAToken, tokenResponse.WebAuthnAvailable))
		return
	}
	utils.SetRefreshTokenCookie(w, target.config.Auth.Cookie.Name, tokenResponse.RefreshToken,
		refreshCookieMaxAge(target.jwtService), target.cookieDomain, target.config.Auth.Cookie.Secure)
	target.writeOAuthLoginRedirect(w, r, oauthLoginSuccess(provider))
}

// HandleOAuthRelayCompleteHTTP is the client API host's half of a
// client-tier web login (GET /v1/auth/client/oauth/complete?relay=<id>).
// It runs on the host that set the state cookie at start, so the browser
// binding the operator-host callback had to defer is verified HERE and is
// required. Every refusal is a terminal 400 with no redirect and no token:
// the record was the trust, and it has just been consumed.
func (h *AuthHandler) HandleOAuthRelayCompleteHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.Default()
	reject := func(outcome string) {
		logger.Warn("oauth relay rejected", slog.String("tier", h.tier), slog.String("outcome", outcome))
		http.Error(w, "Invalid OAuth relay", http.StatusBadRequest)
	}

	id := r.URL.Query().Get("relay")
	if id == "" {
		reject("missing_relay")
		return
	}
	rec, err := h.oauthStateService.TakeOAuthRelay(ctx, id) // atomic one-shot
	if err != nil {
		reject("relay_missing_or_replayed")
		return
	}
	if rec.Tier != h.tier || rec.Mode == services.OAuthStateModeLink {
		reject("relay_tier_or_mode_mismatch")
		return
	}
	if err := verifyRelayBinding(r, rec.CSRF); err != nil {
		reject("relay_unbound")
		return
	}
	w.Header().Add("Set-Cookie", clearOAuthStateCookie(h.config.Auth.Cookie.Secure))
	h.finishOAuthCompletion(w, r, h, rec.Provider, rec.UserInfo, rec.Tokens, rec.SecurityContext, rec.DeviceInfo)
}

// RegisterOAuthRelayRoute mounts the relay endpoint on the CLIENT host mux.
// Only the client-tier handler registers it, and only when a client
// surface exists; it is not part of the OpenAPI document — the browser is
// redirected to it, no client ever calls it.
func (h *AuthHandler) RegisterOAuthRelayRoute(router chi.Router) {
	router.Get(oauthRelayCompletePath, h.HandleOAuthRelayCompleteHTTP)
}
```

- [ ] **Step 5: Rewrite the handlers in `auth_handler.go`**

(a) Field + constructor: line 33 `oauthResolver     *services.OAuthConfigResolver` → `oauthResolver     services.OAuthResolver`; line 177 `oauthResolver *services.OAuthConfigResolver,` → `oauthResolver services.OAuthResolver,`. (`module.go` keeps passing the concrete pointer.)

(b) Delete, in this order (each a whole function/type, comments included): `oauthSignupDisabled` (242-247), `writeOAuthCallbackError` (293-297), `redirectOAuthSignupDisabled` (299-311), `finishOAuthMFAPartialRedirect` (693-733), `resolveOAuthMFAPartialRedirect` (735-765), `resolveOAuthLinkRedirect` (767-812), `OAuthCallbackRequest` + `OAuthCallbackResponse` (877-889), `HandleGoogleCallbackHTTP` (896-1018), `HandleDiscordCallbackHTTP` (1020-1121), `HandleAppleCallbackHTTP` (1123-1290), `HandleAppleCallback` (1292-1363), `HandleGitHubCallback` (1365-1439). Then delete their tests in `error_mapping_test.go`: `TestErrorMapping_WriteOAuthCallbackErrorStaysNeutralAndSanitized` (167-216), `TestOAuthSignupDisabled_MatchesSentinel` (335-345), `TestRedirectOAuthSignupDisabled_BouncesToFrontendURL` and `_NoFrontendURLFallsTo403` (347-375) — their subject (neutral outcome, no marker in response or logs, signup-disabled redirect) is now `TestCallback_ApplicationErrorsMapToAllowlist`. Reword the Huma `Description` of `RegisterOAuthLinkRoute` (2539) so it no longer spells the path — `"… The callback redirects back to the security page's OAuth tab with link=success|failed."` — the structural scan forbids a callback-path literal outside the builder file. Keep `oauthErrorResponseFor`, `mapOAuthError`, `logOAuthAuthenticationFailure`, `invalidOAuthAuthenticationDetail` (the mobile path uses them; Task 8 extends them). The compiler will flag imports the deletions orphan (`fmt`, `url`, `time` …) — remove exactly those; `iface`, `errors`, `chi` stay.

(c) Add the thin wrappers:

```go
// HandleGoogleCallbackHTTP handles the Google web callback (GET, query).
func (h *AuthHandler) HandleGoogleCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	h.completeOAuthCallback(w, r, models.OAuthProviderGoogle, queryCallbackParams(r), exchangeWithUserInfo())
}

// HandleDiscordCallbackHTTP handles the Discord web callback (GET, query).
func (h *AuthHandler) HandleDiscordCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	h.completeOAuthCallback(w, r, models.OAuthProviderDiscord, queryCallbackParams(r), exchangeWithUserInfo())
}

// HandleGitHubCallbackHTTP handles the GitHub web callback (GET, query).
// Raw chi handler like the others so it can set the refresh cookie — the
// previous Huma operation never did, which left GitHub logins without a
// session.
func (h *AuthHandler) HandleGitHubCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	h.completeOAuthCallback(w, r, models.OAuthProviderGitHub, queryCallbackParams(r), exchangeWithUserInfo())
}

// HandleAppleCallbackHTTP handles Apple's form-post callback. A missing
// state is a terminal 400 in EVERY environment — the former dev-only
// fallback that fabricated state is gone (trust before destination).
func (h *AuthHandler) HandleAppleCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	params, err := formCallbackParams(r)
	if err != nil {
		http.Error(w, "Invalid OAuth callback", http.StatusBadRequest)
		return
	}
	h.completeOAuthCallback(w, r, models.OAuthProviderApple, params, exchangeAppleIDToken())
}
```

(d) Rewrite `finishOAuthLinkRedirect` (635-691) onto the builder; same signature:

```go
// finishOAuthLinkRedirect drives the link-mode branch of the shared
// callback: bind the returning identity to the authenticated user named by
// the signed state, then render the link-mode return contract through the
// target tier's builder. Never mints tokens. Link mode is operator-only
// (the route is mounted on the operator side), so it never relays.
func (h *AuthHandler) finishOAuthLinkRedirect(
	w http.ResponseWriter,
	r *http.Request,
	target *AuthHandler,
	provider models.OAuthProvider,
	userInfo map[string]interface{},
	oauthTokens *models.OAuthProviderTokens,
	linkUserUUID string,
) {
	logger := slog.Default()
	if err := target.authService.SelfLinkOAuthFromCallback(r.Context(), linkUserUUID, iface.OAuthProvider(provider), userInfo, oauthTokens); err != nil {
		code := oauthLinkCodeInternal
		switch {
		case errors.Is(err, services.ErrOAuthLinkClaimedByOther):
			code = oauthLinkCodeAlreadyLinked
		case errors.Is(err, services.ErrOAuthLinkAlreadyExists):
			code = oauthLinkCodeDuplicateProvider
		case errors.Is(err, services.ErrOAuthLinkInvalidUserInfo):
			code = oauthLinkCodeInvalidUserInfo
		}
		logger.Warn("oauth link refused", slog.String("provider", string(provider)), slog.String("outcome", code))
		target.writeOAuthLinkRedirect(w, r, provider, false, code)
		return
	}
	target.writeOAuthLinkRedirect(w, r, provider, true, "")
}
```

(e) `InitiateOAuthLogin` (449-462): replace the whole "Backend always determines frontend redirect URL automatically" block with

```go
	// Stored for state compatibility only; the callback never reads it
	// and it is NEVER derived from the Origin header (spec §4.10).
	frontendRedirectURL := h.spaURL() + oauthCallbackPath
```

and `InitiateOAuthLink` (575-585) likewise with `frontendRedirectURL := h.spaURL() + oauthLinkReturnPath`. (Task 8 rewrites the provider resolution in both; this step only removes the Origin read.)

(f) `RegisterOAuthRoutes` (2448-2470): add `router.Get("/v1/auth/oauth/github/callback", h.HandleGitHubCallbackHTTP)` after the Discord line; delete the `huma.Register(publicAPI, huma.Operation{OperationID: "github-oauth-callback", …}, h.HandleGitHubCallback)` block; rename the now-unused `publicAPI` parameter to `_` if the compiler/vet complains. Update the doc comment: "All four callbacks are raw chi handlers so every one of them can set the refresh cookie and the redirect headers; client-tier logins are relayed to the client host mux (RegisterOAuthRelayRoute)."

(g) `structured_logging_safety_test.go`: parse both files and add the new targets. Replace lines 16-25 with:

```go
	fset := token.NewFileSet()
	targets := map[string]bool{
		"InitiateOAuthLogin": true, "InitiateOAuthLink": true,
		"finishOAuthLinkRedirect": true, "completeOAuthCallback": true, "finishOAuthCompletion": true,
		"HandleOAuthRelayCompleteHTTP": true,
		"HandleGoogleCallbackHTTP": true, "HandleDiscordCallbackHTTP": true,
		"HandleAppleCallbackHTTP": true, "HandleGitHubCallbackHTTP": true,
		"HandleMobileGoogleAuth": true, "HandleMobileAppleAuth": true,
		"RefreshTokens": true, "RefreshTokensWithHeaderHTTP": true, "RefreshTokensHTTP": true,
		"GetSessionHTTP": true, "LogoutHTTP": true,
	}
	var decls []ast.Decl
	for _, name := range []string{"auth_handler.go", "oauth_callback_flow.go"} {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		decls = append(decls, file.Decls...)
	}
```

and change the loop header `for _, decl := range file.Decls {` to `for _, decl := range decls {`.

(h) `module.go`: after `m.operatorAuthHandler.SetPolicy(authPolicy)` (1079) add `m.operatorAuthHandler.SetSPAURL(opDeps.frontendURL)`; after `m.clientAuthHandler.SetPolicy(authPolicy)` (1245) add `m.clientAuthHandler.SetSPAURL(clDeps.frontendURL)`; inside `if ri.ClientRouter != nil { … }` (1683-1686) add `m.clientAuthHandler.RegisterOAuthRelayRoute(ri.ClientRouter)` with the comment `// Client-tier web logins complete HERE (spec §4.10): the operator-host callback relays them because only this host can set the client refresh cookie and verify the state cookie it set at start.`

- [ ] **Step 6: Run the tests and the structural scan**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/... ./internal/shared/config/... -count=1 && go vet ./... && go test ./internal/core/auth/handlers/ -run 'StructuralScan|StructuredFields|RouteMounts' -count=1 -v 2>&1 | tail -20`
Expected: every package `ok`; `TestCallbackURLBuilders_StructuralScan`, `TestAuthFlowLogsUseOnlyAllowlistedStructuredFields`, `TestRouteMountsRegisterDistinctPaths` PASS.

- [ ] **Step 7: Regenerate OpenAPI and run the analyzers**

Run: `grep "^ENV=" /home/tore/orkestra/docker/.env && make -C /home/tore/orkestra/backend openapi-dump && git diff --stat backend/openapi/enterprise.json`
Expected: the diff removes the `github-oauth-callback` operation and nothing else. Then run the analyzers (`make ci-help` lists the target; `backend-policycoverage` or the full `ci-backend`): if the baseline names the removed operation, delete that baseline line.

- [ ] **Step 8: Document the contract and the relay**

In `backend/internal/core/auth/CLAUDE.md`:

(a) "What it owns" table — add after the `handlers/auth_handler.go` row:

```markdown
| `handlers/oauth_callback_flow.go` | The ONE web-callback implementation (`completeOAuthCallback`): trust before destination, strict provider resolution from one config read, per-provider `oauthExchange` closures, inline completion for operator/legacy flows, one-shot relay for client-tier flows; `HandleOAuthRelayCompleteHTTP` (client host); the four `Handle*CallbackHTTP` wrappers are thin |
| `handlers/oauth_callback_redirect.go` | The closed SPA callback contract — the ONLY file that may build `/auth/callback` or `/user/security` URLs; per-tier `spaURL()`, allowlisted codes, MFA in the fragment, relay destination, `Referrer-Policy: no-referrer`; policed by `TestCallbackURLBuilders_StructuralScan` |
| `services/oauth_provider_usability.go` | `ProviderStructurallyConfigured` (pure), `OAuthWebProviderUsable` / `UsableWebProviders` (strict, one read), `OAuthResolver` interface |
```

(b) Endpoint rows 448-452 — replace with:

```markdown
| GET | `/v1/auth/oauth/google/callback` | Web OAuth callback (raw HTTP). Single shared callback per provider — dispatches to operator or client via `state.tier`; operator/legacy flows complete here, client-tier flows are **relayed** to the client API host under the closed contract below |
| GET | `/v1/auth/oauth/discord/callback` | Web OAuth callback (raw HTTP) |
| POST | `/v1/auth/oauth/apple/callback` | Apple returns form-post, not a redirect (raw HTTP). No dev-only "missing state" fallback: a missing state is a terminal 400 everywhere |
| GET | `/v1/auth/oauth/github/callback` | GitHub web OAuth callback (raw HTTP since PR 2 — the former Huma operation never set the refresh cookie) |
| GET | `/v1/auth/client/oauth/complete` | **Client host mux only.** The relay endpoint that completes a client-tier web login: takes the one-shot relay record, requires the state cookie this host set at start, sets the client refresh cookie, redirects to the client SPA. Not in OpenAPI |
| GET | `/v1/auth/session` | Poll for session after OAuth redirect finishes |
```

(c) After the "OAuth state-encoded tier dispatch" section (before `## HTTP endpoints`) add:

```markdown
### OAuth callback contract (spec §4.10)

Every provider callback runs `completeOAuthCallback` (`handlers/oauth_callback_flow.go`), whose order is **trust before destination**: `resolveStateForCallback(ctx, raw, provider)` checks signature/expiry → browser binding → atomic one-shot take → `tier` → `provider` → link-mode pair, and any failure is a terminal generic **400 with no redirect** — no trusted tier exists yet, so there is no SPA to send anyone to. Only then does the handler dispatch to the tier-bound instance, clear the `orkestra_oauth_state` cookie (if it lives on this host), interpret the IdP's `error`, require the code, resolve the provider **strictly from one config read** (`OAuthWebProviderUsable`, so a provider disabled mid-flow is refused and the provider is built from the value that answered the check) and exchange. Every failure from that point redirects to the **configured tier SPA** (`spaURL()` — `OPERATOR_FRONTEND_URL` / `CLIENT_FRONTEND_URL` → `FRONTEND_URL`, handed to the handler by `module.go` via `SetSPAURL`; the `Origin` header is never read) with a coarse allowlisted code; raw IdP/error text stays in sanitized log fields.

**Where a flow completes depends on who owns the cookie.** Every provider callback is mounted on the operator host, and a response from `console.*` cannot set a cookie for `api.*` (RFC 6265 §5.3; the cross-tier isolation model has no shared parent domain). So an operator/legacy login completes inline (`finishOAuthCompletion`: application half, refresh cookie with the target tier's domain and TTL, redirect), while a **client-tier login is relayed**: the callback stores an encrypted one-shot `OAuthRelayRecord` (tier, provider, the state's CSRF nonce, user-info map, provider tokens, security context, device info; `OAuthRelayTTL` 60 s) and redirects to `{CLIENT_API_URL}/v1/auth/client/oauth/complete?relay=<id>`. `HandleOAuthRelayCompleteHTTP`, on the client host mux, takes the record atomically, **requires** the state cookie its own host set at start to equal the nonce (`verifyRelayBinding`) — the browser binding the operator host had to defer — refuses a missing/foreign cookie, a replay, a link-mode or wrong-tier record with 400 and no redirect, clears the cookie, then runs the same `finishOAuthCompletion` and sets the client refresh cookie on its own host. A login-CSRF attempt reaches the relay without the attacker's nonce and is refused before any token exists. The relay id is a single-use, browser-bound handle like the IdP code, never a credential.

The wire shape is **closed** and lives in `handlers/oauth_callback_redirect.go`, the only file allowed to build these URLs:

- success: `{spa}/auth/callback?success=true&provider=<google|apple|github|discord>` — the refresh cookie is the only credential; the SPA bootstraps through `GET /v1/auth/session`;
- failure: `{spa}/auth/callback?success=false&error=<oauth_access_denied | oauth_signup_disabled | oauth_link_disabled | auth.oauth_email_unverified | oauth_provider_unavailable | oauth_login_failed>` — anything else collapses to `oauth_login_failed`; account status and lookup results are never encoded; config uncertainty on this surface is `oauth_provider_unavailable`;
- MFA continuation: `{spa}/auth/callback#requiresMfa=true&mfaToken=<one-shot id>&webauthnAvailable=<bool>` — in the **fragment**, so the five-minute challenge id never reaches a server log, a proxy or a Referer; **no cookie is written** on a partial;
- link mode: `{spa}/user/security?tab=oauth&link=success|failed&provider=<p>[&code=already_linked|duplicate_provider|invalid_userinfo|access_denied|provider_unavailable|internal]` — its own builder, never the login state machine, operator-only.

Every redirect sets `Referrer-Policy: no-referrer` and `Cache-Control: no-store`. **No callback URL may carry `access_token`, `refresh_token`, `email` or `user_id`** — `TestCallbackURLBuilders_StructuralScan` fails the build on a literal outside the builder file or a forbidden `url.Values` key inside it, and `oauth_callback_flow_test.go` checks every `Location`'s exact parameter names and values. The legacy `success=true&user_id=…&email=…` and the unregistered Huma Apple callback's `access_token=…` are gone.
```

(d) Rules: add `- **Never build a `/auth/callback` or `/user/security` URL outside `handlers/oauth_callback_redirect.go`**, never put a token, an email or a user id in one, and never set a client-tier cookie from the operator host — relay instead. The structural scan and `oauth_callback_flow_test.go` are the guards.`

In `docs/site/operating/cookie-hardening-cross-tier.mdx`, before `## Verifying` (line 89), add:

```markdown
## Why a client-tier social login relays to the client API host

Every OAuth provider is registered with **one** redirect URI per provider, and that URI lives on the operator host. A client-tier flow therefore returns from the identity provider to `console.example.com` — a host that, by the model above, **cannot** set a cookie for `api.example.com` (the browser rejects a `Domain` that does not match the response host, and a shared parent domain is exactly what we refuse to configure). The callback does not try: for a client-tier flow it completes the identity-provider half, stores a 60-second single-use record, and redirects the browser to `CLIENT_API_URL/v1/auth/client/oauth/complete?relay=…`. That endpoint, on the client API host, takes the record once, requires the `orkestra_oauth_state` cookie the same host set when the flow started (so a login-CSRF attempt that finished someone else's flow is refused before any token exists), sets the client refresh cookie on its own host and sends the user to the client SPA. Set `CLIENT_API_URL` to the client API's public origin; it is derived from `CLIENT_API_HOST` when empty.
```

- [ ] **Step 9: Commit**

```bash
cd /home/tore/orkestra && git add backend/internal/core/auth/handlers/oauth_callback_flow.go backend/internal/core/auth/handlers/oauth_callback_flow_test.go backend/internal/core/auth/handlers/oauth_callback_scan_test.go backend/internal/core/auth/handlers/auth_handler.go backend/internal/core/auth/handlers/structured_logging_safety_test.go backend/internal/core/auth/handlers/error_mapping_test.go backend/internal/core/auth/module.go backend/internal/shared/config/config.go docker/.env.example backend/openapi/enterprise.json backend/internal/core/auth/CLAUDE.md docs/site/operating/cookie-hardening-cross-tier.mdx
# plus any compose file / policycoverage baseline Steps 3 and 7 changed
git commit -m "fix(auth): one trust-before-destination OAuth callback flow — client-tier logins complete through a one-shot relay on the client API host; GitHub sets the refresh cookie; dead Huma Apple callback removed"
```

---

### Task 8: Provider list and OAuth start on the strict resolver; mobile mapping of the new sentinels

**Files:**
- Modify: `backend/internal/core/auth/handlers/auth_handler.go` (`oauthErrorResponseFor` / `mapOAuthError` 251-284; `ListOAuthProviders` 396-407; `InitiateOAuthLogin` 429-533; `InitiateOAuthLink` 548-633)
- Create: `backend/internal/core/auth/handlers/oauth_providers_handler_test.go`
- Modify: `backend/internal/core/auth/handlers/error_mapping_test.go` (add rows)
- Modify: `backend/internal/core/auth/CLAUDE.md` (`GET /v1/auth/{tier}/providers` row line 443, `POST /v1/auth/{tier}/oauth/login` row 445, `POST /me/oauth/link/{provider}` row 488; "OAuth Providers" policy row 184)

**Interfaces:**
- Consumes: `services.OAuthResolver.UsableWebProviders` / `OAuthWebProviderUsable` (Task 3), `errcode.AuthPolicyUnavailable`, `errcode.AuthOAuthEmailUnverified` (Task 2), the Task 7 harness fakes.
- Produces: `oauthErrorResponse.code string`; `mapOAuthError` returns `*errcode.Error` for the two new sentinels.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/handlers/oauth_providers_handler_test.go`:

```go
package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

func codeOf(t *testing.T, err error) (int, string) {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *errcode.Error, got %T (%v)", err, err)
	}
	return e.Status, e.Code
}

func TestListOAuthProviders_UsableOnly(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.resolver.list = []models.OAuthProvider{models.OAuthProviderGoogle, models.OAuthProviderGitHub}
	resp, err := hx.operator.ListOAuthProviders(context.Background(), &ListOAuthProvidersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(resp.Body.Providers, ",") != "google,github" {
		t.Fatalf("providers = %v", resp.Body.Providers)
	}
}

func TestListOAuthProviders_DocumentLevelFailureIs503(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.resolver.err = errors.New("mongo down")
	_, err := hx.operator.ListOAuthProviders(context.Background(), &ListOAuthProvidersRequest{})
	if status, code := codeOf(t, err); status != http.StatusServiceUnavailable || code != errcode.AuthPolicyUnavailable {
		t.Fatalf("got %d %s", status, code)
	}
}

func TestListOAuthProviders_EmptyIsNotAnError(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.resolver.list = nil
	resp, err := hx.operator.ListOAuthProviders(context.Background(), &ListOAuthProvidersRequest{})
	if err != nil || resp.Body.Providers == nil || len(resp.Body.Providers) != 0 {
		t.Fatalf("resp=%+v err=%v; an empty list is a 200 with [], never null", resp, err)
	}
}

func startCtx(host string) context.Context {
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/operator/oauth/login", nil)
	r.Host = host
	return context.WithValue(r.Context(), "http_request", r)
}

func TestInitiateOAuthLogin_UsableProviderStartsFlowFromResolvedConfig(t *testing.T) {
	hx := newCallbackHarness(t)
	req := &OAuthLoginRequest{}
	req.Body.Provider = models.OAuthProviderGoogle
	resp, err := hx.operator.InitiateOAuthLogin(startCtx("console.example"), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Body.AuthURL, "redirect_uri=https%3A%2F%2Fconsole.example%2Fv1%2Fauth%2Foauth%2Fgoogle%2Fcallback") {
		t.Fatalf("authUrl must use the backend callback from the resolved config: %q", resp.Body.AuthURL)
	}
	if hx.resolver.calls != 1 {
		t.Fatalf("resolver consulted %d times, want exactly 1 (no check-then-reread)", hx.resolver.calls)
	}
	if !strings.Contains(resp.SetCookie, OAuthStateCookieName+"=") {
		t.Fatalf("state cookie missing: %q", resp.SetCookie)
	}
	if len(hx.state.stored) != 1 || hx.state.stored[0].RedirectURI != "https://console.example/auth/callback" || hx.state.stored[0].Tier != services.AudienceOperator {
		t.Fatalf("stored state = %+v", hx.state.stored)
	}
}

func TestInitiateOAuthLogin_PerProviderDefectIs403(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.resolver.usable = false
	req := &OAuthLoginRequest{}
	req.Body.Provider = models.OAuthProviderApple
	_, err := hx.operator.InitiateOAuthLogin(startCtx("console.example"), req)
	if status, code := codeOf(t, err); status != http.StatusForbidden || code != errcode.AuthOAuthProviderDisabled {
		t.Fatalf("got %d %s", status, code)
	}
	if len(hx.state.stored) != 0 {
		t.Fatal("no state may be stored for a refused start")
	}
}

func TestInitiateOAuthLogin_DocumentLevelFailureIs503(t *testing.T) {
	hx := newCallbackHarness(t)
	hx.resolver.err = errors.New("mongo down")
	req := &OAuthLoginRequest{}
	req.Body.Provider = models.OAuthProviderGoogle
	_, err := hx.operator.InitiateOAuthLogin(startCtx("console.example"), req)
	if status, code := codeOf(t, err); status != http.StatusServiceUnavailable || code != errcode.AuthPolicyUnavailable {
		t.Fatalf("got %d %s", status, code)
	}
}

func TestInitiateOAuthLink_UsesStrictResolverAndSPAURL(t *testing.T) {
	hx := newCallbackHarness(t)
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/operator/me/oauth/link/github", nil)
	r.Host = "console.example"
	r.Header.Set("Origin", "https://evil.example")
	ctx := context.WithValue(r.Context(), "http_request", r)
	ctx = context.WithValue(ctx, ctxauth.KeyUserUUID, "u-1") // what AuthMiddleware stamps; InitiateOAuthLink reads it via ctxauth.GetUserUUID
	_, err := hx.operator.InitiateOAuthLink(ctx, &OAuthLinkRequest{Provider: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hx.state.stored) != 1 || hx.state.stored[0].RedirectURI != "https://console.example/user/security" || hx.state.stored[0].Mode != services.OAuthStateModeLink {
		t.Fatalf("stored = %+v", hx.state.stored)
	}
	hx.resolver.usable = false
	_, err = hx.operator.InitiateOAuthLink(ctx, &OAuthLinkRequest{Provider: "github"})
	if status, code := codeOf(t, err); status != http.StatusForbidden || code != errcode.AuthOAuthProviderDisabled {
		t.Fatalf("got %d %s", status, code)
	}
}
```

Add to `error_mapping_test.go` a new test (the existing `TestMapPasswordError_KnownCodes` stays untouched):

```go
func TestMapOAuthError_NewSentinels(t *testing.T) {
	cases := []struct {
		in       error
		wantCode int
		wantSlug string
	}{
		{services.ErrOAuthEmailUnverified, http.StatusForbidden, errcode.AuthOAuthEmailUnverified},
		{services.ErrAuthPolicyUnavailable, http.StatusServiceUnavailable, errcode.AuthPolicyUnavailable},
		{services.ErrInvalidCredentials, http.StatusUnauthorized, ""},
		{errors.New("anything else"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		err := mapOAuthError(tc.in)
		if got := statusOf(t, err); got != tc.wantCode {
			t.Errorf("%v → %d, want %d", tc.in, got, tc.wantCode)
		}
		if tc.wantSlug != "" {
			var e *errcode.Error
			if !errors.As(err, &e) || e.Code != tc.wantSlug {
				t.Errorf("%v → %v, want code %s", tc.in, err, tc.wantSlug)
			}
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/handlers/ -run 'ListOAuthProviders|InitiateOAuth|MapOAuthError_NewSentinels' -count=1`
Expected: `TestListOAuthProviders_UsableOnly` FAIL (list empty — `ConfiguredProviders` is consulted), `_DocumentLevelFailureIs503` FAIL (200), `TestInitiateOAuthLogin_PerProviderDefectIs403` FAIL (200), `_DocumentLevelFailureIs503` FAIL, `TestMapOAuthError_NewSentinels` FAIL (500 for both sentinels).

- [ ] **Step 3: Rewrite the three handlers and the mobile mapper**

`ListOAuthProviders` (396-407):

```go
// ListOAuthProviders returns the providers that are USABLE on this
// handler's surface: toggle on, structurally complete (spec §4.4) — from one
// config read. Public endpoint — no auth required — because it's used by
// the unauthenticated login screen. A document-level failure (missing auth
// document, repository error, undecryptable stored secret) is 503 rather
// than an empty list, because "no provider" is a legitimate steady state
// the SPA renders differently from "we could not ask"; a per-provider
// defect only omits that provider (WARN names the key).
func (h *AuthHandler) ListOAuthProviders(ctx context.Context, _ *ListOAuthProvidersRequest) (*ListOAuthProvidersResponse, error) {
	usable, err := h.oauthResolver.UsableWebProviders(ctx, h.policyAudience())
	if err != nil {
		slog.Default().Warn("oauth providers unavailable", slog.String("tier", h.tier), slog.String("outcome", "config_unavailable"))
		return nil, errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable, "Sign-in policy is temporarily unavailable; try again shortly")
	}
	resp := &ListOAuthProvidersResponse{}
	resp.Body.Providers = make([]string, 0, len(usable))
	for _, p := range usable {
		resp.Body.Providers = append(resp.Body.Providers, string(p))
	}
	return resp, nil
}
```

`InitiateOAuthLogin`: replace the opening `if err := h.oauthProviderAllowed(ctx, string(req.Body.Provider)); err != nil { return nil, err }` (437-439) with

```go
	if !h.loginAllowed(ctx) {
		return nil, errcode.Forbidden(errcode.AuthLoginDisabled,
			"Login is temporarily disabled for this surface. Contact an administrator.")
	}
	// One strict read decides toggle + structure and yields the config the
	// provider is built from below — no check-then-reread (spec §4.4).
	cfg, usable, err := h.oauthResolver.OAuthWebProviderUsable(ctx, h.policyAudience(), req.Body.Provider)
	if err != nil {
		logger.Warn("oauth initiation failed", slog.String("provider", string(req.Body.Provider)), slog.String("outcome", "config_unavailable"))
		return nil, errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable, "Sign-in policy is temporarily unavailable; try again shortly")
	}
	if !usable {
		return nil, errcode.Forbidden(errcode.AuthOAuthProviderDisabled,
			"This OAuth provider is not enabled for this surface. Contact an administrator.")
	}
```

and replace the provider-construction block (509-521, from `// Create OAuth provider from live admin-panel config.` to `authURL := …`) with

```go
	provider, err := h.oauthFactory.CreateProvider(req.Body.Provider, cfg)
	if err != nil {
		logger.Error("oauth initiation failed", slog.String("outcome", "provider_construct_failed"))
		return nil, huma.Error500InternalServerError("OAuth not available", err)
	}
	// Non-empty by the structural predicate that just passed.
	backendCallbackURL := cfg.AdditionalConfig["redirect_url"]
	authURL := provider.GetAuthURL(signedState, "", backendCallbackURL)
```

`InitiateOAuthLink`: the same two substitutions (the `oauthProviderAllowed` call at 562-564 and the `resolveProvider` + `RedirectURL` block at 612-621), using `provider` (the lower-cased path value) as the provider argument. `oauthProviderAllowed` stays for `HandleMobileGoogleAuth` / `HandleMobileAppleAuth`; `resolveProvider` stays for the same two.

`oauthErrorResponseFor` / `mapOAuthError` (251-284): add a `code string` field to `oauthErrorResponse` and two cases before the default:

```go
	if errors.Is(err, services.ErrOAuthEmailUnverified) {
		return oauthErrorResponse{
			status:     http.StatusForbidden,
			code:       errcode.AuthOAuthEmailUnverified,
			humaDetail: "The identity provider has not verified this email address",
			rawDetail:  "The identity provider has not verified this email address",
			outcome:    "email_unverified",
		}
	}
	if errors.Is(err, services.ErrAuthPolicyUnavailable) {
		return oauthErrorResponse{
			status:     http.StatusServiceUnavailable,
			code:       errcode.AuthPolicyUnavailable,
			humaDetail: "Sign-in policy is temporarily unavailable; try again shortly",
			rawDetail:  "Sign-in policy is temporarily unavailable; try again shortly",
			outcome:    "policy_unavailable",
		}
	}
```

and in `mapOAuthError`, before the 401 branch: `if response.code != "" { return errcode.New(response.status, response.code, response.humaDetail) }`.

- [ ] **Step 4: Run the tests**

Run: `cd /home/tore/orkestra/backend && go test ./internal/core/auth/... -count=1 && go vet ./...`
Expected: `ok`. Then `make -C /home/tore/orkestra/backend openapi-dump && git -C /home/tore/orkestra status --short backend/openapi/` — if the providers/start operations changed their documented responses the file changes; stage it with this commit.

- [ ] **Step 5: Document**

In `backend/internal/core/auth/CLAUDE.md`:

- `GET /v1/auth/{tier}/providers` row (443): replace with `| GET | `/v1/auth/{tier}/providers` | List OAuth providers **usable** on this audience — from one strict config read (`OAuthConfigResolver.UsableWebProviders`): the per-surface toggle parsed strictly (absent → `false`, malformed → omitted + WARN naming the key) AND `ProviderStructurallyConfigured` (client ID, redirect URL, secret present; Apple team/key + inline key or readable key file). A document-level failure (missing auth document, repository error, undecryptable stored secret) is **503 `auth.policy_unavailable`**, never an empty list. The unauthenticated login pages drive their social-login buttons off this endpoint; edits resolve on the next request. |`
- `POST /v1/auth/{tier}/oauth/login` row (445): append ` Refuses with 403 `auth.login_disabled` (kill switch), 403 `auth.oauth_provider_disabled` (per-provider defect) or 503 `auth.policy_unavailable` (document-level), and builds the provider from the same resolved config the check answered with. The stored `RedirectURI` is the configured tier SPA + `/auth/callback`, never the `Origin` header.`
- `POST /v1/auth/{tier}/me/oauth/link/{provider}` row (488): replace `/user/security?tab=oauth&link=success\|failed&provider=<x>&code=<reason>` with `/user/security?tab=oauth&link=success\|failed&provider=<x>[&code=already_linked\|duplicate_provider\|invalid_userinfo\|access_denied\|provider_unavailable\|internal]` and append ` Provider usability is checked the same strict way as the login start. Operator-only: link mode never relays.`
- "OAuth Providers" policy row (184): replace "`ListOAuthProviders` filters its return per audience; `InitiateOAuthLogin` + mobile handlers return 403 `oauth_provider_disabled` for a disabled surface." with "The **web** path (`/providers`, OAuth start, callback) reads the toggles strictly through `OAuthConfigResolver.OAuthWebProviderUsable` — absent → `false` (the schema default), malformed → that provider alone is unusable with a WARN naming the key — while the **mobile** ID-token endpoints keep the permissive `OAuthProviderEnabled` (absent → `true`) until the native flow gets its own decision. A disabled or incomplete provider answers 403 `auth.oauth_provider_disabled` on start; an unreadable auth document answers 503 `auth.policy_unavailable`."

- [ ] **Step 6: Commit**

```bash
git add backend/internal/core/auth/handlers/auth_handler.go backend/internal/core/auth/handlers/oauth_providers_handler_test.go backend/internal/core/auth/handlers/error_mapping_test.go backend/internal/core/auth/CLAUDE.md backend/openapi/enterprise.json
git commit -m "feat(auth): provider list and OAuth start resolve usability strictly from one config read — 503 document-level, 403 per-provider"
```

---

### Task 9: frontend-admin — scrub-first callback, awaited session, local MFA panel, closed parser, 10-minute return target taken in an effect

**Pre-flight (orkestra-frontend-admin skill — every file below was opened with Read while writing this plan; re-open them before editing):**
- Production precedent: `src/components/authentication/SocialAuthCallback.tsx`, `LoginMfaVerify.tsx`, `SocialLoginForm.tsx` (Spinner / Alert states), `EmailPasswordForm.tsx` (MFA navigate shape), `Login.tsx` (`AuthCardLayout` + `Card` shell), `LoginMfaVerify.test.tsx` (router-state test pattern)
- Reference read: `src/reference/components/ui/Alerts.tsx`, `src/reference/components/ui/Spinners.tsx`
- Primitives: React Bootstrap `Alert`, `Spinner`, `Button`, `Card`, `Form`; `layouts/AuthCardLayout`; `react-i18next` `t()`; RTK Query `authApi.endpoints.getSession.initiate`; `utils/returnTo` `sanitizeReturnTo` / `DEFAULT_POST_LOGIN`

**Files:**
- Modify: `frontend-admin/src/utils/socialAuthUtils.ts` (`OAUTH_RETURN_TO_KEY` + `initiateSocialLogin` lines 21-72)
- Create: `frontend-admin/src/utils/socialAuthUtils.test.ts`
- Create: `frontend-admin/src/utils/oauthCallbackParams.ts`, `frontend-admin/src/utils/oauthCallbackParams.test.ts`
- Create: `frontend-admin/src/components/authentication/MfaVerifyPanel.tsx`
- Modify: `frontend-admin/src/components/authentication/LoginMfaVerify.tsx` (becomes a wrapper)
- Modify: `frontend-admin/src/components/authentication/SocialAuthCallback.tsx` (rewrite)
- Create: `frontend-admin/src/components/authentication/SocialAuthCallback.test.tsx`
- Modify: `frontend-admin/src/locales/en.json`, `it.json` (`auth.social.callback`)
- Modify: `frontend-admin/CLAUDE.md` (line 66 paragraph)

**Interfaces:**
- Consumes: backend contract from Task 5 (`?success=true&provider=`, `?success=false&error=`, `#requiresMfa=true&mfaToken=&webauthnAvailable=`), `authApi.endpoints.getSession` (`SessionResponse | null`; `authApi` is the named export at `authApi.ts:212`), `setUserFromApiResponse` / `setAccessToken` (`store/slices/authSlice`), `useLoginVerifyMfaMutation` etc. (`store/api/mfaApi`).
- Produces: `stashOAuthReturnTo(target, now?)`, `takeOAuthReturnTo(now?)`, `OAUTH_RETURN_TO_TTL_MS`; `OAUTH_PROVIDERS`, `OAuthProviderName`, `parseOAuthCallback(search, hash): OAuthCallbackOutcome`, `OAUTH_CALLBACK_ERROR_KEYS`; `<MfaVerifyPanel challengeId email? webauthnAvailable returnTo />`.

- [ ] **Step 1: Write the failing unit tests for the two pure modules**

Create `frontend-admin/src/utils/oauthCallbackParams.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import { parseOAuthCallback } from './oauthCallbackParams';

const GENERIC = { kind: 'error', errorKey: 'loginFailed' };

describe('parseOAuthCallback (closed contract)', () => {
  it('reads a success outcome with an allowlisted provider from the query', () => {
    for (const p of ['google', 'apple', 'github', 'discord']) {
      expect(parseOAuthCallback(`?provider=${p}&success=true`, '')).toEqual({ kind: 'success', provider: p });
    }
  });

  it('refuses a success without an allowlisted provider or with an error', () => {
    expect(parseOAuthCallback('?success=true', '')).toEqual(GENERIC);
    expect(parseOAuthCallback('?success=true&provider=facebook', '')).toEqual(GENERIC);
    expect(parseOAuthCallback('?success=true&provider=Google', '')).toEqual(GENERIC);
    expect(parseOAuthCallback('?success=true&provider=google&error=oauth_access_denied', '')).toEqual(GENERIC);
  });

  it('reads the MFA continuation from the fragment only, with every field explicit', () => {
    expect(parseOAuthCallback('', '#mfaToken=ch-1&requiresMfa=true&webauthnAvailable=true')).toEqual({
      kind: 'mfa',
      challengeId: 'ch-1',
      webauthnAvailable: true
    });
    expect(parseOAuthCallback('', '#mfaToken=ch-1&requiresMfa=true&webauthnAvailable=false')).toEqual({
      kind: 'mfa',
      challengeId: 'ch-1',
      webauthnAvailable: false
    });
    // A query cannot smuggle an MFA continuation.
    expect(parseOAuthCallback('?requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false', '')).toEqual(GENERIC);
  });

  it('refuses an incomplete or malformed MFA fragment', () => {
    expect(parseOAuthCallback('', '#requiresMfa=true&mfaToken=ch-1')).toEqual(GENERIC); // webauthnAvailable missing
    expect(parseOAuthCallback('', '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=yes')).toEqual(GENERIC);
    expect(parseOAuthCallback('', '#requiresMfa=true&mfaToken=&webauthnAvailable=false')).toEqual(GENERIC);
    expect(parseOAuthCallback('', '#requiresMfa=false&mfaToken=ch-1&webauthnAvailable=false')).toEqual(GENERIC);
    expect(parseOAuthCallback('', '#mfaToken=ch-1&webauthnAvailable=false')).toEqual(GENERIC);
  });

  it('refuses an ambiguous payload that mixes an MFA fragment with a query outcome', () => {
    expect(parseOAuthCallback('?success=true&provider=google', '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false')).toEqual(GENERIC);
    expect(parseOAuthCallback('?success=false&error=oauth_access_denied', '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false')).toEqual(GENERIC);
    expect(parseOAuthCallback('?provider=google', '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false')).toEqual(GENERIC);
  });

  it('maps every allowlisted error code to its i18n key', () => {
    const expected: Record<string, string> = {
      oauth_access_denied: 'accessDenied',
      oauth_signup_disabled: 'signupDisabled',
      oauth_link_disabled: 'linkDisabled',
      'auth.oauth_email_unverified': 'emailUnverified',
      oauth_provider_unavailable: 'providerUnavailable',
      oauth_login_failed: 'loginFailed'
    };
    for (const [code, key] of Object.entries(expected)) {
      expect(parseOAuthCallback(`?success=false&error=${encodeURIComponent(code)}`, '')).toEqual({ kind: 'error', errorKey: key });
    }
  });

  it('collapses unknown, empty and hostile codes — and anything else — to the generic key', () => {
    for (const code of ['', 'internal: mongo down', '<script>alert(1)</script>', 'constructor', '__proto__', 'hasOwnProperty']) {
      expect(parseOAuthCallback(`?success=false&error=${encodeURIComponent(code)}`, '')).toEqual(GENERIC);
    }
    expect(parseOAuthCallback('', '')).toEqual(GENERIC);
    expect(parseOAuthCallback('?success=maybe', '')).toEqual(GENERIC);
    expect(parseOAuthCallback('?provider=google', '')).toEqual(GENERIC);
  });
});
```

Create `frontend-admin/src/utils/socialAuthUtils.test.ts`:

```ts
import { describe, it, expect, beforeEach } from 'vitest';
import {
  OAUTH_RETURN_TO_KEY,
  OAUTH_RETURN_TO_TTL_MS,
  stashOAuthReturnTo,
  takeOAuthReturnTo
} from './socialAuthUtils';

describe('OAuth return-target stash', () => {
  beforeEach(() => sessionStorage.clear());

  it('round-trips a safe target within ten minutes and deletes it on take', () => {
    stashOAuthReturnTo('/admin/modules?tab=x', 1_000);
    expect(takeOAuthReturnTo(1_000 + OAUTH_RETURN_TO_TTL_MS)).toBe('/admin/modules?tab=x');
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
    expect(takeOAuthReturnTo(2_000)).toBeNull();
  });

  it('ignores a stale record but still deletes it', () => {
    stashOAuthReturnTo('/admin/modules', 1_000);
    expect(takeOAuthReturnTo(1_000 + OAUTH_RETURN_TO_TTL_MS + 1)).toBeNull();
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
  });

  it('ignores a record from the future', () => {
    stashOAuthReturnTo('/admin/modules', 5_000);
    expect(takeOAuthReturnTo(4_999)).toBeNull();
  });

  it('never stashes an unsafe target and clears a stale one instead', () => {
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, JSON.stringify({ target: '/old', createdAt: 1 }));
    stashOAuthReturnTo('//evil.example', 1_000);
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
    stashOAuthReturnTo(null, 1_000);
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
  });

  it('re-sanitises on take (sessionStorage is client-writable)', () => {
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, JSON.stringify({ target: 'https://evil.example', createdAt: 1_000 }));
    expect(takeOAuthReturnTo(1_001)).toBeNull();
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, JSON.stringify({ target: '/login', createdAt: 1_000 }));
    expect(takeOAuthReturnTo(1_001)).toBeNull();
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, '/legacy-plain-string');
    expect(takeOAuthReturnTo(1_001)).toBeNull();
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, JSON.stringify({ target: '/ok', createdAt: 'yesterday' }));
    expect(takeOAuthReturnTo(1_001)).toBeNull();
  });
});
```

Run: `cd /home/tore/orkestra/frontend-admin && npx vitest run src/utils/oauthCallbackParams.test.ts src/utils/socialAuthUtils.test.ts`
Expected: FAIL — module `./oauthCallbackParams` not found; `stashOAuthReturnTo` is not exported.

- [ ] **Step 2: Implement the two pure modules**

Create `frontend-admin/src/utils/oauthCallbackParams.ts`:

```ts
// The SPA side of the CLOSED OAuth callback contract
// (backend: handlers/oauth_callback_redirect.go). Everything the backend may
// put in the URL is enumerated here; anything else — an unknown provider, a
// half-formed MFA fragment, an MFA fragment next to a query outcome, a
// success next to an error — is the generic failure. Raw URL text is never
// surfaced: only the mapped i18n key is.

export const OAUTH_PROVIDERS = ['google', 'apple', 'github', 'discord'] as const;
export type OAuthProviderName = (typeof OAUTH_PROVIDERS)[number];

export const OAUTH_CALLBACK_ERROR_KEYS = {
  oauth_access_denied: 'accessDenied',
  oauth_signup_disabled: 'signupDisabled',
  oauth_link_disabled: 'linkDisabled',
  'auth.oauth_email_unverified': 'emailUnverified',
  oauth_provider_unavailable: 'providerUnavailable',
  oauth_login_failed: 'loginFailed'
} as const;

export type OAuthCallbackErrorKey =
  (typeof OAUTH_CALLBACK_ERROR_KEYS)[keyof typeof OAUTH_CALLBACK_ERROR_KEYS];

export type OAuthCallbackOutcome =
  | { kind: 'success'; provider: OAuthProviderName }
  | { kind: 'mfa'; challengeId: string; webauthnAvailable: boolean }
  | { kind: 'error'; errorKey: OAuthCallbackErrorKey };

const GENERIC: OAuthCallbackOutcome = { kind: 'error', errorKey: 'loginFailed' };

const isProvider = (v: string | null): v is OAuthProviderName =>
  v !== null && (OAUTH_PROVIDERS as readonly string[]).includes(v);

const errorKeyFor = (code: string): OAuthCallbackErrorKey =>
  Object.prototype.hasOwnProperty.call(OAUTH_CALLBACK_ERROR_KEYS, code)
    ? OAUTH_CALLBACK_ERROR_KEYS[code as keyof typeof OAUTH_CALLBACK_ERROR_KEYS]
    : 'loginFailed';

const MFA_KEYS = ['requiresMfa', 'mfaToken', 'webauthnAvailable'] as const;
const OUTCOME_KEYS = ['success', 'error', 'provider'] as const;

/**
 * Parse the callback URL parts. The MFA continuation is honoured ONLY from
 * the fragment and only when complete; a success only from the query and
 * only with an allowlisted provider; a failure only with `success=false`.
 */
export const parseOAuthCallback = (search: string, hash: string): OAuthCallbackOutcome => {
  const query = new URLSearchParams(search);
  const frag = new URLSearchParams(hash.startsWith('#') ? hash.slice(1) : hash);
  const hasMfa = MFA_KEYS.some(k => frag.has(k));
  const hasOutcome = OUTCOME_KEYS.some(k => query.has(k));

  if (hasMfa) {
    if (hasOutcome) return GENERIC; // ambiguous: the backend never sends both
    const token = frag.get('mfaToken');
    const webauthn = frag.get('webauthnAvailable');
    if (frag.get('requiresMfa') !== 'true' || !token || (webauthn !== 'true' && webauthn !== 'false')) {
      return GENERIC;
    }
    return { kind: 'mfa', challengeId: token, webauthnAvailable: webauthn === 'true' };
  }

  const success = query.get('success');
  if (success === 'true') {
    const provider = query.get('provider');
    if (query.has('error') || !isProvider(provider)) return GENERIC;
    return { kind: 'success', provider };
  }
  if (success === 'false') {
    return { kind: 'error', errorKey: errorKeyFor(query.get('error') ?? '') };
  }
  return GENERIC;
};
```

In `frontend-admin/src/utils/socialAuthUtils.ts`, add `import { sanitizeReturnTo } from 'utils/returnTo';` and replace the `OAUTH_RETURN_TO_KEY` comment + constant (lines 21-24) with:

```ts
// sessionStorage key holding the deep link to return to after the OAuth
// round-trip completes. Router state can't survive the redirect out to the
// IdP, so it is stashed here as a `{target, createdAt}` record;
// SocialAuthCallback takes-and-deletes it (in an effect, never during
// render) on EVERY outcome and honours it only when it is younger than
// OAUTH_RETURN_TO_TTL_MS and still passes sanitizeReturnTo (sessionStorage
// is client-writable).
export const OAUTH_RETURN_TO_KEY = 'oauth_return_to';
export const OAUTH_RETURN_TO_TTL_MS = 10 * 60 * 1000;

interface OAuthReturnRecord {
  target: string;
  createdAt: number;
}

export const stashOAuthReturnTo = (target: string | null | undefined, now: number = Date.now()): void => {
  const safe = sanitizeReturnTo(target);
  if (!safe) {
    // Also clears any stale value from a previous, abandoned attempt.
    sessionStorage.removeItem(OAUTH_RETURN_TO_KEY);
    return;
  }
  const record: OAuthReturnRecord = { target: safe, createdAt: now };
  sessionStorage.setItem(OAUTH_RETURN_TO_KEY, JSON.stringify(record));
};

/** Take-and-delete: the record is removed on every call, whatever its state. */
export const takeOAuthReturnTo = (now: number = Date.now()): string | null => {
  const raw = sessionStorage.getItem(OAUTH_RETURN_TO_KEY);
  sessionStorage.removeItem(OAUTH_RETURN_TO_KEY);
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== 'object') return null;
  const { target, createdAt } = parsed as Partial<OAuthReturnRecord>;
  if (typeof createdAt !== 'number' || !Number.isFinite(createdAt)) return null;
  if (now < createdAt || now - createdAt > OAUTH_RETURN_TO_TTL_MS) return null;
  return sanitizeReturnTo(target);
};
```

and in `initiateSocialLogin` replace the `if (returnTo) { sessionStorage.setItem(OAUTH_RETURN_TO_KEY, returnTo); } else { … removeItem … }` block (lines 64-69) with `stashOAuthReturnTo(returnTo);`.

Run: `npx vitest run src/utils/oauthCallbackParams.test.ts src/utils/socialAuthUtils.test.ts`
Expected: PASS.

- [ ] **Step 3: Extract `MfaVerifyPanel`**

Create `frontend-admin/src/components/authentication/MfaVerifyPanel.tsx` — today's `LoginMfaVerify` body (lines 45-236) turned into a prop-driven component. Only the props, the removed `location.state` reads and the removed "bounce back" effect differ from the current file; the form, the TOTP/backup toggle and the passkey ceremony are verbatim:

```tsx
import { useState, FormEvent } from 'react';
import { Alert, Button, Card, Form } from 'react-bootstrap';
import { Link, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import AuthCardLayout from 'layouts/AuthCardLayout';
import { useAppDispatch } from 'store/hooks';
import {
  useLoginVerifyMfaMutation,
  useWebAuthnLoginBeginMutation,
  useWebAuthnLoginFinishMutation
} from 'store/api/mfaApi';
import {
  browserSupportsWebAuthn,
  decodeRequestOptions,
  encodeAssertion
} from 'store/api/webauthnCodec';
import { login as loginAction } from 'store/slices/authSlice';

export interface MfaVerifyPanelProps {
  /**
   * One-shot login challenge id. The caller holds it in component memory
   * (OAuth path) or in location.state (password path) — never in a URL.
   */
  challengeId: string;
  email?: string;
  webauthnAvailable: boolean;
  /** Already sanitised by the caller (sanitizeReturnTo ?? DEFAULT_POST_LOGIN). */
  returnTo: string;
}

/**
 * Completes a login that paused on the MFA challenge. Shared by the
 * password path (LoginMfaVerify page) and the OAuth callback
 * (SocialAuthCallback renders it locally from the scrubbed fragment). Either:
 *   - POST a TOTP / backup code to /v1/auth/operator/mfa/login/verify, or
 *   - run the WebAuthn assertion ceremony when webauthnAvailable and the
 *     user picks "Use a passkey".
 *
 * Both branches dispatch loginAction with the same BackendUser shape so
 * downstream consumers don't care which factor satisfied the partial.
 */
const MfaVerifyPanel = ({
  challengeId,
  email,
  webauthnAvailable,
  returnTo
}: MfaVerifyPanelProps) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const dispatch = useAppDispatch();
  const passkeyOffered = webauthnAvailable && browserSupportsWebAuthn();

  const [code, setCode] = useState('');
  const [useBackup, setUseBackup] = useState(false);
  const [localError, setLocalError] = useState<string | null>(null);
  const [passkeyBusy, setPasskeyBusy] = useState(false);

  const [verify, { isLoading }] = useLoginVerifyMfaMutation();
  const [waBegin] = useWebAuthnLoginBeginMutation();
  const [waFinish] = useWebAuthnLoginFinishMutation();

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setLocalError(null);
    if (!code.trim()) {
      setLocalError(t('auth.mfa.errors.missingCode'));
      return;
    }

    try {
      const res = await verify({
        challengeId,
        code: code.trim(),
        useBackup
      }).unwrap();
      dispatch(loginAction({ userData: res.user }));
      navigate(returnTo, { replace: true });
    } catch (err: unknown) {
      const anyErr = err as { status?: number; data?: { detail?: string } };
      if (anyErr?.status === 401) {
        setLocalError(t('auth.mfa.errors.incorrectCode'));
      } else if (anyErr?.status === 429) {
        setLocalError(t('auth.mfa.errors.tooMany'));
      } else {
        setLocalError(anyErr?.data?.detail ?? t('auth.mfa.errors.generic'));
      }
    }
  };

  const handlePasskey = async () => {
    setLocalError(null);
    setPasskeyBusy(true);
    try {
      const beginRes = await waBegin({
        loginChallengeId: challengeId
      }).unwrap();
      const opts = decodeRequestOptions(beginRes.publicKey);
      const cred = (await navigator.credentials.get({
        publicKey: opts
      })) as PublicKeyCredential | null;
      if (!cred) {
        setPasskeyBusy(false);
        return;
      }
      const finishRes = await waFinish({
        loginChallengeId: challengeId,
        webauthnChallengeId: beginRes.challengeId,
        assertionResponse: encodeAssertion(cred)
      }).unwrap();
      dispatch(loginAction({ userData: finishRes.user }));
      navigate(returnTo, { replace: true });
    } catch (err: unknown) {
      const anyErr = err as {
        name?: string;
        status?: number;
        data?: { detail?: string };
      };
      if (anyErr?.name === 'NotAllowedError') {
        setLocalError(t('auth.mfa.errors.passkeyCancelled'));
      } else if (anyErr?.status === 401) {
        setLocalError(t('auth.mfa.errors.passkeyFailed'));
      } else {
        setLocalError(
          anyErr?.data?.detail ?? t('auth.mfa.errors.passkeyGeneric')
        );
      }
      setPasskeyBusy(false);
    }
  };

  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5">
          <div className="text-center mb-4">
            <h3 className="mb-2">{t('auth.mfa.title')}</h3>
            <p className="text-muted mb-0">
              {email
                ? t('auth.mfa.promptForEmail', { email })
                : t('auth.mfa.promptDefault')}
            </p>
          </div>

          {localError && (
            <Alert
              variant="danger"
              className="mb-3"
              onClose={() => setLocalError(null)}
              dismissible
            >
              {localError}
            </Alert>
          )}

          {passkeyOffered && (
            <div className="d-grid mb-3">
              <Button
                variant="outline-primary"
                size="lg"
                disabled={passkeyBusy}
                onClick={handlePasskey}
              >
                {passkeyBusy
                  ? t('auth.mfa.passkeyWaiting')
                  : t('auth.mfa.passkeyButton')}
              </Button>
              <div className="text-center text-muted fs-10 mt-2">
                {t('auth.mfa.passkeyOr')}
              </div>
            </div>
          )}

          <Form onSubmit={handleSubmit} noValidate>
            <Form.Group className="mb-3">
              <Form.Label>
                {useBackup
                  ? t('auth.mfa.backupCode')
                  : t('auth.mfa.authenticatorCode')}
              </Form.Label>
              <Form.Control
                type="text"
                inputMode={useBackup ? 'text' : 'numeric'}
                autoComplete="one-time-code"
                autoFocus
                value={code}
                onChange={e => setCode(e.target.value)}
                placeholder={
                  useBackup
                    ? t('auth.mfa.backupPlaceholder')
                    : t('auth.mfa.authenticatorPlaceholder')
                }
                required
              />
            </Form.Group>

            <div className="d-grid mb-3">
              <Button
                type="submit"
                variant="primary"
                size="lg"
                disabled={isLoading}
              >
                {isLoading ? t('auth.mfa.submitting') : t('auth.mfa.submit')}
              </Button>
            </div>

            <div className="d-flex justify-content-between fs-10">
              <button
                type="button"
                className="btn btn-link p-0"
                onClick={() => {
                  setUseBackup(v => !v);
                  setCode('');
                }}
              >
                {useBackup
                  ? t('auth.mfa.useAuthenticator')
                  : t('auth.mfa.useBackup')}
              </button>
              <Link to="/login">{t('auth.mfa.back')}</Link>
            </div>
          </Form>
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default MfaVerifyPanel;
```

Replace `frontend-admin/src/components/authentication/LoginMfaVerify.tsx` with the wrapper (password path, unchanged behaviour):

```tsx
import { useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router';
import MfaVerifyPanel from 'components/authentication/MfaVerifyPanel';
import { DEFAULT_POST_LOGIN, sanitizeReturnTo } from 'utils/returnTo';

interface LocationState {
  challengeId?: string;
  email?: string;
  webauthnAvailable?: boolean;
  // Deep link captured before login, forwarded by EmailPasswordForm so MFA
  // completion lands on the originally-requested page.
  returnTo?: string;
}

/**
 * Password-login MFA page: the caller (EmailPasswordForm) arrives here with
 * the challenge in `location.state`. The OAuth path does NOT use this page —
 * SocialAuthCallback renders MfaVerifyPanel locally from a scrubbed fragment
 * so the one-shot challenge never sits in router or browser history state.
 */
const LoginMfaVerify = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const state = (location.state ?? {}) as LocationState;
  const returnTo = sanitizeReturnTo(state.returnTo) ?? DEFAULT_POST_LOGIN;

  // Without a challenge id we cannot complete the flow — bounce back.
  useEffect(() => {
    if (!state.challengeId) {
      navigate('/login', { replace: true });
    }
  }, [state.challengeId, navigate]);

  if (!state.challengeId) return null;
  return (
    <MfaVerifyPanel
      challengeId={state.challengeId}
      email={state.email}
      webauthnAvailable={!!state.webauthnAvailable}
      returnTo={returnTo}
    />
  );
};

export default LoginMfaVerify;
```

Run: `npx vitest run src/components/authentication/LoginMfaVerify.test.tsx && npm run typecheck`
Expected: the three existing tests PASS unchanged; typecheck clean.

- [ ] **Step 4: Write the failing `SocialAuthCallback` tests**

Create `frontend-admin/src/components/authentication/SocialAuthCallback.test.tsx`:

```tsx
import { describe, it, expect, beforeEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { Routes, Route, useLocation } from 'react-router';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import SocialAuthCallback from './SocialAuthCallback';
import { DEFAULT_POST_LOGIN } from 'utils/returnTo';
import { OAUTH_RETURN_TO_KEY, OAUTH_RETURN_TO_TTL_MS } from 'utils/socialAuthUtils';

const Probe = ({ label }: { label: string }) => {
  const location = useLocation();
  return (
    <>
      <div data-testid={`${label}-location`}>
        {location.pathname + location.search + location.hash}
      </div>
      <div data-testid={`${label}-state`}>{JSON.stringify(location.state)}</div>
    </>
  );
};

const sessionBody = {
  accessToken: 'at-1',
  tokenType: 'Bearer',
  expiresIn: 900,
  success: true,
  user: {
    id: 'u-1',
    email: 'op@example.com',
    fullName: 'Op User',
    isActive: true,
    roles: ['operator'],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z'
  }
};

// A session endpoint the test releases by hand, so "scrub before the first
// await" is observable: the URL must be clean while the request is pending.
const deferredSession = () => {
  let release!: () => void;
  const gate = new Promise<void>(resolve => {
    release = resolve;
  });
  let hits = 0;
  server.use(
    http.get(url('/v1/auth/session'), async () => {
      hits++;
      await gate;
      return HttpResponse.json(sessionBody);
    })
  );
  return { release, hits: () => hits };
};

const renderCallback = (search: string, hash = '') =>
  renderWithProviders(
    <Routes>
      <Route
        path="/auth/callback"
        element={
          <>
            <SocialAuthCallback />
            <Probe label="cb" />
          </>
        }
      />
      <Route path={DEFAULT_POST_LOGIN} element={<Probe label="dashboard" />} />
      <Route path="/admin/modules" element={<Probe label="deeplink" />} />
      <Route path="/login" element={<Probe label="login" />} />
    </Routes>,
    { routerEntries: [{ pathname: '/auth/callback', search, hash }] }
  );

describe('SocialAuthCallback', () => {
  beforeEach(() => sessionStorage.clear());

  it('scrubs the URL before the session request resolves, then lands on the default page', async () => {
    const session = deferredSession();
    renderCallback('?success=true&provider=google');

    await waitFor(() => expect(screen.getByTestId('cb-location')).toHaveTextContent(/^\/auth\/callback$/));
    // The request is issued only after the scrub, and nothing navigates
    // while it is pending.
    await waitFor(() => expect(session.hits()).toBe(1));
    expect(screen.getByTestId('cb-location')).toHaveTextContent(/^\/auth\/callback$/);
    expect(screen.queryByTestId('dashboard-location')).toBeNull();

    session.release();
    expect(await screen.findByTestId('dashboard-location')).toHaveTextContent(DEFAULT_POST_LOGIN);
  });

  it('honours a fresh stashed return target and deletes it', async () => {
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, JSON.stringify({ target: '/admin/modules', createdAt: Date.now() }));
    const session = deferredSession();
    renderCallback('?success=true&provider=github');
    // Taken in the layout effect — already gone once render returns.
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
    session.release();
    expect(await screen.findByTestId('deeplink-location')).toHaveTextContent('/admin/modules');
  });

  it('ignores a stale stashed return target', async () => {
    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({ target: '/admin/modules', createdAt: Date.now() - OAUTH_RETURN_TO_TTL_MS - 1 })
    );
    const session = deferredSession();
    renderCallback('?success=true&provider=github');
    session.release();
    expect(await screen.findByTestId('dashboard-location')).toHaveTextContent(DEFAULT_POST_LOGIN);
  });

  it('takes the return target on an error outcome too', async () => {
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, JSON.stringify({ target: '/admin/modules', createdAt: Date.now() }));
    renderCallback('?success=false&error=oauth_access_denied');
    expect(await screen.findByText(/cancelled at the identity provider/i)).toBeInTheDocument();
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
  });

  it('renders the MFA panel locally from the fragment, with no router state and a clean URL', async () => {
    server.use(
      http.post(url('/v1/auth/operator/mfa/login/verify'), async ({ request }) => {
        const body = (await request.json()) as { challengeId: string };
        if (body.challengeId !== 'ch-1') return HttpResponse.json({ detail: 'wrong challenge' }, { status: 401 });
        return HttpResponse.json({ success: true, user: sessionBody.user });
      })
    );
    renderCallback('', '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false');

    expect(await screen.findByRole('heading', { name: /two-factor/i })).toBeInTheDocument();
    expect(screen.getByTestId('cb-location')).toHaveTextContent(/^\/auth\/callback$/);
    // The challenge lives in component memory only: no router state.
    expect(screen.getByTestId('cb-state')).toHaveTextContent(/^null$/);

    const user = userEvent.setup();
    await user.type(screen.getByRole('textbox'), '123456');
    await user.click(screen.getByRole('button', { name: /verify and sign in/i }));
    expect(await screen.findByTestId('dashboard-location')).toHaveTextContent(DEFAULT_POST_LOGIN);
  });

  it('treats an ambiguous payload (MFA fragment + query outcome) as the generic failure', async () => {
    renderCallback('?success=true&provider=google', '#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false');
    expect(await screen.findByText(/authentication failed\. please try again/i)).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: /two-factor/i })).toBeNull();
    expect(screen.queryByTestId('dashboard-location')).toBeNull();
  });

  it('renders the mapped copy for an allowlisted code, never the raw code', async () => {
    renderCallback('?success=false&error=oauth_signup_disabled');
    expect(await screen.findByText(/invitation-only/i)).toBeInTheDocument();
    expect(screen.queryByText(/oauth_signup_disabled/)).toBeNull();
    expect(screen.getByTestId('cb-location')).toHaveTextContent(/^\/auth\/callback$/);
  });

  it('collapses an unknown code to the generic copy and never renders raw URL text', async () => {
    renderCallback('?success=false&error=%3Cscript%3Ealert(1)%3C%2Fscript%3E');
    expect(await screen.findByText(/authentication failed\. please try again/i)).toBeInTheDocument();
    expect(document.body.textContent).not.toContain('<script>');
    expect(document.body.textContent).not.toContain('alert(1)');
  });

  it('treats a signed-out session as a login error, never a protected route', async () => {
    server.use(http.get(url('/v1/auth/session'), () => HttpResponse.json({ authenticated: false })));
    renderCallback('?success=true&provider=google');
    expect(await screen.findByText(/no session could be established/i)).toBeInTheDocument();
    expect(screen.queryByTestId('dashboard-location')).toBeNull();
  });

  it('keeps the bootstrap state and offers retry when the session endpoint is unavailable', async () => {
    let calls = 0;
    server.use(
      http.get(url('/v1/auth/session'), () => {
        calls++;
        return calls === 1
          ? HttpResponse.json({ code: 'session_enforcement_unavailable' }, { status: 503 })
          : HttpResponse.json(sessionBody);
      })
    );
    renderCallback('?success=true&provider=google');
    const retry = await screen.findByRole('button', { name: /try again/i });
    expect(screen.queryByTestId('dashboard-location')).toBeNull();
    await userEvent.setup().click(retry);
    expect(await screen.findByTestId('dashboard-location')).toHaveTextContent(DEFAULT_POST_LOGIN);
    expect(calls).toBe(2);
  });
});
```

Run: `npx vitest run src/components/authentication/SocialAuthCallback.test.tsx`
Expected: FAIL (the current component invalidates the cache and navigates after 100 ms without a session, renders raw text, and navigates to `/mfa/verify` with router state).

- [ ] **Step 5: Rewrite `SocialAuthCallback`**

Replace `frontend-admin/src/components/authentication/SocialAuthCallback.tsx` with:

```tsx
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { Alert, Button, Card, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useAppDispatch } from 'store/hooks';
import { authApi } from 'store/api/authApi';
import { setAccessToken, setUserFromApiResponse } from 'store/slices/authSlice';
import AuthCardLayout from 'layouts/AuthCardLayout';
import MfaVerifyPanel from 'components/authentication/MfaVerifyPanel';
import { parseOAuthCallback, type OAuthCallbackOutcome } from 'utils/oauthCallbackParams';
import { takeOAuthReturnTo } from 'utils/socialAuthUtils';
import { DEFAULT_POST_LOGIN } from 'utils/returnTo';

type Phase = 'working' | 'signedOut' | 'unavailable' | 'error';

/**
 * Landing page of the backend's OAuth callback redirect
 * (handlers/oauth_callback_redirect.go — a CLOSED contract, parsed by
 * utils/oauthCallbackParams):
 *   ?success=true&provider=<p>                  → bootstrap the session from the refresh cookie
 *   ?success=false&error=<allowlisted code>     → mapped copy, never raw text
 *   #requiresMfa=true&mfaToken=<id>&webauthn…   → render the MFA panel locally
 *
 * The URL is parsed ONCE on the first render (pure) and scrubbed in a
 * layout effect — before any await — so neither the one-shot challenge id
 * nor the outcome survives in history, referrers or a reload. The stashed
 * return target is taken-and-deleted in that same effect (a destructive
 * read never runs during render). Success navigates only after
 * GET /v1/auth/session confirmed the refresh-cookie session; a signed-out
 * answer is a login error, an unavailable one keeps the page and offers
 * retry.
 */
const SocialAuthCallback = () => {
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const dispatch = useAppDispatch();

  // Parsed once, in component memory only. Pure — no storage touched here.
  const outcomeRef = useRef<OAuthCallbackOutcome | null>(null);
  if (outcomeRef.current === null) {
    outcomeRef.current = parseOAuthCallback(location.search, location.hash);
  }
  const outcome = outcomeRef.current;

  // Set by the layout effect below; null until then (first paint only).
  const [returnTo, setReturnTo] = useState<string | null>(null);
  const [phase, setPhase] = useState<Phase>(outcome.kind === 'error' ? 'error' : 'working');
  const [attempt, setAttempt] = useState(0);

  // One-shot, before paint and before any effect: take the return target
  // (destructive read → effect, never render) and replace the history entry
  // with the bare path.
  const initialised = useRef(false);
  useLayoutEffect(() => {
    if (initialised.current) return;
    initialised.current = true;
    setReturnTo(takeOAuthReturnTo() ?? DEFAULT_POST_LOGIN);
    if (location.search || location.hash) {
      navigate(location.pathname, { replace: true });
    }
  }, [location.pathname, location.search, location.hash, navigate]);

  // Success: force a fresh /v1/auth/session and navigate only once it
  // confirms a user. `attempt` re-arms the effect for the retry button.
  useEffect(() => {
    if (outcome.kind !== 'success' || returnTo === null) return;
    let cancelled = false;
    const subscription = dispatch(
      authApi.endpoints.getSession.initiate(undefined, { forceRefetch: true })
    );
    subscription
      .unwrap()
      .then(session => {
        if (cancelled) return;
        if (!session) {
          setPhase('signedOut');
          return;
        }
        // Mirror what useAuth does from the same cache entry, so the guard
        // on the destination sees an authenticated store immediately.
        dispatch(setUserFromApiResponse(session.user));
        if (session.accessToken) {
          dispatch(setAccessToken({ accessToken: session.accessToken, expiresIn: session.expiresIn }));
        }
        navigate(returnTo, { replace: true });
      })
      .catch(() => {
        if (!cancelled) setPhase('unavailable');
      })
      .finally(() => subscription.unsubscribe());
    return () => {
      cancelled = true;
    };
  }, [attempt, outcome.kind, returnTo, dispatch, navigate]);

  // Terminal error states bounce to the login page after a short pause.
  useEffect(() => {
    if (phase !== 'error' && phase !== 'signedOut') return;
    const timer = setTimeout(() => navigate('/login', { replace: true }), 3000);
    return () => clearTimeout(timer);
  }, [phase, navigate]);

  if (outcome.kind === 'mfa') {
    if (returnTo === null) return null; // before the layout effect — never painted
    return (
      <MfaVerifyPanel
        challengeId={outcome.challengeId}
        webauthnAvailable={outcome.webauthnAvailable}
        returnTo={returnTo}
      />
    );
  }

  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5 text-center">
          {phase === 'working' && (
            <div aria-busy="true">
              <Spinner animation="border" size="sm" className="me-2" />
              <span className="text-muted">{t('auth.social.callback.verifying')}</span>
            </div>
          )}

          {phase === 'error' && outcome.kind === 'error' && (
            <>
              <Alert variant="danger" className="mb-3">
                <h6>{t('auth.social.callback.failureTitle')}</h6>
                <p className="mb-0">{t(`auth.social.callback.errors.${outcome.errorKey}`)}</p>
              </Alert>
              <p className="text-muted">{t('auth.social.callback.redirectingToLogin')}</p>
            </>
          )}

          {phase === 'signedOut' && (
            <>
              <Alert variant="danger" className="mb-3">
                <h6>{t('auth.social.callback.failureTitle')}</h6>
                <p className="mb-0">{t('auth.social.callback.sessionSignedOut')}</p>
              </Alert>
              <p className="text-muted">{t('auth.social.callback.redirectingToLogin')}</p>
            </>
          )}

          {phase === 'unavailable' && (
            <>
              <Alert variant="warning" className="mb-3">
                <p className="mb-0">{t('auth.social.callback.sessionUnavailable')}</p>
              </Alert>
              <div className="d-grid gap-2">
                <Button
                  variant="orkestra-primary"
                  onClick={() => {
                    setPhase('working');
                    setAttempt(a => a + 1);
                  }}
                >
                  {t('auth.social.callback.retry')}
                </Button>
                <Link to="/login" className="fs-10">
                  {t('auth.social.callback.backToLogin')}
                </Link>
              </div>
            </>
          )}
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default SocialAuthCallback;
```

- [ ] **Step 6: Add the i18n keys (EN and IT — the parity test fails otherwise)**

In `frontend-admin/src/locales/en.json`, replace `auth.social.callback` (currently `failureTitle`, `redirectingToLogin`, `oauthErrorPrefix`, `genericFailure`) with:

```json
"callback": {
  "failureTitle": "Authentication failed",
  "redirectingToLogin": "Redirecting to the sign-in page...",
  "verifying": "Completing sign-in...",
  "sessionSignedOut": "Sign-in completed, but no session could be established. Please sign in again.",
  "sessionUnavailable": "The server could not confirm your session right now. Try again in a moment.",
  "retry": "Try again",
  "backToLogin": "Back to sign-in",
  "errors": {
    "accessDenied": "Sign-in was cancelled at the identity provider.",
    "signupDisabled": "Sign-up is currently invitation-only. Contact an administrator.",
    "linkDisabled": "An account with this email already exists. Sign in and link the provider from your security settings.",
    "emailUnverified": "The identity provider has not verified this email address, so it cannot be used to sign in.",
    "providerUnavailable": "This sign-in provider is temporarily unavailable. Try again later.",
    "loginFailed": "Authentication failed. Please try again."
  }
}
```

and in `it.json`:

```json
"callback": {
  "failureTitle": "Autenticazione fallita",
  "redirectingToLogin": "Reindirizzamento alla pagina di accesso...",
  "verifying": "Completamento dell'accesso...",
  "sessionSignedOut": "Accesso completato, ma non è stato possibile stabilire una sessione. Accedi di nuovo.",
  "sessionUnavailable": "Il server non riesce a confermare la sessione in questo momento. Riprova tra poco.",
  "retry": "Riprova",
  "backToLogin": "Torna all'accesso",
  "errors": {
    "accessDenied": "L'accesso è stato annullato presso il provider di identità.",
    "signupDisabled": "La registrazione è attualmente solo su invito. Contatta un amministratore.",
    "linkDisabled": "Esiste già un account con questa email. Accedi e collega il provider dalle impostazioni di sicurezza.",
    "emailUnverified": "Il provider di identità non ha verificato questo indirizzo email, quindi non può essere usato per accedere.",
    "providerUnavailable": "Questo provider di accesso non è al momento disponibile. Riprova più tardi.",
    "loginFailed": "Autenticazione fallita. Riprova."
  }
}
```

`oauthErrorPrefix` and `genericFailure` are removed from both files (no consumer remains — `grep -rn "oauthErrorPrefix\|callback.genericFailure" src` must print nothing).

- [ ] **Step 7: Run the frontend gates**

Run: `cd /home/tore/orkestra/frontend-admin && npx vitest run src/components/authentication src/utils src/locales && npm run typecheck && npm run lint`
Expected: all PASS (the `SocialLoginForm.test.tsx` mock of `initiateSocialLogin` still applies; `LoginMfaVerify.test.tsx` unchanged and green; `parity.test.ts` green). The pinned prettier may reformat untouched locale lines — accept the churn, never `--no-verify`.

- [ ] **Step 8: Document**

In `frontend-admin/CLAUDE.md`, line 66, after "…never hardcode the post-login destination." append:

```markdown
The OAuth landing is `SocialAuthCallback`, bound to the backend's CLOSED callback contract (`?success=true&provider=<google|apple|github|discord>`, `?success=false&error=<allowlisted>`, MFA continuation in the **fragment** `#requiresMfa=true&mfaToken=&webauthnAvailable=<true|false>`; parser in `utils/oauthCallbackParams.ts` — an unknown provider, a half-formed fragment, a fragment next to a query outcome or a success next to an error is the generic failure; error codes → `auth.social.callback.errors.*`, raw URL text is never rendered). It parses the URL once on first render, then in a layout effect takes-and-deletes the return target and scrubs the URL (`navigate(pathname, {replace:true})`) before its first await; it force-fetches `getSession` and navigates only after the refresh-cookie session is confirmed (signed-out → login error, 503 → retry), and renders the MFA challenge through `MfaVerifyPanel` from component memory — never `location.state` (the password path's `LoginMfaVerify` page still reads router state, which never travels in a URL). The OAuth return target is a `{target, createdAt}` record under `oauth_return_to`, written by `stashOAuthReturnTo` and taken by `takeOAuthReturnTo` — a destructive read that must run in an effect, never during render — honoured only within 10 minutes and after `sanitizeReturnTo`.
```

- [ ] **Step 9: Commit**

```bash
git add frontend-admin/src/utils/socialAuthUtils.ts frontend-admin/src/utils/socialAuthUtils.test.ts frontend-admin/src/utils/oauthCallbackParams.ts frontend-admin/src/utils/oauthCallbackParams.test.ts frontend-admin/src/components/authentication/MfaVerifyPanel.tsx frontend-admin/src/components/authentication/LoginMfaVerify.tsx frontend-admin/src/components/authentication/SocialAuthCallback.tsx frontend-admin/src/components/authentication/SocialAuthCallback.test.tsx frontend-admin/src/locales/en.json frontend-admin/src/locales/it.json frontend-admin/CLAUDE.md
git commit -m "feat(frontend-admin): OAuth callback page — closed parser, scrub and take in a layout effect, awaited session, local MFA panel, 10-minute return target"
```

---

### Task 10: Documentation sweep and the full gates

**Files:**
- Modify: `docs/site/architecture/authentication-flow.mdx` (lines 46-64 endpoint list; §5 "Web flow" lines 184-192)
- Modify: `docs/site/modules/core/auth.mdx` ("Two surfaces, one module", line 20-25)
- Verify: every CLAUDE.md / docs edit from Tasks 1–9 is present (`git log --stat dev..HEAD -- '*CLAUDE.md' 'docs/site/**' docker/.env.example`)

- [ ] **Step 1: `authentication-flow.mdx`**

In the public-endpoint block (line 60): `GET   /v1/auth/{tier}/providers             list OAuth providers configured for this audience` → `list OAuth providers USABLE on this audience (toggle on + structurally complete; 503 when the auth document cannot be read)`, and add after the `oauth/login` line: `GET   /v1/auth/client/oauth/complete        (client host only) completes a relayed client-tier web login`.

Replace §5 "Web flow" steps 1–5 (lines 188-192) with:

```markdown
1. Frontend calls `POST /v1/auth/{tier}/oauth/login` with `{provider}`. The backend resolves the provider **strictly from one config read** (toggle on for this surface, client ID + redirect URL + secret present — 403 `auth.oauth_provider_disabled` otherwise, 503 `auth.policy_unavailable` when the auth document cannot be read), constructs a signed HS256 state JWT `{tier, csrf, shost, exp}` (HMAC secret deterministically derived from the JWT private key — every replica agrees without an env var, rotates implicitly when JWT keys rotate), drops the CSRF nonce in an HttpOnly cookie **on the host that served this call**, and stores per-flow side data (`provider`, `tier`, `deviceInfo`, `securityContext`, and a `redirectUri` that is always the configured tier SPA — never the `Origin` header) in Redis keyed by the nonce, with a 10-minute TTL. Returns `{authUrl, state}`.
2. Frontend redirects the user to the provider.
3. Provider redirects back to **the single shared callback URL** registered with each provider (`/v1/auth/oauth/{provider}/callback`, mounted on the operator mux only — one redirect URI per provider in IdP config; all four are raw handlers). The callback establishes **trust before destination**: signature/expiry, the browser binding, the **one-shot** Redis row (consumed atomically — a replay or a concurrent second presentation is refused), `state.tier == redis.tier`, `redis.provider == this endpoint's provider`, and the link-mode pair; any failure is a terminal generic 400 with no redirect. Only then does it dispatch to the matching tier's `AuthHandler` (empty / unknown tier falls through to the legacy operator handler) and interpret the IdP's `error`.
4. The dispatched-to handler re-resolves the provider from the same strict read, exchanges the code and fetches user info. **Where the flow completes depends on who owns the cookie.** An operator/legacy flow completes here: `HandleOAuthCallbackWithLinking` (find by `(provider, providerId)`; for an unlinked identity it first requires a provider-**verified** email and an establishable auto-link policy — before any local email lookup — then links an existing email account or signs up), active-user check, a token pair stamped with the audience's `aud`, the refresh cookie on the operator cookie domain, and a redirect to the operator SPA. A **client-tier flow cannot** get its cookie from the operator host (a response from `console.*` cannot set a cookie for `api.*`), so the callback stores a 60-second single-use encrypted relay record and redirects the browser to `CLIENT_API_URL/v1/auth/client/oauth/complete?relay=<id>`; that endpoint, on the client API host, takes the record once, **requires** the state cookie the same host set in step 1 (a login-CSRF attempt that finished someone else's flow is refused before any token exists), runs the same application half on the client authService, sets the client refresh cookie on its own host and redirects to the client SPA. Both destinations use one closed contract: `?success=true&provider=<p>` on success, `?success=false&error=<oauth_access_denied|oauth_signup_disabled|oauth_link_disabled|auth.oauth_email_unverified|oauth_provider_unavailable|oauth_login_failed>` on any valid-state failure, and `#requiresMfa=true&mfaToken=<one-shot id>&webauthnAvailable=<bool>` in the **fragment** for an MFA continuation (no cookie is written then). Every redirect carries `Referrer-Policy: no-referrer`; **no callback URL ever contains an access token, refresh token, email or user id.**
5. Frontend scrubs the URL, then calls `GET /v1/auth/session` (operator-only mount, post-OAuth cookie-based bootstrap) to exchange the refresh cookie for a fresh access token + user payload, and navigates only after that answer.
```

Keep the closing sentence ("The signed state + CSRF-keyed Redis row …") and append: ` The one-shot state, the provider binding and the relay keep the challenge id, every credential and every cross-tier cookie exactly where they belong.`

- [ ] **Step 2: `auth.mdx`**

After the paragraph ending "…independently gated by config." (line 24) add:

```markdown
Web OAuth is tier-aware end to end: a flow started on the client surface lands on the client SPA and a flow started on the console lands on the console, each under one closed callback contract (`success`/`provider`, an allowlisted `error` code, or an MFA continuation carried in the URL fragment) that never contains a token, an email or a user id. Because every provider redirects to the operator host and that host cannot set a client cookie, a client-tier login finishes through a single-use relay on the client API host, which also verifies that the browser completing the flow is the one that started it. A provider is offered to a surface only when its toggle is on **and** every field the flow needs is present; a defect on one provider removes that provider alone, while an unreadable auth document answers 503 everywhere so a configuration outage can never quietly widen access. An identity that has never been linked is matched to an existing account — or allowed to sign up — only with an address the identity provider itself marked verified.
```

- [ ] **Step 3: Verify the docs of every earlier task are in place**

Run: `cd /home/tore/orkestra && git log --stat --oneline dev..HEAD -- backend/pkg/sdk/CLAUDE.md docs/site/sdk/config-service.mdx backend/internal/core/auth/CLAUDE.md frontend-admin/CLAUDE.md backend/openapi/enterprise.json docker/.env.example docs/site/operating/cookie-hardening-cross-tier.mdx`
Expected: each file appears in the commit of the task that changed what it documents (1; 3/4/6/7/8; 9; 7/8; 7; 7). If one is missing, fix it in this task's commit and say so in the ledger.

- [ ] **Step 4: Run every gate on the exact HEAD**

```bash
cd /home/tore/orkestra && git diff --check dev..HEAD
MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true' make -C /home/tore/orkestra ci-backend 2>&1 | tail -40
make -C /home/tore/orkestra ci-frontend-admin 2>&1 | tail -20
grep -rn "internal/" backend/pkg/sdk/ --include="*.go" | grep -v '_test.go' | grep -v '^\S*:\s*//'
```
Expected: `ci-backend` green with **0 SKIP** (lint, tenantscope, policycoverage, piiscan, errquality, vuln, tests, build, openapi-check); `ci-frontend-admin` green; the grep prints nothing. Also render the docs site: fresh `git clone` of `orkestra-docs` into the scratchpad, `npm ci`, `MONOREPO_LOCAL_PATH=/home/tore/orkestra npm run sync`, `CI=true npm run build` — must succeed (the `.mdx` edits are MDX; `{` / `<` in prose must be escaped or fenced).

- [ ] **Step 5: Commit**

```bash
cd /home/tore/orkestra && git add docs/site/architecture/authentication-flow.mdx docs/site/modules/core/auth.mdx
git commit -m "docs(auth): OAuth web flow — one-shot state, provider binding, client-tier relay, per-tier SPA, closed callback contract, strict provider usability"
```

Then stop: the branch is ready for the final whole-branch review and the user's own review (`feedback_spec_first_hard_reviews`). Merge target is `dev`; PR 3 branches from `dev` after this merges.
