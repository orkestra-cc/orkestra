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
	// Attachments records only metadata of the files sent — never bytes.
	Attachments []AttachmentMeta `bson:"attachments,omitempty" json:"attachments,omitempty"`
	// FailureReason classifies a failed row (iface.FailureAttachmentRejected);
	// returned again by the idempotent replay.
	FailureReason string `bson:"failureReason,omitempty" json:"failureReason,omitempty"`
	// Push carries the device counts of a push row (channel "push"). Never
	// the token, title body or payload.
	Push *PushStats `bson:"push,omitempty" json:"push,omitempty"`
}

// PushStats are the per-user device counts of one push delivery row.
type PushStats struct {
	Attempted int `bson:"attempted" json:"attempted"`
	Delivered int `bson:"delivered" json:"delivered"`
	Disabled  int `bson:"disabled" json:"disabled"`
}

// AttachmentMeta is what the delivery log keeps of an attachment: its
// sanitized name, media type and raw size. The content is never persisted.
type AttachmentMeta struct {
	Filename    string `bson:"filename" json:"filename"`
	ContentType string `bson:"contentType" json:"contentType"`
	SizeBytes   int    `bson:"sizeBytes" json:"sizeBytes"`
}

// SuppressionDoc is an address that must not receive any email, of any type
// (hard bounce, spam complaint, an operator marking it invalid). dispatchEmail
// checks it before every send, transactional included, and SendTest refuses
// it too. Written only through iface.EmailSuppressionManager.
//
// The row is platform-wide: Address is the key and TenantID is attribution
// only, like MarketingOptoutDoc's. A bounce is a fact of the mailbox, not of
// the tenant that saw it.
type SuppressionDoc struct {
	ID      primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	Address string             `bson:"address" json:"address"`
	// Reason is why the address is suppressed (iface.SuppressionReason*).
	Reason    string    `bson:"reason" json:"reason"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	// Source names the path that wrote the row (e.g. "operator.manual",
	// "operator.import"); ActorUUID the operator, when one did; TenantID the
	// tenant the request acted in. All three are attribution, never scope.
	Source    string `bson:"source,omitempty" json:"source,omitempty"`
	ActorUUID string `bson:"actorUuid,omitempty" json:"actorUuid,omitempty"`
	TenantID  string `bson:"tenantId,omitempty" json:"tenantId,omitempty"`
}
