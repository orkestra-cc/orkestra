// Phase 4 — Card lifecycle.
//
// CardTypeService is the orchestration layer over CardTypeRepository:
// it parses + caches the code-format AST at create/update time,
// enforces the structural invariants captured in
// docs/plans/marketing-addon/schemas/marketing_card_types.md, and
// rejects deletion when cards still reference the type. Handlers
// call this service; no handler talks to the repository directly.

package services

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"go.mongodb.org/mongo-driver/bson"
)

// ErrCardTypeInUse signals that DeleteCardType refused to proceed
// because at least one card record still references the type. The
// schema's note on deletion makes this rule explicit; the operator
// path is to set `active = false` instead.
var ErrCardTypeInUse = errors.New("marketing: card type still has cards")

// CardTypeService is the public surface for card-type CRUD.
type CardTypeService struct {
	repo     *repository.CardTypeRepository
	cardRepo *repository.CardRepository

	// astCache holds the parsed CodeFormat AST keyed by CardType UUID.
	// Populated on the first Get / List that decodes a type and
	// invalidated on Update / Delete. The cache is process-local — a
	// rolling restart picks up the latest format on the next request.
	astCache   map[string]*CardCodeFormatAST
	astCacheMu sync.RWMutex
}

// NewCardTypeService wires the service against the two repositories.
// The cardRepo dependency is needed so DeleteCardType can check
// CountByType before allowing a deletion.
func NewCardTypeService(repo *repository.CardTypeRepository, cardRepo *repository.CardRepository) *CardTypeService {
	return &CardTypeService{
		repo:     repo,
		cardRepo: cardRepo,
		astCache: make(map[string]*CardCodeFormatAST),
	}
}

// Create validates the structural invariants + parses the CodeFormat
// before inserting. The handler-facing error surface is intentionally
// thin: model.Validate returns the human-readable field message and
// ParseCardCodeFormat returns a positioned message — both are good
// enough for the admin UI to surface directly via the `detail` field.
//
// UUID + CreatedBy are stamped here (handlers do not pre-fill them).
// Active defaults to true unless the caller passes false.
func (s *CardTypeService) Create(ctx context.Context, t *models.CardType) (*models.CardType, error) {
	if t == nil {
		return nil, errors.New("marketing: nil card type")
	}
	if t.UUID == "" {
		t.UUID = uuid.New().String()
	}
	t.Key = strings.ToLower(strings.TrimSpace(t.Key))
	t.DisplayName = strings.TrimSpace(t.DisplayName)
	t.CodeFormat = strings.TrimSpace(t.CodeFormat)

	// Default Active=true on creation. Operators can toggle later.
	t.Active = true

	if err := t.Validate(); err != nil {
		return nil, err
	}

	ast, err := ParseCardCodeFormat(t.CodeFormat)
	if err != nil {
		return nil, err
	}

	if actor, ok := ctxauth.GetUserUUID(ctx); ok {
		t.CreatedBy = actor
		t.UpdatedBy = actor
	}

	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}

	s.astCacheMu.Lock()
	s.astCache[t.UUID] = ast
	s.astCacheMu.Unlock()

	return t, nil
}

// Get returns the type by UUID + ensures its CodeFormat AST is cached
// for the next Issue call.
func (s *CardTypeService) Get(ctx context.Context, uuidStr string) (*models.CardType, error) {
	t, err := s.repo.GetByUUID(ctx, uuidStr)
	if err != nil {
		return nil, err
	}
	s.warmAST(t)
	return t, nil
}

// GetByKey returns the type by slug key — used by external integrations
// that reference types by name rather than UUID.
func (s *CardTypeService) GetByKey(ctx context.Context, key string) (*models.CardType, error) {
	t, err := s.repo.GetByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	s.warmAST(t)
	return t, nil
}

// List returns every type matching the filter.
func (s *CardTypeService) List(ctx context.Context, f repository.CardTypeFilter) ([]models.CardType, error) {
	out, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, err
	}
	for i := range out {
		s.warmAST(&out[i])
	}
	return out, nil
}

// Update applies a partial patch. Only DisplayName, Description,
// Tiers, CodeFormat, DefaultBenefits, AllowMultiplePerPerson, and
// Active are settable — Key, UUID, TenantID, and the audit timestamps
// are not patchable through this surface (renaming Key would silently
// break programmatic references).
//
// If CodeFormat changes, it is parsed before write and the AST cache
// is invalidated for this type so the next Issue picks up the new
// template.
func (s *CardTypeService) Update(ctx context.Context, uuidStr string, patch UpdateCardTypePatch) (*models.CardType, error) {
	existing, err := s.repo.GetByUUID(ctx, uuidStr)
	if err != nil {
		return nil, err
	}

	bsonPatch := bson.M{}
	if patch.DisplayName != nil {
		v := strings.TrimSpace(*patch.DisplayName)
		if v == "" {
			return nil, errors.Join(models.ErrInvalidCardType, errors.New("displayName cannot be blanked"))
		}
		bsonPatch["displayName"] = v
		existing.DisplayName = v
	}
	if patch.Description != nil {
		bsonPatch["description"] = *patch.Description
	}
	if patch.Tiers != nil {
		for _, t := range *patch.Tiers {
			if strings.TrimSpace(t) == "" {
				return nil, errors.Join(models.ErrInvalidCardType, errors.New("empty tier in tiers"))
			}
		}
		bsonPatch["tiers"] = *patch.Tiers
	}
	if patch.CodeFormat != nil {
		v := strings.TrimSpace(*patch.CodeFormat)
		ast, perr := ParseCardCodeFormat(v)
		if perr != nil {
			return nil, perr
		}
		bsonPatch["codeFormat"] = v
		existing.CodeFormat = v
		// Refresh AST cache immediately so a concurrent Issue picks
		// up the new template.
		s.astCacheMu.Lock()
		s.astCache[uuidStr] = ast
		s.astCacheMu.Unlock()
	}
	if patch.DefaultBenefits != nil {
		bsonPatch["defaultBenefits"] = *patch.DefaultBenefits
	}
	if patch.AllowMultiplePerPerson != nil {
		bsonPatch["allowMultiplePerPerson"] = *patch.AllowMultiplePerPerson
	}
	if patch.Active != nil {
		bsonPatch["active"] = *patch.Active
	}

	if len(bsonPatch) == 0 {
		// No-op patch; return the existing record unchanged.
		return existing, nil
	}

	if actor, ok := ctxauth.GetUserUUID(ctx); ok {
		bsonPatch["updatedBy"] = actor
	}

	if err := s.repo.Update(ctx, uuidStr, bsonPatch); err != nil {
		return nil, err
	}
	return s.repo.GetByUUID(ctx, uuidStr)
}

// Delete hard-deletes a card type. Refused with ErrCardTypeInUse
// when any cards of the type exist (active or not). Operators who
// want to retire a type without deleting cards should patch
// `active = false` instead.
func (s *CardTypeService) Delete(ctx context.Context, uuidStr string) error {
	count, err := s.cardRepo.CountByType(ctx, uuidStr)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrCardTypeInUse
	}
	if err := s.repo.Delete(ctx, uuidStr); err != nil {
		return err
	}

	s.astCacheMu.Lock()
	delete(s.astCache, uuidStr)
	s.astCacheMu.Unlock()

	return nil
}

// CodeFormatAST returns the cached AST for a card type, lazily
// populating the cache on the first call. Used by CardService.Issue
// — the AST is parsed once per type per process and reused for
// every emit.
func (s *CardTypeService) CodeFormatAST(ctx context.Context, cardTypeUUID string) (*CardCodeFormatAST, error) {
	s.astCacheMu.RLock()
	if ast, ok := s.astCache[cardTypeUUID]; ok {
		s.astCacheMu.RUnlock()
		return ast, nil
	}
	s.astCacheMu.RUnlock()

	t, err := s.repo.GetByUUID(ctx, cardTypeUUID)
	if err != nil {
		return nil, err
	}
	ast, perr := ParseCardCodeFormat(t.CodeFormat)
	if perr != nil {
		// A stored type with a malformed CodeFormat means someone
		// bypassed Create/Update — surface loudly rather than masking.
		return nil, perr
	}
	s.astCacheMu.Lock()
	s.astCache[cardTypeUUID] = ast
	s.astCacheMu.Unlock()
	return ast, nil
}

// warmAST best-effort populates the AST cache from a fetched type.
// Errors are ignored — the next CodeFormatAST call will surface them
// at the right callsite.
func (s *CardTypeService) warmAST(t *models.CardType) {
	if t == nil {
		return
	}
	s.astCacheMu.RLock()
	_, cached := s.astCache[t.UUID]
	s.astCacheMu.RUnlock()
	if cached {
		return
	}
	ast, err := ParseCardCodeFormat(t.CodeFormat)
	if err != nil {
		return
	}
	s.astCacheMu.Lock()
	s.astCache[t.UUID] = ast
	s.astCacheMu.Unlock()
}

// UpdateCardTypePatch is the partial-update DTO consumed by Update.
// Each pointer field denotes "set this if non-nil". Slice fields use
// pointer-to-slice so explicit clearing ([]) is distinguishable from
// no-change (nil).
type UpdateCardTypePatch struct {
	DisplayName            *string
	Description            *string
	Tiers                  *[]string
	CodeFormat             *string
	DefaultBenefits        *[]string
	AllowMultiplePerPerson *bool
	Active                 *bool
}
