package handlers

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/internal/core/llm/services"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// Fakes over the exported AccountsService collaborators (ruling 3.T8.8):
// the handler tests drive the real service. They mirror the real
// repository on the points the handlers rely on (tenant-scoped rows,
// Insert stamps the tenant, repository errors).

var (
	_ services.AccountRepo  = (*fakeAccounts)(nil)
	_ services.HostIDSource = fakeHost("")
	_ services.OIDC         = (*fakeSIWCOIDC)(nil)
	_ services.PlanCatalog  = (*fakeSIWCOIDC)(nil)
	_ module.RedisClient    = (*memRedis)(nil)
)

type fakeAccounts struct {
	mu   sync.Mutex
	rows map[string]*models.UserAccount
}

func newFakeAccounts() *fakeAccounts { return &fakeAccounts{rows: map[string]*models.UserAccount{}} }

func (f *fakeAccounts) Insert(ctx context.Context, a *models.UserAccount) error {
	tenant, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.TenantID == tenant && (r.UUID == a.UUID || (r.UserUUID == a.UserUUID && r.ClientID == a.ClientID) ||
			(a.Selected && r.Selected && r.UserUUID == a.UserUUID && r.Provider == a.Provider)) {
			return repository.ErrDuplicateName
		}
	}
	a.TenantID = tenant
	cp := *a
	cp.SavedAt = cp.SavedAt.Truncate(time.Millisecond)
	f.rows[a.UUID] = &cp
	return nil
}

func (f *fakeAccounts) scoped(ctx context.Context, keep func(*models.UserAccount) bool) []models.UserAccount {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []models.UserAccount{}
	for _, r := range f.rows {
		if r.TenantID == tenantOf(ctx) && keep(r) {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LinkedAt.Before(out[j].LinkedAt) })
	return out
}

func (f *fakeAccounts) ListByUser(ctx context.Context, user string) ([]models.UserAccount, error) {
	return f.scoped(ctx, func(r *models.UserAccount) bool { return r.UserUUID == user }), nil
}

func (f *fakeAccounts) ListAdmin(ctx context.Context) ([]models.UserAccount, error) {
	return f.scoped(ctx, func(*models.UserAccount) bool { return true }), nil
}

func (f *fakeAccounts) Get(ctx context.Context, id string) (*models.UserAccount, error) {
	out := f.scoped(ctx, func(r *models.UserAccount) bool { return r.UUID == id })
	if len(out) == 0 {
		return nil, repository.ErrNotFound
	}
	return &out[0], nil
}

func (f *fakeAccounts) GetSelected(ctx context.Context, user, provider string) (*models.UserAccount, error) {
	out := f.scoped(ctx, func(r *models.UserAccount) bool { return r.UserUUID == user && r.Provider == provider && r.Selected })
	if len(out) == 0 {
		return nil, repository.ErrNotFound
	}
	return &out[0], nil
}

func (f *fakeAccounts) Select(ctx context.Context, user, provider, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok || t.TenantID != tenantOf(ctx) || t.UserUUID != user || t.Provider != provider {
		return repository.ErrNotFound
	}
	for _, r := range f.rows {
		if r.TenantID == t.TenantID && r.UserUUID == user && r.Provider == provider {
			r.Selected = r.UUID == id
		}
	}
	return nil
}

func (f *fakeAccounts) Delete(ctx context.Context, id string) (*models.UserAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok || r.TenantID != tenantOf(ctx) {
		return nil, repository.ErrNotFound
	}
	delete(f.rows, id)
	cp := *r
	return &cp, nil
}

func (f *fakeAccounts) mutate(ctx context.Context, id string, fn func(*models.UserAccount) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok || r.TenantID != tenantOf(ctx) {
		return repository.ErrNotFound
	}
	return fn(r)
}

func (f *fakeAccounts) SetLastUsed(ctx context.Context, id string, at time.Time) error {
	return f.mutate(ctx, id, func(r *models.UserAccount) error { r.LastUsedAt = &at; return nil })
}

func (f *fakeAccounts) SetStatus(ctx context.Context, id, status, lastError string) error {
	return f.mutate(ctx, id, func(r *models.UserAccount) error { r.Status, r.LastError = status, lastError; return nil })
}

func (f *fakeAccounts) SetStatusIf(ctx context.Context, id string, prev time.Time, status, lastError string) error {
	return f.mutate(ctx, id, func(r *models.UserAccount) error {
		if !r.SavedAt.Equal(prev) {
			return repository.ErrStaleTokenSet
		}
		r.Status, r.LastError = status, lastError
		return nil
	})
}

func (f *fakeAccounts) ReplaceTokens(ctx context.Context, id string, prev time.Time, set models.AccountTokenSet) error {
	return f.mutate(ctx, id, func(r *models.UserAccount) error {
		if !r.SavedAt.Equal(prev) {
			return repository.ErrStaleTokenSet
		}
		if set.IDToken != nil {
			r.IDToken = *set.IDToken
		}
		r.AccessToken, r.RefreshToken, r.TokenType = set.AccessToken, set.RefreshToken, set.TokenType
		r.ExpiresAt, r.RefreshExpiresAt, r.EarliestRefreshAt = set.ExpiresAt, set.RefreshExpiresAt, set.EarliestRefreshAt
		r.SavedAt, r.Scopes, r.PlanUsageGranted, r.Status, r.LastError = set.SavedAt.Truncate(time.Millisecond), set.Scopes, set.PlanUsageGranted, set.Status, ""
		if set.Email != "" {
			r.Email = set.Email
		}
		return nil
	})
}

// all returns every row, whatever its tenant (test reads).
func (f *fakeAccounts) all() []models.UserAccount {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []models.UserAccount{}
	for _, r := range f.rows {
		out = append(out, *r)
	}
	return out
}

type fakeHost string

func (h fakeHost) GetOrCreateHostID(context.Context) (string, error) { return string(h), nil }

// fakeSIWCOIDC plays OpenAI: the authorize URL carries the state, the code
// exchange mints synthetic tokens (never real credentials), the ID token
// "validates" to sub/email unless idErr is set.
type fakeSIWCOIDC struct {
	mu        sync.Mutex
	sub       string
	scope     string
	idErr     error
	exchanges int
	revoked   []string
}

func (o *fakeSIWCOIDC) AuthorizeURL(p services.AuthorizeParams) string {
	q := url.Values{"state": {p.State}, "client_id": {p.ClientID}, "redirect_uri": {p.RedirectURI}}
	return "https://auth.openai.example/oauth/authorize?" + q.Encode()
}

func (o *fakeSIWCOIDC) Exchange(_ context.Context, clientID, code, _, _ string) (*services.TokenResponse, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.exchanges++
	return &services.TokenResponse{
		AccessToken: "AT-SENTINEL-" + code, RefreshToken: "RT-SENTINEL-" + code, IDToken: "IDT-SENTINEL-" + clientID,
		TokenType: "Bearer", Scope: o.scope, ExpiresIn: 3600,
	}, nil
}

func (o *fakeSIWCOIDC) Refresh(context.Context, string, string) (*services.TokenResponse, error) {
	return nil, errors.New("refresh not expected in handler tests")
}

func (o *fakeSIWCOIDC) Revoke(_ context.Context, _, refreshToken string) error {
	o.mu.Lock()
	o.revoked = append(o.revoked, refreshToken)
	o.mu.Unlock()
	return nil
}

func (o *fakeSIWCOIDC) VerifyIDToken(_ context.Context, _, aud, nonce string) (*services.IDClaims, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.idErr != nil {
		return nil, o.idErr
	}
	return &services.IDClaims{Issuer: "https://auth.openai.example", Subject: o.sub, Email: "plan@example.test", Nonce: nonce, Audience: []string{aud}}, nil
}

func (o *fakeSIWCOIDC) ListPlanModels(context.Context, string) ([]string, error) {
	return []string{"gpt-plan"}, nil
}

func (o *fakeSIWCOIDC) exchanged() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.exchanges
}

// memRedis is an in-memory module.RedisClient (Get on a missing key is an
// error, like the adapter's redis.Nil). flush drops every key: the
// ten-minute state TTL elapsing.
type memRedis struct {
	mu sync.Mutex
	kv map[string]string
	n  map[string]int64
}

func newMemRedis() *memRedis { return &memRedis{kv: map[string]string{}, n: map[string]int64{}} }

func (r *memRedis) Get(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.kv[key]
	if !ok {
		return "", errors.New("redis: nil")
	}
	return v, nil
}

func (r *memRedis) Set(_ context.Context, key string, value interface{}, _ time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, _ := value.(string)
	r.kv[key] = s
	return nil
}

func (r *memRedis) Del(_ context.Context, keys ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range keys {
		delete(r.kv, k)
		delete(r.n, k)
	}
	return nil
}

func (r *memRedis) Keys(_ context.Context, pattern string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := strings.TrimSuffix(pattern, "*")
	out := []string{}
	for k := range r.kv {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (r *memRedis) Incr(_ context.Context, key string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n[key]++
	return r.n[key], nil
}

func (r *memRedis) Expire(context.Context, string, time.Duration) error { return nil }

func (r *memRedis) flushState() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.kv {
		if strings.HasPrefix(k, "llm:siwc:state:") {
			delete(r.kv, k)
		}
	}
	for k := range r.n {
		if strings.HasPrefix(k, "llm:siwc:state:") {
			delete(r.n, k)
		}
	}
}

// counted reports whether any key starting with prefix was incremented.
func (r *memRedis) counted(prefix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.n {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// recordingMetrics is a handlers.SIWCMetrics that keeps the outcomes.
type recordingMetrics struct {
	mu       sync.Mutex
	outcomes []string
}

func (m *recordingMetrics) RecordLLMSIWCLink(outcome string) {
	m.mu.Lock()
	m.outcomes = append(m.outcomes, outcome)
	m.mu.Unlock()
}

func (m *recordingMetrics) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.outcomes) == 0 {
		return ""
	}
	return m.outcomes[len(m.outcomes)-1]
}
