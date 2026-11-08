package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

var _ iface.PushSender = (*NotificationService)(nil)

// ---- fakes ----------------------------------------------------------------

type fakePushLog struct {
	repository.NotificationRepository
	findErr error
	mu      sync.Mutex
	rows    []*models.NotificationDoc
}

func (f *fakePushLog) Create(_ context.Context, d *models.NotificationDoc) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *d
	f.rows = append(f.rows, &cp)
	return nil
}

func (f *fakePushLog) FindByIdempotencyKey(_ context.Context, key string, _ time.Time) (*models.NotificationDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if key == "" {
		return nil, nil
	}
	if f.findErr != nil {
		return nil, f.findErr
	}
	for _, r := range f.rows {
		if r.IdempotencyKey == key {
			return r, nil
		}
	}
	return nil, nil
}

type disableCall struct{ token, reason string }

type fakeDevices struct {
	repository.DeviceTokenRepository
	tokens   []models.DeviceToken
	err      error
	disabled []disableCall
}

func (f *fakeDevices) ActiveForUsers(_ context.Context, tier string, users []string) ([]models.DeviceToken, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []models.DeviceToken
	for _, t := range f.tokens {
		if t.Tier != tier || !t.Active {
			continue
		}
		for _, u := range users {
			if u == t.UserUUID {
				out = append(out, t)
			}
		}
	}
	return out, nil
}

func (f *fakeDevices) DisableByToken(_ context.Context, token, reason string, _ time.Time) error {
	f.disabled = append(f.disabled, disableCall{token, reason})
	for i := range f.tokens {
		if f.tokens[i].Token == token {
			f.tokens[i].Active = false
		}
	}
	return nil
}

type fakeSessions struct {
	active map[string]bool
	err    error
	calls  int
}

func (f *fakeSessions) ActiveSessions(_ context.Context, ids []string) (map[string]bool, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = f.active[id]
	}
	return out, nil
}

type fakePushDriver struct {
	outcomes map[string]PushOutcome // by token; default delivered
	sent     []PushMessage
}

func (d *fakePushDriver) Name() string { return "fake" }
func (d *fakePushDriver) Send(_ context.Context, msgs []PushMessage) []PushOutcome {
	d.sent = append(d.sent, msgs...)
	out := make([]PushOutcome, len(msgs))
	for i, m := range msgs {
		if o, ok := d.outcomes[m.Token]; ok {
			out[i] = o
		} else {
			out[i] = PushOutcome{Delivered: true}
		}
	}
	return out
}

type fakeDriverProvider struct {
	d   PushDriver
	err error
}

func (p fakeDriverProvider) PushDriver(context.Context) (PushDriver, error) { return p.d, p.err }

type pushHarness struct {
	svc      *PushService
	log      *fakePushLog
	devices  *fakeDevices
	sessions *fakeSessions
	driver   *fakePushDriver
	prefs    *fakePrefRepo
}

func newPushHarness(t *testing.T) *pushHarness {
	t.Helper()
	h := &pushHarness{
		log:      &fakePushLog{},
		devices:  &fakeDevices{},
		sessions: &fakeSessions{active: map[string]bool{}},
		driver:   &fakePushDriver{outcomes: map[string]PushOutcome{}},
		prefs:    newFakePrefRepo(),
	}
	h.svc = NewPushService(PushDeps{
		LogRepo:     h.log,
		Prefs:       NewPreferenceService(h.prefs),
		Devices:     h.devices,
		Drivers:     fakeDriverProvider{d: h.driver},
		Sessions:    func(string) iface.SessionActivityReader { return h.sessions },
		Logger:      discardLogger(),
		Idempotency: time.Hour,
	})
	return h
}

func (h *pushHarness) addDevice(user, token, session string) {
	h.devices.tokens = append(h.devices.tokens, models.DeviceToken{
		Tier: "operator", UserUUID: user, DeviceID: "d-" + token, SessionID: session,
		Token: token, Platform: "android", Active: true,
	})
	if session != "" {
		h.sessions.active[session] = true
	}
}

func pushReq(users ...string) iface.PushRequest {
	return iface.PushRequest{
		Tier: "operator", UserUUIDs: users, Category: "news", Type: "transactional",
		Title: "Hello", Body: "secret body",
	}
}

func oneUser(t *testing.T, r *iface.PushResult, err error) iface.PushUserResult {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Users) != 1 {
		t.Fatalf("users=%d", len(r.Users))
	}
	return r.Users[0]
}

// ---- tests ----------------------------------------------------------------

func TestSendPush_Delivered(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-aaaaaaaaaaaa", "s1")
	h.addDevice("u1", "tok-bbbbbbbbbbbb", "s2")
	res, err := h.svc.SendPush(context.Background(), pushReq("u1"))
	u := oneUser(t, res, err)
	if u.Status != iface.PushStatusDelivered || u.Attempted != 2 || u.Delivered != 2 {
		t.Fatalf("%+v", u)
	}
	if len(h.log.rows) != 1 {
		t.Fatalf("rows=%d", len(h.log.rows))
	}
	r := h.log.rows[0]
	if r.Channel != "push" || r.RecipientUserUUID != "u1" || r.Provider != "fake" || r.Subject != "Hello" || r.Status != models.StatusSent {
		t.Fatalf("%+v", r)
	}
	if r.Push == nil || r.Push.Attempted != 2 || r.Push.Delivered != 2 {
		t.Fatalf("%+v", r.Push)
	}
	if r.UUID == "" || r.Category != "news" || r.Type != "transactional" {
		t.Fatalf("%+v", r)
	}
}

func TestSendPush_NeverLogsBodyOrToken(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-aaaaaaaaaaaa", "s1")
	_, _ = h.svc.SendPush(context.Background(), pushReq("u1"))
	for _, r := range h.log.rows {
		s := r.Subject + r.Error + r.RecipientAddress + r.Provider
		if strings.Contains(s, "secret body") || strings.Contains(s, "tok-aaaa") {
			t.Fatalf("leak: %+v", r)
		}
	}
}

func TestSendPush_InvalidRequestWritesNothing(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "s1")
	req := pushReq("u1")
	req.Title = ""
	res, err := h.svc.SendPush(context.Background(), req)
	if res != nil || !errors.Is(err, iface.ErrPushInvalidRequest) {
		t.Fatalf("%v %v", res, err)
	}
	if len(h.log.rows) != 0 || len(h.driver.sent) != 0 {
		t.Fatal("wrote or sent")
	}
}

func TestSendPush_DriverUnavailable(t *testing.T) {
	h := newPushHarness(t)
	h.svc.drivers = fakeDriverProvider{err: errors.New("config broken")}
	res, err := h.svc.SendPush(context.Background(), pushReq("u1"))
	if res != nil || !errors.Is(err, iface.ErrPushUnavailable) {
		t.Fatalf("%v %v", res, err)
	}
}

// Review Focus 2: a session ended by any path, app never called DELETE.
func TestSendPush_EndedSessionTokenDisabledNotSent(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-ended-xxxx", "s-ended")
	h.sessions.active["s-ended"] = false
	h.addDevice("u1", "tok-live-xxxxx", "s-live")
	res, err := h.svc.SendPush(context.Background(), pushReq("u1"))
	u := oneUser(t, res, err)
	if u.Status != iface.PushStatusDelivered || u.Attempted != 1 || u.Delivered != 1 || u.Disabled != 1 {
		t.Fatalf("%+v", u)
	}
	if len(h.driver.sent) != 1 || h.driver.sent[0].Token != "tok-live-xxxxx" {
		t.Fatalf("sent %+v", h.driver.sent)
	}
	if len(h.devices.disabled) != 1 || h.devices.disabled[0] != (disableCall{"tok-ended-xxxx", models.DeviceDisabledSessionEnded}) {
		t.Fatalf("%+v", h.devices.disabled)
	}
}

func TestSendPush_AllSessionsEnded(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "s1")
	h.sessions.active["s1"] = false
	res, err := h.svc.SendPush(context.Background(), pushReq("u1"))
	u := oneUser(t, res, err)
	if u.Status != iface.PushStatusSkippedNoDevice || u.Disabled != 1 || len(h.driver.sent) != 0 {
		t.Fatalf("%+v", u)
	}
}

func TestSendPush_EmptySessionIDTreatedAsEnded(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "")
	res, err := h.svc.SendPush(context.Background(), pushReq("u1"))
	u := oneUser(t, res, err)
	if len(h.driver.sent) != 0 || u.Disabled != 1 || h.devices.disabled[0].reason != models.DeviceDisabledSessionEnded {
		t.Fatalf("%+v %+v", u, h.devices.disabled)
	}
}

func TestSendPush_SessionReaderErrorFailsClosed(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "s1")
	h.sessions.err = errors.New("mongo down")
	res, err := h.svc.SendPush(context.Background(), pushReq("u1"))
	u := oneUser(t, res, err)
	if u.Status != iface.PushStatusFailed || u.Error != "session check unavailable" {
		t.Fatalf("%+v", u)
	}
	if len(h.driver.sent) != 0 || len(h.devices.disabled) != 0 {
		t.Fatal("must not send or disable")
	}
}

func TestSendPush_SessionReaderMissingFailsClosed(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "s1")
	h.svc.sessions = func(string) iface.SessionActivityReader { return nil }
	res, err := h.svc.SendPush(context.Background(), pushReq("u1"))
	u := oneUser(t, res, err)
	if u.Status != iface.PushStatusFailed || u.Error != "session check unavailable" || len(h.driver.sent) != 0 {
		t.Fatalf("%+v", u)
	}
}

// Review Focus 4.
func TestSendPush_MarketingOptOutSkipped_TransactionalDelivered(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "s1")
	h.prefs.prefs[prefKey("u1", "news", "push")] = &models.PreferenceDoc{UserUUID: "u1", Category: "news", Channel: "push", OptedIn: false}

	req := pushReq("u1")
	req.Type = "marketing"
	u := h.send(t, req)
	if u.Status != iface.PushStatusSkippedPreference || len(h.driver.sent) != 0 {
		t.Fatalf("%+v", u)
	}
	if len(h.log.rows) != 1 || h.log.rows[0].Status != models.StatusSuppressed {
		t.Fatalf("%+v", h.log.rows)
	}

	req.Type = "transactional"
	u = h.send(t, req)
	if u.Status != iface.PushStatusDelivered {
		t.Fatalf("%+v", u)
	}
}

func (h *pushHarness) send(t *testing.T, req iface.PushRequest) iface.PushUserResult {
	t.Helper()
	r, err := h.svc.SendPush(context.Background(), req)
	return oneUser(t, r, err)
}

// Review Focus 5.
func TestSendPush_IdempotencyKeyNotDeliveredTwice(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "s1")
	h.addDevice("u2", "tok-2", "s2")
	req := pushReq("u1")
	req.IdempotencyKey = "k1"
	if u := h.send(t, req); u.Status != iface.PushStatusDelivered {
		t.Fatalf("%+v", u)
	}
	u := h.send(t, req)
	if u.Status != iface.PushStatusSkippedDuplicate || len(h.driver.sent) != 1 {
		t.Fatalf("%+v sent=%d", u, len(h.driver.sent))
	}
	// the key is per user: another user with the same key still gets it
	req.UserUUIDs = []string{"u2"}
	if u := h.send(t, req); u.Status != iface.PushStatusDelivered {
		t.Fatalf("%+v", u)
	}
	if len(h.driver.sent) != 2 {
		t.Fatalf("sent=%d", len(h.driver.sent))
	}
}

func TestSendPush_EmptyKeyNoDedup(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "s1")
	h.svc.SendPush(context.Background(), pushReq("u1"))
	h.svc.SendPush(context.Background(), pushReq("u1"))
	if len(h.driver.sent) != 2 {
		t.Fatalf("sent=%d", len(h.driver.sent))
	}
}

func TestSendPush_NoDevice(t *testing.T) {
	h := newPushHarness(t)
	u := h.send(t, pushReq("u1"))
	if u.Status != iface.PushStatusSkippedNoDevice {
		t.Fatalf("%+v", u)
	}
	if h.sessions.calls != 0 {
		t.Fatal("no session query without tokens")
	}
}

func TestSendPush_MixedIsPartial_PermanentDisablesToken(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-dead-xxxx", "s1")
	h.addDevice("u1", "tok-good-xxxx", "s2")
	h.driver.outcomes["tok-dead-xxxx"] = PushOutcome{Permanent: true, Reason: "fcm_unregistered", Err: errors.New("x")}
	u := h.send(t, pushReq("u1"))
	if u.Status != iface.PushStatusPartial || u.Attempted != 2 || u.Delivered != 1 || u.Disabled != 1 {
		t.Fatalf("%+v", u)
	}
	if len(h.devices.disabled) != 1 || h.devices.disabled[0] != (disableCall{"tok-dead-xxxx", "fcm_unregistered"}) {
		t.Fatalf("%+v", h.devices.disabled)
	}
	if h.log.rows[0].Status != models.StatusPartial {
		t.Fatalf("%+v", h.log.rows[0])
	}
}

func TestSendPush_AllFailedTransientNotDisabled(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "s1")
	h.driver.outcomes["tok-1"] = PushOutcome{Reason: "transient", Err: errors.New("boom")}
	u := h.send(t, pushReq("u1"))
	if u.Status != iface.PushStatusFailed || u.Delivered != 0 || u.Disabled != 0 || len(h.devices.disabled) != 0 {
		t.Fatalf("%+v", u)
	}
	// a failed send must not poison a retry with the same key
	h.devices.tokens[0].Active = true
	req := pushReq("u1")
	req.IdempotencyKey = "k"
	h.svc.SendPush(context.Background(), req)
	delete(h.driver.outcomes, "tok-1")
	u = h.send(t, req)
	if u.Status != iface.PushStatusDelivered {
		t.Fatalf("retry: %+v", u)
	}
}

func TestSendPush_MessageFieldsAndTitleTruncationInLog(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "s1")
	req := pushReq("u1")
	req.Data = map[string]string{"k": "v"}
	req.CollapseKey = "c"
	req.Priority = "normal"
	req.TTL = time.Minute
	h.svc.SendPush(context.Background(), req)
	m := h.driver.sent[0]
	if m.Title != "Hello" || m.Body != "secret body" || m.Data["k"] != "v" || m.CollapseKey != "c" || m.Priority != "normal" || m.TTL != time.Minute || m.Platform != "android" {
		t.Fatalf("%+v", m)
	}
	if got := truncateRunes(strings.Repeat("é", 250), 200); len([]rune(got)) != 200 {
		t.Fatalf("len=%d", len([]rune(got)))
	}
}

func TestSendPush_DeviceLookupErrorFailsUsers(t *testing.T) {
	h := newPushHarness(t)
	h.devices.err = errors.New("db")
	u := h.send(t, pushReq("u1"))
	if u.Status != iface.PushStatusFailed {
		t.Fatalf("%+v", u)
	}
}

func TestSendPush_PerUserIndependent(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "s1")
	res, err := h.svc.SendPush(context.Background(), pushReq("u1", "u2"))
	if err != nil || len(res.Users) != 2 || res.Users[0].UserUUID != "u1" || res.Users[0].Status != "delivered" || res.Users[1].Status != "skipped_no_device" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestNotificationService_SendPushWithoutPushServiceUnavailable(t *testing.T) {
	s := &NotificationService{}
	if _, err := s.SendPush(context.Background(), pushReq("u1")); !errors.Is(err, iface.ErrPushUnavailable) {
		t.Fatal(err)
	}
}

func TestSendPush_IdempotencyLookupErrorFailsClosed(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-1", "s1")
	h.log.findErr = errors.New("mongo down")
	req := pushReq("u1")
	req.IdempotencyKey = "k"
	u := h.send(t, req)
	if u.Status != iface.PushStatusFailed || u.Error != "idempotency check unavailable" {
		t.Fatalf("%+v", u)
	}
	if len(h.driver.sent) != 0 || len(h.devices.disabled) != 0 {
		t.Fatal("must not send or disable")
	}
	// no key: lookup is not consulted, delivery proceeds
	if u := h.send(t, pushReq("u1")); u.Status != iface.PushStatusDelivered {
		t.Fatalf("%+v", u)
	}
}
