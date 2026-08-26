package notification

import (
	"context"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/services"
)

// TestLegacyKeysAloneResolveToTheSynthesizedProfile pins ADR-0019 D6: with no
// routing map, the flat keys become one profile with LegacySlug routing "*",
// and its provider/host/port/from are exactly what the flat keys said.
func TestLegacyKeysAloneResolveToTheSynthesizedProfile(t *testing.T) {
	loader := func(context.Context) services.SenderConfig {
		return services.SenderConfig{Legacy: services.LegacyProfile(services.SenderProfile{
			Provider: "smtp", SMTPHost: "relay.internal", SMTPPort: 25, SMTPTLSMode: "none", FromAddress: "no-reply@example.com",
		})}
	}
	r := services.NewSenderResolver(loader)
	drivers := services.NewDriverRegistry(services.CoreDrivers(nil)...)

	for _, cat := range []string{"auth.verify_email", "auth.reset_password", "crm.campaign", "marketing"} {
		p, err := r.Resolve(context.Background(), services.ResolveInput{Category: cat})
		if err != nil || p.Slug != services.LegacySlug || p.Provider != "smtp" || p.SMTPHost != "relay.internal" {
			t.Fatalf("Resolve(%q) = %+v, %v", cat, p, err)
		}
		d, ok := drivers.Get(p.Provider)
		if !ok {
			t.Fatalf("smtp driver not registered")
		}
		// Anonymous relay: no credentials, and it validates — as isSMTPConfigured did.
		if err := services.ValidateProfile(d, p, services.RuntimeView); err != nil {
			t.Fatalf("anonymous relay must validate: %v", err)
		}
	}
}
