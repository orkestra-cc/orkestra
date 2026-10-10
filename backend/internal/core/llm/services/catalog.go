package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// Narrow repository contracts so tests inject in-memory fakes. The real
// repositories satisfy them (asserted below).
type CredentialRepo interface {
	Insert(ctx context.Context, c *models.Credential) error
	List(ctx context.Context) ([]models.Credential, error)
	Get(ctx context.Context, uuid string) (*models.Credential, error)
	// Patch writes only the provided fields (and updatedAt); it returns the
	// updatedAt it wrote.
	Patch(ctx context.Context, uuid string, p models.CredentialPatch) (time.Time, error)
	// SetSecret writes the secret envelope and its display tail only and
	// returns the updatedAt it wrote.
	SetSecret(ctx context.Context, uuid string, env models.Envelope, last4 string) (time.Time, error)
	Delete(ctx context.Context, uuid string) error
}

type ModelRepo interface {
	Insert(ctx context.Context, m *models.Model) error
	List(ctx context.Context) ([]models.Model, error)
	ListActive(ctx context.Context) ([]models.Model, error)
	ListActiveByCredential(ctx context.Context, credentialUUID string) ([]models.Model, error)
	Get(ctx context.Context, uuid string) (*models.Model, error)
	// Update writes every editable field except access (owned by
	// GrantRepo.Replace).
	Update(ctx context.Context, m *models.Model) error
	Delete(ctx context.Context, uuid string) error
}

type GrantRepo interface {
	ListByModel(ctx context.Context, modelUUID string) ([]models.LLMGrant, error)
	ListByUser(ctx context.Context, userUUID string) ([]models.LLMGrant, error)
	// Replace sets the model's access and its complete grant list in one
	// transaction; repository.ErrNotFound when the model is unknown.
	Replace(ctx context.Context, modelUUID, access, actor string, userUUIDs []string) error
	DeleteByModel(ctx context.Context, modelUUID string) error
}

var (
	_ CredentialRepo = (*repository.Credentials)(nil)
	_ ModelRepo      = (*repository.Models)(nil)
	_ GrantRepo      = (*repository.Grants)(nil)
)

// CatalogConfig is read on every call so admin changes apply without a
// restart.
type CatalogConfig struct {
	AllowHosted    bool
	ProductionLike bool
}

// CatalogService owns credentials, models and grants for an org and the
// coherence rules between them. Every public method first requires an
// internal (Tier-1) tenant in ctx: the route-level RequireInternalTenant
// passes external tenants through under TENANT_KIND_ENFORCEMENT=warn, and
// llm rows must never be written or read under a client org. In dev an
// unset tenant kind panics there (a wiring fault); elsewhere it is
// tenantrepo.ErrTenantScopeMissing.
type CatalogService struct {
	creds  CredentialRepo
	models ModelRepo
	grants GrantRepo
	vault  *Vault
	dir    iface.TenantDirectoryReader
	cfg    func() CatalogConfig
	logger *slog.Logger

	auditMu sync.RWMutex
	audit   iface.AuditSink
}

func NewCatalogService(creds CredentialRepo, mods ModelRepo, grants GrantRepo, vault *Vault, dir iface.TenantDirectoryReader, cfg func() CatalogConfig, logger *slog.Logger) *CatalogService {
	if logger == nil {
		logger = slog.Default()
	}
	return &CatalogService{creds: creds, models: mods, grants: grants, vault: vault, dir: dir, cfg: cfg, logger: logger}
}

// SetAuditSink satisfies iface.AuditSinkSetter; nil-safe.
func (s *CatalogService) SetAuditSink(sink iface.AuditSink) {
	if sink == nil {
		return
	}
	s.auditMu.Lock()
	s.audit = sink
	s.auditMu.Unlock()
}

// emit records a successful mutation. Metadata must never carry secrets or
// base URLs (they can embed credentials).
func (s *CatalogService) emit(ctx context.Context, action, resourceType, resourceID string, meta map[string]any) {
	s.auditMu.RLock()
	sink := s.audit
	s.auditMu.RUnlock()
	if sink == nil {
		return
	}
	tenantID, _ := tenantrepo.CurrentTenantID(ctx)
	kind := tenantrepo.CurrentTenantKind(ctx)
	if kind == "" {
		kind = "internal"
	}
	actor, _ := ctxauth.GetUserUUID(ctx)
	email, _ := ctxauth.GetUserEmail(ctx)
	ip, _ := ctxauth.GetClientIP(ctx)
	sink.Emit(ctx, iface.AuditEvent{
		TenantID: tenantID, TenantKind: kind, ActorUserID: actor, ActorEmail: email, ActorType: "user",
		Action: action, ResourceType: resourceType, ResourceID: resourceID, Outcome: "success", IPAddress: ip, Metadata: meta,
	})
}

func mapRepoErr(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, repository.ErrDuplicateName):
		return ErrNameInUse
	}
	return err
}

// --- credentials ---------------------------------------------------------

// providerGate applies the operator opt-ins to provider under cfg: hosted
// providers need allow_hosted, the mock provider never runs in
// production-like envs. The catalog applies it on every write that leaves
// something live, the AccessResolver on every read, so turning
// allow_hosted off takes effect for existing models without editing them.
func providerGate(cfg CatalogConfig, provider string) error {
	if models.IsHostedProvider(provider) && !cfg.AllowHosted {
		return ErrHostedDisabled
	}
	if provider == models.ProviderMock && cfg.ProductionLike {
		return ErrMockNotAllowed
	}
	return nil
}

func (s *CatalogService) checkProviderGate(provider string) error {
	return providerGate(s.cfg(), provider)
}

// resolveBaseURL returns the endpoint to store: none for mock, the fixed
// vendor URL for cloud providers, the validated + normalized operator URL
// otherwise.
func (s *CatalogService) resolveBaseURL(provider, baseURL string) (string, error) {
	if provider == models.ProviderMock {
		return "", nil
	}
	if fixed := models.FixedBaseURL(provider); fixed != "" {
		return fixed, nil
	}
	return ValidateEndpoint(baseURL, s.cfg().ProductionLike)
}

func (s *CatalogService) CreateCredential(ctx context.Context, in models.CredentialInput) (*models.Credential, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	if err := models.ValidateCredentialInput(in, true); err != nil {
		return nil, fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, err)
	}
	if err := s.checkProviderGate(in.Provider); err != nil {
		return nil, err
	}
	baseURL, err := s.resolveBaseURL(in.Provider, in.BaseURL)
	if err != nil {
		return nil, err
	}
	tenantID, ok := tenantrepo.CurrentTenantID(ctx)
	if !ok || tenantID == "" {
		return nil, tenantrepo.ErrTenantScopeMissing
	}
	actor, _ := ctxauth.GetUserUUID(ctx)
	now := time.Now().UTC()
	c := &models.Credential{
		UUID: uuid.NewString(), TenantID: tenantID, Name: in.Name, Provider: in.Provider, BaseURL: baseURL,
		Status: models.CredentialStatusActive, CreatedBy: actor, CreatedAt: now, UpdatedAt: now,
	}
	if in.Secret != "" {
		env, err := s.vault.Seal(ctx, tenantID, c.UUID, "secret", in.Secret)
		if err != nil {
			return nil, err
		}
		c.Secret = env
		c.SecretLast4 = models.Last4(in.Secret)
	}
	if err := s.creds.Insert(ctx, c); err != nil {
		return nil, mapRepoErr(err)
	}
	s.emit(ctx, "llm.credential.created", "llm_credential", c.UUID, map[string]any{"provider": c.Provider, "name": c.Name})
	return c, nil
}

func (s *CatalogService) ListCredentials(ctx context.Context) ([]models.Credential, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	return s.creds.List(ctx)
}

func (s *CatalogService) GetCredential(ctx context.Context, id string) (*models.Credential, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	c, err := s.creds.Get(ctx, id)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return c, nil
}

// PatchCredential changes name, base URL and/or status. The hosted/mock
// gate is skipped when the credential ends up disabled, so an operator who
// turned allow_hosted off can still disable what already exists; enabling
// or editing a live credential still goes through the gate.
func (s *CatalogService) PatchCredential(ctx context.Context, id string, name, baseURL, status *string) (*models.Credential, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	c, err := s.GetCredential(ctx, id)
	if err != nil {
		return nil, err
	}
	in := models.CredentialInput{Name: c.Name, Provider: c.Provider, BaseURL: c.BaseURL}
	if name != nil {
		in.Name = *name
	}
	if baseURL != nil {
		in.BaseURL = *baseURL
	}
	if err := models.ValidateCredentialInput(in, false); err != nil {
		return nil, fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, err)
	}
	newStatus := c.Status
	if status != nil {
		if *status != models.CredentialStatusActive && *status != models.CredentialStatusDisabled {
			return nil, fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, ErrInvalidStatus)
		}
		newStatus = *status
	}
	if newStatus != models.CredentialStatusDisabled {
		if err := s.checkProviderGate(c.Provider); err != nil {
			return nil, err
		}
	}
	// Only what the request provided reaches the repository, so a patch
	// built on a stale read cannot revert a concurrent change to the rest.
	var patch models.CredentialPatch
	if name != nil {
		patch.Name = &in.Name
		c.Name = in.Name
	}
	if status != nil {
		patch.Status = &newStatus
		c.Status = newStatus
	}
	if baseURL != nil {
		normalized, err := s.resolveBaseURL(c.Provider, in.BaseURL)
		if err != nil {
			return nil, err
		}
		patch.BaseURL = &normalized
		c.BaseURL = normalized
	}
	updatedAt, err := s.creds.Patch(ctx, c.UUID, patch)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	c.UpdatedAt = updatedAt
	s.emit(ctx, "llm.credential.updated", "llm_credential", c.UUID, map[string]any{"provider": c.Provider, "name": c.Name, "status": c.Status})
	return c, nil
}

func (s *CatalogService) RotateCredential(ctx context.Context, id, secret string) (*models.Credential, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	if secret == "" {
		return nil, fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, models.ErrSecretRequired)
	}
	c, err := s.GetCredential(ctx, id)
	if err != nil {
		return nil, err
	}
	tenantID, ok := tenantrepo.CurrentTenantID(ctx)
	if !ok || tenantID == "" {
		return nil, tenantrepo.ErrTenantScopeMissing
	}
	env, err := s.vault.Seal(ctx, tenantID, c.UUID, "secret", secret)
	if err != nil {
		return nil, err
	}
	last4 := models.Last4(secret)
	// SetSecret, not Patch: the rotation owns the secret and nothing else,
	// so a rename or disable racing it is neither reverted nor reverts it.
	updatedAt, err := s.creds.SetSecret(ctx, c.UUID, env, last4)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	c.UpdatedAt = updatedAt
	c.Secret, c.SecretLast4 = env, last4
	c.LastTestedAt, c.LastTestStatus, c.LastTestError = nil, "", ""
	s.emit(ctx, "llm.credential.rotated", "llm_credential", c.UUID, map[string]any{"provider": c.Provider, "name": c.Name})
	return c, nil
}

func (s *CatalogService) DeleteCredential(ctx context.Context, id string) error {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return err
	}
	c, err := s.GetCredential(ctx, id)
	if err != nil {
		return err
	}
	using, err := s.models.ListActiveByCredential(ctx, id)
	if err != nil {
		return err
	}
	if len(using) > 0 {
		return ErrCredentialInUse
	}
	if err := s.creds.Delete(ctx, id); err != nil {
		return mapRepoErr(err)
	}
	s.emit(ctx, "llm.credential.deleted", "llm_credential", c.UUID, map[string]any{"provider": c.Provider, "name": c.Name})
	return nil
}

// OpenCredentialSecret decrypts just-in-time; the caller must not retain
// the plaintext beyond the provider call it builds. c must come from the
// repository (its TenantID is part of the AAD).
func (s *CatalogService) OpenCredentialSecret(ctx context.Context, c *models.Credential) (string, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return "", err
	}
	if c.Secret.IsZero() {
		return "", nil
	}
	return s.vault.Open(ctx, c.TenantID, c.UUID, "secret", c.Secret)
}

// --- models ---------------------------------------------------------------

// checkModelInput validates the shape and the credential coherence of in.
// A model that will be live (created, or patched to end up active) also
// needs its org credential to be active, the same rule the AccessResolver
// applies on every read; a model that ends up disabled may keep pointing at
// a disabled credential, so disabling always works.
func (s *CatalogService) checkModelInput(ctx context.Context, in models.ModelInput, live bool) error {
	if err := models.ValidateModelInput(in); err != nil {
		return fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, err)
	}
	if in.CredentialRef.Kind == models.CredentialKindOrg {
		c, err := s.GetCredential(ctx, in.CredentialRef.CredentialUUID)
		if err != nil {
			return err
		}
		if c.Provider != in.Provider {
			return fmt.Errorf("%w: %w (%s, %s)", iface.ErrLLMInvalidRequest, ErrProviderMismatch, c.Provider, in.Provider)
		}
		if live && c.Status != models.CredentialStatusActive {
			return fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, ErrCredentialDisabled)
		}
	}
	return nil
}

func (s *CatalogService) CreateModel(ctx context.Context, in models.ModelInput) (*models.Model, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	if err := s.checkModelInput(ctx, in, true); err != nil {
		return nil, err
	}
	// New models are active: same opt-ins as the credential they ride on.
	if err := s.checkProviderGate(in.Provider); err != nil {
		return nil, err
	}
	actor, _ := ctxauth.GetUserUUID(ctx)
	now := time.Now().UTC()
	m := &models.Model{
		UUID: uuid.NewString(), Name: in.Name, Provider: in.Provider, ModelID: in.ModelID, Capabilities: in.Capabilities,
		CredentialRef: in.CredentialRef, Defaults: in.Defaults, BudgetReserveOutputTokens: in.BudgetReserveOutputTokens,
		Purposes: in.Purposes, Access: models.AccessGranted, Status: models.ModelStatusActive, CreatedBy: actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.models.Insert(ctx, m); err != nil {
		return nil, mapRepoErr(err)
	}
	s.emit(ctx, "llm.model.created", "llm_model", m.UUID, map[string]any{"provider": m.Provider, "name": m.Name, "modelId": m.ModelID, "credentialKind": m.CredentialRef.Kind})
	return m, nil
}

func (s *CatalogService) withGrants(ctx context.Context, m models.Model) (*models.LLMModelView, error) {
	g, err := s.grants.ListByModel(ctx, m.UUID)
	if err != nil {
		return nil, err
	}
	return &models.LLMModelView{Model: m, Grants: g}, nil
}

func (s *CatalogService) ListModels(ctx context.Context) ([]models.LLMModelView, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	list, err := s.models.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.LLMModelView, 0, len(list))
	for _, m := range list {
		v, err := s.withGrants(ctx, m)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

func (s *CatalogService) GetModel(ctx context.Context, id string) (*models.LLMModelView, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	m, err := s.models.Get(ctx, id)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return s.withGrants(ctx, *m)
}

// PatchModel applies the provided fields of p onto the stored model and
// validates the merged result with the create rules (shape, credential
// coherence, provider gate, active credential); absent fields keep their
// stored value. A model that ends up disabled skips the provider gate and
// the active-credential rule, so disabling always works.
func (s *CatalogService) PatchModel(ctx context.Context, id string, p models.LLMModelPatchBody) (*models.Model, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	m, err := s.models.Get(ctx, id)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	in := p.ApplyTo(m.Input())
	if p.Status != nil {
		if *p.Status != models.ModelStatusActive && *p.Status != models.ModelStatusDisabled {
			return nil, fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, ErrInvalidStatus)
		}
		m.Status = *p.Status
	}
	if err := s.checkModelInput(ctx, in, m.Status != models.ModelStatusDisabled); err != nil {
		return nil, err
	}
	m.Name, m.Provider, m.ModelID, m.Capabilities = in.Name, in.Provider, in.ModelID, in.Capabilities
	m.CredentialRef, m.Defaults, m.BudgetReserveOutputTokens, m.Purposes = in.CredentialRef, in.Defaults, in.BudgetReserveOutputTokens, in.Purposes
	// Same rule as PatchCredential: a model that ends up disabled is never
	// gated (disabling must always work); enabling or editing a live one is.
	if m.Status != models.ModelStatusDisabled {
		if err := s.checkProviderGate(in.Provider); err != nil {
			return nil, err
		}
	}
	if err := s.models.Update(ctx, m); err != nil {
		return nil, mapRepoErr(err)
	}
	s.emit(ctx, "llm.model.updated", "llm_model", m.UUID, map[string]any{"provider": m.Provider, "name": m.Name, "modelId": m.ModelID, "status": m.Status})
	return m, nil
}

func (s *CatalogService) DeleteModel(ctx context.Context, id string) error {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return err
	}
	m, err := s.models.Get(ctx, id)
	if err != nil {
		return mapRepoErr(err)
	}
	if err := s.grants.DeleteByModel(ctx, id); err != nil {
		return err
	}
	if err := s.models.Delete(ctx, id); err != nil {
		return mapRepoErr(err)
	}
	s.emit(ctx, "llm.model.deleted", "llm_model", m.UUID, map[string]any{"provider": m.Provider, "name": m.Name})
	return nil
}

// ReplaceGrants decides who may use a model: it sets access and replaces
// the complete grant list together, atomically. Access and every grantee are
// validated first (a single non-member rejects the whole call) and nothing
// is written on a refusal or a failed write. With access everyone the grants are stored but dormant.
// This is the only place access changes; it sits behind llm.grants.admin
// and step-up, never behind the model write permission.
func (s *CatalogService) ReplaceGrants(ctx context.Context, modelUUID, access string, userUUIDs []string) (*models.LLMModelView, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	if err := models.ValidateAccess(access); err != nil {
		return nil, fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, err)
	}
	m, err := s.models.Get(ctx, modelUUID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	tenantID, ok := tenantrepo.CurrentTenantID(ctx)
	if !ok || tenantID == "" {
		return nil, tenantrepo.ErrTenantScopeMissing
	}
	members, err := s.dir.ListTenantMembers(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("llm: list members: %w", err)
	}
	isMember := make(map[string]bool, len(members))
	for _, mb := range members {
		isMember[mb.UserUUID] = true
	}
	seen := make(map[string]bool, len(userUUIDs))
	unique := make([]string, 0, len(userUUIDs))
	for _, u := range userUUIDs {
		if seen[u] {
			continue
		}
		if !isMember[u] {
			return nil, fmt.Errorf("%w: %s", ErrGrantNotMember, u)
		}
		seen[u] = true
		unique = append(unique, u)
	}
	actor, _ := ctxauth.GetUserUUID(ctx)
	// Access and the grant list are written together in one transaction
	// that also bumps the model's grants revision: concurrent replacements
	// of one model are serialized and a failure writes nothing. Access is
	// always written, never skipped on the value read above (it may be
	// stale by now).
	if err := s.grants.Replace(ctx, modelUUID, access, actor, unique); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, mapRepoErr(err)
		}
		return nil, fmt.Errorf("llm: replace grants: %w", err)
	}
	m.Access = access
	s.emit(ctx, "llm.grants.replaced", "llm_model", modelUUID, map[string]any{"access": access, "granted": len(unique)})
	return s.withGrants(ctx, *m)
}
