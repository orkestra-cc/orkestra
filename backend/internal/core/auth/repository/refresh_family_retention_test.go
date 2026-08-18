package repository

import (
	"testing"
	"time"
)

func TestRefreshFamilyFenceExpiryTracksConfiguredTokenLifetime(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	configuredExpiry := now.Add(45 * 24 * time.Hour)
	if got := refreshFamilyFenceExpiry(now, configuredExpiry); !got.Equal(configuredExpiry) {
		t.Fatalf("fence expiry = %v, want token expiry %v", got, configuredExpiry)
	}
}

func TestRefreshFamilyFenceExpiryHasSafeFallback(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	if got := refreshFamilyFenceExpiry(now, time.Time{}); got.Before(now.Add(24 * time.Hour)) {
		t.Fatalf("fallback fence expiry = %v, want at least 24h", got)
	}
}
