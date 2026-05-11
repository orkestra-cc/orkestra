package services

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestEncodeQuotedPrintable_Empty(t *testing.T) {
	if got := encodeQuotedPrintable(""); got != "" {
		t.Fatalf("empty input should return empty, got %q", got)
	}
}

func TestEncodeQuotedPrintable_EscapesEquals(t *testing.T) {
	in := "url?token=ABC"
	got := encodeQuotedPrintable(in)
	if !strings.Contains(got, "=3D") {
		t.Fatalf("expected literal '=' to be escaped as =3D, got %q", got)
	}
	if strings.Contains(got, "token=ABC") {
		t.Fatalf("unescaped '=ABC' would break strict QP decoders; got %q", got)
	}
}

func TestEncodeQuotedPrintable_PlainASCIIPassesThrough(t *testing.T) {
	in := "hello world"
	got := encodeQuotedPrintable(in)
	if got != "hello world" {
		t.Fatalf("plain ASCII should pass through unchanged, got %q", got)
	}
}

func TestEncodeQuotedPrintable_LongLineWrapping(t *testing.T) {
	in := strings.Repeat("a", 200)
	got := encodeQuotedPrintable(in)
	// QP wraps at 76 chars with a trailing '=' soft break, so the output
	// must contain at least one such soft line break.
	if !strings.Contains(got, "=\r\n") {
		t.Fatalf("expected soft line break in long QP output, got %q", got)
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

func TestIsSMTPConfigured(t *testing.T) {
	complete := EmailSettings{
		Provider: "smtp", Host: "mail.example.com", Port: 587, FromAddress: "no-reply@example.com",
	}
	cases := []struct {
		name string
		cfg  EmailSettings
		want bool
	}{
		{"complete", complete, true},
		{"non-smtp", EmailSettings{Provider: "noop", Host: "h", Port: 1, FromAddress: "f"}, false},
		{"missing host", EmailSettings{Provider: "smtp", Port: 587, FromAddress: "f"}, false},
		{"zero port", EmailSettings{Provider: "smtp", Host: "h", FromAddress: "f"}, false},
		{"missing from", EmailSettings{Provider: "smtp", Host: "h", Port: 587}, false},
		{"empty everything", EmailSettings{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isSMTPConfigured(c.cfg); got != c.want {
				t.Fatalf("isSMTPConfigured(%+v) = %v, want %v", c.cfg, got, c.want)
			}
		})
	}
}

func makeLoader(cfg EmailSettings) SettingsLoader {
	return func(_ context.Context) EmailSettings { return cfg }
}

func TestEmailService_IsConfigured_NoopAlwaysTrue(t *testing.T) {
	svc := NewEmailService(makeLoader(EmailSettings{Provider: "noop"}), discardLogger())
	if !svc.IsConfigured() {
		t.Fatalf("noop provider should report configured")
	}
}

func TestEmailService_IsConfigured_SMTPRequiresCompleteSettings(t *testing.T) {
	svc := NewEmailService(makeLoader(EmailSettings{Provider: "smtp"}), discardLogger())
	if svc.IsConfigured() {
		t.Fatalf("smtp provider with empty host/port/from must not be reported configured")
	}
	svc = NewEmailService(makeLoader(EmailSettings{
		Provider: "smtp", Host: "h", Port: 587, FromAddress: "f",
	}), discardLogger())
	if !svc.IsConfigured() {
		t.Fatalf("complete smtp settings should be reported configured")
	}
}

func TestEmailService_IsConfigured_UnknownProvider(t *testing.T) {
	svc := NewEmailService(makeLoader(EmailSettings{Provider: "ses"}), discardLogger())
	if svc.IsConfigured() {
		t.Fatalf("unknown provider should not be considered configured")
	}
}

func TestEmailService_ProviderName(t *testing.T) {
	cases := []struct {
		provider string
		want     string
	}{
		{"smtp", "smtp"},
		{"noop", "noop"},
		{"", "noop"},
	}
	for _, c := range cases {
		svc := NewEmailService(makeLoader(EmailSettings{Provider: c.provider}), discardLogger())
		if got := svc.ProviderName(); got != c.want {
			t.Fatalf("ProviderName(%q) = %q, want %q", c.provider, got, c.want)
		}
	}
}

func TestEmailService_Send_NoopReturnsSuccess(t *testing.T) {
	svc := NewEmailService(makeLoader(EmailSettings{Provider: "noop"}), discardLogger())
	err := svc.Send(context.Background(), EmailMessage{
		To:       "alice@example.com",
		Subject:  "hi",
		BodyText: "body",
	})
	if err != nil {
		t.Fatalf("noop send should never error, got %v", err)
	}
}

func TestEmailService_Send_EmptyProviderTreatedAsNoop(t *testing.T) {
	svc := NewEmailService(makeLoader(EmailSettings{}), discardLogger())
	if err := svc.Send(context.Background(), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"}); err != nil {
		t.Fatalf("empty provider should be treated as noop, got %v", err)
	}
}

func TestEmailService_Send_SMTPMisconfiguredReturnsError(t *testing.T) {
	// Provider set to smtp but no host/from → must return ErrEmailNotConfigured.
	svc := NewEmailService(makeLoader(EmailSettings{Provider: "smtp"}), discardLogger())
	err := svc.Send(context.Background(), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})
	if !errors.Is(err, ErrEmailNotConfigured) {
		t.Fatalf("expected ErrEmailNotConfigured, got %v", err)
	}
}

func TestBuildMIMEMessage_TextOnly(t *testing.T) {
	cfg := EmailSettings{
		FromAddress: "no-reply@example.com",
		FromName:    "Orkestra",
		ReplyTo:     "support@example.com",
	}
	msg := EmailMessage{
		To:       "alice@example.com",
		ToName:   "Alice",
		Subject:  "Hello",
		BodyText: "Body with = sign",
	}
	out := buildMIMEMessage(cfg, msg)

	for _, want := range []string{
		"From: Orkestra <no-reply@example.com>\r\n",
		"To: Alice <alice@example.com>\r\n",
		"Subject: Hello\r\n",
		"Reply-To: support@example.com\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=\"utf-8\"\r\n",
		"Content-Transfer-Encoding: quoted-printable\r\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing header/line %q in:\n%s", want, out)
		}
	}
	// Body's '=' must be QP-escaped.
	if !strings.Contains(out, "=3D") {
		t.Fatalf("expected QP-escaped body, got:\n%s", out)
	}
	// No multipart boundary for text-only payload.
	if strings.Contains(out, "multipart/alternative") {
		t.Fatalf("text-only message should not declare multipart/alternative")
	}
}

func TestBuildMIMEMessage_MultipartWhenHTMLPresent(t *testing.T) {
	cfg := EmailSettings{FromAddress: "no-reply@example.com"}
	msg := EmailMessage{
		To:       "alice@example.com",
		Subject:  "Hello",
		BodyText: "plain",
		BodyHTML: "<p>html</p>",
	}
	out := buildMIMEMessage(cfg, msg)

	if !strings.Contains(out, "Content-Type: multipart/alternative;") {
		t.Fatalf("missing multipart/alternative header in:\n%s", out)
	}
	if !strings.Contains(out, "Content-Type: text/plain; charset=\"utf-8\"") {
		t.Fatalf("missing text/plain part in:\n%s", out)
	}
	if !strings.Contains(out, "Content-Type: text/html; charset=\"utf-8\"") {
		t.Fatalf("missing text/html part in:\n%s", out)
	}
}

func TestBuildMIMEMessage_OmitsFromNameAndReplyToWhenBlank(t *testing.T) {
	cfg := EmailSettings{FromAddress: "no-reply@example.com"}
	msg := EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"}
	out := buildMIMEMessage(cfg, msg)

	if !strings.Contains(out, "From: no-reply@example.com\r\n") {
		t.Fatalf("expected bare From: address when FromName is blank, got:\n%s", out)
	}
	if strings.Contains(out, "Reply-To:") {
		t.Fatalf("Reply-To header should be omitted when ReplyTo is blank")
	}
}
