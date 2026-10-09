package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"sort"
	"strings"
	"time"
)

// smtp2goTimeout bounds the whole request, for the same reason as
// mailUpTimeout: Go's default client has none.
const smtp2goTimeout = 30 * time.Second

// smtp2goHosts maps the profile's region to SMTP2GO's API host. "global" is
// the regionless host; the regional ones keep the request (and the message
// it carries) inside that region — "eu" is the choice for EU data residency.
var smtp2goHosts = map[string]string{
	"global": "api.smtp2go.com",
	"eu":     "eu-api.smtp2go.com",
	"us":     "us-api.smtp2go.com",
	"au":     "au-api.smtp2go.com",
}

// smtp2goRegions lists the region options in the order the console shows them.
var smtp2goRegions = []string{"global", "eu", "us", "au"}

// smtp2goEndpoint resolves a region to the send endpoint. An empty region is
// global (a profile saved before the field existed); an unknown one resolves
// to nothing, so it can never fall through to some other region's host.
func smtp2goEndpoint(region string) (string, bool) {
	if region == "" {
		region = "global"
	}
	host, ok := smtp2goHosts[region]
	if !ok {
		return "", false
	}
	return "https://" + host + "/v3/email/send", true
}

// smtp2goRequest is the body of POST /v3/email/send, field names per the
// SMTP2GO API reference ("Send an email") and its official Go client
// (smtp2go-oss/smtp2go-go). The API key is NOT a body field: it rides in the
// X-Smtp2go-Api-Key header, so the payload carries no credential.
//
// There is no Reply-To field; the profile's reply_to rides in custom_headers.
// EmailMessage.Category is not put on the wire: SMTP2GO documents no
// per-message campaign field equivalent to MailUp's CampaignCode.
type smtp2goRequest struct {
	Sender        string              `json:"sender"`
	To            []string            `json:"to"`
	Subject       string              `json:"subject"`
	TextBody      string              `json:"text_body,omitempty"`
	HTMLBody      string              `json:"html_body,omitempty"`
	CustomHeaders []smtp2goHeader     `json:"custom_headers,omitempty"`
	Attachments   []smtp2goAttachment `json:"attachments,omitempty"`
}

type smtp2goHeader struct {
	Header string `json:"header"`
	Value  string `json:"value"`
}

// smtp2goAttachment is one file with its bytes inline as standard base64
// (the API's alternative, a url to fetch, is not used).
type smtp2goAttachment struct {
	Filename string `json:"filename"`
	Fileblob string `json:"fileblob"`
	Mimetype string `json:"mimetype"`
}

// smtp2goResponse is the envelope. Only the counters and error_code are
// ever read. "error" (free text) and "failures" (which names recipients) are
// deliberately not declared, so neither can be persisted by accident.
type smtp2goResponse struct {
	Data struct {
		Succeeded *int   `json:"succeeded"`
		Failed    *int   `json:"failed"`
		ErrorCode string `json:"error_code"`
	} `json:"data"`
}

type smtp2goDriver struct {
	logger   *slog.Logger
	endpoint func(region string) (string, bool)
	client   *http.Client
}

// NewSMTP2GODriver sends through SMTP2GO's HTTP API with an explicit client timeout.
func NewSMTP2GODriver(logger *slog.Logger) EmailDriver {
	return newSMTP2GODriver(logger, smtp2goEndpoint, &http.Client{Timeout: smtp2goTimeout})
}

// newSMTP2GODriver is the test seam: a region resolver pointing at an
// httptest.Server, and a client.
func newSMTP2GODriver(logger *slog.Logger, endpoint func(string) (string, bool), client *http.Client) EmailDriver {
	if logger == nil {
		logger = slog.Default()
	}
	return &smtp2goDriver{logger: logger, endpoint: endpoint, client: client}
}

func (d *smtp2goDriver) Name() string { return "smtp2go" }

// Requires: identity plus the API key. The region is not listed: the record
// list defaults it to global, and an empty one reads as global. The secret
// is invisible to the save-time gate (D5).
func (d *smtp2goDriver) Requires() []ProfileRequirement {
	return []ProfileRequirement{{Key: SubFromAddress}, {Key: SubSMTP2GOAPIKey, Secret: true}}
}

// Capabilities: ListUnsubscribeHeaders is false for the reason the mailup
// driver gives — the API accepting custom_headers is not proof that
// List-Unsubscribe / List-Unsubscribe-Post reach the inbox covered by the
// DKIM signature; flipping it is the outcome of the release gate's
// real-mailbox test, not a setting. Attachments is true: the payload shape
// is the one the API reference and SMTP2GO's own client both declare.
func (d *smtp2goDriver) Capabilities() DriverCapabilities {
	return DriverCapabilities{ListUnsubscribeHeaders: false, Attachments: true}
}

// smtp2goHeadersFrom converts EmailMessage.Headers into custom_headers, keys
// sorted for a deterministic payload, then appends the profile's Reply-To —
// the same set, in the same order, the smtp driver writes into its MIME. An
// entry safeExtraHeader refuses (CR/LF, or a header the driver owns) is
// dropped here too: the vendor builds the MIME from these pairs.
func smtp2goHeadersFrom(headers map[string]string, replyTo string) []smtp2goHeader {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []smtp2goHeader
	for _, k := range keys {
		if !safeExtraHeader(k, headers[k]) {
			continue
		}
		out = append(out, smtp2goHeader{Header: k, Value: headers[k]})
	}
	if replyTo != "" && !strings.ContainsAny(replyTo, "\r\n") {
		out = append(out, smtp2goHeader{Header: "Reply-To", Value: replyTo})
	}
	return out
}

func (d *smtp2goDriver) Send(ctx context.Context, p SenderProfile, msg EmailMessage) error {
	if err := ValidateProfile(d, p, RuntimeView); err != nil {
		return err
	}
	endpoint, ok := d.endpoint(p.SMTP2GORegion)
	if !ok {
		return &ProfileIncompleteError{Driver: d.Name(), Missing: []string{SubSMTP2GORegion}}
	}
	payload := smtp2goRequest{
		Sender: (&mail.Address{Name: p.FromName, Address: p.FromAddress}).String(),
		// mail.Address quotes a name carrying a comma or a quote and
		// RFC 2047-encodes a non-ASCII one, so the vendor's parser
		// receives one well-formed address.
		To:            []string{(&mail.Address{Name: msg.ToName, Address: msg.To}).String()},
		Subject:       msg.Subject,
		TextBody:      msg.BodyText,
		HTMLBody:      msg.BodyHTML,
		CustomHeaders: smtp2goHeadersFrom(msg.Headers, p.ReplyTo),
	}
	if len(msg.Attachments) > 0 {
		atts := make([]smtp2goAttachment, 0, len(msg.Attachments))
		for _, a := range msg.Attachments {
			atts = append(atts, smtp2goAttachment{Filename: a.Filename, Fileblob: base64.StdEncoding.EncodeToString(a.Data), Mimetype: a.ContentType})
		}
		payload.Attachments = atts
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return transportError("smtp2go", "", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return transportError("smtp2go", "", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Smtp2go-Api-Key", p.SMTP2GOAPIKey)

	resp, err := d.client.Do(req)
	if err != nil {
		return transportError("smtp2go", "", err)
	}
	defer resp.Body.Close()

	raw, tooLarge, err := readBounded(resp.Body, maxResponseBody)
	if err != nil {
		return transportError("smtp2go", "read", err)
	}
	if tooLarge {
		return vendorBodyError("smtp2go", resp.StatusCode, bodyTooLarge, 0, "")
	}

	var env smtp2goResponse
	if len(raw) == 0 || json.Unmarshal(raw, &env) != nil {
		return vendorBodyError("smtp2go", resp.StatusCode, bodyUnparseable, len(raw), strings.TrimSpace(resp.Header.Get("Content-Type")))
	}

	// Success is an allowlist. SMTP2GO answers 200 for a request whose
	// recipient failed, reporting it only in the counters, so the status
	// alone proves nothing: exactly one recipient is sent, so exactly one
	// must have succeeded and none failed.
	data := env.Data
	ok = resp.StatusCode >= 200 && resp.StatusCode < 300 && data.ErrorCode == "" &&
		data.Succeeded != nil && *data.Succeeded == 1 && data.Failed != nil && *data.Failed == 0
	if !ok {
		status := "not_accepted"
		if data.ErrorCode != "" {
			status = "error"
		}
		envErr := vendorEnvelopeError("smtp2go", resp.StatusCode, status, data.ErrorCode)
		// SMTP2GO answers 400 to every refused request (an unverified
		// sender, a malformed field) and documents no attachment-specific
		// error code, so a 4xx says nothing about the attachment. Only 413 —
		// the request was too large — is a verdict on it. Callers treat
		// ErrAttachmentRejected as final, so a wrong positive would turn a
		// fixable configuration error into a permanent one.
		if len(msg.Attachments) > 0 && resp.StatusCode == http.StatusRequestEntityTooLarge {
			return fmt.Errorf("%w: %w", ErrAttachmentRejected, envErr)
		}
		return envErr
	}
	d.logger.Info("notification.email accepted",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
		slog.String("provider", "smtp2go"),
	)
	return nil
}
