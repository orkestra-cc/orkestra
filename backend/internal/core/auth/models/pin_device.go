package models

import "time"

// PINDeviceDoc is one device registered for PIN sign-in by one user.
// It is deliberately NOT an auth_device_trust
// grant: that collection lets a password login skip the MFA prompt, and a
// tablet registered for a field worker's PIN must never do that.
//
// A device proves itself with a server-issued secret, not with its
// deviceId: the id is chosen by the client and travels in clear, so on its
// own it binds nothing. The secret is returned once, at enrollment, and
// only its SHA-256 is stored.
type PINDeviceDoc struct {
	UUID       string     `bson:"uuid" json:"id"`
	Tier       string     `bson:"tier,omitempty" json:"-"`
	UserUUID   string     `bson:"userUuid" json:"-"`
	DeviceID   string     `bson:"deviceId" json:"deviceId"`
	SecretHash string     `bson:"secretHash" json:"-"`
	DeviceName string     `bson:"deviceName,omitempty" json:"deviceName,omitempty"`
	Platform   string     `bson:"platform,omitempty" json:"platform,omitempty"`
	EnrolledBy string     `bson:"enrolledBy" json:"-"`
	Via        string     `bson:"via" json:"via" enum:"admin,self"`
	CreatedAt  time.Time  `bson:"createdAt" json:"createdAt"`
	LastUsedAt *time.Time `bson:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
}

// PINDeviceVia* record who minted the enrollment code a device redeemed.
const (
	PINDeviceViaAdmin = "admin"
	PINDeviceViaSelf  = "self"
)

// PINMethodView is the PIN slice of the auth-methods aggregators (admin
// and self). It never carries the PIN, its hash or a device secret.
type PINMethodView struct {
	// Available is false when the surface has the PIN method switched
	// off or the deployment cannot store PINs; the UI hides the card.
	Available   bool           `json:"available"`
	Set         bool           `json:"set"`
	UpdatedAt   *time.Time     `json:"updatedAt,omitempty"`
	LockedUntil *time.Time     `json:"lockedUntil,omitempty"`
	Length      int            `json:"length"`
	Devices     []PINDeviceDoc `json:"devices"`
	// RequiresTrustedDevice mirrors auth.pinRequiresTrustedDevice so the
	// UI can say whether a device must be enrolled before the PIN works.
	RequiresTrustedDevice bool `json:"requiresTrustedDevice"`
	// CanManage is set on the administrator's view only: whether the
	// caller may change this PIN (user.update), so a read-only caller gets
	// the panel without controls.
	CanManage bool `json:"canManage,omitempty"`
}
