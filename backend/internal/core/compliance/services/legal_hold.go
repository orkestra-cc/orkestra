package services

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
)

var (
	// ErrLegalHoldExpiryPast is returned when an expiry is not in the future.
	ErrLegalHoldExpiryPast = errors.New("compliance: a legal hold expiry must be in the future")
	// ErrLegalHoldUpdateInvalid is returned for an update that changes nothing
	// or both sets and clears the same date.
	ErrLegalHoldUpdateInvalid = errors.New("compliance: legal hold update needs a date to set or clear, not both")
)

const (
	maxLegalHoldNoteLen = 1000
	// legalHoldExpiryBatch bounds one ExpireDue call; the next run takes the rest.
	legalHoldExpiryBatch = 500
)

// legalHoldRepo is the persistence seam the service depends on. Satisfied by
// *repository.LegalHoldRepository in production; a fake stands in for unit
// tests so the service logic is exercised without MongoDB.
type legalHoldRepo interface {
	Insert(ctx context.Context, h *models.LegalHold) error
	Release(ctx context.Context, uuid, releasedBy, reason string, now time.Time) error
	ListActive(ctx context.Context, userUUID string, now time.Time) ([]models.LegalHold, error)
	List(ctx context.Context, userUUID string, includeReleased bool, now time.Time) ([]models.LegalHold, error)
	IsHeld(ctx context.Context, userUUID string, now time.Time) (bool, error)
	Get(ctx context.Context, uuid string) (*models.LegalHold, error)
	Update(ctx context.Context, uuid string, set bson.M, unset []string) (*models.LegalHold, error)
	AddNote(ctx context.Context, uuid string, note models.LegalHoldNote) (*models.LegalHold, error)
	ExpireDue(ctx context.Context, now time.Time, limit int) ([]models.LegalHold, error)
}

// LegalHoldService manages litigation/investigation holds and answers the
// DSR pipeline's IsHeld check — it satisfies DSRService's LegalHoldChecker.
type LegalHoldService struct {
	repo legalHoldRepo
	sink iface.AuditSink // nil-safe
	now  func() time.Time
}

// NewLegalHoldService binds the service to its repository.
func NewLegalHoldService(repo legalHoldRepo) *LegalHoldService {
	return &LegalHoldService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

// SetAuditSink wires the audit trail; nil leaves auditing off.
func (s *LegalHoldService) SetAuditSink(sink iface.AuditSink) { s.sink = sink }

// PlaceInput carries the fields an operator supplies when placing a hold.
type PlaceInput struct {
	UserUUID  string
	TenantID  string
	Reason    string
	CaseRef   string
	PlacedBy  string
	ReviewAt  *time.Time
	ExpiresAt *time.Time
}

func (s *LegalHoldService) audit(ctx context.Context, actor, action string, h *models.LegalHold, extra map[string]any) {
	if s.sink == nil {
		return
	}
	actorType := "user"
	if actor == models.LegalHoldExpiryActor {
		actorType = "system"
	}
	meta := map[string]any{"subjectUserUuid": h.UserUUID}
	for k, v := range extra {
		meta[k] = v
	}
	s.sink.Emit(ctx, iface.AuditEvent{
		TenantID:     h.TenantID,
		ActorUserID:  actor,
		ActorType:    actorType,
		Action:       action,
		ResourceType: "legal_hold",
		ResourceID:   h.UUID,
		Outcome:      "success",
		Metadata:     meta,
	})
}

func (s *LegalHoldService) mark(h *models.LegalHold, now time.Time) *models.LegalHold {
	h.ReviewDue = h.Active && h.ReviewAt != nil && !h.ReviewAt.After(now)
	return h
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// Place records a new active hold for the subject. The reason is required
// (1 to 500 characters) and stored masked; an expiry must be in the future.
func (s *LegalHoldService) Place(ctx context.Context, in PlaceInput) (*models.LegalHold, error) {
	reason, ok := NormalizeReason(in.Reason)
	if !ok {
		return nil, ErrReasonRequired
	}
	now := s.now()
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		return nil, ErrLegalHoldExpiryPast
	}
	h := &models.LegalHold{
		UUID:      uuid.NewString(),
		UserUUID:  in.UserUUID,
		TenantID:  in.TenantID,
		Reason:    maskReason(reason),
		CaseRef:   strings.TrimSpace(in.CaseRef),
		PlacedBy:  in.PlacedBy,
		PlacedAt:  now,
		Active:    true,
		ReviewAt:  utc(in.ReviewAt),
		ExpiresAt: utc(in.ExpiresAt),
	}
	if err := s.repo.Insert(ctx, h); err != nil {
		return nil, err
	}
	extra := map[string]any{}
	if h.ReviewAt != nil {
		extra["reviewAt"] = h.ReviewAt.Format(time.RFC3339)
	}
	if h.ExpiresAt != nil {
		extra["expiresAt"] = h.ExpiresAt.Format(time.RFC3339)
	}
	s.audit(ctx, in.PlacedBy, "compliance.legal_hold.placed", h, extra)
	return s.mark(h, now), nil
}

// Release marks a hold inactive. The reason is required (1 to 500 characters)
// and stored masked.
func (s *LegalHoldService) Release(ctx context.Context, uuid, releasedBy, reason string) error {
	reason, ok := NormalizeReason(reason)
	if !ok {
		return ErrReasonRequired
	}
	if err := s.repo.Release(ctx, uuid, releasedBy, maskReason(reason), s.now()); err != nil {
		return err
	}
	if s.sink != nil {
		// The row is only read for the subject UUID and tenant; a failed
		// read must not undo the release.
		if h, err := s.repo.Get(ctx, uuid); err == nil {
			s.audit(ctx, releasedBy, "compliance.legal_hold.released", h, nil)
		} else {
			s.audit(ctx, releasedBy, "compliance.legal_hold.released", &models.LegalHold{UUID: uuid}, nil)
		}
	}
	return nil
}

// Update changes the review date and/or expiry of an active hold. A date is
// set by passing it, removed with the matching clear flag; setting and
// clearing the same date, or changing nothing, is ErrLegalHoldUpdateInvalid.
func (s *LegalHoldService) Update(ctx context.Context, uuid, actor string, reviewAt, expiresAt *time.Time, clearReview, clearExpiry bool) (*models.LegalHold, error) {
	if (reviewAt != nil && clearReview) || (expiresAt != nil && clearExpiry) ||
		(reviewAt == nil && expiresAt == nil && !clearReview && !clearExpiry) {
		return nil, ErrLegalHoldUpdateInvalid
	}
	now := s.now()
	if expiresAt != nil && !expiresAt.After(now) {
		return nil, ErrLegalHoldExpiryPast
	}
	set := bson.M{"updatedAt": now}
	var unset, changed []string
	switch {
	case reviewAt != nil:
		set["reviewAt"] = reviewAt.UTC()
		changed = append(changed, "reviewAt")
	case clearReview:
		unset = append(unset, "reviewAt")
		changed = append(changed, "reviewAt")
	}
	switch {
	case expiresAt != nil:
		set["expiresAt"] = expiresAt.UTC()
		changed = append(changed, "expiresAt")
	case clearExpiry:
		unset = append(unset, "expiresAt")
		changed = append(changed, "expiresAt")
	}
	h, err := s.repo.Update(ctx, uuid, set, unset)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, actor, "compliance.legal_hold.updated", h, map[string]any{"changed": changed})
	return s.mark(h, now), nil
}

// AddNote appends a masked note (1 to 1000 characters) to an active hold.
// The note text is never copied into the audit event.
func (s *LegalHoldService) AddNote(ctx context.Context, uuid, actor, text string) (*models.LegalHold, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > maxLegalHoldNoteLen {
		return nil, ErrReasonRequired
	}
	now := s.now()
	note := models.LegalHoldNote{ID: uuidNew(), By: actor, At: now, Text: maskReason(text)}
	h, err := s.repo.AddNote(ctx, uuid, note)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, actor, "compliance.legal_hold.note_added", h, map[string]any{"noteId": note.ID})
	return s.mark(h, now), nil
}

func uuidNew() string { return uuid.NewString() }

// ListActive returns the holds in force, optionally scoped to one subject.
func (s *LegalHoldService) ListActive(ctx context.Context, userUUID string) ([]models.LegalHold, error) {
	now := s.now()
	items, err := s.repo.ListActive(ctx, userUUID, now)
	return s.markAll(items, now), err
}

// List returns holds, optionally scoped to one subject; includeReleased adds
// released and expired ones.
func (s *LegalHoldService) List(ctx context.Context, userUUID string, includeReleased bool) ([]models.LegalHold, error) {
	now := s.now()
	items, err := s.repo.List(ctx, userUUID, includeReleased, now)
	return s.markAll(items, now), err
}

func (s *LegalHoldService) markAll(items []models.LegalHold, now time.Time) []models.LegalHold {
	for i := range items {
		s.mark(&items[i], now)
	}
	return items
}

// IsHeld satisfies the DSR pipeline's LegalHoldChecker — a hold in force
// (active and not past its expiry) blocks erasure and retention auto-cleanup
// for the subject, even before the expiry job records the release. An error
// means unknown state.
func (s *LegalHoldService) IsHeld(ctx context.Context, userUUID string) (bool, error) {
	return s.repo.IsHeld(ctx, userUUID, s.now())
}

// ExpireDue releases the active holds whose expiry has passed and audits each
// release this call made (a hold another replica released first is not
// counted). Bounded per call; returns the number released.
func (s *LegalHoldService) ExpireDue(ctx context.Context) (int, error) {
	released, err := s.repo.ExpireDue(ctx, s.now(), legalHoldExpiryBatch)
	for i := range released {
		s.audit(ctx, models.LegalHoldExpiryActor, "compliance.legal_hold.expired", &released[i], nil)
	}
	return len(released), err
}
