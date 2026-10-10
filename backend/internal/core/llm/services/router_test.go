package services

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type chatScript func(ctx context.Context, sink iface.ChatSink) (*providers.ChatOutcome, error)

type scriptedProvider struct {
	name  string
	caps  iface.LLMCapabilities
	chat  chatScript
	embed func(ctx context.Context) (*providers.EmbedOutcome, error)
	calls *int
}

func (p *scriptedProvider) Name() string                        { return p.name }
func (p *scriptedProvider) Capabilities() iface.LLMCapabilities { return p.caps }
func (p *scriptedProvider) Chat(ctx context.Context, _ providers.ChatCall, sink iface.ChatSink) (*providers.ChatOutcome, error) {
	*p.calls++
	return p.chat(ctx, sink)
}
func (p *scriptedProvider) Embed(ctx context.Context, _ providers.EmbedCall) (*providers.EmbedOutcome, error) {
	*p.calls++
	if p.embed != nil {
		return p.embed(ctx)
	}
	return &providers.EmbedOutcome{Vectors: [][]float32{{1, 2}}, Usage: iface.LLMUsage{InputTokens: 1}}, nil
}
func (p *scriptedProvider) ListModels(context.Context) ([]providers.ModelDescriptor, error) {
	return nil, nil
}
func (p *scriptedProvider) Health(context.Context) error { return nil }

var allCaps = iface.LLMCapabilities{Chat: true, Streaming: true, Tools: true, StructuredOutput: true, Embeddings: true}

type staticCreds struct{}

func (staticCreds) Resolve(_ context.Context, m models.Model, _ string) (providers.Credential, string, error) {
	if m.CredentialRef.Kind == models.CredentialKindUserAccount {
		return providers.Credential{}, "", iface.ErrLLMUserAccountRequired
	}
	return providers.Credential{Kind: models.CredentialKindOrg, Secret: "k", BaseURL: "http://x"}, m.CredentialRef.CredentialUUID, nil
}

type memMetrics struct{ requests, tokens map[string]int64 }

func (m *memMetrics) RecordLLMRequest(p, o string)         { m.requests[p+"/"+o]++ }
func (m *memMetrics) RecordLLMTokens(p, d string, n int64) { m.tokens[p+"/"+d] += n }

func model(uuid, kind, cred string) models.Model {
	return models.Model{UUID: uuid, TenantID: "t1", Name: uuid, Provider: kind, ModelID: "m-" + uuid, Status: models.ModelStatusActive, Access: models.AccessEveryone,
		Capabilities:  models.LLMModelCapabilities{Chat: true, Streaming: true, Tools: true, StructuredOutput: true, Embeddings: true, Dimensions: 2},
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: cred},
		Purposes:      []models.LLMModelPurpose{{Purpose: "default", Priority: 1}}}
}

type routerEnv struct {
	r       *Router
	store   *memBudgetStore
	metrics *memMetrics
	calls   map[string]*int
	cfg     RouterConfig
	bcfg    BudgetConfig
}

func newRouterEnv(t *testing.T, store *memBudgetStore, scripts map[string]chatScript) *routerEnv {
	t.Helper()
	if store == nil {
		store = newMemBudgetStore(0, "")
		store.budget = nil
	}
	env := &routerEnv{store: store, metrics: &memMetrics{requests: map[string]int64{}, tokens: map[string]int64{}}, calls: map[string]*int{},
		cfg: RouterConfig{RequestTimeout: 5 * time.Second}, bcfg: BudgetConfig{ReserveOutputTokens: 10, ReservationTTL: time.Minute}}
	reg := providers.NewRegistry()
	for kind, script := range scripts {
		k, s := kind, script
		n := new(int)
		env.calls[k] = n
		reg.Register(k, func(providers.Credential, *http.Client) (providers.Provider, error) {
			return &scriptedProvider{name: k, caps: allCaps, chat: s, calls: n}, nil
		})
	}
	budget := NewBudgetService(store, func() BudgetConfig { return env.bcfg }, slog.Default())
	env.r = NewRouter(reg, staticCreds{}, budget, NewBreaker(3, 30*time.Second), func(string) *http.Client { return &http.Client{} },
		func() RouterConfig { return env.cfg }, env.metrics, slog.Default())
	return env
}

func (e *routerEnv) chat(cands []models.Model, req iface.ChatRequest, sink iface.ChatSink) (*iface.ChatResult, error) {
	if sink == nil {
		sink = func(iface.ChatEvent) error { return nil }
	}
	return e.r.Chat(ctxFor("t1"), cands, "u", req, sink)
}

func (e *routerEnv) count(kind string) int {
	if n := e.calls[kind]; n != nil {
		return *n
	}
	return 0
}

var simpleReq = iface.ChatRequest{Caller: "test", Messages: []iface.ChatMessage{{Role: "user", Content: "hi"}}}

func ok(text string, in, out int) chatScript {
	return func(_ context.Context, sink iface.ChatSink) (*providers.ChatOutcome, error) {
		if err := sink(iface.ChatEvent{Delta: text}); err != nil {
			return nil, err
		}
		u := iface.LLMUsage{InputTokens: in, OutputTokens: out}
		if err := sink(iface.ChatEvent{Done: true, Usage: &u}); err != nil {
			return nil, err
		}
		return &providers.ChatOutcome{Usage: u, FinishReason: "stop"}, nil
	}
}

func fail(err error) chatScript {
	return func(context.Context, iface.ChatSink) (*providers.ChatOutcome, error) { return nil, err }
}

func singleLedger(t *testing.T, store *memBudgetStore) *models.UsageEvent {
	t.Helper()
	if len(store.ledger) != 1 {
		t.Fatalf("ledger rows = %d", len(store.ledger))
	}
	for _, ev := range store.ledger {
		return ev
	}
	return nil
}

func TestRouter_OKResultAndLedger(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": ok("hello", 4, 2)})
	var deltas string
	res, err := env.chat([]models.Model{model("a", "openai", "c1")}, simpleReq, func(ev iface.ChatEvent) error { deltas += ev.Delta; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if deltas != "hello" || res.Model.UUID != "a" || res.Usage != (iface.LLMUsage{InputTokens: 4, OutputTokens: 2}) || res.FinishReason != "stop" ||
		res.BillingScope != models.CredentialKindOrg || res.Fallback {
		t.Fatalf("res = %+v, deltas %q", res, deltas)
	}
	ev := singleLedger(t, env.store)
	if ev.Outcome != models.OutcomeOK || ev.InputTokens != 4 || ev.OutputTokens != 2 || ev.ReservedTokens != 0 || ev.CredentialUUID != "c1" ||
		len(ev.Attempts) != 1 || ev.Attempts[0].Outcome != "ok" || ev.Caller != "test" || ev.Purpose != "default" {
		t.Fatalf("ledger = %+v", ev)
	}
	if env.metrics.requests["openai/ok"] != 1 || env.metrics.tokens["openai/input"] != 4 || env.metrics.tokens["openai/output"] != 2 {
		t.Fatalf("metrics = %+v", env.metrics)
	}
}

func TestRouter_FallbackBeforeOutputSameKind(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{
		"openai": fail(iface.ErrLLMProviderUnavailable), "anthropic": ok("hi", 3, 1),
	})
	cands := []models.Model{model("a", "openai", "c1"), model("b", "anthropic", "c2")}
	res, err := env.chat(cands, simpleReq, nil)
	if err != nil || !res.Fallback || res.Model.UUID != "b" || res.Usage.OutputTokens != 1 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	ev := singleLedger(t, env.store)
	if ev.Outcome != models.OutcomeFallback || !ev.Fallback || len(ev.Attempts) != 2 || ev.Attempts[0].Outcome != "error" ||
		ev.ModelUUID != "b" || ev.Provider != "anthropic" {
		t.Fatalf("ledger = %+v", ev)
	}
	if env.metrics.requests["anthropic/fallback"] != 1 || env.metrics.tokens["anthropic/output"] != 1 {
		t.Fatalf("metrics = %+v", env.metrics)
	}
}

// Review Focus 1: text, then a provider failure. No fallback, partial
// output error, an error ledger row that still charges the tokens the
// stream consumed — conservatively the reservation share when the stream
// never reported usage (ruling 5.5).
func TestRouter_NoFallbackAfterPartialOutput(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{
		"openai": func(_ context.Context, sink iface.ChatSink) (*providers.ChatOutcome, error) {
			_ = sink(iface.ChatEvent{Delta: "partial"})
			return nil, iface.ErrLLMProviderUnavailable
		},
		"anthropic": ok("never", 1, 1),
	})
	cands := []models.Model{model("a", "openai", "c1"), model("b", "anthropic", "c2")}
	var got string
	_, err := env.chat(cands, simpleReq, func(ev iface.ChatEvent) error { got += ev.Delta; return nil })
	if !errors.Is(err, iface.ErrLLMPartialOutput) {
		t.Fatalf("err = %v", err)
	}
	if got != "partial" || env.count("anthropic") != 0 {
		t.Fatalf("deltas %q, fallback calls %d", got, env.count("anthropic"))
	}
	ev := singleLedger(t, env.store)
	// Estimate for "hi" = 1 input token; reserved output = 10.
	if ev.Outcome != models.OutcomeError || ev.ErrorCode != "partial_output" || len(ev.Attempts) != 1 ||
		ev.InputTokens != 1 || ev.OutputTokens != 10 || ev.Attempts[0].OutputTokens != 10 {
		t.Fatalf("ledger = %+v", ev)
	}
	mo := env.store.monthly[models.PeriodOf(time.Now())]
	if mo.Consumed() != 11 || mo.ReservedTokens != 0 {
		t.Fatalf("monthly = %+v", mo)
	}
	if env.metrics.requests["openai/error"] != 1 || env.metrics.tokens["openai/output"] != 10 {
		t.Fatalf("metrics = %+v", env.metrics)
	}
}

// With reported usage the charge is max(reported, emitted bytes / 3), not
// the reservation share (ruling 5.5); a tool call counts as output.
func TestRouter_PartialOutputChargesReportedUsageAndToolCalls(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{
		"openai": func(_ context.Context, sink iface.ChatSink) (*providers.ChatOutcome, error) {
			_ = sink(iface.ChatEvent{ToolCall: &iface.ChatToolCall{ID: "1", Name: "f", Arguments: "{}"}})
			_ = sink(iface.ChatEvent{Usage: &iface.LLMUsage{InputTokens: 50, OutputTokens: 4}})
			return nil, iface.ErrLLMProviderUnavailable
		},
		"anthropic": ok("never", 1, 1),
	})
	_, err := env.chat([]models.Model{model("a", "openai", "c1"), model("b", "anthropic", "c2")}, simpleReq, nil)
	if !errors.Is(err, iface.ErrLLMPartialOutput) || env.count("anthropic") != 0 {
		t.Fatalf("err = %v, fallback calls %d", err, env.count("anthropic"))
	}
	if ev := singleLedger(t, env.store); ev.InputTokens != 50 || ev.OutputTokens != 4 {
		t.Fatalf("ledger = %+v", ev)
	}
}

func TestRouter_NoFallbackOnFinalErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"quota", iface.ErrLLMQuotaExhausted},
		{"canceled", context.Canceled},
		{"deadline", context.DeadlineExceeded},
		{"reauth", iface.ErrLLMUserAccountReauth},
		{"plan usage", iface.ErrLLMPlanUsageDisabled},
		{"invalid", iface.ErrLLMInvalidRequest},
	} {
		env := newRouterEnv(t, nil, map[string]chatScript{"openai": fail(tc.err), "anthropic": ok("x", 1, 1)})
		cands := []models.Model{model("a", "openai", "c1"), model("b", "anthropic", "c2")}
		if _, err := env.chat(cands, simpleReq, nil); !errors.Is(err, tc.err) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
		if env.count("anthropic") != 0 {
			t.Errorf("%s: fell back", tc.name)
		}
		if ev := singleLedger(t, env.store); ev.Outcome != models.OutcomeError || ev.ErrorCode == "" {
			t.Errorf("%s: ledger = %+v", tc.name, ev)
		}
	}
}

func TestRouter_NeverFallsBackAcrossCredentialKind(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": fail(iface.ErrLLMProviderUnavailable)})
	ua := model("b", "openai", "")
	ua.CredentialRef = models.LLMCredentialRef{Kind: models.CredentialKindUserAccount}
	_, err := env.chat([]models.Model{model("a", "openai", "c1"), ua}, simpleReq, nil)
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || env.count("openai") != 1 {
		t.Fatalf("cross-kind err = %v, calls %d", err, env.count("openai"))
	}
	if ev := singleLedger(t, env.store); len(ev.Attempts) != 1 {
		t.Fatalf("ledger = %+v", ev)
	}
}

func TestRouter_UserAccountModelRequiresLinkedAccount(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": ok("x", 1, 1)})
	ua := model("a", "openai", "")
	ua.CredentialRef = models.LLMCredentialRef{Kind: models.CredentialKindUserAccount}
	ua2 := model("b", "openai", "")
	ua2.CredentialRef = ua.CredentialRef
	_, err := env.chat([]models.Model{ua, ua2}, simpleReq, nil)
	if !errors.Is(err, iface.ErrLLMUserAccountRequired) || env.count("openai") != 0 {
		t.Fatalf("err = %v, calls = %d", err, env.count("openai"))
	}
	if ev := singleLedger(t, env.store); ev.ErrorCode != "user_account_required" || ev.CredentialKind != models.CredentialKindUserAccount || len(ev.Attempts) != 1 {
		t.Fatalf("ledger = %+v", ev)
	}
}

func TestRouter_BudgetBlockedBeforeAnyCall(t *testing.T) {
	store := newMemBudgetStore(5, models.BudgetActionBlock)
	env := newRouterEnv(t, store, map[string]chatScript{"openai": ok("x", 1, 1)})
	_, err := env.chat([]models.Model{model("a", "openai", "c1")}, simpleReq, nil)
	if !errors.Is(err, iface.ErrLLMBudgetExceeded) || env.count("openai") != 0 || env.metrics.requests["openai/budget_blocked"] != 1 {
		t.Fatalf("err=%v calls=%d metrics=%+v", err, env.count("openai"), env.metrics)
	}
	if ev := singleLedger(t, store); ev.Outcome != models.OutcomeBudgetBlocked {
		t.Fatalf("ledger = %+v", ev)
	}
}

func TestRouter_BreakerOpensAfterThreeFailures(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": fail(iface.ErrLLMQuotaExhausted)})
	for i := 0; i < 4; i++ {
		_, _ = env.chat([]models.Model{model("a", "openai", "c1")}, simpleReq, nil)
	}
	if env.count("openai") != 3 {
		t.Fatalf("provider calls = %d, want 3 (fourth refused by the breaker)", env.count("openai"))
	}
	_, err := env.chat([]models.Model{model("a", "openai", "c1")}, simpleReq, nil)
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || !errors.Is(err, iface.ErrLLMQuotaExhausted) {
		t.Fatalf("open-circuit err must carry both sentinels: %v", err)
	}
	if env.count("openai") != 3 {
		t.Fatalf("provider calls = %d after open circuit", env.count("openai"))
	}
}

// An open circuit on the primary moves on to the next same-kind candidate
// when its cause is fallbackable.
func TestRouter_OpenCircuitSkipsToNextCandidate(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": fail(iface.ErrLLMProviderUnavailable), "anthropic": ok("hi", 1, 1)})
	for i := 0; i < 3; i++ {
		_, _ = env.chat([]models.Model{model("a", "openai", "c1")}, simpleReq, nil)
	}
	res, err := env.chat([]models.Model{model("a", "openai", "c1"), model("b", "anthropic", "c2")}, simpleReq, nil)
	if err != nil || res.Model.UUID != "b" || !res.Fallback || env.count("openai") != 3 {
		t.Fatalf("res=%+v err=%v openai calls=%d", res, err, env.count("openai"))
	}
}

// Ruling 5.2: callers hanging up never open the circuit.
func TestRouter_CallerCancellationsNeverOpenTheCircuit(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": func(ctx context.Context, sink iface.ChatSink) (*providers.ChatOutcome, error) {
		if ctx.Err() != nil {
			return nil, iface.ErrLLMProviderUnavailable // an adapter that misclassifies the hang-up
		}
		return ok("x", 1, 1)(ctx, sink)
	}})
	m := model("a", "openai", "c1")
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(ctxFor("t1"))
		cancel()
		if _, err := env.r.Chat(ctx, []models.Model{m}, "u", simpleReq, func(iface.ChatEvent) error { return nil }); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled call err = %v", err)
		}
	}
	for i := 0; i < 5; i++ {
		_, _ = env.chat([]models.Model{m}, simpleReq, func(iface.ChatEvent) error { return errors.New("client went away") })
	}
	if _, err := env.chat([]models.Model{m}, simpleReq, nil); err != nil {
		t.Fatalf("circuit opened by caller hang-ups: %v", err)
	}
}

func TestRouter_SinkErrorAbortsWithoutFallback(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": ok("hello", 1, 1), "anthropic": ok("x", 1, 1)})
	gone := errors.New("client went away")
	_, err := env.chat([]models.Model{model("a", "openai", "c1"), model("b", "anthropic", "c2")}, simpleReq, func(iface.ChatEvent) error { return gone })
	if !errors.Is(err, gone) || env.count("anthropic") != 0 {
		t.Fatalf("err = %v, fallback calls %d", err, env.count("anthropic"))
	}
	if ev := singleLedger(t, env.store); ev.Outcome != models.OutcomeError || ev.ErrorCode != "canceled" || ev.OutputTokens == 0 {
		t.Fatalf("ledger = %+v", ev)
	}
}

// Final review M5: when the provider completed and only the consumer's
// sink refuses the Done event, the call is ledgered ok with the reported
// usage (not canceled with the reservation charged); the sink's error still
// comes back to the caller unchanged.
func TestRouter_SinkErrorOnDoneIsLedgeredOK(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": ok("hello", 4, 2), "anthropic": ok("x", 1, 1)})
	gone := errors.New("client went away after the last delta")
	_, err := env.chat([]models.Model{model("a", "openai", "c1"), model("b", "anthropic", "c2")}, simpleReq, func(ev iface.ChatEvent) error {
		if ev.Done {
			return gone
		}
		return nil
	})
	if !errors.Is(err, gone) || env.count("anthropic") != 0 {
		t.Fatalf("err = %v, fallback calls %d", err, env.count("anthropic"))
	}
	ev := singleLedger(t, env.store)
	if ev.Outcome != models.OutcomeOK || ev.ErrorCode != "" || ev.InputTokens != 4 || ev.OutputTokens != 2 {
		t.Fatalf("ledger = %+v, want ok with the reported 4/2 tokens", ev)
	}
	if env.metrics.requests["openai/"+models.OutcomeOK] != 1 {
		t.Fatalf("metrics = %v", env.metrics.requests)
	}
}

// Spec data model: at most 8 attempts per logical call (preflight 3.13).
func TestRouter_FallbackAttemptsAreBounded(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": fail(iface.ErrLLMProviderUnavailable)})
	var cands []models.Model
	for i := 0; i < 12; i++ {
		cands = append(cands, model(string(rune('a'+i)), "openai", "c"+string(rune('a'+i))))
	}
	if _, err := env.chat(cands, simpleReq, nil); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if env.count("openai") != models.MaxLedgerAttempts {
		t.Fatalf("provider calls = %d, want %d", env.count("openai"), models.MaxLedgerAttempts)
	}
	if ev := singleLedger(t, env.store); len(ev.Attempts) != models.MaxLedgerAttempts {
		t.Fatalf("attempts = %d", len(ev.Attempts))
	}
}

func TestRouter_DeadlineIsMinOfConfigAndOption(t *testing.T) {
	var got time.Duration
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": func(ctx context.Context, sink iface.ChatSink) (*providers.ChatOutcome, error) {
		dl, _ := ctx.Deadline()
		got = time.Until(dl)
		return ok("x", 1, 1)(ctx, sink)
	}})
	req := simpleReq
	req.Options.Timeout = 2 * time.Second
	if _, err := env.chat([]models.Model{model("a", "openai", "c1")}, req, nil); err != nil {
		t.Fatal(err)
	}
	if got <= 0 || got > 2*time.Second {
		t.Fatalf("deadline = %v, want <= 2s", got)
	}
	req.Options.Timeout = time.Hour // may only lower the module deadline
	if _, err := env.chat([]models.Model{model("a", "openai", "c1")}, req, nil); err != nil {
		t.Fatal(err)
	}
	if got > 5*time.Second {
		t.Fatalf("deadline = %v, want <= request timeout", got)
	}
}

// Final review I3: the stream idle timeout is max(30s, request_timeout),
// read per call (request_timeout is hot-reloadable), so a model that
// thinks silently is bounded by the attempt deadline, not a fixed 30 s.
func TestRouter_StreamIdleTimeoutFollowsRequestTimeout(t *testing.T) {
	var got time.Duration
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": func(ctx context.Context, sink iface.ChatSink) (*providers.ChatOutcome, error) {
		got = providers.StreamIdleTimeout(ctx)
		return ok("x", 1, 1)(ctx, sink)
	}})
	for _, tc := range []struct{ requestTimeout, want time.Duration }{
		{5 * time.Second, 30 * time.Second},
		{30 * time.Second, 30 * time.Second},
		{10 * time.Minute, 10 * time.Minute},
		{0, defaultRequestTimeout},
	} {
		env.cfg.RequestTimeout = tc.requestTimeout
		if _, err := env.chat([]models.Model{model("a", "openai", "c1")}, simpleReq, nil); err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("request_timeout %v: stream idle = %v, want %v", tc.requestTimeout, got, tc.want)
		}
	}
}

func TestRouter_CapabilityMismatchFallsBack(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"anthropic": ok("hi", 1, 1)})
	// Register a provider without tool support under "openai".
	n := new(int)
	env.calls["openai"] = n
	env.r.registry.Register("openai", func(providers.Credential, *http.Client) (providers.Provider, error) {
		return &scriptedProvider{name: "openai", caps: iface.LLMCapabilities{Chat: true}, chat: ok("x", 1, 1), calls: n}, nil
	})
	req := simpleReq
	req.Tools = []iface.ChatToolSpec{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}}
	res, err := env.chat([]models.Model{model("a", "openai", "c1"), model("b", "anthropic", "c2")}, req, nil)
	if err != nil || res.Model.UUID != "b" || *n != 0 {
		t.Fatalf("res=%+v err=%v openai calls=%d", res, err, *n)
	}
	if ev := singleLedger(t, env.store); ev.Attempts[0].Outcome != "capability_mismatch" {
		t.Fatalf("ledger = %+v", ev)
	}
}

func TestRouter_EmbedOKAndFallback(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": ok("", 0, 0), "gemini": ok("", 0, 0)})
	n := new(int)
	env.calls["openai"] = n
	env.r.registry.Register("openai", func(providers.Credential, *http.Client) (providers.Provider, error) {
		return &scriptedProvider{name: "openai", caps: allCaps, calls: n, embed: func(context.Context) (*providers.EmbedOutcome, error) {
			return nil, iface.ErrLLMProviderUnavailable
		}}, nil
	})
	req := iface.EmbedRequest{Caller: "test", Inputs: []string{"abc"}}
	res, err := env.r.Embed(ctxFor("t1"), []models.Model{model("a", "openai", "c1"), model("b", "gemini", "c2")}, "u", req)
	if err != nil || res.Model.UUID != "b" || res.Dimensions != 2 || len(res.Vectors) != 1 || res.Usage.InputTokens != 1 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	ev := singleLedger(t, env.store)
	if ev.Outcome != models.OutcomeFallback || len(ev.Attempts) != 2 || ev.InputTokens != 1 || ev.OutputTokens != 0 {
		t.Fatalf("ledger = %+v", ev)
	}
	if env.metrics.requests["gemini/fallback"] != 1 || env.metrics.tokens["gemini/input"] != 1 {
		t.Fatalf("metrics = %+v", env.metrics)
	}
}

func TestRouter_EmptyCandidates(t *testing.T) {
	env := newRouterEnv(t, nil, nil)
	if _, err := env.chat(nil, simpleReq, nil); !errors.Is(err, iface.ErrLLMNoEligibleModel) {
		t.Fatalf("err = %v", err)
	}
	if _, err := env.r.Embed(ctxFor("t1"), nil, "u", iface.EmbedRequest{Caller: "t", Inputs: []string{"x"}}); !errors.Is(err, iface.ErrLLMNoEligibleModel) {
		t.Fatalf("err = %v", err)
	}
}

// blockUntilDone is a provider that hangs until its call context ends and
// then reports what the transport would: unavailable + deadline.
func blockUntilDone(ctx context.Context, _ iface.ChatSink) (*providers.ChatOutcome, error) {
	<-ctx.Done()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, errors.Join(iface.ErrLLMProviderUnavailable, context.DeadlineExceeded)
	}
	return nil, iface.ErrLLMProviderUnavailable
}

const keyA = "openai|http://x|m-a|c1"

// Ruling 5.2: a deadline the caller lowered (Options.Timeout) is the
// caller's choice, not a provider fault.
func TestRouter_CallerLoweredDeadlineNeverOpensTheCircuit(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": blockUntilDone})
	req := simpleReq
	req.Options.Timeout = 50 * time.Millisecond
	for i := 0; i < 4; i++ {
		if _, err := env.chat([]models.Model{model("a", "openai", "c1")}, req, nil); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	}
	if err := env.r.breaker.Allow(keyA); err != nil || env.count("openai") != 4 {
		t.Fatalf("lowered deadlines opened the circuit: %v (calls %d)", err, env.count("openai"))
	}
	for _, ev := range env.store.ledger {
		if ev.ErrorCode != "timeout" {
			t.Fatalf("ledger code = %q", ev.ErrorCode)
		}
	}
}

// The module's own request_timeout firing is a provider fault; a circuit
// opened by timeouts still lets the call fall back (only quota is final).
func TestRouter_ModuleTimeoutsOpenTheCircuitButFallBack(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": blockUntilDone, "anthropic": ok("hi", 1, 1)})
	env.cfg.RequestTimeout = 50 * time.Millisecond
	for i := 0; i < 3; i++ {
		_, _ = env.chat([]models.Model{model("a", "openai", "c1")}, simpleReq, nil)
	}
	if err := env.r.breaker.Allow(keyA); !errors.Is(err, iface.ErrLLMProviderUnavailable) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("open circuit err = %v (must be unavailable, not a final deadline)", err)
	}
	res, err := env.chat([]models.Model{model("a", "openai", "c1"), model("b", "anthropic", "c2")}, simpleReq, nil)
	if err != nil || res.Model.UUID != "b" || !res.Fallback || env.count("openai") != 3 {
		t.Fatalf("res=%+v err=%v openai calls=%d", res, err, env.count("openai"))
	}
}

func TestRouter_CallerCanceledDuringCallIsNotRecorded(t *testing.T) {
	var cancel context.CancelFunc
	env := newRouterEnv(t, nil, map[string]chatScript{
		"openai": func(context.Context, iface.ChatSink) (*providers.ChatOutcome, error) {
			cancel() // the client hangs up mid-call; the adapter misreports it
			return nil, iface.ErrLLMProviderUnavailable
		},
		"anthropic": ok("x", 1, 1),
	})
	for i := 0; i < 4; i++ {
		ctx, c := context.WithCancel(ctxFor("t1"))
		cancel = c
		_, err := env.r.Chat(ctx, []models.Model{model("a", "openai", "c1"), model("b", "anthropic", "c2")}, "u", simpleReq, nil)
		c()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	}
	if err := env.r.breaker.Allow(keyA); err != nil || env.count("anthropic") != 0 {
		t.Fatalf("breaker err=%v fallback calls=%d", err, env.count("anthropic"))
	}
	for _, ev := range env.store.ledger {
		if ev.ErrorCode != "canceled" {
			t.Fatalf("ledger code = %q", ev.ErrorCode)
		}
	}
}

func TestRouter_CallerOwnDeadlineIsLedgeredAsTimeout(t *testing.T) {
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": blockUntilDone})
	ctx, cancel := context.WithTimeout(ctxFor("t1"), 50*time.Millisecond)
	defer cancel()
	if _, err := env.r.Chat(ctx, []models.Model{model("a", "openai", "c1")}, "u", simpleReq, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if ev := singleLedger(t, env.store); ev.ErrorCode != "timeout" {
		t.Fatalf("ledger code = %q", ev.ErrorCode)
	}
	if err := env.r.breaker.Allow(keyA); err != nil {
		t.Fatal(err)
	}
}

// The attempt deadline never outlives the reservation: it is clamped to
// the window, and no attempt starts with less than minAttemptWindow left.
func TestRouter_ReservationWindowClampsAndStops(t *testing.T) {
	var got time.Duration
	env := newRouterEnv(t, nil, map[string]chatScript{"openai": func(ctx context.Context, sink iface.ChatSink) (*providers.ChatOutcome, error) {
		dl, _ := ctx.Deadline()
		got = time.Until(dl)
		return ok("x", 1, 1)(ctx, sink)
	}})
	env.bcfg.ReservationTTL = finalizeTimeout + 1500*time.Millisecond
	if _, err := env.chat([]models.Model{model("a", "openai", "c1")}, simpleReq, nil); err != nil {
		t.Fatal(err)
	}
	if got <= 0 || got > 1500*time.Millisecond {
		t.Fatalf("clamped deadline = %v", got)
	}

	env.bcfg.ReservationTTL = finalizeTimeout + 500*time.Millisecond
	_, err := env.chat([]models.Model{model("a", "openai", "c1")}, simpleReq, nil)
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || env.count("openai") != 1 {
		t.Fatalf("err=%v calls=%d", err, env.count("openai"))
	}
}
