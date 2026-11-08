package scheduler

import (
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/models"
	"github.com/orkestra/backend/pkg/sdk/jobs"
)

func entryOf(d jobs.Definition) jobs.Entry {
	d.Title, d.Run = "t", ok
	return jobs.Entry{Key: "m." + d.Name, Module: "m", Def: d.Normalized()}
}

func TestEffectiveOf(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("no tzdata")
	}
	off := false
	on := true
	cron := entryOf(jobs.Definition{Name: "c", Schedule: jobs.Cron("0 2 * * *")})
	manual := entryOf(jobs.Definition{Name: "x", Schedule: jobs.Manual()})
	dark := entryOf(jobs.Definition{Name: "d", Schedule: jobs.Every(time.Hour), DisabledByDefault: true})

	cases := []struct {
		name   string
		entry  jobs.Entry
		st     *models.State
		defLoc *time.Location
		want   string
	}{
		{"default no state", cron, nil, nil, "on|cron 0 2 * * *|UTC"},
		{"module timezone", cron, &models.State{}, rome, "on|cron 0 2 * * *|Europe/Rome"},
		{"disabled override", cron, &models.State{Override: &models.Override{Enabled: &off}}, nil, "off|cron 0 2 * * *|UTC"},
		{"schedule override every", cron, &models.State{Override: &models.Override{Schedule: &models.ScheduleSpec{Kind: "every", EverySeconds: 7200}}}, nil, "on|every 2h0m0s|UTC"},
		{"schedule override cron + tz", cron, &models.State{Override: &models.Override{Schedule: &models.ScheduleSpec{Kind: "cron", Cron: "30 3 * * mon"}, Timezone: "Europe/Rome"}}, nil, "on|cron 30 3 * * mon|Europe/Rome"},
		{"manual ignores schedule override", manual, &models.State{Override: &models.Override{Schedule: &models.ScheduleSpec{Kind: "every", EverySeconds: 3600}}}, nil, "on|manual|UTC"},
		{"disabled by default", dark, nil, nil, "off|every 1h0m0s|UTC"},
		{"enabled override of disabled default", dark, &models.State{Override: &models.Override{Enabled: &on}}, nil, "on|every 1h0m0s|UTC"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eff, err := EffectiveOf(c.entry, c.st, c.defLoc)
			if err != nil {
				t.Fatal(err)
			}
			if eff.Fingerprint != c.want {
				t.Fatalf("fingerprint = %q, want %q", eff.Fingerprint, c.want)
			}
			if eff.Location == nil {
				t.Fatal("nil location")
			}
		})
	}

	bad := []*models.State{
		{Override: &models.Override{Schedule: &models.ScheduleSpec{Kind: "cron", Cron: "61 * * * *"}}},
		{Override: &models.Override{Schedule: &models.ScheduleSpec{Kind: "every", EverySeconds: 5}}},
		{Override: &models.Override{Schedule: &models.ScheduleSpec{Kind: "weekly"}}},
		{Override: &models.Override{Timezone: "Mars/Olympus"}},
	}
	for i, st := range bad {
		if _, err := EffectiveOf(cron, st, nil); err == nil {
			t.Errorf("bad override %d accepted", i)
		}
	}
}

func TestNextRun(t *testing.T) {
	every := Effective{Enabled: true, Schedule: jobs.Every(time.Hour), Location: time.UTC}
	if got := NextRun(every, t0); got == nil || !got.Equal(t0.Add(time.Hour)) {
		t.Fatalf("every: %v", got)
	}
	every.Enabled = false
	if NextRun(every, t0) != nil {
		t.Fatal("disabled must have no next run")
	}
	if NextRun(Effective{Enabled: true, Schedule: jobs.Manual(), Location: time.UTC}, t0) != nil {
		t.Fatal("manual must have no next run")
	}
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("no tzdata")
	}
	c := Effective{Enabled: true, Schedule: jobs.Cron("0 2 * * *"), Location: rome}
	if got := NextRun(c, t0); got == nil || !got.Equal(time.Date(2026, 11, 9, 2, 0, 0, 0, rome)) {
		t.Fatalf("cron: %v", got)
	}
}
