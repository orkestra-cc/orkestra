package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// fakeSuppressionList answers LookupSuppressions from a fixed set and counts
// the lookups. The write methods are not used by the auth module.
type fakeSuppressionList struct {
	mu        sync.Mutex
	addresses map[string]bool
	err       error
	lookups   int
}

func (f *fakeSuppressionList) LookupSuppressions(_ context.Context, addresses []string) (map[string]iface.EmailSuppression, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]iface.EmailSuppression{}
	for _, a := range addresses {
		key := strings.ToLower(strings.TrimSpace(a))
		if f.addresses[key] {
			out[key] = iface.EmailSuppression{Address: key, Reason: iface.SuppressionReasonManual}
		}
	}
	return out, nil
}
func (f *fakeSuppressionList) SuppressAddresses(context.Context, []string, iface.SuppressionEvidence) ([]string, error) {
	panic("not used")
}
func (f *fakeSuppressionList) LiftSuppressions(context.Context, []string, iface.SuppressionEvidence) ([]string, error) {
	panic("not used")
}
func (f *fakeSuppressionList) ListSuppressions(context.Context, iface.SuppressionListQuery) ([]iface.EmailSuppression, error) {
	panic("not used")
}

func (f *fakeSuppressionList) lookupCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lookups
}

func withSuppressions(list iface.EmailSuppressionManager) gatesOption {
	return func(c *PasswordAuthConfig) { c.Suppressions = list }
}

// TestAdminSends_RefuseASuppressedAddress: the three operator-initiated
// sends answer ErrRecipientSuppressed for an address on the suppression list
// (whatever its casing), mint no token and send nothing; an address off the
// list goes out as before.
func TestAdminSends_RefuseASuppressedAddress(t *testing.T) {
	actions := []struct {
		name       string
		unverified bool
		run        func(*PasswordAuthService, string) error
	}{
		{"password reset", false, func(s *PasswordAuthService, id string) error {
			return s.AdminTriggerPasswordReset(context.Background(), id)
		}},
		{"resend verification", true, func(s *PasswordAuthService, id string) error {
			return s.AdminResendVerification(context.Background(), id)
		}},
		{"invite", false, func(s *PasswordAuthService, id string) error {
			return s.AdminSendInvite(context.Background(), id, "Operator")
		}},
	}
	for _, a := range actions {
		t.Run(a.name, func(t *testing.T) {
			list := &fakeSuppressionList{addresses: map[string]bool{"blocked@example.test": true}}
			env := newGatesEnv(t, PolicyAudienceOperator, nil, nil, withSuppressions(list))
			blocked := env.hashedUser("Blocked@Example.test", correctTestPassword)
			blocked.EmailVerified = !a.unverified

			if err := a.run(env.auth, blocked.UUID); !errors.Is(err, iface.ErrRecipientSuppressed) {
				t.Fatalf("want ErrRecipientSuppressed, got %v", err)
			}
			if n := len(env.emailTokens.created); n != 0 {
				t.Fatalf("no token may be minted for a suppressed address, got %d", n)
			}
			if n := len(env.notifier.sends); n != 0 {
				t.Fatalf("nothing may be sent to a suppressed address, got %d", n)
			}

			ok := env.hashedUser("fine@example.test", correctTestPassword)
			ok.EmailVerified = !a.unverified
			if err := a.run(env.auth, ok.UUID); err != nil {
				t.Fatalf("an address off the list must still be sent to, got %v", err)
			}
			if n := len(env.notifier.sends); n != 1 {
				t.Fatalf("want one send for the address off the list, got %d", n)
			}
		})
	}
}

// TestAdminSends_FailClosedWhenTheListIsUnreadable: a lookup error is
// ErrSuppressionListUnavailable, and nothing is minted or sent.
func TestAdminSends_FailClosedWhenTheListIsUnreadable(t *testing.T) {
	list := &fakeSuppressionList{err: errors.New("mongo down")}
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil, withSuppressions(list))
	u := env.hashedUser("someone@example.test", correctTestPassword)
	if err := env.auth.AdminTriggerPasswordReset(context.Background(), u.UUID); !errors.Is(err, iface.ErrSuppressionListUnavailable) {
		t.Fatalf("want ErrSuppressionListUnavailable, got %v", err)
	}
	if len(env.emailTokens.created) != 0 || len(env.notifier.sends) != 0 {
		t.Fatalf("nothing may be minted or sent: tokens=%d sends=%d", len(env.emailTokens.created), len(env.notifier.sends))
	}
}

// TestAdminSends_NoListWired keeps the pre-existing behaviour when the
// notification module registered no suppression list.
func TestAdminSends_NoListWired(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	u := env.hashedUser("someone@example.test", correctTestPassword)
	if err := env.auth.AdminTriggerPasswordReset(context.Background(), u.UUID); err != nil {
		t.Fatalf("want success, got %v", err)
	}
}

// TestPublicFlows_DoNotConsultTheList pins the anti-enumeration rule: the
// public forgot-password and resend-verification flows answer the same for a
// suppressed address as for any other, and never read the list (the
// notification module suppresses their send on its own).
func TestPublicFlows_DoNotConsultTheList(t *testing.T) {
	list := &fakeSuppressionList{addresses: map[string]bool{"blocked@example.test": true}}
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil, withSuppressions(list))
	u := env.hashedUser("blocked@example.test", correctTestPassword)
	if err := env.auth.ForgotPassword(context.Background(), u.Email, ""); err != nil {
		t.Fatalf("forgot password must not surface the suppression, got %v", err)
	}
	u.EmailVerified = false
	if err := env.auth.ResendVerification(context.Background(), u.Email, ""); err != nil {
		t.Fatalf("resend verification must not surface the suppression, got %v", err)
	}
	if n := list.lookupCount(); n != 0 {
		t.Fatalf("the public flows must not read the suppression list, got %d lookups", n)
	}
}
