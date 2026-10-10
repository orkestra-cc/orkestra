package handlers

import (
	"context"
	"sort"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/internal/core/llm/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// In-memory fakes of the exported services repo contracts. They behave like
// the Mongo repositories on the points the handlers rely on: rows are
// tenant-scoped, Insert stamps the tenant from ctx, names are unique per
// org. The services package keeps its own copies for its internal tests.

var (
	_ services.CredentialRepo     = (*fakeCreds)(nil)
	_ services.ModelRepo          = (*fakeModels)(nil)
	_ services.GrantRepo          = (*fakeGrants)(nil)
	_ iface.TenantDirectoryReader = (*fakeDir)(nil)
)

func tenantOf(ctx context.Context) string {
	id, _ := tenantrepo.CurrentTenantID(ctx)
	return id
}

type fakeCreds struct{ rows map[string]models.Credential }

func (f *fakeCreds) Insert(ctx context.Context, c *models.Credential) error {
	tenant, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	for _, r := range f.rows {
		if r.TenantID == tenant && r.Name == c.Name {
			return repository.ErrDuplicateName
		}
	}
	c.TenantID = tenant
	f.rows[c.UUID] = *c
	return nil
}

func (f *fakeCreds) List(ctx context.Context) ([]models.Credential, error) {
	out := []models.Credential{}
	for _, r := range f.rows {
		if r.TenantID == tenantOf(ctx) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeCreds) Get(ctx context.Context, id string) (*models.Credential, error) {
	if r, ok := f.rows[id]; ok && r.TenantID == tenantOf(ctx) {
		return &r, nil
	}
	return nil, repository.ErrNotFound
}

// Update is field-owned like the real repository: name, baseUrl, status.
func (f *fakeCreds) Update(ctx context.Context, c *models.Credential) error {
	r, ok := f.rows[c.UUID]
	if !ok || r.TenantID != tenantOf(ctx) {
		return repository.ErrNotFound
	}
	r.Name, r.BaseURL, r.Status, r.UpdatedAt = c.Name, c.BaseURL, c.Status, c.UpdatedAt
	f.rows[c.UUID] = r
	return nil
}

func (f *fakeCreds) SetSecret(ctx context.Context, id string, env models.Envelope, last4 string) error {
	r, ok := f.rows[id]
	if !ok || r.TenantID != tenantOf(ctx) {
		return repository.ErrNotFound
	}
	r.Secret, r.SecretLast4 = env, last4
	r.LastTestedAt, r.LastTestStatus, r.LastTestError = nil, "", ""
	f.rows[id] = r
	return nil
}

func (f *fakeCreds) Delete(ctx context.Context, id string) error {
	if r, ok := f.rows[id]; !ok || r.TenantID != tenantOf(ctx) {
		return repository.ErrNotFound
	}
	delete(f.rows, id)
	return nil
}

type fakeModels struct{ rows map[string]models.Model }

func (f *fakeModels) Insert(ctx context.Context, m *models.Model) error {
	tenant, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	for _, r := range f.rows {
		if r.TenantID == tenant && r.Name == m.Name {
			return repository.ErrDuplicateName
		}
	}
	m.TenantID = tenant
	f.rows[m.UUID] = *m
	return nil
}

func (f *fakeModels) List(ctx context.Context) ([]models.Model, error) {
	out := []models.Model{}
	for _, r := range f.rows {
		if r.TenantID == tenantOf(ctx) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeModels) ListActive(ctx context.Context) ([]models.Model, error) {
	all, _ := f.List(ctx)
	out := []models.Model{}
	for _, m := range all {
		if m.Status == models.ModelStatusActive {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeModels) ListActiveByCredential(ctx context.Context, credentialUUID string) ([]models.Model, error) {
	active, _ := f.ListActive(ctx)
	out := []models.Model{}
	for _, m := range active {
		if m.CredentialRef.CredentialUUID == credentialUUID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeModels) Get(ctx context.Context, id string) (*models.Model, error) {
	if r, ok := f.rows[id]; ok && r.TenantID == tenantOf(ctx) {
		return &r, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeModels) Update(ctx context.Context, m *models.Model) error {
	r, ok := f.rows[m.UUID]
	if !ok || r.TenantID != tenantOf(ctx) {
		return repository.ErrNotFound
	}
	for _, o := range f.rows {
		if o.UUID != m.UUID && o.TenantID == r.TenantID && o.Name == m.Name {
			return repository.ErrDuplicateName
		}
	}
	m.TenantID = r.TenantID
	cp := *m
	cp.Access = r.Access // access is owned by Grants.Replace, as in the real repository
	f.rows[m.UUID] = cp
	return nil
}

func (f *fakeModels) Delete(ctx context.Context, id string) error {
	if r, ok := f.rows[id]; !ok || r.TenantID != tenantOf(ctx) {
		return repository.ErrNotFound
	}
	delete(f.rows, id)
	return nil
}

// fakeGrants keys rows by tenant then model, so a grant never leaks across
// orgs the way the real (tenantId, modelUuid, userUuid) rows cannot.
type fakeGrants struct {
	rows map[string]map[string][]string
	// models is where Replace writes access, as the real repository does in
	// the same transaction as the grant rows.
	models *fakeModels
}

func (f *fakeGrants) ListByModel(ctx context.Context, modelUUID string) ([]models.LLMGrant, error) {
	out := []models.LLMGrant{}
	for _, u := range f.rows[tenantOf(ctx)][modelUUID] {
		out = append(out, models.LLMGrant{ModelUUID: modelUUID, UserUUID: u})
	}
	return out, nil
}

func (f *fakeGrants) ListByUser(ctx context.Context, userUUID string) ([]models.LLMGrant, error) {
	out := []models.LLMGrant{}
	for model, users := range f.rows[tenantOf(ctx)] {
		for _, u := range users {
			if u == userUUID {
				out = append(out, models.LLMGrant{ModelUUID: model, UserUUID: u})
			}
		}
	}
	return out, nil
}

func (f *fakeGrants) Replace(ctx context.Context, modelUUID, access, _ string, userUUIDs []string) error {
	tenant := tenantOf(ctx)
	m, ok := f.models.rows[modelUUID]
	if !ok || m.TenantID != tenant {
		return repository.ErrNotFound
	}
	m.Access = access
	m.GrantsRevision++
	f.models.rows[modelUUID] = m
	if f.rows[tenant] == nil {
		f.rows[tenant] = map[string][]string{}
	}
	f.rows[tenant][modelUUID] = append([]string(nil), userUUIDs...)
	return nil
}

func (f *fakeGrants) DeleteByModel(ctx context.Context, modelUUID string) error {
	delete(f.rows[tenantOf(ctx)], modelUUID)
	return nil
}

type fakeDir struct{ members map[string][]string } // tenant -> user UUIDs

func (d *fakeDir) ListTenantMembers(_ context.Context, tenantID string) ([]iface.TenantMemberSummary, error) {
	out := []iface.TenantMemberSummary{}
	for _, u := range d.members[tenantID] {
		out = append(out, iface.TenantMemberSummary{UserUUID: u})
	}
	return out, nil
}
