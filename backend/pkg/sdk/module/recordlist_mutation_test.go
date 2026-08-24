package module

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func svcWith(t *testing.T, values, encrypted map[string]string) (*ModuleConfigService, *fakeConfigRepo) {
	t.Helper()
	repo := newFakeConfigRepo()
	repo.docs["demo"] = &ModuleConfig{
		ModuleName:        "demo",
		ActiveEnvironment: "production",
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: values, EncryptedValues: encrypted, Revision: 1},
		},
	}
	return NewModuleConfigService(repo, fakeRedisClient{}, slog.Default()), repo
}

func rev(v int64) *int64 { return &v }

func TestRemovalDropsEveryKeyIncludingTheSecret(t *testing.T) {
	svc, repo := svcWith(t,
		map[string]string{
			"email.profiles.__items":   "a,a-b",
			"email.profiles.a.host":    "smtp.example",
			"email.profiles.a.__label": "A",
			"email.profiles.a-b.host":  "other.example",
		},
		map[string]string{"email.profiles.a.password": "ciphertext"},
	)

	err := svc.UpdateEnvironmentConfigWithRecordLists(context.Background(), "demo", "production",
		nil, nil, []RecordListMutation{{Field: "email.profiles", Remove: []string{"a"}}}, rev(1))
	if err != nil {
		t.Fatalf("removal failed: %v", err)
	}

	env := repo.docs["demo"].Environments["production"]
	if _, ok := env.EncryptedValues["email.profiles.a.password"]; ok {
		t.Fatal("removal stranded the encrypted secret")
	}
	if _, ok := env.ConfigValues["email.profiles.a.host"]; ok {
		t.Fatal("removal left a value key behind")
	}
	if env.ConfigValues["email.profiles.a-b.host"] != "other.example" {
		t.Fatal("removal of a swallowed the sibling a-b")
	}
	if env.ConfigValues["email.profiles.__items"] != "a-b" {
		t.Fatalf("roster = %q, want \"a-b\"", env.ConfigValues["email.profiles.__items"])
	}
}

func TestRemovalRequiresARevision(t *testing.T) {
	svc, _ := svcWith(t, map[string]string{"email.profiles.__items": "a"}, nil)
	err := svc.UpdateEnvironmentConfigWithRecordLists(context.Background(), "demo", "production",
		nil, nil, []RecordListMutation{{Field: "email.profiles", Remove: []string{"a"}}}, nil)
	if !errors.Is(err, ErrRevisionRequired) {
		t.Fatalf("got %v, want ErrRevisionRequired", err)
	}
}

// An explicit zero is a legitimate expectation for a pre-feature document and
// must not be confused with an omitted revision.
func TestRemovalAcceptsAnExplicitZeroRevision(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = &ModuleConfig{
		ModuleName:        "demo",
		ActiveEnvironment: "production",
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{"email.profiles.__items": "a"}},
		},
	}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	if err := svc.UpdateEnvironmentConfigWithRecordLists(context.Background(), "demo", "production",
		nil, nil, []RecordListMutation{{Field: "email.profiles", Remove: []string{"a"}}}, rev(0)); err != nil {
		t.Fatalf("explicit zero rejected: %v", err)
	}
}

func TestRemovalWithAStaleRevisionIsRejectedAndChangesNothing(t *testing.T) {
	svc, repo := svcWith(t, map[string]string{"email.profiles.__items": "a"},
		map[string]string{"email.profiles.a.password": "ciphertext"})

	err := svc.UpdateEnvironmentConfigWithRecordLists(context.Background(), "demo", "production",
		nil, nil, []RecordListMutation{{Field: "email.profiles", Remove: []string{"a"}}}, rev(99))
	if !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("got %v, want ErrRevisionStale", err)
	}
	if _, ok := repo.docs["demo"].Environments["production"].EncryptedValues["email.profiles.a.password"]; !ok {
		t.Fatal("a rejected removal still destroyed the secret")
	}
}

// Two operators each adding one element: both must survive. The loser of the
// CAS retries against the refreshed roster instead of failing.
func TestConcurrentAddsBothSurvive(t *testing.T) {
	svc, repo := svcWith(t, map[string]string{"email.profiles.__items": "a"}, nil)
	repo.casFailures = 1 // lose the first attempt, as if another add landed

	err := svc.UpdateEnvironmentConfigWithRecordLists(context.Background(), "demo", "production",
		map[string]string{"email.profiles.b.host": "b.example"},
		nil, []RecordListMutation{{Field: "email.profiles", Create: []string{"b"}}}, nil)
	if err != nil {
		t.Fatalf("add did not retry through a lost CAS: %v", err)
	}
	if repo.casCalls < 2 {
		t.Fatalf("expected a retry, saw %d CAS calls", repo.casCalls)
	}
	if got := repo.docs["demo"].Environments["production"].ConfigValues["email.profiles.__items"]; got != "a,b" {
		t.Fatalf("roster = %q, want \"a,b\"", got)
	}
}

func TestDuplicateMutationFieldIsRejected(t *testing.T) {
	svc, _ := svcWith(t, map[string]string{}, nil)
	err := svc.UpdateEnvironmentConfigWithRecordLists(context.Background(), "demo", "production",
		nil, nil, []RecordListMutation{
			{Field: "email.profiles", Create: []string{"a"}},
			{Field: "email.profiles", Create: []string{"b"}},
		}, nil)
	if !errors.Is(err, ErrDuplicateMutationField) {
		t.Fatalf("got %v, want ErrDuplicateMutationField", err)
	}
}

// Membership is explicit intent. A client that writes the SDK-owned roster key
// straight into `config` would otherwise bypass every precondition — inventing
// members, or orphaning an element's values by dropping it from the roster
// without removing its keys.
func TestClientSuppliedRosterKeyIsIgnored(t *testing.T) {
	svc, repo := svcWith(t, map[string]string{"email.profiles.__items": "a"}, nil)

	err := svc.UpdateEnvironmentConfigWithRecordLists(context.Background(), "demo", "production",
		map[string]string{"email.profiles.__items": "a,forged", "other": "1"},
		nil, nil, nil)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	env := repo.docs["demo"].Environments["production"]
	if got := env.ConfigValues["email.profiles.__items"]; got != "a" {
		t.Fatalf("roster = %q, want the stored \"a\" — a forged roster was persisted", got)
	}
	if env.ConfigValues["other"] != "1" {
		t.Fatal("stripping the roster key also dropped an ordinary value")
	}
}
