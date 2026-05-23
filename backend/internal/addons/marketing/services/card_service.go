// Phase 4 — Card lifecycle.
//
// CardService is the single funnel for the card state machine. Every
// transition (Issue, Suspend, Reinstate, Revoke, Expire) lands here;
// each one is gated by models.CanTransitionCardStatus, writes the
// marketing_cards row, keeps marketing_persons.activeCardUuids in
// sync, and emits the matching audit Activity through the existing
// ActivityService. Handlers + the expiration scheduler call this
// service; nothing else touches marketing_cards or the activeCardUuids
// denorm.

package services

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Sentinel errors surfaced by CardService. The handler layer maps
// these onto the matching errcode.MarketingCard* constants for the
// wire response. Tests assert against these directly so the mapping
// is the only place that knows about the HTTP status.

// ErrCardInvalidTransition signals that the requested status change
// is not legal for the card's current state. The transition matrix
// is documented in IMPLEMENTATION_PLAN_PHASE_4.md §3.6.
var ErrCardInvalidTransition = errors.New("marketing: invalid card status transition")

// ErrCardAlreadyExists signals that the AllowMultiplePerPerson=false
// rule is being violated — the (person, type) pair already has an
// active card. The operator must revoke the existing card first or
// flip the type's AllowMultiplePerPerson on.
var ErrCardAlreadyExists = errors.New("marketing: active card of this type already exists for person")

// ErrCardCodeCollision signals that the rendered code already exists
// in the tenant. Mapped from the underlying duplicate-key error.
// Callers may retry — a hot type with {seq:N} normally widens past
// the collision on the next bump.
var ErrCardCodeCollision = errors.New("marketing: card code collision")

// ErrTierRequired / ErrTierNotInType narrow the tier-validation
// failure modes — handlers want to render distinct messages.
var (
	ErrTierRequired  = errors.New("marketing: card type defines tiers but issue has no tier")
	ErrTierNotInType = errors.New("marketing: card tier not declared on card type")
	ErrTierForbidden = errors.New("marketing: card type defines no tiers but issue carries one")
)

// CardServiceDeps groups the dependencies of CardService. Passing a
// struct avoids the long-arg-list anti-pattern as the service grows.
type CardServiceDeps struct {
	TypeService *CardTypeService
	CardRepo    *repository.CardRepository
	SeqRepo     *repository.CardSequenceRepository
	PersonRepo  *repository.PersonRepository
	ActivitySvc *ActivityService
	Logger      *slog.Logger

	// Clock + RandReader are injected so tests can assert exact
	// outputs. Production wiring uses time.Now + crypto/rand.Reader.
	Clock      func() time.Time
	RandReader io.Reader
}

// CardService is the orchestration surface for the §7 lifecycle.
type CardService struct {
	deps CardServiceDeps
}

// NewCardService validates required deps and returns the service.
// Clock + RandReader are wired with sane defaults when nil.
func NewCardService(deps CardServiceDeps) *CardService {
	if deps.Clock == nil {
		deps.Clock = func() time.Time { return time.Now().UTC() }
	}
	if deps.RandReader == nil {
		deps.RandReader = rand.Reader
	}
	return &CardService{deps: deps}
}

// IssueParams is the input DTO for CardService.Issue.
type IssueParams struct {
	PersonUUID   string
	CardTypeUUID string
	Tier         string
	Benefits     []string // optional override; defaults to type.DefaultBenefits
	ExpiresAt    *time.Time
	Notes        string
}

// Issue emits a new card to a person. The full flow:
//
//  1. Look up the type + cache its CodeFormat AST.
//  2. Validate the tier against the type's Tiers list.
//  3. If the type forbids multiple-per-person, probe the
//     (person, type, status=active) index. Reject ErrCardAlreadyExists
//     when a row exists.
//  4. Reserve the next sequence value if the template uses {seq:N}.
//  5. Render the code, insert the card row.
//  6. $addToSet the new UUID onto Person.activeCardUuids.
//  7. Emit KindCardIssued through ActivityService.
//
// Steps 5–7 are not wrapped in a Mongo transaction — the inputs are
// idempotent and the fail-safe (tenantId, code) unique index catches
// the rare code-collision race. If the activeCardUuids update or the
// activity emit fail, the card row is still consistent (the denorm
// is rebuildable from marketing_cards).
func (s *CardService) Issue(ctx context.Context, p IssueParams) (*models.Card, error) {
	if strings.TrimSpace(p.PersonUUID) == "" {
		return nil, errors.New("marketing: missing personUuid")
	}
	if strings.TrimSpace(p.CardTypeUUID) == "" {
		return nil, errors.New("marketing: missing cardTypeUuid")
	}

	cardType, err := s.deps.TypeService.Get(ctx, p.CardTypeUUID)
	if err != nil {
		return nil, err
	}
	if !cardType.Active {
		return nil, errors.New("marketing: card type is inactive — cannot issue")
	}

	if err := validateTier(cardType, p.Tier); err != nil {
		return nil, err
	}

	// Person must exist in the tenant — issuing a card to a missing
	// person would orphan the audit activity. Repo Get returns
	// ErrPersonNotFound on a miss, which the handler maps to 404.
	if _, err := s.deps.PersonRepo.GetByUUID(ctx, p.PersonUUID); err != nil {
		return nil, err
	}

	// Enforce one-active-per-(person, type) when the type forbids
	// multiples. The (tenant, person, type, status=active) probe is
	// a service-layer check because the SDK IndexSpec does not yet
	// support partial unique indexes — same limitation the Membership
	// invariants live with.
	if !cardType.AllowMultiplePerPerson {
		existing, err := s.deps.CardRepo.FindActiveByPersonAndType(ctx, p.PersonUUID, p.CardTypeUUID)
		if err != nil && !errors.Is(err, repository.ErrCardNotFound) {
			return nil, err
		}
		if existing != nil {
			return nil, ErrCardAlreadyExists
		}
	}

	ast, err := s.deps.TypeService.CodeFormatAST(ctx, p.CardTypeUUID)
	if err != nil {
		return nil, err
	}

	now := s.deps.Clock()
	var seq int64
	if ast.HasSequence() {
		seq, err = s.deps.SeqRepo.NextSequence(ctx, p.CardTypeUUID)
		if err != nil {
			return nil, fmt.Errorf("marketing: reserve sequence: %w", err)
		}
	}
	code, err := RenderCardCode(ast, now, seq, s.deps.RandReader)
	if err != nil {
		return nil, fmt.Errorf("marketing: render code: %w", err)
	}

	benefits := p.Benefits
	if benefits == nil {
		// Snapshot from the type; the slice is copied so future
		// edits to cardType.DefaultBenefits don't propagate.
		benefits = append([]string(nil), cardType.DefaultBenefits...)
	}

	actor, _ := ctxauth.GetUserUUID(ctx)
	card := &models.Card{
		UUID:         uuid.New().String(),
		CardTypeUUID: p.CardTypeUUID,
		Code:         code,
		PersonUUID:   p.PersonUUID,
		Tier:         p.Tier,
		Status:       models.CardStatusActive,
		Benefits:     benefits,
		Notes:        p.Notes,
		ExpiresAt:    p.ExpiresAt,
		IssuedAt:     now,
		IssuedBy:     actor,
	}
	if err := card.Validate(); err != nil {
		return nil, err
	}

	if err := s.deps.CardRepo.Create(ctx, card); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, ErrCardCodeCollision
		}
		return nil, err
	}

	if err := s.deps.PersonRepo.AddActiveCard(ctx, p.PersonUUID, card.UUID); err != nil {
		// Best-effort log; the denorm is rebuildable from
		// marketing_cards if it ever drifts.
		if s.deps.Logger != nil {
			s.deps.Logger.WarnContext(ctx, "marketing: failed to update person activeCardUuids after issue",
				slog.String("personUuid", p.PersonUUID),
				slog.String("cardUuid", card.UUID),
				slog.Any("err", err),
			)
		}
	}

	s.emitIssued(ctx, card, cardType.Key)
	return card, nil
}

// Suspend transitions an active card to suspended state. Reversible
// via Reinstate. The card stays in marketing_persons.activeCardUuids
// (see §3.4 of the plan).
func (s *CardService) Suspend(ctx context.Context, cardUUID, reason string) (*models.Card, error) {
	card, err := s.deps.CardRepo.GetByUUID(ctx, cardUUID)
	if err != nil {
		return nil, err
	}
	if !models.CanTransitionCardStatus(card.Status, models.CardStatusSuspended) {
		return nil, ErrCardInvalidTransition
	}
	now := s.deps.Clock()
	actor, _ := ctxauth.GetUserUUID(ctx)
	patch := bson.M{
		"status":        models.CardStatusSuspended,
		"suspendedAt":   now,
		"suspendedBy":   actor,
		"suspendReason": reason,
	}
	if err := s.deps.CardRepo.Update(ctx, cardUUID, patch); err != nil {
		return nil, err
	}
	prev := card.Status
	card.Status = models.CardStatusSuspended
	card.SuspendedAt = &now
	card.SuspendedBy = actor
	card.SuspendReason = reason
	s.emitStatusChanged(ctx, card, prev, reason)
	return card, nil
}

// Reinstate transitions a suspended card back to active. Clears the
// suspended_* fields via $unset. Re-adds the card to
// activeCardUuids — it never left, but the $addToSet is idempotent
// and safe to call defensively.
func (s *CardService) Reinstate(ctx context.Context, cardUUID string) (*models.Card, error) {
	card, err := s.deps.CardRepo.GetByUUID(ctx, cardUUID)
	if err != nil {
		return nil, err
	}
	if !models.CanTransitionCardStatus(card.Status, models.CardStatusActive) {
		return nil, ErrCardInvalidTransition
	}
	set := bson.M{"status": models.CardStatusActive}
	unset := bson.M{
		"suspendedAt":   "",
		"suspendedBy":   "",
		"suspendReason": "",
	}
	if err := s.deps.CardRepo.UpdateWithUnset(ctx, cardUUID, set, unset); err != nil {
		return nil, err
	}
	// Defensive re-add: Suspend left the uuid in the list (per §3.4)
	// so this is normally a no-op via $addToSet, but a manual fixup
	// that bypassed the service may have removed it.
	_ = s.deps.PersonRepo.AddActiveCard(ctx, card.PersonUUID, cardUUID)
	prev := card.Status
	card.Status = models.CardStatusActive
	card.SuspendedAt = nil
	card.SuspendedBy = ""
	card.SuspendReason = ""
	s.emitStatusChanged(ctx, card, prev, "")
	return card, nil
}

// Revoke transitions a card to the terminal revoked state. Pulls the
// uuid from Person.activeCardUuids atomically with the row update.
// The reason is operator-supplied free text — the schema's
// revoke_reason field has no enum.
func (s *CardService) Revoke(ctx context.Context, cardUUID, reason string) (*models.Card, error) {
	return s.revoke(ctx, cardUUID, reason, "")
}

// Expire is the scheduler's entry point. Differs from Revoke only in
// the sentinel reason ("expired") that the audit payload carries —
// downstream consumers can distinguish ops-initiated revocations
// from scheduler-driven ones by inspecting payload.reason.
func (s *CardService) Expire(ctx context.Context, cardUUID string) (*models.Card, error) {
	return s.revoke(ctx, cardUUID, "expired", "expired")
}

func (s *CardService) revoke(ctx context.Context, cardUUID, reason, sentinel string) (*models.Card, error) {
	card, err := s.deps.CardRepo.GetByUUID(ctx, cardUUID)
	if err != nil {
		return nil, err
	}
	if !models.CanTransitionCardStatus(card.Status, models.CardStatusRevoked) {
		return nil, ErrCardInvalidTransition
	}
	now := s.deps.Clock()
	actor, _ := ctxauth.GetUserUUID(ctx)
	patch := bson.M{
		"status":       models.CardStatusRevoked,
		"revokedAt":    now,
		"revokedBy":    actor,
		"revokeReason": reason,
	}
	if err := s.deps.CardRepo.Update(ctx, cardUUID, patch); err != nil {
		return nil, err
	}
	if err := s.deps.PersonRepo.RemoveActiveCard(ctx, card.PersonUUID, cardUUID); err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.WarnContext(ctx, "marketing: failed to remove from activeCardUuids on revoke",
				slog.String("personUuid", card.PersonUUID),
				slog.String("cardUuid", cardUUID),
				slog.Any("err", err),
			)
		}
	}
	prev := card.Status
	card.Status = models.CardStatusRevoked
	card.RevokedAt = &now
	card.RevokedBy = actor
	card.RevokeReason = reason
	if sentinel != "" {
		// Tag the emit so downstream consumers can tell scheduler
		// revocations apart from manual ones. We carry it in the
		// reason field of the payload via emitStatusChanged.
		s.emitStatusChanged(ctx, card, prev, sentinel)
	} else {
		s.emitStatusChanged(ctx, card, prev, reason)
	}
	return card, nil
}

// validateTier enforces the type's tier policy:
//   - type.Tiers empty → issue must NOT carry a tier.
//   - type.Tiers non-empty → issue MUST carry one, and it must appear
//     in the list.
func validateTier(t *models.CardType, tier string) error {
	tier = strings.TrimSpace(tier)
	if len(t.Tiers) == 0 {
		if tier != "" {
			return ErrTierForbidden
		}
		return nil
	}
	if tier == "" {
		return ErrTierRequired
	}
	for _, allowed := range t.Tiers {
		if allowed == tier {
			return nil
		}
	}
	return ErrTierNotInType
}

// emitIssued posts a KindCardIssued activity. Payload follows the
// shape documented in IMPLEMENTATION_PLAN_PHASE_4.md §5.4.
func (s *CardService) emitIssued(ctx context.Context, card *models.Card, typeKey string) {
	if s.deps.ActivitySvc == nil {
		return
	}
	payload := map[string]any{
		"cardUuid":     card.UUID,
		"cardTypeUuid": card.CardTypeUUID,
		"cardTypeKey":  typeKey,
		"code":         card.Code,
	}
	if card.Tier != "" {
		payload["tier"] = card.Tier
	}
	if card.ExpiresAt != nil {
		payload["expiresAt"] = card.ExpiresAt.UTC()
	}
	a := &models.Activity{
		Kind:       models.KindCardIssued,
		PersonUUID: card.PersonUUID,
		OccurredAt: card.IssuedAt,
		RecordedAt: card.IssuedAt,
		Source:     models.ActivitySourceSystem,
		Payload:    payload,
		Refs: models.ActivityRefs{
			CardUUID: card.UUID,
		},
		ExternalID: "card_issued:" + card.UUID,
	}
	if _, err := s.deps.ActivitySvc.Create(ctx, a); err != nil && s.deps.Logger != nil {
		s.deps.Logger.WarnContext(ctx, "marketing: emit card_issued failed",
			slog.String("cardUuid", card.UUID),
			slog.Any("err", err),
		)
	}
}

// emitStatusChanged posts a KindCardStatusChanged activity. The
// payload carries `from`, `to`, and `reason` so the timeline UI can
// render the transition without re-reading the card row.
func (s *CardService) emitStatusChanged(ctx context.Context, card *models.Card, from models.CardStatus, reason string) {
	if s.deps.ActivitySvc == nil {
		return
	}
	now := s.deps.Clock()
	payload := map[string]any{
		"cardUuid":     card.UUID,
		"cardTypeUuid": card.CardTypeUUID,
		"from":         string(from),
		"to":           string(card.Status),
	}
	if reason != "" {
		payload["reason"] = reason
	}
	a := &models.Activity{
		Kind:       models.KindCardStatusChanged,
		PersonUUID: card.PersonUUID,
		OccurredAt: now,
		RecordedAt: now,
		Source:     models.ActivitySourceSystem,
		Payload:    payload,
		Refs: models.ActivityRefs{
			CardUUID: card.UUID,
		},
		// ExternalID embeds both the card uuid and the transition's
		// recorded-at timestamp so concurrent transitions stay
		// distinct under the dedupKey unique index.
		ExternalID: fmt.Sprintf("card_status_changed:%s:%d", card.UUID, now.UnixNano()),
	}
	if _, err := s.deps.ActivitySvc.Create(ctx, a); err != nil && s.deps.Logger != nil {
		s.deps.Logger.WarnContext(ctx, "marketing: emit card_status_changed failed",
			slog.String("cardUuid", card.UUID),
			slog.Any("err", err),
		)
	}
}
