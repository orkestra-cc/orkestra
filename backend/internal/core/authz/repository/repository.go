package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/authz/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	CollPermissions = "authz_permissions"
	CollRoles       = "authz_roles"
	CollBindings    = "authz_bindings"

	// ADR-0003 PR-B: tier-split role catalogs. The operator catalog
	// continues to seed the six platform system roles plus any
	// operator-side custom roles; the client catalog will eventually
	// hold the four canonical tenant roles (owner/admin/member/viewer)
	// plus client-defined custom roles. Bindings + permissions stay
	// single — both reference the per-tier role UUID.
	CollOperatorRoles = "operator_roles"
	CollClientRoles   = "client_roles"
)

var ErrNotFound = errors.New("authz: not found")

// ErrRoleExists is returned by InsertRole when the (tenantId, name) pair
// it was asked to insert is already taken. Surfaced from the collection's
// unique compound index rather than from a read-then-write check, so two
// callers racing the same name cannot both pass.
var ErrRoleExists = errors.New("authz: a role with that name already exists in this tenant")

type Repository struct {
	db *mongo.Database
}

func New(db *mongo.Database) *Repository { return &Repository{db: db} }

// --- Permissions catalog ---

func (r *Repository) UpsertPermission(ctx context.Context, p *models.Permission) error {
	p.CreatedAt = time.Now()
	_, err := r.db.Collection(CollPermissions).UpdateOne(ctx,
		bson.M{"key": p.Key},
		bson.M{"$set": bson.M{
			"module":      p.Module,
			"description": p.Description,
			"system":      p.System,
			"createdAt":   p.CreatedAt,
		}},
		options.Update().SetUpsert(true))
	return err
}

func (r *Repository) ListPermissions(ctx context.Context) ([]models.Permission, error) {
	cur, err := r.db.Collection(CollPermissions).Find(ctx, bson.M{}, options.Find().SetSort(bson.M{"key": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Permission
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) ListAllPermissionKeys(ctx context.Context) ([]string, error) {
	cur, err := r.db.Collection(CollPermissions).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Permission
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(out))
	for _, p := range out {
		keys = append(keys, p.Key)
	}
	return keys, nil
}

// --- Roles ---

// UpsertRole is the SEEDER's write: it is keyed on (tenantId, name), so
// re-running SeedSystemRoles on every boot converges on one row per
// system role instead of duplicating it. That key is also why it must
// never serve role CREATION — a create whose name is already taken would
// rewrite the incumbent's uuid and permissions in place, dangling every
// binding that points at the old uuid. Use InsertRole for that.
func (r *Repository) UpsertRole(ctx context.Context, role *models.Role) error {
	role.UpdatedAt = time.Now()
	if role.CreatedAt.IsZero() {
		role.CreatedAt = role.UpdatedAt
	}
	// IsActive is set on insert only — once a role exists, its active state
	// is controlled via UpdateRoleFields so re-seeding system roles on boot
	// never stomps an operator's disable toggle.
	_, err := r.db.Collection(CollRoles).UpdateOne(ctx,
		bson.M{"tenantId": role.TenantID, "name": role.Name},
		bson.M{
			"$set": bson.M{
				"uuid":        role.UUID,
				"description": role.Description,
				"permissions": role.Permissions,
				"isSystem":    role.IsSystem,
				"updatedAt":   role.UpdatedAt,
			},
			"$setOnInsert": bson.M{
				"createdAt": role.CreatedAt,
				"isActive":  role.IsActive,
			},
		},
		options.Update().SetUpsert(true))
	return err
}

// InsertRole inserts a brand-new role. Unlike UpsertRole it is not keyed
// on anything: a name already taken in this tenant collides on the
// collection's unique (tenantId, name) index and comes back as
// ErrRoleExists, which the service maps to a 409. That is what makes
// "a role this call created has no bindings" true rather than hopeful —
// with the upsert, a create on a taken name silently rewrote the
// existing role's uuid and permissions, stranding its bindings and
// revoking its holders' access with no invalidation.
//
// The index does the work, so there is no read-then-write window: two
// callers racing the same name produce one row and one 409, not two rows
// or one overwrite.
func (r *Repository) InsertRole(ctx context.Context, role *models.Role) error {
	role.UpdatedAt = time.Now()
	if role.CreatedAt.IsZero() {
		role.CreatedAt = role.UpdatedAt
	}
	if _, err := r.db.Collection(CollRoles).InsertOne(ctx, role); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrRoleExists
		}
		return err
	}
	return nil
}

// UpdateRoleFields applies a partial update to an existing role. Used by the
// service layer's UpdateRole, which is responsible for enforcing the
// system-role immutability policy before calling this.
func (r *Repository) UpdateRoleFields(ctx context.Context, uuid string, fields bson.M) error {
	if len(fields) == 0 {
		return nil
	}
	fields["updatedAt"] = time.Now()
	res, err := r.db.Collection(CollRoles).UpdateOne(ctx,
		bson.M{"uuid": uuid},
		bson.M{"$set": fields})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) GetRoleByName(ctx context.Context, tenantID, name string) (*models.Role, error) {
	var role models.Role
	err := r.db.Collection(CollRoles).FindOne(ctx, bson.M{"tenantId": tenantID, "name": name}).Decode(&role)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &role, err
}

func (r *Repository) GetRoleByUUID(ctx context.Context, uuid string) (*models.Role, error) {
	var role models.Role
	err := r.db.Collection(CollRoles).FindOne(ctx, bson.M{"uuid": uuid}).Decode(&role)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &role, err
}

// CountSystemRoles returns how many system roles (tenantId="", isSystem=true)
// exist. Used by the service to detect a wiped catalog and lazy-reseed it.
func (r *Repository) CountSystemRoles(ctx context.Context) (int64, error) {
	return r.db.Collection(CollRoles).CountDocuments(ctx,
		bson.M{"tenantId": "", "isSystem": true})
}

// ListRoles returns system roles (tenantId=="") plus roles for the given tenant.
func (r *Repository) ListRoles(ctx context.Context, tenantID string) ([]models.Role, error) {
	filter := bson.M{"$or": []bson.M{{"tenantId": ""}, {"tenantId": tenantID}}}
	cur, err := r.db.Collection(CollRoles).Find(ctx, filter, options.Find().SetSort(bson.M{"name": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Role
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) DeleteRole(ctx context.Context, uuid string) error {
	res, err := r.db.Collection(CollRoles).DeleteOne(ctx, bson.M{"uuid": uuid, "isSystem": false})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Bindings ---

// bindingIsExpired reports whether b carries an absolute expiry that has
// already passed. A nil ExpiresAt is a permanent grant, never expired.
func bindingIsExpired(b *models.Binding, now time.Time) bool {
	return b != nil && b.ExpiresAt != nil && !b.ExpiresAt.After(now)
}

// reapExpiredBinding deletes the (tenantId, userUUID, roleId) row when — and
// only when — it carries an expiry that has already passed, reporting
// whether it removed anything.
//
// An expired binding confers nothing: ListActiveBindingsForUser filters it
// out of every effective-permission computation. But authz_bindings has no
// TTL index and no reaper (see this module's CLAUDE.md — both are tracked as
// future work), so the row survives forever, and the unique
// (tenantId, userUUID, roleId) index then makes it block every subsequent
// grant of that role. Reaping it at grant time is what keeps "expired" from
// meaning "permanently un-re-grantable". Deleting a row that already grants
// nothing loses no authority, which is why this is safe to do implicitly.
//
// The `$type: "date"` clause is load-bearing, not defensive noise: BSON's
// canonical type ordering sorts null BELOW dates, so a bare
// {"expiresAt": {"$lte": now}} would also match permanent grants — whose
// expiresAt is null or missing — and silently reap the very bindings that
// must never be touched.
func (r *Repository) reapExpiredBinding(ctx context.Context, tenantID, userUUID, roleUUID string, now time.Time) (bool, error) {
	//tenantscope:allow authz owns the global authz_bindings registry; the filter pins tenantId, userUUID and roleId explicitly (reap of the tuple's own expired row before a re-grant)
	res, err := r.db.Collection(CollBindings).DeleteOne(ctx, bson.M{
		"tenantId":  tenantID,
		"userUUID":  userUUID,
		"roleId":    roleUUID,
		"expiresAt": bson.M{"$type": "date", "$lte": now},
	})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

// CreateBinding inserts a new binding, surfacing the unique index's
// duplicate-key error when the tuple is already granted (the service maps it
// to ErrBindingExists → 409).
//
// An EXPIRED incumbent is not such a duplicate: it grants nothing, so it is
// reaped and the insert retried once. Without that, an expired contractor
// grant would answer 409 "already granted" forever on a role the user does
// not actually hold, and the only way out would be deleting a binding the
// operator cannot see in any effective-permission view.
func (r *Repository) CreateBinding(ctx context.Context, b *models.Binding) error {
	b.GrantedAt = time.Now()
	_, err := r.db.Collection(CollBindings).InsertOne(ctx, b)
	if err == nil || !mongo.IsDuplicateKeyError(err) {
		return err
	}

	reaped, rerr := r.reapExpiredBinding(ctx, b.TenantID, b.UserUUID, b.RoleUUID, time.Now())
	if rerr != nil {
		return rerr
	}
	if !reaped {
		// A live incumbent holds the tuple: the duplicate-key error stands.
		return err
	}
	// Exactly one retry. A second collision means a concurrent writer took
	// the tuple in the meantime, which is a genuine duplicate.
	_, err = r.db.Collection(CollBindings).InsertOne(ctx, b)
	return err
}

// EnsureBinding grants the (tenantId, userUUID, roleId) tuple if absent and
// returns the persisted row either way. Concurrent-safe: $setOnInsert upsert
// against the unique compound index; the loser of a race reads the winner.
// A fresh insert persists every field of b, including ExpiresAt — nil is a
// legitimate value and is stored as BSON null rather than omitted, so an
// inserting caller's expiry is honored exactly like CreateBinding's.
// Existing LIVE rows are returned untouched — uuid/grantedBy/grantedAt/
// expiresAt of the winner are preserved regardless of what a later, losing
// caller's b carries.
//
// An existing EXPIRED row is not a grant and is never reused: it is reaped
// and the ensure replayed once, so the caller gets a live binding. Returning
// it verbatim (the previous behavior) reported success while granting
// nothing at all — and because every OwnerRoleBinder call site
// (tenant.CreateTenant, SetMemberRoles, AttachMember) discards the returned
// row and only checks the error, an expired org_owner binding made
// re-establishing ownership a silent no-op.
func (r *Repository) EnsureBinding(ctx context.Context, b *models.Binding) (*models.Binding, error) {
	out, err := r.ensureBindingOnce(ctx, b)
	if err != nil {
		return nil, err
	}
	if !bindingIsExpired(out, time.Now()) {
		return out, nil
	}
	if _, rerr := r.reapExpiredBinding(ctx, b.TenantID, b.UserUUID, b.RoleUUID, time.Now()); rerr != nil {
		return nil, rerr
	}
	// Exactly one replay, never a loop: a caller whose own ExpiresAt is
	// already in the past legitimately produces an expired row, and that
	// result must be returned rather than retried forever.
	return r.ensureBindingOnce(ctx, b)
}

// ensureBindingOnce is EnsureBinding's single upsert attempt, without the
// expired-incumbent handling its caller layers on top.
func (r *Repository) ensureBindingOnce(ctx context.Context, b *models.Binding) (*models.Binding, error) {
	//tenantscope:allow authz owns the global authz_bindings registry; the ensure filter pins tenantId, userUUID and roleId explicitly (owner-binding ensure — see authz/CLAUDE.md)
	res := r.db.Collection(CollBindings).FindOneAndUpdate(ctx,
		bson.M{"tenantId": b.TenantID, "userUUID": b.UserUUID, "roleId": b.RoleUUID},
		bson.M{"$setOnInsert": bson.M{
			"uuid": b.UUID, "userUUID": b.UserUUID, "tenantId": b.TenantID,
			"roleId": b.RoleUUID, "roleName": b.RoleName,
			"grantedBy": b.GrantedBy, "grantedAt": time.Now(), "expiresAt": b.ExpiresAt,
		}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	)
	var out models.Binding
	if err := res.Decode(&out); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// Upsert raced another insert between its find and insert phases;
			// the tuple now exists — reread it.
			//tenantscope:allow authz owns the global authz_bindings registry; reread of the winning tuple after a duplicate-key race
			ferr := r.db.Collection(CollBindings).FindOne(ctx,
				bson.M{"tenantId": b.TenantID, "userUUID": b.UserUUID, "roleId": b.RoleUUID}).Decode(&out)
			if ferr != nil {
				return nil, ferr
			}
			return &out, nil
		}
		return nil, err
	}
	return &out, nil
}

// DeleteBinding removes a binding by UUID scoped to tenantID. The tenantId
// filter is load-bearing: it stops a member of one tenant from revoking a
// binding in another by UUID. Returns ErrNotFound when no binding matches both
// the UUID and the tenant scope (so the handler can 404 rather than silently
// succeeding on a cross-tenant miss).
func (r *Repository) DeleteBinding(ctx context.Context, tenantID, uuid string) error {
	res, err := r.db.Collection(CollBindings).DeleteOne(ctx, bson.M{"uuid": uuid, "tenantId": tenantID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBindingsByRoleUUID removes every binding pointing at the given role.
// Called by the service layer right before DeleteRole so deleting a role
// never leaves orphaned bindings behind. Returns the number of bindings
// removed so the caller can log/report it.
func (r *Repository) DeleteBindingsByRoleUUID(ctx context.Context, roleUUID string) (int64, error) {
	res, err := r.db.Collection(CollBindings).DeleteMany(ctx, bson.M{"roleId": roleUUID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// DeleteBindingsByTenant removes every tenant-scoped binding for the given
// tenant. Called by the cascade hook the authz module registers on the
// tenant service so a deleted tenant leaves no dangling org_owner /
// org_admin / custom-role bindings. Global bindings (tenantId=="") are
// untouched — those carry platform system roles that outlive any single
// tenant.
func (r *Repository) DeleteBindingsByTenant(ctx context.Context, tenantUUID string) (int64, error) {
	if tenantUUID == "" {
		return 0, nil
	}
	res, err := r.db.Collection(CollBindings).DeleteMany(ctx, bson.M{"tenantId": tenantUUID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// DeleteBindingsByUserAndTenant removes every binding for a single
// (user, tenant) pair. Called when a member is removed from a tenant or has
// their tenant role changed, so the binding never outlives the membership /
// role that justified it (the dangling-binding bug: attach→remove→re-attach
// or a role change otherwise accumulates bindings the evaluator unions).
// Global bindings (tenantId=="") are untouched.
func (r *Repository) DeleteBindingsByUserAndTenant(ctx context.Context, userUUID, tenantUUID string) (int64, error) {
	if userUUID == "" || tenantUUID == "" {
		return 0, nil
	}
	//tenantscope:allow authz owns the global authz_bindings registry; the filter pins both userUUID and tenantId explicitly (mirrors DeleteBindingsByTenant). Membership-unbind hook — see backend/internal/core/authz/CLAUDE.md#org-scoping-invariants-system-wide.
	res, err := r.db.Collection(CollBindings).DeleteMany(ctx, bson.M{"userUUID": userUUID, "tenantId": tenantUUID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// ListBindingsByUser returns every binding for a user across all tenants
// (and global bindings, tenantId==""). Used by the compliance DSR export
// pipeline (right of access) — a data subject's role grants are their data.
func (r *Repository) ListBindingsByUser(ctx context.Context, userUUID string) ([]models.Binding, error) {
	if userUUID == "" {
		return nil, nil
	}
	//tenantscope:allow DSR export is cross-tenant by data-subject; the filter pins userUUID explicitly.
	cur, err := r.db.Collection(CollBindings).Find(ctx, bson.M{"userUUID": userUUID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Binding
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteBindingsByUser removes every binding for a user across all tenants
// (and global bindings). Used by the compliance DSR erase pipeline.
func (r *Repository) DeleteBindingsByUser(ctx context.Context, userUUID string) (int64, error) {
	if userUUID == "" {
		return 0, nil
	}
	//tenantscope:allow DSR erase is cross-tenant by data-subject; the filter pins userUUID explicitly (mirrors DeleteBindingsByUserAndTenant).
	res, err := r.db.Collection(CollBindings).DeleteMany(ctx, bson.M{"userUUID": userUUID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// ListActiveBindingsForUser returns all bindings for (userUUID, tenantID)
// that have not expired. Pass tenantID="" to fetch global/system bindings.
func (r *Repository) ListActiveBindingsForUser(ctx context.Context, userUUID, tenantID string) ([]models.Binding, error) {
	now := time.Now()
	filter := bson.M{
		"userUUID": userUUID,
		"tenantId": tenantID,
		"$or": []bson.M{
			{"expiresAt": nil},
			{"expiresAt": bson.M{"$gt": now}},
		},
	}
	cur, err := r.db.Collection(CollBindings).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Binding
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) ListBindingsByTenant(ctx context.Context, tenantID string) ([]models.Binding, error) {
	cur, err := r.db.Collection(CollBindings).Find(ctx, bson.M{"tenantId": tenantID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Binding
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
