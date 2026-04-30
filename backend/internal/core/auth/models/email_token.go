package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Email token purposes.
const (
	EmailTokenPurposeVerifyEmail   = "verify_email"
	EmailTokenPurposeResetPassword = "reset_password"
)

// EmailTokenDoc is a single-use signed token for email verification or
// password reset. The raw token is never stored — only a sha256 hash.
// The collection has a TTL index on ExpiresAt so used and unused tokens
// are cleaned up automatically.
type EmailTokenDoc struct {
	ID   primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID string             `bson:"uuid" json:"id"`
	// Tier is "operator" / "client" / "" — set by the tier-aware repo
	// constructor (ADR-0003 PR-D). Defense-in-depth so a misrouted
	// query against the wrong collection fails the integrity test.
	Tier      string     `bson:"tier,omitempty" json:"-"`
	UserUUID  string     `bson:"userUuid" json:"userUuid"`
	TokenHash string             `bson:"tokenHash" json:"-"`
	Purpose   string             `bson:"purpose" json:"purpose"`
	IP        string             `bson:"ip,omitempty" json:"ip,omitempty"`
	CreatedAt time.Time          `bson:"createdAt" json:"createdAt"`
	ExpiresAt time.Time          `bson:"expiresAt" json:"expiresAt"`
	UsedAt    *time.Time         `bson:"usedAt,omitempty" json:"usedAt,omitempty"`
}

const (
	EmailTokensCollection = "auth_email_tokens"

	// ADR-0003 PR-B: tier-split email tokens (verification + password
	// reset). Each tier's flow reads from its own collection at
	// cutover; the legacy auth_email_tokens stays authoritative until
	// then.
	OperatorEmailTokensCollection = "operator_email_tokens"
	ClientEmailTokensCollection   = "client_email_tokens"
)
