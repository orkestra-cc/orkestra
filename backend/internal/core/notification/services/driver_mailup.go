package services

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// mailUpSendURL is MailUp's transactional SendMessage endpoint (SMTP+ REST).
const mailUpSendURL = "https://send.mailup.com/API/v2.0/messages/sendmessage"

// mailUpTimeout bounds the whole request. Go's default client has none, so a
// vendor that accepts a connection and never answers would hold the send
// goroutine indefinitely.
const mailUpTimeout = 30 * time.Second

// Request shape of SendMessage. Authentication rides in the body's User
// field — SMTP+ credentials — not in an Authorization header (that belongs
// to the management APIs); that much is quoted verbatim from the vendor
// page, as is CampaignCode's role. The content field names below follow the
// vendor documentation as read while writing this; the page does not render
// its full JSON schema publicly, so confirm them there when touching this
// struct — a mismatch is a JSON-tag change plus the fixture in
// TestMailUpDriver_RequestShapeAndSuccess, and moves neither the success
// predicate nor the error contract.
type mailUpRequest struct {
	User     mailUpUser      `json:"User"`
	Subject  string          `json:"Subject"`
	Html     mailUpHTML      `json:"Html"`
	Text     string          `json:"Text"`
	From     mailUpAddress   `json:"From"`
	To       []mailUpAddress `json:"To"`
	ReplyTo  string          `json:"ReplyTo,omitempty"`
	CharSet  string          `json:"CharSet"`
	XSmtpAPI mailUpXSmtpAPI  `json:"XSmtpAPI"`
}

type mailUpUser struct {
	Username string `json:"Username"`
	Secret   string `json:"Secret"`
}

type mailUpHTML struct {
	Body string `json:"Body"`
}

type mailUpAddress struct {
	Name  string `json:"Name,omitempty"`
	Email string `json:"Email"`
}

// mailUpXSmtpAPI carries the extras that ride as the X-SMTPAPI header over
// the relay. CampaignCode is what MailUp aggregates statistics by — mapped
// from EmailMessage.Category so the vendor's reporting lines up with the
// routing this design introduces. Left empty, MailUp falls back to the
// SMTP+ user's console default. CampaignName is a distinct vendor field
// and is deliberately not set: the spec decides CampaignCode only.
type mailUpXSmtpAPI struct {
	CampaignCode string `json:"CampaignCode,omitempty"`
}

// mailUpResponse is the envelope. Only Status and Code are ever read;
// Message is deliberately not declared so it cannot be persisted by accident.
type mailUpResponse struct {
	Status string `json:"Status"`
	Code   string `json:"Code"`
}

type mailUpDriver struct {
	logger   *slog.Logger
	endpoint string
	client   *http.Client
}

// NewMailUpDriver sends through MailUp's SendMessage endpoint with an explicit client timeout.
func NewMailUpDriver(logger *slog.Logger) EmailDriver {
	return newMailUpDriver(logger, mailUpSendURL, &http.Client{Timeout: mailUpTimeout})
}

// newMailUpDriver is the test seam: an httptest.Server endpoint and a client.
func newMailUpDriver(logger *slog.Logger, endpoint string, client *http.Client) EmailDriver {
	if logger == nil {
		logger = slog.Default()
	}
	return &mailUpDriver{logger: logger, endpoint: endpoint, client: client}
}

func (d *mailUpDriver) Name() string { return "mailup" }

// Requires: identity plus both SMTP+ credentials — the API cannot function
// without them. The secret is invisible to the save-time gate (D5).
func (d *mailUpDriver) Requires() []ProfileRequirement {
	return []ProfileRequirement{{Key: SubFromAddress}, {Key: SubMailUpUser}, {Key: SubMailUpSecret, Secret: true}}
}

func (d *mailUpDriver) Send(ctx context.Context, p SenderProfile, msg EmailMessage) error {
	if err := ValidateProfile(d, p, RuntimeView); err != nil {
		return err
	}
	payload := mailUpRequest{
		User:     mailUpUser{Username: p.MailUpUser, Secret: p.MailUpSecret},
		Subject:  msg.Subject,
		Html:     mailUpHTML{Body: msg.BodyHTML},
		Text:     msg.BodyText,
		From:     mailUpAddress{Name: p.FromName, Email: p.FromAddress},
		To:       []mailUpAddress{{Name: msg.ToName, Email: msg.To}},
		ReplyTo:  p.ReplyTo,
		CharSet:  "utf-8",
		XSmtpAPI: mailUpXSmtpAPI{CampaignCode: msg.Category},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return transportError("mailup", "", err)
	}
	// The request payload never reaches an error path below: only the
	// response is inspected, and only through the bounded, typed route.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(body))
	if err != nil {
		return transportError("mailup", "", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return transportError("mailup", "", err)
	}
	defer resp.Body.Close()

	// Read under a bound before deciding anything. A body over the limit is
	// not parsed; its connection is closed without draining — losing
	// keep-alive on one connection is the cheaper half of that trade.
	raw, tooLarge, err := readBounded(resp.Body, maxResponseBody)
	if err != nil {
		return transportError("mailup", "read", err)
	}
	if tooLarge {
		return vendorBodyError("mailup", resp.StatusCode, bodyTooLarge, 0, "")
	}

	var env mailUpResponse
	if len(raw) == 0 || json.Unmarshal(raw, &env) != nil {
		return vendorBodyError("mailup", resp.StatusCode, bodyUnparseable, len(raw), strings.TrimSpace(resp.Header.Get("Content-Type")))
	}

	// Success is an allowlist, not the absence of an error: MailUp's WCF-
	// derived surface can answer 200 with an error envelope in the body.
	// Every other shape — including ones nobody anticipated — fails.
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300 && env.Status == "done" && env.Code == "0"
	if !ok {
		return vendorEnvelopeError("mailup", resp.StatusCode, env.Status, env.Code)
	}
	d.logger.Info("notification.email accepted",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
		slog.String("provider", "mailup"),
	)
	return nil
}
