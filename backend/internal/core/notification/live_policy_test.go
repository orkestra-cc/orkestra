package notification

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/services"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// stubConfigRepo is a ConfigRepository holding one in-memory document, so a
// test can change module configuration the way an operator's PATCH does and
// watch the running service observe it. Only FindByName is exercised; the
// rest satisfy the interface (which is documented as one a test double may
// implement) and would signal a test reaching further than intended.
type stubConfigRepo struct {
	mu  sync.Mutex
	doc *module.ModuleConfig
}

func (r *stubConfigRepo) set(values map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// An Environments map is supplied so GetConfig does not try to migrate a
	// legacy document, which would be a write this stub does not model.
	r.doc = &module.ModuleConfig{
		ModuleName:        "notification",
		ActiveEnvironment: "production",
		Environments: map[string]module.EnvironmentConfig{
			"production": {ConfigValues: values},
		},
	}
}

func (r *stubConfigRepo) FindByName(context.Context, string) (*module.ModuleConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.doc, nil
}

func (r *stubConfigRepo) FindAll(context.Context) ([]module.ModuleConfig, error) { return nil, nil }
func (r *stubConfigRepo) Upsert(context.Context, *module.ModuleConfig) error     { return nil }
func (r *stubConfigRepo) UpdateEnabled(context.Context, string, bool) error      { return nil }
func (r *stubConfigRepo) MigrateToEnvironments(context.Context, string, map[string]string, map[string]string, int64) (bool, error) {
	return false, nil
}
func (r *stubConfigRepo) ClearNeedsRestart(context.Context, string) error { return nil }
func (r *stubConfigRepo) ClearNeedsRestartAt(context.Context, string, int64) (bool, error) {
	return false, nil
}
func (r *stubConfigRepo) RefreshMetadata(context.Context, module.Module) error { return nil }
func (r *stubConfigRepo) CompareAndSwapEnvironment(context.Context, string, string, int64, module.EnvironmentConfig, bool) (bool, error) {
	return false, nil
}
func (r *stubConfigRepo) CompareAndSwapConfig(context.Context, string, module.ConfigMutation) (bool, error) {
	return false, nil
}

// An unreadable configuration must resolve to the requirement being in force.
// livePolicy is consulted on the marketing path, so "we could not read the
// config" must never be the answer "marketing may go out without an
// unsubscribe header".
func TestLivePolicy_UnreadableConfigurationFailsClosed(t *testing.T) {
	p := NewModule().livePolicy(context.Background()) // no config service wired at all
	if p.Waived {
		t.Fatal("a config the module cannot read must leave the requirement in force")
	}
	if p.PublicAPIBaseURL != "" {
		t.Fatalf("nothing to build a header on, got %q", p.PublicAPIBaseURL)
	}
}

// The same schema fallback the validator applies: a value the deployment
// supplies through the environment is a value, so validation and dispatch
// agree with each other and with what the operator configured.
func TestLivePolicy_ReadsTheDeploymentEnvironment(t *testing.T) {
	t.Setenv("NOTIFICATION_PUBLIC_API_BASE_URL", "https://api.example")
	t.Setenv("NOTIFICATION_REQUIRE_ONE_CLICK_UNSUBSCRIBE", "false")

	p := NewModule().livePolicy(context.Background())
	if !p.Waived {
		t.Fatal("an operator who set the env var to false has waived the requirement")
	}
	if p.PublicAPIBaseURL != "https://api.example" {
		t.Fatalf("base URL = %q, want the env var's value", p.PublicAPIBaseURL)
	}
}

// End to end through Init: change the stored configuration and the ALREADY
// RUNNING service must answer differently on the very next question, with no
// restart. Without this the operator switches the requirement on, the config
// service records no restart-required flag (HotReloadConfig() == true), and
// marketing keeps going out unsubscribable with nothing saying so.
func TestInit_TheOneClickRequirementHotReloads(t *testing.T) {
	repo := &stubConfigRepo{}
	base := map[string]string{
		module.RosterKey(services.SendersField):                                "mkt",
		module.ItemKey(services.SendersField, "mkt", services.SubProvider):     "noop",
		module.ItemKey(services.SendersField, "mkt", services.SubAllowedTypes): "marketing",
	}
	withRequirement := func(require string) map[string]string {
		out := map[string]string{requireOneClickKey: require}
		for k, v := range base {
			out[k] = v
		}
		return out
	}

	// Start waived: nothing is configured to build a header on, but the
	// operator has accepted that, so the profile is ready for marketing.
	repo.set(withRequirement("false"))

	// Same cheap handle the other Init tests use: mongo.Connect never dials
	// for it, and nothing below runs a query — ListEligibleSenders reads the
	// roster from the config service, not from the database.
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI("mongodb://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })

	m := NewModule()
	if err := m.Init(&module.Dependencies{
		DB:            client.Database("notification_live_policy_test"),
		Services:      module.NewServiceRegistry(),
		Platform:      fakePlatform{},
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		ConfigService: module.NewModuleConfigService(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil))),
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	ready := func() bool {
		t.Helper()
		got, err := m.svc.ListEligibleSenders(context.Background(), models.TypeMarketing)
		if err != nil {
			t.Fatalf("ListEligibleSenders: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("want the marketing profile listed, got %+v", got)
		}
		return got[0].Ready
	}

	if !ready() {
		t.Fatal("with the requirement waived the profile must be ready for marketing")
	}

	// The operator switches the requirement ON. This is the direction that
	// fails open when the policy is captured at Init.
	repo.set(withRequirement("true"))
	if ready() {
		t.Fatal("switching the requirement on must take effect immediately, without a restart")
	}

	// And the other direction: setting the base URL the refusal asked for
	// must unblock it, again with no restart.
	values := withRequirement("true")
	values[services.PublicAPIBaseURLField] = "https://api.example"
	repo.set(values)
	if !ready() {
		t.Fatal("setting the public API base URL must take effect immediately, without a restart")
	}
}
