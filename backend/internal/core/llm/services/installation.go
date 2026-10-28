package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/orkestra/backend/internal/core/llm/installation"
	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// Ledger identity of installation-level inference (ADR-0023).
const (
	InstallationCaller     = "retrieval"
	PurposeRetrievalEmbed  = "retrieval.embed"
	PurposeRetrievalRerank = "retrieval.rerank"
)

var profileIDRE = regexp.MustCompile(`^ip_[0-9a-f]{64}$`)

// InstallationService runs the operator-declared TEI endpoints for the
// retrieval module: validation, tenant checks, profile verification, budget
// reservation, breaker and ledger. It never reaches the org catalog.
type InstallationService struct {
	set     *installation.Set
	budget  *BudgetService
	breaker *Breaker
	cfg     func() RouterConfig
	metrics LLMMetrics
	logger  *slog.Logger
}

func NewInstallationService(set *installation.Set, budget *BudgetService, breaker *Breaker, cfg func() RouterConfig, metrics LLMMetrics, logger *slog.Logger) *InstallationService {
	if logger == nil {
		logger = slog.Default()
	}
	if breaker == nil {
		breaker = NewBreaker(3, 30*time.Second)
	}
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &InstallationService{set: set, budget: budget, breaker: breaker, cfg: cfg, metrics: metrics, logger: logger}
}

func validateInstallationCommon(caller, onBehalfOf, profile string) error {
	if caller != InstallationCaller {
		return iface.ErrLLMModelAccessDenied
	}
	if onBehalfOf == "" {
		return fmt.Errorf("%w: onBehalfOf", iface.ErrLLMInvalidRequest)
	}
	if !profileIDRE.MatchString(profile) {
		return fmt.Errorf("%w: profile", iface.ErrLLMInvalidRequest)
	}
	return nil
}

func validateTexts(texts []string, field string) error {
	if len(texts) == 0 || len(texts) > maxEmbedInputs {
		return fmt.Errorf("%w: %s count", iface.ErrLLMInvalidRequest, field)
	}
	for _, t := range texts {
		if t == "" || len(t) > maxEmbedInputBytes {
			return fmt.Errorf("%w: %s size", iface.ErrLLMInvalidRequest, field)
		}
	}
	return nil
}

// ValidatePinnedEmbedRequest enforces the contract limits before anything else.
func ValidatePinnedEmbedRequest(req iface.PinnedEmbedRequest) error {
	if err := validateInstallationCommon(req.Caller, req.OnBehalfOf, req.Profile); err != nil {
		return err
	}
	if req.Kind != iface.EmbedInputQuery && req.Kind != iface.EmbedInputDocument {
		return fmt.Errorf("%w: kind", iface.ErrLLMInvalidRequest)
	}
	return validateTexts(req.Inputs, "inputs")
}

// ValidateRerankRequest enforces the contract limits before anything else.
func ValidateRerankRequest(req iface.RerankRequest) error {
	if err := validateInstallationCommon(req.Caller, req.OnBehalfOf, req.Profile); err != nil {
		return err
	}
	if req.Query == "" || len(req.Query) > maxEmbedInputBytes {
		return fmt.Errorf("%w: query size", iface.ErrLLMInvalidRequest)
	}
	if err := validateTexts(req.Documents, "documents"); err != nil {
		return err
	}
	if req.TopN < 0 || req.TopN > len(req.Documents) {
		return fmt.Errorf("%w: topN", iface.ErrLLMInvalidRequest)
	}
	return nil
}

// InferenceProfile returns the verified profile of the named endpoint. It
// needs no tenant: retrieval resolves profiles outside a request.
func (s *InstallationService) InferenceProfile(ctx context.Context, endpoint string) (*iface.InferenceProfile, error) {
	v, err := s.set.Verify(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	p := v.Profile
	return &p, nil
}

// resolve runs steps 2-4 of the call path.
func (s *InstallationService) resolve(ctx context.Context, kind iface.InferenceKind, onBehalfOf, profile string) (installation.Endpoint, *installation.Verified, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return installation.Endpoint{}, nil, err
	}
	if id, _ := tenantrepo.CurrentTenantID(ctx); id != onBehalfOf {
		return installation.Endpoint{}, nil, fmt.Errorf("%w: onBehalfOf is not the tenant in context", iface.ErrLLMInvalidRequest)
	}
	if !s.set.HasKind(kind) {
		return installation.Endpoint{}, nil, fmt.Errorf("%w: no %s inference endpoint", iface.ErrLLMNotConfigured, kind)
	}
	ep, ok := s.set.ByProfile(profile)
	if !ok {
		return installation.Endpoint{}, nil, iface.ErrLLMProfileMismatch
	}
	if ep.Kind != kind {
		return installation.Endpoint{}, nil, fmt.Errorf("%w: profile is not a %s profile", iface.ErrLLMInvalidRequest, kind)
	}
	v, err := s.set.Verify(ctx, ep.Name)
	if err != nil {
		return installation.Endpoint{}, nil, err
	}
	return ep, v, nil
}

// admit runs steps 5-6: breaker, then budget reservation. On success the
// caller owns the breaker ticket and the reservation.
func (s *InstallationService) admit(ctx context.Context, ep installation.Endpoint, purpose string, estimate int) (string, uint64, *Reservation, error) {
	key := "installation:" + ep.Name
	ticket, err := s.breaker.Allow(key)
	if err != nil {
		return "", 0, nil, err
	}
	res, err := s.budget.Reserve(ctx, ReserveInput{
		ModelUUID: ep.LedgerModelUUID(), Provider: models.ProviderTEI, ModelID: ep.Model,
		CredentialKind: models.CredentialKindInstallation, Caller: InstallationCaller, Purpose: purpose,
		EstimatedInput: estimate, ReserveOutput: 1,
	})
	if err != nil {
		s.breaker.Release(key, ticket)
		outcome := models.OutcomeError
		if errors.Is(err, iface.ErrLLMBudgetExceeded) {
			outcome = models.OutcomeBudgetBlocked
		}
		s.metrics.RecordLLMRequest(models.ProviderTEI, outcome)
		return "", 0, nil, err
	}
	return key, ticket, res, nil
}

func (s *InstallationService) timeout() time.Duration {
	if d := s.cfg().RequestTimeout; d > 0 {
		return d
	}
	return defaultRequestTimeout
}

// settle runs steps 9-10: the breaker verdict and the ledger row.
func (s *InstallationService) settle(ctx context.Context, ep installation.Endpoint, purpose, key string, ticket uint64, res *Reservation, usage iface.LLMUsage, err error) {
	if ctx.Err() != nil || errors.Is(err, iface.ErrLLMProfileMismatch) {
		s.breaker.Release(key, ticket)
	} else {
		s.breaker.Record(key, ticket, err)
	}
	outcome, attemptOutcome := models.OutcomeOK, attemptOK
	if err != nil {
		outcome, attemptOutcome = models.OutcomeError, attemptError
	}
	finalizeCall(ctx, s.budget, s.metrics, s.logger, res,
		[]models.Attempt{{Provider: models.ProviderTEI, CredentialKind: models.CredentialKindInstallation, Outcome: attemptOutcome, InputTokens: max(usage.InputTokens, 0)}},
		FinalizeInput{Outcome: outcome, ErrorCode: errorCodeOf(err), Provider: models.ProviderTEI, ModelID: ep.Model,
			ModelUUID: ep.LedgerModelUUID(), CredentialKind: models.CredentialKindInstallation})
	s.metrics.RecordLLMRequest(models.ProviderTEI, outcome)
}

// EmbedWithProfile embeds req.Inputs on the endpoint serving req.Profile.
func (s *InstallationService) EmbedWithProfile(ctx context.Context, req iface.PinnedEmbedRequest) (*iface.EmbedResult, error) {
	if err := ValidatePinnedEmbedRequest(req); err != nil {
		return nil, err
	}
	ep, v, err := s.resolve(ctx, iface.InferenceKindEmbed, req.OnBehalfOf, req.Profile)
	if err != nil {
		return nil, err
	}
	prefix := ep.DocumentPrefix
	if req.Kind == iface.EmbedInputQuery {
		prefix = ep.QueryPrefix
	}
	inputs := make([]string, len(req.Inputs))
	for i, in := range req.Inputs {
		inputs[i] = prefix + in
	}
	key, ticket, res, err := s.admit(ctx, ep, PurposeRetrievalEmbed, EstimateEmbedTokens(inputs))
	if err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, s.timeout())
	var usage iface.LLMUsage
	var vectors [][]float32
	outcome, err := s.set.Adapter(ep.Name).Embed(callCtx, inputs,
		providers.TEIEmbedOptions{Normalize: ep.Normalize, Truncate: ep.Truncate, BatchSize: v.BatchSize})
	cancel()
	if outcome != nil {
		usage = outcome.Usage
		vectors = outcome.Vectors
	}
	if err == nil {
		err = checkDimensions(vectors, len(inputs), v.Profile.Dimensions)
	}
	if errors.Is(err, iface.ErrLLMProfileMismatch) {
		s.set.Invalidate(ep.Name)
	}
	s.settle(ctx, ep, PurposeRetrievalEmbed, key, ticket, res, usage, err)
	if err != nil {
		return nil, err
	}
	return &iface.EmbedResult{
		Vectors: vectors, Dimensions: v.Profile.Dimensions, Usage: usage,
		Model: iface.LLMModelInfo{
			UUID: ep.LedgerModelUUID(), Name: ep.Name, Provider: models.ProviderTEI, ModelID: ep.Model,
			Capabilities:   iface.LLMCapabilities{Embeddings: true, Dimensions: v.Profile.Dimensions},
			CredentialKind: models.CredentialKindInstallation, Purposes: []string{PurposeRetrievalEmbed},
		},
	}, nil
}

func checkDimensions(vectors [][]float32, want, dims int) error {
	if len(vectors) != want {
		return fmt.Errorf("%w: adapter returned a vector count that does not match the inputs", iface.ErrLLMProviderUnavailable)
	}
	for _, v := range vectors {
		if len(v) != dims {
			return fmt.Errorf("%w: dimensions", iface.ErrLLMProfileMismatch)
		}
	}
	return nil
}

// Rerank scores req.Documents against req.Query on the endpoint serving
// req.Profile and keeps the best TopN (0 = all).
func (s *InstallationService) Rerank(ctx context.Context, req iface.RerankRequest) (*iface.RerankResult, error) {
	if err := ValidateRerankRequest(req); err != nil {
		return nil, err
	}
	ep, v, err := s.resolve(ctx, iface.InferenceKindRerank, req.OnBehalfOf, req.Profile)
	if err != nil {
		return nil, err
	}
	estimate := len(req.Query) * len(req.Documents) // covers the query repeated per document
	for _, d := range req.Documents {
		estimate += len(d)
	}
	key, ticket, res, err := s.admit(ctx, ep, PurposeRetrievalRerank, (estimate+2)/3)
	if err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, s.timeout())
	scores, usage, err := s.set.Adapter(ep.Name).Rerank(callCtx, req.Query, req.Documents,
		providers.TEIRerankOptions{Truncate: ep.Truncate, BatchSize: v.BatchSize})
	cancel()
	s.settle(ctx, ep, PurposeRetrievalRerank, key, ticket, res, usage, err)
	if err != nil {
		return nil, err
	}
	if req.TopN > 0 && req.TopN < len(scores) {
		scores = scores[:req.TopN]
	}
	return &iface.RerankResult{Results: scores, Profile: req.Profile, Usage: usage}, nil
}

func (g *Gateway) installationService() (*InstallationService, error) {
	if g.installation == nil {
		return nil, fmt.Errorf("%w: installation inference not wired", iface.ErrLLMNotConfigured)
	}
	return g.installation, nil
}

// InferenceProfile implements iface.LLMInferenceProfiles.
func (g *Gateway) InferenceProfile(ctx context.Context, endpoint string) (*iface.InferenceProfile, error) {
	s, err := g.installationService()
	if err != nil {
		return nil, err
	}
	return s.InferenceProfile(ctx, endpoint)
}

// EmbedWithProfile implements iface.LLMPinnedEmbedder.
func (g *Gateway) EmbedWithProfile(ctx context.Context, req iface.PinnedEmbedRequest) (*iface.EmbedResult, error) {
	s, err := g.installationService()
	if err != nil {
		return nil, err
	}
	return s.EmbedWithProfile(ctx, req)
}

// Rerank implements iface.LLMReranker.
func (g *Gateway) Rerank(ctx context.Context, req iface.RerankRequest) (*iface.RerankResult, error) {
	s, err := g.installationService()
	if err != nil {
		return nil, err
	}
	return s.Rerank(ctx, req)
}
