package module

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// Activation no longer copies the target profile server-side: the service
// reads the document, strips the schema-secret keys the profile may still
// carry in plaintext, and writes that map itself. What makes the client-side
// copy safe is the revision guard — a write landing between the read and the
// update bumps configRevision, so the stale activation matches nothing and
// nothing it read can be republished. duringActivate models exactly that
// writer: it removes the element AND advances the revision, as any real
// write does.
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
	// The concurrent removal: element "a" and its secret leave production,
	// and the document moves on.
	repo.duringActivate = func() {
		doc := repo.docs["demo"]
		env := doc.Environments["production"]
		delete(env.EncryptedValues, "email.profiles.a.password")
		delete(env.ConfigValues, "email.profiles.__items")
		doc.Environments["production"] = env
		doc.ConfigRevision++
	}

	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	err := svc.SetActiveEnvironment(context.Background(), "demo", "production")
	if !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("SetActiveEnvironment = %v, want ErrRevisionStale", err)
	}

	doc := repo.docs["demo"]
	if _, ok := doc.EncryptedValues["email.profiles.a.password"]; ok {
		t.Fatal("a lost activation still copied its stale snapshot into the mirror")
	}
	if _, ok := doc.ConfigValues["email.profiles.__items"]; ok {
		t.Fatal("a lost activation resurrected the roster of a removed element")
	}
	if doc.ActiveEnvironment != "sandbox" {
		t.Fatalf("activeEnvironment = %q, want the untouched sandbox", doc.ActiveEnvironment)
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
