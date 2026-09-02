# orkestra-client

Tier-2 (external client) demo SPA. Consumes the ADR-0003 **client** API surface (`api.orkestra.com` in prod / `client.localhost:3000` in dev, JWT `aud=client`). Sibling to the operator console at [`/frontend-admin`](../frontend-admin) — separate cookie domain, separate OpenAPI surface, distinct visual language.

## Stack

- React 19 + TypeScript 5.9 (strict)
- Vite 7 + Tailwind v4 (zero-config, design tokens in `src/index.css`)
- React Router v8 + TanStack Query v5
- No generated API client: every endpoint is a hand-typed `fetch` wrapper in `src/api/*` — the OpenAPI type generator, its runtime client and the `codegen` script were dropped with [#325](https://github.com/orkestra-cc/orkestra/issues/325) because nothing imported them (spec §8 #4)
- `react-i18next` (Italian + English from day 1)
- `@stripe/stripe-js` — installed but **not imported anywhere in the base**; kept because the fork chain's billing layer builds on it
- Vitest 4 + React Testing Library + MSW (happy-dom) — `npm test`

## Dev quickstart

Both backend and the client SPA run in Docker. Most resolvers answer `*.localhost` themselves; if yours does not, add the **hosts file** entries the dev stack uses:

```
127.0.0.1 console.localhost client.localhost
```

**The SPA and the client API share `client.localhost` on purpose — do not split them onto `api.localhost`.** Every client-tier cookie (refresh, OAuth state, device) is minted `SameSite=Lax` with no `Domain`, and `localhost` is not in the Public Suffix List, so a browser treats `client.localhost` and `api.localhost` as different _sites_: a cross-site `fetch(..., {credentials: "include"})` neither stores nor sends those cookies, and client login succeeds while the very next refresh 401s. A port is not part of a site, so `:8081` (SPA) and `:3000` (API) are same-site while staying cross-origin — the CORS preflight is still on the path. In staging/prod the three-host ADR-0003 split stands, because there the hosts share a registrable domain (`app.orkestra.cc` + `staging-api.orkestra.cc` under `orkestra.cc`). The rule is _same site_, not _same host_.

No payment keys are needed: `docker/.env` still carries a `VITE_STRIPE_PUBLISHABLE_KEY` slot that the compose files pass through into `/config.js`, but no base code reads it — it stays reserved for a fork that rebuilds the billing layer.

Then bring the dev stack up:

```bash
cd docker
docker compose -f docker-compose.infra.yml up -d
docker compose -f docker-compose.dev.yml up -d
```

Open:

- **Demo SPA** — http://client.localhost:8081
- Operator console — http://localhost:8080 (not `console.localhost:8080`: the console's origin has to be same-site with its `VITE_API_URL`, which defaults to `http://localhost:3000`)
- Backend API — http://client.localhost:3000 (client surface) + http://console.localhost:3000 (operator)

## Tests

```bash
cd frontend-client
npm test              # vitest run — what `make ci-frontend-client` runs between lint and build
npm run test:watch
```

MSW runs with `onUnhandledRequest: 'error'`: stub every endpoint a component mounts.

## Typed API client

There isn't one, on purpose. Every endpoint is a hand-typed wrapper in `src/api/<feature>.ts` — authenticated calls through `src/api/authedFetch.ts`, anonymous ones through `jsonFetch` in `src/api/auth.ts`. The OpenAPI type generator, the typed-client runtime that consumed its output, the `codegen` script and the committed types stub under `src/api/` all left with issue #325: nothing imported any of them, so the generated types typed nothing and the dependency's Dependabot bumps were vacuous by construction.

If a typed client is ever wanted it re-adds a pinned dependency in the same PR that writes the middleware, against a freshly generated type rather than a stub — and that middleware must **delegate** to `authedFetch`'s 401 policy rather than restate it. See §8 #3 of [`docs/superpowers/specs/2026-09-01-client-401-recovery-design.md`](../docs/superpowers/specs/2026-09-01-client-401-recovery-design.md); the client #325 deleted is what a restatement looks like.

## Layout

```
src/
├── api/
│   ├── client.ts           # apiBaseURL — the base-URL resolver, and nothing else
│   ├── authedFetch.ts      # THE authenticated request path + the only 401 recovery
│   ├── auth.ts             # register, login, /me, password recovery, MFA, policy, OAuth providers + start; jsonFetch, the anonymous path
│   ├── avatar.ts           # /v1/me/avatar/* self-service (putAvatarBlob stays on raw fetch — presigned, credentials:'omit')
│   ├── billingProfile.ts   # /v1/me/billing-identity
│   ├── dsr.ts              # /v1/me/dsr/{export,erasure-request} — GDPR Art. 15 / 17 self-service
│   └── verifyEmail.ts      # /v1/auth/client/verify-email{,/resend} — anonymous, raw fetch
├── auth/
│   ├── AuthProvider.tsx    # React context (in-memory access token, bootstrapFromRefreshCookie)
│   ├── tokenStore.ts       # module-scoped token; one unconditional, coalesced refresh; cookie bootstrap
│   ├── sessionMarker.ts    # localStorage hint that a refresh cookie probably exists
│   ├── useAuth.ts          # context hook
│   ├── useMe.ts            # /me TanStack Query wrapper
│   └── RequireAuth.tsx     # router guard: waits for the bootstrap, then the ?next= round-trip
├── components/             # Layout shell, language switcher, avatar, MfaChallenge
├── lib/                    # format helpers, avatar colour, safeNext, OAuth return-target + callback parser
├── locales/                # it.json, en.json — react-i18next bundles
├── pages/                  # routed views (LoginPage, OAuthCallbackPage, AccountPage, …)
├── test/                   # Vitest harness: setup, MSW server + handlers, renderWithProviders
├── App.tsx                 # router
├── App.test.tsx            # the OAuth callback through the real route table
├── main.tsx                # entry: providers + render
├── i18n.ts                 # i18next bootstrap (IT default, EN fallback)
└── index.css               # Tailwind v4 entry + @theme overrides
```

## Roadmap (per the MVP plan)

| Phase | Scope                                                                        | Status in the base                      |
| ----- | ---------------------------------------------------------------------------- | --------------------------------------- |
| 1     | Scaffold (Vite + React + auth shell + i18n + Tailwind + dev container)       | ✅ shipped                              |
| 2     | Anonymous signup + email verification                                        | ✅ shipped                              |
| 3     | Login / account / profile / password change / MFA enrol                      | ✅ shipped                              |
| 3b    | Web OAuth login (Google / Apple / GitHub / Discord) through the client relay | ✅ shipped                              |
| 4     | Self-subscribe + Stripe Checkout (setup mode) + return URL                   | ❌ removed by ADR-0006 — fork territory |
| 5     | Subscriptions / transactions / payment-methods dashboard + owner switcher    | ❌ removed by ADR-0006 — fork territory |

Phases 4–5 were built and then removed with the `subscriptions`/`payments` addons — as was phase 2's anonymous **catalog browse**, so the base's anonymous surface is home + signup + email verify. A fork rebuilding that layer can crib from the archived `orkestra-cc/orkestra-addon-{subscriptions,payments}` repos or from this repo's history before the ADR-0006 removal; `CLAUDE.md` keeps the design notes under its "Fork reference" headings.

Web OAuth login for the client tier landed with the password-login toggle work (spec: `docs/superpowers/specs/2026-08-29-password-login-toggle-design.md` §4.10).

## Production build

```bash
docker build -t orkestra-client:staging frontend-client/
```

**One image serves every environment.** `VITE_API_BASE` is no longer a build arg (`ORKESTRA_VERSION` is the only one the `Dockerfile` declares): the nginx entrypoint regenerates `/config.js` from the container's `ORKESTRA_API_BASE` — plus the reserved `ORKESTRA_STRIPE_PUBLISHABLE_KEY` — at start-up, so staging (`app.orkestra.cc`) and prod (`app.orkestra.com`) run the same tag with different env. See "Runtime config" in [CLAUDE.md](CLAUDE.md).

## Backend routes this SPA needs

Everything the base calls is already mounted on the client surface: `/v1/auth/client/*` (register, verify-email, login, OAuth start + relay completion, password recovery, MFA) and the `/v1/me/*` self-service slice (profile, avatar, billing identity, DSR). There is no `backend/internal/addons/` in the base — a fork adding a vertical mounts its own routes on `ri.Client.ProtectedRouter`; see "Adding a feature" in [CLAUDE.md](CLAUDE.md).
