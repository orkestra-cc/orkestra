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

// newHeaderKitWithPolicy builds a NotificationService whose driver only
// records what dispatchEmail decided to send it, with public_api_base_url set
// to base and the one-click unsubscribe requirement on or waived. unsub and
// tmpl are returned so a test can inspect issuance counts and exercise the
// templated path.
func newHeaderKitWithPolicy(t *testing.T, base string, waived bool) (svc *NotificationService, driver *headerDriverCapture, unsub *fakeUnsubService, tmpl *fakeTemplateService) {
	t.Helper()
	driver = &headerDriverCapture{}
	unsub = &fakeUnsubService{token: "raw-token"}
	tmpl = &fakeTemplateService{}
	resolver := &fakeResolver{profile: SenderProfile{Slug: "default", Provider: "capture", Categories: []string{"*"}}}
	svc = NewNotificationService(
		newFakeNotifRepo(), tmpl, &fakePrefService{can: true}, unsub,
		resolver, NewDriverRegistry(driver), discardLogger(),
		Options{PublicAPIBaseURL: base, OneClickWaived: waived},
	)
	svc.SetOptouts(&fakeOptouts{}) // neutral: nobody opted out — these tests are about headers, not opt-outs
	return svc, driver, unsub, tmpl
}

// newHeaderKit is the default posture: the requirement in force.
func newHeaderKit(t *testing.T, base string) (*NotificationService, *headerDriverCapture, *fakeUnsubService, *fakeTemplateService) {
	t.Helper()
	return newHeaderKitWithPolicy(t, base, false)
}

// newHeaderTestService is the two-collaborator shape the header tests below
// need when they never touch the templated path or the unsub fake directly.
func newHeaderTestService(t *testing.T, base string) (*NotificationService, *headerDriverCapture) {
	t.Helper()
	svc, driver, _, _ := newHeaderKit(t, base)
	return svc, driver
}

// newWaivedHeaderTestService is the same, for the tests that need the posture
// of an operator who turned require_one_click_unsubscribe off.
func newWaivedHeaderTestService(t *testing.T, base string) (*NotificationService, *headerDriverCapture) {
	t.Helper()
	svc, driver, _, _ := newHeaderKitWithPolicy(t, base, true)
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

// A marketing send must never emit a malformed or unsafe header. What happens
// to the SEND depends on the requirement, and both postures are pinned below:
//
//   - require_one_click_unsubscribe ON (the default): a base nothing can be
//     built on is exactly the state the preflight exists to prevent, so the
//     send is refused rather than delivered with no way to unsubscribe.
//   - OFF: the operator accepted that consequence knowingly, so the send goes
//     out — still without a malformed or relative header, exactly as it would
//     for a driver that cannot place headers on the wire at all.
//
// An embedded CRLF is refused in either posture; it is a header-injection
// attempt in operator-typed config rather than a missing setting, and it
// keeps its own test below.

func TestDispatch_MarketingWithABaseNoHeaderCanBeBuiltOn(t *testing.T) {
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
			t.Run("requirement on: the send is refused", func(t *testing.T) {
				svc, driver := newHeaderTestService(t, c.base)

				res, err := svc.Send(context.Background(), marketingTo("ada@example.test"))
				if !errors.Is(err, iface.ErrSenderInvalid) {
					t.Fatalf("want iface.ErrSenderInvalid, got %v", err)
				}
				if res != nil && res.Status == models.StatusSent {
					t.Fatal("the send must never be recorded as sent")
				}
				if driver.sends != 0 {
					t.Fatalf("the driver must not be reached, got %d sends", driver.sends)
				}
			})
			t.Run("requirement waived: the send goes out, still without a header", func(t *testing.T) {
				svc, driver := newWaivedHeaderTestService(t, c.base)

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
		})
	}
}

func TestDispatch_MarketingRefusesToSendWithAnEmbeddedCRLF(t *testing.T) {
	const injected = "https://api.example\r\nX-Injected: 1"
	// Refused in BOTH postures: waiving the one-click requirement accepts
	// marketing without an unsubscribe header, not a header-injection
	// attempt in operator-typed config.
	for _, tc := range []struct {
		name string
		svc  func() (*NotificationService, *headerDriverCapture)
	}{
		{"requirement on", func() (*NotificationService, *headerDriverCapture) { return newHeaderTestService(t, injected) }},
		{"requirement waived", func() (*NotificationService, *headerDriverCapture) { return newWaivedHeaderTestService(t, injected) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, driver := tc.svc()

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
		})
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

// A literal '<' or '>' in the configured base is absorbed into url.Parse's
// Host with no error and no path/query/fragment — the same shape oneClickBase
// otherwise treats as usable — yet a '>' would prematurely close the
// angle-bracket delimiter RFC 8058 requires around the header value, and a
// '<' would open a second one where none belongs. Refused in BOTH postures,
// exactly like the embedded-CRLF case above: this is operator-typed config
// carrying header-syntax-breaking characters, not a missing setting.
func TestDispatch_MarketingRefusesToSendWithAnEmbeddedAngleBracket(t *testing.T) {
	const injected = "https://api.example>evil"
	for _, tc := range []struct {
		name string
		svc  func() (*NotificationService, *headerDriverCapture)
	}{
		{"requirement on", func() (*NotificationService, *headerDriverCapture) { return newHeaderTestService(t, injected) }},
		{"requirement waived", func() (*NotificationService, *headerDriverCapture) { return newWaivedHeaderTestService(t, injected) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, driver := tc.svc()

			_, err := svc.Send(context.Background(), marketingTo("ada@example.test"))
			if err == nil {
				t.Fatal("expected the send to be refused")
			}
			if !errors.Is(err, iface.ErrSenderInvalid) {
				t.Fatalf("expected iface.ErrSenderInvalid, got %v", err)
			}
			if driver.sends != 0 {
				t.Fatal("the driver must never be reached when the configured base carries an unescaped angle bracket")
			}
		})
	}
}

// ---- unsubscribe_page_url: the footer link a person clicks ---------------
//
// There are two different affordances in a marketing email, and these tests
// keep them straight. The List-Unsubscribe header is for the mail client's
// own one-click button — there is no browser involved, so it must always
// name the API's POST endpoint, never a page. The footer link is for a
// person who scrolls down and clicks; when a hosted page is configured, that
// link — {{.UnsubscribeURL}} in a template's footer — points there instead,
// with the raw token in the URL fragment so it never reaches the page's
// access logs.

// newHeaderKitWithPage is newHeaderKit's sibling: same shape, but also wires
// unsubscribe_page_url so a test can inspect what {{.UnsubscribeURL}} became.
func newHeaderKitWithPage(t *testing.T, apiBase, pageURL string) (svc *NotificationService, driver *headerDriverCapture, unsub *fakeUnsubService, tmpl *fakeTemplateService) {
	t.Helper()
	driver = &headerDriverCapture{}
	unsub = &fakeUnsubService{token: "raw-token"}
	tmpl = &fakeTemplateService{}
	resolver := &fakeResolver{profile: SenderProfile{Slug: "default", Provider: "capture", Categories: []string{"*"}}}
	svc = NewNotificationService(
		newFakeNotifRepo(), tmpl, &fakePrefService{can: true}, unsub,
		resolver, NewDriverRegistry(driver), discardLogger(),
		Options{PublicAPIBaseURL: apiBase, UnsubscribePageURL: pageURL},
	)
	svc.SetOptouts(&fakeOptouts{})
	return svc, driver, unsub, tmpl
}

func TestSendTemplated_UnsubscribeURLPointsAtTheHostedPageWithTokenInFragmentWhenConfigured(t *testing.T) {
	cases := []struct {
		name string
		page string
	}{
		{"bare origin", "https://public.example"},
		{"a single trailing slash normalizes rather than doubling up", "https://public.example/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, driver, _, tmpl := newHeaderKitWithPage(t, "https://api.example", c.page)
			tmpl.tmpl = &models.TemplateDoc{TemplateID: "tpl", Locale: "en"}
			tmpl.rendered = &Rendered{Subject: "rendered", BodyText: "txt", BodyHTML: "<p>html</p>"}

			_, err := svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
				TemplateID: "tpl",
				Type:       models.TypeTransactional,
				Recipients: []iface.Recipient{{Address: "ada@example.test"}},
			})
			if err != nil {
				t.Fatalf("SendTemplated: %v", err)
			}
			wantSubject := "[unsub=https://public.example/u#raw-token] rendered"
			if driver.last.Subject != wantSubject {
				t.Fatalf("UnsubscribeURL = %q, want %q", driver.last.Subject, wantSubject)
			}
		})
	}
}

func TestSendTemplated_UnsubscribeURLStaysTheAPIEndpointWithoutAPageConfigured(t *testing.T) {
	svc, driver, _, tmpl := newHeaderKitWithPage(t, "https://api.example", "")
	tmpl.tmpl = &models.TemplateDoc{TemplateID: "tpl", Locale: "en"}
	tmpl.rendered = &Rendered{Subject: "rendered", BodyText: "txt", BodyHTML: "<p>html</p>"}

	_, err := svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
		TemplateID: "tpl",
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: "ada@example.test"}},
	})
	if err != nil {
		t.Fatalf("SendTemplated: %v", err)
	}
	wantSubject := "[unsub=/notifications/unsubscribe?token=raw-token] rendered"
	if driver.last.Subject != wantSubject {
		t.Fatalf("UnsubscribeURL without a configured page = %q, want %q", driver.last.Subject, wantSubject)
	}
}

// A page URL that is not a bare https origin can never work as a link — a
// query string would swallow the fragment, a path would relocate the page
// under a route it does not own, and an embedded CRLF or angle bracket is
// operator-typed config that must not slip through unexamined. All of these
// fall back to the plain API link — a clean omission, not a dead page link —
// and, crucially, none of them may ever block the send: this footer renders
// on transactional templates too, and a typo in an optional cosmetic field
// must never be able to stop a password-reset email from going out.
func TestSendTemplated_UnsubscribeURLFallsBackCleanlyOnAMalformedPageURL(t *testing.T) {
	cases := []struct {
		name string
		page string
	}{
		{"path", "https://public.example/some/path"},
		{"query string", "https://public.example?x=1"},
		{"fragment", "https://public.example#already-has-one"},
		{"double trailing slash", "https://public.example//"},
		{"not https", "http://public.example"},
		{"embedded CRLF", "https://public.example\r\nX-Injected: 1"},
		{"embedded angle bracket", "https://public.example>evil"},
		{"whitespace only", "   "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, driver, _, tmpl := newHeaderKitWithPage(t, "https://api.example", c.page)
			tmpl.tmpl = &models.TemplateDoc{TemplateID: "tpl", Locale: "en"}
			tmpl.rendered = &Rendered{Subject: "rendered", BodyText: "txt", BodyHTML: "<p>html</p>"}

			res, err := svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
				TemplateID: "tpl",
				Type:       models.TypeTransactional,
				Recipients: []iface.Recipient{{Address: "ada@example.test"}},
			})
			if err != nil {
				t.Fatalf("SendTemplated: %v", err)
			}
			if res.Status != models.StatusSent {
				t.Fatalf("a malformed unsubscribe_page_url must never block the send, got status %q", res.Status)
			}
			wantSubject := "[unsub=/notifications/unsubscribe?token=raw-token] rendered"
			if driver.last.Subject != wantSubject {
				t.Fatalf("a malformed unsubscribe_page_url must fall back to the API link rather than ship a broken one, got %q", driver.last.Subject)
			}
		})
	}
}

// The footer link must hot-reload too, for the same reason the one-click
// requirement does (see TestDispatch_TheRequirementIsReadPerSendNotCapturedAtStartup
// in preflight_unsubscribe_test.go, which this test mirrors): this module
// declares HotReloadConfig() == true, and its admin surface has no
// per-field way to say unsubscribe_page_url is the one exception in its
// group. A value captured at Init would leave an operator who just set it
// seeing a 200 with the footer still pointing at the old destination, with
// nothing anywhere saying why.
func TestSendTemplated_UnsubscribeURLIsReadPerSendNotCapturedAtStartup(t *testing.T) {
	// Starts with no page configured.
	policy := OneClickPolicy{PublicAPIBaseURL: "https://api.example"}
	driver := &headerDriverCapture{}
	unsub := &fakeUnsubService{token: "raw-token"}
	tmpl := &fakeTemplateService{
		tmpl:     &models.TemplateDoc{TemplateID: "tpl", Locale: "en"},
		rendered: &Rendered{Subject: "rendered", BodyText: "txt", BodyHTML: "<p>html</p>"},
	}
	resolver := &fakeResolver{profile: SenderProfile{Slug: "default", Provider: "capture", Categories: []string{"*"}}}
	svc := NewNotificationService(
		newFakeNotifRepo(), tmpl, &fakePrefService{can: true}, unsub,
		resolver, NewDriverRegistry(driver), discardLogger(),
		Options{
			// Deliberately contradicting the source: a stale static value
			// must never win over the live one.
			UnsubscribePageURL: "https://stale.example",
			OneClickSource:     func(context.Context) OneClickPolicy { return policy },
		},
	)
	svc.SetOptouts(&fakeOptouts{})

	send := func() {
		t.Helper()
		if _, err := svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
			TemplateID: "tpl",
			Type:       models.TypeTransactional,
			Recipients: []iface.Recipient{{Address: "ada@example.test"}},
		}); err != nil {
			t.Fatalf("SendTemplated: %v", err)
		}
	}

	// 1. No page configured yet: the footer stays the API link, proving the
	//    live source — not the stale static Options field — decided.
	// (fakeTemplateService.Render prepends onto f.rendered.Subject on every
	// call, so HasPrefix — not an exact match — is what a second send below
	// can still assert against.)
	send()
	wantNoPage := "[unsub=/notifications/unsubscribe?token=raw-token] "
	if !strings.HasPrefix(driver.last.Subject, wantNoPage) {
		t.Fatalf("before configuring a page: Subject = %q, want prefix %q", driver.last.Subject, wantNoPage)
	}

	// 2. The operator sets the page URL. The very next templated send must
	//    render the new footer, with no restart.
	policy = OneClickPolicy{PublicAPIBaseURL: "https://api.example", UnsubscribePageURL: "https://public.example"}
	send()
	wantPage := "[unsub=https://public.example/u#raw-token] "
	if !strings.HasPrefix(driver.last.Subject, wantPage) {
		t.Fatalf("after configuring a page: Subject = %q, want prefix %q", driver.last.Subject, wantPage)
	}
}

// footerCaptureTemplateService embeds the shared template fake (for Get and
// friends) but renders UnsubscribeURL directly into the message body instead
// of just the subject — the shared fake's embed-into-subject trick is enough
// for the tests above, but the test below inspects the body, which is where
// a human actually reads the footer link from.
type footerCaptureTemplateService struct {
	*fakeTemplateService
}

func (f *footerCaptureTemplateService) Render(_ *models.TemplateDoc, data map[string]any) (*Rendered, error) {
	u, _ := data["UnsubscribeURL"].(string)
	return &Rendered{Subject: "S", BodyText: "Unsubscribe: " + u, BodyHTML: "<p>Unsubscribe: " + u + "</p>"}, nil
}

// pageFooterService fronts SendTemplated behind Send's exact signature: the
// footer link only exists on the templated path (Send has no template to
// carry a footer in), so this lets the test below issue what reads like an
// ordinary marketing Send call while actually exercising the path that
// renders {{.UnsubscribeURL}}.
type pageFooterService struct {
	*NotificationService
	templateID string
}

func (p *pageFooterService) Send(ctx context.Context, req iface.NotificationRequest) (*iface.NotificationResult, error) {
	return p.SendTemplated(ctx, iface.TemplatedNotificationRequest{
		TemplateID: p.templateID,
		Type:       req.Type,
		Recipients: req.Recipients,
	})
}

// newHeaderTestServiceWithPage builds a service with both the API base URL
// (what the header is built on) and a hosted unsubscribe page URL (what the
// footer link is built on) configured, so a test can prove the two never
// point at the same place.
func newHeaderTestServiceWithPage(t *testing.T, apiBase, pageURL string) (*pageFooterService, *headerDriverCapture) {
	t.Helper()
	driver := &headerDriverCapture{}
	unsub := &fakeUnsubService{token: "raw-token"}
	tmpl := &footerCaptureTemplateService{fakeTemplateService: &fakeTemplateService{tmpl: &models.TemplateDoc{TemplateID: "tpl", Locale: "en"}}}
	resolver := &fakeResolver{profile: SenderProfile{Slug: "default", Provider: "capture", Categories: []string{"*"}}}
	svc := NewNotificationService(
		newFakeNotifRepo(), tmpl, &fakePrefService{can: true}, unsub,
		resolver, NewDriverRegistry(driver), discardLogger(),
		Options{PublicAPIBaseURL: apiBase, UnsubscribePageURL: pageURL},
	)
	svc.SetOptouts(&fakeOptouts{})
	return &pageFooterService{NotificationService: svc, templateID: "tpl"}, driver
}

func TestHeaderAlwaysPointsAtTheAPIEvenWhenAPageIsConfigured(t *testing.T) {
	svc, driver := newHeaderTestServiceWithPage(t, "https://api.example", "https://public.example")

	if _, err := svc.Send(context.Background(), marketingTo("ada@example.test")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	lu := driver.last.Headers["List-Unsubscribe"]
	if strings.Contains(lu, "public.example") {
		t.Fatalf("the header must never point at the page: the one-click flow has no browser. %q", lu)
	}
	if !strings.Contains(lu, "api.example/v1/notifications/unsubscribe") {
		t.Fatalf("the header must point at the API's POST endpoint, got %q", lu)
	}
	// The page, on the other hand, is exactly what a person clicking from the
	// footer sees, and the token reaches it in the fragment so it never ends
	// up in the page's access logs.
	if !strings.Contains(driver.last.BodyText+driver.last.BodyHTML, "https://public.example/u#") {
		t.Fatal("the footer must point at the page, with the token in the fragment")
	}
}
