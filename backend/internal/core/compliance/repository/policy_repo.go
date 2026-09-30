package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrPolicyNotFound          = errors.New("compliance: policy not found")
	ErrPolicyVersionConflict   = errors.New("compliance: policy version changed")
	ErrPolicyNameTaken         = errors.New("compliance: policy name already used")
	ErrPlatformPolicyExists    = errors.New("compliance: platform policy already exists")
	ErrAssignmentNotFound      = errors.New("compliance: tenant has no policy assignment")
	ErrChangeRequestNotFound   = errors.New("compliance: change request not found")
	ErrChangeRequestNotPending = errors.New("compliance: change request is not pending")
)

// maxChangeRequestList caps a change-request listing.
const maxChangeRequestList = 500

// PolicyRepository persists the policy engine (compliance spec §1.1). Every
// collection is platform state managed by Tier-1 operators; versions,
// history and change requests are evidence: the repository only inserts
// them and moves a request out of pending.
type PolicyRepository struct {
	db          *mongo.Database
	policies    *mongo.Collection
	versions    *mongo.Collection
	assignments *mongo.Collection
	history     *mongo.Collection
	requests    *mongo.Collection
}

func NewPolicyRepo(db *mongo.Database) *PolicyRepository {
	return &PolicyRepository{
		db:          db,
		policies:    db.Collection(models.PoliciesCollection),
		versions:    db.Collection(models.PolicyVersionsCollection),
		assignments: db.Collection(models.PolicyAssignmentsCollection),
		history:     db.Collection(models.PolicyAssignmentHistoryCollection),
		requests:    db.Collection(models.PolicyChangeRequestsCollection),
	}
}

// WithTxn runs fn in a multi-document transaction (replica set required, as
// in every Orkestra environment). fn must use the ctx it receives and may be
// called more than once on a transient error.
func (r *PolicyRepository) WithTxn(ctx context.Context, fn func(ctx context.Context) error) error {
	sess, err := r.db.Client().StartSession()
	if err != nil {
		return fmt.Errorf("compliance: start session: %w", err)
	}
	defer sess.EndSession(ctx)
	_, err = sess.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		return nil, fn(sc)
	})
	return err
}

// mapPolicyDup turns a duplicate key on compliance_policies into a sentinel.
func mapPolicyDup(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	switch msg := err.Error(); {
	case strings.Contains(msg, "isPlatformDefault_1"):
		return ErrPlatformPolicyExists
	case strings.Contains(msg, "name_1"):
		return ErrPolicyNameTaken
	}
	return err
}

func (r *PolicyRepository) InsertPolicy(ctx context.Context, p *models.Policy) error {
	_, err := r.policies.InsertOne(ctx, p)
	return mapPolicyDup(err)
}

func (r *PolicyRepository) findPolicy(ctx context.Context, filter bson.M) (*models.Policy, error) {
	var p models.Policy
	//tenantscope:allow compliance policies are platform state managed by Tier-1 operators
	err := r.policies.FindOne(ctx, filter).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrPolicyNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PolicyRepository) GetPolicy(ctx context.Context, uuid string) (*models.Policy, error) {
	return r.findPolicy(ctx, bson.M{"uuid": uuid})
}

func (r *PolicyRepository) GetPlatformPolicy(ctx context.Context) (*models.Policy, error) {
	return r.findPolicy(ctx, bson.M{"isPlatformDefault": true})
}

func (r *PolicyRepository) ListPolicies(ctx context.Context) ([]models.Policy, error) {
	//tenantscope:allow compliance policies are platform state managed by Tier-1 operators
	cur, err := r.policies.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	out := []models.Policy{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PolicyRepository) NameTaken(ctx context.Context, name, exceptUUID string) (bool, error) {
	//tenantscope:allow policy names are unique platform-wide
	n, err := r.policies.CountDocuments(ctx, bson.M{"name": name, "uuid": bson.M{"$ne": exceptUUID}})
	return n > 0, err
}

// ReplacePolicy writes p over the document with p.UUID and expectedVersion.
// The filter also pins isPlatformDefault, so a replace can never flip it: a
// replace that tries is a version conflict and changes nothing.
func (r *PolicyRepository) ReplacePolicy(ctx context.Context, p *models.Policy, expectedVersion int) error {
	filter := bson.M{"uuid": p.UUID, "version": expectedVersion, "isPlatformDefault": p.IsPlatformDefault}
	//tenantscope:allow compliance policies are platform state managed by Tier-1 operators
	res, err := r.policies.ReplaceOne(ctx, filter, p)
	if err != nil {
		return mapPolicyDup(err)
	}
	if res.MatchedCount == 0 {
		return r.missingOrConflict(ctx, p.UUID)
	}
	return nil
}

// DeletePolicy removes a tenant policy at expectedVersion; the platform
// policy is never matched.
func (r *PolicyRepository) DeletePolicy(ctx context.Context, uuid string, expectedVersion int) error {
	//tenantscope:allow compliance policies are platform state managed by Tier-1 operators
	res, err := r.policies.DeleteOne(ctx, bson.M{"uuid": uuid, "version": expectedVersion, "isPlatformDefault": false})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return r.missingOrConflict(ctx, uuid)
	}
	return nil
}

func (r *PolicyRepository) missingOrConflict(ctx context.Context, uuid string) error {
	if _, err := r.GetPolicy(ctx, uuid); err != nil {
		return err
	}
	return ErrPolicyVersionConflict
}

// InsertVersion appends an immutable version; a second copy of the same
// (policyUuid, version) is a lost race: ErrPolicyVersionConflict.
func (r *PolicyRepository) InsertVersion(ctx context.Context, v *models.PolicyVersion) error {
	_, err := r.versions.InsertOne(ctx, v)
	if mongo.IsDuplicateKeyError(err) {
		return ErrPolicyVersionConflict
	}
	return err
}

func (r *PolicyRepository) ListVersions(ctx context.Context, policyUUID string) ([]models.PolicyVersion, error) {
	//tenantscope:allow policy versions are platform evidence keyed by policy
	cur, err := r.versions.Find(ctx, bson.M{"policyUuid": policyUUID}, options.Find().SetSort(bson.D{{Key: "version", Value: -1}}))
	if err != nil {
		return nil, err
	}
	out := []models.PolicyVersion{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PolicyRepository) GetAssignment(ctx context.Context, tenantID string) (*models.PolicyAssignment, error) {
	var a models.PolicyAssignment
	//tenantscope:allow the assignment is keyed by the tenant it describes (Tier-1 platform state)
	err := r.assignments.FindOne(ctx, bson.M{"tenantId": tenantID}).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrAssignmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *PolicyRepository) ListAssignments(ctx context.Context) ([]models.PolicyAssignment, error) {
	//tenantscope:allow the policy resolver and the Tier-1 console read every tenant's assignment
	cur, err := r.assignments.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	out := []models.PolicyAssignment{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PolicyRepository) CountAssignments(ctx context.Context, policyUUID string) (int64, error) {
	//tenantscope:allow counts the tenants of a platform policy across tenants
	return r.assignments.CountDocuments(ctx, bson.M{"policyUuid": policyUUID})
}

func (r *PolicyRepository) UpsertAssignment(ctx context.Context, a *models.PolicyAssignment) error {
	//tenantscope:allow the assignment is keyed by the tenant it describes (Tier-1 platform state)
	_, err := r.assignments.ReplaceOne(ctx, bson.M{"tenantId": a.TenantID}, a, options.Replace().SetUpsert(true))
	return err
}

func (r *PolicyRepository) DeleteAssignment(ctx context.Context, tenantID string) error {
	//tenantscope:allow the assignment is keyed by the tenant it describes (Tier-1 platform state)
	res, err := r.assignments.DeleteOne(ctx, bson.M{"tenantId": tenantID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrAssignmentNotFound
	}
	return nil
}

func (r *PolicyRepository) InsertAssignmentHistory(ctx context.Context, h *models.PolicyAssignmentHistory) error {
	_, err := r.history.InsertOne(ctx, h)
	return err
}

func (r *PolicyRepository) InsertChangeRequest(ctx context.Context, cr *models.PolicyChangeRequest) error {
	_, err := r.requests.InsertOne(ctx, cr)
	return err
}

func (r *PolicyRepository) GetChangeRequest(ctx context.Context, uuid string) (*models.PolicyChangeRequest, error) {
	var cr models.PolicyChangeRequest
	//tenantscope:allow change requests are platform evidence resolved by their own UUID
	err := r.requests.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&cr)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrChangeRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &cr, nil
}

// ListChangeRequests lists the newest requests, all of them or one status.
func (r *PolicyRepository) ListChangeRequests(ctx context.Context, status string) ([]models.PolicyChangeRequest, error) {
	filter := bson.M{}
	if status != "" {
		filter["status"] = status
	}
	opts := options.Find().SetSort(bson.D{{Key: "requestedAt", Value: -1}}).SetLimit(maxChangeRequestList)
	//tenantscope:allow change requests are platform evidence reviewed by Tier-1 operators
	cur, err := r.requests.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	out := []models.PolicyChangeRequest{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DecideChangeRequest moves a pending request to status. The pending filter
// makes it a compare-and-set: of two concurrent decisions one wins, the
// other gets ErrChangeRequestNotPending.
func (r *PolicyRepository) DecideChangeRequest(ctx context.Context, uuid, status, decidedBy, note string, at time.Time) error {
	//tenantscope:allow change requests are platform evidence resolved by their own UUID
	res, err := r.requests.UpdateOne(ctx,
		bson.M{"uuid": uuid, "status": models.ChangeStatusPending},
		bson.M{"$set": bson.M{"status": status, "decidedBy": decidedBy, "decidedAt": at, "decisionNote": note}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		if _, err := r.GetChangeRequest(ctx, uuid); err != nil {
			return err
		}
		return ErrChangeRequestNotPending
	}
	return nil
}

func (r *PolicyRepository) ListPendingBefore(ctx context.Context, before time.Time) ([]models.PolicyChangeRequest, error) {
	//tenantscope:allow the expiry job scans pending platform change requests
	cur, err := r.requests.Find(ctx, bson.M{"status": models.ChangeStatusPending, "requestedAt": bson.M{"$lt": before}})
	if err != nil {
		return nil, err
	}
	out := []models.PolicyChangeRequest{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
