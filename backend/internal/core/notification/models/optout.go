package models

import "time"

// MarketingOptoutDoc is the durable fact that an address asked to stop
// receiving marketing. It is deliberately NOT the suppression list: a
// suppression blocks every notification to an address, transactional ones
// included, which is the wrong answer to "stop marketing to me".
//
// There is no TTL. An opt-out does not expire. Two audited, programmatic paths
// touch it, both through iface.MarketingOptoutManager: a consumer may lift it
// when the person opts back in, and record one it collected itself. Neither
// ever touches the suppression list. There is no admin endpoint yet.
type MarketingOptoutDoc struct {
	// Address is normalized (trimmed, lowercased) and unique.
	Address string `bson:"address" json:"address"`
	// Category narrows the opt-out; empty means all marketing.
	Category string    `bson:"category,omitempty" json:"category,omitempty"`
	At       time.Time `bson:"at" json:"at"`
	// SourceTokenUUID records which unsubscribe token produced this, for
	// support questions. It is never the token itself, only its uuid.
	SourceTokenUUID string `bson:"sourceTokenUuid,omitempty" json:"-"`
	// TenantID is attribution only ("" when unknown): the opt-out stays
	// platform-wide whatever it says. Stamped by RecordMarketingOptout.
	TenantID string `bson:"tenantId,omitempty" json:"-"`
	// SourceContext is the producer context of the unsubscribe token that
	// created this opt-out, copied at consume time because the token itself
	// expires through its 30-day TTL.
	SourceContext string `bson:"sourceContext,omitempty" json:"-"`
}
