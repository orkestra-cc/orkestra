package retrieval

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/internal/core/retrieval/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

type stubSource struct{}

func (stubSource) Load(context.Context, string, []string) ([]iface.RetrievalLoaded, error) {
	return nil, nil
}
func (stubSource) Enumerate(context.Context, string, string, int) (iface.RetrievalPage, error) {
	return iface.RetrievalPage{}, nil
}
func (stubSource) Visible(context.Context, string, iface.RetrievalPrincipalContext, []string) (map[string]iface.RetrievalVisibility, error) {
	return nil, nil
}

func validSpec() iface.RetrievalCorpusSpec {
	return iface.RetrievalCorpusSpec{
		Name:       "contacts",
		Owner:      "crm",
		Permission: "crm.contact.read",
		Filters: []iface.RetrievalFilterField{
			{Name: "status", Type: iface.RetrievalFieldKeyword, Description: "Contact status"},
		},
		Ingestion: iface.RetrievalIngestionProfile{
			EmbedEndpoint: "embed",
			Chunking:      iface.RetrievalChunking{Strategy: "none"},
		},
		Source: stubSource{},
	}
}

func initModule(t *testing.T) (*Module, *module.ServiceRegistry, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reg := module.NewServiceRegistry()
	m := NewModule()
	if err := m.Init(&module.Dependencies{Logger: logger, Services: reg}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return m, reg, &buf
}

func service(t *testing.T, reg *module.ServiceRegistry) *services.Service {
	t.Helper()
	if _, ok := module.GetTyped[iface.RetrievalCorpusRegistry](reg, module.ServiceRetrieval); !ok {
		t.Fatal("ServiceRetrieval is not a RetrievalCorpusRegistry")
	}
	svc, ok := module.GetTyped[*services.Service](reg, module.ServiceRetrieval)
	if !ok {
		t.Fatal("ServiceRetrieval is not a *services.Service")
	}
	return svc
}

func TestModule_Declarations(t *testing.T) {
	m := NewModule()
	if m.Name() != "retrieval" || m.Category() != module.CategoryCore {
		t.Fatalf("name/category = %q/%v", m.Name(), m.Category())
	}
	if got := m.Dependencies(); !slices.Equal(got, []string{"tenant", "authz", "llm"}) {
		t.Fatalf("dependencies = %v", got)
	}
	if got := m.ProvidedServices(); !slices.Equal(got, []module.ServiceKey{module.ServiceRetrieval}) {
		t.Fatalf("provided = %v", got)
	}
	cols := m.Collections()
	if len(cols) != 1 || cols[0].Name != "retrieval_corpus_state" || repository.CollCorpusState != "retrieval_corpus_state" {
		t.Fatalf("collections = %v", cols)
	}
	if p := module.PermissionsOf(m); len(p) != 0 {
		t.Fatalf("permissions = %v", p)
	}
	if c := module.ConfigSchemaOf(m); len(c) != 0 {
		t.Fatalf("config schema = %v", c)
	}
}

func TestModule_InitWithoutQdrantURL(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	m, reg, logs := initModule(t)
	svc := service(t, reg)
	if _, err := svc.Engine(); !errors.Is(err, iface.ErrRetrievalNotConfigured) {
		t.Fatalf("Engine() err = %v", err)
	}
	if err := m.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck = %v, want nil when not configured", err)
	}
	if !strings.Contains(logs.String(), "retrieval: QDRANT_URL is not set; search answers not configured until the retrieval overlay is enabled") {
		t.Fatalf("missing INFO log: %s", logs)
	}
}

func TestModule_InitNeverContactsTheEngine(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { n.Add(1) }))
	defer srv.Close()
	t.Setenv("QDRANT_URL", srv.URL)
	t.Setenv("QDRANT_API_KEY", "k-SECRET")
	_, reg, logs := initModule(t)
	if _, err := service(t, reg).Engine(); err != nil {
		t.Fatalf("Engine() = %v", err)
	}
	if n.Load() != 0 {
		t.Fatalf("Init made %d requests", n.Load())
	}
	if strings.Contains(logs.String(), "k-SECRET") {
		t.Fatalf("logs leak the key: %s", logs)
	}
}

func TestModule_InitMalformedURLWarns(t *testing.T) {
	t.Setenv("QDRANT_URL", "ftp://user:pw@host")
	t.Setenv("QDRANT_API_KEY", "k")
	_, reg, logs := initModule(t)
	if _, err := service(t, reg).Engine(); !errors.Is(err, iface.ErrRetrievalNotConfigured) {
		t.Fatalf("Engine() err = %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "retrieval: QDRANT_URL is not a valid http(s) URL; the vector engine is treated as not configured") {
		t.Fatalf("missing WARN: %s", out)
	}
	if strings.Contains(out, "pw@host") {
		t.Fatalf("logs leak the URL: %s", out)
	}
}

func TestModule_InitMissingKeyWarns(t *testing.T) {
	t.Setenv("QDRANT_URL", "http://qdrant.invalid:6333")
	t.Setenv("QDRANT_API_KEY", "")
	_, _, logs := initModule(t)
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "retrieval: QDRANT_API_KEY is not set") {
		t.Fatalf("missing WARN: %s", logs)
	}
}

func TestModule_InitBadPrefixFallsBack(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	t.Setenv("QDRANT_COLLECTION_PREFIX", "Bad Prefix")
	_, reg, logs := initModule(t)
	if got := service(t, reg).CollectionPrefix(); got != "orkestra" {
		t.Fatalf("prefix = %q", got)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("missing WARN: %s", logs)
	}
	t.Setenv("QDRANT_COLLECTION_PREFIX", " acme_1 ")
	_, reg, _ = initModule(t)
	if got := service(t, reg).CollectionPrefix(); got != "acme_1" {
		t.Fatalf("prefix = %q", got)
	}
}

func TestModule_HealthCheckUnavailable(t *testing.T) {
	status := http.StatusServiceUnavailable
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		if r.URL.Path == "/" {
			_, _ = w.Write([]byte(`{"title":"qdrant","version":"1.20.0"}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":{"collections":[]}}`))
	}))
	defer srv.Close()
	t.Setenv("QDRANT_URL", srv.URL)
	t.Setenv("QDRANT_API_KEY", "k-SECRET")
	m, _, logs := initModule(t)

	err := m.HealthCheck(context.Background())
	if !errors.Is(err, iface.ErrRetrievalUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if msg := err.Error(); strings.Contains(msg, srv.URL) || strings.Contains(msg, "k-SECRET") || strings.Contains(msg, "503") {
		t.Fatalf("error leaks driver detail: %q", msg)
	}
	if strings.Contains(logs.String(), "k-SECRET") {
		t.Fatalf("logs leak the key: %s", logs)
	}

	status = http.StatusOK
	if err := m.HealthCheck(context.Background()); err != nil {
		t.Fatalf("healthy HealthCheck = %v", err)
	}
}

func TestModule_StartSealsRegistration(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	m, reg, _ := initModule(t)
	svc := service(t, reg)
	if err := svc.RegisterCorpus(validSpec()); err != nil {
		t.Fatalf("RegisterCorpus before Start: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	other := validSpec()
	other.Name = "orders"
	err := svc.RegisterCorpus(other)
	if err == nil || !strings.Contains(err.Error(), "registration is closed") {
		t.Fatalf("RegisterCorpus after Start err = %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
