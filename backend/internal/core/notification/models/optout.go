package models

import "time"

// MarketingOptoutDoc is the durable fact that an address asked to stop
// receiving marketing. It is deliberately NOT the suppression list: a
// suppression blocks every notification to an address, transactional ones
// included, which is the wrong answer to "stop marketing to me".
//
// There is no TTL. An opt-out does not expire, and nothing in this module
// removes one.
type MarketingOptoutDoc struct {
	// Address is normalized (trimmed, lowercased) and unique.
	Address string `bson:"address" json:"address"`
	// Category narrows the opt-out; empty means all marketing.
	Category string    `bson:"category,omitempty" json:"category,omitempty"`
	At       time.Time `bson:"at" json:"at"`
	// SourceTokenUUID records which unsubscribe token produced this, for
	// support questions. It is never the token itself, only its uuid.
	SourceTokenUUID string `bson:"sourceTokenUuid,omitempty" json:"-"`
}
