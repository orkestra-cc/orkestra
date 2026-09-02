package config

import (
	"strings"
	"testing"
)

// productionLikeConfig is the smallest Config that clears every other
// production check in Validate, so a failure in these tests is about the
// object-storage secret and nothing else.
func productionLikeConfig(env string) *Config {
	c := &Config{}
	c.Server.Environment = env
	c.Auth.JWT.KeysLoaded = true
	c.Auth.Cookie.Secure = true
	c.Auth.AllowLocalhostRedirects = false
	c.Auth.Google.ClientID = "client-id-for-tests"
	c.Auth.Google.ClientSecret = "client-secret-for-tests"
	return c
}

// On the bundled RustFS the backend's secret IS the store's root secret
// (docker-compose.infra.yml derives RUSTFS_SECRET_KEY from it) and the S3 API
// is browser-facing behind the proxy, so a placeholder left in docker/.env is
// the root password printed in a public repository. Production and staging
// must refuse to boot on one — the same rule scripts/env-validate.sh applies
// before a deploy, kept here for anyone who bypasses the scripts.
func TestValidate_ProductionLikeRefusesWeakStorageSecret(t *testing.T) {
	for _, env := range []string{"production", "staging"} {
		for _, secret := range []string{
			"changeme-rustfs", // the literal the compose files used to fall back to
			"REPLACE_WITH_RANDOM_HEX_32_STORAGE_SECRET", // an unfilled .env.example placeholder
			"rustfsadmin",  // the image's own default
			"RustFSAdmin",  // case must not matter
			"short-secret", // real-looking but under 16 characters
			"",             // a key id with no secret at all
		} {
			c := productionLikeConfig(env)
			c.Storage.AccessKey = "orkestra"
			c.Storage.SecretKey = secret
			err := c.Validate()
			if err == nil {
				t.Errorf("%s: Validate() accepted STORAGE_SECRET_KEY=%q", env, secret)
				continue
			}
			if !strings.Contains(err.Error(), "STORAGE_SECRET_KEY") {
				t.Errorf("%s: error for %q does not name the variable: %v", env, secret, err)
			}
		}
	}
}

func TestValidate_ProductionLikeAcceptsRealOrDisabledStorage(t *testing.T) {
	// A generated secret (`openssl rand -hex 16`).
	c := productionLikeConfig("production")
	c.Storage.AccessKey = "orkestra"
	c.Storage.SecretKey = "0123456789abcdef0123456789abcdef"
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() refused a generated secret: %v", err)
	}

	// Object storage disabled outright: both keys empty is a supported
	// production layout (the backend logs it at boot), not a weak secret.
	c = productionLikeConfig("production")
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() refused a deployment without object storage: %v", err)
	}
}

// Development keeps working with whatever is in a stale docker/.env — the
// validator warns there, the backend does not refuse.
func TestValidate_DevelopmentToleratesPlaceholderStorageSecret(t *testing.T) {
	c := &Config{}
	c.Server.Environment = "development"
	c.Storage.AccessKey = "orkestra"
	c.Storage.SecretKey = "changeme-rustfs"
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() in development refused a placeholder: %v", err)
	}
}

func TestWeakSecretReason(t *testing.T) {
	cases := map[string]string{
		"":                "is empty",
		"   ":             "is empty",
		"changeme":        "is a placeholder",
		"changeme-rustfs": "is a placeholder",
		"REPLACE_WITH_RANDOM_HEX_32_STORAGE_SECRET": "is a placeholder",
		"your_secret_here":                          "is a placeholder",
		"minioadmin":                                "is a placeholder",
		"dev-jwt-secret-key-change-in-production":   "is a placeholder",
		"abc123def456":                              "is shorter than 16 characters",
		"0123456789abcdef":                          "",
		"0123456789abcdef0123456789abcdef":          "",
	}
	for in, want := range cases {
		if got := weakSecretReason(in); got != want {
			t.Errorf("weakSecretReason(%q) = %q, want %q", in, got, want)
		}
	}
}
