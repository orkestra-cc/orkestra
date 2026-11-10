package services

import (
	"context"

	"github.com/orkestra/backend/pkg/sdk/jobs"
)

// RunDSRExportsCleanup is the body of the compliance.dsr_exports_cleanup job.
func RunDSRExportsCleanup(svc *DSRExportService) func(ctx context.Context, run *jobs.Run) error {
	return func(ctx context.Context, run *jobs.Run) error {
		n, err := svc.CleanupExpired(ctx)
		run.Detail("deleted", n)
		return err
	}
}
