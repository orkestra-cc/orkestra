package utils

import (
	"testing"
	"time"
)

func TestParseDuration_DaySuffix(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
		ok   bool
	}{
		{"15m", 15 * time.Minute, true},
		{"1h", time.Hour, true},
		{"30d", 30 * 24 * time.Hour, true},
		{"720h", 720 * time.Hour, true},
		{"0.5d", 12 * time.Hour, true},
		{"1d12h", 0, false}, // compound day forms stay unsupported, not half-supported
		{"forever", 0, false},
		{"", 0, false},
		{"   ", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, ok := ParseDuration(tc.raw)
			if ok != tc.ok {
				t.Fatalf("ParseDuration(%q) ok = %v, want %v", tc.raw, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
