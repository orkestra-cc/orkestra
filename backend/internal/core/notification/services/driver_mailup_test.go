package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
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
