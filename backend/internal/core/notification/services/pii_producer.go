package services

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// piiProducer implements iface.PIIProducer for the notification module: the
// subject's delivered-message history (notification_messages) and per-category
// delivery preferences (notification_preferences). Suppressions are keyed by
// email address, not userUUID, so they ride the auth/email erasure path rather
// than this producer. Queries hit the collections directly (the module owns
// them) and are deliberately cross-tenant by data-subject.
type piiProducer struct {
	db *mongo.Database
}

// NewPIIProducer returns a PIIProducer bound to the module database.
func NewPIIProducer(db *mongo.Database) iface.PIIProducer {
	return &piiProducer{db: db}
}

// Subject is the stable bundle-key identifier for this producer.
func (p *piiProducer) Subject() string { return "notification" }

// ExportPersonalData returns the subject's message history + preferences.
func (p *piiProducer) ExportPersonalData(ctx context.Context, userUUID string) (any, error) {
	//tenantscope:allow DSR export is by data-subject; filter pins recipientUserUuid explicitly.
	msgs, err := findAll(ctx, p.db, models.NotificationMessagesCollection, bson.M{"recipientUserUuid": userUUID})
	if err != nil {
		return nil, err
	}
	//tenantscope:allow DSR export is by data-subject; filter pins userUuid explicitly.
	prefs, err := findAll(ctx, p.db, models.NotificationPreferencesCollection, bson.M{"userUuid": userUUID})
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 && len(prefs) == 0 {
		return nil, nil
	}
	out := map[string]any{}
	if len(msgs) > 0 {
		out["messages"] = msgs
	}
	if len(prefs) > 0 {
		out["preferences"] = prefs
	}
	return out, nil
}

// PurgePersonalData removes the subject's messages + preferences. Mode-
// agnostic: a per-user message log carries no anonymized residue worth
// keeping, so both erase modes delete the rows.
func (p *piiProducer) PurgePersonalData(ctx context.Context, userUUID string, _ iface.EraseMode) (iface.PurgeResult, error) {
	var deleted int64
	cols := make([]string, 0, 2)

	//tenantscope:allow DSR erase is by data-subject; filter pins recipientUserUuid explicitly.
	mres, err := p.db.Collection(models.NotificationMessagesCollection).DeleteMany(ctx, bson.M{"recipientUserUuid": userUUID})
	if err != nil {
		return iface.PurgeResult{}, err
	}
	if mres.DeletedCount > 0 {
		deleted += mres.DeletedCount
		cols = append(cols, models.NotificationMessagesCollection)
	}

	//tenantscope:allow DSR erase is by data-subject; filter pins userUuid explicitly.
	pres, err := p.db.Collection(models.NotificationPreferencesCollection).DeleteMany(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return iface.PurgeResult{}, err
	}
	if pres.DeletedCount > 0 {
		deleted += pres.DeletedCount
		cols = append(cols, models.NotificationPreferencesCollection)
	}

	return iface.PurgeResult{RowsDeleted: int(deleted), Collections: cols}, nil
}

func findAll(ctx context.Context, db *mongo.Database, coll string, filter bson.M) ([]bson.M, error) {
	//tenantscope:allow DSR export/erase is by data-subject; every caller pins the user field in the filter.
	cur, err := db.Collection(coll).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	var out []bson.M
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
