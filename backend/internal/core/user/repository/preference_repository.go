package repository

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// UserPreferencesCollection holds one document per (user, module): the
// user's values for the preference fields that module declares
// (module.HasUserPreferences). Keyed by the user's UUID, not by tenant —
// preferences are personal presentation choices that follow the user
// across tenants — so every query here is user-scoped on purpose.
const UserPreferencesCollection = "user_preferences"

// UserPreferenceDoc is the stored shape. Values is the flat key → string
// map the config-field vocabulary uses; keys not declared by the module
// are refused before they get here (module.ValidateValues).
type UserPreferenceDoc struct {
	UserUUID  string            `bson:"userUuid" json:"userUuid"`
	Module    string            `bson:"module" json:"module"`
	Values    map[string]string `bson:"values" json:"values"`
	CreatedAt time.Time         `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time         `bson:"updatedAt" json:"updatedAt"`
}

// PreferenceRepository persists per-user, per-module preference documents.
type PreferenceRepository interface {
	// Get returns the document for (userUUID, module), or (nil, nil).
	Get(ctx context.Context, userUUID, module string) (*UserPreferenceDoc, error)
	// ListByUser returns every module document the user has, any order.
	ListByUser(ctx context.Context, userUUID string) ([]*UserPreferenceDoc, error)
	// Upsert replaces the values for (userUUID, module) — PUT semantics.
	Upsert(ctx context.Context, userUUID, module string, values map[string]string) (*UserPreferenceDoc, error)
	// DeleteByUser removes every document of the user (DSR erasure).
	DeleteByUser(ctx context.Context, userUUID string) (int64, error)
}

type preferenceRepository struct {
	coll *mongo.Collection
	now  func() time.Time
}

// NewPreferenceRepository binds the repository to the user_preferences
// collection of db.
func NewPreferenceRepository(db *mongo.Database) PreferenceRepository {
	return &preferenceRepository{coll: db.Collection(UserPreferencesCollection), now: time.Now}
}

func (r *preferenceRepository) Get(ctx context.Context, userUUID, module string) (*UserPreferenceDoc, error) {
	var doc UserPreferenceDoc
	//tenantscope:allow system: user_preferences rows are keyed by the user's UUID, not by tenant — personal presentation choices that follow the user across tenants; every query here is bound to the caller's own UUID
	err := r.coll.FindOne(ctx, bson.M{"userUuid": userUUID, "module": module}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *preferenceRepository) ListByUser(ctx context.Context, userUUID string) ([]*UserPreferenceDoc, error) {
	//tenantscope:allow system: user_preferences rows are keyed by the user's UUID, not by tenant — personal presentation choices that follow the user across tenants; every query here is bound to the caller's own UUID
	cursor, err := r.coll.Find(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var out []*UserPreferenceDoc
	for cursor.Next(ctx) {
		var d UserPreferenceDoc
		if err := cursor.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, cursor.Err()
}

func (r *preferenceRepository) Upsert(ctx context.Context, userUUID, module string, values map[string]string) (*UserPreferenceDoc, error) {
	if values == nil {
		values = map[string]string{}
	}
	now := r.now().UTC()
	update := bson.M{
		"$set":         bson.M{"values": values, "updatedAt": now},
		"$setOnInsert": bson.M{"userUuid": userUUID, "module": module, "createdAt": now},
	}
	var doc UserPreferenceDoc
	//tenantscope:allow system: user_preferences rows are keyed by the user's UUID, not by tenant — personal presentation choices that follow the user across tenants; every query here is bound to the caller's own UUID
	err := r.coll.FindOneAndUpdate(ctx, bson.M{"userUuid": userUUID, "module": module}, update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *preferenceRepository) DeleteByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow system: user_preferences rows are keyed by the user's UUID, not by tenant — personal presentation choices that follow the user across tenants; every query here is bound to the caller's own UUID
	res, err := r.coll.DeleteMany(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
