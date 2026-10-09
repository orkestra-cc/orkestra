package services

// CreateUserFromOAuth ignored input.UUID, which is what broke the pairing
// between the first-admin sentinel and the account it names: the auth
// module claims the sentinel with the uuid it is ABOUT to create, and a
// rollback Release deletes only a matching uuid. Spec §4.7 D30.

import (
	"context"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestCreateUserFromOAuth_HonoursTheSuppliedUUID(t *testing.T) {
	svc := NewUserService(newFakeUserRepo(), &fakeOAuthProviderRepo{})
	want := "01890000-0000-7000-8000-000000000001"
	u, err := svc.CreateUserFromOAuth(context.Background(), &iface.CreateUserInput{
		UUID: want, Email: "a@example.com", FullName: "A", Role: "guest", EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("CreateUserFromOAuth: %v", err)
	}
	if u.UUID != want {
		t.Fatalf("UUID = %q, want the supplied %q", u.UUID, want)
	}
}

func TestCreateUserFromOAuth_GeneratesAUUIDWhenNoneSupplied(t *testing.T) {
	svc := NewUserService(newFakeUserRepo(), &fakeOAuthProviderRepo{})
	u, err := svc.CreateUserFromOAuth(context.Background(), &iface.CreateUserInput{
		Email: "b@example.com", FullName: "B", Role: "guest", EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("CreateUserFromOAuth: %v", err)
	}
	if u.UUID == "" {
		t.Fatal("an empty input UUID must still produce one")
	}
}
