package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// LLMMetrics is the slice of pkg/sdk/metrics.Collector the router uses.
// Labels are closed sets (provider kind, outcome, direction).
type LLMMetrics interface {
	RecordLLMRequest(provider, outcome string)
	RecordLLMTokens(provider, direction string, n int64)
}

// RouterConfig is read on every call (hot-reload).
type RouterConfig struct {
	RequestTimeout time.Duration // per-attempt deadline (request_timeout)
}

const (
	defaultRequestTimeout = 60 * time.Second
	// finalizeTimeout bounds the ledger write after the provider call; it
	// runs detached from the caller's context so a hang-up never leaves a
	// row for the reconciler to charge in full.
	finalizeTimeout = 5 * time.Second
	// minAttemptWindow: no new attempt starts with less time than this
	// left before the reservation expires.
	minAttemptWindow = time.Second
	// minStreamIdle is the floor of the per-call stream idle timeout
	// (max(minStreamIdle, request_timeout), see streamIdle).
	minStreamIdle = 30 * time.Second
)

// Attempt outcomes on a ledger row.
const (
	attemptOK                 = "ok"
	attemptError              = "error"
	attemptCircuitOpen        = "circuit_open"
	attemptCapabilityMismatch = "capability_mismatch"
)

// Router executes a call over the ordered candidates the AccessResolver
// chose: one budget reservation, then for each same-kind candidate the
// credential, the breaker, the provider call under a deadline, and an
// explicit fallback only before any output. Every logical call ends with
// exactly one ledger finalization and one requests metric.
type Router struct {
	registry *providers.Registry
	creds    CredentialResolver
	budget   *BudgetService
	breaker  *Breaker
	clients  func(kind string) *http.Client
	cfg      func() RouterConfig
	metrics  LLMMetrics
	logger   *slog.Logger

	// AfterCall (optional) runs after every attempt that reached a provider,
	// with the credential identity the attempt used and its error (nil on
	// success). The module stamps a linked account's lastUsedAt from it
	// (ruling 2.17: success only). It is not called for attempts that never
	// reached a provider (credential, breaker, adapter or capability
	// refusals). Set once, before the router serves calls.
	AfterCall func(ctx context.Context, m models.Model, credentialUUID string, err error)
}

func NewRouter(registry *providers.Registry, creds CredentialResolver, budget *BudgetService, breaker *Breaker, clients func(kind string) *http.Client, cfg func() RouterConfig, metrics LLMMetrics, logger *slog.Logger) *Router {
	if logger == nil {
		logger = slog.Default()
	}
	if metrics == nil {
		metrics = noopMetrics{}
	}
	if breaker == nil {
		breaker = NewBreaker(3, 30*time.Second)
	}
	return &Router{registry: registry, creds: creds, budget: budget, breaker: breaker, clients: clients, cfg: cfg, metrics: metrics, logger: logger}
}

// Credentials is the resolver the router was built with (read-only, for the
// module's wiring checks).
func (r *Router) Credentials() CredentialResolver { return r.creds }

type noopMetrics struct{}

func (noopMetrics) RecordLLMRequest(string, string)       {}
func (noopMetrics) RecordLLMTokens(string, string, int64) {}

// fallbackable: the next candidate may be tried only for a failure of
// THIS provider or credential. Cancellation, timeouts, the budget, the
// user's linked account, an exhausted quota, a malformed request and
// partial output are final (spec "Routing", step 4). The plan route's
// transient usage_unavailable / user_unavailable are fallbackable faults:
// their bounded backoff in v1 is the breaker (3 failures → 30 s open,
// ruling 5.13). A plan-route configuration rejection
// (subscription_sharing_route_not_supported, chatpass_v2_*) and an
// invalid_client from the token endpoint are final too: the next
// user_account candidate bills the same account through the same issued
// client and grant, so it would fail the same way (the remedy is the
// configuration, never another call).
func fallbackable(err error) bool {
	for _, final := range []error{context.Canceled, context.DeadlineExceeded, iface.ErrLLMBudgetExceeded,
		iface.ErrLLMUserAccountRequired, iface.ErrLLMPlanUsageDisabled, iface.ErrLLMUserAccountReauth,
		iface.ErrLLMQuotaExhausted, iface.ErrLLMInvalidRequest, iface.ErrLLMPartialOutput,
		providers.ErrSIWCRouteNotSupported, providers.ErrSIWCInvalidClient} {
		if errors.Is(err, final) {
			return false
		}
	}
	return true
}

// errorCodeOf is the ledger's errorCode for a failed call: a closed set of
// codes, never provider text.
func errorCodeOf(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, iface.ErrLLMPartialOutput):
		return "partial_output"
	case errors.Is(err, iface.ErrLLMQuotaExhausted):
		return "plan_quota_exhausted"
	case errors.Is(err, iface.ErrLLMCapabilityMismatch):
		return "capability_mismatch"
	case errors.Is(err, iface.ErrLLMUserAccountRequired):
		return "user_account_required"
	case errors.Is(err, iface.ErrLLMPlanUsageDisabled):
		return "plan_usage_disabled"
	case errors.Is(err, iface.ErrLLMUserAccountReauth):
		return "user_account_reauth_required"
	case errors.Is(err, iface.ErrLLMInvalidRequest):
		return "invalid_request"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, providers.ErrUnauthorized):
		return "credential_rejected"
	case errors.Is(err, ErrCredentialDisabled):
		return "credential_disabled"
	case errors.Is(err, iface.ErrLLMNoEligibleModel):
		return "no_eligible_model"
	}
	return "provider_unavailable"
}

// reserveOutput: the output share of the reservation — the caller's
// explicit limit, the model override, the model default, or the module
// default, in that order.
func reserveOutput(m models.Model, req iface.ChatRequest, cfg BudgetConfig) int {
	switch {
	case req.Options.MaxOutputTokens != nil:
		return *req.Options.MaxOutputTokens
	case m.BudgetReserveOutputTokens != nil && *m.BudgetReserveOutputTokens > 0:
		return *m.BudgetReserveOutputTokens
	case m.Defaults.MaxOutputTokens != nil:
		return *m.Defaults.MaxOutputTokens
	}
	return cfg.ReserveOutputTokens
}

// callResult is what one provider attempt produced.
type callResult struct {
	err       error // the provider's error
	callerErr error // the consumer's sink refused an event: the caller aborted
	// doneErr: the sink refused only the final Done event. The provider
	// had completed, so the attempt is ok; the error goes back to the
	// caller after the ledger is written.
	doneErr error
	usage   iface.LLMUsage
	finish  string
	vectors [][]float32
	// Streaming bookkeeping for the partial-output charge.
	emitted      bool
	emittedBytes int
	streamed     iface.LLMUsage
	reported     bool // a Usage event arrived
}

// execSpec describes one logical call to run().
type execSpec struct {
	reserve ReserveInput
	timeout time.Duration // consumer's Options.Timeout (0 = module default)
	// refuse (optional) rejects a candidate before its credential is
	// resolved: a mismatch decidable from the model alone.
	refuse   func(m models.Model) error
	supports func(m models.Model, p providers.Provider) bool
	call     func(ctx context.Context, p providers.Provider, m models.Model) callResult
}

// resolved is a credential already obtained for the next attempt (the
// refreshed one of a plan-route 401 retry).
type resolved struct {
	cred providers.Credential
	uuid string
}

// execOutcome is the winning attempt.
type execOutcome struct {
	model    models.Model
	fallback bool
	result   callResult
}

func (r *Router) run(ctx context.Context, cands []models.Model, spec execSpec) (*execOutcome, error) {
	if len(cands) == 0 {
		return nil, iface.ErrLLMNoEligibleModel
	}
	first := cands[0]
	bcfg := r.budget.Config()
	ttl := bcfg.ReservationTTL
	if ttl <= 0 {
		ttl = defaultReservationTTL
	}
	reservedAt := time.Now()
	res, err := r.budget.Reserve(ctx, spec.reserve)
	if err != nil {
		outcome := models.OutcomeError
		if errors.Is(err, iface.ErrLLMBudgetExceeded) {
			outcome = models.OutcomeBudgetBlocked
		}
		r.metrics.RecordLLMRequest(first.Provider, outcome)
		return nil, err
	}
	// A call must end before the reconciler may claim its reservation.
	windowEnd := reservedAt.Add(ttl - finalizeTimeout)

	var attempts []models.Attempt
	var lastErr error
	abortCode := "" // set when the caller ended the call: canceled | timeout
	// Ruling 5.2: one forced refresh per logical call; next carries the
	// refreshed credential into the retry of the same candidate.
	refreshed := false
	var next *resolved
	for i := 0; i < len(cands); i++ {
		m := cands[i]
		if len(attempts) >= models.MaxLedgerAttempts {
			break
		}
		if m.CredentialRef.Kind != first.CredentialRef.Kind {
			continue // D12: a fallback never changes who pays
		}
		if err := ctx.Err(); err != nil {
			lastErr, abortCode = err, errorCodeOf(err)
			break
		}
		// lowered: the deadline is shorter than the module's
		// request_timeout, so its firing is not the provider's fault.
		deadline, lowered := r.attemptTimeout(spec.timeout)
		if left := time.Until(windowEnd); left < deadline {
			if left < minAttemptWindow {
				if lastErr == nil {
					lastErr = fmt.Errorf("%w: reservation window exhausted", iface.ErrLLMProviderUnavailable)
				}
				break
			}
			deadline, lowered = left, true
		}
		attempt := models.Attempt{Provider: m.Provider, CredentialKind: m.CredentialRef.Kind, Outcome: attemptError}
		if spec.refuse != nil {
			if err := spec.refuse(m); err != nil {
				attempt.Outcome = attemptCapabilityMismatch
				attempts, lastErr = append(attempts, attempt), err
				continue
			}
		}

		var cred providers.Credential
		var credUUID string
		if next != nil {
			cred, credUUID, next = next.cred, next.uuid, nil
		} else {
			cred, credUUID, err = r.creds.Resolve(ctx, m, spec.reserve.UserUUID)
			if err != nil {
				attempts, lastErr = append(attempts, attempt), err
				if !fallbackable(err) {
					break
				}
				continue
			}
		}
		key := m.Provider + "|" + cred.BaseURL + "|" + m.ModelID + "|" + credUUID
		ticket, err := r.breaker.Allow(key)
		if err != nil {
			attempt.Outcome = attemptCircuitOpen
			attempts, lastErr = append(attempts, attempt), err
			if !fallbackable(err) { // a circuit opened by an exhausted quota stays final
				break
			}
			continue
		}
		prov, err := r.registry.Build(m.Provider, cred, r.clients(m.Provider))
		if err != nil {
			r.breaker.Release(key, ticket)
			// %v: a constructor's invalid-config error is not the caller's
			// malformed request.
			attempts, lastErr = append(attempts, attempt), fmt.Errorf("%w: %s adapter: %v", iface.ErrLLMProviderUnavailable, m.Provider, err)
			continue
		}
		if !spec.supports(m, prov) {
			r.breaker.Release(key, ticket)
			attempt.Outcome = attemptCapabilityMismatch
			attempts, lastErr = append(attempts, attempt), fmt.Errorf("%w: model %s", iface.ErrLLMCapabilityMismatch, m.Name)
			continue
		}

		callCtx, cancel := context.WithTimeout(ctx, deadline)
		callCtx = providers.WithStreamIdleTimeout(callCtx, r.streamIdle())
		cr := spec.call(callCtx, prov, m)
		deadlineHit := errors.Is(callCtx.Err(), context.DeadlineExceeded) || errors.Is(cr.err, context.DeadlineExceeded)
		cancel()
		callerGone := ctx.Err() != nil
		// Ruling 5.2 (PR 3): the plan route rejected the user's access
		// token before any output. Refresh it once and retry this same
		// candidate; the rejection says nothing about the provider.
		refresher, canRefresh := r.creds.(CredentialRefresher)
		retryPlan := canRefresh && !refreshed && cr.err != nil && cr.callerErr == nil && !callerGone && !cr.emitted &&
			m.CredentialRef.Kind == models.CredentialKindUserAccount && errors.Is(cr.err, providers.ErrSIWCInvalidUser)
		// Ruling 5.2 (PR 2): hang-ups, sink aborts and deadlines the caller
		// (or the reservation window) shortened say nothing about the
		// provider.
		if retryPlan || cr.callerErr != nil || callerGone || (cr.err != nil && lowered && deadlineHit) {
			r.breaker.Release(key, ticket)
		} else {
			r.breaker.Record(key, ticket, cr.err)
		}
		r.afterCall(ctx, m, credUUID, cr)

		if retryPlan {
			refreshed = true
			attempts = append(attempts, attempt)
			r.logger.WarnContext(ctx, "llm siwc: the plan route rejected the linked account token; refreshing once", planRouteAttrs(cr.err, credUUID)...)
			ncred, nuuid, rerr := refresher.Refresh(ctx, m, spec.reserve.UserUUID, cred)
			if rerr == nil {
				next = &resolved{cred: ncred, uuid: nuuid}
				i-- // the same candidate once more
				continue
			}
			if !accountClassified(rerr) && !errors.Is(rerr, iface.ErrLLMProviderUnavailable) {
				rerr = fmt.Errorf("%w: linked account refresh: %w", iface.ErrLLMProviderUnavailable, rerr)
			}
			if !errors.Is(rerr, iface.ErrLLMUserAccountReauth) {
				r.logger.WarnContext(ctx, "llm siwc: refresh after a plan-route 401 did not yield a usable token",
					append(planRouteAttrs(cr.err, credUUID), slog.String("refresh", errorCodeOf(rerr)))...)
			}
			lastErr = rerr
			if !fallbackable(rerr) {
				break
			}
			continue
		}

		if cr.err == nil && cr.callerErr == nil {
			attempt.Outcome = attemptOK
			attempt.InputTokens, attempt.OutputTokens = cr.usage.InputTokens, cr.usage.OutputTokens
			attempts = append(attempts, attempt)
			outcome := models.OutcomeOK
			if i > 0 {
				outcome = models.OutcomeFallback
			}
			r.finish(ctx, res, attempts, FinalizeInput{Outcome: outcome, Fallback: i > 0,
				Provider: m.Provider, ModelID: m.ModelID, ModelUUID: m.UUID, CredentialKind: m.CredentialRef.Kind, CredentialUUID: credUUID})
			r.metrics.RecordLLMRequest(m.Provider, outcome)
			return &execOutcome{model: m, fallback: i > 0, result: cr}, nil
		}

		charged := cr.streamed
		if cr.emitted {
			// Ruling 5.5: output already reached the caller but the stream
			// ended without final usage. With reported usage charge it, or
			// the emitted bytes / 3 if more; with none, charge the
			// reservation's share conservatively, as the reconciler would.
			emittedTokens := (cr.emittedBytes + 2) / 3
			if cr.reported {
				if charged.InputTokens <= 0 {
					charged.InputTokens = spec.reserve.EstimatedInput
				}
				charged.OutputTokens = max(charged.OutputTokens, emittedTokens)
			} else {
				charged.InputTokens = spec.reserve.EstimatedInput
				charged.OutputTokens = max(res.ReservedOutput, emittedTokens)
			}
		}
		attempt.InputTokens, attempt.OutputTokens = max(charged.InputTokens, 0), max(charged.OutputTokens, 0)
		attempts = append(attempts, attempt)

		switch {
		case cr.callerErr != nil:
			lastErr, abortCode = cr.callerErr, "canceled"
		case callerGone:
			lastErr, abortCode = fmt.Errorf("%w (provider: %v)", ctx.Err(), cr.err), errorCodeOf(ctx.Err())
		case cr.emitted:
			lastErr = fmt.Errorf("%w: %v", iface.ErrLLMPartialOutput, cr.err)
		default:
			lastErr = cr.err
		}
		if abortCode != "" || !fallbackable(lastErr) {
			break
		}
	}
	if lastErr == nil {
		lastErr = iface.ErrLLMNoEligibleModel
	}
	code := errorCodeOf(lastErr)
	if abortCode != "" {
		code = abortCode
	}
	r.finish(ctx, res, attempts, FinalizeInput{Outcome: models.OutcomeError, ErrorCode: code,
		Provider: first.Provider, ModelID: first.ModelID, ModelUUID: first.UUID, CredentialKind: first.CredentialRef.Kind, CredentialUUID: first.CredentialRef.CredentialUUID})
	r.metrics.RecordLLMRequest(first.Provider, models.OutcomeError)
	return nil, lastErr
}

// afterCall runs the AfterCall hook on a context detached from the caller
// (a hang-up after the provider completed must not lose the stamp),
// bounded like the ledger write.
func (r *Router) afterCall(ctx context.Context, m models.Model, credUUID string, cr callResult) {
	if r.AfterCall == nil {
		return
	}
	err := cr.err
	if err == nil {
		err = cr.callerErr
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finalizeTimeout)
	defer cancel()
	r.AfterCall(actx, m, credUUID, err)
}

// planRouteAttrs are the log attributes of a plan-route failure: the
// account and the allowlisted identifiers only (ruling 5.3: never provider
// text, never a token).
func planRouteAttrs(err error, accountUUID string) []any {
	attrs := []any{slog.String("account", accountUUID), slog.String("code", errorCodeOf(err))}
	var f *providers.PlanRouteFailure
	if errors.As(err, &f) {
		if f.Status != 0 {
			attrs = append(attrs, slog.Int("status", f.Status))
		}
		if f.Code != "" {
			attrs = append(attrs, slog.String("providerCode", f.Code))
		}
		if f.RequestID != "" {
			attrs = append(attrs, slog.String("requestId", f.RequestID))
		}
	}
	return attrs
}

// attemptTimeout is min(request_timeout, Options.Timeout): a consumer may
// only lower the module deadline. lowered reports that Options.Timeout won.
func (r *Router) attemptTimeout(opt time.Duration) (d time.Duration, lowered bool) {
	d = r.cfg().RequestTimeout
	if d <= 0 {
		d = defaultRequestTimeout
	}
	if opt > 0 && opt < d {
		return opt, true
	}
	return d, false
}

// streamIdle is the stream idle timeout of one call: max(30s,
// request_timeout), read per call because request_timeout hot-reloads. A
// reasoning model may stay silent for most of the attempt before its
// first output event; an idle cut is a fallbackable provider fault that
// counts toward the breaker, so it must not fire before the operator's
// own deadline does. Stall detection is then the attempt deadline.
func (r *Router) streamIdle() time.Duration {
	d := r.cfg().RequestTimeout
	if d <= 0 {
		d = defaultRequestTimeout
	}
	return max(d, minStreamIdle)
}

// finish writes the ledger (usage = every attempt's charge) and the token
// metrics. It never fails the call: a Mongo error leaves the row pending
// for the reconciler, which charges the whole reservation.
func (r *Router) finish(ctx context.Context, res *Reservation, attempts []models.Attempt, in FinalizeInput) {
	for _, a := range attempts {
		in.Usage.InputTokens += a.InputTokens
		in.Usage.OutputTokens += a.OutputTokens
		r.metrics.RecordLLMTokens(a.Provider, "input", int64(a.InputTokens))
		r.metrics.RecordLLMTokens(a.Provider, "output", int64(a.OutputTokens))
	}
	in.Attempts = attempts
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finalizeTimeout)
	defer cancel()
	if err := r.budget.Finalize(fctx, res, in); err != nil && !errors.Is(err, ErrAlreadyFinalized) {
		r.logger.ErrorContext(ctx, "llm: finalize usage failed; the reconciler will charge the reservation",
			slog.String("ledger", res.LedgerUUID), slog.String("error", err.Error()))
	}
}

// Chat runs a chat call over cands (ordered, already access-filtered).
func (r *Router) Chat(ctx context.Context, cands []models.Model, userUUID string, req iface.ChatRequest, sink iface.ChatSink) (*iface.ChatResult, error) {
	if len(cands) == 0 {
		return nil, iface.ErrLLMNoEligibleModel
	}
	if sink == nil {
		sink = func(iface.ChatEvent) error { return nil }
	}
	first := cands[0]
	out, err := r.run(ctx, cands, execSpec{
		reserve: ReserveInput{
			UserUUID: userUUID, ModelUUID: first.UUID, Provider: first.Provider, ModelID: first.ModelID,
			CredentialKind: first.CredentialRef.Kind, CredentialUUID: first.CredentialRef.CredentialUUID,
			Caller: req.Caller, Purpose: req.EffectivePurpose(),
			EstimatedInput: EstimateInputTokens(req), ReserveOutput: reserveOutput(first, req, r.budget.Config()),
		},
		timeout:  req.Options.Timeout,
		supports: func(m models.Model, p providers.Provider) bool { return chatSupported(m, p, req) },
		call: func(ctx context.Context, p providers.Provider, m models.Model) callResult {
			var cr callResult
			outcome, err := p.Chat(ctx, providers.ChatCall{ModelID: m.ModelID, Request: req,
				Defaults: providers.ChatDefaults{Temperature: m.Defaults.Temperature, MaxOutputTokens: m.Defaults.MaxOutputTokens, Effort: m.Defaults.Effort}},
				func(ev iface.ChatEvent) error {
					if ev.Delta != "" || ev.ToolCall != nil {
						cr.emitted = true
						cr.emittedBytes += len(ev.Delta)
						if tc := ev.ToolCall; tc != nil {
							cr.emittedBytes += len(tc.Name) + len(tc.Arguments)
						}
					}
					if ev.Usage != nil {
						cr.streamed, cr.reported = *ev.Usage, true
					}
					if err := sink(ev); err != nil {
						if ev.Done {
							// The provider has completed: charge and ledger
							// the call as ok, return the error afterwards.
							cr.doneErr = err
							return nil
						}
						cr.callerErr = err
						return err
					}
					return nil
				})
			switch {
			case err != nil:
				cr.err = err
			case outcome == nil:
				cr.err = fmt.Errorf("%w: adapter returned no outcome", iface.ErrLLMProviderUnavailable)
			default:
				cr.usage, cr.finish = outcome.Usage, outcome.FinishReason
			}
			return cr
		},
	})
	if err != nil {
		return nil, err
	}
	if out.result.doneErr != nil {
		return nil, out.result.doneErr
	}
	return &iface.ChatResult{Model: out.model.Info(), Usage: out.result.usage, FinishReason: out.result.finish,
		BillingScope: out.model.CredentialRef.Kind, Fallback: out.fallback}, nil
}

// chatSupported intersects the operator's promise (model capabilities)
// with what the adapter can do.
func chatSupported(m models.Model, p providers.Provider, req iface.ChatRequest) bool {
	pc := p.Capabilities()
	if !pc.Chat || !m.Capabilities.Chat {
		return false
	}
	if len(req.Tools) > 0 && !(pc.Tools && m.Capabilities.Tools) {
		return false
	}
	if req.ResponseFormat != nil && !(pc.StructuredOutput && m.Capabilities.StructuredOutput) {
		return false
	}
	return true
}

// Embed runs an embedding call over cands. Embeddings have no output
// tokens; the reservation's output share is a single token.
func (r *Router) Embed(ctx context.Context, cands []models.Model, userUUID string, req iface.EmbedRequest) (*iface.EmbedResult, error) {
	if len(cands) == 0 {
		return nil, iface.ErrLLMNoEligibleModel
	}
	first := cands[0]
	out, err := r.run(ctx, cands, execSpec{
		reserve: ReserveInput{
			UserUUID: userUUID, ModelUUID: first.UUID, Provider: first.Provider, ModelID: first.ModelID,
			CredentialKind: first.CredentialRef.Kind, CredentialUUID: first.CredentialRef.CredentialUUID,
			Caller: req.Caller, Purpose: req.EffectivePurpose(),
			EstimatedInput: EstimateEmbedTokens(req.Inputs), ReserveOutput: 1,
		},
		// The ChatGPT plan route has no embeddings: refused before the
		// linked account is even resolved.
		refuse: func(m models.Model) error {
			if m.CredentialRef.Kind == models.CredentialKindUserAccount {
				return fmt.Errorf("%w: embeddings are not available on the ChatGPT plan route", iface.ErrLLMCapabilityMismatch)
			}
			return nil
		},
		supports: func(m models.Model, p providers.Provider) bool {
			return p.Capabilities().Embeddings && m.Capabilities.Embeddings
		},
		call: func(ctx context.Context, p providers.Provider, m models.Model) callResult {
			var cr callResult
			outcome, err := p.Embed(ctx, providers.EmbedCall{ModelID: m.ModelID, Inputs: req.Inputs})
			switch {
			case err != nil:
				cr.err = err
				if outcome != nil {
					// Batches the provider completed before failing:
					// charged on the failed attempt (see Provider.Embed).
					cr.streamed.InputTokens = max(outcome.Usage.InputTokens, 0)
				}
			case outcome == nil || len(outcome.Vectors) != len(req.Inputs):
				cr.err = fmt.Errorf("%w: adapter returned a vector count that does not match the inputs", iface.ErrLLMProviderUnavailable)
			default:
				cr.usage, cr.vectors = iface.LLMUsage{InputTokens: outcome.Usage.InputTokens}, outcome.Vectors
			}
			return cr
		},
	})
	if err != nil {
		return nil, err
	}
	dims := 0
	if len(out.result.vectors) > 0 {
		dims = len(out.result.vectors[0])
	}
	return &iface.EmbedResult{Vectors: out.result.vectors, Dimensions: dims, Model: out.model.Info(), Usage: out.result.usage}, nil
}
