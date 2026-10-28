package services

import (
	"context"
	"fmt"
	"sort"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

var _ iface.TenantIDLister = (*Service)(nil)

// ListActiveTenantIDs implements iface.TenantIDLister: the UUIDs of live
// (not soft-deleted) tenants of the given kind whose status is active, sorted
// ascending. Only "internal" and "external" are accepted.
func (s *Service) ListActiveTenantIDs(ctx context.Context, kind string) ([]string, error) {
	k := models.TenantKind(kind)
	if !k.Valid() {
		return nil, fmt.Errorf("tenant: unknown tenant kind %q", kind)
	}
	rows, err := s.repo.ListTenantsByKind(ctx, k, false)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for i := range rows {
		if rows[i].Status == models.TenantStatusActive {
			ids = append(ids, rows[i].UUID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

var _ iface.PurgedTenantIDLister = (*Service)(nil)

// ListPurgedTenantIDs implements iface.PurgedTenantIDLister: the UUIDs of every
// tenant whose hard purge has committed (status purged), soft-deleted rows
// included, of any kind, sorted ascending.
func (s *Service) ListPurgedTenantIDs(ctx context.Context) ([]string, error) {
	ids, err := s.repo.ListPurgedTenantUUIDs(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	return ids, nil
}
