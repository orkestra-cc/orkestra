package services

import (
	"log/slog"
	"testing"
	"time"
)

func TestClampPersistedDuration_SaturatesAndWarns(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	cases := []struct {
		name     string
		raw      string
		fallback time.Duration
		min, max time.Duration
		want     time.Duration
	}{
		{"in range passes through", "2h", 0, time.Minute, 24 * time.Hour, 2 * time.Hour},
		{"day suffix parses", "1d", 0, time.Minute, 24 * time.Hour, 24 * time.Hour},
		{"below min saturates up", "30s", 0, time.Minute, 24 * time.Hour, time.Minute},
		{"above max saturates down", "9999h", 0, time.Minute, 24 * time.Hour, 24 * time.Hour},
		{"malformed uses fallback", "forever", 30 * time.Minute, 5 * time.Minute, 24 * time.Hour, 30 * time.Minute},
		{"zero uses fallback", "0s", 30 * time.Minute, 5 * time.Minute, 24 * time.Hour, 30 * time.Minute},
		{"negative uses fallback", "-5m", 30 * time.Minute, 5 * time.Minute, 24 * time.Hour, 30 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clampPersistedDuration(tc.raw, tc.fallback, tc.min, tc.max, "testKey", log)
			if got != tc.want {
				t.Errorf("clampPersistedDuration(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
