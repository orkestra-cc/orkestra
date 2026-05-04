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

func (r *Repository) CreateBinding(ctx context.Context, b *models.Binding) error {
	b.GrantedAt = time.Now()
	_, err := r.db.Collection(CollBindings).InsertOne(ctx, b)
	return err
}

func (r *Repository) DeleteBinding(ctx context.Context, uuid string) error {
	_, err := r.db.Collection(CollBindings).DeleteOne(ctx, bson.M{"uuid": uuid})
	return err
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
