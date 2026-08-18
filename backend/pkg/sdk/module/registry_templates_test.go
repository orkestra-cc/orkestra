package module

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

// newTestRegistry builds a *ModuleRegistry with r.deps already populated,
// mirroring the construction used across registry_depsfor_test.go and
// config_groups_test.go. Tests that need to exercise registry internals
// directly (bypassing InitAll) should use this instead of duplicating the
// setup.
func newTestRegistry(t *testing.T) *ModuleRegistry {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := NewModuleRegistry(logger)
	r.deps = &Dependencies{
		Services: NewServiceRegistry(),
		Logger:   logger,
	}
	return r
}

type recordingSeeder struct{ got []NotificationTemplateSpec }

func (r *recordingSeeder) SeedModuleTemplates(_ context.Context, specs []NotificationTemplateSpec) error {
	r.got = append(r.got, specs...)
	return nil
}

// Every initialized module's declarations reach the seeder in one call,
// and a deployment without a notification module must not panic — the
// registry degrades to a warning, as it does for the authz catalog.
func TestRegisterNotificationTemplates(t *testing.T) {
	seeder := &recordingSeeder{}
	r := newTestRegistry(t)
	r.deps.Services.Register(ServiceNotificationTemplateSeeder, NotificationTemplateSeeder(seeder))
	r.initialized = []Module{tmplModule{}, plainModule{}}

	r.registerNotificationTemplates()

	if len(seeder.got) != 1 || seeder.got[0].TemplateID != "tmpl.hello" {
		t.Fatalf("seeder received %+v, want the one tmpl.hello spec", seeder.got)
	}
}

func TestRegisterNotificationTemplatesWithoutSeeder(t *testing.T) {
	r := newTestRegistry(t)
	r.initialized = []Module{tmplModule{}}
	r.registerNotificationTemplates() // must not panic
}
