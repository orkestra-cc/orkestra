// Package repository provides tenant-scoped CRUD for identity IdP configs.
//
// Every query must be tenant-scoped — an external client inspecting or
// updating another tenant's IdP would leak issuer/clientID/etc. The module
// uses tenantrepo.Scope for all reads and tenantrepo.StampInsertM for
// inserts to enforce that invariant at the helper level.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra-cc/orkestra-addon-identity/models"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrIdPConfigNotFound is returned when a lookup finds no document in the
// tenant scope. Kept distinct from mongo.ErrNoDocuments so handlers can map
// it cleanly to a 404.
var ErrIdPConfigNotFound = errors.New("identity: IdP config not found")

// Repository exposes tenant-scoped CRUD on identity_idp_configs. The
// context must carry a resolved tenantID — middleware.RequireAuth puts it
// there; handlers that bypass auth (notably the public OIDC callback) must
// not use this repository directly, they resolve by UUID via Internal* APIs.
type Repository struct {
	coll *mongo.Collection
}

// New constructs a Repository against the shared Mongo database.
func New(db *mongo.Database) *Repository {
	return &Repository{coll: db.Collection(models.IdPConfigsCollection)}
}

// Create inserts a new config, stamping tenantId from context and the
// create/update timestamps.
func (r *Repository) Create(ctx context.Context, cfg *models.IdPConfig) error {
	if cfg == nil {
		return errors.New("identity: nil IdP config")
	}
	now := time.Now().UTC()
	cfg.CreatedAt = now
	cfg.UpdatedAt = now
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	cfg.TenantID = tenantID
	_, err = r.coll.InsertOne(ctx, cfg)
	return err
}

// GetForCurrentTenant returns the single OIDC config owned by the tenant
// in context, or ErrIdPConfigNotFound when none is configured. The v1
// schema pins one OIDC config per tenant via the (tenantId, protocol)
// composite unique index, so this always returns ≤1 row.
func (r *Repository) GetForCurrentTenant(ctx context.Context, protocol string) (*models.IdPConfig, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"protocol": protocol})
	if err != nil {
		return nil, err
	}
	var out models.IdPConfig
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrIdPConfigNotFound
		}
		return nil, err
	}
	return &out, nil
}

// UpdateForCurrentTenant replaces the mutable fields on the tenant's IdP
// config. `cfg.UUID` is ignored — we look up by (tenantId, protocol) so a
// renamed or reminted UUID never creates a duplicate row.
func (r *Repository) UpdateForCurrentTenant(ctx context.Context, cfg *models.IdPConfig) error {
	if cfg == nil {
		return errors.New("identity: nil IdP config")
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{"protocol": cfg.Protocol})
	if err != nil {
		return err
	}
	cfg.UpdatedAt = time.Now().UTC()
	update := bson.M{"$set": bson.M{
		"displayName":  cfg.DisplayName,
		"issuerURL":    cfg.IssuerURL,
		"clientId":     cfg.ClientID,
		"clientSecret": cfg.ClientSecret, // already encrypted by service layer
		"redirectURL":  cfg.RedirectURL,
		"scopes":       cfg.Scopes,
		"subClaim":     cfg.SubClaim,
		"emailClaim":   cfg.EmailClaim,
		"nameClaim":    cfg.NameClaim,
		"enabled":      cfg.Enabled,
		"updatedAt":    cfg.UpdatedAt,
	}}
	res, err := r.coll.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrIdPConfigNotFound
	}
	return nil
}

// DeleteForCurrentTenant removes the tenant's IdP config for the given
// protocol. Returns ErrIdPConfigNotFound when no row matched so callers
// can map to 404.
func (r *Repository) DeleteForCurrentTenant(ctx context.Context, protocol string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"protocol": protocol})
	if err != nil {
		return err
	}
	res, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrIdPConfigNotFound
	}
	return nil
}

// GetByUUIDUnscoped is the only non-tenant-scoped read on this collection.
// Used by the public OIDC start/callback handlers: the state parameter
// carries the idpConfigUUID (and the state is signed/bound at issue time),
// so the handler resolves the config by UUID without a tenant in context.
//
// Do not call this from admin/authenticated code — always use
// GetForCurrentTenant there.
func (r *Repository) GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.IdPConfig, error) {
	var out models.IdPConfig
	if err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrIdPConfigNotFound
		}
		return nil, err
	}
	return &out, nil
}

// GetBySlugUnscoped resolves a config by tenant slug + protocol for the
// public `/v1/identity/oidc/{tenantSlug}/start` entry point. The handler
// must still require the returned config to be `Enabled` before starting
// a flow — repository does not filter on enabled.
func (r *Repository) GetByTenantUUIDUnscoped(ctx context.Context, tenantUUID, protocol string) (*models.IdPConfig, error) {
	var out models.IdPConfig
	if err := r.coll.FindOne(ctx, bson.M{"tenantId": tenantUUID, "protocol": protocol}).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrIdPConfigNotFound
		}
		return nil, err
	}
	return &out, nil
}
