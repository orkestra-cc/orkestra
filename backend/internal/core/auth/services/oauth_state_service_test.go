package services

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
)

const stateTestEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newStateServiceForTest(t *testing.T) OAuthStateService {
	t.Helper()
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", stateTestEncryptionKey)
	return NewOAuthStateService(NewMemoryOAuthStateStore())
}

func TestValidateOAuthState_IsOneShot(t *testing.T) {
	svc := newStateServiceForTest(t)
	ctx := context.Background()
	if _, err := svc.StoreOAuthState(ctx, &StoreOAuthStateRequest{Provider: models.OAuthProviderGoogle, Tier: AudienceClient, State: "nonce-1"}); err != nil {
		t.Fatal(err)
	}
	first, err := svc.ValidateOAuthState(ctx, "nonce-1")
	if err != nil || first == nil || first.Provider != models.OAuthProviderGoogle {
		t.Fatalf("first validation: %+v %v", first, err)
	}
	if _, err := svc.ValidateOAuthState(ctx, "nonce-1"); err == nil {
		t.Fatal("a replayed state must be refused — the first validation consumed it")
	}
}

func TestValidateOAuthState_ConcurrentPresentationsHaveOneWinner(t *testing.T) {
	svc := newStateServiceForTest(t)
	ctx := context.Background()
	if _, err := svc.StoreOAuthState(ctx, &StoreOAuthStateRequest{Provider: models.OAuthProviderGoogle, State: "nonce-race"}); err != nil {
		t.Fatal(err)
	}
	const n = 64
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := svc.ValidateOAuthState(ctx, "nonce-race"); err == nil {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("winners = %d, want exactly 1 (Get-then-Delete would let several through)", wins.Load())
	}
}

func TestValidateOAuthState_ExpiredIsRefused(t *testing.T) {
	svc := newStateServiceForTest(t)
	ctx := context.Background()
	if _, err := svc.StoreOAuthState(ctx, &StoreOAuthStateRequest{Provider: models.OAuthProviderGoogle, State: "nonce-old", ExpiryDuration: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := svc.ValidateOAuthState(ctx, "nonce-old"); err == nil {
		t.Fatal("an expired state must be refused")
	}
}

func TestOAuthRelay_RoundTripIsEncryptedAndOneShot(t *testing.T) {
	svc := newStateServiceForTest(t)
	store := NewMemoryOAuthStateStore()
	svc = NewOAuthStateService(store)
	ctx := context.Background()
	rec := &OAuthRelayRecord{
		Tier: AudienceClient, Provider: models.OAuthProviderGitHub, CSRF: "nonce-9",
		UserInfo:        map[string]interface{}{"email": "u@example.com", "provider_id": "gh-1", "email_verified": true, "name": "U"},
		Tokens:          &models.OAuthProviderTokens{AccessToken: "idp-at", TokenType: "Bearer"},
		SecurityContext: &models.SecurityContext{IPAddress: "203.0.113.5"},
		DeviceInfo:      &models.DeviceInfo{DeviceID: "dev-1"},
	}
	id, err := svc.StoreOAuthRelay(ctx, rec)
	if err != nil || len(id) < 32 {
		t.Fatalf("id=%q err=%v", id, err)
	}
	raw, err := store.Get(ctx, "oauth:relay:"+id)
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"u@example.com", "idp-at", "nonce-9", "gh-1"} {
		if string(raw) != "" && containsBytes(raw, plain) {
			t.Fatalf("relay record stored in clear: contains %q", plain)
		}
	}
	got, err := svc.TakeOAuthRelay(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != AudienceClient || got.Provider != models.OAuthProviderGitHub || got.CSRF != "nonce-9" ||
		got.UserInfo["email"] != "u@example.com" || got.UserInfo["email_verified"] != true ||
		got.Tokens == nil || got.Tokens.AccessToken != "idp-at" || got.DeviceInfo.DeviceID != "dev-1" || got.SecurityContext.IPAddress != "203.0.113.5" {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if _, err := svc.TakeOAuthRelay(ctx, id); err == nil {
		t.Fatal("a relay id is single-use")
	}
	if _, err := svc.TakeOAuthRelay(ctx, "never-stored"); err == nil {
		t.Fatal("an unknown relay id must be refused")
	}
}

func TestOAuthRelay_FailureRecordRoundTrips(t *testing.T) {
	svc := newStateServiceForTest(t)
	ctx := context.Background()
	id, err := svc.StoreOAuthRelay(ctx, &OAuthRelayRecord{Tier: AudienceClient, Provider: models.OAuthProviderGoogle, CSRF: "n", FailureCode: "oauth_access_denied"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.TakeOAuthRelay(ctx, id)
	if err != nil || got.FailureCode != "oauth_access_denied" || got.UserInfo != nil || got.Tokens != nil {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestOAuthRelay_ExpiresWithTTL(t *testing.T) {
	if OAuthRelayTTL != 60*time.Second {
		t.Fatalf("OAuthRelayTTL = %v, want 60s (spec §4.10)", OAuthRelayTTL)
	}
	svc := newStateServiceForTest(t)
	ctx := context.Background()
	id, err := svc.StoreOAuthRelay(ctx, &OAuthRelayRecord{Tier: AudienceClient, Provider: models.OAuthProviderGoogle, CSRF: "n", ExpiresAt: time.Now().Add(-time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TakeOAuthRelay(ctx, id); err == nil {
		t.Fatal("a record past its own ExpiresAt must be refused even if the store still holds it")
	}
}

func containsBytes(b []byte, s string) bool {
	return len(s) > 0 && len(b) >= len(s) && func() bool {
		for i := 0; i+len(s) <= len(b); i++ {
			if string(b[i:i+len(s)]) == s {
				return true
			}
		}
		return false
	}()
}
