package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// ---- Fakes / helpers ------------------------------------------------------

// deafDriver accepts a message but cannot put anything extra on the wire —
// the shape of a vendor API that takes a recipient and a body and offers no
// header field at all. The core mailup driver is the real example; this fake
// exists so the dispatch tests do not have to build a complete mailup
// profile to reach the rule under test.
type deafDriver struct{ sends int }

func (d *deafDriver) Name() string                   { return "deaf" }
func (d *deafDriver) Requires() []ProfileRequirement { return nil }
func (d *deafDriver) Capabilities() DriverCapabilities {
	return DriverCapabilities{ListUnsubscribeHeaders: false}
}
func (d *deafDriver) Send(context.Context, SenderProfile, EmailMessage) error {
	d.sends++
	return nil
}

// oneClickProfile builds the save-time value map for one COMPLETE profile on
// provider declaring allowedTypes. Every transport field either driver could
// require is filled, so the completeness rule never fires before the rule
// under test.
func oneClickProfile(provider, allowedTypes string) map[string]string {
	return profileValues([]string{"mkt"}, map[string]map[string]string{
		"mkt": {
			SubProvider:     provider,
			SubAllowedTypes: allowedTypes,
			SubFromAddress:  "f@x",
			SubSMTPHost:     "h",
			SubMailUpUser:   "s1_2",
		},
	})
}

// requiredWith is the default posture — the requirement in force — with a
// base URL a header could actually be built on.
func requiredWith(base string) OneClickPolicy { return OneClickPolicy{PublicAPIBaseURL: base} }

// oneClickKit builds a service around a driver of the caller's choosing, so
// a test can dispatch through a driver that cannot carry the headers.
type oneClickKit struct {
	svc      *NotificationService
	logRepo  *fakeNotifRepo
	resolver *fakeResolver
}

func newOneClickKit(t *testing.T, driver EmailDriver, base string, waived bool) *oneClickKit {
	t.Helper()
	k := &oneClickKit{
		logRepo: newFakeNotifRepo(),
		resolver: &fakeResolver{profile: SenderProfile{
			Slug: "camp", Provider: driver.Name(),
			Categories:   []string{"*"},
			AllowedTypes: []string{models.TypeMarketing},
		}},
	}
	k.svc = NewNotificationService(
		k.logRepo, &fakeTemplateService{}, &fakePrefService{can: true},
		&fakeUnsubService{token: "raw-token"},
		k.resolver, NewDriverRegistry(driver), discardLogger(),
		Options{PublicAPIBaseURL: base, OneClickWaived: waived},
	)
	k.svc.SetOptouts(&fakeOptouts{}) // neutral: nobody opted out
	return k
}

// ---- Save time: the moment the operator can still fix it ------------------
//
// These four are the task brief's tests, moved onto the package's real
// save-time chokepoint (ValidateSenderConfig) instead of a parallel entry
// point of their own. The fixed point is unchanged: the refusal happens when
// the profile is SAVED, not after a campaign has gone out without an
// unsubscribe header.

func TestValidateSenderConfig_RefusesMarketingOnADriverWithoutCapability(t *testing.T) {
	err := ValidateSenderConfig(oneClickProfile("mailup", "marketing"), validationDrivers(), requiredWith("https://api.example"))
	if err == nil {
		t.Fatal("a marketing profile on a driver that cannot guarantee the headers must be refused at save time")
	}
	var ve *module.ConfigValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ConfigValidationError, got %v", err)
	}
	if ve.Code != errcode.NotificationSenderDriverNoOneClick {
		t.Fatalf("code = %q, want %q", ve.Code, errcode.NotificationSenderDriverNoOneClick)
	}
	if ve.Field != module.ItemKey(SendersField, "mkt", SubProvider) {
		t.Fatalf("field = %q, want the offending profile's provider", ve.Field)
	}
	if !strings.Contains(err.Error(), "one-click") {
		t.Fatalf("the reason must tell the operator what is missing, got %q", err)
	}
}

func TestValidateSenderConfig_RefusesMarketingWithoutAPublicBaseURL(t *testing.T) {
	err := ValidateSenderConfig(oneClickProfile("smtp", "marketing"), validationDrivers(), requiredWith(""))
	if err == nil {
		t.Fatal("without a public API base URL the header cannot be built: a marketing profile must be refused")
	}
	var ve *module.ConfigValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ConfigValidationError, got %v", err)
	}
	if ve.Code != errcode.NotificationPublicBaseURLMissing {
		t.Fatalf("code = %q, want %q", ve.Code, errcode.NotificationPublicBaseURLMissing)
	}
	if ve.Field != PublicAPIBaseURLField {
		t.Fatalf("field = %q, want the module-level base URL field", ve.Field)
	}
	if !strings.Contains(err.Error(), "one-click") {
		t.Fatalf("the reason must tell the operator what is missing, got %q", err)
	}
}

func TestValidateSenderConfig_TransactionalOnTheSameDriverIsFine(t *testing.T) {
	if err := ValidateSenderConfig(oneClickProfile("mailup", "transactional"), validationDrivers(), requiredWith("https://api.example")); err != nil {
		t.Fatalf("a transactional profile has nothing to do with unsubscribing: %v", err)
	}
}

func TestValidateSenderConfig_OperatorCanOptOutOfTheRequirement(t *testing.T) {
	waived := OneClickPolicy{Waived: true, PublicAPIBaseURL: "https://api.example"}
	if err := ValidateSenderConfig(oneClickProfile("mailup", "marketing"), validationDrivers(), waived); err != nil {
		t.Fatalf("with require_one_click_unsubscribe off the choice is the operator's: %v", err)
	}
}

// A profile that never declares marketing is outside the rule entirely, even
// on the same capability-less driver: a draft nobody can select, and a
// profile that only routes categories by pattern.

func TestValidateSenderConfig_OneClickRuleIgnoresProfilesThatCannotCarryMarketing(t *testing.T) {
	policy := requiredWith("")
	cases := []struct {
		name  string
		value map[string]string
	}{
		{"a draft declares nothing and is selectable for nothing",
			profileValues([]string{"mkt"}, map[string]map[string]string{"mkt": {SubProvider: "mailup"}})},
		{"a routing-only profile declares no allowed_types",
			profileValues([]string{"mkt"}, map[string]map[string]string{
				"mkt": {SubProvider: "mailup", SubCategories: "*", SubFromAddress: "f@x", SubMailUpUser: "s1_2"}})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateSenderConfig(c.value, validationDrivers(), policy); err != nil {
				t.Fatalf("the save-time one-click rule judges allowed_types only: %v", err)
			}
		})
	}
}

// The zero OneClickPolicy must fail closed. A caller that builds one without
// thinking about the requirement gets "required", never "marketing may go
// out without an unsubscribe".
func TestOneClickPolicy_ZeroValueRequiresOneClick(t *testing.T) {
	if err := ValidateSenderConfig(oneClickProfile("mailup", "marketing"), validationDrivers(), OneClickPolicy{}); err == nil {
		t.Fatal("the zero policy must require one-click, not waive it")
	}
}

// ---- "Not configured for that work" ---------------------------------------
//
// PreflightDelivery is the type-aware question (it takes typ); IsConfiguredFor
// takes a CATEGORY and has no type axis at all, so it is not made to answer
// this one — see TestIsConfiguredFor_AnswersAboutCategoryRoutingOnly below.

func TestPreflightDelivery_MarketingIsRefusedWhenOneClickCannotBeGuaranteed(t *testing.T) {
	cases := []struct {
		name   string
		driver EmailDriver
		base   string
	}{
		{"the driver cannot place the headers", &deafDriver{}, "https://api.example"},
		{"there is no public API base URL", &fakeDriver{name: "noop"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := newOneClickKit(t, c.driver, c.base, false)
			for _, sender := range []string{"camp", ""} {
				err := k.svc.PreflightDelivery(context.Background(), sender, "crm.campaign", models.TypeMarketing)
				if !errors.Is(err, iface.ErrSenderInvalid) {
					t.Fatalf("sender=%q: want ErrSenderInvalid, got %v", sender, err)
				}
			}
			// The same profile is fine for transactional work.
			if err := k.svc.PreflightDelivery(context.Background(), "", "auth.verify_email", models.TypeTransactional); err != nil {
				t.Fatalf("transactional preflight must be untouched: %v", err)
			}
		})
	}
}

func TestPreflightDelivery_MarketingPassesOnceOneClickIsGuaranteed(t *testing.T) {
	k := newOneClickKit(t, &fakeDriver{name: "noop"}, "https://api.example", false)
	if err := k.svc.PreflightDelivery(context.Background(), "camp", "crm.campaign", models.TypeMarketing); err != nil {
		t.Fatalf("a capable driver with a usable base URL must preflight clean: %v", err)
	}
}

func TestListEligibleSenders_MarketingProfileWithoutOneClickIsNotReady(t *testing.T) {
	k := newOneClickKit(t, &deafDriver{}, "https://api.example", false)
	got, err := k.svc.ListEligibleSenders(context.Background(), models.TypeMarketing)
	if err != nil {
		t.Fatalf("ListEligibleSenders: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want the profile listed, got %d entries", len(got))
	}
	if got[0].Ready {
		t.Fatal("a profile the rule refuses must not be offered to a campaign picker as ready")
	}
}

// IsConfiguredFor keeps answering the question it has always answered —
// "does a profile route this CATEGORY, and can its driver accept it" — and is
// deliberately not taught to guess a message type from a category name. A
// caller about to send marketing asks PreflightDelivery, which takes the type.
func TestIsConfiguredFor_AnswersAboutCategoryRoutingOnly(t *testing.T) {
	k := newOneClickKit(t, &deafDriver{}, "", false)
	if !k.svc.IsConfiguredFor(context.Background(), "crm.campaign") {
		t.Fatal("the category routes and its driver accepts the profile: that is the whole question IsConfiguredFor asks")
	}
	if err := k.svc.PreflightDelivery(context.Background(), "", "crm.campaign", models.TypeMarketing); err == nil {
		t.Fatal("the type-aware question must still refuse the same profile for marketing")
	}
}

// ---- Send time: the backstop ----------------------------------------------

func TestDispatch_MarketingIsRefusedWhenOneClickCannotBeGuaranteed(t *testing.T) {
	cases := []struct {
		name   string
		driver EmailDriver
		base   string
	}{
		{"the driver cannot place the headers", &deafDriver{}, "https://api.example"},
		{"there is no public API base URL", &fakeDriver{name: "noop"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := newOneClickKit(t, c.driver, c.base, false)

			res, err := k.svc.Send(context.Background(), marketingTo("ada@example.test"))
			if !errors.Is(err, iface.ErrSenderInvalid) {
				t.Fatalf("want ErrSenderInvalid, got %v", err)
			}
			if res != nil && res.Status == models.StatusSent {
				t.Fatal("the send must never be recorded as sent")
			}
			if len(k.logRepo.created) != 1 || k.logRepo.created[0].Status != models.StatusFailed {
				t.Fatalf("expected one failed delivery-log row, got %+v", k.logRepo.created)
			}
			if reason := k.logRepo.created[0].Error; !strings.Contains(reason, "one_click_unavailable") {
				t.Fatalf("the delivery log must name the cause, got %q", reason)
			}
		})
	}
}

func TestDispatch_TransactionalOnADriverWithoutCapabilityStillSends(t *testing.T) {
	d := &deafDriver{}
	k := newOneClickKit(t, d, "", false)

	res, err := k.svc.Send(context.Background(), transactionalTo("ada@example.test"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != models.StatusSent {
		t.Fatalf("status = %q, want sent", res.Status)
	}
	if d.sends != 1 {
		t.Fatalf("a transactional message has nothing to do with unsubscribing, got %d sends", d.sends)
	}
}

func TestDispatch_MarketingSendsWhenTheOperatorWaivedTheRequirement(t *testing.T) {
	d := &deafDriver{}
	k := newOneClickKit(t, d, "https://api.example", true)

	res, err := k.svc.Send(context.Background(), marketingTo("ada@example.test"))
	if err != nil {
		t.Fatalf("with require_one_click_unsubscribe off the send goes out: %v", err)
	}
	if res.Status != models.StatusSent {
		t.Fatalf("status = %q, want sent", res.Status)
	}
	if d.sends != 1 {
		t.Fatalf("expected the send to reach the driver, got %d", d.sends)
	}
}

// ---- The requirement must hot-reload ---------------------------------------
//
// The module declares HotReloadConfig() == true, so a config write records no
// restart-required flag anywhere. A policy captured at Init would therefore be
// stale with nothing saying so, and stale in BOTH directions:
//
//   - the operator sets the base URL the refusal message told them to set,
//     gets a 200, and every marketing send keeps failing identically;
//   - worse, the operator switches the requirement ON and marketing keeps
//     going out with no unsubscribe header — the fail-OPEN direction, and the
//     exact failure this whole rule exists to prevent.
//
// So the policy is read per marketing send, and this test flips it under a
// live service.
func TestDispatch_TheRequirementIsReadPerSendNotCapturedAtStartup(t *testing.T) {
	// Starts waived, with nothing a header could be built on.
	policy := OneClickPolicy{Waived: true}
	d := &fakeDriver{name: "noop"} // capable of carrying the headers
	logRepo := newFakeNotifRepo()
	resolver := &fakeResolver{profile: SenderProfile{
		Slug: "camp", Provider: "noop", Categories: []string{"*"},
		AllowedTypes: []string{models.TypeMarketing},
	}}
	svc := NewNotificationService(
		logRepo, &fakeTemplateService{}, &fakePrefService{can: true},
		&fakeUnsubService{token: "raw-token"},
		resolver, NewDriverRegistry(d), discardLogger(),
		Options{
			// Deliberately contradicting the source: a stale static value
			// must never win over the live one.
			PublicAPIBaseURL: "https://stale.example",
			OneClickWaived:   false,
			OneClickSource:   func(context.Context) OneClickPolicy { return policy },
		},
	)
	svc.SetOptouts(&fakeOptouts{})

	send := func() (*iface.NotificationResult, error) {
		return svc.Send(context.Background(), marketingTo("ada@example.test"))
	}

	// 1. Waived: the send goes out, and with no header — proving the live
	//    source, not the static PublicAPIBaseURL, decided the origin.
	res, err := send()
	if err != nil {
		t.Fatalf("waived: %v", err)
	}
	if res.Status != models.StatusSent || d.sends != 1 {
		t.Fatalf("waived: status=%q sends=%d, want sent/1", res.Status, d.sends)
	}
	if len(d.sent) != 1 || d.sent[0].Headers["List-Unsubscribe"] != "" {
		t.Fatalf("waived: the live source's empty base must decide, got %v", d.sent[0].Headers)
	}

	// 2. The operator switches the requirement ON. This is the fail-open
	//    direction: the very next send must be refused, with no restart.
	policy = OneClickPolicy{Waived: false}
	if _, err := send(); !errors.Is(err, iface.ErrSenderInvalid) {
		t.Fatalf("switching the requirement on must refuse the next send immediately, got %v", err)
	}
	if d.sends != 1 {
		t.Fatalf("the refused send must not reach the driver, got %d sends", d.sends)
	}

	// 3. The operator then sets the base URL the refusal asked for. The next
	//    send must go out, carrying the header built on the NEW origin.
	policy = OneClickPolicy{Waived: false, PublicAPIBaseURL: "https://api.example"}
	res, err = send()
	if err != nil {
		t.Fatalf("setting the base URL must unblock the next send without a restart: %v", err)
	}
	if res.Status != models.StatusSent || d.sends != 2 {
		t.Fatalf("status=%q sends=%d, want sent/2", res.Status, d.sends)
	}
	lu := d.sent[1].Headers["List-Unsubscribe"]
	if !strings.HasPrefix(lu, "<https://api.example/v1/notifications/unsubscribe?token=") {
		t.Fatalf("the header must be built on the newly configured origin, got %q", lu)
	}
}

// The same liveness for the two answers a caller asks BEFORE sending, so a
// picker and a preflight cannot keep reporting a profile ready after the
// operator switched the requirement on.
func TestPreflightAndDirectory_ObserveAPolicyChangeWithoutARestart(t *testing.T) {
	policy := OneClickPolicy{Waived: true}
	d := &deafDriver{} // cannot carry the headers: only the policy decides here
	resolver := &fakeResolver{profile: SenderProfile{
		Slug: "camp", Provider: "deaf", Categories: []string{"*"},
		AllowedTypes: []string{models.TypeMarketing},
	}}
	svc := NewNotificationService(
		newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true},
		&fakeUnsubService{token: "raw-token"},
		resolver, NewDriverRegistry(d), discardLogger(),
		Options{OneClickSource: func(context.Context) OneClickPolicy { return policy }},
	)

	ctx := context.Background()
	if err := svc.PreflightDelivery(ctx, "camp", "crm.campaign", models.TypeMarketing); err != nil {
		t.Fatalf("waived: preflight must pass: %v", err)
	}
	got, err := svc.ListEligibleSenders(ctx, models.TypeMarketing)
	if err != nil || len(got) != 1 || !got[0].Ready {
		t.Fatalf("waived: want one ready sender, got %+v (err %v)", got, err)
	}

	policy = OneClickPolicy{Waived: false}

	if err := svc.PreflightDelivery(ctx, "camp", "crm.campaign", models.TypeMarketing); !errors.Is(err, iface.ErrSenderInvalid) {
		t.Fatalf("requirement on: preflight must refuse immediately, got %v", err)
	}
	got, err = svc.ListEligibleSenders(ctx, models.TypeMarketing)
	if err != nil || len(got) != 1 || got[0].Ready {
		t.Fatalf("requirement on: the sender must stop reporting ready, got %+v (err %v)", got, err)
	}
}

// Neither a non-templated transactional Send nor a transactional
// PreflightDelivery pays for the policy read — the one-click rule is
// marketing-only, and OneClickSource is where a config read would happen.
//
// This is deliberately not a claim about every transactional path.
// SendTemplated DOES read the policy for a transactional message, on purpose:
// the same struct carries UnsubscribePageURL, which the footer link renders on
// transactional templates too. See livePolicy's cost model.
func TestDispatch_TransactionalSendAndPreflightNeverReadTheOneClickPolicy(t *testing.T) {
	reads := 0
	d := &deafDriver{}
	svc := NewNotificationService(
		newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true},
		&fakeUnsubService{token: "raw-token"},
		&fakeResolver{profile: SenderProfile{Slug: "camp", Provider: "deaf", Categories: []string{"*"}}},
		NewDriverRegistry(d), discardLogger(),
		Options{OneClickSource: func(context.Context) OneClickPolicy {
			reads++
			return OneClickPolicy{}
		}},
	)
	svc.SetOptouts(&fakeOptouts{})

	if _, err := svc.Send(context.Background(), transactionalTo("ada@example.test")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := svc.PreflightDelivery(context.Background(), "", "auth.verify_email", models.TypeTransactional); err != nil {
		t.Fatalf("PreflightDelivery: %v", err)
	}
	if reads != 0 {
		t.Fatalf("transactional work must never read the one-click policy, got %d reads", reads)
	}
}

// ListEligibleSenders reads the policy once per call, not once per profile.
func TestListEligibleSenders_ReadsTheOneClickPolicyOncePerCall(t *testing.T) {
	reads := 0
	resolver := &fakeResolver{all: []SenderProfile{
		{Slug: "a", Provider: "deaf", AllowedTypes: []string{models.TypeMarketing}},
		{Slug: "b", Provider: "deaf", AllowedTypes: []string{models.TypeMarketing}},
		{Slug: "c", Provider: "deaf", AllowedTypes: []string{models.TypeMarketing}},
	}}
	svc := NewNotificationService(
		newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true},
		&fakeUnsubService{}, resolver, NewDriverRegistry(&deafDriver{}), discardLogger(),
		Options{OneClickSource: func(context.Context) OneClickPolicy {
			reads++
			return OneClickPolicy{Waived: true}
		}},
	)

	if _, err := svc.ListEligibleSenders(context.Background(), models.TypeMarketing); err != nil {
		t.Fatalf("ListEligibleSenders: %v", err)
	}
	if reads != 1 {
		t.Fatalf("want one policy read for the whole answer, got %d", reads)
	}
}

// The refusal a fork logs must lead with what is actually wrong. Wrapping the
// iface sentinel the other way round produced "sender slug malformed: ...",
// sending an operator hunting for a slug problem that does not exist.
func TestPreflightError_LeadsWithTheTrueStatement(t *testing.T) {
	k := newOneClickKit(t, &deafDriver{}, "https://api.example", false)
	err := k.svc.PreflightDelivery(context.Background(), "camp", "crm.campaign", models.TypeMarketing)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.HasPrefix(err.Error(), "one-click unsubscribe cannot be guaranteed") {
		t.Fatalf("the message must lead with the true statement, got %q", err)
	}
	if !errors.Is(err, iface.ErrSenderInvalid) {
		t.Fatalf("the wrap order must not change errors.Is, got %v", err)
	}
}
