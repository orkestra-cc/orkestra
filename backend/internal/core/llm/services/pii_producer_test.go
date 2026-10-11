package services

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Fakes for the cross-tenant DSR slices. They hold full rows so the tests
// can assert every personal-data field the producer touches.

type dsrGrants struct {
	rows []models.LLMGrant
	err  error
}

func (d *dsrGrants) ListByUserAllTenants(_ context.Context, user string) ([]models.LLMGrant, error) {
	out := []models.LLMGrant{}
	for _, g := range d.rows {
		if g.UserUUID == user {
			out = append(out, g)
		}
	}
	return out, d.err
}

func (d *dsrGrants) DeleteByUser(_ context.Context, user string) (int64, error) {
	if d.err != nil {
		return 0, d.err
	}
	kept := d.rows[:0]
	var n int64
	for _, g := range d.rows {
		if g.UserUUID == user {
			n++
			continue
		}
		kept = append(kept, g)
	}
	d.rows = kept
	return n, nil
}

func (d *dsrGrants) ListByGranter(_ context.Context, user string) ([]models.LLMGrant, error) {
	out := []models.LLMGrant{}
	for _, g := range d.rows {
		if g.GrantedBy == user {
			out = append(out, g)
		}
	}
	return out, d.err
}

func (d *dsrGrants) PseudonymizeGranter(_ context.Context, user string) (int64, error) {
	if d.err != nil {
		return 0, d.err
	}
	var n int64
	for i := range d.rows {
		if d.rows[i].GrantedBy == user {
			d.rows[i].GrantedBy = models.ErasedActor
			n++
		}
	}
	return n, nil
}

type dsrCreds struct{ rows []models.Credential }

func (d *dsrCreds) ListByCreator(_ context.Context, user string) ([]models.Credential, error) {
	out := []models.Credential{}
	for _, c := range d.rows {
		if c.CreatedBy == user {
			out = append(out, c)
		}
	}
	return out, nil
}

func (d *dsrCreds) PseudonymizeCreator(_ context.Context, user string) (int64, error) {
	var n int64
	for i := range d.rows {
		if d.rows[i].CreatedBy == user {
			d.rows[i].CreatedBy = models.ErasedActor
			n++
		}
	}
	return n, nil
}

type dsrModels struct{ rows []models.Model }

func (d *dsrModels) ListByCreator(_ context.Context, user string) ([]models.Model, error) {
	out := []models.Model{}
	for _, m := range d.rows {
		if m.CreatedBy == user {
			out = append(out, m)
		}
	}
	return out, nil
}

func (d *dsrModels) PseudonymizeCreator(_ context.Context, user string) (int64, error) {
	var n int64
	for i := range d.rows {
		if d.rows[i].CreatedBy == user {
			d.rows[i].CreatedBy = models.ErasedActor
			n++
		}
	}
	return n, nil
}

type dsrUsage struct {
	rows []models.UsageEvent
	err  error
}

func (d *dsrUsage) ListByUserAllTenants(_ context.Context, user string) ([]models.UsageEvent, error) {
	out := []models.UsageEvent{}
	for _, u := range d.rows {
		if u.UserUUID == user {
			out = append(out, u)
		}
	}
	return out, d.err
}

// DeleteByUser mirrors the repository: pending rows are skipped.
func (d *dsrUsage) DeleteByUser(_ context.Context, user string) (int64, error) {
	if d.err != nil {
		return 0, d.err
	}
	kept := d.rows[:0]
	var n int64
	for _, u := range d.rows {
		if u.UserUUID == user && u.Outcome != models.OutcomePending {
			n++
			continue
		}
		kept = append(kept, u)
	}
	d.rows = kept
	return n, nil
}

func (d *dsrUsage) AnonymizeByUser(_ context.Context, user string, newID func() string) (int64, error) {
	if d.err != nil {
		return 0, d.err
	}
	var n int64
	for i := range d.rows {
		if d.rows[i].UserUUID == user {
			d.rows[i].UserUUID = newID()
			d.rows[i].CredentialUUID = ""
			n++
		}
	}
	return n, nil
}

type dsrBudgets struct {
	rows []models.Budget
	err  error
}

func (d *dsrBudgets) ListReferencingUser(_ context.Context, user string) ([]models.Budget, error) {
	out := []models.Budget{}
	for _, b := range d.rows {
		if b.UpdatedBy == user || contains(b.NotifyExtraUserUUIDs, user) {
			out = append(out, b)
		}
	}
	return out, d.err
}

func (d *dsrBudgets) RemoveRecipient(_ context.Context, user string) (int64, error) {
	if d.err != nil {
		return 0, d.err
	}
	var n int64
	for i := range d.rows {
		if contains(d.rows[i].NotifyExtraUserUUIDs, user) {
			d.rows[i].NotifyExtraUserUUIDs = without(d.rows[i].NotifyExtraUserUUIDs, user)
			n++
		}
	}
	return n, nil
}

func (d *dsrBudgets) PseudonymizeUpdater(_ context.Context, user string) (int64, error) {
	if d.err != nil {
		return 0, d.err
	}
	var n int64
	for i := range d.rows {
		if d.rows[i].UpdatedBy == user {
			d.rows[i].UpdatedBy = models.ErasedActor
			n++
		}
	}
	return n, nil
}

type dsrOutbox struct {
	rows []models.OutboxEvent
	err  error
}

func (d *dsrOutbox) ListDeliveredTo(_ context.Context, user string) ([]models.OutboxEvent, error) {
	out := []models.OutboxEvent{}
	for _, e := range d.rows {
		if contains(e.DeliveredTo, user) {
			out = append(out, e)
		}
	}
	return out, d.err
}

func (d *dsrOutbox) RemoveDelivered(_ context.Context, user string) (int64, error) {
	if d.err != nil {
		return 0, d.err
	}
	var n int64
	for i := range d.rows {
		if contains(d.rows[i].DeliveredTo, user) {
			d.rows[i].DeliveredTo = without(d.rows[i].DeliveredTo, user)
			n++
		}
	}
	return n, nil
}

// dsrAccounts mirrors repository.UserAccounts' DSR pair, including its
// refusal of an empty subject.
type dsrAccounts struct {
	rows  []models.UserAccount
	err   error
	calls int
}

func (d *dsrAccounts) ListByUserAllTenants(_ context.Context, user string) ([]models.UserAccount, error) {
	d.calls++
	if user == "" {
		return nil, repository.ErrEmptySubject
	}
	out := []models.UserAccount{}
	for _, a := range d.rows {
		if a.UserUUID == user {
			out = append(out, a)
		}
	}
	return out, d.err
}

func (d *dsrAccounts) DeleteByUser(_ context.Context, user string) (int64, error) {
	d.calls++
	if user == "" {
		return 0, repository.ErrEmptySubject
	}
	if d.err != nil {
		return 0, d.err
	}
	kept := d.rows[:0]
	var n int64
	for _, a := range d.rows {
		if a.UserUUID == user {
			n++
			continue
		}
		kept = append(kept, a)
	}
	d.rows = kept
	return n, nil
}

func (d *dsrAccounts) has(uuid string) bool {
	for _, a := range d.rows {
		if a.UUID == uuid {
			return true
		}
	}
	return false
}

// revokeLog records each revocation and whether the row still existed then
// (revoke must come before the delete).
type revokeLog struct {
	accounts *dsrAccounts
	seen     []string // uuid@tenant
	before   []bool
}

func (r *revokeLog) revoke(_ context.Context, a models.UserAccount) {
	r.seen = append(r.seen, a.UUID+"@"+a.TenantID)
	r.before = append(r.before, r.accounts.has(a.UUID))
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func without(list []string, v string) []string {
	out := []string{}
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

type dsrSet struct {
	g *dsrGrants
	c *dsrCreds
	m *dsrModels
	u *dsrUsage
	b *dsrBudgets
	o *dsrOutbox
	a *dsrAccounts
	r *revokeLog
}

var (
	tm0 = time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	tm1 = tm0.Add(time.Hour)
	tm2 = tm0.Add(2 * time.Hour)
	tm3 = tm0.Add(3 * time.Hour)
	tm4 = tm0.Add(4 * time.Hour)
	tm5 = tm0.Add(5 * time.Hour)
	tm6 = tm0.Add(6 * time.Hour)
)

// dsrFixture: subject "u1" is grantee of g1, g2 and g5, granter of g3 and g5,
// creator of cred-1 and model-1, owner of usage rows us1 (finalized), us2
// (finalized, user account) and us3 (pending), extra recipient of the t1
// and t3 budgets and last editor of t1's, received alert ob1, and linked
// the ChatGPT accounts acct-1 (t1) and acct-2 (t2).
// Everything else belongs to other people.
func dsrFixture() (*PIIProducer, *dsrSet) {
	set := &dsrSet{}
	set.g = &dsrGrants{rows: []models.LLMGrant{
		{UUID: "g1", TenantID: "t1", ModelUUID: "m1", UserUUID: "u1", GrantedBy: "admin1", CreatedAt: tm0},
		{UUID: "g2", TenantID: "t2", ModelUUID: "m2", UserUUID: "u1", GrantedBy: "u2", CreatedAt: tm1},
		{UUID: "g3", TenantID: "t1", ModelUUID: "m1", UserUUID: "u2", GrantedBy: "u1", CreatedAt: tm2},
		{UUID: "g4", TenantID: "t1", ModelUUID: "m3", UserUUID: "u3", GrantedBy: "admin1", CreatedAt: tm3},
		{UUID: "g5", TenantID: "t2", ModelUUID: "m2", UserUUID: "u1", GrantedBy: "u1", CreatedAt: tm4},
	}}
	set.c = &dsrCreds{rows: []models.Credential{
		{UUID: "cred-1", TenantID: "t1", Name: "Team OpenAI", Provider: models.ProviderOpenAI, BaseURL: "https://api.openai.com/v1",
			Secret:      models.Envelope{Alg: models.EnvelopeAlgLocal, KeyID: "key-id-xyz", KeyVersion: 1, SchemaVersion: 1, Ciphertext: "CIPHERTEXT-SENTINEL"},
			SecretLast4: "L4ST", Status: models.CredentialStatusActive, CreatedBy: "u1", CreatedAt: tm5},
		{UUID: "cred-2", TenantID: "t1", Name: "Other", Provider: models.ProviderOllama, BaseURL: "http://ollama.internal:11434", Status: models.CredentialStatusActive, CreatedBy: "admin1", CreatedAt: tm0},
	}}
	set.m = &dsrModels{rows: []models.Model{
		{UUID: "model-1", TenantID: "t1", Name: "Fast", Provider: models.ProviderOpenAI, ModelID: "gpt-x", CreatedBy: "u1", CreatedAt: tm6},
		{UUID: "model-2", TenantID: "t1", Name: "Slow", Provider: models.ProviderOpenAI, ModelID: "gpt-y", CreatedBy: "admin1", CreatedAt: tm0},
	}}
	exp := tm3.Add(time.Minute)
	set.u = &dsrUsage{rows: []models.UsageEvent{
		{UUID: "us1", TenantID: "t1", UserUUID: "u1", ModelUUID: "model-1", Provider: models.ProviderOpenAI, ModelID: "gpt-x",
			CredentialKind: models.CredentialKindOrg, CredentialUUID: "cred-1", Caller: "assistant", Purpose: "chat",
			InputTokens: 120, OutputTokens: 30, LatencyMs: 900, Outcome: models.OutcomeOK, Fallback: false,
			Attempts:  []models.Attempt{{Provider: models.ProviderOpenAI, CredentialKind: models.CredentialKindOrg, Outcome: models.OutcomeOK, InputTokens: 120, OutputTokens: 30}},
			CreatedAt: tm1, FinalizedAt: &tm2},
		{UUID: "us2", TenantID: "t2", UserUUID: "u1", ModelUUID: "model-9", Provider: models.ProviderOpenAI, ModelID: "gpt-z",
			CredentialKind: models.CredentialKindUserAccount, CredentialUUID: "acct-9", Caller: "rag", Purpose: "summarize",
			InputTokens: 7, OutputTokens: 0, LatencyMs: 40, Outcome: models.OutcomeError, ErrorCode: "llm.provider_unavailable", Fallback: true,
			Attempts: []models.Attempt{
				{Provider: models.ProviderOpenAI, CredentialKind: models.CredentialKindUserAccount, Outcome: models.OutcomeError, InputTokens: 7},
				{Provider: models.ProviderAnthropic, CredentialKind: models.CredentialKindUserAccount, Outcome: models.OutcomeError},
			},
			CreatedAt: tm2, FinalizedAt: &tm3},
		{UUID: "us3", TenantID: "t1", UserUUID: "u1", ModelUUID: "model-1", Provider: models.ProviderOpenAI, ModelID: "gpt-x",
			CredentialKind: models.CredentialKindOrg, CredentialUUID: "cred-1", Caller: "assistant", Purpose: "chat",
			ReservedTokens: 500, ReservedOutputTokens: 400, ReservationExpiresAt: &exp, Outcome: models.OutcomePending, CreatedAt: tm3},
		{UUID: "us4", TenantID: "t1", UserUUID: "u2", ModelUUID: "model-1", CredentialKind: models.CredentialKindOrg, CredentialUUID: "cred-1",
			InputTokens: 5, Outcome: models.OutcomeOK, CreatedAt: tm0},
	}}
	set.b = &dsrBudgets{rows: []models.Budget{
		{TenantID: "t1", MonthlyTokenLimit: 1000, WarnPct: 80, Action: models.BudgetActionBlock, NotifyExtraUserUUIDs: []string{"u1", "u3"}, UpdatedBy: "u1", UpdatedAt: tm4},
		{TenantID: "t2", MonthlyTokenLimit: 2000, WarnPct: 80, Action: models.BudgetActionWarnOnly, NotifyExtraUserUUIDs: []string{"u3"}, UpdatedBy: "admin1", UpdatedAt: tm0},
		{TenantID: "t3", MonthlyTokenLimit: 3000, WarnPct: 90, Action: models.BudgetActionBlock, NotifyExtraUserUUIDs: []string{"u1"}, UpdatedBy: "u2", UpdatedAt: tm5},
	}}
	set.o = &dsrOutbox{rows: []models.OutboxEvent{
		{UUID: "ob1", TenantID: "t1", Period: "2026-03", Threshold: models.ThresholdWarn, Status: models.OutboxStatusSent,
			Payload: map[string]any{"pct": 80}, DeliveredTo: []string{"u1", "u3"}, CreatedAt: tm4, SentAt: &tm5},
		{UUID: "ob2", TenantID: "t2", Period: "2026-03", Threshold: models.ThresholdExhausted, Status: models.OutboxStatusSending,
			DeliveredTo: []string{"u3"}, CreatedAt: tm5},
	}}
	sealed := func(tag string) models.Envelope {
		return models.Envelope{Alg: models.EnvelopeAlgLocal, KeyVersion: 1, SchemaVersion: 1, Ciphertext: "TOKEN-SENTINEL-" + tag}
	}
	set.a = &dsrAccounts{rows: []models.UserAccount{
		{UUID: "acct-1", TenantID: "t1", UserUUID: "u1", Provider: models.ProviderOpenAI, Label: "Work", Issuer: "https://auth.openai.com",
			Subject: "SUBJECT-SENTINEL", Email: "u1@example.test", ClientID: "oaiapp_CLIENT-SENTINEL", ExtAgentHostID: "urn:uuid:HOST-SENTINEL",
			IDToken: sealed("id"), AccessToken: sealed("access"), RefreshToken: sealed("refresh"), TokenType: "Bearer",
			ExpiresAt: tm3, RefreshExpiresAt: tm6, SavedAt: tm2, Scopes: []string{"openid", models.ScopePlanUsage}, PlanUsageGranted: true,
			Selected: true, Status: models.AccountStatusActive, LinkedAt: tm1, LastUsedAt: &tm4, LastError: "llm.provider_unavailable", UpdatedAt: tm4},
		{UUID: "acct-2", TenantID: "t2", UserUUID: "u1", Provider: models.ProviderOpenAI, Label: "Personal", Subject: "SUBJECT-SENTINEL-2",
			ClientID: "oaiapp_CLIENT-SENTINEL-2", RefreshToken: sealed("refresh-2"), Status: models.AccountStatusNeedsReauth, LinkedAt: tm2},
		{UUID: "acct-3", TenantID: "t1", UserUUID: "u2", Provider: models.ProviderOpenAI, Label: "Other", ClientID: "oaiapp_other", Status: models.AccountStatusActive, LinkedAt: tm0},
	}}
	set.r = &revokeLog{accounts: set.a}
	p := NewPIIProducer(set.c, set.m, set.g, set.u, set.b, set.o, set.a, set.r.revoke, slog.Default())
	return p, set
}

func TestPIIProducer_Subject(t *testing.T) {
	p, _ := dsrFixture()
	if p.Subject() != "llm" {
		t.Fatalf("Subject = %q", p.Subject())
	}
}

func TestPIIProducer_ExportEveryField(t *testing.T) {
	p, _ := dsrFixture()
	out, err := p.ExportPersonalData(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := out.(*PersonalDataExport)
	if !ok {
		t.Fatalf("export type = %T", out)
	}

	// Grants held as grantee (g1, g2, g5), every projected field.
	want := []ExportedGrant{
		{UUID: "g1", TenantID: "t1", ModelUUID: "m1", CreatedAt: tm0},
		{UUID: "g2", TenantID: "t2", ModelUUID: "m2", CreatedAt: tm1},
		{UUID: "g5", TenantID: "t2", ModelUUID: "m2", CreatedAt: tm4},
	}
	if len(b.Grants) != len(want) {
		t.Fatalf("grants = %+v", b.Grants)
	}
	for i := range want {
		if b.Grants[i] != want[i] {
			t.Errorf("grants[%d] = %+v, want %+v", i, b.Grants[i], want[i])
		}
	}

	// Grants the subject handed out (g3, g5): model + grantee + when.
	wantIssued := []ExportedIssuedGrant{
		{ModelUUID: "m1", UserUUID: "u2", CreatedAt: tm2},
		{ModelUUID: "m2", UserUUID: "u1", CreatedAt: tm4},
	}
	if len(b.GrantsIssued) != len(wantIssued) {
		t.Fatalf("grantsIssued = %+v", b.GrantsIssued)
	}
	for i := range wantIssued {
		if b.GrantsIssued[i] != wantIssued[i] {
			t.Errorf("grantsIssued[%d] = %+v, want %+v", i, b.GrantsIssued[i], wantIssued[i])
		}
	}

	// Credentials created: uuid + name + createdAt only.
	wantCred := ExportedCredentialRef{UUID: "cred-1", Name: "Team OpenAI", CreatedAt: tm5}
	if len(b.CredentialsCreated) != 1 || b.CredentialsCreated[0] != wantCred {
		t.Fatalf("credentialsCreated = %+v, want [%+v]", b.CredentialsCreated, wantCred)
	}
	// Models created.
	wantModel := ExportedModelRef{UUID: "model-1", Name: "Fast", CreatedAt: tm6}
	if len(b.ModelsCreated) != 1 || b.ModelsCreated[0] != wantModel {
		t.Fatalf("modelsCreated = %+v, want [%+v]", b.ModelsCreated, wantModel)
	}

	// llm_usage.userUuid: us1, us2, us3 (pending included), every field.
	wantUsage := []ExportedUsage{
		{UUID: "us1", TenantID: "t1", ModelUUID: "model-1", Provider: models.ProviderOpenAI, ModelID: "gpt-x",
			CredentialKind: models.CredentialKindOrg, Caller: "assistant", Purpose: "chat", InputTokens: 120, OutputTokens: 30,
			LatencyMs: 900, Outcome: models.OutcomeOK,
			Attempts:  []ExportedAttempt{{Provider: models.ProviderOpenAI, CredentialKind: models.CredentialKindOrg, Outcome: models.OutcomeOK, InputTokens: 120, OutputTokens: 30}},
			CreatedAt: tm1, FinalizedAt: &tm2},
		{UUID: "us2", TenantID: "t2", ModelUUID: "model-9", Provider: models.ProviderOpenAI, ModelID: "gpt-z",
			CredentialKind: models.CredentialKindUserAccount, Caller: "rag", Purpose: "summarize", InputTokens: 7,
			LatencyMs: 40, Outcome: models.OutcomeError, ErrorCode: "llm.provider_unavailable", Fallback: true,
			Attempts: []ExportedAttempt{
				{Provider: models.ProviderOpenAI, CredentialKind: models.CredentialKindUserAccount, Outcome: models.OutcomeError, InputTokens: 7},
				{Provider: models.ProviderAnthropic, CredentialKind: models.CredentialKindUserAccount, Outcome: models.OutcomeError},
			},
			CreatedAt: tm2, FinalizedAt: &tm3},
		{UUID: "us3", TenantID: "t1", ModelUUID: "model-1", Provider: models.ProviderOpenAI, ModelID: "gpt-x",
			CredentialKind: models.CredentialKindOrg, Caller: "assistant", Purpose: "chat", Outcome: models.OutcomePending,
			Attempts: []ExportedAttempt{}, CreatedAt: tm3},
	}
	if !reflect.DeepEqual(b.Usage, wantUsage) {
		t.Errorf("usage =\n%+v\nwant\n%+v", b.Usage, wantUsage)
	}

	// llm_budgets.notifyExtraUserUuids (t1, t3) and updatedBy (t1).
	wantRecipient := []ExportedBudgetRef{{TenantID: "t1", UpdatedAt: tm4}, {TenantID: "t3", UpdatedAt: tm5}}
	if !reflect.DeepEqual(b.BudgetRecipientOf, wantRecipient) {
		t.Errorf("budgetRecipientOf = %+v, want %+v", b.BudgetRecipientOf, wantRecipient)
	}
	wantUpdated := []ExportedBudgetRef{{TenantID: "t1", UpdatedAt: tm4}}
	if !reflect.DeepEqual(b.BudgetsUpdated, wantUpdated) {
		t.Errorf("budgetsUpdated = %+v, want %+v", b.BudgetsUpdated, wantUpdated)
	}

	// llm_budget_outbox.deliveredTo (ob1).
	wantAlerts := []ExportedBudgetAlert{{TenantID: "t1", Period: "2026-03", Threshold: models.ThresholdWarn, CreatedAt: tm4, SentAt: &tm5}}
	if !reflect.DeepEqual(b.BudgetAlerts, wantAlerts) {
		t.Errorf("budgetAlertsReceived = %+v, want %+v", b.BudgetAlerts, wantAlerts)
	}

	// llm_user_accounts.userUuid (acct-1, acct-2), every projected field.
	wantAccounts := []ExportedLinkedAccount{
		{UUID: "acct-1", Label: "Work", Email: "u1@example.test", Status: models.AccountStatusActive, LinkedAt: tm1, LastUsedAt: &tm4},
		{UUID: "acct-2", Label: "Personal", Status: models.AccountStatusNeedsReauth, LinkedAt: tm2},
	}
	if !reflect.DeepEqual(b.Accounts, wantAccounts) {
		t.Errorf("accounts = %+v, want %+v", b.Accounts, wantAccounts)
	}
}

// The DSR listing returns whole credentials, envelope included. None of it
// may reach the bundle that is handed to the data subject.
func TestPIIProducer_ExportNeverLeaksCredentialMaterial(t *testing.T) {
	p, _ := dsrFixture()
	out, err := p.ExportPersonalData(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, forbidden := range []string{
		"secret", "Secret", "ciphertext", "Ciphertext", "CIPHERTEXT-SENTINEL", "secretLast4", "L4ST",
		"baseUrl", "api.openai.com", "keyId", "key-id-xyz", "keyVersion", "schemaVersion", "alg",
		// Usage bookkeeping and the credential reference stay out too.
		"credentialUuid", "acct-9", "reservedTokens", "reservedOutputTokens", "reservationExpiresAt",
		// Budget settings and alert payloads are org data, not the subject's.
		"monthlyTokenLimit", "warnPct", "action", "notifyExtraUserUuids", "updatedBy", "payload", "u3", "sending",
		// Linked accounts: never a token, the issued client id, the OIDC
		// subject, the issuer, the host id or the scopes.
		"TOKEN-SENTINEL", "accessToken", "refreshToken", "idToken", "clientId", "CLIENT-SENTINEL", "oaiapp",
		"subject", "SUBJECT-SENTINEL", "issuer", "auth.openai.com", "extAgentHostId", "HOST-SENTINEL", "scopes",
		models.ScopePlanUsage, "Bearer", "lastError", "acct-3",
	} {
		if strings.Contains(s, forbidden) {
			t.Errorf("export JSON contains %q: %s", forbidden, s)
		}
	}
	// The created credentials and models are references: name only, no
	// provider or upstream model id (usage rows do carry the model used).
	for _, section := range []any{out.(*PersonalDataExport).CredentialsCreated, out.(*PersonalDataExport).ModelsCreated} {
		sec, _ := json.Marshal(section)
		for _, forbidden := range []string{"provider", "modelId", "gpt-x"} {
			if strings.Contains(string(sec), forbidden) {
				t.Errorf("credential/model section contains %q: %s", forbidden, sec)
			}
		}
	}
	// And the keys it does carry are exactly the intended ones.
	var generic map[string][]map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	wantKeys := map[string][]string{
		"grants":             {"createdAt", "modelUuid", "tenantId", "uuid"},
		"grantsIssued":       {"createdAt", "modelUuid", "userUuid"},
		"credentialsCreated": {"createdAt", "name", "uuid"},
		"modelsCreated":      {"createdAt", "name", "uuid"},
		"usage": {"attempts", "caller", "createdAt", "credentialKind", "errorCode", "fallback", "finalizedAt", "inputTokens",
			"latencyMs", "modelId", "modelUuid", "outcome", "outputTokens", "provider", "purpose", "tenantId", "uuid"},
		"budgetRecipientOf":    {"tenantId", "updatedAt"},
		"budgetsUpdated":       {"tenantId", "updatedAt"},
		"budgetAlertsReceived": {"createdAt", "period", "sentAt", "tenantId", "threshold"},
		"accounts":             {"email", "label", "lastUsedAt", "linkedAt", "status", "uuid"},
	}
	for section, keys := range wantKeys {
		rows := generic[section]
		if len(rows) == 0 {
			t.Errorf("section %q missing", section)
			continue
		}
		for _, row := range rows {
			if len(row) != len(keys) {
				t.Errorf("%s row has keys %v, want %v", section, row, keys)
			}
			for _, k := range keys {
				if _, ok := row[k]; !ok {
					t.Errorf("%s row missing key %q: %v", section, k, row)
				}
			}
		}
	}
}

func TestPIIProducer_ExportNothingIsNil(t *testing.T) {
	p, _ := dsrFixture()
	out, err := p.ExportPersonalData(context.Background(), "nobody")
	if err != nil || out != nil {
		t.Fatalf("no data must export untyped nil, got %#v, %v", out, err)
	}
}

func TestPIIProducer_ExportActorOnly(t *testing.T) {
	// A subject who only created a model (no grants anywhere) still has data.
	p, _ := dsrFixture()
	out, err := p.ExportPersonalData(context.Background(), "admin1")
	if err != nil || out == nil {
		t.Fatalf("admin1 export = %#v, %v", out, err)
	}
	b := out.(*PersonalDataExport)
	if len(b.Grants) != 0 || len(b.GrantsIssued) != 2 || len(b.CredentialsCreated) != 1 || len(b.ModelsCreated) != 1 ||
		len(b.Usage) != 0 || len(b.BudgetRecipientOf) != 0 || len(b.BudgetsUpdated) != 1 || b.BudgetsUpdated[0].TenantID != "t2" || len(b.BudgetAlerts) != 0 {
		t.Fatalf("admin1 bundle = %+v", b)
	}
	// u3 only appears as a budget recipient and an alert recipient.
	out, err = p.ExportPersonalData(context.Background(), "u3")
	if err != nil || out == nil {
		t.Fatalf("u3 export = %#v, %v", out, err)
	}
	b = out.(*PersonalDataExport)
	if len(b.BudgetRecipientOf) != 2 || len(b.BudgetAlerts) != 2 || len(b.BudgetsUpdated) != 0 || len(b.Usage) != 0 {
		t.Fatalf("u3 bundle = %+v", b)
	}
}

func TestPIIProducer_PurgeBothModes(t *testing.T) {
	for _, mode := range []iface.EraseMode{iface.EraseAnonymize, iface.EraseHardDelete} {
		p, set := dsrFixture()
		g, c, m := set.g, set.c, set.m
		res, err := p.PurgePersonalData(context.Background(), "u1", mode)
		if err != nil {
			t.Fatalf("mode %v: %v", mode, err)
		}
		// Both modes: deleted acct-1, acct-2 (linked accounts) and g1, g2, g5
		// (grantee); anonymized g3 (granter) +
		// cred-1 + model-1 + budgets t1, t3 (recipient) + t1 (editor) + ob1.
		// g5 was both grantee and granter; it is deleted, so not counted twice.
		// Usage: hard delete removes us1, us2 and anonymizes the pending us3;
		// anonymize rewrites all three.
		wantDeleted, wantAnon := 2+3+2, 3+3+1+1
		if mode == iface.EraseAnonymize {
			wantDeleted, wantAnon = 2+3, 3+3+3+1
		}
		if res.RowsDeleted != wantDeleted || res.RowsAnonymized != wantAnon {
			t.Errorf("mode %v result = %+v, want %d deleted / %d anonymized", mode, res, wantDeleted, wantAnon)
		}
		wantColls := map[string]bool{"llm_grants": true, "llm_credentials": true, "llm_models": true,
			"llm_usage": true, "llm_budgets": true, "llm_budget_outbox": true, "llm_user_accounts": true}
		if len(res.Collections) != len(wantColls) {
			t.Errorf("mode %v collections = %v", mode, res.Collections)
		}
		for _, coll := range res.Collections {
			if !wantColls[coll] {
				t.Errorf("mode %v unexpected collection %q", mode, coll)
			}
		}

		// llm_user_accounts: each of the subject's refresh tokens revoked
		// under its own org while the row still existed, then every
		// registration deleted; acct-3 (u2) untouched.
		if !reflect.DeepEqual(set.r.seen, []string{"acct-1@t1", "acct-2@t2"}) || !reflect.DeepEqual(set.r.before, []bool{true, true}) {
			t.Errorf("mode %v revocations = %v (row present: %v)", mode, set.r.seen, set.r.before)
		}
		if len(set.a.rows) != 1 || set.a.rows[0].UUID != "acct-3" {
			t.Errorf("mode %v remaining accounts = %+v", mode, set.a.rows)
		}

		// llm_grants.userUuid: the subject's grant rows are gone.
		if len(g.rows) != 2 {
			t.Fatalf("mode %v remaining grants = %+v", mode, g.rows)
		}
		for _, row := range g.rows {
			if row.UserUUID == "u1" {
				t.Errorf("mode %v: grantee row survived: %+v", mode, row)
			}
		}
		// llm_grants.grantedBy: g3 rewritten, g4 untouched.
		byUUID := map[string]models.LLMGrant{}
		for _, row := range g.rows {
			byUUID[row.UUID] = row
		}
		if byUUID["g3"].GrantedBy != models.ErasedActor || byUUID["g3"].UserUUID != "u2" {
			t.Errorf("mode %v g3 = %+v", mode, byUUID["g3"])
		}
		if byUUID["g4"].GrantedBy != "admin1" || byUUID["g4"].UserUUID != "u3" {
			t.Errorf("mode %v g4 must be untouched: %+v", mode, byUUID["g4"])
		}
		// llm_credentials.createdBy.
		if c.rows[0].CreatedBy != models.ErasedActor || c.rows[0].Name != "Team OpenAI" {
			t.Errorf("mode %v cred-1 = %+v", mode, c.rows[0])
		}
		if c.rows[1].CreatedBy != "admin1" {
			t.Errorf("mode %v cred-2 must be untouched: %+v", mode, c.rows[1])
		}
		// llm_models.createdBy.
		if m.rows[0].CreatedBy != models.ErasedActor || m.rows[0].Name != "Fast" {
			t.Errorf("mode %v model-1 = %+v", mode, m.rows[0])
		}
		if m.rows[1].CreatedBy != "admin1" {
			t.Errorf("mode %v model-2 must be untouched: %+v", mode, m.rows[1])
		}

		// llm_usage.userUuid + credentialUuid.
		usage := map[string]models.UsageEvent{}
		for _, row := range set.u.rows {
			usage[row.UUID] = row
		}
		kept := []string{"us3"}
		if mode == iface.EraseAnonymize {
			kept = []string{"us1", "us2", "us3"}
		} else if _, ok := usage["us1"]; ok || len(set.u.rows) != 2 {
			t.Errorf("mode %v: finalized usage rows survived a hard delete: %+v", mode, set.u.rows)
		}
		seen := map[string]bool{}
		for _, id := range kept {
			row, ok := usage[id]
			if !ok {
				t.Fatalf("mode %v: usage row %s missing", mode, id)
			}
			if !strings.HasPrefix(row.UserUUID, models.ErasedUsagePrefix) || len(row.UserUUID) <= len(models.ErasedUsagePrefix) || strings.Contains(row.UserUUID, "u1") {
				t.Errorf("mode %v %s userUuid = %q", mode, id, row.UserUUID)
			}
			if seen[row.UserUUID] {
				t.Errorf("mode %v: replacement %q reused across rows (rows must not stay linkable)", mode, row.UserUUID)
			}
			seen[row.UserUUID] = true
			if row.CredentialUUID != "" {
				t.Errorf("mode %v %s credentialUuid kept: %q", mode, id, row.CredentialUUID)
			}
		}
		// Counters and the pending reservation stay intact.
		if mode == iface.EraseAnonymize && (usage["us1"].InputTokens != 120 || usage["us1"].OutputTokens != 30 || usage["us2"].ErrorCode != "llm.provider_unavailable") {
			t.Errorf("mode %v: anonymize changed counters: %+v / %+v", mode, usage["us1"], usage["us2"])
		}
		if us3 := usage["us3"]; us3.Outcome != models.OutcomePending || us3.ReservedTokens != 500 {
			t.Errorf("mode %v: pending row altered beyond the subject: %+v", mode, us3)
		}
		if us4 := usage["us4"]; us4.UserUUID != "u2" || us4.CredentialUUID != "cred-1" {
			t.Errorf("mode %v us4 must be untouched: %+v", mode, us4)
		}

		// llm_budgets.notifyExtraUserUuids + updatedBy.
		bud := set.b.rows
		if !reflect.DeepEqual(bud[0].NotifyExtraUserUUIDs, []string{"u3"}) || bud[0].UpdatedBy != models.ErasedActor || !bud[0].UpdatedAt.Equal(tm4) || bud[0].MonthlyTokenLimit != 1000 {
			t.Errorf("mode %v budget t1 = %+v", mode, bud[0])
		}
		if !reflect.DeepEqual(bud[1].NotifyExtraUserUUIDs, []string{"u3"}) || bud[1].UpdatedBy != "admin1" {
			t.Errorf("mode %v budget t2 must be untouched: %+v", mode, bud[1])
		}
		if len(bud[2].NotifyExtraUserUUIDs) != 0 || bud[2].UpdatedBy != "u2" {
			t.Errorf("mode %v budget t3 = %+v", mode, bud[2])
		}

		// llm_budget_outbox.deliveredTo.
		if !reflect.DeepEqual(set.o.rows[0].DeliveredTo, []string{"u3"}) || set.o.rows[0].Status != models.OutboxStatusSent {
			t.Errorf("mode %v ob1 = %+v", mode, set.o.rows[0])
		}
		if !reflect.DeepEqual(set.o.rows[1].DeliveredTo, []string{"u3"}) {
			t.Errorf("mode %v ob2 must be untouched: %+v", mode, set.o.rows[1])
		}

		// Nothing is left that names the subject.
		out, err := p.ExportPersonalData(context.Background(), "u1")
		if err != nil || out != nil {
			t.Errorf("mode %v export after purge = %#v, %v", mode, out, err)
		}
	}
}

func TestPIIProducer_PurgeUnknownSubjectIsNoop(t *testing.T) {
	p, set := dsrFixture()
	g := set.g
	res, err := p.PurgePersonalData(context.Background(), "nobody", iface.EraseHardDelete)
	if err != nil || res.RowsDeleted != 0 || res.RowsAnonymized != 0 || len(g.rows) != 5 {
		t.Fatalf("noop purge = %+v, %v, grants %d", res, err, len(g.rows))
	}
}

func TestPIIProducer_PropagatesRepositoryErrors(t *testing.T) {
	boom := errors.New("db down")
	for _, which := range []string{"grants", "usage", "budgets", "outbox", "accounts"} {
		p, set := dsrFixture()
		switch which {
		case "grants":
			set.g.err = boom
		case "usage":
			set.u.err = boom
		case "budgets":
			set.b.err = boom
		case "outbox":
			set.o.err = boom
		case "accounts":
			set.a.err = boom
		}
		if _, err := p.ExportPersonalData(context.Background(), "u1"); !errors.Is(err, boom) {
			t.Errorf("%s: export err = %v", which, err)
		}
		for _, mode := range []iface.EraseMode{iface.EraseHardDelete, iface.EraseAnonymize} {
			if _, err := p.PurgePersonalData(context.Background(), "u1", mode); !errors.Is(err, boom) {
				t.Errorf("%s mode %v: purge err = %v", which, mode, err)
			}
		}
	}
}

// M4: an empty subject would match every row created without a user in
// context, across every org. The producer refuses it before any query.
func TestPIIProducer_RejectsEmptySubject(t *testing.T) {
	p, set := dsrFixture()
	g, c, m := set.g, set.c, set.m
	g.rows = append(g.rows, models.LLMGrant{UUID: "g-anon", TenantID: "t3", ModelUUID: "m9", UserUUID: "u4", GrantedBy: "", CreatedAt: tm0})
	c.rows = append(c.rows, models.Credential{UUID: "cred-anon", TenantID: "t3", Name: "Seeded", CreatedBy: ""})
	m.rows = append(m.rows, models.Model{UUID: "model-anon", TenantID: "t3", Name: "Seeded", CreatedBy: ""})
	set.u.rows = append(set.u.rows, models.UsageEvent{UUID: "us-anon", TenantID: "t3", UserUUID: "", Outcome: models.OutcomeOK, CreatedAt: tm0})
	set.b.rows = append(set.b.rows, models.Budget{TenantID: "t4", UpdatedBy: "", NotifyExtraUserUUIDs: []string{}})

	set.a.rows = append(set.a.rows, models.UserAccount{UUID: "acct-anon", TenantID: "t3", UserUUID: ""})
	if out, err := p.ExportPersonalData(context.Background(), ""); !errors.Is(err, ErrEmptySubject) || out != nil {
		t.Fatalf("export of an empty subject = %#v, %v", out, err)
	}
	res, err := p.PurgePersonalData(context.Background(), "", iface.EraseHardDelete)
	if !errors.Is(err, ErrEmptySubject) || res.RowsDeleted != 0 || res.RowsAnonymized != 0 {
		t.Fatalf("purge of an empty subject = %+v, %v", res, err)
	}
	if g.rows[len(g.rows)-1].GrantedBy != "" || c.rows[len(c.rows)-1].CreatedBy != "" || m.rows[len(m.rows)-1].CreatedBy != "" || len(g.rows) != 6 ||
		len(set.u.rows) != 5 || set.u.rows[4].UserUUID != "" || set.b.rows[3].UpdatedBy != "" {
		t.Fatal("a purge of an empty subject rewrote rows")
	}
	if set.a.calls != 0 || len(set.r.seen) != 0 || len(set.a.rows) != 4 {
		t.Fatalf("an empty subject reached the accounts (calls %d, revoked %v)", set.a.calls, set.r.seen)
	}
}

// The accounts repository refuses an empty subject itself; were the guard
// above ever lost, that refusal must stop the request, never read as "no
// rows" (carry from Task 2).
func TestPIIProducer_RepositoryEmptySubjectIsARefusal(t *testing.T) {
	p, set := dsrFixture()
	set.a.err = repository.ErrEmptySubject
	if _, err := p.ExportPersonalData(context.Background(), "u1"); !errors.Is(err, repository.ErrEmptySubject) {
		t.Fatalf("export err = %v", err)
	}
	res, err := p.PurgePersonalData(context.Background(), "u1", iface.EraseHardDelete)
	if !errors.Is(err, repository.ErrEmptySubject) || res.RowsDeleted != 0 || len(set.g.rows) != 5 {
		t.Fatalf("purge = %+v, %v (grants %d)", res, err, len(set.g.rows))
	}
}
