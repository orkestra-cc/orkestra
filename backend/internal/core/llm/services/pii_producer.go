package services

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

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

var (
	_ GrantDSRRepo      = (*repository.Grants)(nil)
	_ CredentialDSRRepo = (*repository.Credentials)(nil)
	_ ModelDSRRepo      = (*repository.Models)(nil)
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

// PersonalDataExport is the llm module's right-of-access bundle. Every
// section is an explicit projection, never a raw model, so credential
// envelopes and endpoints cannot reach the data subject by accident.
type PersonalDataExport struct {
	Grants             []ExportedGrant         `json:"grants"`
	GrantsIssued       []ExportedIssuedGrant   `json:"grantsIssued"`
	CredentialsCreated []ExportedCredentialRef `json:"credentialsCreated"`
	ModelsCreated      []ExportedModelRef      `json:"modelsCreated"`
}

// PIIProducer exports and erases what the llm module holds about a data
// subject: the grants they hold, and the actor ids (credential/model
// createdBy, grant grantedBy) that name them. PR 2 adds usage rows and
// budget recipients, PR 3 linked accounts.
//
// Erasure (identical in both modes): grants held by the subject are
// satellite rows with no residue worth keeping, so they are deleted;
// credentials, models and grants issued to others are org assets and stay,
// with the actor field rewritten to models.ErasedActor.
type PIIProducer struct {
	creds  CredentialDSRRepo
	models ModelDSRRepo
	grants GrantDSRRepo
	logger *slog.Logger
}

func NewPIIProducer(creds CredentialDSRRepo, mods ModelDSRRepo, grants GrantDSRRepo, logger *slog.Logger) *PIIProducer {
	if logger == nil {
		logger = slog.Default()
	}
	return &PIIProducer{creds: creds, models: mods, grants: grants, logger: logger}
}

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
	if len(held)+len(issued)+len(creds)+len(mods) == 0 {
		return nil, nil
	}

	out := &PersonalDataExport{
		Grants:             make([]ExportedGrant, 0, len(held)),
		GrantsIssued:       make([]ExportedIssuedGrant, 0, len(issued)),
		CredentialsCreated: make([]ExportedCredentialRef, 0, len(creds)),
		ModelsCreated:      make([]ExportedModelRef, 0, len(mods)),
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
	// Mongo gives no order; a stable bundle is easier to diff and review.
	sort.SliceStable(out.Grants, func(i, j int) bool { return out.Grants[i].CreatedAt.Before(out.Grants[j].CreatedAt) })
	sort.SliceStable(out.GrantsIssued, func(i, j int) bool { return out.GrantsIssued[i].CreatedAt.Before(out.GrantsIssued[j].CreatedAt) })
	sort.SliceStable(out.CredentialsCreated, func(i, j int) bool {
		return out.CredentialsCreated[i].CreatedAt.Before(out.CredentialsCreated[j].CreatedAt)
	})
	sort.SliceStable(out.ModelsCreated, func(i, j int) bool { return out.ModelsCreated[i].CreatedAt.Before(out.ModelsCreated[j].CreatedAt) })
	return out, nil
}

// PurgePersonalData deletes the subject's grants and pseudonymizes every
// actor field that names them. Grants are deleted first so a self-grant
// (grantee and granter) is counted once, as deleted. The result reflects
// what was done up to a failure, which is also returned.
func (p *PIIProducer) PurgePersonalData(ctx context.Context, userUUID string, _ iface.EraseMode) (iface.PurgeResult, error) {
	if userUUID == "" {
		return iface.PurgeResult{}, ErrEmptySubject
	}
	res := iface.PurgeResult{Collections: []string{repository.CollGrants, repository.CollCredentials, repository.CollModels}}

	deleted, err := p.grants.DeleteByUser(ctx, userUUID)
	if err != nil {
		return iface.PurgeResult{}, fmt.Errorf("llm purge: grants held: %w", err)
	}
	res.RowsDeleted = int(deleted)

	n, err := p.grants.PseudonymizeGranter(ctx, userUUID)
	if err != nil {
		return res, fmt.Errorf("llm purge: grants issued: %w", err)
	}
	res.RowsAnonymized += int(n)

	n, err = p.creds.PseudonymizeCreator(ctx, userUUID)
	if err != nil {
		return res, fmt.Errorf("llm purge: credentials: %w", err)
	}
	res.RowsAnonymized += int(n)

	n, err = p.models.PseudonymizeCreator(ctx, userUUID)
	if err != nil {
		return res, fmt.Errorf("llm purge: models: %w", err)
	}
	res.RowsAnonymized += int(n)
	return res, nil
}

var _ iface.PIIProducer = (*PIIProducer)(nil)
