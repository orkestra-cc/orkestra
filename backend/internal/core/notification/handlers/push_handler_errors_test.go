package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// failingDevices fails every store call; the Upsert error quotes the token
// the way a Mongo duplicate-key message quotes the key.
type failingDevices struct{ fakeDevices }

func (f *failingDevices) Upsert(_ context.Context, d *models.DeviceToken, _ time.Time) (*models.DeviceToken, error) {
	return nil, errors.New(`E11000 duplicate key error dup key: { token: "` + d.Token + `" }`)
}

func (f *failingDevices) Disable(context.Context, string, string, string, string, time.Time) (bool, error) {
	return false, errors.New("disable: connection reset")
}

func (f *failingDevices) ListForUser(context.Context, string, string) ([]models.DeviceToken, error) {
	return nil, errors.New("list: server selection timeout")
}

// T5-1: a store failure answers a generic 500 and logs its cause, without
// the push token.
func TestPushRoutes_StoreErrorsAreLoggedNotLeaked(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	h := pushAPI(t, NewPushHandler(&failingDevices{}, nil, logger), iface.PushTierOperator, "", false)

	cases := []struct {
		method, path, body, cause, generic string
	}{
		{http.MethodPut, "/v1/notifications/push/devices/dev-1", regBody, "E11000 duplicate key", "device registration failed"},
		{http.MethodDelete, "/v1/notifications/push/devices/dev-1", "", "disable: connection reset", "device removal failed"},
		{http.MethodGet, "/v1/notifications/push/devices", "", "list: server selection timeout", "device list failed"},
	}
	for _, c := range cases {
		buf.Reset()
		rec := do(t, h, c.method, c.path, c.body, authed)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s %s: status %d", c.method, c.path, rec.Code)
		}
		if body := rec.Body.String(); !strings.Contains(body, c.generic) || strings.Contains(body, c.cause) {
			t.Fatalf("%s %s: client body must stay generic: %s", c.method, c.path, body)
		}
		out := buf.String()
		if !strings.Contains(out, "push device store failed") || !strings.Contains(out, c.cause) || !strings.Contains(out, "tier=operator") {
			t.Fatalf("%s %s: cause not logged:\n%s", c.method, c.path, out)
		}
		if strings.Contains(out, "tok-AAAAAAAAAAAA") {
			t.Fatalf("%s %s: token leaked to the log:\n%s", c.method, c.path, out)
		}
	}
}
