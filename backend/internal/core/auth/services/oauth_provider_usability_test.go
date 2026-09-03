package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func TestProviderStructurallyConfigured(t *testing.T) {
	full := ProviderStructuralFields{ClientID: "id", RedirectURL: "https://x/cb", SecretPresent: true, TeamID: "team", KeyID: "key"}
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	cases := []struct {
		name    string
		p       models.OAuthProvider
		f       ProviderStructuralFields
		probe   KeyFileProbe
		missing string
	}{
		{"google complete", models.OAuthProviderGoogle, full, no, ""},
		{"google no client id", models.OAuthProviderGoogle, ProviderStructuralFields{RedirectURL: "u", SecretPresent: true}, no, "googleClientId"},
		{"github no redirect", models.OAuthProviderGitHub, ProviderStructuralFields{ClientID: "id", SecretPresent: true}, no, "githubRedirectURL"},
		{"discord no secret", models.OAuthProviderDiscord, ProviderStructuralFields{ClientID: "id", RedirectURL: "u"}, no, "discordClientSecret"},
		{"apple inline key", models.OAuthProviderApple, full, no, ""},
		{"apple path-backed key", models.OAuthProviderApple, ProviderStructuralFields{ClientID: "id", RedirectURL: "u", TeamID: "t", KeyID: "k", PrivateKeyPath: "/k.p8"}, yes, ""},
		{"apple unreadable path and no inline key", models.OAuthProviderApple, ProviderStructuralFields{ClientID: "id", RedirectURL: "u", TeamID: "t", KeyID: "k", PrivateKeyPath: "/k.p8"}, no, "applePrivateKey"},
		{"apple nil probe never counts a path", models.OAuthProviderApple, ProviderStructuralFields{ClientID: "id", RedirectURL: "u", TeamID: "t", KeyID: "k", PrivateKeyPath: "/k.p8"}, nil, "applePrivateKey"},
		{"apple no team", models.OAuthProviderApple, ProviderStructuralFields{ClientID: "id", RedirectURL: "u", KeyID: "k", SecretPresent: true}, no, "appleTeamId"},
		{"apple no key id", models.OAuthProviderApple, ProviderStructuralFields{ClientID: "id", RedirectURL: "u", TeamID: "t", SecretPresent: true}, no, "appleKeyId"},
		{"unknown provider", models.OAuthProvider("facebook"), full, yes, "provider"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			missing, ok := ProviderStructurallyConfigured(tc.p, tc.f, tc.probe)
			if ok != (tc.missing == "") || missing != tc.missing {
				t.Fatalf("got missing=%q ok=%v, want missing=%q", missing, ok, tc.missing)
			}
		})
	}
}

func TestReadableNonEmptyFile(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "key.p8")
	if err := os.WriteFile(full, []byte("-----BEGIN PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.p8")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !ReadableNonEmptyFile(full) {
		t.Error("a readable non-empty regular file counts")
	}
	if ReadableNonEmptyFile(empty) {
		t.Error("an empty file does not count")
	}
	if ReadableNonEmptyFile(dir) {
		t.Error("a directory does not count")
	}
	if ReadableNonEmptyFile(filepath.Join(dir, "missing.p8")) {
		t.Error("a missing file does not count")
	}
	if ReadableNonEmptyFile("") {
		t.Error("an empty path does not count")
	}
}

// fakeActiveConfigReader hands the resolver a prebuilt view (or an error).
type fakeActiveConfigReader struct {
	view *module.ActiveConfigView
	err  error
}

func (f *fakeActiveConfigReader) GetValue(context.Context, string, string) string  { return "" }
func (f *fakeActiveConfigReader) GetSecret(context.Context, string, string) string { return "" }
func (f *fakeActiveConfigReader) ActiveConfigRequiredModule(context.Context, string) (*module.ActiveConfigView, error) {
	return f.view, f.err
}

var usabilitySchema = []module.ConfigField{
	{Key: "googleEnabledAdmin", Type: module.FieldBool, Default: "false"},
	{Key: "googleEnabledClient", Type: module.FieldBool, Default: "false"},
	{Key: "githubEnabledAdmin", Type: module.FieldBool, Default: "false"},
	{Key: "appleEnabledAdmin", Type: module.FieldBool, Default: "false"},
	{Key: "discordEnabledAdmin", Type: module.FieldBool, Default: "false"},
	{Key: "googleClientId", Type: module.FieldString},
	{Key: "googleClientSecret", Type: module.FieldSecret},
	{Key: "googleRedirectURL", Type: module.FieldString},
	{Key: "githubClientId", Type: module.FieldString},
	{Key: "githubClientSecret", Type: module.FieldSecret},
	{Key: "githubRedirectURL", Type: module.FieldString},
	{Key: "appleClientId", Type: module.FieldString},
	{Key: "appleTeamId", Type: module.FieldString},
	{Key: "appleKeyId", Type: module.FieldString},
	{Key: "applePrivateKey", Type: module.FieldSecret},
	{Key: "applePrivateKeyPath", Type: module.FieldString},
	{Key: "appleRedirectURL", Type: module.FieldString},
}

func usabilityResolver(view *module.ActiveConfigView, err error, probe KeyFileProbe) (*OAuthConfigResolver, *bytes.Buffer) {
	var buf bytes.Buffer
	r := &OAuthConfigResolver{
		cs:     &fakeActiveConfigReader{view: view, err: err},
		logger: slog.New(slog.NewTextHandler(&buf, nil)),
		probe:  probe,
	}
	return r, &buf
}

func googleView(values map[string]string, secrets map[string]string) *module.ActiveConfigView {
	base := map[string]string{"googleEnabledAdmin": "true", "googleClientId": "g-id", "googleRedirectURL": "https://console/cb"}
	for k, v := range values {
		base[k] = v
	}
	sec := map[string]string{"googleClientSecret": "g-secret-value"}
	for k, v := range secrets {
		sec[k] = v
	}
	return module.NewActiveConfigView("auth", usabilitySchema, base, sec, 1)
}

func TestOAuthWebProviderUsable_DocumentLevelFailureIsAnError(t *testing.T) {
	r, _ := usabilityResolver(nil, errors.New("mongo down"), nil)
	_, _, err := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderGoogle)
	if !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("err = %v, want ErrAuthPolicyUnavailable", err)
	}
	if _, err := r.UsableWebProviders(context.Background(), PolicyAudienceOperator); !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("list: err = %v, want ErrAuthPolicyUnavailable", err)
	}
	var nilResolver *OAuthConfigResolver
	if _, _, err := nilResolver.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderGoogle); !errors.Is(err, ErrAuthPolicyUnavailable) {
		t.Fatalf("nil resolver: err = %v", err)
	}
}

func TestOAuthWebProviderUsable_UsableProviderReturnsResolvedConfig(t *testing.T) {
	r, _ := usabilityResolver(googleView(nil, nil), nil, nil)
	cfg, ok, err := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderGoogle)
	if err != nil || !ok || cfg == nil {
		t.Fatalf("got cfg=%v ok=%v err=%v", cfg, ok, err)
	}
	if cfg.ClientID != "g-id" || cfg.ClientSecret != "g-secret-value" || cfg.AdditionalConfig["redirect_url"] != "https://console/cb" {
		t.Fatalf("config must be built from the SAME view: %+v", cfg)
	}
}

func TestOAuthWebProviderUsable_PerProviderDefectsAreNotErrors(t *testing.T) {
	structural := map[string]string{"googleClientId": "g-id", "googleRedirectURL": "https://console/cb"}
	secret := map[string]string{"googleClientSecret": "g-secret-value"}
	cases := []struct {
		name    string
		view    *module.ActiveConfigView
		wantKey string // expected in the WARN; "" = no WARN at all
	}{
		{"absent toggle → schema default false, no WARN", module.NewActiveConfigView("auth", usabilitySchema, structural, secret, 1), ""},
		{"toggle false", googleView(map[string]string{"googleEnabledAdmin": "false"}, nil), ""},
		{"malformed toggle names the key", googleView(map[string]string{"googleEnabledAdmin": "treu"}, nil), "googleEnabledAdmin"},
		{"readBool-style '1' is malformed", googleView(map[string]string{"googleEnabledAdmin": "1"}, nil), "googleEnabledAdmin"},
		{"present-empty toggle is malformed", googleView(map[string]string{"googleEnabledAdmin": ""}, nil), "googleEnabledAdmin"},
		{"missing client id", googleView(map[string]string{"googleClientId": ""}, nil), "googleClientId"},
		{"missing redirect", googleView(map[string]string{"googleRedirectURL": ""}, nil), "googleRedirectURL"},
		{"missing secret", googleView(nil, map[string]string{"googleClientSecret": ""}), "googleClientSecret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, logs := usabilityResolver(tc.view, nil, nil)
			cfg, ok, err := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderGoogle)
			if err != nil || ok || cfg != nil {
				t.Fatalf("got cfg=%v ok=%v err=%v; want (nil,false,nil)", cfg, ok, err)
			}
			if tc.wantKey == "" {
				if strings.Contains(logs.String(), "level=WARN") {
					t.Fatalf("no WARN expected: %s", logs.String())
				}
				return
			}
			if !strings.Contains(logs.String(), tc.wantKey) {
				t.Fatalf("WARN must name %q: %s", tc.wantKey, logs.String())
			}
			if strings.Contains(logs.String(), "g-secret-value") || strings.Contains(logs.String(), "g-id") {
				t.Fatalf("WARN must carry key names only: %s", logs.String())
			}
		})
	}
}

func TestOAuthWebProviderUsable_AudienceIsolation(t *testing.T) {
	view := googleView(map[string]string{"googleEnabledAdmin": "false", "googleEnabledClient": "true"}, nil)
	r, _ := usabilityResolver(view, nil, nil)
	if _, ok, _ := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderGoogle); ok {
		t.Fatal("operator surface must be off")
	}
	if _, ok, _ := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceClient, models.OAuthProviderGoogle); !ok {
		t.Fatal("client surface must be on")
	}
}

func TestOAuthWebProviderUsable_ApplePathBackedKeyUsesProbe(t *testing.T) {
	values := map[string]string{
		"appleEnabledAdmin": "true", "appleClientId": "a-id", "appleTeamId": "team", "appleKeyId": "key",
		"appleRedirectURL": "https://console/apple", "applePrivateKeyPath": "/etc/apple/key.p8",
	}
	view := module.NewActiveConfigView("auth", usabilitySchema, values, nil, 1)
	var probed string
	r, _ := usabilityResolver(view, nil, func(p string) bool { probed = p; return true })
	cfg, ok, err := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderApple)
	if err != nil || !ok {
		t.Fatalf("got ok=%v err=%v", ok, err)
	}
	if probed != "/etc/apple/key.p8" || cfg.AdditionalConfig["private_key_path"] != "/etc/apple/key.p8" {
		t.Fatalf("probe path %q, cfg %+v", probed, cfg.AdditionalConfig)
	}
	r, logs := usabilityResolver(view, nil, func(string) bool { return false })
	if _, ok, _ := r.OAuthWebProviderUsable(context.Background(), PolicyAudienceOperator, models.OAuthProviderApple); ok {
		t.Fatal("unreadable key file must make apple unusable")
	}
	if !strings.Contains(logs.String(), "applePrivateKey") {
		t.Fatalf("WARN must name applePrivateKey: %s", logs.String())
	}
}

func TestUsableWebProviders_ListsOnlyUsableInCanonicalOrder(t *testing.T) {
	values := map[string]string{
		"discordEnabledAdmin": "true", // enabled but no client id → omitted
		"githubEnabledAdmin":  "treu", // malformed → omitted, WARN
		"googleEnabledAdmin":  "true", "googleClientId": "g", "googleRedirectURL": "u",
		"appleEnabledAdmin": "true", "appleClientId": "a", "appleTeamId": "t", "appleKeyId": "k", "appleRedirectURL": "u",
	}
	secrets := map[string]string{"googleClientSecret": "s", "applePrivateKey": "pem"}
	r, logs := usabilityResolver(module.NewActiveConfigView("auth", usabilitySchema, values, secrets, 1), nil, nil)
	got, err := r.UsableWebProviders(context.Background(), PolicyAudienceOperator)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != models.OAuthProviderGoogle || got[1] != models.OAuthProviderApple {
		t.Fatalf("got %v, want [google apple]", got)
	}
	if !strings.Contains(logs.String(), "githubEnabledAdmin") || !strings.Contains(logs.String(), "discordClientId") {
		t.Fatalf("WARNs must name both defects: %s", logs.String())
	}
}
