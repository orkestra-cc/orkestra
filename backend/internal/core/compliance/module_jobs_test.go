package compliance

import (
	"context"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/jobs"
	"github.com/orkestra/backend/pkg/sdk/module"
)

var _ module.HasJobs = (*Module)(nil)

func TestJobsDeclareLegalHoldExpiry(t *testing.T) {
	defs := (&Module{}).Jobs()
	if len(defs) == 0 || defs[0].Name != "legal_hold_expiry" {
		t.Fatalf("Jobs() = %+v", defs)
	}
	cat := jobs.NewCatalog()
	if err := cat.Register("compliance", defs); err != nil {
		t.Fatalf("catalog rejects the definitions: %v", err)
	}
	if _, ok := cat.Get("compliance.legal_hold_expiry"); !ok {
		t.Fatal("key compliance.legal_hold_expiry not registered")
	}
	if defs[0].Timeout != 5*time.Minute {
		t.Fatalf("timeout = %v", defs[0].Timeout)
	}
}

func TestJobsBeforeInitIsHarmless(t *testing.T) {
	run := jobs.NewRun("compliance.legal_hold_expiry", "r", jobs.TriggerSchedule, 1, time.Now())
	if err := (&Module{}).Jobs()[0].Run(context.Background(), run); err != nil {
		t.Fatalf("Run without service: %v", err)
	}
}
