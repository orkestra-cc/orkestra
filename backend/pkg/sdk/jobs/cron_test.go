package jobs

import (
	"strings"
	"testing"
	"time"
)

func TestParseCron_Valid(t *testing.T) {
	for _, e := range []string{"*/15 9-17 * * mon-fri", "0 2 * * *", "@daily", "0 0 1 jan *", "0 0 * * 7",
		"5/10 * * * *", "0,30 * * * *", "@Hourly", "0 0 * * SUN", "@yearly", "@annually", "@monthly", "@weekly", "@midnight"} {
		c, err := ParseCron(e)
		if err != nil {
			t.Errorf("%q: unexpected error %v", e, err)
			continue
		}
		if c.String() != e {
			t.Errorf("String() = %q want %q", c.String(), e)
		}
	}
}

func TestParseCron_Invalid(t *testing.T) {
	cases := []struct{ expr, want string }{
		{"", "expression"},
		{"* * * *", "5 fields"},
		{"60 * * * *", "minute"},
		{"*/0 * * * *", "minute"},
		{"5-1 * * * *", "minute"},
		{"* * * foo *", "month"},
		{"* 24 * * *", "hour"},
		{"* * 0 * *", "day-of-month"},
		{"* * * * 8", "day-of-week"},
		{"@reboot", "@reboot"},
	}
	for _, tc := range cases {
		_, err := ParseCron(tc.expr)
		if err == nil {
			t.Errorf("%q: expected error", tc.expr)
		} else if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %q lacks %q", tc.expr, err, tc.want)
		}
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCronNext(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	next := func(expr string, after time.Time, loc *time.Location) (time.Time, bool) {
		c, err := ParseCron(expr)
		if err != nil {
			t.Fatal(err)
		}
		return c.Next(after, loc)
	}

	got, ok := next("0 2 * * *", mustTime(t, "2026-03-07T02:00:00Z"), nil)
	if !ok || !got.Equal(mustTime(t, "2026-03-08T02:00:00Z")) {
		t.Errorf("daily: %v %v", got, ok)
	}

	// DST gap: 29 March 02:30 does not exist in Rome.
	got, ok = next("30 2 * * *", time.Date(2026, 3, 28, 12, 0, 0, 0, rome), rome)
	if !ok || !got.Equal(mustTime(t, "2026-03-30T02:30:00+02:00")) {
		t.Errorf("gap: %v %v", got, ok)
	}

	// Ambiguous hour: fires once, at the first occurrence.
	got, ok = next("30 2 * * *", time.Date(2026, 10, 24, 12, 0, 0, 0, rome), rome)
	if !ok || !got.Equal(mustTime(t, "2026-10-25T02:30:00+02:00")) {
		t.Fatalf("ambiguous first: %v %v", got, ok)
	}
	got2, ok := next("30 2 * * *", got, rome)
	if !ok || !got2.Equal(mustTime(t, "2026-10-26T02:30:00+01:00")) {
		t.Errorf("ambiguous next: %v %v", got2, ok)
	}
	// Starting inside the repeated hour (second occurrence of 02:10) must not refire 02:30.
	inside := mustTime(t, "2026-10-25T02:10:00+01:00")
	got3, ok := next("30 2 * * *", inside, rome)
	if !ok || !got3.Equal(mustTime(t, "2026-10-26T02:30:00+01:00")) {
		t.Errorf("repeated hour: %v %v", got3, ok)
	}

	// Vixie rule: 13th OR Friday. 2026-11-08 is a Sunday.
	after := mustTime(t, "2026-11-08T00:00:00Z")
	got, _ = next("0 0 13 * fri", after, nil)
	if !got.Equal(mustTime(t, "2026-11-13T00:00:00Z")) { // Friday and the 13th
		t.Errorf("fri13: %v", got)
	}
	got, _ = next("0 0 13 * fri", mustTime(t, "2026-11-13T00:00:00Z"), nil)
	if !got.Equal(mustTime(t, "2026-11-20T00:00:00Z")) {
		t.Errorf("fri: %v", got)
	}
	got, _ = next("0 0 13 * fri", mustTime(t, "2026-12-01T00:00:00Z"), nil)
	if !got.Equal(mustTime(t, "2026-12-04T00:00:00Z")) {
		t.Errorf("fri: %v", got)
	}
	got, _ = next("0 0 13 * fri", mustTime(t, "2026-12-05T00:00:00Z"), nil)
	if !got.Equal(mustTime(t, "2026-12-11T00:00:00Z")) {
		t.Errorf("fri: %v", got)
	}
	got, _ = next("0 0 13 * mon", mustTime(t, "2026-12-08T00:00:00Z"), nil) // Tue; Mon 14th, 13th is Sunday
	if !got.Equal(mustTime(t, "2026-12-13T00:00:00Z")) {
		t.Errorf("13th: %v", got)
	}

	// Never matching.
	if _, ok := next("0 0 31 2 *", after, nil); ok {
		t.Error("31 Feb should never match")
	}
	// Strictly after.
	got, _ = next("*/15 * * * *", mustTime(t, "2026-01-01T10:15:00Z"), nil)
	if !got.Equal(mustTime(t, "2026-01-01T10:30:00Z")) {
		t.Errorf("strict: %v", got)
	}
	// Sunday as 7 and as 0; leap day.
	got, _ = next("0 0 * * 7", after, nil)
	if !got.Equal(mustTime(t, "2026-11-15T00:00:00Z")) {
		t.Errorf("sun7: %v", got)
	}
	got, _ = next("0 0 29 2 *", after, nil)
	if !got.Equal(mustTime(t, "2028-02-29T00:00:00Z")) {
		t.Errorf("leap: %v", got)
	}
}
