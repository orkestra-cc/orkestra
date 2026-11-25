package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// DeviceTokenRepository persists push device tokens.
type DeviceTokenRepository interface {
	// Upsert registers d by tier+userUuid+deviceId, sets it active and clears
	// the disabled fields. Any OTHER active row holding the same token is
	// disabled first (reason "reassigned"): the token belongs to the device.
	Upsert(ctx context.Context, d *models.DeviceToken, now time.Time) (*models.DeviceToken, error)
	Disable(ctx context.Context, tier, userUUID, deviceID, reason string, now time.Time) (bool, error)
	DisableByToken(ctx context.Context, token, reason string, now time.Time) error
	ListForUser(ctx context.Context, tier, userUUID string) ([]models.DeviceToken, error)
	ActiveForUsers(ctx context.Context, tier string, userUUIDs []string) ([]models.DeviceToken, error)
	// ListActiveBatch pages active rows in uuid order, for cleanup.
	ListActiveBatch(ctx context.Context, afterUUID string, limit int) ([]models.DeviceToken, error)
	DisableStale(ctx context.Context, seenBefore time.Time, now time.Time) (int64, error)
	DeleteDisabledBefore(ctx context.Context, before time.Time) (int64, error)
	DeleteForUser(ctx context.Context, tier, userUUID string) (int64, error)
	// CountForUser counts what DeleteForUser would remove (DSR preview).
	CountForUser(ctx context.Context, tier, userUUID string) (int64, error)
}

type deviceTokenRepo struct{ db *mongo.Database }

func NewDeviceTokenRepository(db *mongo.Database) DeviceTokenRepository {
	return &deviceTokenRepo{db: db}
}

func (r *deviceTokenRepo) col() *mongo.Collection {
	return r.db.Collection(models.DeviceTokensCollection)
}

func disableSet(reason string, now time.Time) bson.D {
	return bson.D{{Key: "$set", Value: bson.D{
		{Key: "active", Value: false},
		{Key: "disabledAt", Value: now},
		{Key: "disabledReason", Value: reason},
		{Key: "updatedAt", Value: now},
	}}}
}

func (r *deviceTokenRepo) Upsert(ctx context.Context, d *models.DeviceToken, now time.Time) (*models.DeviceToken, error) {
	if d == nil {
		return nil, errors.New("device token is nil")
	}
	key := bson.D{
		{Key: "tier", Value: d.Tier},
		{Key: "userUuid", Value: d.UserUUID},
		{Key: "deviceId", Value: d.DeviceID},
	}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "sessionId", Value: d.SessionID},
			{Key: "token", Value: d.Token},
			{Key: "platform", Value: d.Platform},
			{Key: "appVersion", Value: d.AppVersion},
			{Key: "active", Value: true},
			{Key: "updatedAt", Value: now},
			{Key: "lastSeenAt", Value: now},
		}},
		{Key: "$unset", Value: bson.D{
			{Key: "disabledAt", Value: ""},
			{Key: "disabledReason", Value: ""},
		}},
		{Key: "$setOnInsert", Value: bson.D{
			{Key: "uuid", Value: uuid.NewString()},
			{Key: "createdAt", Value: now},
		}},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		// The token belongs to the device: free it from any other owner.
		//tenantscope:allow system: device tokens are keyed by user, device and push token across tiers; the tier is an explicit filter field, not a tenant org scope
		_, err := r.col().UpdateMany(ctx, bson.D{
			{Key: "token", Value: d.Token},
			{Key: "active", Value: true},
			{Key: "$nor", Value: bson.A{key}},
		}, disableSet(models.DeviceDisabledReassigned, now))
		if err != nil {
			return nil, err
		}
		var out models.DeviceToken
		//tenantscope:allow system: device tokens are keyed by user, device and push token across tiers; the tier is an explicit filter field, not a tenant org scope
		err = r.col().FindOneAndUpdate(ctx, key, update, opts).Decode(&out)
		if err == nil {
			return &out, nil
		}
		// A concurrent registration of the same token won the partial
		// unique index between the two statements: retry once.
		if !mongo.IsDuplicateKeyError(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func (r *deviceTokenRepo) Disable(ctx context.Context, tier, userUUID, deviceID, reason string, now time.Time) (bool, error) {
	//tenantscope:allow system: device tokens are keyed by user, device and push token across tiers; the tier is an explicit filter field, not a tenant org scope
	res, err := r.col().UpdateOne(ctx, bson.D{
		{Key: "tier", Value: tier},
		{Key: "userUuid", Value: userUUID},
		{Key: "deviceId", Value: deviceID},
		{Key: "active", Value: true},
	}, disableSet(reason, now))
	if err != nil {
		return false, err
	}
	return res.ModifiedCount > 0, nil
}

func (r *deviceTokenRepo) DisableByToken(ctx context.Context, token, reason string, now time.Time) error {
	//tenantscope:allow system: device tokens are keyed by user, device and push token across tiers; the tier is an explicit filter field, not a tenant org scope
	_, err := r.col().UpdateMany(ctx, bson.D{
		{Key: "token", Value: token},
		{Key: "active", Value: true},
	}, disableSet(reason, now))
	return err
}

func (r *deviceTokenRepo) find(ctx context.Context, filter bson.D, opts ...*options.FindOptions) ([]models.DeviceToken, error) {
	//tenantscope:allow system: device tokens are keyed by user, device and push token across tiers; the tier is an explicit filter field, not a tenant org scope
	cur, err := r.col().Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := []models.DeviceToken{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// userTierFilter is the per-subject selector shared by list, delete and count.
func userTierFilter(tier, userUUID string) bson.D {
	return bson.D{{Key: "tier", Value: tier}, {Key: "userUuid", Value: userUUID}}
}

func (r *deviceTokenRepo) ListForUser(ctx context.Context, tier, userUUID string) ([]models.DeviceToken, error) {
	return r.find(ctx, userTierFilter(tier, userUUID),
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
}

func (r *deviceTokenRepo) ActiveForUsers(ctx context.Context, tier string, userUUIDs []string) ([]models.DeviceToken, error) {
	if len(userUUIDs) == 0 {
		return []models.DeviceToken{}, nil
	}
	return r.find(ctx, bson.D{
		{Key: "tier", Value: tier},
		{Key: "userUuid", Value: bson.D{{Key: "$in", Value: userUUIDs}}},
		{Key: "active", Value: true},
	})
}

func (r *deviceTokenRepo) ListActiveBatch(ctx context.Context, afterUUID string, limit int) ([]models.DeviceToken, error) {
	f := bson.D{{Key: "active", Value: true}}
	if afterUUID != "" {
		f = append(f, bson.E{Key: "uuid", Value: bson.D{{Key: "$gt", Value: afterUUID}}})
	}
	return r.find(ctx, f, options.Find().SetSort(bson.D{{Key: "uuid", Value: 1}}).SetLimit(int64(limit)))
}

func (r *deviceTokenRepo) DisableStale(ctx context.Context, seenBefore time.Time, now time.Time) (int64, error) {
	//tenantscope:allow system: device tokens are keyed by user, device and push token across tiers; the tier is an explicit filter field, not a tenant org scope
	res, err := r.col().UpdateMany(ctx, bson.D{
		{Key: "active", Value: true},
		{Key: "lastSeenAt", Value: bson.D{{Key: "$lt", Value: seenBefore}}},
	}, disableSet(models.DeviceDisabledStale, now))
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (r *deviceTokenRepo) DeleteDisabledBefore(ctx context.Context, before time.Time) (int64, error) {
	//tenantscope:allow system: device tokens are keyed by user, device and push token across tiers; the tier is an explicit filter field, not a tenant org scope
	res, err := r.col().DeleteMany(ctx, bson.D{
		{Key: "active", Value: false},
		{Key: "disabledAt", Value: bson.D{{Key: "$lt", Value: before}}},
	})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func (r *deviceTokenRepo) DeleteForUser(ctx context.Context, tier, userUUID string) (int64, error) {
	//tenantscope:allow system: device tokens are keyed by user, device and push token across tiers; the tier is an explicit filter field, not a tenant org scope
	res, err := r.col().DeleteMany(ctx, userTierFilter(tier, userUUID))
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func (r *deviceTokenRepo) CountForUser(ctx context.Context, tier, userUUID string) (int64, error) {
	//tenantscope:allow system: device tokens are keyed by user, device and push token across tiers; the tier is an explicit filter field, not a tenant org scope
	return r.col().CountDocuments(ctx, userTierFilter(tier, userUUID))
}
