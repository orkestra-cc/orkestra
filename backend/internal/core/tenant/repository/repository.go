package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	CollOrgs        = "tenant_orgs"
	CollMemberships = "tenant_memberships"
	CollInvites     = "tenant_org_invites"
	// CollAncestors materializes the transitive-closure hierarchy for
	// external multi-tenant clients. See ADR-0001.
	CollAncestors = "tenant_ancestors"
)

var ErrNotFound = errors.New("tenant: not found")

type Repository struct {
	db *mongo.Database
}

func New(db *mongo.Database) *Repository {
	return &Repository{db: db}
}

// --- Orgs ---

func (r *Repository) CreateOrg(ctx context.Context, o *models.Org) error {
	o.CreatedAt = time.Now()
	o.UpdatedAt = o.CreatedAt
	// Defaults applied at the repository boundary so callers that predate
	// ADR-0001 (setup wizard, legacy tests) continue to produce valid rows.
	// New code SHOULD set Kind + Status explicitly.
	if !o.Kind.Valid() {
		o.Kind = models.TenantKindInternal
	}
	if !o.Status.Valid() {
		o.Status = models.TenantStatusActive
	}
	if o.Region == "" {
		o.Region = "eu-west"
	}
	_, err := r.db.Collection(CollOrgs).InsertOne(ctx, o)
	return err
}

// ListOrgsByKind returns every tenant of a specific tier. Used by platform
// operators to inspect the internal/external split (e.g. billing dashboards
// that only care about Tier-2 clients).
func (r *Repository) ListOrgsByKind(ctx context.Context, kind models.TenantKind, includeDeleted bool) ([]models.Org, error) {
	filter := bson.M{"kind": string(kind)}
	if !includeDeleted {
		filter["deletedAt"] = nil
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cur, err := r.db.Collection(CollOrgs).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Org
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateOrgStatus transitions a tenant to a new lifecycle state. The caller
// is responsible for validating the transition (services/service.go enforces
// the state machine). SoftDeleteOrg is kept as a convenience alias that sets
// Status=archived in addition to the legacy DeletedAt field.
func (r *Repository) UpdateOrgStatus(ctx context.Context, uuid string, status models.TenantStatus) error {
	update := bson.M{"status": string(status), "updatedAt": time.Now()}
	if status == models.TenantStatusArchived {
		update["archivedAt"] = time.Now()
	}
	if status == models.TenantStatusPurged {
		update["purgedAt"] = time.Now()
	}
	res, err := r.db.Collection(CollOrgs).UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{"$set": update})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Tenant hierarchy (closure table) ---

// InsertSelfAncestor inserts the depth=0 self-row for a freshly created
// tenant. Called inside CreateOrg (via the service).
func (r *Repository) InsertSelfAncestor(ctx context.Context, tenantUUID string) error {
	_, err := r.db.Collection(CollAncestors).InsertOne(ctx, &models.TenantAncestor{
		DescendantUUID: tenantUUID,
		AncestorUUID:   tenantUUID,
		Depth:          0,
		CreatedAt:      time.Now(),
	})
	return err
}

// AttachToParent populates the closure table for a new descendant by copying
// every ancestor row of the parent and adding one for the direct parent.
// Call this when creating a sub-tenant. It is idempotent-unsafe — the caller
// must only invoke it once per new tenant (the closure shape is linked to
// tenant creation, not mutation).
func (r *Repository) AttachToParent(ctx context.Context, childUUID, parentUUID string) error {
	// Pull all ancestors of the parent (including the parent itself at depth 0).
	cur, err := r.db.Collection(CollAncestors).Find(ctx, bson.M{"descendantUUID": parentUUID})
	if err != nil {
		return err
	}
	defer cur.Close(ctx)
	now := time.Now()
	var rows []any
	for cur.Next(ctx) {
		var anc models.TenantAncestor
		if err := cur.Decode(&anc); err != nil {
			return err
		}
		rows = append(rows, &models.TenantAncestor{
			DescendantUUID: childUUID,
			AncestorUUID:   anc.AncestorUUID,
			Depth:          anc.Depth + 1,
			CreatedAt:      now,
		})
	}
	if err := cur.Err(); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	_, err = r.db.Collection(CollAncestors).InsertMany(ctx, rows)
	return err
}

// ListAncestors returns every ancestor row of a tenant, sorted by depth
// ascending (self first, root last). Includes the self-row at depth=0.
func (r *Repository) ListAncestors(ctx context.Context, tenantUUID string) ([]models.TenantAncestor, error) {
	opts := options.Find().SetSort(bson.D{{Key: "depth", Value: 1}})
	cur, err := r.db.Collection(CollAncestors).Find(ctx, bson.M{"descendantUUID": tenantUUID}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.TenantAncestor
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListDescendantUUIDs returns the UUIDs of every descendant (including self)
// of the given tenant. Used for bulk operations that cascade — e.g. archiving
// a parent must flag every sub-tenant as archived.
func (r *Repository) ListDescendantUUIDs(ctx context.Context, ancestorUUID string) ([]string, error) {
	cur, err := r.db.Collection(CollAncestors).Find(ctx, bson.M{"ancestorUUID": ancestorUUID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []string
	for cur.Next(ctx) {
		var anc models.TenantAncestor
		if err := cur.Decode(&anc); err != nil {
			return nil, err
		}
		out = append(out, anc.DescendantUUID)
	}
	return out, cur.Err()
}

// IsAncestorOf returns true if ancestorUUID is an ancestor (at any depth,
// including equal) of descendantUUID. O(1) indexed lookup.
func (r *Repository) IsAncestorOf(ctx context.Context, ancestorUUID, descendantUUID string) (bool, error) {
	err := r.db.Collection(CollAncestors).FindOne(ctx, bson.M{
		"ancestorUUID":   ancestorUUID,
		"descendantUUID": descendantUUID,
	}).Err()
	if err == nil {
		return true, nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	return false, err
}

func (r *Repository) GetOrgByUUID(ctx context.Context, uuid string) (*models.Org, error) {
	var o models.Org
	err := r.db.Collection(CollOrgs).FindOne(ctx, bson.M{"uuid": uuid, "deletedAt": nil}).Decode(&o)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &o, err
}

func (r *Repository) GetOrgBySlug(ctx context.Context, slug string) (*models.Org, error) {
	var o models.Org
	err := r.db.Collection(CollOrgs).FindOne(ctx, bson.M{"slug": slug, "deletedAt": nil}).Decode(&o)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &o, err
}

func (r *Repository) UpdateOrg(ctx context.Context, uuid string, update bson.M) error {
	update["updatedAt"] = time.Now()
	res, err := r.db.Collection(CollOrgs).UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{"$set": update})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) SoftDeleteOrg(ctx context.Context, uuid string) error {
	now := time.Now()
	res, err := r.db.Collection(CollOrgs).UpdateOne(ctx, bson.M{"uuid": uuid}, bson.M{"$set": bson.M{"deletedAt": now, "updatedAt": now}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAllOrgs returns every org in the system for platform-admin views.
// When includeDeleted is false, soft-deleted orgs are filtered out.
func (r *Repository) ListAllOrgs(ctx context.Context, includeDeleted bool) ([]models.Org, error) {
	filter := bson.M{}
	if !includeDeleted {
		filter["deletedAt"] = nil
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cur, err := r.db.Collection(CollOrgs).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Org
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CountMembersByOrgs returns a map orgUUID -> member count for the given
// orgs. Uses a single $group aggregation on tenant_memberships so list
// views can attach member counts without N round trips.
func (r *Repository) CountMembersByOrgs(ctx context.Context, orgUUIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(orgUUIDs))
	if len(orgUUIDs) == 0 {
		return out, nil
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"orgId": bson.M{"$in": orgUUIDs}}}},
		{{Key: "$group", Value: bson.M{"_id": "$orgId", "count": bson.M{"$sum": 1}}}},
	}
	cur, err := r.db.Collection(CollMemberships).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var row struct {
			ID    string `bson:"_id"`
			Count int    `bson:"count"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		out[row.ID] = row.Count
	}
	return out, cur.Err()
}

// --- Memberships ---

func (r *Repository) CreateMembership(ctx context.Context, m *models.Membership) error {
	m.JoinedAt = time.Now()
	_, err := r.db.Collection(CollMemberships).InsertOne(ctx, m)
	return err
}

func (r *Repository) DeleteMembership(ctx context.Context, userUUID, orgUUID string) error {
	_, err := r.db.Collection(CollMemberships).DeleteOne(ctx, bson.M{"userUUID": userUUID, "orgId": orgUUID})
	return err
}

func (r *Repository) ListMembershipsByUser(ctx context.Context, userUUID string) ([]models.Membership, error) {
	cur, err := r.db.Collection(CollMemberships).Find(ctx, bson.M{
		"userUUID": userUUID,
		"$or":      []bson.M{{"expiresAt": nil}, {"expiresAt": bson.M{"$gt": time.Now()}}},
	})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Membership
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) ListMembershipsByOrg(ctx context.Context, orgUUID string) ([]models.Membership, error) {
	cur, err := r.db.Collection(CollMemberships).Find(ctx, bson.M{"orgId": orgUUID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Membership
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) GetMembership(ctx context.Context, userUUID, orgUUID string) (*models.Membership, error) {
	var m models.Membership
	err := r.db.Collection(CollMemberships).FindOne(ctx, bson.M{"userUUID": userUUID, "orgId": orgUUID}).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &m, err
}

func (r *Repository) UpdateMembershipRoles(ctx context.Context, userUUID, orgUUID string, roles []string) error {
	_, err := r.db.Collection(CollMemberships).UpdateOne(ctx,
		bson.M{"userUUID": userUUID, "orgId": orgUUID},
		bson.M{"$set": bson.M{"roles": roles}})
	return err
}

// --- Invites ---

func (r *Repository) CreateInvite(ctx context.Context, inv *models.Invite) error {
	inv.CreatedAt = time.Now()
	_, err := r.db.Collection(CollInvites).InsertOne(ctx, inv)
	return err
}

// GetInviteByTokenHash looks up an invite by the SHA-256 hash of the raw
// token. The caller (tenant service) computes the hash from the plaintext
// token supplied by the user; we never store or query the plaintext.
func (r *Repository) GetInviteByTokenHash(ctx context.Context, tokenHash string) (*models.Invite, error) {
	var inv models.Invite
	err := r.db.Collection(CollInvites).FindOne(ctx, bson.M{"tokenHash": tokenHash}).Decode(&inv)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &inv, err
}

func (r *Repository) MarkInviteAccepted(ctx context.Context, uuid string) error {
	now := time.Now()
	_, err := r.db.Collection(CollInvites).UpdateOne(ctx,
		bson.M{"uuid": uuid},
		bson.M{"$set": bson.M{"acceptedAt": now}})
	return err
}

// ListInvitesByOrg returns invites for an org. When onlyPending is true,
// accepted and expired invites are filtered out.
func (r *Repository) ListInvitesByOrg(ctx context.Context, orgUUID string, onlyPending bool) ([]models.Invite, error) {
	filter := bson.M{"orgId": orgUUID}
	if onlyPending {
		filter["acceptedAt"] = nil
		filter["expiresAt"] = bson.M{"$gt": time.Now()}
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cur, err := r.db.Collection(CollInvites).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Invite
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteInvite hard-deletes an invite scoped to the given org. The orgUUID
// parameter is there to prevent cross-org ID spoofing from an attacker who
// guessed an invite UUID.
func (r *Repository) DeleteInvite(ctx context.Context, orgUUID, inviteUUID string) error {
	res, err := r.db.Collection(CollInvites).DeleteOne(ctx, bson.M{"uuid": inviteUUID, "orgId": orgUUID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
