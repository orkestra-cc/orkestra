package services

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestNoopDriver_RequiresNothingAndNeverFails(t *testing.T) {
	d := NewNoopDriver(nil)
	if d.Name() != "noop" || len(d.Requires()) != 0 {
		t.Fatalf("noop must be named noop and require nothing: %v", d.Requires())
	}
	if err := ValidateProfile(d, SenderProfile{}, RuntimeView); err != nil {
		t.Fatalf("a noop profile with nothing but a slug must validate, got %v", err)
	}
	if err := d.Send(context.Background(), SenderProfile{}, EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"}); err != nil {
		t.Fatalf("noop send must never error, got %v", err)
	}
}

// TestNoopDriver_Capabilities: noop delivers nothing, so it can guarantee
// nothing — but "nothing" trivially includes List-Unsubscribe. It reports
// true because it never lies about a header it never sent to a wire.
func TestNoopDriver_Capabilities(t *testing.T) {
	if !NewNoopDriver(nil).Capabilities().ListUnsubscribeHeaders {
		t.Fatal("noop must report ListUnsubscribeHeaders=true")
	}
}

// TestNoopDriver_LogsHeaderNamesAtDebug_NeverValues: an operator watching the
// noop log at Debug level can still confirm a header like List-Unsubscribe
// was present on a send, but never sees its value — a header's value can
// carry a secret (the RFC 8058 header embeds a raw, single-use unsubscribe
// token), and "the raw token never reaches a log, at any level" has no
// dev-driver exception.
func TestNoopDriver_LogsHeaderNamesAtDebug_NeverValues(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	msg := EmailMessage{
		To: "ada@example.test", Subject: "Ciao", BodyText: "corpo",
		Headers: map[string]string{
			"List-Unsubscribe":      "<https://api.example/v1/notifications/unsubscribe?token=abc123raw>",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
	}
	if err := NewNoopDriver(logger).Send(context.Background(), SenderProfile{}, msg); err != nil {
		t.Fatalf("noop send must never error, got %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "List-Unsubscribe") || !strings.Contains(got, "List-Unsubscribe-Post") {
		t.Fatalf("expected both header names to reach the debug log, got:\n%s", got)
	}
	if strings.Contains(got, "abc123raw") {
		t.Fatalf("the raw token must never reach the log, even at debug: %s", got)
	}
}

// TestNoopDriver_RedactsUnsubscribeTokenFromLoggedBody: the seeded marketing
// template's footer ("Unsubscribe from marketing: {{.UnsubscribeURL}}")
// embeds the SAME raw token the RFC 8058 header now carries. Fixing the
// header line alone (TestNoopDriver_LogsHeaderNamesAtDebug_NeverValues)
// would have been a half-fix: the body debug line puts the identical
// secret right back into the log.
func TestNoopDriver_RedactsUnsubscribeTokenFromLoggedBody(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	msg := EmailMessage{
		To: "ada@example.test", Subject: "Ciao",
		BodyText: "Ciao Ada,\n\nUnsubscribe from marketing: https://api.example/v1/notifications/unsubscribe?token=raw-secret-token-xyz&extra=1",
	}
	if err := NewNoopDriver(logger).Send(context.Background(), SenderProfile{}, msg); err != nil {
		t.Fatalf("noop send must never error, got %v", err)
	}
	got := buf.String()
	if strings.Contains(got, "raw-secret-token-xyz") {
		t.Fatalf("the raw token embedded in the rendered footer link must never reach the log: %s", got)
	}
	if !strings.Contains(got, "Unsubscribe from marketing") || !strings.Contains(got, "token=[redacted]") {
		t.Fatalf("the rest of the body should still be visible for dev debugging, with the token redacted in place: %s", got)
	}
	if !strings.Contains(got, "extra=1") {
		t.Fatalf("only the token query value should be redacted, not the whole query string: %s", got)
	}
}

func TestRedactTokens(t *testing.T) {
	cases := []struct{ in, want string }{
		{"no token here", "no token here"},
		{"token=abc", "token=[redacted]"},
		{"?token=abc&other=1", "?token=[redacted]&other=1"},
		{"", ""},
	}
	for _, c := range cases {
		if got := redactTokens(c.in); got != c.want {
			t.Fatalf("redactTokens(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abc", 3, "abc"},
		{"abcdef", 3, "abc..."},
		{"", 5, ""},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.n); got != c.want {
			t.Fatalf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
