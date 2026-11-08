package services

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	pushSubjectMax      = 200
	pushSessionUnavail  = "session check unavailable"
	pushDeviceLookupErr = "device lookup failed"
	pushKeyPrefix       = "push:"
	// pushWriteTimeout bounds each write made after the driver call (the
	// dead-token disables and the delivery-log rows). Those writes run
	// detached from the caller's cancellation: once the provider has
	// answered, losing them would drop the idempotency row (a retry would
	// resend) or keep a dead token active.
	pushWriteTimeout = 5 * time.Second
)

// PushDriverProvider yields the active push driver for one send.
type PushDriverProvider interface {
	PushDriver(ctx context.Context) (PushDriver, error)
}

// PushSessionReaders resolves, per send, the session reader of a tier. It is
// a function because the auth module inits after notification: the readers
// only exist in the ServiceRegistry once the platform is up. nil result =
// not available (fail closed).
type PushSessionReaders func(tier string) iface.SessionActivityReader

// PushDeps are the collaborators of PushService.
type PushDeps struct {
	LogRepo     repository.NotificationRepository
	Prefs       PreferenceService
	Devices     repository.DeviceTokenRepository
	Drivers     PushDriverProvider
	Sessions    PushSessionReaders
	Logger      *slog.Logger
	Idempotency time.Duration // dedup window; default 1h
}

// PushService turns an iface.PushRequest into device deliveries.
type PushService struct {
	log      repository.NotificationRepository
	prefs    PreferenceService
	devices  repository.DeviceTokenRepository
	drivers  PushDriverProvider
	sessions PushSessionReaders
	logger   *slog.Logger
	ttl      time.Duration
	now      func() time.Time
}

func NewPushService(d PushDeps) *PushService {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Idempotency == 0 {
		d.Idempotency = time.Hour
	}
	return &PushService{
		log: d.LogRepo, prefs: d.Prefs, devices: d.Devices, drivers: d.Drivers,
		sessions: d.Sessions, logger: d.Logger, ttl: d.Idempotency, now: time.Now,
	}
}

// pushUserState tracks one user through the pipeline.
type pushUserState struct {
	res       iface.PushUserResult
	tokens    []models.DeviceToken // usable (live session) tokens to send to
	done      bool                 // result final, no send needed
	idemKey   string
	logStatus string
	failures  map[string]int // undelivered outcomes by driver reason
}

// formatPushReasons renders reason counts in a stable order, e.g.
// "fcm_auth ×2, transient ×1". Reasons are driver codes, never tokens.
func formatPushReasons(counts map[string]int) string {
	reasons := make([]string, 0, len(counts))
	for r := range counts {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	parts := make([]string, len(reasons))
	for i, r := range reasons {
		parts[i] = fmt.Sprintf("%s ×%d", r, counts[r])
	}
	return strings.Join(parts, ", ")
}

// writeCtx detaches a post-send write from the caller's cancellation and
// bounds it with pushWriteTimeout.
func writeCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), pushWriteTimeout)
}

func pushIdemKey(key, user string) string {
	if key == "" {
		return ""
	}
	return pushKeyPrefix + key + ":" + user
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// SendPush implements iface.PushSender.
func (p *PushService) SendPush(ctx context.Context, req iface.PushRequest) (*iface.PushResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	driver, err := p.drivers.PushDriver(ctx)
	if err != nil || driver == nil {
		p.logger.Error("notification: push driver unavailable", "error", err)
		return nil, fmt.Errorf("%w: driver not available", iface.ErrPushUnavailable)
	}

	states := make([]*pushUserState, 0, len(req.UserUUIDs))
	byUser := map[string]*pushUserState{}
	var candidates []string
	for _, u := range req.UserUUIDs {
		st := &pushUserState{res: iface.PushUserResult{UserUUID: u}, idemKey: pushIdemKey(req.IdempotencyKey, u)}
		states = append(states, st)
		if _, dup := byUser[u]; dup {
			st.res.Status, st.done = iface.PushStatusSkippedDuplicate, true
			continue
		}
		byUser[u] = st
		if st.idemKey != "" {
			ex, ierr := p.log.FindByIdempotencyKey(ctx, st.idemKey, p.now().Add(-p.ttl))
			if ierr != nil {
				// Fail closed: without the dedup answer a retry could deliver twice.
				p.logger.Error("notification: push idempotency lookup failed", "error", ierr)
				st.res.Status, st.res.Error, st.done = iface.PushStatusFailed, "idempotency check unavailable", true
				st.idemKey = ""
				continue
			}
			if ex != nil {
				st.res.Status, st.done = iface.PushStatusSkippedDuplicate, true
				continue
			}
		}
		ok, perr := p.prefs.CanDeliver(ctx, u, req.Category, models.ChannelPush, req.Type)
		if perr != nil {
			st.res.Status, st.res.Error, st.done = iface.PushStatusFailed, "preference check failed", true
			st.idemKey = ""
			continue
		}
		if !ok {
			st.res.Status, st.done = iface.PushStatusSkippedPreference, true
			continue
		}
		candidates = append(candidates, u)
	}

	if len(candidates) > 0 {
		p.resolveDevices(ctx, req, candidates, byUser)
	}

	// One driver call for the whole request.
	var msgs []PushMessage
	var owners []*pushUserState
	for _, st := range states {
		if st.done {
			continue
		}
		for _, t := range st.tokens {
			msgs = append(msgs, PushMessage{
				Token: t.Token, Title: req.Title, Body: req.Body, Data: req.Data,
				CollapseKey: req.CollapseKey, Priority: req.Priority, TTL: req.TTL, Platform: t.Platform,
			})
			owners = append(owners, st)
		}
	}
	var outcomes []PushOutcome
	if len(msgs) > 0 {
		outcomes = driver.Send(ctx, msgs)
	}
	failures := map[string]int{} // undelivered outcomes of the whole send, by reason
	for i, m := range msgs {
		st := owners[i]
		st.res.Attempted++
		var o PushOutcome
		if i < len(outcomes) {
			o = outcomes[i]
		} else {
			o = PushOutcome{Reason: "transient"}
		}
		if o.Delivered {
			st.res.Delivered++
			continue
		}
		reason := o.Reason
		if reason == "" {
			if o.Permanent {
				reason = models.DeviceDisabledFCMInvalid
			} else {
				reason = "transient"
			}
		}
		if st.failures == nil {
			st.failures = map[string]int{}
		}
		st.failures[reason]++
		failures[reason]++
		if o.Permanent {
			wctx, cancel := writeCtx(ctx)
			derr := p.devices.DisableByToken(wctx, m.Token, reason, p.now())
			cancel()
			if derr != nil {
				p.logger.Warn("notification: push disable token failed", "device_ref", tokenPrefix(m.Token), "error", derr)
			} else {
				st.res.Disabled++
			}
		}
	}
	// One Warn per distinct failure reason per send: the reason code, how
	// many devices it hit and the driver. Never a token, and never the
	// provider's own text (the module's error contract).
	reasons := make([]string, 0, len(failures))
	for r := range failures {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	for _, r := range reasons {
		p.logger.Warn("notification: push delivery failed", "reason", r, "count", failures[r], "driver", driver.Name())
	}

	out := &iface.PushResult{Users: make([]iface.PushUserResult, 0, len(states))}
	for _, st := range states {
		if !st.done {
			switch {
			case st.res.Attempted == 0:
				st.res.Status = iface.PushStatusSkippedNoDevice
			case st.res.Delivered == st.res.Attempted:
				st.res.Status = iface.PushStatusDelivered
			case st.res.Delivered > 0:
				st.res.Status = iface.PushStatusPartial
				st.res.Error = "partially delivered: " + formatPushReasons(st.failures)
			default:
				st.res.Status = iface.PushStatusFailed
				st.res.Error = "delivery failed: " + formatPushReasons(st.failures)
			}
		}
		out.Users = append(out.Users, st.res)
		p.record(ctx, req, driver.Name(), st)
	}
	return out, nil
}

// resolveDevices loads active tokens for the candidate users, applies the
// session filter (fail closed) and fills each state's usable tokens.
func (p *PushService) resolveDevices(ctx context.Context, req iface.PushRequest, users []string, byUser map[string]*pushUserState) {
	toks, err := p.devices.ActiveForUsers(ctx, req.Tier, users)
	if err != nil {
		p.logger.Error("notification: push device lookup failed", "error", err)
		for _, u := range users {
			st := byUser[u]
			st.res.Status, st.res.Error, st.done = iface.PushStatusFailed, pushDeviceLookupErr, true
			st.idemKey = ""
		}
		return
	}
	var ids []string
	seen := map[string]bool{}
	for _, t := range toks {
		if t.SessionID != "" && !seen[t.SessionID] {
			seen[t.SessionID] = true
			ids = append(ids, t.SessionID)
		}
	}
	var active map[string]bool
	var sessErr bool
	if len(ids) > 0 {
		var reader iface.SessionActivityReader
		if p.sessions != nil {
			reader = p.sessions(req.Tier)
		}
		if reader == nil {
			sessErr = true
		} else if m, rerr := reader.ActiveSessions(ctx, ids); rerr != nil {
			p.logger.Error("notification: push session check failed", "error", rerr)
			sessErr = true
		} else {
			active = m
		}
	}
	for _, t := range toks {
		st := byUser[t.UserUUID]
		if st == nil || st.done {
			continue
		}
		if t.SessionID == "" || (!sessErr && !active[t.SessionID]) {
			if derr := p.devices.DisableByToken(ctx, t.Token, models.DeviceDisabledSessionEnded, p.now()); derr != nil {
				p.logger.Warn("notification: push disable token failed", "device_ref", tokenPrefix(t.Token), "error", derr)
			} else {
				st.res.Disabled++
			}
			continue
		}
		if sessErr {
			continue // handled below; nothing sent, nothing disabled
		}
		st.tokens = append(st.tokens, t)
	}
	if sessErr {
		for _, t := range toks {
			if st := byUser[t.UserUUID]; st != nil && !st.done && t.SessionID != "" {
				st.res.Status, st.res.Error, st.done = iface.PushStatusFailed, pushSessionUnavail, true
				st.idemKey = ""
				st.tokens = nil
			}
		}
	}
}

// record writes the one delivery-log row of (request, user). A duplicate
// writes nothing (the original row exists). Only delivered/partial rows
// carry the idempotency key: a failed or device-less attempt must stay
// retryable. Never stores body, data or token.
func (p *PushService) record(ctx context.Context, req iface.PushRequest, provider string, st *pushUserState) {
	if st.res.Status == iface.PushStatusSkippedDuplicate {
		return
	}
	var status string
	switch st.res.Status {
	case iface.PushStatusDelivered:
		status = models.StatusSent
	case iface.PushStatusPartial:
		status = models.StatusPartial
	case iface.PushStatusSkippedPreference:
		status = models.StatusSuppressed
	case iface.PushStatusSkippedNoDevice:
		status = models.StatusSkippedNoDevice
	default:
		status = models.StatusFailed
	}
	now := p.now()
	doc := &models.NotificationDoc{
		UUID:              uuid.NewString(),
		Channel:           models.ChannelPush,
		Type:              req.Type,
		Category:          req.Category,
		RecipientUserUUID: st.res.UserUUID,
		Subject:           truncateRunes(req.Title, pushSubjectMax),
		Status:            status,
		Provider:          provider,
		Error:             st.res.Error,
		CreatedAt:         now,
		Push: &models.PushStats{
			Attempted: st.res.Attempted, Delivered: st.res.Delivered, Disabled: st.res.Disabled,
		},
	}
	if status == models.StatusSent || status == models.StatusPartial {
		doc.SentAt = &now
		doc.IdempotencyKey = st.idemKey
	}
	wctx, cancel := writeCtx(ctx)
	defer cancel()
	if err := p.log.Create(wctx, doc); err != nil {
		p.logger.Warn("notification: push log write failed", "user", st.res.UserUUID, "error", err)
	}
}
