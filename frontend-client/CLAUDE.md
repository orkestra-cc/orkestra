# Frontend Client — Tier-2 External Client SPA

_Path: `/frontend-client`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [Operator console](../frontend-admin/CLAUDE.md)

The customer-facing SPA — sibling to the operator console at [`../frontend-admin`](../frontend-admin/CLAUDE.md), with a separate origin, cookie domain, design system, and data layer. External (Tier-2) tenants register, manage their account (profile, security/MFA, billing identity), and log in here.

> **ADR-0006:** this SPA is a **thin auth/account demo**. The catalog → subscribe → Stripe-checkout → transactions → payment-methods flows left with the `subscriptions`/`payments` addons; only login + account + billing-identity remain. The sections describing subscribe/Stripe are retained as a record of how a fork rebuilds that layer; they are headed **Fork reference**, and nothing under them exists in this tree.

This app **does not** render anything operator-specific. Internal admin pages live in `../frontend-admin`. If a feature targets internal staff, it does not belong here.

## Tier model recap

This SPA only ever speaks to the **client** API audience (`client.localhost:3000` in dev, `api.orkestra.com` in prod, JWT `aud=client`). The split is enforced server-side by ADR-0003 PR-D D-8: cross-audience tokens get rejected with 401 `audience_mismatch`, and the client refresh cookie is host-only (empty `Domain` unless `CLIENT_COOKIE_DOMAIN` is set) so a session minted here cannot be replayed on `console.*`. **In dev the client API deliberately runs on the SPA's own hostname** — `client.localhost:3000`, not `api.localhost:3000` — because every client-tier cookie is `SameSite=Lax` and, `localhost` being absent from the Public Suffix List, `client.localhost` and `api.localhost` are different _sites_ to a browser; ports are not part of a site, so `:8081` → `:3000` is same-site and still cross-origin. Staging/prod keep the ADR-0003 three-host split because there the hosts share a registrable domain. See [`../docs/site/architecture/authentication-flow.mdx`](../docs/site/architecture/authentication-flow.mdx) for the wire-level walkthrough.

## Tech stack

| Layer           | Choice                                                                                                                                                                      |
| --------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Framework       | React 19, React Router v8 (single `react-router` package — `react-router-dom` no longer exists)                                                                             |
| Build           | Vite 7                                                                                                                                                                      |
| Language        | TypeScript 5.9 strict mode                                                                                                                                                  |
| Styling         | Tailwind v4 (zero-config, design tokens in `src/index.css`) — **not** Bootstrap/Falcon                                                                                      |
| Server state    | TanStack Query v5 — **not** RTK Query (the operator console uses RTK Query; this app intentionally diverges)                                                                |
| Client state    | React state + module-scoped stores (no Redux)                                                                                                                               |
| HTTP client     | Hand-typed `fetch` wrappers in `src/api/*`, every authenticated call through the one `src/api/authedFetch.ts` helper; `openapi-fetch` is a dependency **nothing imports**   |
| OpenAPI codegen | `openapi-typescript` against `${VITE_API_BASE}/openapi.json`                                                                                                                |
| i18n            | `react-i18next` (Italian default, English fallback) — wired from day 1                                                                                                      |
| Auth            | In-memory access token + httpOnly refresh cookie (host-only on the API host — no `Domain` attribute by default; `SameSite=Lax`, so the API must be same-site with this SPA) |
| Tests           | Vitest 4 + React Testing Library + MSW 2 on happy-dom — `npm test`; an unhandled request fails the run                                                                      |
| Payments        | _(none in the base — the Stripe Checkout flow left with the addons. `@stripe/stripe-js` stays in `package.json` unimported, for the fork chain)_                            |

## Directory layout

```
frontend-client/
├── src/
│   ├── App.tsx                 # Router — flat route table, no backend-driven nav
│   ├── main.tsx                # Entry: providers (QueryClient, AuthProvider, i18n) + render
│   ├── i18n.ts                 # i18next bootstrap (IT default, EN fallback, lang detector)
│   ├── index.css               # Tailwind v4 entry + @theme overrides
│   ├── vite-env.d.ts
│   ├── api/
│   │   ├── client.ts           # apiBaseURL — the base-URL resolver, and nothing else
│   │   ├── authedFetch.ts      # THE authenticated request path + the only 401 recovery
│   │   ├── openapi.gen.ts      # Generated by `npm run codegen` (committed for CI; nothing imports it)
│   │   ├── auth.ts             # /v1/auth/client/{register,login,me,...,mfa/...,password recovery} + jsonFetch, the anonymous path
│   │   ├── avatar.ts           # /v1/me/avatar/{presign-upload,commit,source} self-service
│   │   ├── verifyEmail.ts      # /v1/auth/client/verify-email{,/resend}
│   │   ├── dsr.ts              # /v1/me/dsr/{export,erasure-request} — GDPR Art. 15 / 17 self-service
│   │   └── billingProfile.ts   # /v1/me/billing-identity (personal-tenant billing identity)
│   ├── auth/
│   │   ├── AuthProvider.tsx    # React context: in-memory access token + signIn/signOut
│   │   ├── authContext.ts      # Context shape
│   │   ├── useAuth.ts          # Context hook
│   │   ├── useMe.ts            # /me TanStack Query wrapper, gated on isAuthenticated
│   │   ├── tokenStore.ts       # Module-scoped token **and the moment it expires** + coalesced refresh
│   │   ├── sessionMarker.ts    # localStorage hint — "we have a refresh cookie, try refreshing"
│   │   └── RequireAuth.tsx     # Route guard: waits for the bootstrap, then the ?next= round-trip
│   ├── components/
│   │   ├── Layout.tsx          # App shell — header (logo + nav + lang switcher), main, footer
│   │   ├── LanguageSwitcher.tsx
│   │   ├── MfaChallenge.tsx        # TOTP / backup-code step shared by LoginPage and OAuthCallbackPage (challenge id in caller state only)
│   │   └── UserAvatar.tsx      # Renders user avatar (image URL or initials over deterministic color)
│   ├── lib/
│   │   ├── avatarColor.ts      # Deterministic per-user color + initials helper for UserAvatar fallback
│   │   ├── oauthProviders.ts   # the closed provider union (google|apple|github|discord) + display labels
│   │   ├── safeNext.ts         # sanitizeNext — the ONE open-redirect gate; auth routes judged on the decoded, normalised path
│   │   ├── oauthReturnTo.ts    # ten-minute take-and-delete `next` record across the IdP round-trip
│   │   ├── oauthCallback.ts    # closed exact-key parser for /auth/callback + the error-code allowlist
│   │   └── format.ts           # Intl currency + date helpers
│   ├── locales/                # it.json (default), en.json — react-i18next bundles
│   ├── test/
│   │   ├── setup.ts            # jest-dom, EN copy, MSW lifecycle (unhandled request = error), explicit RTL cleanup, spy restore, global-stub + storage + token reset
│   │   ├── server.ts           # the one MSW server
│   │   ├── handlers.ts         # url() + reusable stubs (clientPolicyHandler, providersHandler, …)
│   │   ├── render.tsx          # renderWithProviders (QueryClient + AuthProvider + MemoryRouter), waitForQuerySettled
│   │   └── webStorage.ts       # restores localStorage/sessionStorage under Node ≥ 25
│   └── pages/                  # Routed views — one file per route, no nested folders
├── README.md                   # User-facing project intro + dev quickstart
├── Dockerfile                  # Multi-stage: node:24-alpine builder → nginx:alpine
├── eslint.config.js
├── package.json
├── vite.config.ts              # Path alias `@/*` → `src/*`
└── tsconfig.{,app,node}.json
```

## How auth works

Five moving parts:

1. **In-memory access token** — `src/auth/tokenStore.ts` holds the RS256 JWT in a module-scoped variable, **together with the moment it expires**. Never localStorage, never sessionStorage. The expiry is derived from the `expiresIn` **duration** the server reported at receipt, not from the token's absolute `exp`: both ends of the eventual comparison then come from the same clock, so a badly set client clock cancels out instead of reopening a broken window every TTL cycle. Every path that installs a token must pass the lifetime alongside it (`setAccessToken(token, expiresInSeconds)`); a dropped one records an **unknown** expiry, which reads as "live" and silently disables the 401 recovery. A response without `expiresIn` falls back to `src/lib/jwtExp.ts` (no signature verification — a scheduling hint, never a security decision), and an unreadable one leaves the expiry unknown. Read the pair through `getAccessTokenSnapshot()`, never the two separately. The token is read synchronously by `authedFetch` (`src/api/authedFetch.ts`) on every authenticated call, so the React tree is never in the fetch path. That helper is the **single** authenticated path — `auth.ts`, `avatar.ts`, `billingProfile.ts` and `dsr.ts` all route through it — and it is the only reader of the store in the request path: the openapi-fetch client in `client.ts` that used to carry a second one was deleted along with both its middlewares (#325).
2. **httpOnly refresh cookie** — set by the backend at login on the client API host, **host-only**: `CLIENT_COOKIE_DOMAIN` defaults to empty (`config.go:467-474`, since `bdcbb7ab`), so no `Domain` attribute is written and the cookie is scoped to whatever host minted it. It is `HttpOnly; SameSite=Lax` (`password_handler.go:411-424`, `utils/http.go:53-64`) — which is why the client API must be **same-site** with this SPA (`client.localhost:3000` in dev): a cross-site response cannot even store it. The SPA cannot read it directly; it only triggers `POST /v1/auth/client/refresh-cookie`, which mints a fresh access token. Per ADR-0003 PR-D D-9 the operator host (`console.*`) and the client API host get distinct cookies — a token minted here cannot refresh on the operator console and vice versa.
3. **Session marker** — a tiny `client.session=1` localStorage flag stamped on `signIn` and cleared on `signOut`/401. `refreshAccessToken` short-circuits when the marker is missing so anonymous visitors don't fire a guaranteed-401 on every cold load. `bootstrapFromRefreshCookie()` (on the auth context, implemented in `tokenStore.ts`) is the one place that stamps the marker _speculatively_: the OAuth callback page calls it to adopt the cookie the client-tier relay set on the API host, and it presents the cookie **whether or not the stamp succeeded** — a storage that throws must not turn a valid cookie into a sign-out. Both it and the automatic `refreshAccessToken` go through one unconditional `performRefresh`, serialised across tabs by a Web Lock (`orkestra:auth-refresh`) and bounded by a 10 s `AbortController` timeout **per fetch** (so the lock is held for at most two, the 409 retry being a second attempt inside it): `ok` installs the memory-only token; **only a 401** clears marker and token; everything else — a 503, a 429, a twice-raced 409, any other non-2xx, a 2xx with no token, a transport failure, the timeout — is `unavailable` and keeps both so the caller can retry. Neither function rejects; see [Refresh choreography](#refresh-choreography) for the full table.
4. **Public policy + OAuth start** — `fetchAuthPolicy()` (`GET /v1/auth/client/policy`) falls open on failure, and `passwordLoginUsable(policy)` is the **only** reader of `passwordLoginEnabled`: `undefined` (still loading) reads as usable, `false` **and** `null` read as off, so an SSO-only client surface hides the password UI instead of showing a form the backend refuses with 403 (spec §4.10, G5). `fetchOAuthProviders()` deliberately does **not** fall open — a 503, a network error or a body without a `providers` array is a retryable error state, never "no method"; only `{providers: []}` is empty. `initiateOAuthLogin(provider, next)` POSTs the allowlisted provider **with `credentials:'include'`** (the response sets the HttpOnly `orkestra_oauth_state` cookie the relay endpoint requires), stashes the validated `next` and leaves through `browserNavigation.assign` — the seam tests spy on. On the login page this means: nothing paints until `/policy` resolves; with the method on, the password form renders above an "or continue with" provider section; with it off (`false` or `null`) only the providers render, the forgot/sign-up links disappear, and a provider list that _resolved_ empty shows the no-sign-in-method notice — a provider-query error (503, network, malformed body) is a retryable alert, never that notice. The kill switch (`loginEnabled=false`) keeps the maintenance banner and hides the provider section.
5. **Bootstrap readiness** — on a cold load `token === null` means two different things, "signed out" and "not decided yet", and the difference is a _fetch_ wide: the mount refresh in part 3 starts in a passive effect, i.e. strictly after the first commit. `AuthProvider` therefore publishes **`isBootstrapping`** on the auth context (`src/auth/authContext.ts`) — `true` from mount until that one-shot refresh has **settled** (`ok`, `signed-out`, `unavailable`, _and_ the marker-less short-circuit that never leaves), then false for the life of the provider; `signIn`/`signOut` do not touch it. **`RequireAuth` renders nothing while it is true** and only judges the session afterwards — unauthenticated → a `<Navigate replace>` to `/login?next=` + `encodeURIComponent(pathname + search)`, authenticated → the children. A synchronous guard redirected on the first commit, before the refresh had even left, and nothing navigated back: every reload / deep link / bookmark of `/account*` showed a returning user a login form under a signed-in header (spec §8 #11). **`LoginPage` closes the same loop from the other side**: an authenticated, settled visitor is forwarded to `sanitizeNext(?next=) ?? /account` — the very `destination` its own post-sign-in `complete()` navigates to, so a bookmarked `/login`, the guard's `?next=` round-trip and a password sign-in cannot drift apart. Any new consumer that branches on `isAuthenticated === false` owes the same wait.

### Refresh choreography

A refresh happens in exactly **three** places, and the third one **is** a mid-session 401 — the recovery landed with #325, so a reader who remembers "this SPA does not refresh mid-session" is remembering the old behaviour:

```
AuthProvider mounts   → tokenStore.refreshAccessToken(apiBaseURL)   # one-shot, no-op without the session marker
/auth/callback        → bootstrapFromRefreshCookie()                # speculative: stamps the marker, then presents the cookie
authedFetch 401       → refreshAfterUnauthorized(apiBaseURL)        # the mid-session recovery: only on proof the handler
                                                                    # never ran; marker gate skipped; retries once
        → POST /v1/auth/client/refresh-cookie   (serialised across tabs by a Web Lock;
                                                 10s per fetch; one 409 retry)
        → ok           → in-memory access token + expiry installed
        → signed-out   → marker + token cleared   (401 ONLY)
        → unavailable  → both kept, caller may retry
```

> **`signed-out` is an allowlist of exactly one status.** Only a **401** clears anything — it is the one answer that means "the credential I presented was rejected". A **429** (the endpoint sits under the router's global rate limiter, and a burst of tabs rotating together is what trips it), a 408, any 5xx, any other 4xx, a 2xx with no token, a transport failure and the 10 s timeout are all `unavailable`: they say something about the server and nothing about the session, so the token _and_ the marker survive. A denylist here is how a Mongo blip became a logout. A **409 `refresh_rotation_raced`** is retried exactly once — the sibling that won the CAS left the successor cookie in the jar — and a second 409 is `unavailable`, never a sign-out.
>
> The Web Lock is deliberately **not** bounded with an `AbortSignal`; it is bounded _transitively_, because everything done while holding it happens inside a fetch timeout. The bound is **10 s per fetch**, and the lock is therefore held for at most **two** of them (2 × `REFRESH_FETCH_TIMEOUT_MS` = 20 s) — the single 409 retry is a second full attempt inside the same lock. The timer spans the fetch, the classification **and the body read** — `fetch` resolves on headers, so a `clearTimeout` placed right after the `await` would leave a stalled body holding the lock indefinitely. (The read is raced against the abort signal explicitly: a mocked or proxied body stream does not always observe the request's signal, and the bound must not depend on it.) Do not weaken the timeout: it is what makes the lock safe.
>
> A `navigator.locks.request` that **rejects** — the document is not fully active, an implementation that throws — is `unavailable` as well, and is never propagated: `AuthProvider` calls `void refreshAccessToken(...)` on mount, so a rejection escaping `performRefresh` would be an unhandled rejection. The catch is scoped to the acquisition; a throw from inside the lock callback still propagates.

All **three** entry points above go through one coalesced `performRefresh`, so concurrent callers share a single in-flight promise, and none of the three rejects. The in-flight promise **wraps** the lock, so a second caller in the same tab shares the first one's answer instead of queueing behind the lock.

The third of them, `refreshAfterUnauthorized(apiBase)`, is the authenticated-retry path, and as of this branch it is **wired**. It deliberately **skips the marker gate** and goes straight through `performRefresh`, so — unlike `refreshAccessToken`, whose gate returns `signed-out` while clearing nothing — every `signed-out` it yields clears **both** the token and the marker (G3), and an `ok` repairs a marker that was missing. It is for a 401 that answered a request which actually carried a bearer: a bearer in memory is proof a session existed, so the anonymous-visitor optimisation has no business vetoing a cookie that may still be valid. It too never rejects. Its one caller is `src/api/authedFetch.ts` — the one authenticated request path and, now that every `src/api/*` wrapper routes through it, the only 401 algorithm in this tree.

**An expired access token on an authenticated call recovers silently.**
Every authenticated request goes through `src/api/authedFetch.ts`, which
attaches the bearer, sets `credentials:'include'`, and on a **401** decides
in this order:

| #   | Condition                                                                                                                                       | Action                                                                                                             |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| 1   | the body carries a **terminal** top-level `code` (`session_revoked`, `session_max_age_reached`)                                                 | clear token **and** marker; no refresh, no retry — a token minted from the same cookie carries the same dead `sid` |
| 2   | **no proof the handler never ran** — neither `code: access_token_expired` from the server nor a token that was already expired when it was sent | return the 401 **unchanged**: no refresh, no replay                                                                |
| 3   | the store now holds a **different** token                                                                                                       | a sibling already rotated — retry once with it, no refresh                                                         |
| 4   | otherwise                                                                                                                                       | refresh (un-gated when a bearer was sent, marker-gated when none was) and retry **once**                           |

**Branch 2 is the replay guard and it sits ahead of every recovery branch.**
`change-password` is an authenticated endpoint that answers **401** when the
_current password in the body_ is wrong, and the backend counts the failed
attempt: a blanket "401 → refresh → retry" re-sends it, so two mistypes trip
the lockout as though there had been four. Recovery is therefore permitted
only on proof the request never reached its handler — the server's own
`access_token_expired` code, or a token that was **already expired at send**.
There is deliberately **no margin**: a token with 20 s of life left is still
accepted by the server, so the handler ran. An **unknown** expiry counts as
live for the same reason. Any future authenticated endpoint that answers 401
for a body credential inherits the protection without being listed anywhere.

At most **one** retry per call. The retry's own 401 is inspected for terminal
codes only — a codeless 401 there stays ambiguous, and clearing on it would
sign out a user whose session is fine because they mistyped a password.
Every body inspection reads a `res.clone()`: the caller's `readError`
swallows the `TypeError` from a consumed body, so reading the original would
degrade _silently_ into "fallback message, no code".

`jsonFetch` (the **anonymous** path in `auth.ts`) is deliberately untouched —
a 401 from `login`, `register`, `forgot-password`, `policy` or `providers`
means "those credentials are wrong", never "the token expired".

A 503 or a network failure during a refresh is `unavailable` — as is everything else in the table above except a 401: the token is kept, because nothing is known about the session (ADR-0017). The `?next=` parameter on the login redirect carries the originally requested path so post-login the user lands where they were headed.

### Fork reference: reading tenant memberships from the JWT

`src/auth/memberships.ts` and `useOwnedTenants()` **do not exist in the base** — they left with the subscribe flow. The technique is recorded because a fork rebuilding an owner-scoped feature needs it.

The client API surface deliberately **does not** expose `/v1/tenants` or `/v1/me/tenants` — that endpoint is mounted on the operator router only. To know which tenant the caller owns, decode the access token's `mbr` claim:

```
{ mbr: [ { tid: "uuid", k: "external", r: ["org_owner"] }, ... ] }
```

Compact keys come from `backend/internal/core/auth/services/jwt_service.go::claimsToMap`. Ownership is inferred from `r.includes("org_owner")` since the JWT does not carry an `isOwner` boolean. The backend re-validates ownership against `TenantProvider.ListUserMemberships` on every `/v1/me/*` call, so a client-side filter is a UX hint, **not** a security gate. Subscribe to the token store with `useSyncExternalStore` so the derived list re-renders when the token rotates (login, refresh, logout).

### OAuth login (web)

`LoginPage` lists the providers the backend currently accepts (`GET /v1/auth/client/providers` — toggle on **and** structurally configured; a 503, a network failure or a malformed body is a retryable error state, never "no method") and starts a flow with `initiateOAuthLogin(provider, next)`: `POST /v1/auth/client/oauth/login {provider}` **with `credentials:'include'`** — the response sets the HttpOnly `orkestra_oauth_state` cookie on the API host and the relay endpoint later _requires_ it — then stashes the validated `next` (`lib/oauthReturnTo.ts`, a ten-minute record) and leaves for `authUrl`. Every provider redirects to the **operator** host, which cannot set a cookie for `api.*`, so the backend relays the client-tier outcome to `GET {CLIENT_API_URL}/v1/auth/client/oauth/complete?relay=<id>`; that endpoint verifies the browser binding against the state cookie, sets the client refresh cookie on its own host and redirects to `{CLIENT_FRONTEND_URL}/auth/callback` under a **closed contract** — `?success=true&provider=<p>`, `?success=false&error=<allowlisted code>`, or `#requiresMfa=true&mfaToken=<id>&webauthnAvailable=<bool>`; never a token, an email or a user id.

`pages/OAuthCallbackPage.tsx` parses that URL once with `lib/oauthCallback.ts` (exact key sets — anything else is the generic failure), scrubs it in its first passive effect **before any request**, take-and-deletes the return target on every outcome, and then: success → `bootstrapFromRefreshCookie()` and navigate to the target or `/account` only once a token exists (signed-out is a login error; a 503 or a network failure offers retry); MFA → the same `components/MfaChallenge.tsx` the password path uses, challenge id in component memory only (`webauthnAvailable` is parsed but the client SPA has no WebAuthn login, so the TOTP / backup-code form renders — a passkey-only user cannot complete an OAuth-MFA continuation here yet); error → the mapped `oauth.callback.errors.*` copy. Raw URL text is never rendered. `src/App.test.tsx` mounts this page through the real route table so the shell's own queries are proven not to disturb the order scrub → bootstrap.

## How data fetching works

All server state goes through **TanStack Query v5**, not RTK Query. (The operator console at `../frontend-admin` uses RTK Query; this app intentionally diverges because the surface is small enough that the Redux infrastructure is not worth it.)

```
QueryClient (created once in main.tsx — retry: 1, staleTime: 30s)
  → useQuery / useMutation in pages
    → hand-typed wrappers in src/api/* (auth, avatar, billingProfile, dsr)
      → authedFetch (src/api/authedFetch.ts) — the in-memory bearer,
        credentials:'include', and the one 401 recovery
        (jsonFetch, in auth.ts, for the anonymous endpoints)
```

Conventions:

- **Cache keys are flat tuples**: `['me']`, `['authPolicy']`, `['oauthProviders']`, `['mfa-status']`, `['billing-profile']`. Add a discriminator (`['thing', id]`) only when the query is per-resource. Keep them stable.
- **`staleTime` is a global 30s default** set on the `QueryClient` in `main.tsx` — it is _not_ opt-in. Pass a per-query `staleTime` when a view must always be fresh; the test harness (`src/test/render.tsx`) uses `staleTime: 0` and `retry: false` so tests never serve a cached answer.
- **`enabled` gates auth-only queries**: `useMe` checks `isAuthenticated` before firing.
- **Mutations call query invalidation explicitly**: there is no global tag system like RTK Query — invalidate `queryClient` keys by hand after a successful mutation.

There is **no axios**. Every endpoint today is a hand-typed wrapper in `src/api/<feature>.ts`: authenticated calls go through `authedFetch` (`src/api/authedFetch.ts`), anonymous ones through `jsonFetch` (`auth.ts`). `openapi-fetch` is still a dependency and `openapi.gen.ts` is still generated, but **nothing imports either** — the typed client that eventually consumes them must _delegate_ to `authedFetch`'s policy rather than restate it, because a second restatement is precisely what #325 deleted. Both paths share the same `apiBaseURL` constant — mirror `billingProfile.ts` for a simple read/write pair, `auth.ts` for a multi-flow module.

## How navigation works

**Flat React Router table in `src/App.tsx`.** Unlike the operator console, this app does NOT consume `/v1/navigation` — there is no dynamic sidebar, no module catalog, no role-based menu rebuild. The route surface is small (home, signup, login, account/{profile,security,billing}, …) and each route is mounted explicitly.

Auth-gated routes wrap their element in `<RequireAuth>` — which renders nothing until the auth bootstrap has settled, and only then either the children or a `<Navigate>` to `/login?next=<pathname+search>` (see [How auth works](#how-auth-works)). Anonymous routes mount directly under `<Layout>`.

Anonymous entry points are policy-aware: the header's Sign-up CTA, `/signup` and `/forgot-password` hide their password forms behind a notice when `passwordLoginUsable(policy)` is false for the client surface (the backend refuses those routes with 403 anyway), and the CTA also stays hidden when `registrationEnabled` is off. When the policy read fell open, the forgot-password form still renders the backend's answer mapped by code (`forgot.passwordDisabled`, `error.policyUnavailable`, `error.generic`) — never the raw detail. `/reset-password` and `/accept-invite` stay open — the backend keeps those routes open too (spec §4.3).

When you add a new page:

1. Create the page in `src/pages/MyPage.tsx`.
2. Import it in `src/App.tsx` and add a `<Route>` (wrap in `<RequireAuth>` if it needs auth).
3. Add navigation links to `src/components/Layout.tsx` only if the page should appear in the header — most pages are reached by deep link, not navbar.
4. Add new strings to **both** `src/locales/en.json` and `src/locales/it.json` (see i18n section).

## How i18n works

`react-i18next` is wired in `src/i18n.ts` with `i18next-browser-languagedetector`. Italian is the default + fallback, English is the only other locale today.

- **Every user-visible string** comes from `t('key')`. No raw English in JSX.
- **Locale files**: `src/locales/{en,it}.json`. Add new keys to **both** files in the same PR — there is no missing-key fallback to "show the key" in production builds.
- **Language switcher**: `src/components/LanguageSwitcher.tsx`. Persists choice via the language detector's localStorage cache.
- **Currency / dates**: use `src/lib/format.ts::formatPrice` for money, `Intl.DateTimeFormat` for dates. Don't hand-format with `${amount}€`.
- **Addons (ADR-0007)**: this SPA has no module/manifest system today, so all strings live in the core `src/locales/{en,it}.json`. When/if a fork gives the client an addon seam (as `frontend-admin` has), addon strings must follow [ADR-0007](../docs/adr/0007-per-addon-i18n-namespaces.md): a per-addon i18next namespace registered via `i18n.addResourceBundle(lng, '<name>', bundle)`, never appended to the core locale files. Mirror `frontend-admin/src/modules/useModuleI18n.ts` when that day comes.

## Fork reference: Stripe Checkout

> **None of this is in the base.** The endpoints, pages and `src/lib/stripe.ts` left with the `subscriptions`/`payments` addons under ADR-0006. What follows is the design record a fork rebuilding the layer should follow — written in the present tense as it was when it shipped.

The flow used **hosted Stripe Checkout** — the backend opens a session via `POST /v1/me/payments/{,setup-}checkout-session`, returns a URL, and the SPA redirects via `window.location.href`. **No Stripe Elements**, **no PaymentIntent choreography on the SPA**. The publishable key (`VITE_STRIPE_PUBLISHABLE_KEY`, still plumbed through `/config.js` but read by nothing in the base) was therefore not required to subscribe; it was reserved in `src/lib/stripe.ts` for a future Elements/Embedded Checkout migration.

Two modes are available:

| Mode        | Endpoint                                      | When to use                                                                                                                                                                                                                        |
| ----------- | --------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **setup**   | `POST /v1/me/payments/setup-checkout-session` | Cold subscribe — user picks a tier, we create the subscription (status `active`, NextBillingAt now), open setup-mode Checkout to save the card. The renewal job (~1h cadence) generates the first invoice and charges off-session. |
| **payment** | `POST /v1/me/payments/checkout-session`       | Pay an already-pending invoice from the subscription detail page. Returns 409 if no pending invoice exists for the subscription — the planner does **not** generate one on demand.                                                 |

Return URL: `/subscribe/return?sub={uuid}&result=success|cancel`. Stripe is told the `success_url` and `cancel_url` at session-creation time; the SPA reads `result` from the query string to render the right state. There is no polling needed — the subscription is `active` at creation and entitlements are granted synchronously by the entitlement syncer.

If you add a third Stripe flow (refunds UI, customer portal redirect, etc.), follow the same pattern: backend creates the session, frontend redirects via `window.location.href`. Never embed Stripe Elements without explicit sign-off — the MVP design call is hosted-only to minimize PCI scope.

### Fork reference: owner & billing-profile gate

`SubscribePage` and `useOwnedTenants()` are part of the removed layer; `GET /v1/me/billing-identity` and `BillingProfilePage` are the parts that **do** survive in the base. Subscribes were polymorphic: `SubscribePage` defaults to **personal** (`ownerKind:"user"` — the calling user is the owner) and only renders an organization picker when `useOwnedTenants()` returns rows. Personal subscribes pre-flight `GET /v1/me/billing-identity` (Phase 6 of the Unified Client Aggregate refactor — the predecessor `/v1/me/billing-profile` and the `clientbilling_customers` collection are gone, replaced by a per-user personal Tenant aggregate that the backend lazy-provisions via `EnsureTenantForUser`). The SPA bounces to `/account/billing?next=/subscribe?...` if `hasBillingProfile()` reports the identity is missing (no `billingAddress.country`, or `isCompany=true` without `legalName`); the same redirect runs as a fallback when a post-create checkout-session call still 409s. Tenant-owner subscribes reuse the existing `tenant.StripeCustomerID` seam and do not need the billing-identity form.

## Path aliases

Vite + tsconfig declare a single alias: `@/*` → `src/*`. Use it everywhere:

```ts
import { useAuth } from "@/auth/useAuth";
import { getBillingProfile } from "@/api/billingProfile";
import { Layout } from "@/components/Layout";
```

This is **different from the operator console** at `../frontend-admin`, which uses bare aliases (`components/`, `pages/`, …). Don't mix the two styles.

## Build & dev

The SPA runs in a Docker container with Vite hot reload (service `client-frontend` in `docker-compose.dev.yml`; container name is stack-namespaced, `${APP_NAME}-client-frontend-${ENV}`). The host hits it at `http://client.localhost:8081`, and it calls the client API at `http://client.localhost:3000` — same hostname, one port over, for the same-site reason above. See [`README.md`](README.md) for the full quickstart (hosts file, env vars, infra dependencies).

Local commands you'll actually run from your editor:

```bash
npm run typecheck          # tsc -b --noEmit — CI-safe, run before pushing
npm test                   # vitest run — happy-dom + MSW; CI runs it between lint and build
npm run test:watch
npm run lint               # eslint src --ext .ts,.tsx
npm run build              # tsc -b && vite build (production bundle)
npm run codegen            # openapi-typescript against $VITE_API_BASE/openapi.json
```

Re-run `npm run codegen` whenever you add or change a backend route — `src/api/openapi.gen.ts` is committed so CI builds without a live backend, but it will drift if you don't regenerate.

The Vite dev server runs inside Docker; if you need to rebuild outside Docker (e.g. for local typechecking), `npm install` is fine — the dependency tree is small and the container's `node_modules` is volume-isolated.

### Runtime config

`index.html` loads `public/config.js` (`window.__ORKESTRA_CONFIG__` — `apiBase`, plus a `stripePublishableKey` slot the base declares in the `Window` type but never reads) before the bundle; `src/api/client.ts` consults it first and uses the build-time `VITE_API_BASE` only as a fallback for environments that don't set it (Vitest, scratch SSR). `public/config.js` is **gitignored** — each environment writes its own:

- **Dev / staging (Vite in Docker)**: the `client-frontend` service `command` in `docker-compose.{dev,staging}.yml` regenerates the file from `VITE_API_BASE` / `VITE_STRIPE_PUBLISHABLE_KEY` at container start. It is written **only at startup** — after any git operation that deletes it under the bind mount (checkout, merge), recreate the container; otherwise Vite serves `index.html` for `/config.js` and the browser blocks it (`nosniff`).
- **Prod (nginx image)**: the entrypoint at `/docker-entrypoint.d/10-write-config.sh` (baked in the `Dockerfile`) writes it from `ORKESTRA_API_BASE` / `ORKESTRA_STRIPE_PUBLISHABLE_KEY` before nginx starts.
- **Bare `npm run dev` on the host**: no generator runs — copy the tracked template `public/config.example.js` to `public/config.js`.

**`/config.js` is served `no-store` on both paths, and that is load-bearing.** It is rewritten on every container start, so a client holding a stale copy points at the wrong `apiBase` until its cache expires. Prod nginx sets the header in `Dockerfile` (`location = /config.js`); the dev server — dev **and** staging — gets it from the `orkestra-client-runtime-config` plugin in `vite.config.ts`, which serves the file itself rather than letting Vite's public-dir middleware answer. Two reasons, both non-obvious:

- Vite's public-dir middleware (sirv) writes `Cache-Control` **unconditionally into the response head**, so a middleware that only calls `res.setHeader` upstream of it is silently overridden. Short-circuiting is the only way to own the header — the same shape `healthCheckPlugin` uses.
- `no-cache`, which is what sirv sends by default, is not "don't cache" but "cache and revalidate" — enough for a CDN to keep a copy. Cloudflare classifies `.js` as a static asset **by extension** and replaces the origin header with its own default `max-age`; the operator console served a runtime config four hours stale after a deploy because of exactly this ([`frontend-admin/CLAUDE.md`](../frontend-admin/CLAUDE.md), Runtime config).

The plugin also makes the missing-file case legible: when `config.js` is absent it logs `[orkestra-client-runtime-config] /config.js not served from disk` and falls through to the old behaviour, so the `nosniff` block above announces its cause instead of being a puzzle. If a middleware edit to `vite.config.ts` looks inert, restart the container: Vite's own config reload logs `server restarted` but cannot be relied on to replace a middleware a previous `configureServer` registered (it did pick this plugin up here, and did not on the operator console).

## Adding a feature — canonical workflow

1. **Backend first**. If the feature needs a new endpoint, add it to the relevant backend module (a core module, or a fork's `backend/internal/addons/<module>/`), declare the route on `ri.Client.ProtectedRouter` if it's a Tier-2 self-service endpoint, ship the backend PR.
2. **Re-run codegen**: `npm run codegen` against the running backend so `src/api/openapi.gen.ts` picks up the new operation.
3. **Add an API wrapper** in `src/api/<feature>.ts`, hand-typed, calling `authedFetch` for anything that carries a bearer — a raw `fetch` silently opts the endpoint out of the 401 recovery. Mirror the file closest to your shape (`billingProfile.ts` for self-service reads/writes, `auth.ts` for the heavy multi-flow modules).
4. **Add the page** in `src/pages/<Name>Page.tsx`. Co-locate any one-off helpers or components in the same file unless they're reused.
5. **Wire the route** in `src/App.tsx`. Wrap in `<RequireAuth>` if the endpoint requires `aud=client` + a logged-in user.
6. **Add i18n strings** to **both** `src/locales/{en,it}.json`. Lead with the IT translation since IT is the default locale.
7. **Write the test** next to the page (`<Name>Page.test.tsx`) with `renderWithProviders` from `@/test/render`. Stub every endpoint the component mounts (`src/test/handlers.ts` or `server.use(...)`) — MSW runs with `onUnhandledRequest: 'error'`, so a missing stub is a red run. Anchor every absence assertion on a settled positive state first (`waitForQuerySettled(queryClient, key)` when the tree is identical before and after the query lands).
8. **Run** `npm run typecheck && npm run lint && npm test && npm run build` before committing. The build catches type errors that `tsc --noEmit` misses (Vite plugins).
9. **Test in a browser** if it's a UI change — start the dev stack, walk the golden path, check the relevant edge cases. The build passing does **not** mean the feature works.

## Conventions

- **Tailwind v4 utility classes only** — no inline `style={{ ... }}` for colors/spacing, no SCSS files, no CSS-in-JS. Custom design tokens go in `src/index.css` `@theme`.
- **One page per route**, named `<Thing>Page.tsx`. No nested folders under `pages/` for now — the surface is small enough.
- **`credentials: 'include'`** on every call that needs the session cookie. `authedFetch` (`src/api/authedFetch.ts`) forces it — a caller that passes `'omit'` gets `'include'` anyway — and `jsonFetch` (`src/api/auth.ts`) sets it for the anonymous endpoints; a raw `fetch` must set it itself. Two deliberate exceptions, and both are why those two calls stay off `authedFetch`: `verifyEmail.ts` talks to anonymous endpoints and sends no cookie, and `avatar.ts`'s `putAvatarBlob` PUTs to the presigned object-store URL with `credentials: 'omit'` so nothing of ours leaks to a foreign origin.
- **Never persist the access token to storage**. In-memory only. The session marker in localStorage is _just a hint_ that a refresh cookie probably exists.
- **Co-locate sub-components** next to the page that uses them. Promote to `src/components/` only on second use.
- **Keep `src/components/` small**. The operator console has dozens of shared primitives because it's a 50-page admin app. This SPA has ~12 routes — most "shared" UI is one inline `<Field>` away from being co-located instead.

## Don't

- **Don't import RTK Query, redux, or redux-persist.** TanStack Query is the only server-state library. Adding Redux is a breaking architectural change — discuss first.
- **Don't import Bootstrap, Falcon, react-bootstrap, or copy components from `../frontend-admin`.** This SPA's design language is intentionally distinct from the operator console (locked decision in the MVP plan). Tailwind utilities + a fresh aesthetic.
- **Don't hit operator-tier endpoints** (`/v1/tenants`, `/v1/admin/*`, `/v1/auth/operator/*`, etc.). They're not mounted on the client API surface and will 404. If you need data they expose, add a `/v1/me/*` mirror in the backend instead.
- **Don't store the access token in localStorage / sessionStorage.** In-memory is the policy. The session marker is the only thing that goes to localStorage and it's not a credential.
- **Don't bypass `RequireAuth`** by checking `getAccessToken()` in components. Use the auth context — it re-renders on token rotation.
- **Don't add Stripe Elements without explicit sign-off.** Hosted Checkout is the locked decision; Elements would re-introduce PCI scope.
- **Don't ship English-only strings.** Every user-visible string goes through `t()` with both `en.json` and `it.json` entries in the same PR.
- **Don't share storage / cookies / auth state with `../frontend-admin`.** Cookie domains, JWT audiences, and refresh cookies are deliberately split per ADR-0003 PR-D D-8/D-9.
- **Don't build, parse or trust an `/auth/callback` URL outside `src/lib/oauthCallback.ts`**, don't render raw callback text, and don't put the MFA challenge id in router state or storage — it lives in the callback page's component memory only.
- **Don't navigate to a `?next=` value without `sanitizeNext`** (`src/lib/safeNext.ts`) — it is the SPA's only open-redirect gate, for the password path and the OAuth return target alike.

## Current surface (ADR-0006)

The base SPA is a **thin auth/account demo**: anonymous home + signup + email verify, login with email/password **and web OAuth** (Google / Apple / GitHub / Discord through the client-tier relay, `/auth/callback`), password recovery + MFA enrol, account (profile, security, billing identity), accept-invite. The subscribe/transactions/payment-methods dashboard, the service catalog, the owner-scope switcher, and the Stripe checkout flow were removed with the `subscriptions`/`payments` backend addons. The sections above describe how a fork **rebuilds** that layer — they are not present in the base.

## Related

- [Operator console](../frontend-admin/CLAUDE.md) — the Tier-1 admin SPA (different stack, different audience, different cookie domain)
- [Backend auth core](../backend/internal/core/auth/CLAUDE.md) — `/v1/auth/client/*` audience-split routes, JWT claims, refresh-cookie behaviour
- [Backend tenant core](../backend/internal/core/tenant/CLAUDE.md) — ownership model, why `org_owner` is the proxy used here
- [Authentication flow doc](../docs/site/architecture/authentication-flow.mdx) — wire-level walkthrough of the post-PR-D world
- [Docker compose](../docker/CLAUDE.md) — `client-frontend` service / container wiring
