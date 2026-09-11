package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ---- Fakes / helpers ------------------------------------------------------

// headerDriverCapture is a minimal EmailDriver double whose only job is to
// remember the last EmailMessage dispatchEmail asked it to send, so a test
// can inspect the headers the chokepoint built for it.
type headerDriverCapture struct {
	last  EmailMessage
	sends int
}

func (d *headerDriverCapture) Name() string                   { return "capture" }
func (d *headerDriverCapture) Requires() []ProfileRequirement { return nil }
func (d *headerDriverCapture) Capabilities() DriverCapabilities {
	return DriverCapabilities{ListUnsubscribeHeaders: true}
}
func (d *headerDriverCapture) Send(_ context.Context, _ SenderProfile, msg EmailMessage) error {
	d.sends++
	d.last = msg
	return nil
}

// newHeaderKit builds a NotificationService whose driver only records what
// dispatchEmail decided to send it, with public_api_base_url set to base.
// unsub and tmpl are returned so a test can inspect issuance counts and
// exercise the templated path.
func newHeaderKit(t *testing.T, base string) (svc *NotificationService, driver *headerDriverCapture, unsub *fakeUnsubService, tmpl *fakeTemplateService) {
	t.Helper()
	driver = &headerDriverCapture{}
	unsub = &fakeUnsubService{token: "raw-token"}
	tmpl = &fakeTemplateService{}
	resolver := &fakeResolver{profile: SenderProfile{Slug: "default", Provider: "capture", Categories: []string{"*"}}}
	svc = NewNotificationService(
		newFakeNotifRepo(), tmpl, &fakePrefService{can: true}, unsub,
		resolver, NewDriverRegistry(driver), discardLogger(),
		Options{PublicAPIBaseURL: base},
	)
	svc.SetOptouts(&fakeOptouts{}) // neutral: nobody opted out — these tests are about headers, not opt-outs
	return svc, driver, unsub, tmpl
}

// newHeaderTestService is the two-collaborator shape the header tests below
// need when they never touch the templated path or the unsub fake directly.
func newHeaderTestService(t *testing.T, base string) (*NotificationService, *headerDriverCapture) {
	t.Helper()
	svc, driver, _, _ := newHeaderKit(t, base)
	return svc, driver
}

// ---- Tests ------------------------------------------------------------

func TestDispatch_TransactionalCarriesNoUnsubscribeHeaders(t *testing.T) {
	svc, driver := newHeaderTestService(t, "https://api.example")

	if _, err := svc.Send(context.Background(), transactionalTo("ada@example.test")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if driver.last.Headers["List-Unsubscribe"] != "" || driver.last.Headers["List-Unsubscribe-Post"] != "" {
		t.Fatalf("a transactional email must not carry unsubscribe headers: %v", driver.last.Headers)
	}
}

func TestDispatch_MarketingCarriesBothHeadersBuiltOnTheConfiguredBase(t *testing.T) {
	svc, driver := newHeaderTestService(t, "https://api.example/")

	if _, err := svc.Send(context.Background(), marketingTo("ada@example.test")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	lu := driver.last.Headers["List-Unsubscribe"]
	if !strings.HasPrefix(lu, "<https://api.example/v1/notifications/unsubscribe?token=") || !strings.HasSuffix(lu, ">") {
		// The config's trailing slash must not double up in the URL.
		t.Fatalf("malformed List-Unsubscribe: %q", lu)
	}
	if got := driver.last.Headers["List-Unsubscribe-Post"]; got != "List-Unsubscribe=One-Click" {
		t.Fatalf("wrong List-Unsubscribe-Post: %q", got)
	}
}

// Both entry points funnel through the same chokepoint (dispatchEmail), and
// that placement is the whole point: a header wired into one and not the
// other would be a marketing path that silently ships without an
// unsubscribe. These two tests prove SendTemplated independently, mirroring
// the two Send-path tests above.

func TestDispatch_SendTemplatedTransactionalCarriesNoUnsubscribeHeaders(t *testing.T) {
	svc, driver, _, tmpl := newHeaderKit(t, "https://api.example")
	tmpl.tmpl = &models.TemplateDoc{TemplateID: "tpl", Locale: "en"}

	_, err := svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		TemplateID: "tpl",
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: "ada@example.test"}},
	})
	if err != nil {
		t.Fatalf("SendTemplated: %v", err)
	}
	if driver.last.Headers["List-Unsubscribe"] != "" || driver.last.Headers["List-Unsubscribe-Post"] != "" {
		t.Fatalf("a transactional templated email must not carry unsubscribe headers: %v", driver.last.Headers)
	}
}

func TestDispatch_SendTemplatedMarketingCarriesBothHeaders(t *testing.T) {
	svc, driver, _, tmpl := newHeaderKit(t, "https://api.example/")
	tmpl.tmpl = &models.TemplateDoc{TemplateID: "tpl", Locale: "en"}

	_, err := svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		TemplateID: "tpl",
		Type:       models.TypeMarketing,
		Recipients: []iface.Recipient{{Address: "ada@example.test"}},
	})
	if err != nil {
		t.Fatalf("SendTemplated: %v", err)
	}
	lu := driver.last.Headers["List-Unsubscribe"]
	if !strings.HasPrefix(lu, "<https://api.example/v1/notifications/unsubscribe?token=") || !strings.HasSuffix(lu, ">") {
		t.Fatalf("malformed List-Unsubscribe from SendTemplated: %q", lu)
	}
	if got := driver.last.Headers["List-Unsubscribe-Post"]; got != "List-Unsubscribe=One-Click" {
		t.Fatalf("wrong List-Unsubscribe-Post from SendTemplated: %q", got)
	}
}

// One token per send, proven by counting IssueToken calls — not by
// inferring it from the token value, since the fake always returns the same
// string whether it is called once or twice.

func TestDispatch_SendMarketingIssuesExactlyOneToken(t *testing.T) {
	svc, _, unsub, _ := newHeaderKit(t, "https://api.example")

	if _, err := svc.Send(context.Background(), marketingTo("ada@example.test")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if unsub.issueN != 1 {
		t.Fatalf("expected exactly one token issuance for a non-templated marketing send, got %d", unsub.issueN)
	}
}

// The non-templated Send path has no template footer to carry
// UnsubscribeContext through, so dispatchEmail must thread req.UnsubscribeContext
// into the token it mints itself — otherwise a token minted on this path
// would be silently mis-attributed to no producer context at all. The
// shared fake normally discards this argument (it is the "_" 5th
// parameter almost everywhere else), so this test makes it capture it.
func TestDispatch_SendMarketingThreadsUnsubscribeContextToIssueToken(t *testing.T) {
	svc, _, unsub, _ := newHeaderKit(t, "https://api.example")

	req := marketingTo("ada@example.test")
	req.UnsubscribeContext = "campaign-42"
	if _, err := svc.Send(context.Background(), req); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if unsub.lastContext != "campaign-42" {
		t.Fatalf("UnsubscribeContext must reach IssueToken on the non-templated path, got %q", unsub.lastContext)
	}
}

func TestDispatch_SendTemplatedMarketingReusesTheFooterToken(t *testing.T) {
	svc, driver, unsub, tmpl := newHeaderKit(t, "https://api.example")
	tmpl.tmpl = &models.TemplateDoc{TemplateID: "tpl", Locale: "en"}

	_, err := svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		TemplateID: "tpl",
		Type:       models.TypeMarketing,
		Recipients: []iface.Recipient{{Address: "ada@example.test"}},
	})
	if err != nil {
		t.Fatalf("SendTemplated: %v", err)
	}
	if unsub.issueN != 1 {
		t.Fatalf("expected exactly one token issuance for a templated marketing send (footer link + header must share it), got %d", unsub.issueN)
	}
	if lu := driver.last.Headers["List-Unsubscribe"]; !strings.Contains(lu, "token=raw-token") {
		t.Fatalf("List-Unsubscribe must carry the same token the footer link used: %q", lu)
	}
}

// The raw token must never reach a log line at any level.

func TestDispatch_RawTokenNeverReachesTheLog(t *testing.T) {
	var buf bytes.Buffer
	driver := &headerDriverCapture{}
	unsub := &fakeUnsubService{token: "super-secret-raw-token"}
	resolver := &fakeResolver{profile: SenderProfile{Slug: "default", Provider: "capture", Categories: []string{"*"}}}
	svc := NewNotificationService(
		newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, unsub,
		resolver, NewDriverRegistry(driver),
		slog.New(slog.NewTextHandler(&buf, nil)),
		Options{PublicAPIBaseURL: "https://api.example"},
	)
	svc.SetOptouts(&fakeOptouts{})

	if _, err := svc.Send(context.Background(), marketingTo("ada@example.test")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(driver.last.Headers["List-Unsubscribe"], "super-secret-raw-token") {
		t.Fatal("sanity check failed: the header should carry the raw token")
	}
	if strings.Contains(buf.String(), "super-secret-raw-token") {
		t.Fatalf("the raw token must never reach a log line: %s", buf.String())
	}
}

// SendTemplated's own token-issuance failure (for the footer {{.UnsubscribeURL}})
// is a sibling of dispatchEmail's — both must give the same guarantee: the
// underlying repository error, which can embed document field values, is
// never logged, only a bounded, non-secret marker.
func TestSendTemplated_TokenIssuanceFailureDoesNotLogTheRawRepositoryError(t *testing.T) {
	var buf bytes.Buffer
	unsub := &fakeUnsubService{tokenErr: errors.New(`mongo: E11000 duplicate key error collection: orkestra.tokens index: tokenHash_1 dup key: { tokenHash: "leaked-detail-should-not-appear" }`)}
	tmpl := &fakeTemplateService{tmpl: &models.TemplateDoc{TemplateID: "tpl", Locale: "en"}}
	resolver := &fakeResolver{profile: SenderProfile{Slug: "default", Provider: "capture", Categories: []string{"*"}}}
	driver := &headerDriverCapture{}
	svc := NewNotificationService(
		newFakeNotifRepo(), tmpl, &fakePrefService{can: true}, unsub,
		resolver, NewDriverRegistry(driver),
		slog.New(slog.NewTextHandler(&buf, nil)),
		Options{},
	)
	svc.SetOptouts(&fakeOptouts{})

	_, err := svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		TemplateID: "tpl",
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: "ada@example.test"}},
	})
	if err != nil {
		t.Fatalf("SendTemplated: %v", err)
	}
	if strings.Contains(buf.String(), "leaked-detail-should-not-appear") {
		t.Fatalf("a failed token issuance must not log the raw repository error: %s", buf.String())
	}
}

// A marketing send must never emit a malformed or unsafe header. For a base
// that is simply not configured (empty, or missing an https scheme), Task 7
// does not refuse the send — that is Task 8's job (require_one_click_unsubscribe,
// enforced at save time and via IsConfiguredFor); here the header is just
// omitted, same as it would be for a driver that cannot place headers on
// the wire at all. An embedded CRLF is different: it is a header-injection
// attempt in operator-typed config, and this task owns refusing that send
// outright (see the task report for why refuse-to-send was chosen over
// refuse-to-save).

func TestDispatch_MarketingSendsWithoutAHeaderWhenBaseIsNotConfigured(t *testing.T) {
	cases := []struct {
		name string
		base string
	}{
		{"empty", ""},
		{"no scheme, would otherwise produce a relative URL", "api.example"},
		{"plain http, not https", "http://api.example"},
		{"https scheme with no host", "https://"},
		{"scheme with no host, empty path", "https:///"},
		// A query string would swallow this function's own "?token=" into
		// an unrelated value, or (with a leading "&") merge into the
		// existing one — either way the raw token no longer sits in a
		// "token" parameter a mail client's one-click handler can find.
		{"existing query string", "https://api.example?x=1"},
		{"existing fragment", "https://api.example#section"},
		// A path relocates "/v1/notifications/..." to somewhere that is
		// not this API's actual route.
		{"existing path", "https://api.example/some/path"},
		// TrimSuffix only ever strips ONE trailing slash; a double slash
		// must be caught before that point or it survives into the header
		// as "//v1/...".
		{"double trailing slash", "https://api.example//"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, driver := newHeaderTestService(t, c.base)

			res, err := svc.Send(context.Background(), marketingTo("ada@example.test"))
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			if res.Status != models.StatusSent {
				t.Fatalf("status = %q, want sent", res.Status)
			}
			if driver.sends != 1 {
				t.Fatalf("expected the send to go through, got %d driver sends", driver.sends)
			}
			if driver.last.Headers["List-Unsubscribe"] != "" || driver.last.Headers["List-Unsubscribe-Post"] != "" {
				t.Fatalf("an unconfigured base must not produce a malformed or relative header: %v", driver.last.Headers)
			}
		})
	}
}

func TestDispatch_MarketingRefusesToSendWithAnEmbeddedCRLF(t *testing.T) {
	svc, driver := newHeaderTestService(t, "https://api.example\r\nX-Injected: 1")

	_, err := svc.Send(context.Background(), marketingTo("ada@example.test"))
	if err == nil {
		t.Fatal("expected the send to be refused")
	}
	if !errors.Is(err, iface.ErrSenderInvalid) {
		t.Fatalf("expected iface.ErrSenderInvalid, got %v", err)
	}
	if driver.sends != 0 {
		t.Fatal("the driver must never be reached when the configured base carries a header-injection attempt")
	}
}

func TestDispatch_MarketingRefusesToSendWhenTokenIssuanceFails(t *testing.T) {
	svc, driver, unsub, _ := newHeaderKit(t, "https://api.example")
	unsub.tokenErr = errors.New("token store down")

	_, err := svc.Send(context.Background(), marketingTo("ada@example.test"))
	if err == nil {
		t.Fatal("expected the send to be refused when a token cannot be issued")
	}
	if !errors.Is(err, ErrUnsubscribeTokenUnavailable) {
		t.Fatalf("expected ErrUnsubscribeTokenUnavailable, got %v", err)
	}
	if driver.sends != 0 {
		t.Fatal("the driver must never be reached without a valid unsubscribe token")
	}
}
