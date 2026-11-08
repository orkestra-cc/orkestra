package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// fakeDevices is an in-memory DeviceTokenRepository. Only the methods the
// handler uses are real; the rest panic through the embedded nil interface.
type fakeDevices struct {
	repository.DeviceTokenRepository
	rows []models.DeviceToken
}

func (f *fakeDevices) Upsert(_ context.Context, d *models.DeviceToken, now time.Time) (*models.DeviceToken, error) {
	for i := range f.rows {
		r := &f.rows[i]
		if r.Tier == d.Tier && r.UserUUID == d.UserUUID && r.DeviceID == d.DeviceID {
			r.SessionID, r.Token, r.Platform, r.AppVersion, r.Active, r.LastSeenAt = d.SessionID, d.Token, d.Platform, d.AppVersion, true, now
			c := *r
			return &c, nil
		}
	}
	c := *d
	c.Active, c.CreatedAt, c.LastSeenAt = true, now, now
	f.rows = append(f.rows, c)
	return &c, nil
}

func (f *fakeDevices) Disable(_ context.Context, tier, user, device, reason string, now time.Time) (bool, error) {
	for i := range f.rows {
		r := &f.rows[i]
		if r.Tier == tier && r.UserUUID == user && r.DeviceID == device && r.Active {
			r.Active, r.DisabledReason = false, reason
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeDevices) ListForUser(_ context.Context, tier, user string) ([]models.DeviceToken, error) {
	var out []models.DeviceToken
	for _, r := range f.rows {
		if r.Tier == tier && r.UserUUID == user {
			out = append(out, r)
		}
	}
	return out, nil
}

type fakeSender struct {
	got iface.PushRequest
	res *iface.PushResult
	err error
}

func (f *fakeSender) SendPush(_ context.Context, req iface.PushRequest) (*iface.PushResult, error) {
	f.got = req
	return f.res, f.err
}

// pushAPI mounts the handler the way module.go does, behind a middleware
// that stands in for auth by stamping the context from test headers.
func pushAPI(t *testing.T, h *PushHandler, tier, opPrefix string, admin bool) http.Handler {
	t.Helper()
	mux := chi.NewMux()
	mux.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if u := r.Header.Get("X-User"); u != "" {
				ctx = context.WithValue(ctx, ctxauth.KeyUserUUID, u)
			}
			if s := r.Header.Get("X-Sid"); s != "" {
				ctx = context.WithValue(ctx, ctxauth.KeySessionID, s)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	api := humachi.New(mux, huma.DefaultConfig("push-test", "1.0.0"))
	if admin {
		h.RegisterAdminTestRoute(api)
	} else {
		h.RegisterDeviceRoutes(api, tier, opPrefix)
	}
	return mux
}

func do(t *testing.T, h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var authed = map[string]string{"X-User": "u1", "X-Sid": "sid-1"}

const regBody = `{"token":"tok-AAAAAAAAAAAA","platform":"android","appVersion":"1.2.3"}`

func TestPushRegister_BindsSessionAndNeverReturnsToken(t *testing.T) {
	devs := &fakeDevices{}
	h := pushAPI(t, NewPushHandler(devs, nil), iface.PushTierOperator, "", false)
	rec := do(t, h, http.MethodPut, "/v1/notifications/push/devices/dev-1", regBody, authed)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "tok-AAAA") || strings.Contains(strings.ToLower(rec.Body.String()), "token") {
		t.Fatalf("response leaks the token: %s", rec.Body)
	}
	if len(devs.rows) != 1 || devs.rows[0].SessionID != "sid-1" || devs.rows[0].UserUUID != "u1" ||
		devs.rows[0].Tier != "operator" || devs.rows[0].Token != "tok-AAAAAAAAAAAA" {
		t.Fatalf("stored row: %+v", devs.rows)
	}
}

func TestPushRegister_MissingSidIs401WithCode(t *testing.T) {
	devs := &fakeDevices{}
	h := pushAPI(t, NewPushHandler(devs, nil), iface.PushTierOperator, "", false)
	rec := do(t, h, http.MethodPut, "/v1/notifications/push/devices/dev-1", regBody, map[string]string{"X-User": "u1"})
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "notification.push_session_missing") {
		t.Fatalf("want 401 push_session_missing, got %d %s", rec.Code, rec.Body)
	}
	if len(devs.rows) != 0 {
		t.Fatal("nothing must be stored")
	}
}

func TestPushRegister_InputValidation(t *testing.T) {
	h := pushAPI(t, NewPushHandler(&fakeDevices{}, nil), iface.PushTierOperator, "", false)
	cases := map[string]struct{ path, body string }{
		"empty token":      {"/v1/notifications/push/devices/d", `{"token":"","platform":"ios"}`},
		"token too long":   {"/v1/notifications/push/devices/d", `{"token":"` + strings.Repeat("a", 4097) + `","platform":"ios"}`},
		"bad platform":     {"/v1/notifications/push/devices/d", `{"token":"t","platform":"windows"}`},
		"appVersion long":  {"/v1/notifications/push/devices/d", `{"token":"t","platform":"ios","appVersion":"` + strings.Repeat("1", 65) + `"}`},
		"deviceId charset": {"/v1/notifications/push/devices/a%20b", `{"token":"t","platform":"ios"}`},
		"deviceId long":    {"/v1/notifications/push/devices/" + strings.Repeat("d", 129), `{"token":"t","platform":"ios"}`},
	}
	for name, c := range cases {
		if rec := do(t, h, http.MethodPut, c.path, c.body, authed); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: want 422, got %d %s", name, rec.Code, rec.Body)
		}
	}
	for _, ok := range []string{"d", strings.Repeat("d", 128), "A.b_c:d-1"} {
		if rec := do(t, h, http.MethodPut, "/v1/notifications/push/devices/"+ok, `{"token":"t","platform":"web"}`, authed); rec.Code != http.StatusOK {
			t.Errorf("deviceId %q: want 200, got %d %s", ok, rec.Code, rec.Body)
		}
	}
}

func TestPushList_NeverContainsAToken(t *testing.T) {
	devs := &fakeDevices{}
	h := pushAPI(t, NewPushHandler(devs, nil), iface.PushTierOperator, "", false)
	do(t, h, http.MethodPut, "/v1/notifications/push/devices/dev-1", regBody, authed)
	rec := do(t, h, http.MethodGet, "/v1/notifications/push/devices", "", authed)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if strings.Contains(strings.ToLower(body), "token") || strings.Contains(body, "tok-AAAA") {
		t.Fatalf("list leaks a token: %s", body)
	}
	var out struct {
		Devices []map[string]any `json:"devices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Devices) != 1 {
		t.Fatalf("body: %s (%v)", body, err)
	}
	d := out.Devices[0]
	if d["deviceId"] != "dev-1" || d["platform"] != "android" || d["active"] != true || d["appVersion"] != "1.2.3" {
		t.Fatalf("device view: %v", d)
	}
}

func TestPushRemove_DisablesWithLogoutAndUnknownIs404(t *testing.T) {
	devs := &fakeDevices{}
	h := pushAPI(t, NewPushHandler(devs, nil), iface.PushTierOperator, "", false)
	do(t, h, http.MethodPut, "/v1/notifications/push/devices/dev-1", regBody, authed)
	if rec := do(t, h, http.MethodDelete, "/v1/notifications/push/devices/dev-1", "", authed); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if devs.rows[0].Active || devs.rows[0].DisabledReason != models.DeviceDisabledLogout {
		t.Fatalf("row: %+v", devs.rows[0])
	}
	rec := do(t, h, http.MethodDelete, "/v1/notifications/push/devices/nope", "", authed)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "notification.push_device_not_found") {
		t.Fatalf("want 404 push_device_not_found, got %d %s", rec.Code, rec.Body)
	}
}

func TestPushRoutes_UnauthenticatedIs401(t *testing.T) {
	h := pushAPI(t, NewPushHandler(&fakeDevices{}, nil), iface.PushTierOperator, "", false)
	if rec := do(t, h, http.MethodGet, "/v1/notifications/push/devices", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

// The tier comes from the mount: a client mount stores tier "client", sees
// only client rows, and a client body cannot choose another tier.
func TestPushRoutes_ClientTierMount(t *testing.T) {
	devs := &fakeDevices{}
	ph := NewPushHandler(devs, nil)
	op := pushAPI(t, ph, iface.PushTierOperator, "", false)
	cl := pushAPI(t, ph, iface.PushTierClient, "client-", false)
	if rec := do(t, cl, http.MethodPut, "/v1/notifications/push/devices/dev-1", `{"token":"t-c","platform":"ios","tier":"operator"}`, authed); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a tier in the body must be refused: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, cl, http.MethodPut, "/v1/notifications/push/devices/dev-1", `{"token":"t-c","platform":"ios"}`, authed); rec.Code != http.StatusOK {
		t.Fatalf("client put: %d %s", rec.Code, rec.Body)
	}
	if devs.rows[0].Tier != iface.PushTierClient {
		t.Fatalf("tier = %q, want client", devs.rows[0].Tier)
	}
	var out struct {
		Devices []any `json:"devices"`
	}
	rec := do(t, op, http.MethodGet, "/v1/notifications/push/devices", "", authed)
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Devices) != 0 {
		t.Fatalf("operator mount must not see client devices: %s", rec.Body)
	}
	rec = do(t, cl, http.MethodGet, "/v1/notifications/push/devices", "", authed)
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Devices) != 1 {
		t.Fatalf("client mount lists its device: %s", rec.Body)
	}
	if rec := do(t, op, http.MethodDelete, "/v1/notifications/push/devices/dev-1", "", authed); rec.Code != http.StatusNotFound {
		t.Fatalf("operator mount cannot remove a client device: %d", rec.Code)
	}
}

func TestPushTest_DefaultsAndResult(t *testing.T) {
	s := &fakeSender{res: &iface.PushResult{Users: []iface.PushUserResult{{UserUUID: "u9", Status: iface.PushStatusDelivered, Attempted: 1, Delivered: 1}}}}
	h := pushAPI(t, NewPushHandler(nil, s), "", "", true)
	rec := do(t, h, http.MethodPost, "/v1/notifications/push/test", `{"tier":"client","userUuid":"u9"}`, authed)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	g := s.got
	if g.Tier != "client" || len(g.UserUUIDs) != 1 || g.UserUUIDs[0] != "u9" || g.Title != "Orkestra test" ||
		g.Body != "Test push notification" || g.Type != iface.PushTypeTransactional || g.Category != "notification.test" {
		t.Fatalf("request: %+v", g)
	}
	if !strings.Contains(rec.Body.String(), `"delivered":1`) || !strings.Contains(rec.Body.String(), `"status":"delivered"`) {
		t.Fatalf("result: %s", rec.Body)
	}
	do(t, h, http.MethodPost, "/v1/notifications/push/test", `{"tier":"operator","userUuid":"u9","title":"T","body":"B"}`, authed)
	if s.got.Title != "T" || s.got.Body != "B" {
		t.Fatalf("overrides lost: %+v", s.got)
	}
}

func TestPushTest_ErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{errors.Join(iface.ErrPushInvalidRequest, errors.New("tier")), http.StatusUnprocessableEntity, "notification.push_invalid_request"},
		{iface.ErrPushUnavailable, http.StatusServiceUnavailable, "notification.push_unavailable"},
	}
	for _, c := range cases {
		h := pushAPI(t, NewPushHandler(nil, &fakeSender{err: c.err}), "", "", true)
		rec := do(t, h, http.MethodPost, "/v1/notifications/push/test", `{"tier":"x","userUuid":"u"}`, authed)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.code) {
			t.Errorf("%v: want %d %s, got %d %s", c.err, c.status, c.code, rec.Code, rec.Body)
		}
	}
}
