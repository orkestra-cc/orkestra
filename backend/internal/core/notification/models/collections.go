package models

// Collection names owned by the notification module.
const (
	NotificationMessagesCollection       = "notification_messages"
	NotificationTemplatesCollection      = "notification_templates"
	NotificationPreferencesCollection    = "notification_preferences"
	NotificationSuppressionsCollection   = "notification_suppressions"
	NotificationUnsubscribeTokensCollect = "notification_unsubscribe_tokens"
)

// Notification categories used for preference lookup and template IDs.
const (
	CategoryAuthVerifyEmail   = "auth.verify_email"
	CategoryAuthResetPassword = "auth.reset_password"
	CategoryAuthWelcome       = "auth.welcome"
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
