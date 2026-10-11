package handlers

// The SPA-facing Sign in with ChatGPT return contract is CLOSED and lives
// only in this file (TestSIWC_CallbackRedirectBuilderIsTheOnlyOne):
//
//	{console}/user/security?tab=ai&linked=ok
//	{console}/user/security?tab=ai&error=<state_invalid|denied|token_exchange_failed|id_token_invalid|disabled|unsupported_deployment>
//
// Never a code, a state, a token, an email or a user id. Every redirect
// sets Referrer-Policy: no-referrer and Cache-Control: no-store, like the
// auth module's OAuth callback (auth/handlers/oauth_callback_redirect.go).

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

const siwcReturnPath = "/user/security"

// Redirect error codes of the closed contract above.
const (
	siwcRedirectStateInvalid          = "state_invalid"
	siwcRedirectDenied                = "denied"
	siwcRedirectExchangeFailed        = "token_exchange_failed"
	siwcRedirectIDTokenInvalid        = "id_token_invalid"
	siwcRedirectDisabled              = "disabled"
	siwcRedirectUnsupportedDeployment = "unsupported_deployment"
)

var siwcRedirectAllowlist = map[string]bool{
	siwcRedirectStateInvalid: true, siwcRedirectDenied: true, siwcRedirectExchangeFailed: true,
	siwcRedirectIDTokenInvalid: true, siwcRedirectDisabled: true, siwcRedirectUnsupportedDeployment: true,
}

// SIWCMetrics is the narrow metrics seam of the link flow (ruling 6.8);
// *metrics.Collector satisfies it.
type SIWCMetrics interface {
	RecordLLMSIWCLink(outcome string)
}

// SIWCHandler serves the Sign in with ChatGPT routes: the user's own
// registrations (list, link, paste completion, select, unlink), the
// public loopback callback, and the org operators' list and forced unlink
// (admin_accounts.go).
type SIWCHandler struct {
	accounts    *services.AccountsService
	cfg         func() services.SIWCConfig
	limiter     *services.RateLimiter
	metrics     SIWCMetrics
	frontendURL string
	logger      *slog.Logger
}

// NewSIWCHandler: cfg is the live SIWC config (the mode is re-read on every
// request), frontendURL the operator console origin the callback returns
// to. metrics may be nil.
func NewSIWCHandler(accounts *services.AccountsService, cfg func() services.SIWCConfig, limiter *services.RateLimiter, m SIWCMetrics, frontendURL string, logger *slog.Logger) *SIWCHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SIWCHandler{accounts: accounts, cfg: cfg, limiter: limiter, metrics: m,
		frontendURL: strings.TrimRight(strings.TrimSpace(frontendURL), "/"), logger: logger}
}

func (h *SIWCHandler) record(outcome string) {
	if h.metrics != nil {
		h.metrics.RecordLLMSIWCLink(outcome)
	}
}

// --- self-service --------------------------------------------------------------

type LLMAccountsListResponse struct {
	Body struct {
		Mode         string                      `json:"mode" enum:"disabled,loopback_local,hosted_paste"`
		Items        []models.LLMUserAccountView `json:"items"`
		SelectedUUID string                      `json:"selectedUuid,omitempty"`
	}
}

// List returns the caller's own registrations in the current org and the
// installation's mode, so the console knows which flow to offer.
func (h *SIWCHandler) List(ctx context.Context, _ *struct{}) (*LLMAccountsListResponse, error) {
	items, err := h.accounts.List(ctx)
	if err != nil {
		return nil, failure(ctx, h.logger, "list my linked accounts", err)
	}
	resp := &LLMAccountsListResponse{}
	resp.Body.Mode = h.cfg().Mode
	resp.Body.Items = make([]models.LLMUserAccountView, 0, len(items))
	for i := range items {
		resp.Body.Items = append(resp.Body.Items, items[i].SelfView())
		if items[i].Selected {
			resp.Body.SelectedUUID = items[i].UUID
		}
	}
	return resp, nil
}

type LLMAccountLinkRequest struct {
	Body struct {
		Label             string `json:"label,omitempty" maxLength:"100" doc:"Optional name for the registration"`
		ReauthAccountUUID string `json:"reauthAccountUuid,omitempty" format:"uuid" doc:"Sign in again to one of your registrations instead of linking a new one"`
	}
}

type LLMAccountLinkResponse struct {
	Body struct {
		AuthorizeURL string `json:"authorizeUrl" doc:"Open in the browser; it is single-use and expires in ten minutes"`
		Mode         string `json:"mode" enum:"loopback_local,hosted_paste"`
	}
}

// Link starts an authorization attempt (5 a user per 10 minutes).
func (h *SIWCHandler) Link(ctx context.Context, req *LLMAccountLinkRequest) (*LLMAccountLinkResponse, error) {
	res, err := h.accounts.Link(ctx, req.Body.Label, req.Body.ReauthAccountUUID)
	if err != nil {
		return nil, failure(ctx, h.logger, "start linking", err)
	}
	resp := &LLMAccountLinkResponse{}
	resp.Body.AuthorizeURL, resp.Body.Mode = res.AuthorizeURL, res.Mode
	return resp, nil
}

type LLMAccountCompleteRequest struct {
	Body struct {
		CallbackURL string `json:"callbackUrl" minLength:"1" maxLength:"4096" doc:"The http://127.0.0.1:<port>/v1/llm/accounts/openai/callback?… address the browser landed on"`
	}
}

type LLMAccountResponse struct {
	Body models.LLMUserAccountView
}

// Complete finishes an attempt from the URL the user pasted (10 a user per
// 10 minutes). The URL is parsed for its state only; whether it points at
// the attempt's redirect URI is decided against the attempt itself, after
// it is consumed (ruling 5.17), never against the live configuration.
func (h *SIWCHandler) Complete(ctx context.Context, req *LLMAccountCompleteRequest) (*LLMAccountResponse, error) {
	if !h.cfg().Enabled() {
		return nil, failure(ctx, h.logger, "complete linking", services.ErrSIWCDisabled)
	}
	userUUID, _ := ctxauth.GetUserUUID(ctx)
	if err := h.limiter.CheckSIWCComplete(ctx, userUUID); err != nil {
		return nil, failure(ctx, h.logger, "complete linking", err)
	}
	in, err := h.accounts.ParsePastedCallback(req.Body.CallbackURL)
	if err != nil {
		return nil, failure(ctx, h.logger, "complete linking", err)
	}
	acc, err := h.accounts.Complete(ctx, in, true)
	h.record(siwcOutcome(err, acc))
	if err != nil {
		return nil, failure(ctx, h.logger, "complete linking", err)
	}
	return &LLMAccountResponse{Body: acc.SelfView()}, nil
}

type LLMAccountByUUID struct {
	UUID string `path:"uuid" format:"uuid"`
}

// Select makes one of the caller's registrations the one plan-billed
// models use.
func (h *SIWCHandler) Select(ctx context.Context, req *LLMAccountByUUID) (*struct{}, error) {
	if err := h.accounts.Select(ctx, req.UUID); err != nil {
		return nil, failure(ctx, h.logger, "select linked account", err)
	}
	return nil, nil
}

// Unlink revokes (best-effort) and deletes one of the caller's
// registrations.
func (h *SIWCHandler) Unlink(ctx context.Context, req *LLMAccountByUUID) (*struct{}, error) {
	if err := h.accounts.Unlink(ctx, req.UUID, false); err != nil {
		return nil, failure(ctx, h.logger, "unlink account", err)
	}
	return nil, nil
}

// --- public loopback callback ----------------------------------------------------

// Callback is the raw HTTP handler mounted on the operator root router only
// in loopback_local (decided once at RegisterRoutes). It renders nothing:
// every outcome is the closed redirect above. The mode is still re-read
// live, so switching away from loopback_local takes effect at once.
func (h *SIWCHandler) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := h.cfg()
	if !cfg.CallbackRouteMounted() {
		h.redirect(w, r, siwcRedirectDisabled)
		return
	}
	// OpenAI redirects to http://127.0.0.1 only; localhost (or any other
	// name) is refused before anything is consumed.
	if host, _, err := net.SplitHostPort(r.Host); (err == nil && host != services.SIWCLoopbackHost) || (err != nil && r.Host != services.SIWCLoopbackHost) {
		h.redirect(w, r, siwcRedirectUnsupportedDeployment)
		return
	}
	// The per-IP limit runs before the state is taken (Task 5 carry); the
	// address is the one RealIP resolved, never ctxauth's (unset here).
	if err := h.limiter.CheckSIWCCallback(ctx, utils.GetClientIP(r)); err != nil {
		h.redirect(w, r, siwcRedirectStateInvalid)
		return
	}
	if err := services.ValidateSIWCConfig(cfg); err != nil {
		h.logger.WarnContext(ctx, "llm siwc: callback refused, the live configuration is invalid", slog.String("code", siwcRedirectCode(err)))
		h.redirect(w, r, siwcRedirectCode(err))
		return
	}
	q := r.URL.Query()
	in := services.CallbackInput{
		Code: q.Get("code"), State: q.Get("state"), ClientID: q.Get("client_id"), Error: q.Get("error"),
		// Where the callback actually arrived; Complete requires it to
		// equal the attempt's redirect URI (scheme, host, port, path).
		RedirectURI: requestScheme(r) + "://" + r.Host + r.URL.EscapedPath(),
	}
	acc, err := h.accounts.Complete(ctx, in, false)
	h.record(siwcOutcome(err, acc))
	if err != nil {
		if code := siwcRedirectCode(err); code == siwcRedirectExchangeFailed {
			// The cause is logged like failure() does for a 5xx; the
			// service errors never carry a token, a code or the state.
			h.logger.WarnContext(ctx, "llm siwc: callback completion failed", slog.String("code", errorCode(err)), slog.String("error", err.Error()))
		}
		h.redirect(w, r, siwcRedirectCode(err))
		return
	}
	h.redirect(w, r, "")
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func (h *SIWCHandler) redirect(w http.ResponseWriter, r *http.Request, code string) {
	q := url.Values{"tab": {"ai"}}
	if code == "" {
		q.Set("linked", "ok")
	} else {
		if !siwcRedirectAllowlist[code] {
			code = siwcRedirectExchangeFailed
		}
		q.Set("error", code)
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, h.frontendURL+siwcReturnPath+"?"+q.Encode(), http.StatusFound)
}

// siwcRedirectCode maps a completion failure onto the closed redirect set.
// A redirect mismatch is reported like a bad state: either way the attempt
// is gone and the user starts again.
func siwcRedirectCode(err error) string {
	switch {
	case errors.Is(err, services.ErrSIWCStateInvalid), errors.Is(err, services.ErrSIWCCallbackInvalid), errors.Is(err, services.ErrRateLimited):
		return siwcRedirectStateInvalid
	case errors.Is(err, services.ErrSIWCDenied):
		return siwcRedirectDenied
	case errors.Is(err, services.ErrSIWCIDTokenInvalid):
		return siwcRedirectIDTokenInvalid
	case errors.Is(err, services.ErrSIWCDisabled), errors.Is(err, services.ErrSIWCConfigInvalid):
		return siwcRedirectDisabled
	case errors.Is(err, services.ErrSIWCUnsupportedDeployment):
		return siwcRedirectUnsupportedDeployment
	}
	return siwcRedirectExchangeFailed
}

// siwcOutcome is the orkestra_llm_siwc_link_total label of a completion.
func siwcOutcome(err error, acc *models.UserAccount) string {
	switch {
	case err == nil && acc != nil && acc.Status == models.AccountStatusPlanDisabled:
		return "plan_disabled"
	case err == nil:
		return "linked"
	case errors.Is(err, services.ErrSIWCDenied):
		return "denied"
	case errors.Is(err, services.ErrSIWCStateInvalid), errors.Is(err, services.ErrSIWCCallbackInvalid):
		return "state_invalid"
	case errors.Is(err, services.ErrSIWCIDTokenInvalid):
		return "id_token_invalid"
	}
	return "exchange_failed"
}

// errorCode is the stable llm.* code MapError gives err (for logs).
func errorCode(err error) string {
	var e *errcode.Error
	if errors.As(MapError(err), &e) {
		return e.Code
	}
	return ""
}
