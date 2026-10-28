package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/orkestra/backend/internal/core/retrieval/chunk"
	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	MaxTitleBytes        = 512
	MaxPrincipalsPerItem = 1000
	MaxSubjectsPerItem   = 100
	MaxRefBytes          = 256

	maxKeywordBytes = 1024
	maxKeywordItems = 100
	maxExactFloat   = 1 << 53
)

// errInvalidItem marks an owner item the module refuses to index. Messages
// built on it name a field key at most, never a value.
var errInvalidItem = errors.New("retrieval: invalid item")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidItem, fmt.Sprintf(format, args...))
}

// validateItem checks the principal and subject lists of an item. Every
// subject must be erasable (ValidateSubject): the owner of the item's corpus
// counts as registered even before isOwner sees it, and isOwner may be nil
// (only the corpus owner is registered then).
func validateItem(item iface.RetrievalItem, corpusOwner string, isOwner func(owner string) bool) error {
	if len(item.VisibleTo) > MaxPrincipalsPerItem {
		return invalid("too many visibleTo entries")
	}
	if len(item.Subjects) > MaxSubjectsPerItem {
		return invalid("too many subjects")
	}
	for _, list := range [][]string{item.VisibleTo, item.Subjects} {
		for _, s := range list {
			if s == "" || len(s) > MaxRefBytes {
				return invalid("entry empty or longer than %d bytes", MaxRefBytes)
			}
		}
	}
	registered := func(owner string) bool {
		return owner == corpusOwner || (isOwner != nil && isOwner(owner))
	}
	for _, s := range item.Subjects {
		if _, err := ValidateSubject(s, registered); err != nil {
			return invalid("subject is not an erasable user or registered-owner reference")
		}
	}
	return nil
}

// RegisteredOwner reports whether owner registered a corpus in c; it reads
// the catalog on every call, so corpora registered later count.
func RegisteredOwner(c CorpusCatalog) func(owner string) bool {
	return func(owner string) bool {
		for _, spec := range c.Corpora() {
			if spec.Owner == owner {
				return true
			}
		}
		return false
	}
}

// normalizeFields checks item fields against the corpus schema and converts
// them to the engine representation: keyword string, keyword[] []string,
// integer int64, float float64, bool, datetime Unix milliseconds UTC int64.
// A nil value omits the key.
func normalizeFields(schema []iface.RetrievalFilterField, in map[string]any) (map[string]any, error) {
	types := make(map[string]iface.RetrievalFieldType, len(schema))
	for _, f := range schema {
		types[f.Name] = f.Type
	}
	out := make(map[string]any, len(in))
	for key, v := range in {
		if v == nil {
			continue
		}
		typ, ok := types[key]
		if !ok {
			return nil, invalid("field %q is not declared", key)
		}
		n, err := normalizeValue(typ, v)
		if err != nil {
			return nil, invalid("field %q: %s", key, err)
		}
		out[key] = n
	}
	return out, nil
}

func normalizeValue(typ iface.RetrievalFieldType, v any) (any, error) {
	switch typ {
	case iface.RetrievalFieldKeyword:
		s, ok := v.(string)
		if !ok || len(s) > maxKeywordBytes {
			return nil, errors.New("expected a keyword string of at most 1024 bytes")
		}
		return s, nil
	case iface.RetrievalFieldKeywords:
		var items []string
		switch t := v.(type) {
		case []string:
			items = t
		case []any:
			items = make([]string, 0, len(t))
			for _, e := range t {
				s, ok := e.(string)
				if !ok {
					return nil, errors.New("expected strings")
				}
				items = append(items, s)
			}
		default:
			return nil, errors.New("expected a list of strings")
		}
		if len(items) > maxKeywordItems {
			return nil, errors.New("too many elements")
		}
		out := make([]string, len(items))
		for i, s := range items {
			if len(s) > maxKeywordBytes {
				return nil, errors.New("element too long")
			}
			out[i] = s
		}
		return out, nil
	case iface.RetrievalFieldInteger:
		return toInt64(v)
	case iface.RetrievalFieldFloat:
		f, err := toFloat64(v)
		if err != nil {
			return nil, err
		}
		return f, nil
	case iface.RetrievalFieldBool:
		b, ok := v.(bool)
		if !ok {
			return nil, errors.New("expected a bool")
		}
		return b, nil
	case iface.RetrievalFieldDatetime:
		switch t := v.(type) {
		case time.Time:
			return t.UTC().UnixMilli(), nil
		case string:
			p, err := time.Parse(time.RFC3339, t)
			if err != nil {
				return nil, errors.New("expected an RFC 3339 datetime")
			}
			return p.UTC().UnixMilli(), nil
		}
		return nil, errors.New("expected a datetime")
	}
	return nil, errors.New("unsupported field type")
}

func toInt64(v any) (int64, error) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if rv.Uint() > math.MaxInt64 {
			return 0, errors.New("integer out of range")
		}
		return int64(rv.Uint()), nil
	case reflect.Float64:
		f := rv.Float()
		if math.IsNaN(f) || math.Abs(f) > maxExactFloat || f != math.Trunc(f) {
			return 0, errors.New("expected an integral number within 2^53")
		}
		return int64(f), nil
	}
	return 0, errors.New("expected an integer")
}

func toFloat64(v any) (float64, error) {
	rv := reflect.ValueOf(v)
	var f float64
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f = float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		f = float64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		f = rv.Float()
	default:
		return 0, errors.New("expected a number")
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errors.New("number is not finite")
	}
	return f, nil
}

// indexedText prefixes a chunk with the item title (cut to MaxTitleBytes at a
// rune boundary); a blank title leaves the chunk text alone.
func indexedText(title, chunkText string) string {
	if strings.TrimSpace(title) == "" {
		return chunkText
	}
	if len(title) > MaxTitleBytes {
		cut := MaxTitleBytes
		for cut > 0 && !utf8.RuneStart(title[cut]) {
			cut--
		}
		title = title[:cut]
	}
	return title + "\n\n" + chunkText
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// ChunkID is the stable identifier of one chunk of an owner item.
func ChunkID(sourceID string, ordinal int) string {
	return sourceID + "#" + strconv.Itoa(ordinal)
}

// buildPoints turns chunks into engine points (Dense is filled by the worker
// after embedding). Chunks whose indexed text is blank are skipped.
func buildPoints(tenantID, corpus, profileHash string, item iface.RetrievalItem, chunks []chunk.Chunk, fields map[string]any) []engine.Point {
	points := make([]engine.Point, 0, len(chunks))
	for _, c := range chunks {
		text := indexedText(item.Title, c.Text)
		if strings.TrimSpace(text) == "" {
			continue
		}
		points = append(points, engine.Point{
			ID:   engine.PointID(tenantID, corpus, item.SourceID, c.Ordinal),
			Text: text,
			Runtime: engine.Runtime{
				TenantID:         tenantID,
				SourceID:         item.SourceID,
				SourceRevision:   item.Revision,
				ChunkID:          ChunkID(item.SourceID, c.Ordinal),
				Ordinal:          c.Ordinal,
				VisibleTo:        append([]string(nil), item.VisibleTo...),
				Subjects:         append([]string(nil), item.Subjects...),
				ContentHash:      contentHash(text),
				IngestionProfile: profileHash,
				CharStart:        c.CharStart,
				CharEnd:          c.CharEnd,
			},
			Fields: fields,
		})
	}
	return points
}

// indexedHash fingerprints the indexed text of all points, in order.
func indexedHash(points []engine.Point) string {
	hashes := make([]string, len(points))
	for i, p := range points {
		hashes[i] = p.Runtime.ContentHash
	}
	return contentHash(strings.Join(hashes, "\n"))
}
