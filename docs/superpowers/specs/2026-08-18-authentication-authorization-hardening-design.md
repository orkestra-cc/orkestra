# Authentication and Authorization Hardening Design

**Date:** 2026-08-18
**Status:** Approved for planning
**Scope:** Orkestra core authentication, session lifecycle, security logging, and degraded revocation behavior

## Context

A static security review of the authentication and authorization system found four implementation defects and one explicit residual risk:

1. Apple OAuth configuration and private-key material are written to process logs.
2. OAuth web and mobile login can issue tokens for an inactive user even though password login and refresh reject that user.
3. JWT `sid`, refresh-token `sessionUuid`, and the persisted authentication-session UUID diverge during OAuth login and refresh rotation, weakening immediate revocation.
4. OAuth repositories and services emit unconditional debug logs containing PII and security identifiers.
5. Redis-backed access-token revocation deliberately fails open during a Redis outage, widening the stolen-token window to the access-token TTL.

The existing audience split, tenant scoping, permission coverage, password hashing, refresh-family replay detection, and atomic first-admin claim remain valid and are outside the redesign except where the session identifier must pass through them.

## Goals

- Prevent secrets, OAuth credentials, PII, session identifiers, device fingerprints, and provider identifiers from reaching ordinary application logs.
- Make account activation state a single invariant for every credential-to-token path.
- Give each login exactly one cryptographically random session UUID and preserve it across access tokens, refresh tokens, refresh rows, session documents, and refresh rotation.
- Ensure logout, password change, account deactivation, administrator revocation, and self-service session revocation target the same `sid` checked by middleware.
- Keep refresh-family replay detection and audience separation intact.
- Make degraded revocation observable and explicitly bounded without turning a Redis outage into a platform-wide authentication outage.
- Add regression tests that exercise token claims, not only response metadata.

## Non-goals

- Replacing RS256, Argon2id, Cedar, Huma, MongoDB, or Redis.
- Adding a new identity provider, MFA method, token format, database, or external secret manager.
- Changing role or permission semantics.
- Making Redis mandatory for all authenticated requests.
- Retrofitting a general-purpose logging framework across unrelated modules.
- Automatically deleting or rotating credentials in an external Apple developer account.

## Considered approaches

### A. Patch each call site independently

Add an inactive-user check to every OAuth handler, pass a session ID to the two visibly broken JWT calls, and delete the most obvious debug statements.

This is small but fragile. Authentication has web, mobile, MFA, refresh, and future provider entry points; duplicating security invariants at handlers makes another bypass likely. It also leaves the legacy JWT helper methods capable of silently minting unrelated session IDs.

### B. Centralize token eligibility and session issuance (selected)

Enforce token eligibility in the service immediately before any full token pair is issued, while retaining earlier checks for better error mapping. Introduce one session-aware issuance path that requires a caller-supplied session UUID and use it for password, OAuth, MFA completion, refresh, and access-token step-up. Remove secret-bearing debug output and replace the small number of operationally useful events with allowlisted structured attributes.

This approach makes the invariant difficult to bypass, minimizes architectural change, and preserves the current repository and module boundaries.

### C. Replace JWT sessions with opaque server-side access tokens

This would make revocation authoritative in a database or cache but would substantially change the authentication architecture, availability model, clients, and service integrations. It is disproportionate to the findings and is not selected.

## Design

### 1. Security logging and secret response

All `APPLE_DEBUG`, `AUTH_DEBUG`, and `REPO_DEBUG` output in the auth module will be removed. In particular, code must never print `OAuthProviderConfig.AdditionalConfig`, PEM contents or fragments, filesystem secret paths, OAuth provider IDs, email addresses, device fingerprints, refresh-row UUIDs, session UUIDs, access or refresh tokens, encrypted token ciphertext, or raw provider metadata.

Operational events that are still useful will use the module's injected `*slog.Logger` and an allowlist of non-sensitive fields such as provider name, audience, outcome, and stable error category. Raw third-party errors must not be logged if they can contain request bodies, tokens, or secrets; they will be wrapped for internal control flow and mapped to a stable category at the log boundary.

The repository documentation will include an incident-response note: deployments that initialized Apple OAuth with the affected code must treat log access as potential key disclosure, remove or restrict retained logs according to the deployment's logging system, rotate the Apple private key, update the encrypted module configuration, and invalidate the old key in Apple Developer. The application cannot perform these external actions automatically.

A source-level regression test will scan the auth production packages for forbidden debug markers and direct `fmt.Print*` calls. Focused unit tests will use a capture handler to ensure Apple service construction and parse failures do not emit configuration values or PEM fragments.

### 2. Uniform account eligibility

Full token issuance requires a non-nil user with a non-empty UUID and `IsActive == true`. The invariant will live in the central auth service immediately before session creation or token signing. Password login and refresh retain their existing earlier checks because they produce flow-specific neutral errors and audit events.

OAuth callbacks, Google/Apple mobile authentication, MFA login completion, password login, refresh, and read-only access minting from a refresh token must all pass through an eligibility check before receiving a full access token. Partial MFA challenge responses do not contain credentials and may be built only after the same active-user check, preventing an inactive account from entering the MFA completion flow.

Externally, inactive OAuth accounts receive the same neutral authentication failure used for invalid OAuth login. The response must not reveal whether the account exists or is inactive. The attempt produces a sanitized security event containing user UUID only in the protected audit sink, not ordinary process logs.

### 3. Canonical session identity

The session UUID is generated exactly once at the start of a successful login after primary credentials are accepted and before any JWT is signed. It uses the existing random UUID generator, never a timestamp-derived `session_<unix>` identifier.

The canonical value is propagated as follows:

```text
successful credential verification
        |
        v
canonical random session UUID
        |--------------------|----------------------|
        v                    v                      v
access JWT sid          refresh JWT sid      session/refresh documents
```

The JWT service will expose session-aware methods that require an explicit `SecurityContext` or explicit session UUID. Authentication production code must not call convenience helpers that synthesize session identifiers. Convenience methods needed by development tooling must use an explicitly labeled synthetic session path and cannot be used by normal login or refresh services.

OAuth login will create both the authentication-session document and refresh-token row using the canonical UUID, matching password login. Its session record uses `LoginMethod="oauth:<provider>"` (or the closest existing model-compatible value), the actual device data, audience-tier repository, expiry, IP, and risk result. If persisting either credential-bearing refresh state or the session record fails, the login fails and any partial persisted state is rolled back or revoked best-effort; a usable token pair must not be returned after persistence failure.

Refresh rotation preserves the original canonical `tokenDoc.SessionUUID`. Both newly signed JWTs receive that value, and the successor refresh row retains it. Rotation never creates a new session or synthetic `sid`.

Step-up access tokens preserve the current JWT `sid`, device ID, audience, and memberships while changing only the authentication-method references and `last_otp_at`. MFA completion after a partial login uses the challenge's canonical pending session UUID and creates/persists the session consistently before returning tokens.

Revocation continues to use the canonical UUID for all three actions: revoke refresh rows by session, mark the session document inactive, and add `auth:revoked:session:<sid>` to Redis. Middleware compares the exact same value from the access token.

### 4. Degraded revocation

Redis revocation lookup remains fail-open to avoid making every protected request unavailable during a Redis outage. This is an explicit availability choice, not silent success.

Every non-`redis.Nil` lookup failure will:

- increment a bounded-cardinality metric for revocation-store failures;
- emit a rate-limited structured warning without user, token, session, or Redis credential data;
- preserve the existing short access-token TTL as the maximum degraded exposure window.

Revocation writes remain best-effort only after Mongo-backed refresh/session invalidation has succeeded. A failed Redis write returns a typed degradation signal to the caller responsible for security-sensitive administrative revocation so it can be audited, while user logout remains idempotent and does not expose backend health details. The precise HTTP behavior remains unchanged unless an existing endpoint already represents partial failure; observability carries the degradation.

The design does not add a Mongo lookup to every authenticated request because that would alter latency and availability characteristics. Operators must run Redis with appropriate persistence/high availability and alert on the new metric.

### 5. Authorization and tenant invariants

No route becomes public and no permission is weakened. Existing `RequireAuth`, `RequireAudience`, `RequireGlobal`, tenant-kind, system permission, MFA, and step-up middleware remain in place.

The changes must preserve:

- mandatory JWT issuer, type, expiry, and known-audience validation;
- operator/client collection separation;
- tenant membership resolution and administrative impersonation checks;
- live `authz.HasPermission` evaluation rather than token-embedded permissions;
- refresh-token hash storage, atomic rotation, and family replay response;
- immediate permission changes independent of token refresh.

## Error handling and rollback

- Secret/configuration errors return wrapped internal errors without embedding secret values.
- Inactive users fail before token signing and before refresh/session persistence.
- A JWT signing failure leaves no usable persisted refresh token.
- A persistence failure after signing returns no tokens and revokes/deletes any partial session state best-effort.
- A rotation CAS loss continues to revoke the refresh family as replay.
- Redis revocation degradation never changes an authorization denial into an allow decision; it only affects already-issued access-token revocation until expiry.

## Test strategy

### Unit regression tests

- Apple configuration and private-key parsing never log config values or PEM fragments.
- Auth production files contain no forbidden debug markers or direct print calls.
- Existing linked OAuth identity for an inactive user cannot obtain tokens.
- Email-matched OAuth identity for an inactive user cannot obtain tokens.
- Google and Apple mobile paths inherit the same inactive-user denial.
- OAuth token pair access `sid`, refresh `sid`, response `sessionId`, refresh-row `sessionUuid`, and auth-session UUID are identical.
- Password token pair retains the same invariant.
- After one and multiple refresh rotations, both new JWT `sid` claims still equal the original session UUID.
- Step-up and MFA-completion access tokens preserve the canonical session UUID.
- Revoking a session causes middleware to reject its current access token.
- Revoking one session does not revoke another session for the same user.
- Redis `Nil` is treated as not revoked; a Redis transport failure records the metric and rate-limited warning while following the documented fail-open result.

### Static and integration verification

Run the focused auth/authz/middleware suites, then repository gates:

```bash
cd backend
go test ./internal/core/auth/... ./internal/core/authz/... ./internal/shared/middleware/...
cd ..
make backend-lint
make backend-tenantscope
make backend-policycoverage
make backend-piiscan
make backend-vulncheck
make backend-test-ci
make backend-build-ci
make backend-openapi-check
```

No OpenAPI change is expected. If typed error envelopes or endpoint schemas change during implementation, regenerate `backend/openapi/enterprise.json` and include it in the same task.

## Rollout and incident response

1. Before deployment, identify environments where Apple OAuth was initialized and rotate affected Apple keys.
2. Restrict and sanitize retained application/OTLP logs; follow the deployment's incident and privacy processes if logs left the trusted boundary.
3. Deploy the code with the existing access-token TTL unchanged or reduced, never increased as part of this rollout.
4. Force reauthentication for existing sessions whose historical `sid` may not match their persisted session. Revoking all refresh rows prevents further rotation; existing access tokens age out within the access TTL.
5. Monitor OAuth failure rate, refresh replay detection, revocation-store failure metric, and authentication latency.
6. After one refresh-token TTL, old mismatched refresh rows can be removed by the existing retention process without weakening replay detection.

## Success criteria

- No auth code logs Apple configuration, key material, OAuth tokens, PII, provider IDs, session IDs, device IDs, or fingerprints through unconditional debug statements.
- Every full credential issuance path rejects inactive accounts.
- A decoded access token and refresh token always carry the persisted canonical session UUID.
- The canonical session UUID survives arbitrary refresh rotations and step-up token replacement.
- Logout and explicit session revocation reject the corresponding current access token on the next request when Redis is healthy.
- Redis degradation is visible through bounded metrics and rate-limited logs and remains bounded by access-token TTL.
- Focused tests and all backend security gates pass with no new baseline exemptions.
