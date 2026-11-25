package module

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/jobs"
)

type jobsTestModule struct {
	BaseModule
	name       string
	defs       []jobs.Definition
	initErr    error
	sawCatalog bool
}

func (m *jobsTestModule) Name() string             { return m.name }
func (m *jobsTestModule) Category() ModuleCategory { return CategoryToggleable }
func (m *jobsTestModule) Init(deps *Dependencies) error {
	m.sawCatalog = deps.Services.Get(ServiceJobCatalog) != nil
	return m.initErr
}
func (m *jobsTestModule) Jobs() []jobs.Definition { return m.defs }

func jobDef(name string) jobs.Definition {
	return jobs.Definition{
		Name: name, Title: name, Schedule: jobs.Every(time.Hour),
		Run: func(context.Context, *jobs.Run) error { return nil },
	}
}

func TestRegistryCollectsJobs(t *testing.T) {
	buf := &bytes.Buffer{}
	root := slog.New(slog.NewTextHandler(buf, nil))
	reg := NewModuleRegistry(root)
	good := &jobsTestModule{name: "testmod", defs: []jobs.Definition{jobDef("sweep")}}
	failing := &jobsTestModule{name: "crm", defs: []jobs.Definition{jobDef("x")}, initErr: errors.New("boom")}
	bad := &jobsTestModule{name: "badmod", defs: []jobs.Definition{{Name: "Bad-Name"}}}
	reg.Register(good)
	reg.Register(failing)
	reg.Register(bad)

	services := NewServiceRegistry()
	if err := reg.InitAll(&Dependencies{Services: services, Logger: root}); err != nil {
		t.Fatalf("InitAll: %v", err)
	}
	if !good.sawCatalog || !failing.sawCatalog {
		t.Fatal("catalog must be registered before any Init")
	}
	cat, ok := services.Get(ServiceJobCatalog).(*jobs.Catalog)
	if !ok {
		t.Fatal("catalog not registered")
	}
	l := cat.List()
	if len(l) != 1 || l[0].Key != "testmod.sweep" {
		t.Fatalf("catalog = %+v", l)
	}
	if !strings.Contains(buf.String(), "job contribution rejected") {
		t.Fatalf("expected rejection log, got %q", buf.String())
	}
}

func TestRetryInitRegistersJobs(t *testing.T) {
	root := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := NewModuleRegistry(root)
	m := &jobsTestModule{name: "flaky", defs: []jobs.Definition{jobDef("tick")}, initErr: errors.New("boom")}
	reg.Register(m)
	services := NewServiceRegistry()
	if err := reg.InitAll(&Dependencies{Services: services, Logger: root}); err != nil {
		t.Fatal(err)
	}
	m.initErr = nil
	if err := reg.RetryInit("flaky"); err != nil {
		t.Fatal(err)
	}
	cat := services.Get(ServiceJobCatalog).(*jobs.Catalog)
	if _, ok := cat.Get("flaky.tick"); !ok {
		t.Fatal("recovered module's job not registered")
	}
}
