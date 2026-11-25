package models

import "time"

const (
	CorpusStatusActive         = "active"
	CorpusStatusProfileChanged = "profile_changed"
	CorpusStatusError          = "error"
)

// Reasons recorded on an errored corpus.
const (
	ReasonLocality        = "locality"         // local_only corpus, embed endpoint boundary hosted
	ReasonEndpointUnknown = "endpoint_unknown" // InferenceProfile -> ErrLLMNotConfigured
	ReasonEndpointKind    = "endpoint_kind"    // endpoint kind is not embed
)

// CorpusState is the installation-wide record of one corpus (_id = corpus).
type CorpusState struct {
	Corpus               string    `bson:"_id"`
	Owner                string    `bson:"owner"`
	Status               string    `bson:"status"`
	Reason               string    `bson:"reason,omitempty"`
	ActiveGeneration     int       `bson:"activeGeneration"` // 0 = none yet
	Collection           string    `bson:"collection,omitempty"`
	IngestionProfileHash string    `bson:"ingestionProfileHash,omitempty"`
	EmbeddingProfileID   string    `bson:"embeddingProfileId,omitempty"`
	Dimensions           int       `bson:"dimensions,omitempty"`
	Locality             string    `bson:"locality"`
	DeclaredProfileHash  string    `bson:"declaredProfileHash,omitempty"` // set while profile_changed
	Version              int64     `bson:"version"`
	CreatedAt            time.Time `bson:"createdAt"`
	UpdatedAt            time.Time `bson:"updatedAt"`
	// Delivery step 2 adds the generation being built.
}
