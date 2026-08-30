package services

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// ProviderStructuralFields are the effective (fallback-resolved) values the
// web OAuth flow needs for one provider. Secrets travel as PRESENCE only —
// the predicate never sees a secret value, so it can be logged, tested and
// reused by the PR 3 validator without a secret crossing any boundary.
type ProviderStructuralFields struct {
	ClientID    string
	RedirectURL string
	// SecretPresent is the client secret for google/github/discord and the
	// inline PEM (applePrivateKey) for apple.
	SecretPresent bool
	// Apple only.
	TeamID         string
	KeyID          string
	PrivateKeyPath string
}

// KeyFileProbe reports whether path names a readable regular file with
// non-empty content. Injected so the pure predicate is testable without a
// filesystem; ReadableNonEmptyFile is the production probe. A nil probe
// never counts a path.
type KeyFileProbe func(path string) bool

// ReadableNonEmptyFile is the production KeyFileProbe (spec §4.4: "a
// path-backed Apple key counts only when the path identifies a readable
// regular file with non-empty content"). PEM validity is operational
// validation, not structure.
func ReadableNonEmptyFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 1)
	n, err := f.Read(buf)
	return n == 1 && (err == nil || err == io.EOF)
}

// ProviderStructurallyConfigured is the single pure predicate behind
// "structurally configured" (spec §4.4): every field the web flow needs is
// present. It returns the schema key of the FIRST missing field so a WARN or
// a validation error can name it. Credential correctness, PEM validity and
// IdP reachability are outside it by design.
//
//	structural(p) := clientId ≠ "" ∧ redirectURL ≠ "" ∧ secrets(p)
//	secrets(google|github|discord) := clientSecret present
//	secrets(apple) := teamId ≠ "" ∧ keyId ≠ "" ∧ (inline key present ∨ readable key file)
func ProviderStructurallyConfigured(p models.OAuthProvider, f ProviderStructuralFields, probe KeyFileProbe) (missing string, ok bool) {
	prefix := string(p)
	switch p {
	case models.OAuthProviderGoogle, models.OAuthProviderGitHub, models.OAuthProviderDiscord, models.OAuthProviderApple:
	default:
		return "provider", false
	}
	if f.ClientID == "" {
		return prefix + "ClientId", false
	}
	if f.RedirectURL == "" {
		return prefix + "RedirectURL", false
	}
	if p != models.OAuthProviderApple {
		if !f.SecretPresent {
			return prefix + "ClientSecret", false
		}
		return "", true
	}
	if f.TeamID == "" {
		return "appleTeamId", false
	}
	if f.KeyID == "" {
		return "appleKeyId", false
	}
	if !f.SecretPresent && (probe == nil || !probe(f.PrivateKeyPath)) {
		return "applePrivateKey", false
	}
	return "", true
}

// WebProviderOrder is the order GET /v1/auth/{tier}/providers advertises.
var WebProviderOrder = []models.OAuthProvider{
	models.OAuthProviderGoogle,
	models.OAuthProviderApple,
	models.OAuthProviderGitHub,
	models.OAuthProviderDiscord,
}

// OAuthResolver is the resolver surface AuthHandler consumes. The concrete
// *OAuthConfigResolver satisfies it; tests inject a fake.
type OAuthResolver interface {
	Get(ctx context.Context, p models.OAuthProvider) (*OAuthProviderConfig, bool)
	RedirectURL(ctx context.Context, p models.OAuthProvider) string
	MobileAudience(ctx context.Context, p models.OAuthProvider, platform string) string
	ConfiguredProviders(ctx context.Context) []models.OAuthProvider
	// OAuthWebProviderUsable resolves ONE provider for ONE surface from ONE
	// config read. (cfg, true, nil) is usable and cfg is what the provider
	// is built from; (nil, false, nil) is a per-provider defect (toggle
	// off/absent/malformed, structural field missing) — already WARNed by
	// key; a non-nil error is a document-level outage (missing document,
	// repository error, undecryptable secret) and maps to 503.
	OAuthWebProviderUsable(ctx context.Context, audience PolicyAudience, p models.OAuthProvider) (*OAuthProviderConfig, bool, error)
	// UsableWebProviders is OAuthWebProviderUsable over WebProviderOrder from
	// a single read, returning the usable ones in canonical order.
	UsableWebProviders(ctx context.Context, audience PolicyAudience) ([]models.OAuthProvider, error)
}

var _ OAuthResolver = (*OAuthConfigResolver)(nil)

// activeConfigReader is the slice of ModuleConfigService the resolver
// depends on. The two legacy accessors serve Get/RedirectURL/MobileAudience
// (the mobile path); the strict web path reads only through
// ActiveConfigRequiredModule.
type activeConfigReader interface {
	GetValue(ctx context.Context, moduleName, key string) string
	GetSecret(ctx context.Context, moduleName, key string) string
	ActiveConfigRequiredModule(ctx context.Context, name string) (*module.ActiveConfigView, error)
}

// OAuthWebProviderUsable implements OAuthResolver.
func (r *OAuthConfigResolver) OAuthWebProviderUsable(ctx context.Context, audience PolicyAudience, p models.OAuthProvider) (*OAuthProviderConfig, bool, error) {
	view, err := r.activeView(ctx)
	if err != nil {
		return nil, false, err
	}
	return r.usableFromView(view, audience, p)
}

// UsableWebProviders implements OAuthResolver.
func (r *OAuthConfigResolver) UsableWebProviders(ctx context.Context, audience PolicyAudience) ([]models.OAuthProvider, error) {
	view, err := r.activeView(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.OAuthProvider, 0, len(WebProviderOrder))
	for _, p := range WebProviderOrder {
		if _, ok, _ := r.usableFromView(view, audience, p); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *OAuthConfigResolver) activeView(ctx context.Context) (*module.ActiveConfigView, error) {
	if r == nil || r.cs == nil {
		return nil, fmt.Errorf("%w: oauth config resolver not wired", ErrAuthPolicyUnavailable)
	}
	view, err := r.cs.ActiveConfigRequiredModule(ctx, "auth")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAuthPolicyUnavailable, err)
	}
	return view, nil
}

// usableFromView is the per-provider decision over an already-read view:
// strict toggle (absent → schema default false; malformed → unusable +
// WARN naming the key), then the structural predicate over the same view,
// then the provider config built from that same view.
func (r *OAuthConfigResolver) usableFromView(view *module.ActiveConfigView, audience PolicyAudience, p models.OAuthProvider) (*OAuthProviderConfig, bool, error) {
	key, known := providerToggleKey(audience, string(p))
	if !known {
		return nil, false, nil
	}
	on := false
	if raw, present := view.Raw(key); present {
		v, err := strictBool(raw)
		if err != nil {
			r.log().Warn("oauth provider toggle is not a canonical boolean; provider treated as unusable",
				slog.String("provider", string(p)), slog.String("key", key))
			return nil, false, nil
		}
		on = v
	}
	if !on {
		return nil, false, nil
	}
	fields := ProviderStructuralFields{
		ClientID:       view.Effective(string(p) + "ClientId"),
		RedirectURL:    view.Effective(string(p) + "RedirectURL"),
		SecretPresent:  view.SecretPresent(string(p) + "ClientSecret"),
		TeamID:         view.Effective("appleTeamId"),
		KeyID:          view.Effective("appleKeyId"),
		PrivateKeyPath: view.Effective("applePrivateKeyPath"),
	}
	if p == models.OAuthProviderApple {
		fields.SecretPresent = view.SecretPresent("applePrivateKey")
	}
	if missing, ok := ProviderStructurallyConfigured(p, fields, r.probe); !ok {
		r.log().Warn("oauth provider enabled but structurally incomplete; omitted",
			slog.String("provider", string(p)), slog.String("missing", missing))
		return nil, false, nil
	}
	cfg, _ := providerConfigFrom(p, view.Effective, view.Secret)
	return cfg, true, nil
}

func (r *OAuthConfigResolver) log() *slog.Logger {
	if r != nil && r.logger != nil {
		return r.logger
	}
	return slog.Default()
}
