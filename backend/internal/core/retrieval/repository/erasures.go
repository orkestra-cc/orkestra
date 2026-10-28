package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// CollErasures holds erasure tombstones: global (user) and tenant-scoped.
const CollErasures = "retrieval_erasures"

// ErrInvalidErasure: the tombstone's shape is not one of the allowed forms.
var ErrInvalidErasure = errors.New("retrieval repository: invalid erasure")

const userSubjectPrefix = "user:"

// ErasureIndexes are the declared indexes of CollErasures; the module and the
// repository tests share them. {uuid} and {scope, kind, subject} are system
// sweeps (global user tombstones carry no tenant).
func ErasureIndexes() []module.IndexSpec {
	return []module.IndexSpec{
		{OrderedKeys: []module.IndexKey{{Field: "uuid", Direction: 1}}, Unique: true},
		{OrderedKeys: []module.IndexKey{{Field: "scope", Direction: 1}, {Field: "kind", Direction: 1}, {Field: "subject", Direction: 1}}},
		{OrderedKeys: []module.IndexKey{{Field: "tenantId", Direction: 1}, {Field: "kind", Direction: 1}, {Field: "corpus", Direction: 1}, {Field: "sourceId", Direction: 1}}},
		{OrderedKeys: []module.IndexKey{{Field: "tenantId", Direction: 1}, {Field: "kind", Direction: 1}, {Field: "subject", Direction: 1}}},
	}
}

// Erasures owns retrieval_erasures.
type Erasures struct{ coll *mongo.Collection }

func NewErasures(db *mongo.Database) *Erasures {
	return &Erasures{coll: db.Collection(CollErasures)}
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidErasure, fmt.Sprintf(format, a...))
}

// Insert validates the shape, sets UUID (if empty) and CreatedAt (now UTC).
// global: Kind user, TenantID "", Subject "user:<non-empty>", no Corpus/SourceID.
// tenant: TenantID = ctx tenant (a different non-empty TenantID is refused),
// Kind subject (Subject set) | item (Corpus and SourceID set) | tenant.
func (r *Erasures) Insert(ctx context.Context, e *models.Erasure) error {
	switch e.Scope {
	case models.ErasureScopeGlobal:
		if e.TenantID != "" || e.Kind != models.ErasureKindUser || e.Corpus != "" || e.SourceID != "" ||
			!strings.HasPrefix(e.Subject, userSubjectPrefix) || len(e.Subject) == len(userSubjectPrefix) {
			return invalid("global tombstone must be kind user with subject user:<id> and no tenant, corpus or source")
		}
	case models.ErasureScopeTenant:
		switch e.Kind {
		case models.ErasureKindSubject:
			if e.Subject == "" {
				return invalid("subject tombstone needs a subject")
			}
		case models.ErasureKindItem:
			if e.Corpus == "" || e.SourceID == "" {
				return invalid("item tombstone needs corpus and source id")
			}
		case models.ErasureKindTenant:
		default:
			return invalid("tenant tombstone kind %q", e.Kind)
		}
		tenantID, err := tenantrepo.StampInsert(ctx)
		if err != nil {
			return err
		}
		if e.TenantID != "" && e.TenantID != tenantID {
			return invalid("tenant tombstone for a different tenant")
		}
		e.TenantID = tenantID
	default:
		return invalid("scope %q", e.Scope)
	}
	if e.UUID == "" {
		e.UUID = uuid.NewString()
	}
	e.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	if e.Scope == models.ErasureScopeGlobal {
		//tenantscope:allow dsr: global user tombstone written by the erasure path (ADR-0023 D12)
		_, err := r.coll.InsertOne(ctx, e)
		return err
	}
	_, err := r.coll.InsertOne(ctx, e)
	return err
}

// Covering returns the global user tombstones naming any of subjects and the
// tenant tombstones of ctx's tenant (kind tenant; item for corpus + sourceIDs;
// subject in subjects).
func (r *Erasures) Covering(ctx context.Context, corpus string, sourceIDs, subjects []string) ([]models.Erasure, error) {
	var out []models.Erasure
	var users []string
	for _, s := range subjects {
		if strings.HasPrefix(s, userSubjectPrefix) {
			users = append(users, s)
		}
	}
	if len(users) > 0 {
		//tenantscope:allow system: global user tombstones carry no tenant (ADR-0023 D12)
		cur, err := r.coll.Find(ctx, bson.M{"scope": models.ErasureScopeGlobal, "kind": models.ErasureKindUser, "subject": bson.M{"$in": users}})
		if err != nil {
			return nil, err
		}
		if err := cur.All(ctx, &out); err != nil {
			return nil, err
		}
	}
	or := []bson.M{{"kind": models.ErasureKindTenant}}
	if len(sourceIDs) > 0 {
		or = append(or, bson.M{"kind": models.ErasureKindItem, "corpus": corpus, "sourceId": bson.M{"$in": sourceIDs}})
	}
	if len(subjects) > 0 {
		or = append(or, bson.M{"kind": models.ErasureKindSubject, "subject": bson.M{"$in": subjects}})
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{"scope": models.ErasureScopeTenant, "$or": or})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	var tenant []models.Erasure
	if err := cur.All(ctx, &tenant); err != nil {
		return nil, err
	}
	return append(out, tenant...), nil
}
