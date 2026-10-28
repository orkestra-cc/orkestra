package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

type fakeErasures struct {
	docs  []models.Erasure
	err   error
	calls int
}

func (f *fakeErasures) Covering(context.Context, string, []string, []string) ([]models.Erasure, error) {
	f.calls++
	return f.docs, f.err
}

func tombCtx(tenantID string) context.Context {
	id := testkit.NewIdentity("u-admin", "admin@example.test", "administrator").WithTenant(tenantID, []string{"org_owner"}, true)
	return ctxauth.WithTenantKind(id.ContextFor(context.Background(), tenantID), "internal")
}

func TestTombstones_Covered(t *testing.T) {
	probes := []Probe{
		{SourceID: "a", Subjects: []string{"user:u1", "crm:1"}},
		{SourceID: "b", Subjects: []string{"user:u2"}},
	}
	cases := []struct {
		name string
		docs []models.Erasure
		a, b bool
	}{
		{"no erasures", nil, false, false},
		{"global user", []models.Erasure{{Scope: models.ErasureScopeGlobal, Kind: models.ErasureKindUser, Subject: "user:u1"}}, true, false},
		{"tenant", []models.Erasure{{Scope: models.ErasureScopeTenant, TenantID: "t1", Kind: models.ErasureKindTenant}}, true, true},
		{"item", []models.Erasure{{Scope: models.ErasureScopeTenant, TenantID: "t1", Kind: models.ErasureKindItem, Corpus: "crm_contacts", SourceID: "a"}}, true, false},
		{"item other corpus", []models.Erasure{{Scope: models.ErasureScopeTenant, TenantID: "t1", Kind: models.ErasureKindItem, Corpus: "other_corpus", SourceID: "a"}}, false, false},
		{"foreign tenant (reader misbehaves)", []models.Erasure{{Scope: models.ErasureScopeTenant, TenantID: "t2", Kind: models.ErasureKindTenant}}, false, false},
		{"subject", []models.Erasure{{Scope: models.ErasureScopeTenant, TenantID: "t1", Kind: models.ErasureKindSubject, Subject: "crm:1"}}, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NewTombstones(&fakeErasures{docs: c.docs}).Covered(tombCtx("t1"), "t1", "crm_contacts", probes)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got["a"] != c.a || got["b"] != c.b {
				t.Fatalf("got %v, want a=%v b=%v", got, c.a, c.b)
			}
		})
	}
}

func TestTombstones_CoveredRefusesForeignTenantAndPropagatesErrors(t *testing.T) {
	probes := []Probe{{SourceID: "a"}}
	f := &fakeErasures{}
	if _, err := NewTombstones(f).Covered(tombCtx("t2"), "t1", "c", probes); err == nil || f.calls != 0 {
		t.Fatalf("foreign tenant: err=%v calls=%d", err, f.calls)
	}
	if _, err := NewTombstones(f).Covered(tombCtx("t1"), "", "c", probes); err == nil || f.calls != 0 {
		t.Fatalf("empty tenant: err=%v calls=%d", err, f.calls)
	}
	if _, err := NewTombstones(f).Covered(context.Background(), "t1", "c", probes); err == nil || f.calls != 0 {
		t.Fatalf("no ctx tenant: err=%v calls=%d", err, f.calls)
	}
	boom := errors.New("boom")
	got, err := NewTombstones(&fakeErasures{err: boom}).Covered(tombCtx("t1"), "t1", "c", probes)
	if !errors.Is(err, boom) || got != nil {
		t.Fatalf("got %v, %v", got, err)
	}
	f = &fakeErasures{}
	got, err = NewTombstones(f).Covered(tombCtx("t1"), "t1", "c", nil)
	if err != nil || got == nil || len(got) != 0 || f.calls != 0 {
		t.Fatalf("no probes: %v %v calls=%d", got, err, f.calls)
	}
}
