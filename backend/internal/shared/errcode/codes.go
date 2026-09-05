package errcode

// Code constants. Every code is <module>.<situation> in snake_case;
// the module owns its namespace, the situation names the failure
// semantically (not the HTTP status). Adding a code is additive — the
// SPA falls back to `detail` when an unknown code arrives — but
// renaming or removing one is a wire-contract break. codes_test.go
// pins every const here against a golden snapshot so a silent rename
// fails CI loudly.
//
// Grouping below is by module to make it easy to scan ownership.
// Insert new entries inside the matching block; one-off codes that
// don't belong to a module (rare) go at the bottom.

// --- auth ---

// AuthEmailInUse signals that a sign-up or invite was rejected because
// the email already maps to a live user in this audience tier. 409.
const AuthEmailInUse = "auth.email_in_use"

// AuthJWTNotConfigured signals that the server cannot read its RS256
// signing keys, so no token can be issued or verified. This is a
// deployment fault, never the caller's: it answers 503, and the detail
// names the cause so an operator reading the response alone can act on
// it without tailing container logs.
const AuthJWTNotConfigured = "auth.jwt_not_configured"

// AuthUnavailable is the honest fallback for an unrecognized error on
// any auth mapper (password, MFA, WebAuthn). An error the handler
// cannot name is a server fault, so it answers 500 — never a 4xx,
// which would blame the caller for the server's own gap.
const AuthUnavailable = "auth.unavailable"

// AuthEmailNotVerified signals that credentials were valid but the
// account's email address has not been verified yet — the caller must
// check their inbox (or request a new verification email) before
// retrying. 403.
const AuthEmailNotVerified = "auth.email_not_verified"

// AuthRegistrationDisabled signals that self-service registration is
// turned off for the surface (operator or client) the request came in
// on. 403.
const AuthRegistrationDisabled = "auth.registration_disabled"

// AuthEmailDomainNotAllowed signals that a registration was rejected
// because the email's domain is not on the surface's allow-list. 403.
const AuthEmailDomainNotAllowed = "auth.email_domain_not_allowed"

// AuthLoginDisabled signals that login (password or OAuth) is turned
// off for the surface the request came in on. 403.
const AuthLoginDisabled = "auth.login_disabled"

// AuthCountryBlocked signals that the request's resolved country is on
// the surface's block-list. 403.
const AuthCountryBlocked = "auth.country_blocked"

// AuthPasswordConfirmUnavailable signals that the step-up
// "confirm with your password" endpoint cannot be used by this
// account — it has an MFA factor enrolled, or no password at all — so
// the caller must use MFA or reauthenticate via OAuth instead. 409.
const AuthPasswordConfirmUnavailable = "auth.password_confirm_unavailable"

// AuthOAuthProviderDisabled signals that the requested OAuth provider
// is not enabled for the surface the request came in on. 403.
const AuthOAuthProviderDisabled = "auth.oauth_provider_disabled"

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

// AuthPasswordLoginDisabled signals that the email/password method is
// administratively disabled for the surface the request came in on
// (auth module keys passwordLoginEnabled{Admin,Client}). 403 on the
// self-service routes (login, register, forgot-password, password-
// sourced MFA completion); 409 on the two admin send-password-reset
// routes, where the operator asked to mint a reset for a method the
// target's surface refuses.
const AuthPasswordLoginDisabled = "auth.password_login_disabled"

// AuthLoginMethodLockout signals that a module-config mutation would
// leave a surface with no way to authenticate: password off with no
// structurally configured OAuth provider, or with verified-email
// auto-link off. Emitted by the auth module's snapshot validator on
// every mutation surface. 422.
const AuthLoginMethodLockout = "auth.login_method_lockout"

// AuthTooManyAttempts signals that a per-IP, per-email or per-client
// attempt counter reached its threshold inside its window, or that the
// account carries a durable lock that has not yet expired. Always 429,
// always accompanied by a Retry-After header carrying the remaining
// life of the window (never below 1 second).
//
// It deliberately covers BOTH the unknown-address and the known-address
// case: answering 429 for one and 401 for the other would be an
// existence oracle, which is the defect (M-7) the counters close.
const AuthTooManyAttempts = "auth.too_many_attempts"

// AuthIPThresholdBelowAccount signals a refused module-config write:
// ipLockoutThreshold must be >= accountLockoutThreshold. A source
// address that locks BEFORE the account does turns a shared egress into
// an existence oracle — the caller learns "an account is being attacked
// behind this NAT" from the early 429.
const AuthIPThresholdBelowAccount = "auth.ip_threshold_below_account"

// --- authz ---

// AuthzPermissionUnknown signals that a role create or update named a
// permission key that no module has registered. The catalog is the union
// of every module's Permissions(), collected once at boot; a key outside
// it can never be checked by anything, so a role carrying it is at best
// dead weight and at worst a typo hiding a real grant. The detail names
// the offending key. 422.
const AuthzPermissionUnknown = "authz.permission_unknown"

// AuthzSystemPermissionForbidden signals that a role create or update
// tried to put a platform-reserved key (or the wildcard "*") into a
// tenant-scoped custom role. Platform permissions are granted through
// platform system roles and global bindings only — a tenant role
// carrying one would be an escalation path out of its own tenant. Binds
// every caller, super_admin included: the wildcard governs what an ACTOR
// may grant, not what a TENANT role may carry. The detail names the
// offending key. 422.
const AuthzSystemPermissionForbidden = "authz.system_permission_forbidden"

// AuthzCacheUnavailable signals that a permission GRANT was REFUSED
// because the effective-permission cache could not be retired. A grant
// retires the cached verdicts it invalidates before it writes; a counter
// the store cannot bump means the change's effect could not be
// guaranteed, so nothing was written and the caller may retry once the
// cache store recovers. It is the server's own transient fault, never
// the caller's request — 503, never a 4xx, and never the codeless 500
// the role-update path answered before.
//
// REVOCATIONS never carry this code. A stale verdict after a grant is a
// DENY (harmless, late); after a revocation it is an ALLOW, and refusing
// the revocation would leave the privilege held indefinitely instead of
// for at most the cache's 60s TTL. Revocations therefore write first and
// report an invalidation failure through logs and metrics.
//
// A deployment with NO cache configured is a different thing entirely:
// there are no cached verdicts to retire, so those mutations succeed and
// this code is never emitted.
const AuthzCacheUnavailable = "authz.cache_unavailable"

// --- tenant ---

// TenantSlugAlreadyInUse signals that a tenant create or update would reuse an
// existing organization slug. 409.
const TenantSlugAlreadyInUse = "tenant.slug_already_in_use"

// TenantProvisioningLocked signals that the active provisioning policy
// refused to create or assign a tenant. Two distinct call sites share this
// one stable code with two different fixed details, never a shared string:
//   - Tier-1 (internal): the `single` provisioning mode blocks occupying a
//     second Tier-1 provisioning slot.
//   - Tier-2 (external): `external.mode == manual` blocks lazy
//     personal-tenant provisioning for a caller with no admin-assigned
//     tenant. `single` is not a valid external mode, so this branch must
//     never reuse the Tier-1 wording. 409.
const TenantProvisioningLocked = "tenant.provisioning_locked"

// TenantInternalModeInvalid rejects a Tier-1 provisioning policy that is
// not manual or single (open was removed from Tier-1). 422.
const TenantInternalModeInvalid = "tenant.internal_mode_invalid"

// TenantSingleModeConflict rejects selecting `single` while more than one
// Tier-1 tenant occupies a provisioning slot. 422.
const TenantSingleModeConflict = "tenant.single_mode_conflict"

// TenantDefaultReassignmentRequired blocks suspending, archiving, purging,
// or deleting the platform default tenant before the default is
// transferred. 409.
const TenantDefaultReassignmentRequired = "tenant.default_reassignment_required"

// --- user ---

// UserSelfDeleteForbidden signals that an admin tried to delete (or
// soft-delete) their own user row. The /admin/users surface must never
// let the caller wipe themselves — they'd lock themselves out and the
// audit trail loses its source. 403.
const UserSelfDeleteForbidden = "user.self_delete_forbidden"

// UserLastAdminForbidden signals that a delete, deactivate, or
// role-demote would leave zero live, active users with a
// platform-administrating system role (super_admin or administrator).
// The check is best-effort under concurrent edits; a follow-up may
// promote it to a Mongo transaction. 403.
const UserLastAdminForbidden = "user.last_admin_forbidden"

// UserRoleEscalationForbidden signals that the requested role change
// would assign a system role with a tier higher than the caller's own
// — i.e. an administrator trying to promote another user (or
// themselves) to super_admin. The cascade rule that protects
// authz.CreateBinding does NOT apply to the User.Role field because
// it's not a binding; this guard is the user module's own version of
// the same invariant. 403.
const UserRoleEscalationForbidden = "user.role_escalation_forbidden"

// UserRoleLookupUnavailable signals that a role a guard on the
// operator-tier user routes had to read was unavailable, so the request
// could not be reasoned about and was refused. Two lookups can raise it,
// both on a patch that names a role:
//
//   - The CALLING user's row, needed for the tier comparison. The guards
//     take the caller's role from the database, not from the `srole` JWT
//     claim (spec §4.6 D28): the claim can be a whole access-token
//     lifetime stale, which is exactly the window the role-change
//     propagation exists to close. Falling back to the claim here would
//     make it authoritative again precisely when the database cannot
//     contradict it.
//   - The TARGET's current role, needed to know whether the role actually
//     changes. Only a change retires the authz permission cache and ends
//     the sessions minted under the old role (§4.6 D27), so writing
//     through an unreadable pre-read would apply the new role while
//     silently skipping both. A "no such user" is not this case — nothing
//     is written, and the request gets its ordinary 404.
//
// Either way the request fails closed. 500.
const UserRoleLookupUnavailable = "user.role_lookup_unavailable"

// --- marketing ---

// MarketingCardCodeCollision signals that the card-emit path
// generated a code that collides with an existing card in the same
// tenant. The fail-safe (tenantId, code) unique index catches the
// collision; the handler maps the underlying duplicate-key error
// onto this code. Callers may retry — a hot card type that races
// on {seq:N} normally widens away from collision after one bump.
// 409.
const MarketingCardCodeCollision = "marketing.card_code_collision"

// MarketingCardInvalidTransition signals that the card lifecycle
// service was asked to move a card to a status it cannot legally
// reach from the current one — for example, reinstating a revoked
// card. The transition matrix is documented in
// docs/plans/marketing-addon/IMPLEMENTATION_PLAN_PHASE_4.md §3.6.
// 422.
const MarketingCardInvalidTransition = "marketing.card_invalid_transition"

// --- logging ---

// LoggingLogProviderUnavailable signals that no trusted Loki query base is
// configured for the optional Tier-1 preview. 503.
const LoggingLogProviderUnavailable = "logging.log_provider_unavailable"

// LoggingLogPreviewInvalid signals a filter outside the closed module,
// window, level, search-length, or limit contract. 400.
const LoggingLogPreviewInvalid = "logging.log_preview_invalid"

// LoggingLogProviderTimeout signals that Loki exceeded the three-second
// preview deadline. 504.
const LoggingLogProviderTimeout = "logging.log_provider_timeout"

// LoggingLogProviderFailed signals a non-timeout upstream, response-size, or
// response-shape failure. 502.
const LoggingLogProviderFailed = "logging.log_provider_failed"

// LoggingMutationInvalid signals malformed or unsupported durable/diagnostic
// log-level input. Details intentionally do not echo request content. 400.
const LoggingMutationInvalid = "logging.mutation_invalid"

// LoggingConfigConflict signals that a durable editor token is stale or a
// bounded Mongo CAS retry could not win. The caller must reload and retry. 409.
const LoggingConfigConflict = "logging.config_conflict"

// LoggingPersistenceFailed signals an internal repository failure. The
// underlying cause is logged server-side and never enters the response. 500.
const LoggingPersistenceFailed = "logging.persistence_failed"

// --- navigation ---

// NavigationOverrideUnknownParent signals that a PATCH against the
// navigation ordering admin referenced a parentKey the registry does
// not recognise — either a stale key for a renamed/removed item or a
// malformed synthetic root. 404 from the write endpoints; 400 when
// the field is missing entirely.
const NavigationOverrideUnknownParent = "navigation.override_unknown_parent"

// NavigationOverrideChildNotFound signals that an entry in the
// orderedChildren payload is not an actual child of the referenced
// parentKey. Includes the empty-string sentinel. 422.
const NavigationOverrideChildNotFound = "navigation.override_child_not_found"

// NavigationOverrideDuplicateChild signals that the same itemKey
// appeared twice in the orderedChildren list. 400.
const NavigationOverrideDuplicateChild = "navigation.override_duplicate_child"

// --- setup (shared bootstrap infrastructure, not a module) ---

// SetupStatusUnavailable: the authoritative setup phase cannot be read;
// the response carries Retry-After and no inferred phase. 503.
const SetupStatusUnavailable = "setup.status_unavailable"

// SetupFinalizerStateUnavailable: coordinator or bound-user state cannot be
// read; never treated as recovery eligibility. 503.
const SetupFinalizerStateUnavailable = "setup.finalizer_state_unavailable"

// SetupFinalizerBoundToAnotherAdmin: the finalize POST caller is not the
// usable bound administrator. 403.
const SetupFinalizerBoundToAnotherAdmin = "setup.finalizer_bound_to_another_admin"

// SetupRecoveryRequiresSuperAdmin: recovering an empty or unusable binding
// requires an active super_admin. 403.
const SetupRecoveryRequiresSuperAdmin = "setup.recovery_requires_super_admin"

// SetupFinalizationAlreadyStarted: a different normalized finalization
// request is already reserved. 409.
const SetupFinalizationAlreadyStarted = "setup.finalization_already_started"

// SetupAlreadyCompleted: setup is complete and the payload does not match a
// replayable finalized request. 409.
const SetupAlreadyCompleted = "setup.already_completed"

// SetupTenantNameRequired: the initial organization name is empty once
// normalized (the schema's minLength constrains the raw string only). 422.
const SetupTenantNameRequired = "setup.tenant_name_required"

// SetupTenantSlugRequired: the initial organization slug is empty once
// normalized. 422.
const SetupTenantSlugRequired = "setup.tenant_slug_required"

// --- notification ---

// NotificationSenderPatternInvalid: a sender profile declares a category
// pattern outside the grammar (exact "foo.bar", prefix "foo.*", or "*"). 422.
const NotificationSenderPatternInvalid = "notification.sender_pattern_invalid"

// NotificationSenderNoDefault: patterns are declared but no profile claims
// "*", so an unmatched category would fail closed. 422.
const NotificationSenderNoDefault = "notification.sender_no_default"

// NotificationSenderDuplicateDefault: more than one profile claims "*". 422.
const NotificationSenderDuplicateDefault = "notification.sender_duplicate_default"

// NotificationSenderPatternConflict: the same pattern is declared by two
// profiles, so the send would be ambiguous. 422.
const NotificationSenderPatternConflict = "notification.sender_pattern_conflict"

// NotificationSenderUnknownDriver: a routing profile's provider names no
// registered driver. 422.
const NotificationSenderUnknownDriver = "notification.sender_unknown_driver"

// NotificationSenderIncomplete: a profile lacks a non-secret field its
// driver requires (save time), or any required field (test send). 422.
const NotificationSenderIncomplete = "notification.sender_incomplete"

// NotificationSenderNotFound: a test send named a profile slug that is not
// in the roster. 404.
const NotificationSenderNotFound = "notification.sender_not_found"

// NotificationSendFailed: the sender's transport or vendor refused a test
// message. The detail carries the bounded diagnostic, never vendor text. 502.
const NotificationSendFailed = "notification.send_failed"
