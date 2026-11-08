package models

import "time"

// Reasons a device token row is disabled.
const (
	DeviceDisabledLogout          = "logout"
	DeviceDisabledReassigned      = "reassigned"
	DeviceDisabledFCMUnregistered = "fcm_unregistered"
	DeviceDisabledFCMInvalid      = "fcm_invalid"
	DeviceDisabledSessionEnded    = "session_ended"
	DeviceDisabledStale           = "stale"
	DeviceDisabledErased          = "erased"
)

// DeviceToken is one push registration: one row per tier+user+device, bound
// to the auth session id. The push token belongs to the device, so
// registering it for another user disables the previous owner's row.
//
// Token is a credential-like value: never serialized (json:"-"), never
// logged beyond its first 8 characters.
type DeviceToken struct {
	UUID           string     `bson:"uuid" json:"uuid"`
	Tier           string     `bson:"tier" json:"tier"`
	UserUUID       string     `bson:"userUuid" json:"userUuid"`
	DeviceID       string     `bson:"deviceId" json:"deviceId"`
	SessionID      string     `bson:"sessionId" json:"sessionId"`
	Token          string     `bson:"token" json:"-"`
	Platform       string     `bson:"platform" json:"platform"`
	AppVersion     string     `bson:"appVersion,omitempty" json:"appVersion,omitempty"`
	Active         bool       `bson:"active" json:"active"`
	CreatedAt      time.Time  `bson:"createdAt" json:"createdAt"`
	UpdatedAt      time.Time  `bson:"updatedAt" json:"updatedAt"`
	LastSeenAt     time.Time  `bson:"lastSeenAt" json:"lastSeenAt"`
	DisabledAt     *time.Time `bson:"disabledAt,omitempty" json:"disabledAt,omitempty"`
	DisabledReason string     `bson:"disabledReason,omitempty" json:"disabledReason,omitempty"`
}
