package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// Admin operation limits (spec "API"): 10 per minute per org+credential for
// test and discovery, per org+user for the prompt test.
const (
	opsLimit  = 10
	opsWindow = time.Minute
	// probeTimeout bounds a credential test or a model listing.
	probeTimeout = 15 * time.Second
	// maxPromptOutputBytes is what the prompt test collects for display.
	maxPromptOutputBytes = 64 << 10
	maxDiscoveredModels  = 1000
	// AdminTestCaller is the ledger caller of the admin prompt test.
	AdminTestCaller = "llm.admin_test"
)

// PromptRunner runs a chat call on one model, bypassing grants (the
// Gateway's ChatPinned).
type PromptRunner interface {
	ChatPinned(ctx context.Context, m models.Model, req iface.ChatRequest, sink iface.ChatSink) (*iface.ChatResult, error)
}

var _ PromptRunner = (*Gateway)(nil)

// AdminOps runs the operator's live checks: a credential's connectivity
// test, model discovery and the prompt test. Each applies the same
// provider gate as the live path (ruling 5.3), so nothing reaches a cloud
// provider while allow_hosted is off, and each is rate-limited in Redis.
type AdminOps struct {
	catalog  *CatalogService
	registry *providers.Registry
	clients  func(kind string) *http.Client
	limiter  *RateLimiter
	prompts  PromptRunner
	logger   *slog.Logger
}

func NewAdminOps(catalog *CatalogService, registry *providers.Registry, clients func(kind string) *http.Client, limiter *RateLimiter, prompts PromptRunner, logger *slog.Logger) *AdminOps {
	if logger == nil {
		logger = slog.Default()
	}
	if clients == nil {
		clients = func(string) *http.Client { return nil }
	}
	return &AdminOps{catalog: catalog, registry: registry, clients: clients, limiter: limiter, prompts: prompts, logger: logger}
}

// guard requires an internal tenant and charges one call of op on the
// org's counter for subject.
func (o *AdminOps) guard(ctx context.Context, op, subject string) error {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return err
	}
	tenantID, ok := tenantrepo.CurrentTenantID(ctx)
	if !ok || tenantID == "" {
		return tenantrepo.ErrTenantScopeMissing
	}
	return o.limiter.Check(ctx, op+":"+tenantID+":"+subject, opsLimit, opsWindow)
}

// credential loads the credential and applies the provider gate.
func (o *AdminOps) credential(ctx context.Context, id string) (*models.Credential, error) {
	c, err := o.catalog.GetCredential(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := o.catalog.checkProviderGate(c.Provider); err != nil {
		return nil, err
	}
	return c, nil
}

// build decrypts the secret just-in-time and builds the adapter.
func (o *AdminOps) build(ctx context.Context, c *models.Credential) (providers.Provider, error) {
	secret, err := o.catalog.OpenCredentialSecret(ctx, c)
	if err != nil {
		return nil, err
	}
	p, err := o.registry.Build(c.Provider, providers.Credential{Kind: models.CredentialKindOrg, Secret: secret, BaseURL: c.BaseURL}, o.clients(c.Provider))
	if err != nil {
		// %v: no adapter, or a constructor refusing the config, is the
		// provider being unavailable, never the caller's request.
		return nil, fmt.Errorf("%w: %s adapter: %v", iface.ErrLLMProviderUnavailable, c.Provider, err)
	}
	return p, nil
}

// probeCode is the llm.* code a failed probe records: a closed set, never
// provider text.
func probeCode(err error) string {
	switch {
	case errors.Is(err, providers.ErrUnauthorized):
		return errcode.LLMCredentialRejected
	case errors.Is(err, iface.ErrLLMQuotaExhausted):
		return errcode.LLMPlanQuotaExhausted
	case errors.Is(err, providers.ErrDestinationNotPublic):
		return errcode.LLMEndpointNotAllowed
	case errors.Is(err, ErrSecretKeyMissing), errors.Is(err, ErrEnvelopeCorrupt):
		return errcode.LLMSecretKeyMissing
	}
	return errcode.LLMProviderUnavailable
}

// TestCredential checks that the credential reaches its provider and is
// accepted (the adapter's Health), records the outcome on the credential
// (lastTestedAt/lastTestStatus/lastTestError) and audits it. A provider
// failure is a result (outcome error + code), not an error; errors are the
// caller's (not found, gate, rate limit) or the server's.
func (o *AdminOps) TestCredential(ctx context.Context, id string) (*models.LLMCredentialTestResult, error) {
	if err := o.guard(ctx, "test", id); err != nil {
		return nil, err
	}
	c, err := o.credential(ctx, id)
	if err != nil {
		return nil, err
	}
	p, err := o.build(ctx, c)
	if err == nil {
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		err = p.Health(pctx)
		cancel()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err() // the admin hung up: nothing was learned about the credential
	}
	res := &models.LLMCredentialTestResult{Outcome: models.TestStatusOK, TestedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err != nil {
		res.Outcome, res.Code = models.TestStatusError, probeCode(err)
		o.logger.InfoContext(ctx, "llm: credential test failed", slog.String("resource_uuid", c.UUID), slog.String("provider", c.Provider), slog.String("code", res.Code))
	}
	o.catalog.recordCredentialTest(ctx, c, res.TestedAt, res.Outcome, res.Code)
	return res, nil
}

// DiscoverModels lists the model ids the credential's provider reports
// (best effort for openai_compatible). Nothing is persisted; the call is
// audited.
func (o *AdminOps) DiscoverModels(ctx context.Context, id string) ([]models.LLMDiscoveredModel, error) {
	if err := o.guard(ctx, "models", id); err != nil {
		return nil, err
	}
	c, err := o.credential(ctx, id)
	if err != nil {
		return nil, err
	}
	p, err := o.build(ctx, c)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	list, err := p.ListModels(pctx)
	cancel()
	if err != nil {
		return nil, err
	}
	out := make([]models.LLMDiscoveredModel, 0, min(len(list), maxDiscoveredModels))
	for _, d := range list {
		if len(out) == maxDiscoveredModels {
			break
		}
		out = append(out, models.LLMDiscoveredModel{ID: d.ID, DisplayName: d.DisplayName})
	}
	o.catalog.emit(ctx, "llm.credential.models_discovered", "llm_credential", c.UUID, map[string]any{"provider": c.Provider, "name": c.Name, "count": len(out)})
	return out, nil
}

// PromptTest sends prompt as one user message to the model, ignoring the
// grants, and collects the streamed answer (at most 64 KiB is kept). The
// call is reserved against the budget and ledgered with caller
// llm.admin_test and the admin's user UUID; the prompt itself is stored
// nowhere.
func (o *AdminOps) PromptTest(ctx context.Context, modelUUID, prompt string) (*models.LLMPromptTestResult, error) {
	userUUID, _ := ctxauth.GetUserUUID(ctx)
	if err := o.guard(ctx, "prompt", userUUID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(prompt) == "" || len(prompt) > 4000 {
		return nil, fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, ErrInvalidPrompt)
	}
	v, err := o.catalog.GetModel(ctx, modelUUID)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	truncated := false
	res, err := o.prompts.ChatPinned(ctx, v.Model, iface.ChatRequest{
		Caller:   AdminTestCaller,
		Messages: []iface.ChatMessage{{Role: "user", Content: prompt}},
	}, func(ev iface.ChatEvent) error {
		if ev.Delta == "" || truncated {
			return nil
		}
		if buf.Len()+len(ev.Delta) > maxPromptOutputBytes {
			truncated = true
			return nil
		}
		buf.WriteString(ev.Delta)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &models.LLMPromptTestResult{Text: buf.String(), Truncated: truncated, Usage: res.Usage, FinishReason: res.FinishReason, Fallback: res.Fallback}, nil
}
