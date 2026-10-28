package services

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/filter"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// joinedAt is the datetime payload of every integration point: 2026-06-01Z.
var joinedAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// searchIntegrationEnv is a searchFixture whose engine, tombstones and search
// log are real (Qdrant and MongoDB); the module, authz, embedder, profiles and
// source stay the fixture's recording fakes. The principal resolver is real.
type searchIntegrationEnv struct {
	*searchFixture
	db      *mongo.Database
	eng     engine.Engine
	erasure *repository.Erasures
	coll    string
}

// newSearchIntegrationEnv skips unless MONGO_TEST_URI, QDRANT_TEST_URL and
// QDRANT_TEST_API_KEY are all set. A skip proves nothing about these paths.
func newSearchIntegrationEnv(t *testing.T) *searchIntegrationEnv {
	t.Helper()
	db, drv, prefix := integrationStores(t, "sit", "itest")
	coll := engine.CollectionName(prefix, "contacts", 1)
	t.Cleanup(func() { dropCollection(t, drv, coll) })
	ensureIndexes(t, db, map[string][]module.IndexSpec{
		repository.CollErasures:  repository.ErasureIndexes(),
		repository.CollSearchLog: repository.SearchLogIndexes(),
	})

	fx := newSearchFixture(t)
	fx.spec.Filters = []iface.RetrievalFilterField{
		{Name: "team", Type: iface.RetrievalFieldKeyword},
		{Name: "joined", Type: iface.RetrievalFieldDatetime},
	}
	fx.state.gens = []PinnedGeneration{{Collection: coll, Generation: 1, EmbedProfileID: "ip_a"}}
	fx.profiles.profiles["embed"] = &iface.InferenceProfile{
		ID: "ip_a", Endpoint: "embed", Kind: iface.InferenceKindEmbed, Boundary: iface.InferenceBoundaryLocal, Dimensions: 4}
	fx.embedder.vec = []float32{1, 0, 0, 0}

	e := &searchIntegrationEnv{searchFixture: fx, db: db, eng: drv, erasure: repository.NewErasures(db), coll: coll}
	require.NoError(t, drv.EnsureCollection(context.Background(), engine.CollectionSpec{
		Name: coll, Dimensions: 4, Fields: fx.spec.Filters}))

	texts := []struct {
		source, text, subject string
		dense                 []float32
	}{
		{"s1", "Maria Rossi responsabile commerciale Milano", "user:alice", []float32{1, 0, 0, 0}},
		{"s2", "Luca Bianchi responsabile commerciale Torino", "crm:c-2", []float32{0.9, 0.1, 0, 0}},
		{"s3", "Anna Verdi responsabile commerciale Roma", "user:bob", []float32{0.8, 0.2, 0, 0}},
	}
	var points []engine.Point
	for _, x := range texts {
		points = append(points, engine.Point{
			ID:    engine.PointID("t-1", "contacts", x.source, 0),
			Dense: x.dense,
			Text:  x.text,
			Runtime: engine.Runtime{
				TenantID: "t-1", SourceID: x.source, SourceRevision: 1, ChunkID: ChunkID(x.source, 0),
				VisibleTo: []string{"tenant:*"}, Subjects: []string{x.subject},
				CharEnd: len([]rune(x.text)),
			},
			Fields: map[string]any{"team": "sales", "joined": joinedAt.UnixMilli()},
		})
	}
	require.NoError(t, drv.Upsert(context.Background(), coll, points))

	fx.tweak = func(d *SearchDeps) {
		d.Engine = drv
		d.Tombstones = NewTombstones(e.erasure)
		d.Principals = &PrincipalResolver{Members: searchMembers{member: true}, Roles: searchRoles{ids: []string{"r-1"}}}
		d.Log = repository.NewSearchLog(db)
	}
	return e
}

func (e *searchIntegrationEnv) req(query string) iface.RetrievalSearchRequest {
	return iface.RetrievalSearchRequest{Corpus: "contacts", Query: query}
}

func sourceIDsOf(hits []iface.RetrievalHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.SourceID)
	}
	return out
}

// lastLogRow reads the newest search_log row of tenant t-1 straight from Mongo.
func (e *searchIntegrationEnv) lastLogRow(t *testing.T) models.SearchLog {
	t.Helper()
	var row models.SearchLog
	//tenantscope:allow system: test assertion reads a row directly
	err := e.db.Collection(repository.CollSearchLog).FindOne(context.Background(),
		bson.M{"tenantId": "t-1"}, options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})).Decode(&row)
	require.NoError(t, err)
	return row
}

func TestIntegrationSearch_PrivateRecordDropped(t *testing.T) { // Review Focus 1
	e := newSearchIntegrationEnv(t)
	res, err := e.search(e.req("responsabile commerciale"))
	require.NoError(t, err)
	got := sourceIDsOf(res.Hits)
	slices.Sort(got)
	assert.Equal(t, []string{"s1", "s2", "s3"}, got)

	e.source.visible = map[string]iface.RetrievalVisibility{"s2": {Visible: false, Revision: 1}}
	res, err = e.search(e.req("responsabile commerciale"))
	require.NoError(t, err)
	got = sourceIDsOf(res.Hits)
	slices.Sort(got)
	assert.Equal(t, []string{"s1", "s3"}, got)

	e.source.visible = nil
	e.source.err = errors.New("down")
	res, err = e.search(e.req("responsabile commerciale"))
	requireIs(t, err, iface.ErrRetrievalVisibilityCheck)
	assert.Nil(t, res)

	e.source.err = nil
	e.source.block = true
	e.setVisibleTimeout(50 * time.Millisecond)
	res, err = e.search(e.req("responsabile commerciale"))
	requireIs(t, err, iface.ErrRetrievalVisibilityCheck)
	assert.Nil(t, res)
}

// setVisibleTimeout shortens the Visible timeout of later searches.
func (e *searchIntegrationEnv) setVisibleTimeout(d time.Duration) {
	prev := e.tweak
	e.tweak = func(deps *SearchDeps) {
		prev(deps)
		deps.VisibleTimeout = d
	}
}

func TestIntegrationSearch_ErasedUserNeverReturned(t *testing.T) { // Review Focus 2
	e := newSearchIntegrationEnv(t)
	require.NoError(t, e.erasure.Insert(context.Background(), &models.Erasure{
		Scope: models.ErasureScopeGlobal, Kind: models.ErasureKindUser, Subject: "user:bob"}))

	res, err := e.search(e.req("responsabile commerciale"))
	require.NoError(t, err)
	got := sourceIDsOf(res.Hits)
	slices.Sort(got)
	assert.Equal(t, []string{"s1", "s2"}, got)

	n, err := e.eng.Count(context.Background(), e.coll, engine.Selector{TenantID: "t-1", SourceIDs: []string{"s3"}})
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	row := e.lastLogRow(t)
	assert.Equal(t, false, slices.Contains(row.Subjects, "user:bob"))
	assert.Equal(t, false, slices.Contains(row.SourceIDs, "s3"))
	assert.Equal(t, true, slices.Contains(row.SourceIDs, "s1"))
}

func TestIntegrationSearch_ProfileMismatchKeywordOnly(t *testing.T) { // Review Focus 3
	e := newSearchIntegrationEnv(t)
	e.profiles.profiles["embed"] = &iface.InferenceProfile{
		ID: "ip_b", Endpoint: "embed", Kind: iface.InferenceKindEmbed, Boundary: iface.InferenceBoundaryLocal, Dimensions: 4}
	res, err := e.search(e.req("Rossi"))
	require.NoError(t, err)
	assert.Equal(t, true, res.Degraded)
	assert.Equal(t, 0, len(e.embedder.reqs))
	if len(res.Hits) == 0 {
		t.Fatalf("no hits")
	}
	assert.Equal(t, "s1", res.Hits[0].SourceID)
}

// The first real Mongo write of a filter: the stored row decodes and carries
// what the plan requires. This pins the storage shape as it is today (a
// datetime value is stored as a BSON datetime).
func TestIntegrationSearch_LogRowWithDatetimeFilter(t *testing.T) {
	e := newSearchIntegrationEnv(t)
	req := e.req("responsabile commerciale")
	req.Filter = &iface.RetrievalFilter{And: []iface.RetrievalFilter{
		{Field: "team", Op: iface.RetrievalOpEq, Value: "sales"},
		{Field: "joined", Op: iface.RetrievalOpGte, Value: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	}}
	res, err := e.search(req)
	require.NoError(t, err)
	assert.Equal(t, 3, len(res.Hits))

	row := e.lastLogRow(t)
	assert.Equal(t, "t-1", row.TenantID)
	assert.Equal(t, "u-1", row.UserUUID)
	assert.Equal(t, "contacts", row.Corpus)
	assert.Equal(t, "responsabile commerciale", row.Query)
	assert.Equal(t, models.SearchOutcomeOK, row.Outcome)
	assert.Equal(t, 3, row.HitCount)
	assert.Equal(t, false, row.Degraded)
	assert.NotEmpty(t, row.UUID)
	assert.Equal(t, true, slices.Contains(row.Principals, "user:u-1"))
	assert.Equal(t, true, slices.Contains(row.Principals, "tenant:*"))
	assert.Equal(t, true, slices.Contains(row.Principals, "role:r-1"))
	got := slices.Clone(row.SourceIDs)
	slices.Sort(got)
	assert.Equal(t, []string{"s1", "s2", "s3"}, got)
	assert.Equal(t, []string{"crm:c-2", "user:alice", "user:bob"}, row.Subjects)

	// The filter is stored as canonical JSON (F3): the raw BSON value is a
	// string, it decodes back into the SDK type, and it still validates
	// against the corpus schema, datetime included.
	var raw bson.M
	//tenantscope:allow system: test assertion reads a row directly
	require.NoError(t, e.db.Collection(repository.CollSearchLog).FindOne(context.Background(),
		bson.M{"tenantId": "t-1", "uuid": row.UUID}).Decode(&raw))
	if _, ok := raw["filter"].(string); !ok {
		t.Fatalf("stored filter is %T, want a JSON string", raw["filter"])
	}
	assert.Equal(t,
		`{"and":[{"field":"team","op":"eq","value":"sales"},{"field":"joined","op":"gte","value":"2026-01-01T00:00:00Z"}]}`,
		row.FilterJSON)
	var stored iface.RetrievalFilter
	require.NoError(t, json.Unmarshal([]byte(row.FilterJSON), &stored))
	require.NoError(t, filter.Validate(&stored, e.spec.Filters))
	if len(stored.And) != 2 {
		t.Fatalf("stored filter = %+v, want an and of 2 nodes", stored)
	}
	assert.Equal(t, "team", stored.And[0].Field)
	assert.Equal(t, "sales", stored.And[0].Value)
	assert.Equal(t, "joined", stored.And[1].Field)
	assert.Equal(t, iface.RetrievalOpGte, stored.And[1].Op)
	assert.Equal(t, "2026-01-01T00:00:00Z", stored.And[1].Value)
}

// The datetime bound must filter: points joined 2026-06-01, so a bound after
// that excludes all of them and a bound before keeps all of them.
func TestIntegrationSearch_DatetimeFilterNarrows(t *testing.T) {
	e := newSearchIntegrationEnv(t)
	for bound, want := range map[time.Time]int{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC): 3,
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC): 0,
	} {
		req := e.req("responsabile commerciale")
		req.Filter = &iface.RetrievalFilter{Field: "joined", Op: iface.RetrievalOpGte, Value: bound}
		res, err := e.search(req)
		require.NoError(t, err)
		assert.Equal(t, want, len(res.Hits), bound.String())
	}
}
