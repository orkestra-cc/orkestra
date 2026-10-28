package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// fakePrefRepo is an in-memory PreferenceRepository.
type fakePrefRepo struct {
	docs map[string]*repository.UserPreferenceDoc // key userUUID+"/"+module
	err  error
}

func newFakePrefRepo() *fakePrefRepo {
	return &fakePrefRepo{docs: map[string]*repository.UserPreferenceDoc{}}
}

func (f *fakePrefRepo) Get(_ context.Context, u, m string) (*repository.UserPreferenceDoc, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.docs[u+"/"+m], nil
}
func (f *fakePrefRepo) ListByUser(_ context.Context, u string) ([]*repository.UserPreferenceDoc, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []*repository.UserPreferenceDoc
	for _, d := range f.docs {
		if d.UserUUID == u {
			out = append(out, d)
		}
	}
	return out, nil
}
func (f *fakePrefRepo) Upsert(_ context.Context, u, m string, values map[string]string) (*repository.UserPreferenceDoc, error) {
	if f.err != nil {
		return nil, f.err
	}
	d := &repository.UserPreferenceDoc{UserUUID: u, Module: m, Values: values, UpdatedAt: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	f.docs[u+"/"+m] = d
	return d, nil
}
func (f *fakePrefRepo) DeleteByUser(_ context.Context, u string) (int64, error) {
	var n int64
	for k, d := range f.docs {
		if d.UserUUID == u {
			delete(f.docs, k)
			n++
		}
	}
	return n, nil
}

func prefCatalog(t *testing.T) *module.UserPreferenceRegistry {
	t.Helper()
	reg := module.NewUserPreferenceRegistry()
	max := 100
	if err := reg.RegisterFields("crm", []module.ConfigField{
		{Key: "columns", Type: module.FieldStringList, Default: "name,email"},
		{Key: "pageSize", Type: module.FieldInt, Max: &max, Default: "25"},
		{Key: "digest", Type: module.FieldEnum, Options: []string{"daily", "weekly"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterFields("forms", []module.ConfigField{{Key: "compact", Type: module.FieldBool, Default: "false"}}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestPreferenceService_GetFillsDefaultsAndUnknownModule(t *testing.T) {
	repo := newFakePrefRepo()
	svc := NewPreferenceService(repo, prefCatalog(t), nil)

	p, err := svc.Get(context.Background(), "u-1", "crm")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Values["columns"] != "name,email" || p.Values["pageSize"] != "25" || p.Values["digest"] != "" {
		t.Errorf("defaults not filled: %v", p.Values)
	}
	if p.UpdatedAt != nil {
		t.Error("never saved → UpdatedAt must be nil")
	}
	if len(p.Fields) != 3 {
		t.Errorf("fields = %d, want 3", len(p.Fields))
	}
	if _, err := svc.Get(context.Background(), "u-1", "billing"); !errors.Is(err, ErrPreferenceModuleUnknown) {
		t.Errorf("unknown module: err = %v, want ErrPreferenceModuleUnknown", err)
	}
}

func TestPreferenceService_DisabledModuleIsUnknown(t *testing.T) {
	disabled := func(_ context.Context, m string) bool { return m != "forms" }
	svc := NewPreferenceService(newFakePrefRepo(), prefCatalog(t), disabled)
	if _, err := svc.Get(context.Background(), "u-1", "forms"); !errors.Is(err, ErrPreferenceModuleUnknown) {
		t.Errorf("disabled module: err = %v", err)
	}
	all, err := svc.GetAll(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) != 1 || all[0].Module != "crm" {
		t.Errorf("GetAll must skip disabled modules: %+v", all)
	}
}

func TestPreferenceService_PutValidatesAndStores(t *testing.T) {
	repo := newFakePrefRepo()
	svc := NewPreferenceService(repo, prefCatalog(t), nil)

	p, err := svc.Put(context.Background(), "u-1", "crm", map[string]string{"columns": "name", "digest": "weekly"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if p.Values["columns"] != "name" || p.Values["digest"] != "weekly" || p.Values["pageSize"] != "25" {
		t.Errorf("values after put = %v", p.Values)
	}
	if p.UpdatedAt == nil {
		t.Error("UpdatedAt must be set after a save")
	}
	// PUT semantics: a later put without `columns` falls back to the default.
	p, err = svc.Put(context.Background(), "u-1", "crm", map[string]string{"pageSize": "50"})
	if err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	if p.Values["columns"] != "name,email" || p.Values["pageSize"] != "50" {
		t.Errorf("PUT semantics broken: %v", p.Values)
	}

	_, err = svc.Put(context.Background(), "u-1", "crm", map[string]string{"pageSize": "500", "ghost": "x", "digest": "monthly"})
	if !errors.Is(err, ErrPreferenceInvalid) {
		t.Fatalf("invalid values: err = %v, want ErrPreferenceInvalid", err)
	}
	var verrs module.ValueErrors
	if !errors.As(err, &verrs) || len(verrs) != 3 {
		t.Errorf("expected 3 value errors, got %v", err)
	}
	if stored := repo.docs["u-1/crm"].Values["pageSize"]; stored != "50" {
		t.Errorf("a refused put must not write; stored pageSize = %q", stored)
	}
	if _, err := svc.Put(context.Background(), "u-1", "nope", map[string]string{}); !errors.Is(err, ErrPreferenceModuleUnknown) {
		t.Errorf("unknown module on put: %v", err)
	}
}

func TestPreferenceService_GetAllMergesStoredValues(t *testing.T) {
	repo := newFakePrefRepo()
	svc := NewPreferenceService(repo, prefCatalog(t), nil)
	if _, err := svc.Put(context.Background(), "u-1", "forms", map[string]string{"compact": "true"}); err != nil {
		t.Fatal(err)
	}
	all, err := svc.GetAll(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) != 2 || all[0].Module != "crm" || all[1].Module != "forms" {
		t.Fatalf("GetAll order/content = %+v", all)
	}
	if all[1].Values["compact"] != "true" || all[1].UpdatedAt == nil {
		t.Errorf("forms entry = %+v", all[1])
	}
	if all[0].UpdatedAt != nil {
		t.Errorf("crm never saved → UpdatedAt nil, got %v", all[0].UpdatedAt)
	}
	// Another user sees defaults only.
	other, _ := svc.GetAll(context.Background(), "u-2")
	if other[1].Values["compact"] != "false" {
		t.Errorf("other user must not see u-1's values: %v", other[1].Values)
	}
}
