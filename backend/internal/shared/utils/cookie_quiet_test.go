package utils

// Cookie helpers must not print to stdout.
//
// SetSecureCookie / SetRefreshTokenCookie / ClearRefreshTokenCookie each
// emitted several `[COOKIE_DEBUG] ...` lines via fmt.Printf on every
// login, refresh, and logout. Nothing redacted the fields — the cookie
// name, domain, Max-Age, and flags all went to stdout — and it bypassed
// the structured logger entirely, so ADR-0005's allowlisted, correlated
// request log could neither filter nor level it.

import (
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestCookieHelpers_WriteNothingToStdout(t *testing.T) {
	out := captureStdout(t, func() {
		rec := httptest.NewRecorder()
		SetRefreshTokenCookie(rec, "orkestra_cookie", "a-refresh-token", 604800, "console.example.com", true)
		ClearRefreshTokenCookie(rec, "orkestra_cookie", "console.example.com", true)
		SetSecureCookie(rec, &CookieOptions{Name: "x", Value: "y", Path: "/"})
	})

	if strings.TrimSpace(out) != "" {
		t.Errorf("cookie helpers must be silent on stdout, got:\n%s", out)
	}
}

func TestSetRefreshTokenCookie_StillSetsTheCookie(t *testing.T) {
	// Guard against "silenced it by deleting the function body".
	rec := httptest.NewRecorder()
	SetRefreshTokenCookie(rec, "orkestra_cookie", "a-refresh-token", 604800, "console.example.com", true)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected exactly one cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != "orkestra_cookie" || c.Value != "a-refresh-token" {
		t.Errorf("cookie = %s=%s", c.Name, c.Value)
	}
	if !c.HttpOnly || !c.Secure {
		t.Error("the refresh cookie must stay HttpOnly + Secure")
	}
}
