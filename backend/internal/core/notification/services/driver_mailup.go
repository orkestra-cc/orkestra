package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
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
//
// Attachments follows spec §2 V2 (design doc
// docs/superpowers/specs/2026-09-26-forms-pdf-copy-design.md), the outcome
// of the blocking Task 0 verification run on 2026-09-26: the shape —
// Attachments: [{Filename, Body}] — is sourced from MailUp's "Transactional
// APIs – FAQ" (which lists Attachments among the advanced options) and a
// production MailUp client (pagopa/io-functions-commons,
// src/mailer/mailup.ts, which sends Body as a .NET byte[] array). Real
// sends of 1 MB and 5 MB PDFs from the staging SMTP+ account, with Body as
// a standard base64 string instead, both answered HTTP 200
// {"Status":"done","Code":"0"} with an openable attachment on receipt —
// confirming base64 is accepted and cheaper on the wire (~4x) than the byte
// array. MailUp's declared attachment limit is 10 MB (same FAQ), above our
// 5 MB cap. ContentId (inline images) exists in the same field but is not
// used here.
type mailUpRequest struct {
	User            mailUpUser             `json:"User"`
	Subject         string                 `json:"Subject"`
	Html            *mailUpHTML            `json:"Html,omitempty"`
	Text            string                 `json:"Text,omitempty"`
	From            mailUpAddress          `json:"From"`
	To              []mailUpAddress        `json:"To"`
	ReplyTo         string                 `json:"ReplyTo,omitempty"`
	CharSet         string                 `json:"CharSet"`
	XSmtpAPI        mailUpXSmtpAPI         `json:"XSmtpAPI"`
	ExtendedHeaders []mailUpExtendedHeader `json:"ExtendedHeaders,omitempty"`
	Attachments     []mailUpAttachment     `json:"Attachments,omitempty"`
}

// mailUpAttachment is one file, base64-encoded (see the mailUpRequest
// comment for the source and the base64-vs-byte-array choice).
type mailUpAttachment struct {
	Filename string `json:"Filename"`
	Body     string `json:"Body"`
}

// mailUpExtendedHeader is one name/value pair MailUp's SendMessage accepts
// as an extra header. Accepting it is not the same as putting it on the
// wire — see mailUpDriver.Capabilities.
type mailUpExtendedHeader struct {
	N string `json:"N"`
	V string `json:"V"`
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

// Capabilities: ListUnsubscribeHeaders is false until a real send proves
// otherwise — MailUp's documentation says it adds only approved headers, so
// accepting our ExtendedHeaders is not the same as delivering them, and
// RFC 8058 additionally needs them covered by the DKIM signature. Flipping
// that field is the outcome of the release gate's test, not a configuration
// an operator can set. Attachments is true: spec §2 V2 confirmed the
// payload shape and proved it with real sends (see the mailUpRequest
// comment).
func (d *mailUpDriver) Capabilities() DriverCapabilities {
	return DriverCapabilities{ListUnsubscribeHeaders: false, Attachments: true}
}

// mailUpExtendedHeadersFrom converts EmailMessage.Headers into MailUp's
// ExtendedHeaders shape, keys sorted for a deterministic payload — the same
// reason buildMIMEMessageAt sorts before writing.
func mailUpExtendedHeadersFrom(headers map[string]string) []mailUpExtendedHeader {
	if len(headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]mailUpExtendedHeader, 0, len(keys))
	for _, k := range keys {
		out = append(out, mailUpExtendedHeader{N: k, V: headers[k]})
	}
	return out
}

func (d *mailUpDriver) Send(ctx context.Context, p SenderProfile, msg EmailMessage) error {
	if err := ValidateProfile(d, p, RuntimeView); err != nil {
		return err
	}
	payload := mailUpRequest{
		User:            mailUpUser{Username: p.MailUpUser, Secret: p.MailUpSecret},
		Subject:         msg.Subject,
		Text:            msg.BodyText,
		From:            mailUpAddress{Name: p.FromName, Email: p.FromAddress},
		To:              []mailUpAddress{{Name: msg.ToName, Email: msg.To}},
		ReplyTo:         p.ReplyTo,
		CharSet:         "utf-8",
		XSmtpAPI:        mailUpXSmtpAPI{CampaignCode: msg.Category},
		ExtendedHeaders: mailUpExtendedHeadersFrom(msg.Headers),
	}
	// Only send the Html part when there is an HTML body: an empty-but-present
	// Html:{"Body":""} gives mail clients an empty HTML alternative to render
	// next to Text, which for a text-only message (e.g. SendTest) shows the
	// recipient a blank email instead of the plain-text content.
	if msg.BodyHTML != "" {
		payload.Html = &mailUpHTML{Body: msg.BodyHTML}
	}
	if len(msg.Attachments) > 0 {
		atts := make([]mailUpAttachment, 0, len(msg.Attachments))
		for _, a := range msg.Attachments {
			atts = append(atts, mailUpAttachment{Filename: a.Filename, Body: base64.StdEncoding.EncodeToString(a.Data)})
		}
		payload.Attachments = atts
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
		envErr := vendorEnvelopeError("mailup", resp.StatusCode, env.Status, env.Code)
		// Task 0 (spec §2 V2) found no MailUp-specific attachment-rejection
		// code — its public FAQ and the confirmed integrations do not
		// document one. Fallback: a 4xx on a send that carried attachments
		// is classified ErrAttachmentRejected; a 4xx without attachments,
		// or any 200 carrying an error envelope, is not (it may be an
		// unrelated rejection, and a 200 error envelope has no HTTP status
		// to key off). Coarse by necessity, same trade as the SMTP driver's
		// 552/554 classification. 401/403 (credentials), 408 (timeout) and
		// 429 (throttling) are operational conditions, not a verdict on the
		// attachment: the caller treats ErrAttachmentRejected as final, so
		// those keep their normal classification and stay retryable.
		if len(msg.Attachments) > 0 && mailUpAttachmentRejectionStatus(resp.StatusCode) {
			return fmt.Errorf("%w: %w", ErrAttachmentRejected, envErr)
		}
		return envErr
	}
	d.logger.Info("notification.email accepted",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
		slog.String("provider", "mailup"),
	)
	return nil
}

// mailUpAttachmentRejectionStatus reports whether an HTTP status on a send
// that carried attachments may be read as a rejection of the attachment:
// any 4xx except the auth, timeout and throttling statuses.
func mailUpAttachmentRejectionStatus(status int) bool {
	if status < 400 || status >= 500 {
		return false
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return false
	}
	return true
}
