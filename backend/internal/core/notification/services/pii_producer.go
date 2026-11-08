package services

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// piiProducer implements iface.PIIProducer for the notification module: the
// subject's delivered-message history (notification_messages) and per-category
// delivery preferences (notification_preferences). Suppressions are keyed by
// email address, not userUUID, so they ride the auth/email erasure path rather
// than this producer. notification_marketing_optouts is a second address-keyed
// exclusion, and it is retained past erasure by design, the same reasoning a
// suppression record carries: erasing it would let marketing resume to an
// address that asked it to stop (see the module's AGENTS.md GDPR/DSR entry).
// Queries hit the collections directly (the module owns them) and are
// deliberately cross-tenant by data-subject.
type piiProducer struct {
	db      *mongo.Database
	devices repository.DeviceTokenRepository
}

// NewPIIProducer returns a PIIProducer bound to the module database.
func NewPIIProducer(db *mongo.Database) iface.PIIProducer {
	return &piiProducer{db: db, devices: repository.NewDeviceTokenRepository(db)}
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
	devices, err := p.exportDevices(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 && len(prefs) == 0 && len(devices) == 0 {
		return nil, nil
	}
	out := map[string]any{}
	if len(msgs) > 0 {
		out["messages"] = msgs
	}
	if len(prefs) > 0 {
		out["preferences"] = prefs
	}
	if len(devices) > 0 {
		out["pushDevices"] = devices
	}
	return out, nil
}

// pushDeviceExport is the data-subject view of one push registration. The
// token is a credential: only its first 8 characters leave, enough to
// recognise the device without handing the secret back.
type pushDeviceExport struct {
	Tier           string     `json:"tier"`
	DeviceID       string     `json:"deviceId"`
	Platform       string     `json:"platform"`
	AppVersion     string     `json:"appVersion,omitempty"`
	Active         bool       `json:"active"`
	CreatedAt      time.Time  `json:"createdAt"`
	LastSeenAt     time.Time  `json:"lastSeenAt"`
	DisabledAt     *time.Time `json:"disabledAt,omitempty"`
	DisabledReason string     `json:"disabledReason,omitempty"`
	Token          string     `json:"token"`
}

func maskPushToken(t string) string {
	r := []rune(t)
	if len(r) > 8 {
		r = r[:8]
	}
	return string(r) + "…"
}

func (p *piiProducer) exportDevices(ctx context.Context, userUUID string) ([]pushDeviceExport, error) {
	var out []pushDeviceExport
	for _, tier := range []string{iface.PushTierOperator, iface.PushTierClient} {
		rows, err := p.devices.ListForUser(ctx, tier, userUUID)
		if err != nil {
			return nil, err
		}
		for _, d := range rows {
			out = append(out, pushDeviceExport{
				Tier: d.Tier, DeviceID: d.DeviceID, Platform: d.Platform, AppVersion: d.AppVersion,
				Active: d.Active, CreatedAt: d.CreatedAt, LastSeenAt: d.LastSeenAt,
				DisabledAt: d.DisabledAt, DisabledReason: d.DisabledReason, Token: maskPushToken(d.Token),
			})
		}
	}
	return out, nil
}

// PurgePersonalData removes the subject's messages + preferences. Mode-
// agnostic: a per-user message log carries no anonymized residue worth
// keeping, so both erase modes delete the rows.
func (p *piiProducer) PurgePersonalData(ctx context.Context, userUUID string, _ iface.EraseMode) (iface.PurgeResult, error) {
	var deleted int64
	cols := make([]string, 0, 3)

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

	// Push registrations of both tiers go in both erase modes: a device
	// token carries no anonymizable residue.
	var devDeleted int64
	for _, tier := range []string{iface.PushTierOperator, iface.PushTierClient} {
		n, err := p.devices.DeleteForUser(ctx, tier, userUUID)
		if err != nil {
			return iface.PurgeResult{}, err
		}
		devDeleted += n
	}
	if devDeleted > 0 {
		deleted += devDeleted
		cols = append(cols, models.DeviceTokensCollection)
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
