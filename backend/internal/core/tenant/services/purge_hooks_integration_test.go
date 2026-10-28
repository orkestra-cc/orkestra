package services

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
)

func TestPurgeTenant_RunsPurgeHooksWithTenantUUID(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	ctx := context.Background()
	tn := seedDefaultsTenant(t, repo, nil)

	var got []string
	svc.RegisterTenantPurgeHook(func(_ context.Context, id string) error {
		got = append(got, id)
		return nil
	})
	if err := svc.PurgeTenant(ctx, tn.UUID); err != nil {
		t.Fatalf("PurgeTenant: %v", err)
	}
	if want := []string{tn.UUID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("hook calls = %v, want %v", got, want)
	}
}

func TestDeleteTenant_DoesNotRunPurgeHooks(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	ctx := context.Background()
	tn := seedDefaultsTenant(t, repo, nil)

	var got []string
	svc.RegisterTenantPurgeHook(func(_ context.Context, id string) error {
		got = append(got, id)
		return nil
	})
	if err := svc.DeleteTenant(ctx, tn.UUID); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("soft delete ran purge hooks: %v", got)
	}
}

func TestPurgeTenant_PurgeHookErrorAuditedAndLaterHooksRun(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	sink := &recordingAuditSink{}
	svc.SetAuditSink(sink)
	ctx := context.Background()
	tn := seedDefaultsTenant(t, repo, nil)

	calls := 0
	svc.RegisterTenantPurgeHook(func(context.Context, string) error { return errors.New("boom") })
	svc.RegisterTenantPurgeHook(func(context.Context, string) error { calls++; return nil })
	if err := svc.PurgeTenant(ctx, tn.UUID); err != nil {
		t.Fatalf("PurgeTenant: %v", err)
	}
	if calls != 1 {
		t.Fatalf("second hook calls = %d, want 1", calls)
	}
	ev, ok := sink.lastByAction("tenant.cascade.hook_failed")
	if !ok {
		t.Fatal("no tenant.cascade.hook_failed audit event")
	}
	if ev.Outcome != "failure" || ev.Metadata["hook"] != "purge" || ev.ResourceID != tn.UUID {
		t.Fatalf("audit event = %+v", ev)
	}
}

func TestPurgeTenant_DefaultTargetRunsNoPurgeHook(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	ctx := context.Background()
	tn := seedDefaultsTenant(t, repo, nil)

	var got []string
	svc.RegisterTenantPurgeHook(func(_ context.Context, id string) error {
		got = append(got, id)
		return nil
	})
	if err := svc.AssignDefaultTenant(ctx, tn.UUID, "", models.DefaultUpdateSourceSetup); err != nil {
		t.Fatalf("AssignDefaultTenant: %v", err)
	}
	if err := svc.PurgeTenant(ctx, tn.UUID); !errors.Is(err, ErrDefaultReassignmentRequired) {
		t.Fatalf("PurgeTenant err = %v, want ErrDefaultReassignmentRequired", err)
	}
	if len(got) != 0 {
		t.Fatalf("hook ran for the default target: %v", got)
	}
}
