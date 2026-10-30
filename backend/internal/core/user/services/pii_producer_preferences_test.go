package services

import (
	"context"
	"testing"

	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestPIIProducer_ExportsAndPurgesPreferences(t *testing.T) {
	users := newFakeUserRepo()
	users.seed(&iface.User{UUID: "u-1", Email: "u1@orkestra.local", FullName: "U One", Role: "operator", IsActive: true})
	prefs := newFakePrefRepo()
	if _, err := prefs.Upsert(context.Background(), "u-1", "crm", map[string]string{"columns": "name"}); err != nil {
		t.Fatal(err)
	}
	if _, err := prefs.Upsert(context.Background(), "u-2", "crm", map[string]string{"columns": "email"}); err != nil {
		t.Fatal(err)
	}
	p := NewPIIProducer(users, prefs)

	data, err := p.ExportPersonalData(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	m, _ := data.(map[string]any)
	byModule, _ := m["preferences"].(map[string]map[string]string)
	if byModule["crm"]["columns"] != "name" {
		t.Errorf("export must include the user's preferences, got %v", m["preferences"])
	}

	res, err := p.PurgePersonalData(context.Background(), "u-1", iface.EraseAnonymize)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if res.RowsDeleted != 1 || res.RowsAnonymized != 1 {
		t.Errorf("purge result = %+v, want 1 deleted (preferences) + 1 anonymized (user)", res)
	}
	found := false
	for _, c := range res.Collections {
		if c == repository.UserPreferencesCollection {
			found = true
		}
	}
	if !found {
		t.Errorf("collections = %v, want %s listed", res.Collections, repository.UserPreferencesCollection)
	}
	if _, ok := prefs.docs["u-1/crm"]; ok {
		t.Error("u-1's preferences must be gone")
	}
	if _, ok := prefs.docs["u-2/crm"]; !ok {
		t.Error("u-2's preferences must be untouched")
	}
	// nil preference repo: still works, exports no preferences key.
	data, err = NewPIIProducer(users, nil).ExportPersonalData(context.Background(), "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if data != nil {
		if m, _ := data.(map[string]any); m != nil {
			if _, has := m["preferences"]; has {
				t.Error("without a preference repo the export must not carry a preferences key")
			}
		}
	}
}
