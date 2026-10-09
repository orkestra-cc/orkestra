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
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

const smtp2goOK = `{"request_id":"r-1","data":{"succeeded":1,"failed":0,"failures":[],"email_id":"1abc-2def"}}`

func smtp2goProfile() SenderProfile {
	return SenderProfile{Slug: "smtp2go-sistema", Provider: "smtp2go", FromAddress: "sys@example.com", FromName: "Sistema",
		ReplyTo: "help@example.com", SMTP2GOAPIKey: "api-hunter2-secret", SMTP2GORegion: "eu"}
}

// smtp2goServer points every region at one httptest server and records the
// region the driver asked for, so a test can assert the routing without DNS.
func smtp2goServer(t *testing.T, handler http.HandlerFunc) (EmailDriver, *string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	var region string
	endpoint := func(r string) (string, bool) {
		region = r
		if _, ok := smtp2goEndpoint(r); !ok {
			return "", false
		}
		return srv.URL + "/v3/email/send", true
	}
	return newSMTP2GODriver(discardLogger(), endpoint, &http.Client{Timeout: 2 * time.Second}), &region
}

func okHandler(capture *[]byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			*capture, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(smtp2goOK))
	}
}

func TestSMTP2GODriver_Requires(t *testing.T) {
	d := NewSMTP2GODriver(nil)
	if d.Name() != "smtp2go" {
		t.Fatal(d.Name())
	}
	want := map[string]bool{SubFromAddress: false, SubSMTP2GOAPIKey: true}
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
	if err := ValidateProfile(d, SenderProfile{FromAddress: "f"}, SaveTimeView); err != nil {
		t.Fatalf("save-time view must accept a secret-only gap: %v", err)
	}
	if err := ValidateProfile(d, SenderProfile{FromAddress: "f"}, RuntimeView); !errors.Is(err, ErrSenderNotConfigured) {
		t.Fatalf("runtime view must reject a missing API key: %v", err)
	}
}

// TestSMTP2GODriver_Capabilities: custom_headers being accepted is not proof
// that List-Unsubscribe reaches the inbox DKIM-signed — the same position
// the mailup driver takes until a real-mailbox release-gate test proves it.
func TestSMTP2GODriver_Capabilities(t *testing.T) {
	c := NewSMTP2GODriver(nil).Capabilities()
	if c.ListUnsubscribeHeaders {
		t.Fatal("smtp2go must report ListUnsubscribeHeaders=false until a real send proves otherwise")
	}
	if !c.Attachments {
		t.Fatal("smtp2go carries attachments")
	}
}

func TestSMTP2GOEndpoint_Regions(t *testing.T) {
	cases := map[string]string{
		"":       "https://api.smtp2go.com/v3/email/send",
		"global": "https://api.smtp2go.com/v3/email/send",
		"eu":     "https://eu-api.smtp2go.com/v3/email/send",
		"us":     "https://us-api.smtp2go.com/v3/email/send",
		"au":     "https://au-api.smtp2go.com/v3/email/send",
	}
	for region, want := range cases {
		got, ok := smtp2goEndpoint(region)
		if !ok || got != want {
			t.Fatalf("region %q: got %q ok=%v, want %q", region, got, ok, want)
		}
	}
	if _, ok := smtp2goEndpoint("mars"); ok {
		t.Fatal("an unknown region must not resolve to any host")
	}
}

func TestSMTP2GODriver_UnknownRegionRefusedBeforeRequest(t *testing.T) {
	called := false
	d, _ := smtp2goServer(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	p := smtp2goProfile()
	p.SMTP2GORegion = "mars"
	err := d.Send(context.Background(), p, EmailMessage{To: "a@example.com"})
	var inc *ProfileIncompleteError
	if !errors.As(err, &inc) || len(inc.Missing) != 1 || inc.Missing[0] != SubSMTP2GORegion {
		t.Fatalf("err = %v, want ProfileIncompleteError naming %s", err, SubSMTP2GORegion)
	}
	if called {
		t.Fatal("no request may be made for an unusable region")
	}
}

// TestSMTP2GODriver_UnknownRegionIsNotReady: the config plane does not
// enforce enum options, so a region the driver cannot use must fail the
// shared ValidateProfile seam — the one save, activation, readiness and send
// all read — never only Send, or the profile would look ready and fail
// every message.
func TestSMTP2GODriver_UnknownRegionIsNotReady(t *testing.T) {
	d := NewSMTP2GODriver(nil)
	for _, view := range []RequirementView{SaveTimeView, RuntimeView} {
		err := ValidateProfile(d, SenderProfile{FromAddress: "f", SMTP2GOAPIKey: "k", SMTP2GORegion: "mars"}, view)
		var inc *ProfileIncompleteError
		if !errors.As(err, &inc) || len(inc.Missing) != 1 || inc.Missing[0] != SubSMTP2GORegion {
			t.Fatalf("view %d: err = %v, want ProfileIncompleteError naming %s", view, err, SubSMTP2GORegion)
		}
	}
	for _, region := range []string{"", "global", "eu"} {
		if err := ValidateProfile(d, SenderProfile{FromAddress: "f", SMTP2GOAPIKey: "k", SMTP2GORegion: region}, RuntimeView); err != nil {
			t.Fatalf("region %q must be usable: %v", region, err)
		}
	}
}

func TestSMTP2GODriver_RequestShapeAndSuccess(t *testing.T) {
	var raw []byte
	var method, path, ctype, auth, apiKey string
	d, region := smtp2goServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, ctype = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		auth, apiKey = r.Header.Get("Authorization"), r.Header.Get("X-Smtp2go-Api-Key")
		okHandler(&raw)(w, r)
	})
	err := d.Send(context.Background(), smtp2goProfile(), EmailMessage{
		To: "alice@example.com", ToName: "Alice", Subject: "Hi", BodyText: "text", BodyHTML: "<p>html</p>", Category: "crm.campaign",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if *region != "eu" {
		t.Fatalf("region = %q, want eu", *region)
	}
	if method != http.MethodPost || path != "/v3/email/send" || ctype != "application/json" {
		t.Fatalf("method=%s path=%s ctype=%s", method, path, ctype)
	}
	if apiKey != "api-hunter2-secret" || auth != "" {
		t.Fatalf("the key rides in X-Smtp2go-Api-Key only: key=%q auth=%q", apiKey, auth)
	}
	if bytes.Contains(raw, []byte("api-hunter2-secret")) || bytes.Contains(raw, []byte("api_key")) {
		t.Fatalf("the API key must never be in the body: %s", raw)
	}
	var got smtp2goRequest
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Sender != `"Sistema" <sys@example.com>` {
		t.Fatalf("sender = %q", got.Sender)
	}
	if len(got.To) != 1 || got.To[0] != `"Alice" <alice@example.com>` {
		t.Fatalf("to = %q", got.To)
	}
	if got.Subject != "Hi" || got.TextBody != "text" || got.HTMLBody != "<p>html</p>" {
		t.Fatalf("content: %+v", got)
	}
	if len(got.CustomHeaders) != 1 || got.CustomHeaders[0] != (smtp2goHeader{Header: "Reply-To", Value: "help@example.com"}) {
		t.Fatalf("Reply-To must ride as a custom header: %+v", got.CustomHeaders)
	}
}

func TestSMTP2GODriver_AddressFormatting(t *testing.T) {
	var raw []byte
	d, _ := smtp2goServer(t, okHandler(&raw))
	p := smtp2goProfile()
	p.FromName = "Rossi, Mario"
	if err := d.Send(context.Background(), p, EmailMessage{To: "bob@example.com"}); err != nil {
		t.Fatal(err)
	}
	var got smtp2goRequest
	_ = json.Unmarshal(raw, &got)
	if got.Sender != `"Rossi, Mario" <sys@example.com>` {
		t.Fatalf("a name with a comma must be quoted: %q", got.Sender)
	}
	if len(got.To) != 1 || got.To[0] != "<bob@example.com>" {
		t.Fatalf("a nameless recipient is a bare angle address: %q", got.To)
	}
}

func TestSMTP2GODriver_MapsHeadersToCustomHeaders(t *testing.T) {
	var raw []byte
	d, _ := smtp2goServer(t, okHandler(&raw))
	err := d.Send(context.Background(), smtp2goProfile(), EmailMessage{
		To: "alice@example.com", Subject: "Hi", BodyText: "text",
		Headers: map[string]string{
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
			"List-Unsubscribe":      "<https://api.example/v1/notifications/unsubscribe?token=abc>",
			"From":                  "spoof@example.com",            // reserved: dropped
			"X-Injected":            "a\r\nBcc: victim@example.com", // CR/LF: dropped
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var got smtp2goRequest
	_ = json.Unmarshal(raw, &got)
	want := []smtp2goHeader{
		{Header: "List-Unsubscribe", Value: "<https://api.example/v1/notifications/unsubscribe?token=abc>"},
		{Header: "List-Unsubscribe-Post", Value: "List-Unsubscribe=One-Click"},
		{Header: "Reply-To", Value: "help@example.com"},
	}
	if len(got.CustomHeaders) != len(want) {
		t.Fatalf("custom_headers = %+v, want %+v", got.CustomHeaders, want)
	}
	for i, h := range want {
		if got.CustomHeaders[i] != h {
			t.Fatalf("custom_headers[%d] = %+v, want %+v (order must be deterministic)", i, got.CustomHeaders[i], h)
		}
	}
}

func TestSMTP2GODriver_OptionalKeysOmitted(t *testing.T) {
	var raw []byte
	d, _ := smtp2goServer(t, okHandler(&raw))
	p := smtp2goProfile()
	p.ReplyTo = ""
	if err := d.Send(context.Background(), p, EmailMessage{To: "a@example.com", Subject: "s", BodyText: "plain text only"}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"html_body", "custom_headers", "attachments"} {
		if bytes.Contains(raw, []byte(`"`+key+`"`)) {
			t.Fatalf("%s must be omitted when empty: %s", key, raw)
		}
	}
}

func TestSMTP2GODriver_AttachmentsInPayload(t *testing.T) {
	var raw []byte
	d, _ := smtp2goServer(t, okHandler(&raw))
	err := d.Send(context.Background(), smtp2goProfile(), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b",
		Attachments: []iface.Attachment{{Filename: "r.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1")}}})
	if err != nil {
		t.Fatal(err)
	}
	var got smtp2goRequest
	_ = json.Unmarshal(raw, &got)
	want := smtp2goAttachment{Filename: "r.pdf", Fileblob: base64.StdEncoding.EncodeToString([]byte("%PDF-1")), Mimetype: "application/pdf"}
	if len(got.Attachments) != 1 || got.Attachments[0] != want {
		t.Fatalf("attachments = %+v, want %+v", got.Attachments, want)
	}
}

// Success is an allowlist: everything that is not (2xx ∧ within limit ∧
// parses ∧ no error_code ∧ succeeded==1 ∧ failed==0) fails with a bounded
// diagnostic. SMTP2GO answers 200 for a send whose recipient failed, so the
// counters — not the status — are the verdict.
func TestSMTP2GODriver_FailureTable(t *testing.T) {
	secret := "api-hunter2-secret"
	cases := []struct {
		name     string
		status   int
		ctype    string
		body     string
		wantDiag string
	}{
		{"error envelope", 400, "application/json",
			`{"request_id":"r","data":{"error_code":"E_ApiResponseCodes.NON_VALIDATING_IN_PAYLOAD","error":"sender sys@example.com not verified ` + secret + `"}}`,
			"http=400 status=error code=E_ApiResponseCodes.NON_VALIDATING_IN_PAYLOAD"},
		{"unauthorized", 401, "application/json", `{"data":{"error_code":"E_ApiResponseCodes.API_KEY_INVALID","error":"bad key"}}`,
			"http=401 status=error code=E_ApiResponseCodes.API_KEY_INVALID"},
		{"200 with a failed recipient", 200, "application/json",
			`{"data":{"succeeded":0,"failed":1,"failures":["alice@example.com rejected"]}}`, "http=200 status=not_accepted code="},
		{"200 with an error code", 200, "application/json", `{"data":{"succeeded":1,"failed":0,"error_code":"X"}}`, "http=200 status=error code=X"},
		{"200 missing counters", 200, "application/json", `{"request_id":"r","data":{}}`, "http=200 status=not_accepted code="},
		{"2xx not 200 still needs counters", 202, "application/json", `{}`, "http=202 status=not_accepted code="},
		{"non-2xx with success counters", 500, "application/json", `{"data":{"succeeded":1,"failed":0}}`, "http=500 status=not_accepted code="},
		{"empty body", 200, "application/json", ``, "http=200 body=unparseable bytes=0 type=application/json"},
		{"html error page", 502, "text/html; charset=utf-8", "<html>gateway</html>", "http=502 body=unparseable bytes=20 type=invalid"},
		{"data not an object", 200, "application/json", `{"data":"oops"}`, "http=200 body=unparseable bytes=15 type=application/json"},
		{"error code carrying a sentence", 400, "application/json", `{"data":{"error_code":"the key ` + secret + ` is wrong"}}`, "http=400 status=error code=invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, _ := smtp2goServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", c.ctype)
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			})
			err := d.Send(context.Background(), smtp2goProfile(), EmailMessage{To: "a@example.com"})
			var se *SendError
			if !errors.As(err, &se) {
				t.Fatalf("want *SendError, got %v", err)
			}
			if se.Error() != c.wantDiag {
				t.Fatalf("diagnostic = %q, want %q", se.Error(), c.wantDiag)
			}
			for _, leak := range []string{secret, "not verified", "alice@example.com", "bad key", "<html>"} {
				if strings.Contains(err.Error(), leak) {
					t.Fatalf("remote text leaked (%q): %q", leak, err.Error())
				}
			}
		})
	}
}

func TestSMTP2GODriver_OversizedBodyIsNotParsed(t *testing.T) {
	d, _ := smtp2goServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		chunk := []byte(strings.Repeat("<b>", 1024))
		for i := 0; i < 5*1024; i++ { // ~15 MB
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	err := d.Send(context.Background(), smtp2goProfile(), EmailMessage{To: "a@example.com"})
	var se *SendError
	if !errors.As(err, &se) || se.Error() != "http=200 body=too_large" {
		t.Fatalf("got %v", err)
	}
}

func TestSMTP2GODriver_TimeoutAndRefusedProfile(t *testing.T) {
	d, _ := smtp2goServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(time.Second) })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := d.Send(ctx, smtp2goProfile(), EmailMessage{To: "a@example.com"})
	var se *SendError
	if !errors.As(err, &se) || se.Error() != "smtp2go err=timeout" {
		t.Fatalf("got %v", err)
	}
	p := smtp2goProfile()
	p.SMTP2GOAPIKey = ""
	if err := d.Send(context.Background(), p, EmailMessage{To: "a@example.com"}); !errors.Is(err, ErrSenderNotConfigured) {
		t.Fatalf("incomplete profile must be refused before any request: %v", err)
	}
}

// TestSMTP2GODriver_AttachmentRejection_StatusTable: SMTP2GO answers 400 to
// every malformed or refused request (unverified sender, bad field) and
// documents no attachment-specific error code, so a 4xx says nothing about
// the attachment. Only 413 — the payload itself was too large — is read as
// a verdict on it; ErrAttachmentRejected is final for callers, so a wrong
// positive would turn a fixable configuration error into a permanent one.
func TestSMTP2GODriver_AttachmentRejection_StatusTable(t *testing.T) {
	pdf := []iface.Attachment{{Filename: "r.pdf", ContentType: "application/pdf", Data: []byte("%PDF-")}}
	envelope := `{"data":{"error_code":"x"}}`
	cases := []struct {
		name        string
		status      int
		body        string
		attachments []iface.Attachment
		rejected    bool
	}{
		{"413 with attachments", http.StatusRequestEntityTooLarge, envelope, pdf, true},
		// A proxy in front of the API answers 413 with an HTML page or
		// nothing: the verdict is the status, not whether the body parses.
		{"413 html page with attachments", http.StatusRequestEntityTooLarge, "<html>too large</html>", pdf, true},
		{"413 empty body with attachments", http.StatusRequestEntityTooLarge, "", pdf, true},
		{"413 without attachments", http.StatusRequestEntityTooLarge, envelope, nil, false},
		{"400 with attachments", http.StatusBadRequest, envelope, pdf, false},
		{"401 with attachments", http.StatusUnauthorized, envelope, pdf, false},
		{"429 with attachments", http.StatusTooManyRequests, envelope, pdf, false},
		{"500 with attachments", http.StatusInternalServerError, envelope, pdf, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := smtp2goServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			err := d.Send(context.Background(), smtp2goProfile(), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b", Attachments: tc.attachments})
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := errors.Is(err, ErrAttachmentRejected); got != tc.rejected {
				t.Fatalf("attachment_rejected = %v, want %v (err = %v)", got, tc.rejected, err)
			}
			var se *SendError
			if !errors.As(err, &se) || se.HTTP != tc.status {
				t.Fatalf("normal SendError classification lost: %v", err)
			}
		})
	}
}
