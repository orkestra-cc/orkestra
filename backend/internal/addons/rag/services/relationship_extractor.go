package services

import (
	"math"
	"strings"
)

// Definition represents an extracted term-definition pair from a terms section.
type Definition struct {
	Term       string
	Definition string
}

// ReferenceEdge represents a resolved cross-reference within a document.
type ReferenceEdge struct {
	SourceChunkIdx int    // index in the chunks slice
	TargetNumber   string // the numbering of the target section
	ReferenceText  string // the matched text, e.g. "see 4.1.3"
}

// SimilarityEdge represents a pair of chunks above the similarity threshold.
type SimilarityEdge struct {
	ChunkIdxA  int
	ChunkIdxB  int
	Similarity float64
}

// ExtractDefinitions finds term-definition pairs in a terms section node.
// Supports patterns like:
//   - "term\ndefinition text" (term on its own line)
//   - "3.1 term\ndefinition text" (numbered term)
//   - "— term: definition" (dash-separated)
func ExtractDefinitions(termsNode *StructuralNode) []Definition {
	var defs []Definition

	// Look in children first (each child might be a definition entry)
	for _, child := range termsNode.Children {
		text := strings.TrimSpace(child.Text)
		title := strings.TrimSpace(child.Title)

		if title != "" && text != "" {
			defs = append(defs, Definition{
				Term:       cleanTerm(title),
				Definition: text,
			})
			continue
		}

		// Try to parse from text body
		if d := parseDefinitionFromText(text); d != nil {
			defs = append(defs, *d)
		}
	}

	// Also look in the terms node's own text for dash-separated definitions
	if termsNode.Text != "" {
		lines := strings.Split(termsNode.Text, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if d := parseDefinitionFromText(line); d != nil {
				defs = append(defs, *d)
			}
		}
	}

	return defs
}

func parseDefinitionFromText(text string) *Definition {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// Pattern: "— term: definition" or "- term: definition"
	for _, prefix := range []string{"— ", "– ", "- "} {
		if strings.HasPrefix(text, prefix) {
			rest := strings.TrimPrefix(text, prefix)
			if colonIdx := strings.Index(rest, ":"); colonIdx > 0 && colonIdx < 80 {
				return &Definition{
					Term:       cleanTerm(rest[:colonIdx]),
					Definition: strings.TrimSpace(rest[colonIdx+1:]),
				}
			}
			// No colon — check if next line is the definition
			parts := strings.SplitN(rest, "\n", 2)
			if len(parts) == 2 {
				return &Definition{
					Term:       cleanTerm(parts[0]),
					Definition: strings.TrimSpace(parts[1]),
				}
			}
		}
	}

	return nil
}

func cleanTerm(term string) string {
	term = strings.TrimSpace(term)
	// Remove leading numbering like "3.1 " or "3.1.2 "
	parts := strings.SplitN(term, " ", 2)
	if len(parts) == 2 {
		// Check if first part looks like a number
		first := parts[0]
		isNum := true
		for _, c := range first {
			if c != '.' && (c < '0' || c > '9') {
				isNum = false
				break
			}
		}
		if isNum {
			return strings.TrimSpace(parts[1])
		}
	}
	return term
}

// ResolveInternalReferences matches cross-references found in chunks to their target sections.
func ResolveInternalReferences(chunks []StructuredChunk, sections []*StructuralNode) []ReferenceEdge {
	// Build a lookup from numbering to section
	numberingMap := make(map[string]bool)
	for _, sec := range sections {
		if sec.Numbering != "" {
			numberingMap[sec.Numbering] = true
		}
	}

	var edges []ReferenceEdge
	for i, chunk := range chunks {
		refs := ExtractCrossReferences(chunk.Text)
		for _, ref := range refs {
			// Only create edges for references that resolve to actual sections
			if numberingMap[ref.TargetNumber] {
				edges = append(edges, ReferenceEdge{
					SourceChunkIdx: i,
					TargetNumber:   ref.TargetNumber,
					ReferenceText:  ref.SourceText,
				})
			}
		}
	}

	return edges
}

// ComputeSimilarityEdges finds pairs of chunks whose cosine similarity exceeds the threshold.
// Only compares chunks that are not already adjacent (position diff > 2).
func ComputeSimilarityEdges(embeddings [][]float64, threshold float64) []SimilarityEdge {
	n := len(embeddings)
	if n < 2 {
		return nil
	}

	var edges []SimilarityEdge
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			// Skip adjacent chunks (already connected via NEXT)
			if j-i <= 2 {
				continue
			}
			sim := cosineSimilarity(embeddings[i], embeddings[j])
			if sim >= threshold {
				edges = append(edges, SimilarityEdge{
					ChunkIdxA:  i,
					ChunkIdxB:  j,
					Similarity: sim,
				})
			}
		}
	}

	return edges
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
