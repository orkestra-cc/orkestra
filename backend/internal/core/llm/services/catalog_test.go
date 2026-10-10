package services

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sort"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// --- in-memory fakes implementing the narrow repo interfaces catalog.go
// declares. They behave like the real repositories: rows are tenant-scoped
// (Get/List/Update/Delete only see the caller's org), Insert and Update
// stamp TenantID from the context, and names are unique per org. ---

func tenantOf(ctx context.Context) string {
	id, _ := tenantrepo.CurrentTenantID(ctx)
	return id
}

type memCreds struct{ rows map[string]*models.Credential }

func (m *memCreds) Insert(ctx context.Context, c *models.Credential) error {
	tenant, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	for _, r := range m.rows {
		if r.TenantID == tenant && r.Name == c.Name {
			return repository.ErrDuplicateName
		}
	}
	c.TenantID = tenant
	cp := *c
	m.rows[c.UUID] = &cp
	return nil
}
func (m *memCreds) List(ctx context.Context) ([]models.Credential, error) {
	out := []models.Credential{}
	for _, r := range m.rows {
		if r.TenantID == tenantOf(ctx) {
			out = append(out, *r)
		}
	}
	return out, nil
}
func (m *memCreds) Get(ctx context.Context, id string) (*models.Credential, error) {
	if r, ok := m.rows[id]; ok && r.TenantID == tenantOf(ctx) {
		cp := *r
		return &cp, nil
	}
	return nil, repository.ErrNotFound
}
func (m *memCreds) Update(ctx context.Context, c *models.Credential) error {
	r, ok := m.rows[c.UUID]
	if !ok || r.TenantID != tenantOf(ctx) {
		return repository.ErrNotFound
	}
	for _, o := range m.rows {
		if o.UUID != c.UUID && o.TenantID == r.TenantID && o.Name == c.Name {
			return repository.ErrDuplicateName
		}
	}
	c.TenantID = tenantOf(ctx)
	cp := *c
	m.rows[c.UUID] = &cp
	return nil
}
func (m *memCreds) Delete(ctx context.Context, id string) error {
	if r, ok := m.rows[id]; !ok || r.TenantID != tenantOf(ctx) {
		return repository.ErrNotFound
	}
	delete(m.rows, id)
	return nil
}

type memModels struct{ rows map[string]*models.Model }

func (m *memModels) Insert(ctx context.Context, x *models.Model) error {
	tenant, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	for _, r := range m.rows {
		if r.TenantID == tenant && r.Name == x.Name {
			return repository.ErrDuplicateName
		}
	}
	x.TenantID = tenant
	cp := *x
	m.rows[x.UUID] = &cp
	return nil
}
func (m *memModels) List(ctx context.Context) ([]models.Model, error) {
	out := []models.Model{}
	for _, r := range m.rows {
		if r.TenantID == tenantOf(ctx) {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (m *memModels) ListActive(ctx context.Context) ([]models.Model, error) {
	all, _ := m.List(ctx)
	out := []models.Model{}
	for _, r := range all {
		if r.Status == models.ModelStatusActive {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memModels) ListActiveByCredential(ctx context.Context, cred string) ([]models.Model, error) {
	act, _ := m.ListActive(ctx)
	out := []models.Model{}
	for _, r := range act {
		if r.CredentialRef.CredentialUUID == cred {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memModels) Get(ctx context.Context, id string) (*models.Model, error) {
	if r, ok := m.rows[id]; ok && r.TenantID == tenantOf(ctx) {
		cp := *r
		return &cp, nil
	}
	return nil, repository.ErrNotFound
}
func (m *memModels) Update(ctx context.Context, x *models.Model) error {
	r, ok := m.rows[x.UUID]
	if !ok || r.TenantID != tenantOf(ctx) {
		return repository.ErrNotFound
	}
	for _, o := range m.rows {
		if o.UUID != x.UUID && o.TenantID == r.TenantID && o.Name == x.Name {
			return repository.ErrDuplicateName
		}
	}
	x.TenantID = tenantOf(ctx)
	cp := *x
	m.rows[x.UUID] = &cp
	return nil
}
func (m *memModels) Delete(ctx context.Context, id string) error {
	if r, ok := m.rows[id]; !ok || r.TenantID != tenantOf(ctx) {
		return repository.ErrNotFound
	}
	delete(m.rows, id)
	return nil
}

type memGrants struct{ rows map[string][]string } // modelUUID -> users

func (m *memGrants) ListByModel(_ context.Context, model string) ([]models.LLMGrant, error) {
	out := []models.LLMGrant{}
	for _, u := range m.rows[model] {
		out = append(out, models.LLMGrant{ModelUUID: model, UserUUID: u})
	}
	return out, nil
}
func (m *memGrants) ListByUser(_ context.Context, user string) ([]models.LLMGrant, error) {
	out := []models.LLMGrant{}
	for model, users := range m.rows {
		for _, u := range users {
			if u == user {
				out = append(out, models.LLMGrant{ModelUUID: model, UserUUID: u})
			}
		}
	}
	return out, nil
}
func (m *memGrants) Replace(_ context.Context, model, _ string, users []string) error {
	m.rows[model] = append([]string(nil), users...)
	return nil
}
func (m *memGrants) DeleteByModel(_ context.Context, model string) error {
	delete(m.rows, model)
	return nil
}

type memDir struct{ members map[string][]string } // tenant -> users

func (d *memDir) ListTenantMembers(_ context.Context, tenant string) ([]iface.TenantMemberSummary, error) {
	out := []iface.TenantMemberSummary{}
	for _, u := range d.members[tenant] {
		out = append(out, iface.TenantMemberSummary{UserUUID: u})
	}
	return out, nil
}

type memAudit struct{ events []iface.AuditEvent }

func (a *memAudit) Emit(_ context.Context, e iface.AuditEvent) { a.events = append(a.events, e) }

func newCatalog(t *testing.T, cfg CatalogConfig) (*CatalogService, *memCreds, *memModels, *memGrants, *memAudit) {
	t.Helper()
	v, err := NewVault(randomKeyHex(t), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	creds := &memCreds{rows: map[string]*models.Credential{}}
	mods := &memModels{rows: map[string]*models.Model{}}
	grants := &memGrants{rows: map[string][]string{}}
	audit := &memAudit{}
	dir := &memDir{members: map[string][]string{"t1": {"u1", "u2"}, "t2": {"u9"}}}
	svc := NewCatalogService(creds, mods, grants, v, dir, func() CatalogConfig { return cfg }, slog.Default())
	svc.SetAuditSink(audit)
	return svc, creds, mods, grants, audit
}

func openAICredential(t *testing.T, svc *CatalogService, ctx context.Context, name string) *models.Credential {
	t.Helper()
	c, err := svc.CreateCredential(ctx, models.CredentialInput{Name: name, Provider: models.ProviderOpenAI, Secret: "sk-live-abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func modelInput(name string, cred *models.Credential) models.ModelInput {
	return models.ModelInput{
		Name: name, Provider: models.ProviderOpenAI, ModelID: "gpt-5.6-terra",
		Capabilities:  models.LLMModelCapabilities{Chat: true, Streaming: true},
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: cred.UUID},
		Purposes:      []models.LLMModelPurpose{{Purpose: "default", Priority: 10}},
	}
}

func TestCatalog_CredentialLifecycle(t *testing.T) {
	svc, _, _, _, audit := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	c, err := svc.CreateCredential(ctx, models.CredentialInput{Name: "OpenAI", Provider: models.ProviderOpenAI, Secret: "sk-live-abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Secret.IsZero() || c.SecretLast4 != "cdef" || c.BaseURL != "https://api.openai.com/v1" || c.TenantID != "t1" {
		t.Fatalf("credential = %+v", c)
	}
	if _, err := svc.CreateCredential(ctx, models.CredentialInput{Name: "OpenAI", Provider: models.ProviderAnthropic, Secret: "sk-ant-abcdef"}); !errors.Is(err, ErrNameInUse) {
		t.Fatalf("duplicate name err = %v", err)
	}
	// Round trip through the repository copy, as the gateway will read it.
	stored, err := svc.GetCredential(ctx, c.UUID)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.OpenCredentialSecret(ctx, stored)
	if err != nil || secret != "sk-live-abcdef" {
		t.Fatalf("open = %q, %v", secret, err)
	}
	rotated, err := svc.RotateCredential(ctx, c.UUID, "sk-live-123456")
	if err != nil || rotated.SecretLast4 != "3456" {
		t.Fatalf("rotate = %+v, %v", rotated, err)
	}
	if secret, err := svc.OpenCredentialSecret(ctx, rotated); err != nil || secret != "sk-live-123456" {
		t.Fatalf("open after rotate = %q, %v", secret, err)
	}
	if len(audit.events) != 2 || audit.events[0].Action != "llm.credential.created" || audit.events[1].Action != "llm.credential.rotated" {
		t.Fatalf("audit = %+v", audit.events)
	}
	for _, e := range audit.events {
		if e.ResourceType != "llm_credential" || e.ResourceID != c.UUID || e.Outcome != "success" || e.TenantID != "t1" {
			t.Fatalf("audit event = %+v", e)
		}
		for _, v := range e.Metadata {
			if s, ok := v.(string); ok && (s == "sk-live-abcdef" || s == "sk-live-123456") {
				t.Fatal("secret leaked into audit metadata")
			}
		}
	}
}

func TestCatalog_RotateClearsLast4WhenSecretIsShort(t *testing.T) {
	svc, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	c := openAICredential(t, svc, ctx, "OpenAI")
	rotated, err := svc.RotateCredential(ctx, c.UUID, "short")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.SecretLast4 != "" {
		t.Fatalf("a short secret must not leak its tail, got %q", rotated.SecretLast4)
	}
	if _, err := svc.RotateCredential(ctx, c.UUID, ""); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("empty secret err = %v", err)
	}
	if _, err := svc.RotateCredential(ctx, "nope", "sk-live-123456"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown credential err = %v", err)
	}
}

func TestCatalog_NamesAreUniquePerOrgAndRowsAreOrgScoped(t *testing.T) {
	svc, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	c1 := openAICredential(t, svc, ctxFor("t1"), "OpenAI")
	c2 := openAICredential(t, svc, ctxFor("t2"), "OpenAI") // same name, other org: legal
	if c1.UUID == c2.UUID || c2.TenantID != "t2" {
		t.Fatalf("c1=%+v c2=%+v", c1, c2)
	}
	if _, err := svc.GetCredential(ctxFor("t2"), c1.UUID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-org get err = %v", err)
	}
	if list, _ := svc.ListCredentials(ctxFor("t2")); len(list) != 1 || list[0].UUID != c2.UUID {
		t.Fatalf("t2 list = %+v", list)
	}
	// A secret sealed for t1 does not open when presented under t2's id.
	forged := *c1
	forged.TenantID = "t2"
	if _, err := svc.OpenCredentialSecret(ctxFor("t2"), &forged); !errors.Is(err, ErrEnvelopeCorrupt) {
		t.Fatalf("forged tenant err = %v", err)
	}
	// Model names: unique per org, repeatable across orgs.
	if _, err := svc.CreateModel(ctxFor("t1"), modelInput("Fast", c1)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateModel(ctxFor("t1"), modelInput("Fast", c1)); !errors.Is(err, ErrNameInUse) {
		t.Fatalf("duplicate model name err = %v", err)
	}
	if _, err := svc.CreateModel(ctxFor("t2"), modelInput("Fast", c2)); err != nil {
		t.Fatalf("same model name in another org: %v", err)
	}
	// A credential of another org cannot back a model.
	if _, err := svc.CreateModel(ctxFor("t2"), modelInput("Stolen", c1)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign credential err = %v", err)
	}
}

func TestCatalog_MissingTenantScope(t *testing.T) {
	// Outside dev an unset tenant kind is an error, not a panic.
	t.Setenv("ENV", "production")
	svc, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	if _, err := svc.CreateCredential(context.Background(), models.CredentialInput{Name: "A", Provider: models.ProviderOpenAI, Secret: "sk-live-abcdef"}); !errors.Is(err, tenantrepo.ErrTenantScopeMissing) {
		t.Fatalf("create err = %v", err)
	}
	if _, err := svc.ReplaceGrants(context.Background(), "m", models.AccessGranted, nil); !errors.Is(err, tenantrepo.ErrTenantScopeMissing) && !errors.Is(err, ErrNotFound) {
		t.Fatalf("grants err = %v", err)
	}
}

// Under TENANT_KIND_ENFORCEMENT=warn an external tenant reaches the
// handlers; the catalog itself refuses it and writes nothing.
func TestCatalog_RefusesExternalTenants(t *testing.T) {
	svc, creds, mods, grants, audit := newCatalog(t, CatalogConfig{AllowHosted: true})
	internal := ctxFor("t1")
	cred := openAICredential(t, svc, internal, "OpenAI")
	m, err := svc.CreateModel(internal, modelInput("Fast", cred))
	if err != nil {
		t.Fatal(err)
	}
	nCreds, nModels, nEvents := len(creds.rows), len(mods.rows), len(audit.events)

	ext := ctxauthExternal(t)
	name := "X"
	calls := map[string]func() error{
		"CreateCredential": func() error {
			_, err := svc.CreateCredential(ext, models.CredentialInput{Name: "E", Provider: models.ProviderOpenAI, Secret: "sk-live-abcdef"})
			return err
		},
		"ListCredentials": func() error { _, err := svc.ListCredentials(ext); return err },
		"GetCredential":   func() error { _, err := svc.GetCredential(ext, cred.UUID); return err },
		"PatchCredential": func() error { _, err := svc.PatchCredential(ext, cred.UUID, &name, nil, nil); return err },
		"RotateCredential": func() error {
			_, err := svc.RotateCredential(ext, cred.UUID, "sk-live-zzzzzz")
			return err
		},
		"DeleteCredential":     func() error { return svc.DeleteCredential(ext, cred.UUID) },
		"OpenCredentialSecret": func() error { _, err := svc.OpenCredentialSecret(ext, cred); return err },
		"CreateModel":          func() error { _, err := svc.CreateModel(ext, modelInput("E", cred)); return err },
		"ListModels":           func() error { _, err := svc.ListModels(ext); return err },
		"GetModel":             func() error { _, err := svc.GetModel(ext, m.UUID); return err },
		"PatchModel": func() error {
			_, err := svc.PatchModel(ext, m.UUID, models.LLMModelPatchBody{Name: &name})
			return err
		},
		"DeleteModel": func() error { return svc.DeleteModel(ext, m.UUID) },
		"ReplaceGrants": func() error {
			_, err := svc.ReplaceGrants(ext, m.UUID, models.AccessEveryone, []string{"u-ext"})
			return err
		},
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, tenantrepo.ErrTenantKindMismatch) {
			t.Errorf("%s under an external tenant err = %v", name, err)
		}
	}
	if len(creds.rows) != nCreds || len(mods.rows) != nModels || len(grants.rows) != 0 || len(audit.events) != nEvents {
		t.Fatalf("an external-tenant call wrote something: creds %d models %d grants %d events %d", len(creds.rows), len(mods.rows), len(grants.rows), len(audit.events))
	}
	if got := mods.rows[m.UUID]; got.Access != models.AccessGranted || got.Name != "Fast" {
		t.Fatalf("model changed under an external tenant: %+v", got)
	}
}

func TestCatalog_HostedGateAndMockGate(t *testing.T) {
	svc, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: false, ProductionLike: true})
	ctx := ctxFor("t1")
	if _, err := svc.CreateCredential(ctx, models.CredentialInput{Name: "A", Provider: models.ProviderAnthropic, Secret: "sk-ant-abcdef"}); !errors.Is(err, ErrHostedDisabled) {
		t.Fatalf("hosted err = %v", err)
	}
	if _, err := svc.CreateCredential(ctx, models.CredentialInput{Name: "M", Provider: models.ProviderMock}); !errors.Is(err, ErrMockNotAllowed) {
		t.Fatalf("mock err = %v", err)
	}
	if _, err := svc.CreateCredential(ctx, models.CredentialInput{Name: "O", Provider: models.ProviderOllama, BaseURL: "http://ollama:11434"}); !errors.Is(err, ErrEndpointNotAllowed) {
		t.Fatalf("endpoint err = %v", err)
	}
	// Outside production a mock credential is fine.
	dev, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: false})
	if _, err := dev.CreateCredential(ctx, models.CredentialInput{Name: "M", Provider: models.ProviderMock}); err != nil {
		t.Fatalf("mock outside production: %v", err)
	}
}

func TestCatalog_RejectsBaseURLForProvidersThatTakeNone(t *testing.T) {
	svc, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	if _, err := svc.CreateCredential(ctx, models.CredentialInput{Name: "M", Provider: models.ProviderMock, BaseURL: "http://169.254.169.254"}); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("mock baseUrl err = %v", err)
	}
	if _, err := svc.CreateCredential(ctx, models.CredentialInput{Name: "O", Provider: models.ProviderOpenAI, Secret: "sk-live-abcdef", BaseURL: "https://evil.example.com"}); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("openai custom baseUrl err = %v", err)
	}
}

func TestCatalog_PatchCredential(t *testing.T) {
	svc, creds, _, _, audit := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	c := openAICredential(t, svc, ctx, "OpenAI")
	disabled, enabled := models.CredentialStatusDisabled, models.CredentialStatusActive
	newName := "Renamed"

	patched, err := svc.PatchCredential(ctx, c.UUID, &newName, nil, nil)
	if err != nil || patched.Name != "Renamed" || patched.Status != models.CredentialStatusActive {
		t.Fatalf("rename = %+v, %v", patched, err)
	}
	if got := creds.rows[c.UUID]; got.SecretLast4 != "cdef" || got.Secret.IsZero() {
		t.Fatalf("patch must keep the secret: %+v", got)
	}

	bad := "paused"
	if _, err := svc.PatchCredential(ctx, c.UUID, nil, nil, &bad); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("bad status err = %v", err)
	}
	if _, err := svc.PatchCredential(ctx, "nope", nil, nil, &disabled); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown err = %v", err)
	}

	// M26: once the operator turns allow_hosted off, an existing hosted
	// credential must still be disable-able...
	svc.cfg = func() CatalogConfig { return CatalogConfig{AllowHosted: false, ProductionLike: true} }
	off, err := svc.PatchCredential(ctx, c.UUID, nil, nil, &disabled)
	if err != nil || off.Status != models.CredentialStatusDisabled {
		t.Fatalf("disable with allow_hosted=false = %+v, %v", off, err)
	}
	// ...but not re-enabled, and not edited while it stays enabled.
	if _, err := svc.PatchCredential(ctx, c.UUID, nil, nil, &enabled); !errors.Is(err, ErrHostedDisabled) {
		t.Fatalf("re-enable with allow_hosted=false err = %v", err)
	}
	if got := creds.rows[c.UUID]; got.Status != models.CredentialStatusDisabled {
		t.Fatalf("status after refused re-enable = %s", got.Status)
	}
	last := audit.events[len(audit.events)-1]
	if last.Action != "llm.credential.updated" || last.Metadata["status"] != models.CredentialStatusDisabled {
		t.Fatalf("last audit = %+v", last)
	}
}

func TestCatalog_PatchBaseURLRevalidatesEndpoint(t *testing.T) {
	svc, _, _, _, audit := newCatalog(t, CatalogConfig{AllowHosted: true, ProductionLike: true})
	ctx := ctxFor("t1")
	c, err := svc.CreateCredential(ctx, models.CredentialInput{Name: "Self", Provider: models.ProviderOpenAICompatible, BaseURL: "https://llm.example.com/v1/", Secret: "sk-live-abcdef"})
	if err != nil || c.BaseURL != "https://llm.example.com/v1" {
		t.Fatalf("create = %+v, %v", c, err)
	}
	internal := "https://10.0.0.5"
	if _, err := svc.PatchCredential(ctx, c.UUID, nil, &internal, nil); !errors.Is(err, ErrEndpointNotAllowed) {
		t.Fatalf("patch to private endpoint err = %v", err)
	}
	ok := "https://other.example.com/"
	patched, err := svc.PatchCredential(ctx, c.UUID, nil, &ok, nil)
	if err != nil || patched.BaseURL != "https://other.example.com" {
		t.Fatalf("patch = %+v, %v", patched, err)
	}
	for _, e := range audit.events {
		for _, v := range e.Metadata {
			if s, ok := v.(string); ok && (s == "https://other.example.com" || s == "https://llm.example.com/v1") {
				t.Fatalf("baseUrl leaked into audit: %+v", e)
			}
		}
	}
}

func TestCatalog_ModelRulesAndGrants(t *testing.T) {
	svc, _, _, grants, audit := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	cred := openAICredential(t, svc, ctx, "OpenAI")
	in := modelInput("Fast", cred)
	m, err := svc.CreateModel(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// credential/provider mismatch
	bad := in
	bad.Name = "Wrong"
	bad.Provider = models.ProviderAnthropic
	if _, err := svc.CreateModel(ctx, bad); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("provider mismatch err = %v", err)
	}
	// unknown credential
	bad = in
	bad.Name = "Orphan"
	bad.CredentialRef.CredentialUUID = "nope"
	if _, err := svc.CreateModel(ctx, bad); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown credential err = %v", err)
	}
	// delete credential in use
	if err := svc.DeleteCredential(ctx, cred.UUID); !errors.Is(err, ErrCredentialInUse) {
		t.Fatalf("in use err = %v", err)
	}
	// grants: non-member rejected atomically
	if _, err := svc.ReplaceGrants(ctx, m.UUID, models.AccessGranted, []string{"u1", "ghost"}); !errors.Is(err, ErrGrantNotMember) {
		t.Fatalf("non-member err = %v", err)
	}
	if view, _ := svc.GetModel(ctx, m.UUID); len(view.Grants) != 0 || view.Access != models.AccessGranted {
		t.Fatalf("partial grants written: %+v", view.Grants)
	}
	// a member of another org is not a member of this one
	if _, err := svc.ReplaceGrants(ctx, m.UUID, models.AccessGranted, []string{"u9"}); !errors.Is(err, ErrGrantNotMember) {
		t.Fatalf("foreign member err = %v", err)
	}
	granted, err := svc.ReplaceGrants(ctx, m.UUID, models.AccessGranted, []string{"u1", "u2", "u1"})
	if err != nil || len(granted.Grants) != 2 || granted.Access != models.AccessGranted {
		t.Fatalf("grants = %+v, %v", granted, err)
	}
	if _, err := svc.ReplaceGrants(ctx, "nope", models.AccessGranted, []string{"u1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("grants on unknown model err = %v", err)
	}
	// delete model removes grants
	if err := svc.DeleteModel(ctx, m.UUID); err != nil {
		t.Fatal(err)
	}
	if len(grants.rows[m.UUID]) != 0 {
		t.Fatalf("grants survived model delete: %+v", grants.rows)
	}
	if err := svc.DeleteCredential(ctx, cred.UUID); err != nil {
		t.Fatalf("delete after model gone: %v", err)
	}
	var actions []string
	for _, e := range audit.events {
		actions = append(actions, e.Action)
	}
	want := []string{"llm.credential.created", "llm.model.created", "llm.grants.replaced", "llm.model.deleted", "llm.credential.deleted"}
	if len(actions) != len(want) {
		t.Fatalf("audit actions = %v, want %v", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("audit actions = %v, want %v", actions, want)
		}
	}
	if g := audit.events[2]; g.ResourceType != "llm_model" || g.Metadata["granted"] != 2 {
		t.Fatalf("grants audit = %+v", g)
	}
}

func TestCatalog_UserAccountModelRules(t *testing.T) {
	svc, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	ua := models.ModelInput{
		Name: "Mine", Provider: models.ProviderOpenAI, ModelID: "gpt-5.6-terra",
		Capabilities:  models.LLMModelCapabilities{Chat: true},
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindUserAccount},
		Purposes:      []models.LLMModelPurpose{{Purpose: "default"}},
	}
	if _, err := svc.CreateModel(ctx, ua); err != nil {
		t.Fatalf("valid user_account model: %v", err)
	}
	notOpenAI := ua
	notOpenAI.Name, notOpenAI.Provider = "Claude", models.ProviderAnthropic
	if _, err := svc.CreateModel(ctx, notOpenAI); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("user_account non-openai err = %v", err)
	}
	emb := ua
	emb.Name = "Emb"
	emb.Capabilities = models.LLMModelCapabilities{Chat: true, Embeddings: true, Dimensions: 8}
	if _, err := svc.CreateModel(ctx, emb); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("user_account embeddings err = %v", err)
	}
}

// fullPatch sets every field of in, the way the admin form saves a model.
func fullPatch(in models.ModelInput, status *string) models.LLMModelPatchBody {
	return models.LLMModelPatchBody{
		Name: &in.Name, Provider: &in.Provider, ModelID: &in.ModelID, Capabilities: &in.Capabilities,
		CredentialRef: &in.CredentialRef, Defaults: &in.Defaults, BudgetReserveOutputTokens: in.BudgetReserveOutputTokens,
		Purposes: in.Purposes, Status: status,
	}
}

func TestCatalog_PatchModelPartial(t *testing.T) {
	svc, _, mods, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	cred := openAICredential(t, svc, ctx, "OpenAI")
	m, err := svc.CreateModel(ctx, modelInput("Fast", cred))
	if err != nil {
		t.Fatal(err)
	}
	before := *mods.rows[m.UUID]

	renamed := "Faster"
	got, err := svc.PatchModel(ctx, m.UUID, models.LLMModelPatchBody{Name: &renamed})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Faster" {
		t.Fatalf("name = %s", got.Name)
	}
	after := *mods.rows[m.UUID]
	after.Name, after.UpdatedAt = before.Name, before.UpdatedAt
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("a one-field patch changed other fields:\n before %+v\n after  %+v", before, after)
	}

	// The merged model is validated as a whole: embeddings without
	// dimensions are rejected, and so is a provider that no longer matches
	// the stored credential.
	emb := models.LLMModelCapabilities{Embeddings: true}
	if _, err := svc.PatchModel(ctx, m.UUID, models.LLMModelPatchBody{Capabilities: &emb}); !errors.Is(err, iface.ErrLLMInvalidRequest) || !errors.Is(err, models.ErrInvalidCapabilities) {
		t.Fatalf("invalid merged capabilities err = %v", err)
	}
	anthropic := models.ProviderAnthropic
	if _, err := svc.PatchModel(ctx, m.UUID, models.LLMModelPatchBody{Provider: &anthropic}); !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("provider no longer matching the credential err = %v", err)
	}
	if mods.rows[m.UUID].Name != "Faster" || mods.rows[m.UUID].Provider != models.ProviderOpenAI {
		t.Fatal("a rejected patch was persisted")
	}

	// Disabling through a status-only patch works under the hosted gate.
	svc.cfg = func() CatalogConfig { return CatalogConfig{} }
	disabled := models.ModelStatusDisabled
	if got, err := svc.PatchModel(ctx, m.UUID, models.LLMModelPatchBody{Status: &disabled}); err != nil || got.Status != models.ModelStatusDisabled {
		t.Fatalf("disable = %+v, %v", got, err)
	}
}

func TestCatalog_PatchModelFull(t *testing.T) {
	svc, _, mods, _, audit := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	cred := openAICredential(t, svc, ctx, "OpenAI")
	m, err := svc.CreateModel(ctx, modelInput("Fast", cred))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateModel(ctx, modelInput("Slow", cred)); err != nil {
		t.Fatal(err)
	}
	in := modelInput("Faster", cred)
	disabled := models.ModelStatusDisabled
	updated, err := svc.PatchModel(ctx, m.UUID, fullPatch(in, &disabled))
	if err != nil || updated.Name != "Faster" || updated.Status != models.ModelStatusDisabled {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if mods.rows[m.UUID].Status != models.ModelStatusDisabled {
		t.Fatal("update not persisted")
	}
	if last := audit.events[len(audit.events)-1]; last.Action != "llm.model.updated" {
		t.Fatalf("audit = %+v", last)
	}
	// Renaming onto another model's name collides.
	if _, err := svc.PatchModel(ctx, m.UUID, fullPatch(modelInput("Slow", cred), nil)); !errors.Is(err, ErrNameInUse) {
		t.Fatalf("rename collision err = %v", err)
	}
	bad := "paused"
	if _, err := svc.PatchModel(ctx, m.UUID, fullPatch(in, &bad)); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("bad status err = %v", err)
	}
	if _, err := svc.PatchModel(ctx, "nope", fullPatch(in, nil)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown err = %v", err)
	}
	// The disabled model no longer pins the credential, but "Slow" (still
	// active) does.
	if err := svc.DeleteCredential(ctx, cred.UUID); !errors.Is(err, ErrCredentialInUse) {
		t.Fatalf("the other active model still pins the credential: %v", err)
	}
}

func TestCatalog_ModelsFollowTheProviderGate(t *testing.T) {
	svc, _, mods, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	cred := openAICredential(t, svc, ctx, "OpenAI")
	live, err := svc.CreateModel(ctx, modelInput("Live", cred))
	if err != nil {
		t.Fatal(err)
	}
	off, err := svc.CreateModel(ctx, modelInput("Off", cred))
	if err != nil {
		t.Fatal(err)
	}
	disabled, active := models.ModelStatusDisabled, models.ModelStatusActive
	if _, err := svc.PatchModel(ctx, off.UUID, fullPatch(modelInput("Off", cred), &disabled)); err != nil {
		t.Fatal(err)
	}

	// The operator turns allow_hosted off.
	svc.cfg = func() CatalogConfig { return CatalogConfig{AllowHosted: false, ProductionLike: true} }

	// A still-active hosted credential cannot back a new model.
	before := len(mods.rows)
	if _, err := svc.CreateModel(ctx, modelInput("New", cred)); !errors.Is(err, ErrHostedDisabled) {
		t.Fatalf("create on hosted credential err = %v", err)
	}
	if len(mods.rows) != before {
		t.Fatal("a refused create must not write a model")
	}
	// A disabled hosted model cannot be re-enabled; the stored status holds.
	if _, err := svc.PatchModel(ctx, off.UUID, fullPatch(modelInput("Off", cred), &active)); !errors.Is(err, ErrHostedDisabled) {
		t.Fatalf("re-enable err = %v", err)
	}
	if mods.rows[off.UUID].Status != models.ModelStatusDisabled {
		t.Fatalf("status after refused re-enable = %s", mods.rows[off.UUID].Status)
	}
	// Editing a live hosted model is gated too...
	if _, err := svc.PatchModel(ctx, live.UUID, fullPatch(modelInput("Live2", cred), nil)); !errors.Is(err, ErrHostedDisabled) {
		t.Fatalf("edit live err = %v", err)
	}
	// ...but disabling always works.
	got, err := svc.PatchModel(ctx, live.UUID, fullPatch(modelInput("Live", cred), &disabled))
	if err != nil || got.Status != models.ModelStatusDisabled {
		t.Fatalf("disable under gate = %+v, %v", got, err)
	}
	// A subscription-backed (user_account) model is hosted as well.
	ua := models.ModelInput{
		Name: "Mine", Provider: models.ProviderOpenAI, ModelID: "gpt-5.6-terra",
		Capabilities:  models.LLMModelCapabilities{Chat: true},
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindUserAccount},
		Purposes:      []models.LLMModelPurpose{{Purpose: "default"}},
	}
	if _, err := svc.CreateModel(ctx, ua); !errors.Is(err, ErrHostedDisabled) {
		t.Fatalf("user_account create err = %v", err)
	}
	// Local providers stay available.
	oll, err := svc.CreateCredential(ctx, models.CredentialInput{Name: "Local", Provider: models.ProviderOllama, BaseURL: "https://ollama.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	local := models.ModelInput{
		Name: "Llama", Provider: models.ProviderOllama, ModelID: "llama3",
		Capabilities:  models.LLMModelCapabilities{Chat: true},
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: oll.UUID},
		Purposes:      []models.LLMModelPurpose{{Purpose: "default"}},
	}
	if _, err := svc.CreateModel(ctx, local); err != nil {
		t.Fatalf("ollama model under allow_hosted=false: %v", err)
	}
}

func TestCatalog_MockModelRefusedInProduction(t *testing.T) {
	svc, _, _, _, _ := newCatalog(t, CatalogConfig{})
	ctx := ctxFor("t1")
	mock, err := svc.CreateCredential(ctx, models.CredentialInput{Name: "Mock", Provider: models.ProviderMock})
	if err != nil {
		t.Fatal(err)
	}
	in := models.ModelInput{
		Name: "M", Provider: models.ProviderMock, ModelID: "mock-1",
		Capabilities:  models.LLMModelCapabilities{Chat: true},
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: mock.UUID},
		Purposes:      []models.LLMModelPurpose{{Purpose: "default"}},
	}
	if _, err := svc.CreateModel(ctx, in); err != nil {
		t.Fatalf("mock model outside production: %v", err)
	}
	svc.cfg = func() CatalogConfig { return CatalogConfig{ProductionLike: true} }
	in.Name = "M2"
	if _, err := svc.CreateModel(ctx, in); !errors.Is(err, ErrMockNotAllowed) {
		t.Fatalf("mock model in production err = %v", err)
	}
}

// I2: who may use a model is decided only on the grants route (its own
// permission and step-up). A new model is closed (access granted, no
// grants); ReplaceGrants sets access and the grant list together.
func TestCatalog_AccessIsSetOnlyThroughReplaceGrants(t *testing.T) {
	svc, _, mods, grants, audit := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	cred := openAICredential(t, svc, ctx, "OpenAI")
	m, err := svc.CreateModel(ctx, modelInput("Fast", cred))
	if err != nil {
		t.Fatal(err)
	}
	if m.Access != models.AccessGranted {
		t.Fatalf("a new model must default to access granted, got %q", m.Access)
	}

	view, err := svc.ReplaceGrants(ctx, m.UUID, models.AccessEveryone, []string{"u1"})
	if err != nil || view.Access != models.AccessEveryone || len(view.Grants) != 1 {
		t.Fatalf("replace = %+v, %v", view, err)
	}
	if mods.rows[m.UUID].Access != models.AccessEveryone {
		t.Fatal("access not persisted")
	}
	last := audit.events[len(audit.events)-1]
	if last.Action != "llm.grants.replaced" || last.Metadata["access"] != models.AccessEveryone || last.Metadata["granted"] != 1 {
		t.Fatalf("grants audit = %+v", last)
	}

	// With access everyone the stored grants stay dormant: going back to
	// granted restores them as they are.
	view, err = svc.ReplaceGrants(ctx, m.UUID, models.AccessGranted, []string{"u1"})
	if err != nil || view.Access != models.AccessGranted || len(view.Grants) != 1 {
		t.Fatalf("back to granted = %+v, %v", view, err)
	}

	// An unknown access value and a non-member are refused before anything
	// is written, access included.
	if _, err := svc.ReplaceGrants(ctx, m.UUID, "friends", nil); !errors.Is(err, iface.ErrLLMInvalidRequest) || !errors.Is(err, models.ErrInvalidAccess) {
		t.Fatalf("bad access err = %v", err)
	}
	if _, err := svc.ReplaceGrants(ctx, m.UUID, models.AccessEveryone, []string{"ghost"}); !errors.Is(err, ErrGrantNotMember) {
		t.Fatalf("non-member err = %v", err)
	}
	if got := mods.rows[m.UUID]; got.Access != models.AccessGranted || len(grants.rows[m.UUID]) != 1 {
		t.Fatalf("a refused replace changed the model: access %s grants %v", got.Access, grants.rows[m.UUID])
	}

	// A model patch cannot change access: the patch body has no such field
	// (asserted in models), and a full edit keeps what the grants route set.
	if _, err := svc.ReplaceGrants(ctx, m.UUID, models.AccessEveryone, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PatchModel(ctx, m.UUID, fullPatch(modelInput("Fast", cred), nil)); err != nil {
		t.Fatal(err)
	}
	if mods.rows[m.UUID].Access != models.AccessEveryone {
		t.Fatalf("a model edit changed access to %s", mods.rows[m.UUID].Access)
	}
}

// M5: a model cannot be created on, or brought back to life on, a credential
// that is disabled. Disabling a model on it always works.
func TestCatalog_LiveModelsNeedAnActiveCredential(t *testing.T) {
	svc, _, mods, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	ctx := ctxFor("t1")
	cred := openAICredential(t, svc, ctx, "OpenAI")
	m, err := svc.CreateModel(ctx, modelInput("Fast", cred))
	if err != nil {
		t.Fatal(err)
	}
	disabled, active := models.ModelStatusDisabled, models.ModelStatusActive
	if _, err := svc.PatchModel(ctx, m.UUID, models.LLMModelPatchBody{Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	credOff := models.CredentialStatusDisabled
	if _, err := svc.PatchCredential(ctx, cred.UUID, nil, nil, &credOff); err != nil {
		t.Fatal(err)
	}

	before := len(mods.rows)
	if _, err := svc.CreateModel(ctx, modelInput("New", cred)); !errors.Is(err, iface.ErrLLMInvalidRequest) || !errors.Is(err, ErrCredentialDisabled) {
		t.Fatalf("create on a disabled credential err = %v", err)
	}
	if len(mods.rows) != before {
		t.Fatal("a refused create wrote a model")
	}
	if _, err := svc.PatchModel(ctx, m.UUID, models.LLMModelPatchBody{Status: &active}); !errors.Is(err, ErrCredentialDisabled) {
		t.Fatalf("re-enable on a disabled credential err = %v", err)
	}
	if mods.rows[m.UUID].Status != models.ModelStatusDisabled {
		t.Fatal("a refused re-enable was persisted")
	}
	// Editing the disabled model, or keeping it disabled, is fine.
	renamed := "Fast2"
	if _, err := svc.PatchModel(ctx, m.UUID, models.LLMModelPatchBody{Name: &renamed}); err != nil {
		t.Fatalf("edit a disabled model on a disabled credential: %v", err)
	}
	// Once the credential is active again the model can be re-enabled.
	credOn := models.CredentialStatusActive
	if _, err := svc.PatchCredential(ctx, cred.UUID, nil, nil, &credOn); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.PatchModel(ctx, m.UUID, models.LLMModelPatchBody{Status: &active}); err != nil || got.Status != models.ModelStatusActive {
		t.Fatalf("re-enable on an active credential = %+v, %v", got, err)
	}
}
