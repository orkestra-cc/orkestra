package services

import (
	"context"
	"errors"
	"testing"
)

func fixedLoader(cfg SenderConfig) SenderConfigLoader {
	return func(context.Context) SenderConfig { return cfg }
}

// TestSenderResolver_EmptyRosterReturnsLegacy: with no routing map the
// category is not inspected at all — "" included, which is legal today
// (D6 byte-identical). The malformed-category rule belongs to routing.
func TestSenderResolver_EmptyRosterReturnsLegacy(t *testing.T) {
	legacy := LegacyProfile(SenderProfile{Provider: "smtp", SMTPHost: "h", SMTPPort: 25, FromAddress: "f"})
	r := NewSenderResolver(fixedLoader(SenderConfig{Legacy: legacy}))

	for _, cat := range []string{"auth.verify_email", "crm.campaign", "marketing", "", " padded "} {
		got, err := r.Resolve(context.Background(), ResolveInput{Category: cat, Type: "transactional", TenantID: "t-1"})
		if err != nil || got.Slug != LegacySlug || got.Provider != "smtp" {
			t.Fatalf("Resolve(%q) = %+v, %v", cat, got, err)
		}
	}
	got, err := r.Default(context.Background())
	if err != nil || got.Slug != LegacySlug {
		t.Fatalf("Default = %+v, %v", got, err)
	}
}

func TestSenderResolver_ConfigErrorFailsClosed(t *testing.T) {
	r := NewSenderResolver(fixedLoader(SenderConfig{Err: errors.New("mongo down"), Legacy: LegacyProfile(SenderProfile{})}))
	if _, err := r.Resolve(context.Background(), ResolveInput{Category: "auth.verify_email"}); !errors.Is(err, ErrSenderConfigUnavailable) {
		t.Fatalf("want ErrSenderConfigUnavailable, got %v", err)
	}
	if _, err := r.Default(context.Background()); !errors.Is(err, ErrSenderConfigUnavailable) {
		t.Fatalf("want ErrSenderConfigUnavailable, got %v", err)
	}
}

func rosterCfg(profiles ...SenderProfile) SenderConfig {
	return SenderConfig{Profiles: profiles, Legacy: LegacyProfile(SenderProfile{Provider: "noop"})}
}

func TestSenderResolver_MostSpecificWins(t *testing.T) {
	r := NewSenderResolver(fixedLoader(rosterCfg(
		SenderProfile{Slug: "default", Provider: "noop", Categories: []string{"*"}},
		SenderProfile{Slug: "auth", Provider: "smtp", Categories: []string{"auth.*"}},
		SenderProfile{Slug: "auth-x", Provider: "smtp", Categories: []string{"auth.x"}},
		SenderProfile{Slug: "oauth", Provider: "smtp", Categories: []string{"auth.oauth.*"}},
		SenderProfile{Slug: "mkt", Provider: "smtp", Categories: []string{"marketing"}},
		SenderProfile{Slug: "draft", Provider: "ses"}, // no patterns: never selected
	)))
	cases := map[string]string{
		"auth.x":            "auth-x", // exact (6) beats prefix (5)
		"AUTH.X":            "auth-x", // category lowercased
		"auth.y":            "auth",
		"auth.oauth.google": "oauth",   // auth.oauth.* (11) beats auth.* (5)
		"auth":              "default", // auth.* never matches the bare token
		"marketing":         "mkt",
		"marketing.promo":   "default", // exact "marketing" does not match deeper
		"crm.campaign":      "default",
	}
	for cat, want := range cases {
		got, err := r.Resolve(context.Background(), ResolveInput{Category: cat})
		if err != nil || got.Slug != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", cat, got.Slug, err, want)
		}
	}
}

// TestSenderResolver_MalformedCategoryFailsClosed: with a "*" profile present,
// an empty or untrimmed category must NOT ride the default.
func TestSenderResolver_MalformedCategoryFailsClosed(t *testing.T) {
	r := NewSenderResolver(fixedLoader(rosterCfg(
		SenderProfile{Slug: "default", Provider: "noop", Categories: []string{"*"}},
		SenderProfile{Slug: "mkt", Provider: "noop", Categories: []string{"marketing"}},
	)))
	for _, cat := range []string{"", " marketing", "marketing ", "\tmarketing", " "} {
		if p, err := r.Resolve(context.Background(), ResolveInput{Category: cat}); !errors.Is(err, ErrNoSenderForCategory) {
			t.Errorf("Resolve(%q) = %+v, %v; want ErrNoSenderForCategory", cat, p, err)
		}
	}
	if p, err := r.Resolve(context.Background(), ResolveInput{Category: "MARKETING"}); err != nil || p.Slug != "mkt" {
		t.Fatalf("case is normalized, whitespace is not: %+v, %v", p, err)
	}
	// The rule starts with the routing map: an all-draft roster still sends "" through the legacy profile.
	drafts := rosterCfg(SenderProfile{Slug: "a", Provider: "noop"})
	if p, err := NewSenderResolver(fixedLoader(drafts)).Resolve(context.Background(), ResolveInput{Category: ""}); err != nil || p.Slug != LegacySlug {
		t.Fatalf("legacy mode must not inspect the category: %+v, %v", p, err)
	}
}

func TestSenderResolver_NoMatchFailsClosed(t *testing.T) {
	r := NewSenderResolver(fixedLoader(rosterCfg(
		SenderProfile{Slug: "auth", Provider: "smtp", Categories: []string{"auth.*"}},
	)))
	if _, err := r.Resolve(context.Background(), ResolveInput{Category: "crm.campaign"}); !errors.Is(err, ErrNoSenderForCategory) {
		t.Fatalf("want ErrNoSenderForCategory, got %v", err)
	}
	if _, err := r.Default(context.Background()); !errors.Is(err, ErrNoSenderForCategory) {
		t.Fatalf("Default without a * profile must fail closed, got %v", err)
	}
}

// TestSenderResolver_DraftsOnlyKeepsLegacy pins the D6 cutover: the flat
// keys carry every send until some profile declares a pattern. Creating the
// first (pattern-less) profile — or removing the last pattern — must not
// strand mail; a draft is still reachable by slug for the test send.
func TestSenderResolver_DraftsOnlyKeepsLegacy(t *testing.T) {
	drafts := rosterCfg(
		SenderProfile{Slug: "a", Provider: "smtp"},
		SenderProfile{Slug: "b", Provider: "sendgrid"},
	)
	drafts.Legacy = LegacyProfile(SenderProfile{Provider: "smtp", SMTPHost: "relay", SMTPPort: 25, FromAddress: "f@x"})
	r := NewSenderResolver(fixedLoader(drafts))
	for _, cat := range []string{"auth.verify_email", "crm.campaign"} {
		if p, err := r.Resolve(context.Background(), ResolveInput{Category: cat}); err != nil || p.Slug != LegacySlug {
			t.Fatalf("Resolve(%q) with an all-draft roster = %+v, %v; want the legacy profile", cat, p, err)
		}
	}
	if p, err := r.Default(context.Background()); err != nil || p.Slug != LegacySlug {
		t.Fatalf("Default = %+v, %v", p, err)
	}
	if p, err := r.BySlug(context.Background(), "a"); err != nil || p.Provider != "smtp" {
		t.Fatalf("a draft must be reachable by slug for the test send: %+v, %v", p, err)
	}
	if p, err := r.BySlug(context.Background(), LegacySlug); err != nil || p.Slug != LegacySlug {
		t.Fatalf("the legacy slug resolves while it is what carries mail: %+v, %v", p, err)
	}

	// Once one profile routes, the legacy profile is out — including by slug.
	live := rosterCfg(SenderProfile{Slug: "a", Provider: "noop", Categories: []string{"*"}})
	rl := NewSenderResolver(fixedLoader(live))
	if p, _ := rl.Resolve(context.Background(), ResolveInput{Category: "x"}); p.Slug != "a" {
		t.Fatalf("routing map present: got %+v", p)
	}
	if _, err := rl.BySlug(context.Background(), LegacySlug); !errors.Is(err, ErrSenderNotFound) {
		t.Fatalf("legacy slug must not resolve once a routing map exists, got %v", err)
	}
}

func TestSenderResolver_DefaultAndBySlug(t *testing.T) {
	roster := rosterCfg(
		SenderProfile{Slug: "auth", Provider: "smtp", Categories: []string{"auth.*"}},
		SenderProfile{Slug: "fallback", Provider: "noop", Categories: []string{"*"}},
	)
	r := NewSenderResolver(fixedLoader(roster))
	if p, err := r.Default(context.Background()); err != nil || p.Slug != "fallback" {
		t.Fatalf("Default = %+v, %v", p, err)
	}
	if p, err := r.BySlug(context.Background(), "auth"); err != nil || p.Provider != "smtp" {
		t.Fatalf("BySlug = %+v, %v", p, err)
	}
	if _, err := r.BySlug(context.Background(), "nope"); !errors.Is(err, ErrSenderNotFound) {
		t.Fatalf("want ErrSenderNotFound, got %v", err)
	}

	// Empty roster: the legacy slug resolves and nothing else does.
	legacyOnly := NewSenderResolver(fixedLoader(SenderConfig{Legacy: LegacyProfile(SenderProfile{})}))
	if p, err := legacyOnly.BySlug(context.Background(), LegacySlug); err != nil || p.Slug != LegacySlug {
		t.Fatalf("legacy BySlug = %+v, %v", p, err)
	}
	if _, err := legacyOnly.BySlug(context.Background(), "auth"); !errors.Is(err, ErrSenderNotFound) {
		t.Fatalf("want ErrSenderNotFound on an empty roster, got %v", err)
	}
}
