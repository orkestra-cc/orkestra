package services

import "context"

// ResolveInput identifies one send for routing. TenantID is reserved (D4)
// and ignored today.
type ResolveInput struct {
	Category string
	Type     string
	TenantID string
}

// SenderConfig is the resolver's view of configuration at one instant:
// the decoded roster (empty until PR 2 reads email.senders) and the profile
// synthesized from the flat legacy keys (D6). Err marks a roster that could
// not be read at all.
type SenderConfig struct {
	Profiles []SenderProfile
	Legacy   SenderProfile
	Err      error
}

// SenderConfigLoader supplies the current configuration at send time so
// admin UI changes take effect without a restart.
type SenderConfigLoader func(ctx context.Context) SenderConfig

// SenderResolver picks the profile that carries one send.
type SenderResolver interface {
	// Resolve returns the most specific matching profile, or
	// ErrNoSenderForCategory when nothing matches (fail-closed, D5).
	Resolve(ctx context.Context, in ResolveInput) (SenderProfile, error)
	// Default returns the profile declaring "*" — what IsConfigured reports on.
	Default(ctx context.Context) (SenderProfile, error)
}

type senderResolver struct{ load SenderConfigLoader }

func NewSenderResolver(load SenderConfigLoader) SenderResolver {
	return &senderResolver{load: load}
}

// PR 1: the roster is never populated, so every send resolves to the legacy
// profile. PR 2 adds pattern matching over cfg.Profiles.
func (r *senderResolver) Resolve(ctx context.Context, _ ResolveInput) (SenderProfile, error) {
	cfg := r.load(ctx)
	if cfg.Err != nil {
		return SenderProfile{}, ErrSenderConfigUnavailable
	}
	return cfg.Legacy, nil
}

func (r *senderResolver) Default(ctx context.Context) (SenderProfile, error) {
	cfg := r.load(ctx)
	if cfg.Err != nil {
		return SenderProfile{}, ErrSenderConfigUnavailable
	}
	return cfg.Legacy, nil
}
