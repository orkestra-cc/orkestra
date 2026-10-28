package services

import (
	"errors"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/orkestra/backend/internal/core/retrieval/chunk"
	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func ingestSchema() []iface.RetrievalFilterField {
	return []iface.RetrievalFilterField{
		{Name: "stage", Type: iface.RetrievalFieldKeyword},
		{Name: "tags", Type: iface.RetrievalFieldKeywords},
		{Name: "size", Type: iface.RetrievalFieldInteger},
		{Name: "score", Type: iface.RetrievalFieldFloat},
		{Name: "vip", Type: iface.RetrievalFieldBool},
		{Name: "seen", Type: iface.RetrievalFieldDatetime},
	}
}

func TestNormalizeFields(t *testing.T) {
	got, err := normalizeFields(ingestSchema(), map[string]any{
		"stage": "lead", "tags": []any{"a", "b"}, "size": float64(3), "score": 2,
		"vip": true, "seen": "2026-10-26T12:00:00+02:00", "x": nil,
	})
	// "x" is nil but undeclared: nil omits the key before the declared check.
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"stage": "lead", "tags": []string{"a", "b"}, "size": int64(3),
		"score": float64(2), "vip": true, "seen": int64(1793008800000),
	}, got)

	bad := []map[string]any{
		{"undeclared": 1},
		{"size": 3.5},
		{"score": math.Inf(1)},
		{"seen": "yesterday"},
		{"tags": make([]string, 101)},
		{"stage": 1},
		{"size": float64(1 << 54)},
		{"stage": strings.Repeat("x", 1025)},
	}
	for _, in := range bad {
		_, err := normalizeFields(ingestSchema(), in)
		assert.Equal(t, true, errors.Is(err, errInvalidItem), in)
		for k, v := range in {
			assert.Contains(t, err.Error(), k)
			if s, ok := v.(string); ok && len(s) > 3 {
				assert.Equal(t, false, strings.Contains(err.Error(), s))
			}
		}
	}
}

func TestValidateItem_Bounds(t *testing.T) {
	n := func(k int, s string) []string {
		out := make([]string, k)
		for i := range out {
			out[i] = s
		}
		return out
	}
	assert.NoError(t, validateItem(iface.RetrievalItem{VisibleTo: n(1000, "u"), Subjects: n(100, strings.Repeat("s", 256))}))
	for _, it := range []iface.RetrievalItem{
		{VisibleTo: n(1001, "u")},
		{Subjects: n(101, "s")},
		{VisibleTo: []string{""}},
		{Subjects: []string{strings.Repeat("s", 257)}},
	} {
		assert.Equal(t, true, errors.Is(validateItem(it), errInvalidItem))
	}
}

func TestIndexedText(t *testing.T) {
	assert.Equal(t, "Mario Rossi\n\nsales lead", indexedText("Mario Rossi", "sales lead"))
	assert.Equal(t, "x", indexedText("  ", "x"))
	got := indexedText(strings.Repeat("é", 300), "t") // 600 bytes
	title := strings.TrimSuffix(got, "\n\nt")
	assert.Equal(t, 512, len(title))
	assert.Equal(t, true, utf8.ValidString(got))
	odd := indexedText(strings.Repeat("a", 511)+"é", "t")
	assert.Equal(t, strings.Repeat("a", 511)+"\n\nt", odd)
}

func splitFixture(t *testing.T) []chunk.Chunk {
	t.Helper()
	cs, _, err := chunk.Split("a b c d e f g h i j", iface.RetrievalChunking{Strategy: chunk.StrategyFixed, MaxTokens: 4, Overlap: 1})
	require.NoError(t, err)
	return cs
}

func TestBuildPoints(t *testing.T) {
	item := iface.RetrievalItem{SourceID: "c1", Revision: 4, VisibleTo: []string{"u1"}, Subjects: []string{"s1"}}
	pts := buildPoints("t1", "crm_contacts", "ing_x", item, splitFixture(t), map[string]any{"stage": "lead"})
	require.Len(t, pts, 3)
	spans := [][2]int{{0, 7}, {6, 13}, {12, 19}}
	for i, p := range pts {
		assert.Equal(t, engine.PointID("t1", "crm_contacts", "c1", i), p.ID)
		assert.Equal(t, ChunkID("c1", i), p.Runtime.ChunkID)
		assert.Equal(t, spans[i][0], p.Runtime.CharStart)
		assert.Equal(t, spans[i][1], p.Runtime.CharEnd)
		assert.Equal(t, int64(4), p.Runtime.SourceRevision)
		assert.Equal(t, "ing_x", p.Runtime.IngestionProfile)
		assert.Equal(t, contentHash(p.Text), p.Runtime.ContentHash)
		assert.Equal(t, "t1", p.Runtime.TenantID)
		assert.Nil(t, p.Dense)
	}
	assert.Equal(t, "c1#2", pts[2].Runtime.ChunkID)
	item.VisibleTo[0] = "mutated"
	item.Subjects[0] = "mutated"
	assert.Equal(t, []string{"u1"}, pts[0].Runtime.VisibleTo)
	assert.Equal(t, []string{"s1"}, pts[0].Runtime.Subjects)

	blank := buildPoints("t1", "c", "h", iface.RetrievalItem{SourceID: "c1"}, []chunk.Chunk{{Ordinal: 0, Text: " "}}, nil)
	assert.Len(t, blank, 0)
}

func TestIndexedHash_ChangesWithAnyChunk(t *testing.T) {
	item := iface.RetrievalItem{SourceID: "c1", Revision: 1}
	a := buildPoints("t", "c", "h", item, splitFixture(t), nil)
	b := buildPoints("t", "c", "h", item, splitFixture(t), nil)
	assert.Equal(t, indexedHash(a), indexedHash(b))
	cs := splitFixture(t)
	cs[1].Text += "!"
	assert.NotEqual(t, indexedHash(a), indexedHash(buildPoints("t", "c", "h", item, cs, nil)))
	rev := []engine.Point{a[1], a[0], a[2]}
	assert.NotEqual(t, indexedHash(a), indexedHash(rev))
}
