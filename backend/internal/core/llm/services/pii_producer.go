package services

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Narrow, cross-tenant slices of the repositories the DSR pipeline needs.
// The real repositories satisfy them (asserted below); each method is a
// subject-scoped query across every org.

// GrantDSRRepo covers llm_grants.userUuid (grantee) and llm_grants.grantedBy.
type GrantDSRRepo interface {
	ListByUserAllTenants(ctx context.Context, userUUID string) ([]models.LLMGrant, error)
	DeleteByUser(ctx context.Context, userUUID string) (int64, error)
	ListByGranter(ctx context.Context, userUUID string) ([]models.LLMGrant, error)
	PseudonymizeGranter(ctx context.Context, userUUID string) (int64, error)
}

// CredentialDSRRepo covers llm_credentials.createdBy. ListByCreator returns
// whole credentials, secret envelope included: the producer must project it.
type CredentialDSRRepo interface {
	ListByCreator(ctx context.Context, userUUID string) ([]models.Credential, error)
	PseudonymizeCreator(ctx context.Context, userUUID string) (int64, error)
}

// ModelDSRRepo covers llm_models.createdBy.
type ModelDSRRepo interface {
	ListByCreator(ctx context.Context, userUUID string) ([]models.Model, error)
	PseudonymizeCreator(ctx context.Context, userUUID string) (int64, error)
}

// UsageDSRRepo covers llm_usage.userUuid (and the credentialUuid that can
// point back to the subject's linked account). DeleteByUser skips pending
// rows (their reservation is still in the rollup); AnonymizeByUser calls
// newID once per row.
type UsageDSRRepo interface {
	ListByUserAllTenants(ctx context.Context, userUUID string) ([]models.UsageEvent, error)
	DeleteByUser(ctx context.Context, userUUID string) (int64, error)
	AnonymizeByUser(ctx context.Context, userUUID string, newID func() string) (int64, error)
}

// BudgetDSRRepo covers llm_budgets.notifyExtraUserUuids and updatedBy.
type BudgetDSRRepo interface {
	ListReferencingUser(ctx context.Context, userUUID string) ([]models.Budget, error)
	RemoveRecipient(ctx context.Context, userUUID string) (int64, error)
	PseudonymizeUpdater(ctx context.Context, userUUID string) (int64, error)
}

// OutboxDSRRepo covers llm_budget_outbox.deliveredTo.
type OutboxDSRRepo interface {
	ListDeliveredTo(ctx context.Context, userUUID string) ([]models.OutboxEvent, error)
	RemoveDelivered(ctx context.Context, userUUID string) (int64, error)
}

// AccountDSRRepo covers llm_user_accounts.userUuid: the subject's Sign in
// with ChatGPT registrations in every org. Both methods refuse an empty
// subject with repository.ErrEmptySubject. DeleteByUser returns the
// documents it removed (each atomically, with the token set it held then),
// also those removed before a failure.
type AccountDSRRepo interface {
	ListByUserAllTenants(ctx context.Context, userUUID string) ([]models.UserAccount, error)
	DeleteByUser(ctx context.Context, userUUID string) ([]models.UserAccount, error)
}

// AccountRevoker revokes one registration's refresh token at the provider,
// best-effort, under the row's own org (AccountsService.RevokeRegistration,
// ruling 2.15): the DSR context carries no tenant. It emits no audit event
// and deletes nothing.
type AccountRevoker func(ctx context.Context, acc models.UserAccount)

var (
	_ AccountDSRRepo    = (*repository.UserAccounts)(nil)
	_ GrantDSRRepo      = (*repository.Grants)(nil)
	_ CredentialDSRRepo = (*repository.Credentials)(nil)
	_ ModelDSRRepo      = (*repository.Models)(nil)
	_ UsageDSRRepo      = (*repository.Usage)(nil)
	_ BudgetDSRRepo     = (*repository.Budgets)(nil)
	_ OutboxDSRRepo     = (*repository.Outbox)(nil)
)

// ExportedGrant is a grant the subject holds as grantee.
type ExportedGrant struct {
	UUID      string    `json:"uuid"`
	TenantID  string    `json:"tenantId"`
	ModelUUID string    `json:"modelUuid"`
	CreatedAt time.Time `json:"createdAt"`
}

// ExportedIssuedGrant is a grant the subject handed out (grantedBy).
type ExportedIssuedGrant struct {
	ModelUUID string    `json:"modelUuid"`
	UserUUID  string    `json:"userUuid"`
	CreatedAt time.Time `json:"createdAt"`
}

// ExportedCredentialRef references a credential the subject created. It
// carries no provider, endpoint or secret material.
type ExportedCredentialRef struct {
	UUID      string    `json:"uuid"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// ExportedModelRef references a model the subject created.
type ExportedModelRef struct {
	UUID      string    `json:"uuid"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// ExportedAttempt is one provider try of a usage row: counters and codes.
type ExportedAttempt struct {
	Provider       string `json:"provider"`
	CredentialKind string `json:"credentialKind"`
	Outcome        string `json:"outcome"`
	InputTokens    int    `json:"inputTokens"`
	OutputTokens   int    `json:"outputTokens"`
}

// ExportedUsage is one ledger row of the subject: which model, for what
// purpose and caller, the counters and the outcome. The ledger never holds
// prompts or outputs; the credential reference and the reservation
// bookkeeping are left out.
type ExportedUsage struct {
	UUID           string            `json:"uuid"`
	TenantID       string            `json:"tenantId"`
	ModelUUID      string            `json:"modelUuid"`
	Provider       string            `json:"provider"`
	ModelID        string            `json:"modelId"`
	CredentialKind string            `json:"credentialKind"`
	Caller         string            `json:"caller"`
	Purpose        string            `json:"purpose"`
	InputTokens    int               `json:"inputTokens"`
	OutputTokens   int               `json:"outputTokens"`
	LatencyMs      int64             `json:"latencyMs"`
	Outcome        string            `json:"outcome"`
	ErrorCode      string            `json:"errorCode"`
	Fallback       bool              `json:"fallback"`
	Attempts       []ExportedAttempt `json:"attempts"`
	CreatedAt      time.Time         `json:"createdAt"`
	FinalizedAt    *time.Time        `json:"finalizedAt"`
}

// ExportedBudgetRef names an org budget that references the subject (as
// an extra alert recipient or as its last editor).
type ExportedBudgetRef struct {
	TenantID  string    `json:"tenantId"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ExportedBudgetAlert is a budget alert delivered to the subject.
type ExportedBudgetAlert struct {
	TenantID  string     `json:"tenantId"`
	Period    string     `json:"period"`
	Threshold string     `json:"threshold"`
	CreatedAt time.Time  `json:"createdAt"`
	SentAt    *time.Time `json:"sentAt"`
}

// ExportedLinkedAccount is one Sign in with ChatGPT registration of the
// subject. It never carries a token, the issued client id or the OIDC
// subject.
type ExportedLinkedAccount struct {
	UUID       string     `json:"uuid"`
	Label      string     `json:"label"`
	Email      string     `json:"email"`
	Status     string     `json:"status"`
	LinkedAt   time.Time  `json:"linkedAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

// PersonalDataExport is the llm module's right-of-access bundle. Every
// section is an explicit projection, never a raw model, so credential
// envelopes and endpoints cannot reach the data subject by accident.
type PersonalDataExport struct {
	Grants             []ExportedGrant         `json:"grants"`
	GrantsIssued       []ExportedIssuedGrant   `json:"grantsIssued"`
	CredentialsCreated []ExportedCredentialRef `json:"credentialsCreated"`
	ModelsCreated      []ExportedModelRef      `json:"modelsCreated"`
	Usage              []ExportedUsage         `json:"usage"`
	BudgetRecipientOf  []ExportedBudgetRef     `json:"budgetRecipientOf"`
	BudgetsUpdated     []ExportedBudgetRef     `json:"budgetsUpdated"`
	BudgetAlerts       []ExportedBudgetAlert   `json:"budgetAlertsReceived"`
	Accounts           []ExportedLinkedAccount `json:"accounts"`
}

// PIIProducer exports and erases what the llm module holds about a data
// subject: the grants they hold, their usage ledger rows, the budgets that
// list them as alert recipient, the alerts delivered to them, and the
// actor ids (credential/model createdBy, grant grantedBy, budget
// updatedBy) that name them, and their linked ChatGPT accounts.
//
// Erasure: grants held by the subject are satellite rows with no residue
// worth keeping, so they are deleted; credentials, models, budgets and
// grants issued to others are org assets and stay, with the actor field
// rewritten to models.ErasedActor; the subject leaves every budget's
// recipients and every alert's deliveredTo. Linked accounts go in both
// modes: every registration is deleted and the refresh token of each
// deleted document is revoked at the provider (best-effort, under the row's
// org). Usage rows are the only part
// that depends on the mode: EraseHardDelete deletes the finalized ones,
// EraseAnonymize keeps them with userUuid replaced by a random identifier
// per row and credentialUuid removed. A pending row (a call in flight; its
// reservation is still in the rollup) is anonymized in both modes and is
// removed later by retention.
type PIIProducer struct {
	creds   CredentialDSRRepo
	models  ModelDSRRepo
	grants  GrantDSRRepo
	usage   UsageDSRRepo
	budgets BudgetDSRRepo
	outbox  OutboxDSRRepo
	// accounts / revoke: the linked registrations (revoke may be nil).
	accounts AccountDSRRepo
	revoke   AccountRevoker
	newID    func() string
	logger   *slog.Logger
}

func NewPIIProducer(creds CredentialDSRRepo, mods ModelDSRRepo, grants GrantDSRRepo, usage UsageDSRRepo, budgets BudgetDSRRepo, outbox OutboxDSRRepo,
	accounts AccountDSRRepo, revoke AccountRevoker, logger *slog.Logger) *PIIProducer {
	if logger == nil {
		logger = slog.Default()
	}
	return &PIIProducer{creds: creds, models: mods, grants: grants, usage: usage, budgets: budgets, outbox: outbox,
		accounts: accounts, revoke: revoke, newID: erasedUsageID, logger: logger}
}

// erasedUsageID is the per-row replacement for an anonymized userUuid:
// random, never derived from the subject.
func erasedUsageID() string { return models.ErasedUsagePrefix + uuid.NewString() }

func (p *PIIProducer) Subject() string { return "llm" }

// ExportPersonalData returns a *PersonalDataExport, or (nil, nil) when the
// module holds nothing about the subject. An empty userUUID is refused with
// ErrEmptySubject before any query (it would match every row whose actor
// field is empty, in every org); PurgePersonalData likewise.
func (p *PIIProducer) ExportPersonalData(ctx context.Context, userUUID string) (any, error) {
	if userUUID == "" {
		return nil, ErrEmptySubject
	}
	held, err := p.grants.ListByUserAllTenants(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("llm export: grants held: %w", err)
	}
	issued, err := p.grants.ListByGranter(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("llm export: grants issued: %w", err)
	}
	creds, err := p.creds.ListByCreator(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("llm export: credentials: %w", err)
	}
	mods, err := p.models.ListByCreator(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("llm export: models: %w", err)
	}
	usage, err := p.usage.ListByUserAllTenants(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("llm export: usage: %w", err)
	}
	budgets, err := p.budgets.ListReferencingUser(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("llm export: budgets: %w", err)
	}
	alerts, err := p.outbox.ListDeliveredTo(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("llm export: budget alerts: %w", err)
	}
	accounts, err := p.accounts.ListByUserAllTenants(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("llm export: linked accounts: %w", err)
	}
	if len(held)+len(issued)+len(creds)+len(mods)+len(usage)+len(budgets)+len(alerts)+len(accounts) == 0 {
		return nil, nil
	}

	out := &PersonalDataExport{
		Grants:             make([]ExportedGrant, 0, len(held)),
		GrantsIssued:       make([]ExportedIssuedGrant, 0, len(issued)),
		CredentialsCreated: make([]ExportedCredentialRef, 0, len(creds)),
		ModelsCreated:      make([]ExportedModelRef, 0, len(mods)),
		Usage:              make([]ExportedUsage, 0, len(usage)),
		BudgetRecipientOf:  []ExportedBudgetRef{},
		BudgetsUpdated:     []ExportedBudgetRef{},
		BudgetAlerts:       make([]ExportedBudgetAlert, 0, len(alerts)),
		Accounts:           make([]ExportedLinkedAccount, 0, len(accounts)),
	}
	for _, g := range held {
		out.Grants = append(out.Grants, ExportedGrant{UUID: g.UUID, TenantID: g.TenantID, ModelUUID: g.ModelUUID, CreatedAt: g.CreatedAt})
	}
	for _, g := range issued {
		out.GrantsIssued = append(out.GrantsIssued, ExportedIssuedGrant{ModelUUID: g.ModelUUID, UserUUID: g.UserUUID, CreatedAt: g.CreatedAt})
	}
	for _, c := range creds {
		out.CredentialsCreated = append(out.CredentialsCreated, ExportedCredentialRef{UUID: c.UUID, Name: c.Name, CreatedAt: c.CreatedAt})
	}
	for _, m := range mods {
		out.ModelsCreated = append(out.ModelsCreated, ExportedModelRef{UUID: m.UUID, Name: m.Name, CreatedAt: m.CreatedAt})
	}
	for _, u := range usage {
		attempts := make([]ExportedAttempt, 0, len(u.Attempts))
		for _, a := range u.Attempts {
			attempts = append(attempts, ExportedAttempt{Provider: a.Provider, CredentialKind: a.CredentialKind, Outcome: a.Outcome, InputTokens: a.InputTokens, OutputTokens: a.OutputTokens})
		}
		out.Usage = append(out.Usage, ExportedUsage{
			UUID: u.UUID, TenantID: u.TenantID, ModelUUID: u.ModelUUID, Provider: u.Provider, ModelID: u.ModelID,
			CredentialKind: u.CredentialKind, Caller: u.Caller, Purpose: u.Purpose, InputTokens: u.InputTokens,
			OutputTokens: u.OutputTokens, LatencyMs: u.LatencyMs, Outcome: u.Outcome, ErrorCode: u.ErrorCode,
			Fallback: u.Fallback, Attempts: attempts, CreatedAt: u.CreatedAt, FinalizedAt: u.FinalizedAt,
		})
	}
	for _, b := range budgets {
		ref := ExportedBudgetRef{TenantID: b.TenantID, UpdatedAt: b.UpdatedAt}
		for _, r := range b.NotifyExtraUserUUIDs {
			if r == userUUID {
				out.BudgetRecipientOf = append(out.BudgetRecipientOf, ref)
				break
			}
		}
		if b.UpdatedBy == userUUID {
			out.BudgetsUpdated = append(out.BudgetsUpdated, ref)
		}
	}
	for _, a := range alerts {
		out.BudgetAlerts = append(out.BudgetAlerts, ExportedBudgetAlert{TenantID: a.TenantID, Period: a.Period, Threshold: a.Threshold, CreatedAt: a.CreatedAt, SentAt: a.SentAt})
	}
	for _, a := range accounts {
		out.Accounts = append(out.Accounts, ExportedLinkedAccount{UUID: a.UUID, Label: a.Label, Email: a.Email, Status: a.Status, LinkedAt: a.LinkedAt, LastUsedAt: a.LastUsedAt})
	}
	// Mongo gives no order; a stable bundle is easier to diff and review.
	sort.SliceStable(out.Grants, func(i, j int) bool { return out.Grants[i].CreatedAt.Before(out.Grants[j].CreatedAt) })
	sort.SliceStable(out.GrantsIssued, func(i, j int) bool { return out.GrantsIssued[i].CreatedAt.Before(out.GrantsIssued[j].CreatedAt) })
	sort.SliceStable(out.CredentialsCreated, func(i, j int) bool {
		return out.CredentialsCreated[i].CreatedAt.Before(out.CredentialsCreated[j].CreatedAt)
	})
	sort.SliceStable(out.ModelsCreated, func(i, j int) bool { return out.ModelsCreated[i].CreatedAt.Before(out.ModelsCreated[j].CreatedAt) })
	sort.SliceStable(out.Usage, func(i, j int) bool { return out.Usage[i].CreatedAt.Before(out.Usage[j].CreatedAt) })
	byTenant := func(refs []ExportedBudgetRef) func(i, j int) bool {
		return func(i, j int) bool { return refs[i].TenantID < refs[j].TenantID }
	}
	sort.SliceStable(out.BudgetRecipientOf, byTenant(out.BudgetRecipientOf))
	sort.SliceStable(out.BudgetsUpdated, byTenant(out.BudgetsUpdated))
	sort.SliceStable(out.BudgetAlerts, func(i, j int) bool { return out.BudgetAlerts[i].CreatedAt.Before(out.BudgetAlerts[j].CreatedAt) })
	sort.SliceStable(out.Accounts, func(i, j int) bool { return out.Accounts[i].LinkedAt.Before(out.Accounts[j].LinkedAt) })
	return out, nil
}

// PurgePersonalData erases the subject (see PIIProducer for what each
// mode does). Grants are deleted first so a self-grant (grantee and
// granter) is counted once, as deleted. RowsAnonymized counts documents
// per rewrite, so a budget naming the subject as recipient and editor
// counts twice. The result reflects what was done up to a failure, which
// is also returned.
func (p *PIIProducer) PurgePersonalData(ctx context.Context, userUUID string, mode iface.EraseMode) (iface.PurgeResult, error) {
	if userUUID == "" {
		return iface.PurgeResult{}, ErrEmptySubject
	}
	res := iface.PurgeResult{Collections: []string{
		repository.CollGrants, repository.CollCredentials, repository.CollModels,
		repository.CollUsage, repository.CollBudgets, repository.CollBudgetOutbox, repository.CollUserAccounts,
	}}

	// Linked accounts first, in both modes: delete every registration, then
	// revoke the refresh token of each deleted document at the provider
	// (best-effort, under the row's own org), so no usable token outlives
	// the request. Revoking what the delete removed, not an earlier list,
	// covers a refresh that rotated a set in between; one that stores after
	// the delete finds no row and revokes its own token. Rows deleted before
	// a failure are revoked too.
	deletedAccounts, err := p.accounts.DeleteByUser(ctx, userUUID)
	if p.revoke != nil {
		for _, a := range deletedAccounts {
			p.revoke(ctx, a)
		}
	}
	if err != nil {
		return iface.PurgeResult{}, fmt.Errorf("llm purge: linked accounts delete: %w", err)
	}
	res.RowsDeleted = len(deletedAccounts)

	deleted, err := p.grants.DeleteByUser(ctx, userUUID)
	if err != nil {
		return res, fmt.Errorf("llm purge: grants held: %w", err)
	}
	res.RowsDeleted += int(deleted)

	if mode == iface.EraseHardDelete {
		n, err := p.usage.DeleteByUser(ctx, userUUID)
		if err != nil {
			return res, fmt.Errorf("llm purge: usage: %w", err)
		}
		res.RowsDeleted += int(n)
	}
	// Anonymize mode: every row. Hard delete: the pending rows the delete
	// skipped, so nothing left names the subject.
	n, err := p.usage.AnonymizeByUser(ctx, userUUID, p.newID)
	if err != nil {
		return res, fmt.Errorf("llm purge: usage anonymize: %w", err)
	}
	res.RowsAnonymized += int(n)

	steps := []struct {
		what string
		run  func(context.Context, string) (int64, error)
	}{
		{"grants issued", p.grants.PseudonymizeGranter},
		{"credentials", p.creds.PseudonymizeCreator},
		{"models", p.models.PseudonymizeCreator},
		{"budget recipients", p.budgets.RemoveRecipient},
		{"budget editor", p.budgets.PseudonymizeUpdater},
		{"budget alerts", p.outbox.RemoveDelivered},
	}
	for _, st := range steps {
		n, err := st.run(ctx, userUUID)
		if err != nil {
			return res, fmt.Errorf("llm purge: %s: %w", st.what, err)
		}
		res.RowsAnonymized += int(n)
	}
	return res, nil
}

var _ iface.PIIProducer = (*PIIProducer)(nil)
