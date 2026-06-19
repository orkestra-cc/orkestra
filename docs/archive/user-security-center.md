# Plan — User self-service security center (`/user/security`)

**Status:** ✅ Shipped. Phases 0–6 in `dd4c733 feat(auth): self-service /user/security page with session control`; Phase 7 OAuth linking in `032ce33`. Follow-ups still open: client-tier mirror on `frontend-client/`, persistent `auth_security_events`, email-change with re-verification, WebAuthn key rename, login history endpoint.

## Context

The admin counterpart shipped 2026-05-10 (commit `55d19ac`): operators can now inspect and manage another user's auth from `/admin/user/profile/:id`. The mirror surface — what an end user can do for **their own** account — is incomplete and partly broken on `frontend-admin`:

- **`/user/settings::ChangePassword.tsx` is a Falcon stub** — `handleSubmit` is a no-op, inputs are `type="text"`, the form never calls `useChangePasswordMutation`. A user clicking "Update Password" today gets nothing.
- **No self-service OAuth unlink UI** — backend has `AuthService.RemoveOAuthLink` but no public route exposes it.
- **No self-service sessions list** — `AuthService.GetUserSessionsByUUID` and friends are stubs that return empty/`"not yet implemented"`. The repo has the data; the wiring is missing.
- **No backup-codes regeneration** — codes are write-only at TOTP enrollment.
- **No trusted-devices UI** — `device_trust_handler.go:135–167` is fully wired on the backend; frontend has nothing.

Goal: one dedicated `/user/security` page that is the canonical self-service auth-management surface for an operator user. Wire what's broken, expose what's missing, mirror the admin card's safeguards.

Scope (confirmed with user):
- **Operator tier only this iteration.** Client-tier reuse on `frontend-client/AccountSecurityPage.tsx` is a small follow-up that consumes the same backend.
- **Sessions IS in scope** — including wiring the four service stubs to the existing repos.
- **All four other items in:** wire ChangePassword, OAuth self-unlink, trusted-devices UI, backup codes regenerate.

## Architecture decisions

- **New page at `/user/security`** with URL-synced tabs (`?tab=password|mfa|oauth|sessions|devices|backup-codes`) per the project's `url-tabs` skill. NOT a tab inside `/user/settings`. Top-level page matches GitHub/Auth0 mental models, makes the suspicious-login email's deep link unambiguous, and lets `/user/settings` stay focused on profile/preferences.
- **Strip `ChangePassword` and `MfaSettings` from `Settings.tsx`** so there's one source of truth. `/user/settings` keeps profile/notifications/billing-stub.
- **Reuse the admin `AuthMethodsView` shape** for the self-service aggregator (`GET /v1/auth/operator/me/auth-methods`). No admin-only fields to redact; one shape simplifies the page.
- **Extract a shared lockout helper** out of `AdminUnlinkOAuth` (`auth_service.go:340-…`) so the new `SelfUnlinkOAuth` reuses the same `ErrLastCredentialRemoval` math. Self-action check is dropped (self-action is the entire point); last-credential check is kept (UX bug regardless of intent).
- **Step-up gating per endpoint**:
  - `DELETE /me/oauth/{provider}` — yes (credential removal, mirrors admin).
  - `DELETE /me/sessions/{id}` — yes. Reject revoke-current with 409 `cannot_revoke_current`; the user has the existing logout for that.
  - `DELETE /me/sessions` — yes. Revoke-all-except-current to avoid an in-flight self-logout.
  - `POST /me/mfa/backup-codes/regenerate` — yes (destroys existing codes).
  - `GET` endpoints — no step-up.
- **Session revocation = three coordinated ops**: `refreshTokenRepo.RevokeTokensBySession(...)` → mark `AuthSession.IsActive=false` → `sessionRevocationSvc.Revoke(sid, reason)` to push the sid into Redis so middleware rejects in-flight access tokens. Wrap in a service helper so the per-session and revoke-all paths share one code path.
- **Audit logging**: each successful self-service action emits `slog.Info("self_auth_action", event=…, userUUID=…)` mirroring the admin paths. Persistent `auth_security_events` rows remain a follow-up tied to the same `RecordSecurityEvent` work.

## Sentinel errors to add (`auth_service.go`)

- `ErrCannotRevokeCurrent` — 409 `cannot_revoke_current`
- `ErrSessionNotFound` — 404
- Reuse existing `ErrLastCredentialRemoval`, `ErrOAuthLinkNotFound`.

## API contract

### Reads
- `GET  /v1/auth/operator/me/auth-methods` → `models.AuthMethodsView` — same shape as admin.
- `GET  /v1/auth/operator/me/sessions` → `models.SessionsResponse` — `[]SessionInfo` filtered to `isActive && !expired`, with `IsCurrent` flag from JWT `sid`.

### Mutations (all step-up gated except where noted)
- `DELETE /v1/auth/operator/me/oauth/{provider}` → 204; 409 `last_credential` on lockout; 404 `provider_not_linked`.
- `DELETE /v1/auth/operator/me/sessions/{sessionId}` → 204; 409 `cannot_revoke_current`; 404 if session not owned.
- `DELETE /v1/auth/operator/me/sessions` → `{ revoked: int }`; revokes all-except-current.
- `POST   /v1/auth/operator/me/mfa/backup-codes/regenerate` → `{ codes: string[] }`; codes returned exactly once.

Existing endpoints reused unchanged: `POST /change-password`, MFA enroll/confirm/status/remove, WebAuthn register/list/remove, `GET/DELETE /me/devices/trust`.

## Critical files to modify

### Backend
- `backend/internal/core/auth/services/auth_service.go` — add `SelfUnlinkOAuth`, replace stubs `GetUserSessionsByUUID`/`TerminateSessionByUUID`/`TerminateAllSessionsByUUID`, add `revokeSessionInternal` helper (refresh-tokens + session doc + Redis sid), add `ErrCannotRevokeCurrent` / `ErrSessionNotFound`. Extract `wouldLockOutOAuthUnlink(target, provider)` helper from `AdminUnlinkOAuth` so both paths share the math.
- `backend/internal/core/auth/services/mfa_service.go` — add `RegenerateBackupCodes(ctx, userUUID) ([]string, error)` that calls existing `generateBackupCodes(s.recoveryCodesCount(ctx))`, persists hashed codes via the factor repo's update path (atomic replace, not append), returns plaintext.
- `backend/internal/core/auth/handlers/self_user_auth_handler.go` — **new file**. Hosts the GET aggregator, GET sessions, DELETE oauth/{provider}, DELETE sessions/{id}, DELETE sessions, plus `mapSelfAuthError`. Mirrors the structure of `admin_user_auth_handler.go`.
- `backend/internal/core/auth/handlers/mfa_handler.go` — add `RegenerateBackupCodes` handler + step-up Register method `RegisterStepUpRoutes` extension.
- `backend/internal/core/auth/module.go` — wire the new handler to the operator router; mount the GET routes in the protected (`RequireGlobal()`) group, the destructive routes in the step-up (`RequireStepUp(5m)`) group.
- `backend/internal/core/auth/CLAUDE.md` — add the new routes to the Protected table; document the new sentinels and the session-revoke-current safeguard.

### Frontend (`frontend-admin/`)
- `src/store/api/authApi.ts` — add hooks: `useGetSelfAuthMethodsQuery`, `useUnlinkOauthSelfMutation`, `useGetMySessionsQuery`, `useRevokeSessionMutation`, `useRevokeAllSessionsMutation`. New tags `Sessions`, `SelfAuthMethods` in `baseApi.ts`.
- `src/store/api/mfaApi.ts` — add `useRegenerateBackupCodesMutation`. Invalidate `MFA` tag.
- `src/store/api/deviceTrustApi.ts` — **new slice**. `useListTrustedDevicesQuery`, `useRevokeTrustedDeviceMutation`, `useRevokeAllTrustedDevicesMutation` against the existing `device_trust_handler` routes.
- `src/pages/user/security/index.tsx` — **new**. Page shell with URL-synced tabs.
- `src/pages/user/security/PasswordTab.tsx` — **new**. Real `useChangePasswordMutation` wiring with react-hook-form, `type="password"` inputs, password-policy feedback from `useGetAuthPolicyQuery`, toast.
- `src/pages/user/security/MfaTab.tsx` — wraps the existing `MfaSettings` from `pages/user/settings/mfa/`.
- `src/pages/user/security/LinkedProvidersTab.tsx` — list OAuth identities, per-row "Unlink" with confirm modal → `useUnlinkOauthSelfMutation`. Step-up handled by the global `StepUpModal` already in the app.
- `src/pages/user/security/SessionsTab.tsx` — table of active sessions; `IsCurrent` row gets a "Current session" badge and disabled revoke; "Revoke all others" button.
- `src/pages/user/security/TrustedDevicesTab.tsx` — same shape against `deviceTrustApi`.
- `src/pages/user/security/BackupCodesTab.tsx` — shows count from auth-methods aggregator, "Regenerate" button → confirmation → step-up → reuse `BackupCodesDisplay` (extracted from `MfaEnrollWizard.tsx`'s step 2).
- `src/pages/user/security/BackupCodesDisplay.tsx` — extracted shared component.
- `src/routes/coreRoutes.tsx` + `src/routes/paths.ts` — register `/user/security`, add `paths.userSecurity`.
- `src/pages/user/settings/Settings.tsx` — strip `ChangePassword` and `MfaSettings` cards. Add a "Manage security" link card pointing at `/user/security`.
- `src/components/navbar/top/ProfileDropdown.tsx` — add "Security" link below "Settings".

### Notification template (suspicious-login CTA)
- `backend/internal/core/notification/services/default_templates.go` (or wherever the auth.admin_suspicious_login / new_device_login templates live) — confirm the CTA URL points to `/user/security?tab=sessions`. If it doesn't, update.

## Verification

End-to-end manual test on the dev stack:
```bash
cd docker && docker compose -f docker-compose.dev.yml up -d
ORKESTRA_API_URL=http://localhost:3000 ./scripts/devtoken.sh administrator
```
1. Sign in as a normal operator user. Visit `/user/security` — confirm 6 tabs render (Password / MFA / OAuth / Sessions / Trusted devices / Backup codes).
2. **Password tab**: submit a wrong old password → toast error. Submit correct → toast success; verify `db.operator_users.findOne(…).passwordUpdatedAt` advanced.
3. **MFA tab**: enroll TOTP, then on **Backup codes tab** click "Regenerate" → step-up modal → enter code → 10 new codes shown once → verify old codes no longer work via `/v1/auth/operator/mfa/verify`.
4. **OAuth tab**: with two OAuth providers + password → unlink one → row disappears, login still works. Force lockout: remove password (unsupported today; flip `passwordHash` directly in mongo for the test) and try to unlink the only OAuth → 409 `last_credential`, UI shows the inline guard.
5. **Sessions tab**: log in from a second browser → see two rows, current flagged → revoke the other → other browser's next request 401s, current keeps working. Click "Revoke all others" → all except current revoked.
6. Try to revoke the current session via the API directly → 409 `cannot_revoke_current`.
7. **Trusted devices tab**: log in with `trustDevice=true`, see entry; revoke → entry gone; re-login on the same device requires MFA again.
8. Open `/user/settings` — confirm `ChangePassword` and `MfaSettings` cards are gone, replaced by a "Manage security" link.

Backend tests (mirror `auth_service_admin_unlink_test.go` style — service-level, fakes from `gates_fakes_test.go`):
- `services/auth_service_self_unlink_test.go`: happy path, last-credential 409, missing provider 404. Self-action is intentionally allowed.
- `services/auth_service_sessions_test.go`: list filters expired/revoked; `IsCurrent` flag truthy when sid matches; `RevokeOne` rejects current; `RevokeAll` excludes current; both push to Redis revocation set.
- `services/mfa_service_regenerate_test.go`: replaces (does not append) codes, count matches `recoveryCodesCount`, requires factor enrolled (404 otherwise).
- `handlers/route_mount_test.go` extension: assert all 5 new routes mount on the operator host with the right gates (step-up only on the destructive subset).

Frontend tests:
- MSW handlers in `test/handlers.ts` for the 6 endpoints.
- Page render tests for each tab (empty + populated).
- Step-up replay smoke test: trigger a destructive action, mock `401 step_up_required`, simulate `StepUpModal` flow, confirm replay.
- `npm run typecheck` + `npm test` clean.

## Phased ordering

1. **Backend Phase 0 — pre-flight audit (read-only):** confirm `iface.UserProvider` membership of session methods (currently appear local to `*authService` per `auth_service.go:528`), the `sid` context-key name in `shared/middleware/auth.go`, and that `AuthSessionDoc.LastActivity` / `DeviceID` / `Platform` / `IPAddress` are populated at session-create time.
2. **Backend Phase 1 — wire the four session stubs + `SelfUnlinkOAuth` + `RegenerateBackupCodes`** with service-level tests + new sentinels. Lockout helper extracted from `AdminUnlinkOAuth` and shared.
3. **Backend Phase 2 — handlers + routes + error mapping** in a new `self_user_auth_handler.go`, mount in `module.go` operator router; backup-codes route lives on `MFAHandler` for cohesion. Step-up middleware applied per the matrix above. Composition test.
4. **Backend Phase 3 — `slog.Info("self_auth_action", …)` audit lines on each handler success path.** Same RecordSecurityEvent follow-up flagged.
5. **Frontend Phase 4 — RTK Query slices**: extend `authApi.ts`, add `deviceTrustApi.ts`, extend `mfaApi.ts`. New tags in `baseApi.ts`.
6. **Frontend Phase 5 — `/user/security` page + 6 tab components**. Reuse `MfaSettings`, extract `BackupCodesDisplay`. Wire ChangePassword to the real mutation. Strip stale cards from `Settings.tsx`. Register route + paths constant + navbar link.
7. **Frontend Phase 6 — tests**: MSW handlers, render tests, step-up replay.
8. **Phase 7 — polish**: update suspicious-login email template CTA → `/user/security?tab=sessions`. Update `auth/CLAUDE.md` with the new routes.
9. **Manual E2E + screenshots in PR.**

## Out of scope (explicit follow-ups)

- **Client-tier reuse** of the same surface on `frontend-client/AccountSecurityPage.tsx` — separate PR; backend endpoints will need a parallel mount on the client host.
- **Real `RecordSecurityEvent` persistence.** The four self-service paths emit `slog.Info("self_auth_action", …)` for now; back-fill with `auth_security_events` rows once the audit pipeline lands. Same follow-up as the admin work.
- **Email change with re-verification** — separate flow with template work; deferred.
- **WebAuthn credential rename** — small follow-up; current model has `Name` set at enroll only.
- **Mobile (Flutter)** — `mobile/lib` is scaffold-only; not a blocker.
- **Login history / security events read endpoint** — depends on the persistent audit pipeline.
