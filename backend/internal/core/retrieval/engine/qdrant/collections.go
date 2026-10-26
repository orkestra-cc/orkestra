package qdrant

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const maxDimensions = 65536

func collectionPath(name string) string { return "/collections/" + url.PathEscape(name) }

// EnsureCollection creates the collection and its payload and full-text
// indexes when missing; on an existing one it checks the dimension and
// (re)creates the indexes, which Qdrant accepts idempotently.
func (d *Driver) EnsureCollection(ctx context.Context, spec engine.CollectionSpec) error {
	if spec.Name == "" {
		return errors.New("qdrant: collection name is empty")
	}
	if spec.Dimensions < 1 || spec.Dimensions > maxDimensions {
		return fmt.Errorf("qdrant: dimensions %d out of range 1..%d", spec.Dimensions, maxDimensions)
	}
	path := collectionPath(spec.Name)

	var exists struct {
		Exists bool `json:"exists"`
	}
	if err := d.do(ctx, "collection exists", http.MethodGet, path+"/exists", nil, &exists); err != nil {
		return err
	}
	present := exists.Exists
	if !present {
		// One shard keeps BM25 statistics exact (full-text-search docs,
		// "Corpus Statistics").
		create := map[string]any{
			"vectors":      map[string]any{denseVector: map[string]any{"size": spec.Dimensions, "distance": "Cosine"}},
			"shard_number": 1,
		}
		err := d.do(ctx, "create collection", http.MethodPut, path, create, nil)
		var se *statusError
		if errors.As(err, &se) && se.code == http.StatusConflict {
			present = true // created concurrently
		} else if err != nil {
			return err
		}
	}
	if present {
		var info struct {
			Config struct {
				Params struct {
					Vectors map[string]struct {
						Size int `json:"size"`
					} `json:"vectors"`
				} `json:"params"`
			} `json:"config"`
		}
		if err := d.do(ctx, "collection info", http.MethodGet, path, nil, &info); err != nil {
			return err
		}
		if got := info.Config.Params.Vectors[denseVector].Size; got != spec.Dimensions {
			return fmt.Errorf("qdrant: collection has %d dimensions, want %d", got, spec.Dimensions)
		}
	}
	return d.ensureIndexes(ctx, path, spec)
}

func (d *Driver) ensureIndexes(ctx context.Context, path string, spec engine.CollectionSpec) error {
	type idx struct {
		field  string
		schema any
	}
	text := map[string]any{"type": "text", "tokenizer": "word", "lowercase": true, "scoring": true}
	if a := spec.Analyzer; a != (iface.RetrievalAnalyzer{}) {
		if a.Tokenizer != "" {
			text["tokenizer"] = a.Tokenizer
		}
		if a.Stemming {
			text["stemmer"] = map[string]any{"type": "snowball", "language": a.Language}
		}
		if a.StopWords {
			text["stopwords"] = a.Language
		}
	}
	indexes := []idx{
		{"_rt.tenantId", map[string]any{"type": "keyword", "is_tenant": true}},
		{"_rt.sourceId", "keyword"},
		{"_rt.visibleTo", "keyword"},
		{"_rt.subjects", "keyword"},
		{"_rt.sourceRevision", "integer"},
		{textField, text},
	}
	for _, f := range spec.Fields {
		schema, err := payloadSchema(f.Type)
		if err != nil {
			return err
		}
		indexes = append(indexes, idx{"f." + f.Name, schema})
	}
	for _, i := range indexes {
		body := map[string]any{"field_name": i.field, "field_schema": i.schema}
		if err := d.do(ctx, "create index", http.MethodPut, path+"/index?wait=true", body, nil); err != nil {
			return err
		}
	}
	return nil
}

// payloadSchema maps a filter field type to its Qdrant payload index type.
// Datetimes are stored as Unix milliseconds, hence integer.
func payloadSchema(t iface.RetrievalFieldType) (string, error) {
	switch t {
	case iface.RetrievalFieldKeyword, iface.RetrievalFieldKeywords:
		return "keyword", nil
	case iface.RetrievalFieldInteger, iface.RetrievalFieldDatetime:
		return "integer", nil
	case iface.RetrievalFieldFloat:
		return "float", nil
	case iface.RetrievalFieldBool:
		return "bool", nil
	}
	return "", fmt.Errorf("qdrant: unsupported filter field type %q", t)
}

// DropCollection deletes the collection; a missing one is not an error.
func (d *Driver) DropCollection(ctx context.Context, name string) error {
	err := d.do(ctx, "drop collection", http.MethodDelete, collectionPath(name), nil, nil)
	if errors.Is(err, engine.ErrNotFound) {
		return nil
	}
	return err
}
