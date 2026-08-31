# Password-Login Toggle — PR 4: Client OAuth Login — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Tier-2 client SPA (`frontend-client`) the web OAuth login it needs to survive `passwordLoginEnabledClient=false` — provider buttons, a closed-contract `/auth/callback` page that adopts the relay-set refresh cookie, a validated `next` round-trip, MFA continuation held only in component memory, and password-UI gating on `/policy` — with Vitest + React Testing Library + MSW wired into the SPA and into `make ci-frontend-client` so every behaviour lands with a test.

**Architecture (spec v4.5 §4.9–§4.10, §6, §7 row 4):** The backend contract already exists and is safe (PR 2 + PR 3, both in `dev`): `GET /v1/auth/client/providers` → `{providers: string[]}`; `POST /v1/auth/client/oauth/login {provider}` → `{authUrl, state}` and the HttpOnly `orkestra_oauth_state` cookie on the client API host; the operator-host callback relays every client-tier outcome to `GET {CLIENT_API_URL}/v1/auth/client/oauth/complete?relay=<id>`, which verifies the browser binding, sets the client refresh cookie on its own host and redirects to `{CLIENT_FRONTEND_URL}/auth/callback` under the closed contract (`?success=true&provider=<p>` / `?success=false&error=<allowlisted>` / `#requiresMfa=true&mfaToken=<id>&webauthnAvailable=<bool>`); `GET /v1/auth/client/policy` carries `passwordLoginEnabled: boolean|null` + `passwordLoginBreakGlassEffective` (always false on this tier). This PR is **frontend-client only** plus the CI plumbing: `api/auth.ts` gains the policy fields, `fetchOAuthProviders`, `initiateOAuthLogin`; three pure security modules (`lib/safeNext.ts` open-redirect gate, `lib/oauthReturnTo.ts` ten-minute take-and-delete record, `lib/oauthCallback.ts` closed exact-key parser); `tokenStore.bootstrapFromRefreshCookie` over an **unconditional** shared refresh (marker stamped first but never gating; `ok` installs the memory-only token, `signed-out` clears the speculative marker, `unavailable` — 503 **or transport failure** — keeps it for retry); `LoginPage` gains the provider section and hides the password form/links on persisted false/null; a new `OAuthCallbackPage` scrubs the URL in its first passive effect before any await, bootstraps on success, renders the extracted `MfaChallenge` locally, and maps only allowlisted error codes; `SignupPage`, `ForgotPasswordPage` and the `Layout` CTA hide password sign-up/recovery when the method is off. TanStack Query + react-router 8 + Tailwind, no RTK, no Bootstrap (`frontend-client/CLAUDE.md`).

**Tech Stack:** React 19.2, TypeScript 5.9 strict (`verbatimModuleSyntax`, `noUnused*`), Vite 7.3, react-router 8.3 (`react-router`), TanStack Query 5, react-i18next (IT default / EN fallback), Tailwind v4; **new:** Vitest 4.1 (happy-dom), @testing-library/react 16 + user-event 14 + jest-dom 7, MSW 2.14. Node 24 (`.mise.toml`).

**Spec:** `docs/superpowers/specs/2026-08-29-password-login-toggle-design.md` (v4.5 — Approved for implementation). PR 4 implements the §7 row "4 — Client OAuth login"; every §-reference below is to that file. PR 1 (`7574368a`), PR 2 (`8bde7e5e`) and PR 3 (`4d7c0397`) are in `dev`; this plan was verified against `dev` at **`4d7c0397`** (`4d7c0397bcb8dcaf13626a0ad9d3850a73102054`) — every file:line below refers to that commit.

## Revision log

- **v1 (2026-08-31)** — initial plan, 8 tasks.
- **v2 (2026-08-31)** — the reviewer's round 1 found three blockers and several gaps; every one was verified against `4d7c0397` and is resolved in the task named:
  - **B1** a throwing `localStorage` turned a relay-set cookie into `signed-out` (`setSessionMarker` swallows storage errors, `sessionMarker.ts:22-29`; `refreshAccessToken` short-circuits without the marker, `tokenStore.ts:63`) → the refresh is split into an unconditional shared `performRefresh` that `bootstrapFromRefreshCookie` always calls; the automatic path keeps its marker gate; a test stubs a throwing storage (**Task 4**, F13).
  - **B2** a transport failure of the refresh left `/auth/callback` on the spinner forever (`fetch` without `catch`, `tokenStore.ts:67`; the page only used `.then`) → transport failure maps to `unavailable` for every caller, token and marker preserved; "network failure → retry → success" tests at both levels (**Tasks 4, 6**, F11, D17).
  - **B3** a malformed `/providers` body (`{}`) was coerced to `[]` and would have rendered "no sign-in method" → a non-array `providers` rejects like a 503; only `{providers: []}` is the empty state (**Tasks 3, 5**, D18).
  - `sanitizeNext` compared the auth routes case-sensitively on the undecoded path, while react-router matches case-insensitively on the decoded one (`/LOGIN`, `/%6cogin`, `/auth/%63allback` — verified by the reviewer against react-router 8.3.0) → decoded, lower-cased, segment-normalised compare + tests (**Task 2**, F14, D4).
  - `ForgotPasswordPage` never rendered its mutation error and its comments claimed "always 200" (`ForgotPasswordPage.tsx:8-10,56-62`; `api/auth.ts:289-290`) while PR 3 gates the route 403/503 → the form renders the backend's answer mapped by code; comments corrected (**Task 7**, F12).
  - No test mounted the callback through `App` + `Layout` → an integrated test through the real route table (**Task 6**).
  - The deviation table said PENDING while ordering execution → the reviewer's rulings are recorded per row; D4 and D11 are approved conditional on the corrections above; D9 is an explicit MVP limitation.
  - No browser verification before handoff → a browser smoke test on the running staging client (bind-mounted Vite) joins **Task 8**; the IdP round-trip stays manual.
  - `$SCRATCHPAD` was not guaranteed and the scope check covered four directories → `mktemp -d` and an allowlist of every path this PR may touch (**Task 8**).
  - Steps of several hundred lines → Tasks 2–7 are split into RED/GREEN cycles per behaviour; each implementation increment is shown as the code it adds.
- **v3 (2026-08-31)** — round 2 found one blocker and five corrections, all confirmed:
  - **Blocker:** with `globals: false` React Testing Library never registers its automatic `cleanup()` (it needs a global `afterEach`; the admin SPA avoids this with `globals: true`), so every render would have leaked into the next test — two buttons for `AuthProvider.test.tsx`'s second case, stale `AuthProvider`s still subscribed to the token store → `setup.ts` calls `cleanup()` explicitly, right after `vi.unstubAllGlobals()` (**Task 1**).
  - The "scrub before the refresh" tests only proved both facts became true, not their order → the MSW refresh handler now records the router location the page had committed when the request arrived, and the tests assert it is already `/auth/callback` (**Task 6**, page and App level).
  - `auth.test.ts` has 20 tests, not 17 (the providers `it.each` expands to 4 rows: 9 provider cases) → count corrected (**Task 3**).
  - `api/client.ts:47-51` described only the 503 exception → the comment now covers the transport-failure case D17 introduced (**Task 4**).
  - `CLAUDE_SESSION` is unset in the executing environment, so the trailer would have been empty → the value is stated in Global Constraints and every commit uses `${CLAUDE_SESSION:?…}`, which aborts the commit instead of writing an empty trailer; the exit checklist counts the trailers.
  - D17 and D18 approved by the reviewer → table updated.

## Global Constraints

- **Branch:** `feat/auth-client-oauth-login`, created from `dev` = `4d7c0397`. PR 4 targets `dev`. No backend, `frontend-admin`, `pkg/sdk` or OpenAPI change is in scope — if a task believes it needs one, it is a plan defect to rule on, not a change to make silently.
- **Stack rules (`frontend-client/CLAUDE.md`, binding):** TanStack Query is the only server-state library — **never** RTK Query/redux; **never** Bootstrap/Falcon/react-bootstrap or components copied from `frontend-admin`; Tailwind utility classes only (no inline colour/spacing styles, no SCSS); `@/*` alias everywhere (never bare `components/…`); react-router 8 imports from `react-router` (no `react-router-dom`); one page per route named `<Thing>Page.tsx`; co-locate sub-components, promote to `src/components/` only on second use; every user-visible string through `t()` with **both** `en.json` and `it.json` keys in the same commit (the parity test Task 1 adds enforces it); `credentials: 'include'` on every fetch to the API; the access token is memory-only — never `localStorage`/`sessionStorage`; the only persistent auth state is the non-secret session marker (`orkestra_client_session_marker`, `sessionMarker.ts:11`).
- **Fail-open display, fail-closed backend (§4.9, §4.10, §5 #15).** `fetchAuthPolicy` keeps its fail-open fallback on any failure; the fallback gains `passwordLoginEnabled: true` and `passwordLoginBreakGlassEffective: false`. A **loaded** policy shows the password UI only when `passwordLoginEnabled === true`: `null` (the wire type is nullable) and `false` both hide it. `passwordLoginUsable(policy)` is the ONLY reader of the field. `/policy` reachable but the method off → password form/links hidden, never a form that 403s on submit (G5). The **forms** still surface the backend's answer when the display fell open: `ForgotPasswordPage` renders its mutation error mapped by code (`auth.policy_unavailable` → retryable copy, `auth.password_login_disabled` → the disabled notice, anything else → the generic error) — today it renders no error at all (F12); `LoginPage` and `SignupPage` already render theirs. Registration/recovery/reset/invite routes keep their existing behaviour except where §4.10 and D8 say otherwise.
- **Providers: three distinct states, error is retryable (§4.10).** Loading, resolved-empty and query-error render differently; an error is never reported as "no method". The no-method alert renders only when the kill switch is off, the persisted password policy is false/null **and** the provider query has *resolved* to an empty list. A response whose `providers` is not an array is **malformed** and rejects like a 503 — only `{providers: []}` is the empty state (round-1 B3, D18). Provider names are the closed union `google | apple | github | discord` (`lib/oauthProviders.ts`); a name outside it is dropped with a console warning, never rendered.
- **OAuth start (§4.10).** `POST /v1/auth/client/oauth/login {provider}` with `credentials:'include'` (the response sets the HttpOnly state cookie the relay endpoint **requires** — `backend/internal/core/auth/CLAUDE.md:847`); the validated `next` target is stashed **before** `browserNavigation.assign(authUrl)`; nothing about the destination is sent to the backend. Start errors are mapped by `code` (`auth.login_disabled`, `auth.oauth_provider_disabled`, `auth.policy_unavailable`) to i18n copy with a generic fallback; the raw backend detail is not rendered.
- **`next` validation (§4.10 client row, §5 #28).** `sanitizeNext(raw)` parses against `window.location.origin`, requires the same origin and a value beginning with exactly one `/`, rejects protocol-relative (`//`), raw or percent-encoded backslash (`\`, `%5c`), an encoded leading slash (`/%2f`), control characters (in the raw value **and** revealed by decoding), a malformed percent-sequence, and any path under `/auth/callback`, `/login`, `/signup`, `/forgot-password`, `/reset-password`, `/verify-email`, `/accept-invite` — compared on the **decoded, lower-cased, segment-normalised** pathname, because react-router matches routes case-insensitively on the decoded path (F14: `/LOGIN`, `/%6cogin`, `/auth/%63allback`, `/auth//callback` are auth routes too). It returns the canonical `pathname + search + hash` (as parsed, not lower-cased) or `null`. Fallback is `DEFAULT_POST_LOGIN = "/account"`. One gate for **both** sign-in paths (password and OAuth) — F1.
- **Return-target record (§4.10, §5 #28).** `sessionStorage["orkestra_client_oauth_return_to"] = {target, createdAt}`; written only with a sanitized target (otherwise the key is removed); `takeOAuthReturnTo()` removes the key on **every** call, honours the record only when `0 ≤ now − createdAt ≤ 600000` ms and the target still passes `sanitizeNext`, and runs in the callback page's first effect on every outcome (success, error, MFA).
- **Callback contract is CLOSED (§4.10, v4.4 bullet 4).** `parseOAuthCallback(search, hash)` matches exact key sets: success = query exactly `{success=true, provider∈union}` with an empty fragment; failure = query exactly `{success=false, error}` with an empty fragment; MFA = fragment exactly `{requiresMfa=true, mfaToken≠"", webauthnAvailable∈true|false}` with an empty query. Any extra, duplicated or missing key on either side, an unknown provider, or a payload that mixes a fragment with a query is the generic failure. Error codes map through the closed allowlist `oauth_access_denied | oauth_signup_disabled | oauth_link_disabled | auth.oauth_email_unverified | oauth_provider_unavailable | oauth_login_failed` → i18n keys; anything else collapses to `loginFailed`. **Raw URL text is never rendered.**
- **Scrub before the first await (§4.10).** The callback page parses the URL once during the first render (pure, into a ref), then in its **first passive `useEffect`** (never a layout effect — react-router drops a `navigate()` issued from a mount layout effect, PR 2 implementation note in §0 v4.3) takes the return target and replaces the history entry with the bare pathname **before** any request is issued. The MFA challenge id lives in component memory only: never in router state, `sessionStorage` or `localStorage`.
- **Session bootstrap (§4.10, §5 #23, #27; round-1 B1/B2).** One shared, coalesced, **unconditional** `performRefresh(apiBase)` issues `POST /v1/auth/client/refresh-cookie` and decides the outcome: `ok` → token installed in memory; `signed-out` (401, any non-503 non-2xx, **or** 200 with no token) → marker **and** token cleared; `unavailable` (503 **or the fetch rejecting — a transport failure**) → token and marker untouched. `refreshAccessToken` (the automatic path: cold load, 401 middleware) keeps its marker short-circuit and delegates to `performRefresh`. `bootstrapFromRefreshCookie(apiBase)` stamps the marker **first** (speculatively — so the next cold load and any concurrent auto-refresh know a cookie exists) and then calls `performRefresh` **regardless of whether the stamp succeeded**: a storage that throws (private mode, disabled storage) must never turn a relay-set cookie into a signed-out. The SPA never enters a protected route on the `success=true` flag alone.
- **No secret in storage.** Tests scan every `localStorage` and `sessionStorage` value after success and after an MFA completion: no access token, no `mfaToken`, no challenge id.
- **Kill switch.** `loginEnabled=false` keeps today's banner + disabled password submit, and the provider section is not rendered (OAuth start would refuse with 403 `auth.login_disabled`, `auth_handler.go:465-468`; §1 "it stops OAuth too").
- **Testing discipline (spec §6 "frontend-client", `feedback_plan_writing_lessons`):** MSW `onUnhandledRequest: 'error'` — every endpoint a component mounts is stubbed; **every absence assertion is anchored on a settled positive state first** (a DOM anchor, or `waitForQuerySettled(queryClient, key)` when the tree is byte-identical before/after); tests assert English copy pinned to `en.json` (the setup forces `i18n.changeLanguage("en")`); no `vi.mock` of the module under test's own collaborators when an MSW stub can express the behaviour — the only sanctioned stubs are the `browserNavigation.assign` spy and `vi.stubGlobal("localStorage", …)` for a throwing storage (`vi.unstubAllGlobals()` runs in the shared `afterEach`); `globals: false` means React Testing Library does **not** auto-register `cleanup()` — the shared `afterEach` calls it explicitly, so no test inherits the previous test's tree; a test that passes at RED is a defect unless the step names it as a regression guard. Each task is a sequence of RED → GREEN cycles per behaviour; run the named test file after each increment, never only at the end.
- **Toolchain facts (F6, F7):** Node `v24.19.0` locally, `node = "24"` in `.mise.toml`; vitest `4.1.10` peer-accepts vite `^7` (installed `7.3.2`); happy-dom (2–3× faster than jsdom, no MSW/AbortSignal mismatch — `frontend-admin/vitest.config.ts`). `frontend-client` has **no** prettier config, so the pre-commit `prettier` hook (`.pre-commit-config.yaml:76-85`) applies prettier **defaults** (double quotes, semicolons, trailing commas `all`, 80 cols): write new code in that style and run `npx prettier --write <files>` before `git add` so the hook does not rewrite staged files. ESLint runs with `--max-warnings 0`, `@typescript-eslint/consistent-type-imports: error` (use `import type` / inline `type`), `react-refresh/only-export-components` (a `.tsx` file exports components **or** non-components, never both), `no-control-regex` (disable inline where the control-char check needs it).
- **Docs move in the same commit as the code** (`feedback_commit_doc_hygiene`). The mapping is explicit: Task 1 → `frontend-client/CLAUDE.md` (Tech stack row, `src/test/` layout, `npm test` commands, workflow step), `frontend-client/README.md` (Stack + commands), root `CLAUDE.md:170`, `CONTRIBUTING.md:64`, `docs/site/contributing/ci-and-make.mdx:16`, `.github/workflows/frontend-client.yml:3-5` header; Task 3 → `frontend-client/CLAUDE.md` "How auth works" (policy helper) + the `forgotPassword` comment in `api/auth.ts`; Task 4 → `frontend-client/CLAUDE.md` "How auth works" item 3 (session marker + bootstrap) and the refresh-choreography note; Task 5 → `frontend-client/CLAUDE.md` login gating + `docs/site/architecture/authentication-flow.mdx:179` + `docs/site/modules/core/auth.mdx:88-93` (both currently say the client SPA lacks the gating); Task 6 → `frontend-client/CLAUDE.md` new "OAuth login" subsection + Don'ts, `docs/site/operating/oauth-providers.mdx:255` (step 4, predates the relay); Task 7 → `frontend-client/CLAUDE.md` navigation paragraph + the `ForgotPasswordPage` header comment; Task 8 is the cross-cutting VERIFICATION sweep — it completes and reconciles, it is never the first touch.
- **Test commands** (absolute paths — `cd` drifts the shell between calls): single file `cd /home/tore/orkestra/frontend-client && npx vitest run src/path/to/file.test.tsx`; whole suite `cd /home/tore/orkestra/frontend-client && npm test`; `npm run typecheck && npm run lint -- --max-warnings 0` before every commit; full gate `make -C /home/tore/orkestra ci-frontend-client` (lockcheck → typecheck → lint → test → build after Task 1). Never run two vitest processes at once in this checkout. Docs render for the `docs/site/**` edits: `D=$(mktemp -d /tmp/orkestra-docs-pr4.XXXXXX)`, clone `orkestra-docs`, `npm ci`, `MONOREPO_LOCAL_PATH=/home/tore/orkestra npm run sync` (**full** sync, not `sync:site`), `CI=true npm run build`, then `rm -rf "$D"` (`/tmp` is a 16 GB tmpfs; check `df -h /tmp` first). Browser smoke (Task 8) drives the **running** staging client through `integrated-browser-mcp` — this checkout owns the `orkestra-public-*-staging` stack whose `client-frontend` is a Vite dev server bind-mounted on `../frontend-client` (`docker/docker-compose.staging.yml:308-330`), so the branch renders live; **never start or restart a server**.
- **Never start servers manually**; never `git push --tags`; never `--amend`; stage by path, never `git add -A`; conventional-commit subjects (the `conventional-pre-commit` hook rejects anything else). **Every commit carries the `Claude-Session:` trailer.** The variable is NOT set by the harness: once per shell run `export CLAUDE_SESSION=https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE` (the session that owns this plan; an executor in another session substitutes its own URL — the controller states it in every dispatch brief). Every commit command below passes `-m "Claude-Session: ${CLAUDE_SESSION:?…}"`, so an unset variable aborts the commit instead of writing an empty trailer; the exit checklist counts the trailers. If the prettier hook rewrites a staged file, the commit fails once: re-run `git add <same paths>` and commit again.

## Findings against `4d7c0397` that spec v4.5 does not state

Each is folded into a numbered deviation below where it changes the design; none contradicts the spec's contracts.

- **F1 — `LoginPage` navigates its `?next=` unvalidated today.** `const next = params.get("next") ?? "/account"` and `navigate(decodeURIComponent(next))` (`frontend-client/src/pages/LoginPage.tsx:42,61`) — an absolute or protocol-relative value reaches `navigate()` unchecked, and the `decodeURIComponent` is a *second* decode (`useSearchParams` already decoded once). The spec's `next` validator is specified for the OAuth stash; leaving the password path on a different rule would give one page two redirect policies. Deviation D4 routes both through `sanitizeNext`.
- **F2 — `refresh-cookie` is a raw chi route, not in OpenAPI.** Mounted per tier by `RegisterTierMountableRoutes` (`backend/internal/core/auth/handlers/auth_handler.go:1907`), it encodes `models.TokenResponse` (`accessToken`, `tokenType`, `expiresIn`, …), answers **401** with no cookie, and **503** `session_enforcement_unavailable` when the session store cannot be evaluated (ADR-0017). The existing `refreshAccessToken` already distinguishes the three (`tokenStore.ts:71-98`); its 200-with-no-token branch returns `signed-out` **without** clearing the marker (`:94-98`) — D15 closes that in the shared refresh.
- **F3 — The list/start contract is exactly what §4.10 needs.** `GET /v1/auth/client/providers` → `{providers: []string}` (`auth_handler.go:409-411`), 503 `auth.policy_unavailable` on a document-level failure (`:424-428`); `POST …/oauth/login` refuses 403 `auth.login_disabled` (kill switch, `:465-468`), 503 `auth.policy_unavailable` (`:471-474`), 403 `auth.oauth_provider_disabled` (`:475-478`), and sets the `orkestra_oauth_state` cookie in the success response (`SetCookie: buildOAuthStateCookie`, `:530`). The stored `RedirectURI` is `spaURL() + "/auth/callback"` (`:490`), never the `Origin` header.
- **F4 — The relay lands on `{CLIENT_FRONTEND_URL}/auth/callback` under the closed contract.** `oauthCallbackPath = "/auth/callback"` (`handlers/oauth_callback_redirect.go:36`), builder `:116-140`, allowlist `:41-46,57-64`; `HandleOAuthRelayCompleteHTTP` (`handlers/oauth_callback_flow.go:293-336`) clears the state cookie on every bound outcome and either redirects with the recorded failure code or runs `finishOAuthCompletion` (sets the client refresh cookie, then the success redirect; an MFA partial writes **no** cookie). Dev compose defaults `CLIENT_FRONTEND_URL=http://client.localhost:8081` and `CLIENT_API_URL=http://api.localhost:3000` (`docker/docker-compose.dev.yml:76,87`); staging/prod leave both to `docker/.env` (`:91-106`, `:98-107`). The route `/auth/callback` does not exist in the SPA yet (`App.tsx:21-81`) — today a relayed client login lands on `NotFoundPage` **after** the cookie is set.
- **F5 — The client `/policy` already carries the PR 3 fields; the SPA type lags.** OpenAPI `GetAuthPolicyResponseBody` declares `passwordLoginEnabled: boolean|null` and `passwordLoginBreakGlassEffective`; `AuthPolicy` in `frontend-client/src/api/auth.ts:64-68` has neither and `fetchAuthPolicy` returns the raw body (`:85`), so a body from an *older* backend without the key would read `undefined`. Task 3 spreads the body over the fail-open fallback so a missing key reads `true` and a present `null` stays `null`.
- **F6 — `frontend-client` has zero tests.** No vitest, no RTL, no MSW in `package.json`; `make ci-frontend-client` = lockcheck + typecheck + lint + build (`Makefile:392-405`); the workflow header says so (`.github/workflows/frontend-client.yml:3-5`); root `CLAUDE.md:170`, `CONTRIBUTING.md:64` and `docs/site/contributing/ci-and-make.mdx:16` repeat it. The admin SPA's harness (`frontend-admin/src/test/{setup,server,handlers,render,webStorage}.ts*`, `vitest.config.ts`) is the precedent for versions and shape, adapted from RTK to TanStack Query.
- **F7 — Formatting and lint traps.** No `.prettierrc` under `frontend-client/` (the admin's `.prettierrc.cjs` is scoped to `frontend-admin/`), so the shared pre-commit prettier hook formats client files with defaults — which is why `LoginPage.tsx` is double-quoted while older pages are single-quoted. `eslint.config.js` enforces `consistent-type-imports` as an **error** and CI passes `--max-warnings 0`.
- **F8 — The MFA completion sets the refresh cookie itself.** `POST /v1/auth/client/mfa/login/verify` (Huma) returns `Set-Cookie` with the refresh cookie (`handlers/mfa_handler.go:638`), so after an OAuth-sourced MFA completion on the callback page `signIn(accessToken)` + the cookie leave the SPA in the same state as a password+MFA login today.
- **F9 — `MfaChallenge` is a second-use candidate.** It is co-located in `LoginPage.tsx:253-342`; the callback page needs the identical component (§4.10: "renders the same extracted `MfaChallenge` component used by `LoginPage`"). `frontend-client/CLAUDE.md` promotes a co-located component to `src/components/` on its second use.
- **F10 — Docs that describe the gap.** `docs/site/architecture/authentication-flow.mdx:179` and `docs/site/modules/core/auth.mdx:88-93` say the client SPA "still reads only `loginEnabled` / `registrationEnabled`"; `docs/site/operating/oauth-providers.mdx:255` describes the client flow as completing on the operator host (pre-PR 2 wording). `frontend-client/README.md:9` still says "React Router v7".
- **F11 — `refreshAccessToken` rejects on a transport failure.** The `fetch` at `tokenStore.ts:67` has no `catch`; the outer `try/finally` only resets the in-flight promise. A network failure therefore rejects the returned promise — today an unhandled rejection from `AuthProvider`'s mount call (`AuthProvider.tsx:41`, `void refreshAccessToken(...)`), bounded only because the marker gate usually short-circuits first. A callback page that awaited it with `.then` alone would hang on its spinner (round-1 B2). D17 maps a transport failure to `unavailable` for every caller.
- **F12 — `ForgotPasswordPage` renders no mutation error, and two comments claim the route always answers 200.** The page has no `mutation.isError` branch (`ForgotPasswordPage.tsx:14,56-62`); its header (`:8-10`) and `forgotPassword`'s comment (`api/auth.ts:289-290`) say "always returns 200", which PR 3 made false: 403 `auth.password_login_disabled` and 503 `auth.policy_unavailable` are evaluated before the lookup (§4.3). Today a submit on an SSO-only surface silently re-enables the button.
- **F13 — A throwing storage silently degrades "stamp then refresh" to signed-out.** `setSessionMarker` swallows storage errors (`sessionMarker.ts:22-29`) and `refreshAccessToken` returns `signed-out` without a request when `hasSessionMarker()` is false (`tokenStore.ts:63`). In a private window or with storage disabled, the relay would have set the cookie correctly and the SPA would refuse to use it (round-1 B1).
- **F14 — react-router matches routes case-insensitively on the decoded pathname.** Verified by the reviewer against the installed react-router 8.3.0: `/LOGIN`, `/%6cogin` and `/auth/%63allback` resolve to `/login` and `/auth/callback`. A case-sensitive, undecoded prefix compare in `sanitizeNext` would accept them as post-login targets — not an open redirect, but a loop or a forbidden landing (round-1 gap).
- **F15 — Endpoints an integrated callback test must stub.** `Layout` mounts `GET /v1/auth/client/policy` while anonymous (`Layout.tsx:20-25`) and `GET /v1/auth/client/me` once authenticated (`useMe`, `Layout.tsx:14`); `AccountPage` reads only `useMe` (`AccountPage.tsx:16`). A test that mounts `App` at `/auth/callback` and lands on `/account` therefore needs `/policy`, `/refresh-cookie` and `/me` stubbed.

## Declared deviations — decision table (reviewer's round-1 and round-2 rulings recorded)

Every row records the reviewer's rulings from plan review rounds 1 and 2 (2026-08-31). D4 and D11 are approved conditional on the round-1 corrections, which this revision contains; D17 and D18 (from blockers B2 and B3) were approved in round 2. Every row is now approved; none changes a backend contract, so no spec §0 bump is expected.

| # | Deviation | Shape | Status |
|---|---|---|---|
| D1 | `LoginPage` paints neither sign-in surface until `/policy` has resolved (a `role="status"` loading line) | Implementation (spec: fail-open default on **failure**; the PR 3 follow-up list names the loading-vs-failure flash on SSO-only surfaces) | **Approved (round 1)** |
| D2 | The console's closed exact-key callback parser (v4.4) is ported verbatim to the client (`lib/oauthCallback.ts`) | Implementation | **Approved (round 1)** |
| D3 | URL scrub = `navigate(location.pathname, { replace: true })` in the first **passive** effect (router-aware `history.replaceState`) | Implementation (a bare `window.history.replaceState` would leave `useLocation()` stale; PR 2 pitfall recorded in §0 v4.3) | **Approved (round 1)** |
| D4 | `sanitizeNext` also governs the password path's `?next=`, rejects every anonymous auth route, and compares on the decoded, lower-cased, segment-normalised pathname (F14) | Implementation (F1, F14) | **Approved after the round-1 correction** (Task 2) |
| D5 | The no-method alert derives from the two co-located queries' status — no `onProvidersResolved` seam | Implementation | **Approved (round 1)** |
| D6 | OAuth-start and forgot-password errors mapped by `code` to i18n keys with a generic fallback; the backend detail string is not rendered; one shared `error.policyUnavailable` key | Implementation | **Approved (round 1)** |
| D7 | Kill switch (`loginEnabled=false`) hides the provider section; the password form keeps today's banner + disabled submit | Implementation (§1) | **Approved (round 1)** |
| D8 | `ForgotPasswordPage` gains the password-off alert **and** renders its mutation error mapped by code (F12) | Implementation (G5; §4.10 row "direct forms show the backend's retryable policy error") | **Approved (round 1)**; the mutation-error half is the round-1 correction |
| D9 | MFA continuation on the callback renders the TOTP/backup `MfaChallenge`; `webauthnAvailable` is parsed (closed contract) but not acted on — **a WebAuthn-only client user cannot complete an OAuth-MFA continuation** | Explicit MVP limitation (the client SPA has no WebAuthn login at all); named in `frontend-client/CLAUDE.md` and in the PR description's follow-ups | **Accepted as an explicit MVP limitation (round 1)** |
| D10 | `client-test` runs `vitest run` without coverage; no coverage floor for the client | Implementation | **Approved (round 1)** |
| D11 | `bootstrapFromRefreshCookie` lives in `tokenStore.ts` over the unconditional shared `performRefresh`, and is exposed on `AuthState` through `AuthProvider` | Implementation (F13) | **Approved after the round-1 correction** (Task 4) |
| D12 | Tests render with a fresh `QueryClient` (`retry: false`) per test | Test-only | **Approved (round 1)** |
| D13 | `MfaChallenge` promoted to `src/components/MfaChallenge.tsx` (named export) | Implementation (F9) | **Approved (round 1)** |
| D14 | Callback error/signed-out phases render a "Back to sign in" link (no 3-second auto-redirect) | Implementation | **Approved (round 1)** |
| D15 | Every `signed-out` shape of the refresh — 401, other non-503 non-2xx, **200 without a token** — clears the marker and the token, inside the shared `performRefresh` (so the automatic path gains it too) | Implementation (F2) | **Approved (round 1)** |
| D16 | `Layout`'s Sign-up CTA hides on password-off; `oauth-providers.mdx` step 4 is corrected to the relay + `/auth/callback` landing | Implementation / docs | **Approved (round 1)** |
| D17 | A transport failure of the refresh (`fetch` rejecting) is `unavailable` for **every** caller — token and marker preserved. For the 401 middleware this means the original 401 is returned without clearing the token (today: an unhandled rejection) | Implementation (F11; round-1 B2 asked for the callback — the shared function is where the fetch lives) | **Approved (round 2)** |
| D18 | A `/providers` body whose `providers` is not an array rejects with a 500-shaped `ApiError` (same shape as "200 without `authUrl`"); only `{providers: []}` is the empty state | Implementation (round-1 B3) | **Approved (round 2)** |

1. **`LoginPage` waits for the policy before painting either surface.** `fetchAuthPolicy` never rejects (fail-open inside), so `policy === undefined` is exactly "request in flight". Painting the password form optimistically would flash it on an SSO-only surface for ~100 ms on every cold load, and would make every "form absent" test assertion pass vacuously against the first paint — the exact defect PR 3's reviewers caught in the console. A `role="status"` line (`t("loading")`, existing key) renders instead. `SignupPage`, `ForgotPasswordPage` and `Layout` keep painting immediately (their fail-open first paint is the pre-existing `registrationEnabled` pattern and their password-off state swaps the form for an alert, which is itself the settled anchor). Cost if wrong: one extra frame on cold login loads; reverting is a three-line change.
2. **The closed parser is ported, not re-derived.** `lib/oauthCallback.ts` is `frontend-admin/src/utils/oauthCallbackParams.ts` with the provider union imported from `lib/oauthProviders.ts`. Same three shapes, same `exactKeys` cardinality rule, same generic collapse.
3. **Scrub through the router.** `navigate(location.pathname, { replace: true })` calls `history.replaceState` *and* updates the router's location, so no later render can re-read the outcome from `useLocation()`. It runs in the **first passive effect** with a `useRef` guard (StrictMode double-invocation) — a layout effect here is silently dropped by react-router on initial mount (PR 2, §0 v4.3 implementation note).
4. **One redirect gate per page, judged the way the router matches.** `sanitizeNext` replaces the unvalidated `?next=` on the password path as well (F1) and rejects the whole anonymous auth route set. The check decodes the parsed pathname (a malformed sequence rejects), lower-cases it, drops empty and `.` segments, resolves `..`, and compares segment-wise — so `/LOGIN`, `/%6cogin`, `/auth/%63allback`, `/auth//callback`, `/login%2Fx` and `/%2e%2e/login` are all auth routes (F14). The returned value stays the parsed `pathname + search + hash`, which react-router matches case-insensitively anyway. Cost if wrong: a deep link into an auth page is dropped to `/account` — the correct fallback.
5. **No provider-count callback.** Both queries live in `LoginPage`; `noMethod = loginEnabled && !passwordOn && providers.isSuccess && providers.data.length === 0` is decidable inline and cannot fire on the error branch by construction.
6. **Errors mapped by code, one shared key.** OAuth start: `auth.oauth_provider_disabled` → `login.oauth.providerDisabled`, `auth.policy_unavailable` → `error.policyUnavailable`, `auth.login_disabled` → `login.disabled`, else `login.oauth.startFailed`. Forgot-password: `auth.password_login_disabled` → `forgot.passwordDisabled`, `auth.policy_unavailable` → `error.policyUnavailable`, else `error.generic`. The existing login/signup forms keep rendering `mutation.error.message` (pre-existing behaviour, untouched).
7. **Kill switch hides the provider section.** Rendering buttons whose every click answers 403 `auth.login_disabled` would contradict the banner above them.
8. **Forgot-password gets the alert and the error.** `POST /v1/auth/client/forgot-password` is gated 403/503 since PR 3 (§4.3); the client page has no policy read and no error rendering today (F12). The page reads the same cached `["authPolicy"]` query, swaps the form for `forgot.passwordDisabled` + a back link when the method is off, and — when the display fell open — renders the backend's mapped answer under the form. `ResetPasswordPage` and `AcceptInvitePage` stay untouched — both routes stay open (§4.3).
9. **`webauthnAvailable` is parsed, not honoured — an explicit MVP limitation.** The client SPA has no WebAuthn login (no `webauthn/login/*` wrapper in `api/auth.ts`), so the continuation renders the TOTP/backup challenge exactly as the password path does today; a user whose only second factor is a passkey cannot complete an OAuth-MFA continuation on the client until a WebAuthn login lands there. The flag stays in the parser because the contract is closed on the exact key set. Documented in `frontend-client/CLAUDE.md` and listed as a follow-up in the PR description.
10. **No coverage gate for the client.** `client-test` = `npm test` = `vitest run`. The admin's floor feeds a badge pipeline the client does not have.
11. **The bootstrap seam is on the auth context, over an unconditional refresh.** `tokenStore.performRefresh` (unexported) is the one place the cookie is presented; `refreshAccessToken` = marker gate + `performRefresh`; `bootstrapFromRefreshCookie` = speculative stamp + `performRefresh`. `AuthProvider` exposes `bootstrapFromRefreshCookie: () => Promise<RefreshOutcome>` on `AuthState`, and the callback page reaches it through `useAuth()`.
12. **Fresh `QueryClient` per test with `retry: false`.** Mirrors the store-per-test hermeticity of the admin harness.
13. **`MfaChallenge` moves verbatim** to `src/components/MfaChallenge.tsx` as a named export with an exported `MfaChallengeProps`.
14. **No auto-redirect timer on terminal callback errors.** The page renders the mapped copy and a `Link` to `/login`.
15. **Every signed-out shape clears.** Inside `performRefresh`, a 401/other non-2xx and a 200 without a token all end with `clearSessionMarker(); clearAccessToken()`.
16. **`Layout` CTA + `oauth-providers.mdx` step 4.** The header "Sign up" link hides when `!registrationEnabled || !passwordOn`; step 4 of the local OAuth verification recipe is rewritten to the relay + `/auth/callback` landing.
17. **Transport failure → `unavailable`, for every caller.** The fetch lives in `performRefresh`; catching there is the only place that covers the callback page, the mount refresh and the 401 middleware alike. Semantics match ADR-0017's 503: nothing is known about the session, so nothing is discarded. For the middleware the visible change is "original 401 returned, token kept" instead of an unhandled rejection. Cost if wrong: a genuinely dead session on a flaky network stays "signed in" until the next successful request answers 401 — which then clears it through the normal path.
18. **Malformed `/providers` body rejects.** `Array.isArray(body.providers)` is the contract check; a non-array is `err("Sign-in providers response malformed", 500)` — the same 500-shaped `ApiError` the "200 without `authUrl`" case uses — so the page renders its retryable error, never "no sign-in method".

## File Structure

**frontend-client — new:**

| File | Responsibility |
|---|---|
| `src/lib/oauthProviders.ts` | the closed provider union, `isOAuthProvider` guard, display labels |
| `src/lib/safeNext.ts` | `DEFAULT_POST_LOGIN`, `sanitizeNext` — the single open-redirect gate (decoded / normalised auth-route compare) |
| `src/lib/oauthReturnTo.ts` | ten-minute take-and-delete `next` record around the IdP round-trip |
| `src/lib/oauthCallback.ts` | closed exact-key parser + error-code allowlist → i18n keys |
| `src/components/MfaChallenge.tsx` | the TOTP/backup challenge form (moved from `LoginPage.tsx`) |
| `src/pages/OAuthCallbackPage.tsx` | `/auth/callback`: scrub, bootstrap, MFA-in-memory, mapped errors |
| `src/test/webStorage.ts`, `setup.ts`, `server.ts`, `handlers.ts`, `render.tsx` | Vitest/RTL/MSW harness (happy-dom, unhandled → error, EN copy, fresh QueryClient, `vi.unstubAllGlobals`) |
| `src/locales/locales.test.ts` | EN/IT key parity |
| tests: `src/test/harness.test.tsx`, `src/lib/{oauthProviders,safeNext,oauthReturnTo,oauthCallback}.test.ts`, `src/api/auth.test.ts`, `src/auth/{tokenStore,AuthProvider}.test.ts(x)`, `src/pages/{LoginPage,OAuthCallbackPage,SignupPage,ForgotPasswordPage}.test.tsx`, `src/components/Layout.test.tsx`, `src/App.test.tsx` | one file per behaviour surface; `App.test.tsx` mounts the callback through the real route table |

**frontend-client — modify:**

| File | Change |
|---|---|
| `package.json`, `package-lock.json` | devDeps vitest/happy-dom/msw/@testing-library/*; scripts `test`, `test:watch` |
| `vite.config.ts` | `defineConfig` from `vitest/config` + `test` block (happy-dom, setup, include) |
| `src/api/auth.ts` | `AuthPolicy` fields + fallback spread, `passwordLoginUsable`, `apiErrorCode`, `fetchOAuthProviders` (malformed → reject), `browserNavigation`, `initiateOAuthLogin`; `forgotPassword` comment corrected |
| `src/auth/tokenStore.ts`, `src/auth/authContext.ts`, `src/auth/AuthProvider.tsx` | `performRefresh` (unconditional, transport failure → `unavailable`, every signed-out shape clears), `refreshAccessToken` = gate + `performRefresh`, `bootstrapFromRefreshCookie` + context exposure |
| `src/pages/LoginPage.tsx` | policy gate, providers section, no-method alert, `sanitizeNext`, `MfaChallenge` import |
| `src/App.tsx` | `/auth/callback` route |
| `src/pages/SignupPage.tsx`, `src/pages/ForgotPasswordPage.tsx` (+ mutation error rendering, header comment), `src/components/Layout.tsx` | password-off gating |
| `src/locales/en.json`, `src/locales/it.json` | new keys (listed per task) |
| `CLAUDE.md`, `README.md` | per-task doc steps |

**Repo-level — modify:** `Makefile` (`client-test`, `ci-frontend-client` order, `.PHONY`, `ci-help` lines), `.github/workflows/frontend-client.yml` (header comment), `CLAUDE.md:170`, `CONTRIBUTING.md:64`, `docs/site/contributing/ci-and-make.mdx:16`, `docs/site/architecture/authentication-flow.mdx:179`, `docs/site/modules/core/auth.mdx:88-93`, `docs/site/operating/oauth-providers.mdx:255`.

**Allowlist of every path this PR may touch** (Task 8's scope check greps the branch diff against it): `frontend-client/**`, `Makefile`, `.github/workflows/frontend-client.yml`, `CLAUDE.md`, `CONTRIBUTING.md`, `docs/site/contributing/ci-and-make.mdx`, `docs/site/architecture/authentication-flow.mdx`, `docs/site/modules/core/auth.mdx`, `docs/site/operating/oauth-providers.mdx`, `docs/superpowers/plans/2026-08-31-password-login-toggle-pr4-client-oauth-login.md`. **Not touched:** `backend/**`, `frontend-admin/**`, `backend/openapi/enterprise.json`, `docker/**`, `mobile/**`, every other `docs/**` file.

---
### Task 1: Vitest + RTL + MSW harness in `frontend-client`, `client-test` in the CI gate

The foundation every later task's tests stand on: the dependencies, the vitest config, the four harness files (adapted from the admin's, RTK → TanStack Query), a locale-parity test that is real value on day one, a harness smoke test, and the `make ci-frontend-client` order typecheck → lint → **test** → build. Docs that claim "no tests yet" are corrected in the same commit.

**Files:**
- Modify: `frontend-client/package.json` (devDependencies + scripts), `frontend-client/package-lock.json` (regenerated by `npm install`)
- Modify: `frontend-client/vite.config.ts:6` (import) and the `defineConfig` call (`:171-192`)
- Create: `frontend-client/src/test/webStorage.ts`, `frontend-client/src/test/setup.ts`, `frontend-client/src/test/server.ts`, `frontend-client/src/test/handlers.ts`, `frontend-client/src/test/render.tsx`
- Create: `frontend-client/src/locales/locales.test.ts`, `frontend-client/src/test/harness.test.tsx`
- Modify: `Makefile:155` (`.PHONY`), `:392` (`ci-frontend-client` deps), after `:402` (`client-test` recipe), `:447` (`ci-help`)
- Modify: `.github/workflows/frontend-client.yml:3-5` (header comment)
- Modify: `CLAUDE.md:170`, `CONTRIBUTING.md:64`, `docs/site/contributing/ci-and-make.mdx:16`
- Modify: `frontend-client/CLAUDE.md` (Tech stack table `:20-32`, Directory layout `:36-74`, "Build & dev" commands `:189-194`, "Adding a feature" `:215-224`), `frontend-client/README.md` (`:5-12` Stack, new "Tests" section after "Dev quickstart")

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces (used by every later test):
  - `import { renderWithProviders, waitForQuerySettled, makeQueryClient } from "@/test/render"` — `renderWithProviders(ui, { routerEntries?: InitialEntry[]; queryClient?: QueryClient }) → RenderResult & { queryClient }`; `waitForQuerySettled(queryClient, queryKey: readonly unknown[]) → Promise<void>`
  - `import { server } from "@/test/server"` (MSW `SetupServer`)
  - `import { url, openPolicy, clientPolicyHandler, providersHandler, providersUnavailableHandler } from "@/test/handlers"` — `clientPolicyHandler(overrides?: Partial<AuthPolicy>)`, `providersHandler(providers: string[])`
  - `npm test` / `make client-test`

- [ ] **Step 1: Add the test dependencies and scripts**

Edit `frontend-client/package.json`. In `"scripts"` add two entries after `"typecheck"`:

```json
    "typecheck": "tsc -b --noEmit",
    "test": "vitest run",
    "test:watch": "vitest",
    "codegen": "openapi-typescript ${VITE_API_BASE:-http://api.localhost:3000}/openapi.json -o src/api/openapi.gen.ts"
```

In `"devDependencies"` add (keep the block alphabetically sorted — npm rewrites it sorted anyway):

```json
    "@testing-library/jest-dom": "^7.0.0",
    "@testing-library/react": "^16.3.2",
    "@testing-library/user-event": "^14.6.1",
    "happy-dom": "^20.9.0",
    "msw": "^2.14.6",
    "vitest": "^4.1.10",
```

Then regenerate the lockfile on the host (Node 24):

Run: `cd /home/tore/orkestra/frontend-client && npm install --no-audit --no-fund && node -e "const p=require('./node_modules/vitest/package.json');console.log('vitest',p.version)"`
Expected: `vitest 4.1.x`; `package-lock.json` modified; `git -C /home/tore/orkestra status --short frontend-client/package-lock.json` shows ` M`.

- [ ] **Step 2: Point `vite.config.ts` at `vitest/config` and add the `test` block**

Replace line 6 of `frontend-client/vite.config.ts`:

```ts
import { defineConfig, type Plugin } from "vite";
```

with

```ts
import type { Plugin } from "vite";
import { defineConfig } from "vitest/config";
```

and add a `test` block as the last property of the exported config (after `preview: {…},`):

```ts
  preview: {
    host: "0.0.0.0",
    port: 5173,
    allowedHosts: allowedHosts.includes("*") ? true : allowedHosts,
  },
  // Vitest. happy-dom over jsdom: 2-3x faster and free of the
  // "Expected signal to be an instance of AbortSignal" mismatch jsdom +
  // MSW v2 + Node fetch trip over (same call as frontend-admin). No
  // globals: every test imports describe/it/expect/vi from "vitest", so
  // tsc and ESLint see exactly what each file uses. Without globals React
  // Testing Library cannot register its automatic cleanup (it looks for a
  // global afterEach) — src/test/setup.ts calls cleanup() explicitly.
  test: {
    environment: "happy-dom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    globals: false,
    css: false,
  },
});
```

Run: `cd /home/tore/orkestra/frontend-client && npm run typecheck`
Expected: exit 0 (tsconfig.node.json compiles `vite.config.ts` against the installed `vitest/config` types).

- [ ] **Step 3: Create the harness files**

`frontend-client/src/test/webStorage.ts`:

```ts
import { Storage } from "happy-dom";

// Restores `localStorage` / `sessionStorage` under Node >= 25.
//
// Node 25 defines global Web Storage bindings as own accessors on
// globalThis that evaluate to `undefined` unless the process was started
// with `--localstorage-file`. Those own properties pre-empt the ones
// vitest's happy-dom environment would otherwise install, so on a newer
// Node both globals — and `window.localStorage`, the same binding here —
// read back undefined and every storage touch dies with "Cannot read
// properties of undefined". The repo pins Node 24 (.mise.toml), where none
// of this happens; the guard below is a no-op there and keeps a
// contributor on a newer Node from meeting a red suite unrelated to their
// change. Imported for its side effect as the FIRST import of setup.ts —
// ES imports are hoisted, so a plain statement would run too late.
// Mirrors frontend-admin/src/test/webStorage.ts.
for (const key of ["localStorage", "sessionStorage"] as const) {
  if (globalThis[key]) continue;
  Object.defineProperty(globalThis, key, {
    value: new Storage(),
    configurable: true,
    writable: true,
  });
}
```

`frontend-client/src/test/server.ts`:

```ts
import { setupServer } from "msw/node";
import { defaultHandlers } from "./handlers";

// Single MSW server shared across the whole test run. Lifecycle is wired
// in src/test/setup.ts (listen / resetHandlers / close).
export const server = setupServer(...defaultHandlers);
```

`frontend-client/src/test/handlers.ts`:

```ts
import { http, HttpResponse, type RequestHandler } from "msw";

import type { AuthPolicy } from "@/api/auth";

// Wildcard host so handlers match whatever apiBaseURL resolves to
// (window.__ORKESTRA_CONFIG__, VITE_API_BASE, or the built-in default).
export const url = (path: string) => `*${path}`;

// The client /policy with everything enabled. Task 3 widens AuthPolicy
// with the PR 3 fields (passwordLoginEnabled, passwordLoginBreakGlassEffective)
// and extends this literal; per-test overrides then flip the password field.
export const openPolicy: AuthPolicy = {
  registrationEnabled: true,
  loginEnabled: true,
  passwordMinLength: 10,
};

export const clientPolicyHandler = (overrides: Partial<AuthPolicy> = {}) =>
  http.get(url("/v1/auth/client/policy"), () =>
    HttpResponse.json({ ...openPolicy, ...overrides }),
  );

// GET /v1/auth/client/providers → {providers: string[]} (auth_handler.go:409).
export const providersHandler = (providers: string[]) =>
  http.get(url("/v1/auth/client/providers"), () =>
    HttpResponse.json({ providers }),
  );

// The document-level failure: 503 auth.policy_unavailable (auth_handler.go:424).
export const providersUnavailableHandler = () =>
  http.get(url("/v1/auth/client/providers"), () =>
    HttpResponse.json(
      {
        code: "auth.policy_unavailable",
        detail: "Sign-in policy is temporarily unavailable; try again shortly",
      },
      { status: 503 },
    ),
  );

// Default handlers used by every test unless overridden. Keep this list
// empty: a component that mounts an endpoint must stub it explicitly, so a
// missing stub is a red run, never a silently passing one.
export const defaultHandlers: RequestHandler[] = [];
```

`frontend-client/src/test/render.tsx`:

```tsx
import type { PropsWithChildren, ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, type InitialEntry } from "react-router";
import {
  render,
  waitFor,
  type RenderOptions,
  type RenderResult,
} from "@testing-library/react";

import { AuthProvider } from "@/auth/AuthProvider";

// One QueryClient per test. retry:false — the app's retry:1 (main.tsx)
// would put a backoff in front of every error-path assertion.
export const makeQueryClient = () =>
  new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false, staleTime: 0 },
    },
  });

interface ProvidersRenderOptions extends Omit<RenderOptions, "queries"> {
  // Initial URL(s) for the in-memory router. Defaults to "/". Accepts
  // InitialEntry so a test can seed search/hash (the OAuth callback page).
  routerEntries?: InitialEntry[];
  queryClient?: QueryClient;
}

export interface RenderWithProvidersResult extends RenderResult {
  queryClient: QueryClient;
}

// renderWithProviders — the single entry point for component tests. Same
// provider stack as main.tsx (QueryClientProvider → AuthProvider → Router)
// with a MemoryRouter in place of BrowserRouter. Pair with the MSW server
// in src/test/server.ts: stub HTTP, never the query hooks.
export function renderWithProviders(
  ui: ReactElement,
  {
    routerEntries = ["/"],
    queryClient = makeQueryClient(),
    ...renderOptions
  }: ProvidersRenderOptions = {},
): RenderWithProvidersResult {
  const Wrapper = ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <MemoryRouter initialEntries={routerEntries}>{children}</MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>
  );
  return { queryClient, ...render(ui, { wrapper: Wrapper, ...renderOptions }) };
}

// waitForQuerySettled — resolves once the query under `queryKey` is no
// longer pending (success or error). Policy-gated UI can be byte-identical
// before and after its query lands when the answer matches the fail-open
// default; an absence assertion made against the first paint then passes
// vacuously. Anchor on the cache entry when no DOM anchor exists; prefer a
// DOM anchor whenever one does.
export const waitForQuerySettled = (
  queryClient: QueryClient,
  queryKey: readonly unknown[],
) =>
  waitFor(() => {
    const state = queryClient.getQueryState(queryKey);
    if (!state || state.status === "pending") {
      throw new Error(`${JSON.stringify(queryKey)} has not settled yet`);
    }
  });
```

`frontend-client/src/test/setup.ts`:

```ts
// MUST stay the first import: it restores localStorage/sessionStorage on
// Node >= 25 before anything else can touch them. See webStorage.ts.
import "./webStorage";
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, vi } from "vitest";

import i18n from "@/i18n";
import { clearAccessToken } from "@/auth/tokenStore";
import { server } from "./server";

beforeAll(async () => {
  // Deterministic copy: the language detector would otherwise pick
  // whatever happy-dom reports for navigator.language. Tests assert the
  // English strings of src/locales/en.json.
  await i18n.changeLanguage("en");
  // Throw on any unhandled request so a test cannot pass against a
  // missing stub. Add the endpoint to defaultHandlers or override
  // per-test via server.use(...) when this fires.
  server.listen({ onUnhandledRequest: "error" });
});

afterEach(() => {
  // First: a test that stubbed a global (tokenStore.test.ts installs a
  // throwing localStorage) must not leak it into the storage reset below.
  vi.unstubAllGlobals();
  // React Testing Library registers its automatic cleanup only when a
  // global afterEach exists (globals: false here) — unmount explicitly, or
  // the next test finds the previous tree still mounted and its
  // AuthProvider still subscribed to the token store.
  cleanup();
  server.resetHandlers();
  // tokenStore is module-scoped state; a token left by one test would
  // make the next render start authenticated.
  clearAccessToken();
  localStorage.clear();
  sessionStorage.clear();
});

afterAll(() => server.close());
```

- [ ] **Step 4: Write the two first tests**

`frontend-client/src/locales/locales.test.ts` — the EN/IT parity the CLAUDE.md rule ("add new keys to both files in the same PR") relies on; today both bundles hold the same 226 keys:

```ts
import { describe, expect, it } from "vitest";

import en from "./en.json";
import itBundle from "./it.json";

const flatten = (value: unknown, prefix = ""): string[] =>
  Object.entries(value as Record<string, unknown>).flatMap(([key, child]) =>
    child !== null && typeof child === "object"
      ? flatten(child, `${prefix}${key}.`)
      : [`${prefix}${key}`],
  );

describe("locale bundles", () => {
  it("en.json and it.json declare exactly the same keys", () => {
    const enKeys = flatten(en).sort();
    const itKeys = flatten(itBundle).sort();
    expect(itKeys).toEqual(enKeys);
  });

  it("no value is empty", () => {
    const empty = (bundle: unknown, name: string) =>
      flatten(bundle).filter((key) => {
        const value = key
          .split(".")
          .reduce<unknown>(
            (node, part) => (node as Record<string, unknown>)[part],
            bundle,
          );
        return typeof value === "string" && value.trim() === "";
      }).map((key) => `${name}:${key}`);
    expect([...empty(en, "en"), ...empty(itBundle, "it")]).toEqual([]);
  });
});
```

`frontend-client/src/test/harness.test.tsx` — proves the provider stack, the in-memory router, the EN copy and the MSW gate work together (no network is issued: `RequireAuth` redirects synchronously and `AuthProvider`'s mount refresh short-circuits without a session marker):

```tsx
import { describe, expect, it } from "vitest";
import { Route, Routes, useLocation } from "react-router";
import { screen } from "@testing-library/react";
import { useTranslation } from "react-i18next";

import { RequireAuth } from "@/auth/RequireAuth";
import { renderWithProviders } from "@/test/render";

const Probe = ({ label }: { label: string }) => {
  const location = useLocation();
  return (
    <div data-testid={`${label}-location`}>
      {location.pathname + location.search}
    </div>
  );
};

const EnglishCopy = () => {
  const { t } = useTranslation();
  return <span>{t("nav.signin")}</span>;
};

describe("test harness", () => {
  it("renders through QueryClient + AuthProvider + MemoryRouter and guards a route", () => {
    renderWithProviders(
      <Routes>
        <Route
          path="/account/security"
          element={
            <RequireAuth>
              <Probe label="secret" />
            </RequireAuth>
          }
        />
        <Route path="/login" element={<Probe label="login" />} />
      </Routes>,
      { routerEntries: ["/account/security"] },
    );
    expect(screen.getByTestId("login-location")).toHaveTextContent(
      "/login?next=%2Faccount%2Fsecurity",
    );
    expect(screen.queryByTestId("secret-location")).toBeNull();
  });

  it("resolves copy in English (en.json nav.signin)", () => {
    renderWithProviders(<EnglishCopy />);
    expect(screen.getByText("Sign in")).toBeInTheDocument();
  });
});
```

Run: `cd /home/tore/orkestra/frontend-client && npx vitest run src/locales/locales.test.ts src/test/harness.test.tsx`
Expected: 2 files, 4 tests PASS. (These tests pass on first run by design — they verify the harness, not new behaviour; the RED/GREEN cycle starts in Task 2.)

Run: `cd /home/tore/orkestra/frontend-client && npm run typecheck && npm run lint -- --max-warnings 0`
Expected: both exit 0. If ESLint flags `react-refresh/only-export-components` on `render.tsx`, the file exports no component (the `Wrapper` is local) — check the import list rather than adding a disable.

- [ ] **Step 5: Wire `client-test` into the Makefile**

Edit `/home/tore/orkestra/Makefile` (recipe lines start with a **TAB**):

Line 155 — `.PHONY: client-lockcheck client-typecheck client-lint client-build` → `.PHONY: client-lockcheck client-typecheck client-lint client-test client-build`.

Line 392 — `ci-frontend-client: client-lockcheck client-typecheck client-lint client-build` → `ci-frontend-client: client-lockcheck client-typecheck client-lint client-test client-build`.

After the `client-lint:` recipe (`:401-402`) insert:

```make
client-test:
	@cd frontend-client && npm test
```

Line 447 — `"  make ci-frontend-client    - Client SPA CI (lockfile + typecheck + lint + build)"` → `"  make ci-frontend-client    - Client SPA CI (lockfile + typecheck + lint + test + build)"`.

Run: `make -C /home/tore/orkestra client-test`
Expected: vitest runs the 2 files, exit 0.

- [ ] **Step 6: Reconcile the docs that say "no tests"**

`.github/workflows/frontend-client.yml:3-5` — replace the three comment lines with:

```yaml
# Tier-2 customer-facing SPA. `make ci-frontend-client` runs lockfile sync,
# typecheck, lint, the Vitest suite (happy-dom + MSW) and the production
# build — the single target the job below invokes.
```

`CLAUDE.md:170` → `- \`frontend-client.yml\` → \`make ci-frontend-client\` (typecheck, eslint, tests, build)`.

`CONTRIBUTING.md:64` → `| \`make ci-frontend-client\` | Client SPA: lockfile sync, typecheck, lint, tests, build |`.

`docs/site/contributing/ci-and-make.mdx:16` → `| \`make ci-frontend-client\`| typecheck, eslint, tests, build                                     |` (pad the cell so the table column stays aligned with its neighbours).

`frontend-client/CLAUDE.md`:
- Tech stack table (`:20-32`): add a row after `Auth`: `| Tests           | Vitest 4 + React Testing Library + MSW 2 on happy-dom — \`npm test\`; an unhandled request fails the run        |`
- Directory layout (`:36-74`): add under `src/`, after the `locales/` line:
  ```
  │   ├── test/
  │   │   ├── setup.ts            # jest-dom, EN copy, MSW lifecycle (unhandled request = error), explicit RTL cleanup, global-stub + storage + token reset
  │   │   ├── server.ts           # the one MSW server
  │   │   ├── handlers.ts         # url() + reusable stubs (clientPolicyHandler, providersHandler, …)
  │   │   ├── render.tsx          # renderWithProviders (QueryClient + AuthProvider + MemoryRouter), waitForQuerySettled
  │   │   └── webStorage.ts       # restores localStorage/sessionStorage under Node ≥ 25
  ```
- "Build & dev" commands (`:189-194`): add after `npm run typecheck`:
  ```
  npm test                   # vitest run — happy-dom + MSW; CI runs it between lint and build
  npm run test:watch
  ```
- "Adding a feature" (`:215-224`): change step 7 to `**Run** \`npm run typecheck && npm run lint && npm test && npm run build\` before committing…` and insert a new step before it:
  `7. **Write the test** next to the page (\`<Name>Page.test.tsx\`) with \`renderWithProviders\` from \`@/test/render\`. Stub every endpoint the component mounts (\`src/test/handlers.ts\` or \`server.use(...)\`) — MSW runs with \`onUnhandledRequest: 'error'\`, so a missing stub is a red run. Anchor every absence assertion on a settled positive state first (\`waitForQuerySettled(queryClient, key)\` when the tree is identical before and after the query lands).` — renumber the following steps.

`frontend-client/README.md`:
- `:9` → `- React Router v8 + TanStack Query v5`
- add a bullet `- Vitest 4 + React Testing Library + MSW (happy-dom) — \`npm test\``
- add after the "Dev quickstart" section:
  ```
  ## Tests

  ```bash
  cd frontend-client
  npm test              # vitest run — what `make ci-frontend-client` runs between lint and build
  npm run test:watch
  ```

  MSW runs with `onUnhandledRequest: 'error'`: stub every endpoint a component mounts.
  ```

- [ ] **Step 7: Full client gate**

Run: `make -C /home/tore/orkestra ci-frontend-client`
Expected: `frontend-client: package-lock.json is in sync with package.json`, typecheck, lint, `Test Files 2 passed`, build, `Frontend-client CI: OK`.

- [ ] **Step 8: Commit**

```bash
cd /home/tore/orkestra && npx --prefix frontend-client prettier --write frontend-client/src/test/*.ts frontend-client/src/test/*.tsx frontend-client/src/locales/locales.test.ts frontend-client/vite.config.ts frontend-client/package.json >/dev/null
git add frontend-client/package.json frontend-client/package-lock.json frontend-client/vite.config.ts frontend-client/src/test/webStorage.ts frontend-client/src/test/setup.ts frontend-client/src/test/server.ts frontend-client/src/test/handlers.ts frontend-client/src/test/render.tsx frontend-client/src/test/harness.test.tsx frontend-client/src/locales/locales.test.ts Makefile .github/workflows/frontend-client.yml CLAUDE.md CONTRIBUTING.md docs/site/contributing/ci-and-make.mdx frontend-client/CLAUDE.md frontend-client/README.md
git commit -m "test(frontend-client): add Vitest + RTL + MSW harness and the client-test CI gate

Vitest 4 on happy-dom with MSW (unhandled request = error), a
renderWithProviders that mirrors main.tsx (QueryClient → AuthProvider →
router), waitForQuerySettled for policy-gated trees, an EN/IT locale
parity test and a harness smoke test. make ci-frontend-client now runs
lockcheck → typecheck → lint → test → build; the workflow header, root
CLAUDE.md, CONTRIBUTING.md and ci-and-make.mdx stop claiming the client
has no tests." -m "Claude-Session: ${CLAUDE_SESSION:?export CLAUDE_SESSION=<session URL> first}"
```


### Task 2: The OAuth security primitives — provider union, `sanitizeNext`, the return-target record, the closed callback parser

Four pure modules with no React and no network, each the single home of one rule the later tasks rely on. They are the security surface of this PR (open redirect, client-writable storage, URL-carried outcome), so each gets its own RED → GREEN cycle and the task its own review gate.

**Files:**
- Create: `frontend-client/src/lib/oauthProviders.ts`, `frontend-client/src/lib/safeNext.ts`, `frontend-client/src/lib/oauthReturnTo.ts`, `frontend-client/src/lib/oauthCallback.ts`
- Test: `frontend-client/src/lib/oauthProviders.test.ts`, `frontend-client/src/lib/safeNext.test.ts`, `frontend-client/src/lib/oauthReturnTo.test.ts`, `frontend-client/src/lib/oauthCallback.test.ts`
- Modify: `frontend-client/CLAUDE.md` (Directory layout, `lib/` block `:63-65`)

**Interfaces:**
- Consumes: nothing.
- Produces (used by Tasks 3, 5, 6):
  - `OAUTH_PROVIDERS: readonly ["google","apple","github","discord"]`, `type OAuthProviderName`, `isOAuthProvider(v: unknown): v is OAuthProviderName`, `OAUTH_PROVIDER_LABELS: Record<OAuthProviderName, string>`
  - `DEFAULT_POST_LOGIN = "/account"`, `sanitizeNext(raw: unknown): string | null`
  - `OAUTH_RETURN_TO_KEY = "orkestra_client_oauth_return_to"`, `OAUTH_RETURN_TO_TTL_MS = 600000`, `stashOAuthReturnTo(target: unknown, now?: number): void`, `takeOAuthReturnTo(now?: number): string | null`
  - `parseOAuthCallback(search: string, hash: string): OAuthCallbackOutcome`, `type OAuthCallbackOutcome = {kind:"success"; provider} | {kind:"mfa"; challengeId; webauthnAvailable} | {kind:"error"; errorKey}`, `OAUTH_CALLBACK_ERROR_KEYS`, `type OAuthCallbackErrorKey = "accessDenied"|"signupDisabled"|"linkDisabled"|"emailUnverified"|"providerUnavailable"|"loginFailed"`

#### Cycle A — the provider union

- [ ] **Step A1: Write the failing test** — `frontend-client/src/lib/oauthProviders.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import {
  OAUTH_PROVIDERS,
  OAUTH_PROVIDER_LABELS,
  isOAuthProvider,
} from "@/lib/oauthProviders";

describe("oauthProviders", () => {
  it("is exactly the four backend providers, each with a label", () => {
    expect([...OAUTH_PROVIDERS]).toEqual(["google", "apple", "github", "discord"]);
    for (const p of OAUTH_PROVIDERS) expect(OAUTH_PROVIDER_LABELS[p]).toBeTruthy();
  });

  it.each([["google"], ["apple"], ["github"], ["discord"]])(
    "accepts %s",
    (name) => expect(isOAuthProvider(name)).toBe(true),
  );

  it.each([["facebook"], ["Google"], [""], [null], [undefined], [42], [{}]])(
    "rejects %j",
    (value) => expect(isOAuthProvider(value)).toBe(false),
  );
});
```

- [ ] **Step A2: Run it — RED.** `cd /home/tore/orkestra/frontend-client && npx vitest run src/lib/oauthProviders.test.ts` → `Failed to resolve import "@/lib/oauthProviders"`.

- [ ] **Step A3: Implement** — `frontend-client/src/lib/oauthProviders.ts`:

```ts
// The closed set of web OAuth providers the backend can return from
// GET /v1/auth/client/providers and accept on POST /v1/auth/client/oauth/login
// (backend/internal/core/auth/models OAuthProvider). A name outside this
// union is never rendered and never sent: fetchOAuthProviders drops it
// with a console warning, and the callback parser treats it as the
// generic failure. Adding a fifth provider is a deliberate change here,
// in the labels below and in both locale bundles.
export const OAUTH_PROVIDERS = ["google", "apple", "github", "discord"] as const;
export type OAuthProviderName = (typeof OAUTH_PROVIDERS)[number];

export function isOAuthProvider(value: unknown): value is OAuthProviderName {
  return (
    typeof value === "string" &&
    (OAUTH_PROVIDERS as readonly string[]).includes(value)
  );
}

// Display names are brand names, not translated copy.
export const OAUTH_PROVIDER_LABELS: Record<OAuthProviderName, string> = {
  google: "Google",
  apple: "Apple",
  github: "GitHub",
  discord: "Discord",
};
```

- [ ] **Step A4: Run it — GREEN.** Same command → 3 blocks PASS.

#### Cycle B — `sanitizeNext`

- [ ] **Step B1: Write the failing test** — `frontend-client/src/lib/safeNext.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { DEFAULT_POST_LOGIN, sanitizeNext } from "@/lib/safeNext";

const origin = window.location.origin;

describe("sanitizeNext", () => {
  it("exports /account as the fallback destination", () => {
    expect(DEFAULT_POST_LOGIN).toBe("/account");
  });

  it.each([
    ["/account", "/account"],
    ["/account/security?tab=oauth#top", "/account/security?tab=oauth#top"],
    ["  /account/billing  ", "/account/billing"],
    ["/loginx", "/loginx"],
    ["/accounts", "/accounts"],
    ["/Account", "/Account"],
    ["/account/%41", "/account/%41"],
    [
      "/account?redirect=https%3A%2F%2Fevil.example",
      "/account?redirect=https%3A%2F%2Fevil.example",
    ],
  ])("accepts the same-origin relative path %j", (raw, expected) => {
    expect(sanitizeNext(raw)).toBe(expected);
  });

  it.each([
    [null],
    [undefined],
    [""],
    ["   "],
    [42],
    [{ pathname: "/account" }],
    ["account"],
    ["https://evil.example/x"],
    [`${origin}/account`],
    ["//evil.example"],
    ["/\\evil.example"],
    ["/%5Cevil.example"],
    ["/%5cevil.example"],
    ["/%2F%2Fevil.example"],
    ["/%2fevil.example"],
    ["/acc\\ount"],
    ["/a\u0000b"],
    ["/a\nb"],
    ["/account%00x"],
    ["/%E0%A4%A"],
  ])("rejects %j", (raw) => {
    expect(sanitizeNext(raw)).toBeNull();
  });

  it.each([
    "/auth/callback",
    "/auth/callback?success=true&provider=google",
    "/auth/callback/",
    "/login",
    "/login/",
    "/login?next=%2Faccount",
    "/signup",
    "/forgot-password",
    "/reset-password?token=abc",
    "/verify-email?token=abc",
    "/accept-invite?token=abc",
  ])("rejects the auth route %s (it would loop or strand the user)", (raw) => {
    expect(sanitizeNext(raw)).toBeNull();
  });

  // react-router matches routes case-insensitively on the DECODED
  // pathname, so every spelling the router would send to an auth page must
  // be judged as that page (plan F14).
  it.each([
    "/LOGIN",
    "/Login/",
    "/Auth/Callback",
    "/%6cogin",
    "/auth/%63allback",
    "/auth//callback",
    "/login%2Fx",
    "/%2e%2e/login",
    "/account/%2e%2e/login",
    "/account/../login",
    "/account/./../auth/callback",
  ])("rejects the disguised auth route %s", (raw) => {
    expect(sanitizeNext(raw)).toBeNull();
  });

  it("canonicalises dot segments for accepted paths too", () => {
    expect(sanitizeNext("/account/./security")).toBe("/account/security");
    expect(sanitizeNext("/account/billing/../security")).toBe(
      "/account/security",
    );
  });
});
```

- [ ] **Step B2: Run it — RED.** `npx vitest run src/lib/safeNext.test.ts` → `Failed to resolve import "@/lib/safeNext"`.

- [ ] **Step B3: Implement** — `frontend-client/src/lib/safeNext.ts`:

```ts
// Post-login destination validation — the single open-redirect gate of the
// SPA. A `next` value can arrive from the URL (RequireAuth stamps
// `/login?next=<path>`), from a user-crafted link, or survive an OAuth
// round-trip through sessionStorage (client-writable). Every read funnels
// through sanitizeNext; callers fall back to DEFAULT_POST_LOGIN on null.

export const DEFAULT_POST_LOGIN = "/account";

// Routes a successful login must never bounce back into: the callback page
// itself (a self-loop) and the anonymous auth pages, which would redirect
// straight back out or strand the user on a form they no longer need.
// Stored as segment lists and compared against the DECODED, lower-cased,
// normalised path — the way react-router matches (case-insensitively, on
// the decoded pathname) — so "/LOGIN", "/%6cogin", "/auth//callback" and
// "/login%2Fx" are all judged as the page the router would render.
const AUTH_ROUTES: readonly (readonly string[])[] = [
  ["auth", "callback"],
  ["login"],
  ["signup"],
  ["forgot-password"],
  ["reset-password"],
  ["verify-email"],
  ["accept-invite"],
];

// eslint-disable-next-line no-control-regex
const CONTROL_CHARS = /[\x00-\x1f\x7f]/;

/**
 * canonicalSegments — the path as the router will match it: percent-decoded
 * (a malformed sequence → null), lower-cased, empty and "." segments
 * dropped, ".." resolved against what precedes it. A control character or
 * a backslash revealed by decoding → null.
 */
function canonicalSegments(pathname: string): string[] | null {
  let decoded: string;
  try {
    decoded = decodeURIComponent(pathname);
  } catch {
    return null;
  }
  if (CONTROL_CHARS.test(decoded) || decoded.includes("\\")) return null;
  const out: string[] = [];
  for (const segment of decoded.toLowerCase().split("/")) {
    if (segment === "" || segment === ".") continue;
    if (segment === "..") {
      out.pop();
      continue;
    }
    out.push(segment);
  }
  return out;
}

const isAuthRoute = (segments: readonly string[]): boolean =>
  AUTH_ROUTES.some((route) =>
    route.every((segment, index) => segments[index] === segment),
  );

/**
 * Validate a candidate post-login destination. Returns the canonical
 * same-origin relative path (`pathname + search + hash` as the URL parser
 * produced it), or null when the value is missing, malformed, points
 * off-site, or would loop back into the auth flow (spec §4.10
 * frontend-client row, §5 #28):
 *
 *  - must begin with exactly one "/" — rejects absolute URLs (same-origin
 *    ones included), bare paths, protocol-relative "//host" and the
 *    "/\host" variant browsers normalise to "//";
 *  - no raw or percent-encoded backslash anywhere, no control characters,
 *    no encoded leading slash ("/%2F…") that could smuggle "//" past the
 *    prefix rule;
 *  - parses against window.location.origin and the parsed origin must be
 *    the same (belt and braces over the prefix rule; the parser also
 *    resolves "." / ".." / "%2e" segments);
 *  - never one of AUTH_ROUTES, judged on the decoded, lower-cased,
 *    normalised segments.
 */
export function sanitizeNext(raw: unknown): string | null {
  if (typeof raw !== "string") return null;
  const value = raw.trim();
  if (value === "") return null;
  if (value[0] !== "/") return null;
  if (value[1] === "/" || value[1] === "\\") return null;
  if (CONTROL_CHARS.test(value)) return null;
  const lower = value.toLowerCase();
  if (lower.includes("\\") || lower.includes("%5c")) return null;
  if (lower.startsWith("/%2f")) return null;

  let parsed: URL;
  try {
    parsed = new URL(value, window.location.origin);
  } catch {
    return null;
  }
  if (parsed.origin !== window.location.origin) return null;

  const segments = canonicalSegments(parsed.pathname);
  if (segments === null || isAuthRoute(segments)) return null;

  return `${parsed.pathname}${parsed.search}${parsed.hash}`;
}
```

- [ ] **Step B4: Run it — GREEN.** `npx vitest run src/lib/safeNext.test.ts` → every row PASS. If `"/%2e%2e/login"` or `"/account/%2e%2e/login"` is reported as accepted, the URL parser did not fold the encoded dot segment on this runtime — `canonicalSegments` still decodes them to `..` and resolves, so a failure here means the implementation diverged from the block above; do not weaken the test.

#### Cycle C — the return-target record

- [ ] **Step C1: Write the failing test** — `frontend-client/src/lib/oauthReturnTo.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import {
  OAUTH_RETURN_TO_KEY,
  OAUTH_RETURN_TO_TTL_MS,
  stashOAuthReturnTo,
  takeOAuthReturnTo,
} from "@/lib/oauthReturnTo";

const stored = () => sessionStorage.getItem(OAUTH_RETURN_TO_KEY);

describe("OAuth return-target record", () => {
  it("stashes a sanitized target with its creation time", () => {
    stashOAuthReturnTo("/account/security?tab=oauth", 1_000);
    expect(JSON.parse(stored()!)).toEqual({
      target: "/account/security?tab=oauth",
      createdAt: 1_000,
    });
  });

  it("removes any previous record when the new target is unsafe or absent", () => {
    stashOAuthReturnTo("/account", 1_000);
    stashOAuthReturnTo("//evil.example", 2_000);
    expect(stored()).toBeNull();
    stashOAuthReturnTo("/account", 3_000);
    stashOAuthReturnTo(null, 4_000);
    expect(stored()).toBeNull();
  });

  it("take-and-deletes on every call, even when nothing is stored", () => {
    expect(takeOAuthReturnTo()).toBeNull();
    stashOAuthReturnTo("/account", 1_000);
    expect(takeOAuthReturnTo(1_500)).toBe("/account");
    expect(stored()).toBeNull();
    expect(takeOAuthReturnTo(1_600)).toBeNull();
  });

  it("honours a record up to ten minutes old and ignores an older one", () => {
    stashOAuthReturnTo("/account", 1_000);
    expect(takeOAuthReturnTo(1_000 + OAUTH_RETURN_TO_TTL_MS)).toBe("/account");
    stashOAuthReturnTo("/account", 1_000);
    expect(takeOAuthReturnTo(1_000 + OAUTH_RETURN_TO_TTL_MS + 1)).toBeNull();
    expect(stored()).toBeNull();
  });

  it("ignores a record from the future", () => {
    stashOAuthReturnTo("/account", 5_000);
    expect(takeOAuthReturnTo(4_999)).toBeNull();
  });

  it.each([
    "not json",
    "null",
    "42",
    JSON.stringify({ target: "/account" }),
    JSON.stringify({ target: "/account", createdAt: "1000" }),
    JSON.stringify({ target: "/account", createdAt: Number.NaN }),
    JSON.stringify({ createdAt: 1_000 }),
    JSON.stringify({ target: "//evil.example", createdAt: 1_000 }),
    JSON.stringify({ target: "/auth/callback", createdAt: 1_000 }),
    JSON.stringify({ target: "/LOGIN", createdAt: 1_000 }),
  ])("re-validates a client-written record and rejects %s", (raw) => {
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, raw);
    expect(takeOAuthReturnTo(1_500)).toBeNull();
    expect(stored()).toBeNull();
  });

  it("uses Date.now() when no clock is supplied", () => {
    stashOAuthReturnTo("/account");
    expect(takeOAuthReturnTo()).toBe("/account");
  });
});
```

- [ ] **Step C2: Run it — RED.** `npx vitest run src/lib/oauthReturnTo.test.ts` → `Failed to resolve import "@/lib/oauthReturnTo"`.

- [ ] **Step C3: Implement** — `frontend-client/src/lib/oauthReturnTo.ts`:

```ts
import { sanitizeNext } from "@/lib/safeNext";

// sessionStorage record carrying the validated `next` target across the
// OAuth round-trip (router state cannot survive the redirect out to the
// IdP). Written by initiateOAuthLogin just before leaving the SPA; the
// callback page takes-and-deletes it on EVERY outcome and honours it only
// when it is younger than OAUTH_RETURN_TO_TTL_MS and still passes
// sanitizeNext — sessionStorage is client-writable, so the value is
// re-validated on the way out (spec §4.10, §5 #28).
export const OAUTH_RETURN_TO_KEY = "orkestra_client_oauth_return_to";
export const OAUTH_RETURN_TO_TTL_MS = 10 * 60 * 1000;

interface OAuthReturnRecord {
  target: string;
  createdAt: number;
}

export function stashOAuthReturnTo(
  target: unknown,
  now: number = Date.now(),
): void {
  const safe = sanitizeNext(target);
  try {
    if (!safe) {
      // Also drops a stale record from an earlier, abandoned attempt.
      sessionStorage.removeItem(OAUTH_RETURN_TO_KEY);
      return;
    }
    const record: OAuthReturnRecord = { target: safe, createdAt: now };
    sessionStorage.setItem(OAUTH_RETURN_TO_KEY, JSON.stringify(record));
  } catch {
    // Storage can throw (private mode, disabled storage). Losing the deep
    // link degrades to DEFAULT_POST_LOGIN; it never blocks the login.
  }
}

/** Take-and-delete: the record is removed on every call, whatever its state. */
export function takeOAuthReturnTo(now: number = Date.now()): string | null {
  let raw: string | null = null;
  try {
    raw = sessionStorage.getItem(OAUTH_RETURN_TO_KEY);
    sessionStorage.removeItem(OAUTH_RETURN_TO_KEY);
  } catch {
    return null;
  }
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const { target, createdAt } = parsed as Partial<OAuthReturnRecord>;
  if (typeof createdAt !== "number" || !Number.isFinite(createdAt)) return null;
  if (now < createdAt || now - createdAt > OAUTH_RETURN_TO_TTL_MS) return null;
  return sanitizeNext(target);
}
```

- [ ] **Step C4: Run it — GREEN.** `npx vitest run src/lib/oauthReturnTo.test.ts` → 7 blocks PASS.

#### Cycle D — the closed callback parser

- [ ] **Step D1: Write the failing test** — `frontend-client/src/lib/oauthCallback.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import {
  OAUTH_CALLBACK_ERROR_KEYS,
  parseOAuthCallback,
} from "@/lib/oauthCallback";

const generic = { kind: "error", errorKey: "loginFailed" } as const;

describe("parseOAuthCallback — closed contract", () => {
  it.each(["google", "apple", "github", "discord"] as const)(
    "success: ?success=true&provider=%s",
    (provider) => {
      expect(
        parseOAuthCallback(`?success=true&provider=${provider}`, ""),
      ).toEqual({ kind: "success", provider });
    },
  );

  it("failure: every allowlisted code maps to its i18n key", () => {
    for (const [code, key] of Object.entries(OAUTH_CALLBACK_ERROR_KEYS)) {
      expect(
        parseOAuthCallback(
          `?success=false&error=${encodeURIComponent(code)}`,
          "",
        ),
      ).toEqual({ kind: "error", errorKey: key });
    }
    expect(Object.keys(OAUTH_CALLBACK_ERROR_KEYS).sort()).toEqual(
      [
        "auth.oauth_email_unverified",
        "oauth_access_denied",
        "oauth_link_disabled",
        "oauth_login_failed",
        "oauth_provider_unavailable",
        "oauth_signup_disabled",
      ].sort(),
    );
  });

  it("failure: an unknown, empty or hostile code collapses to loginFailed", () => {
    expect(
      parseOAuthCallback("?success=false&error=internal_stack_trace", ""),
    ).toEqual(generic);
    expect(parseOAuthCallback("?success=false&error=", "")).toEqual(generic);
    expect(
      parseOAuthCallback(
        "?success=false&error=%3Cscript%3Ealert(1)%3C%2Fscript%3E",
        "",
      ),
    ).toEqual(generic);
  });

  it("MFA: exactly the three fragment keys with an empty query", () => {
    expect(
      parseOAuthCallback(
        "",
        "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false",
      ),
    ).toEqual({ kind: "mfa", challengeId: "ch-1", webauthnAvailable: false });
    expect(
      parseOAuthCallback(
        "",
        "requiresMfa=true&mfaToken=ch-1&webauthnAvailable=true",
      ),
    ).toEqual({ kind: "mfa", challengeId: "ch-1", webauthnAvailable: true });
  });

  it.each([
    ["success with an unknown provider", "?success=true&provider=facebook", ""],
    ["success with an extra key", "?success=true&provider=google&email=a%40b.c", ""],
    ["success with a duplicated key", "?success=true&provider=google&provider=github", ""],
    ["success next to an MFA fragment", "?success=true&provider=google", "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false"],
    ["success next to any fragment", "?success=true&provider=google", "#x=1"],
    ["failure missing error", "?success=false", ""],
    ["failure with an extra key", "?success=false&error=oauth_access_denied&user_id=u1", ""],
    ["success=maybe", "?success=maybe&provider=google", ""],
    ["MFA missing the token", "", "#requiresMfa=true&webauthnAvailable=false"],
    ["MFA with an empty token", "", "#requiresMfa=true&mfaToken=&webauthnAvailable=false"],
    ["MFA with requiresMfa=false", "", "#requiresMfa=false&mfaToken=ch-1&webauthnAvailable=false"],
    ["MFA with a non-boolean webauthnAvailable", "", "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=yes"],
    ["MFA with an extra key", "", "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false&access_token=x"],
    ["MFA fragment next to any query", "?x=1", "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false"],
    ["nothing at all", "", ""],
    ["unrelated query", "?foo=bar", ""],
  ])("%s is the generic failure", (_label, search, hash) => {
    expect(parseOAuthCallback(search, hash)).toEqual(generic);
  });
});
```

- [ ] **Step D2: Run it — RED.** `npx vitest run src/lib/oauthCallback.test.ts` → `Failed to resolve import "@/lib/oauthCallback"`.

- [ ] **Step D3: Implement** — `frontend-client/src/lib/oauthCallback.ts` (the console's `utils/oauthCallbackParams.ts`, deviation D2):

```ts
// The SPA side of the CLOSED OAuth callback contract
// (backend: handlers/oauth_callback_redirect.go). Each of the three shapes
// is matched on its EXACT key set and cardinality; anything else — an
// unknown provider, a half-formed MFA fragment, an extra or duplicated key,
// a fragment next to a query outcome, a query next to an MFA fragment — is
// the generic failure. Raw URL text is never surfaced: only the mapped i18n
// key is. Identical to the operator console's parser (spec §4.10, v4.4).

import { isOAuthProvider, type OAuthProviderName } from "@/lib/oauthProviders";

// Login-callback failure codes (oauth_callback_redirect.go:41-46) → keys
// under oauth.callback.errors.* in both locale bundles.
export const OAUTH_CALLBACK_ERROR_KEYS = {
  oauth_access_denied: "accessDenied",
  oauth_signup_disabled: "signupDisabled",
  oauth_link_disabled: "linkDisabled",
  "auth.oauth_email_unverified": "emailUnverified",
  oauth_provider_unavailable: "providerUnavailable",
  oauth_login_failed: "loginFailed",
} as const;

export type OAuthCallbackErrorKey =
  (typeof OAUTH_CALLBACK_ERROR_KEYS)[keyof typeof OAUTH_CALLBACK_ERROR_KEYS];

export type OAuthCallbackOutcome =
  | { kind: "success"; provider: OAuthProviderName }
  | { kind: "mfa"; challengeId: string; webauthnAvailable: boolean }
  | { kind: "error"; errorKey: OAuthCallbackErrorKey };

const GENERIC: OAuthCallbackOutcome = {
  kind: "error",
  errorKey: "loginFailed",
};

const errorKeyFor = (code: string): OAuthCallbackErrorKey =>
  Object.prototype.hasOwnProperty.call(OAUTH_CALLBACK_ERROR_KEYS, code)
    ? OAUTH_CALLBACK_ERROR_KEYS[code as keyof typeof OAUTH_CALLBACK_ERROR_KEYS]
    : "loginFailed";

/**
 * exactKeys: `params` holds exactly the given keys, each exactly once.
 * This — not "the expected keys are present" — is what makes the contract
 * closed: an extra, duplicated or missing key on either side is a payload
 * the backend never produces.
 */
const exactKeys = (
  params: URLSearchParams,
  keys: readonly string[],
): boolean => {
  const present = Array.from(new Set(params.keys()));
  if (present.length !== keys.length) return false;
  return keys.every((key) => params.getAll(key).length === 1);
};

const SUCCESS_KEYS = ["success", "provider"] as const;
const FAILURE_KEYS = ["success", "error"] as const;
const MFA_KEYS = ["requiresMfa", "mfaToken", "webauthnAvailable"] as const;

/**
 * Parse the callback URL parts against the three closed shapes:
 *   success:  query = exactly {success=true, provider∈allowlist}, fragment empty
 *   failure:  query = exactly {success=false, error},              fragment empty
 *   MFA:      fragment = exactly {requiresMfa=true, mfaToken≠"", webauthnAvailable∈true|false}, query empty
 * Anything else is the generic failure.
 */
export function parseOAuthCallback(
  search: string,
  hash: string,
): OAuthCallbackOutcome {
  const query = new URLSearchParams(search);
  const frag = new URLSearchParams(hash.startsWith("#") ? hash.slice(1) : hash);
  const queryEmpty = Array.from(query.keys()).length === 0;
  const fragEmpty = Array.from(frag.keys()).length === 0;

  if (queryEmpty && exactKeys(frag, MFA_KEYS)) {
    const token = frag.get("mfaToken");
    const webauthn = frag.get("webauthnAvailable");
    if (
      frag.get("requiresMfa") !== "true" ||
      !token ||
      (webauthn !== "true" && webauthn !== "false")
    ) {
      return GENERIC;
    }
    return {
      kind: "mfa",
      challengeId: token,
      webauthnAvailable: webauthn === "true",
    };
  }

  if (
    fragEmpty &&
    exactKeys(query, SUCCESS_KEYS) &&
    query.get("success") === "true"
  ) {
    const provider = query.get("provider");
    if (!isOAuthProvider(provider)) return GENERIC;
    return { kind: "success", provider };
  }

  if (
    fragEmpty &&
    exactKeys(query, FAILURE_KEYS) &&
    query.get("success") === "false"
  ) {
    return { kind: "error", errorKey: errorKeyFor(query.get("error") ?? "") };
  }

  return GENERIC;
}
```

- [ ] **Step D4: Run it — GREEN.** `npx vitest run src/lib/oauthCallback.test.ts` → 5 blocks PASS.

#### Close the task

- [ ] **Step E1: Whole `lib/` suite, typecheck, lint**

Run: `cd /home/tore/orkestra/frontend-client && npx vitest run src/lib && npm run typecheck && npm run lint -- --max-warnings 0`
Expected: 4 files PASS; typecheck and lint exit 0.

- [ ] **Step E2: Document the four modules**

`frontend-client/CLAUDE.md`, Directory layout `lib/` block (`:63-65`) — add after `avatarColor.ts`:

```
│   │   ├── oauthProviders.ts   # the closed provider union (google|apple|github|discord) + display labels
│   │   ├── safeNext.ts         # sanitizeNext — the ONE open-redirect gate; auth routes judged on the decoded, normalised path
│   │   ├── oauthReturnTo.ts    # ten-minute take-and-delete `next` record across the IdP round-trip
│   │   ├── oauthCallback.ts    # closed exact-key parser for /auth/callback + the error-code allowlist
```

- [ ] **Step E3: Commit**

```bash
cd /home/tore/orkestra && npx --prefix frontend-client prettier --write frontend-client/src/lib/*.ts >/dev/null
git add frontend-client/src/lib/oauthProviders.ts frontend-client/src/lib/safeNext.ts frontend-client/src/lib/oauthReturnTo.ts frontend-client/src/lib/oauthCallback.ts frontend-client/src/lib/oauthProviders.test.ts frontend-client/src/lib/safeNext.test.ts frontend-client/src/lib/oauthReturnTo.test.ts frontend-client/src/lib/oauthCallback.test.ts frontend-client/CLAUDE.md
git commit -m "feat(frontend-client): OAuth security primitives — safe next, return-target record, closed callback parser

sanitizeNext is the single open-redirect gate (same origin, exactly one
leading slash, no raw/encoded backslash, canonical path) and judges the
auth-route loop on the decoded, lower-cased, segment-normalised pathname —
the way react-router matches — so /LOGIN, /%6cogin and /auth//callback are
rejected too; the return target is a ten-minute take-and-delete
sessionStorage record re-validated on the way out; the /auth/callback
parser matches the three closed shapes on exact key sets and maps only the
allowlisted error codes (spec §4.10). Pure modules, no React." -m "Claude-Session: ${CLAUDE_SESSION:?export CLAUDE_SESSION=<session URL> first}"
```

### Task 3: `api/auth.ts` — policy fields, `passwordLoginUsable`, providers list, OAuth start

The API layer the pages consume, in three cycles: the widened `AuthPolicy` (fail-open fallback for the two new fields, `null` preserved) with the one policy reader; a providers fetch that is deliberately **not** fail-open and rejects a malformed body (round-1 B3); the OAuth start that stashes the validated target before leaving through the `browserNavigation` seam. The `forgotPassword` comment that claims "always 200" is corrected here because this file owns it (F12).

**Files:**
- Modify: `frontend-client/src/api/auth.ts:13` (imports), after `:34` (`apiErrorCode`), `:62-93` (policy block), after `:235` (OAuth block), `:288-290` (`forgotPassword` comment)
- Modify: `frontend-client/src/test/handlers.ts` (`openPolicy` gains the two fields)
- Test: `frontend-client/src/api/auth.test.ts` (grown cycle by cycle)
- Modify: `frontend-client/CLAUDE.md` ("How auth works", after item 3 `:82`)

**Interfaces:**
- Consumes: Task 2's `isOAuthProvider`, `OAuthProviderName`, `stashOAuthReturnTo`.
- Produces (used by Tasks 5–7):
  - `interface AuthPolicy { registrationEnabled; loginEnabled; passwordMinLength; passwordLoginEnabled: boolean | null; passwordLoginBreakGlassEffective: boolean }`
  - `passwordLoginUsable(policy: AuthPolicy | undefined): boolean`
  - `apiErrorCode(e: unknown): string | undefined`
  - `fetchOAuthProviders(signal?: AbortSignal): Promise<OAuthProviderName[]>` — rejects with `{status, code}` on non-2xx and with `{status: 500}` on a malformed body
  - `browserNavigation: { assign(url: string): void }`
  - `initiateOAuthLogin(provider: OAuthProviderName, next: string | null): Promise<void>`

#### Cycle A — policy fields, `passwordLoginUsable`, `apiErrorCode`

- [ ] **Step A1: Write the failing tests** — create `frontend-client/src/api/auth.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";

import {
  apiErrorCode,
  browserNavigation,
  fetchAuthPolicy,
  fetchOAuthProviders,
  initiateOAuthLogin,
  passwordLoginUsable,
} from "@/api/auth";
import { OAUTH_RETURN_TO_KEY } from "@/lib/oauthReturnTo";
import {
  clientPolicyHandler,
  openPolicy,
  providersHandler,
  providersUnavailableHandler,
  url,
} from "@/test/handlers";
import { server } from "@/test/server";

const POLICY = url("/v1/auth/client/policy");
const PROVIDERS = url("/v1/auth/client/providers");
const START = url("/v1/auth/client/oauth/login");

describe("fetchAuthPolicy", () => {
  it("returns the wire fields, null included", async () => {
    server.use(clientPolicyHandler({ passwordLoginEnabled: null }));
    const policy = await fetchAuthPolicy();
    expect(policy.passwordLoginEnabled).toBeNull();
    expect(policy.passwordLoginBreakGlassEffective).toBe(false);
  });

  it("fills the PR 3 fields when an older backend omits them", async () => {
    server.use(
      http.get(POLICY, () =>
        HttpResponse.json({
          registrationEnabled: true,
          loginEnabled: false,
          passwordMinLength: 12,
        }),
      ),
    );
    await expect(fetchAuthPolicy()).resolves.toEqual({
      ...openPolicy,
      loginEnabled: false,
      passwordMinLength: 12,
    });
  });

  it("falls open on a 503 and on a network failure", async () => {
    server.use(
      http.get(POLICY, () =>
        HttpResponse.json({ code: "auth.policy_unavailable" }, { status: 503 }),
      ),
    );
    await expect(fetchAuthPolicy()).resolves.toEqual(openPolicy);
    server.use(http.get(POLICY, () => HttpResponse.error()));
    await expect(fetchAuthPolicy()).resolves.toEqual(openPolicy);
  });
});

describe("passwordLoginUsable", () => {
  it("is true only while the policy is unknown or explicitly true", () => {
    expect(passwordLoginUsable(undefined)).toBe(true);
    expect(
      passwordLoginUsable({ ...openPolicy, passwordLoginEnabled: true }),
    ).toBe(true);
    expect(
      passwordLoginUsable({ ...openPolicy, passwordLoginEnabled: false }),
    ).toBe(false);
    expect(
      passwordLoginUsable({ ...openPolicy, passwordLoginEnabled: null }),
    ).toBe(false);
  });
});

describe("apiErrorCode", () => {
  it("reads the stable backend code off this module's errors and nothing else", () => {
    const e = Object.assign(new Error("refused"), {
      status: 403,
      code: "auth.login_disabled",
    });
    expect(apiErrorCode(e)).toBe("auth.login_disabled");
    expect(apiErrorCode(new Error("plain"))).toBeUndefined();
    expect(apiErrorCode(null)).toBeUndefined();
    expect(apiErrorCode({ code: 42 })).toBeUndefined();
  });
});
```

(The imports of `browserNavigation`, `fetchOAuthProviders`, `initiateOAuthLogin`, `OAUTH_RETURN_TO_KEY`, `providersHandler`, `providersUnavailableHandler`, `PROVIDERS`, `START`, `afterEach` and `vi` are used by cycles B and C — leave them in place. `tsc` would flag them as unused, but the typecheck runs only at Step D2, after both cycles have used every one of them.)

- [ ] **Step A2: Run it — RED.** `cd /home/tore/orkestra/frontend-client && npx vitest run src/api/auth.test.ts` → `SyntaxError: The requested module '@/api/auth' does not provide an export named 'apiErrorCode'` (the file fails to load — every case red).

- [ ] **Step A3: Implement**

In `frontend-client/src/api/auth.ts`, add the imports after `import { getAccessToken } from "@/auth/tokenStore";` (`:13`):

```ts
import { isOAuthProvider, type OAuthProviderName } from "@/lib/oauthProviders";
import { stashOAuthReturnTo } from "@/lib/oauthReturnTo";
```

Add `apiErrorCode` right after `readError` (`:34`):

```ts
// apiErrorCode reads the stable backend `code` off an error thrown by this
// module (undefined for anything else). Pages branch on codes, never on
// localized detail strings.
export function apiErrorCode(e: unknown): string | undefined {
  if (!e || typeof e !== "object") return undefined;
  const code = (e as { code?: unknown }).code;
  return typeof code === "string" ? code : undefined;
}
```

Replace the whole `// --- Public auth policy ---` block (`:62-93`) with:

```ts
// --- Public auth policy ---

export interface AuthPolicy {
  registrationEnabled: boolean;
  loginEnabled: boolean;
  passwordMinLength: number;
  // PR 3 (spec §4.9): the persisted per-surface email/password policy.
  // The wire type is nullable (null only in the operator emergency case,
  // never produced on this tier) and null must read as "off" (§4.10), so
  // the type says so and passwordLoginUsable() is the only reader.
  passwordLoginEnabled: boolean | null;
  // Always false on the client endpoint; carried so the type mirrors the
  // wire shape and no reader is tempted to fake it.
  passwordLoginBreakGlassEffective: boolean;
}

// The fail-open display fallback (spec §4.10, §5 #15): everything enabled,
// legacy 10-char password floor. The backend re-validates on submit.
const FAIL_OPEN_POLICY: AuthPolicy = {
  registrationEnabled: true,
  loginEnabled: true,
  passwordMinLength: 10,
  passwordLoginEnabled: true,
  passwordLoginBreakGlassEffective: false,
};

// fetchAuthPolicy reads the public policy slice the unauthenticated
// login + signup pages need so kill switches hide the CTA instead of
// surfacing as a raw 403 on submit. Falls open on a non-2xx or a network
// failure. A 2xx body is spread over the fallback so a key an older
// backend omits reads as enabled while a present `null` stays null.
export async function fetchAuthPolicy(): Promise<AuthPolicy> {
  try {
    const res = await jsonFetch("/v1/auth/client/policy", { method: "GET" });
    if (!res.ok) return { ...FAIL_OPEN_POLICY };
    const body = (await res.json()) as Partial<AuthPolicy>;
    return { ...FAIL_OPEN_POLICY, ...body };
  } catch {
    return { ...FAIL_OPEN_POLICY };
  }
}

// passwordLoginUsable is the ONE reader of passwordLoginEnabled. An
// undefined policy (query still pending, or a caller without the query)
// reads as usable — the fail-open display default; a loaded policy must
// say exactly `true`: `null` means the persisted state is unknown and the
// SPA treats it as off (§4.10 "when persisted false/null"). The backend
// refuses regardless of what renders.
export function passwordLoginUsable(policy: AuthPolicy | undefined): boolean {
  if (policy === undefined) return true;
  return policy.passwordLoginEnabled === true;
}
```

Add **temporary** stubs at the end of the file so the test module loads for this cycle (replaced by the real implementations in cycles B and C — do not commit them):

```ts
export const browserNavigation = { assign(_url: string): void {} };
export async function fetchOAuthProviders(): Promise<OAuthProviderName[]> {
  throw new Error("not implemented");
}
export async function initiateOAuthLogin(): Promise<void> {
  throw new Error("not implemented");
}
```

Update `frontend-client/src/test/handlers.ts` — replace the `openPolicy` literal and its comment with the five-field version:

```ts
// The client /policy with everything enabled. Per-test overrides flip the
// PR 3 password-login field; passwordLoginBreakGlassEffective is always
// false on this tier (spec §4.9).
export const openPolicy: AuthPolicy = {
  registrationEnabled: true,
  loginEnabled: true,
  passwordMinLength: 10,
  passwordLoginEnabled: true,
  passwordLoginBreakGlassEffective: false,
};
```

- [ ] **Step A4: Run it — GREEN for cycle A.** `npx vitest run src/api/auth.test.ts -t "fetchAuthPolicy|passwordLoginUsable|apiErrorCode"` → 5 PASS.

#### Cycle B — `fetchOAuthProviders` (not fail-open; malformed body rejects)

- [ ] **Step B1: Append the failing tests** to `auth.test.ts`:

```ts
describe("fetchOAuthProviders", () => {
  it("returns the allowlisted names in backend order and drops unknown ones with a warning", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    server.use(providersHandler(["github", "facebook", "google"]));
    await expect(fetchOAuthProviders()).resolves.toEqual(["github", "google"]);
    expect(warn).toHaveBeenCalledTimes(1);
    expect(String(warn.mock.calls[0][0])).toContain("facebook");
    warn.mockRestore();
  });

  it("an empty list is the empty state", async () => {
    server.use(providersHandler([]));
    await expect(fetchOAuthProviders()).resolves.toEqual([]);
  });

  it.each([[{}], [{ providers: null }], [{ providers: "google" }], [[]]])(
    "a body whose providers is not an array (%j) is malformed and rejects — never the empty state",
    async (body) => {
      server.use(http.get(PROVIDERS, () => HttpResponse.json(body)));
      await expect(fetchOAuthProviders()).rejects.toMatchObject({ status: 500 });
    },
  );

  it("rejects — never falls open — on 503 auth.policy_unavailable", async () => {
    server.use(providersUnavailableHandler());
    await expect(fetchOAuthProviders()).rejects.toMatchObject({
      status: 503,
      code: "auth.policy_unavailable",
    });
  });

  it("rejects on a network failure", async () => {
    server.use(http.get(PROVIDERS, () => HttpResponse.error()));
    await expect(fetchOAuthProviders()).rejects.toBeInstanceOf(Error);
  });

  it("sends credentials so the API host's cookies travel", async () => {
    let credentials: RequestCredentials | undefined;
    server.use(
      http.get(PROVIDERS, ({ request }) => {
        credentials = request.credentials;
        return HttpResponse.json({ providers: [] });
      }),
    );
    await fetchOAuthProviders();
    expect(credentials).toBe("include");
  });
});
```

- [ ] **Step B2: Run it — RED.** `npx vitest run src/api/auth.test.ts -t fetchOAuthProviders` → every case fails on `not implemented`.

- [ ] **Step B3: Implement** — replace the temporary `fetchOAuthProviders` stub with:

```ts
// --- Web OAuth login (spec §4.10) ---

// fetchOAuthProviders lists the providers the backend will currently
// accept a login from on this surface (GET /v1/auth/client/providers —
// toggle on AND structurally configured, spec §4.4). Unlike
// fetchAuthPolicy this does NOT fall open: a 503 auth.policy_unavailable
// (document-level failure), a network error, or a body that does not carry
// a `providers` array rejects, so the page renders a retryable error
// instead of concluding "no method exists". Only {providers: []} is the
// empty state. Names outside the allowlist are dropped with a console
// warning — a backend that learns a fifth provider needs a matching SPA
// entry first.
export async function fetchOAuthProviders(
  signal?: AbortSignal,
): Promise<OAuthProviderName[]> {
  const res = await jsonFetch("/v1/auth/client/providers", {
    method: "GET",
    signal,
  });
  if (!res.ok) throw await readError(res, "Sign-in providers unavailable");
  const body = (await res.json()) as { providers?: unknown } | null;
  if (!body || typeof body !== "object" || !Array.isArray(body.providers)) {
    throw err("Sign-in providers response malformed", 500);
  }
  const out: OAuthProviderName[] = [];
  for (const name of body.providers) {
    if (isOAuthProvider(name)) {
      out.push(name);
    } else {
      console.warn(
        `fetchOAuthProviders: unknown provider ${JSON.stringify(name)} ignored`,
      );
    }
  }
  return out;
}
```

- [ ] **Step B4: Run it — GREEN.** `npx vitest run src/api/auth.test.ts -t fetchOAuthProviders` → 9 tests PASS (6 blocks; the `it.each` expands to 4).

#### Cycle C — `initiateOAuthLogin` through the `browserNavigation` seam

- [ ] **Step C1: Append the failing tests** to `auth.test.ts`:

```ts
describe("initiateOAuthLogin", () => {
  afterEach(() => vi.restoreAllMocks());

  it("POSTs {provider} with credentials, stashes the validated next, then leaves for authUrl", async () => {
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    const seen: Array<{ body: unknown; credentials: RequestCredentials }> = [];
    server.use(
      http.post(START, async ({ request }) => {
        seen.push({
          body: await request.json(),
          credentials: request.credentials,
        });
        return HttpResponse.json({
          authUrl: "https://idp.example/authorize?state=s1",
          state: "s1",
        });
      }),
    );
    await initiateOAuthLogin("google", "/account/security");
    expect(seen).toEqual([
      { body: { provider: "google" }, credentials: "include" },
    ]);
    expect(
      JSON.parse(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)!).target,
    ).toBe("/account/security");
    expect(assign).toHaveBeenCalledWith(
      "https://idp.example/authorize?state=s1",
    );
  });

  it("stashes BEFORE assigning, and clears a stale record when next is unsafe", async () => {
    const order: string[] = [];
    vi.spyOn(browserNavigation, "assign").mockImplementation(() => {
      order.push(
        sessionStorage.getItem(OAUTH_RETURN_TO_KEY) === null
          ? "assign:empty"
          : "assign:stashed",
      );
    });
    server.use(
      http.post(START, () =>
        HttpResponse.json({ authUrl: "https://idp.example/a", state: "s" }),
      ),
    );
    await initiateOAuthLogin("github", "/account");
    expect(order).toEqual(["assign:stashed"]);

    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({ target: "/account", createdAt: Date.now() }),
    );
    await initiateOAuthLogin("github", "//evil.example");
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
    expect(order).toEqual(["assign:stashed", "assign:empty"]);
  });

  it.each([
    [403, "auth.oauth_provider_disabled"],
    [403, "auth.login_disabled"],
    [503, "auth.policy_unavailable"],
  ])("surfaces %d %s without leaving the page or stashing", async (status, code) => {
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    server.use(
      http.post(START, () =>
        HttpResponse.json({ code, detail: "refused" }, { status }),
      ),
    );
    await expect(initiateOAuthLogin("apple", "/account")).rejects.toMatchObject(
      { status, code },
    );
    expect(assign).not.toHaveBeenCalled();
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
  });

  it("treats a 200 without authUrl as a failure", async () => {
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    server.use(http.post(START, () => HttpResponse.json({ state: "s" })));
    await expect(initiateOAuthLogin("discord", null)).rejects.toMatchObject({
      status: 500,
    });
    expect(assign).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step C2: Run it — RED.** `npx vitest run src/api/auth.test.ts -t initiateOAuthLogin` → every case fails on `not implemented`.

- [ ] **Step C3: Implement** — replace the temporary `browserNavigation` and `initiateOAuthLogin` stubs with:

```ts
// browserNavigation is the one seam through which the SPA leaves for the
// IdP — a plain object so tests can spy on `assign` without touching
// window.location (not configurable under happy-dom / jsdom).
export const browserNavigation = {
  assign(url: string): void {
    window.location.assign(url);
  },
};

// initiateOAuthLogin starts a web OAuth login: POST {provider} to
// /v1/auth/client/oauth/login — credentials:'include' is load-bearing,
// the response sets the HttpOnly `orkestra_oauth_state` cookie the relay
// endpoint later REQUIRES — then stashes the validated `next` target and
// leaves for the IdP's authorization URL. Nothing about the destination
// is sent: the backend redirects to the configured tier SPA. Errors carry
// the backend `code` (auth.login_disabled | auth.oauth_provider_disabled
// | auth.policy_unavailable) for the page to map; on any error nothing is
// stashed and the page is not left.
export async function initiateOAuthLogin(
  provider: OAuthProviderName,
  next: string | null,
): Promise<void> {
  const res = await jsonFetch("/v1/auth/client/oauth/login", {
    method: "POST",
    body: JSON.stringify({ provider }),
  });
  if (!res.ok) throw await readError(res, "Could not start sign-in");
  const body = (await res.json()) as { authUrl?: unknown };
  if (typeof body.authUrl !== "string" || body.authUrl === "") {
    throw err("OAuth start response missing authUrl", 500);
  }
  stashOAuthReturnTo(next);
  browserNavigation.assign(body.authUrl);
}
```

- [ ] **Step C4: Run it — GREEN.** `npx vitest run src/api/auth.test.ts -t initiateOAuthLogin` → 6 blocks PASS.

#### Close the task

- [ ] **Step D1: Correct the `forgotPassword` comment (F12)** — `frontend-client/src/api/auth.ts:288-290` becomes:

```ts
export async function forgotPassword(email: string): Promise<void> {
  // 200 for every account outcome (enumeration-resistant) — the SPA shows
  // a neutral confirmation whether or not the email exists. The only
  // errors are the per-surface policy answers evaluated BEFORE the lookup
  // (spec §4.3): 403 auth.password_login_disabled and 503
  // auth.policy_unavailable; the page maps them by code.
```

- [ ] **Step D2: Whole file, typecheck, lint**

Run: `cd /home/tore/orkestra/frontend-client && npx vitest run src/api/auth.test.ts src/test && npm run typecheck && npm run lint -- --max-warnings 0`
Expected: `auth.test.ts` 20 tests PASS (3 policy + 1 reader + 1 code + 9 providers — the `it.each` expands to 4 — + 6 start), the harness tests still green; typecheck and lint exit 0 (no leftover stub — `grep -n "not implemented" src/api/auth.ts` prints nothing).

- [ ] **Step D3: Document the policy reader and the start contract**

`frontend-client/CLAUDE.md`, "How auth works" — add a fourth moving part after item 3 (`:82`):

```
4. **Public policy + OAuth start** — `fetchAuthPolicy()` (`GET /v1/auth/client/policy`) falls open on failure, and `passwordLoginUsable(policy)` is the **only** reader of `passwordLoginEnabled`: `undefined` (still loading) reads as usable, `false` **and** `null` read as off, so an SSO-only client surface hides the password UI instead of showing a form the backend refuses with 403 (spec §4.10, G5). `fetchOAuthProviders()` deliberately does **not** fall open — a 503, a network error or a body without a `providers` array is a retryable error state, never "no method"; only `{providers: []}` is empty. `initiateOAuthLogin(provider, next)` POSTs the allowlisted provider **with `credentials:'include'`** (the response sets the HttpOnly `orkestra_oauth_state` cookie the relay endpoint requires), stashes the validated `next` and leaves through `browserNavigation.assign` — the seam tests spy on.
```

- [ ] **Step D4: Commit**

```bash
cd /home/tore/orkestra && npx --prefix frontend-client prettier --write frontend-client/src/api/auth.ts frontend-client/src/api/auth.test.ts frontend-client/src/test/handlers.ts >/dev/null
git add frontend-client/src/api/auth.ts frontend-client/src/api/auth.test.ts frontend-client/src/test/handlers.ts frontend-client/CLAUDE.md
git commit -m "feat(frontend-client): policy fields, providers list and OAuth start in the auth API

AuthPolicy carries passwordLoginEnabled (nullable) and
passwordLoginBreakGlassEffective, spread over the fail-open fallback so a
missing key reads enabled while null stays null; passwordLoginUsable is
the one reader (undefined → usable, false/null → off). fetchOAuthProviders
rejects instead of falling open — 503, network failure and a body without
a providers array alike; only {providers: []} is the empty state — and
filters to the closed union; initiateOAuthLogin POSTs with credentials,
stashes the validated next and leaves through the browserNavigation seam
(spec §4.10). The forgot-password comment no longer claims always-200." -m "Claude-Session: ${CLAUDE_SESSION:?export CLAUDE_SESSION=<session URL> first}"
```

### Task 4: The unconditional refresh, `bootstrapFromRefreshCookie`, and the context seam

The session bootstrap of §4.10 / §5 #23 after round 1: one shared `performRefresh` presents the cookie — coalesced, never gated on the marker, transport failure → `unavailable`, every signed-out shape clears (B1, B2, D15, D17). `refreshAccessToken` (the automatic path) keeps its marker short-circuit on top of it; `bootstrapFromRefreshCookie` stamps the marker speculatively and always refreshes. Cycle B exposes it on the auth context.

**Files:**
- Modify: `frontend-client/src/auth/tokenStore.ts:8` (import) and `:51-104` (the refresh half, replaced)
- Modify: `frontend-client/src/auth/authContext.ts:3-8` (`AuthState`)
- Modify: `frontend-client/src/auth/AuthProvider.tsx:9-15` (import), after `:59` (callback), `:61-69` (value)
- Modify: `frontend-client/src/api/client.ts:47-51` (the middleware comment describes only the 503 exception; D17 adds the transport failure)
- Test: `frontend-client/src/auth/tokenStore.test.ts`, `frontend-client/src/auth/AuthProvider.test.tsx`
- Modify: `frontend-client/CLAUDE.md` ("How auth works" item 3 `:82`, "Refresh choreography" `:84-95`)

**Interfaces:**
- Consumes: `RefreshOutcome` (`tokenStore.ts:46-49`), `setSessionMarker`/`clearSessionMarker`/`hasSessionMarker` (`sessionMarker.ts`).
- Produces (used by Task 6):
  - `tokenStore.bootstrapFromRefreshCookie(apiBase: string): Promise<RefreshOutcome>` — never rejects
  - `tokenStore.refreshAccessToken(apiBase)` — unchanged signature, never rejects (was: rejected on transport failure)
  - `AuthState.bootstrapFromRefreshCookie: () => Promise<RefreshOutcome>` via `useAuth()`

> The three `auth/` files are single-quoted (older style); the pre-commit prettier hook will rewrite them to prettier defaults when you commit. Accept that churn — it is the shared hook's configuration, not a choice — and keep the semantic diff minimal.

#### Cycle A — `performRefresh` semantics through both callers

- [ ] **Step A1: Write the failing tests** — `frontend-client/src/auth/tokenStore.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";

import { hasSessionMarker, setSessionMarker } from "@/auth/sessionMarker";
import {
  bootstrapFromRefreshCookie,
  getAccessToken,
  refreshAccessToken,
  setAccessToken,
} from "@/auth/tokenStore";
import { url } from "@/test/handlers";
import { server } from "@/test/server";

const API = "http://api.test";
const REFRESH = url("/v1/auth/client/refresh-cookie");
const okBody = { accessToken: "at-1", tokenType: "Bearer", expiresIn: 900 };

// Every value currently held in web storage — the assertion surface for
// "no access token is ever persisted".
const storageValues = (): Array<string | null> => [
  ...Array.from({ length: localStorage.length }, (_, i) =>
    localStorage.getItem(localStorage.key(i)!),
  ),
  ...Array.from({ length: sessionStorage.length }, (_, i) =>
    sessionStorage.getItem(sessionStorage.key(i)!),
  ),
];

// A refresh endpoint that counts hits and records the marker state at
// request time.
const countingRefresh = (respond: (hit: number) => Response) => {
  let hits = 0;
  let markerAtRequest: boolean | null = null;
  server.use(
    http.post(REFRESH, () => {
      hits++;
      markerAtRequest = hasSessionMarker();
      return respond(hits);
    }),
  );
  return { hits: () => hits, markerAtRequest: () => markerAtRequest };
};

// A Web Storage that refuses every operation — private mode, a disabled
// storage policy. sessionMarker.ts already swallows these errors; the
// refresh must not depend on the marker having been written.
const throwingStorage = new Proxy({} as Storage, {
  get: () => () => {
    throw new Error("SecurityError: storage disabled");
  },
});

describe("bootstrapFromRefreshCookie", () => {
  it("stamps the session marker BEFORE the refresh request and installs the token in memory only", async () => {
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    expect(hasSessionMarker()).toBe(false);
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-1",
    });
    expect(refresh.markerAtRequest()).toBe(true);
    expect(hasSessionMarker()).toBe(true);
    expect(getAccessToken()).toBe("at-1");
    expect(storageValues()).not.toContain("at-1");
  });

  it("still presents the cookie when storage throws (private mode) — B1", async () => {
    vi.stubGlobal("localStorage", throwingStorage);
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-1",
    });
    expect(refresh.hits()).toBe(1);
    expect(getAccessToken()).toBe("at-1");
  });

  it("signed-out (401) clears the speculative marker and the token", async () => {
    setAccessToken("stale");
    server.use(
      http.post(REFRESH, () =>
        HttpResponse.json({ detail: "no session" }, { status: 401 }),
      ),
    );
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(hasSessionMarker()).toBe(false);
    expect(getAccessToken()).toBeNull();
  });

  it("a 200 without a token is signed-out too and clears the marker", async () => {
    server.use(http.post(REFRESH, () => HttpResponse.json({ ok: true })));
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(hasSessionMarker()).toBe(false);
    expect(getAccessToken()).toBeNull();
  });

  it("unavailable (503) keeps the marker and any token so a retry can succeed", async () => {
    const refresh = countingRefresh((hit) =>
      hit === 1
        ? HttpResponse.json(
            { code: "session_enforcement_unavailable" },
            { status: 503 },
          )
        : HttpResponse.json(okBody),
    );
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(hasSessionMarker()).toBe(true);
    expect(getAccessToken()).toBeNull();
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-1",
    });
    expect(refresh.hits()).toBe(2);
  });

  it("a transport failure is unavailable — never a rejection — and a retry can succeed (B2)", async () => {
    const refresh = countingRefresh((hit) =>
      hit === 1 ? HttpResponse.error() : HttpResponse.json(okBody),
    );
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(hasSessionMarker()).toBe(true);
    expect(getAccessToken()).toBeNull();
    await expect(bootstrapFromRefreshCookie(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-1",
    });
    expect(refresh.hits()).toBe(2);
  });

  it("sends the request with credentials so the refresh cookie travels", async () => {
    let credentials: RequestCredentials | undefined;
    server.use(
      http.post(REFRESH, ({ request }) => {
        credentials = request.credentials;
        return HttpResponse.json(okBody);
      }),
    );
    await bootstrapFromRefreshCookie(API);
    expect(credentials).toBe("include");
  });

  it("leaves an existing marker in place and coalesces with a concurrent automatic refresh", async () => {
    setSessionMarker();
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    const [a, b] = await Promise.all([
      bootstrapFromRefreshCookie(API),
      refreshAccessToken(API),
    ]);
    expect(a).toEqual({ status: "ok", accessToken: "at-1" });
    expect(b).toEqual({ status: "ok", accessToken: "at-1" });
    expect(refresh.hits()).toBe(1);
    expect(hasSessionMarker()).toBe(true);
  });
});

describe("refreshAccessToken (the automatic path)", () => {
  it("still short-circuits without a marker — no request for anonymous visitors", async () => {
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(refresh.hits()).toBe(0);
  });

  it("shares the outcome mapping: 503 and a transport failure are unavailable, nothing cleared", async () => {
    setSessionMarker();
    setAccessToken("at-0");
    const refresh = countingRefresh((hit) =>
      hit === 1
        ? HttpResponse.json({}, { status: 503 })
        : HttpResponse.error(),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "unavailable",
    });
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(refresh.hits()).toBe(2);
    expect(hasSessionMarker()).toBe(true);
    expect(getAccessToken()).toBe("at-0");
  });

  it("a 200 without a token now clears the marker on the automatic path too (D15)", async () => {
    setSessionMarker();
    server.use(http.post(REFRESH, () => HttpResponse.json({})));
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(hasSessionMarker()).toBe(false);
  });
});
```

- [ ] **Step A2: Run it — RED.** `cd /home/tore/orkestra/frontend-client && npx vitest run src/auth/tokenStore.test.ts` → the module fails to load (`does not provide an export named 'bootstrapFromRefreshCookie'`) — every case red. Note for after the implementation: the B1, B2, D15 and "shares the outcome mapping" cases are the ones that were red for a **behavioural** reason, not only the missing export.

- [ ] **Step A3: Implement** — `frontend-client/src/auth/tokenStore.ts`. Line 8 becomes:

```ts
import {
  clearSessionMarker,
  hasSessionMarker,
  setSessionMarker,
} from "@/auth/sessionMarker";
```

and lines 51–104 (from the `// In-flight refresh promise` comment to the end of `refreshAccessToken`) are replaced with:

```ts
// In-flight refresh promise — coalesces concurrent callers (a burst of
// 401s, a StrictMode double-invoked bootstrap, a bootstrap racing the
// cold-load refresh) into a single /v1/auth/client/refresh-cookie call, so
// the rotating refresh cookie is never presented twice at once.
let inflightRefresh: Promise<RefreshOutcome> | null = null;

const signedOut = (): RefreshOutcome => {
  clearSessionMarker();
  clearAccessToken();
  return { status: "signed-out" };
};

// performRefresh presents the refresh cookie once — coalesced, and
// UNCONDITIONAL: it never consults the session marker, so a storage that
// throws (private mode, disabled storage) cannot turn a valid cookie into a
// sign-out. It is the single place the outcome is decided:
//   ok           2xx with an access token → installed in memory (never stored)
//   signed-out   401, any other non-503 non-2xx, or a 2xx without a token →
//                marker and token cleared; there is nothing to retry
//   unavailable  503 (ADR-0017: the session could not be evaluated) or the
//                fetch itself rejecting (a transport failure) → token and
//                marker untouched; the caller may retry
async function performRefresh(apiBase: string): Promise<RefreshOutcome> {
  if (inflightRefresh) return inflightRefresh;
  inflightRefresh = (async (): Promise<RefreshOutcome> => {
    try {
      let res: Response;
      try {
        res = await fetch(`${apiBase}/v1/auth/client/refresh-cookie`, {
          method: "POST",
          credentials: "include",
        });
      } catch {
        return { status: "unavailable" };
      }
      if (res.status === 503) return { status: "unavailable" };
      if (!res.ok) return signedOut();
      // models.TokenResponse — we read accessToken from the body (a legacy
      // `token` key is tolerated).
      const body = (await res.json().catch(() => ({}))) as {
        accessToken?: string;
        token?: string;
      };
      const fresh = body.accessToken ?? body.token ?? null;
      // A 2xx with no token is a broken response, not an outage.
      if (!fresh) return signedOut();
      setAccessToken(fresh);
      return { status: "ok", accessToken: fresh };
    } finally {
      inflightRefresh = null;
    }
  })();
  return inflightRefresh;
}

// refreshAccessToken — the AUTOMATIC path (cold-load rehydration in
// AuthProvider, the 401 middleware in api/client.ts). Skips the request
// entirely when no session marker is present (anonymous visitor): refresh
// cookies are httpOnly so the SPA cannot probe for them, and a
// guaranteed-401 on every cold load is console noise for every anonymous
// visitor. The marker is stamped on signIn (and by bootstrapFromRefreshCookie)
// and cleared on signOut / any signed-out outcome. Never rejects.
export async function refreshAccessToken(
  apiBase: string,
): Promise<RefreshOutcome> {
  if (!hasSessionMarker()) return { status: "signed-out" };
  return performRefresh(apiBase);
}

// bootstrapFromRefreshCookie adopts a refresh cookie the SPA did not set
// itself — the client-tier OAuth relay sets it on the API host and lands
// the browser on /auth/callback with nothing else (spec §4.10, §5 #23).
// The marker is stamped FIRST, speculatively, so the next cold load and any
// concurrent automatic refresh know a cookie exists; then the cookie is
// presented REGARDLESS of whether that stamp succeeded. `ok` keeps the
// marker; every `signed-out` shape clears it again; `unavailable` (503 or
// transport failure) keeps it so the caller can offer a retry. Never rejects.
export async function bootstrapFromRefreshCookie(
  apiBase: string,
): Promise<RefreshOutcome> {
  setSessionMarker();
  return performRefresh(apiBase);
}
```

- [ ] **Step A4: Run it — GREEN.** `npx vitest run src/auth/tokenStore.test.ts` → 11 PASS. Then `npx vitest run src/test` to confirm the harness (which imports `clearAccessToken`) still passes.

#### Cycle B — the context seam

- [ ] **Step B1: Write the failing test** — `frontend-client/src/auth/AuthProvider.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { hasSessionMarker } from "@/auth/sessionMarker";
import { useAuth } from "@/auth/useAuth";
import { url } from "@/test/handlers";
import { renderWithProviders } from "@/test/render";
import { server } from "@/test/server";

const REFRESH = url("/v1/auth/client/refresh-cookie");

const Probe = () => {
  const { isAuthenticated, bootstrapFromRefreshCookie } = useAuth();
  return (
    <button type="button" onClick={() => void bootstrapFromRefreshCookie()}>
      {isAuthenticated ? "in" : "out"}
    </button>
  );
};

describe("AuthProvider.bootstrapFromRefreshCookie", () => {
  it("flips the context to authenticated once the refresh cookie yields a token", async () => {
    server.use(
      http.post(REFRESH, () =>
        HttpResponse.json({
          accessToken: "at-9",
          tokenType: "Bearer",
          expiresIn: 900,
        }),
      ),
    );
    renderWithProviders(<Probe />);
    expect(screen.getByRole("button")).toHaveTextContent("out");
    expect(hasSessionMarker()).toBe(false);
    await userEvent.setup().click(screen.getByRole("button"));
    await waitFor(() =>
      expect(screen.getByRole("button")).toHaveTextContent("in"),
    );
    expect(hasSessionMarker()).toBe(true);
  });

  it("stays signed out and leaves no marker when the cookie is rejected", async () => {
    let calls = 0;
    server.use(
      http.post(REFRESH, () => {
        calls++;
        return HttpResponse.json({ detail: "no session" }, { status: 401 });
      }),
    );
    renderWithProviders(<Probe />);
    await userEvent.setup().click(screen.getByRole("button"));
    // Anchor on the request having completed — the marker is false both
    // before the click and after the 401, so its value alone is no anchor.
    await waitFor(() => expect(calls).toBe(1));
    expect(hasSessionMarker()).toBe(false);
    expect(screen.getByRole("button")).toHaveTextContent("out");
  });
});
```

- [ ] **Step B2: Run it — RED.** `npx vitest run src/auth/AuthProvider.test.tsx` → `bootstrapFromRefreshCookie is not a function`.

- [ ] **Step B3: Implement**

`frontend-client/src/auth/authContext.ts` — full new content:

```ts
import { createContext } from "react";

import type { RefreshOutcome } from "@/auth/tokenStore";

export interface AuthState {
  accessToken: string | null;
  isAuthenticated: boolean;
  signIn: (token: string) => void;
  signOut: () => Promise<void>;
  // Adopt a refresh cookie the SPA did not set itself — the client-tier
  // OAuth relay sets it on the API host and lands on /auth/callback with
  // nothing else. See tokenStore.bootstrapFromRefreshCookie for the
  // marker/outcome semantics. Never rejects.
  bootstrapFromRefreshCookie: () => Promise<RefreshOutcome>;
}

// Module-scoped React context. Kept in its own file (separate from
// AuthProvider) so eslint-plugin-react-refresh stays happy — Fast Refresh
// requires a module to export only components OR only non-components, not
// both.
export const AuthContext = createContext<AuthState | null>(null);
```

`frontend-client/src/auth/AuthProvider.tsx` — the import block (`:9-15`) becomes:

```tsx
import {
  bootstrapFromRefreshCookie as bootstrapFromRefreshCookieStore,
  clearAccessToken,
  getAccessToken,
  refreshAccessToken,
  setAccessToken,
  subscribe,
} from "@/auth/tokenStore";
```

add after the `signOut` callback (`:59`):

```tsx
  const bootstrapFromRefreshCookie = useCallback(
    () => bootstrapFromRefreshCookieStore(apiBaseURL),
    [],
  );
```

and the memoised value (`:61-69`) becomes:

```tsx
  const value = useMemo<AuthState>(
    () => ({
      accessToken: token,
      isAuthenticated: token !== null,
      signIn,
      signOut,
      bootstrapFromRefreshCookie,
    }),
    [token, signIn, signOut, bootstrapFromRefreshCookie],
  );
```

- [ ] **Step B4: Run it — GREEN.** `npx vitest run src/auth` → `tokenStore.test.ts` 11 + `AuthProvider.test.tsx` 2 PASS.

#### Close the task

- [ ] **Step C1: Typecheck, lint**

Run: `cd /home/tore/orkestra/frontend-client && npm run typecheck && npm run lint -- --max-warnings 0`
Expected: both exit 0.

- [ ] **Step C2: Document the bootstrap and the refresh outcomes**

`frontend-client/CLAUDE.md`, "How auth works" item 3 (`:82`) — append to the paragraph:

```
`bootstrapFromRefreshCookie()` (on the auth context, implemented in `tokenStore.ts`) is the one place that stamps the marker *speculatively*: the OAuth callback page calls it to adopt the cookie the client-tier relay set on the API host, and it presents the cookie **whether or not the stamp succeeded** — a storage that throws must not turn a valid cookie into a sign-out. Both it and the automatic `refreshAccessToken` go through one unconditional `performRefresh`: `ok` installs the memory-only token; every signed-out shape (401, other non-2xx, a 200 with no token) clears marker and token; `unavailable` — a 503 **or a transport failure** — keeps both so the caller can retry. Neither function rejects.
```

`frontend-client/src/api/client.ts:47-51` — the middleware's comment block (from `// A refresh that comes back 503 is the exception` to `// tokenStore.ts and ADR-0017.`) becomes:

```ts
// A refresh that comes back `unavailable` is the exception: a 503 means
// the server could not evaluate the session, and a transport failure (the
// fetch itself rejecting) means nothing is known about it — neither is the
// same as the session being over. Surfacing the original 401 without
// clearing the token leaves the user signed in so the next request can
// retry. See RefreshOutcome and performRefresh in tokenStore.ts and
// ADR-0017.
```

"Refresh choreography" (`:84-95`) — replace the sentence "Two consecutive 401s trigger logout." with: `Two consecutive 401s trigger logout. A 503 or a network failure during the refresh is \`unavailable\`: the original 401 is surfaced to the caller and the token is kept, because nothing is known about the session (ADR-0017).`

- [ ] **Step C3: Commit**

```bash
cd /home/tore/orkestra && cd /home/tore/orkestra && npx --prefix frontend-client prettier --write frontend-client/src/auth/tokenStore.ts frontend-client/src/auth/authContext.ts frontend-client/src/auth/AuthProvider.tsx frontend-client/src/api/client.ts frontend-client/src/auth/tokenStore.test.ts frontend-client/src/auth/AuthProvider.test.tsx >/dev/null
git add frontend-client/src/auth/tokenStore.ts frontend-client/src/auth/authContext.ts frontend-client/src/auth/AuthProvider.tsx frontend-client/src/api/client.ts frontend-client/src/auth/tokenStore.test.ts frontend-client/src/auth/AuthProvider.test.tsx frontend-client/CLAUDE.md
git commit -m "feat(frontend-client): unconditional refresh + bootstrapFromRefreshCookie for a relay-set cookie

One shared performRefresh presents the cookie: coalesced, never gated on
the session marker (a throwing storage cannot turn a valid cookie into a
sign-out), transport failure mapped to unavailable instead of an unhandled
rejection, and every signed-out shape — 401, other non-2xx, 200 without a
token — clears marker and token. refreshAccessToken keeps its marker
short-circuit on top; bootstrapFromRefreshCookie stamps the marker first
and always refreshes; exposed on the auth context for the OAuth callback
page (spec §4.10, §5 #23; plan F11, F13, D15, D17)." -m "Claude-Session: ${CLAUDE_SESSION:?export CLAUDE_SESSION=<session URL> first}"
```

### Task 5: `LoginPage` — policy gate, password-off, one redirect policy (increment A); provider section, no-method alert, OAuth start (increment B)

The login page becomes SSO-capable in two increments, each its own RED → GREEN cycle. **A:** `MfaChallenge` moves to `src/components/`; the page waits for `/policy`, hides the password form and its links when the persisted method is false/null, keeps the kill-switch banner, and routes the password path's `?next=` through `sanitizeNext`. **B:** the provider section with distinct loading / retryable-error / resolved states (a malformed body is an error, round-1 B3), the no-method alert on a resolved-empty list, and the OAuth start with the validated `next` stashed.

**Files:**
- Create: `frontend-client/src/components/MfaChallenge.tsx` (moved verbatim from `LoginPage.tsx:253-342`)
- Modify: `frontend-client/src/pages/LoginPage.tsx` (rewritten in A, extended in B)
- Modify: `frontend-client/src/locales/en.json` (`login` block `:54-81`, `error` block `:285-287`), `frontend-client/src/locales/it.json` (same blocks)
- Test: `frontend-client/src/pages/LoginPage.test.tsx` (grown per increment)
- Modify: `frontend-client/CLAUDE.md` (Directory layout `components/` `:59-62`, "How auth works" item 4), `docs/site/architecture/authentication-flow.mdx:179`, `docs/site/modules/core/auth.mdx:86-93`

**Interfaces:**
- Consumes: Task 3's `fetchAuthPolicy`, `passwordLoginUsable`, `fetchOAuthProviders`, `initiateOAuthLogin`, `apiErrorCode`, `browserNavigation`; Task 2's `OAUTH_PROVIDER_LABELS`, `OAuthProviderName`, `sanitizeNext`, `DEFAULT_POST_LOGIN`, `OAUTH_RETURN_TO_KEY`.
- Produces (used by Task 6): `import { MfaChallenge, type MfaChallengeProps } from "@/components/MfaChallenge"` — `MfaChallengeProps = { mfaToken: string; onCancel: () => void; onSuccess: (result: MfaLoginVerifyResult) => void }`; i18n keys `login.subtitleSso`, `login.noMethod`, `login.oauth.*`, `error.policyUnavailable` (shared with Task 7).

- [ ] **Step 0: Add the i18n keys (both bundles, once for both increments)**

`frontend-client/src/locales/en.json`, inside `"login"` — add after `"disabled"` (`:63`):

```json
    "disabled": "Login is temporarily disabled by an administrator. Please try again later.",
    "subtitleSso": "Continue with one of the sign-in providers below.",
    "noMethod": "No sign-in method is currently available. Contact an administrator.",
    "oauth": {
      "divider": "or continue with",
      "continueWith": "Continue with {{provider}}",
      "redirecting": "Redirecting to {{provider}}…",
      "loading": "Loading sign-in options…",
      "loadError": "Could not load the sign-in providers.",
      "retry": "Try again",
      "startFailed": "Could not start sign-in with {{provider}}. Please try again.",
      "providerDisabled": "{{provider}} sign-in is not available on this surface. Contact an administrator."
    },
```

and inside `"error"` (`:285-287`):

```json
  "error": {
    "generic": "Something went wrong. Please try again.",
    "policyUnavailable": "Sign-in policy is temporarily unavailable. Try again shortly."
  }
```

`frontend-client/src/locales/it.json`, same positions:

```json
    "disabled": "L'accesso è momentaneamente disabilitato da un amministratore. Riprova più tardi.",
    "subtitleSso": "Continua con uno dei provider di accesso qui sotto.",
    "noMethod": "Nessun metodo di accesso è al momento disponibile. Contatta un amministratore.",
    "oauth": {
      "divider": "oppure continua con",
      "continueWith": "Continua con {{provider}}",
      "redirecting": "Reindirizzamento a {{provider}}…",
      "loading": "Caricamento delle opzioni di accesso…",
      "loadError": "Impossibile caricare i provider di accesso.",
      "retry": "Riprova",
      "startFailed": "Impossibile avviare l'accesso con {{provider}}. Riprova.",
      "providerDisabled": "L'accesso con {{provider}} non è disponibile su questa superficie. Contatta un amministratore."
    },
```

```json
  "error": {
    "generic": "Si è verificato un errore. Riprova.",
    "policyUnavailable": "La policy di accesso non è al momento disponibile. Riprova tra poco."
  }
```

Run: `cd /home/tore/orkestra/frontend-client && npx vitest run src/locales` → PASS (both bundles gained the same 12 keys).

#### Increment A — gate, password-off, one redirect policy, `MfaChallenge` extracted

- [ ] **Step A1: Write the failing tests** — create `frontend-client/src/pages/LoginPage.test.tsx`:

```tsx
import { afterEach, describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { Route, Routes, useLocation } from "react-router";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { browserNavigation } from "@/api/auth";
import { RequireAuth } from "@/auth/RequireAuth";
import { OAUTH_RETURN_TO_KEY } from "@/lib/oauthReturnTo";
import { LoginPage } from "@/pages/LoginPage";
import {
  clientPolicyHandler,
  openPolicy,
  providersHandler,
  url,
} from "@/test/handlers";
import { renderWithProviders, waitForQuerySettled } from "@/test/render";
import { server } from "@/test/server";

const Probe = ({ label }: { label: string }) => {
  const location = useLocation();
  return (
    <div data-testid={`${label}-location`}>
      {location.pathname + location.search}
    </div>
  );
};

const renderLogin = (entry = "/login") =>
  renderWithProviders(
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/account"
        element={
          <RequireAuth>
            <Probe label="account" />
          </RequireAuth>
        }
      />
      <Route
        path="/account/security"
        element={
          <RequireAuth>
            <Probe label="deeplink" />
          </RequireAuth>
        }
      />
    </Routes>,
    { routerEntries: [entry] },
  );

// A GET the test releases by hand, to observe the page while that query is
// still in flight.
const deferredJson = (path: string, body: unknown) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(url(path), async () => {
      await gate;
      return HttpResponse.json(body);
    }),
  );
  return release;
};

const emailField = () => screen.queryByLabelText(/^email$/i);
const START = url("/v1/auth/client/oauth/login");
const LOGIN = url("/v1/auth/client/login");
const PROVIDERS = url("/v1/auth/client/providers");
const tokenBody = {
  success: true,
  accessToken: "at-1",
  tokenType: "Bearer",
  expiresIn: 900,
};

// Increment B makes the page query /providers on mount; every test stubs
// it from the start (an empty list is neutral for increment A's cases) so
// no case trips MSW's unhandled-request error once B lands.

describe("LoginPage — policy gate and password-off (spec §4.10, D1)", () => {
  it("paints no password form until /policy has resolved", async () => {
    const releasePolicy = deferredJson("/v1/auth/client/policy", openPolicy);
    server.use(providersHandler([]));
    renderLogin();
    // en.json "loading": "Loading…" (the providers' own loading copy differs).
    expect(await screen.findByText(/^loading…$/i)).toBeInTheDocument();
    expect(emailField()).toBeNull();
    releasePolicy();
    expect(await screen.findByLabelText(/^email$/i)).toBeInTheDocument();
  });

  it("password on: form and its two links", async () => {
    server.use(clientPolicyHandler(), providersHandler([]));
    renderLogin();
    expect(await screen.findByLabelText(/^email$/i)).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /forgot password/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /create an account/i }),
    ).toBeInTheDocument();
    // login.subtitle, not the SSO subtitle.
    expect(
      screen.getByText(/enter your credentials to continue/i),
    ).toBeInTheDocument();
  });

  it.each([false, null])(
    "passwordLoginEnabled=%s hides the form and its links and switches the subtitle",
    async (value) => {
      server.use(
        clientPolicyHandler({ passwordLoginEnabled: value }),
        providersHandler([]),
      );
      renderLogin();
      // login.subtitleSso renders only once the policy landed and said "off".
      expect(
        await screen.findByText(
          /continue with one of the sign-in providers below/i,
        ),
      ).toBeInTheDocument();
      expect(emailField()).toBeNull();
      expect(screen.queryByLabelText(/^password$/i)).toBeNull();
      expect(screen.queryByRole("link", { name: /forgot password/i })).toBeNull();
      expect(
        screen.queryByRole("link", { name: /create an account/i }),
      ).toBeNull();
    },
  );

  it("kill switch: banner + disabled password submit", async () => {
    server.use(
      clientPolicyHandler({ loginEnabled: false }),
      providersHandler([]),
    );
    renderLogin();
    expect(
      await screen.findByText(/login is temporarily disabled/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^sign in$/i })).toBeDisabled();
  });
});

describe("LoginPage — password path keeps one redirect policy (D4)", () => {
  const signInWithPassword = async () => {
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText(/^email$/i), "a@b.c");
    await user.type(screen.getByLabelText(/^password$/i), "hunter22hunter22");
    await user.click(screen.getByRole("button", { name: /^sign in$/i }));
  };

  it("lands on a safe ?next=", async () => {
    server.use(
      clientPolicyHandler(),
      providersHandler([]),
      http.post(LOGIN, () => HttpResponse.json(tokenBody)),
    );
    renderLogin("/login?next=%2Faccount%2Fsecurity");
    await signInWithPassword();
    expect(await screen.findByTestId("deeplink-location")).toHaveTextContent(
      "/account/security",
    );
  });

  it("falls back to /account on an unsafe ?next=", async () => {
    server.use(
      clientPolicyHandler(),
      providersHandler([]),
      http.post(LOGIN, () => HttpResponse.json(tokenBody)),
    );
    renderLogin("/login?next=https%3A%2F%2Fevil.example%2Fx");
    await signInWithPassword();
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
  });

  // Regression guard — GREEN before the change too: the MFA hand-off must
  // survive the extraction of MfaChallenge.
  it("still hands a partial login to MfaChallenge and completes through it", async () => {
    server.use(
      clientPolicyHandler(),
      providersHandler([]),
      http.post(LOGIN, () =>
        HttpResponse.json({
          success: true,
          requiresMfa: true,
          mfaToken: "ch-9",
          webauthnAvailable: false,
        }),
      ),
      http.post(url("/v1/auth/client/mfa/login/verify"), async ({ request }) => {
        const body = (await request.json()) as { challengeId: string };
        return body.challengeId === "ch-9"
          ? HttpResponse.json({ ...tokenBody, sessionId: "s1" })
          : HttpResponse.json({ detail: "wrong challenge" }, { status: 401 });
      }),
    );
    renderLogin();
    await signInWithPassword();
    // login.mfa.prompt, en.json.
    expect(
      await screen.findByText(/enter the 6-digit code/i),
    ).toBeInTheDocument();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/verification code/i), "123456");
    await user.click(screen.getByRole("button", { name: /^verify$/i }));
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
  });
});
```

(`afterEach`, `vi`, `waitFor`, `browserNavigation`, `OAUTH_RETURN_TO_KEY`, `waitForQuerySettled`, `START` and `PROVIDERS` are used by increment B — keep them; the typecheck runs after B.)

- [ ] **Step A2: Run it — RED.** `cd /home/tore/orkestra/frontend-client && npx vitest run src/pages/LoginPage.test.tsx`
Expected: the gate test finds the email field immediately (no loading gate yet); both password-off rows still render the form and never show the SSO subtitle; the kill-switch case passes (existing behaviour — a guard); the safe-next case lands through the old unvalidated path (may pass); the unsafe-next case never reaches `/account` (the old code navigates to `https://evil.example/x`, which no route matches); the MFA regression guard passes. At least four cases must be red: gate, the two password-off rows, unsafe next.

- [ ] **Step A3: Extract `MfaChallenge`** — create `frontend-client/src/components/MfaChallenge.tsx` with `LoginPage.tsx:259-342` verbatim, exported:

```tsx
import { useState, type FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { mfaLoginVerify, type MfaLoginVerifyResult } from "@/api/auth";

export interface MfaChallengeProps {
  mfaToken: string;
  onCancel: () => void;
  onSuccess: (result: MfaLoginVerifyResult) => void;
}

// The TOTP / backup-code step of a partial login. Shared by the password
// path (LoginPage) and the OAuth continuation (OAuthCallbackPage). The
// challenge id lives only in the caller's component state — never in
// router state or storage — and is one-shot on the backend.
export function MfaChallenge({
  mfaToken,
  onCancel,
  onSuccess,
}: MfaChallengeProps) {
  const { t } = useTranslation();
  const [code, setCode] = useState("");
  const [useBackup, setUseBackup] = useState(false);

  const verify = useMutation<MfaLoginVerifyResult, Error, void>({
    mutationFn: () =>
      mfaLoginVerify({
        challengeId: mfaToken,
        code: code.trim(),
        useBackup,
      }),
    onSuccess,
  });

  function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!code.trim()) return;
    verify.mutate();
  }

  return (
    <form onSubmit={onSubmit} noValidate className="space-y-5">
      <p className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800">
        {t("login.mfa.prompt")}
      </p>
      <div>
        <label
          htmlFor="mfa-code"
          className="mb-1 block text-sm font-medium text-slate-700"
        >
          {useBackup ? t("login.mfa.backupCode") : t("login.mfa.code")}
        </label>
        <input
          id="mfa-code"
          type="text"
          inputMode={useBackup ? "text" : "numeric"}
          autoComplete="one-time-code"
          autoFocus
          required
          value={code}
          onChange={(e) => setCode(e.target.value)}
          className="block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-base tracking-widest focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
        />
      </div>

      <label className="flex items-center gap-2 text-sm text-slate-700">
        <input
          type="checkbox"
          checked={useBackup}
          onChange={(e) => setUseBackup(e.target.checked)}
          className="h-4 w-4 rounded border-slate-300 text-slate-900 focus:ring-slate-500"
        />
        {t("login.mfa.useBackup")}
      </label>

      {verify.isError && (
        <p
          className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700"
          role="alert"
        >
          {verify.error.message}
        </p>
      )}

      <div className="flex gap-3">
        <button
          type="button"
          onClick={onCancel}
          className="flex-1 rounded-md border border-slate-300 px-4 py-2.5 text-sm font-medium text-slate-700 hover:bg-slate-50"
        >
          {t("login.mfa.cancel")}
        </button>
        <button
          type="submit"
          disabled={verify.isPending}
          className="flex-1 rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {verify.isPending ? t("login.mfa.submitting") : t("login.mfa.submit")}
        </button>
      </div>
    </form>
  );
}
```

- [ ] **Step A4: Rewrite `LoginPage.tsx` (increment A — no provider section yet).** Full new content (`EmailNotVerifiedNotice` is unchanged from `:199-251`):

```tsx
import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import {
  apiErrorCode,
  fetchAuthPolicy,
  login,
  passwordLoginUsable,
  type LoginResult,
} from "@/api/auth";
import { resendVerificationEmail } from "@/api/verifyEmail";
import { useAuth } from "@/auth/useAuth";
import { MfaChallenge } from "@/components/MfaChallenge";
import { DEFAULT_POST_LOGIN, sanitizeNext } from "@/lib/safeNext";

// Backend marks the "address not verified" 403 with code="auth.email_not_verified"
// (see auth/handlers/password_handler.go::mapPasswordError). We discriminate
// on the code, not on the localized detail string.
function isEmailNotVerified(err: unknown): boolean {
  return apiErrorCode(err) === "auth.email_not_verified";
}

// Two-state page: credentials (default) → mfa-required (after a partial
// login response carries requiresMfa=true). State lives in the local
// component because a navigation away should drop the in-flight
// challenge — the backend's mfaToken is short-lived and one-shot
// anyway. On full success (either branch) we stamp the in-memory token
// + session marker via AuthProvider.signIn and redirect to the validated
// ?next= or /account.
type Stage =
  | { name: "credentials" }
  | { name: "mfa"; mfaToken: string; webauthnAvailable: boolean };

const INPUT_CLASS =
  "block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500";
const PRIMARY_BUTTON_CLASS =
  "inline-flex w-full items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400";
const NOTICE_CLASS =
  "mb-6 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900";

export function LoginPage() {
  const { t } = useTranslation();
  const { signIn } = useAuth();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  // One redirect gate for both sign-in paths (lib/safeNext.ts): the deep
  // link RequireAuth stamped on ?next= is honoured only when it is a
  // same-origin relative path outside the auth routes; otherwise /account.
  // useSearchParams already decoded the value once — no second decode.
  const next = sanitizeNext(params.get("next"));
  const destination = next ?? DEFAULT_POST_LOGIN;

  const [stage, setStage] = useState<Stage>({ name: "credentials" });
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  // Public policy. fetchAuthPolicy never rejects (it falls open on any
  // failure — spec §4.10), so `policy === undefined` means exactly "still
  // loading": the page paints neither sign-in surface until it lands
  // (deviation D1) rather than flashing a password form on an SSO-only
  // surface.
  const { data: policy } = useQuery({
    queryKey: ["authPolicy"],
    queryFn: fetchAuthPolicy,
    staleTime: 30_000,
  });
  const loginEnabled = policy?.loginEnabled ?? true;
  const registrationEnabled = policy?.registrationEnabled ?? true;
  const passwordOn = passwordLoginUsable(policy);

  function complete(token: string) {
    signIn(token);
    navigate(destination, { replace: true });
  }

  const loginMutation = useMutation<LoginResult, Error, void>({
    mutationFn: () => login({ email: email.trim(), password }),
    onSuccess: (result) => {
      if (result.kind === "mfa_required") {
        setStage({
          name: "mfa",
          mfaToken: result.mfaToken,
          webauthnAvailable: result.webauthnAvailable,
        });
        return;
      }
      complete(result.accessToken);
    },
  });

  function onSubmitCredentials(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!email.trim() || !password) return;
    loginMutation.mutate();
  }

  if (stage.name === "mfa") {
    return (
      <section className="mx-auto max-w-md px-6 py-16">
        <h1 className="mb-6 text-3xl font-semibold tracking-tight">
          {t("login.title")}
        </h1>
        <MfaChallenge
          mfaToken={stage.mfaToken}
          onCancel={() => setStage({ name: "credentials" })}
          onSuccess={(result) => complete(result.accessToken)}
        />
      </section>
    );
  }

  if (policy === undefined) {
    return (
      <section className="mx-auto max-w-md px-6 py-16">
        <h1 className="mb-2 text-3xl font-semibold tracking-tight">
          {t("login.title")}
        </h1>
        <p role="status" className="text-sm text-slate-500">
          {t("loading")}
        </p>
      </section>
    );
  }

  return (
    <section className="mx-auto max-w-md px-6 py-16">
      <h1 className="mb-2 text-3xl font-semibold tracking-tight">
        {t("login.title")}
      </h1>
      <p className="mb-8 text-slate-600">
        {passwordOn ? t("login.subtitle") : t("login.subtitleSso")}
      </p>

      {!loginEnabled && (
        <div className={NOTICE_CLASS} role="alert">
          {t("login.disabled")}
        </div>
      )}

      {passwordOn && (
        <form onSubmit={onSubmitCredentials} noValidate className="space-y-5">
          <div>
            <label
              htmlFor="email"
              className="mb-1 block text-sm font-medium text-slate-700"
            >
              {t("login.email")}
            </label>
            <input
              id="email"
              type="email"
              autoComplete="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className={INPUT_CLASS}
            />
          </div>
          <div>
            <label
              htmlFor="password"
              className="mb-1 block text-sm font-medium text-slate-700"
            >
              {t("login.password")}
            </label>
            <input
              id="password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className={INPUT_CLASS}
            />
          </div>

          {loginMutation.isError &&
            !isEmailNotVerified(loginMutation.error) && (
              <p
                className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700"
                role="alert"
              >
                {loginMutation.error.message}
              </p>
            )}

          {loginMutation.isError && isEmailNotVerified(loginMutation.error) && (
            <EmailNotVerifiedNotice email={email.trim()} />
          )}

          <button
            type="submit"
            disabled={loginMutation.isPending || !loginEnabled}
            className={PRIMARY_BUTTON_CLASS}
          >
            {loginMutation.isPending
              ? t("login.submitting")
              : t("login.submit")}
          </button>

          <div className="flex items-center justify-between text-sm">
            <Link
              to="/forgot-password"
              className="text-slate-600 underline hover:text-slate-900"
            >
              {t("login.forgot")}
            </Link>
            {registrationEnabled && (
              <Link
                to="/signup"
                className="text-slate-600 underline hover:text-slate-900"
              >
                {t("login.signupLink")}
              </Link>
            )}
          </div>
        </form>
      )}
    </section>
  );
}

// Inline panel rendered when login returns code="auth.email_not_verified".
// The email field already has a value (we just submitted it), so we
// don't ask the user to retype — one click triggers the resend.
//
// The 60s cooldown is a UX nudge against rapid clicks; the real abuse
// gate is the shared rate limiter on the backend (per-IP + per-email
// buckets, same surface that protects login). The success message is
// neutral by design: the backend always returns 200, so we cannot tell
// the user whether the address was actually known.
interface EmailNotVerifiedNoticeProps {
  email: string;
}

function EmailNotVerifiedNotice({ email }: EmailNotVerifiedNoticeProps) {
  const { t } = useTranslation();
  const [cooldownLeft, setCooldownLeft] = useState(0);

  const resend = useMutation<unknown, Error, string>({
    mutationFn: (addr: string) => resendVerificationEmail(addr),
    onSuccess: () => setCooldownLeft(60),
  });

  useEffect(() => {
    if (cooldownLeft <= 0) return;
    const id = window.setTimeout(() => setCooldownLeft((s) => s - 1), 1000);
    return () => window.clearTimeout(id);
  }, [cooldownLeft]);

  const canSend = !!email && !resend.isPending && cooldownLeft === 0;

  return (
    <div
      className="rounded-md border border-amber-200 bg-amber-50 px-3 py-3 text-sm text-amber-900"
      role="alert"
    >
      <p className="font-medium">{t("login.notVerified.title")}</p>
      <p className="mt-1 text-amber-800">{t("login.notVerified.body")}</p>

      {resend.isSuccess ? (
        <p
          className="mt-3 rounded-md bg-emerald-50 px-3 py-2 text-emerald-700"
          role="status"
        >
          {t("login.notVerified.resendDone")}
        </p>
      ) : (
        <button
          type="button"
          disabled={!canSend}
          onClick={() => email && resend.mutate(email)}
          className="mt-3 inline-flex items-center justify-center rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          {cooldownLeft > 0
            ? t("login.notVerified.resendCooldown", { seconds: cooldownLeft })
            : resend.isPending
              ? t("login.notVerified.resendSending")
              : t("login.notVerified.resendCta")}
        </button>
      )}
    </div>
  );
}
```

- [ ] **Step A5: Run it — GREEN.** `npx vitest run src/pages/LoginPage.test.tsx` → 8 PASS (5 gate/password-off incl. the two rows + 3 password path).

#### Increment B — provider section, no-method alert, OAuth start

- [ ] **Step B1: Append the failing tests** to `LoginPage.test.tsx`:

```tsx
describe("LoginPage — provider section (spec §4.10 loading / error / empty / list)", () => {
  it("paints no provider button until /policy has resolved either", async () => {
    const releasePolicy = deferredJson("/v1/auth/client/policy", openPolicy);
    server.use(providersHandler(["google"]));
    renderLogin();
    expect(await screen.findByText(/^loading…$/i)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Continue with Google" }),
    ).toBeNull();
    releasePolicy();
    expect(
      await screen.findByRole("button", { name: "Continue with Google" }),
    ).toBeInTheDocument();
  });

  it("password on + providers: divider and one button per provider", async () => {
    server.use(clientPolicyHandler(), providersHandler(["google", "github"]));
    renderLogin();
    expect(
      await screen.findByRole("button", { name: "Continue with Google" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Continue with GitHub" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/or continue with/i)).toBeInTheDocument();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
  });

  it("password off + providers: buttons only, no divider, no alert", async () => {
    server.use(
      clientPolicyHandler({ passwordLoginEnabled: false }),
      providersHandler(["google"]),
    );
    renderLogin();
    expect(
      await screen.findByRole("button", { name: "Continue with Google" }),
    ).toBeInTheDocument();
    expect(emailField()).toBeNull();
    expect(screen.queryByText(/or continue with/i)).toBeNull();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
  });

  it("password off + providers resolved empty → the no-method alert", async () => {
    server.use(
      clientPolicyHandler({ passwordLoginEnabled: false }),
      providersHandler([]),
    );
    renderLogin();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /no sign-in method is currently available/i,
    );
    expect(emailField()).toBeNull();
  });

  it("password off + providers 503 → the retryable error, never the alert; retry recovers", async () => {
    let calls = 0;
    server.use(
      clientPolicyHandler({ passwordLoginEnabled: false }),
      http.get(PROVIDERS, () => {
        calls++;
        return calls === 1
          ? HttpResponse.json(
              { code: "auth.policy_unavailable" },
              { status: 503 },
            )
          : HttpResponse.json({ providers: ["google"] });
      }),
    );
    renderLogin();
    expect(
      await screen.findByText(/could not load the sign-in providers/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: /try again/i }));
    expect(
      await screen.findByRole("button", { name: "Continue with Google" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/could not load the sign-in providers/i),
    ).toBeNull();
    expect(calls).toBe(2);
  });

  it("password off + a malformed providers body → the retryable error, never the alert (B3)", async () => {
    server.use(
      clientPolicyHandler({ passwordLoginEnabled: false }),
      http.get(PROVIDERS, () => HttpResponse.json({})),
    );
    renderLogin();
    expect(
      await screen.findByText(/could not load the sign-in providers/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
    expect(
      screen.getByRole("button", { name: /try again/i }),
    ).toBeInTheDocument();
  });

  it("password on + zero providers → the plain form, no section, no divider, no alert", async () => {
    server.use(clientPolicyHandler(), providersHandler([]));
    const { queryClient } = renderLogin();
    expect(await screen.findByLabelText(/^email$/i)).toBeInTheDocument();
    // The tree is identical before and after the providers query when the
    // list is empty and the method is on — anchor on the cache entry.
    await waitForQuerySettled(queryClient, ["oauthProviders"]);
    expect(screen.queryByText(/or continue with/i)).toBeNull();
    expect(screen.queryByText(/loading sign-in options/i)).toBeNull();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
  });

  it("shows the providers loading state distinctly, then the buttons", async () => {
    server.use(clientPolicyHandler());
    const releaseProviders = deferredJson("/v1/auth/client/providers", {
      providers: ["discord"],
    });
    renderLogin();
    expect(
      await screen.findByText(/loading sign-in options/i),
    ).toBeInTheDocument();
    releaseProviders();
    expect(
      await screen.findByRole("button", { name: "Continue with Discord" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/loading sign-in options/i)).toBeNull();
  });

  it("kill switch hides the provider section", async () => {
    server.use(
      clientPolicyHandler({ loginEnabled: false }),
      providersHandler(["google"]),
    );
    const { queryClient } = renderLogin();
    expect(
      await screen.findByText(/login is temporarily disabled/i),
    ).toBeInTheDocument();
    await waitForQuerySettled(queryClient, ["oauthProviders"]);
    expect(
      screen.queryByRole("button", { name: "Continue with Google" }),
    ).toBeNull();
    expect(screen.queryByText(/no sign-in method/i)).toBeNull();
  });
});

describe("LoginPage — OAuth start (spec §4.10)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("starts the flow with the allowlisted provider, stashes the validated next and leaves", async () => {
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    const bodies: unknown[] = [];
    server.use(
      clientPolicyHandler(),
      providersHandler(["google", "github"]),
      http.post(START, async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({
          authUrl: "https://idp.example/authorize",
          state: "s",
        });
      }),
    );
    renderLogin("/login?next=%2Faccount%2Fsecurity");
    await userEvent
      .setup()
      .click(await screen.findByRole("button", { name: "Continue with GitHub" }));
    await waitFor(() =>
      expect(assign).toHaveBeenCalledWith("https://idp.example/authorize"),
    );
    expect(bodies).toEqual([{ provider: "github" }]);
    expect(
      JSON.parse(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)!).target,
    ).toBe("/account/security");
    expect(
      screen.getByRole("button", { name: /redirecting to github/i }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Continue with Google" }),
    ).toBeDisabled();
  });

  it("an unsafe ?next= is not stashed (and a stale record is dropped)", async () => {
    vi.spyOn(browserNavigation, "assign").mockImplementation(() => {});
    server.use(
      clientPolicyHandler(),
      providersHandler(["google"]),
      http.post(START, () =>
        HttpResponse.json({ authUrl: "https://idp.example/a", state: "s" }),
      ),
    );
    sessionStorage.setItem(
      OAUTH_RETURN_TO_KEY,
      JSON.stringify({ target: "/account", createdAt: Date.now() }),
    );
    renderLogin("/login?next=%2F%2Fevil.example");
    await userEvent
      .setup()
      .click(await screen.findByRole("button", { name: "Continue with Google" }));
    await waitFor(() =>
      expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull(),
    );
  });

  it("maps a 403 auth.oauth_provider_disabled to copy, re-enables the buttons, never renders the detail", async () => {
    const assign = vi
      .spyOn(browserNavigation, "assign")
      .mockImplementation(() => {});
    server.use(
      clientPolicyHandler(),
      providersHandler(["apple"]),
      http.post(START, () =>
        HttpResponse.json(
          { code: "auth.oauth_provider_disabled", detail: "refused-by-backend" },
          { status: 403 },
        ),
      ),
    );
    renderLogin();
    await userEvent
      .setup()
      .click(await screen.findByRole("button", { name: "Continue with Apple" }));
    expect(
      await screen.findByText(/apple sign-in is not available on this surface/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/refused-by-backend/)).toBeNull();
    expect(
      screen.getByRole("button", { name: "Continue with Apple" }),
    ).toBeEnabled();
    expect(assign).not.toHaveBeenCalled();
  });

  it("maps a 503 auth.policy_unavailable to the shared retryable copy", async () => {
    vi.spyOn(browserNavigation, "assign").mockImplementation(() => {});
    server.use(
      clientPolicyHandler(),
      providersHandler(["google"]),
      http.post(START, () =>
        HttpResponse.json({ code: "auth.policy_unavailable" }, { status: 503 }),
      ),
    );
    renderLogin();
    await userEvent
      .setup()
      .click(await screen.findByRole("button", { name: "Continue with Google" }));
    expect(
      await screen.findByText(/sign-in policy is temporarily unavailable/i),
    ).toBeInTheDocument();
  });
});
```

- [ ] **Step B2: Run it — RED.** `npx vitest run src/pages/LoginPage.test.tsx -t "provider section|OAuth start"` → every case red: no provider button, no alert, no loading copy exist yet (`findByRole` times out). Increment A's 8 cases stay green.

- [ ] **Step B3: Implement increment B** — edits to `LoginPage.tsx`:

Imports — replace the `@tanstack/react-query` and `@/api/auth` imports and add the two `lib` imports:

```tsx
import {
  useMutation,
  useQuery,
  type UseQueryResult,
} from "@tanstack/react-query";
…
import {
  apiErrorCode,
  fetchAuthPolicy,
  fetchOAuthProviders,
  initiateOAuthLogin,
  login,
  passwordLoginUsable,
  type LoginResult,
} from "@/api/auth";
…
import {
  OAUTH_PROVIDER_LABELS,
  type OAuthProviderName,
} from "@/lib/oauthProviders";
```

After the policy query (after `const passwordOn = passwordLoginUsable(policy);`) add:

```tsx
  // Providers the backend will accept a login from right now
  // (GET /v1/auth/client/providers). Runs in parallel with the policy
  // read. A rejection — 503, network failure, malformed body — is a
  // retryable error state, never "no method".
  const providers = useQuery({
    queryKey: ["oauthProviders"],
    queryFn: ({ signal }) => fetchOAuthProviders(signal),
    staleTime: 30_000,
  });
```

After the `if (policy === undefined) { … }` early return add:

```tsx
  // The no-method alert needs three settled facts: kill switch off,
  // persisted password policy false/null, and a provider list that has
  // RESOLVED empty. A provider-query error keeps its own retryable state.
  const noMethod =
    loginEnabled &&
    !passwordOn &&
    providers.isSuccess &&
    providers.data.length === 0;
```

In the JSX, after the kill-switch notice add the alert, and after the `{passwordOn && (<form>…</form>)}` block add the section:

```tsx
      {noMethod && (
        <div className={NOTICE_CLASS} role="alert">
          {t("login.noMethod")}
        </div>
      )}
```

```tsx
      {loginEnabled && (
        <OAuthProviderButtons
          providers={providers}
          next={next}
          showDivider={passwordOn}
        />
      )}
```

Append at the end of the file:

```tsx
// startErrorKey maps the backend `code` of a failed OAuth start to copy
// (deviation D6). Anything unmapped is the generic key — the backend
// detail string is never rendered.
function startErrorKey(e: unknown): string {
  switch (apiErrorCode(e)) {
    case "auth.oauth_provider_disabled":
      return "login.oauth.providerDisabled";
    case "auth.policy_unavailable":
      return "error.policyUnavailable";
    case "auth.login_disabled":
      return "login.disabled";
    default:
      return "login.oauth.startFailed";
  }
}

interface OAuthProviderButtonsProps {
  providers: UseQueryResult<OAuthProviderName[], Error>;
  next: string | null;
  // False when no password form renders above: the "or continue with"
  // divider would have nothing to divide from.
  showDivider: boolean;
}

// The provider section of the login page. Three distinct states (spec
// §4.10): loading, a retryable error, and the resolved list — an empty
// list renders nothing here (the page owns the no-method alert). The
// buttons are text-only on purpose: brand names, no icon library.
function OAuthProviderButtons({
  providers,
  next,
  showDivider,
}: OAuthProviderButtonsProps) {
  const { t } = useTranslation();
  const [starting, setStarting] = useState<OAuthProviderName | null>(null);
  const [startError, setStartError] = useState<{
    key: string;
    provider: OAuthProviderName;
  } | null>(null);

  async function start(provider: OAuthProviderName) {
    setStarting(provider);
    setStartError(null);
    try {
      // Stashes the validated `next`, then leaves the SPA — on success
      // there is nothing to reset here.
      await initiateOAuthLogin(provider, next);
    } catch (e) {
      setStartError({ key: startErrorKey(e), provider });
      setStarting(null);
    }
  }

  const divider = showDivider ? (
    <p className="my-6 text-center text-xs uppercase tracking-wide text-slate-400">
      {t("login.oauth.divider")}
    </p>
  ) : null;

  if (providers.isPending) {
    return (
      <>
        {divider}
        <p role="status" className="text-center text-sm text-slate-500">
          {t("login.oauth.loading")}
        </p>
      </>
    );
  }

  if (providers.isError) {
    return (
      <>
        {divider}
        <div
          className="rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900"
          role="alert"
        >
          <p>{t("login.oauth.loadError")}</p>
          <button
            type="button"
            onClick={() => void providers.refetch()}
            className="mt-2 text-sm font-medium underline hover:text-amber-950"
          >
            {t("login.oauth.retry")}
          </button>
        </div>
      </>
    );
  }

  if (providers.data.length === 0) return null;

  return (
    <>
      {divider}
      <div className="space-y-3">
        {startError && (
          <p
            className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700"
            role="alert"
          >
            {t(startError.key, {
              provider: OAUTH_PROVIDER_LABELS[startError.provider],
            })}
          </p>
        )}
        {providers.data.map((provider) => {
          const label = OAUTH_PROVIDER_LABELS[provider];
          return (
            <button
              key={provider}
              type="button"
              disabled={starting !== null}
              onClick={() => void start(provider)}
              className="inline-flex w-full items-center justify-center rounded-md border border-slate-300 bg-white px-4 py-2.5 text-sm font-medium text-slate-900 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {starting === provider
                ? t("login.oauth.redirecting", { provider: label })
                : t("login.oauth.continueWith", { provider: label })}
            </button>
          );
        })}
      </div>
    </>
  );
}
```

- [ ] **Step B4: Run it — GREEN, then the gates.** `npx vitest run src/pages/LoginPage.test.tsx` → 21 PASS (8 + 9 + 4); then `npm run typecheck && npm run lint -- --max-warnings 0 && npm test` → all exit 0.

#### Close the task

- [ ] **Step C1: Documentation (same commit)**

`frontend-client/CLAUDE.md`:
- Directory layout `components/` block (`:59-62`): add `│   │   ├── MfaChallenge.tsx        # TOTP / backup-code step shared by LoginPage and OAuthCallbackPage (challenge id in caller state only)`.
- "How auth works" item 4 (added in Task 3): append `On the login page this means: nothing paints until \`/policy\` resolves; with the method on, the password form renders above an "or continue with" provider section; with it off (\`false\` or \`null\`) only the providers render, the forgot/sign-up links disappear, and a provider list that *resolved* empty shows the no-sign-in-method notice — a provider-query error (503, network, malformed body) is a retryable alert, never that notice. The kill switch (\`loginEnabled=false\`) keeps the maintenance banner and hides the provider section.`

`docs/site/architecture/authentication-flow.mdx:179` — replace the paragraph with:

```
The same policy is the reason the login page can render honestly rather than guessing: `GET /v1/auth/{tier}/policy` carries `passwordLoginEnabled` for the surface, alongside `passwordLoginBreakGlassEffective` — only ever true on the operator endpoint, where it tells the console to render the labelled emergency form. Both SPAs hide the password form on an SSO-only surface instead of showing one that would 403 on submit: the operator console renders its emergency form only under break-glass, and the client SPA — whose `passwordLoginUsable(policy)` treats `false` and `null` as off — lists the surface's usable providers from `GET /v1/auth/client/providers` in the form's place, or a no-sign-in-method notice when that list resolves empty.
```

`docs/site/modules/core/auth.mdx:86-93` — replace the paragraph with:

```
The unauthenticated pages read the resulting state from `GET
/v1/auth/{tier}/policy`, which carries `passwordLoginEnabled` for the
surface, so the login page hides the password form instead of showing one
that would 403 on submit. Both SPAs do this: the operator console renders
its labelled emergency form only under break-glass, and the client SPA
replaces the password form with the surface's usable OAuth providers (or a
no-sign-in-method notice when none is configured).
```

- [ ] **Step C2: Commit**

```bash
cd /home/tore/orkestra && npx --prefix frontend-client prettier --write frontend-client/src/pages/LoginPage.tsx frontend-client/src/pages/LoginPage.test.tsx frontend-client/src/components/MfaChallenge.tsx frontend-client/src/locales/en.json frontend-client/src/locales/it.json >/dev/null
git add frontend-client/src/pages/LoginPage.tsx frontend-client/src/pages/LoginPage.test.tsx frontend-client/src/components/MfaChallenge.tsx frontend-client/src/locales/en.json frontend-client/src/locales/it.json frontend-client/CLAUDE.md docs/site/architecture/authentication-flow.mdx docs/site/modules/core/auth.mdx
git commit -m "feat(frontend-client): SSO-capable login page — policy gate, provider buttons, no-method alert

The page paints nothing until /policy resolves; passwordLoginEnabled
false/null hides the password form and its links; the provider section
has distinct loading / retryable-error / resolved states (a 503, a network
failure and a malformed body are all the error) and the no-method alert
needs a list that resolved empty; OAuth start stashes the validated next
and leaves through browserNavigation; the password path's ?next= goes
through the same sanitizeNext (it was unvalidated and double-decoded).
MfaChallenge moves to src/components for its second use (spec §4.10)." -m "Claude-Session: ${CLAUDE_SESSION:?export CLAUDE_SESSION=<session URL> first}"
```

### Task 6: `OAuthCallbackPage` — success and failures (A), MFA in memory (B), the route and an integrated test through `App` (C)

The landing page of the relay's redirect (F4), in three RED → GREEN cycles. **A:** parse the closed contract once, scrub in the first passive effect before any request, take-and-delete the return target on every outcome, bootstrap on success (signed-out is a login error; 503 **and a transport failure** offer retry — round-1 B2), render only mapped copy for failures. **B:** the MFA continuation renders the shared `MfaChallenge` from component memory. **C:** the `/auth/callback` route in `App.tsx` and a test that mounts the page through the real route table with `Layout` — the reviewer's round-1 request — so the order scrub → bootstrap → layout queries is proven on the real tree.

**Files:**
- Create: `frontend-client/src/pages/OAuthCallbackPage.tsx`
- Modify: `frontend-client/src/App.tsx:7` (import) and `:26` (route)
- Modify: `frontend-client/src/test/handlers.ts` (`meHandler`)
- Modify: `frontend-client/src/locales/en.json`, `frontend-client/src/locales/it.json` (new top-level `oauth` block)
- Test: `frontend-client/src/pages/OAuthCallbackPage.test.tsx` (grown per increment), `frontend-client/src/App.test.tsx`
- Modify: `frontend-client/CLAUDE.md` (new "OAuth login (web)" subsection under "How auth works"; two "Don't" bullets), `docs/site/operating/oauth-providers.mdx:255`

**Interfaces:**
- Consumes: Task 2's `parseOAuthCallback`, `OAuthCallbackOutcome`, `takeOAuthReturnTo`, `DEFAULT_POST_LOGIN`; Task 4's `useAuth().bootstrapFromRefreshCookie` / `signIn`; Task 5's `MfaChallenge`.
- Produces: the route `/auth/callback`; i18n keys `oauth.callback.*`; `meHandler()` in `@/test/handlers`.

- [ ] **Step 0: Add the i18n keys (both bundles) and `meHandler`**

`frontend-client/src/locales/en.json` — add a top-level block after `"login"` (before `"forgot"`):

```json
  "oauth": {
    "callback": {
      "verifying": "Completing sign-in…",
      "failureTitle": "Sign-in failed",
      "sessionSignedOut": "Sign-in completed, but no session could be established. Please sign in again.",
      "sessionUnavailable": "The server could not confirm your session right now. Try again in a moment.",
      "retry": "Try again",
      "backToLogin": "Back to sign in",
      "errors": {
        "accessDenied": "Sign-in was cancelled at the identity provider.",
        "signupDisabled": "Sign-up is currently invitation-only. Contact an administrator.",
        "linkDisabled": "An account with this email already exists and automatic linking is off. Contact an administrator.",
        "emailUnverified": "The identity provider has not verified this email address, so it cannot be used to sign in.",
        "providerUnavailable": "This sign-in provider is temporarily unavailable. Try again later.",
        "loginFailed": "Sign-in failed. Please try again."
      }
    }
  },
```

`frontend-client/src/locales/it.json` — same position:

```json
  "oauth": {
    "callback": {
      "verifying": "Completamento dell'accesso…",
      "failureTitle": "Accesso non riuscito",
      "sessionSignedOut": "Accesso completato, ma non è stato possibile stabilire una sessione. Accedi di nuovo.",
      "sessionUnavailable": "Il server non riesce a confermare la sessione in questo momento. Riprova tra poco.",
      "retry": "Riprova",
      "backToLogin": "Torna all'accesso",
      "errors": {
        "accessDenied": "L'accesso è stato annullato presso il provider di identità.",
        "signupDisabled": "La registrazione è attualmente solo su invito. Contatta un amministratore.",
        "linkDisabled": "Esiste già un account con questa email e il collegamento automatico è disattivato. Contatta un amministratore.",
        "emailUnverified": "Il provider di identità non ha verificato questo indirizzo email, quindi non può essere usato per accedere.",
        "providerUnavailable": "Questo provider di accesso non è al momento disponibile. Riprova più tardi.",
        "loginFailed": "Accesso non riuscito. Riprova."
      }
    }
  },
```

`frontend-client/src/test/handlers.ts` — append:

```ts
// GET /v1/auth/client/me — the minimum MeResponse Layout (header avatar +
// nav) and AccountPage read once a session exists.
export const meHandler = (overrides: Record<string, unknown> = {}) =>
  http.get(url("/v1/auth/client/me"), () =>
    HttpResponse.json({
      id: "u-1",
      email: "user@example.com",
      fullName: "Client User",
      emailVerified: true,
      isActive: true,
      ...overrides,
    }),
  );
```

Run: `cd /home/tore/orkestra/frontend-client && npx vitest run src/locales` → PASS.

#### Increment A — success and failures

- [ ] **Step A1: Write the failing tests** — create `frontend-client/src/pages/OAuthCallbackPage.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { Route, Routes, useLocation } from "react-router";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { RequireAuth } from "@/auth/RequireAuth";
import { hasSessionMarker } from "@/auth/sessionMarker";
import {
  OAUTH_RETURN_TO_KEY,
  OAUTH_RETURN_TO_TTL_MS,
} from "@/lib/oauthReturnTo";
import { OAuthCallbackPage } from "@/pages/OAuthCallbackPage";
import { url } from "@/test/handlers";
import { renderWithProviders } from "@/test/render";
import { server } from "@/test/server";

const REFRESH = url("/v1/auth/client/refresh-cookie");
const VERIFY = url("/v1/auth/client/mfa/login/verify");
const tokenBody = { accessToken: "at-1", tokenType: "Bearer", expiresIn: 900 };
const GENERIC = /^sign-in failed\. please try again\.$/i;

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

// Every value in web storage, joined — the assertion surface for "no
// access token or challenge id is ever persisted".
const storageDump = (): string =>
  [
    ...Array.from(
      { length: localStorage.length },
      (_, i) => localStorage.getItem(localStorage.key(i)!) ?? "",
    ),
    ...Array.from(
      { length: sessionStorage.length },
      (_, i) => sessionStorage.getItem(sessionStorage.key(i)!) ?? "",
    ),
  ].join("\n");

// A refresh endpoint the test releases by hand. At the moment the request
// arrives it records the marker AND the router location the page had
// committed (read from the Probe's DOM) — the order proof for "scrub before
// the first request": both must already hold when the handler runs, not
// merely become true at some point.
const deferredRefresh = () => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let hits = 0;
  let markerAtRequest: boolean | null = null;
  let locationAtRequest: string | null = null;
  server.use(
    http.post(REFRESH, async () => {
      hits++;
      markerAtRequest = hasSessionMarker();
      locationAtRequest =
        screen.queryByTestId("cb-location")?.textContent ?? null;
      await gate;
      return HttpResponse.json(tokenBody);
    }),
  );
  return {
    release,
    hits: () => hits,
    markerAtRequest: () => markerAtRequest,
    locationAtRequest: () => locationAtRequest,
  };
};

// A refresh endpoint whose first answer is `first`, then a token.
const refreshFailingOnce = (first: () => Response) => {
  let calls = 0;
  server.use(
    http.post(REFRESH, () => {
      calls++;
      return calls === 1 ? first() : HttpResponse.json(tokenBody);
    }),
  );
  return () => calls;
};

const renderCallback = (search: string, hash = "") =>
  renderWithProviders(
    <Routes>
      <Route
        path="/auth/callback"
        element={
          <>
            <OAuthCallbackPage />
            <Probe label="cb" />
          </>
        }
      />
      <Route
        path="/account"
        element={
          <RequireAuth>
            <Probe label="account" />
          </RequireAuth>
        }
      />
      <Route
        path="/account/security"
        element={
          <RequireAuth>
            <Probe label="deeplink" />
          </RequireAuth>
        }
      />
      <Route path="/login" element={<Probe label="login" />} />
    </Routes>,
    { routerEntries: [{ pathname: "/auth/callback", search, hash }] },
  );

const stash = (target: string, createdAt = Date.now()) =>
  sessionStorage.setItem(
    OAUTH_RETURN_TO_KEY,
    JSON.stringify({ target, createdAt }),
  );

describe("OAuthCallbackPage — success", () => {
  it("scrubs the URL before the refresh request, stamps the marker first, then lands authenticated on /account", async () => {
    const refresh = deferredRefresh();
    renderCallback("?success=true&provider=google");
    await waitFor(() =>
      expect(screen.getByTestId("cb-location")).toHaveTextContent(
        /^\/auth\/callback$/,
      ),
    );
    await waitFor(() => expect(refresh.hits()).toBe(1));
    // Order, not coincidence: when the request left, the page had already
    // committed the bare path and the marker was already stamped.
    expect(refresh.locationAtRequest()).toBe("/auth/callback");
    expect(refresh.markerAtRequest()).toBe(true);
    // Nothing navigates while the bootstrap pends, and the URL stays clean.
    expect(screen.getByTestId("cb-location")).toHaveTextContent(
      /^\/auth\/callback$/,
    );
    expect(screen.queryByTestId("account-location")).toBeNull();
    refresh.release();
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
    expect(screen.queryByTestId("login-location")).toBeNull();
    expect(storageDump()).not.toContain("at-1");
  });

  it("honours a fresh stashed return target and deletes it in the first effect", async () => {
    stash("/account/security");
    const refresh = deferredRefresh();
    renderCallback("?success=true&provider=github");
    // Taken in the first effect — already gone once render returns.
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
    refresh.release();
    expect(await screen.findByTestId("deeplink-location")).toHaveTextContent(
      "/account/security",
    );
  });

  it("ignores a stale stashed return target", async () => {
    stash("/account/security", Date.now() - OAUTH_RETURN_TO_TTL_MS - 1);
    const refresh = deferredRefresh();
    renderCallback("?success=true&provider=github");
    refresh.release();
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
  });

  it("treats a signed-out bootstrap as a login error, never a protected route, and clears the marker", async () => {
    server.use(
      http.post(REFRESH, () =>
        HttpResponse.json({ detail: "no session" }, { status: 401 }),
      ),
    );
    renderCallback("?success=true&provider=google");
    expect(
      await screen.findByText(/no session could be established/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /back to sign in/i }),
    ).toHaveAttribute("href", "/login");
    expect(screen.queryByTestId("account-location")).toBeNull();
    expect(hasSessionMarker()).toBe(false);
  });

  it("keeps the bootstrap state and offers retry when the refresh endpoint answers 503", async () => {
    const calls = refreshFailingOnce(() =>
      HttpResponse.json(
        { code: "session_enforcement_unavailable" },
        { status: 503 },
      ),
    );
    renderCallback("?success=true&provider=google");
    const retry = await screen.findByRole("button", { name: /try again/i });
    expect(hasSessionMarker()).toBe(true);
    expect(screen.queryByTestId("account-location")).toBeNull();
    await userEvent.setup().click(retry);
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
    expect(calls()).toBe(2);
  });

  it("a network failure is the same retryable state — never a stuck spinner (B2)", async () => {
    const calls = refreshFailingOnce(() => HttpResponse.error());
    renderCallback("?success=true&provider=google");
    const retry = await screen.findByRole("button", { name: /try again/i });
    expect(screen.queryByText(/completing sign-in/i)).toBeNull();
    expect(hasSessionMarker()).toBe(true);
    await userEvent.setup().click(retry);
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
    expect(calls()).toBe(2);
  });
});

describe("OAuthCallbackPage — failures (closed contract)", () => {
  it("takes the return target on an error outcome too", async () => {
    stash("/account/security");
    renderCallback("?success=false&error=oauth_access_denied");
    expect(
      await screen.findByText(/cancelled at the identity provider/i),
    ).toBeInTheDocument();
    expect(sessionStorage.getItem(OAUTH_RETURN_TO_KEY)).toBeNull();
  });

  it("renders the mapped copy for an allowlisted code, never the raw code, on a clean URL", async () => {
    renderCallback("?success=false&error=oauth_signup_disabled");
    expect(await screen.findByText(/invitation-only/i)).toBeInTheDocument();
    expect(screen.queryByText(/oauth_signup_disabled/)).toBeNull();
    await waitFor(() =>
      expect(screen.getByTestId("cb-location")).toHaveTextContent(
        /^\/auth\/callback$/,
      ),
    );
    expect(
      screen.getByRole("link", { name: /back to sign in/i }),
    ).toHaveAttribute("href", "/login");
  });

  it("collapses an unknown code to the generic copy and never renders raw URL text", async () => {
    renderCallback("?success=false&error=%3Cscript%3Ealert(1)%3C%2Fscript%3E");
    expect(await screen.findByText(GENERIC)).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("<script>");
    expect(document.body.textContent).not.toContain("alert(1)");
  });

  it("treats an ambiguous payload (MFA fragment + query outcome) as the generic failure", async () => {
    renderCallback(
      "?success=true&provider=google",
      "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false",
    );
    expect(await screen.findByText(GENERIC)).toBeInTheDocument();
    expect(screen.queryByText(/enter the 6-digit code/i)).toBeNull();
    expect(screen.queryByTestId("account-location")).toBeNull();
    expect(storageDump()).not.toContain("ch-1");
  });

  it("does not issue a refresh on a failure outcome", async () => {
    let hits = 0;
    server.use(
      http.post(REFRESH, () => {
        hits++;
        return HttpResponse.json(tokenBody);
      }),
    );
    renderCallback("?success=false&error=oauth_login_failed");
    expect(await screen.findByText(GENERIC)).toBeInTheDocument();
    expect(hits).toBe(0);
    expect(hasSessionMarker()).toBe(false);
  });
});
```

(`VERIFY` is used by increment B — keep it.)

- [ ] **Step A2: Run it — RED.** `cd /home/tore/orkestra/frontend-client && npx vitest run src/pages/OAuthCallbackPage.test.tsx` → `Failed to resolve import "@/pages/OAuthCallbackPage"`.

- [ ] **Step A3: Implement increment A** — `frontend-client/src/pages/OAuthCallbackPage.tsx`:

```tsx
import { useEffect, useRef, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";

import { useAuth } from "@/auth/useAuth";
import {
  parseOAuthCallback,
  type OAuthCallbackOutcome,
} from "@/lib/oauthCallback";
import { takeOAuthReturnTo } from "@/lib/oauthReturnTo";
import { DEFAULT_POST_LOGIN } from "@/lib/safeNext";

type Phase = "working" | "signedOut" | "unavailable" | "error";

const FAILURE_CLASS =
  "mb-6 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-left text-sm text-red-800";
const LINK_CLASS = "text-sm text-slate-600 underline hover:text-slate-900";

/**
 * Landing page of the backend's OAuth callback redirect — for the client
 * tier, of the relay endpoint on the API host, which has already set the
 * refresh cookie on a success (handlers/oauth_callback_redirect.go; a
 * CLOSED contract parsed by lib/oauthCallback):
 *   ?success=true&provider=<p>                  → adopt the refresh cookie (bootstrapFromRefreshCookie)
 *   ?success=false&error=<allowlisted code>     → mapped copy, never raw text
 *   #requiresMfa=true&mfaToken=<id>&webauthn…   → render MfaChallenge locally
 *
 * The URL is parsed ONCE on the first render (pure, into a ref) and
 * scrubbed in the first passive effect — before any request — so neither
 * the one-shot challenge id nor the outcome survives in history, a
 * referrer or a reload. The return target is taken-and-deleted in that
 * same effect on every outcome. Success navigates only after the refresh
 * cookie produced an access token: signed-out is a login error; a 503 or
 * a transport failure keeps the page and offers retry (spec §4.10,
 * §5 #23/#27).
 */
export function OAuthCallbackPage() {
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const { bootstrapFromRefreshCookie } = useAuth();

  // Parsed once, in component memory only. Pure — no storage touched here.
  const outcomeRef = useRef<OAuthCallbackOutcome | null>(null);
  if (outcomeRef.current === null) {
    outcomeRef.current = parseOAuthCallback(location.search, location.hash);
  }
  const outcome = outcomeRef.current;

  // Set by the first effect; null until then (first paint only).
  const [returnTo, setReturnTo] = useState<string | null>(null);
  const [phase, setPhase] = useState<Phase>(
    outcome.kind === "error" ? "error" : "working",
  );
  const [attempt, setAttempt] = useState(0);

  // One-shot, declared before every other effect so it runs first in the
  // commit: take the return target (a destructive read — effect, never
  // render) and replace the history entry with the bare path. A PASSIVE
  // effect on purpose: react-router drops a navigate() issued from a
  // layout effect on initial mount (PR 2 note, spec §0 v4.3). The ref
  // guard covers StrictMode's double invocation.
  const initialised = useRef(false);
  useEffect(() => {
    if (initialised.current) return;
    initialised.current = true;
    setReturnTo(takeOAuthReturnTo() ?? DEFAULT_POST_LOGIN);
    if (location.search || location.hash) {
      navigate(location.pathname, { replace: true });
    }
  }, [location.pathname, location.search, location.hash, navigate]);

  // Success: adopt the cookie, navigate only once it produced a token.
  // Runs after the first effect has set `returnTo` (a later render), so
  // the URL is already clean when the request leaves. `attempt` re-arms
  // it for the retry button. bootstrapFromRefreshCookie never rejects
  // (transport failure is `unavailable`); the catch is a belt for an
  // unexpected throw — it lands on the same retryable phase.
  useEffect(() => {
    if (outcome.kind !== "success" || returnTo === null) return;
    let cancelled = false;
    bootstrapFromRefreshCookie()
      .then((result) => {
        if (cancelled) return;
        if (result.status === "ok") {
          navigate(returnTo, { replace: true });
        } else if (result.status === "signed-out") {
          setPhase("signedOut");
        } else {
          setPhase("unavailable");
        }
      })
      .catch(() => {
        if (!cancelled) setPhase("unavailable");
      });
    return () => {
      cancelled = true;
    };
  }, [attempt, outcome.kind, returnTo, bootstrapFromRefreshCookie, navigate]);

  return (
    <section className="mx-auto max-w-md px-6 py-16 text-center">
      {phase === "working" && (
        <p role="status" aria-busy="true" className="text-sm text-slate-600">
          {t("oauth.callback.verifying")}
        </p>
      )}

      {phase === "error" && outcome.kind === "error" && (
        <>
          <div role="alert" className={FAILURE_CLASS}>
            <p className="font-medium">{t("oauth.callback.failureTitle")}</p>
            <p className="mt-1">
              {t(`oauth.callback.errors.${outcome.errorKey}`)}
            </p>
          </div>
          <Link to="/login" className={LINK_CLASS}>
            {t("oauth.callback.backToLogin")}
          </Link>
        </>
      )}

      {phase === "signedOut" && (
        <>
          <div role="alert" className={FAILURE_CLASS}>
            <p className="font-medium">{t("oauth.callback.failureTitle")}</p>
            <p className="mt-1">{t("oauth.callback.sessionSignedOut")}</p>
          </div>
          <Link to="/login" className={LINK_CLASS}>
            {t("oauth.callback.backToLogin")}
          </Link>
        </>
      )}

      {phase === "unavailable" && (
        <>
          <div
            role="alert"
            className="mb-6 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-left text-sm text-amber-900"
          >
            {t("oauth.callback.sessionUnavailable")}
          </div>
          <div className="flex flex-col items-center gap-3">
            <button
              type="button"
              onClick={() => {
                setPhase("working");
                setAttempt((a) => a + 1);
              }}
              className="inline-flex items-center justify-center rounded-md bg-slate-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-700"
            >
              {t("oauth.callback.retry")}
            </button>
            <Link to="/login" className={LINK_CLASS}>
              {t("oauth.callback.backToLogin")}
            </Link>
          </div>
        </>
      )}
    </section>
  );
}
```

- [ ] **Step A4: Run it — GREEN.** `npx vitest run src/pages/OAuthCallbackPage.test.tsx` → 11 PASS (6 success + 5 failures).

#### Increment B — the MFA continuation

- [ ] **Step B1: Append the failing tests** to `OAuthCallbackPage.test.tsx`:

```tsx
describe("OAuthCallbackPage — MFA continuation", () => {
  it("renders MfaChallenge from the fragment with a clean URL, no router state and nothing in storage, then completes", async () => {
    server.use(
      http.post(VERIFY, async ({ request }) => {
        const body = (await request.json()) as { challengeId: string };
        return body.challengeId === "ch-1"
          ? HttpResponse.json({
              success: true,
              ...tokenBody,
              accessToken: "at-2",
              sessionId: "s1",
            })
          : HttpResponse.json({ detail: "wrong challenge" }, { status: 401 });
      }),
    );
    renderCallback("", "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=true");
    // login.mfa.prompt — the same MfaChallenge the password path renders.
    expect(
      await screen.findByText(/enter the 6-digit code/i),
    ).toBeInTheDocument();
    expect(screen.getByTestId("cb-location")).toHaveTextContent(
      /^\/auth\/callback$/,
    );
    // The challenge lives in component memory only: no router state.
    expect(screen.getByTestId("cb-state")).toHaveTextContent(/^null$/);
    expect(storageDump()).not.toContain("ch-1");

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/verification code/i), "123456");
    await user.click(screen.getByRole("button", { name: /^verify$/i }));
    expect(await screen.findByTestId("account-location")).toHaveTextContent(
      "/account",
    );
    expect(hasSessionMarker()).toBe(true);
    expect(storageDump()).not.toContain("at-2");
    expect(storageDump()).not.toContain("ch-1");
  });

  it("honours a fresh return target after the MFA completion", async () => {
    stash("/account/security");
    server.use(
      http.post(VERIFY, () =>
        HttpResponse.json({ success: true, ...tokenBody, sessionId: "s1" }),
      ),
    );
    renderCallback("", "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false");
    const user = userEvent.setup();
    await user.type(
      await screen.findByLabelText(/verification code/i),
      "123456",
    );
    await user.click(screen.getByRole("button", { name: /^verify$/i }));
    expect(await screen.findByTestId("deeplink-location")).toHaveTextContent(
      "/account/security",
    );
  });

  it("cancel returns to the login page", async () => {
    renderCallback("", "#requiresMfa=true&mfaToken=ch-1&webauthnAvailable=false");
    await userEvent
      .setup()
      .click(await screen.findByRole("button", { name: /cancel/i }));
    expect(await screen.findByTestId("login-location")).toHaveTextContent(
      "/login",
    );
  });
});
```

- [ ] **Step B2: Run it — RED.** `npx vitest run src/pages/OAuthCallbackPage.test.tsx -t "MFA continuation"` → the three cases time out on the spinner (an `mfa` outcome is not handled yet — the page stays in `working`).

- [ ] **Step B3: Implement increment B** — edits to `OAuthCallbackPage.tsx`:

Add the import and take `signIn` from the context:

```tsx
import { MfaChallenge } from "@/components/MfaChallenge";
…
  const { signIn, bootstrapFromRefreshCookie } = useAuth();
```

Insert **before** the final `return (` of the component:

```tsx
  if (outcome.kind === "mfa") {
    // Never painted before the first effect has scrubbed the URL.
    if (returnTo === null) return null;
    // webauthnAvailable is parsed (closed contract) but the client SPA has
    // no WebAuthn login, so the TOTP / backup-code form renders (D9 — an
    // explicit MVP limitation: a passkey-only user cannot complete here).
    return (
      <section className="mx-auto max-w-md px-6 py-16">
        <h1 className="mb-6 text-3xl font-semibold tracking-tight">
          {t("login.title")}
        </h1>
        <MfaChallenge
          mfaToken={outcome.challengeId}
          onCancel={() => navigate("/login", { replace: true })}
          onSuccess={(result) => {
            signIn(result.accessToken);
            navigate(returnTo, { replace: true });
          }}
        />
      </section>
    );
  }
```

- [ ] **Step B4: Run it — GREEN.** `npx vitest run src/pages/OAuthCallbackPage.test.tsx` → 14 PASS (6 + 5 + 3).

#### Increment C — the route, and the callback through the real route table

- [ ] **Step C1: Write the failing test** — `frontend-client/src/App.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { useLocation } from "react-router";
import { screen, waitFor } from "@testing-library/react";

import { App } from "@/App";
import { hasSessionMarker } from "@/auth/sessionMarker";
import { clientPolicyHandler, meHandler, url } from "@/test/handlers";
import { renderWithProviders } from "@/test/render";
import { server } from "@/test/server";

const REFRESH = url("/v1/auth/client/refresh-cookie");
const tokenBody = { accessToken: "at-1", tokenType: "Bearer", expiresIn: 900 };

// Reads the router location from OUTSIDE <Routes>: App owns the route table,
// this probe only reports where the app is.
const LocationProbe = () => {
  const location = useLocation();
  return (
    <div data-testid="app-location">
      {location.pathname + location.search + location.hash}
    </div>
  );
};

const renderApp = (search: string) =>
  renderWithProviders(
    <>
      <App />
      <LocationProbe />
    </>,
    { routerEntries: [{ pathname: "/auth/callback", search }] },
  );

describe("App — the OAuth callback through the real route table and Layout (F15)", () => {
  it("success: Layout's anonymous header, scrub before the refresh, then AccountPage authenticated", async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    let refreshHits = 0;
    let locationAtRequest: string | null = null;
    server.use(
      clientPolicyHandler(), // Layout mounts /policy while anonymous
      meHandler(), // Layout + AccountPage once authenticated
      http.post(REFRESH, async () => {
        refreshHits++;
        locationAtRequest =
          screen.queryByTestId("app-location")?.textContent ?? null;
        await gate;
        return HttpResponse.json(tokenBody);
      }),
    );
    renderApp("?success=true&provider=google");

    // The shell is up around the callback page, still anonymous.
    expect(
      await screen.findByRole("link", { name: "Sign in" }),
    ).toBeInTheDocument();
    expect(await screen.findByText(/completing sign-in/i)).toBeInTheDocument();
    // Scrubbed before the refresh left, while Layout's own query is in flight.
    await waitFor(() =>
      expect(screen.getByTestId("app-location")).toHaveTextContent(
        /^\/auth\/callback$/,
      ),
    );
    await waitFor(() => expect(refreshHits).toBe(1));
    // Order proof on the real tree: the bare path was committed before the
    // refresh left, with Layout's own query in flight beside it.
    expect(locationAtRequest).toBe("/auth/callback");
    expect(screen.getByTestId("app-location")).toHaveTextContent(
      /^\/auth\/callback$/,
    );

    release();
    // AccountPage (RequireAuth-guarded) with the /me profile, and the header
    // switched to the authenticated nav.
    expect(
      await screen.findByRole("heading", { name: "Account" }),
    ).toBeInTheDocument();
    expect(await screen.findByText("Client User")).toBeInTheDocument();
    expect(screen.getByTestId("app-location")).toHaveTextContent(/^\/account$/);
    expect(screen.queryByRole("link", { name: "Sign in" })).toBeNull();
    expect(hasSessionMarker()).toBe(true);
  });

  it("failure: the mapped copy renders inside the shell, on a clean URL, with no refresh", async () => {
    let refreshHits = 0;
    server.use(
      clientPolicyHandler(),
      http.post(REFRESH, () => {
        refreshHits++;
        return HttpResponse.json(tokenBody);
      }),
    );
    renderApp("?success=false&error=oauth_access_denied");
    expect(
      await screen.findByText(/cancelled at the identity provider/i),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("link", { name: "Sign in" }),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId("app-location")).toHaveTextContent(
        /^\/auth\/callback$/,
      ),
    );
    expect(refreshHits).toBe(0);
  });
});
```

- [ ] **Step C2: Run it — RED.** `npx vitest run src/App.test.tsx` → both cases fail: without the route, `/auth/callback` falls through to `NotFoundPage` — no "Completing sign-in…", no mapped copy (the "Sign in" link from Layout is found, which is why the assertions on the callback copy are what go red).

- [ ] **Step C3: Implement** — `frontend-client/src/App.tsx`: add the import after `LoginPage` (`:7`):

```tsx
import { OAuthCallbackPage } from "@/pages/OAuthCallbackPage";
```

and the route after `/login` (`:26`):

```tsx
        <Route path="/login" element={<LoginPage />} />
        <Route path="/auth/callback" element={<OAuthCallbackPage />} />
```

- [ ] **Step C4: Run it — GREEN, then the gates.** `npx vitest run src/App.test.tsx src/pages/OAuthCallbackPage.test.tsx` → 2 + 14 PASS; then `npm run typecheck && npm run lint -- --max-warnings 0 && npm test && npm run build` → all exit 0.

#### Close the task

- [ ] **Step D1: Documentation (same commit)**

`frontend-client/CLAUDE.md` — add a subsection at the end of "How auth works" (after "Reading tenant memberships from the JWT"):

```
### OAuth login (web)

`LoginPage` lists the providers the backend currently accepts (`GET /v1/auth/client/providers` — toggle on **and** structurally configured; a 503, a network failure or a malformed body is a retryable error state, never "no method") and starts a flow with `initiateOAuthLogin(provider, next)`: `POST /v1/auth/client/oauth/login {provider}` **with `credentials:'include'`** — the response sets the HttpOnly `orkestra_oauth_state` cookie on the API host and the relay endpoint later *requires* it — then stashes the validated `next` (`lib/oauthReturnTo.ts`, a ten-minute record) and leaves for `authUrl`. Every provider redirects to the **operator** host, which cannot set a cookie for `api.*`, so the backend relays the client-tier outcome to `GET {CLIENT_API_URL}/v1/auth/client/oauth/complete?relay=<id>`; that endpoint verifies the browser binding against the state cookie, sets the client refresh cookie on its own host and redirects to `{CLIENT_FRONTEND_URL}/auth/callback` under a **closed contract** — `?success=true&provider=<p>`, `?success=false&error=<allowlisted code>`, or `#requiresMfa=true&mfaToken=<id>&webauthnAvailable=<bool>`; never a token, an email or a user id.

`pages/OAuthCallbackPage.tsx` parses that URL once with `lib/oauthCallback.ts` (exact key sets — anything else is the generic failure), scrubs it in its first passive effect **before any request**, take-and-deletes the return target on every outcome, and then: success → `bootstrapFromRefreshCookie()` and navigate to the target or `/account` only once a token exists (signed-out is a login error; a 503 or a network failure offers retry); MFA → the same `components/MfaChallenge.tsx` the password path uses, challenge id in component memory only (`webauthnAvailable` is parsed but the client SPA has no WebAuthn login, so the TOTP / backup-code form renders — a passkey-only user cannot complete an OAuth-MFA continuation here yet); error → the mapped `oauth.callback.errors.*` copy. Raw URL text is never rendered. `src/App.test.tsx` mounts this page through the real route table so the shell's own queries are proven not to disturb the order scrub → bootstrap.
```

Add two bullets to the "Don't" list:

```
- **Don't build, parse or trust an `/auth/callback` URL outside `src/lib/oauthCallback.ts`**, don't render raw callback text, and don't put the MFA challenge id in router state or storage — it lives in the callback page's component memory only.
- **Don't navigate to a `?next=` value without `sanitizeNext`** (`src/lib/safeNext.ts`) — it is the SPA's only open-redirect gate, for the password path and the OAuth return target alike.
```

`docs/site/operating/oauth-providers.mdx:255` — replace step 4 with:

```
4. Repeat from the client SPA (`http://client.localhost:8081/login`) — its provider buttons call `POST /v1/auth/client/oauth/login`. The signed state carries `tier=client`, so the same operator-host callback performs only the IdP half and relays the outcome to `http://api.localhost:3000/v1/auth/client/oauth/complete?relay=…` on the client API host, which verifies the browser binding against the state cookie it set at start, issues the `aud=client` session and the client-tier refresh cookie, and lands on `http://client.localhost:8081/auth/callback`, where the SPA adopts the cookie through `POST /v1/auth/client/refresh-cookie`.
```

- [ ] **Step D2: Commit**

```bash
cd /home/tore/orkestra && npx --prefix frontend-client prettier --write frontend-client/src/pages/OAuthCallbackPage.tsx frontend-client/src/pages/OAuthCallbackPage.test.tsx frontend-client/src/App.tsx frontend-client/src/App.test.tsx frontend-client/src/test/handlers.ts frontend-client/src/locales/en.json frontend-client/src/locales/it.json >/dev/null
git add frontend-client/src/pages/OAuthCallbackPage.tsx frontend-client/src/pages/OAuthCallbackPage.test.tsx frontend-client/src/App.tsx frontend-client/src/App.test.tsx frontend-client/src/test/handlers.ts frontend-client/src/locales/en.json frontend-client/src/locales/it.json frontend-client/CLAUDE.md docs/site/operating/oauth-providers.mdx
git commit -m "feat(frontend-client): /auth/callback page — scrub before the first request, cookie bootstrap, MFA in memory

Parses the closed callback contract once, replaces the history entry in
the first passive effect before any request leaves, take-and-deletes the
return target on every outcome, adopts the relay-set refresh cookie on
success (signed-out is a login error; a 503 or a network failure offers
retry) and never enters a protected route on the success flag alone; an
MFA continuation renders the shared MfaChallenge from component memory;
failures render only the mapped allowlisted copy. An App-level test
mounts the page through the real route table with Layout (spec §4.10,
§5 #22/#23/#27/#28)." -m "Claude-Session: ${CLAUDE_SESSION:?export CLAUDE_SESSION=<session URL> first}"
```

### Task 7: `SignupPage`, `ForgotPasswordPage` (+ its mutation error) and the `Layout` CTA when the method is off

The remaining §4.10 rows for the client, one RED → GREEN cycle per file: password sign-ups and the header CTA hide on persisted false/null; the forgot-password page — gated 403/503 by the backend since PR 3 — gets the same notice **and** finally renders the backend's answer on submit (round-1 gap, F12), with its "always 200" comment corrected.

**Files:**
- Modify: `frontend-client/src/pages/SignupPage.tsx:6` (import), `:40-41` (derive), after `:81` (early return)
- Modify: `frontend-client/src/pages/ForgotPasswordPage.tsx:3-6` (imports), `:8-10` (header comment), `:11-14` (query + derive + error mapper), before `:36` (early return), inside the form (error rendering)
- Modify: `frontend-client/src/components/Layout.tsx:5` (import), `:26` (derive), `:73` (condition)
- Modify: `frontend-client/src/locales/en.json`, `frontend-client/src/locales/it.json` (`signup.passwordDisabled`, `forgot.passwordDisabled`)
- Test: `frontend-client/src/pages/SignupPage.test.tsx`, `frontend-client/src/pages/ForgotPasswordPage.test.tsx`, `frontend-client/src/components/Layout.test.tsx`
- Modify: `frontend-client/CLAUDE.md` ("How navigation works", after `:133`)

**Interfaces:**
- Consumes: Task 3's `fetchAuthPolicy`, `passwordLoginUsable`, `apiErrorCode`; Task 5's `error.policyUnavailable` key.
- Produces: nothing new.

- [ ] **Step 0: Add the i18n keys (both bundles)**

`en.json`: in `"signup"` after `"disabled"` add `"passwordDisabled": "Sign-up with email and password is disabled here. Sign in with one of the available providers instead."`; in `"forgot"` after `"backToLogin"` add `"passwordDisabled": "Password sign-in is disabled here, so a password cannot be reset. Sign in with one of the available providers or contact an administrator."`.

`it.json`: `"signup.passwordDisabled": "La registrazione con email e password è disabilitata qui. Accedi con uno dei provider disponibili."`; `"forgot.passwordDisabled": "L'accesso con password è disabilitato qui, quindi la password non può essere reimpostata. Accedi con uno dei provider disponibili o contatta un amministratore."`.

Run: `cd /home/tore/orkestra/frontend-client && npx vitest run src/locales` → PASS.

#### Cycle A — `SignupPage`

- [ ] **Step A1: Write the failing test** — `frontend-client/src/pages/SignupPage.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";

import { SignupPage } from "@/pages/SignupPage";
import { clientPolicyHandler } from "@/test/handlers";
import { renderWithProviders, waitForQuerySettled } from "@/test/render";
import { server } from "@/test/server";

describe("SignupPage — password-off (spec §4.10)", () => {
  it.each([false, null])(
    "passwordLoginEnabled=%s replaces the form with the notice and a sign-in link",
    async (value) => {
      server.use(clientPolicyHandler({ passwordLoginEnabled: value }));
      renderWithProviders(<SignupPage />);
      expect(await screen.findByRole("alert")).toHaveTextContent(
        /sign-up with email and password is disabled/i,
      );
      expect(screen.queryByLabelText(/^email$/i)).toBeNull();
      expect(
        screen.queryByRole("button", { name: /create account/i }),
      ).toBeNull();
      expect(screen.getByRole("link", { name: /sign in/i })).toHaveAttribute(
        "href",
        "/login",
      );
    },
  );

  it("keeps the form when the method is on", async () => {
    server.use(clientPolicyHandler());
    const { queryClient } = renderWithProviders(<SignupPage />);
    // The form paints before and after the policy lands — anchor on the
    // cache entry.
    await waitForQuerySettled(queryClient, ["authPolicy"]);
    expect(screen.getByLabelText(/^email$/i)).toBeInTheDocument();
    expect(
      screen.queryByText(/sign-up with email and password is disabled/i),
    ).toBeNull();
  });
});
```

- [ ] **Step A2: Run it — RED.** `npx vitest run src/pages/SignupPage.test.tsx` → the two password-off rows fail (no alert, form present); "keeps the form" passes (guard).

- [ ] **Step A3: Implement** — `frontend-client/src/pages/SignupPage.tsx`:
- line 6 → `import { fetchAuthPolicy, passwordLoginUsable, register, type RegisterInput, type RegisterResult } from '@/api/auth';`
- after `const passwordMinLength = policy?.passwordMinLength ?? 10;` (`:41`) add `const passwordOn = passwordLoginUsable(policy);`
- after the `if (submittedEmail) { … }` block (`:79-81`) add:

```tsx
  // Password sign-ups are hidden, not refused on submit, when the surface's
  // password method is off (spec §4.10, G5). The backend gates
  // POST /v1/auth/client/register with 403 regardless.
  if (!passwordOn) {
    return (
      <section className="mx-auto max-w-md px-6 py-16">
        <h1 className="mb-2 text-3xl font-semibold tracking-tight">{t('signup.title')}</h1>
        <div
          className="mb-6 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900"
          role="alert"
        >
          {t('signup.passwordDisabled')}
        </div>
        <p className="text-center text-sm text-slate-600">
          <Link to="/login" className="font-medium text-slate-900 underline">
            {t('signup.signinLink')}
          </Link>
        </p>
      </section>
    );
  }
```

- [ ] **Step A4: Run it — GREEN.** `npx vitest run src/pages/SignupPage.test.tsx` → 3 PASS.

#### Cycle B — `ForgotPasswordPage` (notice + the backend's answer on submit)

- [ ] **Step B1: Write the failing test** — `frontend-client/src/pages/ForgotPasswordPage.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { ForgotPasswordPage } from "@/pages/ForgotPasswordPage";
import { clientPolicyHandler, url } from "@/test/handlers";
import { renderWithProviders, waitForQuerySettled } from "@/test/render";
import { server } from "@/test/server";

const FORGOT = url("/v1/auth/client/forgot-password");
const POLICY = url("/v1/auth/client/policy");

const submit = async (email = "a@b.c") => {
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText(/^email$/i), email);
  await user.click(screen.getByRole("button", { name: /send link/i }));
};

describe("ForgotPasswordPage — password-off notice (D8, spec §4.3 gate)", () => {
  it("replaces the form with the notice and a back link when the method is off", async () => {
    server.use(clientPolicyHandler({ passwordLoginEnabled: false }));
    renderWithProviders(<ForgotPasswordPage />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /password sign-in is disabled here/i,
    );
    expect(screen.queryByLabelText(/^email$/i)).toBeNull();
    expect(
      screen.getByRole("link", { name: /back to sign in/i }),
    ).toHaveAttribute("href", "/login");
  });

  it("keeps the form when the method is on", async () => {
    server.use(clientPolicyHandler());
    const { queryClient } = renderWithProviders(<ForgotPasswordPage />);
    await waitForQuerySettled(queryClient, ["authPolicy"]);
    expect(screen.getByLabelText(/^email$/i)).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("ForgotPasswordPage — the form shows the backend's answer (spec §4.10 'direct forms')", () => {
  it("503 auth.policy_unavailable → the shared retryable copy, form kept, button re-enabled", async () => {
    server.use(
      clientPolicyHandler(),
      http.post(FORGOT, () =>
        HttpResponse.json(
          { code: "auth.policy_unavailable", detail: "raw-detail" },
          { status: 503 },
        ),
      ),
    );
    renderWithProviders(<ForgotPasswordPage />);
    await submit();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /sign-in policy is temporarily unavailable/i,
    );
    expect(screen.queryByText(/raw-detail/)).toBeNull();
    expect(screen.getByLabelText(/^email$/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /send link/i })).toBeEnabled();
  });

  it("403 auth.password_login_disabled while the display fell open → the disabled notice", async () => {
    // /policy is down (fail-open display) but the backend still gates.
    server.use(
      http.get(POLICY, () =>
        HttpResponse.json({ code: "auth.policy_unavailable" }, { status: 503 }),
      ),
      http.post(FORGOT, () =>
        HttpResponse.json(
          { code: "auth.password_login_disabled", detail: "raw-detail" },
          { status: 403 },
        ),
      ),
    );
    renderWithProviders(<ForgotPasswordPage />);
    await submit();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /password sign-in is disabled here/i,
    );
    expect(screen.queryByText(/raw-detail/)).toBeNull();
  });

  it("any other failure → the generic error, never the raw detail", async () => {
    server.use(
      clientPolicyHandler(),
      http.post(FORGOT, () =>
        HttpResponse.json({ detail: "raw-detail" }, { status: 500 }),
      ),
    );
    renderWithProviders(<ForgotPasswordPage />);
    await submit();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /something went wrong/i,
    );
    expect(screen.queryByText(/raw-detail/)).toBeNull();
  });

  // Regression guard — GREEN before the change: the neutral confirmation.
  it("200 → the neutral confirmation", async () => {
    server.use(
      clientPolicyHandler(),
      http.post(FORGOT, () => HttpResponse.json({ success: true })),
    );
    renderWithProviders(<ForgotPasswordPage />);
    await submit();
    expect(await screen.findByText(/check your email/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step B2: Run it — RED.** `npx vitest run src/pages/ForgotPasswordPage.test.tsx` → every case except the 200 guard fails: the page mounts no `/policy` query yet, so MSW's unhandled-request error is not the failure (nothing calls it) — the notice cases find no alert, and the three error cases find no alert either (today the page renders nothing on `mutation.isError`).

- [ ] **Step B3: Implement** — `frontend-client/src/pages/ForgotPasswordPage.tsx`:

- lines 3–6 become:

```tsx
import { useMutation, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { apiErrorCode, fetchAuthPolicy, forgotPassword, passwordLoginUsable } from '@/api/auth';
```

- the header comment (`:8-10`) becomes:

```tsx
// Single-screen flow: email form → neutral confirmation. The backend
// answers 200 for every account outcome (enumeration-resistant), so the
// UI shows the same message whether or not the email exists. The only
// errors are the per-surface policy answers evaluated before the lookup
// (spec §4.3): 403 auth.password_login_disabled, 503
// auth.policy_unavailable — mapped by code below, never rendered raw.
```

- after `const mutation = useMutation({ mutationFn: forgotPassword });` (`:14`) add:

```tsx
  // Same cached policy the login page reads. When the method is off the
  // form is hidden rather than refused on submit (G5; deviation D8).
  const { data: policy } = useQuery({
    queryKey: ['authPolicy'],
    queryFn: fetchAuthPolicy,
    staleTime: 30_000,
  });
  const passwordOn = passwordLoginUsable(policy);

  // The display may have fallen open (policy 503) while the backend still
  // gates the route: the form then shows the backend's answer (spec §4.10).
  function errorKey(e: unknown): string {
    switch (apiErrorCode(e)) {
      case 'auth.password_login_disabled':
        return 'forgot.passwordDisabled';
      case 'auth.policy_unavailable':
        return 'error.policyUnavailable';
      default:
        return 'error.generic';
    }
  }
```

- before the final `return (` of the form (`:36`) add:

```tsx
  if (!passwordOn) {
    return (
      <section className="mx-auto max-w-md px-6 py-16">
        <h1 className="mb-2 text-3xl font-semibold tracking-tight">{t('forgot.title')}</h1>
        <div
          className="mb-6 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900"
          role="alert"
        >
          {t('forgot.passwordDisabled')}
        </div>
        <Link to="/login" className="text-slate-600 underline hover:text-slate-900">
          {t('forgot.backToLogin')}
        </Link>
      </section>
    );
  }
```

- inside the form, between the email `<div>` and the submit `<button>` (`:55-56`) add:

```tsx
        {mutation.isError && (
          <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
            {t(errorKey(mutation.error))}
          </p>
        )}
```

- [ ] **Step B4: Run it — GREEN.** `npx vitest run src/pages/ForgotPasswordPage.test.tsx` → 6 PASS.

#### Cycle C — the `Layout` CTA

- [ ] **Step C1: Write the failing test** — `frontend-client/src/components/Layout.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { Route, Routes } from "react-router";
import { screen, waitFor } from "@testing-library/react";

import { Layout } from "@/components/Layout";
import { clientPolicyHandler } from "@/test/handlers";
import { renderWithProviders, waitForQuerySettled } from "@/test/render";
import { server } from "@/test/server";

const renderLayout = () =>
  renderWithProviders(
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<div>home</div>} />
      </Route>
    </Routes>,
  );

describe("Layout — anonymous header CTA (spec §4.10)", () => {
  it("hides the Sign-up CTA once the policy says password sign-in is off", async () => {
    server.use(clientPolicyHandler({ passwordLoginEnabled: false }));
    renderLayout();
    // The first paint falls open (CTA present); its disappearance is the
    // settled anchor.
    expect(screen.getByRole("link", { name: "Sign up" })).toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.queryByRole("link", { name: "Sign up" }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("link", { name: "Sign in" })).toBeInTheDocument();
  });

  it("keeps the CTA when the method is on", async () => {
    server.use(clientPolicyHandler());
    const { queryClient } = renderLayout();
    await waitForQuerySettled(queryClient, ["authPolicy"]);
    expect(screen.getByRole("link", { name: "Sign up" })).toBeInTheDocument();
  });

  // Regression guard — GREEN before the change.
  it("still hides the CTA when self-service registration is off", async () => {
    server.use(clientPolicyHandler({ registrationEnabled: false }));
    renderLayout();
    expect(screen.getByRole("link", { name: "Sign up" })).toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.queryByRole("link", { name: "Sign up" }),
      ).not.toBeInTheDocument(),
    );
  });
});
```

- [ ] **Step C2: Run it — RED.** `npx vitest run src/components/Layout.test.tsx` → the password-off case fails (the CTA never disappears); the other two pass (guards).

- [ ] **Step C3: Implement** — `frontend-client/src/components/Layout.tsx`:
- line 5 → `import { fetchAuthPolicy, passwordLoginUsable } from "@/api/auth";`
- after `const registrationEnabled = policy?.registrationEnabled ?? true;` (`:26`) add `const passwordOn = passwordLoginUsable(policy);`
- line 73 `{registrationEnabled && (` → `{registrationEnabled && passwordOn && (`

- [ ] **Step C4: Run it — GREEN.** `npx vitest run src/components/Layout.test.tsx` → 3 PASS.

#### Close the task

- [ ] **Step D1: Whole suite, typecheck, lint**

Run: `cd /home/tore/orkestra/frontend-client && npm test && npm run typecheck && npm run lint -- --max-warnings 0`
Expected: every file green (SignupPage 3, ForgotPasswordPage 6, Layout 3, locales parity), typecheck and lint exit 0.

- [ ] **Step D2: Documentation (same commit)**

`frontend-client/CLAUDE.md`, "How navigation works" — add after the paragraph ending "Anonymous routes mount directly under `<Layout>`." (`:133`):

```
Anonymous entry points are policy-aware: the header's Sign-up CTA, `/signup` and `/forgot-password` hide their password forms behind a notice when `passwordLoginUsable(policy)` is false for the client surface (the backend refuses those routes with 403 anyway), and the CTA also stays hidden when `registrationEnabled` is off. When the policy read fell open, the forgot-password form still renders the backend's answer mapped by code (`forgot.passwordDisabled`, `error.policyUnavailable`, `error.generic`) — never the raw detail. `/reset-password` and `/accept-invite` stay open — the backend keeps those routes open too (spec §4.3).
```

- [ ] **Step D3: Commit**

```bash
cd /home/tore/orkestra && npx --prefix frontend-client prettier --write frontend-client/src/pages/SignupPage.tsx frontend-client/src/components/Layout.tsx frontend-client/src/pages/ForgotPasswordPage.tsx frontend-client/src/pages/SignupPage.test.tsx frontend-client/src/components/Layout.test.tsx frontend-client/src/pages/ForgotPasswordPage.test.tsx frontend-client/src/locales/en.json frontend-client/src/locales/it.json >/dev/null
git add frontend-client/src/pages/SignupPage.tsx frontend-client/src/components/Layout.tsx frontend-client/src/pages/ForgotPasswordPage.tsx frontend-client/src/pages/SignupPage.test.tsx frontend-client/src/components/Layout.test.tsx frontend-client/src/pages/ForgotPasswordPage.test.tsx frontend-client/src/locales/en.json frontend-client/src/locales/it.json frontend-client/CLAUDE.md
git commit -m "feat(frontend-client): hide password sign-up, recovery and the header CTA when the method is off

SignupPage and ForgotPasswordPage swap their forms for a notice and a
link back to sign-in when passwordLoginUsable(policy) is false; the
forgot-password form now also renders the backend's answer mapped by code
(403 auth.password_login_disabled, 503 auth.policy_unavailable, generic)
instead of swallowing it, and its header no longer claims always-200; the
Layout Sign-up CTA hides on password-off as it already did on
registration-off (spec §4.3, §4.10)." -m "Claude-Session: ${CLAUDE_SESSION:?export CLAUDE_SESSION=<session URL> first}"
```

### Task 8: Documentation reconciliation sweep, the full gates, and the browser smoke test

The cross-cutting VERIFICATION pass — it completes and reconciles, it is never the first touch. Every standing doc that described the pre-PR state has already been updated by the task that changed the code; this task proves no stale claim survived, reconciles the one README section that predates ADR-0006, runs the full gate and an allowlist scope check on the tip, renders the docs site, and — the repository's own contract for UI work (`frontend-client/CLAUDE.md` "Test in a browser if it's a UI change") — drives the running staging client through the integrated browser. The IdP round-trip on staging stays manual (spec §7).

**Files:**
- Modify: `frontend-client/CLAUDE.md:248` ("Current surface"), `frontend-client/README.md` (Layout tree `:60-89`, Roadmap note)
- Verify only: everything touched by Tasks 1–7; the running `orkestra-public-client-frontend-staging` container (bind-mounted on this tree — **never started, restarted or rebuilt by this task**)

- [ ] **Step 1: Grep for stale claims**

Run:

```bash
cd /home/tore/orkestra && grep -rn "no tests yet\|not yet wired\|still reads only\|until then its login page\|React Router v7\|gains the same gating with the client OAuth login work\|Always returns 200\|always returns 200 to defeat" CLAUDE.md CONTRIBUTING.md .github/workflows/frontend-client.yml frontend-client/CLAUDE.md frontend-client/README.md frontend-client/src docs/site/architecture/authentication-flow.mdx docs/site/modules/core/auth.mdx docs/site/operating/oauth-providers.mdx docs/site/contributing/ci-and-make.mdx
```

Expected: no output. Any hit is a Task 1/3/5/6/7 doc step that was missed — fix it in that file now and name it in the report.

- [ ] **Step 2: Reconcile the client docs that describe the whole surface**

`frontend-client/CLAUDE.md:248` ("Current surface (ADR-0006)") — replace the first sentence with:

```
The base SPA is a **thin auth/account demo**: anonymous home + signup + email verify, login with email/password **and web OAuth** (Google / Apple / GitHub / Discord through the client-tier relay, `/auth/callback`), password recovery + MFA enrol, account (profile, security, billing identity), accept-invite.
```

`frontend-client/README.md` "Layout" tree (`:60-89`) predates ADR-0006 (it lists `catalog.ts`, `subscriptions.ts`, `payments.ts`, `memberships.ts`, `stripe.ts`, none of which exist). Replace the tree with the actual one:

```
src/
├── api/
│   ├── client.ts           # openapi-fetch wrapper, refresh-cookie 401 retry
│   ├── openapi.gen.ts      # generated by `npm run codegen`
│   ├── auth.ts             # register, login, /me, password recovery, MFA, policy, OAuth providers + start
│   ├── avatar.ts           # /v1/me/avatar/* self-service
│   ├── billingProfile.ts   # /v1/me/billing-identity
│   ├── dsr.ts              # data-subject requests
│   └── verifyEmail.ts      # /v1/auth/client/verify-email{,/resend}
├── auth/
│   ├── AuthProvider.tsx    # React context (in-memory access token, bootstrapFromRefreshCookie)
│   ├── tokenStore.ts       # module-scoped token; one unconditional, coalesced refresh; cookie bootstrap
│   ├── sessionMarker.ts    # localStorage hint that a refresh cookie probably exists
│   ├── useAuth.ts          # context hook
│   ├── useMe.ts            # /me TanStack Query wrapper
│   └── RequireAuth.tsx     # router guard with ?next= round-trip
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

Under the Roadmap table (`:91-101`) add one line: `Post-ADR-0006 the base keeps phases 1–3 only (auth + account); web OAuth login for the client tier landed with the password-login toggle work (spec: \`docs/superpowers/specs/2026-08-29-password-login-toggle-design.md\` §4.10).`

- [ ] **Step 3: Full client gate, whitespace, allowlist scope check**

Run:

```bash
make -C /home/tore/orkestra ci-frontend-client
cd /home/tore/orkestra && git diff --check 4d7c0397..HEAD
cd /home/tore/orkestra && git diff --name-only 4d7c0397..HEAD | grep -vE '^(frontend-client/|Makefile$|\.github/workflows/frontend-client\.yml$|CLAUDE\.md$|CONTRIBUTING\.md$|docs/site/(contributing/ci-and-make|architecture/authentication-flow|modules/core/auth|operating/oauth-providers)\.mdx$|docs/superpowers/plans/2026-08-31-password-login-toggle-pr4-client-oauth-login\.md$)' || echo "scope: OK"
```

Expected: `Frontend-client CI: OK` (lockcheck in sync, typecheck, lint, every test file green, build); `git diff --check` prints nothing; the third command prints only `scope: OK` — any other line is a file outside the allowlist in the plan's File Structure and must be explained or reverted.

- [ ] **Step 4: Render the docs site**

Run (check `df -h /tmp` first — a 16 GB tmpfs; the clone is removed at the end):

```bash
D=$(mktemp -d /tmp/orkestra-docs-pr4.XXXXXX)
git clone --depth 1 https://github.com/orkestra-cc/orkestra-docs "$D"
( cd "$D" && npm ci --no-audit --no-fund && MONOREPO_LOCAL_PATH=/home/tore/orkestra npm run sync && CI=true npm run build ); status=$?
rm -rf "$D"; test "$status" -eq 0
```

Expected: exit 0; the four edited pages (`architecture/authentication-flow`, `modules/core/auth`, `operating/oauth-providers`, `contributing/ci-and-make`) render. (`sync:openapi` / `sync:adrs` pull from `main` and ignore `MONOREPO_LOCAL_PATH` — irrelevant here, no OpenAPI or ADR changed.)

- [ ] **Step 5: Browser smoke test on the running staging client (integrated-browser-mcp)**

This checkout owns the `orkestra-public-*-staging` stack (`docker/.env`: `APP_NAME=orkestra-public`, `ENV=staging`); its `client-frontend` is a Vite dev server bind-mounted on `../frontend-client` (`docker/docker-compose.staging.yml:308-330`), so the branch's working tree is what it serves. **Do not start, restart or rebuild anything.** Resolve the origin and confirm the container is up:

```bash
cd /home/tore/orkestra && grep -E "^(CLIENT_FRONTEND_URL|CLIENT_FRONTEND_PORT|HOST_BIND_ADDRESS)=" docker/.env
docker compose -f docker/docker-compose.staging.yml --env-file docker/.env ps client-frontend
```

`ORIGIN` = `CLIENT_FRONTEND_URL` when set (staging: `https://app.orkestra.cc`), else `http://localhost:${CLIENT_FRONTEND_PORT:-8081}`. If the container is not running, record that and skip this step in the report — it is the user's stack, not this task's to bring up.

Then, with `browser_navigate` / `browser_snapshot` / `browser_url` / `browser_console` (save a `browser_screenshot` of each page under the SDD workspace and cite the file names in the report):

1. `${ORIGIN}/login` — the page must settle into one of the shapes Task 5 tests: password form + provider buttons, password form alone, provider buttons alone, the no-method notice, or the password form + the retryable providers error (the last is expected when the staging API rejects the browser's origin — record which). No raw error text, no `undefined`, no i18n key rendered as text.
2. `${ORIGIN}/auth/callback?success=false&error=oauth_access_denied` — "Sign-in was cancelled at the identity provider." (or its Italian twin if the browser's language is IT) and **`browser_url` reports `${ORIGIN}/auth/callback` with no query** — the scrub happened in the real browser.
3. `${ORIGIN}/auth/callback?success=false&error=%3Cscript%3Ealert(1)%3C%2Fscript%3E` — the generic copy; `browser_snapshot` contains no `<script>` text; `browser_console` shows no uncaught error.
4. `${ORIGIN}/auth/callback?success=true&provider=google` (no relay cookie in this browser) — the 401 path: "no session could be established" and a link back to sign in; **or**, if the API is unreachable from this origin, the unavailable state with its retry button. Either is a correct render; a spinner that never settles is a defect.
5. `${ORIGIN}/signup` and `${ORIGIN}/forgot-password` — render (form or notice according to the staging policy), no console errors.

Record the observed shape of each page in the task report. The provider round-trip with a real IdP (spec §7 "client through the new client-SPA OAuth path") remains a manual staging step after merge.

- [ ] **Step 6: Commit**

```bash
cd /home/tore/orkestra && npx --prefix frontend-client prettier --write frontend-client/README.md frontend-client/CLAUDE.md >/dev/null
git add frontend-client/CLAUDE.md frontend-client/README.md
git commit -m "docs(frontend-client): current surface includes web OAuth login; README layout tree matches the tree

The README's src/ tree still listed the catalog/subscriptions/payments
files that left with ADR-0006; it now lists what exists, including the
test harness and the OAuth modules. CLAUDE.md's surface summary names the
client-tier OAuth login." -m "Claude-Session: ${CLAUDE_SESSION:?export CLAUDE_SESSION=<session URL> first}"
```

## Post-plan verification (not a task — the executor's exit checklist)

Before handing the branch to the reviewer, confirm each line and paste the evidence in the final report:

- `make -C /home/tore/orkestra ci-frontend-client` green on the tip (lockcheck, typecheck, lint, tests, build) — the test count and file count from the vitest summary.
- `git log --oneline 4d7c0397..HEAD` shows the plan commits plus exactly the 8 task commits (plus any reviewed fix commits), every subject conventional; `test "$(git log --format=%B 4d7c0397..HEAD | grep -c '^Claude-Session: https://')" -eq "$(git rev-list --count 4d7c0397..HEAD)"` holds — one non-empty trailer per commit.
- The allowlist scope check from Task 8 step 3 prints `scope: OK`.
- `grep -rn "no tests yet\|still reads only\|React Router v7\|Always returns 200" CLAUDE.md CONTRIBUTING.md frontend-client docs/site .github/workflows/frontend-client.yml --include=*.md --include=*.mdx --include=*.yml --include=*.ts --include=*.tsx` is empty.
- `cd frontend-client && npx vitest run src/locales` green (EN/IT parity after every key added in Tasks 5–7).
- The docs site built from the branch (`CI=true npm run build` exit 0) and the temporary clone was removed.
- The browser smoke test (Task 8 step 5) ran against the running staging client, or the report says why it could not (container down) — never silently skipped.
- No `vi.mock` in any test (`grep -rn "vi.mock" frontend-client/src` prints nothing); the only stubs are MSW handlers, the `browserNavigation.assign` spy and `vi.stubGlobal("localStorage", …)` in `tokenStore.test.ts`.
- Every test file's absence assertions follow a settled positive anchor (spot-check `LoginPage.test.tsx`, `Layout.test.tsx`, `App.test.tsx`).
- The deviation table (D1–D18) is copied into the PR description with its final Status column, plus the fork-facing notes: new client route `/auth/callback`; `AuthState` gains `bootstrapFromRefreshCookie` (a fork with its own `AuthProvider` implements it); `refreshAccessToken` no longer rejects on a transport failure (returns `unavailable`); `make ci-frontend-client` now runs `client-test`; follow-ups — a client coverage floor (D10), WebAuthn login on the client so passkey-only users can complete an OAuth-MFA continuation (D9), the README roadmap table.
- Staging verification (spec §7 step "client through the new client-SPA OAuth path") remains a manual step after merge: provider buttons render on the staging client, a Google login lands on `/auth/callback` and then `/account` with no token, email or user id in the URL, and flipping `passwordLoginEnabledClient=false` hides the password form while the OAuth button keeps working.
