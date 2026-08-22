package repository

import (
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
)

// The repository defaulted to 30 days while both service call sites wrote
// 90. With a TTL index on the field that discrepancy stops being cosmetic:
// a row created through the fallback would be deleted two months early.
func TestSessionRetentionFallbackMatchesCallers(t *testing.T) {
	if models.AuthSessionRetention != 90*24*time.Hour {
		t.Fatalf("AuthSessionRetention = %v, want 90d", models.AuthSessionRetention)
	}
	if sessionRetentionFallback != models.AuthSessionRetention {
		t.Errorf("repository fallback %v disagrees with the callers' %v", sessionRetentionFallback, models.AuthSessionRetention)
	}
}
