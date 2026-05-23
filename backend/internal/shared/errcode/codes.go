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
