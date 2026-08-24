package module

import (
	"context"
	"testing"
)

func TestCompareAndSwapEnvironmentRejectsAStaleRevision(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = &ModuleConfig{
		ModuleName:        "demo",
		ActiveEnvironment: "production",
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{"a": "1"}, Revision: 4},
		},
	}

	next := EnvironmentConfig{ConfigValues: map[string]string{"a": "2"}}

	won, err := repo.CompareAndSwapEnvironment(context.Background(), "demo", "production", 3, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if won {
		t.Fatal("a stale revision won the CAS")
	}

	won, err = repo.CompareAndSwapEnvironment(context.Background(), "demo", "production", 4, next)
	if err != nil || !won {
		t.Fatalf("current revision lost the CAS: won=%v err=%v", won, err)
	}
	if got := repo.docs["demo"].Environments["production"].Revision; got != 5 {
		t.Fatalf("revision = %d, want 5", got)
	}
}

// A document written before this feature has no revision at all. Absent and 0
// must compare equal, or the first mutation on every pre-existing module fails
// against nothing.
func TestCompareAndSwapTreatsAbsentRevisionAsZero(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["legacy"] = &ModuleConfig{
		ModuleName:        "legacy",
		ActiveEnvironment: "production",
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{}}, // Revision zero-valued
		},
	}
	won, err := repo.CompareAndSwapEnvironment(context.Background(), "legacy", "production", 0,
		EnvironmentConfig{ConfigValues: map[string]string{"a": "1"}})
	if err != nil || !won {
		t.Fatalf("legacy document rejected an expected revision of 0: won=%v err=%v", won, err)
	}
}
