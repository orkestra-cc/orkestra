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

func TestNoopDriver_LogsHeadersAtDebug(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	msg := EmailMessage{
		To: "ada@example.test", Subject: "Ciao", BodyText: "corpo",
		Headers: map[string]string{"List-Unsubscribe": "<https://api.example/v1/notifications/unsubscribe?token=abc>"},
	}
	if err := NewNoopDriver(logger).Send(context.Background(), SenderProfile{}, msg); err != nil {
		t.Fatalf("noop send must never error, got %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "List-Unsubscribe") || !strings.Contains(got, "token=abc") {
		t.Fatalf("expected the header to reach the debug log, got:\n%s", got)
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
