package services

import (
	"context"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
)

// CompanyEnrichmentService is the consumer-defined interface for Italian business registry lookups.
// Adapts the company module's service to the sales module's data model.
type CompanyEnrichmentService interface {
	EnrichCompany(ctx context.Context, companyName string, taxCode string) (*models.CompanyEnrichmentData, error)
}
