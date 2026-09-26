package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
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

// goldenProfile and goldenMessage are the inputs that produce mimeGolden.
func goldenProfile() SenderProfile {
	return SenderProfile{FromAddress: "no-reply@example.com", FromName: "Orkestra", ReplyTo: "support@example.com"}
}

func goldenMessage() EmailMessage {
	return EmailMessage{To: "alice@example.com", ToName: "Alice", Subject: "Hello", BodyText: "Body with = sign", BodyHTML: "<p>html</p>"}
}

// TestBuildMIMEMessage_ByteIdentical is the D6 wire test: the profile-based
// builder produces exactly the bytes the EmailSettings-based one did, and
// EmailMessage.Category changes nothing.
func TestBuildMIMEMessage_ByteIdentical(t *testing.T) {
	p, msg := goldenProfile(), goldenMessage()
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
//
// DATA answers 354 unless scripted otherwise; after a 354 the server is in
// data mode: it swallows the message lines (kept in data) and answers only
// at the terminating "." with the reply keyed by ".". Every reply must end
// in CRLF.
type scriptedSMTP struct {
	ln      net.Listener
	replies map[string]string
	greet   bool
	mu      sync.Mutex
	got     []string
	data    []string
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
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line != "." {
				s.mu.Lock()
				s.data = append(s.data, line)
				s.mu.Unlock()
				continue
			}
			inData = false
			reply, ok := s.replies["."]
			if !ok {
				reply = "250 ok\r\n"
			}
			w.WriteString(reply)
			w.Flush()
			continue
		}
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
			case "DATA":
				reply = "354 go ahead\r\n"
			default:
				reply = "250 ok\r\n"
			}
		}
		w.WriteString(reply)
		w.Flush()
		if verb == "DATA" && strings.HasPrefix(reply, "354") {
			inData = true
		}
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

// TestSMTPDriver_Capabilities: smtp writes the MIME itself, so it can
// guarantee List-Unsubscribe reaches the wire.
func TestSMTPDriver_Capabilities(t *testing.T) {
	if !NewSMTPDriver(nil).Capabilities().ListUnsubscribeHeaders {
		t.Fatal("smtp writes its own MIME and must report ListUnsubscribeHeaders=true")
	}
}

func TestBuildMIME_WritesHeadersAfterSubject(t *testing.T) {
	msg := EmailMessage{
		To: "ada@example.test", Subject: "Ciao", BodyText: "corpo",
		Headers: map[string]string{
			"List-Unsubscribe":      "<https://api.example/v1/notifications/unsubscribe?token=abc>",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
	}
	out := buildMIMEMessageAt(SenderProfile{FromAddress: "no-reply@example.test"}, msg, time.Unix(0, 0))

	iSubject := strings.Index(out, "Subject: ")
	iLU := strings.Index(out, "List-Unsubscribe: ")
	iBody := strings.Index(out, "corpo")
	if iSubject < 0 || iLU < 0 || iBody < 0 {
		t.Fatalf("incomplete MIME message:\n%s", out)
	}
	if !(iSubject < iLU && iLU < iBody) {
		t.Fatalf("the headers must sit after Subject and before the body:\n%s", out)
	}
	if !strings.Contains(out, "List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n") {
		t.Fatalf("the List-Unsubscribe-Post header is missing or not CRLF-terminated:\n%s", out)
	}
}

func TestBuildMIME_WithoutHeadersIsUnchanged(t *testing.T) {
	msg := EmailMessage{To: "ada@example.test", Subject: "Ciao", BodyText: "corpo"}
	out := buildMIMEMessageAt(SenderProfile{FromAddress: "no-reply@example.test"}, msg, time.Unix(0, 0))
	if strings.Contains(out, "List-Unsubscribe") {
		t.Fatal("no header may be invented when the map is empty")
	}
}

// An empty header VALUE is well-formed ("X-Foo:" with an empty field body)
// and must not be mistaken for the blank line that ends the header block —
// the body has to survive it.
func TestBuildMIME_EmptyHeaderValueKeepsTheMessageWellFormed(t *testing.T) {
	msg := EmailMessage{
		To: "ada@example.test", Subject: "Ciao", BodyText: "corpo",
		Headers: map[string]string{"List-Unsubscribe": ""},
	}
	out := buildMIMEMessageAt(SenderProfile{FromAddress: "no-reply@example.test"}, msg, time.Unix(0, 0))

	if !strings.Contains(out, "List-Unsubscribe: \r\n") {
		t.Fatalf("an empty value must still be written as an empty field body:\n%s", out)
	}
	iLU := strings.Index(out, "List-Unsubscribe: ")
	iDate := strings.Index(out, "Date: ")
	iBody := strings.Index(out, "corpo")
	if iDate < iLU || iBody < iDate {
		t.Fatalf("an empty value must not end the header block early:\n%s", out)
	}
}

// A caller-supplied entry naming a field the builder writes itself is
// dropped: two Subject: or Content-Type: lines are resolved inconsistently by
// MTAs, filters and DKIM verifiers, which is how a message is made to look
// like it says something it does not. Matching is case-insensitive, because
// header names are.
func TestBuildMIME_ReservedHeaderKeysAreDropped(t *testing.T) {
	msg := EmailMessage{
		To: "ada@example.test", Subject: "real subject", BodyText: "corpo",
		Headers: map[string]string{
			"Subject":                   "forged subject",
			"from":                      "attacker@example.test",
			"MIME-Version":              "9.9",
			"Content-Type":              "text/html; charset=\"utf-8\"",
			"Content-Transfer-Encoding": "base64",
			"Date":                      "Tue, 1 Jan 1980 00:00:00 +0000",
			"To":                        "someone-else@example.test",
			"Reply-To":                  "attacker@example.test",
			"List-Unsubscribe":          "<https://api.example/v1/notifications/unsubscribe?token=abc>",
		},
	}
	out := buildMIMEMessageAt(SenderProfile{FromAddress: "no-reply@example.test"}, msg, time.Unix(0, 0))

	for _, forged := range []string{"forged subject", "attacker@example.test", "9.9", "base64", "1980", "someone-else@example.test"} {
		if strings.Contains(out, forged) {
			t.Fatalf("a reserved header key must not reach the wire (%q):\n%s", forged, out)
		}
	}
	for _, name := range []string{"Subject: ", "From: ", "To: ", "Date: ", "MIME-Version: ", "Content-Type: "} {
		if n := strings.Count(out, "\r\n"+name) + boolToInt(strings.HasPrefix(out, name)); n != 1 {
			t.Fatalf("%q must appear exactly once, got %d:\n%s", name, n, out)
		}
	}
	// The one entry that is not reserved still gets through.
	if !strings.Contains(out, "List-Unsubscribe: <https://api.example/v1/notifications/unsubscribe?token=abc>\r\n") {
		t.Fatalf("a legitimate header must survive the guard:\n%s", out)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Defence in depth for the injection vector itself. oneClickBase already
// refuses a base URL carrying CR/LF, so nothing in this module can produce
// such an entry today — but the builder is what puts bytes on the wire, and
// EmailMessage.Headers is a public field.
func TestBuildMIME_HeaderEntriesCarryingCRLFAreDropped(t *testing.T) {
	msg := EmailMessage{
		To: "ada@example.test", Subject: "Ciao", BodyText: "corpo",
		Headers: map[string]string{
			"X-Evil\r\nBcc": "someone@example.test",
			"X-Also-Evil":   "ok\r\nBcc: someone-else@example.test",
			"  ":            "blank key",
			"X-Fine":        "kept",
		},
	}
	out := buildMIMEMessageAt(SenderProfile{FromAddress: "no-reply@example.test"}, msg, time.Unix(0, 0))

	for _, leaked := range []string{"Bcc", "someone@example.test", "someone-else@example.test", "blank key"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("an unsafe header entry reached the wire (%q):\n%s", leaked, out)
		}
	}
	if !strings.Contains(out, "X-Fine: kept\r\n") {
		t.Fatalf("a safe entry alongside unsafe ones must still be written:\n%s", out)
	}
}

// ---- attachments (multipart/mixed) -------------------------------------

func TestBuildMIME_NoAttachments_ByteIdentical(t *testing.T) {
	// the golden must not move by a byte
	got := buildMIMEMessageAt(goldenProfile(), goldenMessage(), mimeGoldenAt)
	if got != mimeGolden {
		t.Fatalf("MIME without attachments changed:\n%s", got)
	}
}

func TestBuildMIME_WithAttachment_MixedWrapsAlternative(t *testing.T) {
	msg := goldenMessage()
	msg.Attachments = []iface.Attachment{{Filename: "Ricevuta — Iscrizione.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.7 x")}}
	got := buildMIMEMessageAt(goldenProfile(), msg, mimeGoldenAt)
	m, err := mail.ReadMessage(strings.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	mt, params, _ := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if mt != "multipart/mixed" {
		t.Fatalf("top = %s", mt)
	}
	mr := multipart.NewReader(m.Body, params["boundary"])
	first, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	ct, innerParams, _ := mime.ParseMediaType(first.Header.Get("Content-Type"))
	if ct != "multipart/alternative" {
		t.Fatalf("first part = %s", ct)
	}
	// The alternative still carries both bodies, unchanged.
	ir := multipart.NewReader(first, innerParams["boundary"])
	var kinds []string
	for {
		p, err := ir.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		k, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		kinds = append(kinds, k)
	}
	if strings.Join(kinds, ",") != "text/plain,text/html" {
		t.Fatalf("alternative parts = %v", kinds)
	}
	second, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if second.Header.Get("Content-Type") != "application/pdf" || second.Header.Get("Content-Transfer-Encoding") != "base64" {
		t.Fatalf("attachment headers = %v", second.Header)
	}
	if _, p, _ := mime.ParseMediaType(second.Header.Get("Content-Disposition")); p["filename"] != "Ricevuta — Iscrizione.pdf" {
		t.Fatalf("filename = %q", p["filename"])
	}
	raw, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, second))
	if string(raw) != "%PDF-1.7 x" {
		t.Fatalf("body = %q", raw)
	}
	if _, err := mr.NextPart(); err != io.EOF {
		t.Fatalf("want exactly two top-level parts, got err=%v", err)
	}
}

// A text-only message with an attachment: the text/plain body becomes the
// first part of the mixed container.
func TestBuildMIME_TextOnlyWithAttachment(t *testing.T) {
	msg := EmailMessage{To: "a@example.com", Subject: "s", BodyText: "corpo = testo",
		Attachments: []iface.Attachment{{Filename: "r.pdf", ContentType: "application/pdf", Data: []byte("%PDF-")}}}
	m, err := mail.ReadMessage(strings.NewReader(buildMIMEMessageAt(SenderProfile{FromAddress: "f@example.com"}, msg, mimeGoldenAt)))
	if err != nil {
		t.Fatal(err)
	}
	_, params, _ := mime.ParseMediaType(m.Header.Get("Content-Type"))
	mr := multipart.NewReader(m.Body, params["boundary"])
	first, err := mr.NextPart() // multipart.Part decodes quoted-printable itself
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(first)
	if ct, _, _ := mime.ParseMediaType(first.Header.Get("Content-Type")); ct != "text/plain" || string(body) != "corpo = testo" {
		t.Fatalf("first part = %s %q", ct, body)
	}
}

// Review focus 3: a title with non-ASCII, '/' and '"' must give a valid,
// readable header — no path, no broken quoting, no line over 998 octets —
// even for a long name or a 1 KB attachment.
func TestBuildMIME_AttachmentFilenameUTF8(t *testing.T) {
	longName := strings.Repeat("日本語", 50) + ".pdf"         // 150 runes of stem, ~1.4 KB percent-encoded
	pdf := bytes.Repeat([]byte("%PDF-1.7 0123456789"), 60) // > 1 KB: base64 must wrap
	cases := []struct{ in, want string }{
		{"Café “x”.pdf", "Café “x”.pdf"},
		{`Iscrizione a/b "Evento".pdf`, `Iscrizione a/b "Evento".pdf`},
		{longName, longName},
	}
	for _, c := range cases {
		msg := goldenMessage()
		msg.Attachments = []iface.Attachment{{Filename: c.in, ContentType: "application/pdf", Data: pdf}}
		got := buildMIMEMessageAt(goldenProfile(), msg, mimeGoldenAt)
		if !strings.Contains(got, "filename*=UTF-8''") && !strings.Contains(got, "filename*0*=UTF-8''") {
			t.Fatalf("missing RFC 2231 filename*: %s", got)
		}
		for _, line := range strings.Split(got, "\r\n") {
			if len(line) > 998 {
				t.Fatalf("line over 998 octets (%d)", len(line))
			}
		}
		m, err := mail.ReadMessage(strings.NewReader(got))
		if err != nil {
			t.Fatal(err)
		}
		_, params, _ := mime.ParseMediaType(m.Header.Get("Content-Type"))
		mr := multipart.NewReader(m.Body, params["boundary"])
		if _, err := mr.NextPart(); err != nil {
			t.Fatal(err)
		}
		att, err := mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		disp, p, err := mime.ParseMediaType(att.Header.Get("Content-Disposition"))
		if err != nil || disp != "attachment" || p["filename"] != c.want {
			t.Fatalf("disposition %q → %q %v (err %v)", att.Header.Get("Content-Disposition"), disp, p, err)
		}
		raw, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, att))
		if !bytes.Equal(raw, pdf) {
			t.Fatalf("attachment bytes changed in transit")
		}
	}
}

func TestSMTPDriver_Capabilities_Attachments(t *testing.T) {
	if !NewSMTPDriver(nil).Capabilities().Attachments {
		t.Fatal("smtp writes multipart/mixed and must report Attachments=true")
	}
}

// The 552 arrives at end-of-data (the reply to "."), which is where a real
// MTA enforces its size limit.
func TestSMTPDriver_552OnData_IsAttachmentRejected(t *testing.T) {
	for _, code := range []string{"552", "554"} {
		srv := startScriptedSMTP(t, true, map[string]string{".": code + " 5.3.4 Message size exceeds fixed limit\r\n"})
		d := NewSMTPDriver(discardLogger())
		msg := EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b",
			Attachments: []iface.Attachment{{Filename: "r.pdf", ContentType: "application/pdf", Data: []byte("%PDF-")}}}
		err := d.Send(context.Background(), scriptedProfile(srv.port()), msg)
		if !errors.Is(err, ErrAttachmentRejected) {
			t.Fatalf("%s: err = %v", code, err)
		}
		// The code-bearing diagnostic survives the classification.
		var se *SendError
		if !errors.As(err, &se) || se.Error() != "smtp op=close code="+code {
			t.Fatalf("%s: SendError = %v", code, err)
		}
		if got := describeSendError(SenderProfile{Slug: "s1"}, err); got != "sender=s1 smtp op=close code="+code {
			t.Fatalf("%s: persisted reason = %q", code, got)
		}
		if strings.Contains(err.Error(), "fixed limit") {
			t.Fatalf("server text leaked: %q", err.Error())
		}
		srv.mu.Lock()
		sawBody := len(srv.data) > 0
		srv.mu.Unlock()
		if !sawBody {
			t.Fatalf("%s: the rejection must come after the message body", code)
		}
	}
}

func TestSMTPDriver_552WithoutAttachments_NotClassified(t *testing.T) {
	srv := startScriptedSMTP(t, true, map[string]string{".": "552 5.3.4 too big\r\n"})
	err := NewSMTPDriver(discardLogger()).Send(context.Background(), scriptedProfile(srv.port()), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})
	if err == nil {
		t.Fatal("expected the 552 to fail the send")
	}
	if errors.Is(err, ErrAttachmentRejected) {
		t.Fatal("a message without attachments must not be classified as attachment rejection")
	}
	var se *SendError
	if !errors.As(err, &se) || se.Error() != "smtp op=close code=552" {
		t.Fatalf("got %v", err)
	}
}

// A 550 at end-of-data is a policy/recipient refusal, not an attachment one.
func TestSMTPDriver_550WithAttachments_NotClassified(t *testing.T) {
	srv := startScriptedSMTP(t, true, map[string]string{".": "550 5.7.1 policy\r\n"})
	msg := EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b",
		Attachments: []iface.Attachment{{Filename: "r.pdf", ContentType: "application/pdf", Data: []byte("%PDF-")}}}
	err := NewSMTPDriver(discardLogger()).Send(context.Background(), scriptedProfile(srv.port()), msg)
	if err == nil || errors.Is(err, ErrAttachmentRejected) {
		t.Fatalf("got %v", err)
	}
}
