package services

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// ErrInvalidPerson wraps caller-error responses on Person writes —
// failed identity-minimum check, missing required fields, etc.
var ErrInvalidPerson = errors.New("marketing: invalid person")

// PersonUpdateListener is fired after a Person is updated. Receives
// the pre-update copy + the freshly-fetched post-update copy so
// listeners can diff what changed (notably tag membership, which
// drives the auto-emission of `tag_added` / `tag_removed` activities
// from the marketing addon's Phase-3 hooks).
//
// Listeners run synchronously after the repo write succeeds; panics
// are recovered + logged but never propagate (the contract mirrors
// ActivityService.RegisterListener).
type PersonUpdateListener func(ctx context.Context, before, after models.Person)

// PersonService orchestrates Person writes: mints the UUID, enforces
// HasMinimumIdentity, runs custom-field validation, delegates to the
// repository, and cascades to memberships on delete.
type PersonService struct {
	repo       *repository.PersonRepository
	customFlds *CustomFieldService
	mships     *repository.MembershipRepository

	listenersMu     sync.RWMutex
	updateListeners []PersonUpdateListener
}

// NewPersonService wires the service with its collaborators.
func NewPersonService(
	repo *repository.PersonRepository,
	cf *CustomFieldService,
	mships *repository.MembershipRepository,
) *PersonService {
	return &PersonService{repo: repo, customFlds: cf, mships: mships}
}

// RegisterUpdateListener appends an update listener. Safe to call from
// multiple goroutines but typically a single-shot at module Init.
func (s *PersonService) RegisterUpdateListener(l PersonUpdateListener) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	s.updateListeners = append(s.updateListeners, l)
}

// Create persists a new Person. The identity-minimum invariant from
// the schema doc is enforced here — the repository will happily
// accept a fully-empty row (importer staging path), so this is the
// canonical gate for handler-initiated creates.
func (s *PersonService) Create(ctx context.Context, p *models.Person) (*models.Person, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: nil person", ErrInvalidPerson)
	}
	if !p.HasMinimumIdentity() {
		return nil, fmt.Errorf("%w: at least one of (firstName, lastName) or a primary email is required", ErrInvalidPerson)
	}
	if p.UUID == "" {
		p.UUID = uuid.New().String()
	}
	if err := s.customFlds.Validate(ctx, models.CustomFieldTargetPersons, p.CustomFields); err != nil {
		return nil, err
	}
	if actor, ok := ctxauth.GetUserUUID(ctx); ok {
		p.CreatedBy = actor
		p.UpdatedBy = actor
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return s.repo.GetByUUID(ctx, p.UUID)
}

// Get returns the person by UUID inside the caller's tenant.
func (s *PersonService) Get(ctx context.Context, uuid string) (*models.Person, error) {
	return s.repo.GetByUUID(ctx, uuid)
}

// List delegates to the repository.
func (s *PersonService) List(ctx context.Context, f repository.PersonListFilter) ([]models.Person, error) {
	return s.repo.List(ctx, f)
}

// Update applies the patch, re-validating custom_fields when present.
// Emails inside the patch are normalised via the repository's update
// path (which lowercases them on write).
//
// Update fires registered PersonUpdateListeners after the repo write
// succeeds. Listeners receive (before, after) snapshots so they can
// diff the change — Phase 3's auto-emission helpers compute tag-set
// deltas and emit corresponding marketing_activities rows.
func (s *PersonService) Update(ctx context.Context, uuid string, patch map[string]any) (*models.Person, error) {
	if cf, ok := patch["customFields"].(map[string]any); ok {
		if err := s.customFlds.Validate(ctx, models.CustomFieldTargetPersons, cf); err != nil {
			return nil, err
		}
	}
	if actor, ok := ctxauth.GetUserUUID(ctx); ok {
		patch["updatedBy"] = actor
	}

	before, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}

	if err := s.repo.Update(ctx, uuid, patch); err != nil {
		return nil, err
	}
	after, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	s.fireUpdateListeners(ctx, *before, *after)
	return after, nil
}

// fireUpdateListeners walks the slice with panic recovery. Listener
// failures never roll back the parent Update.
func (s *PersonService) fireUpdateListeners(ctx context.Context, before, after models.Person) {
	s.listenersMu.RLock()
	ls := make([]PersonUpdateListener, len(s.updateListeners))
	copy(ls, s.updateListeners)
	s.listenersMu.RUnlock()
	for _, l := range ls {
		func() {
			defer func() { _ = recover() }()
			l(ctx, before, after)
		}()
	}
}

// Delete hard-removes the person and cascades to every membership
// they hold. Activity (Phase 2+) is intentionally not cascaded —
// historical engagement stays queryable with the personUuid orphan.
// Cancellation of activity belongs in an explicit GDPR right-to-be-
// forgotten flow, not in a routine contact delete.
func (s *PersonService) Delete(ctx context.Context, uuid string) error {
	if err := s.mships.DeleteForPerson(ctx, uuid); err != nil {
		return err
	}
	return s.repo.Delete(ctx, uuid)
}
