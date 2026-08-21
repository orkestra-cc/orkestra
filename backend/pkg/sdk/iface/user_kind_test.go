package iface

import "testing"

func TestToResponseCopiesKind(t *testing.T) {
	u := NewUser()
	u.Kind = UserKindService
	if got := u.ToResponse().Kind; got != UserKindService {
		t.Fatalf("ToResponse().Kind = %q, want %q", got, UserKindService)
	}
}

func TestNewUserDefaultsToHumanKind(t *testing.T) {
	if got := NewUser().Kind; got != "" {
		t.Fatalf("NewUser().Kind = %q, want empty (human)", got)
	}
}
