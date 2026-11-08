package services

import (
	"context"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Both tier services are registered in the ServiceRegistry as
// services.AuthService; consumers (notification push) assert the optional
// iface.SessionActivityReader on the registered value.
func TestActiveSessionsBothTiers(t *testing.T) {
	for _, tier := range []string{authModels.TierOperator, authModels.TierClient} {
		t.Run(tier, func(t *testing.T) {
			repo := newFakeAuthSessionRepo()
			repo.seed(&authModels.AuthSessionDoc{UUID: "live", UserUUID: "u", IsActive: true, ExpiresAt: time.Now().Add(time.Hour)})
			repo.seed(&authModels.AuthSessionDoc{UUID: "off", UserUUID: "u", IsActive: false, ExpiresAt: time.Now().Add(time.Hour)})
			repo.seed(&authModels.AuthSessionDoc{UUID: "old", UserUUID: "u", IsActive: true, ExpiresAt: time.Now().Add(-time.Hour)})

			var registered AuthService = &authService{authSessionRepo: repo}
			reader, ok := registered.(iface.SessionActivityReader)
			if !ok {
				t.Fatal("registered auth service must satisfy iface.SessionActivityReader")
			}
			got, err := reader.ActiveSessions(context.Background(), []string{"live", "off", "old", "ghost"})
			if err != nil {
				t.Fatal(err)
			}
			if !got["live"] || got["off"] || got["old"] || got["ghost"] {
				t.Fatalf("got %v", got)
			}
			if m, err := reader.ActiveSessions(context.Background(), nil); err != nil || len(m) != 0 {
				t.Fatalf("empty input: %v %v", m, err)
			}
		})
	}
}
