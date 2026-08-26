package services

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEncodeQuotedPrintable_Empty(t *testing.T) {
	if got := encodeQuotedPrintable(""); got != "" {
		t.Fatalf("empty input should return empty, got %q", got)
	}
}

func TestEncodeQuotedPrintable_EscapesEquals(t *testing.T) {
	got := encodeQuotedPrintable("url?token=ABC")
	if !strings.Contains(got, "=3D") || strings.Contains(got, "token=ABC") {
		t.Fatalf("expected literal '=' escaped as =3D, got %q", got)
	}
}

func TestEncodeQuotedPrintable_PlainASCIIPassesThrough(t *testing.T) {
	if got := encodeQuotedPrintable("hello world"); got != "hello world" {
		t.Fatalf("plain ASCII should pass through unchanged, got %q", got)
	}
}

func TestEncodeQuotedPrintable_LongLineWrapping(t *testing.T) {
	if got := encodeQuotedPrintable(strings.Repeat("a", 200)); !strings.Contains(got, "=\r\n") {
		t.Fatalf("expected soft line break in long QP output, got %q", got)
	}
}

// TestSMTPDriver_Requires is the D6 regression test: an anonymous relay —
// host + port + from, no credentials — must validate exactly as
// isSMTPConfigured accepted it; missing host, port or from must not.
func TestSMTPDriver_Requires(t *testing.T) {
	d := NewSMTPDriver(nil)
	complete := SenderProfile{Provider: "smtp", SMTPHost: "mail.example.com", SMTPPort: 587, FromAddress: "no-reply@example.com"}
	cases := []struct {
		name string
		p    SenderProfile
		ok   bool
	}{
		{"anonymous relay", complete, true},
		{"username without password", SenderProfile{Provider: "smtp", SMTPHost: "h", SMTPPort: 587, FromAddress: "f", SMTPUsername: "u"}, true},
		{"missing host", SenderProfile{Provider: "smtp", SMTPPort: 587, FromAddress: "f"}, false},
		{"zero port", SenderProfile{Provider: "smtp", SMTPHost: "h", FromAddress: "f"}, false},
		{"missing from", SenderProfile{Provider: "smtp", SMTPHost: "h", SMTPPort: 587}, false},
		{"empty", SenderProfile{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateProfile(d, c.p, RuntimeView)
			if (err == nil) != c.ok {
				t.Fatalf("ValidateProfile(%+v) = %v, want ok=%v", c.p, err, c.ok)
			}
			if !c.ok && !errors.Is(err, ErrSenderNotConfigured) {
				t.Fatalf("want ErrSenderNotConfigured, got %v", err)
			}
		})
	}
	for _, r := range d.Requires() {
		if r.Secret {
			t.Fatalf("smtp must not require a secret: %+v", r)
		}
	}
}

func TestSMTPDriver_SendRefusesIncompleteProfile(t *testing.T) {
	err := NewSMTPDriver(nil).Send(context.Background(), SenderProfile{Provider: "smtp"}, EmailMessage{To: "a@example.com"})
	if !errors.Is(err, ErrSenderNotConfigured) {
		t.Fatalf("expected ErrSenderNotConfigured, got %v", err)
	}
}

func TestBuildMIMEMessage_TextOnly(t *testing.T) {
	p := SenderProfile{FromAddress: "no-reply@example.com", FromName: "Orkestra", ReplyTo: "support@example.com"}
	msg := EmailMessage{To: "alice@example.com", ToName: "Alice", Subject: "Hello", BodyText: "Body with = sign", Category: "auth.verify_email"}
	out := buildMIMEMessage(p, msg)
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
	if !strings.Contains(out, "=3D") {
		t.Fatalf("expected QP-escaped body, got:\n%s", out)
	}
	if strings.Contains(out, "multipart/alternative") {
		t.Fatalf("text-only message should not declare multipart/alternative")
	}
	// The wire output ignores Category — what makes extending EmailMessage safe under D6.
	if strings.Contains(out, "auth.verify_email") {
		t.Fatalf("Category must not reach the wire: %s", out)
	}
}

// mimeGolden is the exact wire output captured from the pre-refactor
// transport in Step 0 (see the commit "pin today's MIME wire output"). The
// driver must reproduce it byte for byte, with and without Category set.
const mimeGolden = "From: Orkestra <no-reply@example.com>\r\n" +
	"To: Alice <alice@example.com>\r\n" +
	"Subject: Hello\r\n" +
	"Reply-To: support@example.com\r\n" +
	"Date: Wed, 26 Aug 2026 12:00:00 +0000\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/alternative; boundary=\"orkestra_boundary_1787745600000000000\"\r\n\r\n" +
	"--orkestra_boundary_1787745600000000000\r\n" +
	"Content-Type: text/plain; charset=\"utf-8\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
	"Body with =3D sign\r\n" +
	"--orkestra_boundary_1787745600000000000\r\n" +
	"Content-Type: text/html; charset=\"utf-8\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
	"<p>html</p>\r\n" +
	"--orkestra_boundary_1787745600000000000--\r\n"

var mimeGoldenAt = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

// TestBuildMIMEMessage_ByteIdentical is the D6 wire test: the profile-based
// builder produces exactly the bytes the EmailSettings-based one did, and
// EmailMessage.Category changes nothing.
func TestBuildMIMEMessage_ByteIdentical(t *testing.T) {
	p := SenderProfile{FromAddress: "no-reply@example.com", FromName: "Orkestra", ReplyTo: "support@example.com"}
	msg := EmailMessage{To: "alice@example.com", ToName: "Alice", Subject: "Hello", BodyText: "Body with = sign", BodyHTML: "<p>html</p>"}
	if got := buildMIMEMessageAt(p, msg, mimeGoldenAt); got != mimeGolden {
		t.Fatalf("wire output drifted from the golden:\n%q", got)
	}
	msg.Category = "auth.verify_email"
	if got := buildMIMEMessageAt(p, msg, mimeGoldenAt); got != mimeGolden {
		t.Fatalf("Category must not change the wire output:\n%q", got)
	}
}

func TestBuildMIMEMessage_MultipartWhenHTMLPresent(t *testing.T) {
	out := buildMIMEMessage(SenderProfile{FromAddress: "no-reply@example.com"},
		EmailMessage{To: "alice@example.com", Subject: "Hello", BodyText: "plain", BodyHTML: "<p>html</p>"})
	for _, want := range []string{
		"Content-Type: multipart/alternative;",
		"Content-Type: text/plain; charset=\"utf-8\"",
		"Content-Type: text/html; charset=\"utf-8\"",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestBuildMIMEMessage_OmitsFromNameAndReplyToWhenBlank(t *testing.T) {
	out := buildMIMEMessage(SenderProfile{FromAddress: "no-reply@example.com"}, EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})
	if !strings.Contains(out, "From: no-reply@example.com\r\n") || strings.Contains(out, "Reply-To:") {
		t.Fatalf("bare From expected and no Reply-To, got:\n%s", out)
	}
}

// ---- scripted SMTP server ----------------------------------------------

// scriptedSMTP is a one-connection SMTP server with fixed replies keyed by
// command verb. It scripts rejections — including a 535 that echoes the AUTH
// argument back — without a real MTA. greet=false accepts and never speaks.
type scriptedSMTP struct {
	ln      net.Listener
	replies map[string]string
	greet   bool
	mu      sync.Mutex
	got     []string
}

func startScriptedSMTP(t *testing.T, greet bool, replies map[string]string) *scriptedSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &scriptedSMTP{ln: ln, replies: replies, greet: greet}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *scriptedSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *scriptedSMTP) serve() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	if !s.greet {
		time.Sleep(2 * time.Second) // longer than any test deadline; the goroutine ends with the test binary
		return
	}
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	w.WriteString("220 scripted ESMTP\r\n")
	w.Flush()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.got = append(s.got, line)
		s.mu.Unlock()
		verb := line
		if i := strings.IndexByte(line, ' '); i >= 0 {
			verb = line[:i]
		}
		verb = strings.ToUpper(verb)
		reply, ok := s.replies[verb]
		if !ok {
			switch verb {
			case "EHLO", "HELO":
				reply = "250-scripted\r\n250 AUTH PLAIN\r\n"
			case "QUIT":
				reply = "221 bye\r\n"
			default:
				reply = "250 ok\r\n"
			}
		}
		w.WriteString(reply)
		w.Flush()
		if verb == "QUIT" {
			return
		}
	}
}

func scriptedProfile(port int) SenderProfile {
	return SenderProfile{Provider: "smtp", SMTPHost: "127.0.0.1", SMTPPort: port, SMTPTLSMode: "none", FromAddress: "no-reply@example.com"}
}

// TestSMTPDriver_AuthRejectionKeepsOnlyCode is the regression test for the
// credential path inherited from sendErr.Error(): a 535 line that echoes the
// base64 AUTH argument must leave "smtp op=auth code=535" and nothing else.
func TestSMTPDriver_AuthRejectionKeepsOnlyCode(t *testing.T) {
	user, pass := "s12345_67", "hunter2-secret"
	echo := base64.StdEncoding.EncodeToString([]byte("\x00" + user + "\x00" + pass))
	srv := startScriptedSMTP(t, true, map[string]string{"AUTH": "535 5.7.8 rejected " + echo + "\r\n"})

	p := scriptedProfile(srv.port())
	p.SMTPUsername, p.SMTPPassword = user, pass
	err := NewSMTPDriver(discardLogger()).Send(context.Background(), p, EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})

	var se *SendError
	if !errors.As(err, &se) {
		t.Fatalf("want *SendError, got %T %v", err, err)
	}
	if se.Error() != "smtp op=auth code=535" {
		t.Fatalf("diagnostic = %q", se.Error())
	}
	if s := err.Error(); strings.Contains(s, echo) || strings.Contains(s, pass) || strings.Contains(s, "rejected") {
		t.Fatalf("server text leaked: %q", s)
	}
}

func TestSMTPDriver_RcptRejectionKeepsOnlyCode(t *testing.T) {
	srv := startScriptedSMTP(t, true, map[string]string{"RCPT": "550 5.1.1 <a@example.com> user unknown\r\n"})
	err := NewSMTPDriver(discardLogger()).Send(context.Background(), scriptedProfile(srv.port()), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})
	var se *SendError
	if !errors.As(err, &se) || se.Error() != "smtp op=rcpt_to code=550" {
		t.Fatalf("got %v", err)
	}
}

func TestSMTPDriver_AcceptedMessage(t *testing.T) {
	srv := startScriptedSMTP(t, true, map[string]string{"DATA": "354 go ahead\r\n", ".": "250 queued\r\n"})
	err := NewSMTPDriver(discardLogger()).Send(context.Background(), scriptedProfile(srv.port()), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})
	if err != nil {
		t.Fatalf("expected accepted send, got %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	joined := strings.Join(srv.got, "\n")
	if !strings.Contains(joined, "MAIL FROM:<no-reply@example.com>") || !strings.Contains(joined, "RCPT TO:<a@example.com>") {
		t.Fatalf("envelope not sent: %s", joined)
	}
	if strings.Contains(joined, "AUTH") {
		t.Fatalf("anonymous relay must not authenticate: %s", joined)
	}
}

func TestSMTPDriver_DialRefusedIsKindDial(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	sendErr := NewSMTPDriver(discardLogger()).Send(context.Background(), scriptedProfile(port), EmailMessage{To: "a@example.com"})
	var se *SendError
	if !errors.As(sendErr, &se) || se.Error() != "smtp op=dial err=dial" {
		t.Fatalf("got %v", sendErr)
	}
}

func TestSMTPDriver_HungServerIsKindTimeout(t *testing.T) {
	srv := startScriptedSMTP(t, false, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewSMTPDriver(discardLogger()).Send(ctx, scriptedProfile(srv.port()), EmailMessage{To: "a@example.com"})
	var se *SendError
	if !errors.As(err, &se) || se.Error() != "smtp op=greeting err=timeout" {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("Go error string leaked: %q", err.Error())
	}
}
