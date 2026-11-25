package jobs

import (
	"strings"
	"testing"
	"time"
)

func TestSchedule(t *testing.T) {
	if err := Every(30 * time.Second).Validate(); err == nil {
		t.Error("30s every should be invalid")
	}
	if err := Every(time.Minute).Validate(); err != nil {
		t.Error(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if n, ok := Every(time.Hour).Next(now, nil); !ok || !n.Equal(now.Add(time.Hour)) {
		t.Errorf("every next %v %v", n, ok)
	}
	if _, ok := Manual().Next(now, nil); ok {
		t.Error("manual must not schedule")
	}
	if err := Manual().Validate(); err != nil {
		t.Error(err)
	}
	if err := (Schedule{}).Validate(); err == nil {
		t.Error("zero schedule invalid")
	}
	if err := Cron("nope").Validate(); err == nil {
		t.Error("bad cron invalid")
	}
	err := Cron("0 0 31 2 *").Validate()
	if err == nil || !strings.Contains(err.Error(), "never") {
		t.Errorf("never-matching: %v", err)
	}
	if err := Cron("0 2 * * *").Validate(); err != nil {
		t.Error(err)
	}
	if n, ok := Cron("0 2 * * *").Next(now, nil); !ok || !n.Equal(now.Add(2*time.Hour)) {
		t.Errorf("cron next %v %v", n, ok)
	}
	if _, ok := Cron("bad").Next(now, nil); ok {
		t.Error("invalid cron has no next")
	}
	for s, want := range map[Schedule]string{
		Every(time.Hour):  "every 1h0m0s",
		Cron("0 2 * * *"): "cron 0 2 * * *",
		Manual():          "manual",
	} {
		if s.String() != want {
			t.Errorf("String() = %q want %q", s.String(), want)
		}
	}
	if Every(time.Hour).Kind() != ScheduleEvery || Cron("@daily").Kind() != ScheduleCron || Manual().Kind() != ScheduleManual {
		t.Error("kinds")
	}
	if Every(time.Hour).Every() != time.Hour || Cron("@daily").Expr() != "@daily" {
		t.Error("accessors")
	}
}
