package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/installation"
	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/internal/core/llm/providers/teitest"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

type manualClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type installationEnv struct {
	g                 *Gateway
	svc               *InstallationService
	set               *installation.Set
	embed             *teitest.Server
	rerank            *teitest.Server
	store             *memBudgetStore
	metrics           *memMetrics
	clock             *manualClock
	embedID, rerankID string
}

var (
	embedFake  = teitest.Config{ModelID: "BAAI/bge-m3", Revision: "abc123", Pooling: "cls", Dimensions: 4}
	rerankFake = teitest.Config{ModelID: "BAAI/bge-reranker-v2-m3", Revision: "def456", Kind: "rerank"}
)

func newInstallationEnv(t *testing.T, embedCfg, rerankCfg teitest.Config, store *memBudgetStore) *installationEnv {
	t.Helper()
	embedSrv, rerankSrv := teitest.New(t, embedCfg), teitest.New(t, rerankCfg)
	eps := []installation.Endpoint{
		{Name: "embed", URL: embedSrv.URL, Kind: iface.InferenceKindEmbed, Boundary: iface.InferenceBoundaryLocal,
			Model: "BAAI/bge-m3", Revision: "abc123", Pooling: "cls", Normalize: true, Truncate: true,
			QueryPrefix: "query: ", DocumentPrefix: "passage: "},
		{Name: "rerank", URL: rerankSrv.URL, Kind: iface.InferenceKindRerank, Boundary: iface.InferenceBoundaryLocal,
			Model: "BAAI/bge-reranker-v2-m3", Revision: "def456", Truncate: true},
	}
	return buildInstallationEnv(t, eps, embedSrv, rerankSrv, store)
}

func buildInstallationEnv(t *testing.T, eps []installation.Endpoint, embedSrv, rerankSrv *teitest.Server, store *memBudgetStore) *installationEnv {
	t.Helper()
	clk := &manualClock{t: time.Unix(1_700_000_000, 0)}
	set := installation.NewSet(eps, providers.NewInstallationHTTPClient(providers.TransportConfig{}), installation.Options{Now: clk.Now})
	metrics := &memMetrics{requests: map[string]int64{}, tokens: map[string]int64{}}
	budget := NewBudgetService(store, fixedBudgetCfg, slog.Default())
	svc := NewInstallationService(set, budget, NewBreaker(3, 30*time.Second), func() RouterConfig { return RouterConfig{} }, metrics, slog.Default())
	g := newGatewayForTest(t)
	g.SetInstallation(svc)
	env := &installationEnv{g: g, svc: svc, set: set, embed: embedSrv, rerank: rerankSrv, store: store, metrics: metrics, clock: clk}
	for _, ep := range eps {
		if ep.Kind == iface.InferenceKindEmbed {
			env.embedID = ep.Profile().ID
		} else {
			env.rerankID = ep.Profile().ID
		}
	}
	return env
}

func defaultInstallationEnv(t *testing.T) *installationEnv {
	return newInstallationEnv(t, embedFake, rerankFake, newMemBudgetStore(0, ""))
}

func (e *installationEnv) embedReq() iface.PinnedEmbedRequest {
	return iface.PinnedEmbedRequest{Caller: "retrieval", OnBehalfOf: "t1", Profile: e.embedID, Kind: iface.EmbedInputDocument, Inputs: []string{"a", "bb"}}
}

func (e *installationEnv) rerankReq() iface.RerankRequest {
	return iface.RerankRequest{Caller: "retrieval", OnBehalfOf: "t1", Profile: e.rerankID, Query: "q", Documents: []string{"a", "ccc", "bb"}}
}

// noCalls asserts that nothing was ledgered and no inference call (beyond
// the verification probe) reached an endpoint.
func (e *installationEnv) noCalls(t *testing.T, label string) {
	t.Helper()
	if len(e.store.ledger) != 0 {
		t.Fatalf("%s: ledger rows = %d", label, len(e.store.ledger))
	}
	if e.embed.InfoCalls() != 0 || len(e.embed.EmbedCalls()) != 0 || e.rerank.InfoCalls() != 0 || len(e.rerank.RerankCalls()) != 0 {
		t.Fatalf("%s: endpoints were contacted", label)
	}
}

func TestGateway_ImplementsTheInstallationInterfaces(t *testing.T) {
	var gw iface.LLMGateway = newGatewayForTest(t)
	_, a := gw.(iface.LLMInferenceProfiles)
	_, b := gw.(iface.LLMPinnedEmbedder)
	_, c := gw.(iface.LLMReranker)
	if !a || !b || !c {
		t.Fatalf("assertions = %v %v %v", a, b, c)
	}
	g := newGatewayForTest(t)
	ctx := ctxFor("t1")
	req := iface.PinnedEmbedRequest{Caller: "retrieval", OnBehalfOf: "t1", Profile: "ip_" + strings.Repeat("0", 64), Kind: iface.EmbedInputQuery, Inputs: []string{"a"}}
	rr := iface.RerankRequest{Caller: "retrieval", OnBehalfOf: "t1", Profile: req.Profile, Query: "q", Documents: []string{"a"}}
	if _, err := g.InferenceProfile(ctx, "embed"); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("InferenceProfile err = %v", err)
	}
	if _, err := g.EmbedWithProfile(ctx, req); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("EmbedWithProfile err = %v", err)
	}
	if _, err := g.Rerank(ctx, rr); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("Rerank err = %v", err)
	}
}

func TestInstallation_EmbedWithProfileLedgersOneRowPerCall(t *testing.T) {
	env := defaultInstallationEnv(t)
	res, err := env.g.EmbedWithProfile(ctxFor("t1"), env.embedReq())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Vectors) != 2 || res.Dimensions != 4 {
		t.Fatalf("result = %+v", res)
	}
	calls := env.embed.EmbedCalls()[1:] // the first call is the verification probe
	if len(calls) != 1 || len(calls[0].Inputs) != 2 || calls[0].Inputs[0] != "passage: a" || calls[0].Inputs[1] != "passage: bb" || !calls[0].Normalize || !calls[0].Truncate {
		t.Fatalf("embed calls = %+v", calls)
	}
	wantModel := iface.LLMModelInfo{UUID: "installation:embed", Name: "embed", Provider: "tei", ModelID: "BAAI/bge-m3",
		Capabilities: iface.LLMCapabilities{Embeddings: true, Dimensions: 4}, CredentialKind: "installation", Purposes: []string{"retrieval.embed"}}
	if res.Model.UUID != wantModel.UUID || res.Model.Name != wantModel.Name || res.Model.Provider != wantModel.Provider || res.Model.ModelID != wantModel.ModelID ||
		res.Model.Capabilities != wantModel.Capabilities || res.Model.CredentialKind != wantModel.CredentialKind ||
		len(res.Model.Purposes) != 1 || res.Model.Purposes[0] != "retrieval.embed" {
		t.Fatalf("model = %+v", res.Model)
	}
	row := singleLedger(t, env.store)
	if row.Caller != "retrieval" || row.Purpose != "retrieval.embed" || row.ModelUUID != "installation:embed" || row.Provider != "tei" ||
		row.ModelID != "BAAI/bge-m3" || row.CredentialKind != "installation" || row.CredentialUUID != "" || row.UserUUID != "u-admin" ||
		row.TenantID != "t1" || row.Outcome != models.OutcomeOK || row.InputTokens != 21 {
		t.Fatalf("row = %+v", row)
	}
	if len(row.Attempts) != 1 || row.Attempts[0] != (models.Attempt{Provider: "tei", CredentialKind: "installation", Outcome: "ok", InputTokens: 21}) {
		t.Fatalf("attempts = %+v", row.Attempts)
	}
	if got := env.store.month(models.PeriodOf(time.Now())).OrgInputTokens; got != 21 {
		t.Fatalf("org input tokens = %d", got)
	}
	if env.metrics.requests["tei/ok"] != 1 {
		t.Fatalf("metrics = %+v", env.metrics.requests)
	}

	q := env.embedReq()
	q.Kind, q.Inputs = iface.EmbedInputQuery, []string{"a"}
	if _, err := env.g.EmbedWithProfile(ctxFor("t1"), q); err != nil {
		t.Fatal(err)
	}
	if calls := env.embed.EmbedCalls(); calls[len(calls)-1].Inputs[0] != "query: a" {
		t.Fatalf("query call = %+v", calls[len(calls)-1])
	}
}

func TestInstallation_IndexingCallWithoutUserBillsTheOrg(t *testing.T) {
	env := defaultInstallationEnv(t)
	if _, err := env.g.EmbedWithProfile(ctxWithTenant(context.Background(), "t1"), env.embedReq()); err != nil {
		t.Fatal(err)
	}
	row := singleLedger(t, env.store)
	if row.UserUUID != "" || row.TenantID != "t1" {
		t.Fatalf("row = %+v", row)
	}
	if env.store.month(models.PeriodOf(time.Now())).OrgInputTokens <= 0 {
		t.Fatal("the org was not billed")
	}
}

func TestInstallation_RefusesBeforeAnyCall(t *testing.T) {
	long := strings.Repeat("x", 64<<10+1)
	many := make([]string, 257)
	for i := range many {
		many[i] = "x"
	}
	embedCases := map[string]struct {
		ctx  func(*testing.T) context.Context
		mut  func(*iface.PinnedEmbedRequest)
		want error
	}{
		"other caller":    {nil, func(r *iface.PinnedEmbedRequest) { r.Caller = "crm" }, iface.ErrLLMModelAccessDenied},
		"empty caller":    {nil, func(r *iface.PinnedEmbedRequest) { r.Caller = "" }, iface.ErrLLMModelAccessDenied},
		"other tenant":    {nil, func(r *iface.PinnedEmbedRequest) { r.OnBehalfOf = "t2" }, iface.ErrLLMInvalidRequest},
		"no onBehalfOf":   {nil, func(r *iface.PinnedEmbedRequest) { r.OnBehalfOf = "" }, iface.ErrLLMInvalidRequest},
		"external tenant": {ctxauthExternal, func(r *iface.PinnedEmbedRequest) { r.OnBehalfOf = "ext-1" }, tenantrepo.ErrTenantKindMismatch},
		"bad profile":     {nil, func(r *iface.PinnedEmbedRequest) { r.Profile = "bogus" }, iface.ErrLLMInvalidRequest},
		"no kind":         {nil, func(r *iface.PinnedEmbedRequest) { r.Kind = "" }, iface.ErrLLMInvalidRequest},
		"no inputs":       {nil, func(r *iface.PinnedEmbedRequest) { r.Inputs = nil }, iface.ErrLLMInvalidRequest},
		"too many inputs": {nil, func(r *iface.PinnedEmbedRequest) { r.Inputs = many }, iface.ErrLLMInvalidRequest},
		"oversized input": {nil, func(r *iface.PinnedEmbedRequest) { r.Inputs = []string{long} }, iface.ErrLLMInvalidRequest},
		"empty input":     {nil, func(r *iface.PinnedEmbedRequest) { r.Inputs = []string{"a", ""} }, iface.ErrLLMInvalidRequest},
	}
	for name, tc := range embedCases {
		t.Run("embed/"+name, func(t *testing.T) {
			env := defaultInstallationEnv(t)
			req := env.embedReq()
			tc.mut(&req)
			ctx := ctxFor("t1")
			if tc.ctx != nil {
				ctx = tc.ctx(t)
			}
			if _, err := env.g.EmbedWithProfile(ctx, req); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			env.noCalls(t, name)
		})
	}
	rerankCases := map[string]func(*iface.RerankRequest){
		"no query":       func(r *iface.RerankRequest) { r.Query = "" },
		"no documents":   func(r *iface.RerankRequest) { r.Documents = nil },
		"too many docs":  func(r *iface.RerankRequest) { r.Documents = many },
		"negative topN":  func(r *iface.RerankRequest) { r.TopN = -1 },
		"topN too large": func(r *iface.RerankRequest) { r.TopN = len(r.Documents) + 1 },
	}
	for name, mut := range rerankCases {
		t.Run("rerank/"+name, func(t *testing.T) {
			env := defaultInstallationEnv(t)
			req := env.rerankReq()
			mut(&req)
			if _, err := env.g.Rerank(ctxFor("t1"), req); !errors.Is(err, iface.ErrLLMInvalidRequest) {
				t.Fatalf("err = %v", err)
			}
			env.noCalls(t, name)
		})
	}
}

func TestInstallation_ProfileResolution(t *testing.T) {
	env := defaultInstallationEnv(t)
	ctx := ctxFor("t1")
	rr := env.rerankReq()
	rr.Profile = env.embedID
	if _, err := env.g.Rerank(ctx, rr); !errors.Is(err, iface.ErrLLMInvalidRequest) {
		t.Fatalf("rerank with an embed profile err = %v", err)
	}
	er := env.embedReq()
	er.Profile = "ip_" + strings.Repeat("0", 64)
	if _, err := env.g.EmbedWithProfile(ctx, er); !errors.Is(err, iface.ErrLLMProfileMismatch) {
		t.Fatalf("unknown profile err = %v", err)
	}
	env.noCalls(t, "profile resolution")

	empty := buildInstallationEnv(t, nil, teitest.New(t, embedFake), teitest.New(t, rerankFake), newMemBudgetStore(0, ""))
	if _, err := empty.g.EmbedWithProfile(ctx, env.embedReq()); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("empty set embed err = %v", err)
	}
	if _, err := empty.g.Rerank(ctx, env.rerankReq()); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("empty set rerank err = %v", err)
	}
	if _, err := empty.g.InferenceProfile(ctx, "embed"); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("empty set profile err = %v", err)
	}
	empty.noCalls(t, "empty set")
}

func TestInstallation_InferenceProfileNeedsNoTenant(t *testing.T) {
	env := defaultInstallationEnv(t)
	p, err := env.g.InferenceProfile(context.Background(), "embed")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != env.embedID || p.Boundary != iface.InferenceBoundaryLocal || p.Dimensions != 4 || p.MaxInputTokens != 8192 {
		t.Fatalf("profile = %+v", p)
	}
	p.Dimensions = 99 // a copy: the cache is untouched
	if again, _ := env.g.InferenceProfile(context.Background(), "embed"); again.Dimensions != 4 {
		t.Fatalf("profile was shared: %+v", again)
	}
}

func TestInstallation_EndpointSwappedUnderARunningProfile(t *testing.T) {
	env := defaultInstallationEnv(t)
	ctx := ctxFor("t1")
	if _, err := env.g.EmbedWithProfile(ctx, env.embedReq()); err != nil {
		t.Fatal(err)
	}
	swapped := embedFake
	swapped.Dimensions = 8
	env.embed.SetConfig(swapped)
	if _, err := env.g.EmbedWithProfile(ctx, env.embedReq()); !errors.Is(err, iface.ErrLLMProfileMismatch) {
		t.Fatalf("swapped dimensions err = %v", err)
	}
	if len(env.store.ledger) != 2 {
		t.Fatalf("ledger rows = %d, want 2", len(env.store.ledger))
	}
	var failed *models.UsageEvent
	for _, ev := range env.store.ledger {
		if ev.Outcome == models.OutcomeError {
			failed = ev
		}
	}
	if failed == nil || failed.ErrorCode != "profile_mismatch" || failed.InputTokens == 0 {
		t.Fatalf("failed row = %+v", failed)
	}
	if _, err := env.g.EmbedWithProfile(ctx, env.embedReq()); !errors.Is(err, iface.ErrLLMProfileMismatch) {
		t.Fatalf("after invalidation err = %v", err)
	}
	if len(env.store.ledger) != 2 {
		t.Fatalf("verification failure ledgered a row: %d", len(env.store.ledger))
	}

	fresh := defaultInstallationEnv(t)
	moved := embedFake
	moved.Revision = "new"
	fresh.embed.SetConfig(moved)
	fresh.clock.Advance(61 * time.Second)
	if _, err := fresh.g.EmbedWithProfile(ctx, fresh.embedReq()); !errors.Is(err, iface.ErrLLMProfileMismatch) {
		t.Fatalf("new revision embed err = %v", err)
	}
	if _, err := fresh.g.InferenceProfile(ctx, "embed"); !errors.Is(err, iface.ErrLLMProfileMismatch) {
		t.Fatalf("new revision profile err = %v", err)
	}
	if len(fresh.store.ledger) != 0 {
		t.Fatalf("ledger rows = %d", len(fresh.store.ledger))
	}
}

func TestInstallation_RerankOrdersAndCutsTopN(t *testing.T) {
	env := defaultInstallationEnv(t)
	req := env.rerankReq()
	req.TopN = 2
	res, err := env.g.Rerank(ctxFor("t1"), req)
	if err != nil {
		t.Fatal(err)
	}
	want := []iface.RerankScore{{Index: 1, Score: 0.003}, {Index: 2, Score: 0.002}}
	if len(res.Results) != 2 || res.Results[0] != want[0] || res.Results[1] != want[1] || res.Profile != env.rerankID || res.Usage.InputTokens <= 0 {
		t.Fatalf("result = %+v", res)
	}
	row := singleLedger(t, env.store)
	if row.Purpose != "retrieval.rerank" || row.ModelUUID != "installation:rerank" || row.ModelID != "BAAI/bge-reranker-v2-m3" ||
		row.Provider != "tei" || row.CredentialKind != "installation" || row.Outcome != models.OutcomeOK {
		t.Fatalf("row = %+v", row)
	}
	req.TopN = 0
	if all, err := env.g.Rerank(ctxFor("t1"), req); err != nil || len(all.Results) != 3 {
		t.Fatalf("TopN 0 = %+v, %v", all, err)
	}
}

func TestInstallation_BudgetExceededBeforeAnyCall(t *testing.T) {
	env := newInstallationEnv(t, embedFake, rerankFake, newMemBudgetStore(1, models.BudgetActionBlock))
	if _, err := env.g.EmbedWithProfile(ctxFor("t1"), env.embedReq()); !errors.Is(err, iface.ErrLLMBudgetExceeded) {
		t.Fatalf("err = %v", err)
	}
	row := singleLedger(t, env.store)
	if row.Outcome != models.OutcomeBudgetBlocked {
		t.Fatalf("row = %+v", row)
	}
	if n := len(env.embed.EmbedCalls()); n != 1 { // the verification probe only
		t.Fatalf("embed calls = %d", n)
	}
	if env.metrics.requests["tei/budget_blocked"] != 1 {
		t.Fatalf("metrics = %+v", env.metrics.requests)
	}
}

func TestInstallation_UnavailableEndpointOpensTheBreaker(t *testing.T) {
	env := defaultInstallationEnv(t)
	ctx := ctxFor("t1")
	if _, err := env.g.EmbedWithProfile(ctx, env.embedReq()); err != nil {
		t.Fatal(err)
	}
	env.embed.Fail("/embed", 0, 503)
	for i := range 3 {
		if _, err := env.g.EmbedWithProfile(ctx, env.embedReq()); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
			t.Fatalf("call %d err = %v", i, err)
		}
	}
	rows := 4 // the first success and three failures
	if len(env.store.ledger) != rows {
		t.Fatalf("ledger rows = %d", len(env.store.ledger))
	}
	failures := 0
	for _, ev := range env.store.ledger {
		if ev.ErrorCode == "provider_unavailable" {
			failures++
		}
	}
	if failures != 3 {
		t.Fatalf("provider_unavailable rows = %d", failures)
	}
	before := len(env.embed.EmbedCalls())
	if _, err := env.g.EmbedWithProfile(ctx, env.embedReq()); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("open breaker err = %v", err)
	}
	if len(env.embed.EmbedCalls()) != before || len(env.store.ledger) != rows {
		t.Fatalf("the open breaker still called the endpoint or ledgered a row")
	}
}

func TestInstallation_EndpointsNeverReachTheOrgCatalog(t *testing.T) {
	env := defaultInstallationEnv(t)
	ctx := ctxFor("t1")
	list, err := env.g.ListUsable(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListUsable = %+v, %v", list, err)
	}
	for _, m := range list {
		if m.Provider == "tei" || strings.HasPrefix(m.UUID, "installation:") {
			t.Fatalf("installation endpoint in the org catalog: %+v", m)
		}
	}
	other := ctxFor("t9")
	if l, err := env.g.ListUsable(other); err != nil || len(l) != 0 {
		t.Fatalf("t9 ListUsable = %+v, %v", l, err)
	}
	if _, err := env.g.Resolve(other, "default"); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("t9 Resolve err = %v", err)
	}
	if _, err := env.g.Embed(other, iface.EmbedRequest{Caller: "crm", Inputs: []string{"a"}}); !errors.Is(err, iface.ErrLLMNotConfigured) {
		t.Fatalf("t9 Embed err = %v", err)
	}
	_, err = env.g.catalog.CreateCredential(ctx, models.CredentialInput{Name: "T", Provider: "tei", BaseURL: "http://tei:80"})
	if !errors.Is(err, iface.ErrLLMInvalidRequest) || !errors.Is(err, models.ErrInvalidProvider) {
		t.Fatalf("CreateCredential err = %v", err)
	}
}

func TestInstallation_NilMetricsDefaultsToNoop(t *testing.T) {
	env := defaultInstallationEnv(t)
	svc := NewInstallationService(env.set, NewBudgetService(env.store, fixedBudgetCfg, slog.Default()), nil, func() RouterConfig { return RouterConfig{} }, nil, slog.Default())
	g := newGatewayForTest(t)
	g.SetInstallation(svc)
	if _, err := g.EmbedWithProfile(ctxFor("t1"), env.embedReq()); err != nil {
		t.Fatal(err)
	}
}
