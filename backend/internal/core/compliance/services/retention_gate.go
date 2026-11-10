package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Reason reported for the platform floor. Reporter reasons are the producers'
// own labels.
const retentionReasonPlatformMinimum = "platform_minimum"

// ErrRetentionMinimum is returned when a hard delete comes before the subject's
// retention floor; errors.As to *RetentionGateError for the date.
var ErrRetentionMinimum = errors.New("compliance: the subject's data must be retained")

// ErrRetentionCheckUnavailable is returned when the gate could not be
// evaluated (a lookup or a producer's reporter failed). The hard delete fails
// closed; callers map it to 503, not to the 409 of a known obligation.
var ErrRetentionCheckUnavailable = errors.New("compliance: the retention check could not be completed")

// RetentionGateError is the binding retention obligation of a subject.
type RetentionGateError struct {
	RetainUntil time.Time
	Reasons     []string
}

func (e *RetentionGateError) Error() string {
	return fmt.Sprintf("%s until %s", ErrRetentionMinimum.Error(), e.RetainUntil.Format(time.RFC3339))
}

// Unwrap lets errors.Is match ErrRetentionMinimum.
func (e *RetentionGateError) Unwrap() error { return ErrRetentionMinimum }

// RetentionGate decides whether a subject may be hard-deleted now: the
// platform floor (the configured years from the user row's creation) and the
// obligations the registered producers report through iface.PIIRetentionReporter.
type RetentionGate struct {
	registry *iface.PIIProducerRegistry
	years    func() int
	now      func() time.Time
	// createdAt returns the creation time of the subject's user row; found is
	// false when the subject has none.
	createdAt func(ctx context.Context, userUUID string) (t time.Time, found bool, err error)
}

// NewRetentionGate builds the gate. years is read on every evaluation (0
// disables the platform floor).
func NewRetentionGate(db *mongo.Database, registry *iface.PIIProducerRegistry, years func() int) *RetentionGate {
	return &RetentionGate{
		registry: registry,
		years:    years,
		now:      func() time.Time { return time.Now().UTC() },
		createdAt: func(ctx context.Context, userUUID string) (time.Time, bool, error) {
			for _, coll := range userTombstoneCollections {
				var row struct {
					CreatedAt time.Time `bson:"createdAt"`
				}
				//tenantscope:allow platform retention floor looks the subject up by UUID across tenants.
				err := db.Collection(coll).FindOne(ctx, bson.M{"uuid": userUUID}).Decode(&row)
				if errors.Is(err, mongo.ErrNoDocuments) {
					continue
				}
				if err != nil {
					return time.Time{}, false, err
				}
				if row.CreatedAt.IsZero() {
					return time.Time{}, false, nil
				}
				return row.CreatedAt, true, nil
			}
			return time.Time{}, false, nil
		},
	}
}

// Evaluate returns the binding obligation (the latest Until, with every
// reason that applies) or nil when none applies now. A lookup or reporter
// failure is returned as an error.
func (g *RetentionGate) Evaluate(ctx context.Context, userUUID string) (*RetentionGateError, error) {
	now := g.now()
	var obligations []iface.RetentionObligation
	if years := g.years(); years > 0 {
		created, found, err := g.createdAt(ctx, userUUID)
		if err != nil {
			return nil, fmt.Errorf("retention floor lookup: %w", err)
		}
		if found {
			obligations = append(obligations, iface.RetentionObligation{
				Until: created.AddDate(years, 0, 0), Reason: retentionReasonPlatformMinimum,
			})
		}
	}
	if g.registry != nil {
		for _, p := range g.registry.List() {
			rep, ok := p.(iface.PIIRetentionReporter)
			if !ok {
				continue
			}
			ob, err := reportRetention(ctx, rep, userUUID)
			if err != nil {
				return nil, fmt.Errorf("retention reporter %q: %w", baseProducerKey(p), err)
			}
			if ob != nil {
				obligations = append(obligations, *ob)
			}
		}
	}
	var out *RetentionGateError
	seen := map[string]bool{}
	for _, ob := range obligations {
		if !ob.Until.After(now) {
			continue
		}
		if out == nil {
			out = &RetentionGateError{}
		}
		if ob.Until.After(out.RetainUntil) {
			out.RetainUntil = ob.Until
		}
		if ob.Reason != "" && !seen[ob.Reason] {
			seen[ob.Reason] = true
			out.Reasons = append(out.Reasons, ob.Reason)
		}
	}
	if out != nil {
		sort.Strings(out.Reasons)
	}
	return out, nil
}

// reportRetention calls a reporter, turning a panic into an error so the gate
// fails closed instead of crashing the replica.
func reportRetention(ctx context.Context, rep iface.PIIRetentionReporter, userUUID string) (ob *iface.RetentionObligation, err error) {
	defer func() {
		if r := recover(); r != nil {
			ob, err = nil, errProducerPanicked
		}
	}()
	return rep.RetainUntil(ctx, userUUID)
}

// SetRetentionGate wires the minimum-retention gate. Without one CheckRetention
// always passes.
func (s *DSRService) SetRetentionGate(g *RetentionGate) { s.retentionGate = g }

// CheckRetention returns nil for EraseAnonymize or when no obligation applies,
// a *RetentionGateError (ErrRetentionMinimum) when a hard delete comes too
// early, and an error wrapping ErrRetentionCheckUnavailable when the gate could
// not be evaluated.
func (s *DSRService) CheckRetention(ctx context.Context, userUUID string, mode iface.EraseMode) error {
	if mode != iface.EraseHardDelete || s.retentionGate == nil {
		return nil
	}
	gerr, err := s.retentionGate.Evaluate(ctx, userUUID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRetentionCheckUnavailable, err)
	}
	if gerr != nil {
		return gerr
	}
	return nil
}
