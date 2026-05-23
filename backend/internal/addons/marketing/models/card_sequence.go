package models

import "time"

// CardSequence backs the {seq:N} placeholder in CardType.CodeFormat.
// One row per (tenantId, cardTypeUuid); NextSeq advances atomically
// via findAndModify($inc, upsert=true) in
// CardSequenceRepository.NextSequence.
//
// This collection is an internal implementation detail of code
// generation, not a domain entity. Operators never see it directly,
// and rebuilding it from MAX(card.code) is straightforward if it is
// ever lost — see card_sequence_repo.go's loud comment.
type CardSequence struct {
	UUID         string    `bson:"uuid" json:"uuid"`
	TenantID     string    `bson:"tenantId" json:"-"`
	CardTypeUUID string    `bson:"cardTypeUuid" json:"cardTypeUuid"`
	NextSeq      int64     `bson:"nextSeq" json:"nextSeq"`
	UpdatedAt    time.Time `bson:"updatedAt" json:"updatedAt"`
}
