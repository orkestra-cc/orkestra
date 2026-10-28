// Package chunk splits an item's text into windows before embedding.
//
// Tokenization approximation: a token is a maximal run of runes for which
// unicode.IsSpace is false, when that run has at most 32 runes; a longer run
// counts as consecutive 32-rune pieces (the last may be shorter). Whitespace is
// never a token. One counted token approximates one word; subword tokenizers
// (bge-m3's XLM-R) produce roughly 1-2 model tokens per word in English and
// Italian, so a window of MaxTokens counted tokens may exceed MaxTokens model
// tokens. Operators choose MaxTokens <= MaxInputTokens/2 of the embedding
// profile; the profile's Truncate covers any excess (the tail of an over-long
// window still reaches BM25, not the vector). A chunk's text runs from its
// first token's first rune to its last token's last rune.
package chunk

import (
	"fmt"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	StrategyNone     = "none"
	StrategyFixed    = "fixed"
	MaxPieceRunes    = 32
	MaxChunkBytes    = 64512 // + 512-byte title + "\n\n" stays under the gateway's 64 KiB per input
	MaxChunksPerItem = 512
	MaxTokensLimit   = 4096
)

// Chunk is one window of the input text.
type Chunk struct {
	Ordinal   int
	Text      string // verbatim substring of the input
	CharStart int    // rune offset in the input, inclusive
	CharEnd   int    // rune offset, exclusive
}

// Validate checks a chunking configuration.
func Validate(c iface.RetrievalChunking) error {
	switch c.Strategy {
	case "", StrategyNone:
		if c.MaxTokens != 0 || c.Overlap != 0 {
			return fmt.Errorf("chunking strategy %q takes no maxTokens or overlap", StrategyNone)
		}
	case StrategyFixed:
		if c.MaxTokens < 1 || c.MaxTokens > MaxTokensLimit {
			return fmt.Errorf("chunking maxTokens must be between 1 and %d", MaxTokensLimit)
		}
		if c.Overlap < 0 || c.Overlap >= c.MaxTokens {
			return fmt.Errorf("chunking overlap must be at least 0 and below maxTokens")
		}
	default:
		return fmt.Errorf("unsupported chunking strategy %q", c.Strategy)
	}
	return nil
}

// token holds rune and byte offsets of a counted token in the input.
type token struct{ rs, re, bs, be int }

func tokenize(text string) []token {
	var out []token
	runeIdx, runStart, runStartByte := 0, -1, 0
	flush := func(endRune, endByte int) {
		if runStart < 0 {
			return
		}
		// Split the run into pieces of at most MaxPieceRunes runes.
		rs, bs := runStart, runStartByte
		for rs < endRune {
			re := min(rs+MaxPieceRunes, endRune)
			be := bs
			if re == endRune {
				be = endByte
			} else {
				for i := rs; i < re; i++ {
					_, w := utf8.DecodeRuneInString(text[be:])
					be += w
				}
			}
			out = append(out, token{rs, re, bs, be})
			rs, bs = re, be
		}
		runStart = -1
	}
	for b, r := range text {
		if unicode.IsSpace(r) {
			flush(runeIdx, b)
		} else if runStart < 0 {
			runStart, runStartByte = runeIdx, b
		}
		runeIdx++
	}
	flush(runeIdx, len(text))
	return out
}

// CountTokens returns the approximate token count of text.
func CountTokens(text string) int { return len(tokenize(text)) }

// Split returns the chunks; truncated is true when text was cut by
// MaxChunkBytes (none) or MaxChunksPerItem (fixed).
func Split(text string, c iface.RetrievalChunking) (chunks []Chunk, truncated bool, err error) {
	if err := Validate(c); err != nil {
		return nil, false, err
	}
	toks := tokenize(text)
	n := len(toks)
	if n == 0 {
		return nil, false, nil
	}
	window := func(start, end int) Chunk {
		first, last := toks[start], toks[end-1]
		return Chunk{
			Ordinal:   len(chunks),
			Text:      text[first.bs:last.be],
			CharStart: first.rs,
			CharEnd:   last.re,
		}
	}
	// fit returns the largest end in (start, limit] whose window fits MaxChunkBytes
	// (at least start+1: a single token is at most 32 runes).
	fit := func(start, limit int) int {
		over := sort.Search(limit-start, func(i int) bool {
			return toks[start+i].be-toks[start].bs > MaxChunkBytes
		})
		return max(start+over, start+1)
	}

	if c.Strategy != StrategyFixed {
		end := fit(0, n)
		return []Chunk{window(0, end)}, end < n, nil
	}

	start := 0
	for start < n && len(chunks) < MaxChunksPerItem {
		end := fit(start, min(start+c.MaxTokens, n))
		chunks = append(chunks, window(start, end))
		if end == n {
			return chunks, false, nil
		}
		start = max(end-c.Overlap, start+1)
	}
	return chunks, start < n, nil
}
