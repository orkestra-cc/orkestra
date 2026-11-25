# Notification push channel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a push channel (FCM, `noop` by default) and a per-user/per-device token registry to the core `notification` module, with tokens bound to the auth session so no push reaches a device after its session ends.

**Architecture:** New SDK contracts (`iface.PushSender` asserted off the `ServiceNotificationSender` object, `iface.SessionActivityReader` asserted off each tier's auth service, `ctxauth.GetSessionID`). `notification` gets a device-token repository, self-service device routes on both tiers, a `PushDriver` seam (`noop`, `fcm` over HTTP v1 with `golang.org/x/oauth2/google`), `SendPush` (validation, preferences, session filter, idempotency, delivery log), an admin test push, GDPR coverage and a daily cleanup job on the jobs framework.

**Tech Stack:** Go 1.26, Huma v2, mongo-driver v1, `golang.org/x/oauth2` (already a direct dependency), `net/http`.

**Spec:** the push-channel design, decisions K1–K15. Amendment: the console gets no new page (no email test-send UI exists either); the Push config group renders automatically from `ConfigGroups()`.

## Global Constraints

- No new third-party dependency (`firebase.google.com/go` is forbidden; FCM via `net/http` + `golang.org/x/oauth2/google.JWTConfigFromJSON`, scope `https://www.googleapis.com/auth/firebase.messaging`).
- `pkg/sdk/**` imports nothing from `backend/internal/`; SDK changes are additive (new interfaces, new ctxauth key/getter); `iface.NotificationSender` and `NotificationRequest` are unchanged.
- The push token is never returned by an API nor logged except its first 8 characters.
- Limits (spec K3): ≤ 500 users per request, title ≤ 200, body ≤ 1000 characters, `Data` ≤ 3 KiB serialized; reserved data keys `from`, `notification`, `message_type` and prefixes `google.`, `gcm.` rejected.
- Config keys (spec K9): `push.provider` (enum `noop`/`fcm`, default `noop`, env `NOTIFICATION_PUSH_PROVIDER`), `push.fcm.service_account_json` (`FieldSecret`, env `FCM_SERVICE_ACCOUNT_JSON`), `push.fcm.project_id` (optional; default from the JSON); group **Push**; FCM fields `DependsOn: push.provider in [fcm]`.
- FCM driver (K10): one `messages:send` per token, ≤ 8 in parallel, 10 s timeout per call, no internal retry; `UNREGISTERED`/404 → disable `fcm_unregistered`; `INVALID_ARGUMENT` about the token → disable `fcm_invalid`; 429/5xx → transient failure.
- Session filter (K7b) is fail-closed: if session activity cannot be verified, the push fails for that user.
- Cleanup job (K7c): `notification.push_tokens_cleanup`, daily; disables tokens of ended sessions or not seen for 60 days; deletes tokens disabled for more than 90 days.
- Commits: conventional subject; trailers `Prop: upstream` and `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Gate only via `ci-gate make ci`.

## Review Focus

1. Shared tablet: user A registers token T on device D, then user B logs in on D and registers the same T → A's row is disabled (`reassigned`) and a push to A does not reach T. Test in Task 2.
2. Session ended by any path (logout, admin terminate-all, expiry) without the app calling DELETE → no push is sent to that token. Test in Task 4 (fake SessionActivityReader returning false) and Task 1 (ActiveSessions on real session docs: inactive and expired both false).
3. FCM credentials malformed or token endpoint unreachable → `SendPush` returns a failed result with a bounded diagnostic, no panic, no token disabled. Test in Task 3.
4. A `marketing` push to a user who opted out of the category on channel `push` is skipped; a `transactional` one is delivered. Test in Task 4.
5. Duplicate request with the same `IdempotencyKey` for the same user does not deliver twice. Test in Task 4.

---

### Task 1: SDK contracts, session id in context, auth `ActiveSessions`

**Files:**
- Create: `backend/pkg/sdk/iface/push.go`
- Modify: `backend/pkg/sdk/ctxauth/ctxauth.go` (`KeySessionID = "sessionID"`, `GetSessionID(ctx) (string, bool)`), `backend/internal/shared/middleware/auth.go` (stamp `claims.SessionID` wherever `KeyUserUUID` is stamped — both paths, ~lines 518 and 948), auth tier services (implement `ActiveSessions` on the objects registered as `ServiceAuthService` and `ServiceClientAuthService`, reading that tier's sessions collection through the session repository; add `GetActiveByUUIDs` to `internal/core/auth/repository/auth_session_repository.go`)
- Test: `backend/pkg/sdk/iface/push_test.go`, `backend/pkg/sdk/ctxauth/ctxauth_test.go`, a middleware test next to the existing ones, `backend/internal/core/auth/repository/*_integration_test.go`, a service test proving the type assertion `module.GetTyped`/`.(iface.SessionActivityReader)` succeeds on both registered services

**Interfaces — Produces:**
```go
// iface/push.go
type PushRequest struct {
    Tier string // "operator" | "client"
    UserUUIDs []string
    Category, Type string // Type "transactional" | "marketing"
    Title, Body string
    Data map[string]string
    CollapseKey string
    Priority string // "high" | "normal" (default "high")
    TTL time.Duration // 0 = FCM default
    IdempotencyKey string
}
type PushUserResult struct{ UserUUID, Status string; Attempted, Delivered, Disabled int; Error string } // Status: delivered | partial | failed | skipped_preference | skipped_no_device | skipped_duplicate
type PushResult struct{ Users []PushUserResult }
type PushSender interface{ SendPush(ctx context.Context, req PushRequest) (*PushResult, error) }
func (r PushRequest) Validate() error // limits from Global Constraints; errors wrap ErrPushInvalidRequest
var ErrPushInvalidRequest, ErrPushUnavailable error
type SessionActivityReader interface{ ActiveSessions(ctx context.Context, sessionIDs []string) (map[string]bool, error) } // true only for isActive && expiresAt > now; unknown ids absent/false
```
- [ ] **Step 1: Failing tests** — `TestPushRequestValidate` (501 users, 201-char title, 1001-char body, 3 KiB+1 data, keys `from`, `google.x`, `gcm.y`, bad Type, bad Tier, empty users → each wraps `ErrPushInvalidRequest`; a valid request passes); `TestGetSessionID`; middleware test asserting `ctxauth.GetSessionID` equals the token's `sid`; repository `TestGetActiveByUUIDs` (active, inactive, expired, other tier); `TestActiveSessionsBothTiers`.
- [ ] **Step 2:** run `cd backend && go test ./pkg/sdk/iface/ ./pkg/sdk/ctxauth/ ./internal/shared/middleware/ ./internal/core/auth/...` (+ `MONGO_TEST_URI='mongodb://127.0.0.1:27317/?directConnection=true'`) → FAIL; implement; → PASS.
- [ ] **Step 3: Commit** `feat(sdk): push sender contract and session activity reader`.

### Task 2: Device-token registry

**Files:**
- Create: `backend/internal/core/notification/models/device_token.go`, `backend/internal/core/notification/repository/device_token_repository.go`
- Modify: `backend/internal/core/notification/models/collections.go` (`DeviceTokensCollection = "notification_device_tokens"`), `backend/internal/core/notification/module.go` `Collections()` (indexes below)
- Test: `backend/internal/core/notification/repository/device_token_repository_integration_test.go`

**Interfaces — Produces:**
```go
type DeviceToken struct {
    UUID, Tier, UserUUID, DeviceID, SessionID, Token, Platform, AppVersion string
    Active bool
    CreatedAt, UpdatedAt, LastSeenAt time.Time
    DisabledAt *time.Time; DisabledReason string // logout | reassigned | fcm_unregistered | fcm_invalid | session_ended | stale | erased
} // json:"-" on Token; bson camelCase like the module's other models
type DeviceTokenRepository interface {
    Upsert(ctx, d *models.DeviceToken, now time.Time) (*models.DeviceToken, error) // by tier+userUuid+deviceId; sets Active, clears Disabled*; first disables (reason reassigned) any OTHER active row with the same token
    Disable(ctx, tier, userUUID, deviceID, reason string, now time.Time) (bool, error)
    DisableByToken(ctx, token, reason string, now time.Time) error
    ListForUser(ctx, tier, userUUID string) ([]models.DeviceToken, error)
    ActiveForUsers(ctx, tier string, userUUIDs []string) ([]models.DeviceToken, error)
    ListActiveBatch(ctx, afterUUID string, limit int) ([]models.DeviceToken, error) // for cleanup, uuid order
    DisableStale(ctx, seenBefore time.Time, now time.Time) (int64, error)
    DeleteDisabledBefore(ctx, before time.Time) (int64, error)
    DeleteForUser(ctx, tier, userUUID string) (int64, error)
}
```
Indexes: `uuid` unique; `{tier, userUuid, deviceId}` unique; `{token}` unique with `PartialFilter: {"active": true}`; `{tier, userUuid, active}`; `{lastSeenAt}`; `{disabledAt}` sparse.
- [ ] **Step 1: Failing tests** — `TestUpsertCreatesAndRefreshes` (second upsert same device updates token/lastSeenAt, one row); `TestUpsertReassignsSharedToken` (Review Focus 1); `TestDisable`; `TestActiveForUsersSkipsDisabled`; `TestDisableStale`; `TestDeleteDisabledBefore`; `TestDeleteForUser`; `TestTokenUniqueAmongActive` (two active rows with one token impossible).
- [ ] **Step 2:** FAIL → implement → PASS (`MONGO_TEST_URI` set).
- [ ] **Step 3: Commit** `feat(notification): device-token registry`.

### Task 3: Push drivers (`noop`, `fcm`) and config

**Files:**
- Create: `backend/internal/core/notification/services/push_driver.go`, `push_driver_noop.go`, `push_driver_fcm.go`
- Modify: `backend/internal/core/notification/module.go` (`ConfigSchema` fields + `ConfigGroups` group **Push**, see Global Constraints), config loading alongside the email snapshot loader
- Test: `push_driver_noop_test.go`, `push_driver_fcm_test.go` (httptest server for both the OAuth token endpoint — via the service-account JSON's `token_uri` — and the FCM endpoint, base URL injectable)

**Interfaces — Produces:**
```go
type PushMessage struct{ Token, Title, Body string; Data map[string]string; CollapseKey, Priority string; TTL time.Duration; Platform string }
type PushOutcome struct{ Delivered bool; Permanent bool; Reason string /* fcm_unregistered|fcm_invalid|transient|... */; Err error }
type PushDriver interface{ Name() string; Send(ctx context.Context, msgs []PushMessage) []PushOutcome } // same order as msgs
func NewNoopPushDriver(logger *slog.Logger) PushDriver // logs token prefix (8 chars), title length, data keys; all Delivered
func NewFCMPushDriver(serviceAccountJSON []byte, projectID string, opt FCMOptions) (PushDriver, error) // FCMOptions{BaseURL, HTTPClient, Concurrency 8, Timeout 10s}
```
FCM body: `{"message":{"token","notification":{"title","body"},"data",...,"android":{"priority":"HIGH"|"NORMAL","collapse_key","ttl":"<s>s","notification":{"tag":collapseKey}},"apns":{"headers":{"apns-priority":"10"|"5","apns-collapse-id":collapseKey,"apns-expiration"}}}}`; error mapping from the FCM v1 error body (`error.details[].errorCode` = `UNREGISTERED` / `INVALID_ARGUMENT`, HTTP 404/400) per Global Constraints.
- [ ] **Step 1: Failing tests** — noop delivers all and never logs the full token; FCM: success; 404 UNREGISTERED → Permanent `fcm_unregistered`; 400 INVALID_ARGUMENT → Permanent `fcm_invalid`; 429 and 503 → not permanent; token endpoint down → all not delivered, not permanent, bounded `Err` (Review Focus 3); malformed JSON → constructor error; concurrency never exceeds 8 (counting server); request carries `Authorization: Bearer <token>` and the expected JSON fields (collapse key, priority).
- [ ] **Step 2:** FAIL → implement → PASS.
- [ ] **Step 3: Commit** `feat(notification): noop and FCM push drivers`.

### Task 4: `SendPush`

**Files:**
- Create: `backend/internal/core/notification/services/push_service.go`
- Modify: `notification_service.go` (the registered sender object implements `iface.PushSender` by delegating to the push service), `module.go` wiring (session readers resolved lazily per send from `ServiceAuthService`/`ServiceClientAuthService` because auth inits after notification)
- Test: `push_service_test.go`

**Behaviour:** `req.Validate()` → `ErrPushInvalidRequest`; driver from config per send (`noop` if unset); per user: idempotency (existing log idempotency lookup keyed by `IdempotencyKey`+user; duplicate → `skipped_duplicate`), preferences (`marketing` + opted out on channel `push` → `skipped_preference`), `ActiveForUsers`, session filter via `SessionActivityReader` (reader missing or error → user `failed`, fail closed; ended → token disabled `session_ended` and not sent), no devices → `skipped_no_device`, driver send, permanent outcomes → `DisableByToken` with the reason; one `notification_messages` row per (request, user) with `channel: "push"`, `recipientUserUuid`, counts, status, subject = title truncated to 200, body never stored, idempotency key.
- [ ] **Step 1: Failing tests** — Review Focus 2, 4, 5; mixed delivery → `partial`; permanent outcome disables the token; invalid request returns error and writes nothing; type assertion `var _ iface.PushSender = (*NotificationService)(nil)`.
- [ ] **Step 2:** FAIL → implement → PASS.
- [ ] **Step 3: Commit** `feat(notification): SendPush with session filter, preferences and log`.

### Task 5: HTTP routes, admin test push, GDPR

**Files:**
- Modify: `backend/internal/core/notification/handlers/notification_handler.go` (or a new `push_handler.go`), `module.go` `RegisterRoutes` (self-service group on `ri.Operator.ProtectedRouter` with `RequireGlobal()` like the preference routes, and the same handler on `ri.Client.ProtectedRouter` when `ri.Client != nil`, tier passed in), `services/pii_producer.go`, error codes in `backend/internal/shared/errcode/codes.go` (`notification.push_invalid_request` 422, `notification.push_device_not_found` 404, `notification.push_session_missing` 401 when the token has no `sid`), OpenAPI regenerated
- Test: handler tests per route, PII producer tests

**Routes:** `PUT /v1/notifications/push/devices/{deviceId}` body `{token (1–4096), platform (ios|android|web), appVersion? (≤ 64)}` → device view without token; `DELETE /v1/notifications/push/devices/{deviceId}` → 204 (disable reason `logout`); `GET /v1/notifications/push/devices` → `{devices:[{deviceId, platform, appVersion, active, createdAt, lastSeenAt}]}`; admin `POST /v1/notifications/push/test` `{tier, userUuid, title?, body?}` in the existing admin group, gated additionally by `RequireSystemPermission("notification.test")` → `PushResult`. Huma schema type names prefixed `NotificationPush…`.
GDPR: export devices (platform, deviceId, appVersion, dates, `token` masked to its first 8 chars + `…`), purge = `DeleteForUser` under both erase modes, both tiers.
- [ ] **Step 1: Failing tests** — register binds `sid` from ctx; missing sid → 401 code; GET never contains a token field; DELETE unknown → 404; client-tier route works with client ctx; test push requires `notification.test`; PII export masks the token; purge deletes.
- [ ] **Step 2:** FAIL → implement → PASS; `make openapi-check` (regenerate as documented; needs `MONGO_URI` = CI Mongo).
- [ ] **Step 3: Commit** `feat(notification): device routes, test push and GDPR coverage`.

### Task 6: Cleanup job and documentation (after the jobs framework is on `dev`)

**Files:**
- Modify: `backend/internal/core/notification/module.go` (`Jobs()` returning `push_tokens_cleanup`: `jobs.Every(24*time.Hour)`, Title "Clean up push device tokens"; walks active tokens in batches of 500 via `ListActiveBatch`, disables `session_ended` where `ActiveSessions` says false, then `DisableStale(now-60d)` and `DeleteDisabledBefore(now-90d)`; details `sessionEnded`, `stale`, `deleted`; reader unavailable → return error, disable nothing)
- Docs: `backend/internal/core/notification/AGENTS.md` (Push section: contract, config, collection, routes, session binding, cleanup, what's not here), `backend/internal/core/auth/AGENTS.md` (`ActiveSessions`, `sid` in ctx), `backend/pkg/sdk/AGENTS.md` (iface list: `PushSender`, `SessionActivityReader`; ctxauth getter), `docs/site/modules/core/notification.mdx` (Push section with the app contract: register after every login and on FCM token refresh, DELETE on logout, platforms, what the server does on session end), a one-line note in `docs/adr/0019-notification-multi-sender.md` "Related" pointing to the push channel.
- Test: `module_jobs_test.go` (job definition valid; cleanup with fakes disables ended sessions and fails closed without a reader).
- [ ] Steps: failing test → implement → PASS → commit `feat(notification): daily push-token cleanup job` and `docs(notification): push channel`.
