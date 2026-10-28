package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrErasureRequestNotFound is returned when resolving a request that doesn't
// exist or is no longer pending.
var ErrErasureRequestNotFound = errors.New("compliance: erasure request not found or already resolved")

// ErrErasureRequestBusy is returned when another run holds the request's lease.
var ErrErasureRequestBusy = errors.New("compliance: an erasure run holds this request")

// ErrErasureLeaseLost is returned by Settle when the lease was taken over.
var ErrErasureLeaseLost = errors.New("compliance: the erasure run lease was taken over")

// ErrErasureRequestActive is returned by Insert for an incomplete retention
// request when the subject already has one (the partial unique index of
// models.ActiveRetentionErasurePartialFilter): another run owns the subject.
var ErrErasureRequestActive = errors.New("compliance: the subject already has an incomplete retention erasure")

// ErasureSettlement is the outcome of one leased run.
type ErasureSettlement struct {
	Status        string // models.ErasureRequestCompleted or models.ErasureRequestIncomplete
	Progress      []models.ErasureProducerProgress
	Attempts      int
	NextAttemptAt *time.Time // nil: no automatic retry
	LastError     string     // "" or models.ErasureLastError*
	ResolvedAt    *time.Time // set with Status completed
}

// ErasureRequestRepository persists right-to-erasure requests.
type ErasureRequestRepository struct {
	coll *mongo.Collection
}

// NewErasureRequestRepo binds the repository to its collection.
func NewErasureRequestRepo(db *mongo.Database) *ErasureRequestRepository {
	return &ErasureRequestRepository{coll: db.Collection(models.ErasureRequestsCollection)}
}

// Insert appends a new request. An incomplete retention request for a subject
// that already has one is refused with ErrErasureRequestActive.
func (r *ErasureRequestRepository) Insert(ctx context.Context, req *models.ErasureRequest) error {
	_, err := r.coll.InsertOne(ctx, req)
	if mongo.IsDuplicateKeyError(err) && req.Status == models.ErasureRequestIncomplete && req.Source == models.ErasureSourceRetention {
		return ErrErasureRequestActive
	}
	return err
}

// Get returns one request by UUID.
func (r *ErasureRequestRepository) Get(ctx context.Context, uuid string) (*models.ErasureRequest, error) {
	var req models.ErasureRequest
	//tenantscope:allow compliance admin resolves any erasure request by its own UUID — cross-tenant by design.
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&req)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrErasureRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// ListByStatus lists requests in any of the given statuses, newest-first. No
// status lists every request.
func (r *ErasureRequestRepository) ListByStatus(ctx context.Context, statuses ...string) ([]models.ErasureRequest, error) {
	filter := bson.M{}
	switch len(statuses) {
	case 0:
	case 1:
		filter["status"] = statuses[0]
	default:
		filter["status"] = bson.M{"$in": statuses}
	}
	//tenantscope:allow compliance admin reviews erasure requests platform-wide.
	cur, err := r.coll.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "requestedAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.ErasureRequest
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// leaseFree matches a row nobody holds: leaseUntil absent, null or expired.
func leaseFree(now time.Time) []bson.M {
	return []bson.M{{"leaseUntil": nil}, {"leaseUntil": bson.M{"$lt": now}}}
}

// Claim takes the lease on one request for an erasure run. fromStatus is the
// status the caller expects: pending (first execution: the row becomes
// incomplete with mode and resolver recorded) or incomplete (an operator
// retry: mode and resolver stay). Either way the attempt budget restarts and
// this run is attempt 1. It returns the row after the update.
func (r *ErasureRequestRepository) Claim(ctx context.Context, uuid, fromStatus, mode, actor string, now, leaseUntil time.Time) (*models.ErasureRequest, error) {
	// nextAttemptAt = the lease, so the worker takes the row over if this run
	// dies before Settle (pending rows and exhausted rows have none).
	lease := leaseUntil.Truncate(time.Millisecond)
	set := bson.M{"leaseUntil": lease, "nextAttemptAt": lease, "attempts": 1}
	if fromStatus == models.ErasureRequestPending {
		set["status"] = models.ErasureRequestIncomplete
		set["mode"] = mode
		set["resolvedBy"] = actor
	}
	var req models.ErasureRequest
	//tenantscope:allow compliance admin executes a specific erasure request by its own UUID — cross-tenant by design.
	err := r.coll.FindOneAndUpdate(ctx,
		bson.M{"uuid": uuid, "status": fromStatus, "$or": leaseFree(now)},
		bson.M{"$set": set, "$unset": bson.M{"lastError": ""}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&req)
	if err == nil {
		return &req, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	cur, gerr := r.Get(ctx, uuid)
	if gerr != nil {
		return nil, gerr
	}
	if cur.Status == fromStatus {
		return nil, ErrErasureRequestBusy
	}
	return nil, ErrErasureRequestNotFound
}

// ClaimDue leases the next incomplete request whose retry is due, of any
// tenant, and counts the attempt on the row (attempts + 1), so a run that dies
// before Settle still spends it. A row that already used maxAttempts is never
// claimed. nil, nil when nothing is due.
func (r *ErasureRequestRepository) ClaimDue(ctx context.Context, now, leaseUntil time.Time, maxAttempts int) (*models.ErasureRequest, error) {
	var req models.ErasureRequest
	//tenantscope:allow system: erasure retry worker claims incomplete requests of every tenant
	err := r.coll.FindOneAndUpdate(ctx,
		bson.M{
			"status":        models.ErasureRequestIncomplete,
			"nextAttemptAt": bson.M{"$lte": now},
			"attempts":      bson.M{"$not": bson.M{"$gte": maxAttempts}}, // also matches a row without attempts
			"$or":           leaseFree(now),
		},
		bson.M{
			"$set": bson.M{"leaseUntil": leaseUntil.Truncate(time.Millisecond)},
			"$inc": bson.M{"attempts": 1},
		},
		options.FindOneAndUpdate().SetSort(bson.D{{Key: "nextAttemptAt", Value: 1}}).SetReturnDocument(options.After),
	).Decode(&req)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// RecordProgress stores the progress of a leased run before it calls any
// producer (every registered producer listed, the new ones pending) and keeps
// the lease. It is fenced like Settle: ErrErasureLeaseLost when the row is no
// longer incomplete under exactly leaseUntil.
func (r *ErasureRequestRepository) RecordProgress(ctx context.Context, uuid string, leaseUntil time.Time, progress []models.ErasureProducerProgress) error {
	//tenantscope:allow records the producers of a specific erasure request by its own UUID under its lease — cross-tenant by design.
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"uuid": uuid, "status": models.ErasureRequestIncomplete, "leaseUntil": leaseUntil.Truncate(time.Millisecond)},
		bson.M{"$set": bson.M{"progress": progress}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrErasureLeaseLost
	}
	return nil
}

// Settle records the outcome of one leased run and releases the lease. It is
// fenced by the lease: the row must still be incomplete and carry exactly
// leaseUntil, so a run whose lease expired and was taken over, or whose
// request an operator closed meanwhile, cannot overwrite the newer state.
func (r *ErasureRequestRepository) Settle(ctx context.Context, uuid string, leaseUntil time.Time, s ErasureSettlement) error {
	set := bson.M{
		"status":   s.Status,
		"progress": s.Progress,
		"attempts": s.Attempts,
	}
	unset := bson.M{"leaseUntil": ""}
	if s.NextAttemptAt != nil {
		set["nextAttemptAt"] = *s.NextAttemptAt
	} else {
		unset["nextAttemptAt"] = ""
	}
	if s.LastError != "" {
		set["lastError"] = s.LastError
	} else {
		unset["lastError"] = ""
	}
	if s.ResolvedAt != nil {
		set["resolvedAt"] = *s.ResolvedAt
	}
	//tenantscope:allow settles a specific erasure request by its own UUID under its lease — cross-tenant by design.
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"uuid": uuid, "status": models.ErasureRequestIncomplete, "leaseUntil": leaseUntil.Truncate(time.Millisecond)},
		bson.M{"$set": set, "$unset": unset},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrErasureLeaseLost
	}
	return nil
}

// progressFence is the filter value matching exactly the stored progress p:
// the same array of documents, or no progress at all when p is empty (an
// empty progress is never stored: the field is omitempty).
func progressFence(p []models.ErasureProducerProgress) any {
	if len(p) == 0 {
		return bson.M{"$exists": false}
	}
	return p
}

// HasIncomplete reports whether the data subject has an incomplete erasure.
func (r *ErasureRequestRepository) HasIncomplete(ctx context.Context, userUUID string) (bool, error) {
	//tenantscope:allow dsr: incomplete erasure lookup by data subject across tenants
	n, err := r.coll.CountDocuments(ctx,
		bson.M{"userUuid": userUUID, "status": models.ErasureRequestIncomplete},
		options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Resolve transitions a pending request to completed/rejected.
func (r *ErasureRequestRepository) Resolve(ctx context.Context, uuid, status, resolvedBy, mode, note string) error {
	now := time.Now().UTC()
	//tenantscope:allow resolves a specific erasure request by its own UUID — cross-tenant by design.
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"uuid": uuid, "status": models.ErasureRequestPending},
		bson.M{"$set": bson.M{
			"status":         status,
			"resolvedAt":     now,
			"resolvedBy":     resolvedBy,
			"mode":           mode,
			"resolutionNote": note,
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrErasureRequestNotFound
	}
	return nil
}

// ClosePartial closes an incomplete request whose remaining producers are no
// longer registered: status closed_partial, the abandoned producer keys, who
// closed it and why. It is fenced by the progress the caller checked
// (expected, compared exactly): a run that changed it since makes the close
// miss. It needs the lease free; a live lease means a run is in flight. Both
// misses on a still-incomplete row are ErrErasureRequestBusy. An expired lease
// is removed, so the late Settle of a dead run cannot match. Any other row
// misses (ErrErasureRequestNotFound).
func (r *ErasureRequestRepository) ClosePartial(ctx context.Context, uuid string, now time.Time, expected []models.ErasureProducerProgress, abandoned []string, closedBy, note string) error {
	//tenantscope:allow compliance admin closes a specific erasure request by its own UUID — cross-tenant by design.
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"uuid": uuid, "status": models.ErasureRequestIncomplete, "progress": progressFence(expected), "$or": leaseFree(now)},
		bson.M{
			"$set": bson.M{
				"status":         models.ErasureRequestClosedPartial,
				"abandoned":      abandoned,
				"resolvedBy":     closedBy,
				"resolvedAt":     now,
				"resolutionNote": note,
			},
			"$unset": bson.M{"nextAttemptAt": "", "leaseUntil": ""},
		},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount > 0 {
		return nil
	}
	cur, gerr := r.Get(ctx, uuid)
	if gerr != nil {
		return gerr
	}
	if cur.Status == models.ErasureRequestIncomplete {
		return ErrErasureRequestBusy
	}
	return ErrErasureRequestNotFound
}
