package models

import (
	"errors"
	"log/slog"
	"testing"
)

func TestParse_AcceptedForms(t *testing.T) {
	cases := []struct {
		in   string
		want LogLevel
	}{
		{"debug", LogLevelDebug},
		{"DEBUG", LogLevelDebug},
		{"  Debug  ", LogLevelDebug},
		{"info", LogLevelInfo},
		{"INFO", LogLevelInfo},
		{"warn", LogLevelWarn},
		{"warning", LogLevelWarn}, // synonym for parity with slog/env parser
		{"WARNING", LogLevelWarn},
		{"error", LogLevelError},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q) error = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Parse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParse_RejectsInvalid(t *testing.T) {
	bad := []string{"", "trace", "fatal", "verbose", "panic", "ALL", "off", "0", "5"}
	for _, in := range bad {
		_, err := Parse(in)
		if err == nil {
			t.Errorf("Parse(%q) accepted an invalid level", in)
			continue
		}
		if !errors.Is(err, ErrInvalidLogLevel) {
			t.Errorf("Parse(%q) returned %v, want ErrInvalidLogLevel", in, err)
		}
	}
}

func TestSlog_RoundTrip(t *testing.T) {
	cases := []struct {
		l    LogLevel
		want slog.Level
	}{
		{LogLevelDebug, slog.LevelDebug},
		{LogLevelInfo, slog.LevelInfo},
		{LogLevelWarn, slog.LevelWarn},
		{LogLevelError, slog.LevelError},
	}
	for _, tc := range cases {
		if got := tc.l.Slog(); got != tc.want {
			t.Errorf("LogLevel(%q).Slog() = %v, want %v", tc.l, got, tc.want)
		}
	}
}

func TestSlog_PanicsOnUnknownValue(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on unrecognised LogLevel value")
		}
	}()
	_ = LogLevel("trace").Slog()
}

func TestAllLevels_OrderedBySeverity(t *testing.T) {
	got := AllLevels()
	want := []LogLevel{LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError}
	if len(got) != len(want) {
		t.Fatalf("AllLevels() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllLevels()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Verify the order matches slog's severity order, which is what the
	// frontend dropdown relies on.
	for i := 1; i < len(got); i++ {
		if got[i].Slog() <= got[i-1].Slog() {
			t.Errorf("AllLevels not strictly increasing in severity at %d: %v <= %v", i, got[i], got[i-1])
		}
	}
}
