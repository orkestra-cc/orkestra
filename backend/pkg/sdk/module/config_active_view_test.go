package module

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// activeViewModule is the "auth"-shaped module the view tests register so
// the LIVE schema (not the stored one) supplies fallbacks and secret-ness.
type activeViewModule struct{ BaseModule }

var activeViewSchema = []ConfigField{
	{Key: "googleClientId", Type: FieldString},
	{Key: "googleClientSecret", Type: FieldSecret},
	{Key: "googleRedirectURL", Type: FieldString, Default: "https://default.example/cb"},
	{Key: "applePrivateKey", Type: FieldSecret, EnvVar: "TEST_ACTIVE_VIEW_APPLE_KEY"},
	{Key: "googleEnabledAdmin", Type: FieldBool, Default: "false"},
}

func (activeViewModule) Name() string                { return "auth" }
func (activeViewModule) Init(*Dependencies) error    { return nil }
func (activeViewModule) ConfigSchema() []ConfigField { return activeViewSchema }

func newActiveViewService(t *testing.T) (*ModuleConfigService, *fakeConfigRepo) {
	t.Helper()
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{activeViewModule{}})
	return svc, repo
}

func activeViewDoc(values, encrypted map[string]string, revision int64) *ModuleConfig {
	return &ModuleConfig{
		ModuleName: "auth", ActiveEnvironment: "production",
		// A deliberately STALE stored schema: the live one must win.
		ConfigSchema: []ConfigField{{Key: "googleClientId", Type: FieldString}},
		ConfigValues: map[string]string{}, EncryptedValues: map[string]string{},
		ConfigRevision: revision,
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: values, EncryptedValues: encrypted},
			"sandbox":    {ConfigValues: map[string]string{"googleClientId": "sandbox-id"}, EncryptedValues: map[string]string{}},
		},
	}
}

func TestActiveConfigRequiredModule_MissingDocumentIsAnError(t *testing.T) {
	svc, _ := newActiveViewService(t)
	_, err := svc.ActiveConfigRequiredModule(context.Background(), "auth")
	if !errors.Is(err, ErrRequiredConfigMissing) {
		t.Fatalf("err = %v, want ErrRequiredConfigMissing", err)
	}
}

func TestActiveConfigRequiredModule_RepositoryErrorPropagates(t *testing.T) {
	svc, repo := newActiveViewService(t)
	repo.findErr = errors.New("mongo down")
	_, err := svc.ActiveConfigRequiredModule(context.Background(), "auth")
	if err == nil || !strings.Contains(err.Error(), "mongo down") {
		t.Fatalf("err = %v, want the repository error", err)
	}
}

func TestActiveConfigRequiredModule_UndecryptableSecretIsDocumentLevel(t *testing.T) {
	svc, repo := newActiveViewService(t)
	repo.docs["auth"] = activeViewDoc(map[string]string{"googleClientId": "id"}, map[string]string{"googleClientSecret": "not-base64-ciphertext!"}, 3)
	_, err := svc.ActiveConfigRequiredModule(context.Background(), "auth")
	if !errors.Is(err, ErrConfigSecretUnreadable) {
		t.Fatalf("err = %v, want ErrConfigSecretUnreadable", err)
	}
	if !strings.Contains(err.Error(), "googleClientSecret") {
		t.Fatalf("the error must name the key: %v", err)
	}
	if strings.Contains(err.Error(), "not-base64-ciphertext!") {
		t.Fatalf("the error must not carry the stored value: %v", err)
	}
}

func TestActiveConfigRequiredModule_ViewSemantics(t *testing.T) {
	svc, repo := newActiveViewService(t)
	t.Setenv("TEST_ACTIVE_VIEW_APPLE_KEY", "env-pem")
	secret, _ := encryptSecret("shh")
	repo.docs["auth"] = activeViewDoc(
		map[string]string{
			"googleClientId":     "live-id",
			"googleRedirectURL":  "", // present but empty → Effective falls back to the schema Default
			"googleClientSecret": "plaintext-that-must-be-stripped",
			"googleEnabledAdmin": "true",
		},
		map[string]string{"googleClientSecret": secret, "applePrivateKey": ""},
		7,
	)
	view, err := svc.ActiveConfigRequiredModule(context.Background(), "auth")
	if err != nil {
		t.Fatal(err)
	}
	if view.Module() != "auth" || view.Revision() != 7 {
		t.Fatalf("module/revision = %q/%d", view.Module(), view.Revision())
	}
	if v, ok := view.Raw("googleClientId"); !ok || v != "live-id" {
		t.Fatalf("Raw(googleClientId) = %q,%v", v, ok)
	}
	if _, ok := view.Raw("googleEnabledClient"); ok {
		t.Fatal("an absent key must report absent, never a default")
	}
	if v, ok := view.Raw("googleRedirectURL"); !ok || v != "" {
		t.Fatalf("Raw must preserve present-but-empty: %q,%v", v, ok)
	}
	if got := view.Effective("googleRedirectURL"); got != "https://default.example/cb" {
		t.Fatalf("Effective(googleRedirectURL) = %q, want the LIVE schema default", got)
	}
	if got := view.Effective("googleClientId"); got != "live-id" {
		t.Fatalf("Effective(googleClientId) = %q", got)
	}
	if _, ok := view.Raw("googleClientSecret"); ok {
		t.Fatal("a plaintext under a schema-secret key must be stripped from the non-secret view")
	}
	if got := view.Secret("googleClientSecret"); got != "shh" {
		t.Fatalf("Secret(googleClientSecret) = %q, want the decrypted stored value", got)
	}
	if !view.SecretPresent("googleClientSecret") {
		t.Fatal("a non-empty decrypted secret is present")
	}
	if got := view.Secret("applePrivateKey"); got != "env-pem" {
		t.Fatalf("an empty stored ciphertext must fall back to EnvVar/Default like GetSecret: got %q", got)
	}
	if view.SecretPresent("githubClientSecret") {
		t.Fatal("an undeclared, unstored secret is absent")
	}
	if got := view.Effective("googleClientSecret"); got != "" {
		t.Fatalf("Effective must never surface a secret: %q", got)
	}
}

func TestActiveConfigRequiredModule_ReadsTheActiveProfileOnly(t *testing.T) {
	svc, repo := newActiveViewService(t)
	doc := activeViewDoc(map[string]string{"googleClientId": "prod-id"}, map[string]string{}, 1)
	doc.ActiveEnvironment = "sandbox"
	repo.docs["auth"] = doc
	view, err := svc.ActiveConfigRequiredModule(context.Background(), "auth")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := view.Raw("googleClientId"); v != "sandbox-id" {
		t.Fatalf("Raw(googleClientId) = %q, want the ACTIVE profile's value", v)
	}
}

func TestNewActiveConfigView_StripsSecretsAndCopies(t *testing.T) {
	values := map[string]string{"googleClientId": "id", "googleClientSecret": "leak"}
	secrets := map[string]string{"googleClientSecret": "shh"}
	view := NewActiveConfigView("auth", activeViewSchema, values, secrets, 0)
	if _, ok := view.Raw("googleClientSecret"); ok {
		t.Fatal("constructor must strip schema-secret keys from values")
	}
	secrets["googleClientSecret"] = "mutated"
	if view.Secret("googleClientSecret") != "shh" {
		t.Fatal("the view must own a copy of the secrets map")
	}
}
