package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// NotificationDoc is a delivery log entry for a single notification attempt.
type NotificationDoc struct {
	ID                primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID              string             `bson:"uuid" json:"id"`
	Channel           string             `bson:"channel" json:"channel"`
	Type              string             `bson:"type" json:"type"`
	Category          string             `bson:"category" json:"category"`
	TemplateID        string             `bson:"templateId,omitempty" json:"templateId,omitempty"`
	RecipientUserUUID string             `bson:"recipientUserUuid,omitempty" json:"recipientUserUuid,omitempty"`
	RecipientAddress  string             `bson:"recipientAddress" json:"recipientAddress"`
	Subject           string             `bson:"subject,omitempty" json:"subject,omitempty"`
	Status            string             `bson:"status" json:"status"`
	Provider          string             `bson:"provider,omitempty" json:"provider,omitempty"`
	SenderSlug        string             `bson:"senderSlug,omitempty" json:"senderSlug,omitempty"`
	// AttemptedSenderSlug records what an explicit Sender request tried to
	// name (ADR-0021 D4): the validated slug verbatim when it passes the
	// grammar/length guard, or the constant marker "invalid" when it does
	// not — never the raw input, never a hash. Empty when Sender was empty.
	AttemptedSenderSlug string     `bson:"attemptedSenderSlug,omitempty" json:"attemptedSenderSlug,omitempty"`
	Error               string     `bson:"error,omitempty" json:"error,omitempty"`
	IdempotencyKey      string     `bson:"idempotencyKey,omitempty" json:"idempotencyKey,omitempty"`
	CreatedAt           time.Time  `bson:"createdAt" json:"createdAt"`
	SentAt              *time.Time `bson:"sentAt,omitempty" json:"sentAt,omitempty"`
}

// SuppressionDoc is a recipient that must not receive any notifications
// (hard bounce, spam complaint, manual unsubscribe of all mail).
type SuppressionDoc struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	Address   string             `bson:"address" json:"address"`
	Reason    string             `bson:"reason" json:"reason"`
	CreatedAt time.Time          `bson:"createdAt" json:"createdAt"`
}
