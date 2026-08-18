package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
)

func TestRefreshReplayLogContainsOnlyAllowlistedOutcomeFields(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	repo := newGateRefreshRepo()
	repo.revokeFamilyErr = errors.New("sensitive mongo address")
	svc := &authService{refreshTokenRepo: repo}
	svc.handleRefreshReplay(context.Background(), &models.RefreshTokenDoc{
		UserUUID: "sensitive-user", SessionUUID: "sensitive-session",
		DeviceID: "sensitive-device", FamilyID: "sensitive-family",
	}, &models.SecurityContext{IPAddress: "192.0.2.44"}, "rotated_row")

	output := logs.String()
	for _, forbidden := range []string{"sensitive-user", "sensitive-session", "sensitive-device", "sensitive-family", "192.0.2.44", "sensitive mongo address"} {
		if strings.Contains(output, forbidden) {
			t.Errorf("refresh replay log leaked %q: %s", forbidden, output)
		}
	}
	for _, required := range []string{`"msg":"refresh_token_replay"`, `"kind":"rotated_row"`, `"outcome":"store_error"`} {
		if !strings.Contains(output, required) {
			t.Errorf("refresh replay log missing %s: %s", required, output)
		}
	}
}
