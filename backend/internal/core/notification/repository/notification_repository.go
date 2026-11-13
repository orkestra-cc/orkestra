package repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrNotFound = errors.New("notification: not found")

// NotificationRepository persists delivery log entries and performs
// idempotency lookups.
type NotificationRepository interface {
	Create(ctx context.Context, doc *models.NotificationDoc) error
	FindByIdempotencyKey(ctx context.Context, key string, since time.Time) (*models.NotificationDoc, error)
	List(ctx context.Context, filter Filter, limit int64) ([]*models.NotificationDoc, error)
	// Page returns one page of the log, newest first, and the number of rows
	// the filter matches. skip < 0 is 0; limit is clamped like List's.
	Page(ctx context.Context, filter Filter, skip, limit int64) ([]*models.NotificationDoc, int64, error)
	GetByUUID(ctx context.Context, uuid string) (*models.NotificationDoc, error)
}

// Filter narrows a notification list query.
type Filter struct {
	UserUUID   string
	Category   string
	Status     string
	Channel    string
	SenderSlug string
	Type       string
	// AddressContains keeps rows whose recipient address contains it,
	// case-insensitively and literally (no pattern syntax). Empty keeps
	// every row.
	AddressContains string
}

// query builds the Mongo filter shared by List, Page and the count.
func (f Filter) query() bson.M {
	q := bson.M{}
	if f.UserUUID != "" {
		q["recipientUserUuid"] = f.UserUUID
	}
	if f.Category != "" {
		q["category"] = f.Category
	}
	if f.Status != "" {
		q["status"] = f.Status
	}
	if f.Channel != "" {
		q["channel"] = f.Channel
	}
	if f.SenderSlug != "" {
		q["senderSlug"] = f.SenderSlug
	}
	if f.Type != "" {
		q["type"] = f.Type
	}
	if c := strings.TrimSpace(f.AddressContains); c != "" {
		q["recipientAddress"] = bson.M{"$regex": regexp.QuoteMeta(c), "$options": "i"}
	}
	return q
}

func clampLimit(limit int64) int64 {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}

type notificationRepository struct {
	coll *mongo.Collection
}

func NewNotificationRepository(db *mongo.Database) NotificationRepository {
	return &notificationRepository{
		coll: db.Collection(models.NotificationMessagesCollection),
	}
}

func (r *notificationRepository) Create(ctx context.Context, doc *models.NotificationDoc) error {
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	_, err := r.coll.InsertOne(ctx, doc)
	return err
}

func (r *notificationRepository) FindByIdempotencyKey(ctx context.Context, key string, since time.Time) (*models.NotificationDoc, error) {
	if key == "" {
		return nil, nil
	}
	var doc models.NotificationDoc
	//tenantscope:allow system: the delivery log is platform-global; idempotency keys are minted per send, not per tenant
	err := r.coll.FindOne(ctx, bson.M{
		"idempotencyKey": key,
		"createdAt":      bson.M{"$gte": since},
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *notificationRepository) List(ctx context.Context, filter Filter, limit int64) ([]*models.NotificationDoc, error) {
	return r.find(ctx, filter.query(), 0, clampLimit(limit))
}

// MaxPageSkip bounds how deep Page can go: a skip is a scan of the rows it
// passes, and nobody reads the 10 001st row of a delivery log page by page —
// a filter finds it.
const MaxPageSkip = 10000

func (r *notificationRepository) Page(ctx context.Context, filter Filter, skip, limit int64) ([]*models.NotificationDoc, int64, error) {
	if skip < 0 {
		skip = 0
	}
	if skip > MaxPageSkip {
		skip = MaxPageSkip
	}
	q := filter.query()
	var (
		total int64
		err   error
	)
	if len(q) == 0 {
		// Collection metadata, O(1): an unfiltered count would read the
		// whole 90-day log on every page.
		total, err = r.coll.EstimatedDocumentCount(ctx)
	} else {
		//tenantscope:allow admin-view: operator delivery log spans every tenant (notification.log.read)
		total, err = r.coll.CountDocuments(ctx, q, options.Count().SetMaxTime(countMaxTime))
	}
	if err != nil {
		return nil, 0, err
	}
	items, err := r.find(ctx, q, skip, clampLimit(limit))
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// countMaxTime bounds the filtered count, the one read of a page that has to
// visit every matching row (an address filter is a scan).
const countMaxTime = 10 * time.Second

// find reads newest first. The _id tiebreak keeps the order stable between
// pages when several rows share a createdAt; the indexes {createdAt: -1,
// _id: -1} and {status: 1, createdAt: -1, _id: -1} (module.go) serve this
// sort, so a page reads only its own rows.
func (r *notificationRepository) find(ctx context.Context, q bson.M, skip, limit int64) ([]*models.NotificationDoc, error) {
	//tenantscope:allow admin-view: operator delivery log spans every tenant (notification.log.read)
	cursor, err := r.coll.Find(ctx, q,
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).SetSkip(skip).SetLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	out := []*models.NotificationDoc{}
	for cursor.Next(ctx) {
		var d models.NotificationDoc
		if err := cursor.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, cursor.Err()
}

func (r *notificationRepository) GetByUUID(ctx context.Context, uuid string) (*models.NotificationDoc, error) {
	var doc models.NotificationDoc
	//tenantscope:allow admin-view: operator delivery log lookup by message UUID
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}
