package services

import (
	"context"
	"strings"
)

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
	// BySlug returns one profile by its slug — the explicit-sender test send.
	BySlug(ctx context.Context, slug string) (SenderProfile, error)
}

// hasRoutingMap reports whether any profile declares a pattern. This — not
// roster emptiness — is the D6 cutover: until some profile routes, the flat
// keys carry every send, so creating a first draft (or removing the last
// pattern) never strands mail. It is the same predicate ValidateSenderConfig
// uses to decide whether the routing rules apply.
func hasRoutingMap(profiles []SenderProfile) bool {
	for _, p := range profiles {
		if len(p.Categories) > 0 {
			return true
		}
	}
	return false
}

type senderResolver struct{ load SenderConfigLoader }

func NewSenderResolver(load SenderConfigLoader) SenderResolver {
	return &senderResolver{load: load}
}

func (r *senderResolver) Resolve(ctx context.Context, in ResolveInput) (SenderProfile, error) {
	cfg := r.load(ctx)
	if cfg.Err != nil {
		return SenderProfile{}, ErrSenderConfigUnavailable
	}
	if !hasRoutingMap(cfg.Profiles) {
		// Legacy mode never inspects the category: today an empty Category
		// is legal and sends, and D6 promises byte-identical behaviour
		// until a profile routes. The malformed-category rule below is a
		// property of routing, and starts with the routing map.
		return cfg.Legacy, nil
	}
	// Lowercased for comparison (patterns are lowercased on read) — never
	// repaired: an empty category, or one carrying leading/trailing
	// whitespace, is malformed and fails closed. Without this, "*" would
	// match "" and " marketing " would ride the default sender.
	if in.Category == "" || strings.TrimSpace(in.Category) != in.Category {
		return SenderProfile{}, ErrNoSenderForCategory
	}
	category := strings.ToLower(in.Category)
	best := -1
	var found SenderProfile
	for _, p := range cfg.Profiles {
		for _, pat := range p.Categories {
			if ok, lit := MatchPattern(pat, category); ok && lit > best {
				best, found = lit, p
			}
		}
	}
	if best < 0 {
		return SenderProfile{}, ErrNoSenderForCategory
	}
	return found, nil
}

func (r *senderResolver) Default(ctx context.Context) (SenderProfile, error) {
	cfg := r.load(ctx)
	if cfg.Err != nil {
		return SenderProfile{}, ErrSenderConfigUnavailable
	}
	if !hasRoutingMap(cfg.Profiles) {
		return cfg.Legacy, nil
	}
	for _, p := range cfg.Profiles {
		for _, pat := range p.Categories {
			if pat == "*" {
				return p, nil
			}
		}
	}
	return SenderProfile{}, ErrNoSenderForCategory
}

// BySlug sees drafts — that is how the test send proves a profile before
// any pattern routes traffic to it. The legacy slug resolves only while the
// legacy profile is the one carrying mail.
func (r *senderResolver) BySlug(ctx context.Context, slug string) (SenderProfile, error) {
	cfg := r.load(ctx)
	if cfg.Err != nil {
		return SenderProfile{}, ErrSenderConfigUnavailable
	}
	for _, p := range cfg.Profiles {
		if p.Slug == slug {
			return p, nil
		}
	}
	if slug == cfg.Legacy.Slug && !hasRoutingMap(cfg.Profiles) {
		return cfg.Legacy, nil
	}
	return SenderProfile{}, ErrSenderNotFound
}
