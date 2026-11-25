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

type erroringRevocation struct{ fakeSessionRevocation }

func (e *erroringRevocation) IsRevoked(context.Context, string) (bool, error) {
	return false, context.DeadlineExceeded
}

func TestActiveSessions_ConsultsRevocationSet(t *testing.T) {
	svc, repo, _, rev := newSessionsSvc(t)
	repo.seed(&authModels.AuthSessionDoc{UUID: "a", UserUUID: "u", IsActive: true, ExpiresAt: time.Now().Add(time.Hour)})
	repo.seed(&authModels.AuthSessionDoc{UUID: "b", UserUUID: "u", IsActive: true, ExpiresAt: time.Now().Add(time.Hour)})
	_ = rev.Revoke(context.Background(), "b", "logout")

	got, err := svc.ActiveSessions(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !got["a"] || got["b"] {
		t.Fatalf("revoked sid must be false even when its doc is active: %v", got)
	}
}

func TestActiveSessions_RedisErrorIsIgnored(t *testing.T) {
	repo := newFakeAuthSessionRepo()
	repo.seed(&authModels.AuthSessionDoc{UUID: "a", UserUUID: "u", IsActive: true, ExpiresAt: time.Now().Add(time.Hour)})
	svc := &authService{authSessionRepo: repo, sessionRevocation: &erroringRevocation{}}
	got, err := svc.ActiveSessions(context.Background(), []string{"a"})
	if err != nil || !got["a"] {
		t.Fatalf("doc stays authoritative on Redis error: %v %v", got, err)
	}
}

func TestTerminateCurrentSession_FlipsDocAndActiveSessionsFalse(t *testing.T) {
	svc, repo, _, rev := newSessionsSvc(t)
	repo.seed(&authModels.AuthSessionDoc{UUID: "s1", UserUUID: "u", IsActive: true, ExpiresAt: time.Now().Add(time.Hour)})
	repo.seed(&authModels.AuthSessionDoc{UUID: "s2", UserUUID: "other", IsActive: true, ExpiresAt: time.Now().Add(time.Hour)})

	if err := svc.TerminateCurrentSession(context.Background(), "u", "s1"); err != nil {
		t.Fatal(err)
	}
	// another user's session is untouched
	if err := svc.TerminateCurrentSession(context.Background(), "u", "s2"); err != nil {
		t.Fatal(err)
	}
	// unknown session is a no-op
	if err := svc.TerminateCurrentSession(context.Background(), "u", "ghost"); err != nil {
		t.Fatal(err)
	}
	d, _ := repo.GetByUUID(context.Background(), "s1")
	if d.IsActive {
		t.Fatal("session doc must be inactive")
	}
	d2, _ := repo.GetByUUID(context.Background(), "s2")
	if !d2.IsActive {
		t.Fatal("foreign session must stay active")
	}
	if !contains(rev.revokedList(), "s1") {
		t.Fatal("sid must be pushed to the revocation set")
	}
	// bypass the Redis check to prove the doc alone reports inactive
	bare := &authService{authSessionRepo: repo}
	got, _ := bare.ActiveSessions(context.Background(), []string{"s1", "s2"})
	if got["s1"] || !got["s2"] {
		t.Fatalf("ActiveSessions = %v", got)
	}
}
