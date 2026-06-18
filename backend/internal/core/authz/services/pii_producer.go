package services

import (
	"context"

	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// piiProducer implements iface.PIIProducer for the authz module. A data
// subject's personal data here is their role bindings — which roles they
// hold, in which tenants (and global/system bindings). Roles/permissions
// catalogs are platform metadata, not the subject's data; only the binding
// rows linking this user to a role are exported / erased.
type piiProducer struct {
	repo *repository.Repository
}

// NewPIIProducer returns a PIIProducer bound to the authz repository.
func NewPIIProducer(repo *repository.Repository) iface.PIIProducer {
	return &piiProducer{repo: repo}
}

// Subject is the stable bundle-key identifier for this producer.
func (p *piiProducer) Subject() string { return "authz" }

// ExportPersonalData returns the subject's role bindings across every tenant
// (plus global bindings). (nil, nil) when the user has none.
func (p *piiProducer) ExportPersonalData(ctx context.Context, userUUID string) (any, error) {
	bindings, err := p.repo.ListBindingsByUser(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return nil, nil
	}
	return map[string]any{"roleBindings": bindings}, nil
}

// PurgePersonalData removes every role binding for the subject across all
// tenants and global bindings. Mode-agnostic: a binding row IS the user→role
// linkage, so there is no anonymizable residue — it is deleted under both
// erase modes.
func (p *piiProducer) PurgePersonalData(ctx context.Context, userUUID string, _ iface.EraseMode) (iface.PurgeResult, error) {
	n, err := p.repo.DeleteBindingsByUser(ctx, userUUID)
	if err != nil {
		return iface.PurgeResult{}, err
	}
	return iface.PurgeResult{RowsDeleted: int(n), Collections: []string{"authz_bindings"}}, nil
}
