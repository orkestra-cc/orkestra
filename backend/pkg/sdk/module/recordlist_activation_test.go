package module

import (
	"context"
	"log/slog"
	"testing"
)

// Models the race the two-step activation leaves open: the caller reads the
// document, a removal lands, and the caller then copies the snapshot it read
// BEFORE the removal into the legacy top-level maps — putting the deleted
// secret back. duringActivate fires inside the activation write, which is
// exactly that window.
func TestActivationDoesNotResurrectARemovedSecret(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = &ModuleConfig{
		ModuleName:        "demo",
		ActiveEnvironment: "sandbox",
		ConfigValues:      map[string]string{},
		EncryptedValues:   map[string]string{},
		Environments: map[string]EnvironmentConfig{
			"sandbox": {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}},
			"production": {
				ConfigValues:    map[string]string{"x": "1", "email.profiles.__items": "a"},
				EncryptedValues: map[string]string{"email.profiles.a.password": "ciphertext"},
				Revision:        7,
			},
		},
	}
	// The concurrent removal: element "a" and its secret leave production.
	repo.duringActivate = func() {
		env := repo.docs["demo"].Environments["production"]
		delete(env.EncryptedValues, "email.profiles.a.password")
		delete(env.ConfigValues, "email.profiles.__items")
		repo.docs["demo"].Environments["production"] = env
	}

	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	if err := svc.SetActiveEnvironment(context.Background(), "demo", "production"); err != nil {
		t.Fatalf("SetActiveEnvironment: %v", err)
	}

	doc := repo.docs["demo"]
	if _, ok := doc.EncryptedValues["email.profiles.a.password"]; ok {
		t.Fatal("activation copied a stale snapshot and resurrected a removed secret")
	}
	if _, ok := doc.ConfigValues["email.profiles.__items"]; ok {
		t.Fatal("activation resurrected the roster of a removed element")
	}
	if doc.ConfigValues["x"] != "1" {
		t.Fatal("activation did not copy the newly active environment's values")
	}
	if doc.ActiveEnvironment != "production" {
		t.Fatalf("activeEnvironment = %q, want production", doc.ActiveEnvironment)
	}
}

// A failure to sync the legacy top-level maps used to be logged and swallowed,
// leaving activeEnvironment pointing at a profile whose values were never
// copied. The caller has to hear about that.
func TestActivationSurfacesAnUnknownEnvironment(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = &ModuleConfig{
		ModuleName:        "demo",
		ActiveEnvironment: "production",
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}},
		},
	}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())

	if err := svc.SetActiveEnvironment(context.Background(), "demo", "nope"); err == nil {
		t.Fatal("activating an environment that does not exist reported success")
	}
	if repo.docs["demo"].ActiveEnvironment != "production" {
		t.Fatal("a failed activation still moved activeEnvironment")
	}
}
