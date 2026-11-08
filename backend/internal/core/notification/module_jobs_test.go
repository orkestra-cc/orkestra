package notification

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/internal/core/notification/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/jobs"
	"github.com/orkestra/backend/pkg/sdk/module"
)

var _ module.HasJobs = (*NotificationModule)(nil)

type cleanupDevices struct {
	repository.DeviceTokenRepository
	rows         []models.DeviceToken // all active
	disabled     []string             // deviceIds disabled with session_ended
	staleBefore  time.Time
	deleteBefore time.Time
	batches      int
}

func (f *cleanupDevices) ListActiveBatch(_ context.Context, after string, limit int) ([]models.DeviceToken, error) {
	f.batches++
	var out []models.DeviceToken
	for _, r := range f.rows {
		if r.UUID > after && len(out) < limit {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *cleanupDevices) Disable(_ context.Context, _, _, deviceID, reason string, _ time.Time) (bool, error) {
	if reason == models.DeviceDisabledSessionEnded {
		f.disabled = append(f.disabled, deviceID)
	}
	return true, nil
}
func (f *cleanupDevices) DisableStale(_ context.Context, before, _ time.Time) (int64, error) {
	f.staleBefore = before
	return 2, nil
}
func (f *cleanupDevices) DeleteDisabledBefore(_ context.Context, before time.Time) (int64, error) {
	f.deleteBefore = before
	return 3, nil
}

type fakeReader struct {
	active map[string]bool
	err    error
}

func (r fakeReader) ActiveSessions(_ context.Context, ids []string) (map[string]bool, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = r.active[id]
	}
	return out, nil
}

func cleanupModule(devs *cleanupDevices, readers func(string) iface.SessionActivityReader, now time.Time) *NotificationModule {
	m := NewModule()
	m.pushCleanup = &services.PushCleanup{Devices: devs, Sessions: readers, Now: func() time.Time { return now }}
	return m
}

func TestJobsDeclaresPushTokensCleanup(t *testing.T) {
	defs := NewModule().Jobs()
	if len(defs) != 1 {
		t.Fatalf("%d jobs, want 1", len(defs))
	}
	d := defs[0].Normalized()
	if err := d.Validate(); err != nil {
		t.Fatalf("invalid: %v", err)
	}
	if d.Name != "push_tokens_cleanup" || d.Title != "Clean up push device tokens" ||
		d.Schedule.Kind() != jobs.ScheduleEvery || d.Schedule.Every() != 24*time.Hour {
		t.Errorf("definition = %+v", d)
	}
}

func TestPushCleanup_DisablesEndedSessionsAndPrunes(t *testing.T) {
	t0 := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	devs := &cleanupDevices{rows: []models.DeviceToken{
		{UUID: "a", Tier: "operator", UserUUID: "u1", DeviceID: "live", SessionID: "s-live"},
		{UUID: "b", Tier: "operator", UserUUID: "u1", DeviceID: "dead", SessionID: "s-dead"},
		{UUID: "c", Tier: "client", UserUUID: "u2", DeviceID: "cdead", SessionID: "s-cdead"},
	}}
	readers := func(tier string) iface.SessionActivityReader {
		if tier == "operator" {
			return fakeReader{active: map[string]bool{"s-live": true}}
		}
		return fakeReader{}
	}
	m := cleanupModule(devs, readers, t0)
	run := jobs.NewRun("notification.push_tokens_cleanup", "r1", jobs.TriggerSchedule, 1, t0)
	if err := m.Jobs()[0].Run(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	got := append([]string(nil), devs.disabled...)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "cdead" || got[1] != "dead" {
		t.Errorf("disabled = %v, want {dead, cdead} in any order", devs.disabled)
	}
	if !devs.staleBefore.Equal(t0.Add(-60*24*time.Hour)) || !devs.deleteBefore.Equal(t0.Add(-90*24*time.Hour)) {
		t.Errorf("cutoffs stale=%v delete=%v", devs.staleBefore, devs.deleteBefore)
	}
	d, _ := run.Details()
	if d["sessionEnded"] != int64(2) || d["stale"] != int64(2) || d["deleted"] != int64(3) {
		t.Errorf("details = %v", d)
	}
}

func TestPushCleanup_PagesInBatchesOf500(t *testing.T) {
	devs := &cleanupDevices{}
	for i := 0; i < 1100; i++ {
		devs.rows = append(devs.rows, models.DeviceToken{UUID: string(rune('a'+i/676)) + string(rune('a'+(i/26)%26)) + string(rune('a'+i%26)), Tier: "operator", DeviceID: "d", SessionID: "s"})
	}
	m := cleanupModule(devs, func(string) iface.SessionActivityReader { return fakeReader{} }, time.Now())
	run := jobs.NewRun("k", "r", jobs.TriggerSchedule, 1, time.Now())
	if err := m.Jobs()[0].Run(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if devs.batches != 3 || len(devs.disabled) != 1100 {
		t.Errorf("batches=%d disabled=%d, want 3 and 1100", devs.batches, len(devs.disabled))
	}
}

func TestPushCleanup_FailsClosedWithoutReader(t *testing.T) {
	devs := &cleanupDevices{rows: []models.DeviceToken{{UUID: "a", Tier: "operator", DeviceID: "d", SessionID: "s"}}}
	for name, m := range map[string]*NotificationModule{
		"no readers":      cleanupModule(devs, func(string) iface.SessionActivityReader { return nil }, time.Now()),
		"not initialised": NewModule(),
	} {
		run := jobs.NewRun("k", "r", jobs.TriggerSchedule, 1, time.Now())
		if err := m.Jobs()[0].Run(context.Background(), run); !errors.Is(err, services.ErrPushCleanupNoReader) {
			t.Errorf("%s: err = %v, want ErrPushCleanupNoReader", name, err)
		}
	}
	if len(devs.disabled) != 0 || !devs.staleBefore.IsZero() || !devs.deleteBefore.IsZero() || devs.batches != 0 {
		t.Errorf("something ran without a reader: %+v", devs)
	}
}

func TestPushCleanup_ReaderErrorDisablesNothing(t *testing.T) {
	devs := &cleanupDevices{rows: []models.DeviceToken{{UUID: "a", Tier: "operator", DeviceID: "d", SessionID: "s"}}}
	m := cleanupModule(devs, func(string) iface.SessionActivityReader { return fakeReader{err: errors.New("redis down")} }, time.Now())
	run := jobs.NewRun("k", "r", jobs.TriggerSchedule, 1, time.Now())
	if err := m.Jobs()[0].Run(context.Background(), run); err == nil {
		t.Fatal("reader error reported success")
	}
	if len(devs.disabled) != 0 || !devs.deleteBefore.IsZero() {
		t.Errorf("acted on an unreadable session state: %+v", devs)
	}
}

func TestPushCleanup_MissingTierReaderSkipsThatTier(t *testing.T) {
	devs := &cleanupDevices{rows: []models.DeviceToken{
		{UUID: "a", Tier: "operator", DeviceID: "od", SessionID: "s1"},
		{UUID: "b", Tier: "client", DeviceID: "cd", SessionID: "s2"},
	}}
	m := cleanupModule(devs, func(tier string) iface.SessionActivityReader {
		if tier == "operator" {
			return fakeReader{}
		}
		return nil
	}, time.Now())
	run := jobs.NewRun("k", "r", jobs.TriggerSchedule, 1, time.Now())
	if err := m.Jobs()[0].Run(context.Background(), run); !errors.Is(err, services.ErrPushCleanupNoReader) {
		t.Fatalf("err = %v", err)
	}
	if len(devs.disabled) != 1 || devs.disabled[0] != "od" {
		t.Errorf("disabled = %v, want only the operator row", devs.disabled)
	}
}
