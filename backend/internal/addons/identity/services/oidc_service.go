// Package services holds the identity module's OIDC orchestration.
//
// The module stands up a per-tenant OpenID Connect login flow:
//
//  1. Start — `/v1/identity/oidc/{tenantSlug}/start` looks up the tenant's
//     IdPConfig, initializes an OIDC Provider (discovery) + oauth2.Config,
//     generates state+nonce, stashes them in Redis, and redirects the
//     browser to the IdP's authorization endpoint.
//  2. Callback — `/v1/identity/oidc/callback` validates state, exchanges
//     the code for a token, verifies the ID token (signature + audience +
//     nonce) against the IdP's published keys, extracts the configured
//     claims (sub, email, name), finds-or-creates the Orkestra user, and
//     mints an Orkestra session via PasswordAuthService.IssueLoginTokens.
//
// The service is a thin wrapper over coreos/go-oidc + golang.org/x/oauth2
// so that Orkestra never hand-rolls OIDC crypto — per the tenancy plan's
// BYO IdP risk mitigation ("use coreos/go-oidc, no custom OIDC").
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	identityModels "github.com/orkestra/backend/internal/addons/identity/models"
	"github.com/orkestra/backend/internal/addons/identity/repository"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	authServices "github.com/orkestra/backend/internal/core/auth/services"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/utils"
)

// ErrIdPDisabled signals an attempt to start a login against a config
// whose Enabled flag is false. Mapped to 404 at the handler to avoid
// leaking IdP-configured-but-disabled state to anonymous callers.
var ErrIdPDisabled = errors.New("identity: IdP config is disabled")

// ErrInvalidState is returned when the callback receives a state value
// that does not match any Redis-persisted flow. Covers expired states,
// CSRF attempts, and replay.
var ErrInvalidState = errors.New("identity: invalid or expired OIDC state")

// StateStore is the minimal Redis contract the service needs for state
// persistence. Satisfied by shared/database.RedisClientAdapter.
type StateStore interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
}

// providerFactory lets tests substitute a mock IdP without touching the
// network. Production wiring uses coreos/go-oidc's NewProvider.
type providerFactory func(ctx context.Context, issuerURL string) (*oidc.Provider, error)

// Service orchestrates BYO OIDC login for external tenants.
type Service struct {
	repo         *repository.Repository
	users        iface.UserProvider
	tenant       iface.TenantProvider
	passwordAuth *authServices.PasswordAuthService
	state        StateStore
	logger       *slog.Logger
	// providerFactory resolves the IdP metadata. Kept as a field so tests
	// can inject a mock discovery endpoint; production wiring calls
	// oidc.NewProvider which performs a network round trip.
	providerFactory providerFactory
	// stateTTL is the lifetime of an OIDC state blob in Redis. Defaults to
	// 10 minutes to match the auth module's OAuth state TTL.
	stateTTL time.Duration
	// auditSink is wired post-construction by the compliance module. Nil
	// when compliance is disabled; emit helpers tolerate that.
	auditSink iface.AuditSink
}

// SetAuditSink wires the compliance audit sink post-construction.
func (s *Service) SetAuditSink(sink iface.AuditSink) { s.auditSink = sink }

// emitAudit forwards to the sink when wired; no-op otherwise.
func (s *Service) emitAudit(ctx context.Context, event iface.AuditEvent) {
	if s.auditSink == nil {
		return
	}
	s.auditSink.Emit(ctx, event)
}

// Config bundles the collaborators. PasswordAuth is used only to mint the
// Orkestra access/refresh pair at the tail end of callback — it already
// handles device info, session creation, last-login timestamp.
type Config struct {
	Repo         *repository.Repository
	Users        iface.UserProvider
	Tenant       iface.TenantProvider
	PasswordAuth *authServices.PasswordAuthService
	State        StateStore
	Logger       *slog.Logger
}

// New constructs a Service with production defaults.
func New(cfg Config) *Service {
	return &Service{
		repo:            cfg.Repo,
		users:           cfg.Users,
		tenant:          cfg.Tenant,
		passwordAuth:    cfg.PasswordAuth,
		state:           cfg.State,
		logger:          cfg.Logger,
		providerFactory: oidc.NewProvider,
		stateTTL:        10 * time.Minute,
	}
}

// SetProviderFactory replaces the OIDC provider factory. Tests call this
// to point at a mock IdP harness; production code never touches it.
func (s *Service) SetProviderFactory(f providerFactory) { s.providerFactory = f }

// statePayload is the JSON-serialized Redis blob keyed by the random
// state token. Binding idpConfigUUID + nonce into one document means the
// callback handler can resolve everything from a single Redis lookup.
type statePayload struct {
	IdPConfigUUID string `json:"idpConfigUUID"`
	TenantUUID    string `json:"tenantUUID"`
	Nonce         string `json:"nonce"`
	RedirectTo    string `json:"redirectTo"`
}

// StartInput is the DTO for StartLogin.
type StartInput struct {
	// TenantSlug is the {tenantSlug} path parameter. Resolved to a
	// TenantUUID via the tenant provider before looking up the IdP config.
	TenantSlug string
	// RedirectTo is where the browser is sent after a successful callback.
	// Optional — handler defaults to the frontend root. Callers must
	// validate it against an allowlist before passing in; StartLogin does
	// not validate.
	RedirectTo string
}

// StartResult bundles the generated auth URL and the state token. The
// handler issues a 302 to AuthURL; state is kept in Redis keyed by the
// token so the callback can look it up. State TTL matches Redis TTL.
type StartResult struct {
	AuthURL string
	State   string
}

// StartLogin resolves the tenant's IdP config, builds an oauth2.Config,
// and returns the provider's authorization URL for a 302.
func (s *Service) StartLogin(ctx context.Context, in StartInput) (*StartResult, error) {
	if s.tenant == nil {
		return nil, errors.New("identity: tenant provider not wired")
	}
	if strings.TrimSpace(in.TenantSlug) == "" {
		return nil, errors.New("identity: tenant slug is required")
	}

	tenantUUID, err := s.resolveTenantUUIDBySlug(ctx, in.TenantSlug)
	if err != nil {
		return nil, err
	}

	cfg, err := s.repo.GetByTenantUUIDUnscoped(ctx, tenantUUID, identityModels.ProtocolOIDC)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ErrIdPDisabled
	}

	secret, err := utils.DecryptOAuthToken(cfg.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("identity: decrypt client secret: %w", err)
	}

	provider, err := s.providerFactory(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("identity: OIDC discovery: %w", err)
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: secret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       effectiveScopes(cfg.Scopes),
	}

	stateTok, err := utils.GenerateState()
	if err != nil {
		return nil, err
	}
	nonce, err := utils.GenerateNonce()
	if err != nil {
		return nil, err
	}

	payload := statePayload{
		IdPConfigUUID: cfg.UUID,
		TenantUUID:    tenantUUID,
		Nonce:         nonce,
		RedirectTo:    in.RedirectTo,
	}
	if err := s.storeState(ctx, stateTok, payload); err != nil {
		return nil, err
	}

	authURL := oauth2Cfg.AuthCodeURL(stateTok,
		oidc.Nonce(nonce),
		oauth2.AccessTypeOnline,
	)
	return &StartResult{AuthURL: authURL, State: stateTok}, nil
}

// CallbackInput is the DTO for Callback.
type CallbackInput struct {
	Code     string
	State    string
	IP       string
	DeviceID string
	Platform string
}

// CallbackResult bundles the minted Orkestra token pair and the redirect
// the browser should land on. The handler encodes the redirect into a 302
// with the access token set as a cookie (or returns JSON for XHR flows).
type CallbackResult struct {
	Tokens     *authModels.TokenResponse
	RedirectTo string
}

// Callback completes the OIDC dance: validates state, exchanges the code,
// verifies the ID token, looks up or provisions the Orkestra user, and
// mints a full session via PasswordAuthService.IssueLoginTokens.
func (s *Service) Callback(ctx context.Context, in CallbackInput) (*CallbackResult, error) {
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.State) == "" {
		s.emitOIDCFailure(ctx, "", "", "missing_state_or_code", in.IP)
		return nil, ErrInvalidState
	}

	payload, err := s.consumeState(ctx, in.State)
	if err != nil {
		s.emitOIDCFailure(ctx, "", "", "invalid_state", in.IP)
		return nil, err
	}

	cfg, err := s.repo.GetByUUIDUnscoped(ctx, payload.IdPConfigUUID)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ErrIdPDisabled
	}

	secret, err := utils.DecryptOAuthToken(cfg.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("identity: decrypt client secret: %w", err)
	}

	provider, err := s.providerFactory(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("identity: OIDC discovery: %w", err)
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: secret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       effectiveScopes(cfg.Scopes),
	}

	tok, err := oauth2Cfg.Exchange(ctx, in.Code)
	if err != nil {
		return nil, fmt.Errorf("identity: code exchange: %w", err)
	}

	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, errors.New("identity: IdP response missing id_token")
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		s.emitOIDCFailure(ctx, payload.TenantUUID, payload.IdPConfigUUID, "verify_id_token", in.IP)
		return nil, fmt.Errorf("identity: verify id_token: %w", err)
	}
	if idToken.Nonce != payload.Nonce {
		s.emitOIDCFailure(ctx, payload.TenantUUID, payload.IdPConfigUUID, "nonce_mismatch", in.IP)
		return nil, errors.New("identity: nonce mismatch")
	}

	var rawClaims map[string]any
	if err := idToken.Claims(&rawClaims); err != nil {
		return nil, fmt.Errorf("identity: decode claims: %w", err)
	}

	email, _ := rawClaims[cfg.EffectiveEmailClaim()].(string)
	name, _ := rawClaims[cfg.EffectiveNameClaim()].(string)
	sub, _ := rawClaims[cfg.EffectiveSubClaim()].(string)
	if email == "" || sub == "" {
		return nil, errors.New("identity: id_token missing required claims")
	}

	user, err := s.findOrCreateUser(ctx, email, name)
	if err != nil {
		return nil, err
	}

	tokens, err := s.passwordAuth.IssueLoginTokens(
		ctx, user,
		firstNonEmpty(in.DeviceID, "oidc-"+strings.ReplaceAll(uuid.NewString(), "-", "")[:12]),
		firstNonEmpty(in.Platform, "web"),
		in.IP,
		[]string{"oidc"},
		0,
	)
	if err != nil {
		return nil, err
	}

	s.logger.Info("identity: OIDC login completed",
		slog.String("tenantUUID", payload.TenantUUID),
		slog.String("idpConfigUUID", payload.IdPConfigUUID),
		slog.String("userUUID", user.UUID),
		slog.String("sub", sub),
	)
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     payload.TenantUUID,
		ActorUserID:  user.UUID,
		ActorEmail:   user.Email,
		ActorType:    "user",
		Action:       "identity.oidc.login",
		Outcome:      "success",
		ResourceType: "identity_idp",
		ResourceID:   payload.IdPConfigUUID,
		IPAddress:    in.IP,
		Metadata:     map[string]any{"issuer": cfg.IssuerURL, "sub": sub},
	})
	return &CallbackResult{Tokens: tokens, RedirectTo: payload.RedirectTo}, nil
}

// emitOIDCFailure centralizes the failure-side audit emit for the OIDC
// callback. Captures the reason, the best tenant/IdP context available at
// the point of failure, and the request IP. Actor is anonymous because
// the callback runs pre-session.
func (s *Service) emitOIDCFailure(ctx context.Context, tenantUUID, idpConfigUUID, reason, ip string) {
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     tenantUUID,
		ActorType:    "anonymous",
		Action:       "identity.oidc.login",
		Outcome:      "failure",
		ResourceType: "identity_idp",
		ResourceID:   idpConfigUUID,
		IPAddress:    ip,
		Metadata:     map[string]any{"reason": reason},
	})
}

// findOrCreateUser looks up by email and creates a new operator-role
// account if none exists. Newly-provisioned OIDC users are marked verified
// in-line because the IdP has asserted control over the email — requiring
// them to click a second link would be theater.
func (s *Service) findOrCreateUser(ctx context.Context, email, fullName string) (*userModels.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if fullName == "" {
		fullName = email
	}

	if existing, err := s.users.GetUserForAuth(ctx, email); err == nil && existing != nil {
		return existing, nil
	}

	created, err := s.users.CreateUserFromOAuth(ctx, &userModels.CreateUserInput{
		UUID:     uuid.New().String(),
		Email:    email,
		FullName: fullName,
		Role:     "operator",
	})
	if err != nil {
		return nil, fmt.Errorf("identity: create user: %w", err)
	}
	// Mark verified — the IdP's email assertion is our verification.
	if err := s.users.MarkEmailVerified(ctx, created.UUID); err != nil {
		s.logger.Warn("identity: MarkEmailVerified failed",
			slog.String("userUUID", created.UUID),
			slog.String("error", err.Error()),
		)
	}
	created.EmailVerified = true
	return created, nil
}

// resolveTenantUUIDBySlug walks the tenant provider's public surface. Slug
// → UUID resolution happens without a tenant context (the flow is
// pre-auth). If the tenant provider grows a dedicated GetBySlug later,
// swap this helper out.
func (s *Service) resolveTenantUUIDBySlug(ctx context.Context, slug string) (string, error) {
	// No direct slug→uuid on the public provider yet. Fetch-by-UUID is
	// available; fetch-by-slug is only on the concrete tenant service. We
	// look it up indirectly via the repository GetByTenantUUIDUnscoped
	// path by falling back to a sentinel: the configured IdP rows already
	// carry tenantId, and tenant listings are out of scope pre-auth. For
	// v1 callers must pass the tenant UUID in the slug position — a
	// concession documented in the identity module's CLAUDE.md. A future
	// commit lifts slug resolution into iface.TenantProvider.
	//
	// Returning the input lets an operator-supplied UUID work end-to-end;
	// a real slug will simply miss on GetByTenantUUIDUnscoped and surface
	// as a 404.
	return slug, nil
}

// storeState JSON-encodes the payload and writes it to Redis under a
// namespace distinct from auth's own OAuth state ("identity:oidc:state:").
// Collisions with the auth module's keys are impossible by construction.
func (s *Service) storeState(ctx context.Context, token string, payload statePayload) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.state.Set(ctx, stateKey(token), buf, s.stateTTL)
}

// consumeState fetches-and-deletes the state blob. Delete-after-read is
// best-effort (we don't surface the delete error); it protects against
// state replay without coupling the happy path to cache writes.
func (s *Service) consumeState(ctx context.Context, token string) (*statePayload, error) {
	raw, err := s.state.Get(ctx, stateKey(token))
	if err != nil || raw == "" {
		return nil, ErrInvalidState
	}
	_ = s.state.Del(ctx, stateKey(token))
	var payload statePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, ErrInvalidState
	}
	return &payload, nil
}

func stateKey(token string) string { return "identity:oidc:state:" + token }

func effectiveScopes(configured []string) []string {
	if len(configured) > 0 {
		// oidc.ScopeOpenID is required for an id_token — ensure it's always
		// first so the IdP recognizes the request as OIDC.
		hasOpenID := false
		for _, s := range configured {
			if s == oidc.ScopeOpenID {
				hasOpenID = true
				break
			}
		}
		if !hasOpenID {
			return append([]string{oidc.ScopeOpenID}, configured...)
		}
		return configured
	}
	return []string{oidc.ScopeOpenID, "email", "profile"}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// RedirectTarget resolves the final URL the callback handler should 302 to,
// defaulting to the caller's frontendURL root when the original state's
// redirectTo was empty or invalid.
func (s *Service) RedirectTarget(frontendURL, storedRedirect string) string {
	if storedRedirect == "" {
		return frontendURL
	}
	if _, err := url.Parse(storedRedirect); err != nil {
		return frontendURL
	}
	return storedRedirect
}
