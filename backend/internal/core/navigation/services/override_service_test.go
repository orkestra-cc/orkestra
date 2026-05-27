package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/core/navigation/models"
	"github.com/orkestra/backend/internal/core/navigation/repository"
	"github.com/orkestra/backend/internal/shared/errcode"
)

// fakeOverrideRepo is an in-memory OverrideRepository for unit tests.
type fakeOverrideRepo struct {
	docs    []models.NavOverride
	listErr error
}

func (r *fakeOverrideRepo) ListAll(_ context.Context) ([]models.NavOverride, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]models.NavOverride, len(r.docs))
	copy(out, r.docs)
	return out, nil
}

func (r *fakeOverrideRepo) Upsert(_ context.Context, parentKey string, ordered []string, by string) (*models.NavOverride, error) {
	doc := models.NavOverride{ParentKey: parentKey, OrderedChildren: append([]string(nil), ordered...), UpdatedAt: time.Now().UTC(), UpdatedBy: by}
	for i, d := range r.docs {
		if d.ParentKey == parentKey {
			r.docs[i] = doc
			return &doc, nil
		}
	}
	r.docs = append(r.docs, doc)
	return &doc, nil
}

func (r *fakeOverrideRepo) Delete(_ context.Context, parentKey string) error {
	for i, d := range r.docs {
		if d.ParentKey == parentKey {
			r.docs = append(r.docs[:i], r.docs[i+1:]...)
			return nil
		}
	}
	return repository.ErrNotFound
}

func TestOverrideService_LoadMap_DropsUnknownParent(t *testing.T) {
	repo := &fakeOverrideRepo{docs: []models.NavOverride{
		{ParentKey: "billing.invoicing", OrderedChildren: []string{"billing.invoicing.invoices-received"}},
		{ParentKey: "ghost.parent", OrderedChildren: []string{"ghost.child"}},
	}}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	svc := NewOverrideService(repo, logger)

	parents := map[string]struct{}{"billing.invoicing": {}}
	children := map[string]map[string]struct{}{
		"billing.invoicing": {"billing.invoicing.invoices-received": {}, "billing.invoicing.invoices-issued": {}},
	}
	m, err := svc.LoadMap(context.Background(), parents, children)
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}
	if _, ok := m["ghost.parent"]; ok {
		t.Errorf("unknown parent leaked into override map")
	}
	if got := m["billing.invoicing"]; len(got) != 1 || got[0] != "billing.invoicing.invoices-received" {
		t.Errorf("billing.invoicing override = %v", got)
	}
	if !strings.Contains(buf.String(), "unknown parent") {
		t.Errorf("expected warn log; got %q", buf.String())
	}
}

func TestOverrideService_LoadMap_DropsUnknownChild(t *testing.T) {
	repo := &fakeOverrideRepo{docs: []models.NavOverride{
		{ParentKey: "billing.invoicing", OrderedChildren: []string{"billing.invoicing.removed", "billing.invoicing.invoices-issued"}},
	}}
	svc := NewOverrideService(repo, nil)
	parents := map[string]struct{}{"billing.invoicing": {}}
	children := map[string]map[string]struct{}{
		"billing.invoicing": {"billing.invoicing.invoices-issued": {}, "billing.invoicing.invoices-received": {}},
	}
	m, err := svc.LoadMap(context.Background(), parents, children)
	if err != nil {
		t.Fatalf("LoadMap: %v", err)
	}
	got := m["billing.invoicing"]
	if len(got) != 1 || got[0] != "billing.invoicing.invoices-issued" {
		t.Errorf("expected stale child dropped, got %v", got)
	}
}

func TestOverrideService_SetOrder_RejectsBadInput(t *testing.T) {
	repo := &fakeOverrideRepo{}
	svc := NewOverrideService(repo, nil)
	parents := map[string]struct{}{"p": {}}
	children := map[string]map[string]struct{}{"p": {"a": {}, "b": {}}}

	cases := []struct {
		name      string
		parentKey string
		ordered   []string
		wantCode  string
	}{
		{"missing parent", "", []string{"a"}, errcode.NavigationOverrideUnknownParent},
		{"unknown parent", "ghost", []string{"a"}, errcode.NavigationOverrideUnknownParent},
		{"duplicate child", "p", []string{"a", "a"}, errcode.NavigationOverrideDuplicateChild},
		{"empty child", "p", []string{""}, errcode.NavigationOverrideChildNotFound},
		{"unknown child", "p", []string{"x"}, errcode.NavigationOverrideChildNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.SetOrder(context.Background(), tc.parentKey, tc.ordered, parents, children, "actor")
			var ec *errcode.Error
			if !errors.As(err, &ec) {
				t.Fatalf("want *errcode.Error, got %T %v", err, err)
			}
			if ec.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", ec.Code, tc.wantCode)
			}
		})
	}
}

func TestOverrideService_SetOrder_Persists(t *testing.T) {
	repo := &fakeOverrideRepo{}
	svc := NewOverrideService(repo, nil)
	parents := map[string]struct{}{"p": {}}
	children := map[string]map[string]struct{}{"p": {"a": {}, "b": {}}}

	doc, err := svc.SetOrder(context.Background(), "p", []string{"b", "a"}, parents, children, "actor-uuid")
	if err != nil {
		t.Fatalf("SetOrder: %v", err)
	}
	if doc.ParentKey != "p" || doc.UpdatedBy != "actor-uuid" {
		t.Errorf("unexpected doc: %+v", doc)
	}
	if len(repo.docs) != 1 || repo.docs[0].ParentKey != "p" {
		t.Errorf("expected one persisted doc, got %+v", repo.docs)
	}
}

func TestSectionRootKey_Stable(t *testing.T) {
	cases := []struct{ realm, section, want string }{
		{"platform", "Administration", "__root.platform.administration"},
		{"", "", "__root.shared.other"},
		{"business", "Marketing & Sales", "__root.business.marketing-sales"},
	}
	for _, c := range cases {
		if got := sectionRootKey(c.realm, c.section); got != c.want {
			t.Errorf("sectionRootKey(%q,%q) = %q, want %q", c.realm, c.section, got, c.want)
		}
	}
}

func TestApplyOrderToSpecs_RespectsAndFillsMissing(t *testing.T) {
	siblings := []module.NavItemSpec{
		{Name: "A", ItemKey: "a"},
		{Name: "B", ItemKey: "b"},
		{Name: "C", ItemKey: "c"},
		{Name: "D", ItemKey: "d"},
	}
	out := applyOrderToSpecs(siblings, []string{"c", "a"})
	want := []string{"c", "a", "b", "d"}
	for i, w := range want {
		if out[i].ItemKey != w {
			t.Errorf("out[%d] = %q, want %q", i, out[i].ItemKey, w)
		}
	}
}

func TestApplyOrderToAdminItems_StampsEffectiveAndOverridden(t *testing.T) {
	siblings := []models.AdminNavItem{
		{Name: "A", ItemKey: "a", DeclaredOrder: 0},
		{Name: "B", ItemKey: "b", DeclaredOrder: 1},
		{Name: "C", ItemKey: "c", DeclaredOrder: 2},
	}
	out := applyOrderToAdminItems(siblings, []string{"c", "a"})
	// Caller stamps EffectiveOrder + Overridden — applyOrderToAdminItems
	// only reorders. Verify the reorder happened and DeclaredOrder
	// survived untouched.
	if got := out[0].ItemKey; got != "c" || out[0].DeclaredOrder != 2 {
		t.Errorf("out[0] = (%q, declared=%d), want (c, 2)", got, out[0].DeclaredOrder)
	}
	if got := out[1].ItemKey; got != "a" || out[1].DeclaredOrder != 0 {
		t.Errorf("out[1] = (%q, declared=%d), want (a, 0)", got, out[1].DeclaredOrder)
	}
	if got := out[2].ItemKey; got != "b" || out[2].DeclaredOrder != 1 {
		t.Errorf("out[2] = (%q, declared=%d), want (b, 1)", got, out[2].DeclaredOrder)
	}
}
