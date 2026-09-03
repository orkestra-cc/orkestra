package handlers

// Spec v18 / plan review round 1. The four in-rotation sites are unreachable
// from the browser's /refresh-cookie under an outage: RefreshTokensHTTP
// classifies every cookie through PeekRefreshToken FIRST, and a picker that
// swallowed the lookup error left the handler synthesising a 401 — the one
// status the client treats as the end of the session. These tests drive the
// real handlers, because a test that drives the service cannot see the bug.
//
// The fake EMBEDS services.AuthService with a nil value: every method the
// handler is not supposed to reach panics, which is the assertion that
// nothing else was reached.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
)

type outagePeekAuthService struct {
	services.AuthService // nil: anything not overridden panics
	// peekErr answers every candidate identically; peekByValue, when set,
	// answers per cookie value so a test can mix an unreadable candidate with
	// a readable rotated sibling — the shape that used to fire replay.
	peekErr     error
	peekByValue map[string]peekRow
	// mintErr, when set, replaces the default terminal ErrInvalidRefreshToken
	// that MintAccessTokenFromRefresh returns. It is what produces the
	// Peek-OK → Mint-fail shape: the picker classifies the cookie and the
	// store goes away on the mint's OWN reads, which is a different site from
	// the picker's and answers a different status.
	mintErr      error
	rotateCalled bool
	mintCalled   bool
}

func (s *outagePeekAuthService) setMintErr(err error) {
	s.mintErr = err
}

func (s *outagePeekAuthService) PeekRefreshToken(_ context.Context, raw string) (*models.RefreshTokenDoc, error) {
	if s.peekByValue != nil {
		row, ok := s.peekByValue[raw]
		if !ok {
			return nil, errors.New("unknown token")
		}
		return row.doc, row.err
	}
	return nil, s.peekErr
}

// The WRONG answer on purpose: if the replay fallback fires, the test sees a
// 401 refresh_token_replay instead of a silently green run.
func (s *outagePeekAuthService) RefreshTokensWithRiskAssessment(context.Context, string, *models.SecurityContext) (*models.TokenResponse, error) {
	s.rotateCalled = true
	return nil, services.ErrRefreshTokenReplay
}

func (s *outagePeekAuthService) MintAccessTokenFromRefresh(context.Context, string, *models.SecurityContext) (*models.TokenResponse, error) {
	s.mintCalled = true
	if s.mintErr != nil {
		return nil, s.mintErr
	}
	return nil, services.ErrInvalidRefreshToken
}

func outageHandler(peekErr error) (*AuthHandler, *outagePeekAuthService) {
	cfg := &config.Config{}
	cfg.Auth.Cookie.Name = logoutTestCookieName
	svc := &outagePeekAuthService{peekErr: peekErr}
	return &AuthHandler{authService: svc, config: cfg}, svc
}

func outageHandlerTable(table map[string]peekRow) (*AuthHandler, *outagePeekAuthService) {
	cfg := &config.Config{}
	cfg.Auth.Cookie.Name = logoutTestCookieName
	svc := &outagePeekAuthService{peekByValue: table}
	return &AuthHandler{authService: svc, config: cfg}, svc
}

func withCookie(method, path string, values ...string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if len(values) == 0 {
		values = []string{"any-cookie-value"}
	}
	for _, v := range values {
		req.AddCookie(&http.Cookie{Name: logoutTestCookieName, Value: v})
	}
	return req
}

func assertOutage503(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 — this is the Mongo-blip sign-out, reached through the real handler (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["code"] != "refresh_lookup_unavailable" {
		t.Fatalf("code = %v, want refresh_lookup_unavailable", body["code"])
	}
	if sc := rec.Header().Get("Set-Cookie"); sc != "" {
		t.Fatalf("Set-Cookie = %q — the cookie must NOT be cleared on an outage; that would be unrecoverable", sc)
	}
}

var errOutage = fmt.Errorf("mongo: no reachable servers: %w", services.ErrRefreshLookupUnavailable)

// errMintOutage is what MintAccessTokenFromRefresh's own sites emit verbatim:
// the sentinel first, the cause second, exactly as the production wrap.
var errMintOutage = fmt.Errorf("token mint failed: %w: %w", services.ErrRefreshLookupUnavailable, errors.New("mongo: no reachable servers"))

func TestRefreshTokensHTTP_CookieLookupOutage_Is503_NeverFiresReplay(t *testing.T) {
	h, svc := outageHandler(errOutage)
	rec := httptest.NewRecorder()
	h.RefreshTokensHTTP(rec, withCookie(http.MethodPost, "/v1/auth/client/refresh-cookie"))
	assertOutage503(t, rec)
	if svc.rotateCalled {
		t.Fatal("the replay fallback fired on a candidate the store could not classify")
	}
}

// The PR-D D-9 shape in its new clothes: the browser carries a readable
// ROTATED leftover next to a candidate the store cannot classify. Answering
// the rotated one revokes a family whose real successor may be the cookie we
// could not read.
func TestRefreshTokensHTTP_OutageBesideRotatedLeftover_Is503_NeverFiresReplay(t *testing.T) {
	for _, order := range [][]string{{"broken", "rotated"}, {"rotated", "broken"}} {
		h, svc := outageHandlerTable(map[string]peekRow{
			"broken":  {err: errOutage},
			"rotated": {doc: rotatedDoc()},
		})
		rec := httptest.NewRecorder()
		h.RefreshTokensHTTP(rec, withCookie(http.MethodPost, "/v1/auth/client/refresh-cookie", order...))
		assertOutage503(t, rec)
		if svc.rotateCalled {
			t.Fatalf("order %v: the replay fallback fired on a family whose successor we could not read", order)
		}
	}
}

func TestRefreshTokensWithHeaderHTTP_CookieLookupOutage_Is503(t *testing.T) {
	h, svc := outageHandler(errOutage)
	rec := httptest.NewRecorder()
	h.RefreshTokensWithHeaderHTTP(rec, withCookie(http.MethodPost, "/v1/auth/client/refresh"))
	assertOutage503(t, rec)
	if svc.rotateCalled {
		t.Fatal("the replay fallback fired on a candidate the store could not classify")
	}
}

// The operator console's boot path.
func TestGetSessionHTTP_CookieLookupOutage_Is503(t *testing.T) {
	h, svc := outageHandler(errOutage)
	rec := httptest.NewRecorder()
	h.GetSessionHTTP(rec, withCookie(http.MethodGet, "/v1/auth/operator/session"))
	assertOutage503(t, rec)
	if svc.mintCalled {
		t.Fatal("MintAccessTokenFromRefresh was reached with no classified candidate")
	}
}

// The negative that keeps the picker's existing meaning: a Peek error that is
// NOT the sentinel is an invalid candidate and still answers 401.
func TestRefreshTokensHTTP_CookieInvalid_Still401(t *testing.T) {
	h, svc := outageHandler(fmt.Errorf("invalid refresh token: bad signature"))
	rec := httptest.NewRecorder()
	h.RefreshTokensHTTP(rec, withCookie(http.MethodPost, "/v1/auth/client/refresh-cookie"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an invalid candidate", rec.Code)
	}
	if svc.rotateCalled {
		t.Fatal("rotation reached with no valid candidate")
	}
}

// The mint's own reads (spec §4.9 v20, follow-up 9). The picker classified the
// cookie successfully and THEN the store went away: Peek-OK → Mint-fail. The
// existing TestGetSessionHTTP_CookieLookupOutage_Is503 cannot see this shape —
// it asserts mintCalled == false.
func TestGetSessionHTTP_MintOutage_Is503(t *testing.T) {
	h, svc := outageHandlerTable(map[string]peekRow{"good": {doc: freshDoc()}})
	svc.setMintErr(errMintOutage)
	rec := httptest.NewRecorder()
	h.GetSessionHTTP(rec, withCookie(http.MethodGet, "/v1/auth/operator/session", "good"))
	assertOutage503(t, rec)
	if !svc.mintCalled {
		t.Fatal("the mint was never reached — this test is meant to exercise the mint site, not the picker")
	}
}
