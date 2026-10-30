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
	// prefRepo holds the user's per-module preferences (user_preferences);
	// exported alongside the profile, deleted on purge in both modes — they
	// carry no referential role, so no tombstone is kept. nil-tolerant.
	prefRepo repository.PreferenceRepository
}

// NewPIIProducer returns a PIIProducer bound to the user repository and the
// preference repository (nil allowed: no preferences exported or purged).
func NewPIIProducer(userRepo repository.UserRepository, prefRepo repository.PreferenceRepository) iface.PIIProducer {
	return &piiProducer{userRepo: userRepo, prefRepo: prefRepo}
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
	out := map[string]any{
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
	}
	if p.prefRepo != nil {
		prefs, err := p.prefRepo.ListByUser(ctx, userUUID)
		if err != nil {
			return nil, err
		}
		byModule := map[string]map[string]string{}
		for _, d := range prefs {
			byModule[d.Module] = d.Values
		}
		out["preferences"] = byModule
	}
	return out, nil
}

// purgePreferences deletes the user's preference rows; both erase modes
// remove them outright (nothing references them).
func (p *piiProducer) purgePreferences(ctx context.Context, userUUID string, res *iface.PurgeResult) error {
	if p.prefRepo == nil {
		return nil
	}
	n, err := p.prefRepo.DeleteByUser(ctx, userUUID)
	if err != nil {
		return err
	}
	if n > 0 {
		res.RowsDeleted += int(n)
		res.Collections = append(res.Collections, repository.UserPreferencesCollection)
	}
	return nil
}

// PurgePersonalData erases the user identity row. The user row is the one
// place anonymize is meaningfully different from hard-delete: anonymize keeps
// the UUID (so foreign references stay valid) while aliasing the email and
// blanking the profile — the canonical tombstone the retention job later
// hard-deletes. Hard-delete removes the row outright.
func (p *piiProducer) PurgePersonalData(ctx context.Context, userUUID string, mode iface.EraseMode) (iface.PurgeResult, error) {
	var res iface.PurgeResult
	if err := p.purgePreferences(ctx, userUUID, &res); err != nil {
		return iface.PurgeResult{}, err
	}
	if mode == iface.EraseAnonymize {
		if err := p.userRepo.SoftDeleteAndAliasEmail(ctx, userUUID); err != nil {
			if stderrors.Is(err, repository.ErrUserNotFound) {
				return res, nil
			}
			return iface.PurgeResult{}, err
		}
		res.RowsAnonymized++
		res.Collections = append(res.Collections, "users")
		return res, nil
	}
	if err := p.userRepo.HardDelete(ctx, userUUID); err != nil {
		if stderrors.Is(err, repository.ErrUserNotFound) {
			return res, nil
		}
		return iface.PurgeResult{}, err
	}
	res.RowsDeleted++
	res.Collections = append(res.Collections, "users")
	return res, nil
}
