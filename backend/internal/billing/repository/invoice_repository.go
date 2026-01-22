package repository

import (
	"context"
	"errors"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/billing/models"
)

// Common errors
var (
	ErrInvoiceNotFound     = errors.New("invoice not found")
	ErrInvoiceAlreadyExists = errors.New("invoice with this number already exists")
)

// InvoiceRepository defines the interface for invoice data access
type InvoiceRepository interface {
	// Create operations
	Create(ctx context.Context, invoice *models.Invoice) error

	// Read operations
	GetByID(ctx context.Context, id string) (*models.Invoice, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Invoice, error)
	GetByOpenAPIUUID(ctx context.Context, openAPIUUID string) (*models.Invoice, error)
	GetByNumber(ctx context.Context, number string, direction models.InvoiceDirection) (*models.Invoice, error)
	List(ctx context.Context, filters *models.InvoiceFilters, pagination models.PaginationParams) ([]models.Invoice, int64, error)

	// Update operations
	Update(ctx context.Context, invoice *models.Invoice) error
	UpdateStatus(ctx context.Context, uuid string, status models.InvoiceStatus, sdiStatus models.SDIStatus) error
	UpdateOpenAPIData(ctx context.Context, uuid string, openAPIUUID string, sdiIdentifier string) error

	// Delete operations
	SoftDelete(ctx context.Context, uuid string) error

	// Statistics
	GetStats(ctx context.Context, fromDate, toDate time.Time) (*models.BillingStats, error)
	CountByStatus(ctx context.Context, direction models.InvoiceDirection, status models.InvoiceStatus) (int64, error)
}

type invoiceRepository struct {
	collection *mongo.Collection
}

// NewInvoiceRepository creates a new InvoiceRepository
func NewInvoiceRepository(db *mongo.Database) InvoiceRepository {
	repo := &invoiceRepository{
		collection: db.Collection("invoices"),
	}
	// Ensure indexes are created
	repo.createIndexes(context.Background())
	return repo
}

func (r *invoiceRepository) createIndexes(ctx context.Context) {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "openApiUuid", Value: 1}},
			Options: options.Index().SetSparse(true),
		},
		{
			Keys: bson.D{
				{Key: "number", Value: 1},
				{Key: "direction", Value: 1},
			},
		},
		{
			Keys: bson.D{{Key: "direction", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "status", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "sdiStatus", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "customerId", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "supplierId", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "date", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "createdAt", Value: -1}},
		},
		{
			Keys:    bson.D{{Key: "deletedAt", Value: 1}},
			Options: options.Index().SetSparse(true),
		},
	}

	_, _ = r.collection.Indexes().CreateMany(ctx, indexes)
}

func (r *invoiceRepository) Create(ctx context.Context, invoice *models.Invoice) error {
	invoice.CreatedAt = time.Now()
	invoice.UpdatedAt = time.Now()

	result, err := r.collection.InsertOne(ctx, invoice)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrInvoiceAlreadyExists
		}
		return err
	}

	invoice.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *invoiceRepository) GetByID(ctx context.Context, id string) (*models.Invoice, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, ErrInvoiceNotFound
	}

	var invoice models.Invoice
	err = r.collection.FindOne(ctx, bson.M{
		"_id":       objectID,
		"deletedAt": nil,
	}).Decode(&invoice)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}

	return &invoice, nil
}

func (r *invoiceRepository) GetByUUID(ctx context.Context, uuid string) (*models.Invoice, error) {
	var invoice models.Invoice
	err := r.collection.FindOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}).Decode(&invoice)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}

	return &invoice, nil
}

func (r *invoiceRepository) GetByOpenAPIUUID(ctx context.Context, openAPIUUID string) (*models.Invoice, error) {
	var invoice models.Invoice
	err := r.collection.FindOne(ctx, bson.M{
		"openApiUuid": openAPIUUID,
		"deletedAt":   nil,
	}).Decode(&invoice)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}

	return &invoice, nil
}

func (r *invoiceRepository) GetByNumber(ctx context.Context, number string, direction models.InvoiceDirection) (*models.Invoice, error) {
	var invoice models.Invoice
	err := r.collection.FindOne(ctx, bson.M{
		"number":    number,
		"direction": direction,
		"deletedAt": nil,
	}).Decode(&invoice)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrInvoiceNotFound
		}
		return nil, err
	}

	return &invoice, nil
}

func (r *invoiceRepository) List(ctx context.Context, filters *models.InvoiceFilters, pagination models.PaginationParams) ([]models.Invoice, int64, error) {
	filter := bson.M{"deletedAt": nil}

	if filters != nil {
		if filters.Direction != nil {
			filter["direction"] = *filters.Direction
		}
		if filters.Status != nil {
			filter["status"] = *filters.Status
		}
		if filters.SDIStatus != nil {
			filter["sdiStatus"] = *filters.SDIStatus
		}
		if filters.CustomerID != "" {
			filter["customerId"] = filters.CustomerID
		}
		if filters.SupplierID != "" {
			filter["supplierId"] = filters.SupplierID
		}
		if filters.DocumentType != nil {
			filter["documentType"] = *filters.DocumentType
		}
		if filters.FromDate != nil || filters.ToDate != nil {
			dateFilter := bson.M{}
			if filters.FromDate != nil {
				dateFilter["$gte"] = *filters.FromDate
			}
			if filters.ToDate != nil {
				dateFilter["$lte"] = *filters.ToDate
			}
			filter["date"] = dateFilter
		}
		if filters.Search != "" {
			// Escape special regex characters to prevent ReDoS attacks
			escapedSearch := regexp.QuoteMeta(filters.Search)
			filter["$or"] = []bson.M{
				{"number": bson.M{"$regex": escapedSearch, "$options": "i"}},
				{"cedentePrestatore.denomination": bson.M{"$regex": escapedSearch, "$options": "i"}},
				{"cessionarioCommittente.denomination": bson.M{"$regex": escapedSearch, "$options": "i"}},
			}
		}
	}

	// Count total
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// Set pagination defaults
	if pagination.Page < 1 {
		pagination.Page = 1
	}
	if pagination.PageSize < 1 {
		pagination.PageSize = 20
	}
	if pagination.PageSize > 100 {
		pagination.PageSize = 100
	}

	skip := int64((pagination.Page - 1) * pagination.PageSize)
	limit := int64(pagination.PageSize)

	opts := options.Find().
		SetSort(bson.D{{Key: "date", Value: -1}, {Key: "createdAt", Value: -1}}).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var invoices []models.Invoice
	if err := cursor.All(ctx, &invoices); err != nil {
		return nil, 0, err
	}

	return invoices, total, nil
}

func (r *invoiceRepository) Update(ctx context.Context, invoice *models.Invoice) error {
	invoice.UpdatedAt = time.Now()

	result, err := r.collection.ReplaceOne(ctx, bson.M{
		"uuid":      invoice.UUID,
		"deletedAt": nil,
	}, invoice)

	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrInvoiceNotFound
	}

	return nil
}

func (r *invoiceRepository) UpdateStatus(ctx context.Context, uuid string, status models.InvoiceStatus, sdiStatus models.SDIStatus) error {
	update := bson.M{
		"$set": bson.M{
			"status":    status,
			"sdiStatus": sdiStatus,
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}, update)

	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrInvoiceNotFound
	}

	return nil
}

func (r *invoiceRepository) UpdateOpenAPIData(ctx context.Context, uuid string, openAPIUUID string, sdiIdentifier string) error {
	update := bson.M{
		"$set": bson.M{
			"openApiUuid":   openAPIUUID,
			"sdiIdentifier": sdiIdentifier,
			"updatedAt":     time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}, update)

	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrInvoiceNotFound
	}

	return nil
}

func (r *invoiceRepository) SoftDelete(ctx context.Context, uuid string) error {
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"deletedAt": now,
			"updatedAt": now,
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}, update)

	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrInvoiceNotFound
	}

	return nil
}

func (r *invoiceRepository) GetStats(ctx context.Context, fromDate, toDate time.Time) (*models.BillingStats, error) {
	stats := &models.BillingStats{
		PeriodStart: fromDate,
		PeriodEnd:   toDate,
	}

	dateFilter := bson.M{
		"date":      bson.M{"$gte": fromDate, "$lte": toDate},
		"deletedAt": nil,
	}

	// Issued invoices stats
	issuedFilter := bson.M{"direction": models.DirectionIssued}
	for k, v := range dateFilter {
		issuedFilter[k] = v
	}

	stats.IssuedTotal, _ = r.collection.CountDocuments(ctx, issuedFilter)

	draftFilter := bson.M{"status": models.StatusDraft}
	for k, v := range issuedFilter {
		draftFilter[k] = v
	}
	stats.IssuedDraft, _ = r.collection.CountDocuments(ctx, draftFilter)

	sentFilter := bson.M{"status": models.StatusSent}
	for k, v := range issuedFilter {
		sentFilter[k] = v
	}
	stats.IssuedSent, _ = r.collection.CountDocuments(ctx, sentFilter)

	deliveredFilter := bson.M{"status": models.StatusDelivered}
	for k, v := range issuedFilter {
		deliveredFilter[k] = v
	}
	stats.IssuedDelivered, _ = r.collection.CountDocuments(ctx, deliveredFilter)

	rejectedFilter := bson.M{"status": models.StatusRejected}
	for k, v := range issuedFilter {
		rejectedFilter[k] = v
	}
	stats.IssuedRejected, _ = r.collection.CountDocuments(ctx, rejectedFilter)

	// Sum issued amounts
	issuedAmountPipeline := mongo.Pipeline{
		{{Key: "$match", Value: issuedFilter}},
		{{Key: "$group", Value: bson.M{
			"_id":   nil,
			"total": bson.M{"$sum": "$totalAmount"},
		}}},
	}
	cursor, err := r.collection.Aggregate(ctx, issuedAmountPipeline)
	if err == nil {
		var results []bson.M
		if cursor.All(ctx, &results) == nil && len(results) > 0 {
			if total, ok := results[0]["total"].(float64); ok {
				stats.IssuedAmount = total
			}
		}
	}

	// Received invoices stats
	receivedFilter := bson.M{"direction": models.DirectionReceived}
	for k, v := range dateFilter {
		receivedFilter[k] = v
	}

	stats.ReceivedTotal, _ = r.collection.CountDocuments(ctx, receivedFilter)

	pendingRecFilter := bson.M{"status": bson.M{"$in": []models.InvoiceStatus{models.StatusPending, models.StatusDraft}}}
	for k, v := range receivedFilter {
		pendingRecFilter[k] = v
	}
	stats.ReceivedPending, _ = r.collection.CountDocuments(ctx, pendingRecFilter)

	acceptedRecFilter := bson.M{"status": models.StatusAccepted}
	for k, v := range receivedFilter {
		acceptedRecFilter[k] = v
	}
	stats.ReceivedAccepted, _ = r.collection.CountDocuments(ctx, acceptedRecFilter)

	rejectedRecFilter := bson.M{"status": models.StatusRejected}
	for k, v := range receivedFilter {
		rejectedRecFilter[k] = v
	}
	stats.ReceivedRejected, _ = r.collection.CountDocuments(ctx, rejectedRecFilter)

	// Sum received amounts
	receivedAmountPipeline := mongo.Pipeline{
		{{Key: "$match", Value: receivedFilter}},
		{{Key: "$group", Value: bson.M{
			"_id":   nil,
			"total": bson.M{"$sum": "$totalAmount"},
		}}},
	}
	cursor, err = r.collection.Aggregate(ctx, receivedAmountPipeline)
	if err == nil {
		var results []bson.M
		if cursor.All(ctx, &results) == nil && len(results) > 0 {
			if total, ok := results[0]["total"].(float64); ok {
				stats.ReceivedAmount = total
			}
		}
	}

	return stats, nil
}

func (r *invoiceRepository) CountByStatus(ctx context.Context, direction models.InvoiceDirection, status models.InvoiceStatus) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{
		"direction": direction,
		"status":    status,
		"deletedAt": nil,
	})
}
