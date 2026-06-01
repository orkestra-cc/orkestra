// Package stripe implements iface.PaymentProvider against Stripe's API.
//
// The provider is deliberately thin: it translates between Orkestra's
// generic payment abstractions (iface.CustomerInput, SubscriptionCharge,
// WebhookEvent) and Stripe's SDK types, with no business logic. All state
// transitions live in the subscriptions reconciler.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	stripelib "github.com/stripe/stripe-go/v76"
	checkoutsession "github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/customer"
	"github.com/stripe/stripe-go/v76/paymentintent"
	"github.com/stripe/stripe-go/v76/paymentmethod"
	"github.com/stripe/stripe-go/v76/refund"
	"github.com/stripe/stripe-go/v76/webhook"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Provider is the Stripe implementation of iface.PaymentProvider.
// It uses the stripe-go package-level functions (customer.New, etc.), which
// pick up the global stripe.Key. This is fine for v1 because Salvatore runs
// a single Stripe account; multi-account support would need per-call backends.
type Provider struct {
	apiKey        string
	webhookSecret string
	logger        *slog.Logger
}

// Config is the minimum set of credentials needed to talk to Stripe.
type Config struct {
	APIKey        string
	WebhookSecret string
	APIVersion    string // unused — SDK picks its own default
}

// New returns a configured Stripe provider. It does not validate the key
// with Stripe — that happens on the first real API call.
func New(cfg Config, logger *slog.Logger) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("stripe: API key is required")
	}
	// stripe-go reads the key from the package-level global. Setting it
	// here means any Orkestra binary that loads the payments module with
	// a valid key is ready to make calls.
	stripelib.Key = cfg.APIKey
	return &Provider{
		apiKey:        cfg.APIKey,
		webhookSecret: cfg.WebhookSecret,
		logger:        logger,
	}, nil
}

func (p *Provider) Name() string { return "stripe" }

func (p *Provider) CreateCustomer(ctx context.Context, in iface.CustomerInput) (iface.CustomerRef, error) {
	params := &stripelib.CustomerParams{
		Email: stripelib.String(in.Email),
		Name:  stripelib.String(in.Name),
	}
	if in.VATNumber != "" {
		params.AddMetadata("vatNumber", in.VATNumber)
	}
	if in.Country != "" {
		params.Address = &stripelib.AddressParams{Country: stripelib.String(in.Country)}
	}
	if in.TenantUUID != "" {
		params.AddMetadata("tenantUUID", in.TenantUUID)
	}
	for k, v := range in.Metadata {
		params.AddMetadata(k, v)
	}
	params.Context = ctx
	c, err := customer.New(params)
	if err != nil {
		return iface.CustomerRef{}, fmt.Errorf("stripe.customer.new: %w", err)
	}
	return iface.CustomerRef{Provider: "stripe", ID: c.ID}, nil
}

func (p *Provider) AttachPaymentMethod(ctx context.Context, cust iface.CustomerRef, token string) (iface.PaymentMethodRef, error) {
	attachParams := &stripelib.PaymentMethodAttachParams{
		Customer: stripelib.String(cust.ID),
	}
	attachParams.Context = ctx
	pm, err := paymentmethod.Attach(token, attachParams)
	if err != nil {
		return iface.PaymentMethodRef{}, fmt.Errorf("stripe.paymentmethod.attach: %w", err)
	}
	// Also mark as the customer's default for invoices, so subsequent charges
	// pick it up without having to specify a method each time.
	defParams := &stripelib.CustomerParams{
		InvoiceSettings: &stripelib.CustomerInvoiceSettingsParams{
			DefaultPaymentMethod: stripelib.String(pm.ID),
		},
	}
	defParams.Context = ctx
	if _, err := customer.Update(cust.ID, defParams); err != nil {
		p.logger.Warn("stripe: set default payment method failed",
			slog.String("customerID", cust.ID),
			slog.String("error", err.Error()),
		)
	}
	ref := iface.PaymentMethodRef{Provider: "stripe", ID: pm.ID}
	if pm.Card != nil {
		ref.Brand = string(pm.Card.Brand)
		ref.Last4 = pm.Card.Last4
		ref.ExpiryMonth = int(pm.Card.ExpMonth)
		ref.ExpiryYear = int(pm.Card.ExpYear)
	}
	return ref, nil
}

func (p *Provider) ChargeSubscription(ctx context.Context, in iface.SubscriptionCharge) (iface.ChargeResult, error) {
	params := &stripelib.PaymentIntentParams{
		Amount:   stripelib.Int64(in.AmountCents),
		Currency: stripelib.String(in.Currency),
		Customer: stripelib.String(in.Customer.ID),
		Confirm:  stripelib.Bool(true),
		// Off-session charge — no user in front of the browser right now.
		OffSession: stripelib.Bool(true),
		AutomaticPaymentMethods: &stripelib.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled:        stripelib.Bool(true),
			AllowRedirects: stripelib.String("never"),
		},
		Description: stripelib.String(in.Description),
	}
	if in.PaymentMethod != nil && in.PaymentMethod.ID != "" {
		params.PaymentMethod = stripelib.String(in.PaymentMethod.ID)
	}
	params.AddMetadata("subscriptionUUID", in.SubscriptionUUID)
	params.AddMetadata("invoiceUUID", in.InvoiceUUID)
	for k, v := range in.Metadata {
		params.AddMetadata(k, v)
	}
	// Use InvoiceUUID as the Stripe idempotency key so retries of the same
	// cycle don't double-charge.
	if in.InvoiceUUID != "" {
		params.SetIdempotencyKey(in.InvoiceUUID)
	}
	params.Context = ctx

	pi, err := paymentintent.New(params)
	if err != nil {
		var stripeErr *stripelib.Error
		if errors.As(err, &stripeErr) {
			return iface.ChargeResult{
				Status:      "failed",
				FailureCode: string(stripeErr.Code),
				FailureMsg:  stripeErr.Msg,
			}, nil
		}
		return iface.ChargeResult{}, fmt.Errorf("stripe.paymentintent.new: %w", err)
	}

	result := iface.ChargeResult{
		ProviderTxID: pi.ID,
		ChargedAt:    time.Now().UTC(),
	}
	switch pi.Status {
	case stripelib.PaymentIntentStatusSucceeded:
		result.Status = "succeeded"
	case stripelib.PaymentIntentStatusRequiresAction,
		stripelib.PaymentIntentStatusRequiresConfirmation,
		stripelib.PaymentIntentStatusRequiresPaymentMethod:
		result.Status = "requires_action"
	default:
		result.Status = "failed"
		if pi.LastPaymentError != nil {
			result.FailureCode = string(pi.LastPaymentError.Code)
			result.FailureMsg = pi.LastPaymentError.Msg
		}
	}
	return result, nil
}

func (p *Provider) RefundCharge(ctx context.Context, providerTxID string, amountCents int64, reason string) (iface.RefundResult, error) {
	params := &stripelib.RefundParams{
		PaymentIntent: stripelib.String(providerTxID),
	}
	if amountCents > 0 {
		params.Amount = stripelib.Int64(amountCents)
	}
	if reason != "" {
		params.AddMetadata("reason", reason)
	}
	params.Context = ctx
	ref, err := refund.New(params)
	if err != nil {
		return iface.RefundResult{}, fmt.Errorf("stripe.refund.new: %w", err)
	}
	return iface.RefundResult{
		ProviderRefundID: ref.ID,
		Status:           string(ref.Status),
	}, nil
}

// CreateCheckoutSession opens a payment-mode Stripe Checkout session for a
// one-shot charge that also saves the chosen card off-session for future
// renewal cycles. The PaymentIntent created by Stripe carries the supplied
// metadata, so the existing webhook reconciler picks up subscriptionUUID
// and invoiceUUID without any new wiring.
func (p *Provider) CreateCheckoutSession(ctx context.Context, in iface.CheckoutSessionInput) (iface.CheckoutSessionResult, error) {
	if in.Customer.ID == "" {
		return iface.CheckoutSessionResult{}, errors.New("stripe: checkout session requires a customer id")
	}
	if in.AmountCents <= 0 {
		return iface.CheckoutSessionResult{}, errors.New("stripe: checkout session amount must be positive")
	}
	if in.Currency == "" {
		return iface.CheckoutSessionResult{}, errors.New("stripe: checkout session requires a currency")
	}
	if in.SuccessURL == "" || in.CancelURL == "" {
		return iface.CheckoutSessionResult{}, errors.New("stripe: checkout session requires success and cancel URLs")
	}

	productName := in.Description
	if productName == "" {
		productName = "Subscription charge"
	}

	params := &stripelib.CheckoutSessionParams{
		Mode:       stripelib.String(string(stripelib.CheckoutSessionModePayment)),
		Customer:   stripelib.String(in.Customer.ID),
		SuccessURL: stripelib.String(in.SuccessURL),
		CancelURL:  stripelib.String(in.CancelURL),
		LineItems: []*stripelib.CheckoutSessionLineItemParams{{
			Quantity: stripelib.Int64(1),
			PriceData: &stripelib.CheckoutSessionLineItemPriceDataParams{
				Currency:   stripelib.String(in.Currency),
				UnitAmount: stripelib.Int64(in.AmountCents),
				ProductData: &stripelib.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripelib.String(productName),
				},
			},
		}},
		PaymentIntentData: &stripelib.CheckoutSessionPaymentIntentDataParams{
			SetupFutureUsage: stripelib.String(string(stripelib.PaymentIntentSetupFutureUsageOffSession)),
			Description:      stripelib.String(in.Description),
		},
	}
	for k, v := range in.Metadata {
		// Stamped on both the Checkout Session AND the resulting
		// PaymentIntent so the webhook reconciler keys off
		// subscriptionUUID / invoiceUUID exactly like the renewal job's
		// off-session charges.
		params.AddMetadata(k, v)
		params.PaymentIntentData.AddMetadata(k, v)
	}
	params.Context = ctx

	s, err := checkoutsession.New(params)
	if err != nil {
		return iface.CheckoutSessionResult{}, fmt.Errorf("stripe.checkout.session.new: %w", err)
	}
	return iface.CheckoutSessionResult{SessionID: s.ID, URL: s.URL}, nil
}

// CreateSetupCheckoutSession opens a setup-mode Stripe Checkout session —
// no charge, only collects and stores a payment method against the
// customer. Used by the "add card" flow on the Tier-2 client account page.
func (p *Provider) CreateSetupCheckoutSession(ctx context.Context, in iface.SetupCheckoutInput) (iface.CheckoutSessionResult, error) {
	if in.Customer.ID == "" {
		return iface.CheckoutSessionResult{}, errors.New("stripe: setup checkout requires a customer id")
	}
	if in.SuccessURL == "" || in.CancelURL == "" {
		return iface.CheckoutSessionResult{}, errors.New("stripe: setup checkout requires success and cancel URLs")
	}

	params := &stripelib.CheckoutSessionParams{
		Mode:               stripelib.String(string(stripelib.CheckoutSessionModeSetup)),
		Customer:           stripelib.String(in.Customer.ID),
		SuccessURL:         stripelib.String(in.SuccessURL),
		CancelURL:          stripelib.String(in.CancelURL),
		PaymentMethodTypes: stripelib.StringSlice([]string{"card"}),
		SetupIntentData: &stripelib.CheckoutSessionSetupIntentDataParams{
			Description: stripelib.String("Save payment method for future subscription renewals"),
		},
	}
	for k, v := range in.Metadata {
		params.AddMetadata(k, v)
		params.SetupIntentData.AddMetadata(k, v)
	}
	params.Context = ctx

	s, err := checkoutsession.New(params)
	if err != nil {
		return iface.CheckoutSessionResult{}, fmt.Errorf("stripe.checkout.session.new (setup): %w", err)
	}
	return iface.CheckoutSessionResult{SessionID: s.ID, URL: s.URL}, nil
}

// VerifyWebhook validates the Stripe-Signature header against the shared
// secret and returns a normalized event. Stripe's SDK handles the constant-
// time HMAC comparison for us.
func (p *Provider) VerifyWebhook(ctx context.Context, rawBody []byte, headers map[string]string) (iface.WebhookEvent, error) {
	if p.webhookSecret == "" {
		return iface.WebhookEvent{}, errors.New("stripe: webhook secret not configured")
	}
	sig := headers["Stripe-Signature"]
	if sig == "" {
		sig = headers["stripe-signature"]
	}
	if sig == "" {
		return iface.WebhookEvent{}, errors.New("stripe: missing Stripe-Signature header")
	}
	event, err := webhook.ConstructEvent(rawBody, sig, p.webhookSecret)
	if err != nil {
		return iface.WebhookEvent{}, fmt.Errorf("stripe: webhook verify: %w", err)
	}
	return normalize(event)
}

func normalize(e stripelib.Event) (iface.WebhookEvent, error) {
	out := iface.WebhookEvent{
		Provider:        "stripe",
		ProviderEventID: e.ID,
		Type:            string(e.Type),
		OccurredAt:      time.Unix(e.Created, 0).UTC(),
	}

	// Decode the data object once; for Stripe everything we need lives in
	// the payment_intent or refund payload.
	var raw map[string]any
	if err := json.Unmarshal(e.Data.Raw, &raw); err == nil {
		out.RawPayload = raw
	}

	switch e.Type {
	case "payment_intent.succeeded", "invoice.payment_succeeded":
		out.Normalized = "charge.succeeded"
	case "payment_intent.payment_failed", "invoice.payment_failed":
		out.Normalized = "charge.failed"
	case "charge.refunded":
		out.Normalized = "charge.refunded"
	default:
		// Ignored — record it for audit but don't act on it.
		out.Normalized = ""
	}

	// Pull identifiers from whichever object Stripe sent.
	var obj struct {
		ID               string            `json:"id"`
		LatestCharge     string            `json:"latest_charge"`
		PaymentIntent    string            `json:"payment_intent"`
		Amount           int64             `json:"amount"`
		AmountRefunded   int64             `json:"amount_refunded"`
		Currency         string            `json:"currency"`
		Metadata         map[string]string `json:"metadata"`
		LastPaymentError *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"last_payment_error"`
	}
	if err := json.Unmarshal(e.Data.Raw, &obj); err != nil {
		return out, nil
	}
	out.ProviderTxID = obj.ID
	if obj.PaymentIntent != "" {
		out.ProviderTxID = obj.PaymentIntent // refund → link back to the PI
		out.ProviderRefundID = obj.ID
	}
	out.AmountCents = obj.Amount
	if e.Type == "charge.refunded" && obj.AmountRefunded > 0 {
		out.AmountCents = obj.AmountRefunded
	}
	out.Currency = obj.Currency
	if v := obj.Metadata["subscriptionUUID"]; v != "" {
		out.SubscriptionUUID = v
	}
	if v := obj.Metadata["invoiceUUID"]; v != "" {
		out.InvoiceUUID = v
	}
	if obj.LastPaymentError != nil {
		out.FailureCode = obj.LastPaymentError.Code
		out.FailureMsg = obj.LastPaymentError.Message
	}
	return out, nil
}
