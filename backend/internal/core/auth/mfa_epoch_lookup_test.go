package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// epochProviderFake is one tier's user provider, reduced to the single
// method the epoch lookup is allowed to call. The embedded nil interface
// satisfies the rest at compile time and panics at run time, so a lookup
// that reached for anything else would be caught here rather than
// discovered in production.
type epochProviderFake struct {
	iface.UserProvider
	users map[string]int
	err   error
}

func (f *epochProviderFake) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	epoch, ok := f.users[id]
	if !ok {
		return nil, iface.ErrUserNotFound
	}
	return &iface.User{UUID: id, MFAEpoch: epoch}, nil
}

// The tier dispatch is the reason this constructor exists. main.go hands
// ONE AuthMiddleware to both host muxes, so a lookup built from a single
// user provider would fail to resolve every UUID of the other tier — and
// since the middleware reads a failed lookup as "not current", that would
// strip MFA authority from that whole tier's tokens on every request.
//
// The two tables answer DIFFERENT epochs for the SAME UUID, so a
// constructor that used one provider for both audiences cannot pass.
func TestNewMFAEpochLookup_DispatchesOnAudience(t *testing.T) {
	operator := &epochProviderFake{users: map[string]int{"shared-uuid": 7}}
	client := &epochProviderFake{users: map[string]int{"shared-uuid": 3}}
	lookup := newMFAEpochLookup(operator, client)

	for _, tc := range []struct {
		name     string
		audience string
		want     int
	}{
		{"operator audience reads the operator provider", "operator", 7},
		{"client audience reads the client provider", "client", 3},
		// An empty or unrecognised audience falls back to operator —
		// today's canonical tier — so a legacy single-aud token keeps
		// working. Mirrors the MFA-enrollment lookup's rule.
		{"empty audience falls back to operator", "", 7},
		{"unknown audience falls back to operator", "sidecar", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := lookup(context.Background(), tc.audience, "shared-uuid")
			if err != nil {
				t.Fatalf("lookup: %v", err)
			}
			if got != tc.want {
				t.Fatalf("epoch = %d, want %d", got, tc.want)
			}
		})
	}
}

// A user the tier's provider cannot produce is an ERROR, never epoch 0.
// Zero is the value a token with no "mfae" claim carries, so answering 0
// on a miss would turn a failed lookup into a matching epoch — a pass.
func TestNewMFAEpochLookup_MissingUserIsAnErrorNotZero(t *testing.T) {
	lookup := newMFAEpochLookup(&epochProviderFake{users: map[string]int{}}, &epochProviderFake{})

	epoch, err := lookup(context.Background(), "operator", "ghost")
	if err == nil {
		t.Fatalf("a missing user must be an error; got epoch %d, nil", epoch)
	}
	if epoch != 0 {
		t.Fatalf("epoch = %d on the error path, want the zero value", epoch)
	}
}

// A store outage propagates as an error so the middleware fails closed.
func TestNewMFAEpochLookup_ProviderErrorPropagates(t *testing.T) {
	boom := errors.New("mongo down")
	lookup := newMFAEpochLookup(&epochProviderFake{err: boom}, &epochProviderFake{})

	if _, err := lookup(context.Background(), "operator", "u-1"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the provider's own error", err)
	}
}

// A nil provider for a tier is a wiring failure, and a wiring failure must
// not read as "epoch 0, all good".
func TestNewMFAEpochLookup_NilProviderIsAnError(t *testing.T) {
	lookup := newMFAEpochLookup(&epochProviderFake{users: map[string]int{"u-1": 1}}, nil)

	if _, err := lookup(context.Background(), "client", "u-1"); err == nil {
		t.Fatal("an unwired client provider must fail closed, not answer 0")
	}
}
