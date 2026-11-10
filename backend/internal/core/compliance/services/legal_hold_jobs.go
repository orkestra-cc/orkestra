package services

import (
	"context"

	"github.com/orkestra/backend/pkg/sdk/jobs"
)

// RunLegalHoldExpiry is the body of the compliance.legal_hold_expiry job: it
// releases the holds past their expiry and records how many.
func RunLegalHoldExpiry(svc *LegalHoldService) func(ctx context.Context, run *jobs.Run) error {
	return func(ctx context.Context, run *jobs.Run) error {
		n, err := svc.ExpireDue(ctx)
		run.Detail("released", n)
		return err
	}
}
