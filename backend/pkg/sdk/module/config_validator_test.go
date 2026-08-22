package module

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// validatingModule implements the optional seam and rejects one key.
type validatingModule struct {
	BaseModule
	seen map[string]string
}

func (m *validatingModule) Name() string             { return "validating" }
func (m *validatingModule) Init(*Dependencies) error { return nil }
func (m *validatingModule) ValidateConfig(_ context.Context, values map[string]string) error {
	m.seen = values
	if values["strict"] == "bad" {
		return &ConfigValidationError{Field: "strict", Message: "must not be bad"}
	}
	return nil
}

// plainModule (declared in notification_templates_test.go) omits the seam
// entirely — reused here to prove a module without it is unaffected.

func TestConfigValidationError_MessageIncludesField(t *testing.T) {
	err := error(&ConfigValidationError{Field: "accessTokenTTL", Message: "must be between 1m0s and 24h0m0s"})
	if !strings.Contains(err.Error(), "accessTokenTTL") {
		t.Errorf("Error() = %q, want it to name the field so the operator knows which input to fix", err.Error())
	}
	var typed *ConfigValidationError
	if !errors.As(err, &typed) {
		t.Fatal("ConfigValidationError must be recoverable with errors.As so the handler can map it to 422")
	}
}

// --- Live-Mongo wiring tests ---
//
// ModuleConfigService.repo wraps a concrete *mongo.Collection, not an
// interface, so exercising UpdateConfig/UpdateEnvironmentConfig end to end
// requires a real MongoDB — there is no fake to substitute. This mirrors the
// pattern in internal/core/auth/repository/refresh_token_repository_concurrency_test.go:
// read MONGO_TEST_URI (falling back to MONGO_URI), skip when neither is set,
// connect, use a per-run database name, and drop it on cleanup. CI sets
// MONGO_URI against its mongo:8.0 service, so these genuinely run there even
// though they skip on a workstation with no test Mongo configured.

// fakeRedisClient is a minimal no-op RedisClient. These tests exercise the
// Mongo-backed config wiring, not the Redis cache — InvalidateCache just
// needs somewhere harmless to call Del.
type fakeRedisClient struct{}

func (fakeRedisClient) Get(context.Context, string) (string, error) { return "", nil }
func (fakeRedisClient) Set(context.Context, string, interface{}, time.Duration) error {
	return nil
}
func (fakeRedisClient) Del(context.Context, ...string) error           { return nil }
func (fakeRedisClient) Keys(context.Context, string) ([]string, error) { return nil, nil }
func (fakeRedisClient) Incr(context.Context, string) (int64, error)    { return 1, nil }
func (fakeRedisClient) Expire(context.Context, string, time.Duration) error {
	return nil
}

// newTestConfigService connects to a live MongoDB (MONGO_TEST_URI, falling
// back to MONGO_URI) and returns a ModuleConfigService backed by a
// throwaway, UUID-suffixed database that is dropped on test cleanup. Skips
// the test when neither env var is set — it must never fail merely because
// no test Mongo is configured; only a genuine connection error is fatal.
func newTestConfigService(t *testing.T) (*ModuleConfigService, *ModuleConfigRepository) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = os.Getenv("MONGO_URI")
	}
	if uri == "" {
		t.Skip("set MONGO_TEST_URI or MONGO_URI to run live config service tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("mongo.Ping: %v", err)
	}

	db := client.Database("sdk_config_validator_" + uuid.NewString())
	repo := NewModuleConfigRepository(db)
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})

	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	return svc, repo
}

// seedModuleDoc inserts a module_configs document carrying only the legacy
// top-level ConfigValues — no Environments map — modelling a document that
// predates (or has not yet gone through) the per-environment migration.
func seedModuleDoc(t *testing.T, repo *ModuleConfigRepository, name string, values map[string]string) {
	t.Helper()
	doc := &ModuleConfig{
		ModuleName:      name,
		Category:        CategoryCore,
		ConfigValues:    values,
		EncryptedValues: map[string]string{},
	}
	if err := repo.Upsert(context.Background(), doc); err != nil {
		t.Fatalf("seedModuleDoc: %v", err)
	}
}

// seedModuleDocWithEnv inserts a module_configs document carrying a single
// named environment profile with the given config values.
func seedModuleDocWithEnv(t *testing.T, repo *ModuleConfigRepository, name, envName string, values map[string]string) {
	t.Helper()
	doc := &ModuleConfig{
		ModuleName:      name,
		Category:        CategoryCore,
		ConfigValues:    map[string]string{},
		EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{
			envName: {
				ConfigValues:    values,
				EncryptedValues: map[string]string{},
				UpdatedAt:       time.Now(),
			},
		},
	}
	if err := repo.Upsert(context.Background(), doc); err != nil {
		t.Fatalf("seedModuleDocWithEnv: %v", err)
	}
}

func TestConfigUpdate_ModuleValidatorOptional(t *testing.T) {
	ctx := context.Background()

	// A module WITHOUT the seam is unaffected — any value persists.
	svcPlain, repoPlain := newTestConfigService(t)
	svcPlain.RegisterKnownModules([]Module{plainModule{}})
	seedModuleDoc(t, repoPlain, "plain", map[string]string{"anything": "old"})
	if err := svcPlain.UpdateConfig(ctx, "plain", map[string]string{"anything": "whatever"}, nil); err != nil {
		t.Fatalf("module without validator must keep today's behaviour: %v", err)
	}

	// A module WITH the seam rejects before persistence.
	svc, repo := newTestConfigService(t)
	vm := &validatingModule{}
	svc.RegisterKnownModules([]Module{vm})
	seedModuleDoc(t, repo, "validating", map[string]string{"strict": "good", "other": "keep"})

	err := svc.UpdateConfig(ctx, "validating", map[string]string{"strict": "bad"}, nil)
	var typed *ConfigValidationError
	if !errors.As(err, &typed) {
		t.Fatalf("UpdateConfig = %v, want *ConfigValidationError", err)
	}
	doc, _ := repo.FindByName(ctx, "validating")
	if doc.ConfigValues["strict"] != "good" {
		t.Errorf("rejected value reached persistence: %q", doc.ConfigValues["strict"])
	}

	// The validator sees the MERGED values, not just the PATCH body, so a
	// later cross-field rule cannot be bypassed with a partial PATCH.
	_ = svc.UpdateConfig(ctx, "validating", map[string]string{"strict": "fine"}, nil)
	if vm.seen["other"] != "keep" {
		t.Errorf("validator saw %v, want the merged document including untouched keys", vm.seen)
	}
}

func TestEnvironmentConfigUpdate_InvokesModuleValidator(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestConfigService(t)
	svc.RegisterKnownModules([]Module{&validatingModule{}})
	seedModuleDocWithEnv(t, repo, "validating", "sandbox", map[string]string{"strict": "good"})

	err := svc.UpdateEnvironmentConfig(ctx, "validating", "sandbox", map[string]string{"strict": "bad"}, nil)
	var typed *ConfigValidationError
	if !errors.As(err, &typed) {
		t.Fatalf("named-environment PATCH = %v, want *ConfigValidationError — the profile surface must not be a bypass", err)
	}
	doc, _ := repo.FindByName(ctx, "validating")
	if doc.Environments["sandbox"].ConfigValues["strict"] != "good" {
		t.Error("rejected value reached the environment profile")
	}
}

func TestSetActiveEnvironment_LegacyInvalidValueUsesDefensiveReader(t *testing.T) {
	// Activation must NOT reject a legacy invalid profile: the defensive
	// readers keep the deployment operable and the next edit repairs it.
	ctx := context.Background()
	svc, repo := newTestConfigService(t)
	svc.RegisterKnownModules([]Module{&validatingModule{}})
	seedModuleDocWithEnv(t, repo, "validating", "sandbox", map[string]string{"strict": "bad"})
	if err := svc.SetActiveEnvironment(ctx, "validating", "sandbox"); err != nil {
		t.Fatalf("SetActiveEnvironment must stay recoverable on legacy data: %v", err)
	}
}
