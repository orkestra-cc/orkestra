package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ingestionHashVersion is bumped when the code's transformation of an item
// changes (title prefix, tokenization), so a new binary cannot silently mix
// old and new points in one generation.
const ingestionHashVersion = 1

// hashInput is the fixed, hashed shape of an ingestion profile. Its field
// order and tags are independent of the iface tags.
type hashInput struct {
	V                int    `json:"v"`
	EmbeddingProfile string `json:"embeddingProfile"`
	Analyzer         struct {
		Tokenizer string `json:"tokenizer"`
		Language  string `json:"language"`
		Stemming  bool   `json:"stemming"`
		StopWords bool   `json:"stopWords"`
	} `json:"analyzer"`
	Chunking struct {
		Strategy  string `json:"strategy"`
		MaxTokens int    `json:"maxTokens"`
		Overlap   int    `json:"overlap"`
	} `json:"chunking"`
	Enrichment *struct {
		ChatEndpoint  string `json:"chatEndpoint"`
		PromptVersion string `json:"promptVersion"`
	} `json:"enrichment"`
}

// IngestionProfileHash identifies everything that shapes the indexed points of
// a corpus: "ing_" + hex sha256 of the normalized profile and the resolved
// embedding profile ID. The embed endpoint's name is excluded: the profile ID
// identifies the model, and renaming an endpoint must not start a generation.
func IngestionProfileHash(p iface.RetrievalIngestionProfile, embeddingProfileID string) string {
	in := hashInput{V: ingestionHashVersion, EmbeddingProfile: embeddingProfileID}
	in.Analyzer.Tokenizer = p.Analyzer.Tokenizer
	if in.Analyzer.Tokenizer == "" {
		in.Analyzer.Tokenizer = "word"
	}
	in.Analyzer.Language = p.Analyzer.Language
	in.Analyzer.Stemming = p.Analyzer.Stemming
	in.Analyzer.StopWords = p.Analyzer.StopWords
	in.Chunking.Strategy = p.Chunking.Strategy
	if in.Chunking.Strategy == "" {
		in.Chunking.Strategy = "none"
	}
	if in.Chunking.Strategy != "none" {
		in.Chunking.MaxTokens = p.Chunking.MaxTokens
		in.Chunking.Overlap = p.Chunking.Overlap
	}
	if p.Enrichment != nil {
		in.Enrichment = &struct {
			ChatEndpoint  string `json:"chatEndpoint"`
			PromptVersion string `json:"promptVersion"`
		}{p.Enrichment.ChatEndpoint, p.Enrichment.PromptVersion}
	}
	b, err := json.Marshal(in)
	if err != nil { // plain strings, ints and bools cannot fail
		panic(err)
	}
	sum := sha256.Sum256(b)
	return "ing_" + hex.EncodeToString(sum[:])
}
