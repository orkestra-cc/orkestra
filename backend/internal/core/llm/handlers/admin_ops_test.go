package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/internal/core/llm/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- fakes for the budget path ------------------------------------------------

// ledgerStore is a services.BudgetStore that admits every call (or refuses
// all of them when full) and keeps the ledger rows it was handed.
type ledgerStore struct {
	mu   sync.Mutex
	full bool
	rows map[string]*models.UsageEvent
}

func (s *ledgerStore) GetBudget(context.Context) (*models.Budget, error) {
	if s.full {
		return &models.Budget{MonthlyTokenLimit: 1, Action: models.BudgetActionBlock}, nil
	}
	return nil, repository.ErrNotFound
}

func (s *ledgerStore) Reserve(_ context.Context, _ string, ev *models.UsageEvent, _ int64, block bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *ev
	if s.full && block {
		cp.Outcome = models.OutcomeBudgetBlocked
		s.rows[cp.UUID] = &cp
		return false, nil
	}
	s.rows[cp.UUID] = &cp
	return true, nil
}

func (s *ledgerStore) Finalize(_ context.Context, _ string, uuid string, upd models.FinalizeUpdate) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[uuid]
	if !ok || r.Outcome != models.OutcomePending {
		return nil, repository.ErrAlreadyFinalized
	}
	r.Outcome, r.InputTokens, r.OutputTokens = upd.Outcome, upd.InputTokens, upd.OutputTokens
	return nil, nil
}

// counterRedis is the slice of module.RedisClient the rate limiter uses.
type counterRedis struct {
	mu sync.Mutex
	n  map[string]int64
}

func (r *counterRedis) Get(context.Context, string) (string, error) { return "", errors.New("nil") }
func (r *counterRedis) Set(context.Context, string, interface{}, time.Duration) error {
	return nil
}
func (r *counterRedis) Del(context.Context, ...string) error                { return nil }
func (r *counterRedis) Keys(context.Context, string) ([]string, error)      { return nil, nil }
func (r *counterRedis) Expire(context.Context, string, time.Duration) error { return nil }
func (r *counterRedis) Incr(_ context.Context, key string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n[key]++
	return r.n[key], nil
}

// fakeUsage answers fixed rows and remembers which user a self query was
// for, so a test can prove the caller's own UUID reached the repository.
type fakeUsage struct {
	mu       sync.Mutex
	askedFor []string
}

func (*fakeUsage) Query(context.Context, time.Time, time.Time, string) ([]models.LLMUsageRow, error) {
	return []models.LLMUsageRow{{Key: "m1", Requests: 2, InputTokens: 30, OutputTokens: 5}}, nil
}
func (f *fakeUsage) UserMonth(_ context.Context, userUUID, period string) (*models.LLMSelfUsage, error) {
	f.mu.Lock()
	f.askedFor = append(f.askedFor, userUUID)
	f.mu.Unlock()
	return &models.LLMSelfUsage{Period: period, Requests: 1, OrgInputTokens: 7}, nil
}

type recordingAudit struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (r *recordingAudit) Emit(_ context.Context, ev iface.AuditEvent) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *recordingAudit) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.Action)
	}
	return out
}

type emptyMonthly struct{}

func (emptyMonthly) Get(_ context.Context, period string) (*models.UsageMonthly, error) {
	return &models.UsageMonthly{Period: period, OrgInputTokens: 30}, nil
}

type memBudgets struct{ b map[string]models.Budget }

func (m *memBudgets) Get(ctx context.Context) (*models.Budget, error) {
	if b, ok := m.b[tenantOf(ctx)]; ok {
		return &b, nil
	}
	return nil, repository.ErrNotFound
}
func (m *memBudgets) Put(ctx context.Context, b *models.Budget) error {
	b.TenantID = tenantOf(ctx)
	m.b[b.TenantID] = *b
	return nil
}

// --- harness -----------------------------------------------------------------

type opsHarness struct {
	ops     *OpsHandler
	usage   *UsageHandler
	admin   *AdminHandler
	creds   *fakeCreds
	mods    *fakeModels
	ledger  *ledgerStore
	budgets *memBudgets
	cfg     *services.CatalogConfig
	usageDB *fakeUsage
	audit   *recordingAudit
	ctx     context.Context
}

func newOpsHarness(t *testing.T) *opsHarness {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	vault, err := services.NewVault(testKeyHex, logger)
	if err != nil {
		t.Fatal(err)
	}
	h := &opsHarness{
		creds:   &fakeCreds{rows: map[string]models.Credential{}},
		mods:    &fakeModels{rows: map[string]models.Model{}},
		ledger:  &ledgerStore{rows: map[string]*models.UsageEvent{}},
		budgets: &memBudgets{b: map[string]models.Budget{}},
		cfg:     &services.CatalogConfig{AllowHosted: true},
		usageDB: &fakeUsage{},
		audit:   &recordingAudit{},
	}
	grants := &fakeGrants{rows: map[string]map[string][]string{}, models: h.mods}
	dir := &fakeDir{members: map[string][]string{"t1": {"u-admin", "u1", "u2"}}}
	cfg := func() services.CatalogConfig { return *h.cfg }
	catalog := services.NewCatalogService(h.creds, h.mods, grants, vault, dir, cfg, logger)
	catalog.SetAuditSink(h.audit)
	// Only the mock adapter: any other provider kind has no adapter here.
	reg := providers.NewRegistry()
	reg.Register(models.ProviderMock, func(c providers.Credential, _ *http.Client) (providers.Provider, error) {
		return providers.NewMock(c, nil)
	})
	gw := services.NewGateway(services.NewAccessResolver(h.mods, grants, h.creds, cfg), catalog, reg, vault, logger)
	bcfg := func() services.BudgetConfig {
		return services.BudgetConfig{ReserveOutputTokens: 64, ReservationTTL: 2 * time.Minute, WarnPct: 80}
	}
	budget := services.NewBudgetService(h.ledger, bcfg, logger)
	clients := func(string) *http.Client { return &http.Client{} }
	gw.SetRouter(services.NewRouter(reg, services.NewOrgCredentialResolver(catalog), budget, services.NewBreaker(3, 30*time.Second), clients,
		func() services.RouterConfig { return services.RouterConfig{RequestTimeout: 5 * time.Second} }, nil, logger))
	limiter := services.NewRateLimiter(&counterRedis{n: map[string]int64{}}, logger)
	h.ops = NewOpsHandler(services.NewAdminOps(catalog, reg, clients, limiter, services.NewPinnedRunner(gw), logger), logger)
	h.usage = NewUsageHandler(services.NewUsageService(h.usageDB, emptyMonthly{}, h.budgets, dir, catalog, bcfg, logger), logger)
	h.admin = NewAdminHandler(catalog, logger)
	id := testkit.NewIdentity("u-admin", "admin@example.test", "administrator").WithTenant("t1", []string{"org_owner"}, true)
	h.ctx = ctxauth.WithTenantKind(id.ContextFor(context.Background(), "t1"), "internal")
	return h
}

func (h *opsHarness) credential(t *testing.T, provider, baseURL, secret string) string {
	t.Helper()
	resp, err := h.admin.CreateCredential(h.ctx, &LLMCredentialCreateRequest{Body: models.LLMCredentialCreateBody{
		Name: provider + "-cred", Provider: provider, BaseURL: baseURL, Secret: secret,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Body.UUID
}

func (h *opsHarness) mockModel(t *testing.T) string {
	t.Helper()
	cred := h.credential(t, models.ProviderMock, "", "")
	resp, err := h.admin.CreateModel(h.ctx, &LLMModelCreateRequest{Body: models.LLMModelBody{
		Name: "Mock", Provider: models.ProviderMock, ModelID: "mock-1",
		Capabilities:  models.LLMModelCapabilities{Chat: true},
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: cred},
		Purposes:      []models.LLMModelPurpose{{Purpose: "default", Priority: 1}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Body.UUID
}

func headerOf(t *testing.T, err error, name string) string {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("not an errcode.Error: %v", err)
	}
	return e.GetHeaders().Get(name)
}

// --- credential test / discovery ----------------------------------------------

func TestOps_TestCredentialMockOKIsPersisted(t *testing.T) {
	h := newOpsHarness(t)
	id := h.credential(t, models.ProviderMock, "", "")
	resp, err := h.ops.TestCredential(h.ctx, &LLMCredentialPath{UUID: id})
	if err != nil || resp.Body.Outcome != models.TestStatusOK || resp.Body.Code != "" {
		t.Fatalf("test = %+v, %v", resp, err)
	}
	row := h.creds.rows[id]
	if row.LastTestStatus != models.TestStatusOK || row.LastTestedAt == nil || row.LastTestError != "" {
		t.Fatalf("persisted = %v %q %q", row.LastTestedAt, row.LastTestStatus, row.LastTestError)
	}
}

func TestOps_TestCredentialWithoutAdapterRecordsProviderUnavailable(t *testing.T) {
	h := newOpsHarness(t)
	id := h.credential(t, models.ProviderOllama, "http://ollama.internal:11434", "")
	resp, err := h.ops.TestCredential(h.ctx, &LLMCredentialPath{UUID: id})
	if err != nil || resp.Body.Outcome != models.TestStatusError || resp.Body.Code != errcode.LLMProviderUnavailable {
		t.Fatalf("test = %+v, %v", resp, err)
	}
	if row := h.creds.rows[id]; row.LastTestStatus != models.TestStatusError || row.LastTestError != errcode.LLMProviderUnavailable {
		t.Fatalf("persisted = %q %q", row.LastTestStatus, row.LastTestError)
	}
}

// Ruling 5.3: with allow_hosted off nothing reaches a cloud provider.
func TestOps_HostedGateAppliesToTestAndDiscovery(t *testing.T) {
	h := newOpsHarness(t)
	id := h.credential(t, models.ProviderOpenAI, "", "sk-live-abcdef")
	h.cfg.AllowHosted = false
	_, err := h.ops.TestCredential(h.ctx, &LLMCredentialPath{UUID: id})
	if status, code, _ := codeOf(t, err); status != http.StatusUnprocessableEntity || code != errcode.LLMHostedDisabled {
		t.Fatalf("test = %d %s", status, code)
	}
	if row := h.creds.rows[id]; row.LastTestedAt != nil {
		t.Fatal("a refused test must not be recorded")
	}
	_, err = h.ops.DiscoverModels(h.ctx, &LLMCredentialPath{UUID: id})
	if status, code, _ := codeOf(t, err); status != http.StatusUnprocessableEntity || code != errcode.LLMHostedDisabled {
		t.Fatalf("discover = %d %s", status, code)
	}
}

func TestOps_TestCredentialRateLimited(t *testing.T) {
	h := newOpsHarness(t)
	id := h.credential(t, models.ProviderMock, "", "")
	for i := 0; i < 10; i++ {
		if _, err := h.ops.TestCredential(h.ctx, &LLMCredentialPath{UUID: id}); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	_, err := h.ops.TestCredential(h.ctx, &LLMCredentialPath{UUID: id})
	if status, code, _ := codeOf(t, err); status != http.StatusTooManyRequests || code != errcode.LLMRateLimited {
		t.Fatalf("eleventh = %d %s", status, code)
	}
	secs, convErr := strconv.Atoi(headerOf(t, err, "Retry-After"))
	if convErr != nil || secs < 1 || secs > 61 {
		t.Fatalf("Retry-After = %q", headerOf(t, err, "Retry-After"))
	}
	// Discovery has its own counter.
	if _, err := h.ops.DiscoverModels(h.ctx, &LLMCredentialPath{UUID: id}); err != nil {
		t.Fatalf("discover after test limit: %v", err)
	}
}

func TestOps_DiscoverModelsMock(t *testing.T) {
	h := newOpsHarness(t)
	id := h.credential(t, models.ProviderMock, "", "")
	resp, err := h.ops.DiscoverModels(h.ctx, &LLMCredentialPath{UUID: id})
	if err != nil || len(resp.Body.Items) != 2 || resp.Body.Items[0].ID != "mock-1" {
		t.Fatalf("discover = %+v, %v", resp, err)
	}
	if _, err := h.ops.DiscoverModels(h.ctx, &LLMCredentialPath{UUID: "0b6e3f6a-7a4c-4a4e-9a51-6d1c2b7f8e90"}); err == nil {
		t.Fatal("unknown credential must fail")
	} else if status, code, _ := codeOf(t, err); status != http.StatusNotFound || code != errcode.LLMNotFound {
		t.Fatalf("unknown = %d %s", status, code)
	}
}

// --- prompt test ----------------------------------------------------------------

func TestOps_PromptTestBypassesGrantsAndLedgersAdminCaller(t *testing.T) {
	h := newOpsHarness(t)
	model := h.mockModel(t) // access granted, no grants: u-admin may not use it
	resp, err := h.ops.PromptTest(h.ctx, &LLMPromptTestRequest{UUID: model, Body: models.LLMPromptTestBody{Prompt: "hello there"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body.Text != "mock: hello there" || resp.Body.FinishReason != "stop" || resp.Body.Usage.OutputTokens == 0 {
		t.Fatalf("prompt = %+v", resp.Body)
	}
	if len(h.ledger.rows) != 1 {
		t.Fatalf("ledger rows = %d", len(h.ledger.rows))
	}
	for _, r := range h.ledger.rows {
		if r.Caller != services.AdminTestCaller || r.UserUUID != "u-admin" || r.Outcome != models.OutcomeOK || r.ModelUUID != model {
			t.Fatalf("ledger = %+v", r)
		}
	}
}

func TestOps_PromptTestRefusesNonLiveModels(t *testing.T) {
	h := newOpsHarness(t)
	model := h.mockModel(t)
	disabled := models.ModelStatusDisabled
	if _, err := h.admin.PatchModel(h.ctx, &LLMModelPatchRequest{UUID: model, Body: models.LLMModelPatchBody{Status: &disabled}}); err != nil {
		t.Fatal(err)
	}
	_, err := h.ops.PromptTest(h.ctx, &LLMPromptTestRequest{UUID: model, Body: models.LLMPromptTestBody{Prompt: "hi"}})
	if status, code, _ := codeOf(t, err); status != http.StatusUnprocessableEntity || code != errcode.LLMInvalidRequest {
		t.Fatalf("disabled = %d %s", status, code)
	}
	if len(h.ledger.rows) != 0 {
		t.Fatal("a refused prompt test must not reserve")
	}
}

func TestOps_PromptTestBudgetExceeded(t *testing.T) {
	h := newOpsHarness(t)
	model := h.mockModel(t)
	h.ledger.full = true
	_, err := h.ops.PromptTest(h.ctx, &LLMPromptTestRequest{UUID: model, Body: models.LLMPromptTestBody{Prompt: "hi"}})
	if status, code, _ := codeOf(t, err); status != http.StatusTooManyRequests || code != errcode.LLMBudgetExceeded {
		t.Fatalf("budget = %d %s", status, code)
	}
	if ra := headerOf(t, err, "Retry-After"); ra != "3600" {
		t.Fatalf("Retry-After = %q", ra)
	}
}

func TestOps_PromptTestRateLimitedPerUser(t *testing.T) {
	h := newOpsHarness(t)
	model := h.mockModel(t)
	for i := 0; i < 10; i++ {
		if _, err := h.ops.PromptTest(h.ctx, &LLMPromptTestRequest{UUID: model, Body: models.LLMPromptTestBody{Prompt: "hi"}}); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	_, err := h.ops.PromptTest(h.ctx, &LLMPromptTestRequest{UUID: model, Body: models.LLMPromptTestBody{Prompt: "hi"}})
	if status, code, _ := codeOf(t, err); status != http.StatusTooManyRequests || code != errcode.LLMRateLimited {
		t.Fatalf("eleventh = %d %s", status, code)
	}
}

// --- usage and budget -------------------------------------------------------------

func TestUsage_ReportBudgetAndSelf(t *testing.T) {
	h := newOpsHarness(t)
	rep, err := h.usage.GetUsage(h.ctx, &LLMUsageQuery{})
	if err != nil || rep.Body.GroupBy != "model" || len(rep.Body.Rows) != 1 || rep.Body.Budget != nil || rep.Body.Month.OrgInputTokens != 30 {
		t.Fatalf("report = %+v, %v", rep, err)
	}
	if got := rep.Body.To.Sub(rep.Body.From); got != defaultUsageWindow {
		t.Fatalf("default window = %v", got)
	}
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err = h.usage.GetUsage(h.ctx, &LLMUsageQuery{From: from, To: from.AddDate(1, 1, 0), GroupBy: "user"})
	if status, code, _ := codeOf(t, err); status != http.StatusUnprocessableEntity || code != errcode.LLMInvalidRequest {
		t.Fatalf("too long = %d %s", status, code)
	}

	none, err := h.usage.GetBudget(h.ctx, nil)
	if err != nil || none.Status != http.StatusNoContent || none.Body != nil {
		t.Fatalf("no budget = %+v, %v", none, err)
	}
	_, err = h.usage.PutBudget(h.ctx, &LLMBudgetPutRequest{Body: models.LLMBudgetPutBody{MonthlyTokenLimit: 1000, Action: models.BudgetActionBlock, NotifyExtraUserUUIDs: []string{"u1", "stranger"}}})
	if status, code, _ := codeOf(t, err); status != http.StatusUnprocessableEntity || code != errcode.LLMInvalidRequest {
		t.Fatalf("non-member = %d %s", status, code)
	}
	if len(h.budgets.b) != 0 {
		t.Fatal("a refused budget must write nothing")
	}
	put, err := h.usage.PutBudget(h.ctx, &LLMBudgetPutRequest{Body: models.LLMBudgetPutBody{MonthlyTokenLimit: 1000, Action: models.BudgetActionWarnOnly, NotifyExtraUserUUIDs: []string{"u1", "u1"}}})
	if err != nil || put.Body.WarnPct != 80 || put.Body.UpdatedBy != "u-admin" || len(put.Body.NotifyExtraUserUUIDs) != 1 {
		t.Fatalf("put = %+v, %v", put, err)
	}
	got, err := h.usage.GetBudget(h.ctx, nil)
	if err != nil || got.Status != http.StatusOK || got.Body.MonthlyTokenLimit != 1000 || got.Body.Action != models.BudgetActionWarnOnly {
		t.Fatalf("get = %+v, %v", got, err)
	}
	rep, err = h.usage.GetUsage(h.ctx, &LLMUsageQuery{GroupBy: "caller"})
	if err != nil || rep.Body.Budget == nil || rep.Body.Budget.MonthlyTokenLimit != 1000 {
		t.Fatalf("report with budget = %+v, %v", rep, err)
	}

	me, err := h.usage.MyUsage(h.ctx, nil)
	if err != nil || me.Body.Period != models.PeriodOf(time.Now()) || me.Body.OrgInputTokens != 7 {
		t.Fatalf("me = %+v, %v", me, err)
	}
	if got := h.usageDB.askedFor; len(got) != 1 || got[0] != "u-admin" {
		t.Fatalf("self usage asked for %v, want the caller u-admin only", got)
	}
	var budgetEvents []iface.AuditEvent
	for _, e := range h.audit.events {
		if e.Action == "llm.budget.updated" {
			budgetEvents = append(budgetEvents, e)
		}
	}
	if len(budgetEvents) != 1 {
		t.Fatalf("llm.budget.updated events = %d (a refused PUT must not audit): %v", len(budgetEvents), h.audit.actions())
	}
	if e := budgetEvents[0]; e.TenantID != "t1" || e.ActorUserID != "u-admin" || e.Metadata["action"] != models.BudgetActionWarnOnly || e.Metadata["monthlyTokenLimit"] != int64(1000) || e.Metadata["warnPct"] != 80 {
		t.Fatalf("budget audit = %+v", e)
	}
}

func TestUsage_RefusesExternalTenant(t *testing.T) {
	h := newOpsHarness(t)
	id := testkit.NewIdentity("u-ext", "ext@example.test", "guest").WithTenant("ext-1", []string{"org_member"}, true)
	ext := ctxauth.WithTenantKind(id.ContextFor(context.Background(), "ext-1"), "external")
	for name, call := range map[string]func() error{
		"me":     func() error { _, err := h.usage.MyUsage(ext, nil); return err },
		"usage":  func() error { _, err := h.usage.GetUsage(ext, &LLMUsageQuery{}); return err },
		"budget": func() error { _, err := h.usage.GetBudget(ext, nil); return err },
	} {
		if status, _, _ := codeOf(t, call()); status != http.StatusForbidden {
			t.Fatalf("%s from an external tenant = %d", name, status)
		}
	}
}

// Huma's maxLength counts characters: a 4000-rune non-ASCII prompt (8000
// bytes) is accepted, the 4001st rune is refused.
func TestOps_PromptLengthCountsRunes(t *testing.T) {
	h := newOpsHarness(t)
	model := h.mockModel(t)
	ok := strings.Repeat("é", 4000)
	resp, err := h.ops.PromptTest(h.ctx, &LLMPromptTestRequest{UUID: model, Body: models.LLMPromptTestBody{Prompt: ok}})
	if err != nil || resp.Body.Text != "mock: "+ok {
		t.Fatalf("4000 runes: %v", err)
	}
	_, err = h.ops.PromptTest(h.ctx, &LLMPromptTestRequest{UUID: model, Body: models.LLMPromptTestBody{Prompt: ok + "é"}})
	if status, code, _ := codeOf(t, err); status != http.StatusUnprocessableEntity || code != errcode.LLMInvalidRequest {
		t.Fatalf("4001 runes = %d %s", status, code)
	}
}

// A caller who hangs up gets a quiet 499, not a logged 500, and a test that
// learned nothing is not recorded.
func TestOps_ClientHangUpIs499(t *testing.T) {
	h := newOpsHarness(t)
	model := h.mockModel(t)
	cred := h.mods.rows[model].CredentialRef.CredentialUUID
	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	_, err := h.ops.TestCredential(ctx, &LLMCredentialPath{UUID: cred})
	if status, code, _ := codeOf(t, err); status != 499 || code != errcode.LLMRequestCanceled {
		t.Fatalf("test hang-up = %d %s", status, code)
	}
	if row := h.creds.rows[cred]; row.LastTestedAt != nil {
		t.Fatal("an abandoned test must not be recorded")
	}
	_, err = h.ops.PromptTest(ctx, &LLMPromptTestRequest{UUID: model, Body: models.LLMPromptTestBody{Prompt: "hi"}})
	if status, code, _ := codeOf(t, err); status != 499 || code != errcode.LLMRequestCanceled {
		t.Fatalf("prompt hang-up = %d %s", status, code)
	}
}
