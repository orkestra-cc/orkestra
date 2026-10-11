package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// Envelope field names of the three sealed tokens (part of the AAD).
const (
	accountFieldIDToken      = "idToken"
	accountFieldAccessToken  = "accessToken"
	accountFieldRefreshToken = "refreshToken"
)

const (
	// accountRefreshSkew: a token closer than this to its expiry is refreshed
	// before use.
	accountRefreshSkew = 60 * time.Second
	// accountDefaultExpiresIn is used when the token response does not state
	// expires_in (the documented access-token lifetime is one hour).
	accountDefaultExpiresIn = 3600 * time.Second
	// accountRefreshTimeout bounds one refresh flight. The flight runs on a
	// context detached from the first caller's cancellation, so the callers
	// joined to it are not failed by one that hung up.
	accountRefreshTimeout = 30 * time.Second
	// accountRefreshLeaseTTL: the cross-replica refresh lease (Redis). It
	// outlives one token request (15 s) with margin and frees itself if the
	// holder dies.
	accountRefreshLeaseTTL    = 30 * time.Second
	accountRefreshLeasePrefix = "llm:siwc:refresh:"
	// accountRefreshWait: how long a replica that lost the lease polls the
	// stored set for the winner's rotation before answering unavailable.
	accountRefreshWait = 10 * time.Second
	// accountEarliestRefreshWait: an earliest_refresh_at this close is waited
	// for; a later one answers unavailable.
	accountEarliestRefreshWait = 5 * time.Second
	// accountPlanCacheTTL: the per-account plan catalog cache (spec: 5 min).
	accountPlanCacheTTL = 5 * time.Minute
	// accountInvalidClientBackoff: after the token endpoint answered
	// invalid_client for a registration's set, refreshes of that same set
	// answer unavailable from memory for this long instead of presenting
	// the same failing client again (final review, Minor 2). A reauth or a
	// rotation stores a new set (another savedAt) and is never held back.
	accountInvalidClientBackoff = time.Minute
	// accountCompleteTimeout bounds the code exchange, the ID token check
	// and the store of a completion. They run on a context detached from
	// the request: once the code is spent at OpenAI, a browser that hangs
	// up must not leave an issued refresh token neither stored nor revoked.
	accountCompleteTimeout = 30 * time.Second

	accountLabelMaxLen = 100
	accountStoreRetry  = 3
)

// AccountRepo is what AccountsService needs from repository.UserAccounts.
// The token set is written only through ReplaceTokens (compare-and-swap on
// savedAt) and status / last use only through their targeted writers
// (ruling 2.14): the whole-document Update is never used on these paths.
type AccountRepo interface {
	Insert(ctx context.Context, a *models.UserAccount) error
	ListByUser(ctx context.Context, userUUID string) ([]models.UserAccount, error)
	ListAdmin(ctx context.Context) ([]models.UserAccount, error)
	Get(ctx context.Context, uuid string) (*models.UserAccount, error)
	GetSelected(ctx context.Context, userUUID, provider string) (*models.UserAccount, error)
	Select(ctx context.Context, userUUID, provider, uuid string) error
	Delete(ctx context.Context, uuid string) error
	SetLastUsed(ctx context.Context, uuid string, at time.Time) error
	SetStatus(ctx context.Context, uuid, status, lastError string) error
	SetStatusIf(ctx context.Context, uuid string, prevSavedAt time.Time, status, lastError string) error
	ReplaceTokens(ctx context.Context, uuid string, prevSavedAt time.Time, set models.AccountTokenSet) error
}

var _ AccountRepo = (*repository.UserAccounts)(nil)

// HostIDSource yields the installation's stable ext_agent_host_id.
type HostIDSource interface {
	GetOrCreateHostID(ctx context.Context) (string, error)
}

var _ HostIDSource = (*repository.Installation)(nil)

// OIDC is the slice of *OIDCClient the accounts service calls. The plan
// catalog is not here: it is read through the OpenAI adapter (PlanCatalog,
// ruling 2.10 — one parser).
type OIDC interface {
	AuthorizeURL(p AuthorizeParams) string
	Exchange(ctx context.Context, clientID, code, codeVerifier, redirectURI string) (*TokenResponse, error)
	Refresh(ctx context.Context, clientID, refreshToken string) (*TokenResponse, error)
	Revoke(ctx context.Context, clientID, refreshToken string) error
	VerifyIDToken(ctx context.Context, raw, expectedAud, expectedNonce string) (*IDClaims, error)
}

var _ OIDC = (*OIDCClient)(nil)

// PlanCatalog lists the model slugs a ChatGPT access token may use.
type PlanCatalog interface {
	ListPlanModels(ctx context.Context, accessToken string) ([]string, error)
}

// RegistryPlanCatalog reads the plan catalog through the registry's openai
// adapter in its SIWC profile (credential kind user_account), so the
// {models:[{slug,visibility}]} parser exists once, in providers.
type RegistryPlanCatalog struct{ Registry *providers.Registry }

func (c RegistryPlanCatalog) ListPlanModels(ctx context.Context, accessToken string) ([]string, error) {
	if c.Registry == nil {
		return nil, fmt.Errorf("%w: no provider registry", iface.ErrLLMProviderUnavailable)
	}
	p, err := c.Registry.Build(models.ProviderOpenAI, providers.Credential{Kind: iface.LLMCredentialKindUserAccount, Secret: accessToken}, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build openai adapter: %w", iface.ErrLLMProviderUnavailable, err)
	}
	descs, err := p.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(descs))
	for _, d := range descs {
		out = append(out, d.ID)
	}
	return out, nil
}

// AccountCredential is a selected, usable registration with its decrypted
// access token. The token must not outlive the provider call or reach a log.
type AccountCredential struct {
	Account     *models.UserAccount
	AccessToken string
}

// AccountCredentialSource is the narrow surface the user-account credential
// resolver (Task 7) consumes (ruling 2.13).
type AccountCredentialSource interface {
	CredentialFor(ctx context.Context, userUUID string) (*AccountCredential, error)
	// ForceRefresh rotates the set cred was built from (an inference 401,
	// ruling 5.2); a set already rotated since is returned as is.
	ForceRefresh(ctx context.Context, cred *AccountCredential) (*AccountCredential, error)
	PlanModels(ctx context.Context, cred *AccountCredential) ([]string, error)
	MarkUsed(ctx context.Context, accountUUID string)
}

var _ AccountCredentialSource = (*AccountsService)(nil)

// AccountsDeps are the AccountsService collaborators. Redis (optional) holds
// the cross-replica refresh lease; without it the in-process single-flight
// is the only guard (one replica).
type AccountsDeps struct {
	Repo    AccountRepo
	Host    HostIDSource
	OIDC    OIDC
	Plans   PlanCatalog
	State   *SIWCStateStore
	Vault   *Vault
	Limiter *RateLimiter
	Redis   module.RedisClient
	Config  func() SIWCConfig
	Logger  *slog.Logger
}

// AccountsService owns the Sign in with ChatGPT registrations: link,
// callback/paste completion, selection, unlink and the single-flight token
// refresh. Nothing it logs, returns or audits carries a token, a code, a
// verifier or the reauth authorize URL (which embeds the ID token).
type AccountsService struct {
	repo    AccountRepo
	host    HostIDSource
	oidc    OIDC
	plans   PlanCatalog
	state   *SIWCStateStore
	vault   *Vault
	limiter *RateLimiter
	redis   module.RedisClient
	cfg     func() SIWCConfig
	logger  *slog.Logger
	now     func() time.Time
	// pollEvery is the lease waiter's re-read interval (tests shorten it).
	pollEvery time.Duration

	flights singleflight.Group

	planMu    sync.Mutex
	planCache map[string]planEntry

	// invalidClient: account uuid → the set that met invalid_client, until
	// when (see accountInvalidClientBackoff).
	invalidMu     sync.Mutex
	invalidClient map[string]invalidClientEntry

	auditMu sync.RWMutex
	audit   iface.AuditSink
}

type planEntry struct {
	slugs []string
	at    time.Time
}

type invalidClientEntry struct {
	savedAt time.Time
	until   time.Time
}

func NewAccountsService(d AccountsDeps) *AccountsService {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	cfg := d.Config
	if cfg == nil {
		cfg = func() SIWCConfig { return SIWCConfig{Mode: SIWCModeDisabled} }
	}
	return &AccountsService{
		repo: d.Repo, host: d.Host, oidc: d.OIDC, plans: d.Plans, state: d.State, vault: d.Vault,
		limiter: d.Limiter, redis: d.Redis, cfg: cfg, logger: logger, now: time.Now,
		pollEvery: 200 * time.Millisecond, planCache: map[string]planEntry{}, invalidClient: map[string]invalidClientEntry{},
	}
}

// SetAuditSink satisfies iface.AuditSinkSetter; nil-safe.
func (s *AccountsService) SetAuditSink(sink iface.AuditSink) {
	if sink == nil {
		return
	}
	s.auditMu.Lock()
	s.audit = sink
	s.auditMu.Unlock()
}

// emit records a successful account action. actor overrides the context's
// user (the public loopback callback carries no session: the actor is the
// user the attempt was bound to). Metadata never carries a token or a URL.
func (s *AccountsService) emit(ctx context.Context, actor, action, resourceID string, meta map[string]any) {
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
	if actor == "" {
		actor, _ = ctxauth.GetUserUUID(ctx)
	}
	email, _ := ctxauth.GetUserEmail(ctx)
	ip, _ := ctxauth.GetClientIP(ctx)
	sink.Emit(ctx, iface.AuditEvent{
		TenantID: tenantID, TenantKind: kind, ActorUserID: actor, ActorEmail: email, ActorType: "user",
		Action: action, ResourceType: "llm_user_account", ResourceID: resourceID, Outcome: "success", IPAddress: ip, Metadata: meta,
	})
}

// clock is now in UTC truncated to Mongo's millisecond precision, so a
// savedAt read back compares equal to the one written (the refresh CAS).
func (s *AccountsService) clock() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

func actorOf(ctx context.Context) (tenantID, userUUID string) {
	tenantID, _ = tenantrepo.CurrentTenantID(ctx)
	userUUID, _ = ctxauth.GetUserUUID(ctx)
	return tenantID, userUUID
}

// --- link ------------------------------------------------------------------

// LinkResult carries the authorize URL. On a reauth it embeds the stored ID
// token (id_token_hint): it goes to the caller's browser only, never to a
// log or an audit event.
type LinkResult struct{ AuthorizeURL, Mode string }

// Link starts an authorization attempt for the calling user: a new
// registration, or, with reauthAccountUUID, a reauth of one of their own.
func (s *AccountsService) Link(ctx context.Context, label, reauthAccountUUID string) (*LinkResult, error) {
	cfg := s.cfg()
	if !cfg.Enabled() {
		return nil, ErrSIWCDisabled
	}
	// The live config is not re-validated anywhere else (an env-only
	// LLM_SIWC_MODE bypasses the snapshot validator): refuse it here.
	if err := ValidateSIWCConfig(cfg); err != nil {
		return nil, err
	}
	redirect, err := cfg.RedirectURI()
	if err != nil {
		return nil, err
	}
	if !loopbackRedirect(redirect) {
		return nil, ErrSIWCUnsupportedDeployment
	}
	tenantID, userUUID := actorOf(ctx)
	if tenantID == "" || userUUID == "" {
		return nil, ErrNotFound
	}
	label, err = cleanLabel(label)
	if err != nil {
		return nil, err
	}
	if err := s.limiter.CheckSIWCLink(ctx, userUUID); err != nil {
		return nil, err
	}
	hostID, err := s.host.GetOrCreateHostID(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: installation host id: %w", iface.ErrLLMProviderUnavailable, err)
	}
	state, err := utils.GenerateState()
	if err != nil {
		return nil, fmt.Errorf("llm siwc: generate state: %w", err)
	}
	nonce, err := utils.GenerateNonce()
	if err != nil {
		return nil, fmt.Errorf("llm siwc: generate nonce: %w", err)
	}
	verifier, err := utils.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("llm siwc: generate code verifier: %w", err)
	}
	attempt := SIWCAttempt{
		UserUUID: userUUID, TenantID: tenantID, Label: label, HostID: hostID, RedirectURI: redirect,
		Verifier: verifier, Nonce: nonce, Mode: cfg.Mode, ClientID: SIWCDynamicClientID, CreatedAt: s.now().UTC(),
	}
	params := AuthorizeParams{
		ClientID: SIWCDynamicClientID, RedirectURI: redirect, State: state, Nonce: nonce,
		CodeChallenge: utils.GenerateCodeChallenge(verifier), HostID: hostID, AgentNameHint: SIWCAgentNameHint,
	}
	reauth := reauthAccountUUID != ""
	if reauth {
		acc, err := s.repo.Get(ctx, reauthAccountUUID)
		if err != nil {
			return nil, mapRepoErr(err)
		}
		if acc.UserUUID != userUUID {
			return nil, ErrNotFound
		}
		params.ClientID, params.AgentNameHint, params.LoginHint = acc.ClientID, "", acc.Email
		if idt, err := s.vault.Open(ctx, acc.TenantID, acc.UUID, accountFieldIDToken, acc.IDToken); err == nil {
			params.IDTokenHint = idt
		} else if errors.Is(err, ErrSecretKeyMissing) {
			return nil, err
		} else {
			// A hint that cannot be opened must not block the very flow
			// that repairs the registration: continue without it.
			s.logger.WarnContext(ctx, "llm siwc: stored id token unusable as reauth hint", slog.String("account", acc.UUID))
		}
		attempt.ClientID, attempt.AccountUUID, attempt.Label = acc.ClientID, acc.UUID, acc.Label
	}
	if err := s.state.Put(ctx, state, attempt); err != nil {
		return nil, err
	}
	s.emit(ctx, "", "llm.account.link_started", reauthAccountUUID, map[string]any{"mode": cfg.Mode, "reauth": reauth})
	return &LinkResult{AuthorizeURL: s.oidc.AuthorizeURL(params), Mode: cfg.Mode}, nil
}

// loopbackRedirect: OpenAI registers http://127.0.0.1 redirects only, and
// localhost is refused outright.
func loopbackRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "http" && u.Hostname() == SIWCLoopbackHost && u.User == nil
}

func cleanLabel(label string) (string, error) {
	label = strings.TrimSpace(label)
	// Characters, not bytes: the API schema's maxLength counts runes too.
	if utf8.RuneCountInString(label) > accountLabelMaxLen || strings.ContainsFunc(label, unicode.IsControl) {
		return "", fmt.Errorf("%w: label must be at most %d printable characters", iface.ErrLLMInvalidRequest, accountLabelMaxLen)
	}
	return label, nil
}

// --- completion --------------------------------------------------------------

// CallbackInput is what came back on the redirect. RedirectURI is the
// scheme://host:port/path the callback actually arrived on (ParsePastedCallback
// fills it; the loopback handler sets it from the request). It is
// mandatory: Complete requires it to equal the attempt's.
type CallbackInput struct {
	Code, State, ClientID, Error string
	RedirectURI                  string
}

const pastedCallbackMaxLen = 4096

// ParsePastedCallback reads the URL the user pasted from the browser's
// address bar: an absolute URL without userinfo carrying a state and a code
// or an error; the fragment is ignored. It does not judge where the URL
// points: RedirectURI is filled from it (scheme://host:port/path) and
// Complete compares that, byte for byte, with the redirect URI of the
// attempt the state names, after consuming it (ruling 5.17). A localhost,
// another port or another path therefore fails there, never against the
// live configuration, which may have changed since the link started.
func (s *AccountsService) ParsePastedCallback(raw string) (CallbackInput, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > pastedCallbackMaxLen {
		return CallbackInput{}, ErrSIWCCallbackInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Opaque != "" {
		return CallbackInput{}, ErrSIWCCallbackInvalid
	}
	u.Fragment, u.RawFragment = "", ""
	q := u.Query()
	in := CallbackInput{
		Code: q.Get("code"), State: q.Get("state"), ClientID: q.Get("client_id"), Error: q.Get("error"),
		RedirectURI: u.Scheme + "://" + u.Host + u.EscapedPath(),
	}
	if in.State == "" || (in.Code == "" && in.Error == "") {
		return CallbackInput{}, ErrSIWCCallbackInvalid
	}
	return in, nil
}

// Complete consumes the attempt named by in.State and, on success, stores
// the registration. viaPaste: ctx is the authenticated session, which must
// be the attempt's user and org (otherwise ErrSIWCStateInvalid, and the
// attempt stays consumed). Otherwise ctx is the public loopback callback:
// user and org come from the attempt.
func (s *AccountsService) Complete(ctx context.Context, in CallbackInput, viaPaste bool) (*models.UserAccount, error) {
	attempt, err := s.state.Take(ctx, in.State)
	if err != nil {
		return nil, err
	}
	// The attempt is bound to this installation's host id: a state minted
	// by another installation (or one whose host id cannot be read now)
	// is not honored.
	if hostID, err := s.host.GetOrCreateHostID(ctx); err != nil || hostID == "" || hostID != attempt.HostID {
		return nil, ErrSIWCStateInvalid
	}
	if viaPaste {
		tenantID, userUUID := actorOf(ctx)
		if attempt.UserUUID != userUUID || attempt.TenantID != tenantID {
			return nil, ErrSIWCStateInvalid
		}
	} else {
		ctx = ctxWithTenant(ctx, attempt.TenantID)
	}
	if !s.cfg().Enabled() {
		return nil, ErrSIWCDisabled
	}
	// Mandatory (fail closed): the caller states where the callback arrived.
	if in.RedirectURI == "" || in.RedirectURI != attempt.RedirectURI {
		return nil, ErrSIWCCallbackInvalid
	}
	if in.Error != "" {
		if in.Error == "access_denied" {
			s.emit(ctx, attempt.UserUUID, "llm.account.link_denied", attempt.AccountUUID, map[string]any{"mode": attempt.Mode, "reauth": attempt.AccountUUID != ""})
			return nil, ErrSIWCDenied
		}
		return nil, fmt.Errorf("%w: %w: authorization failed", ErrSIWCExchangeFailed, iface.ErrLLMProviderUnavailable)
	}
	if in.Code == "" {
		return nil, ErrSIWCCallbackInvalid
	}
	clientID := attempt.ClientID
	if clientID == SIWCDynamicClientID {
		if in.ClientID == "" {
			return nil, fmt.Errorf("%w: %w: issued client id missing", ErrSIWCExchangeFailed, iface.ErrLLMProviderUnavailable)
		}
		clientID = in.ClientID
	} else if in.ClientID != "" && in.ClientID != clientID {
		// A reauth that came back under another registration: drop it
		// before any exchange, the stored registration stays untouched.
		return nil, fmt.Errorf("%w: client id mismatch", ErrSIWCIDTokenInvalid)
	}
	if !loopbackRedirect(attempt.RedirectURI) {
		return nil, ErrSIWCUnsupportedDeployment
	}
	// From here the authorization code is spent at OpenAI: the exchange,
	// the ID token check and the store (or the revocation of what cannot
	// be stored) run to the end even if the browser hangs up.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accountCompleteTimeout)
	defer cancel()
	tok, err := s.oidc.Exchange(ctx, clientID, in.Code, attempt.Verifier, attempt.RedirectURI)
	if err != nil {
		return nil, err
	}
	if tok.RefreshToken == "" {
		return nil, fmt.Errorf("%w: %w: no refresh token granted", ErrSIWCExchangeFailed, iface.ErrLLMProviderUnavailable)
	}
	claims, err := s.oidc.VerifyIDToken(ctx, tok.IDToken, clientID, attempt.Nonce)
	if err != nil {
		s.revokeQuiet(ctx, clientID, tok.RefreshToken)
		return nil, err
	}
	var acc *models.UserAccount
	if attempt.AccountUUID != "" {
		acc, err = s.completeReauth(ctx, attempt, clientID, tok, claims)
	} else {
		acc, err = s.completeNew(ctx, attempt, clientID, tok, claims)
	}
	if err != nil {
		return nil, err
	}
	s.dropPlanCache(acc.UUID)
	s.emit(ctx, attempt.UserUUID, "llm.account.linked", acc.UUID, map[string]any{
		"mode": attempt.Mode, "reauth": attempt.AccountUUID != "", "planUsageGranted": acc.PlanUsageGranted,
	})
	return acc, nil
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

func planStatus(plan bool) string {
	if plan {
		return models.AccountStatusActive
	}
	return models.AccountStatusPlanDisabled
}

func expiresIn(tok *TokenResponse) time.Duration {
	if tok.ExpiresIn <= 0 {
		return accountDefaultExpiresIn
	}
	return time.Duration(tok.ExpiresIn) * time.Second
}

// savedAfter keeps savedAt strictly increasing per registration: it is the
// compare-and-swap fence and the "rotated since" marker, so two writes in
// the same millisecond must still differ.
func savedAfter(now, prev time.Time) time.Time {
	if !now.After(prev) {
		return prev.Add(time.Millisecond)
	}
	return now
}

func truncMillis(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC().Truncate(time.Millisecond)
	return &v
}

// sealNewSet seals a freshly issued token set (exchange) under the target
// registration's uuid: the AAD binds the envelope to that uuid.
func (s *AccountsService) sealNewSet(ctx context.Context, tenantID, accountUUID string, tok *TokenResponse, email string) (models.AccountTokenSet, error) {
	now := s.clock()
	scopes := strings.Fields(tok.Scope)
	plan := hasScope(scopes, models.ScopePlanUsage)
	set := models.AccountTokenSet{
		TokenType: tok.TokenType, ExpiresAt: now.Add(expiresIn(tok)), RefreshExpiresAt: now.Add(models.RefreshTokenLifetime),
		EarliestRefreshAt: truncMillis(tok.EarliestRefreshAt), SavedAt: now, Scopes: scopes,
		PlanUsageGranted: plan, Status: planStatus(plan), Email: email,
	}
	var err error
	if set.AccessToken, err = s.vault.Seal(ctx, tenantID, accountUUID, accountFieldAccessToken, tok.AccessToken); err != nil {
		return set, err
	}
	if set.RefreshToken, err = s.vault.Seal(ctx, tenantID, accountUUID, accountFieldRefreshToken, tok.RefreshToken); err != nil {
		return set, err
	}
	idt, err := s.vault.Seal(ctx, tenantID, accountUUID, accountFieldIDToken, tok.IDToken)
	if err != nil {
		return set, err
	}
	set.IDToken = &idt
	return set, nil
}

func applySet(a *models.UserAccount, set models.AccountTokenSet) {
	if set.IDToken != nil {
		a.IDToken = *set.IDToken
	}
	if set.Email != "" {
		a.Email = set.Email
	}
	a.AccessToken, a.RefreshToken, a.TokenType = set.AccessToken, set.RefreshToken, set.TokenType
	a.ExpiresAt, a.RefreshExpiresAt, a.EarliestRefreshAt, a.SavedAt = set.ExpiresAt, set.RefreshExpiresAt, set.EarliestRefreshAt, set.SavedAt
	a.Scopes, a.PlanUsageGranted, a.Status, a.LastError = set.Scopes, set.PlanUsageGranted, set.Status, ""
}

func (s *AccountsService) completeReauth(ctx context.Context, attempt *SIWCAttempt, clientID string, tok *TokenResponse, claims *IDClaims) (*models.UserAccount, error) {
	acc, err := s.repo.Get(ctx, attempt.AccountUUID)
	if err != nil {
		s.revokeQuiet(ctx, clientID, tok.RefreshToken)
		return nil, mapRepoErr(err)
	}
	if acc.UserUUID != attempt.UserUUID || acc.ClientID != clientID {
		s.revokeQuiet(ctx, clientID, tok.RefreshToken)
		return nil, fmt.Errorf("%w: registration mismatch", ErrSIWCIDTokenInvalid)
	}
	if acc.Subject != claims.Subject {
		// Another ChatGPT identity: the fresh set is dropped (and revoked,
		// 3.T6.6); the stored registration is not touched.
		s.revokeQuiet(ctx, clientID, tok.RefreshToken)
		return nil, fmt.Errorf("%w: subject mismatch", ErrSIWCIDTokenInvalid)
	}
	return s.storeOver(ctx, acc, clientID, tok, claims)
}

// storeOver writes a freshly issued set over an existing registration,
// re-reading on a lost compare-and-swap (a refresh may land in between: the
// new authorization supersedes it either way).
func (s *AccountsService) storeOver(ctx context.Context, acc *models.UserAccount, clientID string, tok *TokenResponse, claims *IDClaims) (*models.UserAccount, error) {
	set, err := s.sealNewSet(ctx, acc.TenantID, acc.UUID, tok, claims.Email)
	if err != nil {
		s.revokeQuiet(ctx, clientID, tok.RefreshToken)
		return nil, err
	}
	for i := 0; ; i++ {
		set.SavedAt = savedAfter(set.SavedAt, acc.SavedAt)
		err = s.repo.ReplaceTokens(ctx, acc.UUID, acc.SavedAt, set)
		if err == nil {
			break
		}
		if !errors.Is(err, repository.ErrStaleTokenSet) || i+1 >= accountStoreRetry {
			s.revokeQuiet(ctx, clientID, tok.RefreshToken)
			return nil, mapRepoErr(err)
		}
		if acc, err = s.repo.Get(ctx, acc.UUID); err != nil {
			s.revokeQuiet(ctx, clientID, tok.RefreshToken)
			return nil, mapRepoErr(err)
		}
	}
	applySet(acc, set)
	return acc, nil
}

func (s *AccountsService) completeNew(ctx context.Context, attempt *SIWCAttempt, clientID string, tok *TokenResponse, claims *IDClaims) (*models.UserAccount, error) {
	existing, err := s.repo.ListByUser(ctx, attempt.UserUUID)
	if err != nil {
		s.revokeQuiet(ctx, clientID, tok.RefreshToken)
		return nil, mapRepoErr(err)
	}
	for i := range existing {
		if existing[i].Provider == models.ProviderOpenAI && existing[i].ClientID == clientID {
			return s.relink(ctx, &existing[i], clientID, tok, claims)
		}
	}
	// Selected unless the user already has a selected registration for the
	// provider (unlinking the selected one and linking again selects).
	selected := true
	for _, r := range existing {
		if r.Provider == models.ProviderOpenAI && r.Selected {
			selected = false
		}
	}
	label := attempt.Label
	if label == "" {
		label = claims.Email
	}
	if label == "" {
		label = "ChatGPT"
	}
	acc := &models.UserAccount{
		UUID: uuid.NewString(), TenantID: attempt.TenantID, UserUUID: attempt.UserUUID, Provider: models.ProviderOpenAI,
		Label: label, Issuer: claims.Issuer, Subject: claims.Subject, ClientID: clientID, ExtAgentHostID: attempt.HostID,
		Selected: selected,
	}
	set, err := s.sealNewSet(ctx, attempt.TenantID, acc.UUID, tok, claims.Email)
	if err != nil {
		s.revokeQuiet(ctx, clientID, tok.RefreshToken)
		return nil, err
	}
	applySet(acc, set)
	acc.LinkedAt, acc.UpdatedAt = set.SavedAt, set.SavedAt
	err = s.repo.Insert(ctx, acc)
	if errors.Is(err, repository.ErrDuplicateName) {
		// Ruling 2.4: either the same issued client id was linked
		// concurrently (re-read by client id and update that row) or two
		// first links raced on the selected index (insert unselected).
		rows, lerr := s.repo.ListByUser(ctx, attempt.UserUUID)
		if lerr != nil {
			// Which index refused the insert is unknown: retrying unselected
			// could only duplicate again and read as a name clash.
			s.revokeQuiet(ctx, clientID, tok.RefreshToken)
			return nil, fmt.Errorf("%w: %w: re-read linked accounts", ErrSIWCExchangeFailed, iface.ErrLLMProviderUnavailable)
		}
		for i := range rows {
			if rows[i].Provider == models.ProviderOpenAI && rows[i].ClientID == clientID {
				return s.relink(ctx, &rows[i], clientID, tok, claims)
			}
		}
		acc.Selected = false
		if err = s.repo.Insert(ctx, acc); errors.Is(err, repository.ErrDuplicateName) {
			// The same client id landed between the re-read and this insert:
			// a store race, not a label the user chose.
			s.revokeQuiet(ctx, clientID, tok.RefreshToken)
			return nil, fmt.Errorf("%w: %w: concurrent link", ErrSIWCExchangeFailed, iface.ErrLLMProviderUnavailable)
		}
	}
	if err != nil {
		s.revokeQuiet(ctx, clientID, tok.RefreshToken)
		return nil, mapRepoErr(err)
	}
	return acc, nil
}

// relink: the same issued client id came back on a first-link flow. It is
// the same registration, so its token set is replaced, provided it is the
// same ChatGPT identity (an equal email never merges two registrations).
func (s *AccountsService) relink(ctx context.Context, acc *models.UserAccount, clientID string, tok *TokenResponse, claims *IDClaims) (*models.UserAccount, error) {
	if acc.Subject != claims.Subject {
		s.revokeQuiet(ctx, clientID, tok.RefreshToken)
		return nil, fmt.Errorf("%w: subject mismatch", ErrSIWCIDTokenInvalid)
	}
	return s.storeOver(ctx, acc, clientID, tok, claims)
}

// revokeQuiet revokes a refresh token best-effort; failures are logged
// without any token or provider text.
func (s *AccountsService) revokeQuiet(ctx context.Context, clientID, refreshToken string) {
	if refreshToken == "" || clientID == "" {
		return
	}
	if err := s.oidc.Revoke(ctx, clientID, refreshToken); err != nil {
		s.logger.WarnContext(ctx, "llm siwc: refresh token revocation failed", slog.String("reason", revokeReason(err)))
	}
}

func revokeReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case errors.Is(err, iface.ErrLLMProviderUnavailable):
		return "provider_unavailable"
	}
	return "error"
}

// --- self-service and admin --------------------------------------------------

// List returns the caller's own registrations in the current org.
func (s *AccountsService) List(ctx context.Context) ([]models.UserAccount, error) {
	_, userUUID := actorOf(ctx)
	if userUUID == "" {
		return nil, ErrNotFound
	}
	out, err := s.repo.ListByUser(ctx, userUUID)
	return out, mapRepoErr(err)
}

// ListAdmin returns every registration of the current org.
func (s *AccountsService) ListAdmin(ctx context.Context) ([]models.UserAccount, error) {
	out, err := s.repo.ListAdmin(ctx)
	return out, mapRepoErr(err)
}

// ownAccount reads uuid and requires the caller to own it; another user's
// registration is ErrNotFound (no existence oracle).
func (s *AccountsService) ownAccount(ctx context.Context, id string) (*models.UserAccount, error) {
	_, userUUID := actorOf(ctx)
	acc, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if userUUID == "" || acc.UserUUID != userUUID {
		return nil, ErrNotFound
	}
	return acc, nil
}

// Select makes one of the caller's registrations the selected one.
func (s *AccountsService) Select(ctx context.Context, id string) error {
	acc, err := s.ownAccount(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Select(ctx, acc.UserUUID, acc.Provider, acc.UUID); err != nil {
		return mapRepoErr(err)
	}
	s.dropPlanCache(acc.UUID)
	s.emit(ctx, "", "llm.account.selected", acc.UUID, map[string]any{"provider": acc.Provider})
	return nil
}

// Unlink revokes the registration's refresh token best-effort, then deletes
// it locally even if the revocation failed. force (admin) reaches any user's
// registration in the org; otherwise only the caller's own.
func (s *AccountsService) Unlink(ctx context.Context, id string, force bool) error {
	var (
		acc *models.UserAccount
		err error
	)
	if force {
		if acc, err = s.repo.Get(ctx, id); err != nil {
			return mapRepoErr(err)
		}
	} else if acc, err = s.ownAccount(ctx, id); err != nil {
		return err
	}
	s.RevokeRegistration(ctx, *acc)
	if err := s.repo.Delete(ctx, acc.UUID); err != nil {
		return mapRepoErr(err)
	}
	s.dropPlanCache(acc.UUID)
	s.emit(ctx, "", "llm.account.unlinked", acc.UUID, map[string]any{"force": force, "userUuid": acc.UserUUID})
	return nil
}

// RevokeRegistration revokes acc's refresh token best-effort under acc's own
// org (the DSR purge has no tenant in its context: ruling 2.15). It emits no
// audit event and deletes nothing.
func (s *AccountsService) RevokeRegistration(ctx context.Context, acc models.UserAccount) {
	ctx = ctxWithTenant(ctx, acc.TenantID)
	if acc.RefreshToken.IsZero() {
		return
	}
	rt, err := s.vault.Open(ctx, acc.TenantID, acc.UUID, accountFieldRefreshToken, acc.RefreshToken)
	if err != nil {
		s.logger.WarnContext(ctx, "llm siwc: refresh token cannot be opened for revocation", slog.String("account", acc.UUID))
		return
	}
	s.revokeQuiet(ctx, acc.ClientID, rt)
}

// MarkUsed stamps lastUsedAt after a successful plan-billed call
// (best-effort, a targeted write).
func (s *AccountsService) MarkUsed(ctx context.Context, accountUUID string) {
	if err := s.repo.SetLastUsed(ctx, accountUUID, s.clock()); err != nil {
		s.logger.WarnContext(ctx, "llm siwc: last-use stamp failed", slog.String("account", accountUUID))
	}
}

// MarkReauth flags the registration needs_reauth with an llm.* code
// (best-effort, a targeted write). Anything that is not an llm.* identifier
// is replaced by llm.user_account_reauth_required.
func (s *AccountsService) MarkReauth(ctx context.Context, accountUUID, code string) {
	s.markReauth(ctx, accountUUID, code)
}

func (s *AccountsService) markReauth(ctx context.Context, accountUUID, code string) {
	if !llmCode(code) {
		code = errcode.LLMUserAccountReauthRequired
	}
	if err := s.repo.SetStatus(ctx, accountUUID, models.AccountStatusNeedsReauth, code); err != nil {
		s.logger.WarnContext(ctx, "llm siwc: reauth flag failed", slog.String("account", accountUUID))
	}
	s.dropPlanCache(accountUUID)
}

func llmCode(code string) bool {
	rest, ok := strings.CutPrefix(code, "llm.")
	if !ok || rest == "" || len(code) > 64 {
		return false
	}
	for _, r := range rest {
		if !(r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// --- credentials for the call path --------------------------------------------

// usable gates a registration for inference.
func usable(acc *models.UserAccount) error {
	switch acc.Status {
	case models.AccountStatusActive:
	case models.AccountStatusPlanDisabled:
		return iface.ErrLLMPlanUsageDisabled
	default: // needs_reauth, revoked, unknown: fail closed
		return iface.ErrLLMUserAccountReauth
	}
	if !acc.PlanUsageGranted {
		return iface.ErrLLMPlanUsageDisabled
	}
	return nil
}

// CredentialFor returns the user's selected, active, plan-granted
// registration with a usable access token, refreshing it first when it
// expires within a minute (single-flight per account).
func (s *AccountsService) CredentialFor(ctx context.Context, userUUID string) (*AccountCredential, error) {
	if userUUID == "" {
		return nil, iface.ErrLLMUserAccountRequired
	}
	acc, err := s.repo.GetSelected(ctx, userUUID, models.ProviderOpenAI)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, iface.ErrLLMUserAccountRequired
	case err != nil:
		return nil, fmt.Errorf("%w: read linked account: %w", iface.ErrLLMProviderUnavailable, err)
	}
	if err := usable(acc); err != nil {
		return nil, err
	}
	if s.now().Before(acc.ExpiresAt.Add(-accountRefreshSkew)) {
		return s.credential(ctx, acc)
	}
	return s.refresh(ctx, acc.UUID, nil)
}

// ForceRefresh rotates the token set cred was built from, e.g. after the
// provider answered 401 with it. If the stored set has already moved on (a
// concurrent refresh) that one is returned without another rotation.
func (s *AccountsService) ForceRefresh(ctx context.Context, cred *AccountCredential) (*AccountCredential, error) {
	if cred == nil || cred.Account == nil {
		return nil, iface.ErrLLMUserAccountRequired
	}
	stale := cred.Account.SavedAt
	return s.refresh(ctx, cred.Account.UUID, &stale)
}

func (s *AccountsService) credential(ctx context.Context, acc *models.UserAccount) (*AccountCredential, error) {
	at, err := s.vault.Open(ctx, acc.TenantID, acc.UUID, accountFieldAccessToken, acc.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("%w: open linked account token: %w", iface.ErrLLMProviderUnavailable, err)
	}
	return &AccountCredential{Account: acc, AccessToken: at}, nil
}

// fresh reports whether the stored set can be used without a refresh: on a
// forced refresh, when it was rotated since the stale set and is unexpired;
// otherwise when it is outside the refresh skew.
func (s *AccountsService) fresh(acc *models.UserAccount, stale *time.Time) bool {
	now := s.now()
	if stale != nil {
		return !acc.SavedAt.Equal(*stale) && now.Before(acc.ExpiresAt)
	}
	return now.Before(acc.ExpiresAt.Add(-accountRefreshSkew))
}

// refresh is single-flight per (org, account) inside the process and
// guarded across replicas by a Redis lease (ruling 3.T6.3): a rotating
// refresh token used twice is refresh_token_reused, which would cost the
// user a reauth. Every flight first re-reads the stored set, so a caller
// arriving after another flight finished uses its rotation.
func (s *AccountsService) refresh(ctx context.Context, accountUUID string, stale *time.Time) (*AccountCredential, error) {
	tenantID, _ := tenantrepo.CurrentTenantID(ctx)
	key := tenantID + "|" + accountUUID
	v, err, _ := s.flights.Do(key, func() (any, error) {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accountRefreshTimeout)
		defer cancel()
		return s.refreshFlight(fctx, accountUUID, stale)
	})
	if err != nil {
		return nil, err
	}
	shared := v.(*AccountCredential)
	acc := *shared.Account
	return &AccountCredential{Account: &acc, AccessToken: shared.AccessToken}, nil
}

func (s *AccountsService) readUsable(ctx context.Context, accountUUID string) (*models.UserAccount, error) {
	acc, err := s.repo.Get(ctx, accountUUID)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, iface.ErrLLMUserAccountRequired // unlinked meanwhile
	case err != nil:
		return nil, fmt.Errorf("%w: read linked account: %w", iface.ErrLLMProviderUnavailable, err)
	}
	if err := usable(acc); err != nil {
		return nil, err
	}
	return acc, nil
}

func (s *AccountsService) refreshFlight(ctx context.Context, accountUUID string, stale *time.Time) (*AccountCredential, error) {
	acc, err := s.readUsable(ctx, accountUUID)
	if err != nil {
		return nil, err
	}
	if s.fresh(acc, stale) {
		return s.credential(ctx, acc)
	}
	held, release := s.acquireLease(ctx, accountUUID)
	if !held {
		return s.awaitRotation(ctx, accountUUID, acc.SavedAt)
	}
	defer release()
	// Another replica may have finished between the read and the lease.
	if acc, err = s.readUsable(ctx, accountUUID); err != nil {
		return nil, err
	}
	if s.fresh(acc, stale) {
		return s.credential(ctx, acc)
	}
	if acc.EarliestRefreshAt != nil {
		if wait := acc.EarliestRefreshAt.Sub(s.now()); wait > 0 {
			if s.now().Before(acc.ExpiresAt) && stale == nil {
				return s.credential(ctx, acc) // still valid, not yet refreshable
			}
			if wait > accountEarliestRefreshWait {
				return nil, fmt.Errorf("%w: linked account not yet refreshable", iface.ErrLLMProviderUnavailable)
			}
			if err := sleepCtx(ctx, wait); err != nil {
				return nil, err
			}
		}
	}
	if s.invalidClientHeld(acc) {
		return nil, fmt.Errorf("%w: %w: client configuration rejected recently", ErrSIWCInvalidClient, iface.ErrLLMProviderUnavailable)
	}
	rt, err := s.vault.Open(ctx, acc.TenantID, acc.UUID, accountFieldRefreshToken, acc.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrEnvelopeCorrupt) {
			return s.terminal(ctx, acc)
		}
		return nil, fmt.Errorf("%w: open linked account token: %w", iface.ErrLLMProviderUnavailable, err)
	}
	tok, err := s.oidc.Refresh(ctx, acc.ClientID, rt)
	if err != nil {
		switch {
		case errors.Is(err, ErrSIWCRefreshTerminal):
			// Terminal for the set we presented: the user repeats OAuth with
			// the saved client id, unless a newer set was stored meanwhile.
			// ErrSIWCRefreshTerminal itself never leaves the module.
			return s.terminal(ctx, acc)
		case errors.Is(err, ErrSIWCInvalidClient):
			// The guide files invalid_client under "fix configuration, not
			// tokens": a reauth with the same issued client id would fail
			// the same way, so credentials and status are kept and the call
			// is unavailable (logged for the operator). The same set is not
			// presented again for accountInvalidClientBackoff, and the
			// router treats it as final (no same-kind fallback).
			s.holdInvalidClient(acc)
			s.logger.WarnContext(ctx, "llm siwc: token endpoint rejected the client configuration", slog.String("account", acc.UUID))
		}
		return nil, err // transient: credentials kept
	}
	set, err := s.sealRefreshed(ctx, acc, tok)
	if err != nil {
		return nil, fmt.Errorf("%w: seal refreshed tokens: %w", iface.ErrLLMProviderUnavailable, err)
	}
	if err := s.repo.ReplaceTokens(ctx, acc.UUID, acc.SavedAt, set); err != nil {
		if errors.Is(err, repository.ErrStaleTokenSet) {
			// Someone else stored a rotation first: use theirs, and revoke
			// the set we drop.
			s.revokeDropped(ctx, accountUUID, acc.ClientID, tok.RefreshToken)
			if cred, ok := s.serveNewer(ctx, accountUUID, acc.SavedAt); ok {
				return cred, nil
			}
		} else if errors.Is(err, repository.ErrNotFound) {
			// The row is gone: an unlink or an erasure raced this refresh and
			// revoked only the token it read. The rotated one must not
			// outlive it. (Any other store failure leaves the row, and an
			// unacknowledged write may even have landed: revoking there could
			// kill the stored set, so it is left alone.)
			s.revokeRotated(ctx, acc.ClientID, rt, tok.RefreshToken)
		}
		return nil, fmt.Errorf("%w: store refreshed tokens: %w", iface.ErrLLMProviderUnavailable, mapRepoErr(err))
	}
	s.dropPlanCache(acc.UUID)
	applySet(acc, set)
	if err := usable(acc); err != nil {
		return nil, err
	}
	return &AccountCredential{Account: acc, AccessToken: tok.AccessToken}, nil
}

// revokeRotated revokes a freshly issued refresh token whose registration
// was deleted meanwhile. A token the IdP echoed instead of rotating is the
// one the unlink or erasure already revoked.
func (s *AccountsService) revokeRotated(ctx context.Context, clientID, presented, issued string) {
	if issued == "" || issued == presented {
		return
	}
	s.revokeQuiet(ctx, clientID, issued)
}

// invalidClientHeld reports whether acc's current set met invalid_client
// less than accountInvalidClientBackoff ago.
func (s *AccountsService) invalidClientHeld(acc *models.UserAccount) bool {
	s.invalidMu.Lock()
	defer s.invalidMu.Unlock()
	e, ok := s.invalidClient[acc.UUID]
	if !ok {
		return false
	}
	if !e.savedAt.Equal(acc.SavedAt) || !s.now().Before(e.until) {
		delete(s.invalidClient, acc.UUID)
		return false
	}
	return true
}

func (s *AccountsService) holdInvalidClient(acc *models.UserAccount) {
	now := s.now()
	s.invalidMu.Lock()
	defer s.invalidMu.Unlock()
	for id, e := range s.invalidClient { // bounded by the failing registrations
		if !now.Before(e.until) {
			delete(s.invalidClient, id)
		}
	}
	s.invalidClient[acc.UUID] = invalidClientEntry{savedAt: acc.SavedAt, until: now.Add(accountInvalidClientBackoff)}
}

// revokeDropped revokes the refresh token of a rotation that lost the
// compare-and-swap, but only when it is provably not the one the winning
// stored set uses: an omitted token, an IdP that echoes the same token
// instead of rotating it, or a stored envelope that cannot be opened all
// leave it alone.
func (s *AccountsService) revokeDropped(ctx context.Context, accountUUID, clientID, dropped string) {
	if dropped == "" {
		return
	}
	cur, err := s.repo.Get(ctx, accountUUID)
	if err != nil {
		return
	}
	stored, err := s.vault.Open(ctx, cur.TenantID, cur.UUID, accountFieldRefreshToken, cur.RefreshToken)
	if err != nil || stored == dropped {
		return
	}
	s.revokeQuiet(ctx, clientID, dropped)
}

// serveNewer returns the stored set when it is usable, unexpired and newer
// than prev (a rotation stored by another writer).
func (s *AccountsService) serveNewer(ctx context.Context, accountUUID string, prev time.Time) (*AccountCredential, bool) {
	cur, err := s.readUsable(ctx, accountUUID)
	if err != nil || cur.SavedAt.Equal(prev) || !s.now().Before(cur.ExpiresAt) {
		return nil, false
	}
	cred, err := s.credential(ctx, cur)
	return cred, err == nil
}

// terminal handles a refresh token that is unusable for the set acc holds.
// A stale presenter (lease fail-open, a refresh racing a reauth) gets
// refresh_token_reused while the row already holds a newer valid set: that
// set is served and nothing is flagged. Otherwise the flag is written fenced
// on the savedAt of the set that failed, so it can never land on a newer one.
func (s *AccountsService) terminal(ctx context.Context, acc *models.UserAccount) (*AccountCredential, error) {
	if cred, ok := s.serveNewer(ctx, acc.UUID, acc.SavedAt); ok {
		return cred, nil
	}
	err := s.repo.SetStatusIf(ctx, acc.UUID, acc.SavedAt, models.AccountStatusNeedsReauth, errcode.LLMUserAccountReauthRequired)
	switch {
	case errors.Is(err, repository.ErrStaleTokenSet):
		if cred, ok := s.serveNewer(ctx, acc.UUID, acc.SavedAt); ok {
			return cred, nil
		}
		if _, rerr := s.readUsable(ctx, acc.UUID); rerr != nil {
			return nil, rerr
		}
		return nil, fmt.Errorf("%w: linked account changed during refresh", iface.ErrLLMProviderUnavailable)
	case err != nil:
		s.logger.WarnContext(ctx, "llm siwc: reauth flag failed", slog.String("account", acc.UUID))
	default:
		s.logger.WarnContext(ctx, "llm siwc: refresh refused, account needs reauth", slog.String("account", acc.UUID))
	}
	s.dropPlanCache(acc.UUID)
	return nil, iface.ErrLLMUserAccountReauth
}

// sealRefreshed builds the rotated set: an omitted refresh_token keeps the
// stored one (and its expiry), an omitted scope keeps the stored scopes, an
// omitted id_token keeps the stored one. A refreshed ID token is stored as
// the next reauth hint only; it carries no nonce and is not re-verified.
func (s *AccountsService) sealRefreshed(ctx context.Context, acc *models.UserAccount, tok *TokenResponse) (models.AccountTokenSet, error) {
	now := s.clock()
	scopes := acc.Scopes
	if tok.Scope != "" {
		scopes = strings.Fields(tok.Scope)
	}
	plan := hasScope(scopes, models.ScopePlanUsage)
	tokenType := tok.TokenType
	if tokenType == "" {
		tokenType = acc.TokenType
	}
	set := models.AccountTokenSet{
		RefreshToken: acc.RefreshToken, TokenType: tokenType, ExpiresAt: now.Add(expiresIn(tok)),
		RefreshExpiresAt: acc.RefreshExpiresAt, EarliestRefreshAt: truncMillis(tok.EarliestRefreshAt), SavedAt: savedAfter(now, acc.SavedAt),
		Scopes: scopes, PlanUsageGranted: plan, Status: planStatus(plan),
	}
	var err error
	if set.AccessToken, err = s.vault.Seal(ctx, acc.TenantID, acc.UUID, accountFieldAccessToken, tok.AccessToken); err != nil {
		return set, err
	}
	if tok.RefreshToken != "" {
		if set.RefreshToken, err = s.vault.Seal(ctx, acc.TenantID, acc.UUID, accountFieldRefreshToken, tok.RefreshToken); err != nil {
			return set, err
		}
		set.RefreshExpiresAt = now.Add(models.RefreshTokenLifetime)
	}
	if tok.IDToken != "" {
		idt, err := s.vault.Seal(ctx, acc.TenantID, acc.UUID, accountFieldIDToken, tok.IDToken)
		if err != nil {
			return set, err
		}
		set.IDToken = &idt
	}
	return set, nil
}

// acquireLease takes the cross-replica refresh lease. Without Redis, or
// when Redis fails, the refresh proceeds under the in-process single-flight
// only (logged): blocking every plan call on a Redis outage would be worse
// than the rare duplicate refresh it prevents.
func (s *AccountsService) acquireLease(ctx context.Context, accountUUID string) (bool, func()) {
	if s.redis == nil {
		return true, func() {}
	}
	key := accountRefreshLeasePrefix + accountUUID
	n, err := incrCounter(ctx, s.redis, key, accountRefreshLeaseTTL)
	if err != nil {
		s.logger.WarnContext(ctx, "llm siwc: refresh lease store unavailable; refreshing without it", slog.String("account", accountUUID))
		return true, func() {}
	}
	if n != 1 {
		return false, nil
	}
	return true, func() { _ = s.redis.Del(context.WithoutCancel(ctx), key) }
}

// awaitRotation: another replica holds the lease; re-read the stored set
// until it changes (or the account leaves the usable states).
func (s *AccountsService) awaitRotation(ctx context.Context, accountUUID string, prev time.Time) (*AccountCredential, error) {
	deadline := s.now().Add(accountRefreshWait)
	for {
		if err := sleepCtx(ctx, s.pollEvery); err != nil {
			return nil, err
		}
		acc, err := s.readUsable(ctx, accountUUID)
		if err != nil {
			return nil, err
		}
		if !acc.SavedAt.Equal(prev) && s.now().Before(acc.ExpiresAt) {
			return s.credential(ctx, acc)
		}
		if !s.now().Before(deadline) {
			return nil, fmt.Errorf("%w: linked account refresh in progress elsewhere", iface.ErrLLMProviderUnavailable)
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// --- plan catalog ------------------------------------------------------------

// PlanModels lists the slugs the credential's ChatGPT account may use,
// cached five minutes per account (never shared between accounts) and
// dropped on select, refresh, reauth and unlink. A 401 (invalid user) gets
// one forced refresh and one retry.
func (s *AccountsService) PlanModels(ctx context.Context, cred *AccountCredential) ([]string, error) {
	if cred == nil || cred.Account == nil {
		return nil, iface.ErrLLMUserAccountRequired
	}
	id := cred.Account.UUID
	if slugs, ok := s.cachedPlan(id); ok {
		return slugs, nil
	}
	if s.plans == nil {
		return nil, fmt.Errorf("%w: no plan catalog", iface.ErrLLMProviderUnavailable)
	}
	slugs, err := s.plans.ListPlanModels(ctx, cred.AccessToken)
	if errors.Is(err, ErrSIWCInvalidUser) {
		next, rerr := s.ForceRefresh(ctx, cred)
		if rerr != nil {
			return nil, rerr
		}
		slugs, err = s.plans.ListPlanModels(ctx, next.AccessToken)
	}
	if err != nil {
		return nil, err
	}
	s.planMu.Lock()
	s.planCache[id] = planEntry{slugs: append([]string(nil), slugs...), at: s.now()}
	s.planMu.Unlock()
	return slugs, nil
}

func (s *AccountsService) cachedPlan(id string) ([]string, bool) {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	e, ok := s.planCache[id]
	if !ok || s.now().Sub(e.at) >= accountPlanCacheTTL {
		delete(s.planCache, id)
		return nil, false
	}
	return append([]string(nil), e.slugs...), true
}

func (s *AccountsService) dropPlanCache(id string) {
	s.planMu.Lock()
	delete(s.planCache, id)
	s.planMu.Unlock()
}
