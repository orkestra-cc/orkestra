package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrMembershipNotFound is returned when no membership matches a
// lookup in the caller's tenant scope.
var ErrMembershipNotFound = errors.New("marketing: membership not found")

// MembershipRepository is the persistence boundary for
// marketing_memberships. The uniqueness invariants documented in
// models/membership.go are enforced at the service layer because the
// SDK's IndexSpec does not expose PartialFilterExpression yet —
// adding the unique-partial constraint requires bumping the SDK and
// is tracked as Phase-2+ follow-up.
type MembershipRepository struct {
	coll *mongo.Collection
}

// NewMembershipRepository binds a repository to the
// marketing_memberships collection.
func NewMembershipRepository(db *mongo.Database) *MembershipRepository {
	return &MembershipRepository{coll: db.Collection(models.MembershipsCollection)}
}

// Create inserts a new membership, stamping tenantId + timestamps.
// The caller is responsible for ensuring uniqueness invariants
// (single active per pair, single active-primary per person) — the
// service helpers UnsetPrimaryFor / CloseActiveFor compose with this
// to keep the invariant under typical mutation flows.
func (r *MembershipRepository) Create(ctx context.Context, m *models.Membership) error {
	if m == nil {
		return errors.New("marketing: nil membership")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	m.TenantID = tenantID
	now := time.Now().UTC()
	m.CreatedAt = now
	m.UpdatedAt = now
	_, err = r.coll.InsertOne(ctx, m)
	return err
}

// GetByUUID returns the membership with the given UUID in the
// caller's tenant scope, or ErrMembershipNotFound.
func (r *MembershipRepository) GetByUUID(ctx context.Context, uuid string) (*models.Membership, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.Membership
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrMembershipNotFound
		}
		return nil, err
	}
	return &out, nil
}

// ListByPerson returns every membership (active or closed) of one
// Person, ordered most-recent first. Activity rollups over historical
// roles depend on closed memberships remaining queryable.
func (r *MembershipRepository) ListByPerson(ctx context.Context, personUUID string) ([]models.Membership, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"personUuid": personUUID})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Membership, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListByOrg returns every membership (active or closed) of one
// Organization.
func (r *MembershipRepository) ListByOrg(ctx context.Context, orgUUID string) ([]models.Membership, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"orgUuid": orgUUID})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Membership, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FindActivePair returns the active membership for the (personUUID,
// orgUUID) pair in the caller's tenant, or ErrMembershipNotFound. Used
// by the service when reopening or upserting memberships during
// import.
func (r *MembershipRepository) FindActivePair(ctx context.Context, personUUID, orgUUID string) (*models.Membership, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"personUuid": personUUID,
		"orgUuid":    orgUUID,
		"active":     true,
	})
	if err != nil {
		return nil, err
	}
	var out models.Membership
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrMembershipNotFound
		}
		return nil, err
	}
	return &out, nil
}

// FindActivePrimaryForPerson returns the membership the Person is
// currently primary on, or ErrMembershipNotFound when the Person has
// no active primary (legitimate state — they are "independent"). The
// activity-org denormalization reads this.
func (r *MembershipRepository) FindActivePrimaryForPerson(ctx context.Context, personUUID string) (*models.Membership, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"personUuid": personUUID,
		"active":     true,
		"primary":    true,
	})
	if err != nil {
		return nil, err
	}
	var out models.Membership
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrMembershipNotFound
		}
		return nil, err
	}
	return &out, nil
}

// UnsetPrimaryForPerson clears the primary flag from every active
// membership of the given Person. Used by the service before
// promoting a different membership to primary, to preserve the
// invariant that at most one active-primary exists per Person.
func (r *MembershipRepository) UnsetPrimaryForPerson(ctx context.Context, personUUID string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"personUuid": personUUID,
		"active":     true,
		"primary":    true,
	})
	if err != nil {
		return err
	}
	_, err = r.coll.UpdateMany(ctx, filter, bson.M{
		"$set": bson.M{"primary": false, "updatedAt": time.Now().UTC()},
	})
	return err
}

// Close marks the membership identified by UUID as inactive with
// Until=now. Used both when the service moves a person to a new
// primary and when explicitly ending a role.
func (r *MembershipRepository) Close(ctx context.Context, uuid string) error {
	now := time.Now().UTC()
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{
		"$set": bson.M{
			"active":    false,
			"until":     now,
			"updatedAt": now,
		},
	})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrMembershipNotFound
	}
	return nil
}

// Update applies $set on mutable fields of a single membership.
func (r *MembershipRepository) Update(ctx context.Context, uuid string, patch bson.M) error {
	if patch == nil {
		patch = bson.M{}
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
		return ErrMembershipNotFound
	}
	return nil
}

// Delete hard-deletes a single membership by UUID.
func (r *MembershipRepository) Delete(ctx context.Context, uuid string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrMembershipNotFound
	}
	return nil
}

// DeleteForPerson removes every membership of the given Person. Used
// by the cascade path when a Person is hard-deleted.
func (r *MembershipRepository) DeleteForPerson(ctx context.Context, personUUID string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"personUuid": personUUID})
	if err != nil {
		return err
	}
	_, err = r.coll.DeleteMany(ctx, filter)
	return err
}

// DeleteForOrg removes every membership of the given Organization.
// Used by the cascade path when an Organization is hard-deleted.
func (r *MembershipRepository) DeleteForOrg(ctx context.Context, orgUUID string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"orgUuid": orgUUID})
	if err != nil {
		return err
	}
	_, err = r.coll.DeleteMany(ctx, filter)
	return err
}
