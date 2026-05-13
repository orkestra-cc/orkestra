package services

import (
	"context"
	stderrors "errors"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra/backend/internal/core/user/repository"
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

// PurgePersonalData hard-deletes the user row. The DSR service is the
// only caller — it runs producers in order, and the audit sink records
// the pre-erase export before any producer runs.
func (p *piiProducer) PurgePersonalData(ctx context.Context, userUUID string) (iface.PurgeResult, error) {
	err := p.userRepo.HardDelete(ctx, userUUID)
	if err != nil {
		if stderrors.Is(err, repository.ErrUserNotFound) {
			return iface.PurgeResult{}, nil
		}
		return iface.PurgeResult{}, err
	}
	return iface.PurgeResult{RowsDeleted: 1, Collections: []string{"users"}}, nil
}
