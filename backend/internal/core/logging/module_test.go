package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/logging/handlers"
	"github.com/orkestra/backend/internal/core/logging/logquery"
	"github.com/orkestra/backend/internal/core/logging/models"
	"github.com/orkestra/backend/internal/core/logging/repository"
	"github.com/orkestra/backend/internal/core/logging/services"
	"github.com/orkestra/backend/internal/shared/errcode"
)

func TestRegisterRoutes_ExposesBatchDiagnosticAndLogPreviewOperations(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("test", "1.0.0"))
	RegisterRoutes(api, (*handlers.LogLevelHandler)(nil))

	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodPut, path: "/v1/admin/observability/log-levels"},
		{method: http.MethodPut, path: "/v1/admin/observability/log-levels/{module}/diagnostic"},
		{method: http.MethodDelete, path: "/v1/admin/observability/log-levels/{module}/diagnostic"},
		{method: http.MethodGet, path: "/v1/admin/observability/log-levels/logs"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			path, ok := api.OpenAPI().Paths[tt.path]
			if !ok {
				t.Fatalf("path %q is not registered", tt.path)
			}
			var operation *huma.Operation
			switch tt.method {
			case http.MethodPut:
				operation = path.Put
			case http.MethodDelete:
				operation = path.Delete
			case http.MethodGet:
				operation = path.Get
			}
			if operation == nil {
				t.Fatalf("%s %s is not registered", tt.method, tt.path)
			}
			if len(operation.Security) != 1 || len(operation.Security[0]["bearerAuth"]) != 1 || operation.Security[0]["bearerAuth"][0] != "administrator" {
				t.Errorf("security = %+v, want bearerAuth administrator", operation.Security)
			}
		})
	}
}

func TestRegisterRoutes_LogPreviewMalformedNumericQueriesUseStableBadRequest(t *testing.T) {
	provider := &numericQueryProvider{}
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("test", "1.0.0"))
	RegisterRoutes(api, handlers.NewLogLevelHandler(nil, provider, ""))

	tests := []struct {
		name  string
		query string
	}{
		{name: "malformed window", query: "windowMinutes=not-a-number&limit=20"},
		{name: "overflowing window", query: "windowMinutes=999999999999999999999999999999&limit=20"},
		{name: "malformed limit", query: "windowMinutes=15&limit=not-a-number"},
		{name: "overflowing limit", query: "windowMinutes=15&limit=999999999999999999999999999999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v1/admin/observability/log-levels/logs?module=auth&"+tt.query, nil)

			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", recorder.Code, recorder.Body.String())
			}
			var response struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Code != errcode.LoggingLogPreviewInvalid {
				t.Errorf("code = %q, want %q; body = %s", response.Code, errcode.LoggingLogPreviewInvalid, recorder.Body.String())
			}
		})
	}
	if provider.calls != 0 {
		t.Errorf("provider calls = %d, want zero for malformed numeric queries", provider.calls)
	}
}

func TestNormalizeExternalURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty is absent", raw: "", want: ""},
		{name: "absolute HTTP base", raw: "http://localhost:3010/", want: "http://localhost:3010"},
		{name: "absolute HTTPS path", raw: "https://grafana.example.test/base/?ignored=1#fragment", want: "https://grafana.example.test/base"},
		{name: "relative URL rejected", raw: "/grafana", want: ""},
		{name: "non HTTP scheme rejected", raw: "javascript:alert(1)", want: ""},
		{name: "embedded credentials rejected", raw: "https://admin:secret@grafana.example.test", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeExternalURL(tt.raw); got != tt.want {
				t.Errorf("normalizeExternalURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestLoggingModule_CleanupLifecycle(t *testing.T) {
	repo := newLifecycleRepo()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	svc := services.NewLogLevelService(repo, logger, slog.LevelInfo, nil, []string{"auth"})
	expiredAt := time.Now().Add(-time.Minute)
	if err := svc.StartDiagnostic(context.Background(), "auth", models.LogLevelDebug, &expiredAt, "operator"); err != nil {
		t.Fatalf("seed expired diagnostic: %v", err)
	}
	repo.waitForUpsert(t)

	m := NewModule()
	m.svc = svc
	m.logger = logger
	m.cleanupInterval = 5 * time.Millisecond

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	firstDone := m.cleanupDone
	if firstDone == nil {
		t.Fatal("Start did not create a cleanup loop")
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("repeated Start: %v", err)
	}
	if m.cleanupDone != firstDone {
		t.Error("repeated Start replaced the running cleanup loop")
	}

	cleaned := repo.waitForUpsert(t)
	if _, ok := cleaned.Diagnostics["auth"]; ok {
		t.Errorf("cleanup persisted expired diagnostic: %+v", cleaned.Diagnostics["auth"])
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-firstDone:
	default:
		t.Error("Stop returned before the cleanup loop exited")
	}
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("repeated Stop: %v", err)
	}

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if m.cleanupDone == nil || m.cleanupDone == firstDone {
		t.Error("restart did not create a fresh cleanup loop")
	}
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("stop after restart: %v", err)
	}
}

func TestLoggingModule_CleanupLifecycleLogsOnlyCleanupError(t *testing.T) {
	repo := newLifecycleRepo()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	svc := services.NewLogLevelService(repo, logger, slog.LevelInfo, nil, []string{"auth"})
	expiredAt := time.Now().Add(-time.Minute)
	if err := svc.StartDiagnostic(context.Background(), "auth", models.LogLevelDebug, &expiredAt, "operator"); err != nil {
		t.Fatalf("seed expired diagnostic: %v", err)
	}
	repo.waitForUpsert(t)
	repo.waitForAttempt(t)
	repo.setError(errors.New("persistence down"))

	m := NewModule()
	m.svc = svc
	m.logger = logger
	m.cleanupInterval = 5 * time.Millisecond
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	repo.waitForAttempt(t)
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	got := logs.String()
	if !bytes.Contains([]byte(got), []byte("cleanup expired diagnostics failed")) || !bytes.Contains([]byte(got), []byte("persistence down")) {
		t.Errorf("cleanup log = %q, want cleanup context and persistence error", got)
	}
	for _, forbidden := range []string{"module=auth", "level=debug", "operator"} {
		if bytes.Contains([]byte(got), []byte(forbidden)) {
			t.Errorf("cleanup log contains diagnostic content %q: %s", forbidden, got)
		}
	}
}

type lifecycleRepo struct {
	mu       sync.Mutex
	doc      *models.LogLevelDoc
	err      error
	upserts  chan *models.LogLevelDoc
	attempts chan struct{}
}

type numericQueryProvider struct {
	calls int
}

func (*numericQueryProvider) Status(context.Context) logquery.Status {
	return logquery.Status{Available: true}
}

func (p *numericQueryProvider) Query(context.Context, logquery.Query) ([]models.LogEvent, error) {
	p.calls++
	return nil, nil
}

func newLifecycleRepo() *lifecycleRepo {
	return &lifecycleRepo{
		upserts:  make(chan *models.LogLevelDoc, 8),
		attempts: make(chan struct{}, 8),
	}
}

func (r *lifecycleRepo) Get(context.Context) (*models.LogLevelDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	if r.doc == nil {
		return nil, repository.ErrNotFound
	}
	return cloneLifecycleDoc(r.doc), nil
}

func (r *lifecycleRepo) Upsert(_ context.Context, doc *models.LogLevelDoc) error {
	r.attempts <- struct{}{}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.doc = cloneLifecycleDoc(doc)
	r.upserts <- cloneLifecycleDoc(doc)
	return nil
}

func (r *lifecycleRepo) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *lifecycleRepo) waitForUpsert(t *testing.T) *models.LogLevelDoc {
	t.Helper()
	select {
	case doc := <-r.upserts:
		return doc
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for repository upsert")
		return nil
	}
}

func (r *lifecycleRepo) waitForAttempt(t *testing.T) {
	t.Helper()
	select {
	case <-r.attempts:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for repository attempt")
	}
}

func cloneLifecycleDoc(doc *models.LogLevelDoc) *models.LogLevelDoc {
	clone := *doc
	clone.PerModule = make(map[string]models.LogLevel, len(doc.PerModule))
	for module, level := range doc.PerModule {
		clone.PerModule[module] = level
	}
	clone.Diagnostics = make(map[string]models.DiagnosticOverride, len(doc.Diagnostics))
	for module, diagnostic := range doc.Diagnostics {
		if diagnostic.ExpiresAt != nil {
			expiresAt := *diagnostic.ExpiresAt
			diagnostic.ExpiresAt = &expiresAt
		}
		clone.Diagnostics[module] = diagnostic
	}
	return &clone
}
