package module

import (
	"context"
	"log/slog"
	"testing"
)

func TestSecretStatusCoversRecordListElements(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = &ModuleConfig{
		ModuleName:        "demo",
		ActiveEnvironment: "production",
		ConfigSchema: []ConfigField{
			{Key: "apiKey", Type: FieldSecret},
			{Key: "email.profiles", Type: FieldRecordList, Items: []ConfigItemField{
				{Key: "host", Type: FieldString},
				{Key: "password", Type: FieldSecret},
			}},
		},
		Environments: map[string]EnvironmentConfig{
			"production": {
				ConfigValues:    map[string]string{"email.profiles.__items": "a,b"},
				EncryptedValues: map[string]string{"email.profiles.a.password": "ciphertext"},
			},
		},
	}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())

	_, status, err := svc.GetEnvironmentConfig(context.Background(), "demo", "production")
	if err != nil {
		t.Fatalf("GetEnvironmentConfig: %v", err)
	}
	if !status["email.profiles.a.password"] {
		t.Error("a stored element secret reported as unset")
	}
	if _, ok := status["email.profiles.b.password"]; !ok {
		t.Error("an unset element secret is missing from the map entirely")
	}
	if status["email.profiles.b.password"] {
		t.Error("an unstored element secret reported as set")
	}
	if _, ok := status["email.profiles.a.host"]; ok {
		t.Error("a non-secret sub-field leaked into secretStatus")
	}
}
