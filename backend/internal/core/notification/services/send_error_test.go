package services

import (
	"context"
	"errors"
	"io"
	"net"
	"regexp"
	"strings"
	"testing"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestFailureKind(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"canceled", context.Canceled, "canceled"},
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"net timeout", timeoutErr{}, "timeout"},
		{"dial", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, "dial"},
		{"other", errors.New("boom"), "io"},
	}
	for _, c := range cases {
		if got := failureKind(c.err); got != c.want {
			t.Errorf("%s: failureKind = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSafeToken(t *testing.T) {
	cases := map[string]string{
		"":                      "",
		"done":                  "done",
		"E_1.2-x":               "E_1.2-x",
		"has space":             "invalid",
		"<html>":                "invalid",
		strings.Repeat("a", 65): "invalid",
		strings.Repeat("a", 64): strings.Repeat("a", 64),
	}
	for in, want := range cases {
		if got := safeToken(in); got != want {
			t.Errorf("safeToken(%q) = %q, want %q", in, got, want)
		}
	}
	if safeToken(LegacySlug) != LegacySlug {
		t.Fatalf("LegacySlug %q must survive the diagnostic allowlist", LegacySlug)
	}
	if got := safeMediaType("application/json; charset=utf-8"); got != "invalid" {
		t.Errorf("parameters are not a media type: %q", got)
	}
	if got := safeMediaType("application/vnd.api+json"); got != "application/vnd.api+json" {
		t.Errorf("real media type mangled: %q", got)
	}
}

func TestSendError_Shapes(t *testing.T) {
	cause := errors.New("535 5.7.8 AHVzZXIAcGFzcw==")
	cases := []struct {
		name string
		err  *SendError
		want string
	}{
		{"server rejection", rejectionError("smtp", "auth", 535, cause), "smtp op=auth code=535"},
		{"local failure with op", transportError("smtp", "dial", &net.OpError{Op: "dial", Err: errors.New("refused")}), "smtp op=dial err=dial"},
		{"local failure without op", transportError("mailup", "", context.DeadlineExceeded), "mailup err=timeout"},
		{"vendor envelope", vendorEnvelopeError("mailup", 401, "error", "401"), "http=401 status=error code=401"},
		{"vendor envelope empty fields", vendorEnvelopeError("mailup", 200, "", ""), "http=200 status= code="},
		{"oversized body", vendorBodyError("mailup", 200, bodyTooLarge, 0, ""), "http=200 body=too_large"},
		{"unparseable body", vendorBodyError("mailup", 502, bodyUnparseable, 20, "text/html"), "http=502 body=unparseable bytes=20 type=text/html"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if !errors.Is(rejectionError("smtp", "auth", 535, cause), cause) {
		t.Fatal("Unwrap must expose the cause for errors.Is")
	}
}

// TestSendError_FreeTextCannotReachTheDiagnostic is the chokepoint guarantee:
// a driver — a fork's, or a buggy one of ours — that stuffs remote text into
// every string field of SendError still produces a diagnostic of the fixed
// shape with the marker in place of the text. There is no field it could
// use to pass a body through.
func TestSendError_FreeTextCannotReachTheDiagnostic(t *testing.T) {
	secret := "hunter2 s3cr=t!"
	hostile := &SendError{Driver: "mailup " + secret, Op: "send " + secret, Kind: secret, HTTP: 200,
		Status: "error " + secret, Vendor: secret, Body: secret, Bytes: 3, Media: "text/html; " + secret}
	got := describeSendError(SenderProfile{Slug: "s"}, hostile)
	if strings.Contains(got, secret) {
		t.Fatalf("remote text reached the diagnostic: %q", got)
	}
	if got != "sender=s http=200 status=invalid code=invalid" {
		t.Fatalf("unexpected shape %q", got)
	}
	hostile2 := &SendError{Driver: secret, Op: secret, Kind: secret}
	if got := hostile2.Error(); got != "invalid op=invalid err=io" {
		t.Fatalf("unexpected shape %q", got)
	}
	// An unknown Body marker never selects a body shape.
	if got := (&SendError{HTTP: 200, Body: "<html>"}).Error(); got != "http=200 status= code=" {
		t.Fatalf("unexpected shape %q", got)
	}
}

func TestDescribeSendError_Shapes(t *testing.T) {
	p := SenderProfile{Slug: "esp-campagne", Provider: "smtp"}
	cases := []struct {
		name string
		p    SenderProfile
		err  error
		want string
	}{
		{"no sender", SenderProfile{}, ErrNoSenderForCategory, "sender=- err=no_sender_for_category"},
		{"config unavailable", SenderProfile{}, ErrSenderConfigUnavailable, "sender=- err=config_unavailable"},
		{"unknown driver", SenderProfile{Slug: "x", Provider: "ses"}, ErrUnknownDriver, "sender=x driver=ses err=unknown_driver"},
		{"incomplete", p, &ProfileIncompleteError{Driver: "smtp", Missing: []string{SubSMTPHost, SubFromAddress}},
			"sender=esp-campagne driver=smtp err=not_configured missing=smtp_host,from_address"},
		{"driver error", p, rejectionError("smtp", "auth", 535, errors.New("535 secret")),
			"sender=esp-campagne smtp op=auth code=535"},
		{"deadline", p, context.DeadlineExceeded, "sender=esp-campagne err=timeout"},
		{"canceled", p, context.Canceled, "sender=esp-campagne err=canceled"},
		{"unknown text is dropped", p, errors.New("mailgun: user=s1_2 secret=hunter2"), "sender=esp-campagne err=unknown"},
	}
	for _, c := range cases {
		if got := describeSendError(c.p, c.err); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if !regexp.MustCompile(`^sender=[A-Za-z0-9._-]+ smtp op=[a-z_]+ code=\d{3}$`).MatchString(describeSendError(p, rejectionError("smtp", "auth", 535, nil))) {
		t.Fatal("shape")
	}
}

func TestDescribeSendError_MissingKeysAreAllowlisted(t *testing.T) {
	inc := &ProfileIncompleteError{Driver: "x", Missing: []string{SubSMTPHost, "api key hunter2", strings.Repeat("a", 65)}}
	got := describeSendError(SenderProfile{Slug: "s", Provider: "x"}, inc)
	if got != "sender=s driver=x err=not_configured missing=smtp_host,invalid,invalid" {
		t.Fatalf("got %q", got)
	}
}

func TestDescribeSendError_IsCapped(t *testing.T) {
	many := make([]string, 200)
	for i := range many {
		many[i] = strings.Repeat("a", 64)
	}
	long := &ProfileIncompleteError{Driver: "x", Missing: many}
	got := describeSendError(SenderProfile{Slug: "s"}, long)
	if len(got) > 512 {
		t.Fatalf("len = %d, want ≤ 512", len(got))
	}
}

// countingReader counts bytes handed out so the test can assert how much
// was READ, not merely how much was stored.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestReadBounded(t *testing.T) {
	// 5 MB HTML error page: rejected as too large, and at most max+1 bytes were read.
	big := &countingReader{r: strings.NewReader(strings.Repeat("<html>", 1<<20))}
	body, tooLarge, err := readBounded(big, maxResponseBody)
	if err != nil || !tooLarge || body != nil {
		t.Fatalf("want tooLarge with no body, got %v %v %d", tooLarge, err, len(body))
	}
	if big.n > maxResponseBody+1 {
		t.Fatalf("read %d bytes, must stop at %d", big.n, maxResponseBody+1)
	}

	// Exactly at the cap is fine.
	exact := strings.Repeat("a", maxResponseBody)
	body, tooLarge, err = readBounded(strings.NewReader(exact), maxResponseBody)
	if err != nil || tooLarge || len(body) != maxResponseBody {
		t.Fatalf("exactly at the cap must be accepted: %v %v %d", tooLarge, err, len(body))
	}

	// One over the cap is not.
	_, tooLarge, _ = readBounded(strings.NewReader(exact+"a"), maxResponseBody)
	if !tooLarge {
		t.Fatal("max+1 bytes must be rejected")
	}
}
