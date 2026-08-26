package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// testKeyHex is a 32-byte AES key for OAUTH_TOKEN_ENCRYPTION_KEY.
const testKeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

// encryptForTest produces ciphertext in the SDK's stored form
// (base64(nonce || AES-256-GCM ciphertext)). The SDK does not export its
// encryptor; if its scheme ever changes, decoding these fixtures fails
// loudly (cfg.Err), which is the right way for this test to break.
func encryptForTest(t *testing.T, plain string) string {
	t.Helper()
	key, _ := hex.DecodeString(testKeyHex)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil))
}

// legacyTestSchema is the subset of the flat schema these tests read; the
// real one is module.go's ConfigSchema (a key absent here stays zero).
func legacyTestSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "email.provider", Type: module.FieldEnum, Options: []string{"noop", "smtp"}, Default: "noop"},
		{Key: "email.smtp.host", Type: module.FieldString},
		{Key: "email.smtp.port", Type: module.FieldInt, Default: "587"},
		{Key: "email.smtp.password", Type: module.FieldSecret},
		{Key: "email.from_address", Type: module.FieldString},
	}
}

func twoEnvDoc(t *testing.T) *module.ModuleConfig {
	t.Helper()
	return &module.ModuleConfig{
		ConfigSchema:      legacyTestSchema(),
		ActiveEnvironment: "production",
		Environments: map[string]module.EnvironmentConfig{
			"production": {
				ConfigValues:    map[string]string{"email.provider": "smtp", "email.smtp.host": "prod.relay", "email.from_address": "p@example.com"},
				EncryptedValues: map[string]string{"email.smtp.password": encryptForTest(t, "prod-secret")},
			},
			"sandbox": {
				ConfigValues:    map[string]string{"email.provider": "smtp", "email.smtp.host": "sand.relay", "email.from_address": "s@example.com"},
				EncryptedValues: map[string]string{"email.smtp.password": encryptForTest(t, "sand-secret")},
			},
		},
	}
}

// TestSnapshotLoader_ValuesAndSecretsComeFromOneEnvironment simulates an
// environment switch between two loads. Each load must be internally
// consistent — host and password from the SAME environment — and the
// getter must be called exactly once per load, so there is no second read a
// switch could land between.
func TestSnapshotLoader_ValuesAndSecretsComeFromOneEnvironment(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", testKeyHex)
	doc := twoEnvDoc(t)
	calls := 0
	get := func(context.Context) (*module.ModuleConfig, error) {
		calls++
		if calls > 1 {
			doc.ActiveEnvironment = "sandbox" // the switch
		}
		return doc, nil
	}
	load := NewSnapshotLoader(get)

	first := load(context.Background())
	second := load(context.Background())
	if first.Err != nil || second.Err != nil {
		t.Fatalf("errs: %v %v", first.Err, second.Err)
	}
	if first.Legacy.SMTPHost != "prod.relay" || first.Legacy.SMTPPassword != "prod-secret" || first.Legacy.FromAddress != "p@example.com" {
		t.Fatalf("first load mixed environments: %+v", first.Legacy)
	}
	if second.Legacy.SMTPHost != "sand.relay" || second.Legacy.SMTPPassword != "sand-secret" {
		t.Fatalf("second load mixed environments: %+v", second.Legacy)
	}
	if first.Legacy.SMTPPort != 587 || first.Legacy.Slug != LegacySlug || first.Legacy.Provider != "smtp" {
		t.Fatalf("defaults/identity: %+v", first.Legacy)
	}
	if calls != 2 {
		t.Fatalf("one document read per load, got %d for 2 loads", calls)
	}
}

func TestSnapshotLoader_FailsClosed(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", testKeyHex)
	boom := errors.New("mongo down")
	if cfg := NewSnapshotLoader(func(context.Context) (*module.ModuleConfig, error) { return nil, boom })(context.Background()); !errors.Is(cfg.Err, boom) {
		t.Fatalf("getter error must surface as cfg.Err, got %v", cfg.Err)
	}
	doc := twoEnvDoc(t)
	env := doc.Environments["production"]
	env.EncryptedValues["email.smtp.password"] = "not-ciphertext"
	doc.Environments["production"] = env
	if cfg := NewSnapshotLoader(func(context.Context) (*module.ModuleConfig, error) { return doc, nil })(context.Background()); cfg.Err == nil {
		t.Fatal("undecryptable secret must fail closed, not degrade to an empty password")
	}
}

func TestSnapshotLoader_NoDocumentIsNoop(t *testing.T) {
	cfg := NewSnapshotLoader(func(context.Context) (*module.ModuleConfig, error) { return nil, nil })(context.Background())
	if cfg.Err != nil || cfg.Legacy.Provider != "noop" || cfg.Legacy.Slug != LegacySlug {
		t.Fatalf("no document ⇒ noop legacy profile, got %+v %v", cfg.Legacy, cfg.Err)
	}
	if cfg := NewSnapshotLoader(nil)(context.Background()); cfg.Legacy.Provider != "noop" {
		t.Fatalf("nil getter ⇒ noop, got %+v", cfg.Legacy)
	}
}
