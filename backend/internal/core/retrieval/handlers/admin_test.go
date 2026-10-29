package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/core/retrieval"
	"github.com/orkestra/backend/internal/core/retrieval/handlers"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type fakeAdmin struct {
	corpora   *models.RetrievalCorporaReport
	tenant    *models.RetrievalTenantIndexingReport
	err       error
	dropped   string
	diagnosed string
	diagReq   models.RetrievalDiagnosticsRequest
	gotLog    models.SearchLogQuery
	calls     int
}

func (f *fakeAdmin) Corpora(context.Context) (*models.RetrievalCorporaReport, error) {
	f.calls++
	return f.corpora, f.err
}

func (f *fakeAdmin) TenantIndexing(context.Context) (*models.RetrievalTenantIndexingReport, error) {
	f.calls++
	return f.tenant, f.err
}

func (f *fakeAdmin) Diagnose(_ context.Context, corpus string, in models.RetrievalDiagnosticsRequest) (*models.RetrievalDiagnosticsReport, error) {
	f.calls++
	f.diagnosed, f.diagReq = corpus, in
	return &models.RetrievalDiagnosticsReport{Corpus: corpus}, f.err
}

func (f *fakeAdmin) DropOrphanCorpus(_ context.Context, corpus string) error {
	f.calls++
	f.dropped = corpus
	return f.err
}

func (f *fakeAdmin) SearchLog(_ context.Context, q models.SearchLogQuery) (*models.RetrievalSearchLogPage, error) {
	f.calls++
	f.gotLog = q
	return &models.RetrievalSearchLogPage{Items: []models.RetrievalSearchLogEntry{}, Limit: q.Limit, Offset: q.Offset}, f.err
}

// newAPI mounts the five real routes over h on a chi router.
func newAPI(t *testing.T, svc handlers.AdminAPI) (http.Handler, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	h := handlers.NewAdminHandler(svc, slog.New(slog.NewTextHandler(logs, nil)))
	mux := chi.NewRouter()
	cfg := huma.DefaultConfig("test", "1.0.0")
	cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
	api := humachi.New(mux, cfg)
	retrieval.RegisterCorpusReadRoutes(api, h)
	retrieval.RegisterCorpusAdminRoutes(api, h)
	retrieval.RegisterTenantReadRoutes(api, h)
	retrieval.RegisterSearchLogRoutes(api, h)
	return mux, logs
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("error body %q: %v", rec.Body.String(), err)
	}
	return e.Code
}

// jsonKeys collects every object key anywhere in a JSON document.
func jsonKeys(t *testing.T, raw []byte) map[string]bool {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("body %q: %v", raw, err)
	}
	keys := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				keys[k] = true
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(v)
	return keys
}

func TestHandlers_ReportsCarryNoContent(t *testing.T) {
	f := &fakeAdmin{
		corpora: &models.RetrievalCorporaReport{
			Inference: []models.RetrievalInferenceStatus{{Endpoint: "embed", Kind: "embed", Status: models.ComponentOK}},
			Corpora:   []models.RetrievalCorpusStatus{{Name: "crm", Owner: "crm"}},
		},
		tenant: &models.RetrievalTenantIndexingReport{Corpora: []models.RetrievalTenantCorpus{{Corpus: "crm", Owner: "crm"}}},
	}
	h, _ := newAPI(t, f)
	for _, path := range []string{"/v1/admin/retrieval/corpora", "/v1/admin/retrieval/tenant/indexing"} {
		rec := do(h, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
		}
		keys := jsonKeys(t, rec.Body.Bytes())
		if len(keys) < 3 {
			t.Fatalf("GET %s: body has too few keys to mean anything: %v", path, keys)
		}
		for _, banned := range []string{"query", "snippet", "score", "text"} {
			if keys[banned] {
				t.Errorf("GET %s carries content key %q", path, banned)
			}
		}
	}
}

func TestHandlers_DropReturns204(t *testing.T) {
	f := &fakeAdmin{}
	h, _ := newAPI(t, f)
	rec := do(h, http.MethodDelete, "/v1/admin/retrieval/corpora/crm", "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("DELETE = %d %q, want 204 with no body", rec.Code, rec.Body)
	}
	if f.dropped != "crm" {
		t.Errorf("dropped %q, want crm", f.dropped)
	}
}

func TestHandlers_DropRegisteredIs409(t *testing.T) {
	h, _ := newAPI(t, &fakeAdmin{err: services.ErrCorpusRegistered})
	rec := do(h, http.MethodDelete, "/v1/admin/retrieval/corpora/crm", "")
	if rec.Code != http.StatusConflict || errCode(t, rec) != errcode.RetrievalCorpusRegistered {
		t.Fatalf("DELETE = %d %s", rec.Code, rec.Body)
	}
}

func TestHandlers_BadCorpusNameIs422(t *testing.T) {
	f := &fakeAdmin{}
	h, _ := newAPI(t, f)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/admin/retrieval/corpora/Bad-Name/diagnostics", `{"query":"x"}`},
		{http.MethodDelete, "/v1/admin/retrieval/corpora/Bad-Name", ""},
	} {
		rec := do(h, c.method, c.path, c.body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s %s = %d, want 422", c.method, c.path, rec.Code)
		}
	}
	if f.calls != 0 {
		t.Errorf("service called %d times for a malformed corpus name", f.calls)
	}
}

func TestHandlers_DiagnosticsPassesTheRequest(t *testing.T) {
	f := &fakeAdmin{}
	h, _ := newAPI(t, f)
	rec := do(h, http.MethodPost, "/v1/admin/retrieval/corpora/crm/diagnostics", `{"query":"acme","mode":"dense","limit":5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	if f.diagnosed != "crm" || f.diagReq.Query != "acme" || f.diagReq.Mode != iface.RetrievalMode("dense") || f.diagReq.Limit != 5 {
		t.Errorf("service saw %q %+v", f.diagnosed, f.diagReq)
	}
}

func TestHandlers_SearchLogQueryParams(t *testing.T) {
	f := &fakeAdmin{}
	h, _ := newAPI(t, f)
	const uid = "3f0c1f6e-1d2a-4b7e-9a51-2c4d5e6f7a80"
	rec := do(h, http.MethodGet, "/v1/admin/retrieval/search-log?userUuid="+uid+"&since=2026-10-01T00:00:00Z&limit=20&offset=40", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	q := f.gotLog
	if q.UserUUID != uid || q.Since == nil || !q.Since.Equal(since) || q.Until != nil || q.Limit != 20 || q.Offset != 40 {
		t.Errorf("service saw %+v", q)
	}

	f.gotLog = models.SearchLogQuery{}
	if rec := do(h, http.MethodGet, "/v1/admin/retrieval/search-log", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET without params = %d %s", rec.Code, rec.Body)
	}
	if q := f.gotLog; q.Limit != 50 || q.Offset != 0 || q.Since != nil || q.Until != nil || q.UserUUID != "" {
		t.Errorf("defaults: service saw %+v", q)
	}
}

func TestHandlers_SearchLogRejectsBadParams(t *testing.T) {
	f := &fakeAdmin{}
	h, _ := newAPI(t, f)
	for _, qs := range []string{"userUuid=nope", "limit=0", "limit=201", "offset=10001", "offset=-1", "since=yesterday"} {
		if rec := do(h, http.MethodGet, "/v1/admin/retrieval/search-log?"+qs, ""); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("?%s = %d, want 422", qs, rec.Code)
		}
	}
	if f.calls != 0 {
		t.Errorf("service called %d times for invalid parameters", f.calls)
	}
}

func TestHandlers_SearchLogInvalidRequestIs422(t *testing.T) {
	h, _ := newAPI(t, &fakeAdmin{err: iface.ErrRetrievalInvalidRequest})
	rec := do(h, http.MethodGet, "/v1/admin/retrieval/search-log", "")
	if rec.Code != http.StatusUnprocessableEntity || errCode(t, rec) != errcode.RetrievalInvalidRequest {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
}

func TestHandlers_InternalErrorHidesCause(t *testing.T) {
	h, logs := newAPI(t, &fakeAdmin{err: errors.New("mongo: dial 10.0.0.3")})
	rec := do(h, http.MethodGet, "/v1/admin/retrieval/corpora", "")
	if rec.Code != http.StatusInternalServerError || errCode(t, rec) != errcode.RetrievalInternal {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.3") {
		t.Errorf("response leaks the cause: %s", rec.Body)
	}
	if !strings.Contains(logs.String(), "10.0.0.3") {
		t.Errorf("the cause is not logged: %s", logs)
	}
}

func TestHandlers_NotConfiguredIsNotLogged(t *testing.T) {
	h, logs := newAPI(t, &fakeAdmin{err: iface.ErrRetrievalNotConfigured})
	rec := do(h, http.MethodGet, "/v1/admin/retrieval/corpora", "")
	if rec.Code != http.StatusServiceUnavailable || errCode(t, rec) != errcode.RetrievalNotConfigured {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	if logs.Len() != 0 {
		t.Errorf("an expected unavailable was logged: %s", logs)
	}
}

func TestHandlers_NilServiceIs503(t *testing.T) {
	h, _ := newAPI(t, nil)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/admin/retrieval/corpora", ""},
		{http.MethodGet, "/v1/admin/retrieval/tenant/indexing", ""},
		{http.MethodGet, "/v1/admin/retrieval/search-log", ""},
		{http.MethodDelete, "/v1/admin/retrieval/corpora/crm", ""},
		{http.MethodPost, "/v1/admin/retrieval/corpora/crm/diagnostics", `{"query":"x"}`},
	} {
		rec := do(h, c.method, c.path, c.body)
		if rec.Code != http.StatusServiceUnavailable || errCode(t, rec) != errcode.RetrievalUnavailable {
			t.Errorf("%s %s = %d %s", c.method, c.path, rec.Code, rec.Body)
		}
	}
}

// services.AuditReason is what an audit event records as the reason of a
// failed action; the handler answers the same sentinels with mapError. For
// every sentinel AuditReason names, the two must agree, so the trail never
// says something the caller was not told. Add a row here with each case.
func TestAuditReasonAgreesWithMapError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{iface.ErrRetrievalNotConfigured, errcode.RetrievalNotConfigured},
		{iface.ErrRetrievalUnavailable, errcode.RetrievalUnavailable},
		{services.ErrCorpusRegistered, errcode.RetrievalCorpusRegistered},
		{services.ErrCorpusNotFound, errcode.RetrievalCorpusNotFound},
		{errors.New("anything else"), errcode.RetrievalInternal},
	}
	for _, c := range cases {
		if got := services.AuditReason(c.err); got != c.want {
			t.Errorf("AuditReason(%v) = %q, want %q", c.err, got, c.want)
		}
		f := &fakeAdmin{err: c.err}
		hh, _ := newAPI(t, f)
		rec := do(hh, http.MethodDelete, "/v1/admin/retrieval/corpora/crm", "")
		if got := errCode(t, rec); got != c.want {
			t.Errorf("handler code for %v = %q, AuditReason says %q", c.err, got, c.want)
		}
	}
}
