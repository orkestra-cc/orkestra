package services

import (
	"context"

	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// piiProducer implements iface.PIIProducer for the tenant module. A data
// subject's personal data here is their tenant *memberships* — which orgs
// they belong to and with what roles. The tenants/orgs themselves are not
// the subject's personal data, so they are left intact; only the membership
// rows linking this user to them are exported / erased.
type piiProducer struct {
	repo *repository.Repository
}

// NewPIIProducer returns a PIIProducer bound to the tenant repository.
func NewPIIProducer(repo *repository.Repository) iface.PIIProducer {
	return &piiProducer{repo: repo}
}

// Subject is the stable bundle-key identifier for this producer.
func (p *piiProducer) Subject() string { return "tenant" }

// ExportPersonalData returns the subject's tenant memberships. (nil, nil)
// when the user has none keeps the export bundle tidy.
func (p *piiProducer) ExportPersonalData(ctx context.Context, userUUID string) (any, error) {
	memberships, err := p.repo.ListMembershipsByUser(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	if len(memberships) == 0 {
		return nil, nil
	}
	return map[string]any{"memberships": memberships}, nil
}

// PurgePersonalData removes every membership row for the subject. Mode-
// agnostic: a membership row IS the user→org linkage, so there is no
// anonymizable residue — it is deleted under both erase modes.
func (p *piiProducer) PurgePersonalData(ctx context.Context, userUUID string, _ iface.EraseMode) (iface.PurgeResult, error) {
	n, err := p.repo.DeleteMembershipsByUser(ctx, userUUID)
	if err != nil {
		return iface.PurgeResult{}, err
	}
	return iface.PurgeResult{RowsDeleted: int(n), Collections: []string{"tenant_memberships"}}, nil
}
