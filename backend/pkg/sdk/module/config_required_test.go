package module

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func requiredService(t *testing.T) (*ModuleConfigService, *fakeConfigRepo) {
	t.Helper()
	repo := newFakeConfigRepo()
	repo.docs["user"] = &ModuleConfig{ModuleName: "user", ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{minimalModule{name: "auth"}, minimalModule{name: "user"}, plainModule{}})
	return svc, repo
}

func TestRequirePersistedConfig_GetConfigFailsClosedInsteadOfReseeding(t *testing.T) {
	ctx := context.Background()
	svc, repo := requiredService(t)

	// Before the mark: a known module with no document is lazily rebuilt.
	if doc, err := svc.GetConfig(ctx, "auth"); err != nil || doc == nil {
		t.Fatalf("pre-mark lazy seed: doc=%v err=%v", doc, err)
	}
	delete(repo.docs, "auth")

	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth"} // the gate verifies the document exists
	if err := svc.RequirePersistedConfig(ctx, "auth"); err != nil {
		t.Fatal(err)
	}
	delete(repo.docs, "auth")
	if !svc.IsRequiredPersisted("auth") || svc.IsRequiredPersisted("user") {
		t.Error("IsRequiredPersisted did not reflect the mark")
	}
	_, err := svc.GetConfig(ctx, "auth")
	if !errors.Is(err, ErrRequiredConfigMissing) {
		t.Fatalf("GetConfig after the mark: err = %v, want ErrRequiredConfigMissing", err)
	}
	if _, seeded := repo.docs["auth"]; seeded {
		t.Fatal("a required module was lazily re-seeded with schema defaults")
	}
	// Ordinary modules keep their self-healing.
	if doc, err := svc.GetConfig(ctx, "plain"); err != nil || doc == nil {
		t.Errorf("non-required module lost lazy seed: doc=%v err=%v", doc, err)
	}
}

func TestRequirePersistedConfig_SealedAfterFirstCall(t *testing.T) {
	ctx := context.Background()
	svc, repo := requiredService(t)
	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth"}
	if err := svc.RequirePersistedConfig(ctx, "auth"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequirePersistedConfig(ctx, "user"); !errors.Is(err, ErrRequiredSetSealed) {
		t.Fatalf("second call: err = %v, want ErrRequiredSetSealed", err)
	}
	if svc.IsRequiredPersisted("user") {
		t.Error("a sealed set accepted a late addition")
	}
}

// The mark is the boot gate: a required module whose document is missing,
// or whose seeding/backfill failed, must stop the server before traffic —
// an incomplete auth document is exactly what a strict policy reader must
// never be handed.
func TestRequirePersistedConfig_RefusesAFailedOrMissingSeed(t *testing.T) {
	ctx := context.Background()
	svc, repo := requiredService(t)
	if err := svc.RequirePersistedConfig(ctx, "auth"); !errors.Is(err, ErrRequiredConfigMissing) {
		t.Fatalf("missing document: err = %v, want ErrRequiredConfigMissing", err)
	}
	if svc.requiredSealed {
		t.Fatal("a refused mark must not seal the set — boot is aborting")
	}
	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth"}
	svc.seedFailures["auth"] = errors.New("backfill: write refused")
	if err := svc.RequirePersistedConfig(ctx, "auth"); err == nil || !strings.Contains(err.Error(), "backfill") {
		t.Fatalf("failed seed: err = %v, want the recorded seeding failure", err)
	}
	delete(svc.seedFailures, "auth")
	if err := svc.RequirePersistedConfig(ctx, "auth"); err != nil {
		t.Fatalf("healthy document: %v", err)
	}
}

func TestListConfigs_ReportsMissingRequiredRowAndServesTheRest(t *testing.T) {
	ctx := context.Background()
	svc, repo := requiredService(t)
	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth"}
	if err := svc.RequirePersistedConfig(ctx, "auth"); err != nil {
		t.Fatal(err)
	}
	delete(repo.docs, "auth")
	statuses, err := svc.ListConfigs(ctx)
	if err != nil {
		t.Fatalf("ListConfigs must not fail because one required document is missing: %v", err)
	}
	byName := map[string]ModuleConfigStatus{}
	for _, st := range statuses {
		byName[st.Name] = st
	}
	if st := byName["auth"]; !st.Missing || st.Config != nil {
		t.Errorf("auth row = %+v, want Missing with nil Config", st)
	}
	if st := byName["user"]; st.Missing || st.Config == nil {
		t.Errorf("user row = %+v, want a present config", st)
	}
	if st := byName["plain"]; st.Missing || st.Config == nil {
		t.Errorf("plain (non-required, missing) must be lazily seeded: %+v", st)
	}
	if _, seeded := repo.docs["auth"]; seeded {
		t.Fatal("ListConfigs re-seeded the required module")
	}
	// GetAllConfigs keeps its shape: present documents only.
	docs, err := svc.GetAllConfigs(ctx)
	if err != nil || len(docs) != 2 {
		t.Errorf("GetAllConfigs = %d docs err=%v, want 2 (user, plain)", len(docs), err)
	}
}

func TestGetRawValueRequiredModule(t *testing.T) {
	ctx := context.Background()
	svc, repo := requiredService(t)
	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth", ActiveEnvironment: "production",
		Environments: map[string]EnvironmentConfig{"production": {ConfigValues: map[string]string{"present": "x", "cleared": ""}}}}
	if v, ok, err := svc.GetRawValueRequiredModule(ctx, "auth", "present"); err != nil || !ok || v != "x" {
		t.Errorf("present: (%q,%v,%v)", v, ok, err)
	}
	if v, ok, err := svc.GetRawValueRequiredModule(ctx, "auth", "cleared"); err != nil || !ok || v != "" {
		t.Errorf("cleared: (%q,%v,%v)", v, ok, err)
	}
	if _, ok, err := svc.GetRawValueRequiredModule(ctx, "auth", "absent"); err != nil || ok {
		t.Errorf("absent key in a present document is not an error: ok=%v err=%v", ok, err)
	}
	delete(repo.docs, "auth")
	if _, _, err := svc.GetRawValueRequiredModule(ctx, "auth", "present"); !errors.Is(err, ErrRequiredConfigMissing) {
		t.Errorf("missing document: err = %v, want ErrRequiredConfigMissing", err)
	}
	// The permissive sibling is unchanged: nil document is "absent", not an error.
	if _, ok, err := svc.GetRawValue(ctx, "auth", "present"); err != nil || ok {
		t.Errorf("GetRawValue contract changed: ok=%v err=%v", ok, err)
	}
}

func TestListModules_RendersMissingRow(t *testing.T) {
	svc, repo := requiredService(t)
	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth"}
	_ = svc.RequirePersistedConfig(context.Background(), "auth")
	delete(repo.docs, "auth")
	reg := NewModuleRegistry(slog.Default())
	reg.Register(minimalModule{name: "auth"})
	h := NewModuleAdminHandler(svc, reg)
	out, err := h.ListModules(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var auth *ModuleConfigResponse
	for i := range out.Body.Modules {
		if out.Body.Modules[i].ModuleName == "auth" {
			auth = &out.Body.Modules[i]
		}
	}
	if auth == nil || !auth.Missing || auth.Status != "missing" {
		t.Fatalf("auth row = %+v, want Missing=true Status=missing", auth)
	}
	for name, call := range map[string]func() error{
		"GetModule": func() error { _, err := h.GetModule(context.Background(), &GetModuleInput{Name: "auth"}); return err },
		"GetEnvironment": func() error {
			_, err := h.GetEnvironment(context.Background(), &GetEnvironmentInput{Name: "auth", Env: "production"})
			return err
		},
		"ListEnvironments": func() error {
			_, err := h.ListEnvironments(context.Background(), &ListEnvironmentsInput{Name: "auth"})
			return err
		},
	} {
		err := call()
		se, ok := err.(huma.StatusError)
		if !ok || se.GetStatus() != http.StatusServiceUnavailable {
			t.Errorf("%s on a missing required document: err=%v, want a 503 (never a 404 that reads as 'no such environment')", name, err)
		}
	}
}

func TestRequirePersistedConfig_RefusesAFailedMetadataRefresh(t *testing.T) {
	ctx := context.Background()
	svc, repo := requiredService(t)
	repo.docs["auth"] = &ModuleConfig{ModuleName: "auth", ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}}
	repo.refreshErr = errors.New("schema refresh refused")
	if err := svc.SeedFromModules(ctx, []Module{minimalModule{name: "auth"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequirePersistedConfig(ctx, "auth"); err == nil || !strings.Contains(err.Error(), "schema refresh refused") {
		t.Fatalf("a failed metadata refresh must stop a required module from serving: %v", err)
	}
}
