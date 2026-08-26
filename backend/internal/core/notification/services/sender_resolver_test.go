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
