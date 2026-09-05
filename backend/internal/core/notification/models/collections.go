package models

// Collection names owned by the notification module.
const (
	NotificationMessagesCollection       = "notification_messages"
	NotificationTemplatesCollection      = "notification_templates"
	NotificationPreferencesCollection    = "notification_preferences"
	NotificationSuppressionsCollection   = "notification_suppressions"
	NotificationUnsubscribeTokensCollect = "notification_unsubscribe_tokens"

	// ADR-0003 PR-B: tier-split unsubscribe tokens. The token issuer
	// stamps the audience tier on the token row so /v1/notifications/
	// unsubscribe lookup can hit the correct collection and apply the
	// opt-out to the matching tier's preference row. Empty until the
	// cutover; legacy notification_unsubscribe_tokens stays
	// authoritative at PR-B boundary.
	NotificationOperatorUnsubscribeTokensCollect = "operator_unsubscribe_tokens"
	NotificationClientUnsubscribeTokensCollect   = "client_unsubscribe_tokens"
)

// Notification categories used for preference lookup and template IDs.
const (
	CategoryAuthVerifyEmail   = "auth.verify_email"
	CategoryAuthResetPassword = "auth.reset_password"
	CategoryAuthWelcome       = "auth.welcome"
	// CategoryAuthSuspiciousLogin is sent when the login-risk scorer
	// (Section C of the auth roadmap) flags a session at or above the
	// "high" bucket (>= 0.5). Transactional, cannot be opted out of.
	CategoryAuthSuspiciousLogin = "auth.suspicious_login"
	// CategoryAuthNewDeviceLogin is sent the first time a user logs in
	// from a (deviceId, userUUID) pair the system has not seen before.
	// Helps users notice unauthorised access. Gated by the
	// notifyUserOnNewDeviceLogin admin policy. Transactional.
	CategoryAuthNewDeviceLogin = "auth.new_device_login"
	// CategoryAuthAdminSuspiciousLogin is the admin-side counterpart of
	// CategoryAuthSuspiciousLogin — same risk threshold, but addressed
	// to a configurable admin recipient list with the affected user's
	// metadata up front. Gated by notifyAdminOnSuspiciousLogin +
	// suspiciousLoginRecipients. Transactional.
	CategoryAuthAdminSuspiciousLogin = "auth.admin_suspicious_login"
	// CategoryAuthAdminInvite is the email an operator sends to invite a
	// new Tier-2 client user to the platform. Carries an invite token
	// the recipient redeems on the client SPA's /accept-invite page,
	// where they pick a password; redemption marks the email verified.
	// Transactional.
	CategoryAuthAdminInvite = "auth.admin_invite"
	// CategoryAuthSecurity is the routing/preference category for
	// account-security notices that are not tied to a single login event
	// — today only the second-factor change notice below. Transactional.
	CategoryAuthSecurity = "auth.security"
)

// Template IDs that do NOT share their category's name. Every category
// above doubles as the id of the template it seeds; this one does not,
// because the category is the routing family ("account security") while the
// template is one specific notice inside it. Kept as its own constant so
// the two never get confused at a call site.
const (
	// TemplateAuthMFAFactorAdded announces that a second factor was added
	// to an account — a first TOTP enrolment, a TOTP replacement, or a new
	// passkey (auth spec §4.2 D13). It is what makes an enrolment
	// performed with a stolen bearer token visible to the account holder.
	// Data: AppName, UserName, FactorType ("totp"|"passkey"), Replaced
	// (bool), RequestIP, At, SupportEmail.
	TemplateAuthMFAFactorAdded = "auth.mfa_factor_added"
)

// Notification types — drives whether preferences are honoured.
const (
	TypeTransactional = "transactional"
	TypeMarketing     = "marketing"
)

// Channel identifiers.
const (
	ChannelEmail = "email"
)

// Delivery statuses.
const (
	StatusQueued     = "queued"
	StatusSent       = "sent"
	StatusFailed     = "failed"
	StatusSuppressed = "suppressed"
)
