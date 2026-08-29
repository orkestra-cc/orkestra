package services

import (
	"context"
	"testing"
	"time"
)

// The three-level chain is admin accessTokenTTL → JWT_ACCESS_TOKEN_EXPIRY
// → 15m. Its middle level was unreachable because the policy substituted
// the 15m default for "unset", so the fall-through in accessTokenLifetime
// could never fire. The absence of this test is why the regression went
// unnoticed. ADR-0017 D5.
func TestAccessTokenTTL_Unset_ReturnsZero(t *testing.T) {
	for _, raw := range []string{"", "   ", "forever", "0s", "-5m"} {
		p := newPolicy(map[string]string{"accessTokenTTL": raw})
		if got := p.AccessTokenTTL(context.Background()); got != 0 {
			t.Errorf("accessTokenTTL=%q → %v, want 0 (unset, so the env level can be consulted)", raw, got)
		}
	}
	if got := newPolicy(nil).AccessTokenTTL(context.Background()); got != 0 {
		t.Errorf("absent key → %v, want 0", got)
	}
}

func TestAccessTokenLifetime_FallsBackToEnvWhenPolicyUnset(t *testing.T) {
	priv := testRSAKey()
	const envTTL = 42 * time.Minute
	svc := NewJWTService(priv, &priv.PublicKey, "test", envTTL, 7*24*time.Hour)

	// No policy wired at all.
	if got := svc.AccessTokenTTL(context.Background()); got != envTTL {
		t.Errorf("no policy: %v, want the env-derived %v", got, envTTL)
	}

	// Policy wired but the key is unset — the env value must still win.
	svc.SetPolicy(newPolicy(map[string]string{"accessTokenTTL": ""}))
	if got := svc.AccessTokenTTL(context.Background()); got != envTTL {
		t.Errorf("policy unset: %v, want the env-derived %v", got, envTTL)
	}

	// Policy set — it wins over the env value.
	svc.SetPolicy(newPolicy(map[string]string{"accessTokenTTL": "5m"}))
	if got := svc.AccessTokenTTL(context.Background()); got != 5*time.Minute {
		t.Errorf("policy set: %v, want 5m", got)
	}
}

func TestAccessTokenTTL_EnvironmentAndConstructorClampToMaximum(t *testing.T) {
	priv := testRSAKey()
	svc := NewJWTService(priv, &priv.PublicKey, "test", 48*time.Hour, 7*24*time.Hour)
	if got := svc.AccessTokenTTL(context.Background()); got != MaxAccessTokenTTL {
		t.Errorf("48h constructor value = %v, want it clamped to %v so the denylist window still covers it", got, MaxAccessTokenTTL)
	}
}

// TestNewJWTService_AccessTTLClampedIntoRange pins the constructor's own
// clamping — as distinct from the persisted-value clamping covered by
// TestAccessTokenTTL_PersistedOutOfRangeIsClamped above. NewJWTService is
// fed directly by JWT_ACCESS_TOKEN_EXPIRY and by any other direct caller,
// so an accessTTL below MinAccessTokenTTL (e.g. the 20s example from
// #317) must be raised, not left to mint a token the SPA's proactive
// refresh would rotate on every request (ADR-0020).
func TestNewJWTService_AccessTTLClampedIntoRange(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"below floor is raised to the floor", 20 * time.Second, MinAccessTokenTTL},
		{"exactly at the floor is unchanged", MinAccessTokenTTL, MinAccessTokenTTL},
		{"comfortably above the floor is unchanged", 15 * time.Minute, 15 * time.Minute},
		{"above the ceiling is still clamped to the ceiling", 48 * time.Hour, MaxAccessTokenTTL},
		{"zero falls back to the 15m default", 0, defaultAccessTokenTTL},
		{"negative falls back to the 15m default", -5 * time.Minute, defaultAccessTokenTTL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			priv := testRSAKey()
			svc := NewJWTService(priv, &priv.PublicKey, "test", tc.in, 7*24*time.Hour)
			if got := svc.AccessTokenTTL(context.Background()); got != tc.want {
				t.Errorf("NewJWTService(accessTTL=%v).AccessTokenTTL() = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestAccessTokenTTL_PersistedOutOfRangeIsClamped(t *testing.T) {
	if got := newPolicy(map[string]string{"accessTokenTTL": "9999h"}).AccessTokenTTL(context.Background()); got != MaxAccessTokenTTL {
		t.Errorf("legacy 9999h = %v, want %v", got, MaxAccessTokenTTL)
	}
	if got := newPolicy(map[string]string{"accessTokenTTL": "10s"}).AccessTokenTTL(context.Background()); got != MinAccessTokenTTL {
		t.Errorf("legacy 10s = %v, want %v", got, MinAccessTokenTTL)
	}
}
