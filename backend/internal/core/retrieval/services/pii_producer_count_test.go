package services

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// The count ties to the purge on a real search log: rows where the user
// searched and rows that returned the user count once each; another subject's
// rows are neither counted nor deleted.
func TestPIIProducer_CountMatchesPurge(t *testing.T) {
	db := newServicesMongoDB(t)
	ctx := context.Background()
	//tenantscope:allow Test seeds the search log directly in an isolated test database.
	_, err := db.Collection(repository.CollSearchLog).InsertMany(ctx, []any{
		bson.M{"uuid": "r1", "tenantId": "tA", "userUuid": "u1"},
		bson.M{"uuid": "r2", "tenantId": "tB", "userUuid": "u2", "subjects": bson.A{"user:u1", "crm:7"}},
		bson.M{"uuid": "r3", "tenantId": "tA", "userUuid": "u1", "subjects": bson.A{"user:u1"}}, // both: one row
		bson.M{"uuid": "r4", "tenantId": "tB", "userUuid": "u3"},
	})
	require.NoError(t, err)

	clk := newClock()
	store := newMemStore()
	logs := repository.NewSearchLogDSR(db)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fin := NewFinalizer(store, logs, &staticLister{}, nil, testFinalizerConfig, NewAuditRelay(), clk.Now, logger)
	p := NewPIIProducer(store, logs, fin, clk.Now, logger)

	got, err := p.CountPersonalData(ctx, "u1")
	require.NoError(t, err)
	res, err := p.PurgePersonalData(ctx, "u1", iface.EraseHardDelete)
	require.NoError(t, err)
	require.Equal(t, 3, got.Rows)
	require.Equal(t, got.Rows, res.RowsDeleted+res.RowsAnonymized)
	// Collections is a subset of the purge's: the vector index is not counted.
	require.Equal(t, []string{collSearchLogName}, got.Collections)
	require.True(t, slices.Contains(res.Collections, collSearchLogName))

	other, err := p.CountPersonalData(ctx, "u3")
	require.NoError(t, err)
	require.Equal(t, 1, other.Rows)
	after, err := p.CountPersonalData(ctx, "u1")
	require.NoError(t, err)
	require.Equal(t, iface.PersonalDataCount{}, after)
}

func TestPIIProducer_CountRefusals(t *testing.T) {
	env := newProducerEnv(t, nil, false) // rowLogs has no CountByUserAllTenants
	_, err := env.prod.CountPersonalData(context.Background(), "")
	requireIs(t, err, ErrErasureEmptySubject)
	_, err = env.prod.CountPersonalData(context.Background(), "u1")
	require.Error(t, err)
}
