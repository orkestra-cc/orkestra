package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	fcmScope       = "https://www.googleapis.com/auth/firebase.messaging"
	fcmDefaultBase = "https://fcm.googleapis.com"
)

// FCMOptions tunes the FCM driver; zero values mean production defaults.
type FCMOptions struct {
	BaseURL     string
	HTTPClient  *http.Client
	Concurrency int           // default 8
	Timeout     time.Duration // per call, default 10s
}

type fcmDriver struct {
	baseURL   string
	projectID string
	client    *http.Client
	ts        oauth2.TokenSource
	conc      int
	timeout   time.Duration
	logger    *slog.Logger
}

// NewFCMPushDriver builds an FCM HTTP v1 driver from a service-account JSON.
// No Firebase SDK: OAuth via google.JWTConfigFromJSON (token_uri honoured).
func NewFCMPushDriver(serviceAccountJSON []byte, projectID string, opt FCMOptions) (PushDriver, error) {
	var sa struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		ProjectID   string `json:"project_id"`
	}
	if err := json.Unmarshal(serviceAccountJSON, &sa); err != nil {
		return nil, errors.New("notification: fcm service account JSON is not valid JSON")
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, errors.New("notification: fcm service account JSON lacks client_email or private_key")
	}
	if projectID == "" {
		projectID = sa.ProjectID
	}
	if projectID == "" {
		return nil, errors.New("notification: fcm project id missing (config or service account project_id)")
	}
	jc, err := google.JWTConfigFromJSON(serviceAccountJSON, fcmScope)
	if err != nil {
		return nil, errors.New("notification: fcm service account JSON rejected")
	}
	if opt.BaseURL == "" {
		opt.BaseURL = fcmDefaultBase
	}
	if opt.Concurrency <= 0 {
		opt.Concurrency = 8
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 10 * time.Second
	}
	client := opt.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: opt.Timeout}
	}
	// The token source lives as long as the driver; its fetches use the same client.
	tctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
	return &fcmDriver{
		baseURL: strings.TrimRight(opt.BaseURL, "/"), projectID: projectID, client: client,
		ts: oauth2.ReuseTokenSource(nil, jc.TokenSource(tctx)), conc: opt.Concurrency, timeout: opt.Timeout, logger: slog.Default(),
	}, nil
}

func (d *fcmDriver) Name() string { return "fcm" }

func (d *fcmDriver) Send(ctx context.Context, msgs []PushMessage) []PushOutcome {
	out := make([]PushOutcome, len(msgs))
	if len(msgs) == 0 {
		return out
	}
	tok, err := d.ts.Token()
	if err != nil {
		e := fmt.Errorf("fcm: oauth token: %s", bound(err.Error(), 300))
		for i := range out {
			out[i] = PushOutcome{Reason: "transient", Err: e}
		}
		return out
	}
	var warnOnce sync.Once // one config warning per send
	sem := make(chan struct{}, d.conc)
	var wg sync.WaitGroup
	for i := range msgs {
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = d.sendOne(ctx, tok.AccessToken, msgs[i], &warnOnce)
		}(i)
	}
	wg.Wait()
	return out
}

func fcmBody(m PushMessage) map[string]any {
	high := strings.EqualFold(m.Priority, "high")
	android := map[string]any{"priority": map[bool]string{true: "HIGH", false: "NORMAL"}[high]}
	apnsHeaders := map[string]string{"apns-priority": map[bool]string{true: "10", false: "5"}[high]}
	if m.CollapseKey != "" {
		android["collapse_key"] = m.CollapseKey
		android["notification"] = map[string]string{"tag": m.CollapseKey}
		apnsHeaders["apns-collapse-id"] = m.CollapseKey
	}
	if m.TTL > 0 {
		android["ttl"] = strconv.FormatInt(max(int64((m.TTL+time.Second-1)/time.Second), 1), 10) + "s"
		apnsHeaders["apns-expiration"] = strconv.FormatInt(time.Now().Add(m.TTL).Unix(), 10)
	}
	msg := map[string]any{
		"token":        m.Token,
		"notification": map[string]string{"title": m.Title, "body": m.Body},
		"android":      android,
		"apns":         map[string]any{"headers": apnsHeaders},
	}
	if len(m.Data) > 0 {
		msg["data"] = m.Data
	}
	return map[string]any{"message": msg}
}

func (d *fcmDriver) sendOne(ctx context.Context, accessToken string, m PushMessage, warnOnce *sync.Once) PushOutcome {
	body, _ := json.Marshal(fcmBody(m))
	cctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, d.baseURL+"/v1/projects/"+d.projectID+"/messages:send", bytes.NewReader(body))
	if err != nil {
		return PushOutcome{Reason: "fcm_error", Err: errors.New("fcm: build request")}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		// url.Error carries the URL only (no token in it); still bound + scrub.
		return PushOutcome{Reason: "transient", Err: fmt.Errorf("fcm: request failed: %s", scrub(bound(err.Error(), 300), m.Token))}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode/100 == 2 {
		return PushOutcome{Delivered: true}
	}
	var eb struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Details []struct {
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &eb)
	code := eb.Error.Status
	unregistered := false
	for _, dt := range eb.Error.Details {
		if dt.ErrorCode != "" {
			code = dt.ErrorCode
		}
		if dt.ErrorCode == "UNREGISTERED" {
			unregistered = true
		}
	}
	diag := bound(scrub(fmt.Sprintf("fcm: http %d %s %s", resp.StatusCode, code, eb.Error.Message), m.Token), pushErrBound)
	o := PushOutcome{Err: errors.New(diag)}
	switch {
	// A token is dead ONLY when FCM says so (details[].errorCode UNREGISTERED,
	// on any status, FCM sometimes answers 400). A bare 404 means a wrong
	// project id or endpoint: a configuration problem, never a token verdict.
	case unregistered:
		o.Permanent, o.Reason = true, "fcm_unregistered"
	case resp.StatusCode == http.StatusNotFound:
		o.Reason = "fcm_not_found"
		warnOnce.Do(func() {
			d.logger.Warn("notification: fcm answered 404 without UNREGISTERED; check push.fcm.project_id",
				"project_id", d.projectID)
		})
	case resp.StatusCode == http.StatusBadRequest:
		// Permanent only when the INVALID_ARGUMENT text concerns the
		// registration token (heuristic on FCM's English message; fails safe
		// to non-permanent). A bare 400 or a payload error is never permanent.
		if code == "INVALID_ARGUMENT" || eb.Error.Status == "INVALID_ARGUMENT" {
			if strings.Contains(strings.ToLower(eb.Error.Message), "token") {
				o.Permanent, o.Reason = true, "fcm_invalid"
				break
			}
		}
		o.Reason = "fcm_bad_request"
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		o.Reason = "transient"
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		o.Reason = "fcm_auth"
	default:
		o.Reason = "fcm_error"
	}
	return o
}

func bound(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func scrub(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, tokenPrefix(token)+"...")
}
