package services

import (
	"context"
	"errors"
	"log/slog"
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
		Purposes:      []models.LLMModelPurpose{{Purpose: "default", Priority: 10}}, Access: models.AccessGranted,
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
	svc, _, _, _, _ := newCatalog(t, CatalogConfig{AllowHosted: true})
	if _, err := svc.CreateCredential(context.Background(), models.CredentialInput{Name: "A", Provider: models.ProviderOpenAI, Secret: "sk-live-abcdef"}); !errors.Is(err, tenantrepo.ErrTenantScopeMissing) {
		t.Fatalf("create err = %v", err)
	}
	if _, err := svc.ReplaceGrants(context.Background(), "m", nil); !errors.Is(err, tenantrepo.ErrTenantScopeMissing) && !errors.Is(err, ErrNotFound) {
		t.Fatalf("grants err = %v", err)
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
	if _, err := svc.ReplaceGrants(ctx, m.UUID, []string{"u1", "ghost"}); !errors.Is(err, ErrGrantNotMember) {
		t.Fatalf("non-member err = %v", err)
	}
	if view, _ := svc.GetModel(ctx, m.UUID); len(view.Grants) != 0 {
		t.Fatalf("partial grants written: %+v", view.Grants)
	}
	// a member of another org is not a member of this one
	if _, err := svc.ReplaceGrants(ctx, m.UUID, []string{"u9"}); !errors.Is(err, ErrGrantNotMember) {
		t.Fatalf("foreign member err = %v", err)
	}
	granted, err := svc.ReplaceGrants(ctx, m.UUID, []string{"u1", "u2", "u1"})
	if err != nil || len(granted) != 2 {
		t.Fatalf("grants = %+v, %v", granted, err)
	}
	if _, err := svc.ReplaceGrants(ctx, "nope", []string{"u1"}); !errors.Is(err, ErrNotFound) {
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
		Purposes:      []models.LLMModelPurpose{{Purpose: "default"}}, Access: models.AccessGranted,
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

func TestCatalog_UpdateModel(t *testing.T) {
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
	updated, err := svc.UpdateModel(ctx, m.UUID, in, &disabled)
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
	if _, err := svc.UpdateModel(ctx, m.UUID, modelInput("Slow", cred), nil); !errors.Is(err, ErrNameInUse) {
		t.Fatalf("rename collision err = %v", err)
	}
	bad := "paused"
	if _, err := svc.UpdateModel(ctx, m.UUID, in, &bad); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("bad status err = %v", err)
	}
	if _, err := svc.UpdateModel(ctx, "nope", in, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown err = %v", err)
	}
	// A disabled model no longer pins its credential.
	if err := svc.DeleteCredential(ctx, cred.UUID); !errors.Is(err, ErrCredentialInUse) {
		t.Fatalf("the other active model still pins the credential: %v", err)
	}
}
