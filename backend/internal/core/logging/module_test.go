package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	sdkmodule "github.com/orkestra/backend/pkg/sdk/module"
)

func TestLoggingModule_NavItems(t *testing.T) {
	if items := (&LoggingModule{}).NavItems(); len(items) != 0 {
		t.Fatalf("NavItems() = %+v, want no standalone Log levels navigation item", items)
	}
}

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
		{method: http.MethodPost, path: "/v1/admin/observability/log-levels/logs"},
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
			case http.MethodPost:
				operation = path.Post
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

func TestRegisterRoutes_LogPreviewDocumentsConstrainedFilters(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("test", "1.0.0"))
	RegisterRoutes(api, (*handlers.LogLevelHandler)(nil))
	operation := api.OpenAPI().Paths["/v1/admin/observability/log-levels/logs"].Post
	schema := requireJSONBodySchema(t, operation)

	t.Run("module is required string", func(t *testing.T) {
		property := schema.Properties["module"]
		if property == nil || property.Type != huma.TypeString || !containsString(schema.Required, "module") {
			t.Errorf("module schema = %+v required=%v, want required string", property, schema.Required)
		}
	})
	t.Run("window is required integer enum", func(t *testing.T) {
		property := schema.Properties["windowMinutes"]
		if property == nil || property.Type != huma.TypeInteger || !containsString(schema.Required, "windowMinutes") || !reflect.DeepEqual(property.Enum, []any{5, 15, 60}) {
			t.Errorf("windowMinutes schema = %+v required=%v, want required integer enum [5 15 60]", property, schema.Required)
		}
	})
	t.Run("level is closed enum", func(t *testing.T) {
		property := schema.Properties["level"]
		want := []any{"debug", "info", "warn", "error"}
		if property == nil || property.Type != huma.TypeString || !reflect.DeepEqual(property.Enum, want) {
			t.Errorf("level schema = %+v, want string enum %v", property, want)
		}
	})
	t.Run("search is bounded", func(t *testing.T) {
		property := schema.Properties["q"]
		if property == nil || property.Type != huma.TypeString || property.MaxLength == nil || *property.MaxLength != 200 {
			t.Errorf("q schema = %+v, want string maxLength 200", property)
		}
	})
	t.Run("limit is bounded integer", func(t *testing.T) {
		property := schema.Properties["limit"]
		if property == nil || property.Type != huma.TypeInteger || property.Minimum == nil || *property.Minimum != 1 || property.Maximum == nil || *property.Maximum != 100 || property.Default != 50 {
			t.Errorf("limit schema = %+v, want integer default 50 range 1..100", property)
		}
	})
}

func requireJSONBodySchema(t *testing.T, operation *huma.Operation) *huma.Schema {
	t.Helper()
	if operation == nil || operation.RequestBody == nil {
		t.Fatal("operation request body is missing")
	}
	media := operation.RequestBody.Content["application/json"]
	if media == nil || media.Schema == nil {
		t.Fatal("application/json request schema is missing")
	}
	return media.Schema
}

func TestRegisterRoutes_LogPreviewMalformedNumericQueriesUseStableBadRequest(t *testing.T) {
	provider := &numericQueryProvider{}
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("test", "1.0.0"))
	RegisterRoutes(api, handlers.NewLogLevelHandler(nil, provider, "", slog.Default()))

	tests := []struct {
		name   string
		window string
		limit  string
	}{
		{name: "malformed window", window: `"not-a-number"`, limit: `20`},
		{name: "overflowing window", window: `999999999999999999999999999999`, limit: `20`},
		{name: "malformed limit", window: `15`, limit: `"not-a-number"`},
		{name: "overflowing limit", window: `15`, limit: `999999999999999999999999999999`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			body := `{"module":"auth","windowMinutes":` + tt.window + `,"limit":` + tt.limit + `}`
			request := httptest.NewRequest(http.MethodPost, "/v1/admin/observability/log-levels/logs", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")

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

func TestRegisterRoutes_DocumentsPermanentAndDiagnosticConstraints(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("test", "1.0.0"))
	RegisterRoutes(api, (*handlers.LogLevelHandler)(nil))
	levels := []any{"debug", "info", "warn", "error"}

	apply := requireJSONBodySchema(t, api.OpenAPI().Paths["/v1/admin/observability/log-levels"].Put)
	if !reflect.DeepEqual(apply.Properties["global"].Enum, levels) {
		t.Errorf("apply global enum = %v, want %v", apply.Properties["global"].Enum, levels)
	}
	if apply.Properties["expectedPermanentRevision"].Minimum == nil || *apply.Properties["expectedPermanentRevision"].Minimum != 0 {
		t.Errorf("expectedPermanentRevision schema = %+v, want minimum 0", apply.Properties["expectedPermanentRevision"])
	}
	additional, ok := apply.Properties["perModule"].AdditionalProperties.(*huma.Schema)
	if !ok || !reflect.DeepEqual(additional.Enum, levels) {
		t.Errorf("perModule additionalProperties = %+v, want log-level enum", apply.Properties["perModule"].AdditionalProperties)
	}

	diagnostic := requireJSONBodySchema(t, api.OpenAPI().Paths["/v1/admin/observability/log-levels/{module}/diagnostic"].Put)
	if !reflect.DeepEqual(diagnostic.Properties["level"].Enum, levels) {
		t.Errorf("diagnostic level enum = %v, want %v", diagnostic.Properties["level"].Enum, levels)
	}
	if !reflect.DeepEqual(diagnostic.Properties["durationMinutes"].Enum, []any{15, 60, 240}) {
		t.Errorf("diagnostic duration enum = %v, want [15 60 240]", diagnostic.Properties["durationMinutes"].Enum)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func TestLoggingModule_RegisterRoutesEnforcesRuntimeAuthorization(t *testing.T) {
	repo := newLifecycleRepo()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	svc := services.NewLogLevelService(repo, logger, slog.LevelInfo, nil, []string{"auth"})

	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := r.Header.Get("X-Test-Role")
			if role != "" {
				ctx := context.WithValue(r.Context(), ctxauth.KeyUserUUID, "operator-1")
				ctx = context.WithValue(ctx, ctxauth.KeySystemRole, role)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	})

	m := NewModule()
	m.svc = svc
	m.handler = handlers.NewLogLevelHandler(svc, logquery.New("", nil), "", logger)
	m.RegisterRoutes(&sdkmodule.RouteInfo{
		Operator: &sdkmodule.APISurface{
			Audience:        sdkmodule.AudienceOperator,
			ProtectedRouter: router,
			AuthMW:          &middleware.JWTValidator{},
		},
		APIConfig: huma.DefaultConfig("test", "1.0.0"),
	})

	tests := []struct {
		name       string
		role       string
		wantStatus int
	}{
		{name: "unauthenticated", wantStatus: http.StatusUnauthorized},
		{name: "authenticated without system permission", role: "operator", wantStatus: http.StatusForbidden},
		{name: "administrator", role: "administrator", wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v1/admin/observability/log-levels", nil)
			if tt.role != "" {
				request.Header.Set("X-Test-Role", tt.role)
			}
			router.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", recorder.Code, tt.wantStatus, recorder.Body.String())
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
	repo.setError(errors.New("persistence down"))

	m := NewModule()
	m.svc = svc
	m.logger = logger
	m.cleanupInterval = 5 * time.Millisecond
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
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

func TestLoggingModule_LifecycleRefreshesCrossReplicaStateWithinBound(t *testing.T) {
	repo := newLifecycleRepo()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	writer := services.NewLogLevelService(repo, logger, slog.LevelInfo, nil, []string{"auth"})
	reader := services.NewLogLevelService(repo, logger, slog.LevelInfo, nil, []string{"auth"})

	m := NewModule()
	m.svc = reader
	m.logger = logger
	m.cleanupInterval = 5 * time.Millisecond
	m.refreshInterval = 5 * time.Millisecond
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer func() {
		if err := m.Stop(stopCtx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	if err := writer.SetGlobal(context.Background(), models.LogLevelError, "operator"); err != nil {
		t.Fatalf("writer SetGlobal: %v", err)
	}
	deadline := time.Now().Add(250 * time.Millisecond)
	for reader.Global() != slog.LevelError && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := reader.Global(); got != slog.LevelError {
		t.Errorf("reader global after refresh bound = %v, want error", got)
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

func (r *lifecycleRepo) CompareAndSwap(_ context.Context, expectedRevision int64, doc *models.LogLevelDoc) (bool, error) {
	r.attempts <- struct{}{}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return false, r.err
	}
	currentRevision := int64(0)
	if r.doc != nil {
		currentRevision = r.doc.Revision
	}
	if currentRevision != expectedRevision {
		return false, nil
	}
	r.doc = cloneLifecycleDoc(doc)
	r.upserts <- cloneLifecycleDoc(doc)
	return true, nil
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
