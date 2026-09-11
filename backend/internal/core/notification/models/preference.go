package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PreferenceDoc stores a single user's opt-in/out state for a category.
// Transactional notifications ignore preferences for delivery but still record
// the preference so the user's preference page can reflect it.
type PreferenceDoc struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UserUUID  string             `bson:"userUuid" json:"userUuid"`
	Category  string             `bson:"category" json:"category"` // "auth.verify_email", "marketing.*" etc.
	Channel   string             `bson:"channel" json:"channel"`   // "email"
	OptedIn   bool               `bson:"optedIn" json:"optedIn"`
	UpdatedAt time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// UnsubscribeTokenDoc holds the hashed token generated for an email recipient
// so an unsubscribe link in a footer can opt them out without authentication.
// Tokens are single-use-per-category and TTL to 30 days via MongoDB index.
type UnsubscribeTokenDoc struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID      string             `bson:"uuid" json:"id"`
	TokenHash string             `bson:"tokenHash" json:"-"`
	UserUUID  string             `bson:"userUuid,omitempty" json:"userUuid,omitempty"`
	Address   string             `bson:"address" json:"address"`
	Category  string             `bson:"category,omitempty" json:"category,omitempty"` // empty = all marketing
	Context   string             `bson:"context,omitempty" json:"-"`                   // opaque producer context (campaign/run ref); handed to MarketingUnsubscribeSink on consume
	CreatedAt time.Time          `bson:"createdAt" json:"createdAt"`
	ExpiresAt time.Time          `bson:"expiresAt" json:"expiresAt"`
	UsedAt    *time.Time         `bson:"usedAt,omitempty" json:"usedAt,omitempty"`
}
