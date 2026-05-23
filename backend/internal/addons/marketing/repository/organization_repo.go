package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/importers/match"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrOrgNotFound is returned when an Organization lookup finds no
// document in the tenant scope. Distinct from mongo.ErrNoDocuments so
// the handler layer can map it cleanly to a 404.
var ErrOrgNotFound = errors.New("marketing: organization not found")

// OrganizationRepository is the persistence boundary for
// marketing_organizations. All operations are tenant-scoped via
// pkg/sdk/tenantrepo — the context must carry a resolved tenantID.
type OrganizationRepository struct {
	coll *mongo.Collection
}

// NewOrganizationRepository binds a repository to the
// marketing_organizations collection on db.
func NewOrganizationRepository(db *mongo.Database) *OrganizationRepository {
	return &OrganizationRepository{coll: db.Collection(models.OrganizationsCollection)}
}

// Create inserts a new Organization, stamping tenantId, created/updated
// timestamps, and normalising VAT/TaxCode. The caller is responsible
// for populating UUID (handlers + importers do this so the row's
// external identity is known before the insert lands).
func (r *OrganizationRepository) Create(ctx context.Context, org *models.Organization) error {
	if org == nil {
		return errors.New("marketing: nil organization")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	org.TenantID = tenantID
	org.VAT = NormalizeVAT(org.VAT)
	org.TaxCode = NormalizeTaxCode(org.TaxCode)
	org.LegalNameNormalized = match.NormalizeLegalName(org.LegalName)
	now := time.Now().UTC()
	org.CreatedAt = now
	org.UpdatedAt = now
	_, err = r.coll.InsertOne(ctx, org)
	return err
}

// GetByUUID returns the organization with the given UUID inside the
// caller's tenant scope, or ErrOrgNotFound when no document matches.
func (r *OrganizationRepository) GetByUUID(ctx context.Context, uuid string) (*models.Organization, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.Organization
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrOrgNotFound
		}
		return nil, err
	}
	return &out, nil
}

// LookupByVAT returns the organization in the caller's tenant whose
// normalized VAT matches the input. Returns ErrOrgNotFound when no
// match. Used by the importer dedup path; callers should normalise
// the lookup key with NormalizeVAT before calling, or let this
// function do it (idempotent).
func (r *OrganizationRepository) LookupByVAT(ctx context.Context, vat string) (*models.Organization, error) {
	vat = NormalizeVAT(vat)
	if vat == "" {
		return nil, ErrOrgNotFound
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{"vat": vat})
	if err != nil {
		return nil, err
	}
	var out models.Organization
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrOrgNotFound
		}
		return nil, err
	}
	return &out, nil
}

// LookupByTaxCode is the tax-code analogue of LookupByVAT. Used as the
// secondary dedup key when VAT is empty on the incoming row.
func (r *OrganizationRepository) LookupByTaxCode(ctx context.Context, taxCode string) (*models.Organization, error) {
	taxCode = NormalizeTaxCode(taxCode)
	if taxCode == "" {
		return nil, ErrOrgNotFound
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{"taxCode": taxCode})
	if err != nil {
		return nil, err
	}
	var out models.Organization
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrOrgNotFound
		}
		return nil, err
	}
	return &out, nil
}

// FindSoftMatchByLegalName backs the soft-match dedup pass for
// organizations. The pipeline calls it after VAT + TaxCode strict
// miss; an exact match on legalNameNormalized routes the row to the
// review queue rather than auto-merging (the legal-name signal is too
// noisy to commit without operator review). Returns ErrOrgNotFound
// when no candidate matches.
func (r *OrganizationRepository) FindSoftMatchByLegalName(ctx context.Context, legalName string) (*models.Organization, error) {
	n := match.NormalizeLegalName(legalName)
	if n == "" {
		return nil, ErrOrgNotFound
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{"legalNameNormalized": n})
	if err != nil {
		return nil, err
	}
	var out models.Organization
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrOrgNotFound
		}
		return nil, err
	}
	return &out, nil
}

// ListFilter parameterises the read surface exposed to the handler. All
// fields are optional — empty ListFilter{} returns recent rows for the
// caller's tenant.
type ListFilter struct {
	Kind     models.OrganizationKind
	TagUUIDs []string
	Source   string
	Limit    int64
	Skip     int64
}

// List returns organizations matching filter, newest-first by
// updatedAt. Limit defaults to 50 when zero, capped at 500 to keep
// pages bounded.
func (r *OrganizationRepository) List(ctx context.Context, f ListFilter) ([]models.Organization, error) {
	base := bson.M{}
	if f.Kind != "" {
		base["kind"] = f.Kind
	}
	if len(f.TagUUIDs) > 0 {
		base["tags"] = bson.M{"$in": f.TagUUIDs}
	}
	if f.Source != "" {
		base["sources.importer"] = f.Source
	}
	filter, err := tenantrepo.Scope(ctx, base)
	if err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "updatedAt", Value: -1}}).
		SetLimit(limit).
		SetSkip(f.Skip)
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Organization, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update replaces the mutable fields on the organization identified by
// UUID inside the caller's tenant. Returns ErrOrgNotFound when no row
// matches. The patch is applied via $set so callers can omit fields
// they do not intend to mutate.
func (r *OrganizationRepository) Update(ctx context.Context, uuid string, patch bson.M) error {
	if patch == nil {
		patch = bson.M{}
	}
	// Normalise dedup keys when present in the patch so the unique
	// indexes match the lookup path.
	if v, ok := patch["vat"].(string); ok {
		patch["vat"] = NormalizeVAT(v)
	}
	if v, ok := patch["taxCode"].(string); ok {
		patch["taxCode"] = NormalizeTaxCode(v)
	}
	// Soft-match denorm: keep legalNameNormalized in lock-step with
	// the source-of-truth legalName field.
	if v, ok := patch["legalName"].(string); ok {
		patch["legalNameNormalized"] = match.NormalizeLegalName(v)
	}
	patch["updatedAt"] = time.Now().UTC()
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": patch})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrOrgNotFound
	}
	return nil
}

// AppendSource pushes a new provenance entry onto the organization's
// sources[] array. Used by importers + manual create paths to record
// where the row came from without overwriting prior provenance.
func (r *OrganizationRepository) AppendSource(ctx context.Context, uuid string, src models.ProvenanceSource) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	_, err = r.coll.UpdateOne(ctx, filter, bson.M{
		"$push": bson.M{"sources": src},
		"$set":  bson.M{"updatedAt": time.Now().UTC()},
	})
	return err
}

// Delete hard-deletes an organization by UUID inside the caller's
// tenant. The membership cascade is the service's responsibility —
// the repository does not chain.
func (r *OrganizationRepository) Delete(ctx context.Context, uuid string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrOrgNotFound
	}
	return nil
}
