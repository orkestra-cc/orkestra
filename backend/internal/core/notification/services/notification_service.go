package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// URLBuilder renders the absolute URLs used inside templates.
// It's injected so the notification module stays agnostic of FrontendURL config.
type URLBuilder func(path string) string

// Options configures the orchestrator.
type Options struct {
	AppName        string
	SupportEmail   string
	URLBuilder     URLBuilder
	DefaultLocale  string
	IdempotencyTTL time.Duration

	// PublicAPIBaseURL is the origin the RFC 8058 List-Unsubscribe header
	// points recipients' mail clients back to (dispatchEmail appends
	// "/v1/notifications/unsubscribe?token=..."). It is a config field
	// rather than something derived, because nothing else the module has
	// access to can safely stand in for it: PlatformInfo.FrontendURL()
	// names the frontend, not the API's own origin, and a request's Host
	// header is not a source to trust for a URL that will sit in a
	// recipient's mailbox for months after the request that triggered it is
	// gone. Empty by default; a marketing send never emits a malformed or
	// relative header over it — see oneClickBase for exactly what happens
	// when this is empty, non-https, or unsafe.
	//
	// Like OneClickWaived below, this field is consulted only when
	// OneClickSource is nil: in production the live source resolves this
	// value on every marketing decision, and this one stays a fallback for
	// services built directly (e.g. most tests in this package).
	PublicAPIBaseURL string

	// UnsubscribePageURL is the optional hosted page a PERSON reaches by
	// clicking the unsubscribe link in an email's footer — the
	// {{.UnsubscribeURL}} template variable SendTemplated computes. This is
	// deliberately a different field from PublicAPIBaseURL above: that one is
	// for the mail client's own automatic one-click POST, where there is no
	// browser and no human in the loop; this one is for a person who scrolls
	// to the bottom and clicks, and who deserves a page — a sentence
	// explaining what is about to happen, and a button pressed deliberately.
	//
	// Empty by default, in which case {{.UnsubscribeURL}} stays exactly what
	// it has always been: the direct API GET link built by buildURL. When
	// set, it must validate as a bare https origin — the same rule
	// oneClickBase applies to PublicAPIBaseURL — or it is treated as if it
	// were empty: a clean fallback to the API link, never a broken page URL,
	// and never a reason to refuse the send. That last point matters because
	// this value feeds every templated send's footer, transactional included
	// (see SendTemplated), so a typo in this optional, cosmetic field must
	// never be able to stop a password-reset email from going out.
	//
	// This field is consulted only as the FALLBACK oneClickPolicy uses when
	// OneClickSource is nil (services built directly, e.g. most tests in
	// this package). In production it is not captured at Init at all — like
	// PublicAPIBaseURL, it hot-reloads through OneClickSource/livePolicy,
	// because its two immediate neighbours in the "delivery" config group
	// (public_api_base_url, require_one_click_unsubscribe) already hot-reload
	// and this module's admin surface has no per-field way to tell an
	// operator that one field in the group is the exception: a value
	// captured at Init would leave someone who just set this seeing a 200
	// and a footer that keeps pointing at the old destination, with nothing
	// anywhere saying why.
	UnsubscribePageURL string

	// OneClickWaived is the INVERSE of the require_one_click_unsubscribe
	// config field, stored inverted so this struct's zero value fails
	// closed: an Options built without a thought for the requirement
	// requires one-click, rather than quietly letting marketing out without
	// an unsubscribe header. Consulted only when OneClickSource is nil.
	OneClickWaived bool

	// OneClickSource, when set, is read on every marketing decision instead
	// of the two fields above, and is how the requirement actually
	// hot-reloads. It must be a source, not a captured value: this module
	// declares HotReloadConfig() == true, so the config service records no
	// restart-required flag, and an operator who switches
	// require_one_click_unsubscribe ON would otherwise keep sending
	// marketing with no unsubscribe header until the next restart, with
	// nothing anywhere saying so. A captured value is stale in the
	// fail-OPEN direction, which is the one direction this whole rule
	// exists to prevent.
	//
	// It is consulted only on the marketing path, so a transactional send
	// never pays for it. A source that cannot read configuration must
	// return the fail-closed policy (the requirement in force), never the
	// zero value of whatever it failed to read.
	OneClickSource func(ctx context.Context) OneClickPolicy
}

// NotificationService orchestrates preferences, templates, delivery and
// logging. It satisfies iface.NotificationSender.
type NotificationService struct {
	logRepo      repository.NotificationRepository
	tmplService  TemplateService
	prefService  PreferenceService
	unsubService UnsubscribeService
	resolver     SenderResolver
	drivers      *DriverRegistry
	logger       *slog.Logger
	opts         Options

	// Nil by default. An addon pushes its implementation in at boot through
	// the setters below; the base ships neither.
	emailRewriter   iface.EmailTrackingRewriter
	unsubscribeSink iface.MarketingUnsubscribeSink

	// optouts is nil until SetOptouts wires it — the same post-construction
	// setter shape the platform uses for the audit sink / KMS provider
	// setters (see e.g. auth's PasswordAuthService.SetAuditSink), but NOT
	// the same nil semantics: a nil audit sink means "no audit rows", while
	// a nil opt-out seam would mean "ignore consent" if dispatchEmail let it
	// through. It does not — see the fail-closed branch there. Transactional
	// sends never touch this field regardless of whether it is wired.
	optouts repository.MarketingOptoutRepository
}

func NewNotificationService(
	logRepo repository.NotificationRepository,
	tmplService TemplateService,
	prefService PreferenceService,
	unsubService UnsubscribeService,
	resolver SenderResolver,
	drivers *DriverRegistry,
	logger *slog.Logger,
	opts Options,
) *NotificationService {
	if opts.DefaultLocale == "" {
		opts.DefaultLocale = "en"
	}
	if opts.IdempotencyTTL == 0 {
		opts.IdempotencyTTL = 1 * time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationService{
		logRepo:      logRepo,
		tmplService:  tmplService,
		prefService:  prefService,
		unsubService: unsubService,
		resolver:     resolver,
		drivers:      drivers,
		logger:       logger,
		opts:         opts,
	}
}

// SetOptouts wires the durable marketing opt-out repository
// post-construction. Left nil (the default), a marketing send fails closed
// with ErrOptoutLookupUnavailable rather than skipping the check — an
// unwired seam must never read as "no opt-outs exist". Transactional sends
// are unaffected either way.
func (s *NotificationService) SetOptouts(o repository.MarketingOptoutRepository) {
	s.optouts = o
}

// IsConfigured keeps its pre-ADR-0019 meaning: the default ("*") profile
// resolves and its driver accepts it. It is deliberately coarse — a caller
// about to send should ask IsConfiguredFor (PR 2, D7).
func (s *NotificationService) IsConfigured(ctx context.Context) bool {
	if s.resolver == nil || s.drivers == nil {
		return false
	}
	p, err := s.resolver.Default(ctx)
	if err != nil {
		return false
	}
	_, err = s.usableDriver(p)
	return err == nil
}

// IsConfiguredFor answers for one category: it resolves the profile that
// would carry the send and checks its driver with secrets in view — which
// the save-time gate cannot do. No match ⇒ false (fail-closed, D7).
func (s *NotificationService) IsConfiguredFor(ctx context.Context, category string) bool {
	if s.resolver == nil || s.drivers == nil {
		return false
	}
	// Same input the dispatch path builds — TenantID included — so the
	// pre-flight can never check a different profile than the send uses.
	tenantID, _ := ctxauth.GetTenantID(ctx)
	p, err := s.resolver.Resolve(ctx, ResolveInput{Category: category, TenantID: tenantID})
	if err != nil {
		return false
	}
	_, err = s.usableDriver(p)
	return err == nil
}

// usableDriver is the second and third fail-closed step: the profile's
// driver must be registered and must accept the profile with secrets in view.
func (s *NotificationService) usableDriver(p SenderProfile) (EmailDriver, error) {
	d, ok := s.drivers.Get(p.Provider)
	if !ok {
		return nil, ErrUnknownDriver
	}
	if err := ValidateProfile(d, p, RuntimeView); err != nil {
		return nil, err
	}
	return d, nil
}

// Send dispatches a fully rendered notification. Consumers that already
// produced subject+body use this; consumers that want templating use
// SendTemplated instead.
func (s *NotificationService) Send(ctx context.Context, req iface.NotificationRequest) (*iface.NotificationResult, error) {
	if req.Channel == "" {
		req.Channel = models.ChannelEmail
	}
	if req.Channel != models.ChannelEmail {
		return nil, fmt.Errorf("notification: channel %q not supported", req.Channel)
	}
	if len(req.Recipients) == 0 {
		return nil, errors.New("notification: no recipients")
	}

	if existing, _ := s.logRepo.FindByIdempotencyKey(ctx, req.IdempotencyKey, time.Now().Add(-s.opts.IdempotencyTTL)); existing != nil {
		return &iface.NotificationResult{
			ID:       existing.UUID,
			Status:   existing.Status,
			Provider: existing.Provider,
			Error:    existing.Error,
		}, nil
	}

	recipient := req.Recipients[0]
	return s.dispatchEmail(ctx, dispatchInput{
		Category:           req.Category,
		Type:               req.Type,
		Recipient:          recipient,
		Subject:            req.Subject,
		BodyText:           req.Body,
		BodyHTML:           req.BodyHTML,
		IdempotencyKey:     req.IdempotencyKey,
		TrackingContactRef: req.TrackingContactRef,
		Sender:             req.Sender,
		// Send never pre-issues an unsubscribe token (there is no template
		// footer to put it in), so dispatchEmail mints one itself for a
		// marketing send — with this context attached, exactly like
		// SendTemplated's footer token carries req.UnsubscribeContext.
		UnsubscribeContext: req.UnsubscribeContext,
	})
}

// SendTemplated resolves a template, injects automatic variables
// (unsubscribe URL, preferences URL, app metadata), renders and sends.
func (s *NotificationService) SendTemplated(ctx context.Context, req iface.TemplatedNotificationRequest) (*iface.NotificationResult, error) {
	if req.Channel == "" {
		req.Channel = models.ChannelEmail
	}
	if req.Channel != models.ChannelEmail {
		return nil, fmt.Errorf("notification: channel %q not supported", req.Channel)
	}
	if len(req.Recipients) == 0 {
		return nil, errors.New("notification: no recipients")
	}

	if existing, _ := s.logRepo.FindByIdempotencyKey(ctx, req.IdempotencyKey, time.Now().Add(-s.opts.IdempotencyTTL)); existing != nil {
		return &iface.NotificationResult{
			ID:       existing.UUID,
			Status:   existing.Status,
			Provider: existing.Provider,
			Error:    existing.Error,
		}, nil
	}

	locale := req.Locale
	if locale == "" {
		locale = s.opts.DefaultLocale
	}
	tmpl, err := s.tmplService.Get(ctx, req.TemplateID, locale)
	if err != nil {
		return nil, fmt.Errorf("notification: load template %s: %w", req.TemplateID, err)
	}

	recipient := req.Recipients[0]

	// Build template data: caller values + auto-injected footer URLs.
	data := map[string]any{}
	for k, v := range req.Data {
		data[k] = v
	}
	if _, ok := data["AppName"]; !ok {
		data["AppName"] = s.opts.AppName
	}
	if _, ok := data["SupportEmail"]; !ok {
		data["SupportEmail"] = s.opts.SupportEmail
	}

	unsubToken, err := s.unsubService.IssueToken(ctx, recipient.UserUUID, recipient.Address, req.Category, req.UnsubscribeContext)
	if err != nil {
		// err is a raw repository error and may embed document field
		// values — never logged, same guarantee dispatchEmail's own
		// token-issuance failure gives via ErrUnsubscribeTokenUnavailable.
		// category is a fixed, non-secret routing string.
		s.logger.Warn("notification: failed to issue unsubscribe token", slog.String("category", req.Category))
	}
	// The live policy read (not s.opts.UnsubscribePageURL directly) is what
	// makes the footer link hot-reload: unrelated to marketing admissibility
	// — this runs for every templated send, transactional included — but it
	// shares oneClickPolicy's plumbing so an operator who sets this field
	// sees the very next send pick it up, with no restart.
	data["UnsubscribeURL"] = s.unsubscribeURL(s.oneClickPolicy(ctx).UnsubscribePageURL, unsubToken)
	data["PreferencesURL"] = s.buildURL("/account/notifications")

	rendered, err := s.tmplService.Render(tmpl, data)
	if err != nil {
		return nil, err
	}

	return s.dispatchEmail(ctx, dispatchInput{
		Category:           req.Category,
		Type:               req.Type,
		Recipient:          recipient,
		Subject:            rendered.Subject,
		BodyText:           rendered.BodyText,
		BodyHTML:           rendered.BodyHTML,
		TemplateID:         tmpl.TemplateID,
		IdempotencyKey:     req.IdempotencyKey,
		TrackingContactRef: req.TrackingContactRef,
		Sender:             req.Sender,
		UnsubscribeContext: req.UnsubscribeContext,
		// unsubToken is the raw token already minted above for
		// {{.UnsubscribeURL}} (possibly "" if that issuance failed).
		// dispatchEmail reuses it for the marketing List-Unsubscribe header
		// instead of minting a second token for the same send.
		UnsubscribeToken: unsubToken,
	})
}

type dispatchInput struct {
	Category           string
	Type               string
	Recipient          iface.Recipient
	Subject            string
	BodyText           string
	BodyHTML           string
	TemplateID         string
	IdempotencyKey     string
	TrackingContactRef string
	// Sender optionally names the sender profile (slug) that must carry this
	// send (ADR-0021). Empty takes today's category-routed path unchanged.
	Sender string
	// UnsubscribeContext is the opaque producer context passed to IssueToken
	// when dispatchEmail must mint an unsubscribe token itself (i.e.
	// UnsubscribeToken below is empty). Ignored otherwise.
	UnsubscribeContext string
	// UnsubscribeToken carries a raw token the caller already issued for
	// {{.UnsubscribeURL}} (SendTemplated), so dispatchEmail's marketing
	// List-Unsubscribe header reuses it instead of minting a second token
	// for the same send. Empty for Send (which never pre-issues one) or
	// when the caller's own issuance failed.
	UnsubscribeToken string
}

func (s *NotificationService) dispatchEmail(ctx context.Context, in dispatchInput) (*iface.NotificationResult, error) {
	logDoc := &models.NotificationDoc{
		UUID:              uuid.Must(uuid.NewV7()).String(),
		Channel:           models.ChannelEmail,
		Type:              in.Type,
		Category:          in.Category,
		TemplateID:        in.TemplateID,
		RecipientUserUUID: in.Recipient.UserUUID,
		RecipientAddress:  in.Recipient.Address,
		Subject:           in.Subject,
		IdempotencyKey:    in.IdempotencyKey,
		CreatedAt:         time.Now(),
	}

	// Preference check (marketing only).
	canDeliver, err := s.prefService.CanDeliver(ctx, in.Recipient.UserUUID, in.Category, models.ChannelEmail, in.Type)
	if err != nil {
		s.logger.Warn("notification: preference lookup failed", slog.String("error", err.Error()))
	}
	if !canDeliver {
		logDoc.Status = models.StatusSuppressed
		logDoc.Error = "user opted out of category"
		_ = s.logRepo.Create(ctx, logDoc)
		return &iface.NotificationResult{ID: logDoc.UUID, Status: logDoc.Status}, nil
	}

	// Durable opt-out (RFC 8058 one-click). This is checked for marketing
	// only: a transactional message is not something a recipient can opt out
	// of, and must not even pay the lookup.
	//
	// Fail-closed on purpose. If we cannot tell whether this address opted
	// out, we do not send: an email withheld costs far less than one
	// delivered to someone who asked us to stop. That includes the seam
	// itself being unwired (s.optouts == nil): a nil opt-out repository is
	// not "no check", it is "ignore consent", so it takes the same refusal
	// path as a lookup error rather than silently letting marketing through.
	if in.Type == models.TypeMarketing {
		if s.optouts == nil {
			return s.failSend(ctx, logDoc, SenderProfile{}, ErrOptoutLookupUnavailable)
		}
		optedOut, err := s.optouts.IsOptedOut(ctx, in.Recipient.Address, in.Category)
		if err != nil {
			return s.failSend(ctx, logDoc, SenderProfile{}, ErrOptoutLookupUnavailable)
		}
		if optedOut {
			logDoc.Status = models.StatusSuppressed
			logDoc.Error = "marketing_optout"
			_ = s.logRepo.Create(ctx, logDoc)
			return &iface.NotificationResult{ID: logDoc.UUID, Status: logDoc.Status}, nil
		}
	}

	// An addon-provided rewriter may inject open-pixel + click trackers into
	// the rendered HTML just before transport. nil rewriter / empty ref / non-HTML
	// → unchanged. Best-effort: a panic must never break the send.
	bodyHTML := in.BodyHTML
	if s.emailRewriter != nil && bodyHTML != "" && in.TrackingContactRef != "" {
		func() {
			defer func() {
				if r := recover(); r != nil {
					s.logger.Warn("notification: email rewriter panicked", slog.Any("recover", r))
				}
			}()
			if res := s.emailRewriter.RewriteOutboundEmail(ctx, iface.OutboundEmail{
				BodyHTML:         bodyHTML,
				RecipientAddress: in.Recipient.Address,
				MessageUUID:      logDoc.UUID,
				ContactRef:       in.TrackingContactRef,
			}); res != "" {
				bodyHTML = res
			}
		}()
	}

	// Resolve → validate → send. Every failure before the driver is
	// fail-closed (D5) and still writes a failed log row, so the delivery
	// log answers which profile failed and why.
	// TenantID rides to the resolver (D4) so per-tenant routing later needs
	// no change at this chokepoint; the resolver ignores it today.
	//
	// An explicit Sender (ADR-0021) bypasses category routing and is
	// re-enforced here regardless of how the caller obtained the slug:
	// grammar/length guard → BySlug → Type ∈ AllowedTypes. Any step failing
	// writes the failed row through the same bounded-error contract.
	tenantID, _ := ctxauth.GetTenantID(ctx)
	var profile SenderProfile
	if in.Sender != "" {
		if !module.ValidSlug(in.Sender) { // already exported; enforces MaxSlugLength(64) + grammar
			logDoc.AttemptedSenderSlug = "invalid"
			return s.failSend(ctx, logDoc, SenderProfile{}, iface.ErrSenderInvalid)
		}
		logDoc.AttemptedSenderSlug = in.Sender
		profile, err = s.resolver.BySlug(ctx, in.Sender)
		switch {
		case err != nil:
			// The resolver speaks this package's sentinels; everything the
			// explicit-sender arm reports must speak iface's, exactly as
			// PreflightDelivery does. Without this a slug deleted between
			// pre-flight and send (ADR-0021 D7's mid-run profile deletion)
			// reached the log as the err=unknown catch-all and matched no
			// sentinel a consumer outside this module can name.
			err = mapBySlugErr(err)
		case !typeAllowed(profile, in.Type):
			err = iface.ErrSenderNotEligible
		}
	} else {
		profile, err = s.resolver.Resolve(ctx, ResolveInput{Category: in.Category, Type: in.Type, TenantID: tenantID})
	}
	if err != nil {
		return s.failSend(ctx, logDoc, profile, err)
	}
	driver, err := s.usableDriver(profile)
	if err != nil {
		return s.failSend(ctx, logDoc, profile, err)
	}

	// RFC 8058 one-click unsubscribe headers — marketing only. This is the
	// single chokepoint both Send and SendTemplated funnel through, which is
	// the whole point: a header built in one entry point and not the other
	// would be a marketing path that silently ships without an unsubscribe.
	// A transactional message (verification, password reset, ...) never
	// reaches this branch, so it can never carry one — an unsubscribe
	// header on a password-reset email would invite someone to unsubscribe
	// from their own account security.
	var headers map[string]string
	if in.Type == models.TypeMarketing {
		// One read, reused for the base URL below: the header must be built
		// on the same origin the admissibility check just judged, or a
		// config write landing between the two would make them disagree.
		policy := s.oneClickPolicy(ctx)
		base, status := oneClickBase(policy.PublicAPIBaseURL)
		// The backstop for everything the save-time gate cannot reach: a
		// profile stored before that gate existed, a profile whose driver
		// lost the capability in an upgrade, a base URL a deployment
		// cleared, and — the structural one — a marketing send that arrived
		// through CATEGORY routing, where no allowed_types declaration told
		// the save-time rule that marketing would ever come this way.
		//
		// A refusal, not a warning. A marketing message delivered without a
		// one-click unsubscribe is the exact failure the requirement exists
		// to prevent, and it cannot be undone once the message is in a
		// mailbox; a message withheld can be sent five minutes later. The
		// operator who turns require_one_click_unsubscribe off takes that
		// trade knowingly and this branch stands aside.
		//
		// baseURLUnsafe is excluded because it keeps its own, louder
		// refusal below: an embedded CR/LF is a header-injection attempt in
		// operator-typed config, not a missing setting, and the delivery log
		// should say so.
		if status != baseURLUnsafe && policy.gap(driver.Capabilities()) != oneClickSatisfied {
			return s.failSend(ctx, logDoc, profile, ErrOneClickUnsubscribeUnavailable)
		}
		switch status {
		case baseURLUsable:
			token := in.UnsubscribeToken
			if token == "" {
				// SendTemplated already issued a token for
				// {{.UnsubscribeURL}} on every templated send and threads
				// it through as UnsubscribeToken; Send (no template) never
				// does. Either way this mints at most one token for this
				// send.
				token, err = s.unsubService.IssueToken(ctx, in.Recipient.UserUUID, in.Recipient.Address, in.Category, in.UnsubscribeContext)
				if err != nil {
					return s.failSend(ctx, logDoc, profile, ErrUnsubscribeTokenUnavailable)
				}
			}
			headers = map[string]string{
				"List-Unsubscribe":      "<" + base + "/v1/notifications/unsubscribe?token=" + token + ">",
				"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
			}
		case baseURLNotConfigured:
			// Empty, missing an https scheme/host, or carrying a
			// path/query/fragment: no one-click header can be built. With
			// the requirement on, the guard above already refused this
			// send, so reaching here means an operator turned
			// require_one_click_unsubscribe off and accepted marketing
			// without an unsubscribe header. Even then, do not invent a
			// fallback origin and do not ship a malformed or relative URL —
			// send without the header, exactly like a driver that cannot
			// place headers on the wire at all.
		default: // baseURLUnsafe, and — fail safe — any future status this
			// switch does not yet know about. An embedded CR/LF is a
			// header-injection attempt (or a mangled paste) in
			// operator-typed config, not merely "not configured yet". This
			// task has no save-time validation hook to refuse it there, so
			// refusing the send is the backstop: silently stripping the
			// newline could turn a malicious value into something that
			// looks innocuous, and silently omitting the header (like the
			// "not configured" case above) would hide a misconfiguration
			// worth surfacing loudly. Making this the default rather than
			// an explicit baseURLUnsafe case means a status this switch
			// was not updated for refuses the send instead of silently
			// falling through to baseURLUsable's old zero-value default.
			return s.failSend(ctx, logDoc, profile, iface.ErrSenderInvalid)
		}
	}

	sendErr := driver.Send(ctx, profile, EmailMessage{
		To:       in.Recipient.Address,
		ToName:   in.Recipient.Name,
		Subject:  in.Subject,
		BodyText: in.BodyText,
		BodyHTML: bodyHTML, // the addon-rewritten body when a rewriter is wired
		Category: in.Category,
		Headers:  headers, // nil outside marketing — never on transactional mail
	})
	if sendErr != nil {
		return s.failSend(ctx, logDoc, profile, sendErr)
	}

	now := time.Now()
	logDoc.Status = models.StatusSent
	logDoc.Provider = profile.Provider
	logDoc.SenderSlug = profile.Slug
	logDoc.SentAt = &now
	_ = s.logRepo.Create(ctx, logDoc)

	return &iface.NotificationResult{
		ID:       logDoc.UUID,
		Status:   logDoc.Status,
		Provider: profile.Provider,
	}, nil
}

// ErrSendFailed: the driver refused or could not deliver. The reason is in
// the bounded diagnostic; the driver's own error is never in the chain.
var ErrSendFailed = errors.New("notification: send failed")

// ErrOptoutLookupUnavailable: the durable opt-out list could not be read, so
// the send was refused rather than risked.
var ErrOptoutLookupUnavailable = errors.New("notification: marketing opt-out lookup unavailable")

// ErrUnsubscribeTokenUnavailable: a marketing send needed a one-click
// unsubscribe token (for the List-Unsubscribe header) and IssueToken could
// not mint one. Distinct from iface.ErrSenderInvalid: the sender profile
// itself is fine here, it is the token store that is unavailable — the same
// distinction ErrOptoutLookupUnavailable already draws for the opt-out
// lookup.
var ErrUnsubscribeTokenUnavailable = errors.New("notification: unsubscribe token unavailable")

// ErrOneClickUnsubscribeUnavailable: a marketing message would have gone out
// with no RFC 8058 one-click unsubscribe — the driver cannot put the headers
// on the wire, or there is no public API base URL to point them at — while
// require_one_click_unsubscribe is on.
//
// It WRAPS iface.ErrSenderInvalid rather than standing beside it, for two
// reasons at once: every consumer outside this module matches the iface
// sentinel it already knows (and newDispatchError therefore records
// iface.ErrSenderInvalid as the DispatchError's sentinel, unchanged), while
// describeSendError still gives this cause a bounded reason of its own so the
// delivery log does not read like a malformed sender slug.
//
// The wrap order puts THIS message first and the sentinel's second. %w
// renders the wrapped error's text where the verb sits, so the other order
// made preflightFail produce "sender slug malformed: one-click unsubscribe
// cannot be guaranteed: ..." — leading a reader with a malformed slug that
// does not exist. errors.Is is unaffected by the order.
var ErrOneClickUnsubscribeUnavailable = fmt.Errorf("one-click unsubscribe cannot be guaranteed for this marketing send: %w", iface.ErrSenderInvalid)

// oneClickPolicy answers what the requirement is RIGHT NOW. It prefers the
// configured source — see Options.OneClickSource for why a value captured at
// Init is not acceptable here — and falls back to the static fields for a
// service built without one (every test that does not exercise reloading).
//
// It shares OneClickPolicy.gap with the save-time gate, so the two agree on
// what "one-click is guaranteed" means. They deliberately do NOT agree on
// scope: see the dispatch chokepoint's own comment, and oneClickAdmissible's.
func (s *NotificationService) oneClickPolicy(ctx context.Context) OneClickPolicy {
	if s.opts.OneClickSource != nil {
		return s.opts.OneClickSource(ctx)
	}
	return OneClickPolicy{
		Waived:             s.opts.OneClickWaived,
		PublicAPIBaseURL:   s.opts.PublicAPIBaseURL,
		UnsubscribePageURL: s.opts.UnsubscribePageURL,
	}
}

// trustedSentinels are the only errors a caller may test with errors.Is.
// The local ones serve callers inside this module (the SendTest handler);
// the iface ones serve every consumer that cannot import this package and
// so has no way to name a local sentinel — which is the whole point of the
// ADR-0021 seam. The explicit-Sender arm therefore maps BySlug's local
// error through mapBySlugErr before failing, and both of that mapper's
// outputs must be trusted here or the mapping is invisible to errors.Is.
var trustedSentinels = []error{
	ErrNoSenderForCategory, ErrSenderConfigUnavailable, ErrSenderNotFound, ErrUnknownDriver, ErrSenderNotConfigured,
	iface.ErrSenderInvalid, iface.ErrSenderNotEligible, iface.ErrSenderNotFound, iface.ErrSenderUnavailable,
	ErrOptoutLookupUnavailable, ErrUnsubscribeTokenUnavailable,
}

// DispatchError is the only error dispatchEmail and SendTest return. Error()
// is the sanitized reason (the same string the delivery log stores). Is
// answers true for ErrSendFailed on EVERY dispatch failure, and for the one
// trusted sentinel that names the cause (ErrNoSenderForCategory, …) when
// there is one — never for the raw driver error, which is dropped rather
// than wrapped: auth logs err.Error() of a failed send, so a fork driver's
// fmt.Errorf("vendor: %s", body) must not be reachable there.
type DispatchError struct {
	Reason   string
	sentinel error
}

func (e *DispatchError) Error() string { return e.Reason }

func (e *DispatchError) Is(target error) bool { return target == ErrSendFailed || e.sentinel == target }

func newDispatchError(profile SenderProfile, err error) *DispatchError {
	sentinel := ErrSendFailed
	for _, s := range trustedSentinels {
		if errors.Is(err, s) {
			sentinel = s
			break
		}
	}
	return &DispatchError{Reason: describeSendError(profile, err), sentinel: sentinel}
}

// failSend records a failed attempt with the bounded reason and returns the
// matching DispatchError. Nothing the driver wrote leaves this function.
func (s *NotificationService) failSend(ctx context.Context, logDoc *models.NotificationDoc, profile SenderProfile, err error) (*iface.NotificationResult, error) {
	de := newDispatchError(profile, err)
	logDoc.Status = models.StatusFailed
	logDoc.Provider = profile.Provider
	logDoc.SenderSlug = profile.Slug // empty when no profile resolved
	logDoc.Error = de.Reason
	_ = s.logRepo.Create(ctx, logDoc)
	s.logger.Warn("notification: send failed",
		slog.String("category", logDoc.Category),
		slog.String("reason", de.Reason),
	)
	return &iface.NotificationResult{
		ID:       logDoc.UUID,
		Status:   logDoc.Status,
		Provider: profile.Provider,
		Error:    de.Reason,
	}, de
}

// baseURLStatus classifies public_api_base_url for building the RFC 8058
// List-Unsubscribe header. The two non-usable outcomes get different
// treatment at the call site — see oneClickBase.
type baseURLStatus int

const (
	// baseURLUsable: base is a safe, bare https origin (scheme + host,
	// nothing else) — build the header.
	baseURLUsable baseURLStatus = iota
	// baseURLNotConfigured: empty, missing an https scheme, missing a host,
	// or carrying a path/query/fragment beyond a bare origin. All of these
	// are "nothing usable set up yet", not an attack — the send proceeds
	// without the header. Task 8 (require_one_click_unsubscribe) is what
	// stops a marketing sender profile from reaching this state in a
	// properly configured deployment; this package does not refuse the
	// send over it.
	baseURLNotConfigured
	// baseURLUnsafe: the value contains an embedded CR or LF — the classic
	// header-injection vector — or an unescaped '<' or '>'. dispatchEmail
	// refuses the send rather than risk it.
	baseURLUnsafe
)

// oneClickBase validates and normalizes public_api_base_url (and, reusing
// the exact same rule, unsubscribe_page_url) for use inside an RFC 8058
// List-Unsubscribe header or a footer link. A single trailing slash is
// stripped so the built URL never doubles up ".../v1/..." or "/u#...". base
// is only meaningful when status == baseURLUsable.
//
// Beyond the https-scheme-with-a-host check, this also rejects anything
// carrying a path, query string, or fragment. Any of those would produce a
// syntactically legal but functionally dead one-click link: with a query
// string already present, appending "?token=..." glues a second "?" onto
// the URL — not a new query parameter, just more characters swallowed into
// the existing one — so the raw token never lands where a mail client's
// one-click handler parses a "token" parameter from; a path relocates
// "/v1/notifications/..." to somewhere that is not this API's route. Either
// way the header would be well-formed enough to ship and useless enough to
// never work. url.Parse (rather than another substring check) is what
// catches a double trailing slash too: "https://api.example//" parses with
// Path "//", which TrimSuffix's single-slash strip would otherwise miss,
// yielding a doubled "//v1/".
//
// A literal '<' or '>' is rejected up front, before url.Parse ever sees it:
// appended straight onto the host (e.g. "https://api.example>evil") it is
// absorbed into u.Host with no error and an empty path/query/fragment — the
// exact shape this function otherwise calls usable — yet RFC 8058 requires
// the header value be wrapped in angle brackets, so a stray '>' in the base
// would prematurely close that delimiter and a stray '<' would open a second
// one where none belongs. Grouped with the CR/LF check because it is the
// same class of problem: operator-typed config carrying syntax-breaking
// characters for the exact wire format this value is going to be embedded
// in, not merely "not configured yet".
func oneClickBase(raw string) (base string, status baseURLStatus) {
	trimmed := strings.TrimSpace(raw)
	if strings.ContainsAny(trimmed, "\r\n<>") {
		return "", baseURLUnsafe
	}
	if trimmed == "" {
		return "", baseURLNotConfigured
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", baseURLNotConfigured
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", baseURLNotConfigured
	}
	return strings.TrimSuffix(trimmed, "/"), baseURLUsable
}

func (s *NotificationService) buildURL(path string) string {
	if s.opts.URLBuilder == nil {
		return path
	}
	return s.opts.URLBuilder(path)
}

// unsubscribeURL is the value SendTemplated puts behind {{.UnsubscribeURL}}
// in a template's footer — the link a PERSON clicks, never the
// List-Unsubscribe header a mail client's one-click button POSTs to (that
// one is built at the dispatch chokepoint in dispatchEmail, always against
// OneClickPolicy.PublicAPIBaseURL, never against pageURL below). pageURL is
// passed in rather than read off s.opts directly because the caller reads it
// live through oneClickPolicy — the same hot-reload path PublicAPIBaseURL
// already uses — so a value this function itself resolved from a stale
// Options snapshot could never happen by construction.
//
// With no hosted page configured, or one that does not validate as a bare
// https origin, this stays the direct API GET link it has always been —
// oneClickBase applies the exact same rule here it applies to
// PublicAPIBaseURL, so an operator typo in either field is either used
// cleanly or quietly treated as not set, never shipped as a broken link.
//
// With a page configured, the raw token rides in the URL FRAGMENT
// ("<page>/u#<token>"), not the query string: a fragment is never sent to a
// server, so it never reaches the page's access logs, any proxy in front of
// it, or a Referer header the page might leak it through.
func (s *NotificationService) unsubscribeURL(pageURL, token string) string {
	if base, status := oneClickBase(pageURL); status == baseURLUsable {
		return base + "/u#" + token
	}
	return s.buildURL(fmt.Sprintf("/notifications/unsubscribe?token=%s", token))
}

// TemplateService exposes the template service for admin endpoints.
func (s *NotificationService) TemplateService() TemplateService { return s.tmplService }

// PreferenceService exposes the preference service for user-facing endpoints.
func (s *NotificationService) PreferenceService() PreferenceService { return s.prefService }

// UnsubscribeService exposes the unsubscribe service for public endpoints.
func (s *NotificationService) UnsubscribeService() UnsubscribeService { return s.unsubService }

// LogRepo exposes the log repository for admin listing.
func (s *NotificationService) LogRepo() repository.NotificationRepository { return s.logRepo }

// Drivers exposes the driver registry (tests, admin diagnostics).
func (s *NotificationService) Drivers() *DriverRegistry { return s.drivers }

// Resolver exposes the sender resolver.
func (s *NotificationService) Resolver() SenderResolver { return s.resolver }

// Optouts exposes the marketing opt-out repository (tests, and module.go's
// own wiring assertion — see notification package's Init wiring test).
func (s *NotificationService) Optouts() repository.MarketingOptoutRepository { return s.optouts }

// ---- SenderDirectory (ADR-0021 D6) -----------------------------------------
//
// NotificationService satisfies iface.SenderDirectory directly: it is the
// exact object registered under module.ServiceNotificationSender
// (notification/module.go), so a caller type-asserting the registered
// service off the ServiceRegistry gets these methods for free — the same
// companion-interface idiom CategoryConfiguredChecker already uses.

// mapResolveErr maps the local errors Resolve/Default can return to the
// iface sentinel a caller of PreflightDelivery's default arm is allowed to
// match on. Anything other than ErrNoSenderForCategory — today only
// ErrSenderConfigUnavailable — means the question itself could not be
// answered, not that a specific sender choice failed.
func mapResolveErr(err error) error {
	if errors.Is(err, ErrNoSenderForCategory) {
		return iface.ErrNoSenderForCategory
	}
	return iface.ErrSenderUnavailable
}

// mapBySlugErr is mapResolveErr's counterpart for the explicit arm.
func mapBySlugErr(err error) error {
	if errors.Is(err, ErrSenderNotFound) {
		return iface.ErrSenderNotFound
	}
	return iface.ErrSenderUnavailable
}

// preflightFail wraps sentinel with a bounded, describeSendError-rendered
// reason so the returned error is errors.Is-able against sentinel while
// carrying no secret, host, or username (same allowlisted renderer the
// dispatch chokepoint's failed-send diagnostic uses).
func preflightFail(profile SenderProfile, sentinel error) error {
	return fmt.Errorf("%w: %s", sentinel, describeSendError(profile, sentinel))
}

// ListEligibleSenders lists every profile whose AllowedTypes contains typ,
// Ready computed by running it through the same usableDriver check the
// dispatch chokepoint uses. Identity only on the returned SenderInfo — no
// secret, host, or username ever crosses this boundary.
func (s *NotificationService) ListEligibleSenders(ctx context.Context, typ string) ([]iface.SenderInfo, error) {
	all, err := s.resolver.All(ctx)
	if err != nil {
		return nil, preflightFail(SenderProfile{}, iface.ErrSenderUnavailable)
	}
	// Read outside the loop: one policy read per call, not one per profile,
	// and every profile in one answer is judged against the same policy.
	marketing := typ == models.TypeMarketing
	var policy OneClickPolicy
	if marketing {
		policy = s.oneClickPolicy(ctx)
	}
	out := make([]iface.SenderInfo, 0, len(all))
	for _, p := range all {
		if !typeAllowed(p, typ) {
			continue
		}
		d, driverErr := s.usableDriver(p)
		// Ready must mean "a send of THIS type through this profile would go
		// out now". For marketing that includes the one-click requirement,
		// or a picker would offer a campaign a sender the chokepoint is
		// about to refuse.
		ready := driverErr == nil
		if ready && marketing {
			ready = policy.gap(d.Capabilities()) == oneClickSatisfied
		}
		out = append(out, iface.SenderInfo{
			Slug:        p.Slug,
			Label:       p.Label,
			Provider:    p.Provider,
			FromAddress: p.FromAddress,
			Ready:       ready,
		})
	}
	return out, nil
}

// PreflightDelivery preflights the whole delivery path a send with these
// parameters would take (ADR-0021 D6):
//
//	sender == "": category routing — Resolve(category, typ) → usable driver;
//	sender != "": grammar → BySlug → allowed_types → usable driver.
//
// Returns nil or one of the iface Err* sentinels. Every failure is wrapped
// errors.Is-ably around its sentinel with a bounded, secret-free reason.
func (s *NotificationService) PreflightDelivery(ctx context.Context, sender, category, typ string) error {
	tenantID, _ := ctxauth.GetTenantID(ctx)

	if sender == "" {
		profile, err := s.resolver.Resolve(ctx, ResolveInput{Category: category, Type: typ, TenantID: tenantID})
		if err != nil {
			return preflightFail(SenderProfile{}, mapResolveErr(err))
		}
		d, err := s.usableDriver(profile)
		if err != nil {
			return preflightFail(profile, iface.ErrSenderNotConfigured)
		}
		return s.oneClickPreflight(ctx, profile, d, typ)
	}

	if !module.ValidSlug(sender) {
		return preflightFail(SenderProfile{}, iface.ErrSenderInvalid)
	}
	profile, err := s.resolver.BySlug(ctx, sender)
	if err != nil {
		return preflightFail(SenderProfile{}, mapBySlugErr(err))
	}
	if !typeAllowed(profile, typ) {
		return preflightFail(profile, iface.ErrSenderNotEligible)
	}
	d, err := s.usableDriver(profile)
	if err != nil {
		return preflightFail(profile, iface.ErrSenderNotConfigured)
	}
	return s.oneClickPreflight(ctx, profile, d, typ)
}

// oneClickPreflight is the last step of both PreflightDelivery arms: a
// preflight that passed while the chokepoint would refuse is worse than no
// preflight at all, so the same policy that gates the send gates the answer.
// Transactional work returns before the policy is even read.
func (s *NotificationService) oneClickPreflight(ctx context.Context, profile SenderProfile, d EmailDriver, typ string) error {
	if typ != models.TypeMarketing {
		return nil
	}
	if s.oneClickPolicy(ctx).gap(d.Capabilities()) != oneClickSatisfied {
		return preflightFail(profile, ErrOneClickUnsubscribeUnavailable)
	}
	return nil
}

// TestSendInput is one operator-initiated test message.
type TestSendInput struct {
	To       string
	Subject  string
	BodyText string
	Sender   string // profile slug; empty = the default (*) profile
}

// TestSendResult reports which profile carried (or refused) a test send.
// Diagnostic is the bounded reason on failure — safe to show an operator.
type TestSendResult struct {
	Provider   string
	SenderSlug string
	Diagnostic string
}

// SendTest sends through the default profile, bypassing preferences,
// idempotency and the delivery log exactly as the /test endpoint always has.
func (s *NotificationService) SendTest(ctx context.Context, in TestSendInput) (TestSendResult, error) {
	var (
		profile SenderProfile
		err     error
	)
	if in.Sender != "" {
		profile, err = s.resolver.BySlug(ctx, in.Sender)
	} else {
		profile, err = s.resolver.Default(ctx)
	}
	if err != nil {
		de := newDispatchError(profile, err)
		return TestSendResult{Diagnostic: de.Reason}, de
	}
	res := TestSendResult{Provider: profile.Provider, SenderSlug: profile.Slug}
	driver, err := s.usableDriver(profile)
	if err != nil {
		de := newDispatchError(profile, err)
		res.Diagnostic = de.Reason
		return res, de
	}
	if err := driver.Send(ctx, profile, EmailMessage{To: in.To, Subject: in.Subject, BodyText: in.BodyText}); err != nil {
		de := newDispatchError(profile, err)
		res.Diagnostic = de.Reason
		return res, de
	}
	return res, nil
}

// SetEmailTrackingRewriter wires an addon-provided rewriter that injects email
// open/click tracking into rendered HTML just before transport. nil → no-op.
func (s *NotificationService) SetEmailTrackingRewriter(r iface.EmailTrackingRewriter) {
	s.emailRewriter = r
}

// SetMarketingUnsubscribeSink wires an addon-provided sink fired when a marketing
// unsubscribe is consumed (nil by default → no-op).
func (s *NotificationService) SetMarketingUnsubscribeSink(sink iface.MarketingUnsubscribeSink) {
	s.unsubscribeSink = sink
}

// UpsertTemplate stores or replaces an operator-managed notification template
// (e.g. a campaign template) via the template service. IsSystem is always false
// (campaign templates are operator content, not system defaults); Channel is
// always email. An empty locale falls back to the configured DefaultLocale.
func (s *NotificationService) UpsertTemplate(ctx context.Context, templateID, locale, subject, bodyHTML, bodyText string) error {
	if locale == "" {
		locale = s.opts.DefaultLocale
	}
	return s.tmplService.Upsert(ctx, &models.TemplateDoc{
		TemplateID: templateID, Locale: locale, Channel: models.ChannelEmail,
		Subject: subject, BodyHTML: bodyHTML, BodyText: bodyText, IsSystem: false,
	})
}

// GetTemplate fetches a notification template by (templateID, locale). An empty
// locale falls back to the configured DefaultLocale. Returns ErrTemplateNotFound
// (from the template service) when no matching row exists.
func (s *NotificationService) GetTemplate(ctx context.Context, templateID, locale string) (*iface.TemplateView, error) {
	if locale == "" {
		locale = s.opts.DefaultLocale
	}
	doc, err := s.tmplService.Get(ctx, templateID, locale)
	if err != nil {
		return nil, err
	}
	return &iface.TemplateView{TemplateID: doc.TemplateID, Locale: doc.Locale, Subject: doc.Subject, BodyHTML: doc.BodyHTML, BodyText: doc.BodyText}, nil
}

// FireMarketingUnsubscribe invokes the sink and reports what happened, so the
// caller can mark an opt-out that did not reach its consumer for replay.
//
// A nil sink returns nil: the base ships none, and "there is nothing to
// mirror" must not read as "the mirror failed", or a consume would stay
// pending forever waiting for a consumer that does not exist.
//
// A panicking sink is recovered — one bad consumer cannot take an
// unsubscribe request down — but it is converted into an error rather than
// swallowed. Swallowing it is precisely what made a failed mirror
// unrecoverable before this signature returned an error.
func (s *NotificationService) FireMarketingUnsubscribe(ctx context.Context, address, category, refContext string) (err error) {
	if s.unsubscribeSink == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			// Scrubbed on both paths: the panic value comes from a sink
			// core just handed this address to, and it ends up in a log
			// line AND in the error the caller logs in turn.
			reason := scrubAddress(fmt.Sprintf("%v", r), address)
			s.logger.Warn("notification: unsubscribe sink panicked", slog.String("recover", reason))
			err = fmt.Errorf("notification: unsubscribe sink panicked: %s", reason)
		}
	}()
	return s.unsubscribeSink.OnMarketingUnsubscribe(ctx, address, category, refContext)
}

// NormalizeAddress lowercases and trims an email address.
func NormalizeAddress(addr string) string {
	return strings.ToLower(strings.TrimSpace(addr))
}
