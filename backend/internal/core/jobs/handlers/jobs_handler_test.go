package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/core/jobs/services"
	"github.com/orkestra/backend/internal/shared/errcode"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Every service error maps to its stable code and status; an unnamed one is
// a 500 whose detail does not carry the error text.
func TestFailMapsErrorsToCodes(t *testing.T) {
	h := New(services.New(services.Options{}), quietLogger())
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{services.ErrNotFound, http.StatusNotFound, errcode.JobsNotFound},
		{fmt.Errorf("wrapped: %w", services.ErrRevisionConflict), http.StatusConflict, errcode.JobsRevisionConflict},
		{services.ErrAlreadyRunning, http.StatusConflict, errcode.JobsAlreadyRunning},
		{services.ErrAlreadyRequested, http.StatusConflict, errcode.JobsAlreadyRequested},
		{services.ErrModuleStopped, http.StatusConflict, errcode.JobsModuleStopped},
		{&services.InputError{Kind: services.ErrInvalidSchedule, Msg: "bad cron"}, http.StatusUnprocessableEntity, errcode.JobsInvalidSchedule},
		{&services.InputError{Kind: services.ErrManualScheduleLocked, Msg: "manual"}, http.StatusUnprocessableEntity, errcode.JobsManualScheduleLocked},
		{&services.InputError{Kind: services.ErrInvalidTimezone, Msg: "tz"}, http.StatusUnprocessableEntity, errcode.JobsInvalidTimezone},
		{&services.InputError{Kind: services.ErrInvalidQuery, Msg: "cursor"}, http.StatusBadRequest, errcode.JobsInvalidQuery},
		{errors.New("mongo: connection refused 10.0.0.5:27017"), http.StatusInternalServerError, errcode.JobsPersistenceFailed},
	}
	for _, tc := range cases {
		var e *errcode.Error
		if !errors.As(h.fail(context.Background(), "test", tc.err), &e) {
			t.Fatalf("%v: not an errcode.Error", tc.err)
		}
		if e.Status != tc.status || e.Code != tc.code {
			t.Errorf("%v: %d %s, want %d %s", tc.err, e.Status, e.Code, tc.status, tc.code)
		}
		if strings.Contains(e.Detail, "10.0.0.5") {
			t.Errorf("detail leaks the error text: %q", e.Detail)
		}
	}
}

func TestNotReadyHandlerAnswers500(t *testing.T) {
	h := New(nil, nil)
	_, err := h.List(context.Background(), &ListRequest{})
	var e *errcode.Error
	if !errors.As(err, &e) || e.Status != http.StatusInternalServerError || e.Code != errcode.JobsPersistenceFailed {
		t.Fatalf("err = %v", err)
	}
}

func previewRouter() *chi.Mux {
	svc := services.New(services.Options{Now: func() time.Time { return time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC) }})
	h := New(svc, quietLogger())
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("test", "1.0.0"))
	huma.Register(api, huma.Operation{OperationID: "preview", Method: http.MethodPost, Path: "/preview"}, h.Preview)
	return router
}

// The preview round-trips through Huma: the times carry the zone offset, and
// an invalid cron answers 422 jobs.invalid_schedule naming the field.
func TestPreviewOverHTTP(t *testing.T) {
	router := previewRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/preview", strings.NewReader(`{"kind":"cron","cron":"0 2 * * *","timezone":"Europe/Rome"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Next        []string `json:"next"`
		Description string   `json:"description"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Next) != 5 || body.Next[0] != "2026-11-09T02:00:00+01:00" {
		t.Errorf("next = %v", body.Next)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/preview", strings.NewReader(`{"kind":"cron","cron":"61 * * * *"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	var e errcode.Error
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if rec.Code != http.StatusUnprocessableEntity || e.Code != errcode.JobsInvalidSchedule || !strings.Contains(e.Detail, "minute") {
		t.Errorf("invalid cron: %d %s", rec.Code, rec.Body)
	}
}

// boundedRouter mounts the preview and PATCH operations with the jobs
// routes' transformer, as routes.go does.
func boundedRouter() *chi.Mux {
	svc := services.New(services.Options{Now: func() time.Time { return time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC) }})
	h := New(svc, quietLogger())
	router := chi.NewRouter()
	cfg := huma.DefaultConfig("test", "1.0.0")
	cfg.Transformers = append(cfg.Transformers, ClipEchoedValues)
	api := humachi.New(router, cfg)
	huma.Register(api, huma.Operation{OperationID: "preview", Method: http.MethodPost, Path: "/preview"}, h.Preview)
	huma.Register(api, huma.Operation{OperationID: "update", Method: http.MethodPatch, Path: "/jobs/{key}"}, h.Update)
	return router
}

// I4: cron and timezone strings are bounded on PATCH and preview; the
// rejected value is not echoed back whole, and a parser message quoting the
// input stays within 200 characters.
func TestScheduleInputsBounded(t *testing.T) {
	router := boundedRouter()
	long := "0,0" + strings.Repeat(",0", 59) + " * * * *" // 129 characters, a valid cron
	if len(long) != 129 {
		t.Fatalf("len = %d", len(long))
	}
	longTZ := strings.Repeat("A", 65)
	cases := []struct {
		name, method, path, body, echoed string
	}{
		{"preview cron", http.MethodPost, "/preview", `{"kind":"cron","cron":"` + long + `"}`, long},
		{"preview timezone", http.MethodPost, "/preview", `{"kind":"cron","cron":"0 2 * * *","timezone":"` + longTZ + `"}`, longTZ},
		{"preview everySeconds", http.MethodPost, "/preview", `{"kind":"every","everySeconds":31622401}`, ""},
		{"patch cron", http.MethodPatch, "/jobs/m.a", `{"revision":1,"schedule":{"kind":"cron","cron":"` + long + `"}}`, long},
		{"patch timezone", http.MethodPatch, "/jobs/m.a", `{"revision":1,"timezone":"` + longTZ + `"}`, longTZ},
		{"patch everySeconds", http.MethodPatch, "/jobs/m.a", `{"revision":1,"schedule":{"kind":"every","everySeconds":31622401}}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			if tc.echoed != "" && strings.Contains(rec.Body.String(), tc.echoed) {
				t.Fatalf("the rejected value is echoed whole: %s", rec.Body)
			}
		})
	}

	// 128 characters pass the cap; the parser's message, which quotes the
	// minute field, is cut to 200 characters.
	bad := "61" + strings.Repeat(",0", 59) + " * * * *"
	if len(bad) != 128 {
		t.Fatalf("len = %d", len(bad))
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/preview", strings.NewReader(`{"kind":"cron","cron":"`+bad+`"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	var e errcode.Error
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if rec.Code != http.StatusUnprocessableEntity || e.Code != errcode.JobsInvalidSchedule || !strings.Contains(e.Detail, "minute") {
		t.Fatalf("invalid 128-char cron: %d %s", rec.Code, rec.Body)
	}
	if n := len([]rune(e.Detail)); n > 200 {
		t.Fatalf("detail is %d characters: %q", n, e.Detail)
	}
}
