package services

import (
	"context"
	stderrors "errors"

	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// piiProducer implements iface.PIIProducer for the user module. Exports
// profile fields (email, name, role, OAuth links) and hard-deletes the
// user row on purge. Password hash and PIN are deliberately omitted from
// export — they are server secrets, not personal data the subject can
// meaningfully consume or port elsewhere.
type piiProducer struct {
	userRepo repository.UserRepository
}

// NewPIIProducer returns a PIIProducer bound to the user repository.
func NewPIIProducer(userRepo repository.UserRepository) iface.PIIProducer {
	return &piiProducer{userRepo: userRepo}
}

// Subject is the stable bundle-key identifier for this producer.
func (p *piiProducer) Subject() string { return "user" }

// ExportPersonalData returns the user's profile projection. Returning
// (nil, nil) when the user is already deleted keeps the bundle tidy.
func (p *piiProducer) ExportPersonalData(ctx context.Context, userUUID string) (any, error) {
	user, err := p.userRepo.GetByID(ctx, userUUID)
	if err != nil {
		if stderrors.Is(err, repository.ErrUserNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return map[string]any{
		"uuid":          user.UUID,
		"email":         user.Email,
		"username":      user.Username,
		"fullName":      user.FullName,
		"phone":         user.Phone,
		"avatar":        user.Avatar,
		"role":          user.Role,
		"emailVerified": user.EmailVerified,
		"isActive":      user.IsActive,
		"createdAt":     user.CreatedAt,
		"updatedAt":     user.UpdatedAt,
		"lastLogin":     user.LastLogin,
		"oauthLinks":    user.OAuthLinks,
	}, nil
}

// PurgePersonalData erases the user identity row. The user row is the one
// place anonymize is meaningfully different from hard-delete: anonymize keeps
// the UUID (so foreign references stay valid) while aliasing the email and
// blanking the profile — the canonical tombstone the retention job later
// hard-deletes. Hard-delete removes the row outright.
func (p *piiProducer) PurgePersonalData(ctx context.Context, userUUID string, mode iface.EraseMode) (iface.PurgeResult, error) {
	if mode == iface.EraseAnonymize {
		if err := p.userRepo.SoftDeleteAndAliasEmail(ctx, userUUID); err != nil {
			if stderrors.Is(err, repository.ErrUserNotFound) {
				return iface.PurgeResult{}, nil
			}
			return iface.PurgeResult{}, err
		}
		return iface.PurgeResult{RowsAnonymized: 1, Collections: []string{"users"}}, nil
	}
	if err := p.userRepo.HardDelete(ctx, userUUID); err != nil {
		if stderrors.Is(err, repository.ErrUserNotFound) {
			return iface.PurgeResult{}, nil
		}
		return iface.PurgeResult{}, err
	}
	return iface.PurgeResult{RowsDeleted: 1, Collections: []string{"users"}}, nil
}
