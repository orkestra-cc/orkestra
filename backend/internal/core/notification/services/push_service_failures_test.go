package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (h *pushHarness) captureLogs() *syncBuffer {
	buf := &syncBuffer{}
	h.svc.logger = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return buf
}

// I2: the reasons of undelivered outcomes reach the result, the delivery-log
// row and one Warn per distinct reason, never with a token or provider text.
func TestSendPush_FailureReasonsReportedAndLoggedOnce(t *testing.T) {
	h := newPushHarness(t)
	logs := h.captureLogs()
	h.addDevice("u1", "tok-auth-1111111", "s1")
	h.addDevice("u1", "tok-auth-2222222", "s2")
	h.addDevice("u1", "tok-tran-3333333", "s3")
	h.addDevice("u2", "tok-auth-4444444", "s4")
	providerText := "Request had invalid authentication credentials"
	h.driver.outcomes["tok-auth-1111111"] = PushOutcome{Reason: "fcm_auth", Err: errors.New("fcm: http 401 UNAUTHENTICATED " + providerText)}
	h.driver.outcomes["tok-auth-2222222"] = PushOutcome{Reason: "fcm_auth", Err: errors.New("fcm: http 401 UNAUTHENTICATED " + providerText)}
	h.driver.outcomes["tok-tran-3333333"] = PushOutcome{Reason: "transient", Err: errors.New("fcm: http 503")}
	h.driver.outcomes["tok-auth-4444444"] = PushOutcome{Reason: "fcm_auth", Err: errors.New("fcm: http 401 UNAUTHENTICATED " + providerText)}

	res, err := h.svc.SendPush(context.Background(), pushReq("u1", "u2"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := res.Users[0].Error, "delivery failed: fcm_auth ×2, transient ×1"; got != want {
		t.Fatalf("u1 Error = %q, want %q", got, want)
	}
	if got, want := res.Users[1].Error, "delivery failed: fcm_auth ×1"; got != want {
		t.Fatalf("u2 Error = %q, want %q", got, want)
	}
	if h.log.rows[0].Error != res.Users[0].Error {
		t.Fatalf("log row Error = %q", h.log.rows[0].Error)
	}

	out := logs.String()
	if n := strings.Count(out, "push delivery failed"); n != 2 {
		t.Fatalf("want one Warn per distinct reason (2), got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "reason=fcm_auth count=3 driver=fake") || !strings.Contains(out, "reason=transient count=1 driver=fake") {
		t.Fatalf("Warn lines must carry reason, count and driver:\n%s", out)
	}
	for _, leak := range []string{"tok-auth", "tok-tran", providerText, "secret body"} {
		if strings.Contains(out, leak) {
			t.Fatalf("log leaks %q:\n%s", leak, out)
		}
	}
}

func TestSendPush_PartialCarriesReasons(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-dead-xxxx", "s1")
	h.addDevice("u1", "tok-good-xxxx", "s2")
	h.driver.outcomes["tok-dead-xxxx"] = PushOutcome{Permanent: true, Reason: "fcm_unregistered"}
	u := h.send(t, pushReq("u1"))
	if u.Status != iface.PushStatusPartial || u.Error != "partially delivered: fcm_unregistered ×1" {
		t.Fatalf("%+v", u)
	}
}

func TestSendPush_DeliveredHasNoErrorNorWarn(t *testing.T) {
	h := newPushHarness(t)
	logs := h.captureLogs()
	h.addDevice("u1", "tok-good-xxxx", "s1")
	u := h.send(t, pushReq("u1"))
	if u.Status != iface.PushStatusDelivered || u.Error != "" {
		t.Fatalf("%+v", u)
	}
	if strings.Contains(logs.String(), "push delivery failed") {
		t.Fatalf("no failure Warn expected:\n%s", logs.String())
	}
}

// ---- T4-3: post-send writes survive the caller's cancellation ----------

type cancelingDriver struct {
	*fakePushDriver
	cancel context.CancelFunc
}

func (d *cancelingDriver) Send(ctx context.Context, msgs []PushMessage) []PushOutcome {
	out := d.fakePushDriver.Send(ctx, msgs)
	d.cancel() // the caller gives up right after the provider answered
	return out
}

type ctxRecordingLog struct {
	*fakePushLog
	errs      []error
	deadlines []bool
}

func (l *ctxRecordingLog) Create(ctx context.Context, d *models.NotificationDoc) error {
	l.errs = append(l.errs, ctx.Err())
	_, ok := ctx.Deadline()
	l.deadlines = append(l.deadlines, ok)
	return l.fakePushLog.Create(ctx, d)
}

type ctxRecordingDevices struct {
	*fakeDevices
	errs      []error
	deadlines []bool
}

func (f *ctxRecordingDevices) DisableByToken(ctx context.Context, token, reason string, at time.Time) error {
	f.errs = append(f.errs, ctx.Err())
	_, ok := ctx.Deadline()
	f.deadlines = append(f.deadlines, ok)
	return f.fakeDevices.DisableByToken(ctx, token, reason, at)
}

func TestSendPush_PostSendWritesDetachedFromCallerCancel(t *testing.T) {
	h := newPushHarness(t)
	h.addDevice("u1", "tok-dead-xxxx", "s1")
	h.addDevice("u1", "tok-good-xxxx", "s2")
	h.driver.outcomes["tok-dead-xxxx"] = PushOutcome{Permanent: true, Reason: "fcm_unregistered"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &ctxRecordingLog{fakePushLog: h.log}
	devices := &ctxRecordingDevices{fakeDevices: h.devices}
	h.svc.log = log
	h.svc.devices = devices
	h.svc.drivers = fakeDriverProvider{d: &cancelingDriver{fakePushDriver: h.driver, cancel: cancel}}

	req := pushReq("u1")
	req.IdempotencyKey = "k-1"
	res, err := h.svc.SendPush(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("test setup: the caller context must be canceled after the driver call")
	}
	if res.Users[0].Status != iface.PushStatusPartial {
		t.Fatalf("%+v", res.Users[0])
	}
	if len(devices.errs) != 1 || devices.errs[0] != nil || !devices.deadlines[0] {
		t.Fatalf("dead-token disable must run on a live, bounded context: errs=%v deadlines=%v", devices.errs, devices.deadlines)
	}
	if len(log.errs) != 1 || log.errs[0] != nil || !log.deadlines[0] {
		t.Fatalf("delivery-log write must run on a live, bounded context: errs=%v deadlines=%v", log.errs, log.deadlines)
	}
	if h.log.rows[0].IdempotencyKey == "" {
		t.Fatal("the idempotency row must be kept so a retry does not resend")
	}
}
