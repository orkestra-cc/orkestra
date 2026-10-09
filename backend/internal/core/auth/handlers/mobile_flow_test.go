package handlers

// The two-step mobile flow (spec §4.10 D35): the backend issues the nonce
// and holds it against a client-committed PKCE challenge, so a token
// exfiltrated on its own (SDK logs, crash reports, a leaked debug build)
// is worthless without the verifier, and a replay finds no record.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	authutils "github.com/orkestra/backend/internal/core/auth/utils"
	"github.com/orkestra/backend/internal/shared/types"
)

const testVerifier = "v-0123456789012345678901234567890123456789012"

func challengeOf(verifier string) string { return authutils.GenerateCodeChallenge(verifier) }

func mobileCtx(platform string) context.Context {
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/google/mobile", nil)
	r.Host = callbackHost
	ctx := context.WithValue(r.Context(), "http_request", r)
	if platform != "" {
		ctx = context.WithValue(ctx, "deviceInfo", &types.DeviceInfo{DeviceID: "dev-1", Platform: platform, IP: "1.1.1.1"})
	}
	return ctx
}

func beginMobile(t *testing.T, h *AuthHandler, provider models.OAuthProvider, challenge string) string {
	t.Helper()
	req := &MobileAuthBeginRequest{}
	req.Body.CodeChallenge = challenge
	var (
		res *MobileAuthBeginResponse
		err error
	)
	switch provider {
	case models.OAuthProviderGoogle:
		res, err = h.HandleMobileGoogleBegin(mobileCtx(""), req)
	case models.OAuthProviderApple:
		res, err = h.HandleMobileAppleBegin(mobileCtx(""), req)
	default:
		t.Fatalf("no mobile begin for %s", provider)
	}
	if err != nil {
		t.Fatalf("begin %s: %v", provider, err)
	}
	if res.Body.Nonce == "" {
		t.Fatal("begin must answer a nonce")
	}
	return res.Body.Nonce
}

// idTokenWithNonce makes the harness provider's validator answer this
// nonce claim; the token string itself is opaque to the handler (the
// validators are tested in services/mobile_idtoken_test.go).
func idTokenWithNonce(hx *callbackHarness, nonce string) string {
	hx.provider.info.Nonce = nonce
	return "idt-" + nonce
}

func completeMobileWith(h *AuthHandler, provider models.OAuthProvider, platform, token, verifier string) (*MobileGoogleAuthResponse, error) {
	switch provider {
	case models.OAuthProviderApple:
		req := &MobileAppleAuthRequest{}
		req.Body.IDToken, req.Body.CodeVerifier = token, verifier
		return h.HandleMobileAppleAuth(mobileCtx(platform), req)
	default:
		req := &MobileGoogleAuthRequest{}
		req.Body.IDToken, req.Body.CodeVerifier = token, verifier
		return h.HandleMobileGoogleAuth(mobileCtx(platform), req)
	}
}

func completeMobile(h *AuthHandler, provider models.OAuthProvider, token, verifier string) (*MobileGoogleAuthResponse, error) {
	return completeMobileWith(h, provider, "", token, verifier)
}

func mobileStatusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var se huma.StatusError
	if errors.As(err, &se) {
		return se.GetStatus()
	}
	return http.StatusInternalServerError
}

func completeMobileStatus(h *AuthHandler, provider models.OAuthProvider, token, verifier string) int {
	_, err := completeMobile(h, provider, token, verifier)
	return mobileStatusOf(err)
}

func TestMobileFlow_HappyPathTakesTheRecordExactlyOnce(t *testing.T) {
	hx := newCallbackHarness(t)
	nonce := beginMobile(t, hx.operator, models.OAuthProviderGoogle, challengeOf(testVerifier))

	if _, err := completeMobile(hx.operator, models.OAuthProviderGoogle, idTokenWithNonce(hx, nonce), testVerifier); err != nil {
		t.Fatalf("completion: %v", err)
	}
	if hx.state.recordExists(nonce) {
		t.Fatal("the record must be consumed")
	}
}

func TestMobileFlow_ReplayIsRefused(t *testing.T) {
	hx := newCallbackHarness(t)
	nonce := beginMobile(t, hx.operator, models.OAuthProviderGoogle, challengeOf(testVerifier))
	token := idTokenWithNonce(hx, nonce)

	if _, err := completeMobile(hx.operator, models.OAuthProviderGoogle, token, testVerifier); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	if code := completeMobileStatus(hx.operator, models.OAuthProviderGoogle, token, testVerifier); code != http.StatusUnauthorized {
		t.Fatalf("second presentation = %d, want 401", code)
	}
}

// A stolen token WITHOUT the verifier is worthless — the residual this
// whole design exists to close.
func TestMobileFlow_WrongVerifierIsRefusedAndBurnsTheRecord(t *testing.T) {
	hx := newCallbackHarness(t)
	nonce := beginMobile(t, hx.operator, models.OAuthProviderGoogle, challengeOf(testVerifier))

	if code := completeMobileStatus(hx.operator, models.OAuthProviderGoogle, idTokenWithNonce(hx, nonce), "wrong-verifier"); code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", code)
	}
	if hx.state.recordExists(nonce) {
		t.Fatal("a failed completion must BURN the challenge, as the web relay does")
	}
}

func TestMobileFlow_RecordFromAnotherProviderIsRefused(t *testing.T) {
	hx := newCallbackHarness(t)
	nonce := beginMobile(t, hx.operator, models.OAuthProviderGoogle, challengeOf(testVerifier))

	if code := completeMobileStatus(hx.operator, models.OAuthProviderApple, idTokenWithNonce(hx, nonce), testVerifier); code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a cross-provider record", code)
	}
}

// The operator and client handlers share one store in the harness, as
// they share one Redis in production.
func TestMobileFlow_RecordFromAnotherTierIsRefused(t *testing.T) {
	hx := newCallbackHarness(t)
	nonce := beginMobile(t, hx.client, models.OAuthProviderGoogle, challengeOf(testVerifier))

	if code := completeMobileStatus(hx.operator, models.OAuthProviderGoogle, idTokenWithNonce(hx, nonce), testVerifier); code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a cross-tier record", code)
	}
}

func TestMobileFlow_ExpiredRecordIsRefused(t *testing.T) {
	hx := newCallbackHarness(t)
	nonce := beginMobile(t, hx.operator, models.OAuthProviderGoogle, challengeOf(testVerifier))
	hx.state.fastForward(11 * time.Minute)

	if code := completeMobileStatus(hx.operator, models.OAuthProviderGoogle, idTokenWithNonce(hx, nonce), testVerifier); code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", code)
	}
}

// GETDEL gives the record to exactly one racer — the
// TestMFAChallengeConsume_AllowsExactlyOneConcurrentWinner shape.
func TestMobileFlow_ConcurrentCompletionsHaveOneWinner(t *testing.T) {
	hx := newCallbackHarness(t)
	nonce := beginMobile(t, hx.operator, models.OAuthProviderGoogle, challengeOf(testVerifier))
	token := idTokenWithNonce(hx, nonce)

	var wins atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code := completeMobileStatus(hx.operator, models.OAuthProviderGoogle, token, testVerifier); code == http.StatusOK {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d winners, want exactly 1", wins.Load())
	}
}

// The client-supplied access_token is GONE — it was stored unverified.
func TestMobileFlow_NoAccessTokenIsAcceptedOrPersisted(t *testing.T) {
	for _, body := range []interface{}{MobileGoogleAuthRequest{}.Body, MobileAppleAuthRequest{}.Body} {
		if _, has := reflect.TypeOf(body).FieldByName("AccessToken"); has {
			t.Fatalf("%T still accepts an access_token", body)
		}
	}
	hx := newCallbackHarness(t)
	nonce := beginMobile(t, hx.operator, models.OAuthProviderGoogle, challengeOf(testVerifier))
	if _, err := completeMobile(hx.operator, models.OAuthProviderGoogle, idTokenWithNonce(hx, nonce), testVerifier); err != nil {
		t.Fatalf("completion: %v", err)
	}
	if hx.opAuth.lastTokens != nil {
		t.Fatal("no client-supplied access token may be persisted")
	}
}

// Every miss is ONE opaque 401 — the caller must not learn WHICH check
// failed.
func TestMobileFlow_EveryFailureIsTheSameAnswer(t *testing.T) {
	hx := newCallbackHarness(t)
	type miss struct {
		name string
		run  func() error
	}
	misses := []miss{
		{"no record", func() error {
			_, err := completeMobile(hx.operator, models.OAuthProviderGoogle, idTokenWithNonce(hx, "never-issued"), testVerifier)
			return err
		}},
		{"wrong verifier", func() error {
			nonce := beginMobile(t, hx.operator, models.OAuthProviderGoogle, challengeOf(testVerifier))
			_, err := completeMobile(hx.operator, models.OAuthProviderGoogle, idTokenWithNonce(hx, nonce), "wrong")
			return err
		}},
		{"wrong provider", func() error {
			nonce := beginMobile(t, hx.operator, models.OAuthProviderApple, challengeOf(testVerifier))
			_, err := completeMobile(hx.operator, models.OAuthProviderGoogle, idTokenWithNonce(hx, nonce), testVerifier)
			return err
		}},
		{"expired", func() error {
			nonce := beginMobile(t, hx.operator, models.OAuthProviderGoogle, challengeOf(testVerifier))
			hx.state.fastForward(11 * time.Minute)
			_, err := completeMobile(hx.operator, models.OAuthProviderGoogle, idTokenWithNonce(hx, nonce), testVerifier)
			hx.state.fastForward(-11 * time.Minute)
			return err
		}},
	}
	var seen []string
	for _, m := range misses {
		err := m.run()
		if mobileStatusOf(err) != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", m.name, mobileStatusOf(err))
		}
		seen = append(seen, err.Error())
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] != seen[0] {
			t.Fatalf("answers differ: %q vs %q", seen[0], seen[i])
		}
	}
}

// The Google handler hardcoded platform "android"; it must read
// deviceInfo.Platform like the Apple one.
func TestMobileFlow_GooglePlatformComesFromDeviceInfo(t *testing.T) {
	hx := newCallbackHarness(t)
	nonce := beginMobile(t, hx.operator, models.OAuthProviderGoogle, challengeOf(testVerifier))

	if _, err := completeMobileWith(hx.operator, models.OAuthProviderGoogle, "ios", idTokenWithNonce(hx, nonce), testVerifier); err != nil {
		t.Fatalf("completion: %v", err)
	}
	if hx.resolver.lastPlatform != "ios" {
		t.Fatalf("platform = %q, want the device-reported ios", hx.resolver.lastPlatform)
	}
}

// begin refuses a challenge that is not a plausible S256 value.
func TestMobileBegin_RejectsAnImplausibleChallenge(t *testing.T) {
	hx := newCallbackHarness(t)
	for _, bad := range []string{"", "short", "has spaces in it and is long enough to pass length", "++++++++++++++++++++++++++++++++++++++++++++"} {
		req := &MobileAuthBeginRequest{}
		req.Body.CodeChallenge = bad
		if _, err := hx.operator.HandleMobileGoogleBegin(mobileCtx(""), req); mobileStatusOf(err) != http.StatusBadRequest {
			t.Fatalf("challenge %q: status %d, want 400", bad, mobileStatusOf(err))
		}
	}
}

// Sign in with Apple carries the SHA-256 of the nonce as hex; the record
// key must not depend on the hex case the SDK happened to emit.
func TestMobileRecordKeyFor_AppleClaimIsCaseInsensitive(t *testing.T) {
	nonce := "n-abc"
	key := mobileNonceKey(nonce)
	claim := strings.TrimPrefix(key, services.MobileNonceKeyPrefix)
	if got := mobileRecordKeyFor(models.OAuthProviderApple, strings.ToUpper(claim)); got != key {
		t.Fatalf("upper-case claim → %q, want %q", got, key)
	}
	if got := mobileRecordKeyFor(models.OAuthProviderApple, claim); got != key {
		t.Fatalf("lower-case claim → %q, want %q", got, key)
	}
}
