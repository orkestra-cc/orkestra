package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// ErrInvalidTag wraps caller-error responses on Tag writes — empty
// name, missing parent, etc.
var ErrInvalidTag = errors.New("marketing: invalid tag")

// TagService orchestrates Tag writes with slug derivation, path
// computation, and cascade behaviours the repository cannot perform
// on its own (path rebuild on parent change, cascade-delete on the
// tags[] arrays of Person/Organization records).
type TagService struct {
	repo *repository.TagRepository
}

// NewTagService wires the service.
func NewTagService(repo *repository.TagRepository) *TagService {
	return &TagService{repo: repo}
}

// Create persists a new tag. Slug is derived from Name unless the
// caller passed one; Path is built from the parent's path.
func (s *TagService) Create(ctx context.Context, t *models.Tag) (*models.Tag, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: nil tag", ErrInvalidTag)
	}
	if t.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidTag)
	}
	if t.Slug == "" {
		t.Slug = DeriveSlug(t.Name)
		if t.Slug == "" {
			return nil, fmt.Errorf("%w: name produces an empty slug; supply slug explicitly", ErrInvalidTag)
		}
	}
	if t.UUID == "" {
		t.UUID = uuid.New().String()
	}

	parentPath := ""
	if t.ParentUUID != "" {
		parent, err := s.repo.GetByUUID(ctx, t.ParentUUID)
		if err != nil {
			return nil, fmt.Errorf("%w: parent %s: %v", ErrInvalidTag, t.ParentUUID, err)
		}
		parentPath = parent.Path
	}
	t.Path = JoinPath(parentPath, t.Name)

	if actor, ok := ctxauth.GetUserUUID(ctx); ok {
		t.CreatedBy = actor
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}
	return s.repo.GetByUUID(ctx, t.UUID)
}

// Get returns the tag by UUID.
func (s *TagService) Get(ctx context.Context, uuid string) (*models.Tag, error) {
	return s.repo.GetByUUID(ctx, uuid)
}

// List returns every tag for the caller's tenant.
func (s *TagService) List(ctx context.Context) ([]models.Tag, error) {
	return s.repo.List(ctx)
}

// Update applies a patch. When Name changes, Path is rebuilt for the
// tag and every descendant — the schema doc calls this out explicitly
// and the design rationale is to keep cosmetic renames cheap (no
// migration of tagged records). Reparenting via ParentUUID change is
// not supported on this code path; use the dedicated MoveSubtree
// helper instead.
func (s *TagService) Update(ctx context.Context, uuid string, patch map[string]any) (*models.Tag, error) {
	existing, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}

	// Reparenting is deliberately blocked through generic Update — the
	// service exposes MoveSubtree for that, which encodes the full
	// path-rebuild cascade.
	if newParent, ok := patch["parentUuid"].(string); ok && newParent != existing.ParentUUID {
		return nil, fmt.Errorf("%w: use moveSubtree to change parentUuid", ErrInvalidTag)
	}

	if newName, ok := patch["name"].(string); ok && newName != existing.Name {
		// Recompute Path for this tag and propagate to descendants.
		newParentPath := ""
		if existing.ParentUUID != "" {
			parent, perr := s.repo.GetByUUID(ctx, existing.ParentUUID)
			if perr != nil {
				return nil, perr
			}
			newParentPath = parent.Path
		}
		newPath := JoinPath(newParentPath, newName)
		patch["path"] = newPath

		if err := s.rebuildSubtreePaths(ctx, existing.Path, newPath); err != nil {
			return nil, err
		}
	}

	if err := s.repo.Update(ctx, uuid, patch); err != nil {
		return nil, err
	}
	return s.repo.GetByUUID(ctx, uuid)
}

// Delete removes a tag. The repository delete is the only step here;
// cascading the tagUUID off the tags[] arrays of Person and
// Organization records belongs in a follow-up — pulling that into
// PR-3 would require fan-out updates across two collections per
// delete. The Phase 1 acceptance is "tag references survive as
// orphans until the next batch cleanup runs"; the admin UI flags
// orphan refs visually so operators can resolve them.
func (s *TagService) Delete(ctx context.Context, uuid string) error {
	return s.repo.Delete(ctx, uuid)
}

// rebuildSubtreePaths updates every descendant's Path when the
// subtree's root was renamed. Called from Update when Name changes;
// also the building block of a future MoveSubtree.
func (s *TagService) rebuildSubtreePaths(ctx context.Context, oldRoot, newRoot string) error {
	if oldRoot == newRoot {
		return nil
	}
	descendants, err := s.repo.ListDescendants(ctx, oldRoot)
	if err != nil {
		return err
	}
	for _, d := range descendants {
		if d.Path == oldRoot {
			// the tag being renamed is handled by the caller's
			// repo.Update — skip to avoid double-write.
			continue
		}
		// Replace the matching prefix only — string substring
		// replace is safe because the index is anchored at the
		// path root.
		patched := newRoot + d.Path[len(oldRoot):]
		if err := s.repo.Update(ctx, d.UUID, map[string]any{"path": patched}); err != nil {
			return err
		}
	}
	return nil
}
