package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func mailUpProfile() SenderProfile {
	return SenderProfile{Slug: "mailup-sistema", Provider: "mailup", FromAddress: "sys@example.com", FromName: "Sistema",
		ReplyTo: "help@example.com", MailUpUser: "s12345_67", MailUpSecret: "hunter2-secret"}
}

func mailUpServer(t *testing.T, handler http.HandlerFunc) (EmailDriver, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newMailUpDriver(discardLogger(), srv.URL, &http.Client{Timeout: 2 * time.Second}), srv
}

func TestMailUpDriver_Requires(t *testing.T) {
	d := NewMailUpDriver(nil)
	if d.Name() != "mailup" {
		t.Fatal(d.Name())
	}
	want := map[string]bool{SubFromAddress: false, SubMailUpUser: false, SubMailUpSecret: true}
	for _, r := range d.Requires() {
		secret, ok := want[r.Key]
		if !ok || secret != r.Secret {
			t.Fatalf("unexpected requirement %+v", r)
		}
		delete(want, r.Key)
	}
	if len(want) != 0 {
		t.Fatalf("missing requirements: %v", want)
	}
	if err := ValidateProfile(d, SenderProfile{FromAddress: "f", MailUpUser: "u"}, SaveTimeView); err != nil {
		t.Fatalf("save-time view must accept a secret-only gap: %v", err)
	}
	if err := ValidateProfile(d, SenderProfile{FromAddress: "f", MailUpUser: "u"}, RuntimeView); !errors.Is(err, ErrSenderNotConfigured) {
		t.Fatalf("runtime view must reject a missing secret: %v", err)
	}
}

// TestMailUpDriver_Capabilities: accepting ExtendedHeaders is not the same
// as delivering them — MailUp adds only its own approved headers, and
// RFC 8058 additionally needs DKIM coverage this driver cannot yet prove.
func TestMailUpDriver_Capabilities(t *testing.T) {
	if NewMailUpDriver(nil).Capabilities().ListUnsubscribeHeaders {
		t.Fatal("mailup must report ListUnsubscribeHeaders=false until a real send proves otherwise")
	}
}

func TestMailUpDriver_MapsHeadersToExtendedHeaders(t *testing.T) {
	var got mailUpRequest
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Status":"done","Code":"0","Message":"","Data":{"Id":42}}`))
	})
	err := d.Send(context.Background(), mailUpProfile(), EmailMessage{
		To: "alice@example.com", Subject: "Hi", BodyText: "text",
		Headers: map[string]string{
			"List-Unsubscribe":      "<https://api.example/v1/notifications/unsubscribe?token=abc>",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	want := []mailUpExtendedHeader{
		{N: "List-Unsubscribe", V: "<https://api.example/v1/notifications/unsubscribe?token=abc>"},
		{N: "List-Unsubscribe-Post", V: "List-Unsubscribe=One-Click"},
	}
	if len(got.ExtendedHeaders) != len(want) {
		t.Fatalf("ExtendedHeaders = %+v, want %+v", got.ExtendedHeaders, want)
	}
	for i, h := range want {
		if got.ExtendedHeaders[i] != h {
			t.Fatalf("ExtendedHeaders[%d] = %+v, want %+v (order must be deterministic)", i, got.ExtendedHeaders[i], h)
		}
	}
}

func TestMailUpDriver_OmitsExtendedHeadersWhenNoneSet(t *testing.T) {
	var raw json.RawMessage
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Status":"done","Code":"0"}`))
	})
	if err := d.Send(context.Background(), mailUpProfile(), EmailMessage{To: "a@example.com"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if strings.Contains(string(raw), "ExtendedHeaders") {
		t.Fatalf("ExtendedHeaders must be omitted from the payload when no headers are set: %s", raw)
	}
}

// TestMailUpDriver_TextOnlyBodyOmitsHtmlKey guards against regressing to an
// empty-but-present Html part: a text-only message (e.g. SendTest's
// EmailMessage{To, Subject, BodyText} with no HTML) must not put an Html key
// on the wire at all, or MailUp serves clients an empty HTML alternative next
// to the real Text part and the recipient sees a blank email.
func TestMailUpDriver_TextOnlyBodyOmitsHtmlKey(t *testing.T) {
	var raw map[string]json.RawMessage
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Status":"done","Code":"0"}`))
	})
	err := d.Send(context.Background(), mailUpProfile(), EmailMessage{
		To: "alice@example.com", Subject: "Hi", BodyText: "plain text only",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, present := raw["Html"]; present {
		t.Fatalf("Html key must be omitted from the payload for a text-only message, got: %v", raw)
	}
	var text string
	if err := json.Unmarshal(raw["Text"], &text); err != nil || text != "plain text only" {
		t.Fatalf("Text = %s, err=%v, want %q", raw["Text"], err, "plain text only")
	}
}

// TestMailUpDriver_HTMLBodyIncludesHtmlKey is the symmetric case: when the
// message carries an HTML body, the Html key must still be sent with that
// body, unchanged from before the omitempty fix.
func TestMailUpDriver_HTMLBodyIncludesHtmlKey(t *testing.T) {
	var raw map[string]json.RawMessage
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Status":"done","Code":"0"}`))
	})
	err := d.Send(context.Background(), mailUpProfile(), EmailMessage{
		To: "alice@example.com", Subject: "Hi", BodyHTML: "<p>html</p>",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, present := raw["Html"]; !present {
		t.Fatalf("Html key must be present when the message carries an HTML body, got: %v", raw)
	}
	var html mailUpHTML
	if err := json.Unmarshal(raw["Html"], &html); err != nil || html.Body != "<p>html</p>" {
		t.Fatalf("Html.Body = %+v, err=%v, want Body=%q", html, err, "<p>html</p>")
	}
}

func TestMailUpDriver_RequestShapeAndSuccess(t *testing.T) {
	var got mailUpRequest
	var method, path, ctype, auth string
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, ctype, auth = r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Status":"done","Code":"0","Message":"","Data":{"Id":42}}`))
	})
	err := d.Send(context.Background(), mailUpProfile(), EmailMessage{
		To: "alice@example.com", ToName: "Alice", Subject: "Hi", BodyText: "text", BodyHTML: "<p>html</p>", Category: "crm.campaign",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if method != http.MethodPost || path != "/" || ctype != "application/json" || auth != "" {
		t.Fatalf("method=%s path=%s ctype=%s auth=%q — auth rides in the body, never a header", method, path, ctype, auth)
	}
	if got.User.Username != "s12345_67" || got.User.Secret != "hunter2-secret" {
		t.Fatalf("SMTP+ credentials must be in the body's User field: %+v", got.User)
	}
	if got.Subject != "Hi" || got.Text != "text" || got.Html.Body != "<p>html</p>" {
		t.Fatalf("content: %+v", got)
	}
	if got.From.Email != "sys@example.com" || got.From.Name != "Sistema" || got.ReplyTo != "help@example.com" {
		t.Fatalf("identity: %+v", got)
	}
	if len(got.To) != 1 || got.To[0].Email != "alice@example.com" || got.To[0].Name != "Alice" {
		t.Fatalf("recipient: %+v", got.To)
	}
	if got.XSmtpAPI.CampaignCode != "crm.campaign" {
		t.Fatalf("CampaignCode must carry the category through: %+v", got.XSmtpAPI)
	}
}

// Success is an allowlist: everything that is not (2xx ∧ within limit ∧
// parses ∧ Status==done ∧ Code==0) fails with a bounded diagnostic.
func TestMailUpDriver_FailureTable(t *testing.T) {
	secret := "hunter2-secret"
	cases := []struct {
		name     string
		status   int
		ctype    string
		body     string
		wantDiag string
	}{
		{"non-2xx envelope", 401, "application/json", `{"Status":"error","Code":"401","Message":"Unauthorized user s12345_67 secret ` + secret + `"}`, "http=401 status=error code=401"},
		{"2xx with non-done status", 200, "application/json", `{"Status":"error","Code":"5","Message":"bad"}`, "http=200 status=error code=5"},
		{"2xx with non-zero code", 200, "application/json", `{"Status":"done","Code":"12","Message":""}`, "http=200 status=done code=12"},
		{"missing fields", 200, "application/json", `{"Data":{"Id":1}}`, "http=200 status= code="},
		{"empty body", 200, "application/json", ``, "http=200 body=unparseable bytes=0 type=application/json"},
		{"html error page", 502, "text/html; charset=utf-8", "<html>gateway</html>", "http=502 body=unparseable bytes=20 type=invalid"},
		{"code carrying a sentence", 200, "application/json", `{"Status":"done","Code":"the user ` + secret + ` is wrong"}`, "http=200 status=done code=invalid"},
		{"status over 64 chars", 200, "application/json", `{"Status":"` + strings.Repeat("s", 65) + `","Code":"0"}`, "http=200 status=invalid code=0"},
	}
	envelope := regexp.MustCompile(`^http=\d+ status=[A-Za-z0-9._-]{0,64} code=[A-Za-z0-9._-]{0,64}$`)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", c.ctype)
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			})
			err := d.Send(context.Background(), mailUpProfile(), EmailMessage{To: "a@example.com"})
			var se *SendError
			if !errors.As(err, &se) {
				t.Fatalf("want *SendError, got %v", err)
			}
			if se.Error() != c.wantDiag {
				t.Fatalf("diagnostic = %q, want %q", se.Error(), c.wantDiag)
			}
			if strings.Contains(c.body, "Status") && !strings.Contains(c.wantDiag, "unparseable") && !envelope.MatchString(se.Error()) {
				t.Fatalf("envelope diagnostics must match the shape, got %q", se.Error())
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "Unauthorized") || strings.Contains(err.Error(), "<html>") {
				t.Fatalf("remote text leaked: %q", err.Error())
			}
		})
	}
}

func TestMailUpDriver_OversizedBodyIsNotParsed(t *testing.T) {
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		chunk := []byte(strings.Repeat("<b>", 1024))
		for i := 0; i < 5*1024; i++ { // ~15 MB
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	err := d.Send(context.Background(), mailUpProfile(), EmailMessage{To: "a@example.com"})
	var se *SendError
	if !errors.As(err, &se) || se.Error() != "http=200 body=too_large" {
		t.Fatalf("got %v", err)
	}
}

func TestMailUpDriver_TimeoutAndRefusedProfile(t *testing.T) {
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(time.Second) })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := d.Send(ctx, mailUpProfile(), EmailMessage{To: "a@example.com"})
	var se *SendError
	if !errors.As(err, &se) || se.Error() != "mailup err=timeout" {
		t.Fatalf("got %v", err)
	}
	p := mailUpProfile()
	p.MailUpSecret = ""
	if err := d.Send(context.Background(), p, EmailMessage{To: "a@example.com"}); !errors.Is(err, ErrSenderNotConfigured) {
		t.Fatalf("incomplete profile must be refused before any request: %v", err)
	}
}

// TestMailUpDriver_AttachmentsInPayload: field names and Body encoding per
// the verified shape (see the mailUpRequest doc comment for the source).
func TestMailUpDriver_AttachmentsInPayload(t *testing.T) {
	var got map[string]any
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"Status":"done","Code":"0"}`))
	})
	err := d.Send(context.Background(), mailUpProfile(), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b",
		Attachments: []iface.Attachment{{Filename: "r.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1")}}})
	if err != nil {
		t.Fatal(err)
	}
	atts, _ := got["Attachments"].([]any)
	if len(atts) != 1 {
		t.Fatalf("Attachments = %v", got["Attachments"])
	}
	a := atts[0].(map[string]any)
	if a["Filename"] != "r.pdf" || a["Body"] != base64.StdEncoding.EncodeToString([]byte("%PDF-1")) {
		t.Fatalf("attachment = %v", a)
	}
}

// TestMailUpDriver_NoAttachments_KeyOmitted: a message with no attachments
// must not carry the Attachments key at all — omitempty must not be lost.
func TestMailUpDriver_NoAttachments_KeyOmitted(t *testing.T) {
	var raw []byte
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"Status":"done","Code":"0"}`))
	})
	_ = d.Send(context.Background(), mailUpProfile(), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})
	if bytes.Contains(raw, []byte("Attachments")) {
		t.Fatalf("payload without attachments must not carry the key: %s", raw)
	}
}

// TestMailUpDriver_AttachmentRejection_Classified: the verification run
// found no MailUp-specific attachment-rejection code, so the documented
// fallback applies — a 4xx on a send that carried attachments is
// classified ErrAttachmentRejected; a 4xx on a send without attachments is
// not.
func TestMailUpDriver_AttachmentRejection_Classified(t *testing.T) {
	t.Run("4xx with attachments is classified", func(t *testing.T) {
		d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"Status":"error","Code":"400"}`))
		})
		err := d.Send(context.Background(), mailUpProfile(), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b",
			Attachments: []iface.Attachment{{Filename: "r.pdf", ContentType: "application/pdf", Data: []byte("%PDF-")}}})
		if !errors.Is(err, ErrAttachmentRejected) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("4xx without attachments is not classified", func(t *testing.T) {
		d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"Status":"error","Code":"400"}`))
		})
		err := d.Send(context.Background(), mailUpProfile(), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})
		if errors.Is(err, ErrAttachmentRejected) {
			t.Fatalf("err = %v, must not be classified as an attachment rejection without attachments", err)
		}
	})
}

// TestMailUpDriver_AttachmentRejection_StatusTable: the 4xx fallback excludes
// auth (401/403), timeout (408) and throttling (429) — and every 5xx — so an
// operational blip never becomes the final attachment_rejected verdict.
func TestMailUpDriver_AttachmentRejection_StatusTable(t *testing.T) {
	cases := []struct {
		status   int
		rejected bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusRequestEntityTooLarge, true},
		{http.StatusUnprocessableEntity, true},
		{http.StatusUnsupportedMediaType, true},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusRequestTimeout, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusBadGateway, false},
		{http.StatusServiceUnavailable, false},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"Status":"error","Code":"x"}`))
			})
			err := d.Send(context.Background(), mailUpProfile(), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b",
				Attachments: []iface.Attachment{{Filename: "r.pdf", ContentType: "application/pdf", Data: []byte("%PDF-")}}})
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := errors.Is(err, ErrAttachmentRejected); got != tc.rejected {
				t.Fatalf("status %d: attachment_rejected = %v, want %v (err = %v)", tc.status, got, tc.rejected, err)
			}
			var se *SendError
			if !errors.As(err, &se) || se.HTTP != tc.status {
				t.Fatalf("status %d: normal SendError classification lost: %v", tc.status, err)
			}
		})
	}
}
