package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// passwordProviderFake is one tier's user provider, reduced to the single
// method the password-set lookup may call (the embedded nil interface
// panics on anything else).
type passwordProviderFake struct {
	iface.UserProvider
	hashes map[string]string
	err    error
}

func (f *passwordProviderFake) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	hash, ok := f.hashes[id]
	if !ok {
		return nil, iface.ErrUserNotFound
	}
	return &iface.User{UUID: id, PasswordHash: hash}, nil
}

// One AuthMiddleware serves both host muxes, so the lookup dispatches on
// the token's audience like the MFA-enrollment and epoch lookups. The two
// tiers answer differently for the same UUID, so a constructor that used
// one provider for both cannot pass.
func TestNewPasswordSetLookup_DispatchesOnAudience(t *testing.T) {
	operator := &passwordProviderFake{hashes: map[string]string{"shared-uuid": "$argon2id$x"}}
	client := &passwordProviderFake{hashes: map[string]string{"shared-uuid": ""}}
	lookup := newPasswordSetLookup(operator, client)

	for _, tc := range []struct {
		audience string
		want     bool
	}{
		{"operator", true},
		{"client", false},
		{"", true},
		{"sidecar", true},
	} {
		got, err := lookup(context.Background(), tc.audience, "shared-uuid")
		if err != nil || got != tc.want {
			t.Fatalf("audience %q: hasPassword = %v, %v; want %v", tc.audience, got, err, tc.want)
		}
	}
}

// A store outage, a missing user and a missing provider are errors, never
// "no password": the middleware keeps the reconfirm on offer for an error.
func TestNewPasswordSetLookup_FailuresAreErrors(t *testing.T) {
	boom := errors.New("mongo down")
	if _, err := newPasswordSetLookup(&passwordProviderFake{err: boom}, nil)(context.Background(), "operator", "u-1"); !errors.Is(err, boom) {
		t.Fatalf("provider error = %v, want it propagated", err)
	}
	if _, err := newPasswordSetLookup(&passwordProviderFake{hashes: map[string]string{}}, nil)(context.Background(), "operator", "ghost"); err == nil {
		t.Fatal("a missing user must be an error")
	}
	if _, err := newPasswordSetLookup(&passwordProviderFake{}, nil)(context.Background(), "client", "u-1"); err == nil {
		t.Fatal("a missing provider must be an error")
	}
}
