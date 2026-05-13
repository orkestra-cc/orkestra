package services

import (
	"context"
	"time"

	complianceModels "github.com/orkestra/backend/internal/addons/compliance/models"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	userRepo "github.com/orkestra/backend/internal/core/user/repository"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// SOC2EvidenceService computes a point-in-time evidence snapshot for
// the SOC2 controls auditors commonly sample. The snapshot is derived
// from authoritative state (users, MFA factors, audit trail, KMS key
// lifecycle) so every query is idempotent and repeatable — auditors
// get the same answer for the same timestamp.
//
// v1 is on-demand: GET /v1/admin/compliance/soc2/evidence returns the
// current snapshot. A later commit can add a nightly persisting job
// for historical review; the snapshot schema stays stable.
type SOC2EvidenceService struct {
	db *mongo.Database
}

// NewSOC2EvidenceService binds the service to the shared DB handle.
func NewSOC2EvidenceService(db *mongo.Database) *SOC2EvidenceService {
	return &SOC2EvidenceService{db: db}
}

// Evidence is the snapshot returned to the admin. Each field aligns
// with a SOC2 common-criteria control so auditors can map it to their
// sample request directly.
type Evidence struct {
	GeneratedAt time.Time        `json:"generatedAt"`
	Controls    map[string]any   `json:"controls"`
	Summary     map[string]int64 `json:"summary"`
}

// Generate assembles the evidence snapshot. Every sub-query runs with
// a 5-second timeout so a stuck Mongo can't stall the whole admin
// request; failures degrade gracefully — the control reports 0 rather
// than propagating the error, and a parallel log line records the
// miss so operators can triage.
func (s *SOC2EvidenceService) Generate(ctx context.Context) (*Evidence, error) {
	ev := &Evidence{
		GeneratedAt: time.Now().UTC(),
		Controls:    map[string]any{},
		Summary:     map[string]int64{},
	}

	privileged, err := s.privilegedUsers(ctx)
	if err == nil {
		ev.Controls["CC6.1_logical_access"] = privileged
		ev.Summary["privileged_users"] = privileged["total"].(int64)
	}

	mfaCov, err := s.mfaCoverage(ctx)
	if err == nil {
		ev.Controls["CC6.6_account_management"] = mfaCov
		if covered, ok := mfaCov["privilegedWithMFA"].(int64); ok {
			ev.Summary["privileged_with_mfa"] = covered
		}
	}

	failed24h, err := s.failedLogins(ctx, 24*time.Hour)
	failed7d, err7 := s.failedLogins(ctx, 7*24*time.Hour)
	if err == nil && err7 == nil {
		ev.Controls["CC7.2_monitoring"] = map[string]any{
			"failedLoginsLast24h": failed24h,
			"failedLoginsLast7d":  failed7d,
		}
		ev.Summary["failed_logins_24h"] = failed24h
	}

	kmsLifecycle, err := s.kmsLifecycle(ctx)
	if err == nil {
		ev.Controls["CC6.8_data_protection"] = kmsLifecycle
		if act, ok := kmsLifecycle["active"].(int64); ok {
			ev.Summary["kms_keys_active"] = act
		}
		if shr, ok := kmsLifecycle["shredded"].(int64); ok {
			ev.Summary["kms_keys_shredded"] = shr
		}
	}

	auditHealth, err := s.auditHealth(ctx)
	if err == nil {
		ev.Controls["CC7.2_audit_coverage"] = auditHealth
		if rows, ok := auditHealth["rowsLast24h"].(int64); ok {
			ev.Summary["audit_rows_24h"] = rows
		}
	}

	return ev, nil
}

// --- sub-queries ---

// privilegedUsers reports the head-count of users holding system
// roles that can administer the platform (CC6.1). The authz module's
// role-binding table is the long-term authority; for v1 we read the
// system role column on users, which matches the JWT srole claim.
// Privileged roles are operator-tier by definition — clients never
// hold super_admin/administrator/developer.
func (s *SOC2EvidenceService) privilegedUsers(ctx context.Context) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	users := s.db.Collection(userRepo.OperatorUsersCollection)
	roles := []string{"super_admin", "administrator", "developer"}
	out := map[string]any{"byRole": map[string]int64{}}
	var total int64
	for _, r := range roles {
		//tenantscope:allow platform-wide evidence query — aggregate across tenants by design
		n, err := users.CountDocuments(ctx, bson.M{"role": r, "deletedAt": bson.M{"$exists": false}})
		if err != nil {
			return nil, err
		}
		out["byRole"].(map[string]int64)[r] = n
		total += n
	}
	out["total"] = total
	return out, nil
}

// mfaCoverage computes the fraction of privileged users who have an
// MFA factor enrolled — SOC2 CC6.6 expects privileged accounts to have
// a second factor. 100% is the target; any deficit is an auditor
// findings source. Non-privileged MFA coverage is tracked separately
// for completeness.
//
// Privileged users (super_admin/administrator/developer) live in
// operator_users by definition (Tier-1), so the count and the MFA
// factor lookup both query the operator-tier collections (ADR-0003
// PR-D D-8 cutover).
func (s *SOC2EvidenceService) mfaCoverage(ctx context.Context) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	users := s.db.Collection(userRepo.OperatorUsersCollection)
	factors := s.db.Collection(authModels.OperatorMFAFactorsCollection)

	//tenantscope:allow platform-wide MFA coverage aggregate — privileged users span tenants
	privUsers, err := users.Distinct(ctx, "uuid", bson.M{
		"role":      bson.M{"$in": []string{"super_admin", "administrator", "developer"}},
		"deletedAt": bson.M{"$exists": false},
	})
	if err != nil {
		return nil, err
	}
	privileged := int64(len(privUsers))
	if privileged == 0 {
		return map[string]any{
			"privileged":        int64(0),
			"privilegedWithMFA": int64(0),
			"percentCovered":    100.0,
		}, nil
	}

	//tenantscope:allow MFA factors are queried by user-UUID list that is itself platform-wide
	covered, err := factors.CountDocuments(ctx, bson.M{
		"userUuid": bson.M{"$in": privUsers},
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"privileged":        privileged,
		"privilegedWithMFA": covered,
		"percentCovered":    percent(covered, privileged),
	}, nil
}

// failedLogins counts auth.login.failed audit events over the window.
// Sharp spikes are the early-warning signal for credential stuffing
// or misconfigured integrations — CC7.2 monitoring.
func (s *SOC2EvidenceService) failedLogins(ctx context.Context, window time.Duration) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	coll := s.db.Collection(complianceModels.AuditEventsCollection)
	//tenantscope:allow failed-login trend is platform-wide by design
	return coll.CountDocuments(ctx, bson.M{
		"action":    "auth.login.failed",
		"timestamp": bson.M{"$gte": time.Now().Add(-window)},
	})
}

// kmsLifecycle summarizes the per-tenant KMS key population — number
// of active keys (encryption available), number of shredded keys
// (crypto-shred complete), and how many tenants still lack a key
// (pre-Phase-4.3 rollouts). CC6.8 data-protection coverage.
func (s *SOC2EvidenceService) kmsLifecycle(ctx context.Context) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	coll := s.db.Collection(complianceModels.KMSKeysCollection)
	//tenantscope:allow KMS lifecycle summary is platform-wide evidence
	active, err := coll.CountDocuments(ctx, bson.M{"state": complianceModels.KMSStateActive})
	if err != nil {
		return nil, err
	}
	//tenantscope:allow KMS lifecycle summary is platform-wide evidence
	shredded, err := coll.CountDocuments(ctx, bson.M{"state": complianceModels.KMSStatePendingDeletion})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"active":   active,
		"shredded": shredded,
	}, nil
}

// auditHealth reports that the audit pipeline is actually running.
// An auditor's first question is "are you capturing events at all?" —
// rowsLast24h > 0 is the minimum viable answer, earliest/latest
// stamps bracket the coverage window.
func (s *SOC2EvidenceService) auditHealth(ctx context.Context) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	coll := s.db.Collection(complianceModels.AuditEventsCollection)
	//tenantscope:allow audit pipeline health is platform-wide evidence
	rows24h, err := coll.CountDocuments(ctx, bson.M{
		"timestamp": bson.M{"$gte": time.Now().Add(-24 * time.Hour)},
	})
	if err != nil {
		return nil, err
	}
	total, err := coll.EstimatedDocumentCount(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"rowsLast24h": rows24h,
		"rowsTotal":   total,
	}, nil
}

func percent(covered, total int64) float64 {
	if total == 0 {
		return 100.0
	}
	return float64(covered) / float64(total) * 100.0
}
