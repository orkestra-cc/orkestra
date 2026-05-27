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
